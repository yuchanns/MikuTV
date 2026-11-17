package tvui

import (
	"fmt"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
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
}

// Registration binds a component and its navigation policy in one place.
type Registration struct {
	Policy      app.RouteSpec
	Component   Component
	Title, Icon string
	Navigation  *NavigationLink
	ModalWidth  float32
	ModalHeight float32
	ModalInsets [4]float32
}

type Pages struct {
	Routes     *app.Routes
	ordered    []Registration
	components map[app.Route]Registration
}

func NewPages(assets *Assets, additional ...Registration) (*Pages, error) {
	focus := func(id app.FocusID) app.Position { return app.Position{Focus: id} }
	content := func(page app.Page, initial app.Position) Registration {
		return Registration{
			Policy:    app.RouteSpec{Route: app.Route{Page: page}, Initial: initial, ContentFocus: app.ContentAction, RequiresEntry: true},
			Component: contentPage{}, Title: assets.Text.Nav[page],
			Navigation: &NavigationLink{Title: assets.Text.Nav[page], Icon: string(page)},
		}
	}
	home := content(app.Home, focus(app.ContentAction))
	home.Policy.ExitFocus = app.RailFocus(app.Home)
	registrations := []Registration{
		home,
		content(app.Search, focus(app.ContentAction)),
		content(app.Following, focus(app.ContentAction)),
		content(app.Series, focus(app.ContentAction)),
		content(app.Live, focus(app.ContentAction)),
		content(app.Library, focus(app.ContentAction)),
		{Policy: app.RouteSpec{Route: app.Route{Page: app.Entries}, Transient: true, ResolveFocus: func(s *app.State) app.FocusID { return s.PickerFocus() }},
			Component: entriesPage{}, Navigation: &NavigationLink{Title: assets.Text.Nav[app.Entries], Icon: "switch", Placement: utilityNavigation}},
		{Policy: app.RouteSpec{Route: app.Route{Page: app.Settings}, Redirect: app.Route{Page: app.Settings, Section: "playback"}},
			Component: settingsLayout{}, Navigation: &NavigationLink{Title: assets.Text.Nav[app.Settings], Icon: "settings", Placement: utilityNavigation}},
	}
	account := content(app.Account, focus(app.ContentAction))
	account.Navigation.Icon, account.Navigation.Placement = "user", utilityNavigation
	registrations = append(registrations, account)
	settings := map[string]Component{
		"playback": settingsEmptyPage{}, "danmaku": settingsEmptyPage{}, "appearance": settingsEmptyPage{},
		"privacy": settingsEmptyPage{}, "plugins": settingsEmptyPage{}, "about": aboutPage{},
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
		if policy.RequiresEntry && state.CurrentEntry == "" {
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
