package tvui

import (
	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/theme"
)

type PageContext struct {
	State         *app.State
	Assets        *Assets
	Theme         theme.Theme
	Preferences   app.PreferencesStore
	Pages         *Pages
	Version       string
	Location      app.Location
	Node          app.Route
	Title         string
	Width, Height float32
	active        bool
	host          *View
	left          focusDestination
	outlet        func(*ui.Context)
}

type listKey struct {
	location scrollKey
	name     string
}

func (p *PageContext) listState(name string) *ui.ListState {
	if p.host.lists == nil {
		p.host.lists = make(map[listKey]*ui.ListState)
	}
	key := listKey{location: scrollKey{p.State.CurrentEntry, p.Location.Route}, name: name}
	list := p.host.lists[key]
	if list == nil {
		list = &ui.ListState{}
		p.host.lists[key] = list
	}
	return list
}

func (p *PageContext) Outlet(c *ui.Context) {
	if p.outlet != nil {
		p.outlet(c)
	}
}
func (p *PageContext) dispatch(action func()) { p.host.dispatch(action) }
func (p *PageContext) region(id string, edges focusEdges) {
	if edges[app.Left] == (focusDestination{}) && p.left != (focusDestination{}) {
		if edges == nil {
			edges = make(focusEdges)
		}
		edges[app.Left] = p.left
	}
	if p.active {
		p.host.focus.groups[id] = focusRegion{edges: edges}
	}
}
func (p *PageContext) bind(element ui.Element, id app.FocusID, group string) {
	p.host.bind(element, id, group, p.active, nil)
}
func (p *PageContext) bindDirection(element ui.Element, id app.FocusID, group string, direction func(app.Direction) bool) {
	p.host.bind(element, id, group, p.active, direction)
}

func (p *PageContext) paintFocus() app.FocusID {
	if !p.active && p.Pages != nil &&
		(p.Pages.components[p.Location.Route].PreserveFocusUnderlay || p.Pages.components[p.State.Route].PreserveFocusUnderlay) {
		// Keep the recorded highlight under a detail transition or modal.
		return p.Location.Focus
	}
	return p.State.Focus
}

func (p *PageContext) scroll() *ui.ScrollState { return p.host.scrollFor(p.Location, p.active) }

func (p *PageContext) langTag() string {
	if p != nil && p.Assets != nil {
		return p.Assets.LangTag()
	}
	return "zh-CN"
}

func (p *PageContext) uiFont() string {
	if p != nil && p.Assets != nil {
		return p.Assets.FontFamily()
	}
	return "system-ui, PingFang SC, Hiragino Sans"
}
