package provider

import "context"

type FollowingSection string

const (
	FollowingOverview FollowingSection = "overview"
	FollowingCreators FollowingSection = "creators"
	FollowingUpdates  FollowingSection = "updates"
	FollowingLive     FollowingSection = "live"
)

func (s FollowingSection) Valid() bool {
	switch s {
	case "", FollowingOverview, FollowingCreators, FollowingUpdates, FollowingLive:
		return true
	default:
		return false
	}
}

type FollowingRequest struct {
	Section FollowingSection `json:"section,omitempty"`
	Cursor  string           `json:"cursor,omitempty"`
}

type FollowingCreator struct {
	Creator   Creator `json:"creator"`
	Bio       string  `json:"bio,omitempty"`
	HasUpdate bool    `json:"hasUpdate,omitempty"`
	Live      bool    `json:"live,omitempty"`
}

type FollowingPage struct {
	Creators       []FollowingCreator `json:"creators,omitempty"`
	CreatorsCursor string             `json:"creatorsCursor,omitempty"`
	Updates        []MediaItem        `json:"updates,omitempty"`
	UpdatesCursor  string             `json:"updatesCursor,omitempty"`
	LiveRooms      []MediaItem        `json:"liveRooms,omitempty"`
	LiveCursor     string             `json:"liveCursor,omitempty"`
	SupportsLive   bool               `json:"supportsLive,omitempty"`
}

type FollowingReader interface {
	ReadFollowing(context.Context, FollowingRequest) (FollowingPage, error)
}
