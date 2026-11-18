package tvui

import (
	"log"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/theme"
)

const (
	focusDuration = 140 * time.Millisecond
	railDuration  = 220 * time.Millisecond
)

type scrollKey struct {
	entry string
	route app.Route
}

type Options struct {
	State       *app.State
	Catalog     *app.Catalog
	Pages       *Pages
	Assets      *Assets
	Preferences app.PreferencesStore
	Theme       theme.Theme
	Version     string
}

type View struct {
	State         *app.State
	Catalog       *app.Catalog
	Pages         *Pages
	Assets        *Assets
	Preferences   app.PreferencesStore
	Theme         theme.Theme
	Version       string
	Trace         bool
	native        ui.Theme
	focus         focusController
	scrolls       map[scrollKey]*ui.ScrollState
	activeScroll  *ui.ScrollState
	drawnRevision uint64
	reveal        bool
	notice        string
}

func NewView(options Options) *View {
	return &View{State: options.State, Catalog: options.Catalog, Pages: options.Pages, Assets: options.Assets, Preferences: options.Preferences,
		Theme: options.Theme, Version: options.Version, native: nativeTheme(options.Theme), scrolls: make(map[scrollKey]*ui.ScrollState)}
}

// SetTheme atomically replaces the active theme and refreshes the derived native UI theme.
func (v *View) SetTheme(palette theme.Theme) {
	v.Theme = palette
	v.native = nativeTheme(palette)
}

func (v *View) Render(c *ui.Context) {
	c.SetTheme(&v.native)
	v.focus.begin()
	v.activeScroll = nil
	width, height := c.Size()
	layers := v.State.Layers()
	ui.Box(c).Fill().Clip().Background(paint(v.Theme.Colors.Surface.Canvas)).Children(func() {
		page := v.context(layers[0], len(layers) == 1, width-collapsedRailWidth, height)
		ui.Row(c).Fill().Children(func() {
			navigationRail{}.Render(c, page)
			ui.Box(c.Key("main")).Width(width - collapsedRailWidth).Height(height).Shrink(0).
				Children(func() { v.renderScreen(c, page) })
		})
		if v.notice != "" {
			ui.Text(c, v.notice).Absolute().Right(32).Bottom(28).Padding(18, 24).
				Background(paint(v.Theme.Colors.Surface.Raised)).Radius(12).FontSize(20)
		}
	})
	for index, location := range layers[1:] {
		v.renderModal(c, v.context(location, index == len(layers)-2, width, height))
	}
	v.drawnRevision = v.State.Revision
	v.reveal = false
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
	scroll := page.scroll()
	ui.Scroll(c.Key("page/" + string(chain[0].Page))).Fill().TrackScroll(scroll).Children(func() {
		ui.Column(c).FillWidth().Padding(148, 83, 80, 103).Gap(44).Children(func() { v.Pages.render(c, page) })
	})
}

func (v *View) renderModal(c *ui.Context, page *PageContext) {
	page.left = focusDestination{}
	registration := v.Pages.components[page.Location.Route]
	page.Title, page.Node = registration.Title, registration.Policy.Route
	colors := v.Theme.Colors
	width, height := registration.ModalWidth, registration.ModalHeight
	if width == 0 {
		width = 1100
	}
	if height == 0 {
		height = 850
	}
	insets := registration.ModalInsets
	if insets == ([4]float32{}) {
		insets = [4]float32{45, 45, 45, 45}
	}
	ui.Overlay(c, func() {
		ui.Box(c.Key(page.Location.Route)).Absolute().Left(0).Top(0).Width(page.Width).Height(page.Height).
			Modal().Background(paint(colors.Overlay.Scrim)).Center().Children(func() {
			ui.Column(c).Width(min(width, page.Width-180)).Height(min(height, page.Height-150)).
				Padding(insets[0], insets[1], insets[2], insets[3]).Gap(28).
				Background(paint(colors.Surface.Panel)).Radius(30).Children(func() { registration.Component.Render(c, page) })
		})
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
	v.notice = ""
	position := v.State.Position
	position.Focus, position.Area = v.State.Focus, v.focus.area(v.State.Focus)
	if scroll := v.activeScroll; scroll != nil {
		position.X, position.Y = scroll.X, scroll.Y
	}
	v.State.RecordPosition(position)
	action()
	if v.Catalog != nil {
		entry, _ := v.State.Current()
		v.Catalog.Select(entry)
	}
	if v.Trace {
		log.Printf("navigation: entry=%s page=%s section=%s focus=%s", v.State.CurrentEntry, v.State.Page, v.State.Section, v.State.Focus)
	}
}
