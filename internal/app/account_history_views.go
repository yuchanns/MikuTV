package app

import (
	"context"

	"github.com/yuchanns/MikuTV/internal/provider"
)

const historyViewWorkers = 2

type historyViews struct {
	ctx     context.Context
	cancel  context.CancelFunc
	source  provider.Catalog
	wanted  []provider.MediaRef
	items   map[provider.MediaRef]historyMetadata
	pending int
}

type historyMetadata struct {
	views   *int64
	quality string
}

// PrepareHistoryViews fills in public video metadata for the library viewport.
// The account history request remains independent of these lookups.
func (a *AccountActivity) PrepareHistoryViews(rows []provider.AccountRow) {
	if a.historyViews == nil {
		source, ok := a.reader.(provider.Catalog)
		if !ok || !a.Available(provider.AccountHistory) || !a.entry.Capabilities.Has(provider.GetCatalogItem) {
			return
		}
		ctx, cancel := context.WithCancel(context.Background())
		a.historyViews = &historyViews{ctx: ctx, cancel: cancel, source: source, items: make(map[provider.MediaRef]historyMetadata)}
	}
	views := a.historyViews
	views.wanted = views.wanted[:0]
	for _, row := range rows {
		ref := row.Ref
		if row.Views == nil && ref != nil && ref.EntryID == a.entry.ID && ref.Kind.Valid() {
			views.wanted = append(views.wanted, *ref)
		}
	}
	a.loadHistoryViews(views)
}

func (a *AccountActivity) HistoryMetadata(ref provider.MediaRef) (views *int64, quality string) {
	if a.historyViews == nil {
		return nil, ""
	}
	item := a.historyViews.items[ref]
	return item.views, item.quality
}

func (a *AccountActivity) PauseHistoryViews() {
	if a.historyViews != nil {
		a.historyViews.wanted = a.historyViews.wanted[:0]
	}
}

func (a *AccountActivity) loadHistoryViews(views *historyViews) {
	for _, ref := range views.wanted {
		if views.pending >= historyViewWorkers {
			return
		}
		if _, exists := views.items[ref]; exists {
			continue
		}
		views.items[ref] = historyMetadata{}
		views.pending++
		go func() {
			item, err := views.source.Item(views.ctx, ref)
			a.post(func() {
				if views.ctx.Err() != nil {
					return
				}
				views.pending--
				if err == nil && item.Ref == ref {
					if item.Views != nil && *item.Views < 0 {
						item.Views = nil
					}
					if item.Views != nil || item.Quality != "" {
						views.items[ref] = historyMetadata{views: item.Views, quality: item.Quality}
						a.changed()
					}
				}
				a.loadHistoryViews(views)
			})
		}()
	}
}
