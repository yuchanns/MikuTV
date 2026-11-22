package tvui

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/provider"
	"github.com/yuchanns/MikuTV/internal/theme"
)

type privateMessagesTestSource struct {
	uiAuthSource
	mu            sync.Mutex
	owner         string
	noticeUnread  int
	privateUnread int
	failSend      bool
	sentBodies    []string
	conversations map[string][]provider.PrivateConversation
	messages      map[string][]provider.PrivateMessage
	olderMessages map[string][]provider.PrivateMessage
}

func (s *privateMessagesTestSource) SetAccount(cred *provider.Credential) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cred == nil {
		s.owner = ""
	} else {
		s.owner = cred.Account.ID
	}
	return nil
}

func (s *privateMessagesTestSource) ReadAccount(_ context.Context, req provider.AccountRequest) (provider.AccountPage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if req.Summary {
		u := s.noticeUnread
		return provider.AccountPage{Unread: &u}, nil
	}
	return provider.AccountPage{}, nil
}

func (s *privateMessagesTestSource) ReadCreator(_ context.Context, req provider.CreatorRequest) (provider.CreatorPage, error) {
	return provider.CreatorPage{
		Profile: &provider.CreatorProfile{
			Creator: provider.Creator{ID: req.CreatorID, Name: "CreatorDM"},
			Bio:     "Creator bio",
		},
	}, nil
}

func (s *privateMessagesTestSource) ReadPrivateConversations(_ context.Context, req provider.PrivateConversationsRequest) (provider.PrivateConversationsPage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u := s.privateUnread
	if req.Summary {
		return provider.PrivateConversationsPage{Unread: &u}, nil
	}
	list := s.conversations[s.owner]
	if req.Cursor == "" && len(list) > 2 {
		return provider.PrivateConversationsPage{
			Conversations: list[:2],
			Unread:        &u,
			NextCursor:    "page-2",
		}, nil
	}
	if req.Cursor == "page-2" && len(list) > 2 {
		return provider.PrivateConversationsPage{
			Conversations: list[2:],
		}, nil
	}
	return provider.PrivateConversationsPage{Conversations: list, Unread: &u}, nil
}

func (s *privateMessagesTestSource) ReadPrivateMessages(_ context.Context, req provider.PrivateMessagesRequest) (provider.PrivateMessagesPage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if req.Cursor == "older-1" {
		return provider.PrivateMessagesPage{
			Messages: s.olderMessages[req.ConversationID],
			HasMore:  false,
		}, nil
	}
	older := s.olderMessages[req.ConversationID]
	return provider.PrivateMessagesPage{
		Messages:   s.messages[req.ConversationID],
		HasMore:    len(older) > 0,
		NextCursor: "older-1",
	}, nil
}

func (s *privateMessagesTestSource) SendPrivateMessage(_ context.Context, req provider.PrivateMessageSendRequest) (provider.PrivateMessageSendResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failSend {
		return provider.PrivateMessageSendResult{}, &provider.Error{Code: provider.Denied, Message: "Recipient privacy limit"}
	}
	s.sentBodies = append(s.sentBodies, req.PeerID+":"+req.Body)
	return provider.PrivateMessageSendResult{
		Message: provider.PrivateMessage{
			ID:       "sent-" + req.PeerID,
			SenderID: s.owner,
			FromSelf: true,
			Body:     req.Body,
			SentAt:   1763850999,
		},
	}, nil
}

func (s *privateMessagesTestSource) MarkPrivateMessagesRead(_ context.Context, req provider.PrivateMessageReadRequest) (provider.PrivateMessageReadResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if req.All {
		s.privateUnread = 0
	} else if s.privateUnread > 0 {
		s.privateUnread--
	}
	u := s.privateUnread
	return provider.PrivateMessageReadResult{Unread: &u}, nil
}

func TestPrivateMessagesUIWorkflowDraftRetentionCreatorEntryAndCapabilityHiding(t *testing.T) {
	assets, err := LoadAssets()
	if err != nil {
		t.Fatal(err)
	}
	pages, err := NewPages(assets)
	if err != nil {
		t.Fatal(err)
	}
	entries := []app.Entry{
		{
			ID: "full", SourceID: "full", Name: "Full",
			Capabilities: provider.Capabilities{
				provider.SignIn,
				provider.BrowseCatalog,
				provider.ReadCreator,
				provider.FollowCreator,
				provider.AccountNotifications.Capability(),
				provider.EditNotifications,
				provider.ReadPrivateMessages,
				provider.WritePrivateMessages,
			},
		},
		{
			ID: "readonly-pm", SourceID: "readonly-pm", Name: "ReadOnlyPM",
			Capabilities: provider.Capabilities{
				provider.SignIn,
				provider.ReadCreator,
				provider.AccountNotifications.Capability(),
				provider.ReadPrivateMessages,
				// Intentionally omit WritePrivateMessages
			},
		},
		{
			ID: "no-pm", SourceID: "no-pm", Name: "NoPM",
			Capabilities: provider.Capabilities{
				provider.SignIn,
				provider.ReadCreator,
				provider.AccountNotifications.Capability(),
				// Intentionally omit ReadPrivateMessages
			},
		},
	}

	source := &privateMessagesTestSource{
		noticeUnread:  2,
		privateUnread: 3,
		conversations: map[string][]provider.PrivateConversation{
			"ownerA": {
				{ID: "p1", Peer: provider.PrivatePeer{ID: "p1", Name: "Alice"}, Preview: "First hello", Unread: 1, LastSeq: "10"},
				{ID: "p2", Peer: provider.PrivatePeer{ID: "p2", Name: "Bob"}, Preview: "Second hello", Unread: 2, LastSeq: "20"},
				{ID: "p3", Peer: provider.PrivatePeer{ID: "p3", Name: "Charlie"}, Preview: "Third page", Unread: 0, LastSeq: "30"},
			},
			"ownerB": {
				{ID: "pb1", Peer: provider.PrivatePeer{ID: "pb1", Name: "BetaPeer"}, Preview: "Beta only", Unread: 0, LastSeq: "1"},
			},
		},
		messages: map[string][]provider.PrivateMessage{
			"p1": {{ID: "m1", Seq: "10", SenderID: "p1", Body: "Alice latest message", SentAt: 1763850100}},
			"p2": {{ID: "m2", Seq: "20", SenderID: "p2", Body: "Bob latest message", SentAt: 1763850200}},
			"pb1": {{ID: "mb1", Seq: "1", SenderID: "pb1", Body: "Beta exclusive message", SentAt: 1763850300}},
		},
		olderMessages: map[string][]provider.PrivateMessage{
			"p1": {{ID: "m0", Seq: "9", SenderID: "p1", Body: "Alice earlier message", SentAt: 1763850000}},
		},
	}

	state, err := app.NewState(entries, entries[0].ID, pages.Routes)
	if err != nil {
		t.Fatal(err)
	}
	queued := make(chan func(), 128)
	post := func(fn func()) { queued <- fn }
	drain := func(done func() bool) {
		t.Helper()
		timeout := time.After(3 * time.Second)
		for !done() {
			select {
			case fn := <-queued:
				fn()
			case <-timeout:
				t.Fatal("timed out waiting for UI private messages async state")
			}
		}
	}
	drainPending := func() {
		for {
			select {
			case fn := <-queued:
				fn()
			default:
				return
			}
		}
	}

	catalog := app.NewCatalog(func(string) provider.Session { return source }, post, func() {})
	defer catalog.Close()
	activity := app.NewAccountActivity(nil, post, func() {})
	defer activity.Close()
	pm := app.NewPrivateMessages(post, func() {})
	defer pm.Close()
	store := uiTestKeychain{data: []byte(`{"selected":"ownerA","accounts":[{"account":{"id":"ownerA","name":"ViewerA"},"data":"e30="},{"account":{"id":"ownerB","name":"ViewerB"},"data":"e30="}]}`)}
	var view *View
	auth := app.NewAuth(store, post, func() {}, func() {
		activity.Close()
		pm.Close()
		if view != nil {
			view.SyncAccountActivity()
		}
	}, func() {})
	defer auth.Close()

	state.Navigate(app.Route{Page: app.Account})
	view = newTestView(t, Options{
		State: state, Auth: auth, Activity: activity, PrivateMessages: pm, Catalog: catalog,
		Pages: pages, Assets: assets, Theme: theme.Default(),
	})
	view.SyncEntry()
	drain(func() bool { return auth.Accounts().CurrentID == "ownerA" })
	view.SyncEntry()
	summaryQ := provider.AccountQuery{Section: provider.AccountNotifications, Summary: true}
	drain(func() bool { return activity.Snapshot(summaryQ).Phase == app.CatalogReady && pm.UnreadCount() == 3 })
	tester := ui.NewTester(view.Render, 1920, 1080)
	tester.Frame()

	// 1. Account overview combines notification (2) + private (3) unread counts -> "消息 · 5".
	if !tester.HasText(assets.Text.Activity.Messages + " \u00b7 5") {
		t.Fatal("account overview did not sum notification and private unread counts to 5")
	}

	// 2. Open Messages -> Right moves to Private tab and loads conversations & Alice's thread.
	state.Focus = app.AccountInbox
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	tester.Key(0, ui.KeyRight) // switches from noticeTab -> privateTab
	drain(func() bool { return pm.Conversations().Phase == app.CatalogReady && pm.Thread("p1").Phase == app.CatalogReady })
	drainPending()
	tester.Frame()
	if state.Focus != privateTab || view.messagesTab != messagesTabPrivate {
		t.Fatalf("did not switch to private tab: focus=%q tab=%v", state.Focus, view.messagesTab)
	}
	if !tester.HasText("Alice") || !tester.HasText("Alice latest message") || !tester.HasText(assets.Text.Activity.WritePrivateMessage) {
		t.Fatal("private messages layout missing contacts, bubble, or compose bar")
	}

	// 3. Load older messages in Alice's thread.
	state.Focus = privateOlderFocus
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	drain(func() bool { return len(pm.Thread("p1").Messages) == 2 })
	tester.Frame()
	if !tester.HasText("Alice earlier message") {
		t.Fatal("older message did not appear after LoadOlderMessages")
	}

	// 4. Switch conversation to Bob & load page 2 of contacts.
	state.Focus = privateContactFocus("p2")
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	drain(func() bool { return pm.ActiveConversationID() == "p2" && pm.Thread("p2").Phase == app.CatalogReady })
	tester.Frame()
	if !tester.HasText("Bob latest message") {
		t.Fatal("switching to Bob did not load Bob's thread")
	}
	state.Focus = privateMoreContacts
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	drain(func() bool { return len(pm.Conversations().Conversations) == 3 })
	tester.Frame()
	if !tester.HasText("Charlie") {
		t.Fatal("conversation pagination did not reveal Charlie")
	}

	// 5. Open compose modal -> fail send preserves draft -> retry send succeeds and appends bubble.
	state.Focus = privateComposeFocus
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	if state.Page != app.PrivateMessageCompose || !view.privateUI.draft.editing {
		t.Fatalf("compose modal did not open in editing mode: page=%s editing=%v", state.Page, view.privateUI.draft.editing)
	}
	view.privateUI.draft.body = "Dev test hello"
	view.privateUI.draft.editing = false
	state.Focus = privateComposeSend
	source.mu.Lock()
	source.failSend = true
	source.mu.Unlock()
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	drain(func() bool { return !pm.Sending() })
	drainPending()
	tester.Frame()
	if state.Page != app.PrivateMessageCompose || view.privateUI.draft.body != "Dev test hello" || !tester.HasText("Recipient privacy limit") {
		t.Fatalf("failed send did not keep modal/draft/error: page=%s body=%q failure=%q",
			state.Page, view.privateUI.draft.body, view.privateUI.draft.failure)
	}

	source.mu.Lock()
	source.failSend = false
	source.mu.Unlock()
	tester.Key(0, ui.KeyEnter)
	drain(func() bool { return !pm.Sending() && state.Page == app.AccountMessages })
	drainPending()
	tester.Frame()
	if !tester.HasText("Dev test hello") || view.noticeFeedback.text != assets.Text.Activity.MessageSent {
		t.Fatalf("successful send did not append bubble or show feedback: feedback=%q", view.noticeFeedback.text)
	}

	// 6. Mark all private messages read via ReadAll button on Private tab.
	state.Focus = noticeReadAll
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	drain(func() bool { return !pm.MarkingRead() })
	drainPending()
	if pm.UnreadCount() != 0 {
		t.Fatalf("expected 0 private unread after MarkAllRead, got %d", pm.UnreadCount())
	}

	// 7. Creator page "私信" button opens Private tab with that creator selected, and Back restores creator focus.
	pageCtx := view.context(state.Location, true, 1920, 1080)
	pageCtx.openCreator(provider.Creator{ID: "246649", Name: "CreatorDM"})
	drain(func() bool { return view.creator != nil && view.creator.profileLoaded })
	tester.Frame()
	if state.Page != app.Creator || !tester.HasText(assets.Text.Activity.PrivateTab) {
		t.Fatalf("creator page missing Private button: page=%s", state.Page)
	}
	state.Focus = creatorPrivateFocus
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	drain(func() bool { return pm.ActiveConversationID() == "246649" })
	tester.Frame()
	if state.Page != app.AccountMessages || view.messagesTab != messagesTabPrivate || !tester.HasText("CreatorDM") {
		t.Fatalf("creator Private button did not open creator conversation: page=%s tab=%v", state.Page, view.messagesTab)
	}
	tester.Key(0, ui.KeyEscape)
	tester.Frame()
	if state.Page != app.Creator || state.Focus != creatorPrivateFocus {
		t.Fatalf("Back from creator DM did not restore creator focus: page=%s focus=%q", state.Page, state.Focus)
	}

	// 8. Account switching clears active draft and isolates conversations.
	state.Focus = creatorPrivateFocus
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	view.privateUI.draft = privateMessageDraft{conversationID: "246649", body: "Unsent secret for A"}
	state.Push(app.Route{Page: app.PrivateMessageCompose})
	auth.SelectAccount("ownerB", nil)
	drain(func() bool { return auth.Accounts().CurrentID == "ownerB" })
	drainPending()
	pm.EnsureConversations()
	drain(func() bool { return pm.Conversations().Phase == app.CatalogReady })
	tester.Frame()
	if state.Page == app.PrivateMessageCompose || view.privateUI.draft.body != "" {
		t.Fatalf("account switch leaked Owner A draft or left compose modal open: page=%s body=%q", state.Page, view.privateUI.draft.body)
	}
	if tester.HasText("Alice") || !tester.HasText("BetaPeer") {
		t.Fatal("account switch leaked Owner A contacts or failed to load Owner B contacts")
	}

	// 9. Capability hiding: readonly-pm hides compose bar & ReadAll; no-pm hides Private tab.
	state.SelectEntry("readonly-pm")
	view.SyncEntry()
	drain(func() bool { return auth.Accounts().CurrentID != "" })
	view.messagesTab = messagesTabPrivate
	state.Navigate(app.Route{Page: app.AccountMessages})
	view.SyncEntry()
	drain(func() bool { return pm.Conversations().Phase == app.CatalogReady })
	tester.Frame()
	if tester.HasText(assets.Text.Activity.WritePrivateMessage) || tester.HasText(assets.Text.Activity.ReadAllNotices) {
		t.Fatal("readonly-pm source must hide WritePrivateMessage and ReadAllNotices on Private tab")
	}

	state.SelectEntry("no-pm")
	view.SyncEntry()
	drain(func() bool { return auth.Accounts().CurrentID != "" })
	state.Navigate(app.Route{Page: app.AccountMessages})
	view.SyncEntry()
	tester.Frame()
	if tester.HasText(assets.Text.Activity.PrivateTab) {
		t.Fatal("no-pm source must hide Private tab")
	}
}
