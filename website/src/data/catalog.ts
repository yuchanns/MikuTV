export interface ThemeSpec {
  id: string
  repo: string
  name: string
  family: string
  appearance: 'dark' | 'light'
  canvas: string
  sidebar: string
  panel: string
  accent: string
  accentSoft: string
  text: string
  muted: string
  swatches: [string, string, string, string, string, string]
}

export interface PluginEntry {
  fullName: string
  name: string
  displayName: string
  owner: string
  avatarUrl: string
  description: string
  kind: 'source' | 'theme' | 'locale'
  topics: string[]
  stars: number
  updatedAt: string
  htmlUrl: string
  releasesUrl: string
  license: string
  official: boolean
  swatches?: [string, string, string, string, string, string]
  appearance?: 'dark' | 'light'
}

export const LAUNCH_VIDEO_URL =
  'https://github.com/user-attachments/assets/f3868d0c-1b14-4be0-ae68-f7bb5268de82'

export const OFFICIAL_THEMES: ThemeSpec[] = [
  {
    id: 'mikutv-dark',
    repo: 'yuchanns/MikuTV',
    name: 'MikuTV Dark',
    family: 'Built-in',
    appearance: 'dark',
    canvas: '#080B11',
    sidebar: '#06080D',
    panel: '#101621',
    accent: '#39C5BB',
    accentSoft: '#8EE4D9',
    text: '#F4F6FA',
    muted: '#8D99AE',
    swatches: ['#080B11', '#101621', '#162C30', '#39C5BB', '#8EE4D9', '#F4F6FA'],
  },
  {
    id: 'mikutv-light',
    repo: 'yuchanns/MikuTV',
    name: 'MikuTV Light',
    family: 'Built-in',
    appearance: 'light',
    canvas: '#EDF1F5',
    sidebar: '#E3EAF0',
    panel: '#F7FAFC',
    accent: '#0E7C75',
    accentSoft: '#39C5BB',
    text: '#111922',
    muted: '#526173',
    swatches: ['#EDF1F5', '#F7FAFC', '#D5ECEA', '#0E7C75', '#39C5BB', '#111922'],
  },
  {
    id: 'tokyonight',
    repo: 'yuchanns/mikutv-theme-tokyonight',
    name: 'Tokyo Night',
    family: 'Tokyo Night',
    appearance: 'dark',
    canvas: '#16161E',
    sidebar: '#13131A',
    panel: '#1A1B26',
    accent: '#7AA2F7',
    accentSoft: '#89B4FA',
    text: '#C0CAF5',
    muted: '#9AA5CE',
    swatches: ['#16161E', '#1A1B26', '#283457', '#7AA2F7', '#89B4FA', '#C0CAF5'],
  },
  {
    id: 'tokyonight-day',
    repo: 'yuchanns/mikutv-theme-tokyonight-day',
    name: 'Tokyo Night Day',
    family: 'Tokyo Night',
    appearance: 'light',
    canvas: '#E1E2E7',
    sidebar: '#D5D6DB',
    panel: '#ECEEF4',
    accent: '#2E64B5',
    accentSoft: '#2557A4',
    text: '#262B40',
    muted: '#4A516E',
    swatches: ['#E1E2E7', '#ECEEF4', '#C4D0EB', '#2E64B5', '#2557A4', '#262B40'],
  },
  {
    id: 'catppuccin-mocha',
    repo: 'yuchanns/mikutv-theme-catppuccin-mocha',
    name: 'Catppuccin Mocha',
    family: 'Catppuccin',
    appearance: 'dark',
    canvas: '#1E1E2E',
    sidebar: '#181825',
    panel: '#242437',
    accent: '#CBA6F7',
    accentSoft: '#DFC6FB',
    text: '#CDD6F4',
    muted: '#A6ADC8',
    swatches: ['#1E1E2E', '#242437', '#3B3354', '#CBA6F7', '#DFC6FB', '#CDD6F4'],
  },
  {
    id: 'catppuccin-latte',
    repo: 'yuchanns/mikutv-theme-catppuccin-latte',
    name: 'Catppuccin Latte',
    family: 'Catppuccin',
    appearance: 'light',
    canvas: '#EFF1F5',
    sidebar: '#E6E9EF',
    panel: '#F5F6FA',
    accent: '#7C35DB',
    accentSoft: '#6E2ECE',
    text: '#363952',
    muted: '#5C5F77',
    swatches: ['#EFF1F5', '#F5F6FA', '#E2D7F6', '#7C35DB', '#6E2ECE', '#363952'],
  },
  {
    id: 'nord',
    repo: 'yuchanns/mikutv-theme-nord',
    name: 'Nord',
    family: 'Nord',
    appearance: 'dark',
    canvas: '#242933',
    sidebar: '#1E222A',
    panel: '#2E3440',
    accent: '#88C0D0',
    accentSoft: '#A3D5E3',
    text: '#ECEFF4',
    muted: '#BDC6D6',
    swatches: ['#242933', '#2E3440', '#334559', '#88C0D0', '#A3D5E3', '#ECEFF4'],
  },
  {
    id: 'nord-light',
    repo: 'yuchanns/mikutv-theme-nord-light',
    name: 'Nord Light',
    family: 'Nord',
    appearance: 'light',
    canvas: '#ECEFF4',
    sidebar: '#E2E7EF',
    panel: '#F3F6FA',
    accent: '#3D6691',
    accentSoft: '#345C85',
    text: '#2E3440',
    muted: '#4C566A',
    swatches: ['#ECEFF4', '#F3F6FA', '#D3E1F0', '#3D6691', '#345C85', '#2E3440'],
  },
  {
    id: 'gruvbox-dark',
    repo: 'yuchanns/mikutv-theme-gruvbox-dark',
    name: 'Gruvbox Dark',
    family: 'Gruvbox',
    appearance: 'dark',
    canvas: '#1D2021',
    sidebar: '#17191A',
    panel: '#282828',
    accent: '#FABD2F',
    accentSoft: '#FDD469',
    text: '#FBF1C7',
    muted: '#D5C4A1',
    swatches: ['#1D2021', '#282828', '#453A26', '#FABD2F', '#FDD469', '#FBF1C7'],
  },
  {
    id: 'gruvbox-light',
    repo: 'yuchanns/mikutv-theme-gruvbox-light',
    name: 'Gruvbox Light',
    family: 'Gruvbox',
    appearance: 'light',
    canvas: '#F5ECD0',
    sidebar: '#EBE0BE',
    panel: '#FAF3DC',
    accent: '#9A5B0B',
    accentSoft: '#8C5108',
    text: '#282828',
    muted: '#504945',
    swatches: ['#F5ECD0', '#FAF3DC', '#E8D5A6', '#9A5B0B', '#8C5108', '#282828'],
  },
  {
    id: 'rose-pine',
    repo: 'yuchanns/mikutv-theme-rose-pine',
    name: 'Rosé Pine',
    family: 'Rosé Pine',
    appearance: 'dark',
    canvas: '#191724',
    sidebar: '#14121E',
    panel: '#1F1D2E',
    accent: '#EBBCBA',
    accentSoft: '#F5D5D3',
    text: '#E0DEF4',
    muted: '#ABA6C4',
    swatches: ['#191724', '#1F1D2E', '#392C42', '#EBBCBA', '#F5D5D3', '#E0DEF4'],
  },
  {
    id: 'rose-pine-dawn',
    repo: 'yuchanns/mikutv-theme-rose-pine-dawn',
    name: 'Rosé Pine Dawn',
    family: 'Rosé Pine',
    appearance: 'light',
    canvas: '#F6EFE7',
    sidebar: '#EDE3DA',
    panel: '#FAF5EE',
    accent: '#A34864',
    accentSoft: '#933F59',
    text: '#3E395B',
    muted: '#656084',
    swatches: ['#F6EFE7', '#FAF5EE', '#EEDADF', '#A34864', '#933F59', '#3E395B'],
  },
]

export const THEME_SWATCH_MAP: Record<
  string,
  { swatches: [string, string, string, string, string, string]; appearance: 'dark' | 'light'; displayName: string }
> = Object.fromEntries(
  OFFICIAL_THEMES.filter((t) => t.repo !== 'yuchanns/MikuTV').map((t) => [
    t.repo.toLowerCase(),
    { swatches: t.swatches, appearance: t.appearance, displayName: t.name },
  ]),
)

export const CAPABILITY_ROWS = [
  { cap: 'catalog.browse', exports: 'browse', dep: '—', group: 'catalog' },
  { cap: 'catalog.item', exports: 'getItem', dep: '—', group: 'catalog' },
  { cap: 'catalog.search', exports: 'search', dep: '—', group: 'catalog' },
  { cap: 'catalog.searchSuggest', exports: 'searchSuggest', dep: 'catalog.search', group: 'catalog' },
  { cap: 'catalog.series', exports: 'readSeries', dep: '—', group: 'catalog' },
  { cap: 'catalog.live', exports: 'readLive', dep: '—', group: 'catalog' },
  { cap: 'catalog.creator', exports: 'readCreator', dep: '—', group: 'catalog' },
  { cap: 'catalog.videoComments', exports: 'videoComments', dep: '—', group: 'catalog' },
  { cap: 'catalog.following', exports: 'readFollowing', dep: 'account.signIn', group: 'catalog' },
  { cap: 'catalog.library', exports: 'readAccount', dep: 'account.signIn', group: 'catalog' },
  { cap: 'playback.resolve', exports: 'resolvePlayback', dep: '—', group: 'playback' },
  { cap: 'playback.progress', exports: 'reportPlayback', dep: 'account.signIn', group: 'playback' },
  { cap: 'playback.danmaku', exports: 'readDanmaku', dep: '—', group: 'playback' },
  { cap: 'playback.subtitles', exports: 'readSubtitles', dep: '—', group: 'playback' },
  { cap: 'playback.modes', exports: '—', dep: '—', group: 'playback' },
  { cap: 'live.chat', exports: 'connectLiveChat', dep: '—', group: 'live' },
  { cap: 'live.chatWrite', exports: 'sendLiveChat', dep: 'live.chat + account.signIn', group: 'live' },
  {
    cap: 'account.signIn',
    exports: 'restoreAccount, signOut, beginSignIn/pollSignIn or signInWithCredentials',
    dep: 'authentication.methods',
    group: 'account',
  },
  { cap: 'account.videoInteraction', exports: 'editVideo', dep: 'account.signIn', group: 'account' },
  {
    cap: 'account.videoFavorites',
    exports: 'videoFavoriteFolders, editVideoFavoriteFolders',
    dep: 'account.signIn',
    group: 'account',
  },
  { cap: 'account.danmaku', exports: 'postDanmaku', dep: 'account.signIn', group: 'account' },
  {
    cap: 'account.danmakuRules',
    exports: 'readDanmakuRules, addDanmakuRule, removeDanmakuRule',
    dep: 'account.signIn',
    group: 'account',
  },
  { cap: 'account.followCreator', exports: 'followCreator', dep: 'account.signIn', group: 'account' },
  { cap: 'account.trackSeries', exports: 'trackSeries', dep: '—', group: 'account' },
  {
    cap: 'account.history / watchLater / favorites / tracking / courses / notifications',
    exports: 'readAccount',
    dep: 'account.signIn',
    group: 'account',
  },
  { cap: 'account.editFavoriteFolders', exports: 'editFavoriteFolder', dep: 'account.favorites', group: 'account' },
  {
    cap: 'account.editHistory / editWatchLater / editTracking',
    exports: 'removeAccountItems',
    dep: 'matching read capability',
    group: 'account',
  },
  {
    cap: 'account.privateMessages',
    exports: 'readPrivateConversations, readPrivateMessages',
    dep: 'account.signIn',
    group: 'account',
  },
  {
    cap: 'account.writePrivateMessages',
    exports: 'sendPrivateMessage, markPrivateMessagesRead',
    dep: 'account.privateMessages',
    group: 'account',
  },
]
