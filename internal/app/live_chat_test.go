package app

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yuchanns/MikuTV/internal/provider"
)

type fakeLiveChatSub struct {
	events chan provider.LiveChatEvent
	closed atomic.Bool
	once   sync.Once
}

func newFakeLiveChatSub() *fakeLiveChatSub {
	return &fakeLiveChatSub{events: make(chan provider.LiveChatEvent, 32)}
}

func (s *fakeLiveChatSub) Events() <-chan provider.LiveChatEvent { return s.events }
func (s *fakeLiveChatSub) Close() {
	s.once.Do(func() {
		s.closed.Store(true)
		close(s.events)
	})
}

type fakeLiveChatClient struct {
	mu   sync.Mutex
	subs []*fakeLiveChatSub
	reqs []provider.LiveChatRequest
}

func (c *fakeLiveChatClient) OpenLiveChat(ctx context.Context, req provider.LiveChatRequest) (provider.LiveChatSubscription, error) {
	sub := newFakeLiveChatSub()
	c.mu.Lock()
	c.reqs = append(c.reqs, req)
	c.subs = append(c.subs, sub)
	c.mu.Unlock()
	context.AfterFunc(ctx, sub.Close)
	return sub, nil
}

func (c *fakeLiveChatClient) sub(index int) *fakeLiveChatSub {
	for range 100 {
		c.mu.Lock()
		if len(c.subs) > index {
			s := c.subs[index]
			c.mu.Unlock()
			return s
		}
		c.mu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}
	return nil
}

func TestLiveChatLifecycleDedupMuteReconnectAndIsolation(t *testing.T) {
	queue := make(chan func(), 128)
	drain := func() {
		for {
			select {
			case fn := <-queue:
				fn()
			case <-time.After(15 * time.Millisecond):
				return
			}
		}
	}

	client := &fakeLiveChatClient{}
	lc := NewLiveChat(func(fn func()) { queue <- fn }, func() {})
	lc.SetContext("src-a", 1, "u1", client, true)

	ref := provider.MediaRef{EntryID: "src-a", Kind: provider.Live, ID: "1001"}
	lc.Connect(ref, "1001")
	sub0 := client.sub(0)
	if sub0 == nil {
		t.Fatal("expected first live chat subscription to open")
	}
	drain()

	if snap := lc.Snapshot(); !snap.Active || snap.Status != provider.LiveChatConnecting {
		t.Fatalf("unexpected initial snapshot: %+v", snap)
	}

	// Connected + viewer count + incoming message.
	viewers := int64(25000)
	sub0.events <- provider.LiveChatEvent{Type: provider.LiveChatEventStatus, Status: provider.LiveChatConnected}
	sub0.events <- provider.LiveChatEvent{Type: provider.LiveChatEventViewers, Viewers: &viewers}
	sub0.events <- provider.LiveChatEvent{
		Type:    provider.LiveChatEventMessage,
		Message: &provider.LiveChatMessage{ID: "m1", Author: "Alice", Body: "Hello everyone!"},
	}
	drain()

	snap := lc.Snapshot()
	if snap.Status != provider.LiveChatConnected || snap.Viewers == nil || *snap.Viewers != 25000 {
		t.Fatalf("unexpected connected/viewers snapshot: %+v", snap)
	}
	if len(snap.Messages) != 1 || len(snap.Danmaku) != 1 || snap.Messages[0].Body != "Hello everyone!" {
		t.Fatalf("unexpected messages/danmaku: %+v", snap)
	}

	// Local optimistic send + server self-echo dedup within window.
	lc.RecordLocalMessage("Me", "Great performance!", time.Now())
	sub0.events <- provider.LiveChatEvent{
		Type:    provider.LiveChatEventMessage,
		Message: &provider.LiveChatMessage{ID: "srv-self", Author: "Me", Body: "Great performance!", Self: true},
	}
	drain()

	snap = lc.Snapshot()
	if len(snap.Messages) != 2 || !snap.Messages[1].Self || snap.Messages[1].Body != "Great performance!" {
		t.Fatalf("expected deduplicated self message (count=2), got %+v", snap.Messages)
	}

	// Remote mute and unmute events.
	mutedTrue := true
	mutedFalse := false
	sub0.events <- provider.LiveChatEvent{Type: provider.LiveChatEventMute, Muted: &mutedTrue, MuteReason: "Slow mode"}
	drain()
	if !lc.Snapshot().Muted {
		t.Fatal("expected Muted=true after ROOM_SILENT_ON event")
	}
	sub0.events <- provider.LiveChatEvent{Type: provider.LiveChatEventMute, Muted: &mutedFalse}
	drain()
	if lc.Snapshot().Muted {
		t.Fatal("expected Muted=false after ROOM_SILENT_OFF event")
	}

	// Reconnect closes old subscription and opens a fresh one.
	lc.Reconnect()
	sub1 := client.sub(1)
	if sub1 == nil {
		t.Fatal("expected second live chat subscription after Reconnect")
	}
	drain()
	if !sub0.closed.Load() {
		t.Fatal("expected old subscription to be closed on Reconnect")
	}

	// Entry switch isolates immediately: closes sub1 and ignores any stale events.
	lc.SetContext("src-b", 2, "u1", client, true)
	drain()
	if !sub1.closed.Load() {
		t.Fatal("expected active subscription to be closed on entry switch")
	}
	if snap := lc.Snapshot(); snap.Active || len(snap.Messages) != 0 {
		t.Fatalf("expected clean state after entry switch, got %+v", snap)
	}

	// Connecting with wrong EntryID is rejected/ignored.
	lc.Connect(ref, "1001")
	if lc.Snapshot().Active {
		t.Fatal("must not connect room belonging to another entry")
	}

	// Account switch also cancels and isolates active subscription.
	refB := provider.MediaRef{EntryID: "src-b", Kind: provider.Live, ID: "2002"}
	lc.Connect(refB, "2002")
	sub2 := client.sub(2)
	if sub2 == nil {
		t.Fatal("expected third subscription on src-b")
	}
	drain()
	lc.SetContext("src-b", 2, "u2", client, true)
	drain()
	if !sub2.closed.Load() || lc.Snapshot().Active {
		t.Fatal("expected account switch to cancel and reset live chat")
	}

	// Leave room cancels subscription cleanly.
	lc.Connect(refB, "2002")
	sub3 := client.sub(3)
	if sub3 == nil {
		t.Fatal("expected fourth subscription")
	}
	drain()
	lc.Leave()
	drain()
	if !sub3.closed.Load() || lc.Snapshot().Active {
		t.Fatal("expected Leave() to close subscription and clear state")
	}
}
