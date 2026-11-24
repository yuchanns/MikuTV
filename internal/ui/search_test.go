package tvui

import (
	"context"
	"fmt"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/provider"
	"github.com/yuchanns/MikuTV/internal/theme"
)

type uiSearchSession struct {
	searches chan uiSearchRequest
	suggests chan uiSuggestRequest
	item     provider.MediaItem
}

type uiSearchRequest struct {
	request provider.SearchRequest
	reply   chan provider.SearchPage
	err     chan error
}

type uiSuggestRequest struct {
	query string
	reply chan provider.SearchSuggestResult
}

func (s *uiSearchSession) Browse(context.Context, provider.BrowseRequest) (provider.CollectionPage, error) {
	return provider.CollectionPage{}, nil
}

func (s *uiSearchSession) Item(context.Context, provider.MediaRef) (provider.MediaItem, error) {
	return s.item, nil
}

func (s *uiSearchSession) VideoDetail(context.Context, provider.MediaRef) (provider.MediaItem, error) {
	return s.item, nil
}

func (s *uiSearchSession) Close() {}

func (s *uiSearchSession) Search(ctx context.Context, req provider.SearchRequest) (provider.SearchPage, error) {
	reply := make(chan provider.SearchPage, 1)
	errCh := make(chan error, 1)
	s.searches <- uiSearchRequest{request: req, reply: reply, err: errCh}
	select {
	case page := <-reply:
		return page, nil
	case err := <-errCh:
		return provider.SearchPage{}, err
	case <-ctx.Done():
		return provider.SearchPage{}, ctx.Err()
	}
}

func (s *uiSearchSession) SearchSuggest(ctx context.Context, query string) (provider.SearchSuggestResult, error) {
	reply := make(chan provider.SearchSuggestResult, 1)
	s.suggests <- uiSuggestRequest{query: query, reply: reply}
	select {
	case res := <-reply:
		return res, nil
	case <-ctx.Done():
		return provider.SearchSuggestResult{}, ctx.Err()
	}
}

func TestSearchPageFlowFiltersSuggestionsAndDetailReturnRestore(t *testing.T) {
	assets, err := LoadAssets()
	if err != nil {
		t.Fatal(err)
	}
	pages, err := NewPages(assets)
	if err != nil {
		t.Fatal(err)
	}
	entry := app.Entry{
		ID: "sample-source", SourceID: "sample-source", Name: "SampleSource",
		Capabilities: provider.Capabilities{
			provider.BrowseCatalog, provider.GetCatalogItem,
			provider.SearchCatalog, provider.SearchSuggest,
		},
		Presentation: provider.SourcePresentation{
			Search: provider.SearchPresentation{
				Hint:         "Custom Search Hint",
				CreatorLabel: "UP",
				SeriesLabel:  "Anime & Shows",
			},
		},
	}
	state, err := app.NewState([]app.Entry{entry}, entry.ID, pages.Routes)
	if err != nil {
		t.Fatal(err)
	}
	videoRef := provider.MediaRef{EntryID: entry.ID, Kind: provider.Video, ID: "BVSearch1"}
	session := &uiSearchSession{
		searches: make(chan uiSearchRequest, 8),
		suggests: make(chan uiSuggestRequest, 8),
		item: provider.MediaItem{
			Ref: videoRef, Title: "Cosmos Journey",
			VideoDetails: &provider.VideoDetails{},
		},
	}
	catalogCallbacks := make(chan func(), 32)
	searchCallbacks := make(chan func(), 32)
	runCatalog := func() { (<-catalogCallbacks)() }
	runSearch := func() { (<-searchCallbacks)() }
	catalog := app.NewCatalog(func(string) provider.Session { return session }, func(fn func()) { catalogCallbacks <- fn }, func() {})
	defer catalog.Close()
	search := app.NewCatalogSearch(nil, func(fn func()) { searchCallbacks <- fn }, func() {})
	defer search.Close()

	view := newTestView(t, Options{State: state, Catalog: catalog, Search: search, Pages: pages, Assets: assets, Theme: theme.Default()})
	var frame *ui.Context
	tester := ui.NewTester(func(c *ui.Context) { frame = c; view.Render(c) }, 1920, 1080)
	view.SyncEntry()
	runCatalog()

	// Navigate to Search page.
	view.dispatch(func() { state.Navigate(app.Route{Page: app.Search}) })
	tester.Frame()
	if state.Page != app.Search || ui.FocusedValue(frame, &state.Focus) != searchInputFocus {
		t.Fatalf("entering search focused %s instead of search input", state.Focus)
	}
	if _, ok := tester.Find("Custom Search Hint"); !ok {
		t.Fatal("source presentation search hint was not rendered")
	}
	if _, ok := tester.Find("UP"); !ok {
		t.Fatal("source presentation creator tab label was not rendered")
	}
	if _, ok := tester.Find("Anime & Shows"); !ok {
		t.Fatal("source presentation series tab label was not rendered")
	}

	// Complete initial discovery search.
	initReq := <-session.searches
	initReq.reply <- provider.SearchPage{
		Items:       []provider.MediaItem{{Ref: videoRef, Title: "Cosmos Journey"}},
		HotKeywords: []string{"Cosmos", "Music"},
	}
	runSearch()
	tester.Frame()
	if _, ok := tester.Find(assets.Text.Search.PopularHeading); !ok {
		t.Fatal("popular heading missing on empty query")
	}
	if _, ok := tester.Find("Cosmos"); !ok {
		t.Fatal("hot keyword missing")
	}

	// Activate search box -> native TextInput, cancel with Escape preserves state.
	tester.Key(0, ui.KeyEnter)
	if !view.search.editing {
		t.Fatal("activating search box did not enter text editing mode")
	}
	tester.Key(0, ui.KeyEscape)
	if view.search.editing || state.Focus != searchInputFocus {
		t.Fatalf("escape from search input failed: editing=%v focus=%s", view.search.editing, state.Focus)
	}

	// Move down to first hot keyword and submit it.
	tester.Key(0, ui.KeyDown)
	if state.Focus != searchHotFocus(0) {
		t.Fatalf("down from input focused %s, want %s", state.Focus, searchHotFocus(0))
	}
	tester.Key(0, ui.KeyEnter)
	queryReq := <-session.searches
	suggestReq := <-session.suggests
	if queryReq.request.Query != "Cosmos" || suggestReq.query != "Cosmos" {
		t.Fatalf("unexpected search/suggest requests: %+v / %q", queryReq.request, suggestReq.query)
	}
	total := int64(1)
	queryReq.reply <- provider.SearchPage{
		Items: []provider.MediaItem{{Ref: videoRef, Title: "Cosmos Journey"}},
		Total: &total,
	}
	suggestReq.reply <- provider.SearchSuggestResult{Suggestions: []string{"Cosmos 4K"}}
	runSearch()
	runSearch()
	tester.Frame()
	if _, ok := tester.Find(fmt.Sprintf(assets.Text.Search.ResultsHeading, 1)); !ok {
		t.Fatal("search results count heading missing")
	}
	if _, ok := tester.Find("Cosmos 4K"); !ok {
		t.Fatal("search suggestion row missing")
	}

	// Open Search Filters modal, pick Views + Short, cancel with Escape -> must NOT apply.
	view.dispatch(func() { view.moveFocus(searchFilterFocus, app.Right, false) })
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	if state.Page != app.SearchFilters {
		t.Fatalf("filter button did not open modal: %+v", state.Route)
	}
	tester.Key(0, ui.KeyRight)
	tester.Key(0, ui.KeyRight)
	tester.Key(0, ui.KeyEnter) // Views
	tester.Key(0, ui.KeyEscape)
	if state.Page != app.Search || search.Snapshot().Request.Sort != provider.SearchSortDefault || state.Focus != searchFilterFocus {
		t.Fatalf("canceling filter modal altered sort or lost focus: page=%s sort=%s focus=%s", state.Page, search.Snapshot().Request.Sort, state.Focus)
	}

	// Reopen Filters modal, pick Views + Short, and Apply.
	tester.Key(0, ui.KeyEnter)
	tester.Key(0, ui.KeyRight)
	tester.Key(0, ui.KeyRight)
	tester.Key(0, ui.KeyEnter) // Views
	tester.Key(0, ui.KeyDown)
	tester.Key(0, ui.KeyRight)
	tester.Key(0, ui.KeyEnter) // Short
	tester.Key(0, ui.KeyDown)
	if state.Focus != searchFilterApply {
		t.Fatalf("down from duration did not reach apply button: %s", state.Focus)
	}
	tester.Key(0, ui.KeyEnter)
	filterReq := <-session.searches
	if state.Page != app.Search || filterReq.request.Sort != provider.SearchSortViews || filterReq.request.Duration != provider.SearchDurationShort {
		t.Fatalf("apply filters failed: page=%s req=%+v", state.Page, filterReq.request)
	}
	filterReq.reply <- provider.SearchPage{
		Items: []provider.MediaItem{{Ref: videoRef, Title: "Cosmos Journey"}},
		Total: &total,
	}
	runSearch()
	tester.Frame()

	// Navigate to the result card, open Video Detail, and press Back -> restores Search query, filters & card focus!
	cardFocus := searchItemFocus(videoRef)
	view.dispatch(func() { view.moveFocus(cardFocus, app.Down, false) })
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	if state.Page != app.VideoDetail {
		t.Fatalf("clicking video result did not open VideoDetail: %s", state.Page)
	}
	runCatalog()
	tester.Frame()
	tester.Key(0, ui.KeyEscape)
	if state.Page != app.Search || state.Focus != cardFocus || search.Snapshot().Request.Query != "Cosmos" ||
		search.Snapshot().Request.Sort != provider.SearchSortViews {
		t.Fatalf("Back from VideoDetail did not restore Search state: page=%s focus=%s req=%+v", state.Page, state.Focus, search.Snapshot().Request)
	}

	// Empty result state renders cleanly.
	search.Submit("nothing-matches")
	emptyReq := <-session.searches
	emptySug := <-session.suggests
	zero := int64(0)
	emptyReq.reply <- provider.SearchPage{Total: &zero}
	emptySug.reply <- provider.SearchSuggestResult{}
	runSearch()
	runSearch()
	tester.Frame()
	if _, ok := tester.Find(assets.Text.Search.NoResults); !ok {
		t.Fatal("empty search results state did not render")
	}

	// Switch to Creator tab renders creator cards.
	search.SelectKind(provider.SearchCreator)
	creatorReq := <-session.searches
	followers := int64(39000)
	videos := int64(12)
	creatorReq.reply <- provider.SearchPage{
		Creators: []provider.SearchCreatorItem{{
			Creator:   provider.Creator{ID: "39", Name: "Miku Channel"},
			Followers: &followers, Videos: &videos,
		}},
		Total: &total,
	}
	runSearch()
	tester.Frame()
	if _, ok := tester.Find("Miku Channel"); !ok {
		t.Fatal("creator search card did not render")
	}
}

func TestNeutralSourceSearchUsesNeutralLabelsWithoutSuggestionsCapability(t *testing.T) {
	assets, err := LoadAssets()
	if err != nil {
		t.Fatal(err)
	}
	pages, err := NewPages(assets)
	if err != nil {
		t.Fatal(err)
	}
	entry := app.Entry{
		ID: "neutral", SourceID: "neutral", Name: "Neutral",
		Capabilities: provider.Capabilities{provider.BrowseCatalog, provider.SearchCatalog},
	}
	state, err := app.NewState([]app.Entry{entry}, entry.ID, pages.Routes)
	if err != nil {
		t.Fatal(err)
	}
	session := &uiSearchSession{searches: make(chan uiSearchRequest, 4), suggests: make(chan uiSuggestRequest, 4)}
	callbacks := make(chan func(), 16)
	catalog := app.NewCatalog(func(string) provider.Session { return session }, func(fn func()) { callbacks <- fn }, func() {})
	defer catalog.Close()
	search := app.NewCatalogSearch(nil, func(fn func()) { callbacks <- fn }, func() {})
	defer search.Close()
	view := newTestView(t, Options{State: state, Catalog: catalog, Search: search, Pages: pages, Assets: assets, Theme: theme.Default()})
	tester := ui.NewTester(view.Render, 1920, 1080)
	view.SyncEntry()
	view.dispatch(func() { state.Navigate(app.Route{Page: app.Search}) })
	tester.Frame()
	if _, ok := tester.Find(assets.Text.Search.Hint); !ok {
		t.Fatal("neutral search hint missing")
	}
	if _, ok := tester.Find(assets.Text.Search.KindCreator); !ok {
		t.Fatal("neutral creator tab label missing")
	}
	if _, ok := tester.Find("UP"); ok {
		t.Fatal("source-specific creator label leaked to neutral source")
	}
	if search.CanSuggest() {
		t.Fatal("source without catalog.searchSuggest should not enable suggestions")
	}
}
