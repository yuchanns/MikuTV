package tvui

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/player"
	"github.com/yuchanns/MikuTV/internal/provider"
	"github.com/yuchanns/MikuTV/internal/theme"
)

type uiLiveSession struct {
	mu           sync.Mutex
	ended        bool
	failLine     string
	followed     map[string]bool
	lastLine     string
	lastQuality  string
	lastAreaSeen string
}

func (s *uiLiveSession) Close() {}
func (s *uiLiveSession) Browse(context.Context, provider.BrowseRequest) (provider.CollectionPage, error) {
	return provider.CollectionPage{}, nil
}
func (s *uiLiveSession) Item(_ context.Context, ref provider.MediaRef) (provider.MediaItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return provider.MediaItem{
		Ref:      ref,
		Title:    "Tonight Live Stage",
		Subtitle: "Welcome to our acoustic night.",
		Category: "Music",
		Quality:  "LIVE",
		Creator:  &provider.Creator{ID: "host-1", Name: "CityStage", Following: s.followed["host-1"]},
	}, nil
}
func (s *uiLiveSession) VideoDetail(ctx context.Context, ref provider.MediaRef) (provider.MediaItem, error) {
	return s.Item(ctx, ref)
}
func (s *uiLiveSession) ReadLive(_ context.Context, req provider.LiveRequest) (provider.LivePage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastAreaSeen = req.Area
	views := int64(13200)
	room := provider.MediaItem{
		Ref:      provider.MediaRef{EntryID: "live-src", Kind: provider.Live, ID: "392731"},
		Title:    "Tonight Live Stage",
		Subtitle: "Sing the night away",
		Category: "Music",
		Views:    &views,
		Quality:  "LIVE",
		Creator:  &provider.Creator{ID: "host-1", Name: "CityStage", Following: s.followed["host-1"]},
	}
	if req.Area == "games" {
		room.Ref.ID = "400001"
		room.Title = "Midnight Speedrun"
		room.Category = "Games"
	}
	return provider.LivePage{
		Areas:          []provider.LiveArea{{ID: "", Label: "All"}, {ID: "games", Label: "Games"}},
		Hero:           &room,
		Rooms:          []provider.MediaItem{room},
		FollowingRooms: []provider.MediaItem{room},
	}, nil
}
func (s *uiLiveSession) FollowCreator(_ context.Context, req provider.CreatorFollowRequest) (provider.CreatorFollowResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.followed == nil {
		s.followed = make(map[string]bool)
	}
	s.followed[req.CreatorID] = req.Enabled
	return provider.CreatorFollowResult{Following: req.Enabled}, nil
}
func (s *uiLiveSession) ResolvePlayback(_ context.Context, req provider.PlaybackRequest) (provider.Playback, error) {
	s.mu.Lock()
	s.lastLine = req.Line
	s.lastQuality = req.Quality
	ended := s.ended
	failLine := s.failLine
	s.mu.Unlock()
	if ended {
		return provider.Playback{
			Plan: provider.PlaybackPlan{Live: true, LiveEnded: true, ItemID: req.Ref.ID, PartID: req.Ref.ID},
		}, nil
	}
	if failLine != "" && req.Line == failLine {
		return provider.Playback{}, &provider.Error{Code: provider.Network}
	}
	q := req.Quality
	if q == "" {
		q = "10000"
	}
	line := req.Line
	if line == "" {
		line = "fmp4-0"
	}
	return provider.Playback{
		Transport: uiLiveHLSTransport{},
		Plan: provider.PlaybackPlan{
			Live:    true,
			ItemID:  req.Ref.ID,
			PartID:  req.Ref.ID,
			Quality: q,
			Qualities: []provider.PlaybackQuality{
				{ID: "10000", Label: "Original"},
				{ID: "400", Label: "1080P"},
				{ID: "250", Label: "720P"},
			},
			Line: line,
			Lines: []provider.PlaybackLine{
				{ID: "fmp4-0", Label: "Main Line"},
				{ID: "ts-1", Label: "Backup Line"},
			},
			Streams: []provider.PlaybackStream{
				{
					Format: provider.PlaybackHLS,
					Live:   true,
					Video:  provider.PlaybackTrack{Resource: provider.PlaybackResource{URLs: []string{"https://live.example.com/room.m3u8"}}},
				},
			},
		},
	}, nil
}

type uiLiveHLSTransport struct{}

func (uiLiveHLSTransport) OpenResource(ctx context.Context, resource provider.PlaybackResource, method, _ string) (*http.Response, error) {
	rec := httptest.NewRecorder()
	_, _ = io.WriteString(rec, "#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:4\n#EXTINF:4.0,\nchunk.ts\n")
	res := rec.Result()
	res.Request, _ = http.NewRequestWithContext(ctx, method, resource.URLs[0], nil)
	return res, nil
}

func TestLivePageCapabilityHidingAndLivePlayerStates(t *testing.T) {
	assets, err := LoadAssets()
	if err != nil {
		t.Fatal(err)
	}
	pages, err := NewPages(assets)
	if err != nil {
		t.Fatal(err)
	}

	// 1. Source without catalog.live hides the Live nav link cleanly.
	noLiveEntry := app.Entry{ID: "vod-only", Name: "VOD", Capabilities: provider.Capabilities{provider.BrowseCatalog}}
	noLiveState, err := app.NewState([]app.Entry{noLiveEntry}, "vod-only", pages.Routes)
	if err != nil {
		t.Fatal(err)
	}
	for _, nav := range pages.navigation(noLiveState, primaryNavigation) {
		if nav.Policy.Route.Page == app.Live {
			t.Fatal("Live nav item must be hidden when source lacks catalog.live")
		}
	}

	// 2. Full Live page + Live player flow.
	sess := &uiLiveSession{followed: map[string]bool{"host-1": false}}
	entry := app.Entry{
		ID:   "live-src",
		Name: "LiveSource",
		Capabilities: provider.Capabilities{
			provider.BrowseCatalog, provider.BrowseLive, provider.GetCatalogItem,
			provider.ResolvePlayback, provider.FollowCreator, provider.ReadDanmaku,
			provider.ReadSubtitles, provider.ReadVideoComments, provider.EditVideo,
			provider.EditVideoFavorites,
		},
	}
	state, err := app.NewState([]app.Entry{entry}, "live-src", pages.Routes)
	if err != nil {
		t.Fatal(err)
	}
	queue := make(chan func(), 64)
	post := func(fn func()) { queue <- fn }
	drain := func() {
		for {
			select {
			case fn := <-queue:
				fn()
			case <-time.After(15 * time.Millisecond):
				return
			}
		}
	}

	cat := app.NewCatalog(func(string) provider.Session { return sess }, post, func() {})
	live := app.NewLiveCatalog(post, func() {})
	view := newTestView(t, Options{State: state, Catalog: cat, Live: live, Pages: pages, Assets: assets, Theme: theme.Default()})
	view.Auth = app.NewAuth(nil, post, func() {}, func() {}, func() {})
	var clock player.Status
	view.Playback = app.NewPlayback(cat, player.Engine{
		Open:     func(string, float64) bool { clock = player.Status{HasFrame: true}; return true },
		SetRate:  func(float64) {},
		Stop:     func() {},
		Snapshot: func() player.Status { return clock },
	}, nil, post, func() {})
	view.SyncEntry()
	drain()

	tester := ui.NewTester(view.Render, 1920, 1080)
	if !state.Navigate(app.Route{Page: app.Live}) {
		t.Fatal("failed to navigate to Live page")
	}
	view.SyncAccountActivity()
	drain()
	tester.Frame()

	// Verify Hero controls & Live room cards exist.
	if view.focus.item(liveHeroPlayFocus) == nil || view.focus.item(liveHeroInfoFocus) == nil {
		t.Fatal("expected Live Hero Play and Details buttons to be rendered")
	}
	if view.focus.item(liveRoomsAllFocus) == nil || view.focus.item(liveFollowingAllFocus) == nil {
		t.Fatal("expected Recommended and Following live shelves to be rendered")
	}

	// Switch area tab to "games".
	view.selectLiveArea("games")
	drain()
	tester.Frame()
	if snap := live.Snapshot(); snap.Area != "games" || snap.Hero == nil || snap.Hero.Ref.ID != "400001" {
		t.Fatalf("area switch failed: %+v", snap)
	}

	// Enter live playback directly from the hero room.
	page := view.context(state.Location, true, 1920, 1080)
	page.openLivePlayback(*live.Snapshot().Hero, false)
	drain()
	tester.Frame()

	// Verify VOD-only controls are HIDDEN while Live controls are PRESENT.
	for _, forbidden := range []app.FocusID{playerTimeline, playerPrevious, playerNext, playerSpeed, playerSubtitles, playerDanmaku, playerComments, playerLike, playerFavorite} {
		if view.focus.item(forbidden) != nil {
			t.Fatalf("VOD-only control %q must be hidden in live playback", forbidden)
		}
	}
	for _, required := range []app.FocusID{playerInfo, playerToggle, playerLiveFollow, playerQuality, playerLine} {
		if view.focus.item(required) == nil {
			t.Fatalf("Live player control %q is missing", required)
		}
	}

	// Open Live Quality popover and verify options (no VOD "Auto" row).
	view.player.openPanel(view, playerQuality)
	tester.Frame()
	if view.focus.item(playbackOptionID(playerQuality, "10000")) == nil || view.focus.item(playbackOptionID(playerQuality, "")) != nil {
		t.Fatal("expected live quality options without VOD Auto option")
	}
	view.goBack()
	tester.Frame()

	// Open Live Line popover and switch to Backup Line.
	view.player.openPanel(view, playerLine)
	tester.Frame()
	if view.focus.item(playbackOptionID(playerLine, "")) == nil || view.focus.item(playbackOptionID(playerLine, "ts-1")) == nil {
		t.Fatal("expected Auto and Backup Line options in live line popover")
	}
	view.Playback.ChooseLine("ts-1")
	view.player.panel = ""
	drain()
	tester.Frame()
	if view.Playback.State().SelectedLine != "ts-1" {
		t.Fatalf("expected SelectedLine=ts-1, got %q", view.Playback.State().SelectedLine)
	}

	// Open Room Info overlay (frameless) and verify Back closes cleanly.
	view.player.openPanel(view, playerInfo)
	tester.Frame()
	if view.focus.item(playerRoomFollow) == nil || view.focus.item(playerPanelExit) == nil {
		t.Fatal("expected Room Info overlay follow and close actions")
	}
	view.goBack()
	tester.Frame()
	if view.player.panel != "" {
		t.Fatalf("expected Back to close Room Info overlay, panel=%q", view.player.panel)
	}

	// Disconnected state shows Reconnect + Line switcher, and Line popover can open & close over it.
	sess.mu.Lock()
	sess.failLine = "broken-line"
	sess.mu.Unlock()
	view.Playback.ChooseLine("broken-line")
	drain()
	tester.Frame()
	if view.focus.item(playerStatus) == nil || view.focus.item(playerStatusLine) == nil {
		t.Fatal("expected Disconnected state to render Reconnect and Line buttons")
	}
	view.player.openPanel(view, playerLine)
	tester.Frame()
	if view.focus.item(playbackOptionID(playerLine, "fmp4-0")) == nil {
		t.Fatal("expected Line popover to open over Disconnected screen")
	}
	view.goBack()
	tester.Frame()
	if view.player.panel != "" || state.Page != app.Player || state.Focus != playerStatusLine {
		t.Fatalf("Back from disconnected line popover should stay in Player at playerStatusLine, got page=%s panel=%q focus=%s", state.Page, view.player.panel, state.Focus)
	}

	// Ended state shows "More Live" button and navigates back to Live page.
	sess.mu.Lock()
	sess.ended = true
	sess.mu.Unlock()
	view.Playback.Retry()
	drain()
	tester.Frame()
	if !view.Playback.State().LiveEnded || view.focus.item(playerStatus) == nil || view.focus.item(playerStatusLine) != nil {
		t.Fatalf("expected LiveEnded screen with single primary action, state=%+v", view.Playback.State())
	}
	view.context(state.Location, true, 1920, 1080).leaveLiveToBrowse()
	if state.Page != app.Live {
		t.Fatalf("expected leaveLiveToBrowse to navigate to app.Live, got %s", state.Page)
	}
}
