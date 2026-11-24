#!/usr/bin/env python3
"""Build the Go application against a freshly patched, pinned MyGo vendor tree."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import plistlib
import re
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
APP_VERSION = "0.1.1"
VERSION_PATTERN = re.compile(r"^\d+\.\d+\.\d+$")


def resolve_app_version():
    version = os.environ.get("APP_VERSION", APP_VERSION).strip()
    if version.startswith(("v", "V")):
        version = version[1:]
    if not VERSION_PATTERN.fullmatch(version):
        raise RuntimeError(f"APP_VERSION must be semantic X.Y.Z, got {version!r}")
    return version


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
    app_version = resolve_app_version()
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
    commit = ""
    try:
        commit = output(["git", "rev-parse", "--short=12", "HEAD"])
    except subprocess.CalledProcessError:
        commit = ""
    ldflags = f"-s -w -X main.appVersion={app_version} -X main.appBuild={number}"
    if commit:
        ldflags += f" -X main.appCommit={commit}"
    run(["go", "build", "-trimpath", "-tags=tvos,mygo_noinspector",
         f"-ldflags={ldflags}",
         "-o", str(APP / "MikuTV"), "."], env=environment)
    info = {
        "CFBundleDevelopmentRegion": "en", "CFBundleDisplayName": "MikuTV",
        "CFBundleExecutable": "MikuTV", "CFBundleIdentifier": "dev.yuchanns.MikuTV",
        "CFBundleInfoDictionaryVersion": "6.0", "CFBundleName": "MikuTV",
        "CFBundleIcons": {"CFBundlePrimaryIcon": APP_ICON},
        "CFBundlePackageType": "APPL", "CFBundleShortVersionString": app_version,
        "CFBundleVersion": number, "CFBundleSupportedPlatforms": ["AppleTVOS"],
        "CFBundleLocalizations": ["zh-Hans", "en"],
        "LSApplicationCategoryType": "public.app-category.entertainment",
        "CFBundleURLTypes": [{
            "CFBundleTypeRole": "Viewer",
            "CFBundleURLName": "dev.yuchanns.MikuTV",
            "CFBundleURLSchemes": ["mikutv"],
        }],
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
    bundle_topshelf(environment, app_version, number)
    bundle_plugins(app_version)
    bundle_licenses(environment)
    print(APP)


def bundle_topshelf(build_env, app_version, number):
    ext = APP / "PlugIns/TopShelf.appex"
    ext.mkdir(parents=True, exist_ok=True)
    run(["go", "build", "-trimpath", "-tags=tvos,mygo_noinspector",
         "-ldflags=-s -w",
         "-o", str(ext / "TopShelf"), "./cmd/topshelf"], env=build_env)
    ext_info = {
        "CFBundleDevelopmentRegion": "en",
        "CFBundleDisplayName": "MikuTV TopShelf",
        "CFBundleExecutable": "TopShelf",
        "CFBundleIdentifier": "dev.yuchanns.MikuTV.TopShelf",
        "CFBundleInfoDictionaryVersion": "6.0",
        "CFBundleName": "TopShelf",
        "CFBundlePackageType": "XPC!",
        "CFBundleShortVersionString": app_version,
        "CFBundleVersion": number,
        "CFBundleSupportedPlatforms": ["AppleTVOS"],
        "CFBundleLocalizations": ["zh-Hans", "en"],
        "UIDeviceFamily": [3],
        "MinimumOSVersion": MINIMUM,
        "NSExtension": {
            "NSExtensionPointIdentifier": "com.apple.tv-top-shelf",
            "NSExtensionPrincipalClass": "MikuTVTopShelfProvider",
        },
        "NSAppTransportSecurity": {
            "NSAllowsLocalNetworking": True,
        },
    }
    (ext / "Info.plist").write_bytes(plistlib.dumps(ext_info))


def detect_license_id(text):
    spdx = re.search(r"(?im)^\s*(?://\s*)?SPDX-License-Identifier:\s*([^\r\n]+?)\s*$", text)
    if spdx:
        return spdx.group(1).strip()
    if "Apache License" in text and "Version 2.0" in text:
        return "Apache-2.0"
    if "Permission is hereby granted, free of charge" in text:
        return "MIT"
    if "Redistribution and use in source and binary forms" in text:
        if "Neither the name of" in text:
            return "BSD-3-Clause"
        return "BSD-2-Clause"
    if "Permission to use, copy, modify, and/or distribute this software" in text:
        return "ISC"
    if "Mozilla Public License Version 2.0" in text:
        return "MPL-2.0"
    return ""


def read_module_license(module_dir):
    if not module_dir.is_dir():
        raise RuntimeError(f"Module directory not found for license collection: {module_dir}")
    entries = {child.name.lower(): child for child in module_dir.iterdir() if child.is_file()}
    primary = next(
        (entries[name] for name in ("license", "license.txt", "license.md", "copying", "copying.txt", "copying.md") if name in entries),
        None,
    )
    notice = next(
        (entries[name] for name in ("notice", "notice.txt", "notice.md") if name in entries),
        None,
    )
    parts = []
    for path in (primary, notice):
        if path is not None:
            body = path.read_text(encoding="utf-8").replace("\r\n", "\n").strip()
            if body:
                parts.append(body)
    if not parts:
        raise RuntimeError(f"Missing LICENSE/NOTICE file in module directory {module_dir}")
    return "\n\n".join(parts)


def collect_go_licenses(build_env=None):
    environment = dict(build_env) if build_env is not None else os.environ.copy()
    if build_env is None:
        environment.update(
            GOOS="ios", GOARCH="arm64", CGO_ENABLED="1", GOWORK="off", GOFLAGS="-mod=vendor",
        )
    fmt = "{{with .Module}}{{if not .Main}}{{.Path}}\t{{.Version}}\t{{.Indirect}}\t{{.Dir}}{{end}}{{end}}"
    raw = subprocess.check_output(
        ["go", "list", "-tags=tvos,mygo_noinspector", "-deps", "-f", fmt, ".", "./cmd/topshelf"],
        cwd=ROOT, env=environment, text=True,
    )
    seen = {}
    for line in raw.splitlines():
        if not line.strip():
            continue
        fields = line.split("\t")
        mod_path = fields[0].strip()
        version = fields[1].strip() if len(fields) > 1 else ""
        indirect = (fields[2].strip() == "true") if len(fields) > 2 else False
        mod_dir = fields[3].strip() if len(fields) > 3 else ""
        if mod_path and mod_path not in seen:
            seen[mod_path] = (version, indirect, mod_dir)
    records = []
    for mod_path in sorted(seen, key=str.lower):
        version, indirect, mod_dir = seen[mod_path]
        license_dir = ROOT / "vendor" / mod_path
        if not license_dir.is_dir() and mod_dir:
            license_dir = Path(mod_dir)
        license_text = read_module_license(license_dir)
        entry = {
            "module": mod_path,
            "version": version,
            "license": detect_license_id(license_text),
            "licenseText": license_text,
        }
        if indirect:
            entry["indirect"] = True
        records.append(entry)
    return records


def bundle_licenses(build_env):
    destination = APP / "assets/licenses.json"
    destination.parent.mkdir(parents=True, exist_ok=True)
    records = collect_go_licenses(build_env)
    destination.write_text(json.dumps(records, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")


def bundle_plugins(app_version=APP_VERSION):
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
            "appVersion": app_version,
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
    parser.add_argument("action", choices=["prepare", "build", "package", "licenses"])
    args = parser.parse_args()
    actions = {
        "prepare": prepare,
        "build": build,
        "package": package,
        "licenses": lambda: print(json.dumps(collect_go_licenses(), ensure_ascii=False, indent=2)),
    }
    actions[args.action]()
