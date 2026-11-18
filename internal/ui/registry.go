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
	Policy             app.RouteSpec
	Component          Component
	Title, Icon        string
	Navigation         *NavigationLink
	ModalWidth         float32
	ModalHeight        float32
	ResolveModalHeight func(*View) float32
	ModalInsets        [4]float32
	ScreenInsets  *[4]float32
	Sync               func(*View)
	Leave              func(*View)
	Back               func(*View) bool
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
	home.Component = catalogPage{}
	home.Policy.ExitFocus = app.RailFocus(app.Home)
	library := content(app.Library, provider.BrowseLibrary)
	library.Component, library.Sync = libraryPage{}, syncLibrary
	library.Back = (*View).backFromLibraryManagement
	library.Leave = pauseLibraryHistory
	registrations := []Registration{
		home,
		content(app.Search, provider.SearchCatalog),
		content(app.Following, provider.BrowseFollowing),
		content(app.Series, provider.BrowseSeries),
		content(app.Live, provider.BrowseLive),
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
			RequiresEntry: true, Capability: provider.AccountNotifications.Capability()}, Component: accountMessagesPage{}, Sync: syncAccountMessages,
			ScreenInsets: &[4]float32{}},
		Registration{Policy: authModal(noticePage, app.AccountMessages, app.CloseModal), Component: accountNoticeDialog{},
			ModalWidth: 760, ModalHeight: 405, ModalInsets: [4]float32{50, 50, 50, 50},
			Leave: func(v *View) { v.openNotice = provider.AccountRow{} }},
		Registration{Policy: authModal(app.AccountDetails, app.Account, rightsPhone), Component: accountDetails{}, ModalWidth: 820,
			ModalInsets: [4]float32{51, 51, 51, 51}, ResolveModalHeight: accountDetailsHeight,
			Leave: func(v *View) { v.accountDetailsPhone = false }},
		Registration{Policy: authModal(app.AccountPicker, app.Account, app.AccountAdd), Component: accountPicker{}, ModalWidth: 700, ModalHeight: 460,
			ModalInsets: [4]float32{50, 50, 50, 50},
			ResolveModalHeight: func(v *View) float32 {
				if v.Auth == nil {
					return 382
				}
				return 382 + 70*float32(max(0, len(v.Auth.Accounts().Accounts)-1))
			},
			Leave: func(v *View) {
				if v.Auth != nil {
					v.Auth.CancelAccountSelection()
				}
			}},
		Registration{Policy: authModal(app.SignInQR, app.Account, app.QRAction), Component: qrSignIn{}, ModalWidth: 760, ModalHeight: 584,
			ModalInsets: [4]float32{59, 58, 53, 58},
			Leave: func(v *View) {
				if v.Auth != nil {
					v.Auth.CancelSignIn()
				}
				v.qrPayload, v.qrImage = "", nil
				v.qrHasAction = false
			}},
		Registration{Policy: authModal(app.SignOut, app.Account, app.CloseModal), Component: signOutDialog{}, ModalWidth: 700, ModalHeight: 290,
			ModalInsets: [4]float32{50, 50, 50, 50}},
	)
	for _, section := range libraryTabs {
		tab := Registration{
			Policy: app.RouteSpec{Route: libraryRoute(section), Parent: library.Policy.Route,
				Initial: focus(app.ContentAction), ContentFocus: app.ContentAction, RequiresEntry: true},
			Component: libraryContent{}, Sync: syncLibrary, Back: (*View).backFromLibraryManagement,
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
	}, Registration{
		Policy: app.RouteSpec{Route: app.Route{Page: favoriteFolderDialog}, Initial: focus(app.CloseModal),
			Modal: true, RequiresEntry: true, Capability: provider.EditFavoriteFolders},
		Component: favoriteDialogPage{}, ModalWidth: 700, ResolveModalHeight: favoriteDialogHeight,
		ModalInsets: [4]float32{50, 50, 50, 50}, Back: func(v *View) bool { return v.favorites.dialog.pending },
		Leave: func(v *View) { v.favorites.dialog = favoriteDialog{} },
	})
	settings := map[string]Component{
		"playback": settingsEmptyPage{}, "danmaku": settingsEmptyPage{}, "appearance": settingsEmptyPage{},
		"privacy": privacySettingsPage{}, "plugins": settingsEmptyPage{}, "about": aboutPage{},
	}
	for _, section := range assets.Text.Sections {
		component, ok := settings[section.ID]
		if !ok {
			return nil, fmt.Errorf("unregistered settings component %q", section.ID)
		}
		registrations = append(registrations, Registration{
			Policy:    app.RouteSpec{Route: app.Route{Page: app.Settings, Section: section.ID}, Parent: app.Route{Page: app.Settings}, Initial: focus(app.SectionFocus(section.ID))},
			Component: component, Title: section.Name, Icon: section.Icon,
		})
	}
	registrations = append(registrations, Registration{
		Policy:    app.RouteSpec{Route: app.Route{Page: app.Licenses, Section: "about"}, Parent: app.Route{Page: app.Settings, Section: "about"}, Modal: true, Initial: focus(app.LicenseText)},
		Component: licensesPage{}, Title: assets.Text.Licenses, ModalWidth: 1050, ModalHeight: 706,
		ModalInsets: [4]float32{50, 50, 50, 50},
	})
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
