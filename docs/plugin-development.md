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
      "guestSubtitle": "Scan with the companion mobile app."
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
- Private IPs, loopback (`127.0.0.1`), link-local, CGNAT (`100.64.0.0/10`), and `localhost` are rejected.
- Wildcards (`*.example.com`) require a valid eTLD+1 base domain.

### Sandbox Host API (`globalThis.host`)

Inside `moejs`, plugins have standard ES module syntax, restricted `fetch`, and the `host` global object (`internal/plugin/host.go`):

- `host.storage.get(key)` / `host.storage.set(key, value)` / `host.storage.delete(key)` — Per-plugin persistent key-value store (**8 KiB total quota**).
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
| `account.signIn` | `beginSignIn`, `pollSignIn`, `restoreAccount`, `signOut` | Requires `authentication.methods: ["qrCode"]` |
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

## 4. Agent Workflow & Verification Checklist

When an AI coding agent develops a MikuTV plugin, follow this loop:

1. **Read authoritative Go contracts first**:
   - Manifest & archive validation: `internal/plugin/manifest.go`, `internal/plugin/package.go`, `internal/plugin/theme.go`
   - Provider request/response structs: `internal/provider/*.go`
   - Theme schema & contrast rules: `internal/theme/theme.go`, `internal/theme/parse.go`
2. **Scaffold the plugin repo** with `plugin.json` at root, `LICENSE`, and clean `.js`/`.json` files. Avoid external Node-only dependencies (only pure JS + `fetch` + `host.*` are available inside `moejs`).
3. **Build `plugin.zip` deterministically** from the repository root (no parent directory prefix, no symlinks, no `.git/`):
   ```bash
   zip -r -X plugin.zip plugin.json LICENSE index.js  # include all plugin files at root
   ```
4. **Validate directly with MikuTV's Go loader** before publishing:
   - Run a small Go test or command against `github.com/yuchanns/MikuTV/internal/plugin` calling `plugin.InspectPackage`, `plugin.Open` (for `source`) or `plugin.OpenTheme` (for `theme`).
   - Verify `plugin.Open` succeeds with zero unknown fields, valid host allowlist, and all declared capability exports present.
   - Write unit tests exercising your plugin's JS exports with mocked HTTP fixtures.
5. **Publish to GitHub**:
   - Add repository topic `mikutv-plugin`.
   - Create tag `v<version>` matching `plugin.json` `version`.
   - Attach `plugin.zip` to the GitHub Release.
   - Verify live resolution: `GET https://api.github.com/repos/<owner>/<repo>/releases/latest` contains an asset named `plugin.zip` and `tag_name == "v" + version`.
