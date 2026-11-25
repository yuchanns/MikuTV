package tvui

import (
	"context"
	"fmt"
	"image"
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
		ID: "test-source", SourceID: "test-source", Name: "Test Source",
		Capabilities: provider.Capabilities{provider.BrowseCatalog, provider.SearchCatalog},
	}}
}

type memoryPreferences struct {
	value               app.Preferences
	historySyncDisabled bool
}

func (p *memoryPreferences) Save(value app.Preferences) error {
	p.value = value
	return nil
}

func (p *memoryPreferences) HistorySync(string, string) bool { return !p.historySyncDisabled }

func (p *memoryPreferences) SaveHistorySync(_, _ string, enabled bool) error {
	p.historySyncDisabled = !enabled
	return nil
}

func newTestView(t *testing.T, options Options) *View {
	t.Helper()
	post := func(func()) {}
	changed := func() {}
	preferences := &memoryPreferences{}
	if options.Preferences == nil {
		options.Preferences = preferences
	}
	if options.Catalog == nil {
		options.Catalog = app.NewCatalog(func(string) provider.Session { return viewCatalogSession{} }, post, changed)
		t.Cleanup(options.Catalog.Close)
	}
	if options.Auth == nil {
		options.Auth = app.NewAuth(uiTestKeychain{}, post, changed, changed, changed)
		t.Cleanup(options.Auth.Close)
	}
	if options.Activity == nil {
		options.Activity = app.NewAccountActivity(preferences, post, changed)
		t.Cleanup(options.Activity.Close)
	}
	if options.Search == nil {
		options.Search = app.NewCatalogSearch(nil, post, changed)
		t.Cleanup(options.Search.Close)
	}
	if options.Following == nil {
		options.Following = app.NewFollowingFeed(post, changed)
		t.Cleanup(options.Following.Close)
	}
	if options.Series == nil {
		options.Series = app.NewSeriesCatalog(post, changed)
		t.Cleanup(options.Series.Close)
	}
	return NewView(options)
}

func TestWindowBackUsesHistoryForNewPagesAndModals(t *testing.T) {
	assets, err := LoadAssets()
	if err != nil {
		t.Fatal(err)
	}
	page := app.Route{Page: "test-page"}
	dialog := app.Route{Page: "test-dialog"}
	empty := ComponentFunc(func(*ui.Context, *PageContext) {})
	pages, err := NewPages(assets,
		Registration{Policy: app.RouteSpec{Route: page, Transient: true}, Component: empty},
		Registration{Policy: app.RouteSpec{Route: dialog, Parent: page, Modal: true}, Component: empty},
	)
	if err != nil {
		t.Fatal(err)
	}
	state, err := app.NewState(testEntries(), "test-source", pages.Routes)
	if err != nil {
		t.Fatal(err)
	}
	view := newTestView(t, Options{State: state, Pages: pages, Assets: assets, Theme: theme.Default()})
	tester := ui.NewTester(view.Render, 1920, 1080)
	if !state.Push(page) || !state.Push(dialog) {
		t.Fatal("could not open the empty page and dialog")
	}
	tester.Frame()
	for _, want := range []app.Route{page, {Page: app.Home}} {
		tester.Key(0, ui.KeyEscape)
		if state.Route != want {
			t.Fatalf("Back restored %v, want %v", state.Route, want)
		}
	}
	state.RecordPosition(app.Position{Focus: app.RailFocus(app.Home), Area: app.NavigationArea})
	if view.goBack() {
		t.Fatal("the root consumed the system Back action")
	}
}

func TestHomeBackScrollsToTheCurrentTabBeforeExpandingRail(t *testing.T) {
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
	catalog := app.NewCatalog(func(string) provider.Session { return session }, func(fn func()) { completion <- fn }, func() {})
	t.Cleanup(catalog.Close)
	view := newTestView(t, Options{State: state, Catalog: catalog, Pages: pages, Assets: assets, Theme: theme.Default()})
	var frame *ui.Context
	tester := ui.NewTester(func(c *ui.Context) { frame = c; view.Render(c) }, 1920, 1080)
	view.SyncEntry()
	session.page <- provider.CollectionPage{}
	(<-completion)()
	tester.Frame()
	if state.Focus != homePlayAction {
		t.Fatalf("Home did not start focused on hero play action: %q", state.Focus)
	}
	tester.Key(0, ui.KeyUp)
	tester.Key(0, ui.KeyRight)
	if view.home.tab != "popular" {
		t.Fatalf("Right selected home tab %q, want popular", view.home.tab)
	}

	feed := provider.Collection{ID: "popular", Title: "Popular"}
	for index := range 50 {
		id := fmt.Sprintf("video-%d", index)
		feed.Items = append(feed.Items, provider.MediaItem{Ref: provider.MediaRef{Kind: provider.Video, ID: id}, Title: id})
	}
	session.page <- provider.CollectionPage{Collections: []provider.Collection{feed}}
	(<-completion)()
	tester.Frame()
	tab := homeTabID("popular")
	tester.Key(0, ui.KeyDown)
	if state.Focus == tab || view.activeScroll.Y != 0 {
		t.Fatalf("did not select a card in the first viewport: focus=%q scroll=%v", state.Focus, view.activeScroll.Y)
	}
	tester.Key(0, ui.KeyEscape)
	if ui.FocusedValue(frame, &state.Focus) != tab || view.activeScroll.Y != 0 {
		t.Fatalf("Menu skipped the tab in the first viewport: focus=%q scroll=%v", state.Focus, view.activeScroll.Y)
	}

	card := catalogCardFocus(feed.ID, feed.Items[36].Ref)
	state.RecordPosition(app.Position{Focus: card, Y: view.activeScroll.MaxY})
	view.activeScroll.Y = view.activeScroll.MaxY
	tester.Frame()
	start := view.activeScroll.Y
	if start <= 1080 || ui.FocusedValue(frame, &state.Focus) != card {
		t.Fatalf("did not start from a deep card: focus=%q scroll=%v", state.Focus, start)
	}

	tester.Key(0, ui.KeyBack)
	tester.Advance(homeReturnScroll.Duration / 2)
	if offset := view.activeScroll.Y; offset <= 0 || offset >= start || ui.FocusedValue(frame, &state.Focus) != tab {
		t.Fatalf("Back did not animate toward the original tab: focus=%q scroll=%v", state.Focus, offset)
	}
	tester.Key(0, ui.KeyEscape)
	tester.Advance(homeReturnScroll.Duration/2 + time.Second/60)
	if state.Area != app.ContentArea || state.Focus != tab || view.activeScroll.Y != 0 {
		t.Fatalf("Menu interrupted the return to the tab: %+v scroll=%v", state.Location, view.activeScroll.Y)
	}
	tester.Key(0, ui.KeyEscape)
	if state.Area != app.NavigationArea || ui.FocusedValue(frame, &state.Focus) != app.RailFocus(app.Home) {
		t.Fatalf("Menu at the top did not expand the sidebar: %+v", state.Location)
	}
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
	view := newTestView(t, Options{State: state, Pages: pages, Assets: assets, Theme: theme.Default(), Version: "test"})
	var context *ui.Context
	tester := ui.NewTester(func(c *ui.Context) { context = c; view.Render(c) }, 1920, 1080)
	for _, step := range []struct {
		key   ui.Key
		page  app.Page
		focus app.FocusID
	}{
		{0, app.Home, homePlayAction},
		{ui.KeyDown, app.Search, app.ContentAction},
		{ui.KeyUp, app.Home, homePlayAction},
		{0, app.Home, homePlayAction},
	} {
		tester.Key(0, ui.KeyLeft)
		if step.key != 0 {
			tester.Key(0, step.key)
		}
		tester.Key(0, ui.KeyEnter)
		if state.Page != step.page || state.Area != app.ContentArea || ui.FocusedValue(context, &state.Focus) != step.focus {
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
	entries := testEntries()
	entries[0].Capabilities = nil
	state, err := app.NewState(entries, "test-source", pages.Routes)
	if err != nil {
		t.Fatal(err)
	}
	view := newTestView(t, Options{State: state, Pages: pages, Assets: assets, Theme: theme.Default(), Version: "test"})
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
	v := newTestView(t, Options{State: state, Pages: pages, Assets: assets, Theme: theme.Default(), Version: "test"})
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
	v := newTestView(t, Options{State: state, Pages: pages, Assets: assets, Theme: theme.Default(), Version: "test"})
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
	state, err = app.NewState(entries, entries[0].ID, pages.Routes)
	if err != nil {
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
	page    chan provider.CollectionPage
	artwork chan image.Image
}

func (s viewCatalogSession) Browse(ctx context.Context, _ provider.BrowseRequest) (provider.CollectionPage, error) {
	select {
	case page := <-s.page:
		return page, nil
	case <-ctx.Done():
		return provider.CollectionPage{}, ctx.Err()
	}
}

func (viewCatalogSession) Item(context.Context, provider.MediaRef) (provider.MediaItem, error) {
	return provider.MediaItem{}, nil
}

func (viewCatalogSession) Close() {}

func (s viewCatalogSession) LoadCatalogArtwork(ctx context.Context, _ string) (image.Image, error) {
	select {
	case art := <-s.artwork:
		return art, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestHomeKeepsFocusThroughArtworkPagingAndFiltering(t *testing.T) {
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
	session := viewCatalogSession{
		page: make(chan provider.CollectionPage, 1), artwork: make(chan image.Image, 1),
	}
	completion := make(chan func(), 1)
	var frame *ui.Context
	catalog := app.NewCatalog(func(string) provider.Session { return session }, func(fn func()) { completion <- fn }, func() {
		if frame != nil {
			frame.Invalidate()
		}
	})
	defer catalog.Close()
	view := newTestView(t, Options{State: state, Catalog: catalog, Pages: pages, Assets: assets, Theme: theme.Default()})
	tester := ui.NewTester(func(c *ui.Context) { frame = c; view.Render(c) }, 1920, 1080)
	view.SyncEntry()
	tester.Frame()
	if state.Focus != homePlayAction || !tester.Focused(assets.Text.Home.Play) {
		t.Fatalf("loading changed the initial home hero focus: %q", state.Focus)
	}

	first := provider.MediaRef{Kind: provider.Video, ID: "first"}
	session.page <- provider.CollectionPage{NextCursor: "next", Collections: []provider.Collection{{
		ID: "catalog", Title: "Recommendations",
		Items: []provider.MediaItem{
			{Ref: first, Title: "First video", CoverURL: "https://example.com/cover.jpg"},
			{Ref: provider.MediaRef{Kind: provider.Video, ID: "second"}, Title: "Second video"},
			{Ref: provider.MediaRef{Kind: provider.Video, ID: "third"}, Title: "Third video"},
		},
	}}}
	(<-completion)()
	tester.Frame()
	session.artwork <- image.NewRGBA(image.Rect(0, 0, 16, 9))
	(<-completion)()
	tester.Frame()
	if state.Focus != homePlayAction || !tester.Focused(assets.Text.Home.Play) {
		t.Fatalf("loading the backdrop lost the focused hero action: %q", state.Focus)
	}
	tester.Key(0, ui.KeyRight)
	if view.home.tab != "" {
		t.Fatalf("Right from initial Home focus unexpectedly switched tab to %q", view.home.tab)
	}
	tester.Key(0, ui.KeyLeft)
	if state.Focus != homePlayAction {
		t.Fatalf("Left did not return to homePlayAction: %s", state.Focus)
	}
	tester.Key(0, ui.KeyDown)
	if state.Focus != catalogCardFocus("picks", first) {
		t.Fatalf("down from the hero actions focused %s (expected first picks card when continue history is empty)", state.Focus)
	}
	tester.Key(0, ui.KeyUp)
	view.activeScroll.Y = 90
	state.RecordPosition(app.Position{Focus: homePlayAction, Y: 90})
	tester.Frame()
	tester.Key(0, ui.KeyUp)
	tester.Advance(2 * focusScroll.Duration)
	if state.Focus != homeTabID("") || view.activeScroll.Y != 0 {
		t.Fatalf("return to the home tabs left focus %s and scroll offset %v", state.Focus, view.activeScroll.Y)
	}
	tester.Advance(homeFeatureInterval)
	if view.home.selected != 1 {
		t.Fatalf("idle hero did not rotate with focus on the tab bar: %d", view.home.selected)
	}
	state.RecordPosition(app.Position{Focus: homeFeatureID(2)})
	tester.Frame()
	tester.Advance(2 * homeFeatureInterval)
	if view.home.selected != 2 {
		t.Fatalf("idle timer overwrote the focused feature: %d", view.home.selected)
	}

	card := catalogCardFocus("catalog", first)
	y := view.home.layout.end - 900
	view.activeScroll.Y = y
	state.RecordPosition(app.Position{Focus: card, Y: y})
	tester.Frame()
	if snapshot := catalog.Snapshot(); snapshot.Phase != app.CatalogLoading || !snapshot.Appending {
		t.Fatalf("reaching the last row did not request continuation: %+v", snapshot)
	}
	if got := view.home.layout.sections[0].cards[0]; got != state.Focus {
		t.Fatalf("prefetch moved focus from the current card: %s", state.Focus)
	}
	next := provider.MediaRef{Kind: provider.Video, ID: "next"}
	overflow := provider.MediaRef{Kind: provider.Video, ID: "overflow"}
	session.page <- provider.CollectionPage{NextCursor: "repeated", Collections: []provider.Collection{{
		ID: "catalog", Title: "Recommendations", Items: []provider.MediaItem{
			{Ref: next, Title: "New video"},
			{Ref: overflow, Title: "Last video in the row"},
		},
	}}}
	(<-completion)()
	tester.Frame()
	if state.Focus != card || !tester.Focused("First video") {
		t.Fatalf("appending cards moved focus: %s", state.Focus)
	}
	if count := len(view.home.layout.sections[0].cards); count != 5 {
		t.Fatalf("expected old and new cards, got %d", count)
	}
	session.page <- provider.CollectionPage{NextCursor: "advanced", Collections: []provider.Collection{{
		ID: "catalog", Title: "Recommendations", Items: []provider.MediaItem{{Ref: next, Title: "New video"}},
	}}}
	(<-completion)()
	tester.Frame()
	if snapshot := catalog.Snapshot(); !snapshot.AppendStalled || snapshot.CanLoadMore() {
		t.Fatal("a repeat-only page did not stop pagination")
	}
	rightmost := view.home.layout.sections[0].cards[4]
	state.RecordPosition(app.Position{Focus: view.home.layout.sections[0].cards[3], Y: view.activeScroll.Y})
	tester.Frame()
	if peek := view.focus.item(rightmost); peek == nil || peek.target.X >= 1920 || peek.target.X+peek.target.W <= 1920 || peek.target.W != homeShelfCoverWidth {
		t.Fatalf("the last thumbnail should extend past the screen at its full width: %+v", peek)
	}
	tester.Key(0, ui.KeyRight)
	tester.Advance(2 * focusScroll.Duration)
	if revealed := view.focus.item(rightmost); state.Focus != rightmost || revealed == nil || revealed.target.X+revealed.target.W > 1920.5 {
		t.Fatalf("Right did not reveal the rest of the last card: focus %s, bounds %+v", state.Focus, revealed)
	}
	tester.Key(0, ui.KeyRight)
	if state.Focus != rightmost {
		t.Fatalf("the last card did not keep focus at the end of its row: %s", state.Focus)
	}
	tester.Key(0, ui.KeyUp)
	if state.Focus != homeRefresh {
		t.Fatalf("Up from the rightmost card focused %s", state.Focus)
	}
	refresh := view.focus.item(homeRefresh)
	if refresh == nil || refresh.target.X+refresh.target.W != 1920-homeRefreshRight ||
		refresh.target.W != homeRefreshWidth || refresh.target.H != homeRefreshHeight {
		t.Fatalf("the refresh action does not match its prototype bounds: %+v", refresh)
	}
	tester.Key(0, ui.KeyDown)
	if state.Focus != rightmost {
		t.Fatalf("Down from the heading action focused %s", state.Focus)
	}
	tester.Key(0, ui.KeyDown)
	if state.Focus != homeEnd || catalog.Snapshot().Phase != app.CatalogReady {
		t.Fatal("Down from the last row did not reach the refresh action")
	}
	if !tester.Focused(assets.Text.Refresh) {
		t.Fatal("the refresh action was not focused at the end of recommendations")
	}
	tester.Key(0, ui.KeyDown)
	if state.Focus != homeEnd || catalog.Snapshot().Phase != app.CatalogReady {
		t.Fatal("Down on the last refresh card should leave focus and pagination unchanged")
	}

	state.RecordPosition(app.Position{Focus: homeTabID("")})
	view.activeScroll.Y = 0
	tester.Frame()
	for _, tab := range []string{"popular", "ranking", "regions"} {
		tester.Key(0, ui.KeyRight)
		if state.Focus != homeTabID(tab) || view.home.tab != tab || catalog.Snapshot().Phase != app.CatalogLoading {
			t.Fatalf("moving focus to %s did not switch the catalog", tab)
		}
		initial := provider.CollectionPage{
			Filters: []provider.CatalogFilter{{ID: "0", Title: "All"}, {ID: "1", Title: "Animation"}},
			Collections: []provider.Collection{{ID: tab, Title: tab, Items: []provider.MediaItem{
				{Ref: first, Title: "First video"},
			}}},
		}
		session.page <- initial
		(<-completion)()
		tester.Frame()
		tester.Advance(2 * focusScroll.Duration)

		tester.Key(0, ui.KeyDown)
		filter := homeFilterID("1")
		bounds := func() app.FocusTarget {
			t.Helper()
			control := view.focus.item(filter)
			if control == nil {
				t.Fatalf("%s filter is missing", tab)
			}
			return control.target
		}
		before := bounds()
		tester.Key(0, ui.KeyRight)
		snapshot := catalog.Snapshot()
		if snapshot.Phase != app.CatalogLoading || len(snapshot.Page.Collections) != 0 ||
			view.home.filter != "1" || state.Focus != filter || bounds() != before {
			t.Fatalf("moving right did not request %s results and keep the tag in place", tab)
		}
		session.page <- provider.CollectionPage{Collections: []provider.Collection{{ID: tab, Title: tab, Items: []provider.MediaItem{
			{Ref: next, Title: "Filtered video"},
		}}}}
		(<-completion)()
		tester.Frame()
		if state.Focus != filter || bounds() != before {
			t.Fatalf("receiving %s results changed the tag geometry or focus", tab)
		}
		tester.Key(0, ui.KeyDown)
		if state.Focus != catalogCardFocus(tab, next) {
			t.Fatalf("moving down reached %s instead of the new %s results", state.Focus, tab)
		}
		tester.Key(0, ui.KeyUp)
		if state.Focus != filter {
			t.Fatalf("moving up from %s results focused %s", tab, state.Focus)
		}
		tester.Key(0, ui.KeyLeft)
		if catalog.Snapshot().Phase != app.CatalogLoading || view.home.filter != "0" || state.Focus != homeFilterID("0") {
			t.Fatalf("moving left did not reload the default %s results", tab)
		}
		session.page <- initial
		(<-completion)()
		tester.Frame()
		tester.Key(0, ui.KeyUp)
	}
}

func TestCatalogHoldMovesAtConstantSpeedUntilRelease(t *testing.T) {
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
	catalog := app.NewCatalog(func(string) provider.Session { return session }, func(fn func()) { completion <- fn }, func() {})
	defer catalog.Close()
	view := newTestView(t, Options{State: state, Catalog: catalog, Pages: pages, Assets: assets, Theme: theme.Default()})
	tester := ui.NewTester(view.Render, 1920, 1080)
	view.SyncEntry()
	feed := provider.Collection{ID: "feed", Title: "Recommendations"}
	for n := range 35 {
		id := fmt.Sprintf("video-%d", n)
		feed.Items = append(feed.Items, provider.MediaItem{Ref: provider.MediaRef{Kind: provider.Video, ID: id}, Title: id})
	}
	session.page <- provider.CollectionPage{Collections: []provider.Collection{feed}}
	(<-completion)()
	tester.Frame()

	tester.Key(0, ui.KeyUp)
	playBounds := view.focus.item(homePlayAction).target
	tester.KeyDown(ui.KeyDown)
	tester.Advance(500 * time.Millisecond)
	if focused := view.focus.item(homePlayAction).target; focused != playBounds {
		t.Fatalf("play button changed bounds when focused: %+v to %+v", playBounds, focused)
	}
	if state.Focus != homePlayAction || view.activeScroll.Y != 0 {
		t.Fatalf("before the repeat, focus is %s and offset is %v", state.Focus, view.activeScroll.Y)
	}
	const holdStep = time.Second / 60
	tester.KeyRepeat(0, ui.KeyDown)
	for n := 1; n <= 40; n++ {
		tester.Advance(holdStep)
		want := float32(float64(n) * holdStep.Seconds() * catalogHoldSpeed)
		if got := view.activeScroll.Y; got < want-0.01 || got > want+0.01 {
			t.Fatalf("frame %d offset = %.2f, want %.2f", n, got, want)
		}
	}
	stopped := view.activeScroll.Y
	tester.KeyUp(ui.KeyDown)
	tester.Advance(500 * time.Millisecond)
	if view.home.hold.key != ui.KeyUnknown || view.activeScroll.Y != stopped {
		t.Fatalf("release left hold active or moved from %.2f to %.2f", stopped, view.activeScroll.Y)
	}
	tester.KeyDown(ui.KeyUp)
	tester.Advance(500 * time.Millisecond)
	start := view.activeScroll.Y
	tester.KeyRepeat(0, ui.KeyUp)
	for n := 1; n <= 15; n++ {
		tester.Advance(holdStep)
		want := start - float32(float64(n)*holdStep.Seconds()*catalogHoldSpeed)
		if got := view.activeScroll.Y; got < want-0.01 || got > want+0.01 {
			t.Fatalf("up frame %d offset = %.2f, want %.2f", n, got, want)
		}
	}
	tester.KeyUp(ui.KeyUp)
}

func TestCatalogBuildsTheViewportWithOneRowOfOverscan(t *testing.T) {
	for _, sample := range []struct {
		viewport float32
		want     int
	}{{103 + 3*356, 3}, {103 + 3*356 + 1, 4}, {1920 - collapsedRailWidth, 5}, {2240, 7}} {
		grid := homeGridMetrics(sample.viewport)
		if grid.columns != sample.want || grid.width(sample.viewport) != homeShelfCoverWidth {
			t.Fatalf("viewport %v has %d cards of width %v", sample.viewport, grid.columns, grid.width(sample.viewport))
		}
	}
	section := catalogSection{top: 1750, grid: homeGridMetrics(1920 - collapsedRailWidth)}
	if section.grid.width(1920-collapsedRailWidth) != homeShelfCardSize.width ||
		section.grid.cover != homeShelfCardSize.cover || section.grid.gap != homeShelfGap {
		t.Fatal("home cards changed dimensions between a shelf and the recommendation grid")
	}
	start := section.top + section.grid.heading
	for _, total := range []int{100, 1000, 10000} {
		first, end := section.visibleRows(start+section.grid.stride()*10, 1080, total, 1)
		if first != 9 || end != 15 {
			t.Fatalf("%d slots rendered rows [%d,%d), expected [9,15)", total, first, end)
		}
	}
	for _, input := range []struct{ real, slots int }{{0, 5}, {8, 10}, {10, 15}} {
		if got := section.grid.slots(input.real, true); got != input.slots {
			t.Errorf("%d real cards produced %d loading slots, want %d", input.real, got, input.slots)
		}
	}
}

type artworkStubSession struct {
	viewCatalogSession
}

func (artworkStubSession) LoadCatalogArtwork(context.Context, string) (image.Image, error) {
	return image.NewRGBA(image.Rect(0, 0, 2, 2)), nil
}

func TestCatalogBitmapInitializesLazilyAndEvictsLRU(t *testing.T) {
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
	callbacks := make(chan func(), 128)
	catalog := app.NewCatalog(func(string) provider.Session { return artworkStubSession{} }, func(fn func()) { callbacks <- fn }, func() {})
	defer catalog.Close()
	view := newTestView(t, Options{State: state, Catalog: catalog, Pages: pages, Assets: assets, Theme: theme.Default()})
	view.SyncEntry()
	// Confirm home.images is nil before Home renders.
	if view.home.images != nil {
		t.Fatal("expected nil home.images before Home render")
	}
	page := view.context(state.Location, true, 1920, 1080)
	// Request artwork without ever calling catalogPage.Render.
	catalog.PrepareArtwork([]string{"https://img.example/first.jpg"})
	for catalog.Artwork("https://img.example/first.jpg") == nil {
		(<-callbacks)()
	}
	if bmp := page.catalogCover("https://img.example/first.jpg"); bmp == nil {
		t.Fatal("catalogCover returned nil for loaded artwork")
	}
	if len(view.home.images) != 1 {
		t.Fatalf("home.images size = %d, want 1", len(view.home.images))
	}
	// Populate more than maxCatalogBitmaps entries and verify LRU bound holds.
	for i := range maxCatalogBitmaps + 16 {
		url := fmt.Sprintf("https://img.example/%03d.jpg", i)
		view.home.imageClock++
		view.home.images[catalogImageKey{location: url}] = catalogImage{lastUsed: view.home.imageClock}
	}
	page.pruneCatalogBitmaps()
	if len(view.home.images) > maxCatalogBitmaps {
		t.Fatalf("home.images exceeded bound: %d > %d", len(view.home.images), maxCatalogBitmaps)
	}
}

func TestRailLibraryAndSettingsHoverSwitchImmediately(t *testing.T) {
	assets, err := LoadAssets()
	if err != nil {
		t.Fatal(err)
	}
	pages, err := NewPages(assets)
	if err != nil {
		t.Fatal(err)
	}
	entries := testEntries()
	entries[0].Capabilities = provider.Capabilities{
		provider.BrowseCatalog,
		provider.SearchCatalog,
		provider.BrowseFollowing,
		provider.BrowseSeries,
		provider.BrowseLive,
		provider.BrowseLibrary,
		provider.AccountHistory.Capability(),
		provider.AccountWatchLater.Capability(),
		provider.AccountFavorites.Capability(),
		provider.AccountTracking.Capability(),
		provider.AccountCourses.Capability(),
		provider.SignIn,
	}
	state, err := app.NewState(entries, entries[0].ID, pages.Routes)
	if err != nil {
		t.Fatal(err)
	}
	view := newTestView(t, Options{State: state, Pages: pages, Assets: assets, Theme: theme.DefaultLight(), Version: "test"})
	var context *ui.Context
	tester := ui.NewTester(func(c *ui.Context) { context = c; view.Render(c) }, 1920, 1080)
	tester.Frame()

	// 1. Expanding the rail keeps Home active; scrolling Down switches every rail page immediately without Enter.
	tester.Key(0, ui.KeyLeft)
	if state.Page != app.Home || state.Area != app.NavigationArea || ui.FocusedValue(context, &state.Focus) != app.RailFocus(app.Home) {
		t.Fatalf("Left did not expand rail on Home: %+v", state.Location)
	}
	wantRailOrder := []app.Page{
		app.Search, app.Following, app.Series, app.Live, app.Library, app.Entries, app.Settings, app.Account,
	}
	for _, want := range wantRailOrder {
		tester.Key(0, ui.KeyDown)
		if state.Page != want || state.Area != app.NavigationArea || ui.FocusedValue(context, &state.Focus) != app.RailFocus(want) {
			t.Fatalf("Down in rail did not switch immediately to %s while staying in rail: %+v", want, state.Location)
		}
	}

	// 2. Scroll Up to Settings and press Right into the left settings sidebar, then Down switches sections without Enter.
	tester.Key(0, ui.KeyUp)
	if state.Page != app.Settings || state.Area != app.NavigationArea || state.Focus != app.RailFocus(app.Settings) {
		t.Fatalf("Up from Account did not switch to Settings in rail: %+v", state.Location)
	}
	tester.Key(0, ui.KeyRight)
	if state.Page != app.Settings || state.Area != app.ContentArea || state.Focus != app.SectionFocus("playback") {
		t.Fatalf("Right from rail/settings did not enter playback section: %+v", state.Location)
	}
	tester.Advance(menuTransition.Duration + time.Millisecond)
	settingsTitleBefore, ok := tester.Find(assets.Text.SettingsTitle)
	if !ok {
		t.Fatal("settings title missing")
	}
	for _, wantSection := range []string{"danmaku", "appearance", "privacy", "plugins", "about"} {
		tester.Key(0, ui.KeyDown)
		tester.Advance(menuTransition.Duration / 4)
		settingsTitleAfter, ok := tester.Find(assets.Text.SettingsTitle)
		if !ok || math.Abs(float64(settingsTitleAfter.Y-settingsTitleBefore.Y)) > 0.01 {
			t.Fatalf("switching settings section %s remounted or shifted the outer page: before=%+v after=%+v", wantSection, settingsTitleBefore, settingsTitleAfter)
		}
		if state.Page != app.Settings || state.Section != wantSection || state.Focus != app.SectionFocus(wantSection) {
			t.Fatalf("Down in settings sidebar did not switch immediately to %s: %+v", wantSection, state.Location)
		}
	}
	tester.Key(0, ui.KeyUp)
	tester.Key(0, ui.KeyUp)
	tester.Key(0, ui.KeyUp)
	if state.Section != "appearance" || state.Focus != app.SectionFocus("appearance") {
		t.Fatalf("Up in settings sidebar did not return to appearance: %+v", state.Location)
	}
	tester.Key(0, ui.KeyRight)
	if state.Focus != appearanceLanguageRow {
		t.Fatalf("Right from appearance sidebar focused %q, want %q", state.Focus, appearanceLanguageRow)
	}

	// 3. Return to rail, hover Up to Library, enter Library tabs, and verify Left/Right switch tabs immediately without Enter.
	tester.Key(0, ui.KeyLeft)
	tester.Key(0, ui.KeyLeft)
	if state.Area != app.NavigationArea || state.Focus != app.RailFocus(app.Settings) {
		t.Fatalf("Left did not return to rail/settings: %+v", state.Location)
	}
	tester.Key(0, ui.KeyUp) // Entries
	tester.Key(0, ui.KeyUp) // Library
	if state.Page != app.Library || state.Area != app.NavigationArea || state.Focus != app.RailFocus(app.Library) {
		t.Fatalf("Up did not hover Library in rail: %+v", state.Location)
	}
	tester.Key(0, ui.KeyRight)
	if state.Page != app.Library || state.Area != app.ContentArea {
		t.Fatalf("Right from rail/library did not enter Library content: %+v", state.Location)
	}
	state.Focus = app.FocusID("library/tab/" + provider.AccountHistory)
	tester.Frame()
	tester.Advance(menuTransition.Duration + time.Millisecond)
	libTabBefore, ok := tester.Find(assets.Text.Activity.LibraryHistory)
	if !ok {
		t.Fatal("library history tab missing")
	}
	wantLibraryTabs := []provider.AccountSection{
		provider.AccountWatchLater, provider.AccountFavorites, provider.AccountTracking, provider.AccountCourses,
	}
	for _, sec := range wantLibraryTabs {
		tester.Key(0, ui.KeyRight)
		tester.Advance(menuTransition.Duration / 4)
		libTabAfter, ok := tester.Find(assets.Text.Activity.LibraryHistory)
		if !ok || math.Abs(float64(libTabAfter.Y-libTabBefore.Y)) > 0.01 {
			t.Fatalf("switching library tab %s remounted or shifted the top tab bar: before=%+v after=%+v", sec, libTabBefore, libTabAfter)
		}
		wantFocus := app.FocusID("library/tab/" + sec)
		if state.Route != libraryRoute(sec) || state.Focus != wantFocus {
			t.Fatalf("Right on library tabs did not switch immediately to %s: route=%+v focus=%q", sec, state.Route, state.Focus)
		}
	}
	for i := len(wantLibraryTabs) - 2; i >= 0; i-- {
		sec := wantLibraryTabs[i]
		tester.Key(0, ui.KeyLeft)
		wantFocus := app.FocusID("library/tab/" + sec)
		if state.Route != libraryRoute(sec) || state.Focus != wantFocus {
			t.Fatalf("Left on library tabs did not switch back immediately to %s: route=%+v focus=%q", sec, state.Route, state.Focus)
		}
	}
	tester.Key(0, ui.KeyLeft)
	if state.Route != libraryRoute(provider.AccountHistory) || state.Focus != app.FocusID("library/tab/"+provider.AccountHistory) {
		t.Fatalf("Left on watch_later tab did not switch immediately back to history: route=%+v focus=%q", state.Route, state.Focus)
	}
}
