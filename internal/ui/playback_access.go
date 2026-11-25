package tvui

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/provider"
)

func playbackHasAccessDialog(state app.PlaybackState) bool {
	return state.Limit != "" || state.PreviewPending || playbackPreviewEnded(state)
}

func playbackPreviewEnded(state app.PlaybackState) bool {
	return state.Clock.Ended && state.Resolution.Plan.Access == provider.VideoAccessPreview
}

func playbackHasWorkRestriction(state app.PlaybackState) bool {
	return state.Resolution.Plan.Access == provider.VideoAccessRestricted &&
		(state.Limit == provider.PlaybackMembership || state.Limit == provider.PlaybackPurchase)
}

func (v *View) PlaybackChanged() {
	if v.Playback == nil {
		return
	}
	s := &v.player
	state := v.Playback.State()
	if state.Limit != provider.PlaybackLogin {
		s.loginAttempted = false
		return
	}
	if v.State.Page != app.Player || s.loginAttempted {
		return
	}
	s.loginAttempted = true
	v.dispatch(func() {
		if state.Clock.HasFrame && s.content.requestQuality != "" {
			s.openPanel(v, playerQuality)
			v.State.Focus = playbackOptionID(playerQuality, s.content.requestQuality)
		}
		if v.openPlaybackSignIn() && state.Clock.HasFrame {
			v.Playback.DismissLimit()
		}
	})
}

func (v *View) openPlaybackSignIn() bool {
	v.openSignIn()
	if !isSignInPage(v.State.Page) {
		return false
	}
	s := &v.player
	s.content.afterLogin = func() {
		if s.content.requestQuality == "" {
			v.Playback.RefreshAccess()
			return
		}
		if s.panel == playerQuality {
			s.closePanel(v)
		}
		if v.Playback.State().Loading {
			s.content.resumeQuality = true
		} else {
			v.Playback.ChooseQuality(s.content.requestQuality)
		}
	}
	return true
}

func (v *View) dismissPlaybackLimit() {
	v.player.show(v)
	v.Playback.DismissLimit()
	v.State.Focus, v.reveal = playerQuality, true
}

func (p *PageContext) playbackDetail() {
	item := p.host.player.current().item
	p.State.Back()
	if p.State.Page != app.VideoDetail || p.host.videoDetail.item.Ref != item.Ref {
		p.openVideoDetail(item)
	}
}

type playbackAccessAction struct {
	label, icon string
	activate    func()
}

type playbackAccessDialog struct {
	title, body, work, instructions     string
	actions                             []playbackAccessAction
	workAccess, returnToDetail, purchase bool
}

func (p *PageContext) playbackAccessDialogFor(state app.PlaybackState, item playbackItem) playbackAccessDialog {
	v, s := p.host, &p.host.player
	text := p.Assets.Text.Playback
	labels := text.Access
	plan := state.Resolution.Plan
	closeDialog := func() {
		if state.Clock.HasFrame && plan.Access != provider.VideoAccessRestricted {
			v.dismissPlaybackLimit()
		} else {
			p.State.Back()
		}
	}
	fallback := func(id string) func() {
		return func() {
			closeDialog()
			if p.State.Page == app.Player && id != "" && id != state.Selected {
				v.Playback.ChooseQuality(id)
			}
		}
	}
	if state.PreviewPending || playbackPreviewEnded(state) || playbackHasWorkRestriction(state) {
		purchase := state.Limit == provider.PlaybackPurchase || plan.PreviewRestriction == provider.PlaybackPurchase
		vipTitle, vipNoPreview, vipBody := labels.VIPTitle, labels.VIPNoPreview, labels.VIPBody
		refreshLabel, instr := labels.Refresh, labels.MembershipInstructions
		if state.AudioOnly {
			if labels.AudioVIPTitle != "" {
				vipTitle = labels.AudioVIPTitle
			}
			if labels.AudioVIPNoPreview != "" {
				vipNoPreview = labels.AudioVIPNoPreview
			}
			if labels.AudioVIPBody != "" {
				vipBody = labels.AudioVIPBody
			}
			if labels.AudioRefresh != "" {
				refreshLabel = labels.AudioRefresh
			}
			if labels.AudioMembershipInstructions != "" {
				instr = labels.AudioMembershipInstructions
			}
		}
		dialog := playbackAccessDialog{
			title: p.sourceText(vipTitle), body: p.sourceText(vipNoPreview), work: item.item.Title,
			workAccess: true, returnToDetail: true, purchase: purchase,
		}
		if purchase {
			dialog.title, dialog.body = p.sourceText(labels.PurchaseTitle), p.sourceText(labels.PurchaseBody)
		}
		if state.PreviewPending {
			if !purchase {
				dialog.body = p.sourceText(vipBody)
			}
			trialMin, trialSec := labels.TrialMinutes, labels.TrialSeconds
			if state.AudioOnly {
				if labels.AudioTrialMinutes != "" {
					trialMin = labels.AudioTrialMinutes
				}
				if labels.AudioTrialSeconds != "" {
					trialSec = labels.AudioTrialSeconds
				}
			}
			trialLabel := fmt.Sprintf(trialMin, max(1, int((state.Duration+30)/60)))
			if state.Duration < 60 {
				trialLabel = fmt.Sprintf(trialSec, max(1, int(state.Duration)))
			}
			dialog.actions = append(dialog.actions, playbackAccessAction{trialLabel, "play-filled", func() {
				s.showAccessHelp = false
				s.show(v)
				v.Playback.StartPreview()
				v.State.Focus, v.reveal = playerToggle, true
			}})
		}
		if playbackPreviewEnded(state) {
			endedTitle, endedBody := labels.TrialEndedTitle, labels.TrialEndedBody
			if state.AudioOnly {
				if labels.AudioTrialEndedTitle != "" {
					endedTitle = labels.AudioTrialEndedTitle
				}
				if labels.AudioTrialEndedBody != "" {
					endedBody = labels.AudioTrialEndedBody
				}
			}
			dialog.title, dialog.body = endedTitle, p.sourceText(endedBody)
			if purchase {
				dialog.body = labels.PurchaseTrialEndedBody
			}
		}
		if v.Auth.Accounts().CurrentID == "" {
			dialog.actions = append(dialog.actions, playbackAccessAction{p.signInActionLabel(), "", func() {
				v.openPlaybackSignIn()
			}})
		}
		if !purchase {
			dialog.actions = append(dialog.actions, playbackAccessAction{labels.MembershipHelp, "", func() { s.showAccessHelp = true }})
			if s.showAccessHelp {
				dialog.instructions = p.sourceText(instr)
			}
		}
		dialog.actions = append(dialog.actions, playbackAccessAction{refreshLabel, "", v.Playback.RefreshAccess})
		return dialog
	}
	switch state.Limit {
	case provider.PlaybackDevice:
		var requestedHDR, fallback4K bool
		for _, quality := range plan.Qualities {
			if quality.ID == s.content.requestQuality {
				requestedHDR = quality.HDR
			}
			if quality.ID == state.Fallback {
				fallback4K = quality.Badge == "4K"
			}
		}
		if requestedHDR && fallback4K {
			return playbackAccessDialog{title: labels.DeviceTitle, body: labels.DeviceBody, actions: []playbackAccessAction{
				{labels.UseSDR, "", fallback(state.Fallback)}, {labels.Keep, "", closeDialog},
			}}
		}
		return playbackAccessDialog{title: labels.DeviceGeneric, body: labels.DeviceGenericBody, actions: []playbackAccessAction{
			{labels.UseBest, "", fallback(state.Fallback)}, {labels.Keep, "", closeDialog},
		}}
	case provider.PlaybackSource:
		if plan.Access == provider.VideoAccessRestricted && len(plan.Qualities) == 0 && state.Fallback == "" {
			return playbackAccessDialog{
				title: labels.SourceUnavailableTitle,
				body:  labels.SourceUnavailableBody,
				actions: []playbackAccessAction{
					{labels.Return, "back", closeDialog},
				},
			}
		}
		title, body := text.SourceLimit, labels.SourceGeneric
		srcTitle, srcBody, useBest := labels.SourceTitle, labels.SourceBody, labels.UseBest
		if state.AudioOnly {
			if text.AudioSourceLimit != "" {
				title = text.AudioSourceLimit
			}
			if labels.AudioSourceGeneric != "" {
				body = labels.AudioSourceGeneric
			}
			if labels.AudioSourceTitle != "" {
				srcTitle = labels.AudioSourceTitle
			}
			if labels.AudioSourceBody != "" {
				srcBody = labels.AudioSourceBody
			}
			if labels.AudioUseBest != "" {
				useBest = labels.AudioUseBest
			}
		}
		if state.Fallback != "" {
			for _, quality := range plan.Qualities {
				if quality.ID == state.Fallback {
					title, body = fmt.Sprintf(srcTitle, quality.Label), fmt.Sprintf(srcBody, quality.Label)
					break
				}
			}
		}
		return playbackAccessDialog{title: title, body: body, actions: []playbackAccessAction{
			{useBest, "", fallback(state.Fallback)}, {p.Assets.Text.Close, "", closeDialog},
		}}
	case provider.PlaybackRegion:
		return playbackAccessDialog{title: labels.RegionTitle, body: labels.RegionBody, actions: []playbackAccessAction{
			{labels.SeeRelated, "", func() {
				if p.State.Navigate(app.Route{Page: app.Home}) {
					v.notice = p.Assets.Text.Detail.Related
				}
			}},
			{p.Assets.Text.Close, "", closeDialog},
		}}
	case provider.PlaybackMembership:
		qTitle, qBody, keepLabel, refreshLabel, instr := labels.QualityVIPTitle, labels.QualityVIPBody, labels.Keep, labels.Refresh, labels.MembershipInstructions
		if state.AudioOnly {
			if labels.AudioQualityVIPTitle != "" {
				qTitle = labels.AudioQualityVIPTitle
			}
			if labels.AudioQualityVIPBody != "" {
				qBody = labels.AudioQualityVIPBody
			}
			if labels.AudioKeep != "" {
				keepLabel = labels.AudioKeep
			}
			if labels.AudioRefresh != "" {
				refreshLabel = labels.AudioRefresh
			}
			if labels.AudioMembershipInstructions != "" {
				instr = labels.AudioMembershipInstructions
			}
		}
		dialog := playbackAccessDialog{
			title: p.sourceText(qTitle),
			body:  fmt.Sprintf(p.sourceText(qBody), playbackQualityLabel(state)),
			actions: []playbackAccessAction{
				{labels.MembershipHelp, "", func() { s.showAccessHelp = true }},
				{refreshLabel, "", func() { v.Playback.ChooseQuality(s.content.requestQuality) }},
				{keepLabel, "", closeDialog},
			},
		}
		if s.showAccessHelp {
			dialog.instructions = p.sourceText(instr)
		}
		return dialog
	default:
		return playbackAccessDialog{title: text.LoginLimit, actions: []playbackAccessAction{
			{p.signInActionLabel(), "", func() { v.openPlaybackSignIn() }}, {p.Assets.Text.Close, "", closeDialog},
		}}
	}
}

func (p *PageContext) drawPlaybackAccess(c *ui.Context, state app.PlaybackState, item playbackItem) {
	v := p.host
	dialog := p.playbackAccessDialogFor(state, item)
	colors := p.Theme.Colors
	if p.active && !strings.HasPrefix(string(v.State.Focus), string(playerStatus)) {
		v.State.Focus, v.reveal = playerStatus, true
	}
	p.region(playerPanelGroup, focusEdges{})
	eyebrow := "PLAYBACK \u00b7 ACCESS"
	icon := "lock"
	if dialog.work != "" {
		if dialog.purchase {
			eyebrow = "ACCESS \u00b7 " + dialog.work
		} else {
			eyebrow = "MEMBERSHIP \u00b7 " + dialog.work
		}
	}
	p.renderPlayerFrameless(c, "playback-access", 860, func() {
		p.framelessHeader(c, eyebrow, icon, dialog.title, dialog.body)
		if dialog.instructions != "" {
			ui.Box(c).Margin(24, 0, 0, 0).FillWidth().Height(1).Background(withAlpha(colors.Text.Primary, 0x1a))
			ui.Text(c, dialog.instructions).FillWidth().Margin(18, 0, 0, 0).FontSize(20).LineHeight(1.7).
				TextColor(paint(colors.Text.Secondary))
		}
		ui.Row(c.Key("playback-access-actions")).FillWidth().Margin(42, 0, 0, 0).Gap(16).Wrap().Children(func() {
			for i, action := range dialog.actions {
				id := playerStatus
				if i > 0 {
					id = app.FocusID(fmt.Sprintf("%s/%d", playerStatus, i))
				}
				style := p.framelessButtonAppearance()
				p.styledButton(c, action.label, action.icon, id, playerPanelGroup, action.activate, style)
			}
		})
		if dialog.returnToDetail {
			style := buttonAppearance{
				height: 46, padding: 12, radius: 10, fontSize: 19, iconSize: 19, iconGap: 10,
				foreground: paint(colors.Text.Secondary),
			}
			p.styledButton(c, p.Assets.Text.Playback.Access.Return, "back", playerStatus+"/detail", playerPanelGroup,
				p.playbackDetail, style).Margin(26, 0, 0, 0)
		}
	})
}

func (p *PageContext) playbackPreviewBanner(c *ui.Context, state app.PlaybackState) {
	colors := p.Theme.Colors
	text := p.Assets.Text.Playback.Access
	ink := paint(colors.Accent.Soft)
	rule := withAlpha(colors.Accent.Primary, 0x55)
	ui.Row(c.Key("preview-remaining")).Absolute().Left(playerInset).Top(157).Height(39).Radius(9).
		Padding(0, 15).Gap(12).AlignItems(ui.Center).
		Background(withAlpha(colors.Overlay.PlayerBottom, 0xc8)).Border(1, rule).Children(func() {
		trialState, trialLeft := text.TrialState, text.TrialLeft
		if state.AudioOnly {
			if text.AudioTrialState != "" {
				trialState = text.AudioTrialState
			}
			if text.AudioTrialLeft != "" {
				trialLeft = text.AudioTrialLeft
			}
		}
		ui.Icon(c, p.Assets.Icon("lock")).Size(18, 18).TextColor(ink)
		ui.Text(c, trialState).FontSize(16).TextColor(ink)
		ui.Box(c).Size(1, 15).Background(rule)
		ui.Text(c, trialLeft+" "+playbackTime(state.Duration-state.Position)).FontSize(16).TextColor(ink)
	})
}
