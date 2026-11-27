package tvui

import (
	"context"
	"fmt"
	"log"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/provider"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

const (
	videoDetailPlay      app.FocusID = "video-detail/play"
	videoDetailLater     app.FocusID = "video-detail/later"
	videoDetailTrack     app.FocusID = "video-detail/track"
	videoDetailCreatorID app.FocusID = "video-detail/creator"
	videoDetailFollow    app.FocusID = "video-detail/follow"
	videoDetailLike      app.FocusID = "video-detail/like"
	videoDetailFavorite  app.FocusID = "video-detail/favorite"
	videoDetailRetry     app.FocusID = "video-detail/retry"
	videoDetailControls              = "video-detail"
	videoDetailArtHeight             = 770
	videoDetailInset                 = 110
	videoDetailTabsTop               = 669
	videoDetailFont                  = "system-ui, PingFang SC"
	videoDetailMore      app.FocusID = "video-detail/description"

	videoDetailMetadataTop = 272
)

func videoDetailActionFocus(id string) app.FocusID {
	return app.FocusID("video-detail/action/" + id)
}

func videoDetailCreatorFocus(index int) app.FocusID {
	if index <= 0 {
		return videoDetailCreatorID
	}
	return app.FocusID(fmt.Sprintf("video-detail/creator/%d", index))
}

const videoDetailTextStroke = 0.018

var videoDetailNumbers = message.NewPrinter(language.English)

type videoDetailPage struct{}

type videoDetailEntrance struct {
	from  app.Location
	until time.Time
}

type videoDetailState struct {
	seed, item    provider.MediaItem
	entrance      videoDetailEntrance
	tab           videoDetailTab
	part          int
	expanded      bool
	previous      []videoDetailLocation
	account       string
	cancel        context.CancelFunc
	operation     context.CancelFunc
	loading       bool
	revealSection bool
	failure       provider.ErrorCode
	confirm       *provider.ActionSpec
	discussion    *videoDiscussionState
	creatorScroll *ui.ScrollState
	relatedScroll *ui.ScrollState
	segmentRows   *ui.ListState
	draft         videoCommentDraft
	feedback      videoDetailFeedback
	signInAction  func()
	afterLoad     func()
	synopsisReady bool
	synopsisBody  string
	synopsisWidth float32
	synopsisLang  string
	synopsis      videoDetailSynopsis
	queue         []playbackItem
	queueIndex    int
}

type videoDetailLocation struct {
	seed, item    provider.MediaItem
	tab           videoDetailTab
	part          int
	expanded      bool
	position      app.Position
	discussion    *videoDiscussionState
	creatorScroll *ui.ScrollState
	relatedScroll *ui.ScrollState
	segmentRows   *ui.ListState
	queue         []playbackItem
	queueIndex    int
}

func (s *videoDetailState) actionSpec(id string) (provider.ActionSpec, bool) {
	if s.item.VideoDetails == nil {
		return provider.ActionSpec{}, false
	}
	for _, action := range s.item.VideoDetails.Actions {
		if action.ID == id {
			return action, true
		}
	}
	return provider.ActionSpec{}, false
}

func (s *videoDetailState) close() {
	if s.cancel != nil {
		s.cancel()
	}
	s.cancelOperation()
	s.discussion.cancelLoad()
	*s = videoDetailState{}
}

func (p *PageContext) canOpenVideoDetail(ref provider.MediaRef) bool {
	entry, _ := p.State.Current()
	return ref.Kind.Valid() && entry.Capabilities.Has(provider.GetCatalogItem)
}

func (p *PageContext) openVideoDetail(item provider.MediaItem) {
	p.openVideoDetailInQueue(item, nil, 0)
}

func (p *PageContext) playbackQueueFromList(item provider.MediaItem, list []provider.MediaItem, index int) ([]playbackItem, int) {
	entry, ok := p.State.Current()
	if !ok || !entry.Capabilities.Has(provider.PlaybackModes) || len(list) <= 1 || item.Ref.Kind != provider.Video {
		return nil, -1
	}
	queue := make([]playbackItem, 0, len(list))
	clickedIdx := -1
	for i, cand := range list {
		if cand.Ref.Kind != provider.Video || !p.canPlay(cand.Ref) {
			continue
		}
		if i == index || (clickedIdx < 0 && cand.Ref == item.Ref) {
			clickedIdx = len(queue)
			queue = append(queue, playbackItem{item: item})
			continue
		}
		queue = append(queue, playbackItem{item: cand})
	}
	if clickedIdx < 0 || len(queue) <= 1 {
		return nil, -1
	}
	return queue, clickedIdx
}

func (p *PageContext) openMediaFromList(item provider.MediaItem, list []provider.MediaItem, index int) {
	if queue, clickedIdx := p.playbackQueueFromList(item, list, index); len(queue) > 1 {
		p.openVideoDetailInQueue(item, queue, clickedIdx)
		return
	}
	p.openVideoDetail(item)
}

func (p *PageContext) openVideoDetailInQueue(item provider.MediaItem, queue []playbackItem, queueIndex int) {
	if p.State.Page == app.VideoDetail {
		p.openRelatedVideoDetail(item, queue, queueIndex)
		return
	}
	layers := p.State.Layers()
	if !p.State.Push(app.Route{Page: app.VideoDetail}) {
		return
	}
	view := p.host
	if view.videoDetail.seed.Ref.ID != "" && view.State.RouteCount(app.VideoDetail) > 1 {
		saved := view.videoDetail
		if saved.cancel != nil {
			saved.cancel()
			saved.cancel = nil
		}
		saved.cancelOperation()
		saved.discussion.cancelLoad()
		saved.feedback = videoDetailFeedback{}
		view.videoDetailStack = append(view.videoDetailStack, saved)
		view.videoDetail = videoDetailState{}
	} else {
		view.videoDetail.close()
	}
	view.videoDetail.seed = item
	if len(queue) > 1 && queueIndex >= 0 && queueIndex < len(queue) {
		view.videoDetail.queue = queue
		view.videoDetail.queueIndex = queueIndex
	}
	if len(layers) == 1 {
		view.videoDetail.entrance.from = layers[0]
	}
	view.reveal = true
	view.loadVideoDetail()
}

func (v *View) closeAllVideoDetails() {
	v.videoDetail.close()
	for i := range v.videoDetailStack {
		v.videoDetailStack[i].close()
	}
	v.videoDetailStack = nil
}

func (v *View) leaveVideoDetail() {
	needed := v.State.RouteCount(app.VideoDetail)
	have := len(v.videoDetailStack)
	if v.videoDetail.seed.Ref.ID != "" {
		have++
	}
	for have > needed {
		v.videoDetail.close()
		if len(v.videoDetailStack) > 0 {
			last := len(v.videoDetailStack) - 1
			v.videoDetail = v.videoDetailStack[last]
			v.videoDetailStack[last] = videoDetailState{}
			v.videoDetailStack = v.videoDetailStack[:last]
		}
		have--
	}
}

func (v *View) renderPageEntrance(c *ui.Context, entrance *videoDetailEntrance, width, height float32) {
	if entrance == nil || entrance.from.Page == "" {
		return
	}
	if c.Preferences().ReduceMotion {
		*entrance = videoDetailEntrance{}
		return
	}
	now := c.Now()
	if entrance.until.IsZero() {
		entrance.until = now.Add(videoDetailTransition.Duration)
	}
	if remaining := entrance.until.Sub(now); remaining > 0 {
		v.renderRootPage(c, entrance.from, false, width, height)
		c.After(remaining)
		return
	}
	*entrance = videoDetailEntrance{}
}

func syncVideoDetail(v *View) {
	if v.videoDetail.account != v.Auth.Accounts().CurrentID {
		v.videoDetail.previous, v.videoDetail.afterLoad = nil, nil
		v.videoDetail.cancelOperation()
		v.videoDetail.discussion.cancelLoad()
		v.videoDetail.discussion = nil
		v.loadVideoDetail()
	}
}

func (p *PageContext) openRelatedVideoDetail(item provider.MediaItem, queue []playbackItem, queueIndex int) {
	view, scroll := p.host, p.scroll()
	detail := &view.videoDetail
	if item.Ref == detail.item.Ref {
		return
	}
	detail.feedback, detail.afterLoad = videoDetailFeedback{}, nil
	detail.cancelOperation()
	detail.discussion.cancelLoad()
	detail.previous = append(detail.previous, videoDetailLocation{
		seed: detail.seed, item: detail.item, tab: detail.tab, part: detail.part, expanded: detail.expanded, position: view.State.Position,
		discussion: detail.discussion, creatorScroll: detail.creatorScroll, relatedScroll: detail.relatedScroll, segmentRows: detail.segmentRows,
		queue: detail.queue, queueIndex: detail.queueIndex,
	})
	detail.seed, detail.tab, detail.part = item, videoDetailOverview, 0
	detail.expanded = false
	detail.discussion, detail.creatorScroll, detail.relatedScroll, detail.segmentRows = nil, nil, nil, nil
	if len(queue) > 1 && queueIndex >= 0 && queueIndex < len(queue) {
		detail.queue, detail.queueIndex = queue, queueIndex
	} else {
		detail.queue, detail.queueIndex = nil, 0
	}
	view.State.RecordPosition(app.Position{Focus: videoDetailPlay})
	scroll.ScrollTo(0, 0, ui.Animation{})
	view.reveal = true
	view.loadVideoDetail()
}

func (v *View) backFromVideoDetail() bool {
	detail := &v.videoDetail
	if detail.expanded {
		v.setVideoSynopsisExpanded(false)
		return true
	}
	if detail.tab == videoDetailComments && detail.discussion != nil && detail.discussion.root != nil {
		v.closeVideoThread()
		return true
	}
	if len(detail.previous) == 0 {
		return false
	}
	if detail.cancel != nil {
		detail.cancel()
	}
	detail.cancelOperation()
	detail.discussion.cancelLoad()
	detail.feedback, detail.afterLoad = videoDetailFeedback{}, nil
	last := len(detail.previous) - 1
	parent := detail.previous[last]
	detail.previous[last] = videoDetailLocation{}
	detail.previous = detail.previous[:last]
	detail.seed, detail.item, detail.tab = parent.seed, parent.item, parent.tab
	detail.expanded, detail.part = parent.expanded, parent.part
	detail.discussion, detail.creatorScroll, detail.relatedScroll, detail.segmentRows = parent.discussion, parent.creatorScroll, parent.relatedScroll, parent.segmentRows
	detail.queue, detail.queueIndex = parent.queue, parent.queueIndex
	detail.cancel, detail.loading, detail.failure = nil, false, ""
	v.State.RecordPosition(parent.position)
	if scroll := v.activeScroll; scroll != nil {
		scroll.ScrollTo(parent.position.X, parent.position.Y, ui.Animation{})
	}
	v.reveal = true
	return true
}

func (v *View) loadVideoDetail() {
	detail := &v.videoDetail
	if detail.cancel != nil {
		detail.cancel()
	}
	detail.account = v.Auth.Accounts().CurrentID
	detail.item, detail.loading, detail.failure = detail.seed, true, ""
	if v.State.Focus == videoDetailRetry {
		v.State.Focus = videoDetailOverview.focusID()
		v.reveal = true
	}
	detail.cancel = v.Catalog.ReadVideoDetail(detail.seed.Ref, func(item provider.MediaItem, err error) {
		detail.loading, detail.cancel = false, nil
		if err != nil {
			detail.failure, detail.afterLoad = provider.CodeOf(err), nil
			return
		}
		if item.CoverURL == "" {
			item.CoverURL = detail.seed.CoverURL
		}
		if item.BackdropURL == "" {
			item.BackdropURL = detail.seed.BackdropURL
		}
		if item.Category == "" {
			item.Category = detail.seed.Category
		}
		if item.Quality == "" {
			item.Quality = detail.seed.Quality
		}
		detail.item = item
		episodes := videoEpisodes(item.VideoDetails)
		if detail.part < 0 || detail.part >= len(episodes) || episodes[detail.part].Unavailable ||
			(detail.part == 0 && len(episodes) > 0 && episodes[0].Locked) {
			detail.part = firstPlayableEpisode(episodes)
		}
		if v.Trace && item.VideoDetails != nil {
			log.Printf("video detail episodes: kind=%s count=%d current=%d",
				item.VideoDetails.SegmentKind, len(videoEpisodes(item.VideoDetails)), detail.episodeIndex()+1)
		}
		if v.State.Page == app.VideoDetail {
			// The loaded title can move the focused control below the viewport.
			v.reveal = true
			if detail.tab == videoDetailComments {
				v.context(v.State.Location, true, 1920, 1080).ensureVideoDiscussionHot(detail)
			}
		}
		resume := detail.afterLoad
		detail.afterLoad = nil
		if resume != nil && v.State.Page == app.VideoDetail {
			v.dispatch(resume)
		}
	})
}

func (videoDetailPage) Render(c *ui.Context, p *PageContext) {
	detail := &p.host.videoDetail
	item := detail.item
	catalog := p.host.Catalog
	covers := []string{item.CoverURL}
	for _, creator := range item.DetailCreators() {
		if creator.AvatarURL != "" {
			covers = append(covers, creator.AvatarURL)
		}
	}
	if sections := item.VideoDetails; detail.tab == videoDetailRelated && sections != nil {
		covers = p.videoDetailRelatedArtwork(covers, sections.Related)
	}
	if detail.tab == videoDetailComments {
		covers = detail.videoDiscussion().artwork(covers, item.VideoDetails)
	}
	catalog.PrepareArtwork(covers)
	if item.BackdropURL != "" {
		catalog.PrepareBackdrops([]string{item.BackdropURL})
	}

	p.region(videoDetailControls, nil)
	entry, _ := p.State.Current()
	if p.active && p.State.Focus == videoDetailPlay && !entry.Capabilities.Has(provider.ResolvePlayback) {
		if p.canEditWatchLater(entry) {
			p.State.Focus = videoDetailLater
		} else {
			p.State.Focus = videoDetailOverview.focusID()
		}
		p.host.reveal = true
	}
	synopsis := videoDetailSynopsis{full: item.Subtitle}
	if detail.tab == videoDetailOverview && !detail.loading && detail.failure == "" && !detail.expanded {
		synopsis = detail.cachedSynopsisLocalized(item.Subtitle, videoSynopsisWidth(p.Width), p.langTag(), p.uiFont())
	}
	ui.Box(c.Key("video-detail")).FillWidth().Lang(p.langTag()).Font(p.uiFont()).Children(func() {
		p.videoDetailArtwork(c, item)
		ui.Column(c).FillWidth().Padding(0, videoDetailInset, 0, videoDetailInset).ZIndex(1).Children(func() {
			p.videoDetailSummary(c, detail)
			p.videoDetailTabs(c, detail, synopsis.truncated)
			ui.Column(c).FillWidth().Margin(58, 0, 0, 0).Children(func() {
				p.videoDetailSection(c, detail, synopsis)
			})
		})
	})
}

func (p *PageContext) videoDetailArtwork(c *ui.Context, item provider.MediaItem) {
	canvas := p.Theme.Colors.Surface.Canvas
	source := p.catalogBitmap(catalogImageKey{location: item.BackdropURL, backdrop: true})
	if source == nil {
		source = p.catalogCover(item.CoverURL)
	}
	ui.Box(c.Key("video-detail-art")).Absolute().Top(0).Left(0).Right(0).Height(videoDetailArtHeight).Clip().Children(func() {
		if source != nil {
			ui.Image(c, source).Fill().Fit(ui.Cover).ObjectPosition(0.5, 0.3)
		}
		ui.Box(c).Absolute().Top(0).Left(0).Fill().LinearGradient(ui.LinearGradient{
			Angle: 90, Stops: []ui.GradientStop{
				{Color: withAlpha(canvas, 0xf5)},
				{Color: withAlpha(canvas, 0x99), At: 0.44},
				{Color: withAlpha(canvas, 0x22), At: 1},
			},
		})
		ui.Box(c).Absolute().Top(0).Left(0).Fill().LinearGradient(ui.LinearGradient{
			Angle: 180, Stops: []ui.GradientStop{
				{Color: withAlpha(canvas, 0)},
				{Color: withAlpha(canvas, 0), At: 0.15},
				{Color: paint(canvas), At: 1},
			},
		})
	})
}

func (p *PageContext) videoDetailSummary(c *ui.Context, detail *videoDetailState) {
	const (
		kickerTop       = 115
		kickerHeight    = 23
		titleTop        = 153
		titleFontSize   = 68
		titleWeight     = 550
		titleLineHeight = 1.3
		titleHeight     = titleFontSize * titleLineHeight
	)
	item := detail.item
	firstParagraph, _, _ := strings.Cut(item.Title, "\n")
	_, firstLineHeight := c.MeasureText(0, ui.Span{
		Text: firstParagraph, Font: p.uiFont(), Size: titleFontSize, Weight: titleWeight, Lang: p.langTag(),
	})
	// Native fallback fonts can exceed the designed height of a single line.
	titleGap := max(0, videoDetailMetadataTop-titleTop-max(titleHeight, firstLineHeight))
	if episode := detail.episodeIndex(); episode >= 0 {
		if duration := videoEpisodes(item.VideoDetails)[episode].DurationSeconds; duration != nil {
			item.DurationSeconds = duration
		}
	}
	colors := p.Theme.Colors
	ui.Column(c.Key("video-detail-summary")).FillWidth().MaxWidth(900).Children(func() {
		ui.Row(c).Margin(kickerTop, 0, titleTop-kickerTop-kickerHeight, 0).Height(kickerHeight).Gap(15).
			AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(24, 1).Background(paint(colors.Accent.Soft))
			kicker := p.Assets.Text.Detail.Kicker
			switch item.Ref.Kind {
			case provider.Series:
				kicker = p.Assets.Text.Detail.SeriesKicker
			case provider.Course:
				kicker = p.Assets.Text.Detail.CourseKicker
			}
			if item.Category != "" {
				kicker = item.Category + " / " + kicker
			}
			ui.Text(c, kicker).FontSize(16).FontWeight(500).
				LetterSpacing(5).TextColor(paint(colors.Text.Secondary))
		})
		ui.Text(c, item.Title).FillWidth().MinHeight(titleHeight).Margin(0, 0, titleGap, 0).
			FontSize(titleFontSize).FontWeight(titleWeight).LetterSpacing(2).LineHeight(titleLineHeight).
			Role(ui.RoleHeading).Level(1)
		ui.Box(c).FillWidth().Height(videoDetailTabsTop - videoDetailMetadataTop).Children(func() {
			ui.Box(c).Absolute().Top(0).Left(0).Height(26).Center().Children(func() {
				p.catalogHeroMetadata(c, item)
			})
			if creators := item.DetailCreators(); len(creators) > 0 {
				p.videoDetailCreators(c, detail, creators)
			}
			if item.Subtitle != "" {
				ui.Text(c, item.Subtitle).Absolute().Top(392 - videoDetailMetadataTop).Left(0).Width(min(690, p.Width-2*videoDetailInset)).
					FontSize(21).LineHeight(1.8).MaxLines(2).TextColor(paint(colors.Text.Secondary))
			}
			p.videoDetailActions(c, item)
			p.videoDetailStats(c, item)
		})
	})
}

func (p *PageContext) videoDetailCreators(c *ui.Context, detail *videoDetailState, creators []provider.Creator) {
	text, colors, d := p.Assets.Text.Detail, p.Theme.Colors, p.uiColors()
	entry, _ := p.State.Current()
	item := detail.item
	if detail.creatorScroll == nil {
		detail.creatorScroll = &ui.ScrollState{}
	}

	primary := item.PrimaryCreator()
	showFollow := entry.Capabilities.Has(provider.FollowCreator) && primary != nil
	followIndex := -1
	if showFollow {
		for i := range creators {
			if creators[i].ID == primary.ID {
				followIndex = i
				break
			}
		}
	}

	hasVisibleNames := false
	rowIDs := make([]app.FocusID, 0, len(creators)+1)
	for i := range creators {
		if !creators[i].HideName && strings.TrimSpace(creators[i].Name) != "" {
			hasVisibleNames = true
		}
		if p.canOpenCreator(&creators[i]) {
			rowIDs = append(rowIDs, videoDetailCreatorFocus(i))
		}
		if i == followIndex {
			rowIDs = append(rowIDs, videoDetailFollow)
		}
	}
	if showFollow && followIndex < 0 {
		rowIDs = append(rowIDs, videoDetailFollow)
	}

	rowDirection := func(id app.FocusID) func(app.Direction) bool {
		pos := slices.Index(rowIDs, id)
		return func(direction app.Direction) bool {
			if direction != app.Left && direction != app.Right {
				return false
			}
			next := pos + 1
			if direction == app.Left {
				next = pos - 1
			}
			if next >= 0 && next < len(rowIDs) {
				p.host.moveFocus(rowIDs[next], direction, true)
			}
			return true
		}
	}

	renderFollow := func(followed bool) {
		follow := text.Follow
		if followed {
			follow = text.Followed
		}
		followStyle := buttonAppearance{
			height: 34, padding: 15, radius: 20, fontSize: 16, focusShadow: true,
			background: ui.Transparent, foreground: paint(colors.Text.Secondary), border: d.strongBorder,
			direction: rowDirection(videoDetailFollow),
		}
		p.styledButton(c, follow, "", videoDetailFollow, videoDetailControls, func() {
			p.runVideoDetailAction(videoDetailFollow, follow)
		}, followStyle).Shrink(0).AlignSelf(ui.Center)
	}

	rowGap := float32(14)
	if hasVisibleNames {
		rowGap = 18
	}
	rowWidth := min(float32(908), p.Width-2*videoDetailInset+8)
	ui.Box(c.Key("video-detail-creators")).Absolute().Top(314-videoDetailMetadataTop).Left(-4).Width(rowWidth).Height(58).Children(func() {
		ui.ScrollHorizontal(c.Key("video-detail-creators-scroll")).Fill().TrackScroll(detail.creatorScroll).Children(func() {
			ui.Row(c).Height(58).Padding(4, 4, 4, 4).Gap(rowGap).AlignItems(ui.Center).Children(func() {
				for i := range creators {
					creator := creators[i]
					id := videoDetailCreatorFocus(i)
					canOpen := p.canOpenCreator(&creator)
					focused := canOpen && p.paintFocus() == id
					showName := !creator.HideName && strings.TrimSpace(creator.Name) != ""

					drawPortrait := func() {
						ui.Box(c).Size(42, 42).Radius(21).Clip().Center().Background(paint(colors.Surface.Raised)).Children(func() {
							if portrait := p.catalogCover(creator.AvatarURL); portrait != nil {
								ui.Image(c, portrait).Fill().Fit(ui.Cover).ObjectPosition(0.5, 0.25)
							} else if r, _ := utf8.DecodeRuneInString(strings.TrimSpace(creator.Name)); r != utf8.RuneError {
								ui.Text(c, string(r)).FontSize(18).FontWeight(500).TextColor(paint(colors.Text.Primary))
							} else {
								ui.Icon(c, p.Assets.Icon("user")).Size(20, 20).TextColor(paint(colors.Text.Secondary))
							}
						})
					}

					ui.Row(c.Key(fmt.Sprintf("video-detail-creator-slot/%d/%s", i, creator.ID))).Gap(10).Shrink(0).AlignItems(ui.Center).Children(func() {
						if canOpen {
							selected := creator
							label := strings.TrimSpace(creator.Name)
							if label == "" {
								label = p.Assets.Text.Creator.Eyebrow
							}
							ringWidth, ringColor := float32(2), withAlpha(colors.Text.Primary, 0x1a)
							if focused {
								ringWidth, ringColor = 3, paint(colors.Focus.Ring)
							}
							button := ui.Button(c.Key(string(id)), "").Label(label).Size(50, 50).Padding(0, 0).
								Radius(25).Background(ui.Transparent).Center().Shrink(0).
								OnClick(func() { p.dispatch(func() { p.openCreator(selected) }) })
							button.Border(ringWidth, button.AnimateColor("focus-ring", ringColor, ui.Animation{Duration: focusDuration, NonMotion: true}))
							p.addVideoFocusShadow(button, focused)
							p.bindDirection(button, id, videoDetailControls, rowDirection(id))
							button.Children(drawPortrait)
						} else {
							drawPortrait()
						}
						if showName {
							nameColor := paint(colors.Text.Secondary)
							if focused {
								nameColor = paint(colors.Text.Primary)
							}
							ui.Text(c, creator.Name).FontSize(19).FontWeight(500).SingleLine().MaxWidth(220).TextColor(nameColor)
						}
					})

					if i == followIndex {
						renderFollow(primary.Following || creator.Following)
					}
				}
				if showFollow && followIndex < 0 {
					renderFollow(primary.Following)
				}
			})
		})
	})
}

func (p *PageContext) videoDetailActions(c *ui.Context, item provider.MediaItem) {
	text := p.Assets.Text
	entry, _ := p.State.Current()
	play, icon := text.Home.Play, "play-filled"
	if detail := item.VideoDetails; detail != nil {
		switch detail.Access {
		case provider.VideoAccessPreview:
			play, icon = p.sourceDetailPreviewAction(), "lock"
		case provider.VideoAccessRestricted:
			play, icon = p.sourceDetailAccessAction(), "lock"
		}
	}
	ui.Row(c).Absolute().Top(492 - videoDetailMetadataTop).Left(0).Gap(16).Children(func() {
		if entry.Capabilities.Has(provider.ResolvePlayback) {
			style := p.playButtonAppearance()
			style.focusShadow = true
			style.direction = func(direction app.Direction) bool {
				if direction == app.Up && p.host.focus.item(videoDetailCreatorID) != nil {
					p.host.moveFocus(videoDetailCreatorID, app.Up, true)
					return true
				}
				return false
			}
			p.styledButton(c, play, icon, videoDetailPlay, videoDetailControls, func() {
				if p.canPlay(item.Ref) {
					p.startDetailPlayback(item, nil)
				}
			}, style).Width(179).Center()
		}
		if entry.Capabilities.Has(provider.EditVideo) {
			if p.canEditWatchLater(entry) {
				laterIcon := "detail/plus"
				if item.VideoDetails != nil && item.VideoDetails.Engagement.WatchLater {
					laterIcon = "detail/check"
				}
				p.videoDetailButton(c, text.Home.Later, laterIcon, videoDetailLater, 185)
			}
			if item.VideoDetails != nil {
				for _, action := range item.VideoDetails.Actions {
					if action.Slot == provider.ActionSlotHero {
						p.videoDetailActionButton(c, action, 185)
					}
				}
			}
		}
		if item.Ref.Kind == provider.Series && entry.Capabilities.Has(provider.TrackSeries) {
			tracked := item.VideoDetails != nil && item.VideoDetails.Engagement.Tracked
			trackIcon := "detail/plus"
			if tracked {
				trackIcon = "detail/check"
			}
			d := p.uiColors()
			p.styledButton(c, p.sourceSeriesTrackLabel(tracked), trackIcon, videoDetailTrack, videoDetailControls, func() {
				p.toggleSeriesTrack(provider.SeriesEntry{Item: item, Tracked: tracked})
			}, buttonAppearance{
				height: 62, minWidth: 165, padding: 24, radius: 12, fontSize: 22, fontWeight: 550, iconSize: 28, iconGap: 13, focusShadow: true,
				background: d.secondaryFill, foreground: d.secondaryText, border: d.secondaryBorder,
			}).Center()
		}
	})
}

func (p *PageContext) videoDetailButton(c *ui.Context, label, icon string, id app.FocusID, width float32) {
	d := p.uiColors()
	foreground := d.secondaryText
	if info := p.host.videoDetail.item.VideoDetails; info != nil {
		state := info.Engagement
		switch {
		case id == videoDetailLike && state.Liked:
			icon = "like-filled"
		case id == videoDetailFavorite && state.Favorite:
			icon = "star-filled"
		}
	}
	p.styledButton(c, label, icon, id, videoDetailControls, func() { p.runVideoDetailAction(id, label) }, buttonAppearance{
		height: 62, radius: 12, fontSize: 22, fontWeight: 550, iconSize: 28, iconGap: 13, focusShadow: true,
		background: d.secondaryFill, foreground: foreground, border: d.secondaryBorder,
	}).Width(width).Center()
}

func (p *PageContext) videoDetailActionButton(c *ui.Context, action provider.ActionSpec, width float32) {
	d := p.uiColors()
	icon := action.Icon
	if action.Active && action.ActiveIcon != "" {
		icon = action.ActiveIcon
	}
	id := videoDetailActionFocus(action.ID)
	p.styledButton(c, action.Label, icon, id, videoDetailControls, func() { p.runVideoCustomAction(action) }, buttonAppearance{
		height: 62, minWidth: width, padding: 20, radius: 12, fontSize: 22, fontWeight: 550, iconSize: 28, iconGap: 13, focusShadow: true,
		background: d.secondaryFill, foreground: d.secondaryText, border: d.secondaryBorder,
	}).Center()
}

func (p *PageContext) videoDetailStats(c *ui.Context, item provider.MediaItem) {
	text, colors := p.Assets.Text, p.Theme.Colors
	ui.Row(c).Absolute().Left(0).Top(577 - videoDetailMetadataTop).Height(26).Gap(25).AlignItems(ui.Center).Children(func() {
		if item.Views != nil {
			ui.Text(c, catalogViews(*item.Views, text.Home)+text.Home.Plays).FontSize(17).TextColor(paint(colors.Text.Muted))
		}
		if info := item.VideoDetails; info != nil {
			if info.Comments != nil {
				ui.Text(c, videoDetailNumbers.Sprintf("%d", *info.Comments)+text.Detail.CommentCount).
					FontSize(17).TextColor(paint(colors.Text.Muted))
			}
			if info.PublishedAt > 0 {
				ui.Text(c, time.Unix(info.PublishedAt, 0).Local().Format("2006.01.02")).FontSize(17).TextColor(paint(colors.Text.Muted))
			}
		}
	})
}

func (p *PageContext) addVideoFocusShadow(element ui.Element, focused bool) {
	if !p.Theme.Dark {
		return
	}
	color := ui.Transparent
	if focused {
		color = withAlpha(p.Theme.Colors.Overlay.Shadow, 0x55)
	}
	ink := element.AnimateColor("video-focus-shadow", color, ui.Animation{Duration: 250 * time.Millisecond})
	element.Shadow(0, 8, 36, 0, ink)
}

func (p *PageContext) videoDetailFailure(code provider.ErrorCode) string {
	text := p.Assets.Text
	switch code {
	case provider.NeedsSignIn:
		return text.Detail.SignIn
	case provider.Denied:
		return text.Detail.Denied
	case provider.Expired:
		return text.Detail.Unavailable
	case provider.RateLimited:
		return text.Home.RateLimited
	default:
		return p.sourceDetailLoadFailed()
	}
}
