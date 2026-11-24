package tvui

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/player"
	"github.com/yuchanns/MikuTV/internal/provider"
	"github.com/yuchanns/MikuTV/internal/theme"
)

type uiInteractiveSession struct {
	uiAuthSource
	mu    sync.Mutex
	item  provider.MediaItem
	calls []provider.PlaybackRequest
}

func (s *uiInteractiveSession) Browse(context.Context, provider.BrowseRequest) (provider.CollectionPage, error) {
	return provider.CollectionPage{}, nil
}

func (s *uiInteractiveSession) Item(context.Context, provider.MediaRef) (provider.MediaItem, error) {
	return s.item, nil
}

func (s *uiInteractiveSession) VideoDetail(context.Context, provider.MediaRef) (provider.MediaItem, error) {
	return s.item, nil
}

func (s *uiInteractiveSession) ResolvePlayback(_ context.Context, req provider.PlaybackRequest) (provider.Playback, error) {
	s.mu.Lock()
	s.calls = append(s.calls, req)
	s.mu.Unlock()
	stream := provider.PlaybackStream{
		Format: provider.PlaybackDirect,
		Video:  provider.PlaybackTrack{Resource: provider.PlaybackResource{URLs: []string{"https://example.test/branch.mp4"}}, ContentType: "video/mp4"},
	}
	switch req.BranchID {
	case "":
		return provider.Playback{
			Plan: provider.PlaybackPlan{
				ItemID: "aid-i", PartID: "cid-root", Duration: 30, Access: provider.VideoAccessGranted,
				Streams: []provider.PlaybackStream{stream},
				Branch: &provider.PlaybackBranch{
					NodeID: "1", NodeTitle: "Orbit", Question: "Where next?", TriggerSeconds: 20, PauseVideo: true, State: "st0",
					Options: []provider.PlaybackBranchOption{
						{ID: "b1", Title: "Cross the ring", Subtitle: "Follow the signal", PartID: "cid-b1", State: "st1", Default: true},
						{ID: "b2", Title: "Land on planet", Subtitle: "Explore the light", PartID: "cid-b2", State: "st2"},
					},
				},
			},
			Transport: fakeMediaTransport{},
		}, nil
	case "b2":
		return provider.Playback{
			Plan: provider.PlaybackPlan{
				ItemID: "aid-i", PartID: "cid-b2", Duration: 25, Access: provider.VideoAccessGranted,
				Streams: []provider.PlaybackStream{stream},
				Branch: &provider.PlaybackBranch{
					NodeID: "b2", NodeTitle: "Surface", Question: "Open the gate?", TriggerSeconds: 18, PauseVideo: true, State: "st2",
					Options: []provider.PlaybackBranchOption{
						{ID: "end", Title: "Enter gate", PartID: "cid-end", State: "st3", Default: true},
					},
				},
			},
			Transport: fakeMediaTransport{},
		}, nil
	case "end":
		return provider.Playback{
			Plan: provider.PlaybackPlan{
				ItemID: "aid-i", PartID: "cid-end", Duration: 12, Access: provider.VideoAccessGranted,
				Streams: []provider.PlaybackStream{stream},
				Branch:  &provider.PlaybackBranch{NodeID: "end", NodeTitle: "Dawn Ending", State: "st3"},
			},
			Transport: fakeMediaTransport{},
		}, nil
	default:
		return provider.Playback{}, &provider.Error{Code: provider.Expired}
	}
}

func (s *uiInteractiveSession) Close() {}

func TestInteractiveBranchOverlayDismissReopenMultiStepAndBackHierarchy(t *testing.T) {
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
		},
	}
	state, err := app.NewState([]app.Entry{entry}, entry.ID, pages.Routes)
	if err != nil {
		t.Fatal(err)
	}

	ref := provider.MediaRef{EntryID: entry.ID, Kind: provider.Video, ID: "BV1OrbitInt"}
	dur := int64(30)
	sess := &uiInteractiveSession{
		item: provider.MediaItem{
			Ref: ref, Title: "Orbit Choice", Subtitle: "Interactive journey.", DurationSeconds: &dur,
			VideoDetails: &provider.VideoDetails{Access: provider.VideoAccessGranted},
		},
	}

	queue := make(chan func(), 64)
	post := func(fn func()) { queue <- fn }
	drainOne := func() {
		t.Helper()
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
	clock := player.Status{HasFrame: true, Duration: 30, Position: 5}
	playback := app.NewPlayback(catalog, player.Engine{
		Open:     func(string, float64) bool { return true },
		Snapshot: func() player.Status { return clock },
	}, nil, post, func() {})
	t.Cleanup(playback.Close)

	view := newTestView(t, Options{
		State: state, Catalog: catalog,
		Pages: pages, Assets: assets, Theme: theme.Default(),
	})
	view.Playback = playback
	view.SyncEntry()
	for catalog.Snapshot().Phase == app.CatalogLoading {
		drainOne()
	}
	drain()

	tester := ui.NewTester(view.Render, 1920, 1080)
	pageCtx := view.context(state.Location, true, 1920, 1080)
	pageCtx.openVideoDetail(sess.item)
	for view.videoDetail.loading {
		drainOne()
	}
	drain()
	tester.Frame()

	// 1. Play interactive video from VideoDetail -> resolves root branch.
	state.Focus = videoDetailPlay
	view.reveal = true
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	for playback.State().Loading {
		drainOne()
	}
	drain()
	tester.Frame()
	if state.Page != app.Player || playback.State().Target.PartID != "cid-root" || view.player.branchVisible {
		t.Fatalf("expected playing root clip before trigger, got page=%s part=%q branchVisible=%t",
			state.Page, playback.State().Target.PartID, view.player.branchVisible)
	}

	// 2. Advance position past TriggerSeconds (20s) -> pauses and shows branch overlay focused on option 0!
	clock.Position = 20.5
	tester.Frame()
	if !view.player.branchVisible || !playback.State().BranchPending || playback.State().Playing || state.Focus != playerBranchOptionID(0) {
		t.Fatalf("expected paused branch overlay focused on option 0, got visible=%t pending=%t playing=%t focus=%q",
			view.player.branchVisible, playback.State().BranchPending, playback.State().Playing, state.Focus)
	}

	// 3. Back Layer 1: Escape dismisses branch overlay and restores controls on player/toggle!
	tester.Key(0, ui.KeyEscape)
	tester.Frame()
	if view.player.branchVisible || playback.State().BranchPending || !view.player.visible || state.Focus != playerToggle {
		t.Fatalf("expected Escape from branch overlay to reveal controls at player/toggle, got visible=%t controls=%t focus=%q",
			view.player.branchVisible, view.player.visible, state.Focus)
	}

	// 4. Clicking player/branch button re-opens the branch overlay cleanly!
	state.Focus = playerBranchControl
	view.reveal = true
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	if !view.player.branchVisible || state.Focus != playerBranchOptionID(0) {
		t.Fatalf("expected player/branch control to reopen branch overlay, got visible=%t focus=%q", view.player.branchVisible, state.Focus)
	}

	// 5. Move right to option 1 ("Land on planet") and press Enter -> jumps to cid-b2!
	tester.Key(0, ui.KeyRight)
	tester.Frame()
	if state.Focus != playerBranchOptionID(1) {
		t.Fatalf("expected Right arrow to focus branch option 1, got %q", state.Focus)
	}
	tester.Key(0, ui.KeyEnter)
	for playback.State().Loading {
		drainOne()
	}
	drain()
	clock = player.Status{HasFrame: true, Duration: 25, Position: 3}
	tester.Frame()
	if playback.State().Target.PartID != "cid-b2" || playback.State().BranchHistory != 1 || view.player.branchVisible || state.Focus != playerWake {
		t.Fatalf("expected transition to level 2 branch cid-b2 with wake focus, got part=%q history=%d visible=%t focus=%q",
			playback.State().Target.PartID, playback.State().BranchHistory, view.player.branchVisible, state.Focus)
	}

	// 6. Reach level 2 branch point -> select leaf ending -> reaches Ended -> Escape returns cleanly to VideoDetail!
	clock.Position = 18.5
	tester.Frame()
	if !view.player.branchVisible || state.Focus != playerBranchOptionID(0) {
		t.Fatalf("expected level 2 branch prompt focused on option 0, got visible=%t focus=%q", view.player.branchVisible, state.Focus)
	}
	tester.Key(0, ui.KeyEnter)
	for playback.State().Loading {
		drainOne()
	}
	drain()
	clock = player.Status{HasFrame: true, Duration: 12, Position: 12, Ended: true}
	tester.Frame()
	if playback.State().Target.PartID != "cid-end" || view.player.feedback != playbackFeedbackEnded {
		t.Fatalf("expected leaf ending to reach Ended feedback screen, got part=%q feedback=%v", playback.State().Target.PartID, view.player.feedback)
	}
	tester.Key(0, ui.KeyEscape)
	tester.Frame()
	if state.Page != app.VideoDetail || state.Focus != videoDetailPlay {
		t.Fatalf("expected Back from ending to return cleanly to VideoDetail Play focus, got page=%s focus=%q", state.Page, state.Focus)
	}
}
