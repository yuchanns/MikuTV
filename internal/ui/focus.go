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
	group     string
	direction func(app.Direction) bool
}
type focusController struct {
	controls []focusItem
	groups   map[string]focusRegion
}

func (f *focusController) begin() {
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

func (v *View) move(direction app.Direction) {
	var current *focusItem
	for index := range v.focus.controls {
		if v.focus.controls[index].target.ID == v.State.Focus {
			current = &v.focus.controls[index]
			break
		}
	}
	if current == nil {
		return
	}
	if current.direction != nil && current.direction(direction) {
		return
	}
	var targets []app.FocusTarget
	for _, control := range v.focus.controls {
		if control.group == current.group {
			targets = append(targets, control.target)
		}
	}
	next := app.Neighbor(targets, v.State.Focus, direction)
	if next == v.State.Focus {
		if target := v.focus.destination(v.focus.groups[current.group].edges[direction]); target != "" {
			next = target
		}
	}
	if next != v.State.Focus {
		v.State.Focus, v.State.Area, v.reveal = next, v.focus.area(next), true
	}
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
				log.Printf("remote input: key=%v focus=%s", event.Key, v.State.Focus)
			}
		})
	}
	element.HandleInput(func(event ui.InputEvent) bool {
		if event.Kind != ui.InputKeyDown {
			return false
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
		case ui.KeyEscape:
			return v.goBack()
		default:
			return false
		}
		return true
	})
	if v.drawnRevision != v.State.Revision && v.State.Focus == id {
		element.Focus(ui.FocusOptions{PreventScroll: true})
	}
	r := element.Bounds()
	v.focus.controls = append(v.focus.controls, focusItem{target: app.FocusTarget{ID: id, X: r.X, Y: r.Y, W: r.W, H: r.H}, group: group, direction: direction})
	if v.reveal && v.State.Focus == id {
		element.ScrollIntoView()
	}
}
