package app

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/yuchanns/MikuTV/internal/player"
	"github.com/yuchanns/MikuTV/internal/provider"
)

type interactivePlaybackSession struct {
	mu       sync.Mutex
	requests []provider.PlaybackRequest
}

func (s *interactivePlaybackSession) Browse(context.Context, provider.BrowseRequest) (provider.CollectionPage, error) {
	return provider.CollectionPage{}, nil
}

func (s *interactivePlaybackSession) Item(context.Context, provider.MediaRef) (provider.MediaItem, error) {
	return provider.MediaItem{}, nil
}

func (s *interactivePlaybackSession) Close() {}

type interactiveFakeTransport struct{}

func (interactiveFakeTransport) OpenResource(context.Context, provider.PlaybackResource, string, string) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: http.NoBody}, nil
}

func (s *interactivePlaybackSession) ResolvePlayback(_ context.Context, req provider.PlaybackRequest) (provider.Playback, error) {
	s.mu.Lock()
	s.requests = append(s.requests, req)
	s.mu.Unlock()
	stream := provider.PlaybackStream{
		Format: provider.PlaybackDirect,
		Video:  provider.PlaybackTrack{Resource: provider.PlaybackResource{URLs: []string{"https://example.test/clip.mp4"}}, ContentType: "video/mp4"},
	}
	switch req.BranchID {
	case "":
		// Auto-advance intro node (5s) -> transitions to node "start".
		return provider.Playback{
			Plan: provider.PlaybackPlan{
				ItemID: "aid-1", PartID: "cid-intro", Duration: 5, Access: provider.VideoAccessGranted,
				Streams: []provider.PlaybackStream{stream},
				Branch: &provider.PlaybackBranch{
					NodeID: "1", NodeTitle: "Intro", TriggerSeconds: 4.8, AutoAdvance: true, State: "s0",
					Options: []provider.PlaybackBranchOption{{ID: "start", PartID: "cid-start", State: "s1"}},
				},
			},
			Transport: interactiveFakeTransport{},
		}, nil
	case "start":
		return provider.Playback{
			Plan: provider.PlaybackPlan{
				ItemID: "aid-1", PartID: "cid-start", Duration: 20, Access: provider.VideoAccessGranted,
				Streams: []provider.PlaybackStream{stream},
				Branch: &provider.PlaybackBranch{
					NodeID: "start", NodeTitle: "Fork", Question: "Pick path", TriggerSeconds: 15, PauseVideo: true, State: "s1",
					Options: []provider.PlaybackBranchOption{
						{ID: "end", Title: "Go End", PartID: "cid-end", State: "s2", Default: true},
					},
				},
			},
			Transport: interactiveFakeTransport{},
		}, nil
	case "end":
		return provider.Playback{
			Plan: provider.PlaybackPlan{
				ItemID: "aid-1", PartID: "cid-end", Duration: 10, Access: provider.VideoAccessGranted,
				Streams: []provider.PlaybackStream{stream},
				Branch: &provider.PlaybackBranch{
					NodeID: "end", NodeTitle: "Ending", State: "s2",
				},
			},
			Transport: interactiveFakeTransport{},
		}, nil
	default:
		return provider.Playback{}, &provider.Error{Code: provider.Expired}
	}
}

func TestPlaybackInteractiveAutoAdvancePauseDismissSeekSelectAndReplayRoot(t *testing.T) {
	sess := &interactivePlaybackSession{}
	queue := make(chan func(), 32)
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
	waitLoading := func(pb *Playback) {
		t.Helper()
		for pb.State().Loading {
			select {
			case fn := <-queue:
				fn()
			case <-time.After(2 * time.Second):
				t.Fatal("timed out waiting for playback load")
			}
		}
		drain()
	}

	catalog := NewCatalog(func(string) provider.Session { return sess }, post, func() {})
	defer catalog.Close()
	catalog.Open(Entry{ID: "sample-source", Capabilities: provider.Capabilities{provider.ResolvePlayback}})

	var lastRate float64 = -1
	clock := player.Status{HasFrame: true, Duration: 5, Position: 0}
	pb := NewPlayback(catalog, player.Engine{
		Open:     func(string, float64) bool { return true },
		Snapshot: func() player.Status { return clock },
		SetRate:  func(r float64) { lastRate = r },
		Seek:     func(pos float64) { clock.Position = pos },
	}, nil, post, func() {})
	defer pb.Close()

	ref := provider.MediaRef{EntryID: "sample-source", Kind: provider.Video, ID: "BV1Interact1"}
	pb.Start(provider.PlaybackTarget{Ref: ref}, "u1", false, nil)
	waitLoading(pb)
	if pb.State().Target.PartID != "cid-intro" || !pb.State().Resolution.Plan.Branch.AutoAdvance {
		t.Fatalf("expected intro auto-advance branch, got %+v", pb.State())
	}

	// 1. Intro reaches end -> auto-advances seamlessly to "start" without ending!
	// Also simulate a late Tick while loading (old clip reporting Ended + old Position)
	// and verify the new branch still starts cleanly at position=0 with Playing=true!
	clock.Position = 5
	clock.Ended = true
	pb.Tick(time.Now())
	pb.Tick(time.Now())
	waitLoading(pb)
	if pb.State().Target.PartID != "cid-start" || pb.State().BranchPending || pb.State().BranchHistory != 1 ||
		!pb.State().Playing || pb.State().Position != 0 {
		t.Fatalf("expected seamless auto-advance to cid-start playing from 0, got %+v", pb.State())
	}

	// 2. Playing "start" hits triggerSeconds (15s) -> pauses & sets BranchPending=true!
	clock = player.Status{HasFrame: true, Duration: 20, Position: 15.1}
	pb.Tick(time.Now())
	if !pb.State().BranchPending || pb.State().Playing || lastRate != 0 {
		t.Fatalf("expected pause at branch trigger, got pending=%t playing=%t rate=%v", pb.State().BranchPending, pb.State().Playing, lastRate)
	}

	// 3. DismissBranch clears overlay; seeking before trigger re-arms it!
	pb.DismissBranch()
	if pb.State().BranchPending || !pb.State().BranchDismissed {
		t.Fatalf("expected dismissed state, got %+v", pb.State())
	}
	pb.Seek(5)
	pb.Toggle() // resume playback
	clock.Position = 15.5
	pb.Tick(time.Now())
	if !pb.State().BranchPending {
		t.Fatal("expected seeking before trigger to re-arm branch prompt")
	}

	// 4. SelectBranch("end") transitions to leaf ending node with BranchState propagated!
	if !pb.SelectBranch("end") {
		t.Fatal("SelectBranch(end) failed")
	}
	waitLoading(pb)
	if pb.State().Target.PartID != "cid-end" || pb.State().BranchHistory != 2 || len(pb.State().Resolution.Plan.Branch.Options) != 0 {
		t.Fatalf("expected leaf ending node, got %+v", pb.State())
	}

	// 5. Leaf ending finishes -> Retry() resets to root intro (BranchHistory == 0)!
	clock = player.Status{HasFrame: true, Duration: 10, Position: 10, Ended: true}
	pb.Tick(time.Now())
	if pb.State().Playing || pb.State().BranchPending {
		t.Fatalf("expected leaf ending to stop cleanly, got %+v", pb.State())
	}
	pb.Retry()
	waitLoading(pb)
	if pb.State().Target.PartID != "cid-intro" || pb.State().BranchHistory != 0 {
		t.Fatalf("expected Retry on leaf ending to restart from root, got part=%q history=%d", pb.State().Target.PartID, pb.State().BranchHistory)
	}
}
