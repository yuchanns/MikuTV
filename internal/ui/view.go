package tvui

import (
	"fmt"
	"log"
	"time"

	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/provider"
	"github.com/yuchanns/MikuTV/internal/theme"
	"github.com/yuchanns/MikuTV/internal/topshelf"
)

const (
	focusDuration     = 140 * time.Millisecond
	railDuration      = 220 * time.Millisecond
	focusBottomMargin = 64
	screenBottomInset = 80
)

var focusScroll = ui.Animation{Duration: 220 * time.Millisecond, Ease: ui.EaseOut}

var menuTransition = ui.ElementTransition{
	Duration: 200 * time.Millisecond, Ease: ui.EaseOut, Position: true,
	Enter: &ui.Motion{Y: 14}, Exit: &ui.Motion{Y: -8},
}

var videoDetailTransition = ui.ElementTransition{
	Duration: 300 * time.Millisecond,
	Ease:     ui.EaseInOut, Position: true, CacheTransform: true,
	Enter: &ui.Motion{Scale: 0.90},
	Exit:  &ui.Motion{Scale: 0.90},
}

type scrollKey struct {
	entry string
	route app.Route
}

type navigationTrace struct {
	entry string
	route app.Route
	focus app.FocusID
}

type Options struct {
	State       *app.State
	Catalog     *app.Catalog
	Search      *app.CatalogSearch
	Following   *app.FollowingFeed
	Series      *app.SeriesCatalog
	Live        *app.LiveCatalog
	LiveChat    *app.LiveChat
	Auth             *app.Auth
	Activity         *app.AccountActivity
	PrivateMessages  *app.PrivateMessages
	DanmakuRules     *app.DanmakuRules
	PlaybackSettings *app.PlaybackSettings
	Appearance       *app.AppearanceSettings
	Locale           *app.LocaleSettings
	PluginCatalog    *app.PluginCatalog
	Pages            *Pages
	Assets           *Assets
	Preferences      app.PreferencesStore
	Theme            theme.Theme
	Version          string
	About            app.AboutInfo
	TopShelf         topshelf.Store
}

type View struct {
	State            *app.State
	Catalog          *app.Catalog
	Search           *app.CatalogSearch
	Following        *app.FollowingFeed
	Series           *app.SeriesCatalog
	Live             *app.LiveCatalog
	LiveChat         *app.LiveChat
	Auth             *app.Auth
	Activity         *app.AccountActivity
	PrivateMessages  *app.PrivateMessages
	DanmakuRules     *app.DanmakuRules
	PlaybackSettings *app.PlaybackSettings
	Appearance       *app.AppearanceSettings
	Locale           *app.LocaleSettings
	PluginCatalog    *app.PluginCatalog
	Playback         *app.Playback
	Pages           *Pages
	Assets          *Assets
	Preferences     app.PreferencesStore
	Theme           theme.Theme
	Version         string
	About           app.AboutInfo
	TopShelf        topshelf.Store
	Trace           bool
	projectQRURL    string
	projectQRImage  ui.ImageSource
	lastTopShelfHash   [32]byte
	hasTopShelfHash    bool
	topShelfSnapshot   topshelf.Snapshot
	pendingTopShelfURL string
	lastOpenedURL      string
	lastOpenedAt       time.Time
	lastNavigation  navigationTrace
	native          ui.Theme
	focus           focusController
	scrolls         map[scrollKey]*ui.ScrollState
	lists           map[listKey]*ui.ListState
	activeScroll    *ui.ScrollState
	drawnRevision   uint64
	reveal          bool
	railSpan        float32
	notice          string
	qrPayload       string
	qrImage         ui.ImageSource
	qrHasAction     bool
	credDraft       credentialsDraft
	accountAvatar   *app.AccountAvatar
	avatarImage     ui.ImageSource
	profileGlow     ui.ImageSource
	cardGlow        ui.ImageSource
	home            catalogHome
	homeStates      map[string]catalogHome
	entryGeneration uint64
	search           searchViewState
	following        followingViewState
	series           seriesViewState
	live             liveViewState
	videoDetail      videoDetailState
	videoDetailStack []videoDetailState
	creator          *creatorState
	creatorStack     []*creatorState
	player           playbackView
	favorites        favoritesUI
	playbackUI       playbackSettingsUI
	danmakuUI        danmakuSettingsUI
	appearanceUI     appearanceSettingsUI
	privacyUI        privacySettingsUI
	pluginsUI        pluginsSettingsUI
	settingsFeedback videoDetailFeedback

	accountDetailsPhone bool
	openNotice          provider.AccountRow
	noticeFeedback      videoDetailFeedback
	messagesTab         messagesTabKind
	privateUI           privateMessagesUI
}

func NewView(options Options) *View {
	danmakuRules := options.DanmakuRules
	if danmakuRules == nil {
		danmakuRules = app.NewDanmakuRules(options.Preferences, nil, nil)
	}
	playbackSettings := options.PlaybackSettings
	if playbackSettings == nil {
		playbackSettings = app.NewPlaybackSettings(options.Preferences, nil)
	}
	v := &View{
		State: options.State, Catalog: options.Catalog, Search: options.Search, Following: options.Following, Series: options.Series, Live: options.Live, LiveChat: options.LiveChat, Auth: options.Auth, Activity: options.Activity, PrivateMessages: options.PrivateMessages, DanmakuRules: danmakuRules, PlaybackSettings: playbackSettings, Appearance: options.Appearance, Locale: options.Locale, PluginCatalog: options.PluginCatalog,
		Pages: options.Pages, Assets: options.Assets, Preferences: options.Preferences,
		Version: options.Version, About: options.About.Normalized(options.Version), TopShelf: options.TopShelf, scrolls: make(map[scrollKey]*ui.ScrollState),
	}
	v.SetTheme(options.Theme)
	if v.Auth != nil {
		v.Auth.SetOnSettled(func() {
			if v.Catalog != nil {
				if v.home.tab != "" || v.home.filter != "" {
					v.Catalog.Show(v.home.tab, v.home.filter)
				} else {
					v.Catalog.EnsureLoaded()
				}
			}
			v.SyncTopShelf()
			v.FlushPendingTopShelfURL()
		})
	}
	return v
}

// SetTheme atomically replaces the active resolved theme without resetting routes,
// focus, playback, or account sessions (§7.2).
func (v *View) SetTheme(next theme.Theme) {
	v.Theme = next
	v.native = nativeTheme(next)
	derived := deriveColors(next)
	v.profileGlow = accountGlow(derived.profileGlow, 0.15, 0.65)
	v.cardGlow = accountGlow(withAlpha(next.Colors.Accent.Primary, 0x29), 1, 0.75)
}

// SetLocale atomically hot-swaps UI strings, sidebar navigation labels, and
// script-appropriate font preferences without resetting routes or playback.
func (v *View) SetLocale(tag string, overrideJSON []byte) error {
	if v == nil || v.Assets == nil {
		return nil
	}
	if err := v.Assets.ApplyLocale(tag, overrideJSON); err != nil {
		return err
	}
	if v.Pages != nil {
		v.Pages.RefreshLabels(v.Assets)
	}
	return nil
}

func (v *View) Render(c *ui.Context) {
	v.syncAccountAvatar()
	c.SetTheme(&v.native)
	c.OnWindowBack(v.goBack)
	width, height := c.Size()
	layers := v.State.Layers()
	if len(layers) != 1 || layers[0].Page != app.Home || v.State.Area != app.ContentArea || !v.hoverPreviewEnabled() || v.reduceMotionEnabled(c) {
		v.stopHomePreview()
	} else if v.Playback != nil && v.Playback.InlinePreviewState().Active {
		v.Playback.TickInlinePreview(c.Now())
	}
	v.advanceCatalogHold(c, height, len(layers) == 1 && layers[0].Page == app.Home)
	v.focus.begin(c)
	v.activeScroll = nil
	background := paint(v.Theme.Colors.Surface.Canvas)
	rootReg := v.Pages.components[layers[0].Route]
	homePreviewHole := v.homePreviewShowing(layers)
	if rootReg.TransparentCanvas || homePreviewHole {
		background = ui.Transparent
		c.Root().Background(ui.Transparent)
	}
	screen := ui.Box(c).Fill().Clip().Background(background)
	if v.Assets != nil {
		screen.Lang(v.Assets.LangTag()).Font(v.Assets.FontFamily())
	}
	if !v.Theme.Dark && !rootReg.TransparentCanvas && !homePreviewHole {
		screen.Gradient(paint(v.Theme.Colors.Surface.Raised), paint(v.Theme.Colors.Surface.Canvas), 145)
	}
	fromVideo := rootReg.PreserveFocusUnderlay
	top := layers[len(layers)-1]
	if blur := v.modalBlur(top.Route); blur != 0 {
		screen.Cache().Blur(blur)
	}
	screen.Children(func() {
		ui.Box(c.Key("home-preview-underlay")).Absolute().Left(0).Top(0).Fill().ZIndex(0).IgnoreInput(true).Children(func() {
			if homePreviewHole {
				canvas := paint(v.Theme.Colors.Surface.Canvas)
				railW := v.railSpan
				if railW <= 0 {
					railW = collapsedRailWidth
				}
				var scrollY float32
				if s := v.scrolls[scrollKey{v.State.CurrentEntry, layers[0].Route}]; s != nil {
					scrollY = s.Y
				}
				heroBottom := max(0, float32(homeArtHeight)-scrollY)
				ui.Box(c.Key("home-preview-rail-bg")).Absolute().Left(0).Top(0).Width(railW).Height(height).Background(canvas)
				if heroBottom < height {
					ui.Box(c.Key("home-preview-bottom-bg")).Absolute().Left(railW).Top(heroBottom).Right(0).Bottom(0).Background(canvas)
				}
			}
		})
		if fromVideo && len(layers) == 1 {
			if layers[0].Page == app.Creator {
				if v.creator != nil {
					v.renderPageEntrance(c, &v.creator.entrance, width, height)
				}
			} else {
				v.renderPageEntrance(c, &v.videoDetail.entrance, width, height)
			}
		}
		v.renderRootPage(c, layers[0], len(layers) == 1, width, height)
		v.renderActiveFeedback(c, top.Page)
		if v.notice != "" {
			ui.Text(c, v.notice).Absolute().Right(32).Bottom(28).Padding(18, 24).
				Background(paint(v.Theme.Colors.Surface.Raised)).Radius(12).FontSize(20)
		}
		v.renderPluginRecoveryBanner(c, width)
	})
	for index, location := range layers[1:] {
		v.renderModal(c, v.context(location, index == len(layers)-2, width, height))
	}
	if v.Trace {
		next := navigationTrace{entry: v.State.CurrentEntry, route: v.State.Route, focus: v.State.Focus}
		if next != v.lastNavigation {
			log.Printf("navigation: entry=%s page=%s section=%s focus=%s", next.entry, next.route.Page, next.route.Section, next.focus)
			v.lastNavigation = next
		}
	}
	v.drawnRevision = v.State.Revision
	v.reveal = false
}

func (v *View) renderActiveFeedback(c *ui.Context, page app.Page) {
	var active *videoDetailFeedback
	switch page {
	case app.VideoDetail:
		active = &v.videoDetail.feedback
	case app.Creator:
		if v.creator != nil {
			active = &v.creator.feedback
		}
	case app.Series, app.SeriesCalendar, app.SeriesIndex, app.SeriesAll:
		active = &v.series.feedback
	case app.AccountMessages:
		active = &v.noticeFeedback
	case app.Settings:
		active = &v.settingsFeedback
	}
	slots := []*videoDetailFeedback{
		&v.videoDetail.feedback,
		&v.series.feedback,
		&v.noticeFeedback,
		&v.settingsFeedback,
	}
	if v.creator != nil {
		slots = append(slots, &v.creator.feedback)
	}
	for _, slot := range slots {
		if slot != active {
			*slot = videoDetailFeedback{}
		}
	}
	if active == &v.videoDetail.feedback {
		v.renderVideoDetailFeedback(c)
	} else if active != nil {
		v.renderFeedbackToast(c, active)
	}
}

func (v *View) renderPluginRecoveryBanner(c *ui.Context, width float32) {
	if v.PluginCatalog == nil || v.Assets == nil {
		return
	}
	rec := v.PluginCatalog.RecoveryState()
	if rec.Phase == app.PluginRecoveryNone || rec.Total <= 0 {
		return
	}
	var message string
	accent := v.Theme.Colors.Accent.Primary
	switch rec.Phase {
	case app.PluginRecoveryRunning:
		done := rec.Restored + rec.Failed
		if done > 0 && done < rec.Total {
			message = fmt.Sprintf(v.Assets.Text.Plugins.RecoveryBannerRunning, done, rec.Total)
		} else {
			message = fmt.Sprintf(v.Assets.Text.Plugins.RecoveryBannerStarting, rec.Total)
		}
	case app.PluginRecoveryDone:
		remaining := 6*time.Second - time.Since(rec.UpdatedAt)
		if remaining <= 0 {
			return
		}
		c.After(remaining)
		accent = v.Theme.Colors.Status.Success.Foreground
		message = fmt.Sprintf(v.Assets.Text.Plugins.RecoveryBannerDone, rec.Restored)
	case app.PluginRecoveryPartial:
		remaining := 10*time.Second - time.Since(rec.UpdatedAt)
		if remaining <= 0 {
			return
		}
		c.After(remaining)
		accent = v.Theme.Colors.Status.Warning.Foreground
		message = fmt.Sprintf(v.Assets.Text.Plugins.RecoveryBannerPartial, rec.Restored, rec.Total)
	default:
		return
	}
	if message == "" {
		return
	}
	d := deriveColors(v.Theme)
	ui.Column(c.Key("plugin-recovery-banner")).Absolute().Left(collapsedRailWidth).Right(0).Top(28).AlignItems(ui.Center).
		ZIndex(200).PassThrough().Children(func() {
		pill := ui.Row(c.Key("plugin-recovery-pill")).AlignItems(ui.Center).Gap(14).Padding(14, 24).Radius(18).
			Material(glass.Blur{Radius: 25}).
			Background(d.toastFill).
			Border(1, d.toastBorder).
			MaxWidth(width - collapsedRailWidth - 120).
			Role(ui.RoleStatus).PassThrough()
		if v.Theme.Dark {
			pill.Shadow(0, 12, 40, 0, withAlpha(v.Theme.Colors.Overlay.Shadow, 0x44))
		}
		pill.Children(func() {
			ui.Box(c.Key("plugin-recovery-dot")).Width(10).Height(10).Radius(5).Background(paint(accent)).Shrink(0).PassThrough()
			ui.Text(c.Key("plugin-recovery-text"), message).FontSize(22).TextColor(d.toastText).PassThrough()
		})
	})
}

func (v *View) renderRootPage(c *ui.Context, location app.Location, active bool, width, height float32) {
	if v.Pages.components[location.Route].FullScreen {
		v.renderScreen(c, v.context(location, active, width, height))
		return
	}
	page := v.context(location, active, width-collapsedRailWidth, height)
	row := ui.Row(c.Key("shell-root")).Fill().ZIndex(1).IgnoreInput(!active).AccessHidden(!active)
	row.Children(func() {
		navigationRail{}.Render(c, page)
		ui.Box(c.Key("main")).Width(width - collapsedRailWidth).Height(height).Shrink(0).Clip().
			Children(func() { v.renderScreen(c, page) })
	})
}

func (v *View) syncAccountAvatar() {
	avatar := v.Auth.Avatar()
	if v.accountAvatar == avatar {
		return
	}
	v.accountAvatar, v.avatarImage = avatar, nil
	if avatar != nil {
		v.avatarImage = ui.NewBitmap(avatar.Image)
	}
}

func (v *View) context(location app.Location, active bool, width, height float32) *PageContext {
	owner := v.Pages.navigationOwner(location.Route)
	left := focusDestination{}
	if owner != "" {
		left.Focus = app.RailFocus(owner)
	}
	return &PageContext{State: v.State, Pages: v.Pages, Assets: v.Assets, Theme: v.Theme, Preferences: v.Preferences,
		Version: v.Version, Location: location, Width: width, Height: height, active: active, host: v, left: left}
}

func (v *View) renderScreen(c *ui.Context, page *PageContext) {
	chain := v.Pages.Routes.Ancestors(page.Location.Route)
	rootReg := v.Pages.components[chain[0]]
	if rootReg.RawScreen && rootReg.FullScreen {
		v.Pages.render(c, page)
		return
	}
	scroll := page.scroll()
	insets := [4]float32{148, 83, screenBottomInset, 103}
	if override := rootReg.ScreenInsets; override != nil {
		insets = *override
	}
	insets[2] = max(insets[2], screenBottomInset)
	screenKey := page.Location.Route
	if page.Location.Page == app.Settings || page.Location.Page == app.Library {
		screenKey = app.Route{Page: page.Location.Page}
	}
	screen := ui.Scroll(c.Key(screenKey)).Fill().TrackScroll(scroll)
	transition := menuTransition
	if rootReg.Transition != nil {
		transition = *rootReg.Transition
		screen.Absolute().Left(0).Top(0).ZIndex(1).Background(paint(v.Theme.Colors.Surface.Canvas))
	}
	if v.reduceMotionEnabled(c) {
		transition = ui.ElementTransition{}
	}
	curReg := v.Pages.components[page.Location.Route]
	screen.Transition(transition).Children(func() {
		if rootReg.RawScreen || curReg.RawScreen {
			v.Pages.render(c, page)
			return
		}
		ui.Column(c).FillWidth().Padding(insets[0], insets[1], insets[2], insets[3]).Gap(44).
			Children(func() { v.Pages.render(c, page) })
	})
}

func (v *View) scrollFor(location app.Location, active bool) *ui.ScrollState {
	key := scrollKey{v.State.CurrentEntry, location.Route}
	scroll := v.scrolls[key]
	if scroll == nil {
		scroll = &ui.ScrollState{}
		v.scrolls[key] = scroll
	}
	if v.drawnRevision != v.State.Revision {
		scroll.X, scroll.Y = location.X, location.Y
	}
	if active {
		v.activeScroll = scroll
	}
	return scroll
}

func (v *View) dispatch(action func()) {
	v.home.hold = catalogHold{}
	v.notice = ""
	position := v.State.Position
	position.Focus, position.Area = v.State.Focus, v.focus.area(v.State.Focus)
	if scroll := v.activeScroll; scroll != nil {
		position.X, position.Y = scroll.X, scroll.Y
	}
	v.State.RecordPosition(position)
	previous := v.State.Route
	if previous.Page == app.Home {
		v.home.rotateAt = time.Time{}
	}
	action()
	if previous != v.State.Route {
		if leave := v.Pages.components[previous].Leave; leave != nil {
			leave(v)
		}
	}
	v.SyncEntry()
}

func (v *View) goBack() bool {
	if !v.State.CanGoBack() {
		return false
	}
	v.dispatch(func() {
		back := v.Pages.components[v.State.Route].Back
		if back != nil && back(v) {
			return
		}
		v.State.Back()
	})
	return true
}

func (v *View) SyncEntry() {
	entry, _ := v.State.Current()
	if entry.ID != v.home.entry || entry.Generation != v.entryGeneration {
		if v.home.entry != "" && v.home.entry != entry.ID {
			if v.homeStates == nil {
				v.homeStates = make(map[string]catalogHome)
			}
			saved := v.home
			saved.images, saved.imageClock = nil, 0
			saved.hold = catalogHold{}
			saved.featureInfo = catalogFeatureInfo{}
			saved.heroPreview = catalogHeroPreview{}
			saved.rotateAt = time.Time{}
			v.homeStates[v.home.entry] = saved
		}
		v.closeAllVideoDetails()
		v.closeAllCreators()
		if v.series.trackPending != nil {
			v.series.trackPending()
			v.series.trackPending = nil
		}
		v.series.signInAction = nil
		v.series.feedback = videoDetailFeedback{}
		v.stopHomePreview()
		if v.Playback != nil && v.Playback.Active() {
			v.leavePlayback()
		}
		v.home.featureInfo.stop()
		if prev, ok := v.homeStates[entry.ID]; ok && entry.ID != v.home.entry {
			v.home = prev
		} else {
			v.home = catalogHome{entry: entry.ID}
		}
		v.entryGeneration = entry.Generation
	}
	var service provider.Authenticator
	if v.Catalog != nil {
		v.Catalog.Open(entry)
		if entry.Capabilities.Has(provider.SignIn) {
			service = v.Catalog.Authenticator()
		}
	}
	if v.Auth != nil {
		v.Auth.Bind(entry, service)
	}
	if v.Catalog != nil && (v.Auth == nil || !entry.Capabilities.Has(provider.SignIn) || v.Auth.Accounts().Ready) {
		if v.home.tab != "" || v.home.filter != "" {
			v.Catalog.Show(v.home.tab, v.home.filter)
		} else {
			v.Catalog.EnsureLoaded()
		}
	}
	v.SyncAccountActivity()
}

func (v *View) SyncAccountActivity() {
	entry, _ := v.State.Current()
	owner := ""
	if v.Auth != nil {
		owner = v.Auth.Accounts().CurrentID
	}
	if v.Activity.Bind(entry, owner, v.Catalog.AccountReader()) {
		v.favorites = favoritesUI{}
	}
	if v.PrivateMessages != nil {
		var reader provider.PrivateMessageReader
		var writer provider.PrivateMessageWriter
		if v.Catalog != nil {
			reader = v.Catalog.PrivateMessageReader()
			writer = v.Catalog.PrivateMessageWriter()
		}
		if v.PrivateMessages.Bind(entry, owner, reader, writer) {
			v.privateUI = privateMessagesUI{}
			if v.State.Page == app.PrivateMessageCompose {
				v.State.Back()
			}
		}
	}
	if v.Search != nil {
		var searcher provider.CatalogSearcher
		var suggester provider.CatalogSearchSuggester
		if v.Catalog != nil {
			searcher = v.Catalog.Searcher()
			suggester = v.Catalog.SearchSuggester()
		}
		if v.Search.Bind(entry, owner, searcher, suggester) {
			v.search = searchViewState{}
		}
	}
	if v.Following != nil {
		var reader provider.FollowingReader
		if v.Catalog != nil {
			reader = v.Catalog.FollowingReader()
		}
		if v.Following.Bind(entry, owner, reader) {
			v.following = followingViewState{}
		}
	}
	if v.Series != nil {
		var reader provider.SeriesReader
		if v.Catalog != nil {
			reader = v.Catalog.SeriesReader()
		}
		if v.Series.Bind(entry, owner, reader) {
			v.series = seriesViewState{}
		}
	}
	if v.Live != nil {
		var reader provider.LiveReader
		if v.Catalog != nil {
			reader = v.Catalog.LiveReader()
		}
		if v.Live.Bind(entry, owner, reader) {
			v.live = liveViewState{}
		}
	}
	if v.LiveChat != nil {
		var client provider.LiveChatClient
		if v.Catalog != nil {
			client = v.Catalog.LiveChatClient()
		}
		v.LiveChat.SetContext(entry.ID, entry.Generation, owner, client, entry.Capabilities.Has(provider.LiveChat))
	}
	if v.DanmakuRules != nil {
		prev := v.DanmakuRules.Snapshot()
		v.DanmakuRules.SetScope(entry.ID, entry.Capabilities, owner, v.Catalog)
		if prev.EntryID != entry.ID || prev.Owner != owner {
			v.danmakuUI.draft = danmakuRuleDraft{}
			if v.State.Page == app.DanmakuRuleEditor {
				v.State.Back()
			}
		}
	}
	if v.State.Page == app.PrivacyConfirm && (v.privacyUI.confirmEntry != entry.ID || v.privacyUI.confirmOwner != owner) {
		v.State.Back()
	}
	if sync := v.Pages.components[v.State.Route].Sync; sync != nil {
		sync(v)
	}
	v.SyncTopShelf()
	v.FlushPendingTopShelfURL()
}
