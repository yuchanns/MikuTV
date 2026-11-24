package app

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/yuchanns/MikuTV/internal/player"
	"github.com/yuchanns/MikuTV/internal/provider"
)

type fakeLiveReader struct {
	mu       sync.Mutex
	requests []provider.LiveRequest
	pages    map[string]provider.LivePage
}

func (f *fakeLiveReader) ReadLive(_ context.Context, req provider.LiveRequest) (provider.LivePage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, req)
	key := string(req.Section) + "|" + req.Area + "|" + req.Cursor
	return f.pages[key], nil
}

func TestLiveCatalogAreaSwitchPaginationIsolationAndFollowSync(t *testing.T) {
	queue := make(chan func(), 32)
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

	reader := &fakeLiveReader{
		pages: map[string]provider.LivePage{
			"||": {
				Areas: []provider.LiveArea{{ID: "", Label: "All"}, {ID: "2", Label: "Games"}},
				Rooms: []provider.MediaItem{
					{Ref: provider.MediaRef{EntryID: "e1", Kind: provider.Live, ID: "101"}, Title: "Room 101", Creator: &provider.Creator{ID: "u1", Name: "Alice", Following: true}},
				},
				NextCursor: "2",
				FollowingRooms: []provider.MediaItem{
					{Ref: provider.MediaRef{EntryID: "e1", Kind: provider.Live, ID: "101"}, Title: "Room 101", Creator: &provider.Creator{ID: "u1", Name: "Alice", Following: true}},
				},
				FollowingCursor: "f2",
			},
			"rooms||2": {
				Rooms: []provider.MediaItem{
					{Ref: provider.MediaRef{EntryID: "e1", Kind: provider.Live, ID: "101"}, Title: "Room 101"},
					{Ref: provider.MediaRef{EntryID: "e1", Kind: provider.Live, ID: "102"}, Title: "Room 102"},
				},
			},
			"following||f2": {
				FollowingRooms: []provider.MediaItem{
					{Ref: provider.MediaRef{EntryID: "e1", Kind: provider.Live, ID: "201"}, Title: "Followed 201", Creator: &provider.Creator{ID: "u2", Name: "Bob", Following: true}},
				},
			},
			"|2|": {
				Rooms: []provider.MediaItem{
					{Ref: provider.MediaRef{EntryID: "e1", Kind: provider.Live, ID: "301"}, Title: "Game Room"},
				},
			},
		},
	}

	live := NewLiveCatalog(func(fn func()) { queue <- fn }, func() {})
	entry := Entry{ID: "e1", Generation: 1, Capabilities: provider.Capabilities{provider.BrowseLive}}
	live.Bind(entry, "owner1", reader)
	live.EnsureLoaded()
	drain()

	snap := live.Snapshot()
	if snap.Phase != CatalogReady || snap.Hero == nil || snap.Hero.Ref.ID != "101" || len(snap.Rooms) != 1 || len(snap.FollowingRooms) != 1 {
		t.Fatalf("unexpected initial live snapshot: %+v", snap)
	}

	live.LoadMoreRooms()
	drain()
	snap = live.Snapshot()
	if len(snap.Rooms) != 2 || snap.Rooms[1].Ref.ID != "102" {
		t.Fatalf("expected deduplicated rooms pagination, got %+v", snap.Rooms)
	}

	live.LoadMoreFollowing()
	drain()
	snap = live.Snapshot()
	if len(snap.FollowingRooms) != 2 || snap.FollowingRooms[1].Ref.ID != "201" {
		t.Fatalf("expected following rooms pagination, got %+v", snap.FollowingRooms)
	}

	live.SyncFollow("u1", false)
	snap = live.Snapshot()
	if len(snap.FollowingRooms) != 1 || snap.FollowingRooms[0].Ref.ID != "201" || snap.Hero.Creator.Following {
		t.Fatalf("SyncFollow did not update following/hero: %+v", snap)
	}

	live.SelectArea("2")
	drain()
	snap = live.Snapshot()
	if snap.Area != "2" || len(snap.Rooms) != 1 || snap.Rooms[0].Ref.ID != "301" {
		t.Fatalf("SelectArea failed: %+v", snap)
	}

	// Owner change isolates and resets state.
	if !live.Bind(entry, "owner2", reader) {
		t.Fatal("expected Bind to report reset on owner change")
	}
	if snap := live.Snapshot(); snap.Phase != CatalogIdle || len(snap.Rooms) != 0 {
		t.Fatalf("expected clean state after owner switch, got %+v", snap)
	}
}

type livePlaybackSession struct {
	mu       sync.Mutex
	requests []provider.PlaybackRequest
	ended    bool
}

func (s *livePlaybackSession) Close() {}
func (s *livePlaybackSession) Browse(context.Context, provider.BrowseRequest) (provider.CollectionPage, error) {
	return provider.CollectionPage{}, nil
}
func (s *livePlaybackSession) Item(context.Context, provider.MediaRef) (provider.MediaItem, error) {
	return provider.MediaItem{}, nil
}
func (s *livePlaybackSession) ResolvePlayback(_ context.Context, req provider.PlaybackRequest) (provider.Playback, error) {
	s.mu.Lock()
	s.requests = append(s.requests, req)
	ended := s.ended
	s.mu.Unlock()
	if ended {
		return provider.Playback{
			Plan: provider.PlaybackPlan{Live: true, LiveEnded: true, ItemID: "1001", PartID: "1001"},
		}, nil
	}
	if req.Line == "broken" {
		return provider.Playback{}, &provider.Error{Code: provider.Network}
	}
	return provider.Playback{
		Transport: fakeLiveHLSTransport{},
		Plan: provider.PlaybackPlan{
			Live:    true,
			ItemID:  "1001",
			PartID:  "1001",
			Quality: "10000",
			Qualities: []provider.PlaybackQuality{
				{ID: "10000", Label: "Original"},
				{ID: "400", Label: "1080P"},
			},
			Line: "main",
			Lines: []provider.PlaybackLine{
				{ID: "main", Label: "Main"},
				{ID: "backup", Label: "Backup"},
			},
			Streams: []provider.PlaybackStream{
				{
					Format: provider.PlaybackHLS,
					Live:   true,
					Video:  provider.PlaybackTrack{Resource: provider.PlaybackResource{URLs: []string{"https://live.example.com/1001.m3u8"}}},
				},
			},
		},
	}, nil
}

type fakeLiveHLSTransport struct{}

func (fakeLiveHLSTransport) OpenResource(ctx context.Context, resource provider.PlaybackResource, method, _ string) (*http.Response, error) {
	rec := httptest.NewRecorder()
	_, _ = io.WriteString(rec, "#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:4\n#EXTINF:4.0,\nseg1.ts\n")
	res := rec.Result()
	res.Request, _ = http.NewRequestWithContext(ctx, method, resource.URLs[0], nil)
	return res, nil
}

type livePlaybackStore struct {
	saves int
}

func (s *livePlaybackStore) PlaybackPosition(string, provider.PlaybackTarget) (float64, bool) {
	return 120, true
}
func (s *livePlaybackStore) SavePlaybackPosition(string, provider.PlaybackTarget, float64) error {
	s.saves++
	return nil
}

func TestLivePlaybackLineSwitchEndedStateAndVODGuards(t *testing.T) {
	queue := make(chan func(), 32)
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

	sess := &livePlaybackSession{}
	cat := NewCatalog(func(string) provider.Session { return sess }, func(fn func()) { queue <- fn }, func() {})
	cat.Open(Entry{ID: "sample-source", Generation: 1, Capabilities: provider.Capabilities{provider.ResolvePlayback}})
	store := &livePlaybackStore{}
	var clock player.Status
	var lastRate float64
	var seeks int
	engine := player.Engine{
		Open:     func(string, float64) bool { clock = player.Status{HasFrame: true}; return true },
		Seek:     func(float64) { seeks++ },
		SetRate:  func(r float64) { lastRate = r },
		Stop:     func() {},
		Snapshot: func() player.Status { return clock },
	}
	pb := NewPlayback(cat, engine, store, func(fn func()) { queue <- fn }, func() {})
	pb.Start(provider.PlaybackTarget{Ref: provider.MediaRef{EntryID: "sample-source", Kind: provider.Live, ID: "1001"}}, "u1", true, nil)
	drain()

	if pb.State().Position != 0 || !pb.State().Resolution.Plan.Live || lastRate != 1 {
		t.Fatalf("unexpected live start state: %+v rate=%v", pb.State(), lastRate)
	}

	// VOD-only Seek and non-1x SetRate are ignored/clamped in live mode.
	pb.Seek(55)
	pb.SetRate(2.0)
	if seeks != 0 || lastRate != 1 || pb.Rate() != 1 {
		t.Fatalf("live mode should ignore seek and clamp rate to 1: seeks=%d rate=%v", seeks, lastRate)
	}

	// Switching to a broken line enters disconnected failure state; switching to backup recovers cleanly.
	pb.ChooseLine("broken")
	drain()
	if pb.State().Failure != provider.Network {
		t.Fatalf("expected network failure on broken line, got %+v", pb.State())
	}
	pb.ChooseLine("backup")
	drain()
	if pb.State().Failure != "" || pb.State().SelectedLine != "backup" || !pb.State().Playing {
		t.Fatalf("expected backup line to recover live playback, got %+v", pb.State())
	}

	// When live stream ends and source reports LiveEnded, transition cleanly to LiveEnded (not Failure).
	sess.mu.Lock()
	sess.ended = true
	sess.mu.Unlock()
	clock.Ended = true
	pb.Tick(time.Now())
	drain()
	if !pb.State().LiveEnded || pb.State().Failure != "" || pb.State().Playing {
		t.Fatalf("expected LiveEnded state, got %+v", pb.State())
	}
	pb.Toggle()
	if !pb.State().LiveEnded || pb.State().Playing {
		t.Fatalf("Toggle should not restart ended live stream: %+v", pb.State())
	}

	pb.Close()
	if store.saves != 0 {
		t.Fatalf("live playback must never save VOD progress, got %d saves", store.saves)
	}
}
