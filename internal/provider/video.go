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
	VideoCoin         VideoInteraction = "coin"
	VideoFavorite     VideoInteraction = "favorite"
	VideoTriple       VideoInteraction = "triple"
	VideoReplyLike    VideoInteraction = "replyLike"
	VideoReplyDislike VideoInteraction = "replyDislike"
	VideoPostComment  VideoInteraction = "postComment"
)

func (v VideoInteraction) Valid() bool {
	switch v {
	case VideoWatchLater, VideoFollow, VideoLike, VideoCoin, VideoFavorite, VideoTriple, VideoReplyLike, VideoReplyDislike, VideoPostComment:
		return true
	default:
		return false
	}
}

type VideoEngagement struct {
	WatchLater bool  `json:"watchLater,omitempty"`
	Liked      bool  `json:"liked,omitempty"`
	Favorite   bool  `json:"favorite,omitempty"`
	Coins      int64 `json:"coins,omitempty"`
}

type VideoEdit struct {
	Ref         MediaRef         `json:"ref"`
	Interaction VideoInteraction `json:"interaction"`
	Enabled     bool             `json:"enabled,omitempty"`
	ReplyID     string           `json:"replyID,omitempty"`
	ThreadID    string           `json:"threadID,omitempty"`
	Body        string           `json:"body,omitempty"`
}

type VideoEditResult struct {
	Liked              *bool         `json:"liked,omitempty"`
	Favorite           *bool         `json:"favorite,omitempty"`
	FavoriteCollection string        `json:"favoriteCollection,omitempty"`
	WatchLater         *bool         `json:"watchLater,omitempty"`
	Following          *bool         `json:"following,omitempty"`
	Coins              *int64        `json:"coins,omitempty"`
	Partial            bool          `json:"partial,omitempty"`
	Comment            *VideoComment `json:"comment,omitempty"`
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
