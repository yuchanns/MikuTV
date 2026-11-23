package tvui

import (
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/provider"
)

const (
	noticeTab         app.FocusID = "messages/tab/notifications"
	privateTab        app.FocusID = "messages/tab/private"
	noticeReadAll     app.FocusID = "messages/read-all"
	noticeStatus      app.FocusID = "messages/status"
	noticeOpenTarget  app.FocusID = "messages/open-target"
	noticeOpenCreator app.FocusID = "messages/open-creator"
	noticePage        app.Page    = "account-notice"
	noticeTabs                    = "notice-tabs"
	noticeActions                 = "notice-actions"
	noticeFiltersArea             = "notice-filters"
	noticeRows                    = "notice-rows"
	noticeFooter                  = "notice-footer"
	noticeListTop                 = 376
	noticeRowHeight               = 159
)

func (v *View) showNoticeFeedback(text string) {
	if text == "" {
		return
	}
	v.noticeFeedback = videoDetailFeedback{text: text}
}

type accountMessagesPage struct{}

func (accountMessagesPage) Render(c *ui.Context, p *PageContext) {
	text, colors, d := p.Assets.Text.Activity, p.Theme.Colors, p.uiColors()
	hasNotifications := p.host.Activity != nil && p.host.Activity.Available(provider.AccountNotifications)
	hasPrivate := p.host.PrivateMessages != nil && p.host.PrivateMessages.Available()
	if !hasNotifications && !hasPrivate {
		p.region(noticeFooter, nil)
		p.libraryEmpty(c, "comment", text.SignInNeeded, "", p.signInActionLabel(), "user", noticeStatus,
			func() { p.State.Back() })
		return
	}
	if p.host.messagesTab == messagesTabPrivate && !hasPrivate {
		p.host.messagesTab = messagesTabNotifications
	}
	if p.host.messagesTab == messagesTabNotifications && !hasNotifications {
		p.host.messagesTab = messagesTabPrivate
	}
	showingPrivate := p.host.messagesTab == messagesTabPrivate
	activeTabFocus := noticeTab
	if showingPrivate {
		activeTabFocus = privateTab
	}
	if p.active {
		if p.State.Focus == noticeTab && !hasNotifications {
			p.State.Focus = privateTab
			p.host.reveal = true
		} else if p.State.Focus == privateTab && !hasPrivate {
			p.State.Focus = noticeTab
			p.host.reveal = true
		}
	}

	p.left = focusDestination{Focus: app.RailFocus(app.Account)}
	query := messagesQuery(p.host)
	var snapshot app.AccountSnapshot
	if !showingPrivate {
		snapshot = p.host.Activity.Snapshot(query)
	}
	canMarkRead := false
	markingRead := false
	if showingPrivate {
		canMarkRead = p.host.PrivateMessages.CanMarkRead()
		markingRead = p.host.PrivateMessages.MarkingRead()
	} else {
		canMarkRead = p.host.Activity.CanMarkNotificationsRead()
		markingRead = p.host.Activity.MarkingNotificationsRead()
	}
	activeFilter := app.FocusID("account/messages/" + query.Filter)
	var tabEdges focusEdges
	if showingPrivate {
		tabEdges = focusEdges{app.Down: {Region: privateContactsRegion}}
		if convs := p.host.PrivateMessages.Conversations(); len(convs.Conversations) == 0 && convs.Phase == app.CatalogFailed {
			tabEdges[app.Down] = focusDestination{Focus: privateRetryContacts}
		}
	} else {
		tabEdges = focusEdges{app.Down: {Focus: activeFilter}}
	}
	filterEdges := focusEdges{app.Up: {Focus: activeTabFocus}, app.Down: {Region: noticeRows}}
	if canMarkRead {
		tabEdges[app.Right] = focusDestination{Focus: noticeReadAll, Region: noticeActions}
		filterEdges[app.Right] = focusDestination{Focus: noticeReadAll, Region: noticeActions}
		actionDown := focusDestination{Focus: activeFilter, Region: noticeFiltersArea}
		if showingPrivate {
			actionDown = p.privateReadAllDownTarget()
		}
		p.region(noticeActions, focusEdges{
			app.Left: {Focus: activeTabFocus, Region: noticeTabs},
			app.Down: actionDown,
		})
	} else if p.active && p.State.Focus == noticeReadAll {
		p.State.Focus = activeTabFocus
		p.host.reveal = true
	}
	p.region(noticeTabs, tabEdges)
	p.region(noticeFiltersArea, filterEdges)
	p.region(noticeRows, focusEdges{app.Up: {Focus: activeFilter}, app.Down: {Region: noticeFooter}})
	p.region(noticeFooter, focusEdges{app.Up: {Focus: activeFilter}})
	ui.Column(c.Key("messages")).FillWidth().Padding(0, 83, 60, 103).Lang(p.langTag()).Font(p.uiFont()).Children(func() {
		ui.Box(c).FillWidth().Height(303).Children(func() {
			p.messagesHeaderTabs(c, hasNotifications, hasPrivate, showingPrivate)
			ui.Text(c, text.Messages).Absolute().Top(144).Left(0).FontSize(51).FontWeight(550).LetterSpacing(1)
			ui.Text(c, text.MessagesNote).Absolute().Top(230).Left(0).FontSize(22).
				LineHeight(1.7).TextColor(paint(colors.Text.Muted))
			if canMarkRead {
				label := text.ReadAllNotices
				if markingRead {
					label = text.MarkingRead
				}
				p.styledButton(c, label, "check", noticeReadAll, noticeActions, func() {
					if showingPrivate {
						p.markAllPrivateMessagesRead()
					} else {
						p.markAllNotificationsRead(query)
					}
				}, buttonAppearance{
					height: 56, padding: 20, radius: 12, fontSize: 20, fontWeight: 550, iconSize: 28, iconGap: 13,
					background: d.secondaryFill, foreground: d.secondaryText, border: d.secondaryBorder,
				}).Absolute().Top(212).Right(0)
			}
		})
		if showingPrivate {
			p.renderPrivateMessages(c)
			return
		}
		p.notificationFilters(c, query)
		ui.Box(c).Height(25)
		ui.Box(c).FillWidth().Height(1).Background(d.subtleBorder)
		p.notificationList(c, query, snapshot)
	})
}

func (p *PageContext) markAllNotificationsRead(query provider.AccountQuery) {
	if !p.host.Activity.CanMarkNotificationsRead() {
		return
	}
	p.host.Activity.MarkNotificationsRead(provider.NotificationReadRequest{
		All:    true,
		Filter: query.Filter,
	}, func(err error) {
		if err != nil {
			p.host.showNoticeFeedback(p.authError(provider.CodeOf(err)))
			return
		}
		p.host.showNoticeFeedback(p.Assets.Text.Activity.MarkedAllRead)
	})
}

func (p *PageContext) noticeJumpItem(row provider.AccountRow) (provider.MediaItem, bool) {
	var item provider.MediaItem
	switch {
	case row.Item != nil && row.Item.Ref.Kind.Valid() && row.Item.Ref.ID != "":
		item = *row.Item
	case row.Ref != nil && row.Ref.Kind.Valid() && row.Ref.ID != "":
		item = provider.MediaItem{Ref: *row.Ref, Title: row.Title, CoverURL: row.CoverURL}
	default:
		return provider.MediaItem{}, false
	}
	if !p.canOpenNoticeTarget(item.Ref) {
		return provider.MediaItem{}, false
	}
	return item, true
}

func (p *PageContext) canOpenNoticeTarget(ref provider.MediaRef) bool {
	switch ref.Kind {
	case provider.Video, provider.Movie, provider.Series, provider.Episode, provider.Course:
		return p.canOpenVideoDetail(ref)
	case provider.Live:
		return p.canPlay(ref) || p.canOpenVideoDetail(ref)
	default:
		return false
	}
}

func (p *PageContext) jumpToNoticeItem(item provider.MediaItem, notice *provider.AccountNotice) {
	if item.Ref.Kind == provider.Live {
		if p.canPlay(item.Ref) {
			p.startDetailPlayback(item, nil)
		} else {
			p.openVideoDetail(item)
		}
		return
	}
	p.openVideoDetail(item)
	if notice != nil && notice.Target == provider.NoticeTargetComments {
		p.host.videoDetail.tab = videoDetailComments
		p.host.videoDetail.revealSection = true
		if p.canReadVideoDiscussion() {
			p.State.Focus = videoDetailComments.focusID()
		}
	}
}

func (p *PageContext) openNotification(row provider.AccountRow) {
	if row.Notice != nil && row.Notice.Unread && p.host.Activity.CanMarkNotificationsRead() {
		p.host.Activity.MarkNotificationsRead(provider.NotificationReadRequest{
			IDs:    []string{row.ID},
			Filter: row.Notice.Filter,
			Cursor: row.Notice.Cursor,
		}, func(err error) {
			if err != nil {
				p.host.showNoticeFeedback(p.authError(provider.CodeOf(err)))
			}
		})
	}
	isSystem := row.Notice != nil && row.Notice.Filter == provider.NoticeSystem
	if !isSystem {
		if item, ok := p.noticeJumpItem(row); ok {
			p.jumpToNoticeItem(item, row.Notice)
			return
		}
	}
	p.host.openNotice = row
	p.State.Push(app.Route{Page: noticePage})
}

func (p *PageContext) messagesHeaderTabs(c *ui.Context, hasNotifications, hasPrivate, showingPrivate bool) {
	text, colors, d := p.Assets.Text.Activity, p.Theme.Colors, p.uiColors()
	renderTab := func(id app.FocusID, label string, selected bool, kind messagesTabKind) {
		background, ink := ui.Transparent, paint(colors.Text.Muted)
		borderW, borderColor := float32(0), ui.Transparent
		if selected {
			background, ink = withAlpha(colors.Text.Primary, 0x13), paint(colors.Text.Primary)
		}
		focused := p.paintFocus() == id
		if focused {
			background, ink = d.tabFocusFill, paint(colors.Text.Primary)
			if !p.Theme.Dark {
				borderW, borderColor = 1, d.focusBorder
			}
		}
		tab := ui.Button(c.Key(string(id)), "").Label(label).Role(ui.RoleTab).AccessSelected(selected).
			Size(98, 61).Center().Radius(30).Border(borderW, borderColor).Background(background).
			OnClick(func() { p.dispatch(func() { p.host.selectMessagesTab(kind) }) })
		p.animateFocus(tab, background, ink)
		p.applyFocusElevation(tab, focused)
		p.bind(tab, id, noticeTabs)
		tab.Children(func() {
			ui.Text(c, label).FontSize(24).LetterSpacing(1)
			if selected {
				ui.Box(c).Absolute().Left(41.5).Bottom(2).Size(15, 3).Radius(2).Background(paint(colors.Accent.Primary))
			}
		})
	}
	ui.Row(c).Absolute().Left(-25).Top(45).Height(61).Gap(18).Children(func() {
		if hasNotifications {
			renderTab(noticeTab, text.NotificationTab, !showingPrivate, messagesTabNotifications)
		}
		if hasPrivate {
			renderTab(privateTab, text.PrivateTab, showingPrivate, messagesTabPrivate)
		}
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
		p.dispatch(func() { p.openNotification(row) })
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
	p.region(noticeFooter, nil)
	var meta string
	if row.Notice != nil && row.Notice.Actor != "" && row.Notice.Action != "" {
		meta = row.Notice.Action
	}
	if row.Time > 0 {
		ts := time.Unix(row.Time, 0).Local().Format("2006-01-02 15:04")
		if meta != "" {
			meta += " \u00b7 " + ts
		} else {
			meta = ts
		}
	}
	jumpItem, canJumpItem := p.noticeJumpItem(row)
	entry, _ := p.State.Current()
	canJumpCreator := !canJumpItem && row.Notice != nil && row.Notice.ActorID != "" && row.Notice.Actor != "" &&
		entry.Capabilities.Has(provider.ReadCreator)
	ui.Column(c).FillWidth().Lang(p.langTag()).Font(p.uiFont()).Children(func() {
		p.framelessHeader(c, "NOTIFICATION", "info", row.Title, meta)
		ui.Scroll(c.Key("notice-content")).FillWidth().MaxHeight(360).Children(func() {
			ui.Text(c, row.Subtitle).FontSize(22).LineHeight(1.85).TextColor(paint(colors.Text.Secondary))
		})
		ui.Row(c).Margin(36, 0, 0, 0).Gap(16).Children(func() {
			if canJumpItem {
				p.styledButton(c, p.Assets.Text.Activity.OpenNoticeTarget, "play", noticeOpenTarget, noticeFooter, func() {
					notice := row.Notice
					p.State.Back()
					p.jumpToNoticeItem(jumpItem, notice)
				}, p.framelessButtonAppearance())
			} else if canJumpCreator {
				p.styledButton(c, p.Assets.Text.Activity.OpenNoticeCreator, "user", noticeOpenCreator, noticeFooter, func() {
					creator := provider.Creator{
						ID:        row.Notice.ActorID,
						Name:      row.Notice.Actor,
						AvatarURL: row.CreatorAvatarURL,
					}
					p.State.Back()
					p.openCreator(creator)
				}, p.framelessButtonAppearance())
			}
			p.styledButton(c, p.Assets.Text.Close, "", app.CloseModal, noticeFooter,
				func() { p.State.Back() }, p.framelessButtonAppearance())
		})
	})
}
