package tvui

import (
	"context"
	"fmt"
	"log"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/provider"
)

const (
	playerToggle       app.FocusID = "player/toggle"
	playerPrevious     app.FocusID = "player/previous"
	playerNext         app.FocusID = "player/next"
	playerTimeline     app.FocusID = "player/timeline"
	playerWake         app.FocusID = "player/wake"
	playerInfo         app.FocusID = "player/info"
	playerLike         app.FocusID = "player/like"
	playerComments     app.FocusID = "player/comments"
	playerFavorite     app.FocusID = "player/favorite"
	playerDanmaku      app.FocusID = "player/danmaku"
	playerSubtitles    app.FocusID = "player/subtitles"
	playerAudio        app.FocusID = "player/audio"
	playerQuality      app.FocusID = "player/quality"
	playerLine         app.FocusID = "player/line"
	playerLiveFollow   app.FocusID = "player/live-follow"
	playerRoomFollow   app.FocusID = "player/room-follow"
	playerSpeed        app.FocusID = "player/speed"
	playerStatus       app.FocusID = "player/status"
	playerStatusLine   app.FocusID = "player/status/line"
	playerBuffer       app.FocusID = "player/buffering"
	playerControls                 = "player-controls"
	playerScrubGroup               = "player-timeline"
	playerPanelGroup               = "player-panel"
	playerChapterGroup             = "player-chapters"
	playerInset                    = float32(90)
	playerDrawerWidth              = float32(565)
	playerIdle                     = 6500 * time.Millisecond
)

type playbackItem struct {
	item   provider.MediaItem
	partID string
	label  string
	status string
	locked bool
}

type playbackView struct {
	origin           app.Location
	items            []playbackItem
	index            int
	keepQueue        bool
	metadata         context.CancelFunc
	followPending    context.CancelFunc
	openInfoOnReady  bool
	version          uint64
	visible          bool
	hideAt           time.Time
	panel            app.FocusID
	returnFocus      app.FocusID
	controlFocus       app.FocusID
	panelScroll        ui.ScrollState
	liveChatScroll     ui.ScrollState
	liveChatAutoFollow bool
	liveChatLastRev    uint64
	scrubbing        bool
	scrubPosition    float64
	scrubWasPlaying  bool
	selectingChapter bool
	chapterIndex     int
	announced        string
	bufferSince      time.Time
	feedback         playbackFeedback
	feedbackFocus    app.FocusID
	accessVisible    bool
	branchVisible    bool
	blurredState     *app.PlaybackState
	showAccessHelp   bool
	loginAttempted   bool
	speedSince       time.Time
	speedSampleAt    time.Time
	speedDisplayed   float64
	content          playbackContent
	discussion       videoDetailState
}

func (v *View) leavePlayback() {
	// Sign-in, text entry, and the folder editor are shown over this route.
	for _, location := range v.State.Layers() {
		if location.Page == app.Player {
			return
		}
	}
	if v.player.metadata != nil {
		v.player.metadata()
	}
	if v.player.followPending != nil {
		v.player.followPending()
	}
	v.player.content.close()
	v.player.discussion.discussion.cancelLoad()
	v.player.discussion.cancelOperation()
	v.player = playbackView{}
	if v.LiveChat != nil {
		v.LiveChat.Leave()
	}
	if v.Playback != nil {
		v.Playback.Close()
	}
}

func (p *PageContext) canPlay(ref provider.MediaRef) bool {
	entry, _ := p.State.Current()
	return p.host.Playback != nil && ref.EntryID == entry.ID && ref.Kind.Valid() &&
		entry.Capabilities.Has(provider.ResolvePlayback)
}

func (p *PageContext) startDetailPlayback(item provider.MediaItem, start *float64) {
	partID := ""
	fallback := 0
	if p.host.videoDetail.item.Ref == item.Ref {
		fallback = p.host.videoDetail.part
		if idx := p.host.videoDetail.episodeIndex(); idx >= 0 {
			episodes := videoEpisodes(item.VideoDetails)
			if idx < len(episodes) && !episodes[idx].Unavailable {
				partID = episodes[idx].PartID
			}
		}
	}
	items, selected := playbackEpisodes(item, nil, partID, fallback)
	p.openPlayback(items, selected, false, start)
}

func playbackEpisodes(item provider.MediaItem, segments []provider.VideoSegment, partID string, fallback int) ([]playbackItem, int) {
	if len(segments) == 0 && item.VideoDetails != nil {
		detail := item.VideoDetails
		if detail.SegmentKind == provider.VideoParts || detail.SegmentKind == provider.VideoCollection {
			segments = detail.Segments
		}
	}
	items := make([]playbackItem, 0, len(segments))
	selected := -1
	for _, segment := range segments {
		if segment.Unavailable || segment.Ref == nil && segment.PartID == "" {
			continue
		}
		row := playbackItem{item: item, partID: segment.PartID, label: segment.Title, status: segment.Status, locked: segment.Locked}
		if segment.Ref != nil && *segment.Ref != item.Ref {
			row.item = provider.MediaItem{Ref: *segment.Ref, Title: segment.Title, DurationSeconds: segment.DurationSeconds}
		}
		if row.item.Ref == item.Ref && (row.partID == partID || row.partID == "" && segment.Ref != nil) {
			selected = len(items)
			row.partID = partID
		}
		items = append(items, row)
	}
	if len(items) == 0 || selected < 0 && partID != "" {
		return []playbackItem{{item: item, partID: partID}}, 0
	}
	if selected < 0 {
		selected = min(max(0, fallback), len(items)-1)
	}
	return items, selected
}

func (p *PageContext) playWatchLater(rows []provider.AccountRow) {
	items := make([]playbackItem, 0, len(rows))
	for _, row := range rows {
		if row.Ref == nil || !p.canPlay(*row.Ref) {
			continue
		}
		items = append(items, playbackItem{item: provider.MediaItem{
			Ref: *row.Ref, Title: row.Title, CoverURL: row.CoverURL, DurationSeconds: row.DurationSeconds,
		}})
	}
	p.openPlayback(items, 0, true, nil)
}

func isLivePlayback(state app.PlaybackState, item playbackItem) bool {
	return state.Resolution.Plan.Live || item.item.Ref.Kind == provider.Live
}

func (p *PageContext) openLivePlayback(item provider.MediaItem, openInfo bool) {
	if !p.canPlay(item.Ref) {
		return
	}
	p.openPlayback([]playbackItem{{item: item}}, 0, true, nil)
	if p.State.Page == app.Player {
		p.host.player.openInfoOnReady = openInfo
	}
}

func (p *PageContext) openPlayback(items []playbackItem, index int, keepQueue bool, start *float64) {
	p.host.stopHomePreview()
	origin := p.State.Location
	if index < 0 || index >= len(items) || !p.canPlay(items[index].item.Ref) || !p.State.Push(app.Route{Page: app.Player}) {
		return
	}
	v := p.host
	v.player = playbackView{origin: origin, items: items, keepQueue: keepQueue}
	v.startPlaybackItem(index, start)
}

func (v *View) startPlaybackItem(index int, start *float64) {
	s := &v.player
	if index < 0 || index >= len(s.items) {
		return
	}
	if s.metadata != nil {
		s.metadata()
	}
	if s.followPending != nil {
		s.followPending()
		s.followPending = nil
	}
	s.content.clearTracks()
	s.discussion.discussion.cancelLoad()
	s.discussion.cancelOperation()
	s.discussion = videoDetailState{}
	s.index = index
	if index == 0 && v.State.Focus == playerPrevious || index == len(s.items)-1 && v.State.Focus == playerNext {
		v.State.Focus, v.reveal = playerToggle, true
	}
	s.visible, s.hideAt, s.scrubbing, s.selectingChapter = true, time.Time{}, false, false
	s.showAccessHelp = false
	s.resetNetworkSpeed()
	item, part := s.items[index].item, s.items[index].partID
	if !s.keepQueue && v.videoDetail.item.Ref == item.Ref &&
		v.videoDetail.item.VideoDetails != nil && v.videoDetail.item.VideoDetails.SegmentKind == provider.VideoParts {
		episodes := videoEpisodes(v.videoDetail.item.VideoDetails)
		if part != "" {
			if idx := slices.IndexFunc(episodes, func(seg provider.VideoSegment) bool {
				return !seg.Unavailable && seg.PartID == part
			}); idx >= 0 {
				v.videoDetail.part = idx
			}
		} else if index < len(episodes) && !episodes[index].Unavailable {
			v.videoDetail.part = index
		}
	}
	s.discussion.seed, s.discussion.item = item, item
	owner := v.Auth.Accounts().CurrentID
	syncSource := false
	if setting, ok := v.Preferences.(app.HistorySyncStore); ok && owner != "" {
		syncSource = setting.HistorySync(item.Ref.EntryID, owner)
	}
	if v.PlaybackSettings != nil {
		v.Playback.SetPreferences(v.PlaybackSettings)
	}
	v.Playback.Start(provider.PlaybackTarget{Ref: item.Ref, PartID: part}, owner, syncSource, start)
	if v.LiveChat != nil {
		if item.Ref.Kind == provider.Live {
			s.liveChatAutoFollow = true
			s.liveChatScroll = ui.ScrollState{}
			s.liveChatLastRev = 0
			v.LiveChat.Connect(item.Ref, "")
		} else {
			v.LiveChat.Leave()
		}
	}

	if item.VideoDetails != nil && item.Ref.Kind != provider.Live {
		return
	}
	s.metadata = v.Catalog.ReadVideoDetail(item.Ref, func(metadata provider.MediaItem, err error) {
		if v.Playback == nil || !v.Playback.Active() || s.current().item.Ref != item.Ref {
			return
		}
		s.metadata = nil
		if err == nil {
			for i := range s.items {
				if s.items[i].item.Ref == metadata.Ref {
					if metadata.Ref.Kind == provider.Live {
						if metadata.Views == nil {
							metadata.Views = s.items[i].item.Views
						}
						if metadata.CoverURL == "" {
							metadata.CoverURL = s.items[i].item.CoverURL
						}
						if metadata.BackdropURL == "" {
							metadata.BackdropURL = s.items[i].item.BackdropURL
						}
						if metadata.Creator == nil {
							metadata.Creator = s.items[i].item.Creator
						}
					}
					s.items[i].item = metadata
				}
			}
			s.discussion.item = metadata
		}
	})
}

func (s *playbackView) current() playbackItem {
	if s.index < 0 || s.index >= len(s.items) {
		return playbackItem{}
	}
	return s.items[s.index]
}

func syncPlayback(v *View) {
	if v.Playback != nil && v.Playback.Active() && v.Playback.Owner() != v.Auth.Accounts().CurrentID {
		v.player.content.closeChooser()
		if v.player.content.draft != nil {
			if v.player.content.draft.cancel != nil {
				v.player.content.draft.cancel()
			}
			v.player.content.draft = nil
		}
		state := v.Playback.State()
		if state.Target.PartID != "" {
			v.player.items[v.player.index].partID = state.Target.PartID
		}
		v.startPlaybackItem(v.player.index, &state.Position)
	}
}

func (v *View) togglePlayback() {
	v.player.show(v)
	v.Playback.Toggle()
}

func (s *playbackView) show(v *View) {
	if !s.visible || v.State.Focus == playerWake {
		v.State.Focus = playerToggle
		v.reveal = true
	}
	s.visible = true
	s.hideAt = time.Now().Add(playerIdle)
}

func (s *playbackView) restoreControl(v *View, focus app.FocusID) {
	s.controlFocus = focus
	s.show(v)
	v.State.Focus, v.reveal = focus, true
}

func (v *View) backFromPlayback() bool {
	s := &v.player
	if s.backFromContent(v) {
		return true
	}
	if s.panel != "" {
		s.closePanel(v)
		return true
	}
	if s.feedback != playbackFeedbackNone {
		return false
	}
	if s.selectingChapter {
		s.selectingChapter = false
		v.State.Focus, v.reveal = playerTimeline, true
		return true
	}
	if s.scrubbing {
		s.finishScrub(v, false)
		return true
	}
	state := v.Playback.State()
	if playbackHasBranchOverlay(state) {
		v.Playback.DismissBranch()
		s.show(v)
		v.State.Focus, v.reveal = playerToggle, true
		return true
	}
	if playbackHasAccessDialog(state) {
		if s.showAccessHelp {
			s.showAccessHelp = false
			return true
		}
		if state.Limit != "" && state.Clock.HasFrame && !state.PreviewPending && !state.Clock.Ended &&
			state.Resolution.Plan.Access != provider.VideoAccessRestricted {
			v.dismissPlaybackLimit()
			return true
		}
		return false
	}
	if s.visible && state.Clock.HasFrame && !state.Clock.Ended && state.Failure == "" {
		s.visible = false
		v.State.Focus, v.reveal = playerWake, true
		return true
	}
	return false
}

type playbackPage struct{}

func (playbackPage) Render(c *ui.Context, p *PageContext) {
	v, s := p.host, &p.host.player
	if v.Playback == nil {
		return
	}
	now := c.Now()
	v.Playback.Tick(now)
	state := v.Playback.State()
	liveMode := isLivePlayback(state, s.current())
	autoplay := v.playbackPreferences().Autoplay
	if autoplay && !liveMode && state.Clock.Ended && state.Resolution.Plan.Access != provider.VideoAccessPreview &&
		state.Failure == "" && !playbackHasAccessDialog(state) && !playbackHasBranchChoices(state) && !playbackAutoAdvancingBranch(state) &&
		s.index+1 < len(s.items) && p.active && !s.content.overlay() {
		s.panel, s.controlFocus = "", ""
		s.scrubbing, s.selectingChapter = false, false
		v.startPlaybackItem(s.index+1, nil)
		v.State.Focus, v.reveal = playerToggle, true
		state = v.Playback.State()
	}
	if autoplay && !liveMode && state.Playing && state.Clock.HasFrame && !state.Loading && !state.Switching &&
		!state.Clock.Buffering && !state.Clock.Ended && state.Failure == "" &&
		state.Resolution.Plan.Access != provider.VideoAccessPreview &&
		s.index+1 < len(s.items) && state.Duration >= 30 &&
		state.Duration-state.Position <= 20 && state.Duration-state.Position > 1 {
		next := s.items[s.index+1]
		if !next.locked {
			v.Playback.PreloadNext(provider.PlaybackTarget{Ref: next.item.Ref, PartID: next.partID}, v.Playback.Owner())
		}
	}
	if !playbackShowNetworkSpeed(state, s) {
		s.resetNetworkSpeed()
	}
	s.updatePlan(v, state)
	item := s.current()
	plan := state.Resolution.Plan
	liveMode = isLivePlayback(state, item)
	if liveMode && s.openInfoOnReady && state.Clock.HasFrame && s.panel == "" && !playbackBlocking(state) {
		s.openInfoOnReady = false
		s.openPanel(v, playerInfo)
	}
	if s.visible && s.hideAt.IsZero() && state.Clock.HasFrame {
		s.hideAt = now.Add(playerIdle)
	}
	if s.visible && state.Playing && state.Clock.HasFrame && !state.Clock.Buffering && !s.hideAt.IsZero() &&
		!now.Before(s.hideAt) && s.panel == "" && !s.content.overlay() && !s.scrubbing && !s.selectingChapter && p.State.Focus != playerTimeline {
		s.visible = false
		p.State.Focus, v.reveal = playerWake, true
	}
	if s.visible || s.panel != "" || !state.Clock.HasFrame {
		images := []string{item.item.CoverURL}
		if item.item.Creator != nil {
			images = append(images, item.item.Creator.AvatarURL)
		}
		for _, chapter := range plan.Chapters {
			images = append(images, chapter.ImageURL)
		}
		if s.scrubbing && len(plan.Chapters) == 0 {
			if _, location := playbackThumbnailTile(plan.Thumbnails, s.scrubPosition); location != "" {
				images = append(images, location)
			}
		}
		if s.panel == playerComments {
			images = s.discussion.videoDiscussion().artwork(images, item.item.VideoDetails)
		}
		v.Catalog.PrepareArtwork(images)
	}
	if v.Trace {
		s.trace(state)
	}
	surfaceWidth := p.Width
	drawerOpen := (!liveMode && s.panel == playerInfo) || s.panel == playerComments
	if drawerOpen && !playbackHasAccessDialog(state) {
		surfaceWidth -= playerDrawerWidth
	}
	if s.content.needsAnimationFrame(v, state, now, surfaceWidth) ||
		(liveMode && s.content.needsLiveDanmakuFrame(v, state, now)) {
		c.AnimationFrame()
	} else if !liveMode && state.Playing && !state.BranchPending && plan.Branch != nil && len(plan.Branch.Options) > 0 {
		c.After(100 * time.Millisecond)
	} else {
		c.After(s.content.refresh(v, state))
	}
	wasBlocking := s.feedback != playbackFeedbackNone || s.accessVisible || s.branchVisible
	s.feedback = s.feedbackFor(c, state)
	if s.feedback != playbackFeedbackNone {
		s.accessVisible = false
		s.branchVisible = false
		p.playbackFeedbackScreen(c, s.feedback, state, item.item.CoverURL)
		return
	}
	contentModal := s.content.overlay()
	access := playbackHasAccessDialog(state) && !contentModal && p.State.Page != app.SignInQR
	branchVisible := playbackHasBranchOverlay(state) && !access && !contentModal && p.State.Page != app.SignInQR
	if branchVisible && s.panel != "" {
		s.panel = ""
	}
	s.accessVisible = access
	s.branchVisible = branchVisible
	if p.active && wasBlocking && !access && !branchVisible {
		if v.State.Focus == playerBuffer {
			v.State.Focus = s.feedbackFocus
		}
		if v.State.Focus == "" || strings.HasPrefix(string(v.State.Focus), string(playerStatus)) ||
			strings.HasPrefix(string(v.State.Focus), string(playerBranchControl)+"/") {
			v.State.Focus = playerWake
			if s.visible {
				v.State.Focus = playerToggle
			}
		}
		// A blocking surface unmounts the native focus target. Restore it even
		// when the logical player control is unchanged.
		v.reveal = true
		s.feedbackFocus = ""
	}
	if p.active && !access && !branchVisible && !contentModal && s.panel == "" && v.State.Focus == "" {
		v.State.Focus = playerWake
		if s.visible {
			v.State.Focus = playerToggle
		}
		v.reveal = true
	}
	if p.active && s.visible && !access && !branchVisible && !contentModal && s.panel == "" && s.controlFocus != "" {
		// The popover or seek preview can relinquish native focus during removal.
		// Keep the requested control until the replacement focus is committed.
		if s.controlFocus == ui.FocusedValue(c, &v.State.Focus) {
			s.controlFocus = ""
		} else {
			v.State.Focus, v.reveal = s.controlFocus, true
		}
	}
	externalBlur := v.modalBlur(p.State.Route, true)
	backdropBlur := externalBlur
	liveRoomOverlay := liveMode && s.panel == playerInfo
	if access || contentModal || liveRoomOverlay {
		backdropBlur = 28
	}
	player := ui.Box(c.Key("player")).Absolute().Top(0).Left(0).Fill().Clip().Lang(p.langTag()).Font(p.uiFont())
	// The player can decode the preview before the viewer chooses to watch it.
	useOrigin := state.PreviewPending || !state.Clock.HasFrame &&
		(state.Limit == provider.PlaybackRegion || playbackHasWorkRestriction(state))
	if access && useOrigin && s.origin.Page != "" {
		player.Children(func() {
			ui.Box(c.Key("playback-access-origin")).Absolute().Top(0).Left(0).Fill().Clip().IgnoreInput(true).AccessHidden(true).
				Background(paint(p.Theme.Colors.Surface.Canvas)).Cache().Blur(backdropBlur).Children(func() {
				v.renderRootPage(c, s.origin, false, p.Width, p.Height)
			})
			p.drawPlaybackAccess(c, state, item)
		})
		return
	}
	if !access && p.State.Page != app.SignInQR {
		s.showAccessHelp = false
	}
	surface := *p
	showChapters := !liveMode && len(plan.Chapters) > 0 && (s.selectingChapter || p.State.Focus == playerTimeline)
	if drawerOpen && !access {
		surface.Width -= playerDrawerWidth
	}
	if p.Width > 0 {
		v.Playback.SetViewport(float64(surface.Width / p.Width))
	}
	hasBackdrop := v.Playback.SetBackdropBlur(backdropBlur)
	display := state
	if backdropBlur > 0 {
		if s.blurredState == nil || s.blurredState.Revision != state.Revision {
			s.blurredState = &state
		}
		display = *s.blurredState
	} else {
		s.blurredState = nil
	}
	player.Children(func() {
		video := ui.Box(c.Key("player-surface")).Absolute().Top(0).Bottom(0).Left(0).Width(surface.Width).Clip()
		if (access || contentModal || liveRoomOverlay) && externalBlur == 0 {
			video.Cache().Blur(backdropBlur)
		}
		video.Children(func() {
			if !hasBackdrop && (!state.Clock.HasFrame || backdropBlur > 0) {
				surface.playbackPoster(c, item.item.CoverURL)
			}
			if !branchVisible && s.visible {
				surface.playbackShade(c, showChapters)
				surface.playbackTop(c, item, display, state)
			} else if !branchVisible && playbackShowNetworkSpeed(state, s) {
				ui.Box(c.Key("player-network-speed")).Absolute().Right(playerInset).Top(67).IgnoreInput(true).Children(func() {
					surface.playbackNetworkSpeed(c, state, true)
				})
			}
			if backdropBlur == 0 {
				surface.playbackTextLayers(c, state)
			}
			if !branchVisible && display.Resolution.Plan.Access == provider.VideoAccessPreview && !display.PreviewPending {
				surface.playbackPreviewBanner(c, display)
			}
			if !branchVisible {
				if s.visible {
					surface.playbackControls(c, display, item)
					if showChapters {
						surface.playbackChapters(c, display)
					} else if s.scrubbing {
						surface.playbackSeekPreview(c, display, item.item.CoverURL)
					}
				} else {
					surface.playbackWake(c)
				}
			}
		})
		if s.panel != "" && !access && !branchVisible && !contentModal {
			p.playbackPanel(c, state, item)
		}
		if branchVisible {
			p.drawPlaybackBranch(c, state)
		}
		if access {
			p.drawPlaybackAccess(c, state, item)
		}
		if s.content.chooser != nil {
			p.drawFavoriteChooser(c, s.content.chooser)
		}
		if s.content.draft != nil {
			p.drawDanmakuComposer(c, s.content.draft)
		}
	})
}

func (s *playbackView) updatePlan(v *View, state app.PlaybackState) {
	if state.Revision == s.version {
		return
	}
	s.version = state.Revision
	if state.LiveEnded && v.LiveChat != nil {
		v.LiveChat.Leave()
	}
	if s.content.resumeQuality && !state.Loading {
		s.content.resumeQuality = false
		if s.content.requestQuality != "" {
			v.Playback.ChooseQuality(s.content.requestQuality)
		}
	}
	plan := state.Resolution.Plan
	if !isLivePlayback(state, s.current()) && !s.content.subtitleChosen && len(plan.Subtitles) > 0 {
		if entry, ok := v.State.Current(); ok && entry.Capabilities.Has(provider.ReadSubtitles) {
			s.content.subtitleChosen = true
			if matched, found := app.MatchPreferredSubtitle(v.playbackPreferences().Subtitle, plan.Subtitles); found {
				s.content.setSubtitle(v, matched)
			}
		}
	}
	if s.panel == playerAudio && len(state.AudioTracks) <= 1 {
		s.panel = ""
		if v.State.Focus == playerAudio || strings.HasPrefix(string(v.State.Focus), string(playerAudio)+"/") {
			v.State.Focus, v.reveal = playerToggle, true
		}
	}
	if s.keepQueue || state.Target.PartID == "" {
		return
	}
	current := s.current()
	if len(plan.Episodes) > 0 {
		s.items, s.index = playbackEpisodes(current.item, plan.Episodes, state.Target.PartID, s.index)
	} else if len(s.items) > 0 {
		s.items[s.index].partID = state.Target.PartID
	}
}

func (s *playbackView) trace(state app.PlaybackState) {
	status := fmt.Sprintf("ready=%t buffering=%t ended=%t err=%s native=%d range=%s playing=%t",
		state.Clock.HasFrame, state.Clock.Buffering, state.Clock.Ended, state.Failure, state.Clock.ErrorCode, state.VideoRange, state.Playing)
	if s.announced != status {
		s.announced = status
		log.Printf("player: %s position=%.2f duration=%.2f audio=%t hdr=%t awake=%t display-match=%t",
			status, state.Position, state.Duration, state.Clock.AudioActive, state.Clock.HDRVideo, state.Clock.IdleDisabled, state.Clock.MatchDisplay)
	}
}

func (p *PageContext) playbackPoster(c *ui.Context, cover string) {
	ui.Box(c).Absolute().Left(0).Top(0).Fill().Background(paint(p.Theme.Colors.Surface.Canvas)).Children(func() {
		if art := p.catalogCover(cover); art != nil {
			ui.Image(c, art).Fill().Fit(ui.Cover)
		}
	})
}

func (p *PageContext) playbackShade(c *ui.Context, chapters bool) {
	colors := p.Theme.Colors
	shade := ui.LinearGradient{
		Angle: 180, Stops: []ui.GradientStop{
			{Color: paint(colors.Overlay.PlayerTop)}, {At: .40, Color: ui.Transparent},
			{At: .60, Color: ui.Transparent}, {At: 1, Color: paint(colors.Overlay.PlayerBottom)},
		},
	}
	if chapters {
		base := colors.Overlay.PlayerBottom
		shade.Stops = []ui.GradientStop{
			{Color: withAlpha(base, 0xc9)},
			{At: .25, Color: withAlpha(base, 0x20)},
			{At: .45, Color: withAlpha(base, 0xa6)},
			{At: .68, Color: withAlpha(base, 0xbd)},
			{At: 1, Color: withAlpha(base, 0xf5)},
		}
	}
	ui.Box(c).Absolute().Left(0).Top(0).Fill().IgnoreInput(true).LinearGradient(shade)
}

func (p *PageContext) playbackTop(c *ui.Context, item playbackItem, display, live app.PlaybackState) {
	colors := p.Theme.Colors.Playback
	liveMode := isLivePlayback(display, item)
	showSpeed := playbackShowNetworkSpeed(live, &p.host.player)
	label := playbackQualityLabel(display)
	if liveMode {
		label = p.Assets.Text.Following.LiveBadge
	}
	ui.Row(c).Absolute().Left(playerInset).Right(playerInset).Top(67).Gap(20).Justify(ui.SpaceBetween).AlignItems(ui.Center).Children(func() {
		ui.Column(c).Gap(13).Shrink(1).Children(func() {
			title := item.item.Title
			if branch := display.Resolution.Plan.Branch; branch != nil && branch.NodeTitle != "" && branch.NodeTitle != title {
				title += " · " + branch.NodeTitle
			} else if item.label != "" && item.label != title {
				title += " · " + item.label
			}
			ui.Text(c, title).FontSize(29).FontWeight(450).MaxLines(1).TextColor(paint(colors.Foreground))
			ui.Row(c).Gap(13).AlignItems(ui.Center).Children(func() {
				subtitleTag := item.item.Category
				if liveMode {
					subtitleTag = p.Assets.Text.Following.LiveBadge
				}
				if creator := item.item.Creator; creator != nil {
					ui.Text(c, creator.Name).FontSize(15).TextColor(paint(colors.Secondary))
					if subtitleTag != "" {
						ui.Text(c, "·").FontSize(15).TextColor(paint(colors.Secondary))
					}
				}
				if subtitleTag != "" {
					ui.Text(c, subtitleTag).FontSize(15).TextColor(paint(colors.Secondary))
				}
			})
		})
		if showSpeed || label != "" {
			ui.Row(c).Shrink(0).Gap(22).AlignItems(ui.Center).Children(func() {
				if showSpeed {
					p.playbackNetworkSpeed(c, live, true)
				}
				if label != "" {
					ui.Text(c, label).Shrink(0).Padding(5, 8).Radius(4).FontSize(14).FontWeight(650).TextColor(paint(colors.Foreground)).
						Border(1, withAlpha(colors.Foreground, 0x65))
				}
			})
		}
	})
}

func (p *PageContext) playbackControls(c *ui.Context, state app.PlaybackState, item playbackItem) {
	v, s, text := p.host, &p.host.player, p.Assets.Text.Playback
	colors := p.Theme.Colors
	liveMode := isLivePlayback(state, item)
	regions := focusEdges{}
	if !liveMode {
		regions[app.Up] = focusDestination{Focus: playerTimeline}
		p.region(playerScrubGroup, focusEdges{app.Down: {Focus: playerToggle}})
	}
	p.region(playerControls, regions)
	controls := *p
	controls.active = p.active && s.panel == "" && !s.selectingChapter && !playbackBlocking(state) && !s.content.overlay()
	controlHeight := float32(105)
	if liveMode {
		controlHeight = 65
	}
	ui.Box(c.Key("player-controls")).Absolute().Bottom(68).Left(playerInset).Right(playerInset).Height(controlHeight).Children(func() {
		if !liveMode {
			controls.playbackTimeline(c, state, item.item.Title)
		}
		row := ui.Row(c).Absolute().Bottom(0).Left(0).Right(0).Height(65).Gap(25).AlignItems(ui.Center).Justify(ui.SpaceBetween)
		if s.selectingChapter {
			row.Opacity(.35)
		}
		row.Children(func() {
			infoLabel := text.Info
			if liveMode {
				infoLabel = text.RoomInfo
			}
			ui.Row(c).Height(65).Gap(14).AlignItems(ui.Center).Children(func() {
				if creator := item.item.Creator; creator != nil {
					p.videoCommentAvatar(c, creator.Name, creator.AvatarURL, 49, p.catalogCover(creator.AvatarURL), false)
					ui.Column(c).Width(115).Gap(5).Children(func() {
						ui.Text(c, creator.Name).FillWidth().MaxLines(1).FontSize(19).TextColor(paint(colors.Playback.Foreground))
						ui.Text(c, infoLabel).FontSize(13).TextColor(paint(colors.Playback.Secondary))
					})
				}
				controls.playerButton(c, playerInfo, "info", infoLabel, "", false, func() { s.openPanel(v, playerInfo) })
			})
			ui.Row(c).Height(65).Gap(15).Shrink(0).AlignItems(ui.Center).Children(func() {
				if !liveMode {
					controls.playerButton(c, playerPrevious, "skip-back", text.Previous, "", s.index == 0, func() {
						v.startPlaybackItem(s.index-1, nil)
					})
				}
				icon, label := "play", text.Play
				if state.Playing {
					icon, label = "pause", text.Pause
				}
				controls.playerButton(c, playerToggle, icon, label, "", false, v.togglePlayback)
				if !liveMode {
					controls.playerButton(c, playerNext, "skip-forward", text.Next, "", s.index+1 >= len(s.items), func() {
						v.startPlaybackItem(s.index+1, nil)
					})
				}
			})
			ui.Row(c).Height(65).Gap(15).Shrink(0).AlignItems(ui.Center).Children(func() {
				entry, _ := p.State.Current()
				if liveMode {
					if entry.Capabilities.Has(provider.LiveChat) {
						controls.playerButton(c, playerComments, "comment", text.LiveChatTitle, "", false, func() { s.openPanel(v, playerComments) })
					}
					if entry.Capabilities.Has(provider.FollowCreator) && item.item.Creator != nil {
						followIcon, followTitle := "plus", p.Assets.Text.Detail.Follow
						if item.item.Creator.Following {
							followIcon, followTitle = "check", p.Assets.Text.Detail.Followed
						}
						controls.playerButton(c, playerLiveFollow, followIcon, followTitle, "", false, p.toggleLiveFollow)
					}
					if entry.Capabilities.Has(provider.LiveChat) {
						controls.playerButton(c, playerDanmaku, "", text.Danmaku, text.Danmaku, false, func() { s.openPanel(v, playerDanmaku) })
					}
					if len(state.Resolution.Plan.Qualities) > 0 || playbackQualityLabel(state) != "" {
						controls.playerButton(c, playerQuality, "", text.LiveQuality, playbackQualityLabel(state), false, func() { s.openPanel(v, playerQuality) })
					}
					if len(state.Resolution.Plan.Lines) > 0 {
						controls.playerButton(c, playerLine, "", text.Line, text.Line, false, func() { s.openPanel(v, playerLine) })
					}
					return
				}
				if entry.Capabilities.Has(provider.EditVideo) {
					like := "like"
					if info := item.item.VideoDetails; info != nil && info.Engagement.Liked {
						like = "like-filled"
					}
					controls.playerButton(c, playerLike, like, p.Assets.Text.Detail.Like, "", false, func() { p.togglePlayerLike() })
				}
				if entry.Capabilities.Has(provider.ReadVideoComments) {
					controls.playerButton(c, playerComments, "comment", text.Comments, "", false, func() { s.openPanel(v, playerComments) })
				}
				if entry.Capabilities.Has(provider.EditVideoFavorites) {
					icon := "star"
					if info := item.item.VideoDetails; info != nil && info.Engagement.Favorite {
						icon = "star-filled"
					}
					controls.playerButton(c, playerFavorite, icon, p.Assets.Text.Detail.Favorite, "", false, p.openPlayerFavorites)
				}
				if playbackHasBranchChoices(state) {
					controls.playerButton(c, playerBranchControl, "", text.BranchButton, text.BranchButton, false, func() {
						v.Playback.OpenBranch()
					})
				}
				if entry.Capabilities.Has(provider.ReadDanmaku) {
					controls.playerButton(c, playerDanmaku, "", text.Danmaku, text.Danmaku, false, func() { s.openPanel(v, playerDanmaku) })
				}
				if entry.Capabilities.Has(provider.ReadSubtitles) && (len(state.Resolution.Plan.Subtitles) > 0 || s.content.subtitle != "") {
					controls.playerButton(c, playerSubtitles, "captions", text.Subtitles, "", false, func() { s.openPanel(v, playerSubtitles) })
				}
				if len(state.AudioTracks) > 1 {
					controls.playerButton(c, playerAudio, "", text.AudioTrack, text.AudioTrack, false, func() { s.openPanel(v, playerAudio) })
				}
				if len(state.Resolution.Plan.Qualities) > 0 || playbackQualityLabel(state) != "" {
					controls.playerButton(c, playerQuality, "", text.Quality, playbackQualityLabel(state), false, func() { s.openPanel(v, playerQuality) })
				}
				controls.playerButton(c, playerSpeed, "", text.Speed, playbackRateLabel(v.Playback.Rate()), false, func() { s.openPanel(v, playerSpeed) })
			})
		})
	})
}

func (p *PageContext) playerButton(c *ui.Context, id app.FocusID, icon, title, label string, disabled bool, action func()) {
	color := p.Theme.Colors
	width, height := float32(54), float32(54)
	background, ink := ui.Transparent, paint(color.Playback.Foreground)
	border, borderWidth := ui.Transparent, float32(0)
	if label != "" {
		width = 78
		border, borderWidth = withAlpha(color.Playback.Foreground, 0x25), 1
		if id == playerQuality {
			width = 97
		} else if id == playerBranchControl {
			width = 112
		}
	}
	if id == playerToggle || id == playerPrevious || id == playerNext {
		width, height = 65, 65
	}
	if id == playerToggle {
		background = withAlpha(color.Playback.Foreground, 0x1b)
	}
	if icon == "like-filled" || icon == "star-filled" {
		icon = strings.TrimSuffix(icon, "-filled")
		background, ink = withAlpha(color.Playback.Foreground, 0x12), paint(color.Accent.Soft)
	}
	focused := p.paintFocus() == id
	if focused {
		background, ink = paint(color.Focus.Background), paint(color.Focus.Foreground)
		border, borderWidth = paint(color.Focus.Ring), 3
	}
	if disabled {
		ink = withAlpha(color.Playback.Foreground, 0x40)
	}
	button := ui.Button(c.Key(string(id)), "").Label(title).Width(width).Height(height).
		Padding(0, 0).Radius(height/2).Border(borderWidth, border).
		Background(background).TextColor(ink).Center().Shrink(0).Disabled(disabled).IgnoreInput(!p.active)
	if !disabled {
		button.OnClick(func() {
			p.dispatch(func() { p.host.player.show(p.host); action() })
		})
		p.bind(button, id, playerControls)
	}
	button.Children(func() {
		if icon != "" {
			size := float32(25)
			ui.Icon(c, p.Assets.Icon(icon)).Size(size, size).TextColor(ink)
		}
		if label != "" {
			ui.Text(c, label).FontSize(18).FontWeight(550).TextColor(ink)
		}
	})
}

func (p *PageContext) playbackTimeline(c *ui.Context, state app.PlaybackState, title string) {
	v, s := p.host, &p.host.player
	position := state.Position
	if s.scrubbing {
		position = s.scrubPosition
	}
	colors := p.Theme.Colors
	length := p.Width - 2*playerInset
	duration := playbackTimelineDuration(state, s.current().item)
	progress := playbackFraction(position, duration)
	focused := p.paintFocus() == playerTimeline
	track := ui.Button(c.Key(string(playerTimeline)), "").Label(p.Assets.Text.Playback.Progress).
		Absolute().Left(0).Right(0).Top(0).Height(12).Padding(0, 0).Radius(6).
		Border(0, ui.Transparent).Background(paint(colors.Playback.Track)).
		OnClick(func() {
			p.dispatch(func() {
				if s.scrubbing {
					s.finishScrub(v, true)
				}
			})
		})
	p.bindDirection(track, playerTimeline, playerScrubGroup, func(direction app.Direction) bool {
		switch direction {
		case app.Left, app.Right:
			if state.Duration <= 0 || state.Loading || state.Resolution.Plan.Access == provider.VideoAccessRestricted {
				return true
			}
			if !s.scrubbing {
				s.scrubbing, s.scrubWasPlaying = true, state.Playing
				s.scrubPosition = state.Position
				if state.Playing {
					v.Playback.Toggle()
				}
			}
			step := float64(10)
			if direction == app.Left {
				step = -step
			}
			s.scrubPosition = min(max(s.scrubPosition+step, 0), state.Duration)
			return true
		case app.Up:
			chapters := state.Resolution.Plan.Chapters
			if len(chapters) > 0 {
				s.chapterIndex = currentPlaybackChapter(chapters, position)
				s.selectingChapter = true
				p.State.Focus, v.reveal = playerChapterID(s.chapterIndex), true
			}
			return true
		case app.Down:
			if s.scrubbing {
				s.finishScrub(v, false)
			}
		}
		return false
	})
	track.Children(func() {
		ui.Box(c).Absolute().Left(0).Top(0).Height(12).Width(length * playbackFraction(state.Clock.BufferEnd, duration)).
			Radius(6).Background(withAlpha(colors.Playback.Foreground, 0x26))
		ui.Box(c).Absolute().Left(0).Top(0).Height(12).Width(length * progress).Radius(6).Background(paint(colors.Playback.Progress))
		for _, part := range state.Resolution.Plan.Chapters {
			if part.Start > 0 && part.Start < state.Duration {
				x := length*playbackFraction(part.Start, duration) - 2
				ui.Box(c).Absolute().Left(x).Top(0).Size(4, 12).Background(withAlpha(colors.Surface.Canvas, 0xe0))
			}
		}
		thumb := float32(19)
		knobFill := paint(colors.Playback.Foreground)
		if focused {
			thumb = 26
			knobFill = paint(colors.Focus.Background)
		}
		knob := ui.Box(c).Absolute().Left(length*progress-thumb/2).Top(6-thumb/2).
			Size(thumb, thumb).Radius(thumb / 2).Background(knobFill)
		if focused {
			knob.Border(2, paint(colors.Focus.Ring))
		}
	})
	ui.Text(c, playbackTime(position)+" / "+playbackTime(duration)).Absolute().Right(0).Top(-35).
		FontSize(15).TextColor(paint(colors.Playback.Secondary))
	if chapters := state.Resolution.Plan.Chapters; len(chapters) > 0 {
		title = chapters[currentPlaybackChapter(chapters, position)].Title
	}
	ui.Text(c, title).Absolute().Left(0).Top(-38).MaxWidth(length - 160).MaxLines(1).
		FontSize(17).TextColor(paint(colors.Playback.Foreground))
}

func (s *playbackView) finishScrub(v *View, confirm bool) {
	if confirm {
		v.Playback.Seek(s.scrubPosition)
	}
	s.scrubbing = false
	if s.scrubWasPlaying && !v.Playback.State().Playing {
		v.Playback.Toggle()
	}
	s.restoreControl(v, playerTimeline)
}

func (p *PageContext) playbackWake(c *ui.Context) {
	v := p.host
	if v.player.content.overlay() || playbackBlocking(v.Playback.State()) {
		return
	}
	button := ui.Button(c.Key(string(playerWake)), "").Label(p.Assets.Text.Playback.ShowControls).
		Absolute().Left(0).Top(0).Fill().Background(ui.Transparent).Border(0, ui.Transparent).
		OnClick(func() { p.dispatch(func() { v.player.show(v) }) })
	p.bindDirection(button, playerWake, playerControls, func(app.Direction) bool {
		v.player.show(v)
		return true
	})
}

func playbackFraction(at, duration float64) float32 {
	if duration <= 0 {
		return 0
	}
	return float32(min(max(0, at/duration), 1))
}

func playbackTimelineDuration(state app.PlaybackState, item provider.MediaItem) float64 {
	plan := state.Resolution.Plan
	if plan.Access != provider.VideoAccessPreview {
		return state.Duration
	}
	for _, episode := range plan.Episodes {
		if state.Target.PartID != "" && episode.PartID == state.Target.PartID && episode.DurationSeconds != nil {
			return max(state.Duration, float64(*episode.DurationSeconds))
		}
	}
	if len(plan.Episodes) == 0 && item.DurationSeconds != nil {
		return max(state.Duration, float64(*item.DurationSeconds))
	}
	return state.Duration
}

func playbackTime(seconds float64) string {
	value := int64(max(0, seconds))
	if value >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", value/3600, value%3600/60, value%60)
	}
	return fmt.Sprintf("%02d:%02d", value/60, value%60)
}

func playbackQualityLabel(state app.PlaybackState) string {
	for _, quality := range state.Resolution.Plan.Qualities {
		if quality.ID == state.Resolution.Plan.Quality {
			return quality.Label
		}
	}
	return ""
}

func playbackAudioLabel(state app.PlaybackState) string {
	active := state.ActiveAudio
	if active == "" {
		active = state.DefaultAudio
	}
	for _, track := range state.AudioTracks {
		if track.ID == active {
			return track.Label
		}
	}
	if len(state.AudioTracks) > 0 {
		return state.AudioTracks[0].Label
	}
	return ""
}

func playbackRateLabel(rate float64) string {
	label := strconv.FormatFloat(rate, 'f', -1, 64)
	if !strings.Contains(label, ".") {
		label += ".0"
	}
	return label + "x"
}
