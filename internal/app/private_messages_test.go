package app

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/yuchanns/MikuTV/internal/provider"
)

type stubPrivateMessagesBackend struct {
	mu            sync.Mutex
	conversations func(context.Context, provider.PrivateConversationsRequest) (provider.PrivateConversationsPage, error)
	messages      func(context.Context, provider.PrivateMessagesRequest) (provider.PrivateMessagesPage, error)
	send          func(context.Context, provider.PrivateMessageSendRequest) (provider.PrivateMessageSendResult, error)
	markRead      func(context.Context, provider.PrivateMessageReadRequest) (provider.PrivateMessageReadResult, error)
}

func (s *stubPrivateMessagesBackend) ReadPrivateConversations(ctx context.Context, req provider.PrivateConversationsRequest) (provider.PrivateConversationsPage, error) {
	return s.conversations(ctx, req)
}

func (s *stubPrivateMessagesBackend) ReadPrivateMessages(ctx context.Context, req provider.PrivateMessagesRequest) (provider.PrivateMessagesPage, error) {
	return s.messages(ctx, req)
}

func (s *stubPrivateMessagesBackend) SendPrivateMessage(ctx context.Context, req provider.PrivateMessageSendRequest) (provider.PrivateMessageSendResult, error) {
	return s.send(ctx, req)
}

func (s *stubPrivateMessagesBackend) MarkPrivateMessagesRead(ctx context.Context, req provider.PrivateMessageReadRequest) (provider.PrivateMessageReadResult, error) {
	return s.markRead(ctx, req)
}

func TestPrivateMessagesSwitchingPaginationFailureAndAccountIsolation(t *testing.T) {
	queued := make(chan func(), 64)
	post := func(fn func()) { queued <- fn }
	drain := func(done func() bool) {
		t.Helper()
		timeout := time.After(3 * time.Second)
		for !done() {
			select {
			case fn := <-queued:
				fn()
			case <-timeout:
				t.Fatal("timed out waiting for private message async update")
			}
		}
	}

	var sendFail bool
	var acked []provider.PrivateMessageReadRequest
	accountABlock := make(chan struct{})
	defer close(accountABlock)
	slowEntered := make(chan struct{}, 1)
	accountACanceled := make(chan struct{}, 1)

	backendA := &stubPrivateMessagesBackend{
		conversations: func(ctx context.Context, req provider.PrivateConversationsRequest) (provider.PrivateConversationsPage, error) {
			if req.Summary {
				u := 3
				return provider.PrivateConversationsPage{Unread: &u}, nil
			}
			if req.Cursor == "" {
				u := 3
				return provider.PrivateConversationsPage{
					Unread:     &u,
					NextCursor: "ts-page-2",
					Conversations: []provider.PrivateConversation{
						{ID: "c1", Peer: provider.PrivatePeer{ID: "c1", Name: "Peer One"}, Preview: "Hey", Unread: 1, LastSeq: "10"},
						{ID: "c2", Peer: provider.PrivatePeer{ID: "c2", Name: "Peer Two"}, Preview: "Hi two", Unread: 2, LastSeq: "20"},
					},
				}, nil
			}
			if req.Cursor == "ts-page-2" {
				return provider.PrivateConversationsPage{
					Conversations: []provider.PrivateConversation{
						{ID: "c3", Peer: provider.PrivatePeer{ID: "c3", Name: "Peer Three"}, Preview: "Older chat", Unread: 0, LastSeq: "5"},
					},
				}, nil
			}
			// Stalled slow call for isolation test
			select {
			case <-ctx.Done():
				accountACanceled <- struct{}{}
				return provider.PrivateConversationsPage{}, ctx.Err()
			case <-accountABlock:
				return provider.PrivateConversationsPage{}, nil
			}
		},
		messages: func(ctx context.Context, req provider.PrivateMessagesRequest) (provider.PrivateMessagesPage, error) {
			if req.ConversationID == "c1" && req.Cursor == "" {
				return provider.PrivateMessagesPage{
					HasMore:    true,
					NextCursor: "seq-9",
					Messages: []provider.PrivateMessage{
						{ID: "m10", Seq: "10", SenderID: "c1", Body: "Latest from Peer One", SentAt: 100},
					},
				}, nil
			}
			if req.ConversationID == "c1" && req.Cursor == "seq-9" {
				return provider.PrivateMessagesPage{
					HasMore: false,
					Messages: []provider.PrivateMessage{
						{ID: "m9", Seq: "9", SenderID: "c1", Body: "Older from Peer One", SentAt: 90},
					},
				}, nil
			}
			if req.ConversationID == "c2" {
				return provider.PrivateMessagesPage{
					Messages: []provider.PrivateMessage{
						{ID: "m20", Seq: "20", SenderID: "c2", Body: "From Peer Two", SentAt: 200},
					},
				}, nil
			}
			if req.ConversationID == "slow-iso" {
				slowEntered <- struct{}{}
				select {
				case <-ctx.Done():
					accountACanceled <- struct{}{}
					return provider.PrivateMessagesPage{}, ctx.Err()
				case <-accountABlock:
					return provider.PrivateMessagesPage{Messages: []provider.PrivateMessage{{ID: "leak", Body: "leaked"}}}, nil
				}
			}
			return provider.PrivateMessagesPage{}, nil
		},
		send: func(_ context.Context, req provider.PrivateMessageSendRequest) (provider.PrivateMessageSendResult, error) {
			if sendFail {
				return provider.PrivateMessageSendResult{}, &provider.Error{Code: provider.Network}
			}
			return provider.PrivateMessageSendResult{
				Message: provider.PrivateMessage{
					ID: "m-sent-1", SenderID: "owner-a", FromSelf: true, Body: req.Body, SentAt: 300,
				},
			}, nil
		},
		markRead: func(_ context.Context, req provider.PrivateMessageReadRequest) (provider.PrivateMessageReadResult, error) {
			acked = append(acked, req)
			var remaining int
			if req.All {
				remaining = 0
			} else if req.ConversationID == "c1" {
				remaining = 2
			} else {
				remaining = 0
			}
			return provider.PrivateMessageReadResult{Unread: &remaining}, nil
		},
	}

	entry := Entry{
		ID: "sample-source", Generation: 1,
		Capabilities: provider.Capabilities{provider.SignIn, provider.ReadPrivateMessages, provider.WritePrivateMessages},
	}

	pm := NewPrivateMessages(post, func() {})
	defer pm.Close()

	// 1. Bind Account A -> load page 1 + auto-select c1 + auto-ack c1 unread.
	pm.Bind(entry, "owner-a", backendA, backendA)
	pm.EnsureConversations()
	drain(func() bool {
		return pm.Conversations().Phase == CatalogReady && pm.Thread("c1").Phase == CatalogReady && pm.UnreadCount() == 2
	})
	if pm.ActiveConversationID() != "c1" || len(pm.Conversations().Conversations) != 2 {
		t.Fatalf("unexpected initial conversations state: active=%q len=%d", pm.ActiveConversationID(), len(pm.Conversations().Conversations))
	}

	// 2. Load older messages in c1 -> prepends m9 before m10.
	pm.LoadOlderMessages("c1")
	drain(func() bool { return len(pm.Thread("c1").Messages) == 2 })
	if pm.Thread("c1").Messages[0].ID != "m9" || pm.Thread("c1").Messages[1].ID != "m10" || pm.Thread("c1").HasMore {
		t.Fatalf("older pagination did not prepend cleanly: %+v", pm.Thread("c1"))
	}

	// 3. Load page 2 of conversations -> appends c3.
	pm.LoadMoreConversations()
	drain(func() bool { return len(pm.Conversations().Conversations) == 3 })
	if pm.Conversations().Conversations[2].ID != "c3" || pm.Conversations().NextCursor != "" {
		t.Fatalf("conversation pagination mismatch: %+v", pm.Conversations())
	}

	// 4. Switch conversation to c2 -> marks c2 read and loads c2 thread.
	pm.SelectConversation("c2")
	drain(func() bool { return pm.Thread("c2").Phase == CatalogReady && pm.UnreadCount() == 0 })
	if pm.ActiveConversationID() != "c2" || pm.Thread("c2").Messages[0].ID != "m20" {
		t.Fatalf("conversation switch mismatch: active=%q thread=%+v", pm.ActiveConversationID(), pm.Thread("c2"))
	}

	// 5. Send failure leaves messages untouched; retry succeeds, appends message, and promotes c2 to top.
	sendFail = true
	var sendErr error
	doneCh := make(chan struct{}, 1)
	if !pm.SendMessage("c2", "Hello two", func(_ provider.PrivateMessage, err error) {
		sendErr = err
		doneCh <- struct{}{}
	}) {
		t.Fatal("SendMessage did not start")
	}
	drain(func() bool { return !pm.Sending() })
	<-doneCh
	if provider.CodeOf(sendErr) != provider.Network || len(pm.Thread("c2").Messages) != 1 {
		t.Fatalf("send failure should not mutate thread: err=%v msgs=%+v", sendErr, pm.Thread("c2").Messages)
	}

	sendFail = false
	sendErr = nil
	if !pm.SendMessage("c2", "Hello two", func(_ provider.PrivateMessage, err error) {
		sendErr = err
		doneCh <- struct{}{}
	}) {
		t.Fatal("SendMessage retry did not start")
	}
	drain(func() bool { return !pm.Sending() })
	<-doneCh
	if sendErr != nil || len(pm.Thread("c2").Messages) != 2 || pm.Conversations().Conversations[0].ID != "c2" ||
		pm.Conversations().Conversations[0].Preview != "Hello two" {
		t.Fatalf("send success did not append/promote c2: err=%v convs=%+v msgs=%+v",
			sendErr, pm.Conversations().Conversations, pm.Thread("c2").Messages)
	}

	// 6. Account isolation: start slow fetch on Account A, then switch to Account B -> cancels A & isolates B.
	pm.OpenPeer(provider.PrivatePeer{ID: "slow-iso", Name: "SlowPeer"})
	select {
	case <-slowEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("slow Account A fetch did not start")
	}
	backendB := &stubPrivateMessagesBackend{
		conversations: func(_ context.Context, req provider.PrivateConversationsRequest) (provider.PrivateConversationsPage, error) {
			u := 0
			return provider.PrivateConversationsPage{
				Unread: &u,
				Conversations: []provider.PrivateConversation{
					{ID: "b1", Peer: provider.PrivatePeer{ID: "b1", Name: "AccountB Peer"}, Preview: "B hello"},
				},
			}, nil
		},
		messages: func(_ context.Context, _ provider.PrivateMessagesRequest) (provider.PrivateMessagesPage, error) {
			return provider.PrivateMessagesPage{
				Messages: []provider.PrivateMessage{{ID: "bm1", SenderID: "b1", Body: "Only for B"}},
			}, nil
		},
	}
	pm.Bind(entry, "owner-b", backendB, backendB)
	select {
	case <-accountACanceled:
	case <-time.After(2 * time.Second):
		t.Fatal("switching accounts did not cancel Account A's in-flight private message fetch")
	}
	if len(pm.Conversations().Conversations) != 0 || pm.ActiveConversationID() != "" || len(pm.Thread("c2").Messages) != 0 {
		t.Fatalf("Account A state leaked into Account B immediately after Bind: active=%q convs=%+v", pm.ActiveConversationID(), pm.Conversations())
	}
	pm.EnsureConversations()
	drain(func() bool { return pm.Conversations().Phase == CatalogReady && pm.Thread("b1").Phase == CatalogReady })
	if len(pm.Conversations().Conversations) != 1 || pm.Conversations().Conversations[0].ID != "b1" ||
		len(pm.Thread("slow-iso").Messages) != 0 {
		t.Fatalf("Account B state polluted by Account A: convs=%+v", pm.Conversations())
	}

	// 7. Sign out -> Guest has zero private message visibility.
	pm.Bind(entry, "", backendB, backendB)
	if pm.Available() || pm.CanSend() || pm.UnreadCount() != 0 || len(pm.Conversations().Conversations) != 0 {
		t.Fatalf("signed-out guest retained private message state: avail=%v unread=%d", pm.Available(), pm.UnreadCount())
	}
}
