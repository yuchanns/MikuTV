export type Lang = 'en' | 'zh'

export const COPY = {
  en: {
    langCode: 'en',
    altLabel: '中',
    nav: {
      plugins: 'Plugins',
      develop: 'Develop',
      github: 'GitHub',
    },
    meta: {
      homeTitle: 'MikuTV — Open-Source Player for Apple TV',
      homeDesc:
        'An open-source, plugin-driven media player for Apple TV. Bring your own servers, video sources, themes, and languages into one native tvOS interface.',
      pluginsTitle: 'Plugin Catalogue — MikuTV',
      pluginsDesc: 'Live index of MikuTV plugins published on GitHub under topic:mikutv-plugin.',
      developTitle: 'Plugin Developer Guide — MikuTV',
      developDesc: 'Technical manual for authoring, validating, and publishing MikuTV plugins.',
    },
    hero: {
      titleLines: ['An open player', 'for Apple TV.'] as const,
      lead: 'No built-in catalogue and no platform lock-in. Connect your own media servers, video sources, themes, and languages through open-source plugins in one native tvOS interface.',
      ctaPrimary: 'Download',
      ctaSecondary: 'Browse plugins',
      screenLabel: 'APPLE TV 4K · LIVE CAPTURE',
      soundOff: 'SOUND OFF',
      soundOn: 'SOUND ON',
    },
    statement: [
      'Your movies live on your own server; the rest of what you watch is scattered across different platforms. Instead of jumping between mismatched apps, MikuTV brings them into one interface and one remote.',
      'MikuTV does not host or sell content. That leaves one job: making it calm, fast, and effortless on your television.',
    ],
    evening: {
      m1: {
        time: '01 / HOME SCREEN',
        label: 'TOP SHELF',
        title: 'Continue watching from the Home Screen.',
        body: [
          'Place MikuTV in the top row of Apple TV. Whenever the icon is focused, tvOS Top Shelf surfaces your Continue Watching queue so you can jump straight back in.',
        ],
      },
      m2: {
        time: '02 / APPEARANCE',
        label: 'LIGHT & DARK',
        title: 'Automatic light and dark themes.',
        body: [
          'Follows tvOS system appearance with separate slots for daytime and nighttime. Ships with MikuTV Light and MikuTV Dark, plus ten paired theme plugins across Tokyo Night, Catppuccin, Nord, Gruvbox, and Rosé Pine.',
        ],
      },
      m3: {
        time: '03 / BROWSING',
        label: 'NAVIGATION',
        title: 'Instant hover previews and zero-click switching.',
        body: [
          'Pause on a Home poster to play an inline preview with audio—without triggering HDMI mode-switch blackouts or polluting your watch history.',
          'Moving across the sidebar, library tabs, and settings updates the page immediately as your thumb glides over the Siri Remote.',
        ],
      },
      m4: {
        time: '04 / PLAYBACK',
        label: 'AVPLAYER',
        title: 'Hardware playback with full subtitle and danmaku controls.',
        body: [
          'Video decoding runs directly on Apple TV’s native AVPlayer. Switch audio and subtitle tracks on the fly, adjust playback speed, and filter live danmaku with custom rules.',
          'Player controls slide in from a right-hand frosted drawer instead of interrupting the picture with pop-up dialogs.',
        ],
      },
    },
    fill: {
      label: '05 / PLUGINS',
      title: 'Sources, themes, and languages as open plugins.',
      body: [
        'Every plugin lives in a public GitHub repository. Browse, install, and update directly from Settings → Plugins on Apple TV—changes take effect right away.',
        'Before installation, MikuTV lists every network domain the plugin requests and blocks undeclared hosts at runtime. Packages are verified by Release tag and SHA-256 digest, and self-hosted server plugins can only access the single URL you provide.',
      ],
      rows: [
        { k: 'Sources', v: 'Jellyfin media servers, plus community video and live sources' },
        { k: 'Themes', v: 'Five colour families, ten paired light and dark themes' },
        { k: 'Languages', v: 'Chinese and English built in; Japanese via mikutv-locale-ja' },
      ],
      link: 'Browse all plugins →',
    },
    build: {
      label: '06 / ARCHITECTURE',
      title: 'How it’s built.',
      lead: 'Open source end to end, so you can inspect or build every layer yourself.',
      rows: [
        { k: 'Language', v: 'Go' },
        { k: 'UI Engine', v: 'MyGo on Metal with CoreText text rendering' },
        { k: 'tvOS APIs', v: 'AVFoundation, AVKit, TVServices, and Keychain via purego' },
        { k: 'Sandbox', v: 'moejs, a pure-Go JavaScript runtime' },
        { k: 'License', v: 'Apache-2.0' },
      ],
      darkCap: 'TOKYO NIGHT · ABOUT',
      lightCap: 'ROSÉ PINE DAWN · ABOUT',
    },
    install: {
      label: '07 / INSTALLATION',
      title: 'Installing on Apple TV.',
      body: [
        'GitHub Releases provide an unsigned MikuTV-tvOS.ipa alongside SHA256SUMS.txt. Sideload it using ATVLoadly, Sideloadly, or your own Apple developer certificate—or build it directly from source.',
        'Code signing and renewal are handled by your sideloading tool, not by MikuTV.',
      ],
      req: 'Requires Apple TV · tvOS 18 or later · arm64',
      files: [
        { name: 'MikuTV-tvOS.ipa', note: 'UNSIGNED ARCHIVE ↗', href: 'https://github.com/yuchanns/MikuTV/releases/latest' },
        { name: 'SHA256SUMS.txt', note: 'CHECKSUMS ↗', href: 'https://github.com/yuchanns/MikuTV/releases/latest' },
        { name: 'docs/install.md', note: 'INSTALLATION GUIDE ↗', href: 'https://github.com/yuchanns/MikuTV/blob/main/docs/install.md' },
        { name: 'scripts/tvos.py', note: 'BUILD FROM SOURCE ↗', href: 'https://github.com/yuchanns/MikuTV/blob/main/scripts/tvos.py' },
      ],
    },
    closing: 'One interface. One remote.',
    pluginsPage: {
      title: 'Plugin Catalogue',
      note: 'Live index of repositories tagged topic:mikutv-plugin. Install and update directly inside Settings → Plugins on Apple TV.',
      filters: {
        all: 'All',
        source: 'Sources',
        theme: 'Themes',
        locale: 'Languages',
      },
      searchPlaceholder: 'Filter by name or owner',
      loading: 'LOADING TOPIC:MIKUTV-PLUGIN…',
      live: 'LIVE · TOPIC:MIKUTV-PLUGIN',
      error: 'Could not reach the GitHub API right now. Try again later or browse topic:mikutv-plugin directly on GitHub.',
      empty: 'No matching plugins found.',
      copied: 'COPIED',
      kindLabel: {
        source: 'Source',
        theme: 'Theme',
        locale: 'Locale',
      },
    },
    developPage: {
      title: 'Plugin Developer Guide',
      note: 'Packaging contract, sandbox host API, capability exports, and publishing guide for source, theme, and language plugins.',
      toc: [
        { id: 'contract', num: '01', label: 'Packaging contract' },
        { id: 'source', num: '02', label: 'Content sources' },
        { id: 'sandbox', num: '03', label: 'Sandbox host API' },
        { id: 'capabilities', num: '04', label: 'Capabilities' },
        { id: 'theme', num: '05', label: 'Themes' },
        { id: 'locale', num: '06', label: 'Language packs' },
        { id: 'publish', num: '07', label: 'Verify & publish' },
      ],
      capFilter: 'Filter capability or export',
      figureCaption: 'ONCE PUBLISHED WITH A RELEASE AND TOPIC:MIKUTV-PLUGIN, IT APPEARS IN APPLE TV DISCOVERY.',
    },
    footer: {
      plugins: 'Plugins',
      develop: 'Develop',
    },
  },
  zh: {
    langCode: 'zh-Hans',
    altLabel: 'EN',
    nav: {
      plugins: '插件',
      develop: '开发',
      github: 'GitHub',
    },
    meta: {
      homeTitle: 'MikuTV — 给 Apple TV 的开源播放器',
      homeDesc:
        '给 Apple TV 的开源插件化播放器。不内置片库、不绑定平台，通过开源插件接入自建媒体库、视频源、主题与语言。',
      pluginsTitle: '插件目录 — MikuTV',
      pluginsDesc: 'GitHub topic:mikutv-plugin 实时插件目录，收录 MikuTV 内容源、主题与语言包。',
      developTitle: '插件开发手册 — MikuTV',
      developDesc: 'MikuTV 插件开发手册：打包契约、moejs 沙盒宿主接口、能力导出表与发布规范。',
    },
    hero: {
      titleLines: ['给 Apple TV 的', '开源播放器。'] as const,
      lead: '不内置片库，也不绑定任何平台。通过开源插件接入自建媒体库、视频源、主题配色与语言包，放进同一套原生 tvOS 界面。',
      ctaPrimary: '下载安装',
      ctaSecondary: '浏览插件目录',
      screenLabel: 'APPLE TV 4K · 实机画面',
      soundOff: '声音关',
      soundOn: '声音开',
    },
    statement: [
      '本地影视存在自己的服务器上，常看的视频散落在各个平台。与其在几个风格迥异的客户端之间反复切换，不如把它们收进同一个界面，交给同一只遥控器。',
      'MikuTV 本身不提供任何内容，我们只专心做好一件事：让它在客厅大屏上用起来顺手、安静。',
    ],
    evening: {
      m1: {
        time: '01 / 主屏',
        label: 'TOP SHELF',
        title: '主屏 Top Shelf，直接继续观看。',
        body: [
          '把 MikuTV 放到 Apple TV 主屏第一排，焦点移到图标上方时，Top Shelf 会直接展示你的「继续观看」列表，点一下就能回到上次看到的进度。',
        ],
      },
      m2: {
        time: '02 / 外观',
        label: '日夜主题',
        title: '随系统切换日夜主题。',
        body: [
          '支持跟随 tvOS 系统外观自动切换，白天与夜间分别保留独立主题槽位。除内置的 MikuTV Light 与 MikuTV Dark 外，还有 Tokyo Night、Catppuccin、Nord、Gruvbox、Rosé Pine 五个系列共十套日夜配对主题。',
        ],
      },
      m3: {
        time: '03 / 浏览',
        label: '交互体验',
        title: '焦点停留即播预览，切栏零延迟。',
        body: [
          '在首页封面上稍作停留，内嵌视频预览会自动带声起播，不会触发 HDMI 画面黑屏闪烁，也不会写入观看历史。',
          '左侧主导航栏、片库分栏与设置菜单都支持遥控器滑动即时切页，不必反复按确认键进出层级。',
        ],
      },
      m4: {
        time: '04 / 播放',
        label: '原生硬解',
        title: '原生硬件解码，完整字幕与弹幕控制。',
        body: [
          '视频解码直接交由系统 AVPlayer 完成。支持多音轨与多语言字幕即时切换，实时弹幕可配置自定义屏蔽规则，也能随时调整画质与倍速。',
          '所有播放设置统一从右侧毛玻璃抽屉滑出，不用居中弹窗打断画面。',
        ],
      },
    },
    fill: {
      label: '05 / 插件',
      title: '内容源、主题、语言，全由插件扩展。',
      body: [
        '每个插件都是 GitHub 上的公开开源仓库。直接在 Apple TV 的「设置 → 插件管理」里搜索、安装或更新，装完立刻生效。',
        '安装前会明确列出插件需要访问的域名，经你确认后才放行，未声明的请求在沙盒运行时直接拦截；安装包严格核对 Release tag 与 SHA-256 摘要，连接自建服务器时也只放行你填写的那一个地址。',
      ],
      rows: [
        { k: '内容源', v: 'Jellyfin 自建媒体库，以及社区维护的视频与直播源' },
        { k: '主题', v: '五个配色家族共十套主题，日夜模式成对提供' },
        { k: '语言包', v: '内置中文与英文，社区提供日语包（mikutv-locale-ja）' },
      ],
      link: '查看全部插件 →',
    },
    build: {
      label: '06 / 技术栈',
      title: '技术实现。',
      lead: '全栈开源，方便你随时查阅源码或自行编译。',
      rows: [
        { k: '开发语言', v: 'Go' },
        { k: '渲染框架', v: 'MyGo（Metal 图形绘制，CoreText 文字排版）' },
        { k: '系统绑定', v: '通过 purego 直接调用 AVFoundation、AVKit、TVServices 与 Keychain' },
        { k: '插件沙盒', v: 'moejs（纯 Go 实现的轻量 JavaScript 运行时）' },
        { k: '开源协议', v: 'Apache-2.0' },
      ],
      darkCap: 'TOKYO NIGHT · 关于页面',
      lightCap: 'ROSÉ PINE DAWN · 关于页面',
    },
    install: {
      label: '07 / 安装',
      title: '如何安装到 Apple TV。',
      body: [
        'GitHub Releases 提供未签名的 MikuTV-tvOS.ipa 与 SHA256SUMS.txt 校验文件。你可以使用 ATVLoadly、Sideloadly 或个人 Apple 开发者证书自行签名安装，也可以直接从源码本地构建。',
        '签名与续签由你所选的侧载工具负责，MikuTV 本身不包含自动签名功能。',
      ],
      req: '系统要求：Apple TV · tvOS 18 或更高版本 · arm64',
      files: [
        { name: 'MikuTV-tvOS.ipa', note: '未签名安装包 ↗', href: 'https://github.com/yuchanns/MikuTV/releases/latest' },
        { name: 'SHA256SUMS.txt', note: '校验文件 ↗', href: 'https://github.com/yuchanns/MikuTV/releases/latest' },
        { name: 'docs/install.zh-CN.md', note: '侧载安装指南 ↗', href: 'https://github.com/yuchanns/MikuTV/blob/main/docs/install.zh-CN.md' },
        { name: 'scripts/tvos.py', note: '源码构建脚本 ↗', href: 'https://github.com/yuchanns/MikuTV/blob/main/scripts/tvos.py' },
      ],
    },
    closing: '一个界面，一只遥控器。',
    pluginsPage: {
      title: '插件目录',
      note: '实时索引带有 topic:mikutv-plugin 标签的开源插件；实际安装与更新在 Apple TV 的「设置 → 插件管理」中完成。',
      filters: {
        all: '全部',
        source: '内容源',
        theme: '主题',
        locale: '语言包',
      },
      searchPlaceholder: '按名称或作者筛选',
      loading: '正在读取 TOPIC:MIKUTV-PLUGIN…',
      live: '实时 · TOPIC:MIKUTV-PLUGIN',
      error: '暂时无法连接 GitHub 接口，请稍后刷新，或直接访问 GitHub 查看 mikutv-plugin 主题。',
      empty: '没有符合条件的插件。',
      copied: '已复制',
      kindLabel: {
        source: '内容源',
        theme: '主题',
        locale: '语言包',
      },
    },
    developPage: {
      title: '插件开发手册',
      note: '内容源、主题与语言包的打包契约、运行时接口与发布规范。',
      toc: [
        { id: 'contract', num: '01', label: '打包与分发' },
        { id: 'source', num: '02', label: '内容源' },
        { id: 'sandbox', num: '03', label: '沙盒宿主接口' },
        { id: 'capabilities', num: '04', label: '能力导出表' },
        { id: 'theme', num: '05', label: '主题' },
        { id: 'locale', num: '06', label: '语言包' },
        { id: 'publish', num: '07', label: '校验与发布' },
      ],
      capFilter: '筛选能力或导出函数名',
      figureCaption: '发布正式 RELEASE 并添加 TOPIC:MIKUTV-PLUGIN 后，插件会自动出现在电视端发现列表中。',
    },
    footer: {
      plugins: '插件',
      develop: '开发',
    },
  },
} as const
