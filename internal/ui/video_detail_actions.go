package tvui

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/provider"
)

var detailInteractions = map[app.FocusID]provider.VideoInteraction{
	videoDetailLater:    provider.VideoWatchLater,
	videoDetailFollow:   provider.VideoFollow,
	videoDetailLike:     provider.VideoLike,
	videoDetailFavorite: provider.VideoFavorite,
}

const videoConfirmAccept app.FocusID = "video-confirm/accept"

func (s *videoDetailState) cancelOperation() {
	if s.operation != nil {
		s.operation()
		s.operation = nil
	}
}

func (p *PageContext) runVideoDetailAction(id app.FocusID, label string) {
	interaction := detailInteractions[id]
	p.withVideoAccount(label, func() {
		if p.host.videoDetail.operation != nil {
			return
		}
		p.performVideoInteraction(interaction)
	})
}

func (p *PageContext) runVideoCustomAction(action provider.ActionSpec) {
	actionID := action.ID
	p.withVideoAccount(action.Label, func() {
		if p.host.videoDetail.operation != nil {
			return
		}
		current, ok := p.host.videoDetail.actionSpec(actionID)
		if !ok {
			current = action
		}
		if current.Confirm != nil {
			p.showVideoActionConfirmation(current)
			return
		}
		p.performVideoCustomAction(current)
	})
}

func (p *PageContext) performVideoCustomAction(action provider.ActionSpec) {
	item := p.host.videoDetail.item
	p.editVideo(provider.VideoEdit{
		Ref:         item.Ref,
		Interaction: provider.VideoAction,
		ActionID:    action.ID,
		Enabled:     !action.Active,
	})
}

func (p *PageContext) withVideoAccount(label string, action func()) {
	if p.State.Page == app.Player {
		p.withPlayerAccount(action)
		return
	}
	entry, _ := p.State.Current()
	if !entry.Capabilities.Has(provider.EditVideo) {
		p.host.videoDetail.feedback = videoDetailFeedback{text: fmt.Sprintf(p.Assets.Text.Detail.ActionsUnavailable, label)}
		return
	}
	if p.host.Auth.Accounts().CurrentID != "" {
		action()
		return
	}
	p.openVideoSignIn(action)
}

func (p *PageContext) openVideoSignIn(action func()) bool {
	p.host.openSignIn()
	if p.State.Page != app.SignInQR {
		return false
	}
	p.host.videoDetail.signInAction = action
	return true
}

func (v *View) resumeVideoAfterSignIn(action func()) {
	if v.State.Page != app.VideoDetail || v.Auth.Accounts().CurrentID == "" {
		return
	}
	syncVideoDetail(v)
	detail := &v.videoDetail
	if detail.loading {
		detail.afterLoad = action
	} else if detail.failure == "" {
		v.dispatch(action)
	}
}

func (p *PageContext) performVideoInteraction(action provider.VideoInteraction) {
	item := p.host.videoDetail.item
	var engagement provider.VideoEngagement
	if item.VideoDetails != nil {
		engagement = item.VideoDetails.Engagement
	}
	request := provider.VideoEdit{Ref: item.Ref, Interaction: action}
	switch action {
	case provider.VideoFollow:
		request.Enabled = item.Creator != nil && !item.Creator.Following
	case provider.VideoLike:
		request.Enabled = !engagement.Liked
	case provider.VideoWatchLater:
		request.Enabled = !engagement.WatchLater
	case provider.VideoFavorite:
		request.Enabled = !engagement.Favorite
	}
	p.editVideo(request)
}

func (p *PageContext) editVideo(request provider.VideoEdit) {
	detail, text := p.host.discussionDetail(), p.Assets.Text.Detail
	if detail.failure != "" || detail.loading || detail.operation != nil {
		return
	}
	detail.operation = p.host.Catalog.EditVideo(request, func(result provider.VideoEditResult, err error) {
		detail.operation = nil
		if err != nil {
			var resume func()
			if request.Interaction == provider.VideoAction {
				if action, ok := detail.actionSpec(request.ActionID); ok && action.Confirm != nil {
					resume = func() { p.showVideoActionConfirmation(action) }
				}
			}
			if p.State.Page != app.VideoDetail || !p.reopenVideoSignIn(err, resume) {
				detail.feedback = videoDetailFeedback{text: p.videoOperationError(err, text.UpdateFailed)}
			}
			return
		}
		var action *provider.ActionSpec
		if request.Interaction == provider.VideoAction {
			if spec, ok := detail.actionSpec(request.ActionID); ok {
				action = &spec
			}
		}
		detail.applyVideoEdit(request, result)
		if result.Following != nil && detail.item.Creator != nil {
			p.host.syncCreatorFollowing(*detail.item.Creator, *result.Following)
		}
		if detail == &p.host.player.discussion {
			p.host.syncPlaybackEngagement(request, result)
		}
		if feedback := videoEditFeedback(text, request, result, action); feedback.text != "" {
			detail.feedback = feedback
		}
	})
}

func videoEditFeedback(text detailLabels, request provider.VideoEdit, result provider.VideoEditResult, action *provider.ActionSpec) videoDetailFeedback {
	if result.Partial {
		return videoDetailFeedback{text: text.Partial}
	}
	toggled := func(actual *bool, on, off string) videoDetailFeedback {
		if actual != nil && *actual || actual == nil && request.Enabled {
			return videoDetailFeedback{text: on}
		}
		return videoDetailFeedback{text: off}
	}
	switch request.Interaction {
	case provider.VideoWatchLater:
		return toggled(result.WatchLater, text.LaterAdded, text.LaterRemoved)
	case provider.VideoFollow:
		return toggled(result.Following, text.Followed, text.Unfollowed)
	case provider.VideoLike:
		return toggled(result.Liked, text.Liked, text.Like)
	case provider.VideoFavorite:
		if name := strings.TrimSpace(result.FavoriteCollection); name != "" {
			if request.Enabled {
				return videoDetailFeedback{text: fmt.Sprintf(text.FavoriteAddedTo, name)}
			}
			return videoDetailFeedback{text: text.FavoriteRemoved}
		}
		return toggled(result.Favorite, text.FavoriteAdded, text.FavoriteRemoved)
	case provider.VideoAction:
		message := strings.TrimSpace(result.Message)
		celebrate := result.Celebrate
		var icons []string
		if action != nil {
			if message == "" {
				message = strings.TrimSpace(action.DoneMessage)
			}
			celebrate = celebrate || action.Celebrate
			icons = action.CelebrateIcons
		}
		if message == "" && celebrate {
			message = text.ActionDone
		}
		if message == "" {
			return videoDetailFeedback{}
		}
		return videoDetailFeedback{text: message, celebration: celebrate, icons: icons}
	default:
		return videoDetailFeedback{}
	}
}

func (p *PageContext) reopenVideoSignIn(err error, action func()) bool {
	if provider.CodeOf(err) != provider.NeedsSignIn {
		return false
	}
	return p.openVideoSignIn(action)
}

func (s *videoDetailState) applyVideoEdit(request provider.VideoEdit, result provider.VideoEditResult) {
	if s.item.VideoDetails == nil {
		s.item.VideoDetails = &provider.VideoDetails{}
	}
	detail := s.item.VideoDetails
	if result.Liked != nil {
		detail.Engagement.Liked = *result.Liked
	}
	if result.Favorite != nil {
		detail.Engagement.Favorite = *result.Favorite
	}
	if result.WatchLater != nil {
		detail.Engagement.WatchLater = *result.WatchLater
	}
	if result.Following != nil && s.item.Creator != nil {
		s.item.Creator.Following = *result.Following
	}
	if len(result.ActionStates) != 0 {
		for index := range detail.Actions {
			if active, ok := result.ActionStates[detail.Actions[index].ID]; ok {
				detail.Actions[index].Active = active
			}
		}
	}
	if request.Interaction == provider.VideoReplyLike || request.Interaction == provider.VideoReplyDislike {
		s.videoDiscussion().updateVote(detail.Discussion, request)
	}
}

func (p *PageContext) videoOperationError(err error, fallback string) string {
	if message := provider.MessageOf(err); message != "" {
		return message
	}
	text := p.Assets.Text
	switch provider.CodeOf(err) {
	case provider.NeedsSignIn:
		return text.Auth.AccountExpired
	case provider.RateLimited:
		return text.Home.RateLimited
	case provider.Denied:
		return text.Detail.Denied
	case provider.Expired:
		return text.Detail.Unavailable
	default:
		return fallback
	}
}

type videoConfirmPage struct{}

func (p *PageContext) showVideoActionConfirmation(action provider.ActionSpec) {
	if action.Confirm == nil {
		p.performVideoCustomAction(action)
		return
	}
	if p.State.Push(app.Route{Page: app.VideoConfirm}) {
		copy := action
		p.host.videoDetail.confirm = &copy
	}
}

func (videoConfirmPage) Render(c *ui.Context, p *PageContext) {
	const region = "video-confirm"
	detail, text, colors := &p.host.videoDetail, p.Assets.Text, p.Theme.Colors
	action := detail.confirm
	if action == nil || action.Confirm == nil {
		return
	}
	confirm := action.Confirm
	heading, note := confirm.Title, confirm.Body
	acceptLabel := confirm.Accept
	if acceptLabel == "" {
		acceptLabel = text.Detail.Confirm
	}
	closeLabel := confirm.Cancel
	if closeLabel == "" {
		closeLabel = text.Detail.Reconsider
	}
	icon := confirm.Icon
	if icon == "" {
		icon = action.Icon
	}
	selected := *action
	p.region(region, nil)
	ui.Column(c).FillWidth().Lang(p.langTag()).Font(p.uiFont()).Children(func() {
		p.framelessHeader(c, "ACTION \u00b7 CONFIRM", icon, heading, note)
		ui.Row(c).Gap(16).Margin(40, 0, 0, 0).Children(func() {
			p.styledButton(c, acceptLabel, icon, videoConfirmAccept, region, func() {
				p.State.Back()
				p.performVideoCustomAction(selected)
			}, p.framelessButtonAppearance())
			p.styledButton(c, closeLabel, "", app.CloseModal, region, func() { p.State.Back() }, p.framelessButtonAppearance())
		})
	})
	_ = colors
}

func (p *PageContext) videoModalButton(c *ui.Context, label, icon string, id app.FocusID, region string, action func()) ui.Element {
	style := p.dialogButtonAppearance()
	style.iconSize, style.iconGap, style.fontWeight, style.focusShadow = 24, 13, 550, true
	return p.styledButton(c, label, icon, id, region, action, style)
}

func (p *PageContext) toggleVideoCommentLike(comment provider.VideoComment) {
	p.withVideoAccount(p.Assets.Text.Detail.Like, func() {
		p.editVideo(provider.VideoEdit{
			Ref: p.host.discussionDetail().item.Ref, Interaction: provider.VideoReplyLike,
			Enabled: !comment.Liked, ReplyID: comment.ID,
		})
	})
}
