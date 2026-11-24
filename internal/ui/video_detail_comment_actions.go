package tvui

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/provider"
)

const (
	videoCommentEdit    app.FocusID = "video-comment-compose/edit"
	videoCommentSend    app.FocusID = "video-comment-compose/send"
	videoCommentInput   app.FocusID = "video-comment-compose/input"
	videoCommentPreview app.FocusID = "video-comment-compose/preview"
)

type videoCommentDraft struct {
	editing            bool
	title, body, input string
	thread, reply      string
	failure            string
	preview            ui.ScrollState
}

type videoCommentComposePage struct{}

func videoCommentReplyFocus(id string) app.FocusID {
	return app.FocusID("video-discussion/reply/" + id)
}

func videoCommentVoteFocus(icon, id string) app.FocusID {
	return app.FocusID("video-discussion/" + icon + "/" + id)
}

func (p *PageContext) videoCommentActionsFocused(comment string) bool {
	focus := p.paintFocus()
	return focus == videoCommentReplyFocus(comment) ||
		focus == videoCommentVoteFocus("like", comment) || focus == videoCommentVoteFocus("dislike", comment)
}

func (p *PageContext) videoDiscussionComposer(c *ui.Context, d *videoDiscussionState, fill ui.Color) {
	if !p.canWriteVideoDiscussion() {
		return
	}
	colors, derived, text := p.Theme.Colors, p.uiColors(), p.Assets.Text.Detail
	label := text.CommentPlaceholder
	if d.root != nil {
		label = fmt.Sprintf(text.ReplyTo, p.videoCommentAuthor(*d.root))
	}
	ui.Box(c).FillWidth().Height(1).Background(paint(colors.Border.Subtle))
	ui.Row(c).FillWidth().Height(88).Padding(18, 28, 22, 28).Gap(12).AlignItems(ui.Center).
		Background(fill).Children(func() {
		p.videoCommentAvatar(c, p.videoCommentAccount().Name, "", 34, p.host.avatarImage, true)
		background, ink := derived.subtleFill, paint(colors.Text.Secondary)
		focused := p.paintFocus() == videoDiscussionCompose
		if focused {
			background, ink = paint(colors.Focus.Background), paint(colors.Focus.Foreground)
		}
		button := ui.Button(c.Key(string(videoDiscussionCompose)), "").Label(label).Grow(1).Height(48).
			Padding(0, 14).Radius(11).Border(1, derived.subtleBorder).
			OnClick(func() { p.dispatch(func() { p.openVideoComment(d.root, d.root) }) })
		p.animateFocus(button, background, ink)
		p.addVideoFocusShadow(button, focused)
		p.bindDirection(button, videoDiscussionCompose, videoDiscussionRegion, func(direction app.Direction) bool {
			if direction != app.Up || d.root != nil {
				return false
			}
			rows := d.list(p.host.discussionDetail().item.VideoDetails)
			if len(rows) == 0 {
				return false
			}
			p.host.moveFocus(videoCommentFocus(rows[len(rows)-1].ID), direction, true)
			return true
		})
		button.Children(func() {
			gap := float32(10)
			if p.Location.Page == app.Player {
				gap = 16
			}
			ui.Row(c).FillWidth().Gap(gap).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, p.Assets.Icon("detail/comment")).Size(20, 20)
				ui.Text(c, label).FontSize(17)
			})
		})
	})
}

func (p *PageContext) videoCommentActions(c *ui.Context, d *videoDiscussionState, comment provider.VideoComment) {
	text, colors := p.Assets.Text.Detail, p.Theme.Colors
	entry, _ := p.State.Current()
	ui.Row(c).FillWidth().Height(40).Gap(12).AlignItems(ui.Center).Children(func() {
		if entry.Capabilities.Has(provider.EditVideo) {
			label := text.Like
			if comment.Liked {
				label = text.Liked
			}
			count := ""
			if comment.Likes != nil {
				count = videoDetailNumbers.Sprintf("%d", *comment.Likes)
			}
			p.videoCommentVote(c, label, count, "like", comment.ID, comment.Liked, func() { p.toggleVideoCommentLike(comment) })
		}
		if p.canWriteVideoDiscussion() {
			p.videoCommentVote(c, text.Dislike, "", "dislike", comment.ID, comment.Disliked, func() {
				p.withVideoAccountFor(provider.WriteVideoComments, text.Dislike, func() {
					p.editVideo(provider.VideoEdit{Ref: p.host.discussionDetail().item.Ref, Interaction: provider.VideoReplyDislike,
						Enabled: !comment.Disliked, ReplyID: comment.ID})
				})
			})
			p.styledButton(c, text.Reply, "", videoCommentReplyFocus(comment.ID), videoDiscussionRegion,
				func() { p.openVideoComment(d.root, &comment) }, buttonAppearance{
					background: ui.Transparent, foreground: paint(colors.Text.Muted),
					height: 46, padding: 12, radius: 7, fontSize: 17, focusShadow: true,
				}).Margin(0, 0, 0, 4).AlignSelf(ui.Center)
		}
	})
}

func (p *PageContext) videoCommentVote(c *ui.Context, label, count, icon, comment string, selected bool, action func()) {
	colors := p.Theme.Colors
	ink := paint(colors.Text.Muted)
	glyph := "detail-vote/" + icon
	if selected {
		glyph = icon + "-filled"
	}
	id := videoCommentVoteFocus(icon, comment)
	e := ui.Button(c.Key(string(id)), "").Label(strings.TrimSpace(label+" "+count)).Height(46).MinWidth(46).
		Padding(0, 12).Radius(7).Border(0, ui.Transparent).OnClick(func() { p.dispatch(action) })
	background := ui.Transparent
	focused := p.paintFocus() == id
	if focused {
		background, ink = paint(colors.Focus.Background), paint(colors.Focus.Foreground)
	}
	p.addVideoFocusShadow(e, focused)
	p.animateFocus(e, background, ink)
	p.bind(e, id, videoDiscussionRegion)
	e.Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, p.Assets.Icon(glyph)).Size(17, 17)
			if count != "" {
				ui.Text(c, count).FontSize(17)
			}
		})
	})
}

func (d *videoDiscussionState) updateVote(hot []provider.VideoComment, request provider.VideoEdit) {
	update := func(comment *provider.VideoComment) {
		if comment.ID != request.ReplyID {
			return
		}
		liked := comment.Liked
		if request.Interaction == provider.VideoReplyLike {
			comment.Liked = request.Enabled
			if request.Enabled {
				comment.Disliked = false
			}
		} else {
			comment.Disliked = request.Enabled
			if request.Enabled {
				comment.Liked = false
			}
		}
		if comment.Likes != nil && liked != comment.Liked {
			count := max(0, *comment.Likes-1)
			if comment.Liked {
				count = *comment.Likes + 1
			}
			comment.Likes = &count
		}
	}
	if d.root != nil {
		update(d.root)
	}
	for _, rows := range [][]provider.VideoComment{hot, d.latest, d.replies} {
		for i := range rows {
			update(&rows[i])
			for j := range rows[i].Replies {
				update(&rows[i].Replies[j])
			}
		}
	}
}

func (p *PageContext) videoCommentAccount() provider.Account {
	account, _ := currentAccount(p.host.Auth.Accounts())
	return account
}

func (p *PageContext) openVideoComment(thread, reply *provider.VideoComment) {
	text := p.Assets.Text.Detail
	if !p.canWriteVideoDiscussion() {
		return
	}
	p.withVideoAccountFor(provider.WriteVideoComments, text.WriteComment, func() {
		if thread != nil && p.host.discussionDetail().videoDiscussion().root == nil {
			p.openVideoThread(*thread)
		}
		if !p.State.Push(app.Route{Page: app.VideoCommentCompose}) {
			return
		}
		draft := videoCommentDraft{title: text.WriteComment}
		if thread != nil && reply != nil {
			draft.title = fmt.Sprintf(text.ReplyTo, p.videoCommentAuthor(*reply))
			draft.thread, draft.reply = thread.ID, reply.ID
		}
		p.host.discussionDetail().draft = draft
		p.editVideoCommentDraft()
	})
}

func (p *PageContext) editVideoCommentDraft() {
	v := p.host
	if v.discussionDetail().operation != nil {
		return
	}
	draft := &v.discussionDetail().draft
	draft.editing, draft.input, draft.failure = true, draft.body, ""
	v.moveFocus(videoCommentInput, app.Right, false)
}

func (p *PageContext) finishVideoCommentEdit(key ui.TextKey) bool {
	draft := &p.host.discussionDetail().draft
	return p.finishTextComposeEdit(key, &draft.editing, &draft.body, &draft.preview, draft.canSend, videoCommentEdit, videoCommentSend)
}

func (p *PageContext) finishTextComposeEdit(
	key ui.TextKey,
	editing *bool,
	body *string,
	preview *ui.ScrollState,
	canSend func() bool,
	editFocus, sendFocus app.FocusID,
) bool {
	switch key.Key {
	case ui.KeyEnter:
		*body = key.Text()
		preview.Y = 0
	case ui.KeyEscape:
	default:
		return false
	}
	*editing = false
	focus := editFocus
	if key.Key == ui.KeyEnter && canSend() {
		focus = sendFocus
	}
	p.host.moveFocus(focus, app.Right, true)
	return true
}

func (d *videoCommentDraft) canSend() bool {
	return strings.TrimSpace(d.body) != ""
}

type textComposeModalSpec struct {
	region             string
	eyebrow            string
	title              string
	renderAuthor       func(*ui.Context)
	editing            bool
	inputKey           string
	input              *string
	inputFocus         app.FocusID
	inputPlaceholder   string
	maxLength          int
	onTextKey          func(ui.TextKey) bool
	body               string
	hasDraft           bool
	previewFocus       app.FocusID
	previewScroll      *ui.ScrollState
	previewPlaceholder string
	mutedPlaceholder   bool
	failureKey         string
	failure            string
	actionsKey         string
	editLabel          string
	editFocus          app.FocusID
	onEdit             func()
	sendLabel          string
	sendFocus          app.FocusID
	onSend             func()
	sending            bool
}

func (p *PageContext) renderTextComposeModal(c *ui.Context, spec textComposeModalSpec) {
	colors := p.Theme.Colors
	p.region(spec.region, nil)
	ui.Column(c).FillWidth().Lang(p.langTag()).Font(p.uiFont()).Children(func() {
		p.framelessHeader(c, spec.eyebrow, "comment", spec.title, "")
		ui.Row(c).Gap(15).AlignItems(ui.Center).Margin(0, 0, 24, 0).Children(func() {
			spec.renderAuthor(c)
		})
		if spec.editing {
			field := ui.TextInput(c.Key(spec.inputKey), spec.input).FillWidth().Height(105).FontSize(29).
				Label(spec.title).Placeholder(spec.inputPlaceholder).MaxLength(spec.maxLength).
				ReturnKey(ui.ReturnDone).OnTextKey(spec.onTextKey)
			if p.active {
				field.FocusBind(&p.State.Focus, spec.inputFocus)
				field.Focus(ui.FocusOptions{PreventScroll: true})
			}
		} else {
			body := spec.body
			ink := paint(colors.Text.Primary)
			if !spec.hasDraft {
				body = spec.previewPlaceholder
				if spec.mutedPlaceholder {
					ink = paint(colors.Text.Muted)
				}
			}
			preview := ui.Scroll(c.Key(string(spec.previewFocus))).FillWidth().MinHeight(101).MaxHeight(174).
				TrackScroll(spec.previewScroll)
			preview.Children(func() {
				ui.Text(c, body).FillWidth().Padding(17, 0, 0, 0).FontSize(29).LineHeight(1.8).TextColor(ink)
			})
			if spec.hasDraft {
				p.bindDraftPreview(preview, spec.previewFocus, spec.region, spec.previewScroll, spec.editFocus, 92)
			}
		}
		ui.Text(c, fmt.Sprintf("%d / %d", ui.UTF16Len(spec.body), spec.maxLength)).
			AlignSelf(ui.End).Margin(10, 0, 0, 0).FontSize(16).TextColor(paint(colors.Text.Muted))
		if spec.failure != "" {
			failCtx := c
			if spec.failureKey != "" {
				failCtx = c.Key(spec.failureKey)
			}
			ui.Text(failCtx, spec.failure).Margin(10, 0, 0, 0).FontSize(18).TextColor(paint(colors.Status.Warning.Foreground))
		}
		actionsCtx := c
		if spec.actionsKey != "" {
			actionsCtx = c.Key(spec.actionsKey)
		}
		ui.Row(actionsCtx).Gap(16).Margin(36, 0, 0, 0).AlignSelf(ui.Start).Children(func() {
			editBtn := p.styledButton(c, spec.editLabel, "", spec.editFocus, spec.region, spec.onEdit, p.framelessButtonAppearance())
			if spec.sending {
				editBtn.AccessDisabled(true)
			}
			disabled := !spec.hasDraft || spec.sending
			sendBtn := p.styledButton(c, spec.sendLabel, "send", spec.sendFocus, spec.region, spec.onSend, p.framelessButtonAppearance()).
				Disabled(!spec.hasDraft).AccessDisabled(spec.sending).DimDisabled(false)
			if disabled {
				sendBtn.Opacity(0.35)
			}
		})
	})
}

func (videoCommentComposePage) Render(c *ui.Context, p *PageContext) {
	detail := p.host.discussionDetail()
	draft := &detail.draft
	text, colors := p.Assets.Text.Detail, p.Theme.Colors
	account := p.videoCommentAccount()
	p.renderTextComposeModal(c, textComposeModalSpec{
		region:  "video-comment-compose",
		eyebrow: "DISCUSSION \u00b7 COMPOSE",
		title:   draft.title,
		renderAuthor: func(c *ui.Context) {
			p.videoCommentAvatar(c, account.Name, "", 40, p.host.avatarImage, true)
			ui.Text(c, account.Name).FontSize(20).TextColor(paint(colors.Text.Secondary))
		},
		editing:            draft.editing,
		inputKey:           "video-comment-input",
		input:              &draft.input,
		inputFocus:         videoCommentInput,
		inputPlaceholder:   text.KeyboardPlaceholder,
		maxLength:          provider.VideoCommentLimit,
		onTextKey:          p.finishVideoCommentEdit,
		body:               draft.body,
		hasDraft:           draft.canSend(),
		previewFocus:       videoCommentPreview,
		previewScroll:      &draft.preview,
		previewPlaceholder: text.CommentInput,
		failure:            draft.failure,
		editLabel:          text.EditComment,
		editFocus:          videoCommentEdit,
		onEdit:             p.editVideoCommentDraft,
		sendLabel:          text.PostComment,
		sendFocus:          videoCommentSend,
		onSend:             p.postVideoComment,
		sending:            detail.operation != nil,
	})
}

func (p *PageContext) bindDraftPreview(preview ui.Element, id app.FocusID, region string, scroll *ui.ScrollState, edit app.FocusID, step float32) {
	p.bindDirection(preview, id, region, func(direction app.Direction) bool {
		next := scroll.Y
		switch direction {
		case app.Up:
			next = max(0, next-step)
		case app.Down:
			next = min(scroll.MaxY, next+step)
		case app.Left:
			p.host.moveFocus(edit, direction, true)
			return true
		}
		if next == scroll.Y {
			return false
		}
		scroll.Y = next
		return true
	})
	if p.paintFocus() == id {
		preview.Radius(10).Border(2, paint(p.Theme.Colors.Focus.Ring))
	}
}

func (p *PageContext) postVideoComment() {
	v := p.host
	detail := v.discussionDetail()
	draft := &detail.draft
	if detail.operation != nil || !draft.canSend() {
		return
	}
	request := provider.VideoEdit{
		Ref: detail.item.Ref, Interaction: provider.VideoPostComment, Body: draft.body,
		ThreadID: draft.thread, ReplyID: draft.reply,
	}
	revision := p.State.Revision
	draft.failure = ""
	detail.operation = v.Catalog.EditVideo(request, func(result provider.VideoEditResult, err error) {
		detail.operation = nil
		if err != nil {
			if p.State.Revision == revision && !p.reopenVideoSignIn(err, nil) {
				draft.failure = p.videoOperationError(err, p.Assets.Text.Detail.UpdateFailed)
			}
			return
		}
		if p.State.Page == app.VideoCommentCompose {
			p.State.Back()
		}
		d := detail.videoDiscussion()
		if request.ThreadID == "" && d.root == nil {
			if info := detail.item.VideoDetails; info != nil && info.Comments != nil {
				count := *info.Comments + 1
				info.Comments = &count
			}
			d.showLatest = true
			d.listRows.ScrollTo(0, ui.Start)
			if result.Comment != nil {
				d.latest = append([]provider.VideoComment{*result.Comment}, d.latest...)
				if detail.tab == videoDetailComments {
					v.moveFocus(videoCommentFocus(result.Comment.ID), app.Up, true)
				}
			}
			p.loadVideoDiscussion(d, 0)
		} else if d.root != nil && d.root.ID == request.ThreadID {
			if d.root.ReplyCount != nil {
				count := *d.root.ReplyCount + 1
				d.root.ReplyCount = &count
			}
			if result.Comment != nil {
				d.replies = append(d.replies, *result.Comment)
				d.threadRows.ScrollIntoView(len(d.replies))
				v.moveFocus(videoCommentFocus(result.Comment.ID), app.Down, false)
			} else {
				p.loadVideoDiscussion(d, 1)
			}
		}
		detail.feedback = videoDetailFeedback{text: p.Assets.Text.Detail.CommentPosted}
	})
}
