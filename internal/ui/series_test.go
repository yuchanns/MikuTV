package tvui

import (
	"context"
	"sync"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/provider"
	"github.com/yuchanns/MikuTV/internal/theme"
)

type uiSeriesSession struct {
	mu       sync.Mutex
	calls    []provider.SeriesRequest
	detail   map[string]provider.MediaItem
	hub      provider.SeriesPage
	index    provider.SeriesPage
	calendar provider.SeriesPage
}

func (s *uiSeriesSession) Browse(context.Context, provider.BrowseRequest) (provider.CollectionPage, error) {
	return provider.CollectionPage{}, nil
}

func (s *uiSeriesSession) Item(_ context.Context, ref provider.MediaRef) (provider.MediaItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if item, ok := s.detail[ref.ID]; ok {
		return item, nil
	}
	return provider.MediaItem{}, &provider.Error{Code: provider.Expired}
}

func (s *uiSeriesSession) VideoDetail(ctx context.Context, ref provider.MediaRef) (provider.MediaItem, error) {
	return s.Item(ctx, ref)
}

func (s *uiSeriesSession) ReadSeries(_ context.Context, req provider.SeriesRequest) (provider.SeriesPage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, req)
	switch req.Mode {
	case provider.SeriesModeIndex:
		return s.index, nil
	case provider.SeriesModeCalendar:
		return s.calendar, nil
	default:
		return s.hub, nil
	}
}

func (s *uiSeriesSession) Close() {}

func TestSeriesHubIndexFilterModalCalendarAndDetailNavigation(t *testing.T) {
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
			provider.BrowseCatalog, provider.GetCatalogItem, provider.BrowseSeries,
		},
	}
	state, err := app.NewState([]app.Entry{entry}, entry.ID, pages.Routes)
	if err != nil {
		t.Fatal(err)
	}

	total := int64(2)
	ref1 := provider.MediaRef{EntryID: entry.ID, Kind: provider.Series, ID: "ss100"}
	ref2 := provider.MediaRef{EntryID: entry.ID, Kind: provider.Series, ID: "ss200"}
	entry1 := provider.SeriesEntry{Item: provider.MediaItem{Ref: ref1, Title: "Mountains & Seas", Category: "Doc"}, Badge: "6 eps", Score: "9.6", Status: "6 eps"}
	entry2 := provider.SeriesEntry{Item: provider.MediaItem{Ref: ref2, Title: "Tomorrow Letter", Category: "Anime"}, Badge: "12 eps", Score: "9.2", Status: "12 eps"}

	sess := &uiSeriesSession{
		hub: provider.SeriesPage{
			Genres:           []provider.SeriesFilterOption{{Value: "", Label: "All"}, {Value: "1", Label: "Anime"}},
			Hero:             &entry1,
			Items:            []provider.SeriesEntry{entry1, entry2},
			SupportsIndex:    true,
			SupportsCalendar: true,
		},
		index: provider.SeriesPage{
			FilterGroups: []provider.SeriesFilterGroup{
				{Key: "season_type", Label: "Type", Options: []provider.SeriesFilterOption{{Value: "1", Label: "Anime"}, {Value: "3", Label: "Doc"}}},
				{Key: "year", Label: "Year", Options: []provider.SeriesFilterOption{{Value: "", Label: "All"}, {Value: "2026", Label: "2026"}}},
			},
			Items: []provider.SeriesEntry{entry1, entry2},
			Total: &total,
		},
		calendar: provider.SeriesPage{
			Days: []provider.SeriesCalendarDay{
				{Date: "11-22", Month: 11, Day: 22, Weekday: 0, IsToday: true, Entries: []provider.SeriesScheduleEntry{{Time: "18:00", EpisodeLabel: "EP 6", Entry: entry1}}},
				{Date: "11-23", Month: 11, Day: 23, Weekday: 1, Entries: nil},
			},
		},
		detail: map[string]provider.MediaItem{
			"ss100": {
				Ref: ref1, Title: "Mountains & Seas", Subtitle: "Full documentary synopsis.",
				VideoDetails: &provider.VideoDetails{
					SegmentKind: provider.VideoCollection,
					Segments:    []provider.VideoSegment{{Title: "EP 1"}},
				},
			},
		},
	}

	queue := make(chan func(), 64)
	post := func(fn func()) { queue <- fn }
	drain := func() {
		for {
			select {
			case fn := <-queue:
				fn()
			default:
				return
			}
		}
	}

	catalog := app.NewCatalog(func(string) provider.Session { return sess }, post, func() {})
	t.Cleanup(catalog.Close)
	series := app.NewSeriesCatalog(post, func() {})
	t.Cleanup(series.Close)

	view := newTestView(t, Options{
		State: state, Catalog: catalog, Series: series,
		Pages: pages, Assets: assets, Theme: theme.Default(),
	})
	view.SyncEntry()
	drain()

	tester := ui.NewTester(view.Render, 1920, 1080)
	if !state.Navigate(app.Route{Page: app.Series}) {
		t.Fatal("failed to navigate to Series")
	}
	view.SyncEntry()
	for !series.Hub().Loaded {
		(<-queue)()
	}
	drain()
	tester.Frame()

	// 1. Open SeriesIndex via tool button.
	state.Focus = seriesToolIndexFocus
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	for !series.Index().Loaded {
		(<-queue)()
	}
	drain()
	tester.Frame()
	if state.Page != app.SeriesIndex {
		t.Fatalf("expected SeriesIndex, got %s", state.Page)
	}

	// 2. Open Year filter modal, choose 2026 -> modal closes and filter applies.
	state.Focus = seriesIndexFilterFocus("year")
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	if state.Page != app.SeriesIndexFilter {
		t.Fatalf("expected SeriesIndexFilter modal, got %s", state.Page)
	}
	tester.Key(0, ui.KeyDown)
	tester.Key(0, ui.KeyEnter)
	for series.Index().Phase == app.CatalogLoading {
		(<-queue)()
	}
	drain()
	tester.Frame()
	if state.Page != app.SeriesIndex || series.Index().Filters["year"] != "2026" {
		t.Fatalf("expected filter year=2026 applied on SeriesIndex, got page=%s filters=%+v", state.Page, series.Index().Filters)
	}

	// 3. Open Series Detail from Index grid card -> Back restores exact card focus!
	cardFocus := seriesIndexCardFocus(ref1)
	state.Focus = cardFocus
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	drain()
	tester.Frame()
	if state.Page != app.VideoDetail || view.videoDetail.seed.Ref != ref1 {
		t.Fatalf("expected VideoDetail for ss100, got page=%s ref=%+v", state.Page, view.videoDetail.seed.Ref)
	}
	tester.Key(0, ui.KeyEscape)
	tester.Frame()
	if state.Page != app.SeriesIndex || state.Focus != cardFocus {
		t.Fatalf("expected Back from Detail to restore SeriesIndex card focus, got page=%s focus=%q", state.Page, state.Focus)
	}

	// 4. Back from SeriesIndex -> returns to Series with tool focus restored!
	tester.Key(0, ui.KeyEscape)
	tester.Frame()
	if state.Page != app.Series || state.Focus != seriesToolIndexFocus {
		t.Fatalf("expected return to Series tool focus, got page=%s focus=%q", state.Page, state.Focus)
	}

	// 5. Open SeriesCalendar -> Left/Right switches day immediately -> open Detail from row -> Back restores!
	state.Focus = seriesToolCalendarFocus
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	for !series.Calendar().Loaded {
		(<-queue)()
	}
	drain()
	tester.Frame()
	if state.Page != app.SeriesCalendar || series.Calendar().Selected != 0 {
		t.Fatalf("expected SeriesCalendar day 0, got page=%s selected=%d", state.Page, series.Calendar().Selected)
	}
	state.Focus = seriesCalendarDayFocus(0)
	tester.Frame()
	tester.Key(0, ui.KeyRight)
	tester.Frame()
	if series.Calendar().Selected != 1 || state.Focus != seriesCalendarDayFocus(1) {
		t.Fatalf("expected Right on calendar strip to select day 1, got selected=%d focus=%q", series.Calendar().Selected, state.Focus)
	}
	tester.Key(0, ui.KeyLeft)
	tester.Frame()
	showFocus := seriesCalendarShowFocus(ref1)
	state.Focus = showFocus
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	drain()
	tester.Frame()
	if state.Page != app.VideoDetail {
		t.Fatalf("expected VideoDetail from calendar row, got %s", state.Page)
	}
	tester.Key(0, ui.KeyEscape)
	tester.Frame()
	if state.Page != app.SeriesCalendar || state.Focus != showFocus {
		t.Fatalf("expected Back from calendar detail to restore row focus, got page=%s focus=%q", state.Page, state.Focus)
	}
}
