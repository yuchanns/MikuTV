package tvui

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/provider"
)

var detailInteractions = map[app.FocusID]provider.VideoInteraction{
	videoDetailLater: provider.VideoWatchLater, videoDetailFollow: provider.VideoFollow,
	videoDetailLike: provider.VideoLike, videoDetailCoin: provider.VideoCoin,
	videoDetailFavorite: provider.VideoFavorite, videoDetailTriple: provider.VideoTriple,
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
		switch interaction {
		case provider.VideoCoin, provider.VideoTriple:
			p.showVideoConfirmation(interaction)
		default:
			p.performVideoInteraction(interaction)
		}
	})
}

func (p *PageContext) withVideoAccount(label string, action func()) {
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
			if request.Interaction == provider.VideoCoin || request.Interaction == provider.VideoTriple {
				resume = func() { p.showVideoConfirmation(request.Interaction) }
			}
			if p.State.Page != app.VideoDetail || !p.reopenVideoSignIn(err, resume) {
				detail.feedback = videoDetailFeedback{text: p.videoOperationError(err, text.UpdateFailed)}
			}
			return
		}
		detail.applyVideoEdit(request, result)
		if feedback := videoEditFeedback(text, request, result); feedback.text != "" {
			detail.feedback = feedback
		}
	})
}

func videoEditFeedback(text detailLabels, request provider.VideoEdit, result provider.VideoEditResult) videoDetailFeedback {
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
	case provider.VideoCoin:
		return videoDetailFeedback{text: text.CoinSent}
	case provider.VideoTriple:
		return videoDetailFeedback{text: text.TripleSent, celebration: true}
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
	if result.Coins != nil {
		detail.Engagement.Coins = *result.Coins
	} else if request.Interaction == provider.VideoCoin {
		detail.Engagement.Coins++
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

func (p *PageContext) showVideoConfirmation(interaction provider.VideoInteraction) {
	if p.State.Push(app.Route{Page: app.VideoConfirm}) {
		p.host.videoDetail.confirm = interaction
	}
}

func (videoConfirmPage) Render(c *ui.Context, p *PageContext) {
	const region = "video-confirm"
	detail, text, colors := &p.host.videoDetail, p.Assets.Text, p.Theme.Colors
	heading, note, action, icon := text.Detail.ConfirmCoin, text.Detail.CoinNote, text.Detail.Confirm, "detail-primary/coin"
	closeLabel := text.Detail.Reconsider
	interaction := detail.confirm
	if interaction == provider.VideoTriple {
		action, icon = text.Detail.Triple, "detail-primary/like"
	}
	p.region(region, nil)
	ui.Column(c).FillWidth().Children(func() {
		ui.Text(c, heading).FontSize(36).FontWeight(500).LineHeight(1.3).Translate(0, 4).Role(ui.RoleHeading).Level(2)
		ui.Text(c, note).FillWidth().Margin(18, 0, 0, 0).FontSize(21).LineHeight(1.8).Translate(0, 4).
			TextColor(paint(colors.Text.Secondary))
		ui.Row(c).Gap(12).Margin(38, 0, 0, 0).Children(func() {
			p.videoModalButton(c, action, icon, videoConfirmAccept, region, func() {
				p.State.Back()
				p.performVideoInteraction(interaction)
			})
			p.videoModalButton(c, closeLabel, "", app.CloseModal, region, func() { p.State.Back() })
		})
	})
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
