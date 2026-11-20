package app

import (
	"context"
	"math"
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
)

type preloadedPlayback struct {
	target provider.PlaybackTarget
	owner  string
	result provider.Playback
	media  *player.Media
}

type PlaybackState struct {
	Target         provider.PlaybackTarget
	Resolution     provider.Playback
	Clock          player.Status
	Revision       uint64
	Position       float64
	Duration       float64
	Selected       string
	Fallback       string
	Limit          provider.PlaybackLimit
	Failure        provider.ErrorCode
	Playing        bool
	Loading        bool
	Switching      bool
	NeedsReload    bool
	VideoRange     string
	PreviewPending bool
	BytesPerSecond float64
}

type Playback struct {
	catalog *Catalog
	engine  player.Engine
	store   PlaybackStore
	post    func(func())
	changed func()

	state            PlaybackState
	owner            string
	syncSource       bool
	active           bool
	rate             float64
	media            *player.Media
	cancel           context.CancelFunc
	request          uint64
	refreshed        bool
	lastSaved        int64
	savedAsEnd       bool
	positionAt       time.Time
	startedAt        time.Time
	bufferedAt       time.Time
	loaded           bool
	didEnd           bool
	previewConfirmed bool
	hdrBlocked       bool
	reporting        bool
	reports          []queuedPlaybackReport

	autoQuality     string
	autoHDRBlocked  bool
	autoStallCount  int
	autoStallSince  time.Time
	autoStallActive bool
	autoLastSwitch  time.Time
	autoHealthyAt   time.Time

	preloadCancel  context.CancelFunc
	preloadRequest uint64
	preloadTarget  provider.PlaybackTarget
	preloadOwner   string
	preloaded      *preloadedPlayback
}

type queuedPlaybackReport struct {
	reporter provider.PlaybackReporter
	owner    string
	report   provider.PlaybackProgress
}

func NewPlayback(catalog *Catalog, native player.Engine, store PlaybackStore, post func(func()), changed func()) *Playback {
	return &Playback{catalog: catalog, engine: native, store: store, post: post, changed: changed}
}

func (p *Playback) State() PlaybackState { return p.state }
func (p *Playback) Active() bool         { return p.active }
func (p *Playback) Rate() float64        { return p.rate }
func (p *Playback) Owner() string        { return p.owner }

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
	var hit *preloadedPlayback
	if p.preloaded != nil && p.preloaded.target == target && p.preloaded.owner == owner &&
		(p.preloaded.result.Plan.ExpiresAt == 0 || time.Now().Unix()+preloadExpiryMarginSec < p.preloaded.result.Plan.ExpiresAt) {
		hit = p.preloaded
		p.preloaded = nil
	}
	p.clearPreload()
	if p.active {
		p.save()
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
	p.loaded, p.didEnd, p.refreshed = false, false, false
	p.previewConfirmed, p.hdrBlocked = false, false
	p.lastSaved, p.savedAsEnd = -1, false
	p.resetAutoQuality(true)
	if p.rate == 0 {
		p.rate = 1
	}
	if hit != nil {
		hit.media.Promote()
		p.accept(hit.result, hit.media, nil, "", false, start == nil)
		p.changed()
		return
	}
	p.resolveMode("", "", false, start == nil)
}

func (p *Playback) Close() {
	if !p.active {
		p.clearPreload()
		return
	}
	p.save()
	p.clearPreload()
	p.clearStream()
	if p.engine.Fullscreen != nil {
		p.engine.Fullscreen(false)
	}
	p.active = false
	p.state = PlaybackState{Revision: p.state.Revision}
	p.rate = 1
	p.resetAutoQuality(true)
	p.changed()
}

func (p *Playback) clearStream() {
	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
	}
	if p.engine.Stop != nil {
		p.engine.Stop()
	}
	if p.media != nil {
		p.media.Close()
		p.media = nil
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
			p.preloaded.media.Close()
		}
		p.preloaded = nil
	}
}

// PreloadNext resolves and warms up the next episode in the background so
// sequential auto-advance can start immediately without a metadata round-trip.
func (p *Playback) PreloadNext(target provider.PlaybackTarget, owner string) {
	if !p.active || target.Ref.ID == "" || p.state.Loading || p.state.Switching ||
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
	input := provider.PlaybackRequest{
		PlaybackTarget: target, Quality: "", HDR: caps.HDR, HEVC: caps.HEVC,
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
				media.Close()
				return
			}
			cancel()
			p.preloadCancel = nil
			p.preloaded = &preloadedPlayback{target: target, owner: owner, result: result, media: media}
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
}

func (p *Playback) effectiveQuality() string {
	if p.state.Selected != "" {
		return p.state.Selected
	}
	return p.autoQuality
}

func (p *Playback) resolve(quality string, preserve, useResume bool) {
	p.resolveMode(quality, quality, preserve, useResume)
}

func (p *Playback) resolveMode(selected, requestQuality string, preserve, useResume bool) {
	if p.cancel != nil {
		p.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel
	p.request++
	request := p.request
	p.state.Loading, p.state.Switching = !preserve, preserve
	p.state.Failure, p.state.Limit, p.state.Fallback = "", "", ""
	var caps player.Capabilities
	if p.engine.Capabilities != nil {
		caps = p.engine.Capabilities()
	}
	if p.hdrBlocked || (selected == "" && p.autoHDRBlocked) {
		caps.HDR = false
	}
	resolver, _ := p.catalog.session.(provider.PlaybackReader)
	input := provider.PlaybackRequest{
		PlaybackTarget: p.state.Target, Quality: requestQuality, HDR: caps.HDR, HEVC: caps.HEVC,
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
		if err == nil && result.Plan.Access != provider.VideoAccessRestricted && (!preserve || result.Plan.Restriction == "") {
			timeout, stop := context.WithTimeout(ctx, 15*time.Second)
			media, err = player.OpenMedia(timeout, result)
			stop()
		}
		p.post(func() {
			if !p.active || p.request != request || ctx.Err() != nil {
				if media != nil {
					media.Close()
				}
				return
			}
			cancel()
			p.cancel = nil
			p.accept(result, media, err, selected, preserve, useResume)
			p.changed()
		})
	}()
	p.changed()
}

func (p *Playback) accept(result provider.Playback, media *player.Media, err error, quality string, preserve, useResume bool) {
	p.state.Loading, p.state.Switching = false, false
	if err != nil {
		p.state.Failure = provider.CodeOf(err)
		if p.state.Failure == provider.Expired && !p.refreshed && !preserve {
			p.refreshed = true
			p.resolveMode(quality, p.effectiveQuality(), false, useResume)
			return
		}
		if p.state.Failure == provider.NeedsSignIn {
			p.state.Limit, p.state.Failure = provider.PlaybackLogin, ""
		}
		if preserve && p.media != nil {
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
			media.Close()
		}
		// An unchanged entitlement keeps the finished preview and its last frame.
		p.state.Limit = ""
		return
	}
	if plan.Access == provider.VideoAccessRestricted {
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
	if useResume {
		if p.syncSource && position == 0 {
			position = plan.ResumeSeconds
		}
		if p.store != nil {
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
	if plan.Duration > 0 && position >= end {
		position = plan.Duration
		if useResume {
			position = 0
		}
	}
	if p.engine.Open == nil || media == nil || !p.engine.Open(media.URL, position) {
		if media != nil {
			media.Close()
		}
		p.state.Failure = provider.Unsupported
		return
	}
	if p.media != nil {
		p.media.Close()
	}
	p.media = media
	if endedPreview || p.state.PreviewPending && plan.Access != provider.VideoAccessPreview {
		p.didEnd, p.state.Playing = false, true
	}
	if plan.Access == provider.VideoAccessPreview && !p.previewConfirmed {
		p.state.PreviewPending, p.state.Playing = true, false
	} else if plan.Access != provider.VideoAccessPreview {
		p.state.PreviewPending = false
	}
	p.state.Target, p.state.Resolution, p.state.Selected = target, result, quality
	p.state.Position, p.state.Duration, p.state.VideoRange = position, plan.Duration, media.VideoRange
	p.state.Clock = player.Status{Position: position, Buffering: true}
	p.startedAt = time.Now()
	p.positionAt, p.bufferedAt = time.Time{}, time.Time{}
	p.state.Revision++
	p.applyRate()
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
	if clock.Ended && !p.didEnd {
		p.didEnd, p.state.Playing = true, false
		p.save()
	}
	if p.state.Loading || p.state.Switching || p.state.PreviewPending || p.state.Failure != "" || p.didEnd {
		return
	}
	code := p.media.Failure()
	plan := p.state.Resolution.Plan
	expired := code == provider.Expired || plan.ExpiresAt > 0 && now.Unix() >= plan.ExpiresAt
	if !p.refreshed && expired && (clock.Failed || clock.Buffering) {
		p.refreshed = true
		p.resolveMode(p.state.Selected, p.effectiveQuality(), false, false)
		return
	}
	if clock.Failed && clock.DisplayIncompatible && !p.hdrBlocked && (p.state.VideoRange == "PQ" || p.state.VideoRange == "HLG") {
		p.hdrBlocked = true
		p.resolveMode(p.state.Selected, p.effectiveQuality(), false, false)
		return
	}
	if clock.Buffering {
		if p.bufferedAt.IsZero() {
			p.bufferedAt = now
		}
	} else {
		p.bufferedAt = time.Time{}
	}
	if p.stepAutoQuality(now, clock) {
		return
	}
	timedOut := !clock.HasFrame && now.Sub(p.startedAt) > playbackStallTimeout ||
		!p.bufferedAt.IsZero() && now.Sub(p.bufferedAt) > playbackStallTimeout
	if clock.Failed || timedOut {
		if code == "" {
			code = provider.Network
			if timedOut {
				code = provider.Timeout
			}
		}
		p.state.Failure = code
		if p.engine.SetRate != nil {
			p.engine.SetRate(0)
		}
		return
	}
	if p.positionAt.IsZero() {
		p.positionAt = now
	} else if p.state.Playing && now.Sub(p.positionAt) >= 30*time.Second {
		p.save()
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
	if p.state.Selected != "" || !p.state.Playing || p.media == nil {
		return false
	}
	isRebuffering := clock.Buffering && clock.HasFrame
	if isRebuffering && !p.autoStallActive {
		if p.autoStallSince.IsZero() || now.Sub(p.autoStallSince) > autoStallWindow {
			p.autoStallSince = now
			p.autoStallCount = 1
		} else {
			p.autoStallCount++
		}
	}
	p.autoStallActive = isRebuffering

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
		longRebuffer := isRebuffering && !p.bufferedAt.IsZero() && now.Sub(p.bufferedAt) >= rebufNeed
		repeatedStalls := isRebuffering && p.autoStallCount >= 2 && !p.autoStallSince.IsZero() &&
			now.Sub(p.autoStallSince) <= autoStallWindow && (!crossingHDR || underfed)
		slowStartup := !clock.HasFrame && now.Sub(p.startedAt) >= startupNeed && underfed
		if longRebuffer || repeatedStalls || slowStartup {
			if crossingHDR {
				p.autoHDRBlocked = true
			}
			p.autoQuality = next.ID
			p.autoLastSwitch = now
			p.autoStallCount = 0
			p.autoStallSince = time.Time{}
			p.autoHealthyAt = time.Time{}
			p.resolveMode("", next.ID, true, false)
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
			nextAuto := higher.ID
			if curIdx-1 == 0 {
				nextAuto = ""
			}
			p.autoQuality = nextAuto
			p.autoLastSwitch = now
			p.autoHealthyAt = time.Time{}
			p.resolveMode("", higher.ID, true, false)
			return true
		}
	}
	return false
}

func (p *Playback) Toggle() {
	if !p.active || p.state.Loading || p.state.PreviewPending {
		return
	}
	if p.didEnd || p.state.Failure != "" {
		p.Retry()
		return
	}
	p.state.Playing = !p.state.Playing
	if !p.state.Playing {
		p.save()
	}
	p.applyRate()
	p.changed()
}

func (p *Playback) SetRate(rate float64) {
	p.rate = rate
	p.applyRate()
	p.changed()
}

func (p *Playback) applyRate() {
	if p.engine.SetRate == nil {
		return
	}
	rate := float64(0)
	if p.state.Playing {
		rate = p.rate
	}
	p.engine.SetRate(rate)
}

func (p *Playback) Seek(seconds float64) {
	if p.media == nil {
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
	p.autoHealthyAt = time.Time{}
	p.autoStallActive = false
	if expires := p.state.Resolution.Plan.ExpiresAt; expires > 0 && time.Now().Unix() >= expires {
		p.refreshed = false
		p.resolveMode(p.state.Selected, p.effectiveQuality(), false, false)
		return
	}
	p.engine.Seek(p.state.Position)
	p.applyRate()
	p.changed()
}

func (p *Playback) ChooseQuality(quality string) {
	if !p.active || p.state.Loading || p.state.Switching {
		return
	}
	if quality == p.state.Selected && (quality != "" || p.autoQuality == "") && p.media != nil {
		return
	}
	p.refreshed = false
	p.resetAutoQuality(quality == "")
	p.resolveMode(quality, quality, p.media != nil, false)
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
	p.resolveMode(p.state.Selected, p.effectiveQuality(), false, false)
}

func (p *Playback) Retry() {
	if !p.active {
		return
	}
	p.state.Playing = true
	p.state.Limit = ""
	if p.didEnd && p.media != nil && p.state.Resolution.Plan.Access != provider.VideoAccessPreview {
		p.Seek(0)
		return
	}
	if p.didEnd {
		p.state.Position = 0
	}
	p.loaded, p.didEnd, p.refreshed = false, false, false
	p.resolveMode(p.state.Selected, p.effectiveQuality(), false, false)
}

func (p *Playback) Suspend() {
	if p.active {
		p.save()
		p.bufferedAt = time.Time{}
	}
}

func (p *Playback) save() {
	if !p.loaded || p.state.PreviewPending || p.state.Target.Ref.ID == "" {
		return
	}
	target := p.state.Target
	seconds := int64(p.state.Position)
	finished := p.didEnd && p.state.Resolution.Plan.Access != provider.VideoAccessPreview
	if finished {
		seconds = 0
	}
	if p.lastSaved == seconds && p.savedAsEnd == finished {
		return
	}
	p.lastSaved, p.savedAsEnd = seconds, finished
	if p.store != nil {
		_ = p.store.SavePlaybackPosition(p.owner, target, float64(seconds))
	}
	reporter, _ := p.catalog.session.(provider.PlaybackReporter)
	if !p.syncSource || p.owner == "" || reporter == nil || !p.catalog.CanReportPlayback() {
		return
	}
	report := queuedPlaybackReport{reporter: reporter, owner: p.owner, report: provider.PlaybackProgress{
		PlaybackTarget: target, Owner: p.owner, ItemID: p.state.Resolution.Plan.ItemID, Seconds: seconds, Finished: finished,
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
		_ = item.reporter.ReportPlayback(ctx, item.report)
		p.post(func() {
			p.reporting = false
			p.sendProgress()
		})
	}()
}
