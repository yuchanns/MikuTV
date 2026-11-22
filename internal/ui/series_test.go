package tvui

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/player"
	"github.com/yuchanns/MikuTV/internal/provider"
	"github.com/yuchanns/MikuTV/internal/theme"
)

type uiSeriesSession struct {
	uiAuthSource
	mu          sync.Mutex
	calls       []provider.SeriesRequest
	playCalls   []provider.PlaybackRequest
	trackCalls  []provider.SeriesTrackRequest
	detail      map[string]provider.MediaItem
	plans       map[string]provider.PlaybackPlan
	hub         provider.SeriesPage
	index       provider.SeriesPage
	calendar    provider.SeriesPage
}

func (s *uiSeriesSession) Browse(context.Context, provider.BrowseRequest) (provider.CollectionPage, error) {
	return provider.CollectionPage{}, nil
}

func (s *uiSeriesSession) Item(_ context.Context, ref provider.MediaRef) (provider.MediaItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if item, ok := s.detail[ref.ID]; ok {
		if item.VideoDetails != nil {
			details := *item.VideoDetails
			item.VideoDetails = &details
		}
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

func (s *uiSeriesSession) ResolvePlayback(_ context.Context, req provider.PlaybackRequest) (provider.Playback, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.playCalls = append(s.playCalls, req)
	key := req.Ref.ID + ":" + req.PartID
	plan, ok := s.plans[key]
	if !ok {
		plan, ok = s.plans[req.Ref.ID]
	}
	if !ok {
		return provider.Playback{}, &provider.Error{Code: provider.Expired}
	}
	return provider.Playback{
		Plan:      plan,
		Transport: fakeMediaTransport{},
	}, nil
}

type fakeMediaTransport struct{}

func (fakeMediaTransport) OpenResource(_ context.Context, _ provider.PlaybackResource, _, _ string) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: http.NoBody}, nil
}

func (s *uiSeriesSession) TrackSeries(_ context.Context, req provider.SeriesTrackRequest) (provider.SeriesTrackResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.trackCalls = append(s.trackCalls, req)
	if item, ok := s.detail[req.Ref.ID]; ok && item.VideoDetails != nil {
		details := *item.VideoDetails
		details.Engagement.Tracked = req.Enabled
		item.VideoDetails = &details
		s.detail[req.Ref.ID] = item
	}
	return provider.SeriesTrackResult{Tracked: req.Enabled}, nil
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

func TestSeriesPlaybackHeroEpisodesUpcomingAndAccessStates(t *testing.T) {
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
			provider.BrowseCatalog, provider.GetCatalogItem, provider.BrowseSeries, provider.ResolvePlayback,
		},
	}
	state, err := app.NewState([]app.Entry{entry}, entry.ID, pages.Routes)
	if err != nil {
		t.Fatal(err)
	}

	ref := provider.MediaRef{EntryID: entry.ID, Kind: provider.Series, ID: "ss47836"}
	dur1, dur2 := int64(1440), int64(1450)
	segments := []provider.VideoSegment{
		{Title: "Opening", PartID: "cid-1", DurationSeconds: &dur1},
		{Title: "Training", PartID: "cid-2", DurationSeconds: &dur2, Status: "VIP"},
		{Title: "EP 3", Unavailable: true, Status: "Upcoming"},
	}
	directStream := provider.PlaybackStream{
		Format: provider.PlaybackDirect,
		Video:  provider.PlaybackTrack{Resource: provider.PlaybackResource{URLs: []string{"https://example.test/ep.mp4"}}, ContentType: "video/mp4"},
	}
	hero := provider.SeriesEntry{
		Item: provider.MediaItem{Ref: ref, Title: "Demon Blade Arc", Category: "Anime"},
		Badge: "2 / 3", Score: "9.7", Status: "Updated to EP 2",
	}
	sess := &uiSeriesSession{
		hub: provider.SeriesPage{
			Genres:           []provider.SeriesFilterOption{{Value: "", Label: "All"}},
			Hero:             &hero,
			Items:            []provider.SeriesEntry{hero},
			SupportsIndex:    true,
			SupportsCalendar: true,
		},
		detail: map[string]provider.MediaItem{
			"ss47836": {
				Ref: ref, Title: "Demon Blade Arc", Subtitle: "Full synopsis.", Category: "Anime",
				DurationSeconds: &dur1,
				VideoDetails: &provider.VideoDetails{
					Access:      provider.VideoAccessGranted,
					SegmentKind: provider.VideoParts,
					Segments:    segments,
				},
			},
		},
		plans: map[string]provider.PlaybackPlan{
			"ss47836:": {
				ItemID: "aid-1", PartID: "cid-1", Duration: 1440,
				Access: provider.VideoAccessGranted, Streams: []provider.PlaybackStream{directStream},
				Episodes: segments,
			},
			"ss47836:cid-1": {
				ItemID: "aid-1", PartID: "cid-1", Duration: 1440,
				Access: provider.VideoAccessGranted, Streams: []provider.PlaybackStream{directStream},
				Episodes: segments,
			},
			"ss47836:cid-2": {
				ItemID: "aid-2", PartID: "cid-2", Duration: 180,
				Access: provider.VideoAccessPreview, PreviewRestriction: provider.PlaybackMembership,
				Streams: []provider.PlaybackStream{directStream},
				Episodes: segments,
			},
		},
	}

	queue := make(chan func(), 64)
	post := func(fn func()) { queue <- fn }
	drainOne := func() {
		select {
		case fn := <-queue:
			fn()
		case <-time.After(3 * time.Second):
			t.Fatal("timed out waiting for async delivery")
		}
	}
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

	clock := player.Status{HasFrame: true, Duration: 1440}
	playback := app.NewPlayback(catalog, player.Engine{
		Open:     func(string, float64) bool { return true },
		Snapshot: func() player.Status { return clock },
	}, nil, post, func() {})
	t.Cleanup(playback.Close)

	view := newTestView(t, Options{
		State: state, Catalog: catalog, Series: series,
		Pages: pages, Assets: assets, Theme: theme.Default(),
	})
	view.Playback = playback
	view.SyncEntry()
	drain()

	tester := ui.NewTester(view.Render, 1920, 1080)
	if !state.Navigate(app.Route{Page: app.Series}) {
		t.Fatal("failed to navigate to Series")
	}
	view.SyncEntry()
	for !series.Hub().Loaded {
		drainOne()
	}
	drain()
	tester.Frame()

	// 1. Direct play from Series Hub Hero opens Player and hydrates playable episodes (skipping upcoming EP 3).
	state.Focus = seriesHeroPlayFocus
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	for playback.State().Loading {
		drainOne()
	}
	drain()
	tester.Frame()
	if state.Page != app.Player || len(view.player.items) != 2 || view.player.index != 0 || playback.State().Target.PartID != "cid-1" {
		t.Fatalf("expected Player at ep 1 with 2 playable episodes, got page=%s items=%d idx=%d part=%q",
			state.Page, len(view.player.items), view.player.index, playback.State().Target.PartID)
	}

	// 2. Next switches ep 1 -> ep 2 (VIP preview -> frameless PreviewPending overlay).
	view.startPlaybackItem(1, nil)
	for playback.State().Loading {
		drainOne()
	}
	drain()
	tester.Frame()
	if view.player.index != 1 || !playback.State().PreviewPending || !view.player.accessVisible {
		t.Fatalf("expected ep 2 VIP preview overlay, got idx=%d pending=%t accessVisible=%t",
			view.player.index, playback.State().PreviewPending, view.player.accessVisible)
	}

	// 3. Back from PreviewPending returns cleanly to Series Hero focus.
	tester.Key(0, ui.KeyEscape)
	tester.Frame()
	if state.Page != app.Series || state.Focus != seriesHeroPlayFocus {
		t.Fatalf("expected Back from preview to restore Series Hero play focus, got page=%s focus=%q", state.Page, state.Focus)
	}

	// 4. Open Series Detail -> Episodes tab: clicking upcoming EP 3 shows notice; clicking EP 2 plays directly.
	state.Focus = seriesHeroDetailFocus
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	for view.videoDetail.loading {
		drainOne()
	}
	drain()
	tester.Frame()
	if state.Page != app.VideoDetail || view.videoDetail.item.Ref != ref {
		t.Fatalf("expected VideoDetail for ss47836, got page=%s ref=%+v", state.Page, view.videoDetail.item.Ref)
	}
	view.videoDetail.tab = videoDetailEpisodes
	tester.Frame()

	// Click upcoming EP 3 -> stays on VideoDetail with EpisodeUpcomingNotice feedback toast.
	state.Focus = videoSegmentFocus(2)
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	if state.Page != app.VideoDetail || view.videoDetail.feedback.text != assets.Text.Detail.EpisodeUpcomingNotice {
		t.Fatalf("expected upcoming episode click to show feedback without opening player, got page=%s feedback=%q", state.Page, view.videoDetail.feedback.text)
	}

	// Click playable EP 2 -> sets detail.part=1 and opens Player at cid-2.
	state.Focus = videoSegmentFocus(1)
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	for playback.State().Loading {
		drainOne()
	}
	drain()
	tester.Frame()
	if state.Page != app.Player || view.videoDetail.part != 1 || playback.State().Target.PartID != "cid-2" || !playback.State().PreviewPending {
		t.Fatalf("expected clicking ep 2 on Series detail to play cid-2 preview, got page=%s part=%d targetPart=%q pending=%t",
			state.Page, view.videoDetail.part, playback.State().Target.PartID, playback.State().PreviewPending)
	}
}

func TestSeriesTrackingSyncAcrossHubCalendarDetailAndFeedback(t *testing.T) {
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
		Presentation: provider.SourcePresentation{
			Series: provider.SeriesPresentation{TrackLabel: "追番", TrackedLabel: "已追番"},
		},
		Capabilities: provider.Capabilities{
			provider.BrowseCatalog, provider.GetCatalogItem, provider.BrowseSeries, provider.TrackSeries, provider.SignIn,
		},
	}
	state, err := app.NewState([]app.Entry{entry}, entry.ID, pages.Routes)
	if err != nil {
		t.Fatal(err)
	}

	ref := provider.MediaRef{EntryID: entry.ID, Kind: provider.Series, ID: "ss100"}
	hero := provider.SeriesEntry{Item: provider.MediaItem{Ref: ref, Title: "Mountains & Seas", Category: "Doc"}, Tracked: false}
	sess := &uiSeriesSession{
		hub: provider.SeriesPage{
			Genres:           []provider.SeriesFilterOption{{Value: "", Label: "All"}},
			Hero:             &hero,
			Items:            []provider.SeriesEntry{hero},
			SupportsIndex:    true,
			SupportsCalendar: true,
		},
		calendar: provider.SeriesPage{
			Days: []provider.SeriesCalendarDay{
				{Date: "11-22", Month: 11, Day: 22, Weekday: 0, IsToday: true, Entries: []provider.SeriesScheduleEntry{{Time: "18:00", Entry: hero}}},
			},
		},
		detail: map[string]provider.MediaItem{
			"ss100": {
				Ref: ref, Title: "Mountains & Seas", Subtitle: "Synopsis",
				VideoDetails: &provider.VideoDetails{Engagement: provider.VideoEngagement{Tracked: false}},
			},
		},
	}

	queue := make(chan func(), 64)
	post := func(fn func()) { queue <- fn }
	drainOne := func() {
		select {
		case fn := <-queue:
			fn()
		case <-time.After(3 * time.Second):
			t.Fatal("timed out waiting for async delivery")
		}
	}
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

	store := uiTestKeychain{data: []byte(`{"selected":"u1","accounts":[{"account":{"id":"u1","name":"Tester"},"data":"e30="}]}`)}
	auth := app.NewAuth(store, post, func() {}, func() {}, func() {})
	t.Cleanup(auth.Close)

	view := newTestView(t, Options{
		State: state, Catalog: catalog, Series: series, Auth: auth,
		Pages: pages, Assets: assets, Theme: theme.Default(),
	})
	view.SyncEntry()
	for auth.Accounts().CurrentID != "u1" {
		drainOne()
	}
	drain()

	tester := ui.NewTester(view.Render, 1920, 1080)
	if !state.Navigate(app.Route{Page: app.Series}) {
		t.Fatal("failed to navigate to Series")
	}
	view.SyncEntry()
	for !series.Hub().Loaded {
		drainOne()
	}
	series.EnsureCalendar()
	for !series.Calendar().Loaded {
		drainOne()
	}
	drain()
	tester.Frame()

	// 1. Track from Series Hub Hero -> syncs Hub Hero, Tracked shelf, Calendar, and displays source-specific feedback!
	state.Focus = seriesHeroTrackFocus
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	drainOne()
	drain()
	tester.Frame()
	if !series.Hub().Hero.Tracked || len(series.Hub().Tracked) != 1 || !series.Calendar().Days[0].Entries[0].Entry.Tracked {
		t.Fatalf("Hero track did not sync across Hub/Tracked/Calendar: hub=%+v cal=%+v", series.Hub(), series.Calendar())
	}
	if view.series.feedback.text != "已追番" {
		t.Fatalf("expected source-specific tracked feedback, got %q", view.series.feedback.text)
	}

	// 2. Open Series Detail -> Hero track button reflects Tracked=true -> click untracks and syncs back to Hub & Calendar!
	state.Focus = seriesHeroDetailFocus
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	for view.videoDetail.loading {
		drainOne()
	}
	drain()
	tester.Frame()
	if !view.videoDetail.item.VideoDetails.Engagement.Tracked {
		t.Fatalf("expected detail to load Tracked=true, got %+v", view.videoDetail.item.VideoDetails)
	}
	state.Focus = videoDetailTrack
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	drainOne()
	drain()
	tester.Frame()
	if view.videoDetail.item.VideoDetails.Engagement.Tracked || series.Hub().Hero.Tracked || len(series.Hub().Tracked) != 0 || series.Calendar().Days[0].Entries[0].Entry.Tracked {
		t.Fatalf("Detail untrack did not sync across Detail/Hub/Tracked/Calendar")
	}
	if view.videoDetail.feedback.text != "已取消追番" {
		t.Fatalf("expected source-specific untracked feedback, got %q", view.videoDetail.feedback.text)
	}
}
