package provider

import "context"

const (
	LiveChat      Capability = "live.chat"
	WriteLiveChat Capability = "live.chatWrite"

	LiveChatLimit = 30
)

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

type LiveChatStatus string

const (
	LiveChatIdle         LiveChatStatus = ""
	LiveChatConnecting   LiveChatStatus = "connecting"
	LiveChatConnected    LiveChatStatus = "connected"
	LiveChatReconnecting LiveChatStatus = "reconnecting"
	LiveChatDisconnected LiveChatStatus = "disconnected"
)

type LiveChatMessage struct {
	ID        string `json:"id,omitempty"`
	Author    string `json:"author"`
	AuthorID  string `json:"authorID,omitempty"`
	Body      string `json:"body"`
	Self      bool   `json:"self,omitempty"`
	Timestamp int64  `json:"timestamp,omitempty"`
}

type LiveChatEventType string

const (
	LiveChatEventStatus  LiveChatEventType = "status"
	LiveChatEventMessage LiveChatEventType = "message"
	LiveChatEventViewers LiveChatEventType = "viewers"
	LiveChatEventMute    LiveChatEventType = "mute"
	LiveChatEventEnded   LiveChatEventType = "ended"
)

type LiveChatEvent struct {
	Type       LiveChatEventType `json:"type"`
	Status     LiveChatStatus    `json:"status,omitempty"`
	Error      ErrorCode         `json:"error,omitempty"`
	Message    *LiveChatMessage  `json:"message,omitempty"`
	Viewers    *int64            `json:"viewers,omitempty"`
	Muted      *bool             `json:"muted,omitempty"`
	MuteReason string            `json:"muteReason,omitempty"`
}

type LiveChatRequest struct {
	Ref    MediaRef `json:"ref"`
	ItemID string   `json:"itemID,omitempty"`
}

type LiveChatPost struct {
	Ref    MediaRef `json:"ref"`
	ItemID string   `json:"itemID,omitempty"`
	Owner  string   `json:"-"`
	Text   string   `json:"text"`
}

type LiveChatSendResult struct {
	Muted      bool   `json:"muted,omitempty"`
	MuteReason string `json:"muteReason,omitempty"`
}

type LiveChatSubscription interface {
	Events() <-chan LiveChatEvent
	Close()
}

type LiveChatClient interface {
	OpenLiveChat(context.Context, LiveChatRequest) (LiveChatSubscription, error)
}

type LiveChatWriter interface {
	SendLiveChat(context.Context, LiveChatPost) (LiveChatSendResult, error)
}
