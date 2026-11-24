package tvui

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/plugin"
	"github.com/yuchanns/MikuTV/internal/provider"
)

//go:embed assets
var files embed.FS

type section struct {
	ID, Name, Icon string
}

type labels struct {
	Nav                                                                   map[app.Page]string
	EntriesTitle, EntriesNote, CurrentEntry, EmptyEntries, EmptyEntryNote string
	Manage, EmptyTitle, EmptyNote, SwitchEntry                            string
	LoadingTitle, LoadingNote, FailedTitle, FailedNote                    string
	CanceledTitle, Retry, Cancel, Refresh                                 string
	SettingsTitle, SettingsNote, EmptySettings                            string
	Version, Platform, AboutNote, Licenses, Back, Close, SaveFailed       string
	Sections                                                              []section
	Auth                                                                  authLabels
	Activity                                                              accountActivityLabels
	Home                                                                  homeLabels
	Search                                                                searchLabels
	Following                                                             followingLabels
	Series                                                                seriesLabels
	Live                                                                  liveLabels
	Creator                                                               creatorLabels
	Detail                                                                detailLabels
	Playback                                                              playbackLabels
	PlaybackPrefs                                                         playbackSettingsLabels
	Danmaku                                                               danmakuSettingsLabels
	Appearance                                                            appearanceSettingsLabels
	Privacy                                                               privacySettingsLabels
	About                                                                 aboutLabels
	Plugins                                                               pluginLabels
	TopShelf                                                              topShelfLabels
	Modal                                                                 modalLabels
}

type pluginLabels struct {
	InstalledTitle, InstalledNote                   string
	DiscoverButton, AddRepoButton, CheckUpdates     string
	BackInstalled, BackManage, BackDiscover         string
	BackThemePicker                                 string
	DiscoverTitle, DiscoverNote                     string
	OfflineCachedNote, RateLimitedCachedNote        string
	SearchPlaceholder                               string
	FilterAll, FilterSource, FilterTheme, FilterLocale string
	KindSource, KindTheme, KindLocale, KindUnknown     string
	BuiltinTag, BuiltinRepo, BuiltinUpdateNote         string
	StatusCurrentEntry, StatusEnabled, StatusOff       string
	StatusCurrentTheme, StatusCurrentLocale            string
	StatusInstalled                                    string
	StatusUninstalled, StatusNeedAppUpdate          string
	StatusThemeUnavailable, StatusIncompatible      string
	EnableLabel, EnableCurrentNote, EnableReadyNote string
	VersionLabel, LatestVersionNote, SourceLabel    string
	CompatLabel, CompatOK, CompatOKNote             string
	CompatNeedAppNote, CompatThemeNote              string
	CompatProtocolNote, CompatAuthNote              string
	CompatInvalidNote                               string
	InstallLabel, InstallNote                       string
	LoadingTitle, EmptyTitle, EmptyNote             string
	ErrorTitle, ErrorNote, RateLimitErrorNote       string
	NoMatchTitle, NoMatchNote, ClearSearch          string
	NoMoreTitle, NoMoreNote, ShowAll                string
	ReloadAction, RefreshAction                     string
	LoadMoreAction, LoadingMore                     string
	UpdatesUpToDate, InstallDeferred                string
	AddRepoDeferred, ToggleDeferred                 string
	RepoDialogTitle, RepoFieldLabel                 string
	RepoPlaceholder, RepoInspectAction              string
	RepoInspecting, RepoInvalidErr                  string
	RepoNotFoundErr, RepoVersionMismatchErr         string
	RepoConflictErr, RepoResolveFailedErr           string
	InstallConfirmTitle, InstallPermissionsIntro    string
	InstallNoPermissions, InstallUserServerPermissions string
	InstallConfirmAction                            string
	InstallPendingTitle, InstallPendingBody         string
	InstallFailedTitle, InstallFailedDefault        string
	InstallFailedNotInstalled, InstallRetryAction   string
	InstallRetryLater, InstallSuccessTitle          string
	InstallSuccessBody, InstallOpenEntries          string
	InstallDone, InstallAlreadyInstalled            string
	InstallErrDigest, InstallErrConflict            string
	InstallErrIncompatible, InstallErrNetwork       string
	CheckingUpdates, UpdatesBuiltinOnly             string
	UpdatesFound, UpdatesCheckFailed                string
	UpdateAvailableTag, UpdateAvailableNote         string
	CheckSingleUpdateNote, UpdateDialogTitle        string
	UpdateAddedPermission, UpdateNoNewPermissions   string
	UpdateConfirmAllowAction, UpdateConfirmAction   string
	UpdatePendingTitle, UpdatePendingBody           string
	UpdateFailedTitle, UpdateFailedPreserved        string
	UpdateErrKindChanged, UpdateSuccessToast        string
	EnableDisabledNote, RemoveLabel, RemoveNote     string
	DisableDialogTitle, DisableDialogBody           string
	DisableCurrentNote, DisableConfirmAction        string
	RemoveDialogTitle, RemoveDialogBody             string
	RemoveConfirmAction                             string
	EnabledToast, DisabledToast, RemovedToast       string
	ToggleFailedToast, RemoveFailedToast            string
	ThemeEnableActiveNote, ThemeEnableReadyNote     string
	ThemeInstallNote, ThemeInstallConfirmBody       string
	ThemeInstallSuccessBody, ThemeOpenAppearance    string
	ThemeUpdateConfirmBody, ThemeRemoveNote         string
	ThemeRemoveActiveBody, ThemeRemoveInactiveBody  string
	ThemeDisableActiveBody                          string
	LocaleEnableActiveNote, LocaleEnableReadyNote   string
	LocaleInstallNote, LocaleInstallConfirmBody     string
	LocaleInstallSuccessBody, LocaleUpdateConfirmBody string
	LocaleRemoveNote, LocaleRemoveActiveBody        string
	LocaleRemoveInactiveBody, LocaleDisableActiveBody string
	BackLanguagePicker                              string
	RecoveryBannerStarting, RecoveryBannerRunning   string
	RecoveryBannerDone, RecoveryBannerPartial       string
}

type appearanceSettingsLabels struct {
	Title, Intro                                    string
	LanguageLabel                                   string
	LanguageSystem, LanguageSystemNote              string
	LanguageCustomNote                              string
	LanguagePickerTitle, LanguagePickerSubtitle     string
	LanguageBuiltinNote, LanguagePluginNote         string
	LanguageBuiltinOnlyHint, DiscoverMoreLanguages  string
	LanguageAppliedToast                            string
	ModeLabel                                       string
	ModeSystem, ModeLight, ModeDark                 string
	ModeSystemNote, ModeLightNote, ModeDarkNote     string
	ModePickerTitle, ModePickerSubtitle             string
	ModeAppliedToast                                string
	LightThemeLabel, LightThemeNote                 string
	LightThemeActiveNote                            string
	DarkThemeLabel, DarkThemeNote                   string
	DarkThemeActiveNote                             string
	HoverPreviewLabel, HoverPreviewNote             string
	ReduceMotionLabel, ReduceMotionNote             string
	LightPickerTitle, LightPickerSubtitle           string
	DarkPickerTitle, DarkPickerSubtitle             string
	BuiltinDarkThemeNote, BuiltinLightThemeNote     string
	InstalledDarkThemeNote, InstalledLightThemeNote string
	BuiltinOnlyHint, DiscoverMoreThemes             string
	ErrorTitle, ErrorBody                           string
	ErrorReselect, ErrorReturn                      string
	ThemeAppliedToast                               string
	LightThemeSavedToast, DarkThemeSavedToast       string
}

type topShelfLabels struct {
	SectionContinue, SectionRecommended                     string
	SectionContinueWithSource, SectionRecommendedWithSource string
	EntryUnavailable                                        string
}

type aboutLabels struct {
	Plugins, NoPlugins, PluginsMore, Bundled, External string
	KindSource, KindTheme, KindLocale                  string
	ProjectHome, ProjectBody                           string
	LicenseSummary                                     string
	LicenseBuildHeader, LicensePluginsHeader           string
	LicenseDepsHeader, LicenseFullHeader               string
	LicenseNoPlugins, LicenseDeclaredNone              string
}

type privacySettingsLabels struct {
	ClearSearch, ClearHistory                      string
	ClearSearchTitle, ClearSearchBody              string
	ClearHistoryTitle, ClearHistoryBody            string
	ConfirmClear, Cleared                          string
	ClearFailed, SyncSaveFailed                    string
	DetailsTitle, DetailsBody, ManageAction        string
}

type playbackSettingsLabels struct {
	Title, Intro                                          string
	Quality, QualityNote, QualityAuto                     string
	Speed                                                 string
	Subtitles, SubtitlesOff, SubtitlesChinese, SubtitlesEnglish string
	Autoplay, AutoplayNote                                string
}

type danmakuSettingsLabels struct {
	Title, Intro                                                   string
	DefaultShow, Density, Area, Opacity, FontSize                  string
	Blocklist, BlocklistNote                                       string
	AreaUpper, AreaFull, AreaTop                                   string
	SizeNormal, SizeLarge                                          string
	RulesBack, RulesTitle, RulesNote                               string
	RulesSynced, RulesSyncing, RulesSyncFailed, RulesGuest         string
	RulesLocalOnly, AddRule, FilterAll, FilterKeyword, FilterUser  string
	EmptyRules, EmptyRulesNote                                     string
	AddRuleTitle, KeywordPlaceholder, UserPlaceholder              string
	EditRule, SaveRule, SavingRule                                 string
	RuleRequired, RuleInvalidUser, RuleDuplicate                   string
	RuleAdded, RuleDeleted, RuleRemove                             string
	RuleSaveFailed, RuleRemoveFailed                               string
}

type modalLabels struct {
	CurrentPrefix, Current, ActiveNote, SwitchNote, ItemsCount string
	HintSelect, HintSwitch, HintApply, HintConfirm, HintSave   string
	HintOpen, HintClose                                        string
}

type followingLabels struct {
	Eyebrow, Title, Subtitle                        string
	TabAll, TabLive                                 string
	UpdatesTitle, LiveTitle, LiveNote, LiveBadge    string
	GuestTitle, GuestNote                           string
	EmptyCreatorsTitle, EmptyCreatorsNote           string
	EmptyUpdates, EmptyLive                         string
	Loading, LoadFailed, MoreFailed                 string
}

type seriesLabels struct {
	Eyebrow, PicksTitle, PicksNote, TrackedTitle, TrackedNote   string
	Index, IndexNote, Calendar, CalendarNote                    string
	Reset, Results, WorksCount, RatingSuffix                    string
	NoFilterResults, NoFilterNote                               string
	Today, MonthSuffix, Scheduled, NoUpdates, NoUpdatesNote     string
	Track, Tracked, TrackRemoved                                string
	Loading, LoadFailed, MoreFailed                             string
	Weekdays                                                    []string
}

type liveLabels struct {
	Eyebrow, LiveNow, Viewers, EnterRoom                 string
	RecommendedTitle, RecommendedNote                    string
	FollowingTitle, FollowingNote                        string
	EmptyArea, EmptyAreaNote                             string
	Loading, LoadFailed, MoreFailed                      string
}

type creatorLabels struct {
	Eyebrow, Videos, Posts, Collections, About         string
	Followers, Works, CollectionWorks                  string
	Location, Category, EmptyBio                       string
	NoVideos, NoPosts, NoCollections                   string
	Loading, LoadFailed, MoreFailed                    string
}

type searchLabels struct {
	Eyebrow, Title, Hint, Placeholder                                 string
	History, Suggestions, HotSearch                                   string
	PopularHeading, ResultsHeading, NoResults, NoResultsHint          string
	KindAll, KindVideo, KindCreator, KindSeries, KindLive             string
	SortDefault, SortNewest, SortViews                                string
	FiltersTitle, SortLabel, DurationLabel                            string
	DurationAll, DurationShort, DurationMedium, DurationLong          string
	ApplyFilters, Followers, VideoCount, CreatorVideos, DetailPending string
	MoreFailed                                                        string
}

type playbackLabels struct {
	Info, Previous, Next, Play, Pause, Comments, Danmaku, Subtitles, Quality, Speed, Progress, Chapters   string
	ShowControls, On, Off, Auto, Buffering, Failed, Ended, Replay                                         string
	FailedNote, EndedNote, Reconnect, Episodes                                                            string
	Access                                                                                                playbackAccessLabels
	LoginLimit, MembershipLimit, PurchaseLimit, RegionLimit, DeviceLimit, SourceLimit                     string
	Density, Opacity, SendDanmaku, DanmakuPlaceholder, Send, DanmakuSent, ScrollMode, TopMode, BottomMode string
	SubtitleFailed, PickFavoriteFolders, FavoriteSaved, ChapterHere, SeekTo, MoreComments                 string
	QualityGuest, QualitySignedIn, QualityMember                                                          string
	RoomInfo, RoomNumber, RoomEmptyDesc, LiveQuality, Line, LineAuto, LineChanged                         string
	LiveDisconnected, LiveDisconnectedBody, LiveEnded, LiveEndedBody, MoreLive                            string
	LiveChatTitle, LiveChatViewers, LiveChatSend, LiveChatMuted                                           string
	LiveChatConnecting, LiveChatReconnecting, LiveChatDisconnected, LiveChatEmpty, LiveChatGuest          string
	LiveChatSendFailed, LiveChatRateLimited                                                               string
	Interactive, BranchQuestion, BranchButton                                                             string
	AudioTrack, AudioDefault                                                                              string
}

type playbackAccessLabels struct {
	DeviceTitle, DeviceBody, DeviceGeneric, DeviceGenericBody, UseSDR, Keep, UseBest, SourceTitle, SourceBody, SourceGeneric      string
	RegionTitle, RegionBody, SeeRelated, VIPTitle, VIPBody, VIPNoPreview, MembershipHelp, MembershipInstructions, Refresh, Return string
	PurchaseTitle, PurchaseBody, TrialMinutes, TrialState, TrialLeft                                                              string
	TrialEndedTitle, TrialEndedBody, PurchaseTrialEndedBody, QualityVIPTitle, QualityVIPBody                                      string
	TrialSeconds, MobileClient                                                                                                    string
}

type detailLabels struct {
	Overview, Loading, Empty, CommentCount, Unavailable, Denied, SignIn, LoadFailed        string
	AccessGranted, AccessPreview, AccessRestricted                                         string
	Episodes, CurrentVideo, Collection, Comments, Related                                  string
	Follow, Followed, Unfollowed, Like, Favorite, Preview, GetAccess                       string
	ActionsUnavailable, NoComments, NoRelated, NoSegments, UnknownAuthor                   string
	UpdateFailed, Partial                                                                  string
	Confirm, Reconsider, ActionDone                                                        string
	LaterAdded, LaterRemoved, FavoriteAdded, FavoriteRemoved                               string
	FavoriteAddedTo                                                                        string
	WriteComment, Reply, Liked, More, Collapse                                             string
	Hot, Latest, AllComments, RootComment, AllReplies, ReplyCount, MoreReplies             string
	CommentPlaceholder, ReplyTo, CreatorMark, Pinned, Dislike, EditComment                 string
	PostComment, CommentInput, CommentPosted, CommentLoadFailed, Done, KeyboardPlaceholder string
	MinutesAgo, HoursAgo, DaysAgo, JustNow, Kicker, SeriesKicker                           string
	CourseKicker, SeriesUnit, CourseUnit, EpisodeUpcoming, EpisodeUpcomingNotice           string
	EpisodeLocked                                                                          string
}

type homeLabels struct {
	ForYou, Popular, Ranking, Regions, RankingNote                                         string
	PopularEyebrow, RankingEyebrow, RegionsEyebrow, RankingHeat                            string
	HeroCaption, EyebrowAfterHours, EyebrowMorning, EyebrowMidday, EyebrowAfternoon        string
	EyebrowTonight                                                                         string
	Continue, ContinueNote, Picks, PicksNote, Following, FollowingNote, SeeAll             string
	Play, Details, Later, Featured, Feature, Refresh, RefreshFailed, MoreFailed string
	Recommendations, RateLimited, Plays, TenThousand, HundredMillion            string
}

type accountActivityLabels struct {
	History, Favorites, WatchLater, Messages, MessagesNote, MessagesUnread  string
	NotificationTab, PrivateTab, ReadAllNotices, MarkingRead, MarkedAllRead string
	OpenNoticeTarget, OpenNoticeCreator                                     string
	PrivateConversation, WritePrivateMessage, PrivateDraftPlaceholder       string
	EditMessage, SendMessage, SendingMessage, MessageSent                   string
	NewConversation, LoadOlderMessages, LoadMoreConversations               string
	EmptyConversations, PrivateLoadFailed, PrivateSendFailed                string
	PrivateRateLimited, PrivatePermissionDenied, PrivateScrollMessages      string
	RefreshingRights, RefreshedRights                                       string
	All, Replies, Likes, System                                             string
	SyncOn, SyncOff, SyncNote, SyncAction                                   string
	HistorySync, HistorySyncNote, Enabled, Disabled                         string
	PrivacyIntro                                                            string
	More, Unavailable, SignInNeeded, SignInAction                           string
	FavoritesTitle, FavoritesNote, FoldersBack, FolderContents              string
	FolderCount, FolderPrivate, FolderPublic                                string
	CreateFolder, FolderSettings, EditFolder, DeleteFolder                  string
	FolderName, FolderNamePlaceholder, FolderNameRequired, FolderDuplicate  string
	PrivateFolder, SaveFolder, ManageFolder, DoneManaging                   string
	CancelFolder                                                            string
	SelectedFavorites, SelectAll, ClearSelection                            string
	DeleteFolderTitle, DeleteFolderNote, RemoveFavoritesNote, SavingFolder  string
	LibraryHistory, Tracking, Courses, PlayAll, DiscoverContent             string
	LibraryGuestTitle, LibraryGuestNote, LibraryUnavailable, RemoveSelected string
	ConfirmRemove                                                           string
	LibraryEmpty                                                            map[provider.AccountSection]struct{ Title, Note string }
}

type authLabels struct {
	Body, QR, Waiting, Scanned, PhoneConfirm, Secure                string
	Success, Continuing, Expired, ExpiredBody, Refresh, Loading     string
	Accounts, AccountsNote, Add, SignOut, SignOutTitle, SignOutNote string
	Failed, NetworkFailed, AccountExpired                           string
	AccountTitle, ScanLogin                                         string
	CredentialsLogin, CredentialsTitle, CredentialsBody             string
	ServerURLLabel, ServerURLPlaceholder                            string
	UsernameLabel, UsernamePlaceholder                              string
	PasswordLabel, PasswordPlaceholder                              string
	SubmitCredentials, SubmittingCredentials                        string
	ServerURLInvalid, UsernameRequired, InvalidCredentials          string
	Connected, AccountStatus, AccountNormal, SignOutCancel          string
}

type Assets struct {
	Text         labels
	LocaleTag    string
	Brand        ui.ImageSource
	BrandLight   ui.ImageSource
	License      string
	baseZH       []byte
	baseEN       []byte
	baseSections []section
	icons        map[string]*ui.SVG
	images       map[string]ui.ImageSource
}

// BrandMark returns the frameless brand mark tuned for the active dark or light appearance.
func (a *Assets) BrandMark(dark bool) ui.ImageSource {
	if !dark && a.BrandLight != nil {
		return a.BrandLight
	}
	return a.Brand
}

func LoadAssets() (*Assets, error) {
	a := &Assets{
		LocaleTag: app.LocaleBuiltinZH,
		icons:     make(map[string]*ui.SVG),
		images:    make(map[string]ui.ImageSource),
	}
	text, err := files.ReadFile("assets/labels.json")
	if err != nil {
		return nil, err
	}
	enText, err := files.ReadFile("assets/labels.en.json")
	if err != nil {
		return nil, err
	}
	a.baseZH = text
	a.baseEN = enText
	if err := json.Unmarshal(text, &a.Text); err != nil {
		return nil, fmt.Errorf("decode labels: %w", err)
	}
	a.baseSections = append([]section(nil), a.Text.Sections...)
	icons, err := files.ReadDir("assets/icons")
	if err != nil {
		return nil, err
	}
	for _, file := range icons {
		data, err := files.ReadFile("assets/icons/" + file.Name())
		if err != nil {
			return nil, err
		}
		icon, err := ui.ParseSVG(data)
		if err != nil {
			return nil, fmt.Errorf("decode icon %s: %w", file.Name(), err)
		}
		name := file.Name()[:len(file.Name())-4]
		a.icons[name] = icon
		if err := a.loadDetailIconVariants(name, data); err != nil {
			return nil, err
		}
	}
	for _, image := range []struct {
		path string
		dst  *ui.ImageSource
	}{{"assets/brand.svg", &a.Brand}, {"assets/brand-light.svg", &a.BrandLight}} {
		data, err := files.ReadFile(image.path)
		if err != nil {
			return nil, err
		}
		*image.dst, err = ui.DecodeImage(data)
		if err != nil {
			return nil, fmt.Errorf("decode %s: %w", image.path, err)
		}
	}
	license, err := files.ReadFile("assets/LICENSE.txt")
	if err != nil {
		return nil, err
	}
	a.License = string(license)
	return a, nil
}

func (a *Assets) loadDetailIconVariants(name string, data []byte) error {
	type strokeVariant struct{ prefix, from, to string }
	var variants []strokeVariant
	switch name {
	case "back", "check", "comment", "pin", "play", "plus", "reply", "star", "user":
		variants = []strokeVariant{{"detail/", "2", "1.7"}}
	case "coin":
		variants = []strokeVariant{{"detail-primary/", "1.7", "2"}}
	case "like":
		variants = []strokeVariant{{"detail-primary/", "1.7", "2"}, {"detail-vote/", "1.7", "1.8"}}
	case "dislike":
		variants = []strokeVariant{{"detail-vote/", "2", "1.8"}}
	}
	for _, variant := range variants {
		from, to := fmt.Sprintf(`stroke-width="%s"`, variant.from), fmt.Sprintf(`stroke-width="%s"`, variant.to)
		variantData := bytes.Replace(data, []byte(from), []byte(to), 1)
		if bytes.Equal(data, variantData) {
			return fmt.Errorf("missing %s on icon %s", from, name)
		}
		icon, err := ui.ParseSVG(variantData)
		if err != nil {
			return fmt.Errorf("decode icon %s%s: %w", variant.prefix, name, err)
		}
		a.icons[variant.prefix+name] = icon
	}
	return nil
}

// PrepareArtwork decodes an already validated package cover before rendering.
func (a *Assets) PrepareArtwork(key string, data []byte) {
	if len(data) != 0 {
		a.images[key], _ = ui.DecodeImage(data)
	}
}

func (a *Assets) PrepareAccountArtwork(entryID string, data []byte) {
	a.PrepareArtwork("account/"+entryID, data)
}

func (a *Assets) AccountArtwork(entryID string) ui.ImageSource {
	return a.Artwork("account/" + entryID)
}

func (a *Assets) Icon(name string) *ui.SVG {
	if icon := a.icons[name]; icon != nil {
		return icon
	}
	return a.icons["info"]
}

func (a *Assets) Artwork(path string) ui.ImageSource {
	return a.images[path]
}

// LangTag returns the BCP-47 shaping tag for UI text rendering.
func (a *Assets) LangTag() string {
	if a == nil || a.LocaleTag == "" || strings.EqualFold(a.LocaleTag, app.LocaleBuiltinZH) {
		return "zh-CN"
	}
	return a.LocaleTag
}

// FontFamily selects the system UI font stack with script-appropriate CJK priority.
func (a *Assets) FontFamily() string {
	if a != nil && strings.HasPrefix(strings.ToLower(a.LocaleTag), "ja") {
		return "system-ui, Hiragino Sans, PingFang SC"
	}
	return "system-ui, PingFang SC, Hiragino Sans"
}

// ApplyLocale hot-swaps UI strings to a built-in language ("zh-Hans" / "en")
// or overlays a declarative kind:"locale" plugin JSON payload on top of English.
func (a *Assets) ApplyLocale(tag string, overrideJSON []byte) error {
	if a == nil {
		return nil
	}
	normalized := plugin.NormalizeLocaleTag(tag)
	if normalized == "" {
		normalized = app.LocaleBuiltinZH
	}
	var next labels
	if len(bytes.TrimSpace(overrideJSON)) > 0 {
		merged, err := mergeLocaleJSON(a.baseEN, overrideJSON)
		if err != nil {
			return fmt.Errorf("merge locale %s: %w", normalized, err)
		}
		if err := json.Unmarshal(merged, &next); err != nil {
			return fmt.Errorf("decode merged locale %s: %w", normalized, err)
		}
	} else if strings.HasPrefix(strings.ToLower(normalized), "en") {
		if err := json.Unmarshal(a.baseEN, &next); err != nil {
			return fmt.Errorf("decode en labels: %w", err)
		}
	} else {
		if err := json.Unmarshal(a.baseZH, &next); err != nil {
			return fmt.Errorf("decode zh labels: %w", err)
		}
	}
	next.Sections = normalizeLocaleSections(a.baseSections, next.Sections)
	a.Text = next
	a.LocaleTag = normalized
	return nil
}

func mergeLocaleJSON(baseJSON, overrideJSON []byte) ([]byte, error) {
	var baseObj map[string]any
	if err := json.Unmarshal(baseJSON, &baseObj); err != nil {
		return nil, err
	}
	var overObj map[string]any
	if err := json.Unmarshal(overrideJSON, &overObj); err != nil {
		return nil, err
	}
	deepMergeLocaleMap(baseObj, overObj)
	return json.Marshal(baseObj)
}

func deepMergeLocaleMap(dst, src map[string]any) {
	for k, v := range src {
		if v == nil {
			continue
		}
		if s, ok := v.(string); ok {
			if strings.TrimSpace(s) == "" {
				continue
			}
			dst[k] = s
			continue
		}
		if srcMap, ok := v.(map[string]any); ok {
			if dstMap, exists := dst[k].(map[string]any); exists {
				deepMergeLocaleMap(dstMap, srcMap)
				continue
			}
			dst[k] = srcMap
			continue
		}
		if srcSlice, ok := v.([]any); ok {
			if len(srcSlice) > 0 {
				dst[k] = srcSlice
			}
			continue
		}
		dst[k] = v
	}
}

func normalizeLocaleSections(canonical, translated []section) []section {
	if len(canonical) == 0 {
		return translated
	}
	byID := make(map[string]string, len(translated))
	for _, s := range translated {
		id := strings.ToLower(strings.TrimSpace(s.ID))
		name := strings.TrimSpace(s.Name)
		if id != "" && name != "" {
			byID[id] = name
		}
	}
	out := make([]section, len(canonical))
	for i, base := range canonical {
		out[i] = base
		if name, ok := byID[strings.ToLower(strings.TrimSpace(base.ID))]; ok {
			out[i].Name = name
		}
	}
	return out
}
