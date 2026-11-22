package provider

import "context"

const (
	ReadCreator   Capability = "catalog.creator"
	FollowCreator Capability = "account.followCreator"
)

type CreatorTab string

const (
	CreatorTabVideos      CreatorTab = "videos"
	CreatorTabPosts       CreatorTab = "posts"
	CreatorTabCollections CreatorTab = "collections"
	CreatorTabAbout       CreatorTab = "about"
)

func (t CreatorTab) Valid() bool {
	switch t {
	case "", CreatorTabVideos, CreatorTabPosts, CreatorTabCollections, CreatorTabAbout:
		return true
	default:
		return false
	}
}

type CreatorAttribute struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

type CreatorProfile struct {
	Creator     Creator            `json:"creator"`
	BackdropURL string             `json:"backdropURL,omitempty"`
	Bio         string             `json:"bio,omitempty"`
	Followers   *int64             `json:"followers,omitempty"`
	Works       *int64             `json:"works,omitempty"`
	Location    string             `json:"location,omitempty"`
	Category    string             `json:"category,omitempty"`
	Attributes  []CreatorAttribute `json:"attributes,omitempty"`
	Tabs        []CreatorTab       `json:"tabs,omitempty"`
}

type CreatorPost struct {
	ID             string     `json:"id"`
	PublishedAt    int64      `json:"publishedAt,omitempty"`
	PublishedLabel string     `json:"publishedLabel,omitempty"`
	Body           string     `json:"body,omitempty"`
	ImageURL       string     `json:"imageURL,omitempty"`
	Item           *MediaItem `json:"item,omitempty"`
}

type CreatorCollection struct {
	ID          string      `json:"id"`
	Title       string      `json:"title"`
	Description string      `json:"description,omitempty"`
	Count       *int64      `json:"count,omitempty"`
	Items       []MediaItem `json:"items,omitempty"`
}

type CreatorRequest struct {
	CreatorID      string     `json:"creatorID"`
	Tab            CreatorTab `json:"tab,omitempty"`
	Cursor         string     `json:"cursor,omitempty"`
	IncludeProfile bool       `json:"includeProfile,omitempty"`
}

type CreatorPage struct {
	Profile     *CreatorProfile     `json:"profile,omitempty"`
	Items       []MediaItem         `json:"items,omitempty"`
	Posts       []CreatorPost       `json:"posts,omitempty"`
	Collections []CreatorCollection `json:"collections,omitempty"`
	NextCursor  string              `json:"nextCursor,omitempty"`
}

type CreatorFollowRequest struct {
	CreatorID string `json:"creatorID"`
	Enabled   bool   `json:"enabled"`
}

type CreatorFollowResult struct {
	Following bool `json:"following"`
}

type CreatorReader interface {
	ReadCreator(context.Context, CreatorRequest) (CreatorPage, error)
}

type CreatorFollower interface {
	FollowCreator(context.Context, CreatorFollowRequest) (CreatorFollowResult, error)
}
