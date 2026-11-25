package provider

import (
	"context"
	"net/http"
)

const (
	ResolvePlayback    Capability = "playback.resolve"
	ReportPlayback     Capability = "playback.progress"
	ReadDanmaku        Capability = "playback.danmaku"
	ReadSubtitles      Capability = "playback.subtitles"
	PlaybackModes      Capability = "playback.modes"
	WriteDanmaku       Capability = "account.danmaku"
	EditVideoFavorites Capability = "account.videoFavorites"
)

type PlaybackTarget struct {
	Ref    MediaRef `json:"ref"`
	PartID string   `json:"partID,omitempty"`
}

type PlaybackRequest struct {
	PlaybackTarget
	Quality          string `json:"quality,omitempty"`
	PreferredQuality string `json:"preferredQuality,omitempty"`
	Line             string `json:"line,omitempty"`
	BranchID         string `json:"branchID,omitempty"`
	BranchState      string `json:"branchState,omitempty"`
	HDR              bool   `json:"hdr,omitempty"`
	HEVC             bool   `json:"hevc,omitempty"`
}

type PlaybackTransport interface {
	OpenResource(ctx context.Context, resource PlaybackResource, method, byteRange string) (*http.Response, error)
}

// PlaybackHealthReporter is optionally implemented by a PlaybackTransport that
// tracks per-host CDN health and supports rotating away from slow nodes.
type PlaybackHealthReporter interface {
	ReportSlow(rawURL string)
	HasHealthyAlternative(urls []string, currentURL string) bool
}

type PlaybackReader interface {
	ResolvePlayback(context.Context, PlaybackRequest) (Playback, error)
}

// Playback couples an immutable plan to the entry and account that resolved it.
type Playback struct {
	Plan      PlaybackPlan
	Transport PlaybackTransport
}

type PlaybackPlan struct {
	PartID                string              `json:"partID,omitempty"`
	ItemID                string              `json:"itemID,omitempty"`
	Duration              float64             `json:"duration,omitempty"`
	ResumeSeconds         float64             `json:"resumeSeconds,omitempty"`
	ExpiresAt             int64               `json:"expiresAt,omitempty"`
	DanmakuSegmentSeconds int                 `json:"danmakuSegmentSeconds,omitempty"`
	Live                  bool                `json:"live,omitempty"`
	LiveEnded             bool                `json:"liveEnded,omitempty"`
	Access                VideoAccess         `json:"access,omitempty"`
	PreviewRestriction    PlaybackLimit       `json:"previewRestriction,omitempty"`
	Restriction           PlaybackLimit       `json:"restriction,omitempty"`
	Quality               string              `json:"quality,omitempty"`
	Qualities             []PlaybackQuality   `json:"qualities,omitempty"`
	Line                  string              `json:"line,omitempty"`
	Lines                 []PlaybackLine      `json:"lines,omitempty"`
	Streams               []PlaybackStream    `json:"streams,omitempty"`
	Chapters              []PlaybackChapter   `json:"chapters,omitempty"`
	Subtitles             []PlaybackTrack     `json:"subtitles,omitempty"`
	Episodes              []VideoSegment      `json:"episodes,omitempty"`
	Thumbnails            *PlaybackThumbnails `json:"thumbnails,omitempty"`
	Branch                *PlaybackBranch     `json:"branch,omitempty"`
}

type PlaybackBranch struct {
	NodeID         string                 `json:"nodeID,omitempty"`
	NodeTitle      string                 `json:"nodeTitle,omitempty"`
	Question       string                 `json:"question,omitempty"`
	TriggerSeconds float64                `json:"triggerSeconds,omitempty"`
	PauseVideo     bool                   `json:"pauseVideo,omitempty"`
	AutoAdvance    bool                   `json:"autoAdvance,omitempty"`
	State          string                 `json:"state,omitempty"`
	Options        []PlaybackBranchOption `json:"options,omitempty"`
}

type PlaybackBranchOption struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Subtitle string `json:"subtitle,omitempty"`
	PartID   string `json:"partID,omitempty"`
	State    string `json:"state,omitempty"`
	Default  bool   `json:"default,omitempty"`
}

type PlaybackLimit string

const (
	PlaybackLogin      PlaybackLimit = "login"
	PlaybackMembership PlaybackLimit = "membership"
	PlaybackPurchase   PlaybackLimit = "purchase"
	PlaybackRegion     PlaybackLimit = "region"
	PlaybackDevice     PlaybackLimit = "device"
	PlaybackSource     PlaybackLimit = "source"
)

type PlaybackQuality struct {
	ID        string        `json:"id"`
	Label     string        `json:"label"`
	Limit     PlaybackLimit `json:"limit,omitempty"`
	HDR       bool          `json:"hdr,omitempty"`
	Badge     string        `json:"badge,omitempty"`
	Bandwidth int           `json:"bandwidth,omitempty"`
}

type PlaybackLine struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type PlaybackFormat string

const (
	PlaybackDirect PlaybackFormat = "direct"
	PlaybackHLS    PlaybackFormat = "hls"
	PlaybackDASH   PlaybackFormat = "dash"
)

type PlaybackStream struct {
	Format      PlaybackFormat  `json:"format"`
	Live        bool            `json:"live,omitempty"`
	Video       PlaybackTrack   `json:"video"`
	Audio       *PlaybackTrack  `json:"audio,omitempty"`
	AudioTracks []PlaybackTrack `json:"audioTracks,omitempty"`
}

type PlaybackResource struct {
	URLs    []string          `json:"urls"`
	Headers map[string]string `json:"headers,omitempty"`
}

type PlaybackTrack struct {
	ID          string           `json:"id,omitempty"`
	Label       string           `json:"label,omitempty"`
	Language    string           `json:"language,omitempty"`
	Default     bool             `json:"default,omitempty"`
	Resource    PlaybackResource `json:"resource"`
	Codec       string           `json:"codec,omitempty"`
	ContentType string           `json:"contentType,omitempty"`
	Bandwidth   int              `json:"bandwidth,omitempty"`
	Width       int              `json:"width,omitempty"`
	Height      int              `json:"height,omitempty"`
	FrameRate   float64          `json:"frameRate,omitempty"`
	Init        *PlaybackRange   `json:"init,omitempty"`
	Index       *PlaybackRange   `json:"index,omitempty"`
}

type PlaybackRange struct {
	First int64 `json:"first"`
	Last  int64 `json:"last"`
}

type PlaybackChapter struct {
	Title    string  `json:"title"`
	Start    float64 `json:"start"`
	ImageURL string  `json:"imageURL,omitempty"`
}

type PlaybackProgress struct {
	PlaybackTarget
	Owner    string `json:"-"`
	ItemID   string `json:"itemID,omitempty"`
	Seconds  int64  `json:"seconds"`
	Finished bool   `json:"finished,omitempty"`
}

type PlaybackThumbnails struct {
	Images  []string  `json:"images"`
	Times   []float64 `json:"times"`
	Columns int       `json:"columns"`
	Rows    int       `json:"rows"`
	Width   int       `json:"width"`
	Height  int       `json:"height"`
}

type PlaybackSubtitleQuery struct {
	PlaybackTarget
	Track PlaybackTrack `json:"track"`
}

type SubtitleCue struct {
	Start float64 `json:"start"`
	End   float64 `json:"end"`
	Text  string  `json:"text"`
}

type PlaybackSubtitleReader interface {
	PlaybackSubtitles(context.Context, PlaybackSubtitleQuery) ([]SubtitleCue, error)
}

type PlaybackReporter interface {
	ReportPlayback(context.Context, PlaybackProgress) error
}

type DanmakuQuery struct {
	PlaybackTarget
	ItemID  string `json:"itemID,omitempty"`
	Segment int    `json:"segment"`
}

type DanmakuMode string

const (
	DanmakuScroll DanmakuMode = "scroll"
	DanmakuTop    DanmakuMode = "top"
	DanmakuBottom DanmakuMode = "bottom"
	DanmakuLimit              = 100
)

type Danmaku struct {
	Seconds    float64     `json:"seconds"`
	Mode       DanmakuMode `json:"mode"`
	Text       string      `json:"text"`
	SenderID   string      `json:"senderID,omitempty"`
	SenderHash string      `json:"senderHash,omitempty"`
}

type DanmakuPost struct {
	PlaybackProgress
	Start float64     `json:"start"`
	Mode  DanmakuMode `json:"mode"`
	Text  string      `json:"text"`
}

type PlaybackDanmaku interface {
	Danmaku(context.Context, DanmakuQuery) ([]Danmaku, error)
	PostDanmaku(context.Context, DanmakuPost) error
}
