package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/yuchanns/MikuTV/internal/provider"
)

const (
	maxLiveChatMessages = 200
	maxLiveDanmakuItems = 80
	selfEchoDedupWindow = 10 * time.Second
)

type LiveDanmaku struct {
	Seq        uint64
	AuthorID   string
	Body       string
	Self       bool
	ReceivedAt time.Time
}

type LiveChatSnapshot struct {
	Active     bool
	Ref        provider.MediaRef
	ItemID     string
	Status     provider.LiveChatStatus
	Error      provider.ErrorCode
	Messages   []provider.LiveChatMessage
	Danmaku    []LiveDanmaku
	Viewers    *int64
	Muted      bool
	MuteReason string
	Ended      bool
	Revision   uint64
}

type selfEchoRecord struct {
	body string
	at   time.Time
}

// LiveChat owns the live-room chat subscription for the active entry and account.
// Every state mutation happens on the application thread through post.
type LiveChat struct {
	post    func(func())
	changed func()

	entryID   string
	entryGen  uint64
	owner     string
	client    provider.LiveChatClient
	supported bool

	subGen  uint64
	cancel  context.CancelFunc
	sub     provider.LiveChatSubscription
	danmaku uint64
	sent    []selfEchoRecord
	snap    LiveChatSnapshot
}

func NewLiveChat(post func(func()), changed func()) *LiveChat {
	return &LiveChat{post: post, changed: changed}
}

func (c *LiveChat) Snapshot() LiveChatSnapshot {
	return c.snap
}

func (c *LiveChat) SetContext(entryID string, entryGen uint64, owner string, client provider.LiveChatClient, supported bool) {
	if c.entryID == entryID && c.entryGen == entryGen && c.owner == owner && c.client == client && c.supported == supported {
		return
	}
	c.stopLocked()
	c.entryID = entryID
	c.entryGen = entryGen
	c.owner = owner
	c.client = client
	c.supported = supported
	c.sent = nil
	c.snap = LiveChatSnapshot{}
	c.notify()
}

func (c *LiveChat) Connect(ref provider.MediaRef, itemID string) {
	if ref.Kind != provider.Live || ref.ID == "" || ref.EntryID != c.entryID {
		c.Leave()
		return
	}
	if !c.supported || c.client == nil {
		c.stopLocked()
		c.snap = LiveChatSnapshot{Ref: ref, ItemID: itemID}
		c.notify()
		return
	}
	c.stopLocked()
	c.subGen++
	gen := c.subGen
	entryID, entryGen, owner := c.entryID, c.entryGen, c.owner
	client := c.client
	c.sent = nil
	c.snap = LiveChatSnapshot{
		Active:   true,
		Ref:      ref,
		ItemID:   itemID,
		Status:   provider.LiveChatConnecting,
		Revision: c.snap.Revision + 1,
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel
	req := provider.LiveChatRequest{Ref: ref, ItemID: itemID}

	go func() {
		sub, err := client.OpenLiveChat(ctx, req)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			c.post(func() {
				if !c.matches(entryID, entryGen, owner, gen) {
					return
				}
				c.snap.Status = provider.LiveChatDisconnected
				c.snap.Error = provider.CodeOf(err)
				c.snap.Revision++
				c.notify()
			})
			return
		}
		c.post(func() {
			if !c.matches(entryID, entryGen, owner, gen) {
				sub.Close()
				return
			}
			c.sub = sub
		})
		for event := range sub.Events() {
			if ctx.Err() != nil {
				break
			}
			ev := event
			c.post(func() {
				if !c.matches(entryID, entryGen, owner, gen) {
					return
				}
				c.applyEvent(ev, time.Now())
			})
		}
		sub.Close()
		if ctx.Err() != nil {
			return
		}
		c.post(func() {
			if !c.matches(entryID, entryGen, owner, gen) {
				return
			}
			if c.sub == sub {
				c.sub = nil
			}
			if !c.snap.Ended && c.snap.Status != provider.LiveChatDisconnected {
				c.snap.Status = provider.LiveChatDisconnected
				c.snap.Revision++
				c.notify()
			}
		})
	}()
	c.notify()
}

func (c *LiveChat) Reconnect() {
	if !c.snap.Active || c.snap.Ref.ID == "" {
		return
	}
	c.Connect(c.snap.Ref, c.snap.ItemID)
}

func (c *LiveChat) Leave() {
	if !c.snap.Active && c.cancel == nil && c.sub == nil {
		return
	}
	c.stopLocked()
	c.sent = nil
	c.snap = LiveChatSnapshot{}
	c.notify()
}

func (c *LiveChat) Close() {
	c.stopLocked()
	c.sent = nil
	c.snap = LiveChatSnapshot{}
}

func (c *LiveChat) RecordLocalMessage(author, body string, now time.Time) {
	body = strings.TrimSpace(body)
	if !c.snap.Active || body == "" {
		return
	}
	if now.IsZero() {
		now = time.Now()
	}
	c.danmaku++
	msg := provider.LiveChatMessage{
		ID:        fmt.Sprintf("local-%d", c.danmaku),
		Author:    strings.TrimSpace(author),
		Body:      body,
		Self:      true,
		Timestamp: now.Unix(),
	}
	c.sent = append(c.sent, selfEchoRecord{body: body, at: now})
	c.appendMessage(msg, now)
	c.snap.Revision++
	c.notify()
}

func (c *LiveChat) MarkMuted(reason string) {
	if !c.snap.Active {
		return
	}
	c.snap.Muted = true
	if trimmed := strings.TrimSpace(reason); trimmed != "" {
		c.snap.MuteReason = trimmed
	}
	c.snap.Revision++
	c.notify()
}

func (c *LiveChat) applyEvent(ev provider.LiveChatEvent, now time.Time) {
	switch ev.Type {
	case provider.LiveChatEventStatus:
		c.snap.Status = ev.Status
		c.snap.Error = ev.Error
	case provider.LiveChatEventMessage:
		if ev.Message == nil {
			return
		}
		if ev.Message.Self && c.consumeSelfEcho(ev.Message.Body, now) {
			return
		}
		c.appendMessage(*ev.Message, now)
	case provider.LiveChatEventViewers:
		if ev.Viewers != nil {
			v := *ev.Viewers
			c.snap.Viewers = &v
		}
	case provider.LiveChatEventMute:
		if ev.Muted != nil {
			c.snap.Muted = *ev.Muted
			if !*ev.Muted {
				c.snap.MuteReason = ""
			} else if ev.MuteReason != "" {
				c.snap.MuteReason = ev.MuteReason
			}
		}
	case provider.LiveChatEventEnded:
		c.snap.Ended = true
		c.snap.Status = provider.LiveChatDisconnected
	default:
		return
	}
	c.snap.Revision++
	c.notify()
}

func (c *LiveChat) appendMessage(msg provider.LiveChatMessage, now time.Time) {
	msgs := append([]provider.LiveChatMessage(nil), c.snap.Messages...)
	msgs = append(msgs, msg)
	if len(msgs) > maxLiveChatMessages {
		msgs = msgs[len(msgs)-maxLiveChatMessages:]
	}
	c.snap.Messages = msgs

	c.danmaku++
	dms := append([]LiveDanmaku(nil), c.snap.Danmaku...)
	dms = append(dms, LiveDanmaku{
		Seq:        c.danmaku,
		AuthorID:   msg.AuthorID,
		Body:       msg.Body,
		Self:       msg.Self,
		ReceivedAt: now,
	})
	if len(dms) > maxLiveDanmakuItems {
		dms = dms[len(dms)-maxLiveDanmakuItems:]
	}
	c.snap.Danmaku = dms
}

func (c *LiveChat) consumeSelfEcho(body string, now time.Time) bool {
	trimmed := strings.TrimSpace(body)
	kept := c.sent[:0]
	matched := false
	for _, rec := range c.sent {
		if now.Sub(rec.at) > selfEchoDedupWindow {
			continue
		}
		if !matched && rec.body == trimmed {
			matched = true
			continue
		}
		kept = append(kept, rec)
	}
	c.sent = kept
	return matched
}

func (c *LiveChat) matches(entryID string, entryGen uint64, owner string, subGen uint64) bool {
	return c.entryID == entryID && c.entryGen == entryGen && c.owner == owner && c.subGen == subGen
}

func (c *LiveChat) stopLocked() {
	c.subGen++
	if c.cancel != nil {
		c.cancel()
		c.cancel = nil
	}
	if c.sub != nil {
		c.sub.Close()
		c.sub = nil
	}
}

func (c *LiveChat) notify() {
	if c.changed != nil {
		c.changed()
	}
}
