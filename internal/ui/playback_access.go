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
	if v.State.Page != app.SignInQR {
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
	title, body, work, instructions string
	actions                         []playbackAccessAction
	workAccess, returnToDetail      bool
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
		dialog := playbackAccessDialog{
			title: p.sourceText(labels.VIPTitle), body: p.sourceText(labels.VIPNoPreview), work: item.item.Title, workAccess: true,
		}
		purchase := state.Limit == provider.PlaybackPurchase || plan.PreviewRestriction == provider.PlaybackPurchase
		if purchase {
			dialog.title, dialog.body = p.sourceText(labels.PurchaseTitle), p.sourceText(labels.PurchaseBody)
		}
		if state.PreviewPending {
			if !purchase {
				dialog.body = p.sourceText(labels.VIPBody)
			}
			trialLabel := fmt.Sprintf(labels.TrialMinutes, max(1, int((state.Duration+30)/60)))
			if state.Duration < 60 {
				trialLabel = fmt.Sprintf(labels.TrialSeconds, max(1, int(state.Duration)))
			}
			dialog.actions = append(dialog.actions, playbackAccessAction{trialLabel, "play-filled", func() {
				s.showAccessHelp = false
				s.show(v)
				v.Playback.StartPreview()
				v.State.Focus, v.reveal = playerToggle, true
			}})
		}
		if playbackPreviewEnded(state) {
			dialog.title, dialog.body = labels.TrialEndedTitle, p.sourceText(labels.TrialEndedBody)
			if purchase {
				dialog.body = labels.PurchaseTrialEndedBody
			}
		}
		if v.Auth.Accounts().CurrentID == "" {
			dialog.actions = append(dialog.actions, playbackAccessAction{p.Assets.Text.Activity.SignInAction, "", func() {
				v.openPlaybackSignIn()
			}})
		}
		if !purchase {
			dialog.returnToDetail = true
			dialog.actions = append(dialog.actions, playbackAccessAction{labels.MembershipHelp, "", func() { s.showAccessHelp = true }})
			if s.showAccessHelp {
				dialog.instructions = p.sourceText(labels.MembershipInstructions)
			}
		}
		dialog.actions = append(dialog.actions, playbackAccessAction{labels.Refresh, "", v.Playback.RefreshAccess})
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
		title, body := text.SourceLimit, labels.SourceGeneric
		if state.Fallback != "" {
			for _, quality := range plan.Qualities {
				if quality.ID == state.Fallback {
					title, body = fmt.Sprintf(labels.SourceTitle, quality.Label), fmt.Sprintf(labels.SourceBody, quality.Label)
					break
				}
			}
		}
		return playbackAccessDialog{title: title, body: body, actions: []playbackAccessAction{
			{labels.UseBest, "", fallback(state.Fallback)}, {p.Assets.Text.Close, "", closeDialog},
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
		dialog := playbackAccessDialog{
			title: p.sourceText(labels.QualityVIPTitle),
			body:  fmt.Sprintf(p.sourceText(labels.QualityVIPBody), playbackQualityLabel(state)),
			actions: []playbackAccessAction{
				{labels.MembershipHelp, "", func() { s.showAccessHelp = true }},
				{labels.Refresh, "", func() { v.Playback.ChooseQuality(s.content.requestQuality) }},
				{labels.Keep, "", closeDialog},
			},
		}
		if s.showAccessHelp {
			dialog.instructions = p.sourceText(labels.MembershipInstructions)
		}
		return dialog
	default:
		return playbackAccessDialog{title: text.LoginLimit, actions: []playbackAccessAction{
			{p.Assets.Text.Activity.SignInAction, "", func() { v.openPlaybackSignIn() }}, {p.Assets.Text.Close, "", closeDialog},
		}}
	}
}

func (p *PageContext) drawPlaybackAccess(c *ui.Context, state app.PlaybackState, item playbackItem) {
	v := p.host
	dialog := p.playbackAccessDialogFor(state, item)
	colors, d := p.Theme.Colors, p.uiColors()
	width, height := float32(700), float32(300)
	bodyTop, actionsTop := float32(22), float32(34)
	bodySpacing := float32(1.65)
	if dialog.workAccess {
		width, height = 820, 393
		bodyTop, actionsTop, bodySpacing = 21, 32, 1.82
		if dialog.returnToDetail {
			height += 63
		}
	}
	if dialog.instructions != "" {
		height += 90
	}
	if p.active && !strings.HasPrefix(string(v.State.Focus), string(playerStatus)) {
		v.State.Focus, v.reveal = playerStatus, true
	}
	p.region(playerPanelGroup, focusEdges{})
	ui.Box(c.Key("access-scrim")).Absolute().Top(0).Left(0).Fill().Background(paint(colors.Overlay.Scrim))
	ui.Column(c.Key("playback-access")).Absolute().Left((p.Width-width)/2).Top((p.Height-height)/2).
		Size(width, height).Padding(50).Radius(30).Modal().
		Gradient(d.dialogStart, d.dialogEnd, 140).
		Border(1, d.strongBorder).Shadow(0, 50, 100, 0, paint(colors.Overlay.Shadow)).Children(func() {
		ui.Text(c, dialog.title).FontSize(36).FontWeight(500).FillWidth().MaxLines(2)
		if dialog.work != "" {
			ui.Row(c).FillWidth().Margin(22, 0, 0, 0).Gap(12).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, p.Assets.Icon("lock")).Size(23, 23).TextColor(paint(colors.Accent.Soft))
				ui.Text(c, dialog.work).FontSize(20).TextColor(paint(colors.Accent.Soft)).MaxLines(1)
			})
		}
		if dialog.body != "" {
			ui.Text(c, dialog.body).FillWidth().Margin(bodyTop, 0, 0, 0).FontSize(21).LineHeight(bodySpacing).
				TextColor(paint(colors.Text.Secondary))
		}
		if dialog.instructions != "" {
			ui.Box(c).Margin(18, 0, 0, 0).FillWidth().Height(1).Background(withAlpha(colors.Text.Primary, 0x18))
			ui.Text(c, dialog.instructions).FillWidth().Margin(16, 0, 0, 0).FontSize(19).
				TextColor(paint(colors.Text.Secondary))
		}
		ui.Row(c.Key("playback-access-actions")).FillWidth().Margin(actionsTop, 0, 0, 0).Gap(12).Wrap().Children(func() {
			for i, action := range dialog.actions {
				id := playerStatus
				if i > 0 {
					id = app.FocusID(fmt.Sprintf("%s/%d", playerStatus, i))
				}
				style := p.dialogButtonAppearance()
				style.fontWeight = 550
				style.iconSize, style.iconGap = 22, 11
				if i == 0 && p.paintFocus() != id {
					style.background, style.foreground = paint(colors.Focus.Background), paint(colors.Focus.Foreground)
				}
				p.styledButton(c, action.label, action.icon, id, playerPanelGroup, action.activate, style)
			}
		})
		if dialog.returnToDetail {
			style := buttonAppearance{
				height: 44, radius: 9, fontSize: 18, foreground: paint(colors.Text.Secondary),
			}
			p.styledButton(c, p.Assets.Text.Playback.Access.Return, "", playerStatus+"/detail", playerPanelGroup,
				p.playbackDetail, style).Margin(19, 0, 0, 0)
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
		ui.Icon(c, p.Assets.Icon("lock")).Size(18, 18).TextColor(ink)
		ui.Text(c, text.TrialState).FontSize(16).TextColor(ink)
		ui.Box(c).Size(1, 15).Background(rule)
		ui.Text(c, text.TrialLeft+" "+playbackTime(state.Duration-state.Position)).FontSize(16).TextColor(ink)
	})
}
