#!/usr/bin/env python3
"""Build the Go application against a freshly patched, pinned MyGo vendor tree."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import plistlib
import shlex
import shutil
import subprocess
import zipfile


ROOT = Path(__file__).resolve().parents[1]
BUILD = ROOT / ".build/tvos"
PATCHES = ROOT / "patch/mygo"
DEPENDENCY = ROOT / "vendor/github.com/egoist/mygo"
APP = BUILD / "Payload/MikuTV.app"
# layerutil reads the scale suffix from layered PSD filenames.
APP_ICON = "MikuTVHomeIcon@2x"
MINIMUM = "18.0"
APP_VERSION = "0.1.0"


def run(args, *, env=None):
    subprocess.run(args, cwd=ROOT, env=env, check=True)


def output(args):
    return subprocess.check_output(args, cwd=ROOT, text=True).strip()


def prepare():
    base = json.loads((PATCHES / "base.json").read_text())
    environment = os.environ.copy()
    environment.update(GOWORK="off", GOFLAGS="-mod=mod")
    modules = json.loads(subprocess.check_output(
        ["go", "list", "-m", "-json", base["module"]], cwd=ROOT, env=environment,
    ))
    if modules["Version"] != base["version"] or modules.get("Replace"):
        raise RuntimeError("MyGo must match patch/mygo/base.json without a replacement")
    run(["go", "mod", "download"], env=environment)
    run(["go", "mod", "vendor"], env=environment)
    patches = sorted(PATCHES.glob("[0-9][0-9][0-9][0-9]-*.patch"))
    if not patches:
        raise RuntimeError("No numbered MyGo patches found")
    for patch in patches:
        arguments = ["git", "apply", "--whitespace=error",
                     "--directory=vendor/github.com/egoist/mygo"]
        run([*arguments, str(patch)])
    # Go 1.27 also checks the vendored package index for packages added by patches.
    index = ROOT / "vendor/modules.txt"
    entries = index.read_text().splitlines()
    module = f"# {base['module']} {base['version']}"
    start = entries.index(module) + 1
    end = next((i for i in range(start, len(entries))
                if entries[i].startswith("# ")), len(entries))
    package = base["module"] + "/internal/tvos"
    if package not in entries[start:end]:
        entries.insert(end, package)
    index.write_text("\n".join(entries) + "\n")
    BUILD.mkdir(parents=True, exist_ok=True)


def build():
    number = os.environ.get("BUILD_NUMBER", "1")
    if not number.isascii() or not number.isdecimal() or int(number) < 1:
        raise RuntimeError("BUILD_NUMBER must be a positive integer")
    prepare()
    sdk = output(["xcrun", "--sdk", "appletvos", "--show-sdk-path"])
    clang = output(["xcrun", "--sdk", "appletvos", "--find", "clang"])
    metal = DEPENDENCY / "internal/gpu/metal"
    run(["xcrun", "--sdk", "appletvos", "metal", "-c", str(metal / "shader.metal"),
         "-o", str(BUILD / "shader.air"), f"-mtvos-version-min={MINIMUM}"])
    run(["xcrun", "--sdk", "appletvos", "metallib", str(BUILD / "shader.air"),
         "-o", str(metal / "shader_tvos.metallib")])
    environment = os.environ.copy()
    environment.update(
        GOOS="ios", GOARCH="arm64", CGO_ENABLED="1", GOWORK="off", GOFLAGS="-mod=vendor",
        SDKROOT=sdk, CGO_CFLAGS="", CGO_CPPFLAGS="", CGO_CXXFLAGS="", CGO_LDFLAGS="",
        CC=shlex.join([clang, "-target", f"arm64-apple-tvos{MINIMUM}", "-isysroot", sdk]),
    )
    if APP.exists():
        shutil.rmtree(APP)
    APP.mkdir(parents=True)
    run(["go", "build", "-trimpath", "-tags=tvos,mygo_noinspector",
         f"-ldflags=-s -w -X main.appVersion={APP_VERSION}",
         "-o", str(APP / "MikuTV"), "."], env=environment)
    info = {
        "CFBundleDevelopmentRegion": "en", "CFBundleDisplayName": "MikuTV",
        "CFBundleExecutable": "MikuTV", "CFBundleIdentifier": "dev.yuchanns.MikuTV",
        "CFBundleInfoDictionaryVersion": "6.0", "CFBundleName": "MikuTV",
        "CFBundleIcons": {"CFBundlePrimaryIcon": APP_ICON},
        "CFBundlePackageType": "APPL", "CFBundleShortVersionString": APP_VERSION,
        "CFBundleVersion": number, "CFBundleSupportedPlatforms": ["AppleTVOS"],
        "UIDeviceFamily": [3], "MinimumOSVersion": MINIMUM,
        "LSRequiresIPhoneOS": True, "UILaunchScreen": {},
        "NSAppTransportSecurity": {"NSExceptionDomains": {"127.0.0.1": {"NSExceptionAllowsInsecureHTTPLoads": True}}},
    }
    (APP / "Info.plist").write_bytes(plistlib.dumps(info))
    (APP / "PkgInfo").write_bytes(b"APPL????")
    shutil.copyfile(ROOT / "resources/PrivacyInfo.xcprivacy", APP / "PrivacyInfo.xcprivacy")
    icon = ROOT / "resources/tvos" / f"{APP_ICON}.psd"
    run(["xcrun", "--sdk", "appletvos", "layerutil", "-c", "-l", "1",
         "-o", str(APP / "Assets.car"), str(icon)])
    bundle_plugins()
    print(APP)


def bundle_plugins():
    destination = APP / "assets/plugins"
    destination.mkdir(parents=True)
    records = []
    for manifest_file in sorted((ROOT / "plugins").glob("*/plugin.json")):
        package = manifest_file.parent
        manifest = json.loads(manifest_file.read_text())
        plugin_id = manifest["id"]
        if plugin_id != package.name:
            raise RuntimeError("The plugin ID must match its source directory")
        target = destination / plugin_id / "plugin.zip"
        target.parent.mkdir()
        source_path = str(package.relative_to(ROOT))
        listed = output(["git", "ls-files", "-z", "--cached", "--others",
                         "--exclude-standard", "--", source_path])
        package_files = sorted(set(listed.rstrip("\0").split("\0")))
        with zipfile.ZipFile(target, "w") as archive:
            for name in package_files:
                path = ROOT / name
                if path.is_symlink():
                    raise RuntimeError("A bundled plugin cannot contain a symbolic link")
                if path.is_file():
                    info = zipfile.ZipInfo(path.relative_to(package).as_posix())
                    info.create_system = 3
                    info.external_attr = 0o100644 << 16
                    archive.writestr(info, path.read_bytes(), compress_type=zipfile.ZIP_DEFLATED)
        record = {
            "id": plugin_id,
            "version": manifest["version"],
            "appVersion": APP_VERSION,
            "sha256": hashlib.sha256(target.read_bytes()).hexdigest(),
        }
        if not output(["git", "status", "--porcelain", "--untracked-files=all", "--", source_path]):
            commit = output(["git", "log", "-1", "--format=%H", "--", source_path])
            if commit:
                record["sourceCommit"] = commit
        records.append(record)
    (destination / "index.json").write_text(json.dumps(records, indent=2) + "\n")


def package():
    build()
    target = BUILD / "MikuTV-unsigned.ipa"
    with zipfile.ZipFile(target, "w", compression=zipfile.ZIP_DEFLATED) as archive:
        for path in sorted(APP.rglob("*")):
            if path.is_file():
                archive.write(path, path.relative_to(BUILD))
    print(target)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=["prepare", "build", "package"])
    args = parser.parse_args()
    {"prepare": prepare, "build": build, "package": package}[args.action]()
