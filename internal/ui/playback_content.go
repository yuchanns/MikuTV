package tvui

import (
	"context"
	"fmt"
	"hash/fnv"
	"image"
	"image/draw"
	"math"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/provider"
)

const (
	danmakuMovingTime     = 8.0
	danmakuStaticTime     = 4.5
	danmakuMaxScrollLanes = 14
	danmakuMaxStaticLanes = 4
)

type scheduledDanmaku struct {
	key   uint64
	text  string
	mode  provider.DanmakuMode
	start float64
	end   float64
	lane  int
	width float32
}

type danmakuNodeKey struct {
	id uint64
}

type playbackContent struct {
	subtitle               string
	subtitleQuery          context.CancelFunc
	cues                   []provider.SubtitleCue
	danmakuQuery           context.CancelFunc
	danmakuSegment         int
	danmakuRetry           time.Time
	danmaku                []provider.Danmaku
	danmakuGen             uint64
	danmakuSchedule        []scheduledDanmaku
	danmakuScheduleGen     uint64
	danmakuScheduleDensity int
	danmakuScheduleWidth   int
	danmakuClockAt         time.Time
	danmakuClockPos        float64
	danmakuClockRev        uint64
	danmakuStatus          provider.ErrorCode
	draft                  *danmakuDraft
	chooser                *playerFavoriteChooser
	message                string
	messageUntil           time.Time
	requestQuality         string
	resumeQuality          bool
	thumbIndex             int
	thumbScene             *app.CatalogArtwork
	thumbBitmap            *ui.Bitmap
	afterLogin             func()
}

type playbackSettings struct {
	initialized bool
	show        bool
	density     int
	opacity     int
}

func (s *playbackSettings) defaults() {
	if !s.initialized {
		*s = playbackSettings{initialized: true, show: true, density: 60, opacity: 75}
	}
}

func (s *playbackContent) clearTracks() {
	if s.subtitleQuery != nil {
		s.subtitleQuery()
	}
	if s.danmakuQuery != nil {
		s.danmakuQuery()
	}
	s.subtitle, s.cues, s.subtitleQuery = "", nil, nil
	s.danmakuQuery, s.danmaku, s.danmakuSegment = nil, nil, 0
	s.danmakuGen++
	s.danmakuSchedule = nil
	s.danmakuClockAt, s.danmakuClockPos, s.danmakuClockRev = time.Time{}, 0, 0
	s.danmakuRetry, s.danmakuStatus = time.Time{}, ""
	s.thumbBitmap, s.thumbScene = nil, nil
}

func (s *playbackContent) close() {
	s.clearTracks()
	if s.draft != nil && s.draft.cancel != nil {
		s.draft.cancel()
	}
	s.closeChooser()
}

func (s *playbackContent) notice(text string) {
	s.message, s.messageUntil = text, time.Now().Add(2600*time.Millisecond)
}

func (s *playbackContent) danmakuPosition(now time.Time, state app.PlaybackState, rate float64) float64 {
	if !state.Playing || state.Clock.Buffering || state.Clock.Ended || !state.Clock.HasFrame || rate <= 0 {
		s.danmakuClockAt = time.Time{}
		s.danmakuClockPos = state.Position
		s.danmakuClockRev = state.Revision
		return state.Position
	}
	if s.danmakuClockAt.IsZero() || s.danmakuClockRev != state.Revision {
		s.danmakuClockAt = now
		s.danmakuClockPos = state.Position
		s.danmakuClockRev = state.Revision
		return state.Position
	}
	dt := now.Sub(s.danmakuClockAt).Seconds()
	s.danmakuClockAt = now
	if dt < 0 || dt > 0.25 {
		s.danmakuClockPos = state.Position
		return state.Position
	}
	predicted := s.danmakuClockPos + dt*rate
	drift := state.Position - predicted
	if math.Abs(drift) > 0.12 {
		predicted = state.Position
	} else {
		predicted += drift * 0.10
	}
	s.danmakuClockPos = predicted
	return predicted
}

func (s *playbackContent) ensureDanmakuSchedule(density int, viewportWidth float32) []scheduledDanmaku {
	widthKey := int(math.Round(float64(viewportWidth)))
	if s.danmakuScheduleGen == s.danmakuGen &&
		s.danmakuScheduleDensity == density &&
		s.danmakuScheduleWidth == widthKey {
		return s.danmakuSchedule
	}
	s.danmakuSchedule = scheduleDanmaku(s.danmaku, density, viewportWidth)
	s.danmakuScheduleGen = s.danmakuGen
	s.danmakuScheduleDensity = density
	s.danmakuScheduleWidth = widthKey
	return s.danmakuSchedule
}

func (s *playbackContent) hasActiveDanmaku(pos, lookahead float64, density int, viewportWidth float32) bool {
	schedule := s.ensureDanmakuSchedule(density, viewportWidth)
	if len(schedule) == 0 {
		return false
	}
	first := sort.Search(len(schedule), func(i int) bool { return schedule[i].start > pos-danmakuMovingTime })
	for i := first; i < len(schedule); i++ {
		item := schedule[i]
		if item.start > pos+lookahead {
			break
		}
		if item.mode == provider.DanmakuScroll && pos < item.end {
			return true
		}
	}
	return false
}

func (s *playbackContent) needsAnimationFrame(v *View, state app.PlaybackState, now time.Time, viewportWidth float32) bool {
	v.playbackSettings.defaults()
	entry, _ := v.State.Current()
	if state.Resolution.Plan.Live || state.Target.Ref.Kind == provider.Live ||
		!v.playbackSettings.show || !entry.Capabilities.Has(provider.ReadDanmaku) ||
		state.Target.PartID == "" || !state.Clock.HasFrame || state.Clock.Ended || state.Failure != "" {
		return false
	}
	s.loadDanmaku(v, state)
	if !state.Playing || state.Clock.Buffering || len(s.danmaku) == 0 {
		return false
	}
	pos := state.Position
	if !s.danmakuClockAt.IsZero() && s.danmakuClockRev == state.Revision {
		pos = s.danmakuClockPos
	}
	return s.hasActiveDanmaku(pos, 0.25, v.playbackSettings.density, viewportWidth)
}

func (s *playbackContent) refresh(v *View, state app.PlaybackState) time.Duration {
	v.playbackSettings.defaults()
	entry, _ := v.State.Current()
	live := state.Resolution.Plan.Live || state.Target.Ref.Kind == provider.Live
	if !live && v.playbackSettings.show && entry.Capabilities.Has(provider.ReadDanmaku) &&
		state.Target.PartID != "" && state.Clock.HasFrame && !state.Clock.Ended {
		s.loadDanmaku(v, state)
	}
	if state.Playing || state.Loading || state.Switching || s.subtitle != "" {
		return 160 * time.Millisecond
	}
	return 500 * time.Millisecond
}

func (s *playbackContent) loadDanmaku(v *View, state app.PlaybackState) {
	segSeconds := state.Resolution.Plan.DanmakuSegmentSeconds
	segment := 1
	if segSeconds > 0 {
		segment = int(state.Position/float64(segSeconds)) + 1
	}
	if s.danmakuSegment == segment && (s.danmakuQuery != nil || s.danmakuStatus == "" || time.Now().Before(s.danmakuRetry)) {
		return
	}
	if s.danmakuQuery != nil {
		s.danmakuQuery()
	}
	previousSegment := s.danmakuSegment
	s.danmakuSegment, s.danmakuStatus = segment, ""
	query := provider.DanmakuQuery{PlaybackTarget: state.Target, ItemID: state.Resolution.Plan.ItemID, Segment: segment}
	s.danmakuQuery = v.Catalog.ReadPlaybackDanmaku(query, func(items []provider.Danmaku, err error) {
		s.danmakuQuery = nil
		if err != nil {
			s.danmakuStatus, s.danmakuRetry = provider.CodeOf(err), time.Now().Add(20*time.Second)
			return
		}
		if segSeconds > 0 && segment == previousSegment+1 && len(s.danmaku) > 0 {
			boundary := float64((segment - 1) * segSeconds)
			cutoff := boundary - danmakuMovingTime
			var tail []provider.Danmaku
			for _, old := range s.danmaku {
				if old.Seconds >= cutoff && old.Seconds < boundary {
					tail = append(tail, old)
				}
			}
			if len(tail) > 0 {
				items = append(tail, items...)
			}
		}
		slices.SortStableFunc(items, func(a, b provider.Danmaku) int {
			switch {
			case a.Seconds < b.Seconds:
				return -1
			case a.Seconds > b.Seconds:
				return 1
			default:
				return 0
			}
		})
		s.danmaku = items
		s.danmakuGen++
	})
}

func (s *playbackContent) setSubtitle(v *View, track provider.PlaybackTrack) {
	if s.subtitleQuery != nil {
		s.subtitleQuery()
	}
	s.subtitle, s.cues, s.subtitleQuery = track.ID, nil, nil
	if track.ID == "" {
		return
	}
	query := provider.PlaybackSubtitleQuery{PlaybackTarget: v.Playback.State().Target, Track: track}
	s.subtitleQuery = v.Catalog.ReadPlaybackSubtitles(query, func(cues []provider.SubtitleCue, err error) {
		s.subtitleQuery = nil
		if err != nil {
			s.subtitle = ""
			s.notice(v.Assets.Text.Playback.SubtitleFailed)
			return
		}
		s.cues = cues
	})
}

func playbackThumbnailTile(sheet *provider.PlaybackThumbnails, seconds float64) (int, string) {
	if sheet == nil || len(sheet.Times) == 0 || len(sheet.Images) == 0 {
		return 0, ""
	}
	index := max(0, sort.Search(len(sheet.Times), func(i int) bool { return sheet.Times[i] > seconds })-1)
	perImage := sheet.Columns * sheet.Rows
	if perImage == 0 || index/perImage >= len(sheet.Images) {
		return 0, ""
	}
	return index, sheet.Images[index/perImage]
}

func (s *playbackContent) thumbnail(v *View, seconds float64) ui.ImageSource {
	sheet := v.Playback.State().Resolution.Plan.Thumbnails
	index, location := playbackThumbnailTile(sheet, seconds)
	if location == "" {
		return nil
	}
	art := v.Catalog.Artwork(location)
	if art == nil || art.Image == nil {
		return nil
	}
	if index == s.thumbIndex && art == s.thumbScene {
		return s.thumbBitmap
	}
	tile := index % (sheet.Columns * sheet.Rows)
	x, y := tile%sheet.Columns*sheet.Width, tile/sheet.Columns*sheet.Height
	area := image.Rect(x, y, x+sheet.Width, y+sheet.Height).Add(art.Image.Bounds().Min)
	if !area.In(art.Image.Bounds()) {
		return nil
	}
	result := image.NewRGBA(image.Rect(0, 0, sheet.Width, sheet.Height))
	draw.Draw(result, result.Bounds(), art.Image, area.Min, draw.Src)
	s.thumbIndex, s.thumbScene, s.thumbBitmap = index, art, ui.NewBitmap(result)
	return s.thumbBitmap
}

func (p *PageContext) playbackTextLayers(c *ui.Context, state app.PlaybackState) {
	v, s, colors := p.host, &p.host.player, p.Theme.Colors
	if state.Clock.HasFrame && !state.Clock.Ended && state.Failure == "" {
		if v.playbackSettings.show {
			p.drawDanmaku(c, state)
		}
		for _, cue := range s.content.cues {
			if cue.Start > state.Position {
				break
			}
			if cue.End <= state.Position {
				continue
			}
			bottom := float32(73)
			if s.visible {
				bottom = 212
			}
			ui.Column(c).Absolute().Left(p.Width * 0.17).Right(p.Width * 0.17).Bottom(bottom).AlignItems(ui.Center).Children(func() {
				ui.Text(c, cue.Text).Padding(6, 12).Radius(5).FontSize(30).FontWeight(550).
					TextColor(paint(colors.Playback.Subtitle)).Background(withAlpha(colors.Playback.SubtitleOutline, 0xc0))
			})
			break
		}
	}
	message, expiry := s.content.message, s.content.messageUntil
	if s.discussion.feedback.text != "" {
		message, expiry = s.discussion.feedback.text, c.Now().Add(2500*time.Millisecond)
		s.content.message, s.content.messageUntil = message, expiry
		s.discussion.feedback = videoDetailFeedback{}
	}
	p.playbackToast(c, message, expiry)
}

func estimateDanmakuWidth(text string) float32 {
	var width float32
	for _, r := range text {
		switch {
		case r > 0x7f || unicode.Is(unicode.Han, r):
			width += 30
		case r >= 'A' && r <= 'Z':
			width += 20
		case strings.ContainsRune("iIl1!.,;:'| ", r):
			width += 11
		default:
			width += 17
		}
	}
	return max(36, width+28)
}

func danmakuItemKey(item provider.Danmaku, ordinal int) uint64 {
	h := fnv.New64a()
	ms := int64(math.Round(item.Seconds * 1000))
	var buf [16]byte
	for i := range 8 {
		buf[i] = byte(ms >> (i * 8))
		buf[8+i] = byte(ordinal >> (i * 8))
	}
	_, _ = h.Write(buf[:])
	_, _ = h.Write([]byte(item.Mode))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(item.Text))
	return h.Sum64()
}

type scrollLaneState struct {
	occupied bool
	start    float64
	width    float64
	speed    float64
}

func scheduleDanmaku(items []provider.Danmaku, density int, viewportWidth float32) []scheduledDanmaku {
	if len(items) == 0 || density <= 0 || viewportWidth <= 0 {
		return nil
	}
	clamped := min(max(density, 10), 100)
	scrollCount := max(2, min(danmakuMaxScrollLanes, (clamped*danmakuMaxScrollLanes+99)/100))
	staticCount := max(1, min(danmakuMaxStaticLanes, (clamped*danmakuMaxStaticLanes+99)/100))
	gap := float64(80 + (100-clamped)*4)
	viewW := float64(viewportWidth)

	scrollLanes := make([]scrollLaneState, scrollCount)
	topLanes := make([]float64, staticCount)
	bottomLanes := make([]float64, staticCount)
	for i := range topLanes {
		topLanes[i] = -1
		bottomLanes[i] = -1
	}

	seen := make(map[string]int, len(items))
	result := make([]scheduledDanmaku, 0, len(items))
	for _, item := range items {
		t := item.Seconds
		sig := fmt.Sprintf("%d:%s:%s", int64(math.Round(t*1000)), item.Mode, item.Text)
		ordinal := seen[sig]
		seen[sig] = ordinal + 1
		key := danmakuItemKey(item, ordinal)

		switch item.Mode {
		case provider.DanmakuTop:
			lane := -1
			for l, end := range topLanes {
				if t >= end {
					lane = l
					break
				}
			}
			if lane < 0 {
				continue
			}
			topLanes[lane] = t + danmakuStaticTime
			result = append(result, scheduledDanmaku{
				key: key, text: item.Text, mode: item.Mode,
				start: t, end: t + danmakuStaticTime, lane: lane,
			})
		case provider.DanmakuBottom:
			lane := -1
			for l, end := range bottomLanes {
				if t >= end {
					lane = l
					break
				}
			}
			if lane < 0 {
				continue
			}
			bottomLanes[lane] = t + danmakuStaticTime
			result = append(result, scheduledDanmaku{
				key: key, text: item.Text, mode: item.Mode,
				start: t, end: t + danmakuStaticTime, lane: lane,
			})
		default:
			w := float64(estimateDanmakuWidth(item.Text))
			speed := (viewW + w) / danmakuMovingTime
			lane := -1
			for l, prev := range scrollLanes {
				if !prev.occupied {
					lane = l
					break
				}
				tailClear := prev.start + (prev.width+gap)/prev.speed
				noCatchUp := prev.start + danmakuMovingTime - max(0.0, viewW-gap)/speed
				if t >= tailClear && t >= noCatchUp {
					lane = l
					break
				}
			}
			if lane < 0 {
				continue
			}
			scrollLanes[lane] = scrollLaneState{occupied: true, start: t, width: w, speed: speed}
			result = append(result, scheduledDanmaku{
				key: key, text: item.Text, mode: provider.DanmakuScroll,
				start: t, end: t + danmakuMovingTime, lane: lane, width: float32(w),
			})
		}
	}
	return result
}

func (p *PageContext) drawDanmaku(c *ui.Context, state app.PlaybackState) {
	settings := p.host.playbackSettings
	content := &p.host.player.content
	if len(content.danmaku) == 0 || settings.density == 0 {
		return
	}
	rate := float64(1)
	if p.host.Playback != nil {
		rate = p.host.Playback.Rate()
	}
	position := content.danmakuPosition(c.Now(), state, rate)
	schedule := content.ensureDanmakuSchedule(settings.density, p.Width)
	if len(schedule) == 0 {
		return
	}
	first := sort.Search(len(schedule), func(i int) bool { return schedule[i].start > position-danmakuMovingTime })
	ink := paint(p.Theme.Colors.Playback.Foreground)
	shadow := paint(p.Theme.Colors.Playback.SubtitleOutline)
	opacity := float32(settings.opacity) / 100
	for i := first; i < len(schedule); i++ {
		item := schedule[i]
		if item.start > position {
			break
		}
		if position >= item.end {
			continue
		}
		text := ui.Text(c.Key(danmakuNodeKey{item.key}), item.text).Absolute().FontSize(28).FontWeight(640).
			Opacity(opacity).TextColor(ink).TextShadow(0, 1, 4, shadow).Cache()
		switch item.mode {
		case provider.DanmakuTop:
			text.Top(164 + float32(item.lane*40)).Left(p.Width * .28).Right(p.Width * .28).TextAlign(ui.Center)
		case provider.DanmakuBottom:
			text.Bottom(246 + float32(item.lane*40)).Left(p.Width * .28).Right(p.Width * .28).TextAlign(ui.Center)
		default:
			elapsed := position - item.start
			x := p.Width - (p.Width+item.width)*float32(elapsed/danmakuMovingTime)
			text.Left(0).Top(153 + float32(item.lane*41)).Width(item.width).MaxLines(1).Translate(x, 0)
		}
	}
}

func (p *PageContext) playbackDanmakuOptions(c *ui.Context) {
	v, text := p.host, p.Assets.Text.Playback
	colors := p.Theme.Colors
	options := &v.playbackSettings
	p.playbackOption(c, playbackOptionID(playerDanmaku, "enabled"), text.On, "", options.show, func() { options.show = true })
	p.playbackOption(c, playbackOptionID(playerDanmaku, "disabled"), text.Off, "", !options.show, func() { options.show = false })
	ui.Column(c).FillWidth().Margin(14, 0, 19, 0).Gap(13).Children(func() {
		p.playbackSlider(c, "density", text.Density, &options.density, 10)
		p.playbackSlider(c, "opacity", text.Opacity, &options.opacity, 5)
	})
	entry, _ := p.State.Current()
	if entry.Capabilities.Has(provider.WriteDanmaku) {
		p.styledButton(c, text.SendDanmaku, "comment", playbackOptionID(playerDanmaku, "send"), playerPanelGroup,
			p.beginDanmaku, buttonAppearance{
				height: 62, radius: 11, fontSize: 20, iconSize: 22, iconGap: 12,
				background: withAlpha(colors.Playback.Foreground, 0x06), foreground: paint(colors.Accent.Soft),
			}).FillWidth().Center()
	}
	if code := v.player.content.danmakuStatus; code != "" {
		ui.Text(c, p.authError(code)).Margin(8).FontSize(15).TextColor(paint(p.Theme.Colors.Playback.Secondary))
	}
}

func (p *PageContext) playbackSlider(c *ui.Context, key, label string, value *int, step int) {
	id := playbackOptionID(playerDanmaku, key)
	colors, d := p.Theme.Colors, p.uiColors()
	button := ui.Button(c.Key(key), "").Label(fmt.Sprintf("%s %d%%", label, *value)).FillWidth().
		Padding(0, 16, 16, 16).Radius(5).Background(ui.Transparent).Border(0, ui.Transparent)
	p.bindDirection(button, id, playerPanelGroup, func(direction app.Direction) bool {
		switch direction {
		case app.Left:
			*value = max(10, *value-step)
		case app.Right:
			*value = min(100, *value+step)
		default:
			return false
		}
		return true
	})
	button.Children(func() {
		ui.Column(c).FillWidth().Gap(16).Children(func() {
			ui.Row(c).FillWidth().Justify(ui.SpaceBetween).Children(func() {
				ui.Text(c, label).FontSize(17).TextColor(paint(colors.Playback.Secondary))
				ui.Text(c, fmt.Sprintf("%d%%", *value)).FontSize(15).TextColor(paint(colors.Playback.Secondary))
			})
			ui.Box(c).FillWidth().Height(16).Children(func() {
				const thumbRadius = 8
				fraction := float32(max(0, *value-10)) / 90 * 100
				if p.paintFocus() == id {
					ui.Box(c).Absolute().Left(-2).Right(-6).Top(-4).Height(24).Radius(5).
						Border(2, paint(colors.Focus.Ring))
				}
				ui.Box(c).Absolute().Left(2).Right(-2).Top(4).Height(9).Children(func() {
					ui.Box(c).Absolute().Left(0).Top(0).Fill().Radius(4.5).
						Background(paint(d.sliderTrack)).Border(2, paint(d.sliderEdge))
					ui.Box(c).Absolute().Left(thumbRadius).Right(thumbRadius).Top(0.5).Height(8).Children(func() {
						ui.Box(c).WidthPercent(fraction).Height(8).Children(func() {
							if fraction > 0 {
								ui.Box(c).Absolute().Left(-thumbRadius).Right(0).Top(0).Height(8).Radius(4).
									Background(paint(colors.Accent.Soft)).Border(1.5, paint(d.sliderFillEdge))
							}
							ui.Box(c).Absolute().Right(-thumbRadius).Top(-4).Size(2*thumbRadius, 2*thumbRadius).
								Radius(thumbRadius).Background(paint(colors.Accent.Soft))
						})
					})
				})
			})
		})
	})
}

type danmakuDraft struct {
	mode    provider.DanmakuMode
	body    string
	input   string
	edit    bool
	preview ui.ScrollState
	failure provider.ErrorCode
	cancel  context.CancelFunc
}

const (
	danmakuInput   app.FocusID = "player/danmaku-input"
	danmakuEdit    app.FocusID = "player/danmaku-edit"
	danmakuSend    app.FocusID = "player/danmaku-send"
	danmakuPreview app.FocusID = "player/danmaku-preview"
	danmakuRegion              = "player-danmaku-compose"
)

func (p *PageContext) beginDanmaku() {
	p.withPlayerAccount(func() {
		p.host.player.content.draft = &danmakuDraft{mode: provider.DanmakuScroll, edit: true}
		p.State.Focus, p.host.reveal = danmakuInput, true
	})
}

func (p *PageContext) drawDanmakuComposer(c *ui.Context, draft *danmakuDraft) {
	text, colors := p.Assets.Text.Playback, p.Theme.Colors
	hasDraft := strings.TrimSpace(draft.body) != ""
	p.region(danmakuRegion, focusEdges{})
	p.renderPlayerFrameless(c, "danmaku-compose", 820, func() {
		p.framelessHeader(c, "DANMAKU", "comment", text.SendDanmaku, "")
		if draft.edit {
			input := ui.TextInput(c.Key("danmaku-input"), &draft.input).FillWidth().Height(105).FontSize(28).
				Label(text.SendDanmaku).Placeholder(text.DanmakuPlaceholder).MaxLength(provider.DanmakuLimit).
				ReturnKey(ui.ReturnDone).OnTextKey(func(key ui.TextKey) bool {
				if key.Key != ui.KeyEnter && key.Key != ui.KeyEscape {
					return false
				}
				if key.Key == ui.KeyEnter {
					draft.body = key.Text()
					draft.preview.Y = 0
				}
				draft.edit = false
				p.State.Focus, p.host.reveal = danmakuEdit, true
				if strings.TrimSpace(draft.body) != "" && key.Key == ui.KeyEnter {
					p.State.Focus = danmakuSend
				}
				return true
			})
			if p.active {
				input.FocusBind(&p.State.Focus, danmakuInput).Focus(ui.FocusOptions{PreventScroll: true})
			}
		} else {
			body := draft.body
			if !hasDraft {
				body = text.DanmakuPlaceholder
			}
			preview := ui.Scroll(c.Key(string(danmakuPreview))).FillWidth().Height(105).
				TrackScroll(&draft.preview)
			preview.Children(func() {
				ui.Text(c, body).FillWidth().FontSize(28).LineHeight(1.8).TextColor(paint(colors.Text.Primary))
			})
			if hasDraft {
				p.bindDraftPreview(preview, danmakuPreview, danmakuRegion, &draft.preview, danmakuEdit, 52)
			}
		}
		ui.Row(c).Gap(12).Margin(28, 0, 0, 0).Children(func() {
			for _, option := range []struct {
				mode  provider.DanmakuMode
				label string
			}{
				{provider.DanmakuScroll, text.ScrollMode}, {provider.DanmakuTop, text.TopMode}, {provider.DanmakuBottom, text.BottomMode},
			} {
				id := app.FocusID("player/danmaku-mode/" + option.mode)
				style := p.framelessButtonAppearance()
				style.height, style.radius, style.padding, style.fontSize = 50, 13, 24, 19
				selected := draft.mode == option.mode
				if selected {
					style.background = withAlpha(colors.Accent.Primary, 0x22)
					style.foreground = paint(colors.Accent.Soft)
					style.border = withAlpha(colors.Accent.Soft, 0x58)
				}
				p.styledButton(c, option.label, "", id, danmakuRegion, func() { draft.mode = option.mode }, style).AccessSelected(selected)
			}
		})
		if draft.failure != "" {
			ui.Text(c, p.authError(draft.failure)).Margin(16, 0, 0, 0).FontSize(18).TextColor(paint(colors.Status.Error.Foreground))
		}
		ui.Row(c).Gap(16).Margin(36, 0, 0, 0).AlignSelf(ui.Start).Children(func() {
			p.styledButton(c, p.Assets.Text.Detail.EditComment, "", danmakuEdit, danmakuRegion, func() {
				draft.edit, draft.input = true, draft.body
				p.State.Focus, p.host.reveal = danmakuInput, true
			}, p.framelessButtonAppearance())
			p.styledButton(c, text.Send, "send", danmakuSend, danmakuRegion, func() { p.sendDanmaku(draft) }, p.framelessButtonAppearance()).
				Disabled(draft.cancel != nil || draft.edit || !hasDraft)
		})
	})
}

func (p *PageContext) sendDanmaku(draft *danmakuDraft) {
	if draft.cancel != nil || draft.edit || strings.TrimSpace(draft.body) == "" {
		return
	}
	v, s := p.host, &p.host.player
	state := v.Playback.State()
	input := provider.DanmakuPost{PlaybackProgress: provider.PlaybackProgress{
		PlaybackTarget: state.Target, ItemID: state.Resolution.Plan.ItemID, Owner: v.Playback.Owner(),
	}, Start: state.Position, Mode: draft.mode, Text: strings.TrimSpace(draft.body)}
	draft.cancel = v.Catalog.PostPlaybackDanmaku(input, func(err error) {
		draft.cancel = nil
		if err != nil {
			draft.failure = provider.CodeOf(err)
			return
		}
		s.content.draft = nil
		s.content.danmaku = append(s.content.danmaku, provider.Danmaku{Mode: input.Mode, Text: input.Text, Seconds: input.Start})
		slices.SortStableFunc(s.content.danmaku, func(a, b provider.Danmaku) int {
			if a.Seconds < b.Seconds {
				return -1
			}
			if a.Seconds > b.Seconds {
				return 1
			}
			return 0
		})
		s.content.danmakuGen++
		s.content.notice(p.Assets.Text.Playback.DanmakuSent)
		p.State.Focus, v.reveal = playbackOptionID(playerDanmaku, "send"), true
	})
}
