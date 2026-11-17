package tvui

import (
	"math"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/theme"
)

type memoryPreferences struct{ value app.Preferences }

func (p *memoryPreferences) Load() (app.Preferences, error) { return p.value, nil }
func (p *memoryPreferences) Save(value app.Preferences) error {
	p.value = value
	return nil
}

func TestNavigationConfirmationReturnsFocusToContent(t *testing.T) {
	assets, err := LoadAssets()
	if err != nil {
		t.Fatal(err)
	}
	pages, err := NewPages(assets)
	if err != nil {
		t.Fatal(err)
	}
	state, err := app.NewState(assets.Entries, assets.Entries[0].ID, pages.Routes)
	if err != nil {
		t.Fatal(err)
	}
	view := NewView(Options{State: state, Pages: pages, Assets: assets, Preferences: &memoryPreferences{}, Theme: theme.Default(), Version: "test"})
	var context *ui.Context
	tester := ui.NewTester(func(c *ui.Context) { context = c; view.Render(c) }, 1920, 1080)
	for _, step := range []struct {
		key  ui.Key
		page app.Page
	}{{0, app.Home}, {ui.KeyDown, app.Search}, {ui.KeyUp, app.Home}, {0, app.Home}} {
		tester.Key(0, ui.KeyLeft)
		if step.key != 0 {
			tester.Key(0, step.key)
		}
		tester.Key(0, ui.KeyEnter)
		if state.Page != step.page || state.Area != app.ContentArea || ui.FocusedValue(context, &state.Focus) != app.ContentAction {
			t.Fatalf("confirming %s kept focus in navigation: %+v", step.page, state.Location)
		}
	}
}

func TestExpandedNavigationPushesContentWithoutReflow(t *testing.T) {
	assets, err := LoadAssets()
	if err != nil {
		t.Fatal(err)
	}
	pages, err := NewPages(assets)
	if err != nil {
		t.Fatal(err)
	}
	state, err := app.NewState(assets.Entries, assets.Entries[0].ID, pages.Routes)
	if err != nil {
		t.Fatal(err)
	}
	view := NewView(Options{State: state, Pages: pages, Assets: assets, Preferences: &memoryPreferences{}, Theme: theme.Default(), Version: "test"})
	tester := ui.NewTester(view.Render, 1920, 1080)
	before, ok := tester.Find(assets.Text.EmptyTitle)
	if !ok {
		t.Fatal("content is missing")
	}
	tester.Key(0, ui.KeyLeft)
	tester.Advance(railDuration / 2)
	middle, ok := tester.Find(assets.Text.EmptyTitle)
	if !ok || middle.X <= before.X || middle.X >= before.X+172 {
		t.Fatalf("content did not follow navigation during expansion: %+v", middle)
	}
	tester.Advance(railDuration + time.Millisecond)
	expanded, ok := tester.Find(assets.Text.EmptyTitle)
	near := func(a, b float32) bool { return math.Abs(float64(a-b)) < 0.01 }
	if !ok || !near(expanded.X-before.X, 172) || !near(expanded.W, before.W) || !near(expanded.Y, before.Y) {
		t.Fatalf("navigation covered or reflowed content: before=%+v expanded=%+v", before, expanded)
	}
	tester.Key(0, ui.KeyRight)
	tester.Advance(railDuration + time.Millisecond)
	restored, ok := tester.Find(assets.Text.EmptyTitle)
	if !ok || !near(restored.X, before.X) || !near(restored.Y, before.Y) || !near(restored.W, before.W) || !near(restored.H, before.H) || state.Focus != app.ContentAction {
		t.Fatalf("closing navigation lost the content position: %+v", restored)
	}
}

func TestSettingsKeepFocusAndModalReturnsToItsTrigger(t *testing.T) {
	assets, err := LoadAssets()
	if err != nil {
		t.Fatal(err)
	}
	pages, err := NewPages(assets)
	if err != nil {
		t.Fatal(err)
	}
	state, err := app.NewState(assets.Entries, assets.Entries[0].ID, pages.Routes)
	if err != nil {
		t.Fatal(err)
	}
	v := NewView(Options{State: state, Pages: pages, Assets: assets, Preferences: &memoryPreferences{}, Theme: theme.Default(), Version: "test"})
	var context *ui.Context
	tester := ui.NewTester(func(c *ui.Context) { context = c; v.Render(c) }, 1920, 1080)
	v.dispatch(func() { state.Navigate(app.Route{Page: app.Settings}) })
	tester.Frame()
	for range 5 {
		tester.Key(0, ui.KeyDown)
	}
	tester.Key(0, ui.KeyEnter)
	if state.Section != "about" || ui.FocusedValue(context, &state.Focus) != app.SectionFocus("about") {
		t.Fatalf("section switch lost focus: %+v", state.Location)
	}
	for _, step := range []struct {
		key     ui.Key
		section string
	}{{ui.KeyUp, "plugins"}, {ui.KeyUp, "privacy"}, {ui.KeyDown, "plugins"}, {ui.KeyDown, "about"}} {
		tester.Key(0, step.key)
		tester.Key(0, ui.KeyEnter)
		if state.Section != step.section || ui.FocusedValue(context, &state.Focus) != app.SectionFocus(step.section) {
			t.Fatalf("revisiting %s restored another section's highlight: %+v", step.section, state.Location)
		}
	}
	tester.Key(0, ui.KeyRight)
	tester.Key(0, ui.KeyEnter)
	if state.Page != app.Licenses || ui.FocusedValue(context, &state.Focus) != app.LicenseText {
		t.Fatalf("modal did not take focus: %+v", state.Location)
	}
	tester.Key(0, ui.KeyDown)
	if v.activeScroll.Y <= 0 {
		t.Fatal("remote down did not scroll the license text")
	}
	tester.Key(0, ui.KeyLeft)
	if focused := ui.FocusedValue(context, &state.Focus); state.Page != app.Licenses || (focused != app.LicenseText && focused != app.CloseModal) {
		t.Fatal("modal focus escaped to the background")
	}
	tester.Key(0, ui.KeyEscape)
	if state.Page != app.Settings || state.Section != "about" || ui.FocusedValue(context, &state.Focus) != app.ShowLicenses {
		t.Fatalf("Back did not restore the modal trigger: %+v", state.Location)
	}
}

func TestEmptyEntriesKeepManagementReachable(t *testing.T) {
	assets, err := LoadAssets()
	if err != nil {
		t.Fatal(err)
	}
	pages, err := NewPages(assets)
	if err != nil {
		t.Fatal(err)
	}
	state, err := app.NewState(nil, "removed", pages.Routes)
	if err != nil {
		t.Fatal(err)
	}
	v := NewView(Options{State: state, Pages: pages, Assets: assets, Preferences: &memoryPreferences{}, Theme: theme.Default(), Version: "test"})
	var context *ui.Context
	tester := ui.NewTester(func(c *ui.Context) { context = c; v.Render(c) }, 1920, 1080)
	if len(pages.navigation(state, primaryNavigation)) != 0 || len(pages.navigation(state, utilityNavigation)) != 2 {
		t.Fatal("content navigation remains available without an entry")
	}
	if ui.FocusedValue(context, &state.Focus) != app.ManagePlugins {
		t.Fatal("empty entry selection has no focused management action")
	}
	tester.Key(0, ui.KeyEnter)
	if state.Page != app.Settings || state.Section != "plugins" {
		t.Fatal("management action did not open the plugin settings")
	}
	tester.Key(0, ui.KeyEscape)
	if state.Page != app.Entries || ui.FocusedValue(context, &state.Focus) != app.ManagePlugins {
		t.Fatal("Back did not restore the empty entry action")
	}
}
