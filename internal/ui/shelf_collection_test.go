package tvui

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/player"
	"github.com/yuchanns/MikuTV/internal/provider"
	"github.com/yuchanns/MikuTV/internal/theme"
)

type shelfCollectionTestSession struct {
	mu              sync.Mutex
	owner           string
	browseCalls     int
	followingCalls  int
	seriesHubCalls  int
	liveRoomsCalls  int
	blockSearch     chan struct{}
	blockFollowing  chan struct{}
	blockSeries     chan struct{}
	blockLive       chan struct{}
	blockAccount    chan struct{}
	browseItems     []provider.MediaItem
	browseMore      []provider.MediaItem
	followingUpdate []provider.MediaItem
	followingMore   []provider.MediaItem
	followingLive   []provider.MediaItem
	seriesItems     []provider.SeriesEntry
	seriesMore      []provider.SeriesEntry
	seriesTracked   []provider.SeriesEntry
	liveRooms       []provider.MediaItem
	liveMore        []provider.MediaItem
	liveFollowing   []provider.MediaItem
}

func (s *shelfCollectionTestSession) Close() {}

func (s *shelfCollectionTestSession) Browse(_ context.Context, req provider.BrowseRequest) (provider.CollectionPage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.browseCalls++
	if req.Cursor == "p2" {
		return provider.CollectionPage{
			Collections: []provider.Collection{{ID: "picks", Title: "Picks", Items: s.browseMore}},
		}, nil
	}
	return provider.CollectionPage{
		Collections: []provider.Collection{{ID: "picks", Title: "Picks", Items: s.browseItems}},
		NextCursor:  "p2",
	}, nil
}

func (s *shelfCollectionTestSession) Item(_ context.Context, ref provider.MediaRef) (provider.MediaItem, error) {
	return provider.MediaItem{Ref: ref, Title: "Detail " + ref.ID}, nil
}

func (s *shelfCollectionTestSession) VideoDetail(ctx context.Context, ref provider.MediaRef) (provider.MediaItem, error) {
	return s.Item(ctx, ref)
}

func (s *shelfCollectionTestSession) Search(ctx context.Context, _ provider.SearchRequest) (provider.SearchPage, error) {
	if s.blockSearch != nil {
		select {
		case <-s.blockSearch:
		case <-ctx.Done():
			return provider.SearchPage{}, ctx.Err()
		}
	}
	return provider.SearchPage{}, nil
}

func (s *shelfCollectionTestSession) ReadFollowing(ctx context.Context, req provider.FollowingRequest) (provider.FollowingPage, error) {
	if s.blockFollowing != nil {
		select {
		case <-s.blockFollowing:
		case <-ctx.Done():
			return provider.FollowingPage{}, ctx.Err()
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.followingCalls++
	if req.Section == provider.FollowingUpdates && req.Cursor == "u2" {
		return provider.FollowingPage{Updates: s.followingMore, SupportsLive: true}, nil
	}
	return provider.FollowingPage{
		Creators:      []provider.FollowingCreator{{Creator: provider.Creator{ID: "c1", Name: "Creator One"}}},
		Updates:       s.followingUpdate,
		UpdatesCursor: "u2",
		LiveRooms:     s.followingLive,
		SupportsLive:  true,
	}, nil
}

func (s *shelfCollectionTestSession) ReadSeries(ctx context.Context, req provider.SeriesRequest) (provider.SeriesPage, error) {
	if s.blockSeries != nil {
		select {
		case <-s.blockSeries:
		case <-ctx.Done():
			return provider.SeriesPage{}, ctx.Err()
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seriesHubCalls++
	if req.Cursor == "s2" {
		return provider.SeriesPage{
			Genres:           []provider.SeriesFilterOption{{Value: "", Label: "All"}},
			Items:            s.seriesMore,
			SupportsIndex:    true,
			SupportsCalendar: true,
		}, nil
	}
	hero := s.seriesItems[0]
	return provider.SeriesPage{
		Genres:           []provider.SeriesFilterOption{{Value: "", Label: "All"}},
		Hero:             &hero,
		Items:            s.seriesItems,
		Tracked:          s.seriesTracked,
		NextCursor:       "s2",
		SupportsIndex:    true,
		SupportsCalendar: true,
	}, nil
}

func (s *shelfCollectionTestSession) ReadLive(ctx context.Context, req provider.LiveRequest) (provider.LivePage, error) {
	if s.blockLive != nil {
		select {
		case <-s.blockLive:
		case <-ctx.Done():
			return provider.LivePage{}, ctx.Err()
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.liveRoomsCalls++
	if req.Section == provider.LiveSectionRooms && req.Cursor == "l2" {
		return provider.LivePage{Rooms: s.liveMore}, nil
	}
	hero := s.liveRooms[0]
	return provider.LivePage{
		Areas:          []provider.LiveArea{{ID: "", Label: "All"}},
		Hero:           &hero,
		Rooms:          s.liveRooms,
		NextCursor:     "l2",
		FollowingRooms: s.liveFollowing,
	}, nil
}

func (s *shelfCollectionTestSession) ResolvePlayback(_ context.Context, req provider.PlaybackRequest) (provider.Playback, error) {
	return provider.Playback{
		Plan: provider.PlaybackPlan{Live: true, ItemID: req.Ref.ID, PartID: req.Ref.ID},
	}, nil
}

func (s *shelfCollectionTestSession) ReadAccount(ctx context.Context, _ provider.AccountRequest) (provider.AccountPage, error) {
	if s.blockAccount != nil {
		select {
		case <-s.blockAccount:
		case <-ctx.Done():
			return provider.AccountPage{}, ctx.Err()
		}
	}
	return provider.AccountPage{}, nil
}

func (s *shelfCollectionTestSession) BeginSignIn(context.Context) (provider.QRAttempt, error) {
	return nil, &provider.Error{Code: provider.Unsupported}
}
func (s *shelfCollectionTestSession) RestoreAccount(_ context.Context, cred provider.Credential) (provider.Credential, error) {
	return cred, nil
}
func (s *shelfCollectionTestSession) SetAccount(cred *provider.Credential) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cred == nil {
		s.owner = ""
	} else {
		s.owner = cred.Account.ID
	}
	return nil
}
func (s *shelfCollectionTestSession) SignOut(context.Context, provider.Credential) error { return nil }

func makeTestVideos(entryID, prefix string, n int) []provider.MediaItem {
	items := make([]provider.MediaItem, n)
	for i := range n {
		dur := int64(600 + i*30)
		views := int64(10000 + i*500)
		items[i] = provider.MediaItem{
			Ref:             provider.MediaRef{EntryID: entryID, Kind: provider.Video, ID: fmt.Sprintf("%s-%d", prefix, i+1)},
			Title:           fmt.Sprintf("Video %s %d", prefix, i+1),
			Quality:         "4K",
			DurationSeconds: &dur,
			Views:           &views,
			Creator:         &provider.Creator{ID: "c1", Name: "Creator One"},
		}
	}
	return items
}

func makeTestLiveRooms(entryID, prefix string, n int) []provider.MediaItem {
	items := make([]provider.MediaItem, n)
	for i := range n {
		views := int64(5000 + i*200)
		items[i] = provider.MediaItem{
			Ref:      provider.MediaRef{EntryID: entryID, Kind: provider.Live, ID: fmt.Sprintf("%s-%d", prefix, i+1)},
			Title:    fmt.Sprintf("Live %s %d", prefix, i+1),
			Category: "Music",
			Quality:  "LIVE",
			Views:    &views,
			Creator:  &provider.Creator{ID: fmt.Sprintf("host-%d", i+1), Name: fmt.Sprintf("Host %d", i+1)},
		}
	}
	return items
}

func makeTestSeriesEntries(entryID, prefix string, n int) []provider.SeriesEntry {
	items := make([]provider.SeriesEntry, n)
	for i := range n {
		items[i] = provider.SeriesEntry{
			Item: provider.MediaItem{
				Ref:      provider.MediaRef{EntryID: entryID, Kind: provider.Series, ID: fmt.Sprintf("ss%s%d", prefix, i+1)},
				Title:    fmt.Sprintf("Show %s %d", prefix, i+1),
				Category: "Anime",
			},
			Badge:  "VIP",
			Score:  "9.6",
			Status: "12 eps",
		}
	}
	return items
}

func TestShelfCollectionsFullScreenGridNavigationAndReturnRestore(t *testing.T) {
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
			provider.BrowseCatalog, provider.GetCatalogItem, provider.ResolvePlayback,
			provider.BrowseFollowing, provider.BrowseSeries, provider.BrowseLive,
			provider.SignIn,
		},
	}
	state, err := app.NewState([]app.Entry{entry}, entry.ID, pages.Routes)
	if err != nil {
		t.Fatal(err)
	}
	sess := &shelfCollectionTestSession{
		browseItems:     makeTestVideos(entry.ID, "home", 8),
		browseMore:      makeTestVideos(entry.ID, "home-more", 4),
		followingUpdate: makeTestVideos(entry.ID, "upd", 8),
		followingMore:   makeTestVideos(entry.ID, "upd-more", 4),
		followingLive:   makeTestLiveRooms(entry.ID, "flive", 5),
		seriesItems:     makeTestSeriesEntries(entry.ID, "10", 10),
		seriesMore:      makeTestSeriesEntries(entry.ID, "20", 5),
		seriesTracked:   makeTestSeriesEntries(entry.ID, "30", 5),
		liveRooms:       makeTestLiveRooms(entry.ID, "room", 8),
		liveMore:        makeTestLiveRooms(entry.ID, "room-more", 4),
		liveFollowing:   makeTestLiveRooms(entry.ID, "lfol", 4),
	}

	var mu sync.Mutex
	var queued []func()
	post := func(fn func()) {
		mu.Lock()
		queued = append(queued, fn)
		mu.Unlock()
	}
	drain := func() {
		for {
			mu.Lock()
			if len(queued) == 0 {
				mu.Unlock()
				return
			}
			batch := queued
			queued = nil
			mu.Unlock()
			for _, fn := range batch {
				fn()
			}
		}
	}

	catalog := app.NewCatalog(func(string) provider.Session { return sess }, post, func() {})
	t.Cleanup(catalog.Close)
	creds := newMemoryCreds()
	_ = creds.Save(entry.SourceID, entry.ID, []byte(`{"selected":"u1","accounts":[{"account":{"id":"u1","name":"User"},"data":{"csrf":"x"}}]}`))
	var view *View
	auth := app.NewAuth(creds, post, func() {}, func() {
		if view != nil {
			view.SyncEntry()
		}
	}, func() {})
	t.Cleanup(auth.Close)
	following := app.NewFollowingFeed(post, func() {})
	t.Cleanup(following.Close)
	series := app.NewSeriesCatalog(post, func() {})
	t.Cleanup(series.Close)
	live := app.NewLiveCatalog(post, func() {})
	t.Cleanup(live.Close)

	view = newTestView(t, Options{
		State: state, Catalog: catalog, Auth: auth,
		Following: following, Series: series, Live: live,
		Pages: pages, Assets: assets, Theme: theme.Default(),
	})
	var clock player.Status
	view.Playback = app.NewPlayback(catalog, player.Engine{
		Open:     func(string, float64) bool { clock = player.Status{HasFrame: true}; return true },
		SetRate:  func(float64) {},
		Stop:     func() {},
		Snapshot: func() player.Status { return clock },
	}, nil, post, func() {})
	t.Cleanup(view.Playback.Close)
	view.SyncEntry()
	tester := ui.NewTester(view.Render, 1920, 1080)

	waitUntil := func(cond func() bool) {
		t.Helper()
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			drain()
			view.SyncAccountActivity()
			tester.Frame()
			if cond() {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatalf("timed out waiting for condition: catalogPhase=%v catalogErr=%v authReady=%v authCur=%q browseCalls=%d",
			catalog.Snapshot().Phase, catalog.Snapshot().Error, auth.Accounts().Ready, auth.Accounts().CurrentID, sess.browseCalls)
	}

	// 1. Home "值得在大屏上看" -> Full-screen 4-column grid with rail, initial focus on card 0, Up to Back, opens detail and returns.
	waitUntil(func() bool {
		return catalog.Snapshot().Phase == app.CatalogReady && auth.Accounts().CurrentID == "u1"
	})
	state.Focus = app.FocusID("home/shelf/picks")
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	if state.Page != app.HomeAction {
		t.Fatalf("expected HomeAction, got %s", state.Page)
	}
	if len(state.Layers()) != 1 {
		t.Fatalf("expected non-modal single layer for HomeAction, got %d layers", len(state.Layers()))
	}
	firstHome := catalogCardFocus("shelf-results", sess.browseItems[0].Ref)
	if state.Focus != firstHome {
		t.Fatalf("expected initial focus on first card %s, got %s", firstHome, state.Focus)
	}
	if view.focus.item(homeActionBackFocus) == nil {
		t.Fatal("expected back button on HomeAction")
	}
	// Verify 4-column grid geometry: card 0 and card 4 share X, card 1 is to the right of card 0.
	c0 := view.focus.item(firstHome)
	c1 := view.focus.item(catalogCardFocus("shelf-results", sess.browseItems[1].Ref))
	c4 := view.focus.item(catalogCardFocus("shelf-results", sess.browseItems[4].Ref))
	if c0 == nil || c1 == nil || c4 == nil {
		t.Fatal("expected cards 0, 1, 4 rendered in 4-column grid")
	}
	if c1.target.X <= c0.target.X || c4.target.Y <= c0.target.Y {
		t.Fatalf("expected 4-col grid layout, got c0=(%.0f,%.0f) c1=(%.0f,%.0f) c4=(%.0f,%.0f)",
			c0.target.X, c0.target.Y, c1.target.X, c1.target.Y, c4.target.X, c4.target.Y)
	}
	// Open detail from card 1 and return -> restores HomeAction + card 1 focus.
	tester.Key(0, ui.KeyRight)
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	if state.Page != app.VideoDetail {
		t.Fatalf("expected VideoDetail, got %s", state.Page)
	}
	tester.Key(0, ui.KeyEscape)
	tester.Frame()
	if state.Page != app.HomeAction || state.Focus != catalogCardFocus("shelf-results", sess.browseItems[1].Ref) {
		t.Fatalf("expected return to HomeAction card 1, got page=%s focus=%s", state.Page, state.Focus)
	}
	// Up -> Back button -> Enter -> returns to Home with focus on SeeAll button.
	tester.Key(0, ui.KeyUp)
	tester.Frame()
	if state.Focus != homeActionBackFocus {
		t.Fatalf("expected Up from row 0 to focus back button, got %s", state.Focus)
	}
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	if state.Page != app.Home || state.Focus != app.FocusID("home/shelf/picks") {
		t.Fatalf("expected return to Home picks SeeAll, got page=%s focus=%s", state.Page, state.Focus)
	}

	// 2. Following Live "你关注的直播间" -> Full-screen grid -> opens live Player -> Back preserves Live section.
	state.Navigate(app.Route{Page: app.Following})
	view.SyncAccountActivity()
	waitUntil(func() bool { return following.Snapshot().Loaded })
	state.Focus = followingLiveAll
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	if state.Page != app.FollowingAll || view.following.allSection != followingAllLive {
		t.Fatalf("expected FollowingAll live section, got page=%s section=%s", state.Page, view.following.allSection)
	}
	firstFLive := followingAllItemFocus(sess.followingLive[0].Ref)
	if state.Focus != firstFLive {
		t.Fatalf("expected initial focus on first following live room, got %s", state.Focus)
	}
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	if state.Page != app.Player {
		t.Fatalf("expected Player from following live room, got %s", state.Page)
	}
	tester.Key(0, ui.KeyEscape)
	tester.Frame()
	if state.Page != app.FollowingAll || view.following.allSection != followingAllLive {
		t.Fatalf("expected Back from Player to preserve FollowingAll live section, got page=%s section=%s",
			state.Page, view.following.allSection)
	}
	tester.Key(0, ui.KeyEscape)
	tester.Frame()
	if state.Page != app.Following {
		t.Fatalf("expected Back to Following, got %s", state.Page)
	}

	// 3. Series "精选影视" -> Full-screen 5-column vertical poster grid.
	state.Navigate(app.Route{Page: app.Series})
	view.SyncAccountActivity()
	waitUntil(func() bool { return series.Hub().Loaded })
	state.Focus = seriesPicksAllFocus
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	if state.Page != app.SeriesAll || len(state.Layers()) != 1 {
		t.Fatalf("expected non-modal SeriesAll, got page=%s layers=%d", state.Page, len(state.Layers()))
	}
	s0 := view.focus.item(seriesAllItemFocus(sess.seriesItems[0].Item.Ref))
	s4 := view.focus.item(seriesAllItemFocus(sess.seriesItems[4].Item.Ref))
	s5 := view.focus.item(seriesAllItemFocus(sess.seriesItems[5].Item.Ref))
	if s0 == nil || s4 == nil || s5 == nil {
		t.Fatal("expected series cards 0, 4, 5 in 5-column poster grid")
	}
	if s4.target.Y != s0.target.Y || s5.target.Y <= s0.target.Y {
		t.Fatalf("expected 5-column layout (0..4 on row 0, 5 on row 1), got y0=%.0f y4=%.0f y5=%.0f",
			s0.target.Y, s4.target.Y, s5.target.Y)
	}
	tester.Key(0, ui.KeyEscape)
	tester.Frame()
	if state.Page != app.Series {
		t.Fatalf("expected Back to Series, got %s", state.Page)
	}

	// 4. Live "推荐直播间" -> Full-screen 4-column grid, auto-paginates, and returns cleanly.
	state.Navigate(app.Route{Page: app.Live})
	view.SyncAccountActivity()
	waitUntil(func() bool { return live.Snapshot().Loaded })
	state.Focus = liveRoomsAllFocus
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	if state.Page != app.LiveAll || len(state.Layers()) != 1 {
		t.Fatalf("expected non-modal LiveAll, got page=%s layers=%d", state.Page, len(state.Layers()))
	}
	waitUntil(func() bool { return len(live.Snapshot().Rooms) == 12 })
	tester.Key(0, ui.KeyEscape)
	tester.Frame()
	if state.Page != app.Live {
		t.Fatalf("expected Back to Live, got %s", state.Page)
	}
}

func TestLoadingStatesOmitCancelRequestButton(t *testing.T) {
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
			provider.BrowseCatalog, provider.SearchCatalog, provider.BrowseFollowing,
			provider.BrowseSeries, provider.BrowseLive, provider.BrowseLibrary,
			provider.AccountHistory.Capability(), provider.SignIn,
		},
	}
	state, err := app.NewState([]app.Entry{entry}, entry.ID, pages.Routes)
	if err != nil {
		t.Fatal(err)
	}
	sess := &shelfCollectionTestSession{
		blockSearch:    make(chan struct{}),
		blockFollowing: make(chan struct{}),
		blockSeries:    make(chan struct{}),
		blockLive:      make(chan struct{}),
		blockAccount:   make(chan struct{}),
	}
	t.Cleanup(func() {
		close(sess.blockSearch)
		close(sess.blockFollowing)
		close(sess.blockSeries)
		close(sess.blockLive)
		close(sess.blockAccount)
	})
	var mu sync.Mutex
	var queued []func()
	post := func(fn func()) {
		mu.Lock()
		queued = append(queued, fn)
		mu.Unlock()
	}
	drain := func() {
		for {
			mu.Lock()
			if len(queued) == 0 {
				mu.Unlock()
				return
			}
			batch := queued
			queued = nil
			mu.Unlock()
			for _, fn := range batch {
				fn()
			}
		}
	}
	catalog := app.NewCatalog(func(string) provider.Session { return sess }, post, func() {})
	t.Cleanup(catalog.Close)
	creds := newMemoryCreds()
	_ = creds.Save(entry.SourceID, entry.ID, []byte(`{"selected":"u1","accounts":[{"account":{"id":"u1","name":"User"},"data":{"csrf":"x"}}]}`))
	var view *View
	auth := app.NewAuth(creds, post, func() {}, func() {
		if view != nil {
			view.SyncEntry()
		}
	}, func() {})
	t.Cleanup(auth.Close)
	search := app.NewCatalogSearch(nil, post, func() {})
	t.Cleanup(search.Close)
	following := app.NewFollowingFeed(post, func() {})
	t.Cleanup(following.Close)
	series := app.NewSeriesCatalog(post, func() {})
	t.Cleanup(series.Close)
	live := app.NewLiveCatalog(post, func() {})
	t.Cleanup(live.Close)
	prefs := &memoryPreferences{}
	activity := app.NewAccountActivity(prefs, post, func() {})
	t.Cleanup(activity.Close)

	view = newTestView(t, Options{
		State: state, Catalog: catalog, Auth: auth, Activity: activity, Preferences: prefs,
		Search: search, Following: following, Series: series, Live: live,
		Pages: pages, Assets: assets, Theme: theme.Default(),
	})
	view.SyncEntry()
	tester := ui.NewTester(view.Render, 1920, 1080)
	for range 20 {
		drain()
		view.SyncAccountActivity()
		tester.Frame()
		if auth.Accounts().CurrentID == "u1" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	assertNoCancelButton := func(name string) {
		t.Helper()
		tester.Frame()
		for _, ctrl := range view.focus.controls {
			if strings.HasSuffix(string(ctrl.target.ID), "/cancel") {
				t.Fatalf("%s: unexpected cancel focus control %s", name, ctrl.target.ID)
			}
		}
		if tester.HasText(assets.Text.Cancel) {
			t.Fatalf("%s: unexpected loading cancel button %q rendered", name, assets.Text.Cancel)
		}
	}

	state.Navigate(app.Route{Page: app.Search})
	view.SyncAccountActivity()
	search.Submit("miku")
	assertNoCancelButton("Search loading")

	state.Navigate(app.Route{Page: app.Following})
	view.SyncAccountActivity()
	assertNoCancelButton("Following loading")

	state.Navigate(app.Route{Page: app.Series})
	view.SyncAccountActivity()
	assertNoCancelButton("Series hub loading")

	state.Push(app.Route{Page: app.SeriesIndex})
	view.SyncAccountActivity()
	assertNoCancelButton("Series index loading")

	state.Navigate(app.Route{Page: app.SeriesCalendar})
	view.SyncAccountActivity()
	assertNoCancelButton("Series calendar loading")

	state.Navigate(app.Route{Page: app.Live})
	view.SyncAccountActivity()
	assertNoCancelButton("Live loading")

	state.Navigate(libraryRoute(provider.AccountHistory))
	view.SyncAccountActivity()
	assertNoCancelButton("Library history loading")
}
