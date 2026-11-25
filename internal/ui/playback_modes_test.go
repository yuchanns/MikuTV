package tvui

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/player"
	"github.com/yuchanns/MikuTV/internal/provider"
	"github.com/yuchanns/MikuTV/internal/theme"
)

type modesTestSession struct {
	resolved []string
	folder   []provider.AccountRow
}

func (s *modesTestSession) Close() {}

func (s *modesTestSession) Browse(context.Context, provider.BrowseRequest) (provider.CollectionPage, error) {
	return provider.CollectionPage{}, nil
}

func (s *modesTestSession) Item(context.Context, provider.MediaRef) (provider.MediaItem, error) {
	return provider.MediaItem{}, nil
}

func (s *modesTestSession) ResolvePlayback(_ context.Context, req provider.PlaybackRequest) (provider.Playback, error) {
	s.resolved = append(s.resolved, req.Ref.ID)
	return provider.Playback{
		Plan: provider.PlaybackPlan{
			Duration: 180,
			Streams: []provider.PlaybackStream{{
				Format: provider.PlaybackDirect,
				Audio: &provider.PlaybackTrack{
					ContentType: "audio/mpeg",
					Resource:    provider.PlaybackResource{URLs: []string{"https://cdn.example/" + req.Ref.ID + ".mp3"}},
				},
			}},
		},
		Transport: modesTestTransport{},
	}, nil
}

func (s *modesTestSession) ReadAccount(_ context.Context, req provider.AccountRequest) (provider.AccountPage, error) {
	if req.Folder != "" {
		return provider.AccountPage{Rows: s.folder}, nil
	}
	return provider.AccountPage{Rows: []provider.AccountRow{{ID: "f1", Title: "My Playlist"}}}, nil
}

type modesTestTransport struct{}

func (modesTestTransport) OpenResource(context.Context, provider.PlaybackResource, string, string) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"audio/mpeg"}},
		Body:       io.NopCloser(bytes.NewReader([]byte("ID3"))),
	}, nil
}

func TestPlaybackModesCapabilityGatingAndStepBehavior(t *testing.T) {
	assets, err := LoadAssets()
	if err != nil {
		t.Fatal(err)
	}
	pages, err := NewPages(assets)
	if err != nil {
		t.Fatal(err)
	}

	makeEnv := func(withModes bool) (*View, *app.State, *modesTestSession, *player.Status, *int, func()) {
		caps := provider.Capabilities{provider.BrowseCatalog, provider.ResolvePlayback}
		if withModes {
			caps = append(caps, provider.PlaybackModes)
		}
		entries := []app.Entry{{ID: "demo", SourceID: "demo", Name: "Demo", Capabilities: caps}}
		state, err := app.NewState(entries, "demo", pages.Routes)
		if err != nil {
			t.Fatal(err)
		}
		sess := &modesTestSession{}
		queue := make(chan func(), 32)
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
		catalog := app.NewCatalog(func(string) provider.Session { return sess }, func(fn func()) { queue <- fn }, func() {})
		catalog.Open(entries[0])

		clock := &player.Status{HasFrame: true, AudioOnly: true, Position: 10, Duration: 180}
		seekZeroCount := 0
		pb := app.NewPlayback(catalog, player.Engine{
			Open:     func(string, float64) bool { return true },
			Seek:     func(sec float64) { if sec == 0 { seekZeroCount++ }; clock.Ended = false; clock.Position = sec },
			SetRate:  func(float64) {},
			Stop:     func() {},
			Snapshot: func() player.Status { return *clock },
		}, nil, func(fn func()) { queue <- fn }, func() {})

		prefs := app.NewPlaybackSettings(nil, nil)
		view := newTestView(t, Options{
			State:            state,
			Catalog:          catalog,
			PlaybackSettings: prefs,
			Pages:            pages,
			Assets:           assets,
			Theme:            theme.Default(),
		})
		view.Playback = pb
		t.Cleanup(func() {
			pb.Close()
			catalog.Close()
		})
		return view, state, sess, clock, &seekZeroCount, drain
	}

	items := []playbackItem{
		{item: provider.MediaItem{Ref: provider.MediaRef{EntryID: "demo", Kind: provider.Video, ID: "t1"}, Title: "Track 1"}},
		{item: provider.MediaItem{Ref: provider.MediaRef{EntryID: "demo", Kind: provider.Video, ID: "t2"}, Title: "Track 2"}},
		{item: provider.MediaItem{Ref: provider.MediaRef{EntryID: "demo", Kind: provider.Video, ID: "t3"}, Title: "Track 3"}},
	}

	// 1. Plugin WITHOUT playback.modes must not show loop button.
	{
		view, state, _, _, _, drain := makeEnv(false)
		state.Push(app.Route{Page: app.Player})
		view.player = playbackView{items: items, keepQueue: true, visible: true}
		view.startPlaybackItem(0, nil)
		time.Sleep(30 * time.Millisecond)
		drain()
		tester := ui.NewTester(view.Render, 1920, 1080)
		tester.Frame()
		if _, found := tester.Access(assets.Text.Playback.ModeSequential); found {
			t.Fatal("loop mode button must not appear when plugin does not declare playback.modes")
		}
	}

	// 2. Plugin WITH playback.modes shows button, cycles modes, supports repeat-one zero-reload, repeat-all wrap, and shuffle.
	{
		view, state, sess, clock, seekZero, drain := makeEnv(true)
		state.Push(app.Route{Page: app.Player})
		view.player = playbackView{items: items, keepQueue: true, visible: true}
		view.startPlaybackItem(0, nil)
		time.Sleep(30 * time.Millisecond)
		drain()

		tester := ui.NewTester(view.Render, 1920, 1080)
		tester.Frame()
		if _, found := tester.Access(assets.Text.Playback.ModeSequential); !found {
			t.Fatal("expected loop mode button when plugin declares playback.modes and len(items) > 1")
		}

		// Cycle sequential -> repeat-all -> repeat-one -> shuffle
		view.cyclePlaybackLoopMode()
		if got := view.playbackLoopMode(); got != app.PlaybackLoopRepeatAll {
			t.Fatalf("mode = %q, want repeat-all", got)
		}
		// Previous from 0 in repeat-all wraps to last item (2)
		if !view.stepPlayback(-1, false) || view.player.index != 2 {
			t.Fatalf("repeat-all prev from 0 got index=%d, want 2", view.player.index)
		}
		time.Sleep(30 * time.Millisecond)
		drain()
		// Next from last item (2) wraps to 0
		if !view.stepPlayback(1, false) || view.player.index != 0 {
			t.Fatalf("repeat-all next from 2 got index=%d, want 0", view.player.index)
		}
		time.Sleep(30 * time.Millisecond)
		drain()

		// Repeat-one on ended: zero-reload Seek(0) without calling ResolvePlayback again!
		view.cyclePlaybackLoopMode()
		if got := view.playbackLoopMode(); got != app.PlaybackLoopRepeatOne {
			t.Fatalf("mode = %q, want repeat-one", got)
		}
		resolvedBefore := len(sess.resolved)
		clock.Ended = true
		view.Playback.Tick(time.Now())
		tester.Frame()
		if *seekZero == 0 {
			t.Fatal("expected repeat-one ended to invoke zero-reload Seek(0)")
		}
		if len(sess.resolved) != resolvedBefore {
			t.Fatalf("repeat-one must not re-resolve playback; resolved grew %d -> %d", resolvedBefore, len(sess.resolved))
		}
		if !view.Playback.State().Playing {
			t.Fatal("expected repeat-one to resume playing after Seek(0)")
		}

		// Shuffle visits every track in 3 steps without duplicates
		view.cyclePlaybackLoopMode()
		if got := view.playbackLoopMode(); got != app.PlaybackLoopShuffle {
			t.Fatalf("mode = %q, want shuffle", got)
		}
		seen := map[int]bool{view.player.index: true}
		for i := 0; i < 2; i++ {
			if !view.stepPlayback(1, false) {
				t.Fatalf("shuffle step %d failed", i)
			}
			seen[view.player.index] = true
			time.Sleep(20 * time.Millisecond)
			drain()
		}
		if len(seen) != 3 {
			t.Fatalf("shuffle visited %v, want all 3 tracks", seen)
		}

		// Existing (i) info drawer reuses Playlist heading & items when keepQueue=true
		view.player.openPanel(view, playerInfo)
		tester.Frame()
		if _, found := tester.Access(assets.Text.Playback.Playlist); !found {
			t.Fatal("expected (i) drawer to show Playlist button/section when keepQueue=true and len(items)>1")
		}
	}
}

func TestFavoritesFolderUnlocksPlayAll(t *testing.T) {
	assets, err := LoadAssets()
	if err != nil {
		t.Fatal(err)
	}
	pages, err := NewPages(assets)
	if err != nil {
		t.Fatal(err)
	}
	caps := provider.Capabilities{
		provider.BrowseCatalog,
		provider.BrowseLibrary,
		provider.AccountFavorites.Capability(),
		provider.ResolvePlayback,
	}
	entries := []app.Entry{{ID: "demo", SourceID: "demo", Name: "Demo", Capabilities: caps}}
	state, err := app.NewState(entries, "demo", pages.Routes)
	if err != nil {
		t.Fatal(err)
	}
	ref1 := provider.MediaRef{EntryID: "demo", Kind: provider.Video, ID: "a1"}
	ref2 := provider.MediaRef{EntryID: "demo", Kind: provider.Video, ID: "a2"}
	sess := &modesTestSession{
		folder: []provider.AccountRow{
			{ID: "a1", Title: "Song 1", Ref: &ref1},
			{ID: "a2", Title: "Song 2", Ref: &ref2},
		},
	}
	queue := make(chan func(), 32)
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
	catalog := app.NewCatalog(func(string) provider.Session { return sess }, func(fn func()) { queue <- fn }, func() {})
	defer catalog.Close()
	catalog.Open(entries[0])

	activity := app.NewAccountActivity(nil, func(fn func()) { queue <- fn }, func() {})
	defer activity.Close()
	activity.Bind(entries[0], "user-1", sess)

	pb := app.NewPlayback(catalog, player.Engine{
		Open:     func(string, float64) bool { return true },
		SetRate:  func(float64) {},
		Stop:     func() {},
		Snapshot: func() player.Status { return player.Status{HasFrame: true} },
	}, nil, func(fn func()) { queue <- fn }, func() {})
	defer pb.Close()

	view := newTestView(t, Options{
		State:    state,
		Catalog:  catalog,
		Activity: activity,
		Pages:    pages,
		Assets:   assets,
		Theme:    theme.Default(),
	})
	view.Playback = pb

	// Navigate into favorite folder f1
	state.Navigate(libraryRoute(provider.AccountFavorites))
	activity.SetFolder(provider.AccountRow{ID: "f1", Title: "My Playlist"})
	state.Push(app.Route{Page: app.Library, Section: favoriteFolderSection})
	activity.Ensure(provider.AccountQuery{Section: provider.AccountFavorites, Folder: "f1"})
	time.Sleep(40 * time.Millisecond)
	drain()

	tester := ui.NewTester(view.Render, 1920, 1080)
	tester.Frame()
	if _, found := tester.Access(assets.Text.Activity.PlayAll); !found {
		t.Fatal("expected Play All button inside favorite folder")
	}
}
