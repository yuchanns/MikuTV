package provider

import "context"

const (
	EditVideo          Capability = "account.videoInteraction"
	ReadVideoComments  Capability = "catalog.videoComments"
	WriteVideoComments Capability = "account.videoComments"
	VideoCommentLimit             = 500
)

type VideoInteraction string

const (
	VideoWatchLater   VideoInteraction = "watchLater"
	VideoFollow       VideoInteraction = "follow"
	VideoLike         VideoInteraction = "like"
	VideoFavorite     VideoInteraction = "favorite"
	VideoAction       VideoInteraction = "action"
	VideoReplyLike    VideoInteraction = "replyLike"
	VideoReplyDislike VideoInteraction = "replyDislike"
	VideoPostComment  VideoInteraction = "postComment"
)

func (v VideoInteraction) Valid() bool {
	switch v {
	case VideoWatchLater, VideoFollow, VideoLike, VideoFavorite, VideoAction, VideoReplyLike, VideoReplyDislike, VideoPostComment:
		return true
	default:
		return false
	}
}

type ActionSlot string

const (
	ActionSlotHero     ActionSlot = "hero"
	ActionSlotOverview ActionSlot = "overview"
)

func (s ActionSlot) Valid() bool {
	return s == ActionSlotHero || s == ActionSlotOverview
}

type ActionConfirm struct {
	Title  string `json:"title"`
	Body   string `json:"body,omitempty"`
	Accept string `json:"accept,omitempty"`
	Cancel string `json:"cancel,omitempty"`
	Icon   string `json:"icon,omitempty"`
}

type ActionSpec struct {
	ID             string         `json:"id"`
	Slot           ActionSlot     `json:"slot"`
	Label          string         `json:"label"`
	Icon           string         `json:"icon,omitempty"`
	ActiveIcon     string         `json:"activeIcon,omitempty"`
	Active         bool           `json:"active,omitempty"`
	Confirm        *ActionConfirm `json:"confirm,omitempty"`
	DoneMessage    string         `json:"doneMessage,omitempty"`
	Celebrate      bool           `json:"celebrate,omitempty"`
	CelebrateIcons []string       `json:"celebrateIcons,omitempty"`
}

type VideoEngagement struct {
	WatchLater bool `json:"watchLater,omitempty"`
	Liked      bool `json:"liked,omitempty"`
	Favorite   bool `json:"favorite,omitempty"`
	Tracked    bool `json:"tracked,omitempty"`
}

type VideoEdit struct {
	Ref         MediaRef         `json:"ref"`
	Interaction VideoInteraction `json:"interaction"`
	ActionID    string           `json:"actionID,omitempty"`
	Enabled     bool             `json:"enabled,omitempty"`
	ReplyID     string           `json:"replyID,omitempty"`
	ThreadID    string           `json:"threadID,omitempty"`
	Body        string           `json:"body,omitempty"`
}

type VideoEditResult struct {
	Liked              *bool           `json:"liked,omitempty"`
	Favorite           *bool           `json:"favorite,omitempty"`
	FavoriteCollection string          `json:"favoriteCollection,omitempty"`
	WatchLater         *bool           `json:"watchLater,omitempty"`
	Following          *bool           `json:"following,omitempty"`
	ActionStates       map[string]bool `json:"actionStates,omitempty"`
	Message            string          `json:"message,omitempty"`
	Celebrate          bool            `json:"celebrate,omitempty"`
	Partial            bool            `json:"partial,omitempty"`
	Comment            *VideoComment   `json:"comment,omitempty"`
}

type VideoEditor interface {
	EditVideo(context.Context, VideoEdit) (VideoEditResult, error)
}

type VideoCommentOrder string

const (
	VideoCommentsHot    VideoCommentOrder = "hot"
	VideoCommentsLatest VideoCommentOrder = "latest"
)

type VideoCommentQuery struct {
	Ref      MediaRef          `json:"ref"`
	Order    VideoCommentOrder `json:"order,omitempty"`
	ThreadID string            `json:"threadID,omitempty"`
	Page     int               `json:"page,omitempty"`
}

type VideoCommentPage struct {
	Comments []VideoComment `json:"comments"`
	NextPage int            `json:"nextPage,omitempty"`
}

type VideoCommentReader interface {
	VideoComments(context.Context, VideoCommentQuery) (VideoCommentPage, error)
}

type VideoFavoriteFolder struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Count    int    `json:"count"`
	Private  bool   `json:"private,omitempty"`
	Selected bool   `json:"selected,omitempty"`
}

type VideoFavoriteEdit struct {
	Ref    MediaRef `json:"ref"`
	Add    []string `json:"add,omitempty"`
	Remove []string `json:"remove,omitempty"`
}

type VideoFavoriteEditor interface {
	VideoFavoriteFolders(context.Context, MediaRef) ([]VideoFavoriteFolder, error)
	EditVideoFavoriteFolders(context.Context, VideoFavoriteEdit) error
}
