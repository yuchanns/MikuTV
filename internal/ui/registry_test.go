package tvui

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/theme"
)

func TestRegisteredComponentsAndNestedOverlaysNeedNoShellCases(t *testing.T) {
	assets, err := LoadAssets()
	if err != nil {
		t.Fatal(err)
	}
	screen, one, two := app.Route{Page: "extension-screen"}, app.Route{Page: "extension-dialog-one"}, app.Route{Page: "extension-dialog-two"}
	open, first, second := app.FocusID("extension/open"), app.FocusID("extension/one"), app.FocusID("extension/two")
	renderButton := func(label string, id app.FocusID, action func(*app.State)) Component {
		return ComponentFunc(func(c *ui.Context, p *PageContext) {
			p.region("extension", nil)
			p.button(c, label, id, "extension", func() { action(p.State) })
		})
	}
	pages, err := NewPages(assets,
		Registration{Policy: app.RouteSpec{Route: screen, Initial: app.Position{Focus: open}}, Component: renderButton("Open", open, func(s *app.State) { s.Push(one) }), Navigation: &NavigationLink{Title: "Extension", Icon: "info"}},
		Registration{Policy: app.RouteSpec{Route: one, Parent: screen, Modal: true, Initial: app.Position{Focus: first}}, Component: renderButton("Next", first, func(s *app.State) { s.Push(two) }), Title: "First"},
		Registration{Policy: app.RouteSpec{Route: two, Parent: one, Modal: true, Initial: app.Position{Focus: second}}, Component: renderButton("Close", second, func(s *app.State) { s.Back() }), Title: "Second"},
	)
	if err != nil {
		t.Fatal(err)
	}
	state, err := app.NewState(testEntries(), "test-source", pages.Routes)
	if err != nil {
		t.Fatal(err)
	}
	view := newTestView(t, Options{State: state, Pages: pages, Assets: assets, Theme: theme.Default(), Version: "test"})
	var context *ui.Context
	tester := ui.NewTester(func(c *ui.Context) { context = c; view.Render(c) }, 1920, 1080)
	view.dispatch(func() { state.Navigate(screen) })
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	tester.Key(0, ui.KeyEnter)
	if layers := state.Layers(); len(layers) != 3 || layers[0].Route != screen || ui.FocusedValue(context, &state.Focus) != second {
		t.Fatalf("registered nested overlays did not render: %+v", layers)
	}
	tester.Key(0, ui.KeyEscape)
	if state.Route != one || ui.FocusedValue(context, &state.Focus) != first {
		t.Fatal("Back did not restore the first registered dialog")
	}
	tester.Key(0, ui.KeyEscape)
	if state.Route != screen || ui.FocusedValue(context, &state.Focus) != open {
		t.Fatal("Back did not restore the registered caller")
	}
}
