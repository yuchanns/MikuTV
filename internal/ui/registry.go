package tvui

import (
	"fmt"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/provider"
)

type Component interface {
	Render(*ui.Context, *PageContext)
}

type ComponentFunc func(*ui.Context, *PageContext)

func (f ComponentFunc) Render(c *ui.Context, p *PageContext) { f(c, p) }

type navPlacement uint8

const (
	primaryNavigation navPlacement = iota
	utilityNavigation
)

type NavigationLink struct {
	Title, Icon string
	Placement   navPlacement
	Label       func(*PageContext) string
	Symbol      func(*ui.Context, *PageContext)
}

// Registration binds a component and its navigation policy in one place.
type Registration struct {
	Policy                app.RouteSpec
	Component             Component
	Title, Icon           string
	Navigation            *NavigationLink
	FullScreen            bool
	RawScreen             bool
	TransparentCanvas     bool
	PreserveFocusUnderlay bool
	Transition            *ui.ElementTransition
	ScreenInsets             *[4]float32
	ModalPresentation        modalPresentation
	ResolveModalPresentation func(*View) modalPresentation
	ModalWidth               float32
	ModalBlur                float32
	Sync                     func(*View)
	Leave                 func(*View)
	Back                  func(*View) bool
	HandleKey             func(*View, ui.InputEvent, string) bool
	BeforeMove            func(*View)
	AfterMove             func(*View, app.Direction, string, app.FocusID)
}

type Pages struct {
	Routes     *app.Routes
	ordered    []Registration
	components map[app.Route]Registration
}

func NewPages(assets *Assets, additional ...Registration) (*Pages, error) {
	focus := func(id app.FocusID) app.Position { return app.Position{Focus: id} }
	content := func(page app.Page, capability provider.Capability) Registration {
		return Registration{
			Policy:    app.RouteSpec{Route: app.Route{Page: page}, Initial: focus(app.ContentAction), ContentFocus: app.ContentAction, RequiresEntry: true, Capability: capability},
			Component: contentPage{}, Title: assets.Text.Nav[page],
			Navigation: &NavigationLink{Title: assets.Text.Nav[page], Icon: string(page)},
		}
	}
	home := content(app.Home, "")
	home.Component, home.Sync = catalogPage{}, syncHome
	home.ScreenInsets = &[4]float32{0, 0, 80, 0}
	home.Policy.ContentFocus = ""
	home.Policy.ResolveFocus = func(s *app.State) app.FocusID {
		if entry, ok := s.Current(); ok && entry.Capabilities.Has(provider.BrowseCatalog) {
			return homePlayAction
		}
		return app.ContentAction
	}
	home.Policy.ExitFocus = app.RailFocus(app.Home)
	home.Back = (*View).backFromHome
	home.Leave = func(v *View) {
		v.home.featureInfo.reset()
		v.stopHomePreview()
		v.home.heroPreview.reset()
	}
	home.HandleKey = func(v *View, event ui.InputEvent, group string) bool {
		return event.Repeat && v.startCatalogHold(event.Key, group)
	}
	home.AfterMove = func(v *View, direction app.Direction, group string, next app.FocusID) {
		if direction != app.Left && direction != app.Right {
			return
		}
		switch group {
		case homeHeader:
			for _, tab := range homeTabs {
				if next == homeTabID(tab) {
					v.showHomeTab(tab, "")
					break
				}
			}
		case homeFilters:
			for _, filter := range v.home.filters {
				if next == homeFilterID(filter.ID) {
					v.showHomeTab(v.home.tab, filter.ID)
					break
				}
			}
		}
	}
	libraryAfterMove := func(v *View, direction app.Direction, group string, next app.FocusID) {
		if (direction == app.Left || direction == app.Right) && group == favoriteTabs {
			v.selectLibraryTab(next)
		}
	}
	library := content(app.Library, provider.BrowseLibrary)
	library.Component, library.Sync = libraryPage{}, syncLibrary
	library.RawScreen = true
	library.Back = (*View).backFromLibraryManagement
	library.Leave = pauseLibraryHistory
	library.AfterMove = libraryAfterMove
	search := content(app.Search, provider.SearchCatalog)
	search.Component, search.Sync = searchPage{}, syncSearch
	search.RawScreen = true
	search.Back = (*View).backFromSearch
	search.Leave = func(v *View) {
		v.search.editing = false
	}
	following := content(app.Following, provider.BrowseFollowing)
	following.Component, following.Sync = followingPage{}, syncFollowing
	following.RawScreen = true
	following.Policy.ExitFocus = app.RailFocus(app.Following)
	following.Back = (*View).backFromFollowing
	following.AfterMove = func(v *View, direction app.Direction, group string, next app.FocusID) {
		if (direction == app.Left || direction == app.Right) && group == followingHeaderRegion {
			switch next {
			case followingTabAll:
				v.showFollowingTab(followingViewAll)
			case followingTabLive:
				v.showFollowingTab(followingViewLive)
			}
		}
	}
	series := content(app.Series, provider.BrowseSeries)
	series.Component, series.Sync = seriesPage{}, syncSeriesHub
	series.RawScreen = true
	series.Policy.ExitFocus = app.RailFocus(app.Series)
	series.Back = (*View).backFromSeries
	series.Navigation.Label = func(p *PageContext) string { return p.sourceSearchSeriesLabel() }
	series.AfterMove = func(v *View, direction app.Direction, group string, next app.FocusID) {
		if (direction == app.Left || direction == app.Right) && group == seriesHeaderRegion && v.Series != nil {
			for _, g := range v.Series.Hub().Genres {
				if next == seriesGenreFocus(g.Value) {
					v.selectSeriesGenre(g.Value)
					break
				}
			}
		}
	}
	live := content(app.Live, provider.BrowseLive)
	live.Component, live.Sync = livePage{}, syncLive
	live.RawScreen = true
	live.Policy.ExitFocus = app.RailFocus(app.Live)
	live.Back = (*View).backFromLive
	live.AfterMove = func(v *View, direction app.Direction, group string, next app.FocusID) {
		if (direction == app.Left || direction == app.Right) && group == liveHeaderRegion && v.Live != nil {
			for _, area := range v.Live.Snapshot().Areas {
				if next == liveAreaFocus(area.ID) {
					v.selectLiveArea(area.ID)
					break
				}
			}
		}
	}
	registrations := []Registration{
		home,
		{Policy: app.RouteSpec{Route: app.Route{Page: app.HomeAction}, Parent: app.Route{Page: app.Home},
			Initial: focus(homeActionBackFocus), ContentFocus: homeActionBackFocus, RequiresEntry: true, Transient: true},
			Component: catalogActionPage{}, RawScreen: true,
			Leave: func(v *View) {
				if v.State.Page != app.VideoDetail && v.State.Page != app.Player {
					v.home.action = catalogAction{}
				}
			}},
		{Policy: app.RouteSpec{Route: app.Route{Page: app.VideoDetail},
			Initial: focus(videoDetailPlay), RequiresEntry: true, Capability: provider.GetCatalogItem, Transient: true},
			Component: videoDetailPage{}, FullScreen: true, PreserveFocusUnderlay: true,
			Transition: &videoDetailTransition, ScreenInsets: &[4]float32{},
			Sync: syncVideoDetail, Back: (*View).backFromVideoDetail, Leave: (*View).leaveVideoDetail},
		{Policy: app.RouteSpec{Route: app.Route{Page: app.Creator},
			Initial: focus(creatorFollowFocus), RequiresEntry: true, Capability: provider.ReadCreator, Transient: true},
			Component: creatorPage{}, FullScreen: true, PreserveFocusUnderlay: true,
			Transition: &videoDetailTransition, ScreenInsets: &[4]float32{},
			Sync: syncCreator, Leave: (*View).leaveCreator},
		{Policy: app.RouteSpec{Route: app.Route{Page: app.Player},
			Initial: focus(playerToggle), RequiresEntry: true, Capability: provider.ResolvePlayback, Transient: true},
			Component: playbackPage{}, FullScreen: true, RawScreen: true, TransparentCanvas: true,
			Sync: syncPlayback, Back: (*View).backFromPlayback, Leave: (*View).leavePlayback,
			HandleKey: func(v *View, event ui.InputEvent, _ string) bool {
				if event.Key == ui.KeySpace {
					if !event.Repeat {
						v.dispatch(v.togglePlayback)
					}
					return true
				}
				return false
			},
			BeforeMove: func(v *View) {
				if v.player.visible {
					v.player.controlFocus = ""
					v.player.show(v)
				}
			}},
		{Policy: app.RouteSpec{Route: app.Route{Page: app.VideoConfirm}, Parent: app.Route{Page: app.VideoDetail},
			Initial: focus(videoConfirmAccept), Modal: true}, Component: videoConfirmPage{},
			ModalPresentation: modalFrameless, ModalWidth: 780},
		{Policy: app.RouteSpec{Route: app.Route{Page: app.VideoCommentCompose}, Parent: app.Route{Page: app.VideoDetail},
			Initial: focus(videoCommentEdit), Modal: true}, Component: videoCommentComposePage{},
			ModalPresentation: modalFrameless, ModalWidth: 880},
		search,
		{Policy: app.RouteSpec{Route: app.Route{Page: app.SearchFilters}, Parent: app.Route{Page: app.Search},
			Initial: focus(searchFilterSortFocus(provider.SearchSortDefault)), RequiresEntry: true, Capability: provider.SearchCatalog, Modal: true},
			Component: searchFiltersPage{}, ModalPresentation: modalDrawer, ModalWidth: 660},
		following,
		{Policy: app.RouteSpec{Route: app.Route{Page: app.FollowingAll}, Parent: app.Route{Page: app.Following},
			Initial: focus(followingAllBackFocus), ContentFocus: followingAllBackFocus, RequiresEntry: true, Capability: provider.BrowseFollowing, Transient: true},
			Component: followingAllPage{}, RawScreen: true,
			Leave: func(v *View) {
				if v.State.Page != app.VideoDetail && v.State.Page != app.Player {
					v.following.allSection = ""
				}
			}},
		series,
		{Policy: app.RouteSpec{Route: app.Route{Page: app.SeriesAll}, Parent: app.Route{Page: app.Series},
			Initial: focus(seriesAllBackFocus), ContentFocus: seriesAllBackFocus, RequiresEntry: true, Capability: provider.BrowseSeries, Transient: true},
			Component: seriesAllPage{}, RawScreen: true,
			Leave: func(v *View) {
				if v.State.Page != app.VideoDetail && v.State.Page != app.Player {
					v.series.allSection = ""
				}
			}},
		{Policy: app.RouteSpec{Route: app.Route{Page: app.SeriesIndex}, Parent: app.Route{Page: app.Series},
			Initial: focus(seriesIndexBackFocus), ContentFocus: seriesIndexBackFocus, RequiresEntry: true, Capability: provider.BrowseSeries},
			Component: seriesIndexPage{}, RawScreen: true, Sync: syncSeriesIndex, Back: (*View).backFromSeriesIndex},
		{Policy: app.RouteSpec{Route: app.Route{Page: app.SeriesIndexFilter}, Parent: app.Route{Page: app.SeriesIndex},
			Initial: focus(seriesFilterOptionFocus(0)), RequiresEntry: true, Capability: provider.BrowseSeries, Modal: true},
			Component: seriesIndexFilterPage{}, ModalPresentation: modalDrawer, ModalWidth: 560},
		{Policy: app.RouteSpec{Route: app.Route{Page: app.SeriesCalendar}, Parent: app.Route{Page: app.Series},
			Initial: focus(seriesCalendarDayFocus(0)), ContentFocus: seriesCalendarDayFocus(0), RequiresEntry: true, Capability: provider.BrowseSeries},
			Component: seriesCalendarPage{}, RawScreen: true, Sync: syncSeriesCalendar, Back: (*View).backFromSeriesCalendar,
			AfterMove: func(v *View, direction app.Direction, group string, next app.FocusID) {
				if (direction == app.Left || direction == app.Right) && group == seriesCalendarDaysRegion && v.Series != nil {
					for i := range v.Series.Calendar().Days {
						if next == seriesCalendarDayFocus(i) {
							v.Series.SelectCalendarDay(i)
							break
						}
					}
				}
			}},
		live,
		{Policy: app.RouteSpec{Route: app.Route{Page: app.LiveAll}, Parent: app.Route{Page: app.Live},
			Initial: focus(liveAllBackFocus), ContentFocus: liveAllBackFocus, RequiresEntry: true, Capability: provider.BrowseLive, Transient: true},
			Component: liveAllPage{}, RawScreen: true,
			Leave: func(v *View) {
				if v.State.Page != app.Player && v.State.Page != app.VideoDetail {
					v.live.allSection = ""
				}
			}},
		library,
		{Policy: app.RouteSpec{Route: app.Route{Page: app.Entries}, Transient: true, ResolveFocus: func(s *app.State) app.FocusID { return s.PickerFocus() }},
			Component: entriesPage{}, Navigation: &NavigationLink{Title: assets.Text.Nav[app.Entries], Icon: "switch", Placement: utilityNavigation}},
		{Policy: app.RouteSpec{Route: app.Route{Page: app.Settings}, Redirect: app.Route{Page: app.Settings, Section: "playback"}},
			Component: settingsLayout{}, Navigation: &NavigationLink{Title: assets.Text.Nav[app.Settings], Icon: "settings", Placement: utilityNavigation}},
	}
	account := content(app.Account, provider.SignIn)
	account.Component = accountPage{}
	account.Sync = syncAccountOverview
	account.Policy.Initial, account.Policy.ContentFocus = focus(app.AccountAction), app.AccountAction
	account.Navigation = &NavigationLink{Title: assets.Text.Nav[app.Account], Icon: "user", Placement: utilityNavigation, Label: accountLabel, Symbol: accountSymbol}
	authModal := func(page app.Page, parent app.Page, initial app.FocusID) app.RouteSpec {
		return app.RouteSpec{Route: app.Route{Page: page}, Parent: app.Route{Page: parent}, Initial: focus(initial), Modal: true,
			RequiresEntry: true, Capability: provider.SignIn}
	}
	registrations = append(registrations,
		account,
		Registration{Policy: app.RouteSpec{Route: app.Route{Page: app.AccountMessages}, Initial: focus(noticeTab), ContentFocus: noticeTab,
			RequiresEntry: true, Capability: provider.SignIn}, Component: accountMessagesPage{}, Sync: syncAccountMessages,
			ScreenInsets: &[4]float32{},
			AfterMove: func(v *View, direction app.Direction, group string, next app.FocusID) {
				if (direction == app.Left || direction == app.Right) && group == noticeTabs {
					switch next {
					case noticeTab:
						v.selectMessagesTab(messagesTabNotifications)
					case privateTab:
						v.selectMessagesTab(messagesTabPrivate)
					}
				}
			}},
		Registration{Policy: app.RouteSpec{Route: app.Route{Page: app.PrivateMessageCompose}, Parent: app.Route{Page: app.AccountMessages},
			Initial: focus(privateComposeEdit), Modal: true, RequiresEntry: true, Capability: provider.WritePrivateMessages},
			Component: privateComposePage{}, ModalPresentation: modalFrameless, ModalWidth: 820,
			Back:  func(v *View) bool { return v.PrivateMessages != nil && v.PrivateMessages.Sending() },
			Leave: func(v *View) { v.privateUI.draft.editing = false }},
		Registration{Policy: authModal(noticePage, app.AccountMessages, app.CloseModal), Component: accountNoticeDialog{},
			ModalPresentation: modalFrameless, ModalWidth: 820,
			Leave: func(v *View) { v.openNotice = provider.AccountRow{} }},
		Registration{Policy: authModal(app.AccountDetails, app.Account, rightsPhone), Component: accountDetails{},
			ModalPresentation: modalFrameless, ModalWidth: 860,
			Leave: func(v *View) { v.accountDetailsPhone = false }},
		Registration{Policy: authModal(app.AccountPicker, app.Account, app.AccountAdd), Component: accountPicker{},
			ModalPresentation: modalDrawer, ModalWidth: 620,
			Leave: func(v *View) { v.Auth.CancelAccountSelection() }},
		Registration{Policy: authModal(app.SignInQR, app.Account, app.QRAction), Component: qrSignIn{},
			ModalPresentation: modalFrameless, ModalWidth: 820,
			Leave: func(v *View) {
				v.Auth.CancelSignIn()
				v.qrPayload, v.qrImage = "", nil
				v.qrHasAction = false
				v.clearSignInActions()
			}},
		Registration{Policy: app.RouteSpec{
			Route: app.Route{Page: app.SignInCredentials}, Parent: app.Route{Page: app.Account},
			Initial: focus(credUsernameField),
			ResolveFocus: func(s *app.State) app.FocusID {
				if entry, ok := s.Current(); ok && entry.UsesServerURL() {
					return credServerField
				}
				return credUsernameField
			},
			Modal: true, RequiresEntry: true, Capability: provider.SignIn,
		}, Component: credentialsSignIn{},
			ModalPresentation: modalFrameless, ModalWidth: 820,
			Back: (*View).backFromCredentialsSignIn,
			Leave: func(v *View) {
				v.Auth.CancelSignIn()
				v.credDraft.clear()
				v.clearSignInActions()
			}},
		Registration{Policy: authModal(app.SignOut, app.Account, app.CloseModal), Component: signOutDialog{},
			ModalPresentation: modalFrameless, ModalWidth: 760},
	)
	for _, section := range libraryTabs {
		tab := Registration{
			Policy: app.RouteSpec{Route: libraryRoute(section), Parent: library.Policy.Route,
				Initial: focus(app.ContentAction), ContentFocus: app.ContentAction, RequiresEntry: true,
				Capability: section.Capability()},
			Component: libraryContent{}, Sync: syncLibrary, Back: (*View).backFromLibraryManagement,
			AfterMove: libraryAfterMove,
		}
		if section == provider.AccountHistory {
			tab.Leave = pauseLibraryHistory
		}
		registrations = append(registrations, tab)
	}
	registrations = append(registrations, Registration{
		Policy: app.RouteSpec{Route: app.Route{Page: app.Library, Section: favoriteFolderSection}, Parent: library.Policy.Route,
			Initial: focus(app.ContentAction), ContentFocus: app.ContentAction, RequiresEntry: true,
			Capability: provider.AccountFavorites.Capability()},
		Component: libraryContent{}, Sync: syncLibrary, Back: (*View).backFromLibraryManagement,
		AfterMove: libraryAfterMove,
	}, Registration{
		Policy: app.RouteSpec{Route: app.Route{Page: favoriteFolderDialog}, Initial: focus(app.CloseModal),
			Modal: true, RequiresEntry: true, Capability: provider.EditFavoriteFolders},
		Component: favoriteDialogPage{}, ResolveModalPresentation: favoriteDialogPresentation, ModalWidth: 620,
		Back:  func(v *View) bool { return v.favorites.dialog.pending },
		Leave: func(v *View) { v.favorites.dialog = favoriteDialog{} },
	})
	settings := map[string]Component{
		"playback": playbackSettingsPage{}, "danmaku": danmakuSettingsPage{}, "appearance": appearanceSettingsPage{},
		"privacy": privacySettingsPage{}, "plugins": pluginsSettingsPage{}, "about": aboutPage{},
	}
	for _, section := range assets.Text.Sections {
		component, ok := settings[section.ID]
		if !ok {
			return nil, fmt.Errorf("unregistered settings component %q", section.ID)
		}
		reg := Registration{
			Policy:    app.RouteSpec{Route: app.Route{Page: app.Settings, Section: section.ID}, Parent: app.Route{Page: app.Settings}, Initial: focus(app.SectionFocus(section.ID))},
			Component: component, Title: section.Name, Icon: section.Icon,
			AfterMove: func(v *View, direction app.Direction, group string, next app.FocusID) {
				if (direction == app.Up || direction == app.Down) && group == "sections" {
					v.selectSettingsSection(next)
				}
			},
		}
		if section.ID == "danmaku" {
			reg.Sync = syncDanmakuSettings
			reg.Back = (*View).backFromDanmakuSettings
			reg.Leave = func(v *View) {
				if v.State.Page != app.DanmakuSettingPicker && v.State.Page != app.DanmakuRuleEditor {
					v.danmakuUI.rulesOpen = false
				}
			}
		}
		if section.ID == "plugins" {
			reg.Sync = syncPluginsSettings
			reg.Back = (*View).backFromPluginsSettings
			reg.Leave = func(v *View) {
				v.pluginsUI.editing = false
				switch v.State.Page {
				case app.PluginRepoInput, app.PluginInstallDialog, app.PluginUpdateDialog, app.PluginDisableDialog, app.PluginRemoveDialog:
				default:
					v.pluginsUI.returnToThemePicker = false
					v.pluginsUI.returnToLanguagePicker = false
				}
			}
		}
		registrations = append(registrations, reg)
	}
	settingsModal := func(page app.Page, section string, initial app.FocusID, component Component, presentation modalPresentation, width float32, configure ...func(*Registration)) Registration {
		reg := Registration{
			Policy: app.RouteSpec{
				Route:  app.Route{Page: page, Section: section},
				Parent: app.Route{Page: app.Settings, Section: section},
				Modal:  true, Initial: focus(initial),
			},
			Component: component, ModalPresentation: presentation, ModalWidth: width,
		}
		for _, fn := range configure {
			fn(&reg)
		}
		return reg
	}
	registrations = append(registrations,
		settingsModal(app.PlaybackSettingPicker, "playback", playbackPickerOptionID(0), playbackSettingPickerPage{}, modalDrawer, 560),
		settingsModal(app.DanmakuSettingPicker, "danmaku", danmakuPickerOptionID(0), danmakuSettingPickerPage{}, modalDrawer, 560),
		settingsModal(app.DanmakuRuleEditor, "danmaku", danmakuEditorField, danmakuRuleEditorPage{}, modalFrameless, 820, func(r *Registration) {
			r.Back = func(v *View) bool { return v.DanmakuRules != nil && v.DanmakuRules.Snapshot().Busy }
			r.Leave = func(v *View) { v.danmakuUI.draft.editing = false }
		}),
		settingsModal(app.LanguagePicker, "appearance", languagePickerOptionID(app.LocaleModeSystem), languagePickerPage{}, modalDrawer, 620),
		settingsModal(app.AppearanceModePicker, "appearance", appearanceModeOptionID(app.AppearanceModeDark), appearanceModePickerPage{}, modalDrawer, 560),
		settingsModal(app.ThemePicker, "appearance", themePickerOptionID("mikutv-dark"), themePickerPage{}, modalDrawer, 640),
		settingsModal(app.ThemeError, "appearance", themeErrorReselectBtn, themeErrorPage{}, modalFrameless, 780),
		settingsModal(app.PrivacyConfirm, "privacy", app.CloseModal, privacyConfirmPage{}, modalFrameless, 800, func(r *Registration) {
			r.Leave = func(v *View) { v.privacyUI = privacySettingsUI{} }
		}),
		settingsModal(app.PrivacyDetails, "about", privacyManageAction, privacyDetailsPage{}, modalFrameless, 780),
		settingsModal(app.Licenses, "about", app.LicenseText, licensesPage{}, modalDrawer, 880, func(r *Registration) {
			r.Title = assets.Text.Licenses
		}),
		settingsModal(app.AboutProject, "about", app.CloseModal, aboutProjectPage{}, modalFrameless, 780),
		settingsModal(app.PluginRepoInput, "plugins", pluginRepoField, pluginRepoInputPage{}, modalFrameless, 780, func(r *Registration) {
			r.Back = func(v *View) bool {
				if v.pluginsUI.repo.editing {
					v.pluginsUI.repo.editing = false
					v.pluginsUI.repo.input = v.pluginsUI.repo.text
					v.State.Focus, v.reveal = pluginRepoField, true
					return true
				}
				return false
			}
			r.Leave = func(v *View) {
				if v.PluginCatalog != nil {
					v.PluginCatalog.CancelResolve()
				}
				v.pluginsUI.repo.editing = false
				v.pluginsUI.repo.busy = false
			}
		}),
		settingsModal(app.PluginInstallDialog, "plugins", pluginInstallConfirmBtn, pluginInstallDialogPage{}, modalFrameless, 780, func(r *Registration) {
			r.Leave = func(v *View) {
				if v.PluginCatalog != nil {
					v.PluginCatalog.CancelInstall()
				}
			}
		}),
		settingsModal(app.PluginUpdateDialog, "plugins", pluginUpdateConfirmBtn, pluginUpdateDialogPage{}, modalFrameless, 780, func(r *Registration) {
			r.Leave = func(v *View) {
				if v.PluginCatalog != nil {
					v.PluginCatalog.CancelUpdate()
				}
			}
		}),
		settingsModal(app.PluginDisableDialog, "plugins", pluginDisableConfirmBtn, pluginDisableDialogPage{}, modalFrameless, 780),
		settingsModal(app.PluginRemoveDialog, "plugins", pluginRemoveConfirmBtn, pluginRemoveDialogPage{}, modalFrameless, 780),
	)
	registrations = append(registrations, additional...)
	p := &Pages{components: make(map[app.Route]Registration)}
	var policies []app.RouteSpec
	for _, registration := range registrations {
		if registration.Component == nil {
			return nil, fmt.Errorf("route %+v has no component", registration.Policy.Route)
		}
		if _, exists := p.components[registration.Policy.Route]; exists {
			return nil, fmt.Errorf("duplicate component registration: %+v", registration.Policy.Route)
		}
		p.components[registration.Policy.Route] = registration
		p.ordered = append(p.ordered, registration)
		policies = append(policies, registration.Policy)
	}
	var err error
	p.Routes, err = app.NewRoutes(app.Route{Page: app.Home}, app.Route{Page: app.Entries}, policies)
	return p, err
}

func (v *View) clearSignInActions() {
	v.videoDetail.signInAction = nil
	if v.creator != nil {
		v.creator.signInAction = nil
	}
	v.series.signInAction = nil
	v.player.content.afterLogin = nil
}

func (p *Pages) RefreshLabels(assets *Assets) {
	if p == nil || assets == nil {
		return
	}
	sectionNames := make(map[string]string, len(assets.Text.Sections))
	for _, s := range assets.Text.Sections {
		sectionNames[s.ID] = s.Name
	}
	for i := range p.ordered {
		reg := p.ordered[i]
		route := reg.Policy.Route
		switch {
		case route.Page == app.Settings && route.Section != "":
			if name, ok := sectionNames[route.Section]; ok && name != "" {
				reg.Title = name
			}
		case route.Page == app.Licenses:
			if assets.Text.Licenses != "" {
				reg.Title = assets.Text.Licenses
			}
		default:
			if navTitle, ok := assets.Text.Nav[route.Page]; ok && navTitle != "" {
				if reg.Title != "" {
					reg.Title = navTitle
				}
				if reg.Navigation != nil {
					navCopy := *reg.Navigation
					navCopy.Title = navTitle
					reg.Navigation = &navCopy
				}
			}
		}
		p.ordered[i] = reg
		p.components[route] = reg
	}
}

func (p *Pages) navigation(state *app.State, placement navPlacement) []Registration {
	var links []Registration
	for _, registration := range p.ordered {
		if registration.Navigation == nil || registration.Navigation.Placement != placement {
			continue
		}
		policy, _ := p.Routes.Resolve(registration.Policy.Route)
		if !state.Allows(policy) {
			continue
		}
		links = append(links, registration)
	}
	return links
}

func (p *Pages) navigationOwner(route app.Route) app.Page {
	chain := p.Routes.Ancestors(route)
	for index := len(chain) - 1; index >= 0; index-- {
		registration := p.components[chain[index]]
		if registration.Navigation != nil {
			return registration.Policy.Route.Page
		}
	}
	return ""
}

func (p *Pages) children(route app.Route) []Registration {
	var children []Registration
	for _, registration := range p.ordered {
		if registration.Policy.Parent == route && !registration.Policy.Modal {
			children = append(children, registration)
		}
	}
	return children
}

func (p *Pages) contains(parent, route app.Route) bool {
	for _, ancestor := range p.Routes.Ancestors(route) {
		if ancestor == parent {
			return true
		}
	}
	return false
}

func (p *Pages) render(c *ui.Context, page *PageContext) {
	chain := p.Routes.Ancestors(page.Location.Route)
	var build func(int, *PageContext)
	build = func(index int, context *PageContext) {
		registration := p.components[chain[index]]
		child := *context
		child.Node, child.Title = registration.Policy.Route, registration.Title
		child.outlet = func(c *ui.Context) {
			if index+1 < len(chain) {
				build(index+1, &child)
			}
		}
		registration.Component.Render(c, &child)
	}
	build(0, page)
}
