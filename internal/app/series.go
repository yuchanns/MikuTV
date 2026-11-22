package app

import (
	"context"
	"maps"
	"slices"

	"github.com/yuchanns/MikuTV/internal/provider"
)

type SeriesHubSnapshot struct {
	Phase            CatalogPhase
	Error            provider.ErrorCode
	Revision         uint64
	Loaded           bool
	Genre            string
	Genres           []provider.SeriesFilterOption
	Hero             *provider.SeriesEntry
	Items            []provider.SeriesEntry
	Tracked          []provider.SeriesEntry
	NextCursor       string
	Appending        bool
	AppendStalled    bool
	SupportsIndex    bool
	SupportsCalendar bool
}

func (s SeriesHubSnapshot) CanLoadMore() bool {
	return s.Phase == CatalogReady && !s.Appending && s.NextCursor != "" && !s.AppendStalled
}

type SeriesIndexSnapshot struct {
	Phase         CatalogPhase
	Error         provider.ErrorCode
	Revision      uint64
	FeedRevision  uint64
	Loaded        bool
	Filters       map[string]string
	FilterGroups  []provider.SeriesFilterGroup
	Items         []provider.SeriesEntry
	Total         *int64
	NextCursor    string
	Appending     bool
	AppendStalled bool
}

func (s SeriesIndexSnapshot) CanLoadMore() bool {
	return s.Phase == CatalogReady && !s.Appending && s.NextCursor != "" && !s.AppendStalled
}

func (s SeriesIndexSnapshot) DisplayTotal() int64 {
	if s.Total != nil {
		return *s.Total
	}
	return int64(len(s.Items))
}

type SeriesCalendarSnapshot struct {
	Phase    CatalogPhase
	Error    provider.ErrorCode
	Revision uint64
	Loaded   bool
	Days     []provider.SeriesCalendarDay
	Selected int
}

type pendingSeriesCall struct {
	gen    uint64
	cancel context.CancelFunc
}

func (p *pendingSeriesCall) stop() bool {
	if p.cancel == nil {
		return false
	}
	p.cancel()
	p.cancel = nil
	return true
}

// SeriesCatalog is owned by the UI thread. Provider calls run in workers and re-enter via post.
type SeriesCatalog struct {
	post    func(func())
	changed func()

	entryID   string
	entryGen  uint64
	owner     string
	canBrowse bool
	reader    provider.SeriesReader

	sessionGen   uint64
	hubCall      pendingSeriesCall
	indexCall    pendingSeriesCall
	calendarCall pendingSeriesCall
	hub          SeriesHubSnapshot
	index        SeriesIndexSnapshot
	calendar     SeriesCalendarSnapshot
}

func NewSeriesCatalog(post func(func()), changed func()) *SeriesCatalog {
	return &SeriesCatalog{post: post, changed: changed}
}

func (s *SeriesCatalog) Available() bool {
	return s != nil && s.canBrowse && s.reader != nil
}

func (s *SeriesCatalog) Hub() SeriesHubSnapshot {
	if s == nil {
		return SeriesHubSnapshot{}
	}
	return s.hub
}

func (s *SeriesCatalog) Index() SeriesIndexSnapshot {
	if s == nil {
		return SeriesIndexSnapshot{}
	}
	return s.index
}

func (s *SeriesCatalog) Calendar() SeriesCalendarSnapshot {
	if s == nil {
		return SeriesCalendarSnapshot{}
	}
	return s.calendar
}

func (s *SeriesCatalog) Bind(entry Entry, owner string, reader provider.SeriesReader) bool {
	if s == nil {
		return false
	}
	canBrowse := entry.ID != "" && entry.Capabilities.Has(provider.BrowseSeries) && reader != nil
	if s.entryID == entry.ID && s.entryGen == entry.Generation && s.owner == owner &&
		s.canBrowse == canBrowse && s.reader == reader {
		return false
	}
	s.sessionGen++
	s.cancelAll()
	s.entryID, s.entryGen, s.owner = entry.ID, entry.Generation, owner
	s.canBrowse, s.reader = canBrowse, reader
	s.hub = SeriesHubSnapshot{Revision: s.hub.Revision + 1}
	s.index = SeriesIndexSnapshot{Revision: s.index.Revision + 1, FeedRevision: s.index.FeedRevision + 1}
	s.calendar = SeriesCalendarSnapshot{Revision: s.calendar.Revision + 1}
	s.changed()
	return true
}

func (s *SeriesCatalog) EnsureHub() {
	if s == nil || !s.canBrowse || s.reader == nil {
		return
	}
	if s.hub.Phase == CatalogIdle {
		s.loadHub(s.hub.Genre, "", false)
	}
}

func (s *SeriesCatalog) SelectGenre(genre string) {
	if s == nil || !s.canBrowse || s.reader == nil {
		return
	}
	if s.hub.Genre == genre && s.hub.Phase != CatalogIdle && s.hub.Phase != CatalogFailed && s.hub.Phase != CatalogCanceled {
		return
	}
	s.hub.Genre = genre
	s.hub.Hero = nil
	s.hub.Items = nil
	s.hub.NextCursor = ""
	s.hub.AppendStalled = false
	s.hub.Revision++
	s.loadHub(genre, "", false)
}

func (s *SeriesCatalog) LoadMoreHub() {
	if s == nil || !s.canBrowse || s.reader == nil || !s.hub.CanLoadMore() {
		return
	}
	s.loadHub(s.hub.Genre, s.hub.NextCursor, true)
}

func (s *SeriesCatalog) RetryHub() {
	if s == nil || !s.canBrowse || s.reader == nil {
		return
	}
	if s.hub.Phase == CatalogFailed || s.hub.Phase == CatalogCanceled {
		cursor := ""
		if s.hub.Appending {
			cursor = s.hub.NextCursor
		}
		s.loadHub(s.hub.Genre, cursor, s.hub.Appending)
	}
}

func (s *SeriesCatalog) CancelHub() {
	if s == nil {
		return
	}
	if s.hubCall.stop() {
		s.hub.Phase = CatalogCanceled
		s.hub.Appending = false
		s.hub.Revision++
		s.changed()
	}
}

func (s *SeriesCatalog) EnsureIndex() {
	if s == nil || !s.canBrowse || s.reader == nil {
		return
	}
	if s.index.Phase == CatalogIdle {
		s.loadIndex("", false)
	}
}

func (s *SeriesCatalog) SetIndexFilter(key, value string) {
	if s == nil || !s.canBrowse || s.reader == nil || key == "" {
		return
	}
	next := maps.Clone(s.index.Filters)
	if next == nil {
		next = make(map[string]string)
	}
	if key == "season_type" && next[key] != value {
		// Switching top series type resets type-specific condition dimensions cleanly.
		order := next["order"]
		next = make(map[string]string)
		if value != "" {
			next[key] = value
		}
		if order != "" {
			next["order"] = order
		}
	} else {
		if value == "" {
			delete(next, key)
		} else {
			next[key] = value
		}
	}
	if maps.Equal(s.index.Filters, next) && s.index.Phase != CatalogIdle && s.index.Phase != CatalogFailed && s.index.Phase != CatalogCanceled {
		return
	}
	s.index.Filters = next
	s.index.Items = nil
	s.index.Total = nil
	s.index.NextCursor = ""
	s.index.AppendStalled = false
	s.index.FeedRevision++
	s.index.Revision++
	s.loadIndex("", false)
}

func (s *SeriesCatalog) ResetIndexFilters() {
	if s == nil || !s.canBrowse || s.reader == nil {
		return
	}
	if len(s.index.Filters) == 0 && s.index.Phase == CatalogReady {
		return
	}
	s.index.Filters = nil
	s.index.Items = nil
	s.index.Total = nil
	s.index.NextCursor = ""
	s.index.AppendStalled = false
	s.index.FeedRevision++
	s.index.Revision++
	s.loadIndex("", false)
}

func (s *SeriesCatalog) LoadMoreIndex() {
	if s == nil || !s.canBrowse || s.reader == nil || !s.index.CanLoadMore() {
		return
	}
	s.loadIndex(s.index.NextCursor, true)
}

func (s *SeriesCatalog) RetryIndex() {
	if s == nil || !s.canBrowse || s.reader == nil {
		return
	}
	if s.index.Phase == CatalogFailed || s.index.Phase == CatalogCanceled {
		cursor := ""
		if s.index.Appending {
			cursor = s.index.NextCursor
		}
		s.loadIndex(cursor, s.index.Appending)
	}
}

func (s *SeriesCatalog) CancelIndex() {
	if s == nil {
		return
	}
	if s.indexCall.stop() {
		s.index.Phase = CatalogCanceled
		s.index.Appending = false
		s.index.Revision++
		s.changed()
	}
}

func (s *SeriesCatalog) EnsureCalendar() {
	if s == nil || !s.canBrowse || s.reader == nil {
		return
	}
	if s.calendar.Phase == CatalogIdle {
		s.loadCalendar()
	}
}

func (s *SeriesCatalog) SelectCalendarDay(index int) {
	if s == nil || index < 0 || index >= len(s.calendar.Days) {
		return
	}
	if s.calendar.Selected == index {
		return
	}
	s.calendar.Selected = index
	s.calendar.Revision++
	s.changed()
}

func (s *SeriesCatalog) RetryCalendar() {
	if s == nil || !s.canBrowse || s.reader == nil {
		return
	}
	if s.calendar.Phase == CatalogFailed || s.calendar.Phase == CatalogCanceled {
		s.loadCalendar()
	}
}

func (s *SeriesCatalog) CancelCalendar() {
	if s == nil {
		return
	}
	if s.calendarCall.stop() {
		s.calendar.Phase = CatalogCanceled
		s.calendar.Revision++
		s.changed()
	}
}

func (s *SeriesCatalog) Close() {
	if s == nil {
		return
	}
	s.sessionGen++
	s.cancelAll()
	s.entryID, s.entryGen, s.owner = "", 0, ""
	s.canBrowse, s.reader = false, nil
	s.hub = SeriesHubSnapshot{Revision: s.hub.Revision + 1}
	s.index = SeriesIndexSnapshot{Revision: s.index.Revision + 1, FeedRevision: s.index.FeedRevision + 1}
	s.calendar = SeriesCalendarSnapshot{Revision: s.calendar.Revision + 1}
}

func (s *SeriesCatalog) loadHub(genre, cursor string, appendPage bool) {
	s.hubCall.stop()
	s.hubCall.gen++
	gen := s.hubCall.gen
	sessionGen := s.sessionGen
	reader := s.reader
	s.hub.Phase = CatalogLoading
	s.hub.Error = ""
	s.hub.Appending = appendPage
	s.hub.AppendStalled = false
	ctx, cancel := context.WithCancel(context.Background())
	s.hubCall.cancel = cancel
	s.changed()
	go func() {
		page, err := reader.ReadSeries(ctx, provider.SeriesRequest{
			Mode:   provider.SeriesModeHub,
			Genre:  genre,
			Cursor: cursor,
		})
		s.post(func() {
			if ctx.Err() != nil || s.sessionGen != sessionGen || s.hubCall.gen != gen {
				return
			}
			cancel()
			s.hubCall.cancel = nil
			if err != nil {
				s.hub.Phase = CatalogFailed
				s.hub.Error = provider.CodeOf(err)
				s.hub.Appending = false
			} else {
				genres := s.hub.Genres
				if len(page.Genres) != 0 {
					genres = slices.Clone(page.Genres)
				}
				tracked := s.hub.Tracked
				if !appendPage {
					tracked = dedupSeriesEntries(nil, page.Tracked)
				}
				hero := s.hub.Hero
				if !appendPage {
					hero = page.Hero
				}
				var items []provider.SeriesEntry
				var added bool
				if appendPage {
					before := len(s.hub.Items)
					items = dedupSeriesEntries(s.hub.Items, page.Items)
					added = len(items) > before
				} else {
					items = dedupSeriesEntries(nil, page.Items)
					added = len(items) > 0
				}
				nextCursor := page.NextCursor
				if nextCursor == cursor || (appendPage && !added) {
					nextCursor = ""
				}
				s.hub = SeriesHubSnapshot{
					Phase:            CatalogReady,
					Revision:         s.hub.Revision + 1,
					Loaded:           true,
					Genre:            genre,
					Genres:           genres,
					Hero:             hero,
					Items:            items,
					Tracked:          tracked,
					NextCursor:       nextCursor,
					AppendStalled:    appendPage && !added,
					SupportsIndex:    page.SupportsIndex,
					SupportsCalendar: page.SupportsCalendar,
				}
			}
			s.changed()
		})
	}()
}

func (s *SeriesCatalog) loadIndex(cursor string, appendPage bool) {
	s.indexCall.stop()
	s.indexCall.gen++
	gen := s.indexCall.gen
	sessionGen := s.sessionGen
	reader := s.reader
	filters := maps.Clone(s.index.Filters)
	s.index.Phase = CatalogLoading
	s.index.Error = ""
	s.index.Appending = appendPage
	s.index.AppendStalled = false
	ctx, cancel := context.WithCancel(context.Background())
	s.indexCall.cancel = cancel
	s.changed()
	go func() {
		page, err := reader.ReadSeries(ctx, provider.SeriesRequest{
			Mode:    provider.SeriesModeIndex,
			Filters: filters,
			Cursor:  cursor,
		})
		s.post(func() {
			if ctx.Err() != nil || s.sessionGen != sessionGen || s.indexCall.gen != gen {
				return
			}
			cancel()
			s.indexCall.cancel = nil
			if err != nil {
				s.index.Phase = CatalogFailed
				s.index.Error = provider.CodeOf(err)
				s.index.Appending = false
			} else {
				groups := s.index.FilterGroups
				if len(page.FilterGroups) != 0 {
					groups = slices.Clone(page.FilterGroups)
				}
				var items []provider.SeriesEntry
				var added bool
				if appendPage {
					before := len(s.index.Items)
					items = dedupSeriesEntries(s.index.Items, page.Items)
					added = len(items) > before
				} else {
					items = dedupSeriesEntries(nil, page.Items)
					added = len(items) > 0
				}
				nextCursor := page.NextCursor
				if nextCursor == cursor || (appendPage && !added) {
					nextCursor = ""
				}
				total := page.Total
				if total == nil {
					total = s.index.Total
				}
				s.index = SeriesIndexSnapshot{
					Phase:         CatalogReady,
					Revision:      s.index.Revision + 1,
					FeedRevision:  s.index.FeedRevision,
					Loaded:        true,
					Filters:       filters,
					FilterGroups:  groups,
					Items:         items,
					Total:         total,
					NextCursor:    nextCursor,
					AppendStalled: appendPage && !added,
				}
			}
			s.changed()
		})
	}()
}

func (s *SeriesCatalog) loadCalendar() {
	s.calendarCall.stop()
	s.calendarCall.gen++
	gen := s.calendarCall.gen
	sessionGen := s.sessionGen
	reader := s.reader
	s.calendar.Phase = CatalogLoading
	s.calendar.Error = ""
	ctx, cancel := context.WithCancel(context.Background())
	s.calendarCall.cancel = cancel
	s.changed()
	go func() {
		page, err := reader.ReadSeries(ctx, provider.SeriesRequest{
			Mode: provider.SeriesModeCalendar,
		})
		s.post(func() {
			if ctx.Err() != nil || s.sessionGen != sessionGen || s.calendarCall.gen != gen {
				return
			}
			cancel()
			s.calendarCall.cancel = nil
			if err != nil {
				s.calendar.Phase = CatalogFailed
				s.calendar.Error = provider.CodeOf(err)
			} else {
				days := slices.Clone(page.Days)
				selected := s.calendar.Selected
				if !s.calendar.Loaded || selected >= len(days) {
					selected = 0
					for i, day := range days {
						if day.IsToday {
							selected = i
							break
						}
					}
				}
				s.calendar = SeriesCalendarSnapshot{
					Phase:    CatalogReady,
					Revision: s.calendar.Revision + 1,
					Loaded:   true,
					Days:     days,
					Selected: selected,
				}
			}
			s.changed()
		})
	}()
}

func (s *SeriesCatalog) ApplyTracked(entry provider.SeriesEntry, tracked bool) {
	if s == nil || entry.Item.Ref.ID == "" {
		return
	}
	ref := entry.Item.Ref
	hubChanged, indexChanged, calChanged := false, false, false

	if s.hub.Hero != nil && s.hub.Hero.Item.Ref == ref && s.hub.Hero.Tracked != tracked {
		hero := *s.hub.Hero
		hero.Tracked = tracked
		s.hub.Hero = &hero
		hubChanged = true
	}
	for i := range s.hub.Items {
		if s.hub.Items[i].Item.Ref == ref && s.hub.Items[i].Tracked != tracked {
			s.hub.Items[i].Tracked = tracked
			hubChanged = true
		}
	}
	if tracked {
		idx := slices.IndexFunc(s.hub.Tracked, func(existing provider.SeriesEntry) bool {
			return existing.Item.Ref == ref
		})
		if idx >= 0 {
			if !s.hub.Tracked[idx].Tracked {
				s.hub.Tracked[idx].Tracked = true
				hubChanged = true
			}
		} else if s.hub.Loaded && entry.Item.Title != "" {
			added := entry
			added.Tracked = true
			s.hub.Tracked = append([]provider.SeriesEntry{added}, s.hub.Tracked...)
			hubChanged = true
		}
	} else {
		before := len(s.hub.Tracked)
		s.hub.Tracked = slices.DeleteFunc(slices.Clone(s.hub.Tracked), func(existing provider.SeriesEntry) bool {
			return existing.Item.Ref == ref
		})
		if len(s.hub.Tracked) != before {
			hubChanged = true
		}
	}

	for i := range s.index.Items {
		if s.index.Items[i].Item.Ref == ref && s.index.Items[i].Tracked != tracked {
			s.index.Items[i].Tracked = tracked
			indexChanged = true
		}
	}

	for d := range s.calendar.Days {
		for e := range s.calendar.Days[d].Entries {
			if s.calendar.Days[d].Entries[e].Entry.Item.Ref == ref && s.calendar.Days[d].Entries[e].Entry.Tracked != tracked {
				s.calendar.Days[d].Entries[e].Entry.Tracked = tracked
				calChanged = true
			}
		}
	}

	if hubChanged {
		s.hub.Revision++
	}
	if indexChanged {
		s.index.Revision++
	}
	if calChanged {
		s.calendar.Revision++
	}
	if hubChanged || indexChanged || calChanged {
		s.changed()
	}
}

func (s *SeriesCatalog) cancelAll() {
	s.hubCall.stop()
	s.indexCall.stop()
	s.calendarCall.stop()
}

func dedupSeriesEntries(existing, incoming []provider.SeriesEntry) []provider.SeriesEntry {
	out := slices.Clone(existing)
	seen := make(map[provider.MediaRef]struct{}, len(out)+len(incoming))
	for _, entry := range out {
		seen[entry.Item.Ref] = struct{}{}
	}
	for _, entry := range incoming {
		if _, dup := seen[entry.Item.Ref]; dup {
			continue
		}
		seen[entry.Item.Ref] = struct{}{}
		out = append(out, entry)
	}
	return out
}
