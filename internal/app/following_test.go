package app

import (
	"context"
	"sync"
	"testing"

	"github.com/yuchanns/MikuTV/internal/provider"
)

type stubFollowingReader struct {
	mu    sync.Mutex
	calls []provider.FollowingRequest
	fn    func(context.Context, provider.FollowingRequest) (provider.FollowingPage, error)
}

func (r *stubFollowingReader) ReadFollowing(ctx context.Context, req provider.FollowingRequest) (provider.FollowingPage, error) {
	r.mu.Lock()
	r.calls = append(r.calls, req)
	fn := r.fn
	r.mu.Unlock()
	return fn(ctx, req)
}

func TestFollowingFeedIsolationPaginationAndSync(t *testing.T) {
	queue := make(chan func(), 16)
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
	feed := NewFollowingFeed(func(fn func()) { queue <- fn }, func() {})
	defer feed.Close()

	entry := Entry{
		ID:           "sample-source",
		Capabilities: provider.Capabilities{provider.BrowseFollowing, provider.SignIn},
	}

	// Gate for first account's overview request so we can switch account mid-flight.
	releaseU1 := make(chan struct{})
	u1Called := make(chan struct{}, 1)
	reader1 := &stubFollowingReader{
		fn: func(ctx context.Context, req provider.FollowingRequest) (provider.FollowingPage, error) {
			u1Called <- struct{}{}
			<-releaseU1
			return provider.FollowingPage{
				Creators: []provider.FollowingCreator{{Creator: provider.Creator{ID: "u1-c1", Name: "Account1Creator"}}},
			}, nil
		},
	}

	feed.Bind(entry, "u1", reader1)
	feed.EnsureLoaded()
	<-u1Called

	// Switch to account u2 before u1 completes; u1's late callback must be ignored.
	reader2 := &stubFollowingReader{
		fn: func(ctx context.Context, req provider.FollowingRequest) (provider.FollowingPage, error) {
			switch req.Section {
			case provider.FollowingOverview:
				return provider.FollowingPage{
					SupportsLive:   true,
					Creators:       []provider.FollowingCreator{{Creator: provider.Creator{ID: "c1", Name: "Creator 1"}}},
					CreatorsCursor: "2",
					Updates: []provider.MediaItem{{
						Ref: provider.MediaRef{EntryID: "sample-source", Kind: provider.Video, ID: "v1"},
						Title: "Video 1", Creator: &provider.Creator{ID: "c1", Name: "Creator 1"},
					}},
					UpdatesCursor: "2:off",
					LiveRooms: []provider.MediaItem{{
						Ref: provider.MediaRef{EntryID: "sample-source", Kind: provider.Live, ID: "l1"},
						Title: "Live 1", Creator: &provider.Creator{ID: "c1", Name: "Creator 1"},
					}},
					LiveCursor: "2",
				}, nil
			case provider.FollowingCreators:
				if req.Cursor == "2" {
					return provider.FollowingPage{
						Creators: []provider.FollowingCreator{
							{Creator: provider.Creator{ID: "c1", Name: "Duplicate"}},
							{Creator: provider.Creator{ID: "c2", Name: "Creator 2"}},
						},
						CreatorsCursor: "3",
					}, nil
				}
				// Page 3 stalls with only duplicates.
				return provider.FollowingPage{
					Creators:       []provider.FollowingCreator{{Creator: provider.Creator{ID: "c2", Name: "Duplicate"}}},
					CreatorsCursor: "4",
				}, nil
			case provider.FollowingUpdates:
				return provider.FollowingPage{
					Updates: []provider.MediaItem{{
						Ref: provider.MediaRef{EntryID: "sample-source", Kind: provider.Video, ID: "v2"},
						Title: "Video 2", Creator: &provider.Creator{ID: "c2", Name: "Creator 2"},
					}},
				}, nil
			case provider.FollowingLive:
				return provider.FollowingPage{
					LiveRooms: []provider.MediaItem{{
						Ref: provider.MediaRef{EntryID: "sample-source", Kind: provider.Live, ID: "l2"},
						Title: "Live 2", Creator: &provider.Creator{ID: "c2", Name: "Creator 2"},
					}},
				}, nil
			default:
				return provider.FollowingPage{}, nil
			}
		},
	}
	feed.Bind(entry, "u2", reader2)
	close(releaseU1)
	feed.EnsureLoaded()

	// Wait for u2's callback (and drain any stale u1 callback).
	for feed.Snapshot().Phase != CatalogReady {
		(<-queue)()
	}
	drain()

	snap := feed.Snapshot()
	if len(snap.Creators) != 1 || snap.Creators[0].Creator.ID != "c1" {
		t.Fatalf("account isolation failed, got creators: %+v", snap.Creators)
	}

	// Paginate creators, updates, live.
	feed.LoadMoreCreators()
	for feed.Snapshot().CreatorsLoading {
		(<-queue)()
	}
	if len(feed.Snapshot().Creators) != 2 || feed.Snapshot().Creators[1].Creator.ID != "c2" {
		t.Fatalf("expected 2 deduped creators, got %+v", feed.Snapshot().Creators)
	}

	// Next creators page returns only duplicates -> must stall and clear cursor.
	feed.LoadMoreCreators()
	for feed.Snapshot().CreatorsLoading {
		(<-queue)()
	}
	if !feed.Snapshot().CreatorsStalled || feed.Snapshot().CanLoadMoreCreators() {
		t.Fatalf("expected stalled creators pagination, got %+v", feed.Snapshot())
	}

	feed.LoadMoreUpdates()
	for feed.Snapshot().UpdatesLoading {
		(<-queue)()
	}
	if len(feed.Snapshot().Updates) != 2 {
		t.Fatalf("expected 2 updates, got %+v", feed.Snapshot().Updates)
	}

	feed.LoadMoreLive()
	for feed.Snapshot().LiveLoading {
		(<-queue)()
	}
	if len(feed.Snapshot().LiveRooms) != 2 {
		t.Fatalf("expected 2 live rooms, got %+v", feed.Snapshot().LiveRooms)
	}

	// Unfollow c1 -> removes c1 from Creators, Updates, and LiveRooms immediately.
	feed.SyncFollow(provider.Creator{ID: "c1", Name: "Creator 1"}, false)
	snap = feed.Snapshot()
	if len(snap.Creators) != 1 || snap.Creators[0].Creator.ID != "c2" ||
		len(snap.Updates) != 1 || snap.Updates[0].Ref.ID != "v2" ||
		len(snap.LiveRooms) != 1 || snap.LiveRooms[0].Ref.ID != "l2" {
		t.Fatalf("unfollow sync did not prune c1 cleanly: %+v", snap)
	}

	// Follow c3 -> prepends immediately and marks stale so next EnsureLoaded refreshes.
	feed.SyncFollow(provider.Creator{ID: "c3", Name: "Creator 3"}, true)
	snap = feed.Snapshot()
	if len(snap.Creators) != 2 || snap.Creators[0].Creator.ID != "c3" || !snap.Creators[0].Creator.Following {
		t.Fatalf("follow sync did not prepend c3: %+v", snap.Creators)
	}
	prevCalls := len(reader2.calls)
	feed.EnsureLoaded()
	for feed.Snapshot().Phase == CatalogLoading {
		(<-queue)()
	}
	if len(reader2.calls) <= prevCalls {
		t.Fatal("expected EnsureLoaded to refresh stale feed after new follow")
	}

	// Sign out -> clears everything cleanly.
	feed.Bind(entry, "", reader2)
	if feed.SignedIn() || len(feed.Snapshot().Creators) != 0 || feed.Snapshot().Loaded {
		t.Fatalf("sign-out did not isolate/reset FollowingFeed: %+v", feed.Snapshot())
	}
}
