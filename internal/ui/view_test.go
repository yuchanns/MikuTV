package tvui

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/provider"
	"github.com/yuchanns/MikuTV/internal/theme"
)

func testEntries() []app.Entry {
	return []app.Entry{{
		ID: "test-source", SourceID: "test-source", Name: "Test Source", Available: true,
		Capabilities: provider.Capabilities{provider.BrowseCatalog, provider.SearchCatalog},
	}}
}

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
	state, err := app.NewState(testEntries(), "test-source", pages.Routes)
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
	state, err := app.NewState(testEntries(), "test-source", pages.Routes)
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
	state, err := app.NewState(testEntries(), "test-source", pages.Routes)
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

func TestSourceCapabilitiesHideUnsupportedRoutesAndRailFocus(t *testing.T) {
	assets, err := LoadAssets()
	if err != nil {
		t.Fatal(err)
	}
	pages, err := NewPages(assets)
	if err != nil {
		t.Fatal(err)
	}
	entries := testEntries()
	state, err := app.NewState(entries, entries[0].ID, pages.Routes)
	if err != nil {
		t.Fatal(err)
	}
	if !state.Navigate(app.Route{Page: app.Search}) {
		t.Fatal("declared capability could not open search")
	}
	entries[0].Capabilities = provider.Capabilities{provider.BrowseCatalog}
	if err := state.SetEntries(entries); err != nil {
		t.Fatal(err)
	}
	if state.Page != app.Home || state.Navigate(app.Route{Page: app.Search}) {
		t.Fatalf("hidden route kept a focus target: %+v", state.Location)
	}
	primary := pages.navigation(state, primaryNavigation)
	if len(primary) != 1 || primary[0].Policy.Route.Page != app.Home {
		t.Fatalf("unsupported primary links are still present: %+v", primary)
	}
	utility := pages.navigation(state, utilityNavigation)
	if len(utility) != 2 || utility[0].Policy.Route.Page != app.Entries || utility[1].Policy.Route.Page != app.Settings {
		t.Fatalf("entry or settings navigation is unavailable: %+v", utility)
	}
}

type viewCatalogSession struct {
	page chan provider.CollectionPage
}

func (s viewCatalogSession) Browse(context.Context, provider.BrowseRequest) (provider.CollectionPage, error) {
	return <-s.page, nil
}

func (viewCatalogSession) Item(context.Context, provider.MediaRef) (provider.MediaItem, error) {
	return provider.MediaItem{}, nil
}

func (viewCatalogSession) Close() {}

func TestAsyncCollectionKeepsEntryActionFocused(t *testing.T) {
	assets, err := LoadAssets()
	if err != nil {
		t.Fatal(err)
	}
	pages, err := NewPages(assets)
	if err != nil {
		t.Fatal(err)
	}
	entries := testEntries()
	state, err := app.NewState(entries, entries[0].ID, pages.Routes)
	if err != nil {
		t.Fatal(err)
	}
	session := viewCatalogSession{page: make(chan provider.CollectionPage, 1)}
	completion := make(chan func(), 1)
	var frame *ui.Context
	catalog := app.NewCatalog(func(string) provider.Session { return session }, func(fn func()) { completion <- fn }, func() {
		if frame != nil {
			frame.Invalidate()
		}
	})
	defer catalog.Close()
	view := NewView(Options{State: state, Catalog: catalog, Pages: pages, Assets: assets, Preferences: &memoryPreferences{}, Theme: theme.Default()})
	catalog.Select(entries[0])
	tester := ui.NewTester(func(c *ui.Context) { frame = c; view.Render(c) }, 1920, 1080)
	if !tester.Focused(assets.Text.Cancel) {
		t.Fatal("loading action did not receive focus")
	}
	session.page <- provider.CollectionPage{Collections: []provider.Collection{{
		ID: "catalog", Title: "Catalog",
		Items: []provider.MediaItem{{Ref: provider.MediaRef{Kind: provider.Series, ID: "series"}, Title: "Show"}},
	}}}
	(<-completion)()
	tester.Frame()
	if !tester.Focused(assets.Text.SwitchEntry) {
		t.Fatal("collection replaced the focused action")
	}
	tester.Key(0, ui.KeyEnter)
	if state.Page != app.Entries {
		t.Fatalf("entry confirmation is unreachable: %+v", state.Location)
	}
}
