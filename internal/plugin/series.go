package plugin

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/yuchanns/MikuTV/internal/provider"
)

const (
	maxSeriesFilters       = 16
	maxSeriesFilterGroups  = 12
	maxSeriesFilterOptions = 64
	maxSeriesCalendarDays  = 14
)

func (s *Session) ReadSeries(ctx context.Context, request provider.SeriesRequest) (provider.SeriesPage, error) {
	if !request.Mode.Valid() || len(request.Genre) > maxSeriesFilterOptions ||
		len(request.Cursor) > maxSearchCursorLength || len(request.Filters) > maxSeriesFilters {
		return provider.SeriesPage{}, &provider.Error{Code: provider.Denied}
	}
	for k, v := range request.Filters {
		if strings.TrimSpace(k) == "" || len(k) > 128 || len(v) > 128 {
			return provider.SeriesPage{}, &provider.Error{Code: provider.Denied}
		}
	}
	auth := s.snapshotAuth()
	input := struct {
		provider.SeriesRequest
		SignedIn bool   `json:"signedIn,omitempty"`
		Owner    string `json:"owner,omitempty"`
	}{request, auth.signedIn(), auth.owner}
	var page provider.SeriesPage
	if err := s.invoke(ctx, auth.scope, auth.client, provider.BrowseSeries, "readSeries", input, &page); err != nil {
		return provider.SeriesPage{}, err
	}
	if len(page.NextCursor) > maxSearchCursorLength || (page.Total != nil && *page.Total < 0) {
		return provider.SeriesPage{}, &provider.Error{Code: provider.PluginFailed}
	}
	page.Genres = sanitizeSeriesOptions(page.Genres, maxSeriesFilterOptions)
	page.FilterGroups = sanitizeSeriesFilterGroups(page.FilterGroups)
	if page.Hero != nil {
		if err := s.sanitizeSeriesEntry(page.Hero); err != nil {
			return provider.SeriesPage{}, err
		}
	}
	for i := range page.Items {
		if err := s.sanitizeSeriesEntry(&page.Items[i]); err != nil {
			return provider.SeriesPage{}, err
		}
	}
	for i := range page.Tracked {
		if err := s.sanitizeSeriesEntry(&page.Tracked[i]); err != nil {
			return provider.SeriesPage{}, err
		}
	}
	if len(page.Days) > maxSeriesCalendarDays {
		page.Days = page.Days[:maxSeriesCalendarDays]
	}
	for i := range page.Days {
		day := &page.Days[i]
		if strings.TrimSpace(day.Date) == "" || day.Month < 1 || day.Month > 12 ||
			day.Day < 1 || day.Day > 31 || day.Weekday < 0 || day.Weekday > 6 {
			return provider.SeriesPage{}, &provider.Error{Code: provider.PluginFailed}
		}
		for j := range day.Entries {
			if err := s.sanitizeSeriesEntry(&day.Entries[j].Entry); err != nil {
				return provider.SeriesPage{}, err
			}
		}
	}
	return page, nil
}

func (s *Session) TrackSeries(ctx context.Context, request provider.SeriesTrackRequest) (provider.SeriesTrackResult, error) {
	if !s.ownsVideo(request.Ref) || request.Ref.Kind != provider.Series {
		return provider.SeriesTrackResult{}, &provider.Error{Code: provider.Denied}
	}
	auth, err := s.requireSignedIn()
	if err != nil {
		return provider.SeriesTrackResult{}, err
	}
	input := struct {
		provider.SeriesTrackRequest
		Owner  string          `json:"owner"`
		Secret json.RawMessage `json:"secret"`
	}{request, auth.owner, auth.secret}
	var result provider.SeriesTrackResult
	if err := s.invoke(ctx, auth.scope, auth.client, provider.TrackSeries, "trackSeries", input, &result); err != nil {
		return provider.SeriesTrackResult{}, err
	}
	return result, nil
}

func (s *Session) sanitizeSeriesEntry(entry *provider.SeriesEntry) error {
	if err := s.ownSummaryItem(&entry.Item); err != nil {
		return err
	}
	entry.Badge = strings.TrimSpace(entry.Badge)
	entry.Score = strings.TrimSpace(entry.Score)
	entry.Status = strings.TrimSpace(entry.Status)
	entry.Eyebrow = strings.TrimSpace(entry.Eyebrow)
	return nil
}

func sanitizeSeriesOptions(raw []provider.SeriesFilterOption, limit int) []provider.SeriesFilterOption {
	if len(raw) == 0 {
		return nil
	}
	out := make([]provider.SeriesFilterOption, 0, min(len(raw), limit))
	seen := make(map[string]bool, min(len(raw), limit))
	for _, opt := range raw {
		label := strings.TrimSpace(opt.Label)
		if label == "" || len(opt.Value) > 128 {
			continue
		}
		if seen[opt.Value] {
			continue
		}
		seen[opt.Value] = true
		out = append(out, provider.SeriesFilterOption{Value: opt.Value, Label: label})
		if len(out) >= limit {
			break
		}
	}
	return out
}

func sanitizeSeriesFilterGroups(raw []provider.SeriesFilterGroup) []provider.SeriesFilterGroup {
	if len(raw) == 0 {
		return nil
	}
	out := make([]provider.SeriesFilterGroup, 0, min(len(raw), maxSeriesFilterGroups))
	seen := make(map[string]bool, min(len(raw), maxSeriesFilterGroups))
	for _, group := range raw {
		key := strings.TrimSpace(group.Key)
		label := strings.TrimSpace(group.Label)
		if key == "" || len(key) > 128 || label == "" || seen[key] {
			continue
		}
		options := sanitizeSeriesOptions(group.Options, maxSeriesFilterOptions)
		if len(options) == 0 {
			continue
		}
		seen[key] = true
		out = append(out, provider.SeriesFilterGroup{Key: key, Label: label, Options: options})
		if len(out) >= maxSeriesFilterGroups {
			break
		}
	}
	return out
}
