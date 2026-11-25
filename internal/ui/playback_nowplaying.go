package tvui

import (
	"bytes"
	"image/jpeg"
	"math"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/player"
	"github.com/yuchanns/MikuTV/internal/provider"
)

const (
	nowPlayingSyncInterval = 2 * time.Second
	backgroundAudioPoll    = 220 * time.Millisecond
)

type nowPlayingSyncKey struct {
	active   bool
	revision uint64
	title    string
	artist   string
	album    string
	playing  bool
	rate     float64
	duration int64
	hasArt   bool
}

func (v *View) canContinueAudioInBackground() bool {
	if v == nil || v.Playback == nil || !v.Playback.Active() || len(v.player.items) == 0 {
		return false
	}
	state := v.Playback.State()
	if !state.AudioOnly || isLivePlayback(state, v.player.current()) {
		return false
	}
	if state.Failure != "" || state.Clock.Ended || state.PreviewPending ||
		state.Resolution.Plan.Access == provider.VideoAccessRestricted {
		return false
	}
	return true
}

// HasBackgroundAudio reports whether pure-audio playback is continuing in the
// background while the user browses non-player pages.
func (v *View) HasBackgroundAudio() bool {
	if v == nil || v.State == nil || v.Playback == nil || !v.Playback.Active() || !v.player.background {
		return false
	}
	if v.State.Page == app.Player {
		return false
	}
	for _, location := range v.State.Layers() {
		if location.Page == app.Player {
			return false
		}
	}
	if len(v.player.items) == 0 {
		return false
	}
	state := v.Playback.State()
	if isLivePlayback(state, v.player.current()) {
		return false
	}
	return state.AudioOnly || state.Loading || state.Switching
}

// ResumeBackgroundAudio returns to the fullscreen Player route without
// interrupting or re-resolving the live background audio session.
func (v *View) ResumeBackgroundAudio() bool {
	if !v.HasBackgroundAudio() {
		return false
	}
	origin := v.State.Location
	if !v.State.Push(app.Route{Page: app.Player}) {
		return false
	}
	s := &v.player
	s.origin = origin
	s.background = false
	s.visible = true
	s.hideAt = time.Time{}
	s.panel = ""
	s.controlFocus = ""
	v.State.Focus, v.reveal = playerToggle, true
	v.Playback.SetFullscreen(true)
	return true
}

func (v *View) backgroundAudioPlayPauseAllowed() bool {
	if v.search.editing || v.pluginsUI.editing || v.pluginsUI.repo.editing ||
		v.danmakuUI.draft.editing || v.privateUI.draft.editing {
		return false
	}
	switch v.State.Page {
	case app.VideoCommentCompose, app.PrivateMessageCompose, favoriteFolderDialog,
		app.DanmakuRuleEditor, app.PluginRepoInput, app.SignInCredentials:
		return false
	}
	return true
}

func (v *View) hasBackgroundPlaybackSession() bool {
	if v == nil || v.State == nil || v.Playback == nil || !v.Playback.Active() || !v.player.background {
		return false
	}
	if v.State.Page == app.Player {
		return false
	}
	for _, location := range v.State.Layers() {
		if location.Page == app.Player {
			return false
		}
	}
	return true
}

func (v *View) tickBackgroundAudio(c *ui.Context) {
	if !v.hasBackgroundPlaybackSession() {
		return
	}
	now := c.Now()
	s := &v.player
	v.Playback.Tick(now)
	state := v.Playback.State()
	if !v.Playback.Active() {
		return
	}
	if len(s.items) == 0 || isLivePlayback(state, s.current()) ||
		(!state.Loading && !state.Switching && (!state.AudioOnly || state.Failure != "")) {
		v.closePlayback()
		return
	}
	state = v.advancePlaybackQueue(state, false)
	s.updatePlan(v, state)
	if cover := s.current().item.CoverURL; cover != "" && v.Catalog != nil {
		v.Catalog.PrepareArtwork([]string{cover})
	}
	v.syncNowPlaying(now)
	if state.Playing || state.Loading || state.Switching {
		c.After(backgroundAudioPoll)
	}
}

func (v *View) syncNowPlaying(now time.Time) {
	if v == nil || v.Playback == nil || !v.Playback.Active() {
		return
	}
	s := &v.player
	state := v.Playback.State()
	item := s.current()
	if isLivePlayback(state, item) || state.Failure != "" {
		if s.nowPlayingKey.active {
			s.nowPlayingKey = nowPlayingSyncKey{}
			v.Playback.SetNowPlaying(player.NowPlayingInfo{Active: false})
		}
		return
	}
	title := strings.TrimSpace(item.item.Title)
	artist := ""
	if item.item.Creator != nil {
		artist = strings.TrimSpace(item.item.Creator.Name)
	}
	album := strings.TrimSpace(item.item.Category)

	var art *app.CatalogArtwork
	if v.Catalog != nil && item.item.CoverURL != "" {
		art = v.Catalog.Artwork(item.item.CoverURL)
	}
	if art != s.nowPlayingArt {
		s.nowPlayingArt = art
		s.nowPlayingArtJPG = nil
		if art != nil && art.Image != nil {
			var buf bytes.Buffer
			if err := jpeg.Encode(&buf, art.Image, &jpeg.Options{Quality: 82}); err == nil {
				s.nowPlayingArtJPG = buf.Bytes()
			}
		}
	}

	rate := v.Playback.EffectiveRate()
	key := nowPlayingSyncKey{
		active:   true,
		revision: state.Revision,
		title:    title,
		artist:   artist,
		album:    album,
		playing:  state.Playing,
		rate:     rate,
		duration: int64(state.Duration),
		hasArt:   len(s.nowPlayingArtJPG) > 0,
	}
	jumped := math.Abs(state.Position-s.nowPlayingPos) >= 3.0
	if key == s.nowPlayingKey && !jumped && !s.nowPlayingAt.IsZero() && now.Sub(s.nowPlayingAt) < nowPlayingSyncInterval {
		return
	}
	s.nowPlayingKey = key
	s.nowPlayingAt = now
	s.nowPlayingPos = state.Position
	v.Playback.SetNowPlaying(player.NowPlayingInfo{
		Active:   true,
		Title:    title,
		Artist:   artist,
		Album:    album,
		Duration: state.Duration,
		Elapsed:  state.Position,
		Rate:     rate,
		Artwork:  s.nowPlayingArtJPG,
	})
}

// RemoteHandlers builds tvOS MPRemoteCommandCenter callbacks that safely
// dispatch playback actions onto the UI main thread.
func (v *View) RemoteHandlers(post func(func()), invalidate func()) player.RemoteHandlers {
	run := func(fn func()) {
		exec := func() {
			fn()
			if invalidate != nil {
				invalidate()
			}
		}
		if post != nil {
			post(exec)
		} else {
			exec()
		}
	}
	return player.RemoteHandlers{
		Play: func() {
			run(func() {
				if v.Playback != nil && v.Playback.Active() && !v.Playback.State().Playing {
					v.Playback.Toggle()
					v.syncNowPlaying(time.Now())
				}
			})
		},
		Pause: func() {
			run(func() {
				if v.Playback != nil && v.Playback.Active() && v.Playback.State().Playing {
					v.Playback.Toggle()
					v.syncNowPlaying(time.Now())
				}
			})
		},
		Toggle: func() {
			run(func() {
				if v.Playback != nil && v.Playback.Active() {
					v.Playback.Toggle()
					v.syncNowPlaying(time.Now())
				}
			})
		},
		Next: func() {
			run(func() {
				if v.Playback != nil && v.Playback.Active() && !isLivePlayback(v.Playback.State(), v.player.current()) {
					if v.stepPlayback(1, false) {
						v.syncNowPlaying(time.Now())
					}
				}
			})
		},
		Previous: func() {
			run(func() {
				if v.Playback != nil && v.Playback.Active() && !isLivePlayback(v.Playback.State(), v.player.current()) {
					if v.stepPlayback(-1, false) {
						v.syncNowPlaying(time.Now())
					}
				}
			})
		},
		Seek: func(seconds float64) {
			run(func() {
				if v.Playback != nil && v.Playback.Active() && !isLivePlayback(v.Playback.State(), v.player.current()) {
					v.Playback.Seek(seconds)
					v.syncNowPlaying(time.Now())
				}
			})
		},
	}
}
