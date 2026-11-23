# 在 Apple TV 上安装 MikuTV

[← 返回 README](../README.zh-CN.md) · [English](install.md) · [GitHub Releases](https://github.com/yuchanns/MikuTV/releases)

MikuTV 每次发版都会在 [GitHub Releases](https://github.com/yuchanns/MikuTV/releases) 自动产出未签名 `.ipa` 安装包：

- `MikuTV-tvOS.ipa` — 固定文件名，适合 ATVLoadly 自动追踪更新与续签。
- `MikuTV-vX.Y.Z-tvOS-unsigned.ipa` — 带版本号的归档包（内容一致）。
- `SHA256SUMS.txt` — SHA-256 校验文件。

由于 tvOS 应用必须签名安装，且免费 Apple ID 证书每 7 天过期，请根据你的宿主机环境选择对应方案。

## 侧载方案对比

| 方案 | 费用 | 7 天自动续签 | 宿主机要求 | 适用人群 |
| --- | --- | --- | --- | --- |
| **[ATVLoadly](https://github.com/bitxeno/atvloadly)** *（推荐）* | 免费 Apple ID | 局域网全自动续签 + 自动追踪 GitHub Releases | macOS、Windows，或常开 Linux / NAS / OpenWrt（Docker） | 零成本省心日常使用，彻底免去手动每 7 天重签 |
| **[Sideloadly](https://sideloadly.io/)** | 免费 Apple ID | 电脑开机联网时 Wi-Fi 自动续签 | macOS 或 Windows 桌面端 | 不想部署容器、偶尔在电脑上装机 |
| **付费开发者账号 / 1 年期 tvOS 证书** | 付费（$99/年或签名服务） | 有效期 1 年（无 7 天限制） | 仅首次通过 Xcode / Apple Configurator / 签名工具安装 | 不保留常开续签宿主机，或需要完整 App Group Top Shelf |

---

## 方案一：ATVLoadly（推荐 — 免费 Apple ID + 自动续签）

[ATVLoadly](https://github.com/bitxeno/atvloadly) 可在局域网内自动为未签名 tvOS `.ipa` 签名并每 7 天静默续签，同时支持直接追踪 MikuTV 的 GitHub Releases。

### 1. 在与 Apple TV 同局域网的宿主机上安装 ATVLoadly

- **macOS（Homebrew）**
  ```bash
  brew install --cask bitxeno/tap/atvloadly
  ```
  如果从 Releases 手动下载 `.dmg` 安装，首次打开前清除隔离属性一次：
  ```bash
  sudo xattr -dr com.apple.quarantine /Applications/atvloadly.app
  ```

- **Windows**
  前往 [bitxeno/atvloadly Releases](https://github.com/bitxeno/atvloadly/releases) 下载对应架构（`x64` 或 `arm64`）的安装程序运行。

- **Linux / NAS / OpenWrt（Docker Compose）**
  先确保宿主机已启动 `avahi-daemon`（用于局域网 mDNS 发现 Apple TV）：
  ```bash
  # Debian / Ubuntu
  sudo apt-get update && sudo apt-get install -y avahi-daemon
  sudo systemctl enable --now avahi-daemon
  ```
  创建 `docker-compose.yml` 并启动：
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

### 2. 配对 Apple TV 并添加 MikuTV

1. 在 Apple TV 上打开 **设置 → 遥控器和设备 → 遥控器 App 与设备**，进入配对等待页面。
2. 打开 ATVLoadly（桌面窗口或浏览器访问 `http://<宿主机IP>:5533`），选中你的 Apple TV 并输入电视屏幕上的配对码。
3. 登录一个**专用免费 Apple ID 小号**（推荐与个人主 iCloud 账号隔离）。
4. 添加 `https://github.com/yuchanns/MikuTV/releases`（或上传从 Releases 下载的 `MikuTV-tvOS.ipa`），保持**自动续签**开启即可。

---

## 方案二：Sideloadly（桌面端）

1. 从 [Releases](https://github.com/yuchanns/MikuTV/releases) 下载 `MikuTV-tvOS.ipa` 与 `SHA256SUMS.txt`。
2. 校验 SHA-256：
   ```bash
   shasum -a 256 -c SHA256SUMS.txt --ignore-missing
   ```
3. 通过 Wi-Fi 配对 Apple TV 后，打开 [Sideloadly](https://sideloadly.io/) 选择设备与 `MikuTV-tvOS.ipa` 完成安装，并在 Sideloadly Daemon 中开启 Wi-Fi 自动续签。

---

## 方案三：从源码构建

### 环境要求

- **macOS** + 包含 **tvOS 18.0+ SDK** 的 **Xcode**（`xcrun --sdk appletvos --show-sdk-path` 可用）
- 与 [`go.mod`](../go.mod) 版本一致的 **Go** 工具链（支持 `GOTOOLCHAIN=auto`）
- **Python 3**

### 构建命令

```bash
# 首次拉取后准备带补丁的 MyGo vendor 目录
make prepare

# 运行静态检查与单元测试
go vet -mod=vendor ./...
go test -mod=vendor -race -count=1 ./...

# 编译 Metal 着色器并构建 .build/tvos/Payload/MikuTV.app
make build

# 打包未签名 tvOS IPA（输出 .build/tvos/MikuTV-unsigned.ipa）
APP_VERSION=0.1.0 BUILD_NUMBER=1 python3 scripts/tvos.py package
```

`scripts/tvos.py` 会自动编译 MyGo Metal 着色器（`shader_tvos.metallib`）、交叉编译 `arm64-apple-tvos18.0` 主程序与 `PlugIns/TopShelf.appex/TopShelf`、生成分层图标（`Assets.car`），并采集嵌入第三方 Go 依赖许可清单（`assets/licenses.json`）。

---

## 给 AI 助手的执行手册（Agent Runbook）

如果你是协助用户安装 MikuTV 的终端 AI 助手，请按以下顺序推进：

1. **探测宿主机环境**：识别系统（macOS / Windows / Linux / NAS）、可用工具（`brew`、`docker`、`gh`、`curl`、`shasum`）以及是否与 Apple TV 同处局域网。
2. **安装并启动对应的 ATVLoadly**：
   - macOS → `brew install --cask bitxeno/tap/atvloadly`
   - Linux / NAS / OpenWrt → 确认 `avahi-daemon` 正在运行，再部署上述 `docker-compose.yml`
   - Windows → 引导从 `bitxeno/atvloadly` Releases 下载 `.exe` 安装并启动
3. **下载并校验 MikuTV**：从 `https://github.com/yuchanns/MikuTV/releases/latest` 获取 `MikuTV-tvOS.ipa` 与 `SHA256SUMS.txt`，先核对 SHA-256 再交给签名工具。
4. **明确交由用户本人操作的节点**：绝不索要或保存用户的 Apple ID 密码；在 Apple TV 屏幕输入配对 PIN 码、或在 ATVLoadly / Sideloadly 中登录 Apple ID 时暂停并提示用户完成。
5. **验收**：确认 MikuTV 已出现在 Apple TV 桌面，且 ATVLoadly 中的自动续签开关已启用。

---

## 平台注意事项

- **系统要求**：仅面向 Apple TV（tvOS 18.0+，`arm64`）。
- **免费账号 7 天签名限制**：使用免费 Apple ID 时建议在同一局域网运行 ATVLoadly 自动续签。
- **Top Shelf App Group 权限**：桌面 Top Shelf 跨进程共享数据依赖签名时的 App Group 权限；未配置时宿主会自动回退至容器内存储，不影响正常使用。
- **GitHub API 限流**：在 Apple TV 上搜索插件若遇到未认证 IP 限流，仍可通过直接输入 `owner/repo` 精确安装。
