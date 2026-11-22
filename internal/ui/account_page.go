package tvui

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/provider"
)

type accountPage struct{}

const (
	accountLanguage   = "zh-CN"
	accountFontFamily = "system-ui, PingFang SC"
	accountTextStroke = 0.018
	accountHeader     = "account-header"
	accountProfile    = "account-profile"
	accountBenefits   = "account-benefits"
	accountShortcuts  = "account-shortcuts"
	accountSync       = "account-sync"
	accountFooter     = "account-footer"
)

func accountText(c *ui.Context, text string, size float32, color ui.Color) ui.Element {
	// Match the browser's stroke coverage at TV pixel density.
	return ui.Text(c, text).FontSize(size).TextColor(color).TextStroke(size*accountTextStroke, color)
}

func (p *PageContext) accountButton(c *ui.Context, label, icon string, id app.FocusID, region string, action func(), style buttonAppearance) ui.Element {
	style.textStroke = style.fontSize * accountTextStroke
	return p.styledButton(c, label, icon, id, region, action, style)
}

func (accountPage) Render(c *ui.Context, p *PageContext) {
	if p.active && p.State.Focus == "" {
		p.State.Focus, p.State.Area = app.AccountAction, app.ContentArea
		p.host.reveal = true
	}
	labels, colors := p.Assets.Text.Auth, p.Theme.Colors
	entry, _ := p.State.Current()
	subtitle := entry.Auth.AccountSubtitle
	if subtitle == "" {
		subtitle = entry.Name
	}
	ui.Box(c).FillWidth().Height(139).Lang(accountLanguage).Font(accountFontFamily).Children(func() {
		ui.Column(c).Padding(1, 0, 0, 0).Gap(20).Children(func() {
			accountText(c, labels.AccountTitle, 54, paint(colors.Text.Primary)).Height(75).FontWeight(550).LetterSpacing(2)
			accountText(c, subtitle, 22, paint(colors.Text.Muted)).LineHeight(1.7)
		})
		p.accountInbox(c)
	})
	p.region(accountHeader, focusEdges{app.Down: {Focus: app.AccountAction}})
	p.region(accountProfile, focusEdges{app.Up: {Focus: app.AccountInbox}})
	snapshot := p.host.Auth.Accounts()
	if account, ok := currentAccount(snapshot); ok {
		p.connectedAccount(c, account, snapshot)
	} else {
		p.guestAccount(c, entry, snapshot)
	}
}

func (p *PageContext) guestAccount(c *ui.Context, entry app.Entry, snapshot app.AccountsSnapshot) {
	const cardHeight = 500
	labels, colors := p.Assets.Text.Auth, p.Theme.Colors
	base := paint(colors.Surface.Panel)
	title := entry.Auth.GuestTitle
	if title == "" {
		title = p.Assets.Text.Nav[app.Account]
	}
	ui.Box(c.Key("account-guest")).FillWidth().Height(cardHeight).Lang(accountLanguage).Font(accountFontFamily).Radius(24).Clip().
		Border(1, paint(colors.Border.Subtle)).Background(base).Children(func() {
		if artwork := p.Assets.AccountArtwork(entry.ID); artwork != nil {
			artworkWidth := (p.Width - 186) * 0.63
			ui.Image(c, artwork).Absolute().Right(0).Top(0).Width(artworkWidth).Height(cardHeight).
				Fit(ui.Cover).ObjectPosition(0.5, 0.42).Opacity(0.84)
			ui.Box(c).Absolute().Right(0).Top(0).Width(artworkWidth).Height(cardHeight).ZIndex(1).LinearGradient(ui.LinearGradient{
				Angle: 90, Stops: []ui.GradientStop{
					{Color: base},
					{Color: withAlpha(colors.Surface.Panel, 0x7d), At: 0.4},
					{Color: withAlpha(colors.Surface.Panel, 0), At: 1},
				},
			})
		}
		ui.Column(c).Absolute().Left(0).Top(0).ZIndex(2).Padding(50, 58).Children(func() {
			ui.Box(c).Size(70, 70).Center().Radius(35).Background(withAlpha(colors.Text.Primary, 0x0b)).Children(func() {
				ui.Icon(c, p.Assets.Icon("user")).Size(35, 35).TextColor(paint(colors.Text.Secondary))
			})
			ui.Box(c).Height(28)
			ui.Text(c, title).MinHeight(63).FontSize(45).FontWeight(600)
			if note := entry.Auth.GuestDescription; note != "" {
				ui.Box(c).Height(22)
				ui.Text(c, note).FontSize(23).LineHeight(1.8).TextColor(paint(colors.Text.Secondary))
			}
			ui.Box(c).Height(30)
			ui.Row(c).Gap(16).Children(func() {
				label, action := labels.ScanLogin, p.host.openSignIn
				if !snapshot.Ready && !snapshot.Busy {
					label, action = p.Assets.Text.Retry, p.host.Auth.Load
				}
				p.actionButton(c, label, "user", app.AccountAction, accountProfile, action)
				if len(snapshot.Accounts) != 0 {
					p.actionButton(c, labels.Accounts, "user", app.AccountSwitch, accountProfile, p.openAccountPicker)
				}
			})
			if snapshot.Error != "" {
				ui.Text(c, p.authError(snapshot.Error)).FontSize(19).TextColor(paint(colors.Text.Secondary))
			}
		})
	})
}

func (p *PageContext) connectedAccount(c *ui.Context, account provider.Account, snapshot app.AccountsSnapshot) {
	p.accountFocusRegions()
	ui.Column(c).FillWidth().Lang(accountLanguage).Font(accountFontFamily).Children(func() {
		ui.Row(c.Key("connected-account")).FillWidth().Gap(35).Children(func() {
			p.accountProfile(c, account)
			if account.Card.Title != "" {
				p.accountInfoCard(c, account.Card)
			}
		})
		p.accountSync(c)
		p.accountFooter(c, snapshot)
	})
}

func (p *PageContext) accountFocusRegions() {
	profile := focusDestination{Focus: app.AccountAction}
	inbox := focusDestination{Focus: app.AccountInbox}
	exit := focusDestination{Focus: app.AccountExit}
	lower, aboveExit := exit, profile
	if activity := p.host.Activity; activity != nil && activity.Available(provider.AccountHistory) {
		lower = focusDestination{Focus: app.AccountSync}
		aboveExit = lower
	}
	shortcuts := profile
	for _, section := range librarySections {
		if p.host.Activity != nil && p.host.Activity.Available(section) {
			shortcuts = focusDestination{Region: accountShortcuts}
			break
		}
	}
	next := lower
	if shortcuts.Region != "" {
		next = shortcuts
		if aboveExit == profile {
			aboveExit = shortcuts
		}
	}
	p.region(accountProfile, focusEdges{app.Up: inbox, app.Right: {Focus: app.AccountInfo}, app.Down: next})
	p.region(accountBenefits, focusEdges{app.Left: profile, app.Up: inbox, app.Down: next})
	p.region(accountShortcuts, focusEdges{app.Up: profile, app.Down: lower})
	p.region(accountSync, focusEdges{app.Up: shortcuts, app.Down: exit})
	p.region(accountFooter, focusEdges{app.Up: aboveExit})
}

func (p *PageContext) accountProfile(c *ui.Context, account provider.Account) {
	labels, colors, d := p.Assets.Text.Auth, p.Theme.Colors, p.uiColors()
	ui.Box(c).Grow(1).MinWidth(0).Height(345).Radius(22).Clip().
		Border(1, d.subtleBorder).Background(d.subtleFill).Children(func() {
		ui.Image(c, p.host.profileGlow).Absolute().Left(0).Top(0).Fill().Fit(ui.FillBox)
		ui.Row(c).FillWidth().ZIndex(1).Padding(43, 184, 30, 35).Gap(25).AlignItems(ui.Center).Children(func() {
			p.accountAvatar(c, account, 102, 46)
			ui.Column(c).Grow(1).MinWidth(0).Children(func() {
				accountText(c, labels.Connected, 17, paint(colors.Text.Muted)).Height(20)
				ui.Box(c).Height(10)
				accountText(c, account.Name, 43, paint(colors.Text.Primary)).Height(51).FontWeight(500).LetterSpacing(0.2).MaxLines(1)
				if badge := account.Badge; badge.Text != "" {
					ui.Box(c).Height(20)
					ui.Row(c).Gap(7).AlignItems(ui.Center).Children(func() {
						if badge.Icon != "" {
							ui.Icon(c, p.Assets.Icon(badge.Icon)).Size(16, 16).TextColor(paint(colors.Text.Secondary))
						}
						accountText(c, badge.Text, 15, paint(colors.Text.Secondary)).MaxLines(1)
					})
				}
			})
		})
		p.accountButton(c, labels.Accounts, "user", app.AccountAction, accountProfile, p.openAccountPicker, buttonAppearance{
			height: 50, padding: 16, radius: 25, fontSize: 17, fontWeight: 400, iconSize: 19, iconGap: 9,
			foreground: paint(colors.Text.Secondary), border: d.subtleBorder,
		}).Absolute().Right(33).Top(67).ZIndex(1)
		p.accountShortcuts(c)
	})
}

func (p *PageContext) accountInbox(c *ui.Context) {
	activity := p.host.Activity
	hasNotifications := activity != nil && activity.Available(provider.AccountNotifications)
	hasPrivate := p.host.PrivateMessages != nil && p.host.PrivateMessages.Available()
	if !hasNotifications && !hasPrivate {
		return
	}
	labels, d := p.Assets.Text.Activity, p.uiColors()
	label, accessible := labels.Messages, labels.Messages
	totalUnread := 0
	if hasNotifications {
		snapshot := activity.Snapshot(provider.AccountQuery{Section: provider.AccountNotifications, Summary: true})
		if unread := snapshot.Page.Unread; unread != nil && *unread > 0 {
			totalUnread += *unread
		}
	}
	if hasPrivate {
		totalUnread += p.host.PrivateMessages.UnreadCount()
	}
	if totalUnread > 0 {
		label += " \u00b7 " + strconv.Itoa(totalUnread)
		accessible = fmt.Sprintf(labels.MessagesUnread, totalUnread)
	}
	p.accountButton(c, label, "comment", app.AccountInbox, accountHeader, func() {
		if hasNotifications {
			p.host.messagesTab = messagesTabNotifications
			activity.SetFilter(provider.NoticeAll)
		} else {
			p.host.messagesTab = messagesTabPrivate
		}
		p.State.Push(app.Route{Page: app.AccountMessages})
	}, buttonAppearance{
		height: 53, padding: 23, radius: 12, fontSize: 20, fontWeight: 550, iconSize: 28, iconGap: 13,
		background: d.secondaryFill, foreground: d.secondaryText,
		border: d.secondaryBorder,
	}).Label(accessible).Absolute().Right(0).Top(25)
}

func (p *PageContext) accountShortcuts(c *ui.Context) {
	activity := p.host.Activity
	sections := make([]provider.AccountSection, 0, len(librarySections))
	for _, section := range librarySections {
		if activity.Available(section) {
			sections = append(sections, section)
		}
	}
	if len(sections) == 0 {
		return
	}
	l, colors, d := p.Assets.Text.Activity, p.Theme.Colors, p.uiColors()
	ui.Column(c).Absolute().Left(35).Right(35).Top(205).ZIndex(1).Gap(25).Children(func() {
		ui.Box(c).FillWidth().Height(1).Background(d.subtleBorder)
		ui.Grid(c).FillWidth().Columns(len(sections)).Gap(12).Children(func() {
			for _, section := range sections {
				label, icon := l.section(section)
				query := provider.AccountQuery{Section: section}
				snapshot := activity.Snapshot(query)
				value := l.Unavailable
				if snapshot.Phase == app.CatalogReady {
					value = strconv.Itoa(len(snapshot.Page.Rows))
					if snapshot.Page.Total != nil {
						value = strconv.Itoa(*snapshot.Page.Total)
					} else if snapshot.Page.NextCursor != "" {
						value += "+"
					}
				}
				id := app.FocusID("account/shortcut/" + section)
				button := p.styledButton(c, "", "", id, accountShortcuts, func() {
					p.State.Push(libraryRoute(section))
				}, buttonAppearance{height: 77, padding: 14, radius: 12, fontSize: 25,
					foreground: paint(colors.Text.Primary)}).FillWidth().Label(label + " " + value)
				iconColor, textColor, valueColor := paint(colors.Text.Secondary), paint(colors.Text.Muted), paint(colors.Text.Primary)
				if p.State.Focus == id {
					iconColor, textColor = d.focusSubtext, d.focusSubtext
					valueColor = paint(colors.Focus.Foreground)
				}
				button.Children(func() {
					ui.Row(c).Fill().AlignItems(ui.Center).Gap(14).Children(func() {
						ui.Box(c).Size(25, 23).Center().Children(func() {
							ui.Icon(c, p.Assets.Icon(icon)).Size(23, 23).TextColor(iconColor)
						})
						ui.Column(c).AlignItems(ui.Start).Children(func() {
							accountText(c, value, 25, valueColor).FontWeight(450)
							accountText(c, label, 15, textColor)
						})
					})
				})
			}
		})
	})
}

func (p *PageContext) accountSync(c *ui.Context) {
	activity := p.host.Activity
	if !activity.Available(provider.AccountHistory) {
		ui.Box(c).Height(198)
		return
	}
	labels, colors, d := p.Assets.Text.Activity, p.Theme.Colors, p.uiColors()
	status := labels.SyncOff
	paused := activity.Snapshot(provider.AccountQuery{Section: provider.AccountHistory}).Page.Paused
	if activity.HistorySync() && (paused == nil || !*paused) {
		status = labels.SyncOn
	}
	ui.Box(c).Height(34)
	ui.Row(c).FillWidth().Height(130).Padding(31, 34).AlignItems(ui.Center).Radius(19).
		Border(1, d.subtleBorder).Background(d.subtleFill).Children(func() {
		ui.Row(c).Grow(1).Gap(23).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(49, 49).Radius(24.5).Center().Background(withAlpha(colors.Text.Secondary, 0x12)).Children(func() {
				ui.Icon(c, p.Assets.Icon("history")).Size(25, 25).TextColor(paint(colors.Text.Secondary))
			})
			ui.Column(c).Grow(1).Gap(10).Children(func() {
				foreground := paint(colors.Text.Primary)
				ui.Text(c, status).FontSize(23).MinHeight(32).FontWeight(450).
					TextColor(foreground).TextStroke(0.25, foreground)
				accountText(c, labels.SyncNote, 17, paint(colors.Text.Muted)).MinHeight(24)
			})
		})
		p.accountButton(c, labels.SyncAction, "settings", app.AccountSync, accountSync, func() {
			if p.State.Push(app.Route{Page: app.Settings, Section: "privacy"}) {
				p.State.Focus = app.HistorySync
			}
		}, buttonAppearance{
			height: 50, padding: 20, radius: 12, fontSize: 18, fontWeight: 550, iconSize: 28, iconGap: 13,
			background: d.secondaryFill, foreground: d.secondaryText,
			border: d.secondaryBorder,
		}).AlignSelf(ui.Center)
	})
	ui.Box(c).Height(34)
}

func (p *PageContext) accountFooter(c *ui.Context, snapshot app.AccountsSnapshot) {
	labels, colors := p.Assets.Text.Auth, p.Theme.Colors
	ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
		ui.Row(c).Grow(1).Gap(9).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(6, 6).Radius(3).Background(paint(colors.Status.Success.Foreground))
			accountText(c, labels.AccountStatus+" · "+labels.AccountNormal, 17, paint(colors.Text.Muted))
		})
		p.accountButton(c, labels.SignOut, "logout", app.AccountExit, accountFooter, func() {
			if !p.host.Auth.Accounts().Busy {
				p.State.Push(app.Route{Page: app.SignOut})
			}
		}, buttonAppearance{
			height: 45, padding: 14, radius: 8, fontSize: 17, fontWeight: 400, iconSize: 19, foreground: paint(colors.Text.Muted),
		})
	})
	if snapshot.Error != "" {
		ui.Text(c, p.authError(snapshot.Error)).FontSize(19).TextColor(paint(colors.Text.Secondary))
	}
}

func (p *PageContext) accountInfoCard(c *ui.Context, card provider.AccountCard) {
	colors, d := p.Theme.Colors, p.uiColors()
	ui.Box(c).Width(min(550, (p.Width-186)*0.40)).Height(345).Shrink(0).Radius(22).Clip().
		Border(1, withAlpha(colors.Accent.Primary, 0x35)).
		Gradient(d.accountCardFrom, d.accountCardTo, 135).Children(func() {
		ui.Image(c, p.host.cardGlow).Absolute().Top(0).Left(0).Fill().Fit(ui.FillBox)
		ui.Column(c).FillWidth().ZIndex(1).Padding(33, 35, 30, 35).Children(func() {
			ui.Row(c).FillWidth().Height(28).Gap(10).AlignItems(ui.Center).Children(func() {
				if card.Icon != "" {
					ui.Icon(c, p.Assets.Icon(card.Icon)).Size(26, 26).TextColor(paint(colors.Accent.Soft))
				}
				accountText(c, card.Title, 20, paint(colors.Accent.Soft)).Grow(1).MinWidth(0).MaxLines(1).LetterSpacing(2)
				if card.Status != "" && card.Status != card.ActionLabel {
					accountText(c, card.Status, 18, p.accountCardStatusColor(card)).MaxLines(1)
				}
			})
			if card.Description != "" {
				ui.Box(c).Height(29)
				accountText(c, card.Description, 31, paint(colors.Text.Primary)).MinHeight(46.5).MaxLines(2).FontWeight(500).LetterSpacing(1).
					LineHeight(1.5)
			}
			if len(card.Benefits) != 0 {
				ui.Box(c).Height(17)
				p.accountBenefits(c, card)
			}
			if card.ActionLabel != "" {
				ui.Box(c).Height(27)
				p.accountButton(c, card.ActionLabel, "", app.AccountInfo, accountBenefits, func() {
					p.State.Push(app.Route{Page: app.AccountDetails})
				}, buttonAppearance{
					height: 49, padding: 10, radius: 6, fontSize: 18, fontWeight: 400, foreground: paint(colors.Accent.Soft),
				})
			}
		})
	})
}

func accountGlow(tint ui.Color, center, stop float64) ui.ImageSource {
	// A normalized texture keeps the prototype's elliptical falloff when the card changes width.
	const side = 128
	texture := image.NewNRGBA(image.Rect(0, 0, side, side))
	radiusX, radiusY := max(center, 1-center)*math.Sqrt2, math.Sqrt2
	for y := range side {
		for x := range side {
			dx := (float64(x)/(side-1) - center) / radiusX
			dy := float64(y) / (side - 1) / radiusY
			alpha := math.Round(float64(tint.A) * max(0, 1-math.Hypot(dx, dy)/stop))
			texture.SetNRGBA(x, y, color.NRGBA{R: tint.R, G: tint.G, B: tint.B, A: uint8(alpha)})
		}
	}
	return ui.NewBitmap(texture)
}

func (p *PageContext) accountBenefits(c *ui.Context, card provider.AccountCard) {
	color := paint(p.Theme.Colors.Text.Secondary)
	ui.Row(c).FillWidth().MinHeight(21).Gap(20).Wrap().Children(func() {
		for _, benefit := range card.Benefits {
			ui.Row(c).Gap(5).AlignItems(ui.Center).Children(func() {
				if card.Highlight {
					ui.Icon(c, p.Assets.Icon("check")).Size(14, 14).TextColor(color)
				}
				accountText(c, benefit, 15, color)
			})
		}
	})
}

func (p *PageContext) accountCardStatusColor(card provider.AccountCard) ui.Color {
	if card.Highlight {
		return paint(p.Theme.Colors.Accent.Soft)
	}
	return paint(p.Theme.Colors.Text.Secondary)
}

type accountDetails struct{}

const (
	rightsPhone   app.FocusID = "account/rights-phone"
	rightsRefresh app.FocusID = "account/rights-refresh"
	rightsActions             = "account-rights"
)

func (accountDetails) Render(c *ui.Context, p *PageContext) {
	auth := p.host.Auth
	snapshot := auth.Accounts()
	account, _ := currentAccount(snapshot)
	card, colors := account.Card, p.Theme.Colors
	details := card.Details
	if details == nil {
		if p.active && p.State.Focus == rightsPhone {
			p.State.Focus = app.CloseModal
			p.host.reveal = true
		}
		ui.Column(c.Key("account-rights-basic")).FillWidth().Lang(accountLanguage).Font(accountFontFamily).Children(func() {
			p.framelessHeader(c, "ACCOUNT \u00b7 BENEFITS", "sparkles", card.Title, card.Status)
			ui.Box(c).Height(18)
			p.accountBenefits(c, card)
			ui.Row(c).Margin(36, 0, 0, 0).Children(func() {
				p.styledButton(c, p.Assets.Text.Close, "", app.CloseModal, rightsActions,
					func() { p.State.Back() }, p.framelessButtonAppearance())
			})
		})
		return
	}

	p.region(rightsActions, nil)
	ui.Column(c.Key("account-rights-body")).FillWidth().Lang(accountLanguage).Font(accountFontFamily).Children(func() {
		p.framelessHeader(c, details.Kicker, "sparkles", details.Title, details.Body)
		if p.host.accountDetailsPhone {
			ui.Box(c).Height(24)
			ui.Column(c).FillWidth().Padding(18, 22).Gap(16).Radius(14).
				Border(1, paint(colors.Border.Subtle)).
				Background(p.uiColors().subtleFill).Children(func() {
				ui.Text(c, details.PhoneBody).FontSize(18).LineHeight(1.75).TextColor(paint(colors.Text.Secondary))
				ui.Column(c).AlignSelf(ui.Start).Gap(5).Children(func() {
					ui.Text(c, details.OfficialLabel).FontSize(18).TextColor(paint(colors.Accent.Soft))
					ui.Box(c).FillWidth().Height(1).Background(paint(colors.Accent.Soft))
				})
			})
		}

		ui.Row(c).FillWidth().Margin(40, 0, 0, 0).Gap(16).AlignItems(ui.Center).Children(func() {
			p.styledButton(c, details.PhoneAction, "", rightsPhone, rightsActions, func() {
				p.host.accountDetailsPhone = true
			}, p.framelessButtonAppearance())
			label := details.RefreshAction
			if snapshot.Busy {
				label = p.Assets.Text.Activity.RefreshingRights
			}
			p.styledButton(c, label, "", rightsRefresh, rightsActions, func() {
				auth.RefreshCurrent(func() {
					if p.State.Page == app.AccountDetails {
						p.State.Back()
						p.host.notice = p.Assets.Text.Activity.RefreshedRights
					}
				})
			}, p.framelessButtonAppearance()).AccessDisabled(snapshot.Busy)
			if snapshot.Error != "" {
				ui.Text(c, p.authError(snapshot.Error)).Grow(1).MinWidth(0).FontSize(17).
					TextColor(paint(colors.Status.Error.Foreground))
			}
		})
		ui.Box(c).Height(22)
		focused := p.paintFocus() == app.CloseModal
		foreground := paint(colors.Text.Secondary)
		if focused {
			foreground = paint(colors.Text.Primary)
		}
		close := ui.Button(c.Key(string(app.CloseModal)), "").Label(details.DismissAction).Height(48).Padding(0, 4).
			Border(0, ui.Transparent).Background(ui.Transparent).AlignSelf(ui.Start).
			OnClick(func() { p.dispatch(func() { p.State.Back() }) })
		p.animateFocus(close, ui.Transparent, foreground)
		p.bind(close, app.CloseModal, rightsActions)
		close.Children(func() {
			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, p.Assets.Icon("back")).Size(18, 18).TextColor(foreground)
				link := ui.Text(c, details.DismissAction).FontSize(19).TextColor(foreground)
				if focused {
					link.Underline().UnderlineOffset(5)
				}
			})
		})
	})
}

func (p *PageContext) openAccountPicker() {
	snapshot := p.host.Auth.Accounts()
	if !snapshot.Busy && p.State.Push(app.Route{Page: app.AccountPicker}) {
		switch {
		case snapshot.CurrentID != "":
			p.State.Focus = app.AccountFocus(snapshot.CurrentID)
		case len(snapshot.Accounts) != 0:
			p.State.Focus = app.AccountFocus(snapshot.Accounts[0].ID)
		}
	}
}

func accountLabel(p *PageContext) string {
	if account, ok := currentAccount(p.host.Auth.Accounts()); ok {
		return account.Name
	}
	return p.Assets.Text.Nav[app.Account]
}

func accountSymbol(c *ui.Context, p *PageContext) {
	if account, ok := currentAccount(p.host.Auth.Accounts()); ok {
		p.accountAvatar(c, account, 32, 17)
		return
	}
	ui.Icon(c, p.Assets.Icon("user")).Size(26, 26)
}

func (p *PageContext) accountAvatar(c *ui.Context, account provider.Account, size, font float32) {
	colors, d := p.Theme.Colors, p.uiColors()
	ui.Box(c).Size(size, size).Shrink(0).Center().Radius(size/2).Clip().
		Border(1, d.avatarBorder).Gradient(d.avatarStart, d.avatarEnd, 140).
		Children(func() {
			if avatar := p.host.avatarImage; avatar != nil {
				ui.Image(c, avatar).Fill().Fit(ui.Cover)
				return
			}
			initial, _ := utf8.DecodeRuneInString(strings.TrimSpace(account.Name))
			ui.Text(c, strings.ToUpper(string(initial))).FontSize(font).TextColor(paint(colors.Text.Primary))
		})
}

func currentAccount(snapshot app.AccountsSnapshot) (provider.Account, bool) {
	for _, account := range snapshot.Accounts {
		if account.ID == snapshot.CurrentID {
			return account, true
		}
	}
	return provider.Account{}, false
}
