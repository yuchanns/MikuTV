package provider

import "context"

const (
	SearchSuggest Capability = "catalog.searchSuggest"
)

type SearchKind string

const (
	SearchAll     SearchKind = "all"
	SearchVideo   SearchKind = "video"
	SearchCreator SearchKind = "creator"
	SearchSeries  SearchKind = "series"
	SearchLive    SearchKind = "live"
)

func (k SearchKind) Valid() bool {
	switch k {
	case "", SearchAll, SearchVideo, SearchCreator, SearchSeries, SearchLive:
		return true
	default:
		return false
	}
}

type SearchSort string

const (
	SearchSortDefault SearchSort = "default"
	SearchSortNewest  SearchSort = "newest"
	SearchSortViews   SearchSort = "views"
)

func (s SearchSort) Valid() bool {
	switch s {
	case "", SearchSortDefault, SearchSortNewest, SearchSortViews:
		return true
	default:
		return false
	}
}

type SearchDuration string

const (
	SearchDurationAll    SearchDuration = "all"
	SearchDurationShort  SearchDuration = "short"
	SearchDurationMedium SearchDuration = "medium"
	SearchDurationLong   SearchDuration = "long"
)

func (d SearchDuration) Valid() bool {
	switch d {
	case "", SearchDurationAll, SearchDurationShort, SearchDurationMedium, SearchDurationLong:
		return true
	default:
		return false
	}
}

type SearchRequest struct {
	Query    string         `json:"query,omitempty"`
	Kind     SearchKind     `json:"kind,omitempty"`
	Sort     SearchSort     `json:"sort,omitempty"`
	Duration SearchDuration `json:"duration,omitempty"`
	Cursor   string         `json:"cursor,omitempty"`
}

type SearchCreatorItem struct {
	Creator     Creator `json:"creator"`
	Description string  `json:"description,omitempty"`
	Followers   *int64  `json:"followers,omitempty"`
	Videos      *int64  `json:"videos,omitempty"`
}

type SearchPage struct {
	Items       []MediaItem         `json:"items,omitempty"`
	Creators    []SearchCreatorItem `json:"creators,omitempty"`
	Total       *int64              `json:"total,omitempty"`
	NextCursor  string              `json:"nextCursor,omitempty"`
	HotKeywords []string            `json:"hotKeywords,omitempty"`
	Kinds       []SearchKind        `json:"kinds,omitempty"`
}

type SearchSuggestResult struct {
	Suggestions []string `json:"suggestions,omitempty"`
}

type CatalogSearcher interface {
	Search(context.Context, SearchRequest) (SearchPage, error)
}

type CatalogSearchSuggester interface {
	SearchSuggest(context.Context, string) (SearchSuggestResult, error)
}
