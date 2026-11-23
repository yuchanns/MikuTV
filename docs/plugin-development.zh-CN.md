# MikuTV 插件开发指南（面向 Agent 与开发者）

[← 返回 README](../README.zh-CN.md) · [English](plugin-development.md) · [插件架构设计文档](../plans/20261117-init/plugin-architecture.md)

本指南按**自包含、无歧义的契约文档**编写，既方便开发者查阅，也适合直接交给 AI 编程助手（Claude Code、Codex、Cursor、Antigravity 等）读取并自动完成插件脚手架、测试、打包与发布。

## 1. 分发与打包契约

MikuTV 通过 GitHub Topic **`mikutv-plugin`**（或用户直接输入的 `owner/repo`）发现插件，并在安装前执行严格校验。

| 约束项 | `internal/plugin/` 实际校验规则 |
| --- | --- |
| **GitHub Topic** | 仓库添加 Topic `mikutv-plugin`（且未归档），即可出现在 Apple TV 的 **设置 → 插件管理 → 发现插件**。 |
| **Release 附件** | 在 GitHub Release 上传文件名严格为 **`plugin.zip`** 的附件（上限 8 MiB）。 |
| **版本一致** | Release 标签必须与 `plugin.json` 的 `version` 一致（如 tag `v1.0.0` ↔ `"version": "1.0.0"`，严格语义化版本）。 |
| **压缩包结构** | `plugin.json` 必须位于压缩包根目录（**不要**再套一层外层目录）；禁止软链接、反斜杠、控制字符或大小写冲突路径。 |
| **严格清单解析** | 清单使用 `DisallowUnknownFields` 严格解析，出现未定义字段会直接拒绝加载。 |
| **完整性与来源绑定** | 宿主记录 SHA-256 摘要并将插件 `id` 绑定到源仓库；升级版本时不允许变更 `kind`。 |

### 通用 `plugin.json` 头部字段

```json
{
  "id": "my-plugin",
  "name": "My Plugin",
  "description": "一句话简介。",
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

- `id`：匹配 `^[a-z0-9][a-z0-9._-]{0,127}$`
- `apiVersion`：当前为 `1`
- `minAppVersion`：最低兼容宿主版本（如 `"0.1.0"`）
- `artwork.cover`：可选的包内 PNG / JPEG / WebP 封面路径
- 建议在包根目录附带 `LICENSE` / `LICENSE.txt` / `NOTICE`，MikuTV 会在 **设置 → 关于 MikuTV** 自动展示。

---

## 2. 开发内容源插件（`kind: "source"`）

内容源插件是运行在纯 Go [`moejs`](https://github.com/MoeForge/moejs) 隔离沙箱中的 ES 模块（`.js` 或 `.mjs`）。

### 内容源 `plugin.json` 示例

```json
{
  "id": "example-source",
  "name": "Example Source",
  "description": "视频目录适配器。",
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
      "guestTitle": "扫码登录",
      "guestDescription": "请使用手机客户端扫描二维码。"
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

### 网络域名白名单（`permissions.network`）

- 插件发起的每一条 `fetch` / WebSocket 请求目标域名都必须在 `permissions.network` 中明确声明。
- 安装前会向用户展示完整域名清单；插件更新新增域名时也会再次提示用户确认。
- 内网 IP、回环地址（`127.0.0.1`）、链路本地地址、CGNAT（`100.64.0.0/10`）与 `localhost` 均会被拒绝；通配符（`*.example.com`）要求具备合法的 eTLD+1 根域名。

### 沙箱宿主 API（`globalThis.host`）

在 `moejs` 运行时中，插件可使用标准 ES 模块语法、受域名白名单约束的 `fetch`，以及宿主提供的 `host` 全局对象（定义见 `internal/plugin/host.go`）：

- `host.storage.get(key)` / `host.storage.set(key, value)` / `host.storage.delete(key)` — 插件专属持久化键值存储（**单插件总额 8 KiB**）。
- `host.locale.current()` — 返回当前生效的规范化 BCP-47 语言标签（如 `"zh-Hans"`、`"en"`、`"ja"`），方便内容源插件按语言动态返回分区名、标题和接口参数。
- `host.crypto.sha256(input)` — 返回小写十六进制 SHA-256 摘要。
- `host.crypto.encryptRSAOAEP(base64SPKIDer, plaintext)` — 返回十六进制 RSA-OAEP-SHA256 密文。
- `host.compress.inflateZlib(bytes)` — Zlib 解压（输出上限 4 MiB）。

### 能力声明 ↔ 必需导出函数（`internal/plugin/package.go`）

在 `capabilities` 中声明的每一项能力都必须在 `main` 中导出对应的具名函数；未声明的能力不会被调用，缺失导出函数会导致校验失败。

| 能力标识（Capability） | 必需导出函数 | 依赖约束 |
| --- | --- | --- |
| `catalog.browse` | `browse` | — |
| `catalog.item` | `getItem` | — |
| `catalog.search` | `search` | — |
| `catalog.searchSuggest` | `searchSuggest` | 需同时声明 `catalog.search` |
| `catalog.series` | `readSeries` | — |
| `catalog.live` | `readLive` | — |
| `catalog.creator` | `readCreator` | — |
| `catalog.videoComments` | `videoComments` | — |
| `catalog.following` | `readFollowing` | 需同时声明 `account.signIn` |
| `catalog.library` | `readAccount` | 需同时声明 `account.signIn` |
| `playback.resolve` | `resolvePlayback` | — |
| `playback.progress` | `reportPlayback` | 需同时声明 `account.signIn` |
| `playback.danmaku` | `readDanmaku` | — |
| `playback.subtitles` | `readSubtitles` | — |
| `live.chat` | `connectLiveChat` | — |
| `live.chatWrite` | `sendLiveChat` | 需同时声明 `live.chat` + `account.signIn` |
| `account.signIn` | `beginSignIn`、`pollSignIn`、`restoreAccount`、`signOut` | 需同时声明 `authentication.methods: ["qrCode"]` |
| `account.videoInteraction` | `editVideo` | 需同时声明 `account.signIn` |
| `account.videoFavorites` | `videoFavoriteFolders`、`editVideoFavoriteFolders` | 需同时声明 `account.signIn` |
| `account.danmaku` | `postDanmaku` | 需同时声明 `account.signIn` |
| `account.danmakuRules` | `readDanmakuRules`、`addDanmakuRule`、`removeDanmakuRule` | 需同时声明 `account.signIn` |
| `account.followCreator` | `followCreator` | 需同时声明 `account.signIn` |
| `account.trackSeries` | `trackSeries` | — |
| `account.history` / `watchLater` / `favorites` / `tracking` / `courses` / `notifications` | `readAccount` | 需同时声明 `account.signIn` |
| `account.editFavoriteFolders` | `editFavoriteFolder` | 需同时声明 `account.favorites` |
| `account.editHistory` / `editWatchLater` / `editTracking` | `removeAccountItems` | 需声明对应读取能力 |
| `account.editNotifications` | `markNotificationsRead` | 需同时声明 `account.notifications` |
| `account.privateMessages` | `readPrivateConversations`、`readPrivateMessages` | 需同时声明 `account.signIn` |
| `account.writePrivateMessages` | `sendPrivateMessage`、`markPrivateMessagesRead` | 需同时声明 `account.privateMessages` + `account.signIn` |

每个导出函数的精确入参与返回 JSON 结构，请直接查阅仓库内的 Go 类型定义 [`internal/provider/`](../internal/provider/) 与调用适配层 [`internal/plugin/`](../internal/plugin/)。

---

## 3. 开发主题插件（`kind: "theme"`）

主题插件是纯 JSON 配置（`theme.json`，上限 64 KiB），由 Go 宿主直接解析，不启动任何 JavaScript 运行时。

### 主题 `plugin.json` 示例

```json
{
  "id": "my-theme",
  "name": "My Theme",
  "description": "为 MikuTV 定制的配色主题。",
  "kind": "theme",
  "version": "1.0.0",
  "apiVersion": 1,
  "minAppVersion": "0.1.0",
  "license": "Apache-2.0",
  "theme": "theme.json"
}
```

### `theme.json` 规范（`internal/theme/parse.go`）

- `schemaVersion`：`1`
- `base`：`"mikutv-dark"` 或 `"mikutv-light"`（未覆盖的色彩令牌自动继承基线主题）
- `appearance`：`"dark"` 或 `"light"`
- `colors`：可覆盖 46 个语义色彩令牌（`surface`、`text`、`accent`、`border`、`focus`、`selected`、`hover`、`pressed`、`disabled`、`status`、`overlay`、`playback`、`decoration`），格式为 `#RRGGBB` 或 `#RRGGBBAA`。
- **自动 WCAG 对比度门禁**：宿主加载时会校验主文字、焦点环、选中态和强调色前景对比度，不达标会返回 `ErrContrastFailed`。
- 参考实现：任意一个官方 [`yuchanns/mikutv-theme-*`](https://github.com/yuchanns?tab=repositories&q=mikutv-theme) 仓库。

---

## 4. 开发语言包插件（`kind: "locale"`）

MikuTV 内置简体中文（`zh-Hans`）与英文（`en`）界面，并默认跟随 tvOS 系统语言。社区及官方语言包是纯声明式 JSON（`locale.json`，上限 256 KiB），由 Go 宿主直接热加载，不启动任何 JavaScript 运行时。

### 语言包 `plugin.json` 示例

```json
{
  "id": "mikutv-locale-ja",
  "name": "Japanese (日本語)",
  "description": "MikuTV 日语界面本地化语言包。",
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

### `locale.json` 规范（`internal/plugin/locale.go`）

- `language`（位于 `plugin.json`）：规范 BCP-47 语言标签（如 `"ja"`、`"ko"`、`"zh-Hant"`、`"fr"`、`"es"`）。
- `nativeName`（位于 `plugin.json`）：在 **设置 → 外观与动态 → 界面语言** 中展示的本语种名称（如 `"日本語"`、`"한국어"`、`"繁體中文"`）。
- `locale.json`：直接对照 [`internal/ui/assets/labels.en.json`](../internal/ui/assets/labels.en.json) 的完整或稀疏 JSON 对象；未翻译的键自动回退至内置英文，设置侧栏的分组顺序与图标由宿主自动保留。
- **零重启热切换与跟随系统**：安装后即可在设置中即时切换，在 **跟随系统** 模式下若 tvOS `NSLocale.preferredLanguages` 命中该 `language` 标签会自动生效。
- 参考实现：[`yuchanns/mikutv-locale-ja`](https://github.com/yuchanns/mikutv-locale-ja)。

---

## 5. 给 AI 助手的开发与验收清单（Agent Workflow）

当 AI 编程助手为用户开发 MikuTV 插件时，建议按以下闭环执行：

1. **先读真实 Go 契约源码**：
   - 清单与包结构校验：`internal/plugin/manifest.go`、`internal/plugin/package.go`、`internal/plugin/theme.go`、`internal/plugin/locale.go`
   - Provider 数据结构：`internal/provider/*.go`
   - 主题令牌与对比度规则：`internal/theme/theme.go`、`internal/theme/parse.go`
   - 界面文案基线键表：`internal/ui/assets/labels.en.json`
2. **搭建独立插件仓库**：`plugin.json` 与 `LICENSE` 置于根目录；内容源插件仅使用纯 JS、受控 `fetch` 与 `host.*`（不依赖 Node.js 专属 API）。
3. **从仓库根目录确定性打包 `plugin.zip`**（无外层目录、无软链接、无 `.git/`）：
   ```bash
   zip -r -X plugin.zip plugin.json LICENSE index.js  # 按实际文件列出
   ```
4. **使用 MikuTV 的 Go 加载器实测校验**：
   - 调用 `github.com/yuchanns/MikuTV/internal/plugin` 的 `InspectPackage`、`Open`（内容源）、`OpenTheme`（主题）或 `OpenLocale`（语言包），确认清单字段、域名白名单、对比度、文案结构与导出函数全部通过。
   - 编写基于 Mock HTTP 夹具的自动化测试覆盖核心导出函数。
5. **发布到 GitHub**：
   - 为仓库添加 Topic `mikutv-plugin`；
   - 创建与 `plugin.json` 中 `version` 一致的标签 `v<version>`；
   - 将 `plugin.zip` 上传为该 Release 的附件；
   - 验证 `GET https://api.github.com/repos/<owner>/<repo>/releases/latest` 返回 `plugin.zip` 且 `tag_name == "v" + version`。
