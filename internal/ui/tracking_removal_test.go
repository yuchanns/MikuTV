package tvui

import (
	"context"
	"testing"

	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/provider"
	"github.com/yuchanns/MikuTV/internal/theme"
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
