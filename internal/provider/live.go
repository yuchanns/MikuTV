package provider

import "context"

type LiveSection string

const (
	LiveSectionHub       LiveSection = ""
	LiveSectionRooms     LiveSection = "rooms"
	LiveSectionFollowing LiveSection = "following"
)

func (s LiveSection) Valid() bool {
	switch s {
	case LiveSectionHub, LiveSectionRooms, LiveSectionFollowing:
		return true
	default:
		return false
	}
}

type LiveArea struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type LiveRequest struct {
	Section LiveSection `json:"section,omitempty"`
	Area    string      `json:"area,omitempty"`
	Cursor  string      `json:"cursor,omitempty"`
}

type LivePage struct {
	Areas           []LiveArea  `json:"areas,omitempty"`
	Hero            *MediaItem  `json:"hero,omitempty"`
	Rooms           []MediaItem `json:"rooms,omitempty"`
	FollowingRooms  []MediaItem `json:"followingRooms,omitempty"`
	NextCursor      string      `json:"nextCursor,omitempty"`
	FollowingCursor string      `json:"followingCursor,omitempty"`
}

type LiveReader interface {
	ReadLive(context.Context, LiveRequest) (LivePage, error)
}
