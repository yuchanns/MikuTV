package tvui

import (
	"fmt"
	"math"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/provider"
)

const (
	networkSpeedRefresh      = 400 * time.Millisecond
	networkSpeedStallGrace   = 1500 * time.Millisecond
	networkSpeedLowThreshold = 128 * 1024
)

func playbackShowNetworkSpeed(state app.PlaybackState, s *playbackView) bool {
	if s.content.overlay() || playbackHasAccessDialog(state) || state.Failure != "" || state.Clock.Ended || state.LiveEnded {
		return false
	}
	return state.Loading || state.Switching || (state.Clock.Buffering && state.Playing && !s.scrubbing && !s.selectingChapter)
}

func (s *playbackView) resetNetworkSpeed() {
	s.speedSince = time.Time{}
	s.speedSampleAt = time.Time{}
	s.speedDisplayed = 0
}

func (s *playbackView) displayNetworkSpeed(now time.Time, state app.PlaybackState) (float64, bool) {
	if s.speedSince.IsZero() {
		s.speedSince = now
	}
	if s.speedSampleAt.IsZero() || (s.speedDisplayed == 0 && state.BytesPerSecond > 0) || now.Sub(s.speedSampleAt) >= networkSpeedRefresh {
		s.speedDisplayed = state.BytesPerSecond
		s.speedSampleAt = now
	}
	low := now.Sub(s.speedSince) >= networkSpeedStallGrace && s.speedDisplayed < networkSpeedLowThreshold
	return s.speedDisplayed, low
}

func formatNetworkSpeed(bytesPerSecond float64) string {
	bps := max(0, bytesPerSecond)
	if bps >= 100*1024*1024 {
		return fmt.Sprintf("%.0f MB/s", bps/(1024*1024))
	}
	if bps >= 1023.5*1024 {
		return fmt.Sprintf("%.1f MB/s", bps/(1024*1024))
	}
	return fmt.Sprintf("%d KB/s", int(math.Round(bps/1024)))
}

func (p *PageContext) playbackNetworkSpeed(c *ui.Context, state app.PlaybackState, onVideo bool) {
	s, colors := &p.host.player, p.Theme.Colors
	bps, low := s.displayNetworkSpeed(c.Now(), state)
	iconColor := withAlpha(colors.Accent.Soft, 0xe6)
	textColor := withAlpha(colors.Playback.Secondary, 0xf0)
	if onVideo {
		iconColor = withAlpha(colors.Accent.Soft, 0xf0)
		textColor = withAlpha(colors.Playback.Foreground, 0xe8)
	}
	if low {
		iconColor = withAlpha(colors.Status.Warning.Foreground, 0xe6)
		textColor = withAlpha(colors.Status.Warning.Foreground, 0xf0)
	}
	shadow := withAlpha(colors.Overlay.Shadow, 0x90)
	ui.Row(c.Key("network-speed")).Shrink(0).Gap(8).AlignItems(ui.Center).IgnoreInput(true).Children(func() {
		ui.Icon(c, p.Assets.Icon("wifi")).Size(20, 20).Translate(0, 1).TextColor(iconColor)
		ui.Text(c, formatNetworkSpeed(bps)).FontSize(19).FontWeight(400).FontFeatures("tnum").
			TextColor(textColor).TextShadow(0, 1, 5, shadow)
	})
}

type playbackFeedback uint8

const (
	playbackFeedbackNone playbackFeedback = iota
	playbackFeedbackBuffering
	playbackFeedbackFailed
	playbackFeedbackEnded
)

func (s *playbackView) feedbackFor(c *ui.Context, state app.PlaybackState) playbackFeedback {
	buffering := state.Loading || state.Clock.Buffering && state.Playing && !s.scrubbing && !s.selectingChapter
	if !buffering {
		s.bufferSince = time.Time{}
	}
	if s.content.overlay() || playbackHasAccessDialog(state) || playbackHasBranchOverlay(state) {
		return playbackFeedbackNone
	}
	if state.LiveEnded {
		return playbackFeedbackEnded
	}
	if state.Failure != "" {
		return playbackFeedbackFailed
	}
	if state.Clock.Ended {
		if playbackAutoAdvancingBranch(state) || playbackHasBranchChoices(state) {
			return playbackFeedbackNone
		}
		return playbackFeedbackEnded
	}
	if !buffering {
		return playbackFeedbackNone
	}
	if !state.Clock.HasFrame {
		return playbackFeedbackBuffering
	}
	// Keep a short seek or stream transition from flashing a full-screen layer.
	if s.bufferSince.IsZero() {
		s.bufferSince = c.Now()
	}
	if remaining := 500*time.Millisecond - c.Now().Sub(s.bufferSince); remaining > 0 {
		c.After(remaining)
		return playbackFeedbackNone
	}
	return playbackFeedbackBuffering
}

func (p *PageContext) playbackFeedbackScreen(c *ui.Context, kind playbackFeedback, state app.PlaybackState, cover string) {
	v, s := p.host, &p.host.player
	liveMode := isLivePlayback(state, s.current())
	linePickerOpen := liveMode && kind == playbackFeedbackFailed && s.panel == playerLine
	if kind == playbackFeedbackBuffering && p.active && v.State.Focus != playerBuffer {
		s.feedbackFocus = v.State.Focus
		v.State.Focus, v.reveal = playerBuffer, true
	} else if kind != playbackFeedbackBuffering && p.active && !linePickerOpen {
		s.panel, s.controlFocus = "", ""
		s.scrubbing, s.selectingChapter = false, false
		if v.State.Focus != playerStatus && v.State.Focus != playerStatusLine {
			v.State.Focus, v.reveal = playerStatus, true
		}
	}
	v.Playback.SetViewport(1)
	hasFrame := v.Playback.SetBackdropBlur(16)
	colors, d := p.Theme.Colors, p.uiColors()
	root := ui.Box(c.Key("player-feedback")).Absolute().Top(0).Left(0).Fill().Clip().Lang(p.langTag()).Font(p.uiFont())
	if kind == playbackFeedbackBuffering {
		p.region(playerPanelGroup, focusEdges{})
		p.bind(root.Focusable().Label(p.Assets.Text.Playback.Buffering), playerBuffer, playerPanelGroup)
	}
	root.Children(func() {
		if !hasFrame {
			ui.Box(c.Key("cover")).Absolute().Top(-45).Left(-45).Width(p.Width + 90).Height(p.Height + 90).
				Cache().Blur(16).Children(func() { p.playbackPoster(c, cover) })
		}
		ui.Box(c.Key("dim")).Absolute().Top(0).Left(0).Fill().Background(withAlpha(colors.Overlay.PlayerBottom, 0xc9))
		text := p.Assets.Text.Playback
		if kind == playbackFeedbackBuffering {
			ui.Box(c.Key("buffering-network-speed")).Absolute().Right(playerInset).Top(67).IgnoreInput(true).Children(func() {
				p.playbackNetworkSpeed(c, state, false)
			})
			p.playbackSpinner(c)
			p.playbackStatusLine(c, text.Buffering, 5, 41, 450, paint(colors.Playback.Foreground)).Role(ui.RoleStatus)
			return
		}
		title, note, icon, action, glyph := text.Failed, text.FailedNote, "playback-offline", text.Reconnect, "refresh"
		primaryRun := func() {
			v.Playback.Retry()
			s.show(v)
			v.State.Focus, v.reveal = playerToggle, true
		}
		primaryWidth := float32(184)
		showLineButton := false
		if liveMode && kind == playbackFeedbackFailed {
			title, note = text.LiveDisconnected, text.LiveDisconnectedBody
			showLineButton = len(state.Resolution.Plan.Lines) > 0
		} else if liveMode && kind == playbackFeedbackEnded {
			title, note, icon, action, glyph = text.LiveEnded, text.LiveEndedBody, "live", text.MoreLive, ""
			primaryWidth = 220
			primaryRun = func() {
				p.leaveLiveToBrowse()
			}
		} else if kind == playbackFeedbackEnded {
			title, note, icon, action, glyph = text.Ended, text.EndedNote, "playback-complete", text.Replay, "play-filled"
		}
		ui.Icon(c.Key("status-icon"), p.Assets.Icon(icon)).Absolute().Left(p.Width/2-32.5).Top(p.Height/2-155).
			Size(65, 65).TextColor(paint(colors.Accent.Soft))
		p.playbackStatusLine(c, title, -57, 41, 450, paint(colors.Playback.Foreground)).Role(ui.RoleStatus)
		p.playbackStatusLine(c, note, 22, 24, 400, paint(colors.Playback.Secondary)).Translate(-5, 0)
		actions := *p
		actions.active = p.active && !linePickerOpen
		actions.region(playerPanelGroup, focusEdges{})
		ui.Column(c.Key("status-action")).Absolute().Left(0).Top(p.Height/2 + 95).FillWidth().Gap(18).AlignItems(ui.Center).Children(func() {
			primaryStyle := buttonAppearance{
				height: 61, radius: 12, fontSize: 23, fontWeight: 550, iconSize: 24, iconGap: 12,
				background: paint(colors.Focus.Background), foreground: paint(colors.Focus.Foreground),
			}
			if showLineButton {
				primaryStyle.background = d.secondaryFill
				primaryStyle.foreground = d.secondaryText
				primaryStyle.border = d.secondaryBorder
			}
			actions.styledButton(c, action, glyph, playerStatus, playerPanelGroup, primaryRun, primaryStyle).Width(primaryWidth).AlignSelf(ui.Center)
			if showLineButton {
				actions.styledButton(c, text.Line, "refresh", playerStatusLine, playerPanelGroup, func() {
					s.openPanel(v, playerLine)
				}, buttonAppearance{
					height: 56, padding: 28, radius: 12, fontSize: 21, fontWeight: 500, iconSize: 21, iconGap: 11,
					background: d.secondaryFill, foreground: d.secondaryText, border: d.secondaryBorder,
				}).AlignSelf(ui.Center)
			}
		})
		if linePickerOpen {
			p.playbackPanel(c, state, s.current())
		}
	})
}

func (p *PageContext) leaveLiveToBrowse() {
	v := p.host
	entry, _ := p.State.Current()
	if entry.Capabilities.Has(provider.BrowseLive) {
		p.State.Navigate(app.Route{Page: app.Live})
		return
	}
	v.goBack()
}

func (p *PageContext) playbackStatusLine(c *ui.Context, label string, offset, size float32, weight int, color ui.Color) ui.Element {
	line := ui.Column(c).Absolute().Top(p.Height/2 + offset).Left(0).FillWidth().AlignItems(ui.Center)
	line.Children(func() {
		ui.Text(c, label).FontSize(size).FontWeight(weight).TextColor(color).TextStroke(size*0.008, color)
	})
	return line
}

func (p *PageContext) playbackSpinner(c *ui.Context) {
	colors, d := p.Theme.Colors, p.uiColors()
	var degrees float32
	if !c.Preferences().ReduceMotion {
		degrees = float32(c.Now().UnixNano()%int64(time.Second)) / float32(time.Second) * 360
		c.AnimationFrame()
	}
	ui.Box(c.Key("spinner")).Absolute().Left(p.Width/2-27).Top(p.Height/2-84).Size(54, 54).Children(func() {
		ui.Box(c.Key("track")).Absolute().Top(0).Left(0).Fill().Radius(27).Border(3, d.spinnerTrack)
		ui.Icon(c.Key("arc"), p.Assets.Icon("playback-spinner")).Absolute().Top(0).Left(0).Size(54, 54).
			TextColor(paint(colors.Accent.Soft)).Rotate(degrees)
	})
}
