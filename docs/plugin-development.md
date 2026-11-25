# MikuTV Plugin Development Guide (Agent & Developer Reference)

[← Back to README](../README.md) · [简体中文](plugin-development.zh-CN.md) · [Architecture Spec](../plans/20261117-init/plugin-architecture.md)

This guide is structured as an exact, self-contained specification so both human developers and AI coding agents (Claude Code, Codex, Cursor, Antigravity, etc.) can scaffold, validate, package, and publish MikuTV plugins without guesswork.

## 1. Distribution & Packaging Contract

MikuTV discovers plugins on GitHub via topic **`mikutv-plugin`** (or direct `owner/repo` lookup) and installs strictly verified archives.

| Requirement | Rule enforced by `internal/plugin/` |
| --- | --- |
| **GitHub Topic** | Add repository topic `mikutv-plugin` (and keep repo non-archived) to appear in **Settings → Plugins → Discover**. |
| **Release Asset** | Publish a GitHub Release with an asset named exactly **`plugin.zip`** (max 8 MiB). |
| **Version Alignment** | Release tag must match `v<version>` in `plugin.json` (e.g. tag `v1.0.0` ↔ `"version": "1.0.0"`, strict canonical semver). |
| **Archive Hygiene** | All files live at the archive root (`plugin.json` at root, NOT wrapped inside a top-level folder). No symlinks, no backslashes, no control chars, no case-insensitive duplicate paths. |
| **Manifest Integrity** | Strict JSON decoding (`DisallowUnknownFields`). Unknown keys fail validation immediately. |
| **Integrity & Ownership** | Host records SHA-256 digest and binds `plugin.json` `id` to its source repository on install. Updates cannot change `kind`. |

### Common `plugin.json` Header Fields

```json
{
  "id": "my-plugin",
  "name": "My Plugin",
  "description": "Concise one-line description.",
  "kind": "source",
  "version": "1.0.0",
  "apiVersion": 1,
  "minAppVersion": "0.1.0",
  "license": "Apache-2.0",
  "artwork": {
    "cover": "cover.png"
  }
}
```

- `id`: matches `^[a-z0-9][a-z0-9._-]{0,127}$`
- `apiVersion`: currently `1`
- `minAppVersion`: semantic version (`"0.1.0"`)
- `artwork.cover`: optional PNG/JPEG/WebP inside the zip
- Include a root `LICENSE` / `LICENSE.txt` / `NOTICE` file so MikuTV displays your license cleanly in **Settings → About MikuTV**.

---

## 2. Building a Content Source Plugin (`kind: "source"`)

Source plugins are ES modules (`.js` or `.mjs`) executed inside MikuTV's isolated pure-Go [`moejs`](https://github.com/MoeForge/moejs) sandbox.

### Source `plugin.json` Fields

```json
{
  "id": "example-source",
  "name": "Example Source",
  "description": "Video catalog adapter.",
  "kind": "source",
  "version": "1.0.0",
  "apiVersion": 1,
  "minAppVersion": "0.1.0",
  "license": "Apache-2.0",
  "main": "index.js",
  "permissions": {
    "network": [
      "api.example.com",
      "*.cdn.example.com"
    ]
  },
  "authentication": {
    "methods": ["qrCode"],
    "presentation": {
      "guestTitle": "Sign in",
      "guestDescription": "Scan with the companion mobile app."
    }
  },
  "locales": {
    "en": {
      "name": "Example Source",
      "description": "Video catalog adapter."
    }
  },
  "capabilities": [
    "catalog.browse",
    "catalog.item",
    "catalog.search",
    "playback.resolve",
    "account.signIn"
  ]
}
```

### Network Allowlist Rules (`permissions.network`)

- Every outbound `fetch` / WebSocket host MUST be declared in `permissions.network`.
- Users explicitly review this list before installing and again whenever an update adds new domains.
- Private IPs, loopback (`127.0.0.1`), link-local, CGNAT (`100.64.0.0/10`), and `localhost` are rejected in static `permissions.network`.
- Wildcards (`*.example.com`) require a valid eTLD+1 base domain.
- **Self-Hosted User Server Origin (`authentication.serverURL`)**: When a source declares `"methods": ["credentials"]` and `"serverURL": "required"` (or `"optional"`), `permissions.network` may be empty. At sign-in time, MikuTV validates the user-entered `http://` or `https://` address and grants a strict **per-session single-origin allowlist** (exact `scheme://host:port` only) for `fetch`, artwork, and playback requests — including LAN servers — while still blocking every other LAN IP, port, or undeclared domain.

### Authenticated Artwork Loading (`permissions.artwork`)

By default, MikuTV fetches external artwork URLs without cookies and without following redirects. Starting in host version `0.1.4`, sign-in source plugins whose thumbnails or backdrops live behind cookie-authenticated HTTPS endpoints (such as cloud drives) may opt in via `permissions.artwork`:

```json
"minAppVersion": "0.1.4",
"permissions": {
  "network": [
    "api.example.com",
    "*.cdn.example.com"
  ],
  "artwork": {
    "hosts": [
      "api.example.com"
    ],
    "referer": "https://api.example.com/"
  }
}
```

- Requires the `account.signIn` capability and `"minAppVersion": "0.1.4"` (or higher).
- `hosts` accepts 1–16 exact public HTTPS hostnames (wildcards such as `*.example.com` are rejected), and every host MUST also be permitted by `permissions.network`.
- Optional `referer` MUST be a valid `https://` URL (max 512 characters) whose host is permitted by `permissions.network`.
- At runtime, MikuTV attaches the active session's cookies and declared `Referer` **only** to exact declared `artwork.hosts`, follows up to 5 validated HTTPS redirects, and strips cookies on any cross-origin redirect. Non-declared hosts and plugins without `permissions.artwork` keep the zero-cookie, no-redirect default.

### Credentials & Self-Hosted Authentication

Source plugins targeting self-hosted media servers (e.g. Jellyfin) or username/password accounts declare `"credentials"` in `authentication.methods`:

```json
"authentication": {
  "methods": ["credentials"],
  "serverURL": "required",
  "presentation": {
    "guestTitle": "Connect to Jellyfin",
    "guestDescription": "Sign in with your Jellyfin server URL and account.",
    "credentialsTitle": "Sign In to Jellyfin",
    "credentialsDescription": "Enter your server URL, username, and password.",
    "serverURLPlaceholder": "http://192.168.1.10:8096",
    "usernamePlaceholder": "Username"
  }
}
```

- Export `signInWithCredentials(req)` receiving `{ serverURL, username, password }` and returning `{ account, secret }`.
- MikuTV stores `secret` and the normalized `serverOrigin` in the tvOS Keychain, passes `secret` directly to `restoreAccount(secret)` and `signOut(secret)`, and injects `req.secret` into subsequent `browse`, `getItem`, `search`, `searchSuggest`, `resolvePlayback`, `readSubtitles`, and `readAccount` calls for that session.
- Reference implementation: [`yuchanns/mikutv-plugin-jellyfin`](https://github.com/yuchanns/mikutv-plugin-jellyfin).

### Sandbox Host API (`globalThis.host`)

Inside `moejs`, plugins have standard ES module syntax, restricted `fetch`, and the `host` global object (`internal/plugin/host.go`):

- `host.storage.get(key)` / `host.storage.set(key, value)` / `host.storage.delete(key)` — Per-plugin persistent key-value store (**8 KiB total quota**).
- `host.locale.current()` — Returns the currently active normalized BCP-47 language tag (e.g. `"zh-Hans"`, `"en"`, `"ja"`), allowing source plugins to localize tab titles, shelf labels, and API requests dynamically.
- `host.crypto.sha256(input)` — Lowercase hex SHA-256 digest.
- `host.crypto.encryptRSAOAEP(base64SPKIDer, plaintext)` — Hex-encoded RSA-OAEP-SHA256 ciphertext.
- `host.compress.inflateZlib(bytes)` — Zlib inflate (up to 4 MiB output).

### Capabilities ↔ Required ES Module Exports (`internal/plugin/package.go`)

Every capability declared in `capabilities` MUST have its matching named export(s) in `main`. Undeclared capabilities are not invoked; missing exports fail package open.

| Capability | Required Export(s) | Dependency Constraint |
| --- | --- | --- |
| `catalog.browse` | `browse` | — |
| `catalog.item` | `getItem` | — |
| `catalog.search` | `search` | — |
| `catalog.searchSuggest` | `searchSuggest` | Requires `catalog.search` |
| `catalog.series` | `readSeries` | — |
| `catalog.live` | `readLive` | — |
| `catalog.creator` | `readCreator` | — |
| `catalog.videoComments` | `videoComments` | — |
| `catalog.following` | `readFollowing` | Requires `account.signIn` |
| `catalog.library` | `readAccount` | Requires `account.signIn` |
| `playback.resolve` | `resolvePlayback` | — |
| `playback.progress` | `reportPlayback` | Requires `account.signIn` |
| `playback.danmaku` | `readDanmaku` | — |
| `playback.subtitles` | `readSubtitles` | — |
| `live.chat` | `connectLiveChat` | — |
| `live.chatWrite` | `sendLiveChat` | Requires `live.chat` + `account.signIn` |
| `account.signIn` | `restoreAccount`, `signOut`, plus `beginSignIn` & `pollSignIn` (for `"qrCode"`) and/or `signInWithCredentials` (for `"credentials"`) | Requires `authentication.methods` (`"qrCode"` and/or `"credentials"`) |
| `account.videoInteraction` | `editVideo` | Requires `account.signIn` |
| `account.videoFavorites` | `videoFavoriteFolders`, `editVideoFavoriteFolders` | Requires `account.signIn` |
| `account.danmaku` | `postDanmaku` | Requires `account.signIn` |
| `account.danmakuRules` | `readDanmakuRules`, `addDanmakuRule`, `removeDanmakuRule` | Requires `account.signIn` |
| `account.followCreator` | `followCreator` | Requires `account.signIn` |
| `account.trackSeries` | `trackSeries` | — |
| `account.history` / `watchLater` / `favorites` / `tracking` / `courses` / `notifications` | `readAccount` | Requires `account.signIn` |
| `account.editFavoriteFolders` | `editFavoriteFolder` | Requires `account.favorites` |
| `account.editHistory` / `editWatchLater` / `editTracking` | `removeAccountItems` | Requires matching read capability |
| `account.editNotifications` | `markNotificationsRead` | Requires `account.notifications` |
| `account.privateMessages` | `readPrivateConversations`, `readPrivateMessages` | Requires `account.signIn` |
| `account.writePrivateMessages` | `sendPrivateMessage`, `markPrivateMessagesRead` | Requires `account.privateMessages` + `account.signIn` |

For exact request and response JSON schemas of each export, inspect the Go types in [`internal/provider/`](../internal/provider/) and the invocation adapters in [`internal/plugin/`](../internal/plugin/).

---

## 3. Building a Theme Plugin (`kind: "theme"`)

Theme plugins are pure declarative JSON (`theme.json`, max 64 KiB) loaded directly in Go without starting JavaScript.

### Theme `plugin.json`

```json
{
  "id": "my-theme",
  "name": "My Theme",
  "description": "Custom palette for MikuTV.",
  "kind": "theme",
  "version": "1.0.0",
  "apiVersion": 1,
  "minAppVersion": "0.1.0",
  "license": "Apache-2.0",
  "theme": "theme.json"
}
```

### `theme.json` Rules (`internal/theme/parse.go`)

- `schemaVersion`: `1`
- `base`: `"mikutv-dark"` or `"mikutv-light"` (unmodified tokens inherit cleanly from the base)
- `appearance`: `"dark"` or `"light"`
- `colors`: overrides any of the 46 semantic tokens (`surface`, `text`, `accent`, `border`, `focus`, `selected`, `hover`, `pressed`, `disabled`, `status`, `overlay`, `playback`, `decoration`) using `#RRGGBB` or `#RRGGBBAA`.
- **Automatic WCAG Contrast Gate**: MikuTV validates contrast ratios for primary text, focus rings, selected states, and accent foregrounds when opening the package; failing palettes are rejected with `ErrContrastFailed`.
- Reference implementations: any of the 10 official [`yuchanns/mikutv-theme-*`](https://github.com/yuchanns?tab=repositories&q=mikutv-theme) repositories.

---

## 4. Building a Language Pack Plugin (`kind: "locale"`)

MikuTV includes built-in Simplified Chinese (`zh-Hans`) and English (`en`) UI strings and follows the tvOS system language by default. Community and official language packs are pure declarative JSON (`locale.json`, max 256 KiB) loaded directly in Go without starting JavaScript.

### Locale `plugin.json`

```json
{
  "id": "mikutv-locale-ja",
  "name": "Japanese (日本語)",
  "description": "Japanese UI localization for MikuTV.",
  "kind": "locale",
  "version": "1.0.0",
  "apiVersion": 1,
  "minAppVersion": "0.1.0",
  "license": "Apache-2.0",
  "language": "ja",
  "nativeName": "日本語",
  "locale": "locale.json"
}
```

### `locale.json` Rules (`internal/plugin/locale.go`)

- `language` (in `plugin.json`): canonical BCP-47 tag (e.g. `"ja"`, `"ko"`, `"zh-Hant"`, `"fr"`, `"es"`).
- `nativeName` (in `plugin.json`): native endonym shown in **Settings → Appearance & Motion → Language** (e.g. `"日本語"`, `"한국어"`, `"繁體中文"`).
- `locale.json`: sparse or complete JSON object directly mirroring [`internal/ui/assets/labels.en.json`](../internal/ui/assets/labels.en.json). Any omitted key falls back cleanly to built-in English, and canonical settings section order/icons are preserved automatically.
- **Zero-restart hot-swap & Follow System**: Once installed, a locale pack can be selected immediately in Settings, or activated automatically in **Follow System** mode when tvOS `NSLocale.preferredLanguages` matches its `language` tag.
- Reference implementation: [`yuchanns/mikutv-locale-ja`](https://github.com/yuchanns/mikutv-locale-ja).

---

## 5. Publishing & Verification

Put the plugin in a public GitHub repository with `plugin.json` and `LICENSE` at the root, and add the topic **`mikutv-plugin`** (plus `mikutv-theme` for themes or `mikutv-locale` for language packs). MikuTV reads the latest non-draft GitHub Release whose tag matches `v<version>` in `plugin.json` and installs its `plugin.zip` asset.

### Release Workflow

Add `.github/workflows/release.yml` using [`yuchanns/release-mikutv-plugin`](https://github.com/yuchanns/release-mikutv-plugin). Pushing a `v*` tag validates `plugin.json`, builds a deterministic `plugin.zip` and `SHA256SUMS.txt`, and publishes the GitHub Release (automatically marking SemVer prerelease tags as prereleases):

```yaml
name: Release

on:
  push:
    tags:
      - 'v*'

permissions:
  contents: write

jobs:
  release:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v7
      - uses: yuchanns/release-mikutv-plugin@v1
```

### Verification Checklist

1. **Read authoritative Go contracts first**:
   - Manifest & archive validation: `internal/plugin/manifest.go`, `internal/plugin/package.go`, `internal/plugin/theme.go`, `internal/plugin/locale.go`
   - Provider request/response structs: `internal/provider/*.go`
   - Theme schema & contrast rules: `internal/theme/theme.go`, `internal/theme/parse.go`
   - UI locale baseline keys: `internal/ui/assets/labels.en.json`
2. **Scaffold the repository**: keep `plugin.json`, `LICENSE`, and `.js`/`.json` files at the root (only pure JS, `fetch`, and `host.*` are available inside `moejs`), and add `plugin.zip` and `SHA256SUMS.txt` to `.gitignore`.
3. **Validate with MikuTV's Go loader**: run a Go test against `github.com/yuchanns/MikuTV/internal/plugin` calling `plugin.InspectPackage` along with `plugin.Open` (`source`), `plugin.OpenTheme` (`theme`), or `plugin.OpenLocale` (`locale`) to confirm zero unknown fields, valid host allowlist, contrast/locale rules, and declared exports; cover JS exports with mocked HTTP fixtures.
4. **Tag and publish**: bump `version` in `plugin.json`, push to `main`, and push the matching tag:
   ```bash
   git tag -a v1.0.0 -m "Release v1.0.0"
   git push origin v1.0.0
   ```
   Once the workflow finishes, `GET https://api.github.com/repos/<owner>/<repo>/releases/latest` serves `plugin.zip` under `tag_name == "v" + version`.
