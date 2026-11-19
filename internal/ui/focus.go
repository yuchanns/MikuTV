package tvui

import (
	"log"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
)

type focusDestination struct {
	Focus  app.FocusID
	Region string
}
type focusEdges map[app.Direction]focusDestination
type focusRegion struct {
	edges      focusEdges
	navigation bool
}
type focusItem struct {
	target    app.FocusTarget
	elementID uint64
	group     string
	direction func(app.Direction) bool
}
type focusController struct {
	context  *ui.Context
	controls []focusItem
	groups   map[string]focusRegion
}

func (f *focusController) begin(c *ui.Context) {
	f.context = c
	f.controls = f.controls[:0]
	if f.groups == nil {
		f.groups = make(map[string]focusRegion)
	}
	clear(f.groups)
}

func (f *focusController) area(id app.FocusID) app.FocusArea {
	for _, control := range f.controls {
		if control.target.ID == id && f.groups[control.group].navigation {
			return app.NavigationArea
		}
	}
	return app.ContentArea
}

func (f *focusController) destination(destination focusDestination) app.FocusID {
	for _, control := range f.controls {
		if destination.Focus != "" && control.target.ID == destination.Focus {
			return destination.Focus
		}
	}
	for _, control := range f.controls {
		if destination.Region != "" && control.group == destination.Region {
			return control.target.ID
		}
	}
	return ""
}

func (f *focusController) item(id app.FocusID) *focusItem {
	for index := range f.controls {
		if f.controls[index].target.ID == id {
			return &f.controls[index]
		}
	}
	return nil
}

func (f *focusController) neighbor(current *focusItem, direction app.Direction) app.FocusID {
	var targets []app.FocusTarget
	for _, control := range f.controls {
		if control.group == current.group {
			r := f.context.CommittedBounds(control.elementID)
			targets = append(targets, app.FocusTarget{ID: control.target.ID, X: r.X, Y: r.Y, W: r.W, H: r.H})
		}
	}
	next := app.Neighbor(targets, current.target.ID, direction)
	if next == current.target.ID {
		if target := f.destination(f.groups[current.group].edges[direction]); target != "" {
			next = target
		}
	}
	return next
}

func (v *View) move(direction app.Direction) {
	current := v.focus.item(v.State.Focus)
	if current == nil || (current.direction != nil && current.direction(direction)) {
		return
	}
	next := v.focus.neighbor(current, direction)
	if next == v.State.Focus {
		return
	}
	v.moveFocus(next, direction, true)
	if v.State.Page != app.Home || (direction != app.Left && direction != app.Right) {
		return
	}
	switch current.group {
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

func (v *View) moveFocus(next app.FocusID, direction app.Direction, reveal bool) {
	if v.State.Page == app.Home {
		v.home.featureStep = 1
		if direction == app.Left || direction == app.Up {
			v.home.featureStep = -1
		}
	}
	v.State.Focus, v.State.Area, v.reveal = next, v.focus.area(next), reveal
}

func (v *View) bind(element ui.Element, id app.FocusID, group string, active bool, direction func(app.Direction) bool) {
	element.FocusRing(false)
	if !active {
		return
	}
	element.FocusBind(&v.State.Focus, id)
	if v.Trace {
		element.ObserveInput(func(event ui.InputEvent) {
			if event.Kind == ui.InputKeyDown {
				log.Printf("remote input: key=%v focus=%s repeat=%t", event.Key, v.State.Focus, event.Repeat)
			}
		})
	}
	element.HandleInput(func(event ui.InputEvent) bool {
		if event.Kind != ui.InputKeyDown {
			return false
		}
		if event.Repeat && v.startCatalogHold(event.Key, group) {
			return true
		}
		switch event.Key {
		case ui.KeyLeft:
			v.dispatch(func() { v.move(app.Left) })
		case ui.KeyRight:
			v.dispatch(func() { v.move(app.Right) })
		case ui.KeyUp:
			v.dispatch(func() { v.move(app.Up) })
		case ui.KeyDown:
			v.dispatch(func() { v.move(app.Down) })
		default:
			return false
		}
		return true
	})
	if v.State.Focus == id {
		returningToHeader := group == homeHeader && v.activeScroll != nil && v.activeScroll.Scrolling()
		if v.reveal || v.drawnRevision != v.State.Revision || v.home.hold.key != ui.KeyUnknown || returningToHeader {
			element.Focus(ui.FocusOptions{PreventScroll: true})
		}
	}
	r := element.Bounds()
	v.focus.controls = append(v.focus.controls, focusItem{
		target:    app.FocusTarget{ID: id, X: r.X, Y: r.Y, W: r.W, H: r.H},
		elementID: element.ID(), group: group, direction: direction,
	})
	if v.reveal && v.State.Focus == id {
		if v.State.Page == app.Home && v.activeScroll != nil && group == homeHeader {
			v.activeScroll.ScrollTo(0, 0, focusScroll)
			return
		}
		element.ScrollIntoView(ui.ScrollIntoViewOptions{Animation: &focusScroll, BlockEndMargin: focusBottomMargin})
	}
}
