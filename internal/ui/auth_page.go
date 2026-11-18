package tvui

import (
	"fmt"
	"math"
	"time"

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
	panel := ui.Column(c.Key("auth-qr")).Fill().AlignItems(ui.Center)
	if !actionVisible {
		p.bind(panel.Focusable(), app.QRAction, "qr")
		if moveFocus {
			panel.Focus(ui.FocusOptions{PreventScroll: true})
		}
	}
	panel.Children(func() {
		ui.Text(c, title).FillWidth().Height(52).TextAlign(ui.Center).FontSize(36).FontWeight(550).LineHeight(1.45)
		ui.Box(c).Height(20)
		ui.Text(c, description).FillWidth().MinHeight(36).TextAlign(ui.Center).FontSize(20).LineHeight(1.8).
			TextColor(paint(colors.Text.Secondary))
		ui.Box(c).Height(22)
		ui.Column(c).FillWidth().Grow(1).Center().Children(func() {
			switch snapshot.Phase {
			case app.SignInWaiting:
				ui.Box(c).Size(qrSide, qrSide).Radius(15).Clip().Children(func() {
					ui.Image(c, image).Size(qrSide, qrSide).Label(labels.QR)
				})
				remaining := max(0, int(math.Ceil(time.Until(snapshot.Challenge.ExpiresAt).Seconds())))
				ui.Box(c).Height(24)
				ui.Row(c).Height(qrStatusHeight).Gap(12).AlignItems(ui.Center).Children(func() {
					ui.Box(c).Size(7, 7).Radius(3.5).Background(paint(colors.Accent.Primary))
					ui.Text(c, labels.Waiting).FontSize(19).TextColor(paint(colors.Text.Secondary))
					ui.Text(c, fmt.Sprintf("%02d:%02d", remaining/60, remaining%60)).FontSize(17).
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
				ui.Box(c).Height(24 + qrStatusHeight)
			case app.SignInFailed:
				ui.Text(c, p.authError(snapshot.Error)).FontSize(24).TextColor(paint(colors.Text.Secondary))
			default:
				ui.Text(c, labels.Loading).FontSize(23).TextColor(paint(colors.Text.Muted))
			}
		})
		if snapshot.Phase == app.SignInFailed {
			ui.Box(c).Height(16)
			button := p.actionButton(c, p.Assets.Text.Retry, "refresh", app.QRAction, "qr", restart).
				Height(55).AlignSelf(ui.Center)
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
	snapshot, labels := p.host.Auth.Accounts(), p.Assets.Text.Auth
	colors, d := p.Theme.Colors, p.uiColors()
	ui.Text(c, labels.Accounts).FontSize(36).FontWeight(550)
	if entry, ok := p.State.Current(); ok {
		ui.Text(c, entry.Name+" \u00b7 "+labels.AccountsNote).FontSize(21).TextColor(paint(colors.Text.Secondary))
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
	ui.List(c.Key("saved-accounts"), list, count).FillWidth().Grow(1).
		Padding(0, 0, focusBottomMargin, 0).Rows(func(item ui.ListRow) {
		c := item.Context
		if item.Index == len(snapshot.Accounts) {
			ui.Text(c, status).FontSize(22).TextColor(statusColor)
			return
		}
		account := snapshot.Accounts[item.Index]
		selected := account.ID == snapshot.CurrentID
		foreground := paint(colors.Text.Primary)
		if selected {
			foreground = paint(colors.Accent.Soft)
		}
		row := p.styledButton(c, "", "", app.AccountFocus(account.ID), "accounts", func() {
			p.host.Auth.SelectAccount(account.ID, p.host.dismissAfter(app.AccountPicker))
		}, buttonAppearance{
			height: 60, padding: 18, radius: 10, fontSize: 23,
			background: d.subtleFill, foreground: foreground,
		}).Label(account.Name).FillWidth().Margin(0, 0, 10, 0)
		row.Children(func() {
			ui.Row(c).Fill().AlignItems(ui.Center).Gap(16).Children(func() {
				ui.Text(c, account.Name).Grow(1).MaxLines(1).FontSize(23)
				if selected {
					ui.Icon(c, p.Assets.Icon("check")).Size(26, 26)
				}
			})
		})
	})
	p.styledButton(c, labels.Add, "plus", app.AccountAdd, "accounts", p.host.openSignIn, buttonAppearance{
		height: 56, padding: 18, radius: 12, fontSize: 19, iconSize: 22,
		background: d.secondaryFill, foreground: d.secondaryText,
	})
}

type signOutDialog struct{}

func (signOutDialog) Render(c *ui.Context, p *PageContext) {
	labels, colors := p.Assets.Text.Auth, p.Theme.Colors
	ui.Text(c, labels.SignOutTitle).FontSize(36).FontWeight(550)
	ui.Text(c, labels.SignOutNote).FontSize(21).TextColor(paint(colors.Text.Secondary))
	ui.Row(c).Gap(14).Children(func() {
		p.styledButton(c, labels.SignOut, "", app.ConfirmExit, "sign-out", func() {
			id := signOutID(p.host.Auth.Accounts())
			p.State.Back()
			p.State.Focus = app.AccountAction
			p.host.Auth.SignOut(id)
		}, p.dialogButtonAppearance())
		p.styledButton(c, labels.SignOutCancel, "", app.CloseModal, "sign-out", func() { p.State.Back() }, p.dialogButtonAppearance())
	})
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
		v.dispatch(func() {
			v.State.Back()
			if v.State.Page == app.Account {
				v.State.Focus = app.AccountAction
			}
		})
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
