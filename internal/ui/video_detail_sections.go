package tvui

import (
	"fmt"
	"log"
	"slices"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/provider"
)

type videoDetailTab uint8

const (
	videoDetailOverview videoDetailTab = iota
	videoDetailEpisodes
	videoDetailComments
	videoDetailRelated
)

var videoDetailTabFocus = [...]app.FocusID{
	"video-detail/overview", "video-detail/episodes", "video-detail/comments", "video-detail/related",
}

const (
	videoDetailSegmentsRegion = "video-detail/segments"
	videoDetailRelatedRegion  = "video-detail/related-cards"
	videoDetailRelatedShelf   = "video-detail/related"
	videoDetailPanelTabTop    = videoDetailTabsTop - 272
	videoDetailCommentTabTop  = 125
)

func (tab videoDetailTab) focusID() app.FocusID { return videoDetailTabFocus[tab] }

func (p *PageContext) visibleVideoDetailTabs(detail *videoDetailState) []videoDetailTab {
	info := detail.item.VideoDetails
	tabs := []videoDetailTab{videoDetailOverview}
	if info == nil || len(videoEpisodes(info)) > 0 {
		tabs = append(tabs, videoDetailEpisodes)
	}
	if p.canReadVideoDiscussion() || (info != nil && len(info.Discussion) > 0) {
		tabs = append(tabs, videoDetailComments)
	}
	if info == nil || len(info.Related) > 0 {
		tabs = append(tabs, videoDetailRelated)
	}
	return tabs
}

func (p *PageContext) videoDetailTabs(c *ui.Context, detail *videoDetailState, hasMore bool) {
	text, colors := p.Assets.Text.Detail, p.Theme.Colors
	allLabels := [...]string{text.Overview, text.Episodes, text.Comments, text.Related}
	allWidths := [...]float32{98, 98, 98, 148}
	tabs := p.visibleVideoDetailTabs(detail)
	if !slices.Contains(tabs, detail.tab) {
		detail.tab = videoDetailOverview
	}
	ui.Row(c.Key("video-detail-tabs")).FillWidth().Gap(18).Children(func() {
		switching := p.active && detail.revealSection
		if switching {
			detail.revealSection = false
			// The selected panel determines the scroll range after layout.
			top := float32(videoDetailPanelTabTop)
			if detail.tab == videoDetailComments {
				top = videoDetailCommentTabTop
			}
			ui.Box(c).Absolute().Left(0).Top(-top).Size(1, 1).
				ScrollIntoView(ui.ScrollIntoViewOptions{Block: ui.ScrollStart, Animation: &focusScroll})
		}
		for pos, tab := range tabs {
			label := allLabels[tab]
			id := tab.focusID()
			background, ink := ui.Transparent, paint(colors.Text.Muted)
			selected := detail.tab == tab
			focused := p.State.Focus == id
			if selected {
				background, ink = withAlpha(colors.Text.Primary, 0x12), paint(colors.Text.Primary)
			}
			if focused {
				background, ink = withAlpha(colors.Text.Primary, 0x13), paint(colors.Text.Primary)
			}
			e := ui.Button(c.Key(string(id)), "").Label(label).Size(allWidths[tab], 62).Padding(0, 0).
				Radius(30).Border(0, ui.Transparent).Center().Role(ui.RoleTab).Selected(selected).OnClick(func() {
				p.dispatch(func() { p.selectVideoDetailTab(tab, app.Down) })
			})
			ink = p.animateFocus(e, background, ink)
			p.bindDirection(e, id, videoDetailControls, func(direction app.Direction) bool {
				switch direction {
				case app.Left, app.Right:
					next := pos + 1
					if direction == app.Left {
						next = pos - 1
					}
					if next >= 0 && next < len(tabs) {
						p.selectVideoDetailTab(tabs[next], direction)
					}
					return true
				case app.Down:
				default:
					return false
				}
				if detail.tab != videoDetailOverview {
					id := videoDetailSectionFocus(detail.tab, detail.item.VideoDetails)
					if detail.tab == videoDetailComments {
						id = detail.videoDiscussion().firstFocus(p.canReadVideoDiscussion(), detail.item.VideoDetails)
					}
					if id != "" && p.host.focus.item(id) != nil {
						p.host.moveFocus(id, direction, true)
						return true
					}
					return false
				}
				if hasMore {
					p.host.moveFocus(videoDetailMore, app.Down, true)
					return true
				}
				scroll := p.scroll()
				if scroll.MaxY-scroll.Y <= p.Height/2 {
					return false
				}
				scroll.ScrollTo(scroll.X, scroll.Y+p.Height/2, focusScroll)
				return true
			})
			if switching && focused {
				e.Focus(ui.FocusOptions{PreventScroll: true})
			}
			e.Children(func() {
				// Match the browser's stroke coverage at TV pixel density.
				stroke := float32(24 * videoDetailTextStroke)
				if selected || focused {
					stroke = 24 * 0.023
				}
				ui.Text(c, label).FontSize(24).FontWeight(400).LetterSpacing(1).Translate(0, -1).
					TextColor(ink).TextStroke(stroke, ink)
				p.tabUnderline(c, selected, focused, 3)
			})
		}
	})
}

func (p *PageContext) selectVideoDetailTab(tab videoDetailTab, direction app.Direction) {
	detail := &p.host.videoDetail
	if detail.tab == videoDetailComments && detail.discussion != nil {
		detail.discussion.clearThread()
	}
	detail.tab, detail.expanded, detail.revealSection = tab, false, true
	p.host.moveFocus(tab.focusID(), direction, false)
}

func videoDetailSectionFocus(tab videoDetailTab, info *provider.VideoDetails) app.FocusID {
	if info == nil {
		return ""
	}
	switch tab {
	case videoDetailEpisodes:
		if len(videoEpisodes(info)) != 0 {
			return videoSegmentFocus(0)
		}
	case videoDetailRelated:
		if len(info.Related) != 0 {
			return catalogCardFocus(videoDetailRelatedShelf, info.Related[0].Ref)
		}
	}
	return ""
}

func (p *PageContext) videoDetailSection(c *ui.Context, detail *videoDetailState, synopsis videoDetailSynopsis) {
	text := p.Assets.Text
	if detail.loading {
		p.videoDetailText(c, text.Detail.Loading)
		return
	}
	if detail.failure != "" {
		p.videoDetailText(c, p.videoDetailFailure(detail.failure))
		switch detail.failure {
		case provider.Denied, provider.NeedsSignIn, provider.Expired, provider.Unsupported:
		default:
			p.button(c, text.Retry, videoDetailRetry, videoDetailControls, p.host.loadVideoDetail).Margin(20, 0, 0, 0)
		}
		return
	}
	switch detail.tab {
	case videoDetailOverview:
		p.videoDetailAbout(c, detail, synopsis)
	case videoDetailEpisodes:
		p.videoDetailSegments(c, detail)
	case videoDetailComments:
		p.videoDetailDiscussion(c, detail)
	case videoDetailRelated:
		p.videoDetailRecommendations(c, detail.item.VideoDetails)
	}
}

func (p *PageContext) videoDetailText(c *ui.Context, value string) ui.Element {
	return ui.Text(c, value).FillWidth().FontSize(24).LineHeight(1.8).TextColor(paint(p.Theme.Colors.Text.Secondary))
}

func (p *PageContext) videoDetailAbout(c *ui.Context, detail *videoDetailState, synopsis videoDetailSynopsis) {
	item, text := detail.item, p.Assets.Text.Detail
	colors := p.Theme.Colors
	entry, _ := p.State.Current()
	width := videoSynopsisWidth(p.Width)
	ui.Row(c).FillWidth().Gap(80).AlignItems(ui.Start).Children(func() {
		ui.Column(c).Width(width).Shrink(0).Gap(30).Children(func() {
			p.videoSynopsis(c, synopsis, width, detail.expanded)
			if entry.Capabilities.Has(provider.EditVideo) {
				ui.Row(c).Gap(16).Children(func() {
					p.videoDetailButton(c, text.Like, "like", videoDetailLike, 141)
					if detail.item.VideoDetails != nil {
						for _, action := range detail.item.VideoDetails.Actions {
							if action.Slot == provider.ActionSlotOverview {
								p.videoDetailActionButton(c, action, 141)
							}
						}
					}
					p.videoDetailButton(c, text.Favorite, "detail/star", videoDetailFavorite, 141)
				})
			}
		})
		ui.Column(c).Width(width / 2).Children(func() {
			label := text.CurrentVideo
			segment := provider.VideoSegment{Title: item.Title, DurationSeconds: item.DurationSeconds}
			if index := detail.episodeIndex(); index >= 0 {
				label, segment = text.Collection, videoEpisodes(item.VideoDetails)[index]
			}
			ui.Text(c, label).FontSize(13).LetterSpacing(3).LineHeight(1.8).TextColor(paint(colors.Text.Muted))
			p.videoDetailText(c, segment.Title).MaxLines(1)
			if segment.DurationSeconds != nil {
				ui.Text(c, catalogDuration(*segment.DurationSeconds)).FontSize(17).LineHeight(2.4).
					TextColor(paint(colors.Text.Muted))
			}
		})
	})
}

func videoEpisodes(info *provider.VideoDetails) []provider.VideoSegment {
	if info != nil && (info.SegmentKind == provider.VideoParts || info.SegmentKind == provider.VideoCollection) {
		return info.Segments
	}
	return nil
}

func (d *videoDetailState) episodeIndex() int {
	info := d.item.VideoDetails
	episodes := videoEpisodes(info)
	if len(episodes) == 0 {
		return -1
	}
	if info.SegmentKind == provider.VideoParts {
		return min(d.part, len(episodes)-1)
	}
	return slices.IndexFunc(episodes, func(episode provider.VideoSegment) bool {
		return episode.Ref != nil && *episode.Ref == d.item.Ref
	})
}

func (v *View) videoDetailSectionOffset(scroll *ui.ScrollState) float32 {
	if tab := v.focus.item(videoDetailOverview.focusID()); tab != nil && tab.target.H != 0 {
		return max(0, tab.target.Y+scroll.Y-videoDetailTabsTop)
	}
	return 0
}

func (p *PageContext) videoDetailSegments(c *ui.Context, detail *videoDetailState) {
	text := p.Assets.Text.Detail
	episodes := videoEpisodes(detail.item.VideoDetails)
	if len(episodes) == 0 {
		p.videoDetailText(c, text.NoSegments)
		return
	}
	colors := p.Theme.Colors
	current := detail.episodeIndex()
	p.region(videoDetailSegmentsRegion, focusEdges{app.Up: {Focus: videoDetailEpisodes.focusID()}})
	ui.Row(c).FillWidth().AlignItems(ui.Baseline).Margin(25, 0).Children(func() {
		ui.Text(c, text.Collection).FontSize(24).TextColor(paint(colors.Text.Secondary))
		ui.Spacer(c).Grow(1)
		count := len(episodes)
		ui.Text(c, fmt.Sprintf("%d / %d", min(max(1, current+1), count), count)).FontSize(17).TextColor(paint(colors.Text.Muted))
	})
	const columns = 4
	width := (p.Width - 2*videoDetailInset - 48) / columns
	if detail.segmentRows == nil {
		detail.segmentRows = &ui.ListState{}
	}
	if id, ok := strings.CutPrefix(string(p.State.Focus), "video-detail/segment/"); ok && p.active && p.host.reveal {
		if index, err := strconv.Atoi(id); err == nil && index >= 0 && index < len(episodes) {
			detail.segmentRows.ScrollIntoView(index / columns)
		}
	}
	rows := (len(episodes) + columns - 1) / columns
	ui.List(c.Key("video-detail-segments"), detail.segmentRows, rows).FillWidth().MaxHeight(p.Height*0.55).
		Padding(0, 0, focusBottomMargin, 0).Rows(func(row ui.ListRow) {
		c := row.Context
		first := row.Index * columns
		ui.Row(c).FillWidth().Gap(16).Margin(0, 0, 16, 0).AlignItems(ui.Stretch).Children(func() {
			for index := first; index < min(first+columns, len(episodes)); index++ {
				p.videoEpisode(c, detail, episodes[index], index, current == index, width)
			}
		})
	})
}

func videoSegmentFocus(index int) app.FocusID {
	return app.FocusID(fmt.Sprintf("video-detail/segment/%d", index))
}

func (p *PageContext) videoEpisode(c *ui.Context, detail *videoDetailState, segment provider.VideoSegment, index int, selected bool, width float32) {
	colors, d := p.Theme.Colors, p.uiColors()
	id := videoSegmentFocus(index)
	focused := p.State.Focus == id
	background, foreground := d.subtleFill, paint(colors.Text.Secondary)
	number, icon := paint(colors.Text.Muted), paint(colors.Text.Muted)
	glyph := "detail/play"
	if selected {
		glyph, icon = "check", paint(colors.Accent.Soft)
	}
	if focused {
		background, foreground = paint(colors.Focus.Background), paint(colors.Focus.Foreground)
		number, icon = d.focusSubtext, d.focusSubtext
	}
	ordinal := fmt.Sprintf("%02d", index+1)
	e := ui.Button(c.Key(string(id)), "").Label(ordinal+" "+segment.Title).
		Width(width).MinHeight(77).Padding(22).Center().Radius(13).Border(1, d.subtleBorder).AccessSelected(selected).
		OnClick(func() {
			if detail.item.VideoDetails.SegmentKind == provider.VideoParts {
				p.dispatch(func() {
					detail.part = index
					if p.host.Trace {
						log.Printf("video detail part: selected=%d", index+1)
					}
				})
			} else if segment.Ref != nil && *segment.Ref != detail.item.Ref {
				p.dispatch(func() {
					p.openRelatedVideoDetail(provider.MediaItem{Ref: *segment.Ref, Title: segment.Title})
				})
			}
		})
	foreground = p.animateFocus(e, background, foreground)
	p.addVideoFocusShadow(e, focused)
	p.bind(e, id, videoDetailSegmentsRegion)
	e.Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(17).Children(func() {
			ui.Text(c, ordinal).FontSize(25).FontWeight(450).Shrink(0).TextColor(number)
			ui.Text(c, segment.Title).Grow(1).Shrink(1).FontSize(22).FontWeight(400).Margin(5, 0, 0, 0).
				TextColor(foreground)
			ui.Icon(c, p.Assets.Icon(glyph)).Size(21, 21).Shrink(0).TextColor(icon)
		})
	})
}

func (p *PageContext) videoDetailRelatedArtwork(covers []string, items []provider.MediaItem) []string {
	first := 0
	for index, item := range items {
		if catalogCardFocus(videoDetailRelatedShelf, item.Ref) == p.State.Focus {
			first = max(0, index-homeColumns+2)
			break
		}
	}
	for _, item := range items[first:min(first+homeColumns+1, len(items))] {
		covers = append(covers, item.CoverURL)
		if item.Creator != nil && item.Creator.AvatarURL != "" {
			covers = append(covers, item.Creator.AvatarURL)
		}
	}
	return covers
}

func (p *PageContext) videoDetailRecommendations(c *ui.Context, info *provider.VideoDetails) {
	text := p.Assets.Text.Detail
	if info == nil || len(info.Related) == 0 {
		p.videoDetailText(c, text.NoRelated)
		return
	}
	p.region(videoDetailRelatedRegion, focusEdges{app.Up: {Focus: videoDetailRelated.focusID()}})
	detail := &p.host.videoDetail
	if detail.relatedScroll == nil {
		detail.relatedScroll = &ui.ScrollState{}
	}
	p.catalogStrip(c, videoDetailRelatedShelf, info.Related, detail.relatedScroll)
}
