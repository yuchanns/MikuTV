package app

import (
	"context"
	"log"
	"math"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/yuchanns/MikuTV/internal/player"
	"github.com/yuchanns/MikuTV/internal/provider"
)

type PlaybackStore interface {
	PlaybackPosition(owner string, target provider.PlaybackTarget) (float64, bool)
	SavePlaybackPosition(owner string, target provider.PlaybackTarget, seconds float64) error
}

const (
	playbackStallTimeout      = 25 * time.Second
	autoDowngradeCooldown     = 8 * time.Second
	autoUpgradeCooldown       = 30 * time.Second
	autoUpgradeHealthyWindow  = 15 * time.Second
	autoUpgradeMinBufferSec   = 20.0
	autoUpgradeSpeedMargin    = 1.6
	autoStallWindow           = 30 * time.Second
	autoSDRRebufferLimit      = 5 * time.Second
	autoHDRRebufferLimit      = 8 * time.Second
	autoSDRStartupLimit       = 8 * time.Second
	autoHDRStartupLimit       = 10 * time.Second
	autoCDNGraceExtra         = 2 * time.Second
	preloadExpiryMarginSec    = 10
	inlinePreviewStallTimeout = 12 * time.Second
	seekAutoQualityGrace      = 2 * time.Second
	audioPauseDimDelay        = 5 * time.Minute
)

var playbackTrace = os.Getenv("MIKUTV_NAV_TRACE") == "1"

type savedPositionKey struct {
	owner  string
	target provider.PlaybackTarget
}

type InlinePreviewState struct {
	Target    provider.PlaybackTarget
	Active    bool
	Loading   bool
	HasFrame  bool
	AudioOnly bool
	Playing   bool
	Failed    bool
}

type preloadedPlayback struct {
	target           provider.PlaybackTarget
	owner            string
	preferredQuality string
	result           provider.Playback
	media            *player.Media
}

type PlaybackState struct {
	Target         provider.PlaybackTarget
	Resolution     provider.Playback
	Clock          player.Status
	Revision       uint64
	Position       float64
	Duration       float64
	Selected       string
	SelectedLine   string
	AudioTracks    []provider.PlaybackTrack
	SelectedAudio  string
	ActiveAudio    string
	DefaultAudio   string
	Fallback       string
	Limit          provider.PlaybackLimit
	Failure        provider.ErrorCode
	LiveEnded      bool
	Playing        bool
	Loading        bool
	Switching      bool
	NeedsReload    bool
	VideoRange      string
	AudioOnly       bool
	AudioDimmed     bool
	PreviewPending  bool
	BranchPending   bool
	BranchDismissed bool
	BranchHistory   int
	BytesPerSecond  float64
}

type Playback struct {
	catalog *Catalog
	engine  player.Engine
	store   PlaybackStore
	prefs   *PlaybackSettings
	post    func(func())
	changed func()
	onReported func()

	state            PlaybackState
	owner            string
	syncSource       bool
	active           bool
	rate             float64
	trickRate        float64
	media            *player.Media
	altMode          *cachedMediaMode
	cancel           context.CancelFunc
	request          uint64
	selectedAudio    string
	mediaKind        string
	audioCancel      context.CancelFunc
	audioRequest     uint64
	refreshed        bool
	lastSaved        int64
	savedAsEnd       bool
	positionAt       time.Time
	startedAt        time.Time
	bufferedAt       time.Time
	pausedAt         time.Time
	loaded           bool
	reportedStart    bool
	didEnd           bool
	liveEndChecked   bool
	previewConfirmed bool
	hdrBlocked       bool
	branchID         string
	branchState      string
	branchTriggered  bool
	reporting        bool
	reports          []queuedPlaybackReport

	autoQuality     string
	autoHDRBlocked  bool
	autoStallCount  int
	autoStallSince  time.Time
	autoStallActive bool
	autoLastSwitch  time.Time
	autoHealthyAt   time.Time
	seekSettledAt   time.Time

	saveMu       sync.Mutex
	savePending  map[savedPositionKey]float64
	saveInflight bool
	saveDone     chan struct{}

	preloadCancel  context.CancelFunc
	preloadRequest uint64
	preloadTarget  provider.PlaybackTarget
	preloadOwner   string
	preloaded      *preloadedPlayback

	inlineActive    bool
	inlineState     InlinePreviewState
	inlineMedia     *player.Media
	inlineCancel    context.CancelFunc
	inlineRequest   uint64
	inlineStartedAt time.Time
}

type queuedPlaybackReport struct {
	reporter provider.PlaybackReporter
	owner    string
	report   provider.PlaybackProgress
}

func NewPlayback(catalog *Catalog, native player.Engine, store PlaybackStore, post func(func()), changed func()) *Playback {
	return &Playback{catalog: catalog, engine: native, store: store, post: post, changed: changed}
}

func (p *Playback) SetPreferences(prefs *PlaybackSettings) {
	p.prefs = prefs
	if p.engine.SetAllowScreensaver != nil {
		p.engine.SetAllowScreensaver(p.allowAudioScreensaver())
	}
}

func (p *Playback) allowAudioScreensaver() bool {
	if p.prefs == nil {
		return false
	}
	return p.prefs.Preferences().AllowAudioScreensaver
}

func (p *Playback) SetNowPlaying(info player.NowPlayingInfo) {
	if p != nil && p.engine.SetNowPlaying != nil {
		p.engine.SetNowPlaying(info)
	}
}

func (p *Playback) SetRemoteHandlers(handlers player.RemoteHandlers) {
	if p != nil && p.engine.SetRemoteHandlers != nil {
		p.engine.SetRemoteHandlers(handlers)
	}
}

// SetFullscreen updates the native fullscreen presentation state without stopping
// audio playback when leaving the Player page for in-app background listening.
func (p *Playback) SetFullscreen(active bool) {
	if p != nil && p.engine.Fullscreen != nil {
		p.engine.Fullscreen(active)
	}
}

func (p *Playback) preferredQuality() string {
	if p.prefs == nil {
		return ""
	}
	return string(p.prefs.Preferences().Quality)
}

func (p *Playback) defaultSpeed() float64 {
	if p.prefs == nil {
		return 1
	}
	return p.prefs.Preferences().Speed
}

func (p *Playback) State() PlaybackState { return p.state }
func (p *Playback) Active() bool         { return p.active }
func (p *Playback) Rate() float64 {
	if p.rate > 0 {
		return p.rate
	}
	return p.defaultSpeed()
}
func (p *Playback) TrickRate() float64 { return p.trickRate }
func (p *Playback) EffectiveRate() float64 {
	if p.trickRate != 0 && !p.isLive() {
		if p.trickRate > 0 && p.trickRate <= 2 {
			return p.trickRate
		}
		return 0
	}
	if !p.state.Playing {
		return 0
	}
	if p.isLive() {
		return 1
	}
	return p.Rate()
}
func (p *Playback) Owner() string { return p.owner }
func (p *Playback) isLive() bool {
	return p.state.Target.Ref.Kind == provider.Live || p.state.Resolution.Plan.Live
}

func (p *Playback) SetViewport(widthFraction float64) {
	if p.engine.Viewport != nil {
		p.engine.Viewport(widthFraction)
	}
}

func (p *Playback) SetBackdropBlur(radius float32) bool {
	return p.engine.BackdropBlur != nil && p.engine.BackdropBlur(radius)
}

// Start resumes a saved position when start is nil.
func (p *Playback) Start(target provider.PlaybackTarget, owner string, syncSource bool, start *float64) {
	p.StopInlinePreview()
	var hit *preloadedPlayback
	prefQ := p.preferredQuality()
	if p.preloaded != nil && p.preloaded.target == target && p.preloaded.owner == owner && p.preloaded.preferredQuality == prefQ &&
		(p.preloaded.result.Plan.ExpiresAt == 0 || time.Now().Unix()+preloadExpiryMarginSec < p.preloaded.result.Plan.ExpiresAt) {
		hit = p.preloaded
		p.preloaded = nil
	}
	p.clearPreload()
	p.clearAltMode()
	if p.active {
		p.saveStopped()
		p.clearStream()
	} else if p.engine.Fullscreen != nil {
		p.engine.Fullscreen(true)
	}
	p.active, p.owner, p.syncSource = true, owner, syncSource
	revision := p.state.Revision
	var position float64
	if start != nil {
		position = *start
	}
	p.state = PlaybackState{Target: target, Playing: true, Position: position, Revision: revision}
	p.loaded, p.reportedStart, p.didEnd, p.refreshed = false, false, false, false
	p.liveEndChecked, p.pausedAt = false, time.Time{}
	p.previewConfirmed, p.hdrBlocked = false, false
	p.branchID, p.branchState, p.branchTriggered = "", "", false
	p.selectedAudio = ""
	p.mediaKind = ""
	p.lastSaved, p.savedAsEnd = -1, false
	p.trickRate = 0
	p.resetAutoQuality(true)
	if target.Ref.Kind == provider.Live {
		p.rate = 1
	} else if p.rate == 0 {
		p.rate = p.defaultSpeed()
	}
	if hit != nil {
		hit.media.Promote()
		p.accept(hit.result, hit.media, nil, "", "", false, start == nil)
		p.changed()
		return
	}
	useResume := start == nil && target.Ref.Kind != provider.Live
	p.resolveMode("", "", "", false, useResume)
}

func (p *Playback) Close() {
	p.StopInlinePreview()
	if !p.active {
		p.clearPreload()
		return
	}
	p.saveStopped()
	p.clearPreload()
	p.clearAltMode()
	p.clearStream()
	if p.engine.SetNowPlaying != nil {
		p.engine.SetNowPlaying(player.NowPlayingInfo{Active: false})
	}
	if p.engine.SetAudioOnly != nil {
		p.engine.SetAudioOnly(false)
	}
	if p.engine.Fullscreen != nil {
		p.engine.Fullscreen(false)
	}
	p.active = false
	p.reportedStart = false
	p.branchID, p.branchState, p.branchTriggered = "", "", false
	p.selectedAudio = ""
	p.mediaKind = ""
	p.state = PlaybackState{Revision: p.state.Revision}
	p.rate = 0
	p.trickRate = 0
	p.resetAutoQuality(true)
	p.changed()
}

func (p *Playback) clearStream() {
	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
	}
	if p.audioCancel != nil {
		p.audioCancel()
		p.audioCancel = nil
	}
	p.audioRequest++
	if p.engine.Stop != nil {
		p.engine.Stop()
	}
	if p.media != nil {
		p.media.CloseAsync()
		p.media = nil
	}
}

type cachedMediaMode struct {
	target        provider.PlaybackTarget
	result        provider.Playback
	media         *player.Media
	quality       string
	selectedLine  string
	selectedAudio string
}

func (p *Playback) clearAltMode() {
	if p.altMode != nil {
		if p.altMode.media != nil {
			p.altMode.media.CloseAsync()
		}
		p.altMode = nil
	}
}

func (p *Playback) clearPreload() {
	if p.preloadCancel != nil {
		p.preloadCancel()
		p.preloadCancel = nil
	}
	p.preloadRequest++
	p.preloadTarget = provider.PlaybackTarget{}
	p.preloadOwner = ""
	if p.preloaded != nil {
		if p.preloaded.media != nil {
			p.preloaded.media.CloseAsync()
		}
		p.preloaded = nil
	}
}

// PreloadNext resolves and warms up the next episode in the background so
// sequential auto-advance can start immediately without a metadata round-trip.
func (p *Playback) PreloadNext(target provider.PlaybackTarget, owner string) {
	if !p.active || p.isLive() || target.Ref.ID == "" || target.Ref.Kind == provider.Live || p.state.Loading || p.state.Switching ||
		p.state.Resolution.Plan.Access == provider.VideoAccessPreview || p.post == nil {
		return
	}
	if p.preloaded != nil && p.preloaded.target == target && p.preloaded.owner == owner {
		if p.preloaded.result.Plan.ExpiresAt == 0 || time.Now().Unix()+preloadExpiryMarginSec < p.preloaded.result.Plan.ExpiresAt {
			return
		}
	}
	if p.preloadCancel != nil && p.preloadTarget == target && p.preloadOwner == owner {
		return
	}
	resolver, _ := p.catalog.session.(provider.PlaybackReader)
	if resolver == nil {
		return
	}
	p.clearPreload()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	p.preloadCancel = cancel
	p.preloadTarget = target
	p.preloadOwner = owner
	request := p.preloadRequest
	var caps player.Capabilities
	if p.engine.Capabilities != nil {
		caps = p.engine.Capabilities()
	}
	if p.hdrBlocked {
		caps.HDR = false
	}
	prefQ := p.preferredQuality()
	input := provider.PlaybackRequest{
		PlaybackTarget: target, Quality: "", PreferredQuality: prefQ, HDR: caps.HDR, HEVC: caps.HEVC,
	}
	go func() {
		result, err := resolver.ResolvePlayback(ctx, input)
		var media *player.Media
		if err == nil && (result.Plan.Access == "" || result.Plan.Access == provider.VideoAccessGranted) && result.Plan.Restriction == "" {
			media, err = player.OpenMedia(ctx, result)
			if err == nil && media != nil {
				_ = media.Warmup(ctx)
			}
		}
		if ctx.Err() != nil || err != nil || media == nil {
			if media != nil {
				media.Close()
			}
			p.post(func() {
				if p.preloadRequest == request {
					cancel()
					p.preloadCancel = nil
					p.preloadTarget = provider.PlaybackTarget{}
					p.preloadOwner = ""
				}
			})
			return
		}
		p.post(func() {
			if !p.active || p.preloadRequest != request || ctx.Err() != nil || p.owner != owner {
				media.CloseAsync()
				return
			}
			cancel()
			p.preloadCancel = nil
			p.preloaded = &preloadedPlayback{target: target, owner: owner, preferredQuality: prefQ, result: result, media: media}
		})
	}()
}

func (p *Playback) resetAutoQuality(clearHDRCooldown bool) {
	p.autoQuality = ""
	if clearHDRCooldown {
		p.autoHDRBlocked = false
	}
	p.autoStallCount = 0
	p.autoStallSince = time.Time{}
	p.autoStallActive = false
	p.autoLastSwitch = time.Time{}
	p.autoHealthyAt = time.Time{}
	p.seekSettledAt = time.Time{}
}

func (p *Playback) effectiveQuality() string {
	if p.state.Selected != "" {
		return p.state.Selected
	}
	return p.autoQuality
}

func (p *Playback) resolve(quality string, preserve, useResume bool) {
	p.resolveMode(quality, quality, p.state.SelectedLine, preserve, useResume)
}

func (p *Playback) resolveMode(selected, requestQuality, selectedLine string, preserve, useResume bool) {
	if p.cancel != nil {
		p.cancel()
	}
	if p.audioCancel != nil {
		p.audioCancel()
		p.audioCancel = nil
	}
	p.audioRequest++
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel
	p.request++
	request := p.request
	p.trickRate = 0
	p.state.Loading, p.state.Switching = !preserve, preserve
	p.state.Failure, p.state.Limit, p.state.Fallback = "", "", ""
	p.state.LiveEnded = false
	var caps player.Capabilities
	if p.engine.Capabilities != nil {
		caps = p.engine.Capabilities()
	}
	if p.hdrBlocked || (selected == "" && p.autoHDRBlocked) {
		caps.HDR = false
	}
	preferredQ := ""
	if requestQuality == "" {
		preferredQ = p.preferredQuality()
	}
	selectedAudio := p.selectedAudio
	mediaKind := p.mediaKind
	resolver, _ := p.catalog.session.(provider.PlaybackReader)
	input := provider.PlaybackRequest{
		PlaybackTarget: p.state.Target, Quality: requestQuality, PreferredQuality: preferredQ, Line: selectedLine,
		AudioTrack: selectedAudio, MediaKind: mediaKind, BranchID: p.branchID, BranchState: p.branchState, HDR: caps.HDR, HEVC: caps.HEVC,
	}
	go func() {
		var result provider.Playback
		var media *player.Media
		var err error
		if resolver == nil {
			err = &provider.Error{Code: provider.Unsupported}
		} else {
			result, err = resolver.ResolvePlayback(ctx, input)
		}
		if err == nil && result.Plan.Access != provider.VideoAccessRestricted && !result.Plan.LiveEnded && (!preserve || result.Plan.Restriction == "") {
			timeout, stop := context.WithTimeout(ctx, 15*time.Second)
			media, err = player.OpenMediaWithAudio(timeout, result, selectedAudio)
			stop()
			// While switching media kind or quality in place, pre-buffer opening
			// bytes into RAM before touching the live player so audio stays smooth.
			if err == nil && media != nil && preserve {
				warmCtx, stopWarm := context.WithTimeout(ctx, 2500*time.Millisecond)
				_ = media.Warmup(warmCtx)
				stopWarm()
			}
		}
		p.post(func() {
			if !p.active || p.request != request || ctx.Err() != nil {
				if media != nil {
					media.CloseAsync()
				}
				return
			}
			cancel()
			p.cancel = nil
			p.accept(result, media, err, selected, selectedLine, preserve, useResume)
			p.changed()
		})
	}()
	p.changed()
}

func (p *Playback) accept(result provider.Playback, media *player.Media, err error, quality, selectedLine string, preserve, useResume bool) {
	p.state.Loading, p.state.Switching = false, false
	if err != nil {
		if result.Plan.Live && len(result.Plan.Lines) > 0 {
			p.state.Resolution.Plan.Live = true
			p.state.Resolution.Plan.Lines = result.Plan.Lines
			p.state.Resolution.Plan.Line = result.Plan.Line
		}
		p.state.Failure = provider.CodeOf(err)
		if p.state.Failure == provider.Expired && !p.refreshed && !preserve {
			p.refreshed = true
			p.resolveMode(quality, p.effectiveQuality(), selectedLine, false, useResume)
			return
		}
		if p.state.Failure == provider.NeedsSignIn {
			p.state.Limit, p.state.Failure = provider.PlaybackLogin, ""
		}
		if preserve && p.media != nil {
			p.selectedAudio = p.state.SelectedAudio
			if quality == "" {
				p.state.Limit = ""
			} else if p.state.Limit == "" {
				p.state.Limit = provider.PlaybackSource
			}
			p.state.Failure = ""
		}
		return
	}
	plan := result.Plan
	if plan.Live && plan.LiveEnded {
		if media != nil {
			media.CloseAsync()
		}
		p.clearStream()
		p.state.Resolution = result
		p.state.Selected, p.state.SelectedLine = quality, selectedLine
		p.state.LiveEnded = true
		p.state.Playing = false
		p.state.Failure, p.state.Limit, p.state.Fallback = "", "", ""
		p.didEnd = true
		p.state.Revision++
		return
	}
	endedPreview := p.didEnd && p.state.Resolution.Plan.Access == provider.VideoAccessPreview
	p.state.Limit = plan.Restriction
	if preserve && plan.Restriction != "" {
		if quality == "" {
			p.state.Limit = ""
		} else {
			p.state.Fallback = plan.Quality
		}
		return
	}
	if endedPreview && (plan.Access == provider.VideoAccessPreview || plan.Access == provider.VideoAccessRestricted) {
		if media != nil {
			media.CloseAsync()
		}
		// An unchanged entitlement keeps the finished preview and its last frame.
		p.state.Limit = ""
		return
	}
	if plan.Access == provider.VideoAccessRestricted {
		if plan.PartID != "" {
			p.state.Target.PartID = plan.PartID
		}
		p.state.AudioOnly = p.mediaKind == provider.PlaybackMediaAudio ||
			(len(plan.AvailableKinds) > 0 && plan.AvailableKinds[0] == provider.PlaybackMediaAudio)
		p.state.Resolution = result
		p.state.Revision++
		if p.state.Limit == "" {
			p.state.Limit = provider.PlaybackSource
		}
		return
	}
	target := p.state.Target
	target.PartID = plan.PartID
	position := p.state.Position
	if plan.Live {
		position = 0
		p.rate = 1
	} else if plan.Branch != nil {
		if !preserve {
			position = 0
		}
	} else if useResume {
		if p.syncSource && position == 0 {
			position = plan.ResumeSeconds
		}
		if p.store != nil {
			p.flushPositionSaves()
			if saved, ok := p.store.PlaybackPosition(p.owner, target); ok {
				position = saved
			}
		}
	}
	end := plan.Duration
	if useResume && plan.Access == provider.VideoAccessPreview {
		// An exhausted trial can be saved as the preceding whole second.
		end = math.Floor(end)
	}
	if !plan.Live && plan.Duration > 0 && position >= end {
		position = plan.Duration
		if useResume {
			position = 0
		}
	}
	if p.engine.SetAllowScreensaver != nil {
		p.engine.SetAllowScreensaver(p.allowAudioScreensaver())
	}
	if p.engine.SetAudioOnly != nil {
		p.engine.SetAudioOnly(media != nil && media.AudioOnly)
	}
	if media != nil {
		media.Promote()
	}
	if p.engine.Open == nil || media == nil || !p.engine.Open(media.URL, position) {
		if media != nil {
			media.CloseAsync()
		}
		p.state.Failure = provider.Unsupported
		return
	}
	if p.media != nil {
		if preserve && p.state.Target.Ref == target.Ref && p.state.Target.PartID == target.PartID &&
			p.media.AudioOnly != media.AudioOnly && p.state.Failure == "" {
			p.clearAltMode()
			p.altMode = &cachedMediaMode{
				target:        p.state.Target,
				result:        p.state.Resolution,
				media:         p.media,
				quality:       p.state.Selected,
				selectedLine:  p.state.SelectedLine,
				selectedAudio: p.selectedAudio,
			}
		} else {
			p.media.CloseAsync()
		}
	}
	p.media = media
	p.liveEndChecked = false
	p.state.LiveEnded = false
	if plan.Branch != nil {
		if plan.Branch.State != "" {
			p.branchState = plan.Branch.State
		}
		if !preserve {
			p.branchTriggered = false
			p.state.BranchPending = false
			p.state.BranchDismissed = false
		}
	} else if !preserve {
		p.branchTriggered = false
		p.state.BranchPending = false
		p.state.BranchDismissed = false
	}
	if endedPreview || p.state.PreviewPending && plan.Access != provider.VideoAccessPreview {
		p.didEnd, p.state.Playing = false, true
	}
	if plan.Access == provider.VideoAccessPreview && !p.previewConfirmed {
		p.state.PreviewPending, p.state.Playing = true, false
	} else if plan.Access != provider.VideoAccessPreview {
		p.state.PreviewPending = false
	}
	p.state.Target, p.state.Resolution = target, result
	p.state.Selected, p.state.SelectedLine = quality, selectedLine
	if tracks := media.AudioTracks(); len(tracks) > 0 {
		p.state.AudioTracks = tracks
		p.state.DefaultAudio = media.DefaultAudioID()
		p.state.ActiveAudio = media.ActiveAudioID()
	} else if len(plan.AudioTracks) > 0 {
		p.state.AudioTracks = slices.Clone(plan.AudioTracks)
		p.state.DefaultAudio = defaultPlanAudioID(plan.AudioTracks)
		p.state.ActiveAudio = activePlanAudioID(plan.AudioTracks, plan.AudioTrack, p.selectedAudio)
	} else {
		p.state.AudioTracks = nil
		p.state.DefaultAudio = ""
		p.state.ActiveAudio = ""
	}
	if p.selectedAudio != "" && !slices.ContainsFunc(p.state.AudioTracks, func(t provider.PlaybackTrack) bool { return t.ID == p.selectedAudio }) {
		p.selectedAudio = ""
	}
	p.state.SelectedAudio = p.selectedAudio
	p.state.Position, p.state.Duration, p.state.VideoRange = position, plan.Duration, media.VideoRange
	p.state.AudioOnly = media.AudioOnly
	p.state.AudioDimmed = false
	p.state.Clock = player.Status{Position: position, Buffering: true, AudioOnly: media.AudioOnly}
	p.startedAt = time.Now()
	p.positionAt, p.bufferedAt = time.Time{}, time.Time{}
	p.autoStallCount = 0
	p.autoStallSince = time.Time{}
	p.autoStallActive = false
	p.state.Revision++
	p.applyRate()
}

func defaultPlanAudioID(tracks []provider.PlaybackTrack) string {
	for _, track := range tracks {
		if track.Default {
			return track.ID
		}
	}
	if len(tracks) > 0 {
		return tracks[0].ID
	}
	return ""
}

func activePlanAudioID(tracks []provider.PlaybackTrack, planAudio, selectedAudio string) string {
	if planAudio != "" && slices.ContainsFunc(tracks, func(t provider.PlaybackTrack) bool { return t.ID == planAudio }) {
		return planAudio
	}
	if selectedAudio != "" && slices.ContainsFunc(tracks, func(t provider.PlaybackTrack) bool { return t.ID == selectedAudio }) {
		return selectedAudio
	}
	return defaultPlanAudioID(tracks)
}

func (p *Playback) Tick(now time.Time) {
	if !p.active {
		return
	}
	if p.media != nil {
		p.state.BytesPerSecond = p.media.BytesPerSecond(now)
	} else {
		p.state.BytesPerSecond = 0
	}
	if p.media == nil || p.engine.Snapshot == nil {
		return
	}
	clock := p.engine.Snapshot()
	if clock.Duration > 0 {
		p.state.Duration = clock.Duration
	}
	if clock.HasFrame || clock.Position > 0 {
		p.state.Position = clock.Position
	}
	p.state.Clock = clock
	p.loaded = p.loaded || clock.HasFrame
	live := p.isLive()
	if !live && !p.state.Loading && !p.state.Switching && !p.state.PreviewPending && p.state.Failure == "" {
		if branch := p.state.Resolution.Plan.Branch; branch != nil && len(branch.Options) > 0 {
			if branch.AutoAdvance && len(branch.Options) == 1 {
				if clock.Ended || (p.state.Duration > 0 && clock.HasFrame && p.state.Position >= p.state.Duration-0.15) {
					p.SelectBranch(branch.Options[0].ID)
					return
				}
			} else {
				trigger := branch.TriggerSeconds
				if trigger <= 0 || (p.state.Duration > 0 && trigger > p.state.Duration) {
					trigger = max(0, p.state.Duration-0.3)
				}
				reached := !p.branchTriggered && p.state.Playing && clock.HasFrame && p.state.Position >= trigger
				if reached || (clock.Ended && !p.state.BranchPending) {
					p.branchTriggered = true
					p.state.BranchPending = true
					p.state.BranchDismissed = false
					p.state.Playing = false
					p.trickRate = 0
					p.didEnd = false
					p.applyRate()
					p.changed()
					return
				}
			}
		}
	}
	if clock.Ended && !p.didEnd {
		if live && !p.liveEndChecked {
			p.liveEndChecked = true
			p.resolveMode(p.state.Selected, p.effectiveQuality(), p.state.SelectedLine, false, false)
			return
		}
		p.didEnd, p.state.Playing = true, false
		p.trickRate = 0
		if live {
			p.state.LiveEnded = true
		} else {
			p.save()
		}
	}
	if p.state.AudioOnly {
		if p.engine.SetAllowScreensaver != nil {
			p.engine.SetAllowScreensaver(p.allowAudioScreensaver())
		}
		if !p.state.Playing && !p.state.Loading && !p.state.Switching {
			if p.pausedAt.IsZero() {
				p.pausedAt = now
			}
			p.state.AudioDimmed = now.Sub(p.pausedAt) >= audioPauseDimDelay
		} else {
			p.pausedAt = time.Time{}
			p.state.AudioDimmed = false
		}
	} else {
		p.state.AudioDimmed = false
	}
	if p.state.Loading || p.state.Switching || p.state.PreviewPending || p.state.Failure != "" || p.didEnd {
		return
	}
	code := p.media.Failure()
	plan := p.state.Resolution.Plan
	expired := code == provider.Expired || plan.ExpiresAt > 0 && now.Unix() >= plan.ExpiresAt
	if !p.refreshed && expired && (clock.Failed || clock.Buffering) {
		p.refreshed = true
		p.resolveMode(p.state.Selected, p.effectiveQuality(), p.state.SelectedLine, false, false)
		return
	}
	if clock.Failed && clock.DisplayIncompatible && !p.hdrBlocked && (p.state.VideoRange == "PQ" || p.state.VideoRange == "HLG") {
		p.hdrBlocked = true
		p.resolveMode(p.state.Selected, p.effectiveQuality(), p.state.SelectedLine, false, false)
		return
	}
	trickScanning := p.trickRate < 0 || p.trickRate > 2
	if clock.Buffering && !trickScanning {
		if p.bufferedAt.IsZero() {
			p.bufferedAt = now
		}
	} else {
		p.bufferedAt = time.Time{}
	}
	if !live && p.stepAutoQuality(now, clock) {
		return
	}
	timedOut := !trickScanning && (!clock.HasFrame && now.Sub(p.startedAt) > playbackStallTimeout ||
		!p.bufferedAt.IsZero() && now.Sub(p.bufferedAt) > playbackStallTimeout)
	if clock.Failed || timedOut {
		if code == "" {
			code = provider.Network
			if timedOut {
				code = provider.Timeout
			}
		}
		p.trickRate = 0
		p.state.Failure = code
		if p.engine.SetRate != nil {
			p.engine.SetRate(0)
		}
		return
	}
	if live {
		return
	}
	if p.positionAt.IsZero() {
		p.positionAt = now
	}
	if p.state.AudioOnly && p.loaded && !p.reportedStart && p.state.Playing &&
		p.state.Target.Ref.ID != "" && p.state.Resolution.Plan.Branch == nil && p.state.BranchHistory == 0 &&
		(p.state.Position >= 1 || now.Sub(p.positionAt) >= 2*time.Second) {
		p.reportedStart = true
		p.queueReport(p.state.Target, 0, false, false)
	}
	if p.state.Playing && now.Sub(p.positionAt) >= 30*time.Second {
		p.saveAsync()
		p.positionAt = now
	}
}

func autoPlayableQualities(qualities []provider.PlaybackQuality) []provider.PlaybackQuality {
	hasBandwidth := false
	for _, q := range qualities {
		if q.Bandwidth > 0 {
			hasBandwidth = true
			break
		}
	}
	playable := make([]provider.PlaybackQuality, 0, len(qualities))
	for _, q := range qualities {
		if q.Limit != "" {
			continue
		}
		if hasBandwidth && q.Bandwidth <= 0 {
			continue
		}
		playable = append(playable, q)
	}
	return playable
}

func (p *Playback) stepAutoQuality(now time.Time, clock player.Status) bool {
	if p.state.Selected != "" || p.preferredQuality() != "" || !p.state.Playing || p.media == nil || p.trickRate != 0 {
		return false
	}
	if clock.Seeking {
		p.seekSettledAt = now
		p.autoStallActive = false
		p.autoHealthyAt = time.Time{}
		return false
	}
	seekGrace := !p.seekSettledAt.IsZero() && now.Sub(p.seekSettledAt) < seekAutoQualityGrace
	startupGrace := !p.startedAt.IsZero() && now.Sub(p.startedAt) < seekAutoQualityGrace
	isRebuffering := clock.Buffering && clock.HasFrame && !seekGrace
	if isRebuffering && !startupGrace && !p.autoStallActive {
		if p.autoStallSince.IsZero() || now.Sub(p.autoStallSince) > autoStallWindow {
			p.autoStallSince = now
			p.autoStallCount = 1
		} else {
			p.autoStallCount++
		}
	}
	p.autoStallActive = isRebuffering && !startupGrace

	plan := p.state.Resolution.Plan
	playable := autoPlayableQualities(plan.Qualities)
	if len(playable) < 2 {
		return false
	}
	curIdx := -1
	for i, q := range playable {
		if q.ID == plan.Quality {
			curIdx = i
			break
		}
	}
	if curIdx < 0 {
		return false
	}
	cur := playable[curIdx]

	// Step down when Auto mode hits sustained rebuffering or repeated stalls.
	if curIdx+1 < len(playable) && (p.autoLastSwitch.IsZero() || now.Sub(p.autoLastSwitch) >= autoDowngradeCooldown) {
		next := playable[curIdx+1]
		curIsHDR := cur.HDR || p.state.VideoRange == "PQ" || p.state.VideoRange == "HLG" || clock.HDRVideo
		crossingHDR := curIsHDR && !next.HDR
		rebufNeed := autoSDRRebufferLimit
		startupNeed := autoSDRStartupLimit
		if crossingHDR {
			rebufNeed = autoHDRRebufferLimit
			startupNeed = autoHDRStartupLimit
		}
		if p.media.HasHealthyBackup() {
			rebufNeed += autoCDNGraceExtra
		}
		reqBps := p.media.BandwidthBps()
		if reqBps <= 0 && cur.Bandwidth > 0 {
			reqBps = float64(cur.Bandwidth) / 8.0
		}
		speed := p.state.BytesPerSecond
		if speed <= 0 {
			speed = p.media.TransferBps()
		}
		underfed := reqBps <= 0 || speed < reqBps*0.8
		rebufStart := p.bufferedAt
		if !p.seekSettledAt.IsZero() && p.seekSettledAt.After(rebufStart) {
			rebufStart = p.seekSettledAt
		}
		longRebuffer := isRebuffering && !rebufStart.IsZero() && now.Sub(rebufStart) >= rebufNeed
		repeatedStalls := isRebuffering && !startupGrace && p.autoStallCount >= 2 && !p.autoStallSince.IsZero() &&
			now.Sub(p.autoStallSince) <= autoStallWindow && underfed
		slowStartup := !clock.HasFrame && now.Sub(p.startedAt) >= startupNeed && underfed
		if longRebuffer || repeatedStalls || slowStartup {
			if playbackTrace {
				log.Printf("player-auto: downgrade %s->%s long=%t repeated=%t(%d) startup=%t speed=%.0f req=%.0f",
					cur.ID, next.ID, longRebuffer, repeatedStalls, p.autoStallCount, slowStartup, speed, reqBps)
			}
			if crossingHDR {
				p.autoHDRBlocked = true
			}
			p.autoQuality = next.ID
			p.autoLastSwitch = now
			p.autoStallCount = 0
			p.autoStallSince = time.Time{}
			p.autoHealthyAt = time.Time{}
			p.resolveMode("", next.ID, p.state.SelectedLine, true, false)
			return true
		}
	}

	// Step back up within SDR once buffer and active transfer speed stay healthy.
	if p.autoQuality != "" && curIdx > 0 && now.Sub(p.autoLastSwitch) >= autoUpgradeCooldown {
		higher := playable[curIdx-1]
		if higher.HDR && p.autoHDRBlocked {
			p.autoHealthyAt = time.Time{}
			return false
		}
		bufferAhead := max(0, clock.BufferEnd-p.state.Position)
		higherReqBps := float64(higher.Bandwidth) / 8.0
		transferBps := p.media.TransferBps()
		if transferBps <= 0 {
			transferBps = p.state.BytesPerSecond
		}
		healthy := !clock.Buffering && p.loaded && bufferAhead >= autoUpgradeMinBufferSec &&
			higherReqBps > 0 && transferBps >= higherReqBps*autoUpgradeSpeedMargin
		if !healthy {
			p.autoHealthyAt = time.Time{}
			return false
		}
		if p.autoHealthyAt.IsZero() {
			p.autoHealthyAt = now
			return false
		}
		if now.Sub(p.autoHealthyAt) >= autoUpgradeHealthyWindow {
			if playbackTrace {
				log.Printf("player-auto: upgrade %s->%s bufAhead=%.1fs transfer=%.0f req=%.0f",
					cur.ID, higher.ID, bufferAhead, transferBps, higherReqBps)
			}
			nextAuto := higher.ID
			if curIdx-1 == 0 {
				nextAuto = ""
			}
			p.autoQuality = nextAuto
			p.autoLastSwitch = now
			p.autoHealthyAt = time.Time{}
			p.resolveMode("", higher.ID, p.state.SelectedLine, true, false)
			return true
		}
	}
	return false
}

func (p *Playback) Toggle() {
	if !p.active || p.state.Loading || p.state.PreviewPending || p.state.BranchPending || p.state.LiveEnded {
		return
	}
	if p.state.BranchDismissed && !p.state.Playing {
		if branch := p.state.Resolution.Plan.Branch; branch != nil && len(branch.Options) > 0 && !branch.AutoAdvance {
			if p.state.Clock.Ended || (p.state.Duration > 0 && p.state.Position >= p.state.Duration-0.2) {
				p.OpenBranch()
				return
			}
		}
	}
	if p.didEnd || p.state.Failure != "" {
		p.Retry()
		return
	}
	p.trickRate = 0
	p.state.Playing = !p.state.Playing
	if !p.state.Playing {
		if p.isLive() {
			p.pausedAt = time.Now()
		} else {
			p.save()
		}
	} else if p.isLive() && !p.pausedAt.IsZero() && time.Since(p.pausedAt) >= 4*time.Second {
		p.pausedAt = time.Time{}
		p.Retry()
		return
	}
	p.applyRate()
	p.changed()
}

func (p *Playback) SetRate(rate float64) {
	if p.isLive() {
		rate = 1
	}
	p.rate = rate
	p.trickRate = 0
	p.applyRate()
	p.changed()
}

// SetTrickRate applies a temporary fast-forward or rewind scan rate without
// altering the user's configured playback speed. Rates in (0, 2] play smoothly
// through AVPlayer; negative rates or rates above 2 pause native playback so
// the UI can drive throttled keyframe seeks cleanly across DASH/HLS/MP4.
func (p *Playback) SetTrickRate(rate float64) {
	if rate == 0 {
		if p.trickRate == 0 {
			return
		}
		p.trickRate = 0
		if p.active {
			p.applyRate()
			p.changed()
		}
		return
	}
	if !p.active || p.isLive() || p.state.Loading || p.state.PreviewPending || p.state.BranchPending || p.state.LiveEnded {
		return
	}
	if p.trickRate == rate {
		return
	}
	p.trickRate = rate
	p.bufferedAt = time.Time{}
	p.autoHealthyAt = time.Time{}
	p.autoStallActive = false
	p.applyRate()
	p.changed()
}

func (p *Playback) applyRate() {
	if p.engine.SetRate == nil {
		return
	}
	rate := float64(0)
	if p.trickRate != 0 && !p.isLive() {
		if p.trickRate > 0 && p.trickRate <= 2 {
			rate = p.trickRate
		}
	} else if p.state.Playing {
		rate = p.rate
		if p.isLive() {
			rate = 1
		}
	}
	p.engine.SetRate(rate)
}

func (p *Playback) Seek(seconds float64) {
	if p.media == nil || p.isLive() {
		return
	}
	p.didEnd, p.state.Clock.Ended = false, false
	p.state.Position = max(0, seconds)
	if p.state.Duration > 0 {
		p.state.Position = min(p.state.Duration, p.state.Position)
		if p.state.Duration-p.state.Position > 30 {
			p.clearPreload()
		}
	}
	if branch := p.state.Resolution.Plan.Branch; branch != nil && len(branch.Options) > 0 {
		trigger := branch.TriggerSeconds
		if trigger <= 0 || (p.state.Duration > 0 && trigger > p.state.Duration) {
			trigger = max(0, p.state.Duration-0.3)
		}
		if p.state.Position < trigger-0.8 {
			p.branchTriggered = false
			p.state.BranchPending = false
			p.state.BranchDismissed = false
		}
	}
	p.autoHealthyAt = time.Time{}
	p.autoStallActive = false
	p.seekSettledAt = time.Now()
	if expires := p.state.Resolution.Plan.ExpiresAt; expires > 0 && time.Now().Unix() >= expires {
		p.refreshed = false
		p.resolveMode(p.state.Selected, p.effectiveQuality(), p.state.SelectedLine, false, false)
		return
	}
	if p.engine.Seek != nil {
		p.engine.Seek(p.state.Position)
	}
	p.applyRate()
	p.changed()
}

func (p *Playback) ChooseQuality(quality string) {
	if !p.active || p.state.Loading || p.state.Switching {
		return
	}
	if quality == p.state.Selected && (quality != "" || p.autoQuality == "") && p.media != nil && p.state.Failure == "" {
		return
	}
	p.trickRate = 0
	p.refreshed = false
	p.resetAutoQuality(quality == "")
	p.resolveMode(quality, quality, p.state.SelectedLine, p.media != nil && p.state.Failure == "", false)
}

func (p *Playback) SwitchMediaKind(kind string) bool {
	if !p.active || p.isLive() || p.state.Loading || p.state.Switching {
		return false
	}
	if kind != provider.PlaybackMediaAudio && kind != provider.PlaybackMediaVideo {
		return false
	}
	if !slices.Contains(p.state.Resolution.Plan.AvailableKinds, kind) {
		return false
	}
	wantAudio := kind == provider.PlaybackMediaAudio
	if wantAudio == p.state.AudioOnly && p.media != nil && p.state.Failure == "" {
		return true
	}
	p.trickRate = 0
	p.refreshed = false
	p.mediaKind = kind
	p.resetAutoQuality(true)
	if alt := p.altMode; alt != nil && alt.media != nil &&
		alt.target.Ref == p.state.Target.Ref && alt.target.PartID == p.state.Target.PartID &&
		alt.media.AudioOnly == wantAudio &&
		(alt.result.Plan.ExpiresAt == 0 || time.Now().Unix()+preloadExpiryMarginSec < alt.result.Plan.ExpiresAt) {
		p.altMode = nil
		p.selectedAudio = alt.selectedAudio
		p.accept(alt.result, alt.media, nil, alt.quality, alt.selectedLine, true, false)
		p.changed()
		return true
	}
	p.selectedAudio = ""
	p.resolveMode("", "", p.state.SelectedLine, p.media != nil && p.state.Failure == "", false)
	return true
}

func (p *Playback) ChooseLine(line string) {
	if !p.active || p.state.Loading || p.state.Switching {
		return
	}
	if line == p.state.SelectedLine && p.media != nil && p.state.Failure == "" && !p.state.LiveEnded {
		return
	}
	p.trickRate = 0
	p.state.Playing = true
	p.state.Limit = ""
	p.loaded, p.didEnd, p.refreshed, p.liveEndChecked = false, false, false, false
	p.resolveMode(p.state.Selected, p.effectiveQuality(), line, false, false)
}

func (p *Playback) ChooseAudioTrack(trackID string) bool {
	if !p.active || p.isLive() || p.state.Loading || p.state.Switching || p.media == nil || trackID == "" {
		return false
	}
	if !slices.ContainsFunc(p.state.AudioTracks, func(t provider.PlaybackTrack) bool { return t.ID == trackID }) {
		return false
	}
	if trackID == p.state.ActiveAudio {
		return true
	}
	if len(p.media.AudioTracks()) == 0 && len(p.state.Resolution.Plan.AudioTracks) > 0 {
		p.selectedAudio = trackID
		p.resolveMode(p.state.Selected, p.effectiveQuality(), p.state.SelectedLine, p.media != nil && p.state.Failure == "", false)
		return true
	}
	prev := p.state.ActiveAudio
	if p.engine.SelectAudio != nil && p.media.CanSwitchAudioInPlace(prev, trackID) {
		if name, ok := p.media.AudioOptionName(trackID); ok && p.engine.SelectAudio(name) {
			p.selectedAudio = trackID
			p.state.SelectedAudio = trackID
			p.state.ActiveAudio = trackID
			p.changed()
			return true
		}
	}
	if p.audioCancel != nil {
		p.audioCancel()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	p.audioCancel = cancel
	p.audioRequest++
	reqID := p.audioRequest
	resolution := p.state.Resolution
	p.state.Switching = true
	p.changed()
	go func() {
		media, err := player.OpenMediaWithAudio(ctx, resolution, trackID)
		p.post(func() {
			if !p.active || p.audioRequest != reqID {
				if media != nil {
					media.CloseAsync()
				}
				return
			}
			ctxErr := ctx.Err()
			cancel()
			p.audioCancel = nil
			p.state.Switching = false
			if err != nil || media == nil || ctxErr != nil {
				if media != nil {
					media.CloseAsync()
				}
				p.changed()
				return
			}
			position := p.state.Position
			if p.engine.Open == nil || !p.engine.Open(media.URL, position) {
				media.CloseAsync()
				p.changed()
				return
			}
			if p.media != nil {
				p.media.CloseAsync()
			}
			p.media = media
			p.selectedAudio = trackID
			p.state.SelectedAudio = trackID
			p.state.AudioTracks = media.AudioTracks()
			p.state.DefaultAudio = media.DefaultAudioID()
			p.state.ActiveAudio = media.ActiveAudioID()
			p.state.VideoRange = media.VideoRange
			p.state.Clock = player.Status{Position: position, Buffering: true}
			p.startedAt = time.Now()
			p.bufferedAt = time.Time{}
			p.state.Revision++
			p.applyRate()
			p.changed()
		})
	}()
	return true
}

func (p *Playback) DismissLimit() {
	p.state.Limit, p.state.Fallback = "", ""
	p.changed()
}

func (p *Playback) StartPreview() {
	if !p.state.PreviewPending {
		return
	}
	p.state.PreviewPending, p.state.Playing = false, true
	p.previewConfirmed = true
	p.applyRate()
	p.changed()
}

func (p *Playback) RefreshAccess() {
	if !p.active || p.state.Loading || p.state.Switching {
		return
	}
	p.refreshed = false
	p.resolveMode(p.state.Selected, p.effectiveQuality(), p.state.SelectedLine, false, false)
}

func (p *Playback) OpenBranch() {
	if !p.active || p.state.Loading || p.state.Switching || p.state.PreviewPending || p.state.LiveEnded {
		return
	}
	branch := p.state.Resolution.Plan.Branch
	if branch == nil || len(branch.Options) == 0 || branch.AutoAdvance {
		return
	}
	p.branchTriggered = true
	p.state.BranchPending = true
	p.state.BranchDismissed = false
	p.state.Playing = false
	p.trickRate = 0
	p.applyRate()
	p.changed()
}

func (p *Playback) DismissBranch() {
	if !p.state.BranchPending {
		return
	}
	p.state.BranchPending = false
	p.state.BranchDismissed = true
	p.changed()
}

func (p *Playback) SelectBranch(optionID string) bool {
	if !p.active || p.state.Loading || p.state.Switching {
		return false
	}
	branch := p.state.Resolution.Plan.Branch
	if branch == nil {
		return false
	}
	var chosen *provider.PlaybackBranchOption
	for i := range branch.Options {
		if branch.Options[i].ID == optionID {
			chosen = &branch.Options[i]
			break
		}
	}
	if chosen == nil {
		return false
	}
	p.trickRate = 0
	p.clearPreload()
	p.clearAltMode()
	p.clearStream()
	if chosen.PartID != "" {
		p.state.Target.PartID = chosen.PartID
	}
	p.branchID = chosen.ID
	if chosen.State != "" {
		p.branchState = chosen.State
	}
	p.branchTriggered = false
	p.state.BranchPending = false
	p.state.BranchDismissed = false
	p.state.BranchHistory++
	p.state.Position = 0
	p.state.Duration = 0
	p.state.Clock = player.Status{Buffering: true}
	p.state.Playing = true
	p.state.Limit = ""
	p.loaded, p.didEnd, p.refreshed, p.liveEndChecked = false, false, false, false
	p.resolveMode(p.state.Selected, p.effectiveQuality(), p.state.SelectedLine, false, false)
	return true
}

func (p *Playback) Retry() {
	if !p.active {
		return
	}
	p.trickRate = 0
	p.state.Playing = true
	p.state.Limit = ""
	p.state.LiveEnded = false
	if p.didEnd && p.state.BranchHistory > 0 {
		p.branchID, p.branchState, p.branchTriggered = "", "", false
		p.state.BranchPending, p.state.BranchDismissed, p.state.BranchHistory = false, false, 0
		p.state.Target.PartID = ""
		p.state.Position = 0
		p.loaded, p.didEnd, p.refreshed, p.liveEndChecked = false, false, false, false
		p.resolveMode(p.state.Selected, p.effectiveQuality(), p.state.SelectedLine, false, false)
		return
	}
	if !p.isLive() && p.didEnd && p.media != nil && p.state.Resolution.Plan.Access != provider.VideoAccessPreview {
		p.Seek(0)
		return
	}
	if p.didEnd {
		p.state.Position = 0
	}
	p.loaded, p.didEnd, p.refreshed, p.liveEndChecked = false, false, false, false
	p.resolveMode(p.state.Selected, p.effectiveQuality(), p.state.SelectedLine, false, false)
}

func (p *Playback) Suspend() {
	p.StopInlinePreview()
	if p.active {
		p.save()
		p.bufferedAt = time.Time{}
	}
}

func (p *Playback) InlinePreviewState() InlinePreviewState {
	if p == nil {
		return InlinePreviewState{}
	}
	return p.inlineState
}

func (p *Playback) SetInlinePreviewRect(rect player.ViewportRect) {
	if p == nil || !p.inlineActive || p.active || p.engine.ViewportRect == nil {
		return
	}
	p.engine.ViewportRect(rect)
}

// StartInlinePreview resolves and plays a low-friction SDR preview stream with
// audio inside the Home Hero without recording watch history or triggering
// display mode switching.
func (p *Playback) StartInlinePreview(target provider.PlaybackTarget) {
	if p == nil || p.active || target.Ref.ID == "" || !target.Ref.Kind.Valid() || p.post == nil || p.catalog == nil {
		return
	}
	if p.inlineActive && p.inlineState.Target == target && !p.inlineState.Failed {
		return
	}
	p.StopInlinePreview()
	resolver, _ := p.catalog.session.(provider.PlaybackReader)
	if resolver == nil {
		p.inlineState = InlinePreviewState{Target: target, Failed: true}
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	p.inlineCancel = cancel
	p.inlineRequest++
	reqID := p.inlineRequest
	p.inlineActive = true
	p.inlineState = InlinePreviewState{Target: target, Active: true, Loading: true}
	var caps player.Capabilities
	if p.engine.Capabilities != nil {
		caps = p.engine.Capabilities()
	}
	input := provider.PlaybackRequest{
		PlaybackTarget:   target,
		PreferredQuality: "1080p",
		HDR:              false,
		HEVC:             caps.HEVC,
	}
	go func() {
		result, err := resolver.ResolvePlayback(ctx, input)
		var media *player.Media
		if err == nil && (result.Plan.Access == "" || result.Plan.Access == provider.VideoAccessGranted) &&
			result.Plan.Restriction == "" && !result.Plan.LiveEnded {
			media, err = player.OpenMedia(ctx, result)
		}
		ctxErr := ctx.Err()
		p.post(func() {
			if !p.inlineActive || p.active || p.inlineRequest != reqID {
				if media != nil {
					media.CloseAsync()
				}
				return
			}
			cancel()
			p.inlineCancel = nil
			if err == nil && ctxErr != nil {
				err = ctxErr
			}
			if err != nil || media == nil {
				if media != nil {
					media.CloseAsync()
				}
				p.inlineActive = false
				p.inlineState = InlinePreviewState{Target: target, Failed: true}
				if p.changed != nil {
					p.changed()
				}
				return
			}
			if p.engine.SetAudioOnly != nil {
				p.engine.SetAudioOnly(media.AudioOnly)
			}
			if p.engine.Preview != nil {
				p.engine.Preview(true)
			}
			if p.engine.Open == nil || !p.engine.Open(media.URL, 0) {
				media.CloseAsync()
				if p.engine.SetAudioOnly != nil {
					p.engine.SetAudioOnly(false)
				}
				if p.engine.Preview != nil {
					p.engine.Preview(false)
				}
				p.inlineActive = false
				p.inlineState = InlinePreviewState{Target: target, Failed: true}
				if p.changed != nil {
					p.changed()
				}
				return
			}
			p.inlineMedia = media
			p.inlineStartedAt = time.Now()
			p.inlineState.Loading = false
			p.inlineState.AudioOnly = media.AudioOnly
			if p.engine.SetRate != nil {
				p.engine.SetRate(1)
			}
			if p.changed != nil {
				p.changed()
			}
		})
	}()
}

func (p *Playback) StopInlinePreview() {
	if p == nil {
		return
	}
	started := time.Now()
	if p.inlineCancel != nil {
		p.inlineCancel()
		p.inlineCancel = nil
	}
	p.inlineRequest++
	wasActive := p.inlineActive || p.inlineMedia != nil
	p.inlineActive = false
	p.inlineStartedAt = time.Time{}
	p.inlineState = InlinePreviewState{}
	var stopDur, previewDur, closeDur time.Duration
	if !p.active && wasActive {
		if p.engine.Stop != nil {
			t0 := time.Now()
			p.engine.Stop()
			stopDur = time.Since(t0)
		}
		if p.engine.Preview != nil {
			t0 := time.Now()
			p.engine.Preview(false)
			previewDur = time.Since(t0)
		}
		if p.engine.SetAudioOnly != nil {
			p.engine.SetAudioOnly(false)
		}
	}
	if media := p.inlineMedia; media != nil {
		t0 := time.Now()
		p.inlineMedia = nil
		media.CloseAsync()
		closeDur = time.Since(t0)
	}
	if playbackTrace && wasActive {
		log.Printf("hero-preview: stop-timing stop=%s preview=%s close=%s total=%s",
			stopDur.Round(time.Microsecond), previewDur.Round(time.Microsecond),
			closeDur.Round(time.Microsecond), time.Since(started).Round(time.Microsecond))
	}
}

func (p *Playback) TickInlinePreview(now time.Time) {
	if p == nil || !p.inlineActive || p.active || p.inlineState.Loading || p.inlineMedia == nil || p.engine.Snapshot == nil {
		return
	}
	clock := p.engine.Snapshot()
	prevHasFrame := p.inlineState.HasFrame
	// Audio-only inline preview plays audio without video frames, so keep HasFrame=false
	// to avoid clearing the Home Hero static artwork.
	if p.inlineState.AudioOnly || clock.AudioOnly {
		p.inlineState.AudioOnly = true
		p.inlineState.HasFrame = false
	} else {
		p.inlineState.HasFrame = clock.HasFrame
	}
	p.inlineState.Playing = clock.HasFrame && !clock.Buffering && !clock.Ended && !clock.Failed
	if clock.Failed || (!clock.HasFrame && !p.inlineStartedAt.IsZero() && now.Sub(p.inlineStartedAt) > inlinePreviewStallTimeout) {
		target := p.inlineState.Target
		p.StopInlinePreview()
		p.inlineState = InlinePreviewState{Target: target, Failed: true}
		if p.changed != nil {
			p.changed()
		}
		return
	}
	if clock.Ended {
		if p.engine.Seek != nil {
			p.engine.Seek(0)
		}
		if p.engine.SetRate != nil {
			p.engine.SetRate(1)
		}
	}
	if p.inlineState.HasFrame && !prevHasFrame && p.changed != nil {
		p.changed()
	}
}

// ClearLocalPositions drops cached preloaded streams and resets saved-position
// tracking when matching (entryID, owner) after local watch history is cleared.
func (p *Playback) ClearLocalPositions(entryID, owner string) {
	if p == nil {
		return
	}
	p.saveMu.Lock()
	for key := range p.savePending {
		if key.target.Ref.EntryID == entryID && key.owner == owner {
			delete(p.savePending, key)
		}
	}
	done := p.saveDone
	inflight := p.saveInflight
	p.saveMu.Unlock()
	if inflight && done != nil {
		<-done
	}
	if p.preloaded != nil && p.preloaded.target.Ref.EntryID == entryID && p.preloaded.owner == owner {
		if p.preloaded.media != nil {
			p.preloaded.media.CloseAsync()
		}
		p.preloaded = nil
	}
	if p.preloadCancel != nil && p.preloadTarget.Ref.EntryID == entryID && p.preloadOwner == owner {
		p.preloadCancel()
		p.preloadCancel = nil
	}
	if p.active && p.state.Target.Ref.EntryID == entryID && p.owner == owner {
		p.lastSaved = -1
		p.savedAsEnd = false
	}
}

func (p *Playback) enqueuePositionSave(owner string, target provider.PlaybackTarget, seconds float64, syncWait bool) {
	if p.store == nil {
		return
	}
	key := savedPositionKey{owner: owner, target: target}
	p.saveMu.Lock()
	if p.savePending == nil {
		p.savePending = make(map[savedPositionKey]float64)
	}
	p.savePending[key] = seconds
	if !p.saveInflight {
		p.saveInflight = true
		p.saveDone = make(chan struct{})
		go p.drainPositionSaves()
	}
	done := p.saveDone
	p.saveMu.Unlock()
	if syncWait {
		<-done
	}
}

func (p *Playback) flushPositionSaves() {
	p.saveMu.Lock()
	done := p.saveDone
	inflight := p.saveInflight
	p.saveMu.Unlock()
	if inflight && done != nil {
		<-done
	}
}

func (p *Playback) drainPositionSaves() {
	for {
		p.saveMu.Lock()
		if len(p.savePending) == 0 {
			done := p.saveDone
			p.saveInflight = false
			p.saveDone = nil
			p.saveMu.Unlock()
			if done != nil {
				close(done)
			}
			return
		}
		batch := p.savePending
		p.savePending = make(map[savedPositionKey]float64)
		p.saveMu.Unlock()
		for key, seconds := range batch {
			started := time.Now()
			_ = p.store.SavePlaybackPosition(key.owner, key.target, seconds)
			if playbackTrace {
				log.Printf("player-save: id=%s part=%s sec=%.0f elapsed=%s",
					key.target.Ref.ID, key.target.PartID, seconds, time.Since(started).Round(time.Microsecond))
			}
		}
	}
}

func (p *Playback) OpenPGSSubtitle(track provider.PlaybackTrack, accept func(*player.PGSTrack, error)) context.CancelFunc {
	if !p.active || p.state.Resolution.Transport == nil || len(track.Resource.URLs) == 0 {
		if accept != nil {
			accept(nil, &provider.Error{Code: provider.Unsupported})
		}
		return func() {}
	}
	transport := p.state.Resolution.Transport
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		pgs, err := player.OpenPGSTrack(ctx, transport, track, func() {
			p.post(func() {
				if ctx.Err() == nil && p.active {
					p.changed()
				}
			})
		})
		p.post(func() {
			if ctx.Err() != nil || !p.active {
				if pgs != nil {
					pgs.Close()
				}
				return
			}
			if accept != nil {
				accept(pgs, err)
			}
			p.changed()
		})
	}()
	return cancel
}

func (p *Playback) save() {
	p.savePosition(true, false)
}

func (p *Playback) saveAsync() {
	p.savePosition(false, false)
}

func (p *Playback) saveStopped() {
	p.savePosition(true, true)
}

func (p *Playback) savePosition(syncWait, stopped bool) {
	if !p.loaded || p.isLive() || p.state.PreviewPending || p.state.Target.Ref.ID == "" ||
		p.state.Resolution.Plan.Branch != nil || p.state.BranchHistory > 0 {
		return
	}
	target := p.state.Target
	seconds := int64(p.state.Position)
	finished := p.didEnd && p.state.Resolution.Plan.Access != provider.VideoAccessPreview
	if finished {
		seconds = 0
	}
	if !stopped && p.lastSaved == seconds && p.savedAsEnd == finished {
		if syncWait {
			p.flushPositionSaves()
		}
		return
	}
	p.lastSaved, p.savedAsEnd = seconds, finished
	p.enqueuePositionSave(p.owner, target, float64(seconds), syncWait)
	p.queueReport(target, seconds, finished, stopped)
}

func (p *Playback) queueReport(target provider.PlaybackTarget, seconds int64, finished, stopped bool) {
	reporter, _ := p.catalog.session.(provider.PlaybackReporter)
	if !p.syncSource || p.owner == "" || reporter == nil || !p.catalog.CanReportPlayback() {
		return
	}
	report := queuedPlaybackReport{reporter: reporter, owner: p.owner, report: provider.PlaybackProgress{
		PlaybackTarget: target, Owner: p.owner, ItemID: p.state.Resolution.Plan.ItemID, Seconds: seconds, Finished: finished, Stopped: stopped,
	}}
	for i := range p.reports {
		if p.reports[i].report.PlaybackTarget == target && p.reports[i].owner == p.owner {
			p.reports[i] = report
			return
		}
	}
	p.reports = append(p.reports, report)
	p.sendProgress()
}

func (p *Playback) SetOnReported(fn func()) {
	if p != nil {
		p.onReported = fn
	}
}

func (p *Playback) sendProgress() {
	if p.reporting || len(p.reports) == 0 {
		return
	}
	item := p.reports[0]
	p.reports[0] = queuedPlaybackReport{}
	p.reports = p.reports[1:]
	if item.owner != p.owner || item.report.PlaybackTarget.Ref.EntryID != p.catalog.entryID {
		p.sendProgress()
		return
	}
	p.reporting = true
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer cancel()
		err := item.reporter.ReportPlayback(ctx, item.report)
		p.post(func() {
			p.reporting = false
			if err == nil && p.onReported != nil {
				p.onReported()
			}
			p.sendProgress()
		})
	}()
}
