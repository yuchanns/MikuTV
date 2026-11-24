# Install MikuTV on Apple TV

[← Back to README](../README.md) · [简体中文](install.zh-CN.md) · [GitHub Releases](https://github.com/yuchanns/MikuTV/releases)

MikuTV publishes unsigned tvOS `.ipa` packages on [GitHub Releases](https://github.com/yuchanns/MikuTV/releases):

- `MikuTV-tvOS.ipa` — Stable filename for automated release tracking (ideal for ATVLoadly).
- `MikuTV-vX.Y.Z-tvOS-unsigned.ipa` — Versioned archive with identical contents.
- `SHA256SUMS.txt` — SHA-256 verification checksums.

Because tvOS requires signed apps and free Apple ID certificates expire every 7 days, choose the sideloading workflow that fits your environment.

## Sideloading Comparison

| Method | Cost | 7-Day Auto-Refresh | Host Requirement | Best For |
| --- | --- | --- | --- | --- |
| **[ATVLoadly](https://github.com/bitxeno/atvloadly)** *(Recommended)* | Free Apple ID | Automatic over LAN + tracks GitHub Releases | macOS, Windows, or always-on Linux / NAS / OpenWrt (Docker) | Hands-off daily use without a paid Apple Developer account |
| **[Sideloadly](https://sideloadly.io/)** | Free Apple ID | Automatic over Wi-Fi while PC/Mac is awake | macOS or Windows | Quick desktop install without running a container |
| **Paid Apple Developer / 1-Year tvOS Cert** | Paid ($99/yr or signing service) | Valid for 1 year (no 7-day cycle) | One-time signing via Xcode / Apple Configurator / signing tool | No background refresh host + full App Group Top Shelf support |

---

## Option 1: ATVLoadly (Recommended — Free Apple ID + Auto-Refresh)

[ATVLoadly](https://github.com/bitxeno/atvloadly) signs and automatically refreshes unsigned tvOS `.ipa` files over your local network and can track MikuTV GitHub Releases directly.

### 1. Install ATVLoadly on a Host on the Same LAN

- **macOS (Homebrew)**
  ```bash
  brew install --cask bitxeno/tap/atvloadly
  ```
  If you install the `.dmg` manually from Releases, remove the macOS quarantine flag once:
  ```bash
  sudo xattr -dr com.apple.quarantine /Applications/atvloadly.app
  ```

- **Windows**
  Download the `x64` or `arm64` installer from [bitxeno/atvloadly Releases](https://github.com/bitxeno/atvloadly/releases) and launch it.

- **Linux / NAS / OpenWrt (Docker Compose)**
  Make sure `avahi-daemon` is installed and running on the host for mDNS Apple TV discovery:
  ```bash
  # Debian / Ubuntu
  sudo apt-get update && sudo apt-get install -y avahi-daemon
  sudo systemctl enable --now avahi-daemon
  ```
  Then create `docker-compose.yml` and start the container:
  ```yaml
  services:
    atvloadly:
      image: bitxeno/atvloadly:latest
      container_name: atvloadly
      restart: always
      ports:
        - "5533:80"
      volumes:
        - ./data:/data
        - /var/run/dbus:/var/run/dbus
        - /var/run/avahi-daemon:/var/run/avahi-daemon
      security_opt:
        - seccomp:unconfined
  ```
  ```bash
  docker compose pull && docker compose up -d
  ```

### 2. Pair Apple TV & Sign In

1. On Apple TV, open **Settings → Remotes and Devices → Remote App and Devices** so it enters pairing mode.
2. Open ATVLoadly (desktop app window or `http://<host-ip>:5533`), select your Apple TV, and enter the on-screen PIN.
3. Sign in with a **dedicated secondary free Apple ID** (recommended over using your primary personal iCloud account).
4. Add `https://github.com/yuchanns/MikuTV/releases` (or upload `MikuTV-tvOS.ipa` downloaded from Releases) and ensure **Auto-Refresh** is enabled.

---

## Option 2: Sideloadly (Desktop)

1. Download `MikuTV-tvOS.ipa` and `SHA256SUMS.txt` from [Releases](https://github.com/yuchanns/MikuTV/releases).
2. Verify the checksum:
   ```bash
   shasum -a 256 -c SHA256SUMS.txt --ignore-missing
   ```
3. Pair your Apple TV with your Mac/Windows PC over Wi-Fi, open [Sideloadly](https://sideloadly.io/), select your Apple TV and `MikuTV-tvOS.ipa`, sign in, and enable **Automatic Refresh** in Sideloadly Daemon.

---

## Option 3: Build from Source

### Prerequisites

- **macOS** with **Xcode** including the **tvOS 18.0+ SDK** (`xcrun --sdk appletvos --show-sdk-path`)
- **Go** matching [`go.mod`](../go.mod) (`GOTOOLCHAIN=auto`)
- **Python 3**

### Commands

```bash
# Prepare patched MyGo vendor tree (required on a fresh checkout)
make prepare

# Run Go static checks and unit tests
go vet -mod=vendor ./...
go test -mod=vendor -race -count=1 ./...

# Compile Metal shaders and build .build/tvos/Payload/MikuTV.app
make build

# Package unsigned tvOS IPA (.build/tvos/MikuTV-unsigned.ipa)
APP_VERSION=0.1.0 BUILD_NUMBER=1 python3 scripts/tvos.py package
```

`scripts/tvos.py` compiles the MyGo Metal shader (`shader_tvos.metallib`), cross-compiles both `MikuTV` and `PlugIns/TopShelf.appex/TopShelf` for `arm64-apple-tvos18.0`, packages the layered Home icon (`Assets.car`), and bundles third-party Go dependency licenses (`assets/licenses.json`).

---

## Agent Runbook (For AI Assistants)

If you are an AI coding assistant helping a user install MikuTV:

1. **Detect host environment**: check OS (`uname -s` / Windows), available runtimes (`brew`, `docker`, `gh`, `curl`, `shasum`/`sha256sum`), and whether the host shares a LAN with the Apple TV.
2. **Recommend the matching ATVLoadly install path**:
   - macOS → `brew install --cask bitxeno/tap/atvloadly`
   - Linux / NAS / OpenWrt → verify `avahi-daemon` is active, then deploy the `docker-compose.yml` above
   - Windows → guide download of the `.exe` from `bitxeno/atvloadly` Releases
3. **Fetch & verify MikuTV**: download the latest `MikuTV-tvOS.ipa` and `SHA256SUMS.txt` from `https://github.com/yuchanns/MikuTV/releases/latest` and verify the SHA-256 digest before handing off to the signer.
4. **Pause for human-only steps**: never solicit or store the user's Apple ID password; pause cleanly when the user needs to enter the Apple TV pairing PIN or authenticate their Apple ID in ATVLoadly / Sideloadly.
5. **Confirm completion**: verify MikuTV appears installed on Apple TV and that Auto-Refresh is turned on in ATVLoadly.

---

## Platform Notes

- **tvOS 18.0+ (`arm64`) only** — Built specifically for Apple TV hardware.
- **7-day free Apple ID signing limit** — Keep an auto-refresh host (such as ATVLoadly) on the same LAN when using a free Apple ID.
- **Top Shelf App Groups** — Cross-process Top Shelf shelves require matching App Group entitlements when signing; without them, MikuTV falls back cleanly to app-container storage.
- **GitHub API discovery rate limits** — If unauthenticated GitHub Search hits a per-IP rate limit on Apple TV, direct `owner/repo` install remains available.
