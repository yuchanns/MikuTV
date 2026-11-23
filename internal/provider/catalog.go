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
	Course         MediaKind = "course"
	Live           MediaKind = "live"
	CollectionKind MediaKind = "collection"
)

func (k MediaKind) Valid() bool {
	switch k {
	case Video, Movie, Series, Episode, Course, Live, CollectionKind:
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
	Following bool   `json:"following,omitempty"`
}

type VideoAccess string

const (
	VideoAccessGranted    VideoAccess = "granted"
	VideoAccessPreview    VideoAccess = "preview"
	VideoAccessRestricted VideoAccess = "restricted"
)

type SectionPresentation struct {
	Tracking string `json:"tracking,omitempty"`
	Courses  string `json:"courses,omitempty"`
}

type SearchPresentation struct {
	Hint         string `json:"hint,omitempty"`
	CreatorLabel string `json:"creatorLabel,omitempty"`
	SeriesLabel  string `json:"seriesLabel,omitempty"`
}

type SeriesPresentation struct {
	TrackLabel   string `json:"trackLabel,omitempty"`
	TrackedLabel string `json:"trackedLabel,omitempty"`
}

type SourcePresentation struct {
	CreatorMark       string              `json:"creatorMark,omitempty"`
	CreatorGroupTitle string              `json:"creatorGroupTitle,omitempty"`
	Membership        string              `json:"membership,omitempty"`
	MobileApp         string              `json:"mobileApp,omitempty"`
	Sections          SectionPresentation `json:"sections,omitzero"`
	Search            SearchPresentation  `json:"search,omitzero"`
	Series            SeriesPresentation  `json:"series,omitzero"`
}

type VideoDetails struct {
	PublishedAt int64            `json:"publishedAt,omitempty"`
	Comments    *int64           `json:"comments,omitempty"`
	Access      VideoAccess      `json:"access,omitempty"`
	Segments    []VideoSegment   `json:"segments,omitempty"`
	SegmentKind VideoSegmentKind `json:"segmentKind,omitempty"`
	Discussion  []VideoComment   `json:"discussion,omitempty"`
	Related     []MediaItem      `json:"related,omitempty"`
	Engagement  VideoEngagement  `json:"engagement,omitempty"`
	Actions     []ActionSpec     `json:"actions,omitempty"`
}

type VideoSegmentKind string

const (
	VideoChapters   VideoSegmentKind = "chapters"
	VideoParts      VideoSegmentKind = "parts"
	VideoCollection VideoSegmentKind = "collection"
)

type VideoSegment struct {
	Title           string    `json:"title"`
	DurationSeconds *int64    `json:"durationSeconds,omitempty"`
	Ref             *MediaRef `json:"ref,omitempty"`
	PartID          string    `json:"partID,omitempty"`
	StartSeconds    int64     `json:"startSeconds,omitempty"`
	Unavailable     bool      `json:"unavailable,omitempty"`
	Locked          bool      `json:"locked,omitempty"`
	Status          string    `json:"status,omitempty"`
}

type VideoComment struct {
	ID            string         `json:"id"`
	Author        string         `json:"author,omitempty"`
	ReplyToAuthor string         `json:"replyToAuthor,omitempty"`
	AvatarURL     string         `json:"avatarURL,omitempty"`
	Body          string         `json:"body"`
	PublishedAt   int64          `json:"publishedAt,omitempty"`
	Location      string         `json:"location,omitempty"`
	Likes         *int64         `json:"likes,omitempty"`
	ReplyCount    *int64         `json:"replyCount,omitempty"`
	Liked         bool           `json:"liked,omitempty"`
	Disliked      bool           `json:"disliked,omitempty"`
	Pinned        bool           `json:"pinned,omitempty"`
	IsCreator     bool           `json:"isCreator,omitempty"`
	Replies       []VideoComment `json:"replies,omitempty"`
}

type MediaItem struct {
	RankScore       *int64        `json:"rankScore,omitempty"`
	Ref             MediaRef      `json:"ref"`
	Title           string        `json:"title"`
	Subtitle        string        `json:"subtitle,omitempty"`
	Creator         *Creator      `json:"creator,omitempty"`
	CoverURL        string        `json:"coverURL,omitempty"`
	BackdropURL     string        `json:"backdropURL,omitempty"`
	DurationSeconds *int64        `json:"durationSeconds,omitempty"`
	Views           *int64        `json:"views,omitempty"`
	Category        string        `json:"category,omitempty"`
	Quality         string        `json:"quality,omitempty"`
	ProgressSeconds *int64        `json:"progressSeconds,omitempty"`
	VideoDetails    *VideoDetails `json:"videoDetails,omitempty"`
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

type VideoDetailReader interface {
	VideoDetail(context.Context, MediaRef) (MediaItem, error)
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
