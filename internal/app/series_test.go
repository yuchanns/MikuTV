package app

import (
	"context"
	"sync"
	"testing"

	"github.com/yuchanns/MikuTV/internal/provider"
)

type stubSeriesReader struct {
	mu    sync.Mutex
	calls []provider.SeriesRequest
	fn    func(context.Context, provider.SeriesRequest) (provider.SeriesPage, error)
}

func (r *stubSeriesReader) ReadSeries(ctx context.Context, req provider.SeriesRequest) (provider.SeriesPage, error) {
	r.mu.Lock()
	r.calls = append(r.calls, req)
	fn := r.fn
	r.mu.Unlock()
	return fn(ctx, req)
}

func TestSeriesCatalogFiltersPaginationCalendarAndIsolation(t *testing.T) {
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

	catalog := NewSeriesCatalog(func(fn func()) { queue <- fn }, func() {})
	defer catalog.Close()

	entry := Entry{
		ID:           "sample-source",
		Capabilities: provider.Capabilities{provider.BrowseSeries},
	}

	total := int64(25)
	reader := &stubSeriesReader{
		fn: func(ctx context.Context, req provider.SeriesRequest) (provider.SeriesPage, error) {
			switch req.Mode {
			case provider.SeriesModeHub:
				return provider.SeriesPage{
					Genres: []provider.SeriesFilterOption{{Value: "", Label: "All"}, {Value: "1", Label: "Anime"}},
					Hero:   &provider.SeriesEntry{Item: provider.MediaItem{Ref: provider.MediaRef{EntryID: "sample-source", Kind: provider.Series, ID: "ss1"}, Title: "Hero Show"}},
					Items:  []provider.SeriesEntry{{Item: provider.MediaItem{Ref: provider.MediaRef{EntryID: "sample-source", Kind: provider.Series, ID: "ss1"}, Title: "Hero Show"}}},
					SupportsIndex: true, SupportsCalendar: true,
				}, nil
			case provider.SeriesModeIndex:
				if req.Cursor == "2" {
					return provider.SeriesPage{
						Items: []provider.SeriesEntry{
							{Item: provider.MediaItem{Ref: provider.MediaRef{EntryID: "sample-source", Kind: provider.Series, ID: "ss1"}, Title: "Dup"}},
							{Item: provider.MediaItem{Ref: provider.MediaRef{EntryID: "sample-source", Kind: provider.Series, ID: "ss2"}, Title: "Page 2 Show"}},
						},
					}, nil
				}
				return provider.SeriesPage{
					FilterGroups: []provider.SeriesFilterGroup{
						{Key: "season_type", Label: "Type", Options: []provider.SeriesFilterOption{{Value: "1", Label: "Anime"}, {Value: "3", Label: "Doc"}}},
						{Key: "area", Label: "Region", Options: []provider.SeriesFilterOption{{Value: "", Label: "All"}, {Value: "2", Label: "Japan"}}},
					},
					Items:      []provider.SeriesEntry{{Item: provider.MediaItem{Ref: provider.MediaRef{EntryID: "sample-source", Kind: provider.Series, ID: "ss1"}, Title: "Page 1 Show"}}},
					Total:      &total,
					NextCursor: "2",
				}, nil
			case provider.SeriesModeCalendar:
				return provider.SeriesPage{
					Days: []provider.SeriesCalendarDay{
						{Date: "11-22", Month: 11, Day: 22, Weekday: 0, IsToday: true, Entries: []provider.SeriesScheduleEntry{{Time: "18:00", Entry: provider.SeriesEntry{Item: provider.MediaItem{Ref: provider.MediaRef{EntryID: "sample-source", Kind: provider.Series, ID: "ss1"}, Title: "Today Show"}}}}},
						{Date: "11-23", Month: 11, Day: 23, Weekday: 1},
					},
				}, nil
			default:
				return provider.SeriesPage{}, nil
			}
		},
	}

	catalog.Bind(entry, "u1", reader)
	catalog.EnsureHub()
	for !catalog.Hub().Loaded {
		(<-queue)()
	}
	if catalog.Hub().Hero == nil || !catalog.Hub().SupportsIndex || !catalog.Hub().SupportsCalendar {
		t.Fatalf("unexpected hub snapshot: %+v", catalog.Hub())
	}

	// Hub genre change must not silently skew SeriesIndex away from its active filter chips.
	catalog.SelectGenre("1")
	for catalog.Hub().Phase == CatalogLoading {
		(<-queue)()
	}

	// Index initial load + filter combination + pagination dedup + reset.
	catalog.EnsureIndex()
	for !catalog.Index().Loaded {
		(<-queue)()
	}
	reader.mu.Lock()
	var indexReq provider.SeriesRequest
	for _, call := range reader.calls {
		if call.Mode == provider.SeriesModeIndex {
			indexReq = call
			break
		}
	}
	reader.mu.Unlock()
	if indexReq.Genre != "" {
		t.Fatalf("expected SeriesIndex request to stay independent of hub genre, got Genre=%q", indexReq.Genre)
	}
	catalog.SetIndexFilter("area", "2")
	for catalog.Index().Phase == CatalogLoading {
		(<-queue)()
	}
	if catalog.Index().Filters["area"] != "2" {
		t.Fatalf("expected area filter=2, got %+v", catalog.Index().Filters)
	}
	catalog.LoadMoreIndex()
	for catalog.Index().Appending {
		(<-queue)()
	}
	if len(catalog.Index().Items) != 2 || catalog.Index().Items[1].Item.Ref.ID != "ss2" {
		t.Fatalf("expected 2 deduped index items, got %+v", catalog.Index().Items)
	}
	// Switching season_type resets type-specific filters like area.
	catalog.SetIndexFilter("season_type", "3")
	for catalog.Index().Phase == CatalogLoading {
		(<-queue)()
	}
	if catalog.Index().Filters["season_type"] != "3" || catalog.Index().Filters["area"] != "" {
		t.Fatalf("expected season_type change to clear area filter, got %+v", catalog.Index().Filters)
	}
	catalog.ResetIndexFilters()
	for catalog.Index().Phase == CatalogLoading {
		(<-queue)()
	}
	if len(catalog.Index().Filters) != 0 {
		t.Fatalf("expected empty filters after reset, got %+v", catalog.Index().Filters)
	}

	// Calendar load + day selection.
	catalog.EnsureCalendar()
	for !catalog.Calendar().Loaded {
		(<-queue)()
	}
	if len(catalog.Calendar().Days) != 2 || catalog.Calendar().Selected != 0 {
		t.Fatalf("unexpected calendar: %+v", catalog.Calendar())
	}
	catalog.SelectCalendarDay(1)
	if catalog.Calendar().Selected != 1 {
		t.Fatalf("expected selected day 1, got %d", catalog.Calendar().Selected)
	}

	// ApplyTracked syncs Hub Hero, Hub Items, Hub Tracked shelf, Index Items, and Calendar Days!
	target := catalog.Hub().Hero
	if target == nil || target.Tracked {
		t.Fatalf("expected untracked hub hero, got %+v", target)
	}
	catalog.ApplyTracked(*target, true)
	if !catalog.Hub().Hero.Tracked || len(catalog.Hub().Tracked) != 1 || catalog.Hub().Tracked[0].Item.Ref != target.Item.Ref {
		t.Fatalf("ApplyTracked(true) did not sync Hub hero & tracked shelf: hero=%+v tracked=%+v", catalog.Hub().Hero, catalog.Hub().Tracked)
	}
	if !catalog.Index().Items[0].Tracked || !catalog.Calendar().Days[0].Entries[0].Entry.Tracked {
		t.Fatalf("ApplyTracked(true) did not sync Index & Calendar: idx=%+v cal=%+v", catalog.Index().Items[0], catalog.Calendar().Days[0].Entries[0])
	}
	catalog.ApplyTracked(*target, false)
	if catalog.Hub().Hero.Tracked || len(catalog.Hub().Tracked) != 0 || catalog.Index().Items[0].Tracked || catalog.Calendar().Days[0].Entries[0].Entry.Tracked {
		t.Fatalf("ApplyTracked(false) did not clear tracked state across all series views")
	}

	// Account / entry switch isolates cleanly.
	catalog.Bind(entry, "u2", reader)
	drain()
	if catalog.Hub().Loaded || catalog.Index().Loaded || catalog.Calendar().Loaded {
		t.Fatal("expected account switch to reset series snapshots")
	}
}
