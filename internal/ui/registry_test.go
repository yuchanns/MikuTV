package tvui

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/provider"
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

func TestMinimalSourceHidesUnsupportedModulesAndAllowsNonVideoKinds(t *testing.T) {
	assets, err := LoadAssets()
	if err != nil {
		t.Fatal(err)
	}
	pages, err := NewPages(assets)
	if err != nil {
		t.Fatal(err)
	}
	// Minimal catalog-only minimal catalog source: no SignIn, no EditVideo, no Comments, no Danmaku, no Subtitles.
	entries := []app.Entry{{
		ID: "indie-cinema", SourceID: "indie-cinema", Name: "Indie Cinema",
		Capabilities: provider.Capabilities{provider.BrowseCatalog, provider.GetCatalogItem, provider.ResolvePlayback},
	}}
	state, err := app.NewState(entries, entries[0].ID, pages.Routes)
	if err != nil {
		t.Fatal(err)
	}
	view := newTestView(t, Options{State: state, Pages: pages, Assets: assets, Theme: theme.Default()})
	view.SyncEntry()
	page := view.context(state.Location, true, 1920, 1080)
	movie := provider.MediaItem{
		Ref:   provider.MediaRef{EntryID: "indie-cinema", Kind: provider.Movie, ID: "film-42"},
		Title: "Independent Film",
		VideoDetails: &provider.VideoDetails{
			SegmentKind: provider.VideoChapters,
		},
	}
	if !page.canOpenVideoDetail(movie.Ref) {
		t.Fatal("movie media ref was rejected by canOpenVideoDetail")
	}
	if !state.Push(app.Route{Page: app.VideoDetail}) {
		t.Fatal("could not open movie detail")
	}
	view.videoDetail.item = movie
	tester := ui.NewTester(view.Render, 1920, 1080)
	tester.Frame()
	if !tester.HasText(assets.Text.Home.Play) {
		t.Fatal("expected Play button for ResolvePlayback source")
	}
	if tester.HasText(assets.Text.Detail.Comments) {
		t.Fatal("Comments tab must be hidden when ReadVideoComments is absent and discussion is empty")
	}
	if tester.HasText(assets.Text.Detail.Like) || tester.HasText(assets.Text.Detail.Favorite) || tester.HasText(assets.Text.Home.Later) {
		t.Fatal("EditVideo interaction buttons must be hidden when EditVideo capability is absent")
	}
	if state.Navigate(libraryRoute(provider.AccountFavorites)) {
		t.Fatal("Library sub-route must be rejected when BrowseLibrary / AccountFavorites are absent")
	}

	// With EditVideo alone, Like renders while WatchLater and Favorite stay gated on their own capabilities.
	entries[0].Capabilities = append(entries[0].Capabilities, provider.EditVideo)
	state.UpdateEntries(entries)
	tester.Frame()
	if !tester.HasText(assets.Text.Detail.Like) {
		t.Fatal("Like button should render for any EditVideo source")
	}
	if tester.HasText(assets.Text.Detail.Favorite) || tester.HasText(assets.Text.Home.Later) {
		t.Fatal("Favorite and WatchLater buttons must stay hidden without EditVideoFavorites / AccountWatchLater capabilities")
	}
	if view.focus.item(videoDetailActionFocus("coin")) != nil || view.focus.item(videoDetailActionFocus("triple")) != nil {
		t.Fatal("undeclared custom actions must not appear for neutral sources")
	}

	// Enabling AccountWatchLater and EditVideoFavorites reveals the remaining generic buttons.
	entries[0].Capabilities = append(entries[0].Capabilities, provider.AccountWatchLater.Capability(), provider.EditVideoFavorites)
	state.UpdateEntries(entries)
	tester.Frame()
	if !tester.HasText(assets.Text.Detail.Like) || !tester.HasText(assets.Text.Detail.Favorite) ||
		!tester.HasText(assets.Text.Home.Later) {
		t.Fatal("generic EditVideo buttons should render when their capabilities are declared")
	}

	// Verify atomic SetTheme preserves route & focus.
	light := theme.Default()
	light.ID, light.Dark = "custom-light", false
	light.Colors.Surface.Canvas = 0xf6f8fbff
	light.Colors.Text.Primary = 0x101826ff
	beforeRoute, beforeFocus := state.Route, state.Focus
	view.SetTheme(light)
	tester.Frame()
	if state.Route != beforeRoute || state.Focus != beforeFocus || view.Theme.ID != "custom-light" {
		t.Fatalf("SetTheme disturbed navigation state: route=%+v focus=%q theme=%q", state.Route, state.Focus, view.Theme.ID)
	}
}
