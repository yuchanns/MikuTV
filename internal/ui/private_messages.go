package tvui

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/provider"
)

type messagesTabKind uint8

const (
	messagesTabNotifications messagesTabKind = iota
	messagesTabPrivate
)

const (
	privateContactsRegion = "private-contacts"
	privateThreadRegion   = "private-thread"
	privateComposeRegion  = "private-compose"

	privateHistoryFocus  app.FocusID = "messages/private/history"
	privateComposeFocus  app.FocusID = "messages/private/compose"
	privateRetryContacts app.FocusID = "messages/private/retry-contacts"
	privateRetryThread   app.FocusID = "messages/private/retry-thread"

	privateComposeEdit    app.FocusID = "private-compose/edit"
	privateComposeSend    app.FocusID = "private-compose/send"
	privateComposeInput   app.FocusID = "private-compose/input"
	privateComposePreview app.FocusID = "private-compose/preview"

	privatePanelHeight      = 600
	privateContactsWidth    = 360
	privateContactRowHeight = 104
	privateContactRowGap    = 6
	privateContactRowStride = privateContactRowHeight + privateContactRowGap
	privateContactPad       = 15
)

func privateContactFocus(id string) app.FocusID {
	return app.FocusID("messages/private/contact/" + id)
}

type privateMessageDraft struct {
	conversationID string
	peer           provider.PrivatePeer
	editing        bool
	body           string
	input          string
	failure        string
	preview        ui.ScrollState
}

func (d *privateMessageDraft) canSend() bool {
	return d.conversationID != "" && strings.TrimSpace(d.body) != ""
}

type privateMessagesUI struct {
	contactScroll ui.ScrollState
	threadScroll  ui.ScrollState
	lastConvID    string
	lastRevision  uint64
	stickFrames   int
	draft         privateMessageDraft
}

func (v *View) selectMessagesTab(tab messagesTabKind) {
	if tab == messagesTabPrivate && (v.PrivateMessages == nil || !v.PrivateMessages.Available()) {
		return
	}
	if tab == messagesTabNotifications && (v.Activity == nil || !v.Activity.Available(provider.AccountNotifications)) {
		return
	}
	v.messagesTab = tab
	if tab == messagesTabPrivate && v.PrivateMessages != nil {
		v.PrivateMessages.EnsureConversations()
	} else if tab == messagesTabNotifications && v.Activity != nil {
		v.Activity.Ensure(messagesQuery(v))
	}
}

func (v *View) openCreatorPrivateMessages(creator provider.Creator) {
	if v.PrivateMessages == nil || !v.PrivateMessages.Available() || strings.TrimSpace(creator.ID) == "" {
		return
	}
	v.messagesTab = messagesTabPrivate
	convID := v.PrivateMessages.OpenPeer(provider.PrivatePeer{
		ID:        creator.ID,
		Name:      creator.Name,
		AvatarURL: creator.AvatarURL,
	})
	if !v.State.Push(app.Route{Page: app.AccountMessages}) {
		return
	}
	if v.PrivateMessages.CanSend() {
		v.State.Focus = privateComposeFocus
	} else if convID != "" {
		v.State.Focus = privateContactFocus(convID)
	} else {
		v.State.Focus = privateTab
	}
	v.reveal = true
}

func (p *PageContext) privateReadAllDownTarget() focusDestination {
	if p.host.PrivateMessages != nil {
		if _, ok := p.host.PrivateMessages.ActiveConversation(); ok {
			if p.host.PrivateMessages.CanSend() {
				return focusDestination{Focus: privateComposeFocus, Region: privateComposeRegion}
			}
			return focusDestination{Region: privateThreadRegion}
		}
	}
	return focusDestination{Region: privateContactsRegion}
}

func (p *PageContext) markAllPrivateMessagesRead() {
	if p.host.PrivateMessages == nil || !p.host.PrivateMessages.CanMarkRead() {
		return
	}
	p.host.PrivateMessages.MarkAllRead(func(err error) {
		if err != nil {
			p.host.showNoticeFeedback(p.privateOperationError(err, p.Assets.Text.Activity.PrivateLoadFailed))
			return
		}
		p.host.showNoticeFeedback(p.Assets.Text.Activity.MarkedAllRead)
	})
}

func (p *PageContext) preparePrivateAvatars(conversations []provider.PrivateConversation) {
	if p.host.Catalog == nil || len(conversations) == 0 {
		return
	}
	urls := make([]string, 0, len(conversations))
	for _, conv := range conversations {
		if conv.Peer.AvatarURL != "" {
			urls = append(urls, conv.Peer.AvatarURL)
		}
	}
	if len(urls) != 0 {
		p.host.Catalog.PrepareArtwork(urls)
	}
}

func (p *PageContext) privatePeerAvatar(c *ui.Context, peer provider.PrivatePeer, size, fontSize float32) {
	colors := p.Theme.Colors
	ui.Box(c).Size(size, size).Shrink(0).Radius(size/2).Clip().Center().
		Background(paint(colors.Surface.Raised)).Children(func() {
		if avatar := p.accountRowImage(peer.AvatarURL); avatar != nil {
			ui.Image(c, avatar).Fill().Fit(ui.Cover)
			return
		}
		if r, _ := utf8.DecodeRuneInString(strings.TrimSpace(peer.Name)); r != utf8.RuneError && r != 0 {
			ui.Text(c, strings.ToUpper(string(r))).FontSize(fontSize).FontWeight(500).TextColor(paint(colors.Text.Primary))
			return
		}
		ui.Icon(c, p.Assets.Icon("user")).Size(size*0.48, size*0.48).TextColor(paint(colors.Text.Secondary))
	})
}

func formatPrivateTime(ts int64) string {
	if ts <= 0 {
		return ""
	}
	when := time.Unix(ts, 0).Local()
	if when.Year() == time.Now().Local().Year() {
		return when.Format("01-02 15:04")
	}
	return when.Format("2006-01-02 15:04")
}

func (p *PageContext) privateOperationError(err error, fallback string) string {
	if err == nil {
		return ""
	}
	if msg := strings.TrimSpace(provider.MessageOf(err)); msg != "" {
		return msg
	}
	text := p.Assets.Text.Activity
	switch provider.CodeOf(err) {
	case provider.RateLimited:
		return text.PrivateRateLimited
	case provider.Denied:
		return text.PrivatePermissionDenied
	case provider.Network:
		return p.Assets.Text.Auth.NetworkFailed
	case provider.NeedsSignIn, provider.Expired:
		return p.Assets.Text.Auth.AccountExpired
	default:
		return fallback
	}
}

func (p *PageContext) renderPrivateMessages(c *ui.Context) {
	pm := p.host.PrivateMessages
	d, text := p.uiColors(), p.Assets.Text.Activity
	convs := pm.Conversations()
	active, hasActive := pm.ActiveConversation()
	var thread app.PrivateThreadSnapshot
	if hasActive {
		thread = pm.Thread(active.ID)
	}
	canSend := hasActive && pm.CanSend()

	rightTarget := focusDestination{}
	switch {
	case hasActive && canSend:
		rightTarget = focusDestination{Focus: privateComposeFocus, Region: privateComposeRegion}
	case hasActive && len(thread.Messages) > 0:
		rightTarget = focusDestination{Focus: privateHistoryFocus, Region: privateThreadRegion}
	case hasActive && thread.Phase == app.CatalogFailed:
		rightTarget = focusDestination{Focus: privateRetryThread, Region: privateThreadRegion}
	}

	activeContactTarget := focusDestination{Region: privateContactsRegion}
	if hasActive {
		activeContactTarget = focusDestination{Focus: privateContactFocus(active.ID), Region: privateContactsRegion}
	}

	p.region(privateContactsRegion, focusEdges{
		app.Up:    {Focus: privateTab, Region: noticeTabs},
		app.Right: rightTarget,
	})
	threadDown := focusDestination{}
	if canSend {
		threadDown = focusDestination{Focus: privateComposeFocus, Region: privateComposeRegion}
	}
	p.region(privateThreadRegion, focusEdges{
		app.Up:   {Focus: privateTab, Region: noticeTabs},
		app.Left: activeContactTarget,
		app.Down: threadDown,
	})
	composeUp := focusDestination{Focus: privateTab, Region: noticeTabs}
	switch {
	case hasActive && len(thread.Messages) > 0:
		composeUp = focusDestination{Focus: privateHistoryFocus, Region: privateThreadRegion}
	case hasActive && thread.Phase == app.CatalogFailed:
		composeUp = focusDestination{Focus: privateRetryThread, Region: privateThreadRegion}
	}
	p.region(privateComposeRegion, focusEdges{
		app.Up:   composeUp,
		app.Left: activeContactTarget,
	})

	ui.Row(c.Key("private-layout")).FillWidth().Height(privatePanelHeight).Radius(19).Clip().
		Border(1, d.subtleBorder).Background(d.subtleFill).Children(func() {
		p.renderPrivateContactsPane(c, convs, active.ID, rightTarget)
		ui.Box(c).Width(1).FillHeight().Shrink(0).Background(d.subtleBorder)
		p.renderPrivateThreadPane(c, active, hasActive, thread, canSend, text, d)
	})
}

func (p *PageContext) renderPrivateContactsPane(c *ui.Context, convs app.PrivateConversationsSnapshot, activeID string, rightTarget focusDestination) {
	colors, d, text := p.Theme.Colors, p.uiColors(), p.Assets.Text.Activity
	count := len(convs.Conversations)
	scrollY := p.host.privateUI.contactScroll.Y
	first, end := visibleItemRange(scrollY-privateContactPad, privatePanelHeight, privateContactRowStride, count, 2)
	focusedIdx := -1
	paintFocus := p.paintFocus()
	for idx, conv := range convs.Conversations {
		if paintFocus == privateContactFocus(conv.ID) {
			focusedIdx = idx
			break
		}
	}
	if p.active && count > 0 {
		p.preparePrivateAvatars(convs.Conversations[first:end])
		totalHeight := float32(count)*privateContactRowStride - privateContactRowGap
		nearBottom := end >= count ||
			(focusedIdx >= 0 && focusedIdx >= count-2) ||
			scrollY+privatePanelHeight+2*privateContactRowStride >= totalHeight+2*privateContactPad
		if convs.NextCursor != "" && convs.Phase == app.CatalogReady && nearBottom {
			p.host.PrivateMessages.LoadMoreConversations()
		}
	}
	ui.Box(c.Key("private-contacts-pane")).Width(privateContactsWidth).FillHeight().Shrink(0).Children(func() {
		ui.Scroll(c.Key("private-contacts-scroll")).Fill().TrackScroll(&p.host.privateUI.contactScroll).Children(func() {
			ui.Column(c).FillWidth().Padding(privateContactPad).Gap(privateContactRowGap).Children(func() {
				if count > 0 {
					totalHeight := float32(count)*privateContactRowStride - privateContactRowGap
					ui.Box(c.Key("private-contacts-virtual")).FillWidth().Height(totalHeight).Children(func() {
						for index := first; index < end; index++ {
							conv := convs.Conversations[index]
							p.renderPrivateContactRow(c, conv, float32(index*privateContactRowStride), conv.ID == activeID, index == count-1 && convs.NextCursor != "", rightTarget)
						}
						if focusedIdx >= 0 && (focusedIdx < first || focusedIdx >= end) {
							conv := convs.Conversations[focusedIdx]
							p.renderPrivateContactRow(c, conv, float32(focusedIdx*privateContactRowStride), conv.ID == activeID, focusedIdx == count-1 && convs.NextCursor != "", rightTarget)
						}
					})
				}
				switch {
				case convs.Phase == app.CatalogIdle || (convs.Phase == app.CatalogLoading && count == 0):
					ui.Row(c).FillWidth().Padding(32, 0).Gap(12).AlignItems(ui.Center).Justify(ui.Center).Children(func() {
						p.loadingSpinner(c, 22)
						ui.Text(c, p.Assets.Text.LoadingTitle).FontSize(19).TextColor(paint(colors.Text.Muted))
					})
				case convs.Phase == app.CatalogFailed && count == 0:
					ui.Column(c).FillWidth().Padding(28, 12).Gap(16).AlignItems(ui.Center).Children(func() {
						ui.Text(c, text.PrivateLoadFailed).TextAlign(ui.Center).FontSize(18).LineHeight(1.6).
							TextColor(paint(colors.Text.Secondary))
						p.button(c, p.Assets.Text.Retry, privateRetryContacts, privateContactsRegion, func() {
							p.host.PrivateMessages.RetryConversations()
						})
					})
				case count == 0:
					ui.Text(c, text.EmptyConversations).FillWidth().Padding(48, 16).TextAlign(ui.Center).
						FontSize(20).TextColor(paint(colors.Text.Muted))
				}
				if convs.Phase == app.CatalogLoading && convs.Appending {
					ui.Row(c).FillWidth().Padding(14, 0).Gap(10).AlignItems(ui.Center).Justify(ui.Center).Children(func() {
						p.loadingSpinner(c, 18)
						ui.Text(c, p.Assets.Text.LoadingTitle).FontSize(17).TextColor(paint(colors.Text.Muted))
					})
				} else if convs.Phase == app.CatalogFailed && convs.Appending {
					p.styledButton(c, p.Assets.Text.Retry, "", privateRetryContacts, privateContactsRegion, func() {
						p.host.PrivateMessages.RetryConversations()
					}, buttonAppearance{
						height: 46, padding: 18, radius: 12, fontSize: 17,
						background: d.secondaryFill, foreground: d.secondaryText, border: d.secondaryBorder,
					}).AlignSelf(ui.Center).Margin(8, 0, 0, 0)
				}
			})
		})
	})
}

func (p *PageContext) renderPrivateContactRow(c *ui.Context, conv provider.PrivateConversation, top float32, selected, isLastWithMore bool, rightTarget focusDestination) {
	colors, d := p.Theme.Colors, p.uiColors()
	id := privateContactFocus(conv.ID)
	focused := p.paintFocus() == id

	fill, nameColor, previewColor := ui.Transparent, paint(colors.Text.Primary), paint(colors.Text.Muted)
	if selected {
		fill, nameColor = d.privateSelectedFill, paint(colors.Accent.Soft)
	}
	if focused {
		fill, nameColor, previewColor = paint(colors.Focus.Background), paint(colors.Focus.Foreground), d.focusSubtext
	}

	preview := conv.Preview
	if preview == "" {
		preview = p.Assets.Text.Activity.PrivateConversation
	}
	label := conv.Peer.Name
	if conv.Unread > 0 {
		label += " " + strconv.Itoa(conv.Unread)
	}

	btn := ui.Button(c.Key(string(id)), "").Label(label).Absolute().Top(top).Left(0).Right(0).Height(privateContactRowHeight).
		Padding(17, 15).Radius(13).Border(0, ui.Transparent).Background(fill).
		AccessSelected(selected).OnClick(func() {
		p.dispatch(func() { p.host.PrivateMessages.SelectConversation(conv.ID) })
	})
	p.animateFocus(btn, fill, nameColor)
	p.bindDirection(btn, id, privateContactsRegion, func(direction app.Direction) bool {
		if direction == app.Right {
			if p.host.PrivateMessages.ActiveConversationID() != conv.ID {
				p.host.PrivateMessages.SelectConversation(conv.ID)
			}
			target := rightTarget.Focus
			if target == "" && p.host.PrivateMessages.CanSend() {
				target = privateComposeFocus
			}
			if target != "" {
				p.host.moveFocus(target, app.Right, true)
				return true
			}
			return false
		}
		if direction == app.Down && isLastWithMore {
			p.host.PrivateMessages.LoadMoreConversations()
		}
		return false
	})
	btn.Children(func() {
		ui.Row(c).FillWidth().Gap(16).AlignItems(ui.Center).Children(func() {
			p.privatePeerAvatar(c, conv.Peer, 51, 21)
			ui.Column(c).Grow(1).MinWidth(0).Children(func() {
				ui.Text(c, conv.Peer.Name).FillWidth().FontSize(21).FontWeight(450).MaxLines(1).TextColor(nameColor)
				ui.Text(c, preview).FillWidth().Margin(10, 0, 0, 0).FontSize(16).MaxLines(1).TextColor(previewColor)
			})
			if conv.Unread > 0 {
				badge := strconv.Itoa(conv.Unread)
				if conv.Unread > 99 {
					badge = "99+"
				}
				ui.Box(c).MinWidth(23).Height(23).Padding(0, 6).Shrink(0).Radius(11.5).Center().
					Background(paint(colors.Accent.Primary)).Children(func() {
					ui.Text(c, badge).FontSize(13).FontWeight(600).TextColor(paint(colors.Accent.Foreground))
				})
			}
		})
	})
}

func (p *PageContext) renderPrivateThreadPane(
	c *ui.Context,
	active provider.PrivateConversation,
	hasActive bool,
	thread app.PrivateThreadSnapshot,
	canSend bool,
	text accountActivityLabels,
	d uiColors,
) {
	colors := p.Theme.Colors
	ui.Column(c.Key("private-thread-pane")).Grow(1).MinWidth(0).FillHeight().Children(func() {
		if !hasActive {
			ui.Box(c).Fill().Center().Children(func() {
				ui.Text(c, text.EmptyConversations).FontSize(22).TextColor(paint(colors.Text.Muted))
			})
			return
		}

		// Conversation header (91px)
		ui.Box(c.Key("private-thread-header")).FillWidth().Height(91).Shrink(0).Children(func() {
			ui.Row(c).Fill().Padding(0, 30).Gap(19).AlignItems(ui.Center).Children(func() {
				p.privatePeerAvatar(c, active.Peer, 43, 18)
				ui.Text(c, active.Peer.Name).Grow(1).MinWidth(0).FontSize(24).FontWeight(450).MaxLines(1).
					TextColor(paint(colors.Text.Primary))
				ui.Text(c, text.PrivateConversation).Shrink(0).FontSize(16).TextColor(paint(colors.Text.Muted))
			})
			ui.Box(c).Absolute().Left(0).Right(0).Bottom(0).Height(1).Background(d.subtleBorder)
		})

		// Auto-scroll to bottom when opening a conversation or receiving a newly sent message.
		pui := &p.host.privateUI
		if pui.lastConvID != active.ID || pui.lastRevision != thread.Revision {
			pui.lastConvID = active.ID
			pui.lastRevision = thread.Revision
			pui.stickFrames = 2
		}
		if pui.stickFrames > 0 {
			if pui.threadScroll.MaxY > 0 {
				pui.threadScroll.Y = pui.threadScroll.MaxY
			}
			pui.stickFrames--
			if pui.stickFrames > 0 {
				c.After(16 * time.Millisecond)
			}
		}

		// Messages scroll viewport
		scrollWrap := ui.Box(c.Key("private-messages-wrap")).Grow(1).FillWidth().MinHeight(0)
		if len(thread.Messages) > 0 {
			scrollWrap.Label(text.PrivateScrollMessages)
			p.bindDirection(scrollWrap, privateHistoryFocus, privateThreadRegion, func(direction app.Direction) bool {
				const step = 140
				switch direction {
				case app.Up:
					if pui.threadScroll.Y > 1 {
						pui.stickFrames = 0
						pui.threadScroll.Y = max(0, pui.threadScroll.Y-step)
						if pui.threadScroll.Y <= step && thread.HasMore && thread.Phase == app.CatalogReady {
							p.host.PrivateMessages.LoadOlderMessages(active.ID)
						}
						return true
					}
					if thread.HasMore {
						if thread.Phase == app.CatalogReady {
							pui.stickFrames = 0
							p.host.PrivateMessages.LoadOlderMessages(active.ID)
						}
						return true
					}
					return false
				case app.Down:
					if pui.threadScroll.Y+1 < pui.threadScroll.MaxY {
						pui.stickFrames = 0
						pui.threadScroll.Y = min(pui.threadScroll.MaxY, pui.threadScroll.Y+step)
						return true
					}
					if canSend {
						p.host.moveFocus(privateComposeFocus, app.Down, true)
						return true
					}
					return true
				}
				return false
			})
			if p.paintFocus() == privateHistoryFocus {
				scrollWrap.Radius(12).Border(2, paint(colors.Focus.Ring))
			}
		}

		bubbleMaxW := max(320, (p.Width-186-privateContactsWidth-64)*0.78)
		scrollWrap.Children(func() {
			ui.Scroll(c.Key("private-thread-" + active.ID)).Fill().TrackScroll(&pui.threadScroll).Children(func() {
				ui.Column(c).FillWidth().Padding(24, 32).Gap(18).Children(func() {
					if thread.Phase == app.CatalogLoading && thread.Prepending {
						ui.Row(c).AlignSelf(ui.Center).Gap(10).AlignItems(ui.Center).Children(func() {
							p.loadingSpinner(c, 18)
							ui.Text(c, p.Assets.Text.LoadingTitle).FontSize(17).TextColor(paint(colors.Text.Muted))
						})
					}
					if thread.Phase == app.CatalogFailed && thread.Prepending {
						ui.Row(c).AlignSelf(ui.Center).Gap(12).AlignItems(ui.Center).Children(func() {
							ui.Text(c, text.PrivateLoadFailed).FontSize(17).TextColor(paint(colors.Status.Warning.Foreground))
							p.styledButton(c, p.Assets.Text.Retry, "", privateRetryThread, privateThreadRegion, func() {
								p.host.PrivateMessages.RetryMessages(active.ID)
							}, buttonAppearance{
								height: 42, padding: 16, radius: 10, fontSize: 16,
								background: d.secondaryFill, foreground: d.secondaryText, border: d.secondaryBorder,
							})
						})
					}
					switch {
					case (thread.Phase == app.CatalogIdle || thread.Phase == app.CatalogLoading) && len(thread.Messages) == 0:
						ui.Text(c, p.Assets.Text.LoadingTitle).FillWidth().Padding(48, 0).TextAlign(ui.Center).
							FontSize(20).TextColor(paint(colors.Text.Muted))
					case thread.Phase == app.CatalogFailed && len(thread.Messages) == 0:
						ui.Column(c).FillWidth().Padding(48, 24).Gap(18).AlignItems(ui.Center).Children(func() {
							ui.Text(c, text.PrivateLoadFailed).FontSize(20).TextColor(paint(colors.Text.Secondary))
							p.button(c, p.Assets.Text.Retry, privateRetryThread, privateThreadRegion, func() {
								p.host.PrivateMessages.RetryMessages(active.ID)
							})
						})
					case len(thread.Messages) == 0:
						ui.Text(c, text.NewConversation).FillWidth().Padding(64, 24).TextAlign(ui.Center).
							FontSize(20).LineHeight(1.6).TextColor(paint(colors.Text.Muted))
					default:
						for _, msg := range thread.Messages {
							p.renderPrivateBubble(c, msg, bubbleMaxW, d)
						}
					}
				})
			})
		})

		// Bottom compose trigger (only when source supports writing private messages)
		if canSend {
			ui.Box(c.Key("private-compose-bar")).FillWidth().Shrink(0).Padding(12, 24, 18, 24).Children(func() {
				ui.Box(c).Absolute().Left(0).Right(0).Top(0).Height(1).Background(d.subtleBorder)
				focused := p.paintFocus() == privateComposeFocus
				bg, ink := d.subtleFill, paint(colors.Text.Secondary)
				if focused {
					bg, ink = paint(colors.Focus.Background), paint(colors.Focus.Foreground)
				}
				trigger := ui.Button(c.Key(string(privateComposeFocus)), "").Label(text.WritePrivateMessage).
					FillWidth().Height(48).Padding(0, 19).Radius(12).Border(1, d.subtleBorder).Background(bg).
					OnClick(func() { p.dispatch(func() { p.openPrivateCompose(active) }) })
				p.animateFocus(trigger, bg, ink)
				p.bind(trigger, privateComposeFocus, privateComposeRegion)
				trigger.Children(func() {
					ui.Row(c).Fill().Gap(13).AlignItems(ui.Center).Children(func() {
						ui.Icon(c, p.Assets.Icon("comment")).Size(22, 22).TextColor(ink)
						ui.Text(c, text.WritePrivateMessage).FontSize(20).TextColor(ink)
					})
				})
			})
		}
	})
}

func (p *PageContext) renderPrivateBubble(c *ui.Context, msg provider.PrivateMessage, maxW float32, d uiColors) {
	colors := p.Theme.Colors
	align := ui.Start
	bg := paint(colors.Surface.Raised)
	border := ui.Transparent
	if msg.FromSelf {
		align = ui.End
		bg = d.privateOwnBubble
		border = d.privateOwnBorder
	}
	ui.Column(c.Key("pm-" + msg.ID)).FillWidth().AlignItems(align).Children(func() {
		bubble := ui.Box(c).MaxWidth(maxW).Padding(13, 20).Radius(15).Background(bg)
		if border.A != 0 {
			bubble.Border(1, border)
		}
		bubble.Children(func() {
			ui.Text(c, msg.Body).FontSize(23).LineHeight(1.65).TextColor(paint(colors.Text.Primary))
		})
		if ts := formatPrivateTime(msg.SentAt); ts != "" {
			ui.Text(c, ts).Margin(8, 4, 0, 4).FontSize(14).TextColor(paint(colors.Text.Muted))
		}
	})
}

func (p *PageContext) openPrivateCompose(conv provider.PrivateConversation) {
	if p.host.PrivateMessages == nil || !p.host.PrivateMessages.CanSend() || conv.ID == "" {
		return
	}
	draft := &p.host.privateUI.draft
	if draft.conversationID != conv.ID {
		*draft = privateMessageDraft{
			conversationID: conv.ID,
			peer:           conv.Peer,
		}
	} else {
		draft.peer = conv.Peer
		draft.failure = ""
	}
	if !p.State.Push(app.Route{Page: app.PrivateMessageCompose}) {
		return
	}
	p.editPrivateDraft()
}

func (p *PageContext) editPrivateDraft() {
	if p.host.PrivateMessages != nil && p.host.PrivateMessages.Sending() {
		return
	}
	draft := &p.host.privateUI.draft
	draft.editing, draft.input, draft.failure = true, draft.body, ""
	p.host.moveFocus(privateComposeInput, app.Right, false)
}

func (p *PageContext) finishPrivateDraftEdit(key ui.TextKey) bool {
	draft := &p.host.privateUI.draft
	switch key.Key {
	case ui.KeyEnter:
		draft.body = key.Text()
		draft.preview.Y = 0
	case ui.KeyEscape:
	default:
		return false
	}
	draft.editing = false
	focus := privateComposeEdit
	if key.Key == ui.KeyEnter && draft.canSend() {
		focus = privateComposeSend
	}
	p.host.moveFocus(focus, app.Right, true)
	return true
}

func (p *PageContext) sendPrivateDraft() {
	pm := p.host.PrivateMessages
	draft := &p.host.privateUI.draft
	if pm == nil || pm.Sending() || !draft.canSend() {
		return
	}
	draft.failure = ""
	convID := draft.conversationID
	body := draft.body
	pm.SendMessage(convID, body, func(_ provider.PrivateMessage, err error) {
		if err != nil {
			if p.host.privateUI.draft.conversationID == convID {
				p.host.privateUI.draft.failure = p.privateOperationError(err, p.Assets.Text.Activity.PrivateSendFailed)
			}
			return
		}
		p.host.privateUI.draft = privateMessageDraft{}
		if p.State.Page == app.PrivateMessageCompose {
			p.State.Back()
		}
		p.host.showNoticeFeedback(p.Assets.Text.Activity.MessageSent)
	})
}

type privateComposePage struct{}

func (privateComposePage) Render(c *ui.Context, p *PageContext) {
	const region = "private-compose-modal"
	draft := &p.host.privateUI.draft
	text, detailText, colors := p.Assets.Text.Activity, p.Assets.Text.Detail, p.Theme.Colors
	sending := p.host.PrivateMessages != nil && p.host.PrivateMessages.Sending()
	p.region(region, nil)
	ui.Column(c).FillWidth().Lang(p.langTag()).Font(p.uiFont()).Children(func() {
		p.framelessHeader(c, "PRIVATE MESSAGE", "comment", text.WritePrivateMessage, "")
		ui.Row(c).Gap(15).AlignItems(ui.Center).Margin(0, 0, 24, 0).Children(func() {
			p.privatePeerAvatar(c, draft.peer, 40, 17)
			ui.Text(c, draft.peer.Name).FontSize(20).TextColor(paint(colors.Text.Secondary))
		})
		if draft.editing {
			field := ui.TextInput(c.Key("private-compose-input"), &draft.input).FillWidth().Height(105).FontSize(29).
				Label(text.WritePrivateMessage).Placeholder(detailText.KeyboardPlaceholder).MaxLength(provider.PrivateMessageLimit).
				ReturnKey(ui.ReturnDone).OnTextKey(p.finishPrivateDraftEdit)
			if p.active {
				field.FocusBind(&p.State.Focus, privateComposeInput)
				field.Focus(ui.FocusOptions{PreventScroll: true})
			}
		} else {
			p.drawPrivateDraftPreview(c, draft, region)
		}
		ui.Text(c, fmt.Sprintf("%d / %d", ui.UTF16Len(draft.body), provider.PrivateMessageLimit)).
			AlignSelf(ui.End).Margin(10, 0, 0, 0).FontSize(16).TextColor(paint(colors.Text.Muted))
		if draft.failure != "" {
			ui.Text(c.Key("private-compose-failure"), draft.failure).Margin(10, 0, 0, 0).FontSize(18).TextColor(paint(colors.Status.Warning.Foreground))
		}
		ui.Row(c.Key("private-compose-actions")).Gap(16).Margin(36, 0, 0, 0).AlignSelf(ui.Start).Children(func() {
			p.styledButton(c, text.EditMessage, "", privateComposeEdit, region, p.editPrivateDraft, p.framelessButtonAppearance()).
				AccessDisabled(sending)
			sendLabel := text.SendMessage
			if sending {
				sendLabel = text.SendingMessage
			}
			disabled := !draft.canSend() || sending
			button := p.styledButton(c, sendLabel, "send", privateComposeSend, region, p.sendPrivateDraft, p.framelessButtonAppearance()).
				Disabled(!draft.canSend()).AccessDisabled(sending).DimDisabled(false)
			if disabled {
				button.Opacity(0.35)
			}
		})
	})
}

func (p *PageContext) drawPrivateDraftPreview(c *ui.Context, draft *privateMessageDraft, region string) {
	body := draft.body
	hasDraft := draft.canSend()
	if !hasDraft {
		body = p.Assets.Text.Activity.PrivateDraftPlaceholder
	}
	preview := ui.Scroll(c.Key(string(privateComposePreview))).FillWidth().MinHeight(101).MaxHeight(174).
		TrackScroll(&draft.preview)
	preview.Children(func() {
		ink := paint(p.Theme.Colors.Text.Primary)
		if !hasDraft {
			ink = paint(p.Theme.Colors.Text.Muted)
		}
		ui.Text(c, body).FillWidth().Padding(17, 0, 0, 0).FontSize(29).LineHeight(1.8).TextColor(ink)
	})
	if hasDraft {
		p.bindDraftPreview(preview, privateComposePreview, region, &draft.preview, privateComposeEdit, 92)
	}
}
