package tvui

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/provider"
)

const (
	playerLiveChatScroll    app.FocusID = "player/live-chat-scroll"
	playerLiveChatReconnect app.FocusID = "player/live-chat-reconnect"
	playerLiveCompose       app.FocusID = "player/live-compose"
	playerLiveChatGroup                 = "player-live-chat"
)

func (p *PageContext) liveChatPanel(c *ui.Context, drawer ui.Element, current playbackItem) {
	v, s := p.host, &p.host.player
	colors, text := p.Theme.Colors, p.Assets.Text
	entry, _ := p.State.Current()
	canWrite := entry.Capabilities.Has(provider.WriteLiveChat)

	var snap app.LiveChatSnapshot
	if v.LiveChat != nil {
		snap = v.LiveChat.Snapshot()
	}
	if s.liveChatAutoFollow && snap.Revision != s.liveChatLastRev {
		s.liveChatLastRev = snap.Revision
		s.liveChatScroll.Y = math.MaxFloat32
	}

	viewers := snap.Viewers
	if viewers == nil {
		viewers = current.item.Views
	}
	showReconnect := snap.Status == provider.LiveChatDisconnected && !snap.Ended
	composeActive := canWrite && !snap.Muted

	p.region(playerLiveChatGroup, focusEdges{})

	drawer.Children(func() {
		// Header: [LIVE] Live Chat + viewer count
		ui.Row(c.Key("live-chat-header")).FillWidth().Height(110).Padding(25, 31).Gap(14).
			AlignItems(ui.Center).Children(func() {
			ui.Row(c).Padding(5, 10).Gap(6).AlignItems(ui.Center).Radius(5).
				Background(paint(colors.Accent.Primary)).Children(func() {
				ui.Box(c).Size(6, 6).Radius(3).Background(paint(colors.Accent.Foreground))
				ui.Text(c, text.Following.LiveBadge).FontSize(13).FontWeight(650).LetterSpacing(0.8).
					TextColor(paint(colors.Accent.Foreground))
			})
			ui.Text(c, text.Playback.LiveChatTitle).FontSize(27).FontWeight(550).
				TextColor(paint(colors.Text.Primary)).SingleLine()
			ui.Spacer(c).Grow(1)
			if viewers != nil && *viewers > 0 {
				ui.Text(c, catalogViews(*viewers, text.Home)+" "+text.Playback.LiveChatViewers).
					FontSize(16).TextColor(paint(colors.Text.Secondary)).SingleLine()
			}
		})
		ui.Box(c).FillWidth().Height(1).Background(withAlpha(colors.Playback.Foreground, 0x0f))

		if snap.Status == provider.LiveChatConnecting || snap.Status == provider.LiveChatReconnecting || showReconnect {
			statusText := text.Playback.LiveChatConnecting
			statusColor := paint(colors.Text.Secondary)
			switch snap.Status {
			case provider.LiveChatReconnecting:
				statusText = text.Playback.LiveChatReconnecting
				statusColor = paint(colors.Status.Warning.Foreground)
			case provider.LiveChatDisconnected:
				statusText = text.Playback.LiveChatDisconnected
				statusColor = paint(colors.Status.Error.Foreground)
			}
			ui.Row(c.Key("live-chat-status")).FillWidth().Padding(12, 31).Gap(12).AlignItems(ui.Center).
				Background(withAlpha(colors.Surface.Raised, 0x70)).Children(func() {
				ui.Text(c, statusText).Grow(1).Shrink(1).FontSize(16).TextColor(statusColor).MaxLines(1)
				if showReconnect {
					p.styledButton(c, text.Playback.Reconnect, "refresh", playerLiveChatReconnect, playerLiveChatGroup, func() {
						if v.LiveChat != nil {
							v.LiveChat.Reconnect()
						}
						v.State.Focus, v.reveal = playerLiveChatScroll, true
					}, buttonAppearance{
						height: 40, padding: 14, radius: 9, fontSize: 16, fontWeight: 500, iconSize: 16, iconGap: 8,
						background: withAlpha(colors.Playback.Foreground, 0x10), foreground: paint(colors.Text.Primary),
					})
				}
			})
		}

		// Scrollable chat message feed (focus indicated by subtle background tint, no vertical bar per §7).
		scrollFocused := p.paintFocus() == playerLiveChatScroll
		scrollFill := ui.Transparent
		if scrollFocused {
			scrollFill = withAlpha(colors.Focus.Background, 0x06)
		}
		feed := ui.Scroll(c.Key("live-chat-scroll")).FillWidth().Grow(1).TrackScroll(&s.liveChatScroll).
			Background(scrollFill).Focusable().Label(text.Playback.LiveChatTitle)
		p.bindDirection(feed, playerLiveChatScroll, playerLiveChatGroup, func(dir app.Direction) bool {
			switch dir {
			case app.Up:
				if s.liveChatScroll.Y > 0 {
					s.liveChatAutoFollow = false
					s.liveChatScroll.ScrollTo(0, max(0, s.liveChatScroll.Y-140), focusScroll)
					return true
				}
				if showReconnect {
					v.State.Focus, v.reveal = playerLiveChatReconnect, true
					return true
				}
				return true
			case app.Down:
				if s.liveChatScroll.Y < s.liveChatScroll.MaxY-2 {
					nextY := min(s.liveChatScroll.MaxY, s.liveChatScroll.Y+140)
					s.liveChatScroll.ScrollTo(0, nextY, focusScroll)
					if nextY >= s.liveChatScroll.MaxY-4 {
						s.liveChatAutoFollow = true
					}
					return true
				}
				s.liveChatAutoFollow = true
				if composeActive {
					v.State.Focus, v.reveal = playerLiveCompose, true
				}
				return true
			case app.Left, app.Right:
				return true
			}
			return false
		})
		feed.Children(func() {
			ui.Column(c).FillWidth().Padding(18, 30, 24, 30).Gap(22).Children(func() {
				if len(snap.Messages) == 0 {
					empty := text.Playback.LiveChatEmpty
					if snap.Status == provider.LiveChatConnecting {
						empty = text.Playback.LiveChatConnecting
					}
					ui.Text(c, empty).Margin(24, 0, 0, 0).FontSize(19).TextColor(paint(colors.Text.Muted))
					return
				}
				for idx, msg := range snap.Messages {
					author := msg.Author
					if author == "" {
						author = text.Playback.LiveChatGuest
					}
					authorColor := paint(colors.Text.Secondary)
					if msg.Self {
						authorColor = paint(colors.Accent.Soft)
					}
					key := msg.ID
					if key == "" {
						key = fmt.Sprintf("m-%d", idx)
					}
					ui.Column(c.Key("chat-msg/"+key)).FillWidth().Gap(5).Children(func() {
						ui.Text(c, author).FillWidth().FontSize(17).LineHeight(1.45).TextColor(authorColor).MaxLines(1)
						ui.Text(c, msg.Body).FillWidth().FontSize(23).LineHeight(1.6).TextColor(paint(colors.Text.Primary))
					})
				}
			})
		})

		// Bottom compose bar (hidden if source does not declare live.chatWrite; disabled when muted).
		if canWrite {
			ui.Box(c).FillWidth().Height(1).Background(withAlpha(colors.Playback.Foreground, 0x0f))
			ui.Box(c.Key("live-chat-compose-bar")).FillWidth().Height(85).Padding(15, 25).Children(func() {
				if snap.Muted {
					mutedLabel := text.Playback.LiveChatMuted
					ui.Row(c.Key("live-chat-muted")).Fill().Padding(0, 17).Gap(13).AlignItems(ui.Center).
						Radius(12).Background(paint(colors.Disabled.Background)).
						Border(1, withAlpha(colors.Playback.Foreground, 0x10)).IgnoreInput(true).Children(func() {
						ui.Icon(c, p.Assets.Icon("comment")).Size(23, 23).TextColor(paint(colors.Disabled.Foreground))
						ui.Text(c, mutedLabel).FontSize(21).TextColor(paint(colors.Disabled.Foreground)).SingleLine()
					})
					return
				}
				focused := p.paintFocus() == playerLiveCompose
				fill, ink := ui.Transparent, paint(colors.Text.Secondary)
				border := withAlpha(colors.Playback.Foreground, 0x18)
				if focused {
					fill, ink = paint(colors.Focus.Background), paint(colors.Focus.Foreground)
					border = paint(colors.Focus.Ring)
				}
				btn := ui.Button(c.Key(string(playerLiveCompose)), "").Label(text.Playback.LiveChatSend).
					Fill().Padding(0, 17).Radius(12).Background(fill).Border(1, border).
					OnClick(func() { p.dispatch(func() { p.beginLiveChatCompose(playerLiveCompose) }) })
				p.bindDirection(btn, playerLiveCompose, playerLiveChatGroup, func(dir app.Direction) bool {
					if dir == app.Up {
						v.State.Focus, v.reveal = playerLiveChatScroll, true
						return true
					}
					if dir == app.Left || dir == app.Right || dir == app.Down {
						return true
					}
					return false
				})
				btn.Children(func() {
					ui.Row(c).Fill().Gap(13).AlignItems(ui.Center).Children(func() {
						ui.Icon(c, p.Assets.Icon("comment")).Size(23, 23).TextColor(ink)
						ui.Text(c, text.Playback.LiveChatSend).FontSize(21).TextColor(ink).SingleLine()
					})
				})
			})
		}
	})
}

func (p *PageContext) beginLiveChatCompose(returnFocus app.FocusID) {
	p.withPlayerAccount(func() {
		v := p.host
		if v.LiveChat != nil && v.LiveChat.Snapshot().Muted {
			v.player.content.notice(p.Assets.Text.Playback.LiveChatMuted)
			return
		}
		v.player.content.draft = &danmakuDraft{
			mode:        provider.DanmakuScroll,
			live:        true,
			returnFocus: returnFocus,
			edit:        true,
		}
		p.State.Focus, v.reveal = danmakuInput, true
	})
}

func (p *PageContext) sendLiveChat(draft *danmakuDraft) {
	if draft.cancel != nil || draft.edit || strings.TrimSpace(draft.body) == "" {
		return
	}
	v, s := p.host, &p.host.player
	if v.LiveChat != nil && v.LiveChat.Snapshot().Muted {
		draft.muted = true
		return
	}
	state := v.Playback.State()
	body := strings.TrimSpace(draft.body)
	post := provider.LiveChatPost{
		Ref:    state.Target.Ref,
		ItemID: state.Resolution.Plan.ItemID,
		Owner:  v.Playback.Owner(),
		Text:   body,
	}
	author := ""
	if account, ok := currentAccount(v.Auth.Accounts()); ok {
		author = account.Name
	}
	draft.failure = ""
	draft.failureDetail = ""
	draft.muted = false
	draft.cancel = v.Catalog.SendLiveChat(post, func(res provider.LiveChatSendResult, err error) {
		draft.cancel = nil
		if err != nil {
			if provider.CodeOf(err) == provider.NeedsSignIn {
				v.openSignIn()
				return
			}
			draft.failure = provider.CodeOf(err)
			var pe *provider.Error
			if errors.As(err, &pe) {
				draft.failureDetail = pe.Message
			}
			p.State.Focus, v.reveal = danmakuSend, true
			return
		}
		if res.Muted {
			draft.muted = true
			if v.LiveChat != nil {
				v.LiveChat.MarkMuted(res.MuteReason)
			}
			p.State.Focus, v.reveal = danmakuEdit, true
			return
		}
		returnFocus := draft.returnFocus
		s.content.draft = nil
		if v.LiveChat != nil {
			v.LiveChat.RecordLocalMessage(author, body, time.Now())
		}
		s.content.notice(p.Assets.Text.Playback.DanmakuSent)
		if returnFocus == "" {
			returnFocus = playerLiveCompose
		}
		if returnFocus == playerLiveCompose && s.panel != playerComments {
			returnFocus = playerComments
		}
		p.State.Focus, v.reveal = returnFocus, true
	})
}

type liveDanmakuNodeKey struct {
	seq uint64
}

func (s *playbackContent) needsLiveDanmakuFrame(v *View, state app.PlaybackState, now time.Time) bool {
	snap := v.DanmakuRules.Snapshot()
	display := snap.Display
	entry, _ := v.State.Current()
	if v.LiveChat == nil || !display.Show || display.Density <= 0 ||
		!entry.Capabilities.Has(provider.LiveChat) ||
		!state.Clock.HasFrame || state.Clock.Ended || state.LiveEnded || state.Failure != "" ||
		!state.Playing || state.Clock.Buffering {
		return false
	}
	v.DanmakuRules.EnsureSynced()
	dms := v.LiveChat.Snapshot().Danmaku
	for i := len(dms) - 1; i >= 0; i-- {
		elapsed := now.Sub(dms[i].ReceivedAt).Seconds()
		if elapsed >= 0 && elapsed < danmakuMovingTime {
			if !app.DanmakuBlockedByRules(snap.Rules, dms[i].Body, dms[i].AuthorID, "") {
				return true
			}
		}
		if elapsed >= danmakuMovingTime {
			break
		}
	}
	return false
}

func (p *PageContext) drawLiveDanmaku(c *ui.Context, state app.PlaybackState) {
	v := p.host
	snap := v.DanmakuRules.Snapshot()
	display := snap.Display
	if v.LiveChat == nil || display.Density <= 0 || p.Width <= 0 {
		return
	}
	dms := v.LiveChat.Snapshot().Danmaku
	if len(dms) == 0 {
		return
	}
	now := c.Now()
	clamped := min(max(display.Density, 10), 100)
	scrollMax, _, _ := danmakuLaneBounds(display.Area, display.FontSize)
	lanes := max(2, min(scrollMax, (clamped*scrollMax+99)/100))
	fontSize, stride := danmakuFontMetrics(display.FontSize)
	ink := paint(p.Theme.Colors.Playback.Foreground)
	selfInk := paint(p.Theme.Colors.Accent.Soft)
	shadow := paint(p.Theme.Colors.Playback.SubtitleOutline)
	opacity := float32(display.Opacity) / 100

	for _, dm := range dms {
		elapsed := now.Sub(dm.ReceivedAt).Seconds()
		if elapsed < 0 || elapsed >= danmakuMovingTime {
			continue
		}
		if app.DanmakuBlockedByRules(snap.Rules, dm.Body, dm.AuthorID, "") {
			continue
		}
		w := estimateDanmakuWidthScaled(dm.Body, display.FontSize)
		lane := int(dm.Seq % uint64(lanes))
		x := p.Width - (p.Width+w)*float32(elapsed/danmakuMovingTime)
		color := ink
		if dm.Self {
			color = selfInk
		}
		ui.Text(c.Key(liveDanmakuNodeKey{dm.Seq}), dm.Body).Absolute().
			Left(0).Top(153 + float32(lane)*stride).Width(w).MaxLines(1).
			FontSize(fontSize).FontWeight(640).Opacity(opacity).
			TextColor(color).TextShadow(0, 1, 4, shadow).Translate(x, 0).Cache()
	}
}
