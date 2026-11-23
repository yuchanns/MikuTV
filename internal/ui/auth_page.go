package tvui

import (
	"fmt"
	"math"
	"strings"
	"time"
	"unicode"

	"github.com/egoist/mygo/ui"
	"github.com/skip2/go-qrcode"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/provider"
)

const (
	qrSide         = 244
	qrStatusHeight = 28
)

type qrSignIn struct{}

func (qrSignIn) Render(c *ui.Context, p *PageContext) {
	snapshot, labels, colors := p.host.Auth.SignIn(), p.Assets.Text.Auth, p.Theme.Colors
	entry, _ := p.State.Current()
	title := entry.Auth.QRTitle
	if title == "" {
		title = labels.ScanLogin
	}
	image := p.host.qrImage
	if snapshot.Phase != app.SignInExpired {
		image = p.host.qrCode(snapshot.Challenge.URL)
	}
	if snapshot.Phase == app.SignInWaiting && image == nil {
		snapshot.Phase = app.SignInFailed
	}
	var description string
	switch snapshot.Phase {
	case app.SignInWaiting:
		description = entry.Auth.QRDescription
		if description == "" {
			description = labels.Body
		}
	case app.SignInExpired:
		description = labels.ExpiredBody
	default:
		description = labels.Secure
	}
	actionVisible := snapshot.Phase == app.SignInExpired || snapshot.Phase == app.SignInFailed
	// The modal stays open while its focus moves between the panel and the button.
	moveFocus := p.active && actionVisible != p.host.qrHasAction
	if p.active {
		p.host.qrHasAction = actionVisible
	}
	restart := func() { p.host.Auth.StartSignIn(p.host.dismissAfter(app.SignInQR)) }
	panel := ui.Column(c.Key("auth-qr")).FillWidth().AlignItems(ui.Center)
	if !actionVisible {
		p.bind(panel.Focusable(), app.QRAction, "qr")
		if moveFocus {
			panel.Focus(ui.FocusOptions{PreventScroll: true})
		}
	}
	panel.Children(func() {
		ui.Row(c).Height(24).Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(24, 2).Radius(1).Background(paint(colors.Accent.Soft))
			ui.Text(c, "SIGN IN").FontSize(14).FontWeight(500).LetterSpacing(3.4).TextColor(paint(colors.Accent.Soft))
			ui.Box(c).Size(24, 2).Radius(1).Background(paint(colors.Accent.Soft))
		})
		ui.Text(c, title).FillWidth().Margin(18, 0, 0, 0).TextAlign(ui.Center).FontSize(50).FontWeight(600).LetterSpacing(1.2)
		ui.Text(c, description).FillWidth().Margin(16, 0, 0, 0).TextAlign(ui.Center).FontSize(22).LineHeight(1.7).
			TextColor(paint(colors.Text.Secondary))
		ui.Box(c).Height(38)
		ui.Column(c).FillWidth().MinHeight(310).Center().Children(func() {
			switch snapshot.Phase {
			case app.SignInWaiting:
				ui.Box(c).Size(qrSide+16, qrSide+16).Radius(20).Padding(8).Center().
					Background(ui.Color{R: 255, G: 255, B: 255, A: 255}).
					Shadow(0, 24, 64, 0, paint(colors.Overlay.Shadow)).Children(func() {
					ui.Box(c).Size(qrSide, qrSide).Radius(12).Clip().Children(func() {
						ui.Image(c, image).Size(qrSide, qrSide).Label(labels.QR)
					})
				})
				remaining := max(0, int(math.Ceil(time.Until(snapshot.Challenge.ExpiresAt).Seconds())))
				ui.Box(c).Height(28)
				ui.Row(c).Height(qrStatusHeight).Gap(12).AlignItems(ui.Center).Children(func() {
					ui.Box(c).Size(8, 8).Radius(4).Background(paint(colors.Accent.Primary))
					ui.Text(c, labels.Waiting).FontSize(20).TextColor(paint(colors.Text.Secondary))
					ui.Text(c, fmt.Sprintf("%02d:%02d", remaining/60, remaining%60)).FontSize(18).
						Margin(0, 0, 0, 6).TextColor(paint(colors.Text.Muted))
				})
			case app.SignInScanned:
				p.authProgress(c, "phone", labels.Scanned, labels.PhoneConfirm)
			case app.SignInSuccess:
				p.authProgress(c, "check", labels.Success, labels.Continuing)
			case app.SignInExpired:
				button := p.expiredQRButton(c, image, restart)
				if moveFocus {
					button.Focus(ui.FocusOptions{PreventScroll: true})
				}
				// Keep the refresh overlay at the same position as the code.
				ui.Box(c).Height(28 + qrStatusHeight)
			case app.SignInFailed:
				ui.Text(c, p.authError(snapshot.Error)).FontSize(24).TextColor(paint(colors.Text.Secondary))
			default:
				ui.Text(c, labels.Loading).FontSize(23).TextColor(paint(colors.Text.Muted))
			}
		})
		if snapshot.Phase == app.SignInFailed {
			ui.Box(c).Height(24)
			button := p.styledButton(c, p.Assets.Text.Retry, "refresh", app.QRAction, "qr", restart, p.framelessButtonAppearance()).
				AlignSelf(ui.Center)
			if moveFocus {
				button.Focus(ui.FocusOptions{PreventScroll: true})
			}
		}
	})
}

func (p *PageContext) expiredQRButton(c *ui.Context, image ui.ImageSource, restart func()) ui.Element {
	labels := p.Assets.Text.Auth
	button := p.button(c, "", app.QRAction, "qr", restart).Label(labels.Refresh).
		Size(qrSide, qrSide).Padding(0).Radius(15).Clip().AlignSelf(ui.Center)
	button.Children(func() {
		if image != nil {
			ui.Image(c, image).Absolute().Left(0).Top(0).Size(qrSide, qrSide).Opacity(0.13).Cache().Blur(3)
		}
		ui.Column(c).Fill().Center().Gap(18).ZIndex(1).Children(func() {
			ui.Icon(c, p.Assets.Icon("refresh")).Size(42, 42)
			ui.Text(c, labels.Expired).FontSize(21).FontWeight(500)
		})
	})
	return button
}

func (p *PageContext) authProgress(c *ui.Context, icon, title, note string) {
	colors := p.Theme.Colors
	fill := uint8(0x18)
	if icon == "phone" {
		fill = 0x12
	}
	ui.Box(c).Size(155, 155).Radius(77.5).Center().Background(withAlpha(colors.Accent.Primary, fill)).
		Border(1, withAlpha(colors.Accent.Primary, 0x25)).Children(func() {
		ui.Icon(c, p.Assets.Icon(icon)).Size(65, 65).TextColor(paint(colors.Accent.Soft))
		if icon == "phone" {
			ui.Box(c).Absolute().Right(8).Bottom(5).Size(44, 44).Radius(22).Center().
				Border(5, paint(colors.Surface.Raised)).Background(paint(colors.Accent.Primary)).Children(func() {
				ui.Icon(c, p.Assets.Icon("check")).Size(23, 23).TextColor(paint(colors.Accent.Foreground))
			})
		}
	})
	ui.Box(c).Height(28)
	ui.Text(c, title).FontSize(29).FontWeight(450).TextColor(paint(colors.Text.Primary))
	ui.Box(c).Height(12)
	ui.Text(c, note).FontSize(20).TextColor(paint(colors.Text.Secondary))
}

func (v *View) qrCode(payload string) ui.ImageSource {
	if v.qrPayload == payload {
		return v.qrImage
	}
	v.qrPayload, v.qrImage = payload, nil
	if payload != "" {
		if code, err := qrcode.New(payload, qrcode.Medium); err == nil {
			v.qrImage = ui.NewBitmap(code.Image(qrSide))
		}
	}
	return v.qrImage
}

type accountPicker struct{}

func (accountPicker) Render(c *ui.Context, p *PageContext) {
	snapshot, labels, modalText := p.host.Auth.Accounts(), p.Assets.Text.Auth, p.Assets.Text.Modal
	colors, d := p.Theme.Colors, p.uiColors()
	entryName := ""
	if entry, ok := p.State.Current(); ok {
		entryName = entry.Name
	}
	if p.host.Catalog != nil && len(snapshot.Accounts) != 0 {
		avatars := make([]string, 0, len(snapshot.Accounts))
		for _, account := range snapshot.Accounts {
			if account.AvatarURL != "" {
				avatars = append(avatars, account.AvatarURL)
			}
		}
		if len(avatars) != 0 {
			p.host.Catalog.PrepareArtwork(avatars)
		}
	}
	p.region("accounts", nil)
	status := ""
	statusColor := paint(colors.Text.Secondary)
	if snapshot.Busy {
		status, statusColor = p.Assets.Text.LoadingTitle, paint(colors.Text.Muted)
	} else if snapshot.Error != "" {
		status = p.authError(snapshot.Error)
	}
	count := len(snapshot.Accounts)
	if status != "" {
		count++
	}
	list := p.listState("saved-accounts")
	list.Key = func(index int) any {
		if index == len(snapshot.Accounts) {
			return true
		}
		return snapshot.Accounts[index].ID
	}
	if p.active && (p.host.reveal || p.host.drawnRevision != p.State.Revision) {
		for index, account := range snapshot.Accounts {
			if p.State.Focus == app.AccountFocus(account.ID) {
				list.ScrollIntoView(index)
				break
			}
		}
	}
	listHeight := min(float32(max(count, 1))*96+8, 560)
	ui.Column(c).Fill().Lang("zh-CN").Font("system-ui, PingFang SC").Children(func() {
		p.drawerHeader(c, "PROFILE \u00b7 SWITCH", labels.Accounts, entryName, labels.AccountsNote)
		ui.List(c.Key("saved-accounts"), list, count).FillWidth().Height(listHeight).
			Padding(0, 0, 8, 0).Rows(func(item ui.ListRow) {
			rc := item.Context
			if item.Index == len(snapshot.Accounts) {
				ui.Text(rc, status).Margin(10, 0, 0, 0).FontSize(20).TextColor(statusColor)
				return
			}
			account := snapshot.Accounts[item.Index]
			selected := account.ID == snapshot.CurrentID
			id := app.AccountFocus(account.ID)
			focused := p.paintFocus() == id
			bg, fg, metaFg := d.subtleFill, paint(colors.Text.Primary), paint(colors.Text.Muted)
			border := ui.Transparent
			if selected {
				bg = withAlpha(colors.Accent.Primary, 0x16)
				border = withAlpha(colors.Accent.Soft, 0x2b)
			}
			if focused {
				bg, fg, metaFg = paint(colors.Focus.Background), paint(colors.Focus.Foreground), d.focusSubtext
				border = ui.Transparent
			}
			row := ui.Button(rc.Key(string(id)), "").Label(account.Name).FillWidth().Height(84).
				Margin(0, 0, 10, 0).Padding(0, 22).Radius(16).Border(1, border).Background(bg).TextColor(fg).
				OnClick(func() {
					p.dispatch(func() {
						p.host.Auth.SelectAccount(account.ID, p.host.dismissAfter(app.AccountPicker))
					})
				})
			p.addVideoFocusShadow(row, focused)
			p.animateFocus(row, bg, fg)
			p.bind(row, id, "accounts")
			row.Children(func() {
				ui.Row(rc).Fill().AlignItems(ui.Center).Gap(18).Children(func() {
					var portrait ui.ImageSource
					if selected && p.host.avatarImage != nil {
						portrait = p.host.avatarImage
					} else if account.AvatarURL != "" {
						portrait = p.catalogCover(account.AvatarURL)
					}
					avStart, avEnd, avFg := d.avatarStart, d.avatarEnd, paint(colors.Text.Primary)
					if selected {
						avStart, avEnd, avFg = paint(colors.Accent.Soft), paint(colors.Accent.Strong), paint(colors.Accent.Foreground)
					}
					avatarBox := ui.Box(rc).Size(48, 48).Radius(24).Shrink(0).Clip().Center()
					if portrait != nil {
						avatarBox.Border(1, d.avatarBorder).Children(func() {
							ui.Image(rc.Key("acct-avatar-"+account.ID), portrait).Fill().Fit(ui.Cover)
						})
					} else {
						avatarBox.Gradient(avStart, avEnd, 135).Children(func() {
							ui.Text(rc, accountInitial(account.Name)).FontSize(20).FontWeight(600).TextColor(avFg)
						})
					}
					ui.Column(rc).Grow(1).MinWidth(0).Gap(4).Children(func() {
						ui.Text(rc, account.Name).MaxLines(1).FontSize(22).FontWeight(550)
						sub := modalText.SwitchNote
						if selected {
							sub = modalText.ActiveNote
						}
						ui.Text(rc, sub).MaxLines(1).FontSize(15).TextColor(metaFg)
					})
					if selected && !focused {
						ui.Text(rc, modalText.Current).Padding(5, 12).Radius(999).FontSize(13).
							Background(withAlpha(colors.Accent.Primary, 0x28)).TextColor(paint(colors.Accent.Soft))
					} else if selected {
						ui.Icon(rc, p.Assets.Icon("check")).Size(22, 22)
					}
				})
			})
		})
		ui.Row(c).Margin(18, 0, 0, 0).Children(func() {
			p.styledButton(c, labels.Add, "plus", app.AccountAdd, "accounts", p.host.openSignIn, p.drawerButtonAppearance())
		})
		ui.Spacer(c).Grow(1)
		p.drawerFooter(c, modalText.HintSelect, modalText.HintConfirm)
	})
}

type signOutDialog struct{}

func (signOutDialog) Render(c *ui.Context, p *PageContext) {
	labels := p.Assets.Text.Auth
	p.region("sign-out", nil)
	ui.Column(c).FillWidth().Lang("zh-CN").Font("system-ui, PingFang SC").Children(func() {
		p.framelessHeader(c, "ACCOUNT \u00b7 SIGN OUT", "user", labels.SignOutTitle, labels.SignOutNote)
		ui.Row(c).Margin(40, 0, 0, 0).Gap(16).Children(func() {
			p.styledButton(c, labels.SignOut, "", app.ConfirmExit, "sign-out", func() {
				id := signOutID(p.host.Auth.Accounts())
				p.State.Back()
				p.State.Focus = app.AccountAction
				p.host.Auth.SignOut(id)
			}, p.framelessButtonAppearance())
			p.styledButton(c, labels.SignOutCancel, "", app.CloseModal, "sign-out", func() { p.State.Back() }, p.framelessButtonAppearance())
		})
	})
}

func accountInitial(name string) string {
	for _, r := range strings.TrimSpace(name) {
		return string(unicode.ToUpper(r))
	}
	return "?"
}

func signOutID(snapshot app.AccountsSnapshot) string {
	if snapshot.CurrentID != "" {
		return snapshot.CurrentID
	}
	return snapshot.SelectedID
}

func (v *View) PromptSignIn() { v.dispatch(v.openSignIn) }

func (v *View) openSignIn() {
	if v.Auth.StartSignIn(v.dismissAfter(app.SignInQR)) {
		if v.State.Page == app.AccountPicker {
			v.State.Back()
		}
		v.State.Push(app.Route{Page: app.SignInQR})
	}
}

func (v *View) dismissAfter(page app.Page) func() {
	return func() {
		if v.State.Page != page {
			return
		}
		var resume func()
		var resumeCreator func()
		var resumeSeries func()
		var resumePlayer func()
		if page == app.SignInQR {
			resume = v.videoDetail.signInAction
			if v.creator != nil {
				resumeCreator = v.creator.signInAction
			}
			resumeSeries = v.series.signInAction
			resumePlayer = v.player.content.afterLogin
		}
		v.dispatch(func() {
			v.State.Back()
			if v.State.Page == app.Account {
				v.State.Focus = app.AccountAction
			}
		})
		if resume != nil {
			v.resumeVideoAfterSignIn(resume)
		}
		if resumeCreator != nil {
			v.resumeCreatorAfterSignIn(resumeCreator)
		}
		if resumeSeries != nil {
			v.resumeSeriesAfterSignIn(resumeSeries)
		}
		if resumePlayer != nil && v.State.Page == app.Player {
			v.dispatch(resumePlayer)
		}
	}
}

func (p *PageContext) authError(code provider.ErrorCode) string {
	switch code {
	case provider.Network, provider.Timeout, provider.RateLimited:
		return p.Assets.Text.Auth.NetworkFailed
	case provider.NeedsSignIn, provider.Expired:
		return p.Assets.Text.Auth.AccountExpired
	default:
		return p.Assets.Text.Auth.Failed
	}
}
