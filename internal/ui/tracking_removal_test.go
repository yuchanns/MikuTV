package tvui

import (
	"context"
	"errors"
	"testing"

	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/provider"
	"github.com/yuchanns/MikuTV/internal/theme"
	"github.com/yuchanns/MikuTV/internal/topshelf"
)

type trackingRemoveStub struct {
	removed []string
}

func (s *trackingRemoveStub) ReadAccount(_ context.Context, _ provider.AccountRequest) (provider.AccountPage, error) {
	return provider.AccountPage{
		Rows: []provider.AccountRow{
			{
				ID:        "row-1",
				Title:     "Example Series",
				RemovalID: "rm-42",
				CanDelete: true,
				Item: &provider.MediaItem{
					Ref:   provider.MediaRef{EntryID: "test-source", Kind: provider.Series, ID: "series-42"},
					Title: "Example Series",
				},
			},
		},
	}, nil
}

func (s *trackingRemoveStub) RemoveAccountItems(_ context.Context, _ provider.AccountSection, resourceIDs []string) error {
	s.removed = append(s.removed, resourceIDs...)
	return nil
}

func TestRemoveLibraryTrackingPreservesSourceMediaRef(t *testing.T) {
	assets, err := LoadAssets()
	if err != nil {
		t.Fatal(err)
	}
	pages, err := NewPages(assets)
	if err != nil {
		t.Fatal(err)
	}
	entries := testEntries()
	entries[0].Capabilities = append(entries[0].Capabilities, provider.AccountTracking.Capability(), provider.EditTracking)
	state, err := app.NewState(entries, entries[0].ID, pages.Routes)
	if err != nil {
		t.Fatal(err)
	}

	view := newTestView(t, Options{State: state, Pages: pages, Assets: assets, Theme: theme.Default()})
	stub := &trackingRemoveStub{}
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
	view.Activity = app.NewAccountActivity(nil, func(fn func()) { queue <- fn }, func() {})
	view.Activity.Bind(entries[0], "u1", stub)

	query := provider.AccountQuery{Section: provider.AccountTracking}
	view.Activity.Ensure(query)
	for len(view.Activity.Snapshot(query).Page.Rows) == 0 {
		drain()
	}

	// Cached detail holds non-"ss" MediaRef ID "series-42" marked tracked.
	seriesRef := provider.MediaRef{EntryID: entries[0].ID, Kind: provider.Series, ID: "series-42"}
	view.videoDetail.item = provider.MediaItem{
		Ref:          seriesRef,
		Title:        "Example Series",
		VideoDetails: &provider.VideoDetails{Engagement: provider.VideoEngagement{Tracked: true}},
	}
	view.favorites.management = favoriteManagement{query: query, picked: map[string]bool{"rm-42": true}}

	rows := view.Activity.Snapshot(query).Page.Rows
	p := view.context(state.Location, true, 1920, 1080)
	p.removeLibraryItems(query, rows, []string{"rm-42"})
	for view.favorites.management.pending {
		drain()
	}

	if len(stub.removed) != 1 || stub.removed[0] != "rm-42" {
		t.Fatalf("removed = %v, want [rm-42]", stub.removed)
	}
	if view.videoDetail.item.VideoDetails.Engagement.Tracked {
		t.Fatal("cached detail for non-ss series ref was not marked untracked after tracking removal")
	}
}

type flakyTopShelfStore struct {
	failNext bool
	saves    int
	notifies int
	snapshot topshelf.Snapshot
}

func (f *flakyTopShelfStore) Load() (topshelf.Snapshot, error) {
	return f.snapshot, errors.New("not initialized")
}

func (f *flakyTopShelfStore) Save(s topshelf.Snapshot) error {
	f.saves++
	if f.failNext {
		f.failNext = false
		return errors.New("temporary storage failure")
	}
	f.snapshot = s
	return nil
}

func (f *flakyTopShelfStore) NotifyChanged() {
	f.notifies++
}

func TestTopShelfRetriesAfterTransientSaveFailure(t *testing.T) {
	assets, err := LoadAssets()
	if err != nil {
		t.Fatal(err)
	}
	pages, err := NewPages(assets)
	if err != nil {
		t.Fatal(err)
	}
	state, err := app.NewState(testEntries(), "test-source", pages.Routes)
	if err != nil {
		t.Fatal(err)
	}
	store := &flakyTopShelfStore{failNext: true}
	view := newTestView(t, Options{State: state, Pages: pages, Assets: assets, Theme: theme.Default(), TopShelf: store})

	snap := topshelf.Snapshot{Version: topshelf.SnapshotVersion, EntryID: "test-source", EntryName: "Test Source"}

	// 1st save fails temporarily -> must NOT cache hash as published.
	view.publishTopShelf(snap)
	if store.saves != 1 || store.notifies != 0 {
		t.Fatalf("after failed save: saves=%d notifies=%d, want 1/0", store.saves, store.notifies)
	}

	// 2nd save with identical content retries and succeeds.
	view.publishTopShelf(snap)
	if store.saves != 2 || store.notifies != 1 {
		t.Fatalf("after retry save: saves=%d notifies=%d, want 2/1", store.saves, store.notifies)
	}

	// 3rd call with identical content is deduplicated once successfully saved.
	view.publishTopShelf(snap)
	if store.saves != 2 || store.notifies != 1 {
		t.Fatalf("after cached sync: saves=%d notifies=%d, want 2/1", store.saves, store.notifies)
	}
}

func TestVideoCommentActionsAuthorizeWithWriteVideoCommentsOnly(t *testing.T) {
	assets, err := LoadAssets()
	if err != nil {
		t.Fatal(err)
	}
	pages, err := NewPages(assets)
	if err != nil {
		t.Fatal(err)
	}
	entries := []app.Entry{{
		ID:           "comments-only",
		SourceID:     "comments-only",
		Name:         "Comments Only",
		Capabilities: provider.Capabilities{provider.BrowseCatalog, provider.SignIn, provider.WriteVideoComments},
	}}
	state, err := app.NewState(entries, entries[0].ID, pages.Routes)
	if err != nil {
		t.Fatal(err)
	}
	view := newTestView(t, Options{State: state, Pages: pages, Assets: assets, Theme: theme.Default()})
	p := view.context(state.Location, true, 1920, 1080)

	// Without EditVideo, calling withVideoAccountFor(WriteVideoComments) must NOT reject as unavailable;
	// since signed out, it proceeds to open sign-in (or runs action if signed in).
	p.withVideoAccountFor(provider.WriteVideoComments, assets.Text.Detail.WriteComment, func() {})
	if view.videoDetail.feedback.text != "" {
		t.Fatalf("unexpected unavailable feedback for WriteVideoComments-only source: %q", view.videoDetail.feedback.text)
	}
}
