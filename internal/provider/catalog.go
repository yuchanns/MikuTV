package provider

import (
	"context"
	"image"
	"slices"
)

type Capability string

const (
	BrowseCatalog   Capability = "catalog.browse"
	GetCatalogItem  Capability = "catalog.item"
	SearchCatalog   Capability = "catalog.search"
	BrowseFollowing Capability = "catalog.following"
	BrowseSeries    Capability = "catalog.series"
	BrowseLive      Capability = "catalog.live"
	BrowseLibrary   Capability = "catalog.library"
	SignIn          Capability = "account.signIn"
)

type Capabilities []Capability

func (c Capabilities) Has(capability Capability) bool { return slices.Contains(c, capability) }

type MediaKind string

const (
	Video          MediaKind = "video"
	Movie          MediaKind = "movie"
	Series         MediaKind = "series"
	Episode        MediaKind = "episode"
	Live           MediaKind = "live"
	CollectionKind MediaKind = "collection"
)

func (k MediaKind) Valid() bool {
	switch k {
	case Video, Movie, Series, Episode, Live, CollectionKind:
		return true
	default:
		return false
	}
}

type MediaRef struct {
	EntryID string    `json:"-"`
	Kind    MediaKind `json:"kind"`
	ID      string    `json:"id"`
}

type Creator struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatarURL,omitempty"`
}

type MediaItem struct {
	Ref             MediaRef `json:"ref"`
	Title           string   `json:"title"`
	Subtitle        string   `json:"subtitle,omitempty"`
	Creator         *Creator `json:"creator,omitempty"`
	CoverURL        string   `json:"coverURL,omitempty"`
	BackdropURL     string   `json:"backdropURL,omitempty"`
	DurationSeconds *int64   `json:"durationSeconds,omitempty"`
	Views           *int64   `json:"views,omitempty"`
	RankScore       *int64   `json:"rankScore,omitempty"`
	Category        string   `json:"category,omitempty"`
	Quality         string   `json:"quality,omitempty"`
	ProgressSeconds *int64   `json:"progressSeconds,omitempty"`
}

type Collection struct {
	ID    string      `json:"id"`
	Title string      `json:"title"`
	Items []MediaItem `json:"items"`
}

type CatalogFilter struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type CollectionPage struct {
	Collections []Collection    `json:"collections"`
	NextCursor  string          `json:"nextCursor,omitempty"`
	Filters     []CatalogFilter `json:"filters,omitempty"`
	Shelves     []Collection    `json:"shelves,omitempty"`
}

type BrowseRequest struct {
	Collection string `json:"collection,omitempty"`
	Filter     string `json:"filter,omitempty"`
	Cursor     string `json:"cursor,omitempty"`
	Refresh    bool   `json:"refresh,omitempty"`
}

type Catalog interface {
	Browse(context.Context, BrowseRequest) (CollectionPage, error)
	Item(context.Context, MediaRef) (MediaItem, error)
}

type CatalogArtworkLoader interface {
	LoadCatalogArtwork(context.Context, string) (image.Image, error)
}

type CatalogBackdropLoader interface {
	LoadCatalogBackdrop(context.Context, string) (image.Image, error)
}

// Session owns the resources and cancellation for one active content entry.
type Session interface {
	Catalog
	Close()
}
