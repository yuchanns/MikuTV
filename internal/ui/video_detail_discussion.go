package tvui

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/provider"
)

const (
	videoDiscussionRegion                  = "video-detail/discussion"
	videoDiscussionHot         app.FocusID = "video-discussion/hot"
	videoDiscussionLatest      app.FocusID = "video-discussion/latest"
	videoDiscussionBack        app.FocusID = "video-discussion/back"
	videoDiscussionCompose     app.FocusID = "video-discussion/compose"
	videoDiscussionRetry       app.FocusID = "video-discussion/retry"
	videoDiscussionMoreReplies app.FocusID = "video-discussion/more-replies"
	videoDiscussionVisibleRows             = 10
)

type videoDiscussionState struct {
	latest, replies       []provider.VideoComment
	showLatest, hasLatest bool
	focusCommentOnLoad    bool
	root                  *provider.VideoComment
	activeReply           string
	listRows, threadRows  ui.ListState
	origin                app.FocusID
	cancel                context.CancelFunc
	failed                bool
	nextPage              int
	nextHot, nextLatest   int
	hotLoaded             bool
	failedPage            int
}

func (s *videoDetailState) videoDiscussion() *videoDiscussionState {
	if s.discussion == nil {
		s.discussion = &videoDiscussionState{}
	}
	return s.discussion
}

func (d *videoDiscussionState) cancelLoad() {
	if d != nil && d.cancel != nil {
		d.cancel()
		d.cancel = nil
	}
}

func (d *videoDiscussionState) clearThread() {
	if d.root != nil {
		d.cancelLoad()
		d.root, d.replies, d.activeReply = nil, nil, ""
		d.failed, d.nextPage = false, 0
		d.threadRows = ui.ListState{}
	}
}

func (d *videoDiscussionState) list(info *provider.VideoDetails) []provider.VideoComment {
	if d.showLatest {
		return d.latest
	}
	if info != nil {
		return info.Discussion
	}
	return nil
}

func (d *videoDiscussionState) morePage() int {
	if d.root != nil {
		return d.nextPage
	}
	if d.showLatest {
		return d.nextLatest
	}
	return d.nextHot
}

func (d *videoDiscussionState) artwork(covers []string, info *provider.VideoDetails) []string {
	items := d.list(info)
	list, prefix := &d.listRows, 0
	if d.root != nil {
		covers = append(covers, d.root.AvatarURL)
		items = d.replies
		list, prefix = &d.threadRows, 1
	}
	first, last := list.Visible()
	first = min(len(items), max(0, first-prefix-2))
	last = min(len(items), max(first, last-prefix+3))
	for _, item := range items[first:last] {
		if item.AvatarURL != "" {
			covers = append(covers, item.AvatarURL)
		}
	}
	return covers
}

func (d *videoDiscussionState) firstFocus(canRead bool, info *provider.VideoDetails) app.FocusID {
	if d.root != nil {
		return videoDiscussionBack
	}
	if canRead {
		if d.showLatest {
			return videoDiscussionLatest
		}
		return videoDiscussionHot
	}
	if rows := d.list(info); len(rows) != 0 {
		return videoCommentFocus(rows[0].ID)
	}
	return videoDiscussionCompose
}

func videoCommentFocus(id string) app.FocusID { return app.FocusID("video-discussion/comment/" + id) }

func (p *PageContext) canReadVideoDiscussion() bool {
	entry, _ := p.State.Current()
	return entry.Capabilities.Has(provider.ReadVideoComments)
}

func (p *PageContext) canWriteVideoDiscussion() bool {
	entry, _ := p.State.Current()
	return entry.Capabilities.Has(provider.WriteVideoComments)
}

func (v *View) closeVideoThread() {
	d := v.discussionDetail().discussion
	focus := d.origin
	d.clearThread()
	v.moveFocus(focus, app.Up, true)
}

func (p *PageContext) openVideoThread(comment provider.VideoComment) {
	v := p.host
	d := v.discussionDetail().videoDiscussion()
	d.cancelLoad()
	d.root, d.replies, d.origin = &comment, slices.Clone(comment.Replies), p.State.Focus
	d.failed, d.nextPage, d.activeReply = false, 0, ""
	d.threadRows = ui.ListState{}
	v.moveFocus(videoDiscussionBack, app.Up, p.State.Page != app.VideoDetail)
	if p.State.Page == app.VideoDetail && v.activeScroll != nil {
		scroll := v.activeScroll
		offset := videoDetailTabsTop - videoDetailPanelTabTop + v.videoDetailSectionOffset(scroll)
		scroll.ScrollTo(scroll.X, offset, focusScroll)
	}
	if p.canReadVideoDiscussion() {
		p.loadVideoDiscussion(d, 1)
	}
}

func (d *videoDiscussionState) hasHot(info *provider.VideoDetails) bool {
	return d.hotLoaded || (info != nil && (len(info.Discussion) > 0 || (info.Comments != nil && *info.Comments == 0)))
}

func (p *PageContext) ensureVideoDiscussionHot(detail *videoDetailState) {
	if detail == nil || detail.loading || detail.failure != "" || !p.canReadVideoDiscussion() {
		return
	}
	d := detail.videoDiscussion()
	if !d.showLatest && d.root == nil && d.cancel == nil && !d.failed && !d.hasHot(detail.item.VideoDetails) {
		p.loadVideoDiscussion(d, 0)
	}
}

func (p *PageContext) setVideoDiscussionOrder(latest bool) {
	detail := p.host.discussionDetail()
	d := detail.videoDiscussion()
	hasHot := d.hasHot(detail.item.VideoDetails)
	if d.showLatest == latest && !d.failed && (d.cancel != nil || (latest && d.hasLatest) || (!latest && hasHot)) {
		return
	}
	d.cancelLoad()
	d.showLatest, d.failed = latest, false
	d.focusCommentOnLoad = false
	d.listRows.ScrollTo(0, ui.Start)
	if latest {
		if !d.hasLatest {
			p.loadVideoDiscussion(d, 0)
		}
	} else if !hasHot && p.canReadVideoDiscussion() {
		p.loadVideoDiscussion(d, 0)
	}
}

func (p *PageContext) moveVideoDiscussionOrder(d *videoDiscussionState, direction app.Direction, commentID string) bool {
	if !p.canReadVideoDiscussion() || d.root != nil || direction != app.Left && direction != app.Right {
		return false
	}
	latest := direction == app.Right
	if d.showLatest == latest && !d.failed {
		return true
	}
	p.setVideoDiscussionOrder(latest)
	info := p.host.discussionDetail().item.VideoDetails
	focus := d.firstFocus(true, info)
	if commentID != "" {
		rows := d.list(info)
		if len(rows) != 0 {
			focus = videoCommentFocus(rows[0].ID)
			if slices.ContainsFunc(rows, func(row provider.VideoComment) bool { return row.ID == commentID }) {
				focus = videoCommentFocus(commentID)
			}
		} else if d.cancel != nil {
			d.focusCommentOnLoad = true
		}
	}
	p.host.moveFocus(focus, direction, true)
	return true
}

func (p *PageContext) loadVideoDiscussion(d *videoDiscussionState, page int) {
	d.cancelLoad()
	d.failed = false
	query := provider.VideoCommentQuery{Ref: p.host.discussionDetail().item.Ref, Page: page}
	if d.root != nil {
		query.ThreadID = d.root.ID
	} else if d.showLatest {
		query.Order = provider.VideoCommentsLatest
	} else {
		query.Order = provider.VideoCommentsHot
	}
	d.cancel = p.host.Catalog.ReadVideoComments(query, func(result provider.VideoCommentPage, err error) {
		d.cancel = nil
		d.failed = err != nil
		d.failedPage = page
		focusComment := d.focusCommentOnLoad
		d.focusCommentOnLoad = false
		if err != nil {
			return
		}
		if query.ThreadID == "" {
			if page == 0 && (p.State.Focus == videoDiscussionHot || p.State.Focus == videoDiscussionLatest) {
				d.listRows.ScrollTo(0, ui.Start)
			}
			var appended int
			if query.Order == provider.VideoCommentsLatest {
				d.latest, appended = discussionPage(d.latest, result.Comments, page)
				d.hasLatest, d.nextLatest = true, result.NextPage
				if page != 0 && appended == 0 {
					d.nextLatest = 0
				}
				if focusComment && (p.State.Page == app.VideoDetail || p.State.Page == app.Player) && p.State.Focus == videoDiscussionLatest &&
					d.root == nil && d.showLatest && len(d.latest) != 0 {
					p.host.moveFocus(videoCommentFocus(d.latest[0].ID), app.Down, true)
				}
			} else {
				detail := p.host.discussionDetail()
				if detail.item.VideoDetails == nil {
					detail.item.VideoDetails = &provider.VideoDetails{}
				}
				info := detail.item.VideoDetails
				info.Discussion, appended = discussionPage(info.Discussion, result.Comments, page)
				d.hotLoaded, d.nextHot = true, result.NextPage
				if page != 0 && appended == 0 {
					d.nextHot = 0
				}
				if focusComment && (p.State.Page == app.VideoDetail || p.State.Page == app.Player) && p.State.Focus == videoDiscussionHot &&
					d.root == nil && !d.showLatest && len(info.Discussion) != 0 {
					p.host.moveFocus(videoCommentFocus(info.Discussion[0].ID), app.Down, true)
				}
			}
			if page != 0 && appended != 0 && p.State.Focus == videoDiscussionMoreReplies {
				items := d.list(p.host.discussionDetail().item.VideoDetails)
				p.host.moveFocus(videoCommentFocus(items[len(items)-appended].ID), app.Down, true)
			}
			return
		}
		d.nextPage = result.NextPage
		if page <= 1 {
			d.replies = result.Comments
		} else {
			for _, row := range result.Comments {
				if !slices.ContainsFunc(d.replies, func(current provider.VideoComment) bool { return current.ID == row.ID }) {
					d.replies = append(d.replies, row)
				}
			}
			if p.State.Focus == videoDiscussionMoreReplies && len(result.Comments) != 0 {
				p.host.moveFocus(videoCommentFocus(result.Comments[0].ID), app.Down, true)
			}
		}
	})
}

func discussionPage(previous, current []provider.VideoComment, page int) ([]provider.VideoComment, int) {
	if page == 0 {
		return current, len(current)
	}
	known := make(map[string]bool, len(previous))
	for _, row := range previous {
		known[row.ID] = true
	}
	count := 0
	for _, row := range current {
		if !known[row.ID] {
			known[row.ID] = true
			previous = append(previous, row)
			count++
		}
	}
	return previous, count
}

type videoDiscussionLayout struct {
	listTop, listLeft, listRight   float32
	rowRight, rowRightMargin       float32
	avatarSize, avatarGap          float32
	authorSize, metaSize, bodySize float32
	headingHeight, headerCountGap  float32
	headerTitleX, headerTitleY     float32
	player                         bool
}

var detailDiscussionLayout = videoDiscussionLayout{
	listTop: 8, listLeft: 32, listRight: 32, rowRight: 16, rowRightMargin: -16,
	avatarSize: 44, avatarGap: 18, authorSize: 19, metaSize: 15, bodySize: 22, headingHeight: 26, headerCountGap: 18, headerTitleY: 3,
}

var playerDiscussionLayout = videoDiscussionLayout{
	listTop: 11, listLeft: 16, listRight: 8, rowRight: 12, rowRightMargin: 0,
	avatarSize: 40, avatarGap: 16, authorSize: 19, metaSize: 13, bodySize: 20, headingHeight: 24, headerCountGap: 21, headerTitleX: 3, headerTitleY: 1, player: true,
}

func (p *PageContext) videoDetailDiscussion(c *ui.Context, detail *videoDetailState) {
	d := detail.videoDiscussion()
	colors := p.Theme.Colors
	p.region(videoDiscussionRegion, focusEdges{app.Up: {Focus: videoDetailComments.focusID()}})
	rows := len(d.list(detail.item.VideoDetails))
	if d.root != nil {
		rows = 1 + len(d.replies)
	}
	// Let the page scroll past the video summary before the discussion list scrolls.
	height := float32(max(650, 255+180*min(rows, videoDiscussionVisibleRows)))
	ui.Column(c.Key("video-discussion")).FillWidth().MaxWidth(1480).Height(height).Radius(18).
		Border(1, paint(colors.Border.Subtle)).Clip().Children(func() {
		fill := paint(colors.Surface.Panel)
		p.videoDiscussionHeader(c, d, detail.item.VideoDetails, fill, detailDiscussionLayout)
		p.videoDiscussionRows(c, d, detail.item.VideoDetails, detailDiscussionLayout)
		p.videoDiscussionComposer(c, d, fill)
	})
}

func (p *PageContext) videoDiscussionHeader(c *ui.Context, d *videoDiscussionState, info *provider.VideoDetails, fill ui.Color, layout videoDiscussionLayout) {
	text, colors := p.Assets.Text.Detail, p.Theme.Colors
	ui.Column(c).FillWidth().Padding(36, 32, 0, 32).Background(fill).Children(func() {
		if d.root != nil {
			p.videoDiscussionThreadHeader(c, d.root.ReplyCount)
		} else {
			ui.Row(c).FillWidth().Height(44).Padding(0, 0, 0, layout.headerTitleX).
				Gap(layout.headerCountGap).AlignItems(ui.Center).Children(func() {
				ui.Text(c, text.Comments).FontSize(30).FontWeight(550).TextColor(paint(colors.Text.Primary)).
					Role(ui.RoleHeading).Level(2).Translate(0, layout.headerTitleY)
				if info != nil && info.Comments != nil {
					countY := float32(7)
					if layout.player {
						countY = 5
					}
					ui.Text(c, videoDetailNumbers.Sprintf("%d", *info.Comments)).FontSize(18).FontWeight(400).
						TextColor(paint(colors.Text.Muted)).Translate(0, countY)
				}
			})
			if p.canReadVideoDiscussion() {
				ui.Row(c).FillWidth().Height(51).Margin(22, 0, 0, 0).Gap(28).Children(func() {
					p.videoDiscussionSort(c, text.Hot, videoDiscussionHot, !d.showLatest, false, layout.player)
					p.videoDiscussionSort(c, text.Latest, videoDiscussionLatest, d.showLatest, true, layout.player)
				})
			}
		}
	})
	ui.Box(c).FillWidth().Height(1).Background(paint(colors.Border.Subtle))
}

func (p *PageContext) videoDiscussionThreadHeader(c *ui.Context, count *int64) {
	const padding, iconSize, gap = 7, 22, 8
	text, colors := p.Assets.Text.Detail, p.Theme.Colors
	labelInk, iconInk := paint(colors.Text.Secondary), paint(colors.Text.Muted)
	if p.State.Focus == videoDiscussionBack {
		labelInk, iconInk = paint(colors.Text.Primary), paint(colors.Accent.Soft)
	}
	ui.Row(c).FillWidth().Height(44).AlignItems(ui.Center).Children(func() {
		button := ui.Button(c.Key(string(videoDiscussionBack)), "").Label(text.AllComments).
			Height(42).Padding(0, padding).Radius(8).Border(0, ui.Transparent).
			OnClick(func() { p.dispatch(p.host.closeVideoThread) })
		labelInk = p.animateFocus(button, ui.Transparent, labelInk)
		p.bind(button, videoDiscussionBack, videoDiscussionRegion)
		button.Children(func() {
			ui.Row(c).Gap(gap).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, p.Assets.Icon("detail/back")).Size(iconSize, iconSize).TextColor(iconInk)
				ui.Text(c, text.AllComments).FontSize(22).FontWeight(400).TextColor(labelInk).
					TextStroke(22*videoDetailTextStroke, labelInk)
			})
		})
	})
	if count != nil {
		ui.Text(c, fmt.Sprintf(text.ReplyCount, *count)).FontSize(16).FontWeight(400).
			TextColor(paint(colors.Text.Muted)).Margin(19, 0, 21, padding+iconSize+gap)
	}
}

func (p *PageContext) videoDiscussionSort(c *ui.Context, label string, id app.FocusID, selected, latest, inPlayer bool) {
	colors := p.Theme.Colors
	focused := p.State.Focus == id
	ink := paint(colors.Text.Muted)
	if selected || focused {
		ink = paint(colors.Text.Primary)
	}
	// Selected applies the theme fill. Keep its semantics and override the background.
	button := ui.Button(c.Key(string(id)), "").Label(label).MinWidth(44).Height(42).Padding(0, 4).
		Border(0, ui.Transparent).Role(ui.RoleTab).Selected(selected).Background(ui.Transparent).
		OnClick(func() { p.dispatch(func() { p.setVideoDiscussionOrder(latest) }) })
	p.bindDirection(button, id, videoDiscussionRegion, func(direction app.Direction) bool {
		detail := p.host.discussionDetail()
		d := detail.videoDiscussion()
		if direction == app.Left && latest || direction == app.Right && !latest {
			return p.moveVideoDiscussionOrder(d, direction, "")
		}
		if direction != app.Down {
			return false
		}
		rows := d.list(detail.item.VideoDetails)
		if len(rows) == 0 {
			return false
		}
		p.host.moveFocus(videoCommentFocus(rows[0].ID), direction, true)
		return true
	})
	button.Children(func() {
		x := float32(0)
		if inPlayer && !latest {
			x = 3
		}
		ui.Text(c, label).FontSize(19).FontWeight(400).TextColor(ink).Translate(x, -9).
			TextStroke(19*videoDetailTextStroke, ink)
		p.tabUnderline(c, selected, focused, 0)
	})
}

func (p *PageContext) videoDiscussionRows(c *ui.Context, d *videoDiscussionState, info *provider.VideoDetails, layout videoDiscussionLayout) {
	text, colors := p.Assets.Text, p.Theme.Colors
	rows := d.list(info)
	list, prefix := &d.listRows, 0
	if d.root != nil {
		rows = d.replies
		list, prefix = &d.threadRows, 1
	}
	if p.active && p.host.reveal {
		for i, comment := range rows {
			if p.State.Focus == videoCommentFocus(comment.ID) {
				list.ScrollIntoView(i + prefix)
				break
			}
		}
	}
	end := prefix + len(rows)
	empty := len(rows) == 0 && (d.root == nil || d.cancel != nil)
	count := end
	nextPage := d.morePage()
	if d.failed || empty || nextPage != 0 {
		count++
	}
	type decoration bool
	list.Key = func(index int) any {
		if index >= prefix && index < end {
			return rows[index-prefix].ID
		}
		return decoration(index >= end)
	}
	ui.List(c.Key(d.root != nil), list, count).FillWidth().Grow(1).
		Padding(layout.listTop, layout.listRight, focusBottomMargin, layout.listLeft).Rows(func(row ui.ListRow) {
		c := row.Context
		switch index := row.Index; {
		case index < prefix:
			p.videoDiscussionThreadRoot(c, d, layout)
		case index < end:
			p.videoCommentRow(c, d, rows[index-prefix], layout)
			ui.Box(c).AlignSelf(ui.Stretch).Height(1).Margin(0, layout.rowRightMargin, 0, -16).
				Background(paint(colors.Border.Subtle))
		case d.failed:
			p.videoDetailText(c, text.Detail.CommentLoadFailed)
			p.button(c, text.Retry, videoDiscussionRetry, videoDiscussionRegion, func() {
				p.host.moveFocus(d.firstFocus(p.canReadVideoDiscussion(), info), app.Up, true)
				p.loadVideoDiscussion(d, d.failedPage)
			})
		case empty && d.cancel != nil:
			p.videoDetailText(c, text.Detail.Loading)
		case empty:
			p.videoDetailText(c, text.Detail.NoComments)
		case nextPage != 0:
			label := text.Playback.MoreComments
			if d.root != nil {
				label = text.Detail.MoreReplies
			}
			p.button(c, label, videoDiscussionMoreReplies, videoDiscussionRegion, func() {
				if d.cancel == nil {
					p.loadVideoDiscussion(d, nextPage)
				}
			}).Margin(12, 16, 0, 16)
		}
	})
}

func (p *PageContext) videoDiscussionThreadRoot(c *ui.Context, d *videoDiscussionState, layout videoDiscussionLayout) {
	text, colors, derived := p.Assets.Text.Detail, p.Theme.Colors, p.uiColors()
	top := float32(28)
	if layout.player {
		top = 18
	}
	ui.Column(c).AlignSelf(ui.Stretch).Margin(0, -28).Background(derived.subtleFill).Children(func() {
		ui.Column(c).FillWidth().Padding(top, 28, 2, 28).Children(func() {
			ui.Text(c, text.RootComment).FontSize(12).TextColor(paint(colors.Text.Muted))
			root := ui.Box(c).FillWidth().Padding(14, 4, 23, 4).Radius(8)
			if p.videoCommentActionsFocused(d.root.ID) {
				root.Background(derived.subtleFill)
			}
			root.Children(func() {
				p.videoCommentContent(c, *d.root, false, layout, func() { p.videoCommentActions(c, d, *d.root) })
			})
		})
		ui.Box(c).FillWidth().Height(7).Background(paint(colors.Surface.Canvas))
	})
}

func (p *PageContext) videoCommentRow(c *ui.Context, d *videoDiscussionState, comment provider.VideoComment, layout videoDiscussionLayout) {
	colors, derived := p.Theme.Colors, p.uiColors()
	id := videoCommentFocus(comment.ID)
	top := float32(20)
	if d.root != nil {
		top = 26
	}
	if d.root != nil && d.activeReply == comment.ID {
		row := ui.Column(c.Key(comment.ID)).AlignSelf(ui.Stretch).
			Padding(top, layout.rowRight, 20, 16).Margin(0, layout.rowRightMargin, 0, -16).Radius(12)
		if p.videoCommentActionsFocused(comment.ID) {
			row.Background(derived.subtleFill)
		}
		row.Children(func() {
			p.videoCommentContent(c, comment, false, layout, func() { p.videoCommentActions(c, d, comment) })
		})
		return
	}
	button := ui.Button(c.Key(string(id)), "").Label(p.videoCommentAuthor(comment)).AlignSelf(ui.Stretch).
		Padding(top, layout.rowRight, 20, 16).Margin(0, layout.rowRightMargin, 0, -16).Radius(12).Border(0, ui.Transparent).OnClick(func() {
		p.dispatch(func() {
			if d.root == nil {
				p.openVideoThread(comment)
			} else {
				d.activeReply = comment.ID
				p.host.moveFocus(videoCommentReplyFocus(comment.ID), app.Down, true)
			}
		})
	})
	background := ui.Transparent
	if p.State.Focus == id {
		background = paint(colors.Surface.Raised)
	}
	p.animateFocus(button, background, paint(colors.Text.Primary))
	p.bindDirection(button, id, videoDiscussionRegion, func(direction app.Direction) bool {
		if d.root != nil {
			return false
		}
		if p.moveVideoDiscussionOrder(d, direction, comment.ID) {
			return true
		}
		info := p.host.discussionDetail().item.VideoDetails
		rows := d.list(info)
		index := slices.IndexFunc(rows, func(row provider.VideoComment) bool { return row.ID == comment.ID })
		if index < 0 {
			return false
		}
		focus := app.FocusID("")
		switch {
		case direction == app.Up && index > 0:
			focus = videoCommentFocus(rows[index-1].ID)
		case direction == app.Up && p.canReadVideoDiscussion():
			focus = d.firstFocus(true, info)
		case direction == app.Down && index+1 < len(rows):
			focus = videoCommentFocus(rows[index+1].ID)
		case direction == app.Down && p.canWriteVideoDiscussion():
			focus = videoDiscussionCompose
		}
		if focus == "" {
			return false
		}
		p.host.moveFocus(focus, direction, true)
		return true
	})
	button.Children(func() {
		p.videoCommentContent(c, comment, d.root == nil, layout, func() { p.videoCommentSummary(c, comment) })
	})
}

func (p *PageContext) videoCommentContent(c *ui.Context, comment provider.VideoComment, preview bool, layout videoDiscussionLayout, actions func()) {
	colors := p.Theme.Colors
	name := p.videoCommentAuthor(comment)
	ui.Row(c).FillWidth().Gap(layout.avatarGap).AlignItems(ui.Start).Children(func() {
		p.videoCommentAvatar(c, name, comment.AvatarURL, layout.avatarSize, nil, false)
		ui.Column(c).Grow(1).Shrink(1).Children(func() {
			ui.Row(c).FillWidth().Height(layout.headingHeight).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, name).FontSize(layout.authorSize).FontWeight(400).TextColor(paint(colors.Text.Secondary)).SingleLine()
				if comment.IsCreator {
					ui.Text(c, p.sourceCreatorMark()).Padding(0, 4).Radius(3).Border(1, withAlpha(colors.Accent.Primary, 0x88)).
						FontSize(11).TextColor(paint(colors.Accent.Soft))
				}
				if comment.Pinned && preview {
					ui.Spacer(c).Grow(1)
					ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
						ui.Icon(c, p.Assets.Icon("detail/pin")).Size(12, 12).TextColor(paint(colors.Text.Muted))
						ui.Text(c, p.sourcePinnedLabel()).FontSize(11).TextColor(paint(colors.Text.Muted))
					})
				}
			})
			if meta := p.videoCommentMeta(comment); meta != "" {
				ui.Text(c, meta).Margin(3, 0, 0, 0).FontSize(layout.metaSize).LineHeight(1.5).TextColor(paint(colors.Text.Muted))
			}
			inlineTimestamp := !preview && p.videoCommentTimestamp(c, comment, layout.player)
			if !inlineTimestamp {
				body := p.videoCommentText(c, comment).FillWidth().FontSize(layout.bodySize).FontWeight(400).LineHeight(1.65).
					Margin(11, 0, 9, 0).TextColor(paint(colors.Text.Primary))
				if layout.player {
					body.Translate(0, 3)
				}
			}
			if layout.player && !preview && !inlineTimestamp {
				p.playbackCommentLinks(c, comment)
			}
			actions()
			if preview {
				p.videoReplyPreview(c, comment, layout.player)
			}
		})
	})
}

func (p *PageContext) videoCommentText(c *ui.Context, comment provider.VideoComment) ui.Element {
	if comment.ReplyToAuthor != "" {
		mention := fmt.Sprintf(p.Assets.Text.Detail.ReplyTo, "@"+comment.ReplyToAuthor) + ": "
		return ui.RichText(c, ui.Span{Text: mention, Color: paint(p.Theme.Colors.Accent.Soft)}, ui.Span{Text: comment.Body})
	}
	return ui.Text(c, comment.Body)
}

func (p *PageContext) videoCommentAuthor(comment provider.VideoComment) string {
	if comment.Author != "" {
		return comment.Author
	}
	return p.Assets.Text.Detail.UnknownAuthor
}

func (p *PageContext) videoCommentMeta(comment provider.VideoComment) string {
	var stamp string
	if comment.PublishedAt != 0 {
		text := p.Assets.Text.Detail
		date := time.Unix(comment.PublishedAt, 0).Local()
		age := time.Since(date)
		switch {
		case age < 0 || age >= 7*24*time.Hour:
			stamp = date.Format("2006.01.02")
		case age >= 24*time.Hour:
			stamp = fmt.Sprintf(text.DaysAgo, int(age/(24*time.Hour)))
		case age >= time.Hour:
			stamp = fmt.Sprintf(text.HoursAgo, int(age/time.Hour))
		case age >= time.Minute:
			stamp = fmt.Sprintf(text.MinutesAgo, int(age/time.Minute))
		default:
			stamp = text.JustNow
		}
	}
	if stamp != "" && comment.Location != "" {
		return stamp + " · " + comment.Location
	}
	return stamp + comment.Location
}

func (p *PageContext) videoCommentAvatar(c *ui.Context, name, url string, size float32, image ui.ImageSource, own bool) {
	if image == nil && url != "" {
		if bitmap := p.catalogCover(url); bitmap != nil {
			image = bitmap
		}
	}
	colors := p.Theme.Colors
	fill := colors.Surface.Raised
	if own {
		fill = colors.Selected.Background
	}
	ui.Box(c).Size(size, size).Shrink(0).Radius(size / 2).Clip().Center().Background(paint(fill)).Children(func() {
		if image != nil {
			ui.Image(c, image).Fill().Fit(ui.Cover)
			return
		}
		for _, letter := range strings.TrimSpace(name) {
			ui.Text(c, strings.ToUpper(string(letter))).FontSize(17).TextColor(paint(colors.Text.Primary))
			return
		}
		ui.Icon(c, p.Assets.Icon("detail/user")).Size(19, 19).TextColor(paint(colors.Text.Primary))
	})
}

func (p *PageContext) videoCommentSummary(c *ui.Context, comment provider.VideoComment) {
	colors := p.Theme.Colors
	likeIcon := "like"
	if comment.Liked {
		likeIcon = "like-filled"
	}
	ui.Row(c).Gap(26).Margin(14, 0, 5, 0).AlignItems(ui.Center).Children(func() {
		for _, count := range []struct {
			icon  string
			value *int64
		}{{likeIcon, comment.Likes}, {"detail/reply", comment.ReplyCount}} {
			if count.value != nil {
				ui.Row(c).Gap(9).AlignItems(ui.Center).Children(func() {
					ui.Icon(c, p.Assets.Icon(count.icon)).Size(18, 18).TextColor(paint(colors.Text.Muted))
					ui.Text(c, videoDetailNumbers.Sprintf("%d", *count.value)).FontSize(16).TextColor(paint(colors.Text.Muted))
				})
			}
		}
	})
}

func (p *PageContext) videoReplyPreview(c *ui.Context, comment provider.VideoComment, player bool) {
	text, colors, derived := p.Assets.Text.Detail, p.Theme.Colors, p.uiColors()
	if len(comment.Replies) != 0 {
		reply := comment.Replies[0]
		preview := ui.Column(c).FillWidth().MaxWidth(850).Margin(15, 0, 0, 0).Padding(11, 14, 11, 16).Gap(3).
			Radius(7).Clip().Background(derived.subtleFill)
		if player {
			preview.Margin(10, 0, 0, 0).Padding(10, 12)
		}
		preview.Children(func() {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Text(c, p.videoCommentAuthor(reply)).FontSize(14).TextColor(paint(colors.Text.Secondary))
				if reply.IsCreator {
					accent := colors.Accent.Soft
					ui.Text(c, p.sourceCreatorMark()).FontSize(10).Padding(0, 3).Radius(2).
						Border(1, withAlpha(colors.Accent.Primary, 0x88)).TextColor(paint(accent))
				}
			})
			ui.Text(c, reply.Body).FillWidth().FontSize(15).LineHeight(1.6).TextColor(paint(colors.Text.Muted))
		})
	}
	if comment.ReplyCount != nil && *comment.ReplyCount > 0 {
		label := ui.Text(c, fmt.Sprintf(text.AllReplies, *comment.ReplyCount)).FontSize(17).Margin(14, 0, 3, 6).
			TextColor(paint(colors.Accent.Soft))
		if player {
			label.Height(36).Margin(8, 0, 0, 6)
		}
	}
}
