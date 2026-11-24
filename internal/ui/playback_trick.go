package tvui

import (
	"fmt"
	"math"
	"time"

	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/provider"
)

const (
	trickSkipSeconds     = 10.0
	trickDoubleTapWindow = 550 * time.Millisecond
	trickSkipHUDDuration = 1500 * time.Millisecond
	trickSeekInterval    = 280 * time.Millisecond
)

var trickRates = [...]float64{2, 4, 8, 16}

type playbackTrickState struct {
	rate        float64
	position    float64
	lastTickAt  time.Time
	lastSeekAt  time.Time
	lastSeekPos float64
	wasPlaying  bool

	skipDir    int
	skipDelta  float64
	skipAt     time.Time
	skipUntil  time.Time
	skipTarget float64

	scrubTapDir int
	scrubTapAt  time.Time
	scrubHeld   bool
}

func (t *playbackTrickState) active() bool {
	return t.rate != 0
}

func (t *playbackTrickState) scanning() bool {
	return t.rate < 0 || t.rate > 2
}

func (t *playbackTrickState) hudVisible(now time.Time) bool {
	return t.rate != 0 || (!t.skipUntil.IsZero() && now.Before(t.skipUntil))
}

func (t *playbackTrickState) clearFeedback() {
	t.skipDir = 0
	t.skipDelta = 0
	t.skipAt = time.Time{}
	t.skipUntil = time.Time{}
	t.skipTarget = 0
}

func (t *playbackTrickState) clearScrubTap() {
	t.scrubTapDir = 0
	t.scrubTapAt = time.Time{}
	t.scrubHeld = false
}

func (t *playbackTrickState) reset() {
	*t = playbackTrickState{}
}

func (s *playbackView) trickNow() time.Time {
	if !s.lastFrameAt.IsZero() {
		return s.lastFrameAt
	}
	return time.Now()
}

func nextTrickRate(current float64, dir int) float64 {
	if dir == 0 {
		return current
	}
	if current == 0 {
		return float64(dir) * trickRates[0]
	}
	curDir := 1
	if current < 0 {
		curDir = -1
	}
	mag := math.Abs(current)
	idx := 0
	for i, r := range trickRates {
		if mag >= r {
			idx = i
		}
	}
	if dir == curDir {
		if idx+1 < len(trickRates) {
			return float64(dir) * trickRates[idx+1]
		}
		return float64(dir) * trickRates[len(trickRates)-1]
	}
	if idx == 0 {
		return 0
	}
	return float64(curDir) * trickRates[idx-1]
}

func (v *View) canPlaybackTrick(state app.PlaybackState) bool {
	s := &v.player
	if v.Playback == nil || !v.Playback.Active() || isLivePlayback(state, s.current()) {
		return false
	}
	if !state.Clock.HasFrame || state.Loading || state.Switching || state.Duration <= 0 {
		return false
	}
	if state.Resolution.Plan.Access == provider.VideoAccessRestricted {
		return false
	}
	return !playbackBlocking(state) && !s.content.overlay() && s.panel == ""
}

func (v *View) handlePlaybackTrickKey(event ui.InputEvent) bool {
	if v.Playback == nil || !v.Playback.Active() {
		return false
	}
	s := &v.player
	state := v.Playback.State()
	if isLivePlayback(state, s.current()) {
		return false
	}
	if s.trick.active() {
		switch event.Key {
		case ui.KeyLeft, ui.KeyRight:
			if event.Repeat {
				return true
			}
			dir := 1
			if event.Key == ui.KeyLeft {
				dir = -1
			}
			v.dispatch(func() { v.stepPlaybackTrick(dir) })
			return true
		case ui.KeyEnter, ui.KeySpace:
			if !event.Repeat {
				v.dispatch(func() { v.exitPlaybackTrick(true) })
			}
			return true
		case ui.KeyUp:
			if !event.Repeat {
				v.dispatch(func() {
					v.exitPlaybackTrick(true)
					s.showAt(v, playerTimeline)
				})
			}
			return true
		case ui.KeyDown:
			if !event.Repeat {
				v.dispatch(func() {
					v.exitPlaybackTrick(true)
					s.show(v)
				})
			}
			return true
		}
		return false
	}
	if (event.Key != ui.KeyLeft && event.Key != ui.KeyRight) || !v.canPlaybackTrick(state) {
		return false
	}
	dir := 1
	if event.Key == ui.KeyLeft {
		dir = -1
	}
	if !s.visible {
		v.dispatch(func() { v.handleHiddenPlaybackSeek(dir, event.Repeat) })
		return true
	}
	if !s.selectingChapter && (v.State.Focus == playerTimeline || s.scrubbing) {
		v.dispatch(func() { v.handleTimelinePlaybackSeek(dir, event.Repeat) })
		return true
	}
	return false
}

func (v *View) startPlaybackTrick(dir int, startPos float64, wasPlaying bool, now time.Time) {
	s := &v.player
	rate := float64(dir) * trickRates[0]
	v.Playback.SetTrickRate(rate)
	if v.Playback.TrickRate() != rate {
		s.trick.reset()
		return
	}
	s.trick.rate = rate
	s.trick.wasPlaying = wasPlaying
	s.trick.position = startPos
	s.trick.lastTickAt = now
	s.trick.lastSeekAt = time.Time{}
	s.trick.lastSeekPos = startPos
	s.trick.clearFeedback()
	s.trick.clearScrubTap()
}

func (v *View) stepTimelineScrub(state app.PlaybackState, dir int) {
	s := &v.player
	if !s.scrubbing {
		s.scrubbing, s.scrubWasPlaying = true, state.Playing
		s.scrubPosition = state.Position
		if state.Playing {
			v.Playback.Toggle()
		}
	}
	s.scrubPosition = min(max(s.scrubPosition+float64(dir)*trickSkipSeconds, 0), state.Duration)
}

func (v *View) handleHiddenPlaybackSeek(dir int, repeat bool) {
	s := &v.player
	state := v.Playback.State()
	if !v.canPlaybackTrick(state) {
		return
	}
	now := s.trickNow()
	if repeat {
		startPos := state.Position
		if !s.trick.skipAt.IsZero() && s.trick.skipDir == dir && now.Sub(s.trick.skipAt) <= trickSkipHUDDuration {
			startPos = s.trick.skipTarget
		}
		s.trick.clearFeedback()
		s.visible = true
		s.hideAt = now.Add(playerIdle)
		s.scrubbing, s.scrubWasPlaying = true, state.Playing
		s.scrubPosition = min(max(startPos+float64(dir)*trickSkipSeconds, 0), state.Duration)
		if state.Playing {
			v.Playback.Toggle()
		}
		s.trick.scrubHeld = true
		s.trick.scrubTapDir = 0
		s.trick.scrubTapAt = time.Time{}
		s.controlFocus = playerTimeline
		v.State.Focus, v.reveal = playerTimeline, true
		return
	}
	doubleTap := !s.trick.skipAt.IsZero() && s.trick.skipDir == dir && now.Sub(s.trick.skipAt) <= trickDoubleTapWindow
	if doubleTap {
		v.startPlaybackTrick(dir, state.Position, state.Playing, now)
		return
	}
	target := min(max(state.Position+float64(dir)*trickSkipSeconds, 0), state.Duration)
	v.Playback.Seek(target)
	s.trick.skipDir = dir
	s.trick.skipDelta = float64(dir) * trickSkipSeconds
	s.trick.skipAt = now
	s.trick.skipUntil = now.Add(trickSkipHUDDuration)
	s.trick.skipTarget = target
}

func (v *View) handleTimelinePlaybackSeek(dir int, repeat bool) {
	s := &v.player
	state := v.Playback.State()
	if !v.canPlaybackTrick(state) {
		return
	}
	now := s.trickNow()
	s.hideAt = now.Add(playerIdle)
	if repeat {
		s.trick.scrubHeld = true
		s.trick.scrubTapDir = 0
		s.trick.scrubTapAt = time.Time{}
		v.stepTimelineScrub(state, dir)
		return
	}
	doubleTap := !s.trick.scrubHeld && !s.trick.scrubTapAt.IsZero() &&
		s.trick.scrubTapDir == dir && now.Sub(s.trick.scrubTapAt) <= trickDoubleTapWindow
	if doubleTap {
		startPos := state.Position
		wasPlaying := state.Playing
		if s.scrubbing {
			startPos = s.scrubPosition
			wasPlaying = s.scrubWasPlaying
			s.scrubbing = false
		}
		s.trick.clearScrubTap()
		s.visible = false
		s.controlFocus = ""
		v.State.Focus, v.reveal = playerWake, true
		if math.Abs(startPos-state.Position) > 0.25 {
			v.Playback.Seek(startPos)
		}
		if dir > 0 && wasPlaying && !v.Playback.State().Playing {
			v.Playback.Toggle()
		}
		v.startPlaybackTrick(dir, startPos, wasPlaying, now)
		if !s.trick.active() && wasPlaying && !v.Playback.State().Playing {
			v.Playback.Toggle()
		}
		return
	}
	s.trick.scrubHeld = false
	s.trick.scrubTapDir = dir
	s.trick.scrubTapAt = now
	v.stepTimelineScrub(state, dir)
}

func (v *View) stepPlaybackTrick(dir int) {
	s := &v.player
	if !s.trick.active() || v.Playback == nil || !v.Playback.Active() {
		return
	}
	state := v.Playback.State()
	now := s.trickNow()
	prev := s.trick.rate
	next := nextTrickRate(prev, dir)
	if next == 0 {
		v.exitPlaybackTrick(true)
		return
	}
	if prev > 0 && prev <= 2 && next > 2 {
		s.trick.position = state.Position
		s.trick.lastTickAt = now
		s.trick.lastSeekAt = time.Time{}
		s.trick.lastSeekPos = state.Position
	} else if prev > 2 && next > 0 && next <= 2 {
		if math.Abs(s.trick.position-state.Position) > 0.25 {
			v.Playback.Seek(s.trick.position)
		}
		s.trick.lastTickAt = now
	}
	s.trick.rate = next
	v.Playback.SetTrickRate(next)
}

func (v *View) exitPlaybackTrick(resume bool) {
	s := &v.player
	if !s.trick.active() {
		return
	}
	wasScanning := s.trick.scanning()
	targetPos := s.trick.position
	s.trick.reset()
	if v.Playback == nil || !v.Playback.Active() {
		return
	}
	state := v.Playback.State()
	if wasScanning && (math.Abs(targetPos-state.Position) > 0.25 || state.Clock.Seeking) {
		v.Playback.Seek(targetPos)
	}
	v.Playback.SetTrickRate(0)
	if resume && !v.Playback.State().Playing {
		v.Playback.Toggle()
	}
}

func (v *View) tickPlaybackTrick(c *ui.Context, now time.Time, state app.PlaybackState, liveMode bool) {
	s := &v.player
	if !s.trick.active() && s.trick.skipUntil.IsZero() {
		return
	}
	if liveMode || !v.canPlaybackTrick(state) {
		if s.trick.active() && v.Playback != nil && v.Playback.Active() {
			v.Playback.SetTrickRate(0)
		}
		s.trick.reset()
		return
	}
	if s.trick.active() {
		if s.trick.scanning() {
			if s.trick.lastTickAt.IsZero() {
				s.trick.lastTickAt = now
			}
			dt := now.Sub(s.trick.lastTickAt).Seconds()
			s.trick.lastTickAt = now
			if dt > 0 {
				if dt > 0.25 {
					dt = 0.25
				}
				s.trick.position += dt * s.trick.rate
			}
			if s.trick.position <= 0 {
				s.trick.position = 0
				v.exitPlaybackTrick(s.trick.wasPlaying)
				return
			}
			if state.Duration > 0 && s.trick.position >= state.Duration {
				s.trick.position = max(0, state.Duration-0.5)
				v.exitPlaybackTrick(s.trick.wasPlaying)
				return
			}
			if !state.Clock.Seeking &&
				(s.trick.lastSeekAt.IsZero() || now.Sub(s.trick.lastSeekAt) >= trickSeekInterval) &&
				math.Abs(s.trick.position-s.trick.lastSeekPos) >= 0.25 {
				s.trick.lastSeekAt = now
				s.trick.lastSeekPos = s.trick.position
				v.Playback.Seek(s.trick.position)
			}
			c.AnimationFrame()
			return
		}
		s.trick.position = state.Position
		c.After(100 * time.Millisecond)
		return
	}
	if !now.Before(s.trick.skipUntil) {
		s.trick.skipUntil = time.Time{}
		return
	}
	remaining := s.trick.skipUntil.Sub(now)
	c.After(min(remaining, 160*time.Millisecond))
}

func (s *playbackTrickState) displayPosition(state app.PlaybackState) float64 {
	if s.scanning() {
		return s.position
	}
	return state.Position
}

func (p *PageContext) playbackTrickHUD(c *ui.Context, state app.PlaybackState, now time.Time) {
	s, colors, text := &p.host.player, p.Theme.Colors, p.Assets.Text.Playback
	if !s.trick.hudVisible(now) {
		return
	}
	forward := s.trick.rate > 0 || (s.trick.rate == 0 && s.trick.skipDir >= 0)
	iconName, dirLabel := "rewind", text.Rewind
	if forward {
		iconName, dirLabel = "fast-forward", text.FastForward
	}
	var label string
	if s.trick.rate != 0 {
		label = fmt.Sprintf("%.0f\u00d7", math.Abs(s.trick.rate))
	} else {
		sign := "-"
		if s.trick.skipDelta >= 0 {
			sign = "+"
		}
		label = fmt.Sprintf("%s%.0fs", sign, math.Abs(s.trick.skipDelta))
	}

	pos := s.trick.displayPosition(state)
	duration := playbackTimelineDuration(state, s.current().item)
	progress := playbackFraction(pos, duration)
	buffered := playbackFraction(state.Clock.BufferEnd, duration)
	length := p.Width - 2*playerInset
	thumbX := playerInset + length*progress
	pillWidth := float32(88)
	if s.trick.rate == 0 {
		pillWidth = 106
	} else if math.Abs(s.trick.rate) >= 10 {
		pillWidth = 98
	}
	pillLeft := min(max(playerInset, thumbX-pillWidth/2), p.Width-playerInset-pillWidth)

	ui.Box(c.Key("player-trick-scrim")).Absolute().Left(0).Right(0).Bottom(0).Height(180).IgnoreInput(true).
		LinearGradient(ui.LinearGradient{
			Angle: 180,
			Stops: []ui.GradientStop{
				{Color: ui.Transparent},
				{At: 1, Color: withAlpha(colors.Overlay.PlayerBottom, 0xb0)},
			},
		})

	ui.Row(c.Key("player-trick-pill")).Absolute().Bottom(102).Left(pillLeft).Width(pillWidth).Height(40).
		Gap(7).Radius(20).Center().Role(ui.RoleStatus).Label(dirLabel + " " + label).IgnoreInput(true).
		Material(glass.Blur{Radius: 20}).
		Background(withAlpha(colors.Overlay.PlayerBottom, 0xb8)).
		Border(1, withAlpha(colors.Playback.Foreground, 0x25)).Children(func() {
		ui.Icon(c, p.Assets.Icon(iconName)).Size(19, 19).TextColor(paint(colors.Accent.Soft))
		ui.Text(c, label).FontSize(18).FontWeight(600).FontFeatures("tnum").
			TextColor(paint(colors.Playback.Foreground))
	})

	ui.Column(c.Key("player-trick-bar")).Absolute().Bottom(44).Left(playerInset).Right(playerInset).Gap(11).IgnoreInput(true).Children(func() {
		ui.Box(c).FillWidth().Height(8).Radius(4).Background(paint(colors.Playback.Track)).Children(func() {
			ui.Box(c).Absolute().Left(0).Top(0).Height(8).Width(length * buffered).
				Radius(4).Background(withAlpha(colors.Playback.Foreground, 0x26))
			ui.Box(c).Absolute().Left(0).Top(0).Height(8).Width(length * progress).
				Radius(4).Background(paint(colors.Playback.Progress))
			ui.Box(c).Absolute().Left(length*progress-8).Top(-4).Size(16, 16).Radius(8).
				Background(paint(colors.Playback.Foreground))
		})
		ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Children(func() {
			ui.Text(c, playbackTime(pos)).FontSize(16).FontWeight(500).FontFeatures("tnum").
				TextColor(paint(colors.Playback.Foreground))
			ui.Text(c, playbackTime(duration)).FontSize(15).FontFeatures("tnum").
				TextColor(paint(colors.Playback.Secondary))
		})
	})
}
