package tvui

import (
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/provider"
)

const (
	noticeTab         app.FocusID = "messages/tab/notifications"
	noticeStatus      app.FocusID = "messages/status"
	noticePage        app.Page    = "account-notice"
	noticeTabs                    = "notice-tabs"
	noticeFiltersArea             = "notice-filters"
	noticeRows                    = "notice-rows"
	noticeFooter                  = "notice-footer"
	noticeListTop                 = 376
	noticeRowHeight               = 159
)

type accountMessagesPage struct{}

func (accountMessagesPage) Render(c *ui.Context, p *PageContext) {
	text, colors, d := p.Assets.Text.Activity, p.Theme.Colors, p.uiColors()
	if !p.host.Activity.Available(provider.AccountNotifications) {
		p.region(noticeFooter, nil)
		p.libraryEmpty(c, "comment", text.SignInNeeded, "", text.SignInAction, "user", noticeStatus,
			func() { p.State.Back() })
		return
	}
	p.left = focusDestination{Focus: app.RailFocus(app.Account)}
	query := messagesQuery(p.host)
	snapshot := p.host.Activity.Snapshot(query)
	activeFilter := app.FocusID("account/messages/" + query.Filter)
	p.region(noticeTabs, focusEdges{app.Down: {Focus: activeFilter}})
	p.region(noticeFiltersArea, focusEdges{
		app.Up: {Focus: noticeTab}, app.Down: {Region: noticeRows},
	})
	p.region(noticeRows, focusEdges{app.Up: {Focus: activeFilter}, app.Down: {Region: noticeFooter}})
	p.region(noticeFooter, focusEdges{app.Up: {Focus: activeFilter}})
	ui.Column(c.Key("messages")).FillWidth().Padding(0, 83, 60, 103).Lang(accountLanguage).Font(accountFontFamily).Children(func() {
		ui.Box(c).FillWidth().Height(303).Children(func() {
			p.notificationTabs(c)
			ui.Text(c, text.Messages).Absolute().Top(144).Left(0).FontSize(51).FontWeight(550).LetterSpacing(1)
			ui.Text(c, text.MessagesNote).Absolute().Top(230).Left(0).FontSize(22).
				LineHeight(1.7).TextColor(paint(colors.Text.Muted))
			ui.Button(c.Key("notices-read-all"), "").Label(text.ReadAllNotices).Disabled(true).
				Absolute().Top(212).Right(0).Size(169, 56).Center().Radius(12).
				Background(d.secondaryFill).Border(1, d.secondaryBorder).
				Children(func() {
					ui.Row(c).Gap(13).AlignItems(ui.Center).Children(func() {
						ui.Icon(c, p.Assets.Icon("check")).Size(30, 30).TextColor(d.secondaryText)
						ui.Text(c, text.ReadAllNotices).FontSize(20).FontWeight(550).TextColor(d.secondaryText)
					})
				})
		})
		p.notificationFilters(c, query)
		ui.Box(c).Height(25)
		ui.Box(c).FillWidth().Height(1).Background(d.subtleBorder)
		p.notificationList(c, query, snapshot)
	})
}

func (p *PageContext) notificationTabs(c *ui.Context) {
	text, colors := p.Assets.Text.Activity, p.Theme.Colors
	ui.Row(c).Absolute().Left(-25).Top(45).Height(61).Gap(18).Children(func() {
		background := withAlpha(colors.Text.Primary, 0x13)
		if p.paintFocus() == noticeTab {
			background = withAlpha(colors.Focus.Background, 0x20)
		}
		tab := ui.Button(c.Key(string(noticeTab)), "").Label(text.NotificationTab).Role(ui.RoleTab).AccessSelected(true).
			Size(98, 61).Center().Radius(30).Border(0, ui.Transparent).Background(background)
		p.animateFocus(tab, background, paint(colors.Text.Primary))
		p.bind(tab, noticeTab, noticeTabs)
		tab.Children(func() {
			ui.Text(c, text.NotificationTab).FontSize(24).LetterSpacing(1)
			ui.Box(c).Absolute().Left(41.5).Bottom(2).Size(15, 3).Radius(2).Background(paint(colors.Accent.Primary))
		})
	})
}

func (p *PageContext) notificationFilters(c *ui.Context, query provider.AccountQuery) {
	colors, d, labels := p.Theme.Colors, p.uiColors(), p.Assets.Text.Activity
	ui.Row(c).FillWidth().Height(48).Gap(14).Children(func() {
		for _, filter := range noticeFilters {
			style := buttonAppearance{height: 48, padding: 25, radius: 25, fontSize: 19,
				background: d.subtleFill, foreground: paint(colors.Text.Secondary), border: d.subtleBorder}
			if query.Filter == filter {
				style.background, style.foreground = withAlpha(colors.Focus.Background, 0xee), paint(colors.Focus.Foreground)
				style.border = ui.Transparent
			}
			p.styledButton(c, labels.filter(filter), "", app.FocusID("account/messages/"+filter), noticeFiltersArea, func() {
				p.host.Activity.SetFilter(filter)
			}, style).AccessSelected(query.Filter == filter)
		}
	})
}

func noticeRowFocus(id string) app.FocusID { return app.FocusID("messages/notice/" + id) }

func (p *PageContext) notificationList(c *ui.Context, query provider.AccountQuery, snapshot app.AccountSnapshot) {
	rows := snapshot.Page.Rows
	if len(rows) != 0 {
		y := p.scroll().Y - noticeListTop
		visibleEnd := max(0, y+p.Height)
		first := min(len(rows), max(0, int(y/noticeRowHeight)-1))
		end := min(len(rows), int(visibleEnd/noticeRowHeight)+2)
		if p.active && snapshot.Phase == app.CatalogReady && snapshot.Page.NextCursor != "" &&
			visibleEnd+noticeRowHeight >= float32(len(rows)*noticeRowHeight) {
			p.host.Activity.More(query)
		}
		if p.active {
			p.prepareAccountArtwork(rows[first:end])
		}
		ui.Box(c.Key("notice-list")).FillWidth().Height(float32(len(rows) * noticeRowHeight)).Children(func() {
			for index, row := range rows {
				if index >= first && index < end || p.paintFocus() == noticeRowFocus(row.ID) {
					p.notificationRow(c, row, float32(index*noticeRowHeight))
				}
			}
		})
	}
	activity, labels := p.host.Activity, p.Assets.Text
	switch snapshot.Phase {
	case app.CatalogIdle, app.CatalogLoading:
		ui.Text(c, labels.LoadingTitle).FillWidth().Padding(22, 0).TextAlign(ui.Center).FontSize(19).
			TextColor(paint(p.Theme.Colors.Text.Muted))
	case app.CatalogFailed, app.CatalogCanceled:
		p.button(c, labels.Retry, noticeStatus, noticeFooter, func() { activity.Retry(query) }).Margin(20, 0, 0, 0)
	default:
		if len(rows) == 0 {
			ui.Text(c, labels.EmptyTitle).FillWidth().Margin(85, 0, 0, 0).
				TextAlign(ui.Center).FontSize(32).TextColor(paint(p.Theme.Colors.Text.Muted))
		}
	}
}

func (p *PageContext) notificationRow(c *ui.Context, row provider.AccountRow, top float32) {
	colors, d := p.Theme.Colors, p.uiColors()
	var action string
	var unread bool
	if notice := row.Notice; notice != nil {
		if notice.Actor != "" {
			action = notice.Action
		}
		unread = notice.Unread
	}
	id := noticeRowFocus(row.ID)
	fill, date := ui.Transparent, paint(colors.Text.Muted)
	if p.paintFocus() == id {
		fill, date = paint(colors.Surface.Raised), paint(colors.Text.Secondary)
	}
	button := ui.Button(c.Key(row.ID), "").Label(row.Title+" "+action).Absolute().Top(top).Left(0).Right(0).
		Height(noticeRowHeight).Padding(0).Radius(12).Border(0, ui.Transparent).Background(fill).OnClick(func() {
		p.dispatch(func() {
			p.host.openNotice = row
			p.State.Push(app.Route{Page: noticePage})
		})
	})
	p.animateFocus(button, fill, paint(colors.Text.Primary))
	p.bind(button, id, noticeRows)
	button.Children(func() {
		ui.Row(c).Fill().Padding(0, 27).Gap(27).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(59, 59).Shrink(0).Radius(29.5).Clip().Center().Background(paint(colors.Surface.Raised)).
				Children(func() {
					if avatar := p.accountRowImage(row.CreatorAvatarURL); avatar != nil {
						ui.Image(c, avatar).Fill().Fit(ui.Cover)
					} else {
						ui.Icon(c, p.Assets.Icon("user")).Size(27, 27).TextColor(paint(colors.Text.Secondary))
					}
				})
			ui.Column(c).Grow(1).MinWidth(0).Gap(12).Children(func() {
				ui.Row(c).FillWidth().Height(32).Gap(16).AlignItems(ui.Center).Children(func() {
					ui.Text(c, row.Title).MaxWidth(610).MinWidth(0).FontSize(23).FontWeight(450).MaxLines(1)
					if action != "" {
						ui.Text(c, action).Shrink(0).FontSize(19).TextColor(paint(colors.Text.Muted))
					}
				})
				if row.Subtitle != "" {
					ui.Text(c, row.Subtitle).FillWidth().Height(30).MaxLines(1).FontSize(22).
						TextColor(paint(colors.Text.Secondary))
				}
				if row.Time > 0 {
					ui.Text(c, time.Unix(row.Time, 0).Local().Format("2006-01-02 15:04")).Height(22).
						FontSize(16).TextColor(date)
				}
			})
			if image := p.accountRowImage(row.CoverURL); image != nil {
				ui.Box(c).Size(164, 93).Shrink(0).Radius(10).Clip().Children(func() {
					ui.Image(c, image).Fill().Fit(ui.Cover)
				})
			} else {
				ui.Icon(c, p.Assets.Icon("info")).Size(29, 29).Shrink(0).TextColor(paint(colors.Accent.Soft))
			}
		})
		if unread {
			ui.Box(c).Absolute().Left(6).Top(79).Size(6, 6).Radius(3).Background(paint(colors.Accent.Primary))
		}
		ui.Box(c).Absolute().Left(0).Right(0).Bottom(0).Height(1).Background(d.subtleBorder)
	})
}

type accountNoticeDialog struct{}

func (accountNoticeDialog) Render(c *ui.Context, p *PageContext) {
	row, colors := p.host.openNotice, p.Theme.Colors
	ui.Text(c, row.Title).FontSize(36).FontWeight(500).MaxLines(2)
	ui.Scroll(c.Key("notice-content")).FillWidth().Grow(1).Children(func() {
		ui.Text(c, row.Subtitle).FontSize(21).LineHeight(1.8).TextColor(paint(colors.Text.Secondary))
	})
	p.styledButton(c, p.Assets.Text.Close, "", app.CloseModal, noticeFooter,
		func() { p.State.Back() }, p.dialogButtonAppearance())
}
