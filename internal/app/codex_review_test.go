package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/yuchanns/MikuTV/internal/player"
	"github.com/yuchanns/MikuTV/internal/provider"
)

func TestAuthSignOutDiscardsPendingConfirmation(t *testing.T) {
	store := &authMemoryStore{records: make(map[[2]string][]byte)}
	confirmStarted := make(chan struct{})
	releaseConfirm := make(chan struct{})

	cred := provider.Credential{
		Account: provider.Account{ID: "u1", Name: "Alice"},
		Data:    []byte(`{"step":"initial"}`),
	}
	rotated := provider.Credential{
		Account:             cred.Account,
		Data:                []byte(`{"step":"rotated"}`),
		PendingConfirmation: true,
	}
	confirmed := provider.Credential{
		Account: cred.Account,
		Data:    []byte(`{"step":"confirmed"}`),
	}
	raw, _ := json.Marshal(accountRecord{Selected: "u1", Accounts: []provider.Credential{cred}})
	_ = store.Save("src", "src", raw)

	calls := 0
	service := &testAuthService{
		restore: func(ctx context.Context, c provider.Credential) (provider.Credential, error) {
			calls++
			if calls == 1 {
				return rotated, nil
			}
			close(confirmStarted)
			<-releaseConfirm
			return confirmed, nil
		},
	}

	queue := make(chan func(), 32)
	drain := func() {
		for {
			select {
			case fn := <-queue:
				fn()
			default:
				return
			}
		}
	}
	auth := NewAuth(store, func(fn func()) { queue <- fn }, func() {}, func() {}, func() {})
	auth.Bind(Entry{ID: "src", SourceID: "src"}, service)
	auth.Load()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		drain()
		select {
		case <-confirmStarted:
			goto confirming
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
	t.Fatal("confirmation never started")

confirming:
	auth.SignOut("u1")
	close(releaseConfirm)
	time.Sleep(30 * time.Millisecond)
	drain()

	if snap := auth.Accounts(); len(snap.Accounts) != 0 || snap.CurrentID != "" {
		t.Fatalf("signed-out account resurrected after confirmation: %+v", snap)
	}
}

func TestAuthBackgroundConfirmationPreservesSelectedAccount(t *testing.T) {
	store := &authMemoryStore{records: make(map[[2]string][]byte)}
	confirmStarted := make(chan struct{})
	releaseConfirm := make(chan struct{})

	credA := provider.Credential{
		Account: provider.Account{ID: "u1", Name: "Alice"},
		Data:    []byte(`{"step":"initial-a"}`),
	}
	rotatedA := provider.Credential{
		Account:             credA.Account,
		Data:                []byte(`{"step":"rotated-a"}`),
		PendingConfirmation: true,
	}
	confirmedA := provider.Credential{
		Account: credA.Account,
		Data:    []byte(`{"step":"confirmed-a"}`),
	}
	credB := provider.Credential{
		Account: provider.Account{ID: "u2", Name: "Bob"},
		Data:    []byte(`{"step":"initial-b"}`),
	}
	raw, _ := json.Marshal(accountRecord{Selected: "u1", Accounts: []provider.Credential{credA, credB}})
	_ = store.Save("src", "src", raw)

	callsA := 0
	service := &testAuthService{
		restore: func(ctx context.Context, c provider.Credential) (provider.Credential, error) {
			if c.Account.ID == "u1" {
				callsA++
				if callsA == 1 {
					return rotatedA, nil
				}
				close(confirmStarted)
				<-releaseConfirm
				return confirmedA, nil
			}
			return c, nil
		},
	}

	queue := make(chan func(), 32)
	drain := func() {
		for {
			select {
			case fn := <-queue:
				fn()
			default:
				return
			}
		}
	}
	auth := NewAuth(store, func(fn func()) { queue <- fn }, func() {}, func() {}, func() {})
	auth.Bind(Entry{ID: "src", SourceID: "src"}, service)
	auth.Load()

	waitUntil(t, drain, func() bool {
		select {
		case <-confirmStarted:
			return true
		default:
			return false
		}
	})

	// User switches to account B while account A's rotation confirmation is still in flight.
	auth.SelectAccount("u2", nil)
	waitUntil(t, drain, func() bool { return auth.Accounts().CurrentID == "u2" && !auth.Accounts().Busy })

	// Now account A's background confirmation finishes.
	close(releaseConfirm)
	waitUntil(t, drain, func() bool {
		data, _ := store.Load("src", "src")
		var rec accountRecord
		_ = json.Unmarshal(data, &rec)
		for _, item := range rec.Accounts {
			if item.Account.ID == "u1" && string(item.Data) == `{"step":"confirmed-a"}` {
				return true
			}
		}
		return false
	})

	data, _ := store.Load("src", "src")
	var persisted accountRecord
	if err := json.Unmarshal(data, &persisted); err != nil {
		t.Fatalf("unmarshal persisted record: %v", err)
	}
	if persisted.Selected != "u2" {
		t.Fatalf("persisted Selected = %q, want %q", persisted.Selected, "u2")
	}
	if got := auth.Accounts().CurrentID; got != "u2" {
		t.Fatalf("live CurrentID = %q, want %q", got, "u2")
	}
}

func TestPrivateMessagesPrependRetainsConcurrentSend(t *testing.T) {
	olderReady := make(chan struct{})
	releaseOlder := make(chan struct{})

	reader := &fakePrivateReader{
		list: func(ctx context.Context, req provider.PrivateConversationsRequest) (provider.PrivateConversationsPage, error) {
			return provider.PrivateConversationsPage{
				Conversations: []provider.PrivateConversation{{ID: "c1", Peer: provider.PrivatePeer{ID: "p1", Name: "Peer"}}},
			}, nil
		},
		read: func(ctx context.Context, req provider.PrivateMessagesRequest) (provider.PrivateMessagesPage, error) {
			if req.Cursor == "" {
				return provider.PrivateMessagesPage{
					Messages:   []provider.PrivateMessage{{ID: "m2", Body: "initial", SentAt: 20}},
					HasMore:    true,
					NextCursor: "older-1",
				}, nil
			}
			close(olderReady)
			<-releaseOlder
			return provider.PrivateMessagesPage{
				Messages: []provider.PrivateMessage{{ID: "m1", Body: "older", SentAt: 10}},
			}, nil
		},
		send: func(ctx context.Context, req provider.PrivateMessageSendRequest) (provider.PrivateMessageSendResult, error) {
			return provider.PrivateMessageSendResult{
				Message: provider.PrivateMessage{ID: "m3", Body: req.Body, SentAt: 30, FromSelf: true},
			}, nil
		},
	}

	queue := make(chan func(), 32)
	drain := func() {
		for {
			select {
			case fn := <-queue:
				fn()
			default:
				return
			}
		}
	}
	pm := NewPrivateMessages(func(fn func()) { queue <- fn }, func() {})
	pm.Bind(Entry{ID: "src", Capabilities: provider.Capabilities{provider.ReadPrivateMessages, provider.WritePrivateMessages}}, "owner-1", reader, reader)
	pm.EnsureConversations()
	waitUntil(t, drain, func() bool { return len(pm.Thread("c1").Messages) == 1 })

	pm.LoadOlderMessages("c1")
	<-olderReady

	sent := false
	pm.SendMessage("c1", "concurrent", func(_ provider.PrivateMessage, err error) {
		if err != nil {
			t.Errorf("SendMessage error: %v", err)
		}
		sent = true
	})
	waitUntil(t, drain, func() bool { return sent })

	close(releaseOlder)
	waitUntil(t, drain, func() bool { return pm.Thread("c1").Phase == CatalogReady && !pm.Thread("c1").Prepending })

	msgs := pm.Thread("c1").Messages
	if len(msgs) != 3 || msgs[0].ID != "m1" || msgs[1].ID != "m2" || msgs[2].ID != "m3" {
		t.Fatalf("expected [m1 m2 m3] retaining concurrent send, got %+v", msgs)
	}
}

func TestPrivateMessagesConversationPaginationRetainsConcurrentSend(t *testing.T) {
	page2Ready := make(chan struct{})
	releasePage2 := make(chan struct{})

	reader := &fakePrivateReader{
		list: func(ctx context.Context, req provider.PrivateConversationsRequest) (provider.PrivateConversationsPage, error) {
			if req.Cursor == "" {
				return provider.PrivateConversationsPage{
					Conversations: []provider.PrivateConversation{{ID: "c1", Peer: provider.PrivatePeer{ID: "p1", Name: "Peer 1"}, Preview: "old preview"}},
					NextCursor:    "page-2",
				}, nil
			}
			close(page2Ready)
			<-releasePage2
			return provider.PrivateConversationsPage{
				Conversations: []provider.PrivateConversation{
					{ID: "c1", Peer: provider.PrivatePeer{ID: "p1", Name: "Peer 1"}, Preview: "stale page2 preview"},
					{ID: "c2", Peer: provider.PrivatePeer{ID: "p2", Name: "Peer 2"}, Preview: "second"},
				},
			}, nil
		},
		read: func(ctx context.Context, req provider.PrivateMessagesRequest) (provider.PrivateMessagesPage, error) {
			return provider.PrivateMessagesPage{}, nil
		},
		send: func(ctx context.Context, req provider.PrivateMessageSendRequest) (provider.PrivateMessageSendResult, error) {
			return provider.PrivateMessageSendResult{
				Message: provider.PrivateMessage{ID: "m-new", Body: req.Body, SentAt: 50, FromSelf: true},
			}, nil
		},
	}

	queue := make(chan func(), 32)
	drain := func() {
		for {
			select {
			case fn := <-queue:
				fn()
			default:
				return
			}
		}
	}
	pm := NewPrivateMessages(func(fn func()) { queue <- fn }, func() {})
	pm.Bind(Entry{ID: "src", Capabilities: provider.Capabilities{provider.ReadPrivateMessages, provider.WritePrivateMessages}}, "owner-1", reader, reader)
	pm.EnsureConversations()
	waitUntil(t, drain, func() bool { return len(pm.Conversations().Conversations) == 1 })

	pm.LoadMoreConversations()
	<-page2Ready

	sent := false
	pm.SendMessage("c1", "fresh sent body", func(_ provider.PrivateMessage, err error) {
		if err != nil {
			t.Errorf("SendMessage error: %v", err)
		}
		sent = true
	})
	waitUntil(t, drain, func() bool { return sent })

	close(releasePage2)
	waitUntil(t, drain, func() bool { return pm.Conversations().Phase == CatalogReady && len(pm.Conversations().Conversations) == 2 })

	convs := pm.Conversations().Conversations
	if convs[0].ID != "c1" || convs[0].Preview != "fresh sent body" || convs[1].ID != "c2" {
		t.Fatalf("conversation pagination overwrote live local update: %+v", convs)
	}
}

type fakeNotificationPaginationReader struct {
	read func(context.Context, provider.AccountRequest) (provider.AccountPage, error)
	ack  func(context.Context, provider.NotificationReadRequest) (provider.NotificationReadResult, error)
}

func (f *fakeNotificationPaginationReader) ReadAccount(ctx context.Context, req provider.AccountRequest) (provider.AccountPage, error) {
	return f.read(ctx, req)
}

func (f *fakeNotificationPaginationReader) MarkNotificationsRead(ctx context.Context, req provider.NotificationReadRequest) (provider.NotificationReadResult, error) {
	return f.ack(ctx, req)
}

func TestAccountActivityPaginationRetainsNotificationReadAck(t *testing.T) {
	page2Ready := make(chan struct{})
	releasePage2 := make(chan struct{})

	reader := &fakeNotificationPaginationReader{
		read: func(_ context.Context, req provider.AccountRequest) (provider.AccountPage, error) {
			if req.Cursor == "" {
				return provider.AccountPage{
					Rows: []provider.AccountRow{{
						ID:     "n1",
						Time:   20,
						Notice: &provider.AccountNotice{Unread: true},
					}},
					NextCursor: "page-2",
				}, nil
			}
			close(page2Ready)
			<-releasePage2
			return provider.AccountPage{
				Rows: []provider.AccountRow{
					{ID: "n1", Time: 20, Notice: &provider.AccountNotice{Unread: true}},
					{ID: "n2", Time: 10, Notice: &provider.AccountNotice{Unread: false}},
				},
			}, nil
		},
		ack: func(_ context.Context, _ provider.NotificationReadRequest) (provider.NotificationReadResult, error) {
			zero := 0
			return provider.NotificationReadResult{Unread: &zero}, nil
		},
	}

	queue := make(chan func(), 32)
	drain := func() {
		for {
			select {
			case fn := <-queue:
				fn()
			default:
				return
			}
		}
	}

	act := NewAccountActivity(nil, func(fn func()) { queue <- fn }, func() {})
	act.Bind(Entry{
		ID: "src",
		Capabilities: provider.Capabilities{
			provider.AccountNotifications.Capability(),
			provider.EditNotifications,
		},
	}, "u1", reader)

	query := provider.AccountQuery{Section: provider.AccountNotifications, Filter: provider.NoticeAll}
	act.Ensure(query)
	waitUntil(t, drain, func() bool { return act.Snapshot(query).Phase == CatalogReady && len(act.Snapshot(query).Page.Rows) == 1 })

	act.More(query)
	<-page2Ready

	acked := false
	if !act.MarkNotificationsRead(provider.NotificationReadRequest{All: true}, func(err error) {
		if err != nil {
			t.Errorf("MarkNotificationsRead error: %v", err)
		}
		acked = true
	}) {
		t.Fatal("MarkNotificationsRead returned false")
	}
	waitUntil(t, drain, func() bool { return acked })

	close(releasePage2)
	waitUntil(t, drain, func() bool { return act.Snapshot(query).Phase == CatalogReady && len(act.Snapshot(query).Page.Rows) == 2 })

	rows := act.Snapshot(query).Page.Rows
	if rows[0].ID != "n1" || rows[0].Notice == nil || rows[0].Notice.Unread {
		t.Fatalf("notification pagination reverted read acknowledgement: %+v", rows[0])
	}
}

type gatedDanmakuRulesSession struct {
	fakeDanmakuRulesSession
	firstSyncStarted chan struct{}
	releaseFirstSync chan struct{}
	syncCalls        int
}

func (g *gatedDanmakuRulesSession) DanmakuRules(ctx context.Context) ([]provider.DanmakuRule, error) {
	g.mu.Lock()
	g.syncCalls++
	call := g.syncCalls
	staleSnapshot := append([]provider.DanmakuRule(nil), g.rules...)
	g.mu.Unlock()
	if call == 1 {
		close(g.firstSyncStarted)
		<-g.releaseFirstSync
		return staleSnapshot, nil
	}
	return g.fakeDanmakuRulesSession.DanmakuRules(ctx)
}

func TestDanmakuRulesMutationInvalidatesInFlightSync(t *testing.T) {
	queue := make(chan func(), 32)
	drain := func() {
		for {
			select {
			case fn := <-queue:
				fn()
			default:
				return
			}
		}
	}

	sess := &gatedDanmakuRulesSession{
		fakeDanmakuRulesSession: fakeDanmakuRulesSession{
			rules: []provider.DanmakuRule{{
				ID: "remote:old", RemoteID: "r-old", Kind: provider.DanmakuRuleKeyword, Pattern: "old", Enabled: true, Synced: true,
			}},
		},
		firstSyncStarted: make(chan struct{}),
		releaseFirstSync: make(chan struct{}),
	}
	post := func(fn func()) { queue <- fn }
	caps := provider.Capabilities{provider.BrowseCatalog, provider.ReadDanmaku, provider.SignIn, provider.DanmakuRules}
	cat := NewCatalog(func(string) provider.Session { return sess }, post, func() {})
	defer cat.Close()
	cat.Open(Entry{ID: "src", SourceID: "src", Capabilities: caps})

	store := newMemoryDanmakuStore()
	dr := NewDanmakuRules(store, post, func() {})
	defer dr.Close()
	dr.SetScope("src", caps, "u1", cat)

	dr.Sync()
	<-sess.firstSyncStarted

	// Add a new cloud rule while the initial sync is still in flight.
	added := false
	dr.Add(provider.DanmakuRuleKeyword, "fresh-spoiler", func(err error) {
		if err != nil {
			t.Errorf("Add error: %v", err)
		}
		added = true
	})
	waitUntil(t, drain, func() bool { return added })

	// Release the stale initial sync (which did NOT contain "fresh-spoiler").
	close(sess.releaseFirstSync)
	waitUntil(t, drain, func() bool { return dr.Snapshot().SyncState == DanmakuRulesSynced })

	snap := dr.Snapshot()
	foundFresh := false
	foundOld := false
	for _, r := range snap.Rules {
		if r.Pattern == "fresh-spoiler" {
			foundFresh = true
		}
		if r.Pattern == "old" {
			foundOld = true
		}
	}
	if !foundFresh || !foundOld {
		t.Fatalf("expected both fresh-spoiler and old rules retained after stale sync release, got %+v", snap.Rules)
	}
}

func TestDanmakuRulesLocalIDNoCollisionAfterReload(t *testing.T) {
	store := newMemoryDanmakuStore()
	_ = store.SaveDanmakuRules("src", "", []provider.DanmakuRule{
		{ID: "local:1", Kind: provider.DanmakuRuleKeyword, Pattern: "spoiler", Enabled: true},
		{ID: "local:5", Kind: provider.DanmakuRuleKeyword, Pattern: "spam", Enabled: true},
	})
	dr := NewDanmakuRules(store, func(fn func()) { fn() }, func() {})
	dr.SetScope("src", nil, "", nil)

	var addErr error
	dr.Add(provider.DanmakuRuleKeyword, "new-rule", func(err error) { addErr = err })
	if addErr != nil {
		t.Fatalf("Add failed: %v", addErr)
	}
	snap := dr.Snapshot()
	if len(snap.Rules) != 3 {
		t.Fatalf("expected 3 rules, got %d", len(snap.Rules))
	}
	seen := make(map[string]bool)
	for _, r := range snap.Rules {
		if seen[r.ID] {
			t.Fatalf("duplicate local rule ID %q in %+v", r.ID, snap.Rules)
		}
		seen[r.ID] = true
	}
}

type codexHTTPTransport struct{}

func (codexHTTPTransport) OpenResource(ctx context.Context, resource provider.PlaybackResource, method, byteRange string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, resource.URLs[0], nil)
	if err != nil {
		return nil, err
	}
	if byteRange != "" {
		req.Header.Set("Range", byteRange)
	}
	return http.DefaultClient.Do(req)
}

func TestChooseAudioTrackClearsSwitchingOnTimeout(t *testing.T) {
	stalled := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v.mp4" {
			w.Header().Set("Content-Type", "video/mp4")
			w.Header().Set("Content-Length", "4")
			_, _ = w.Write([]byte("mp40"))
			return
		}
		select {
		case <-stalled:
		case <-r.Context().Done():
		}
	}))
	defer func() {
		close(stalled)
		srv.Close()
	}()

	queue := make(chan func(), 32)
	drain := func() {
		for {
			select {
			case fn := <-queue:
				fn()
			default:
				return
			}
		}
	}

	p := NewPlayback(nil, player.Engine{Open: func(string, float64) bool { return true }}, nil, func(fn func()) { queue <- fn }, func() {})
	media, err := player.OpenMedia(context.Background(), provider.Playback{
		Transport: codexHTTPTransport{},
		Plan: provider.PlaybackPlan{
			Streams: []provider.PlaybackStream{
				{
					Format: provider.PlaybackDirect,
					Video:  provider.PlaybackTrack{Resource: provider.PlaybackResource{URLs: []string{srv.URL + "/v.mp4"}}},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("OpenMedia: %v", err)
	}
	defer media.Close()

	p.active = true
	p.media = media
	p.state.ActiveAudio = "a1"
	p.state.AudioTracks = []provider.PlaybackTrack{{ID: "a1", Label: "Track 1"}, {ID: "a2", Label: "Track 2"}}
	p.state.Resolution = provider.Playback{
		Transport: codexHTTPTransport{},
		Plan: provider.PlaybackPlan{
			Streams: []provider.PlaybackStream{
				{
					Format: provider.PlaybackDASH,
					Video:  provider.PlaybackTrack{Resource: provider.PlaybackResource{URLs: []string{srv.URL + "/v.m4s"}}},
					Audio:  &provider.PlaybackTrack{ID: "a2", Resource: provider.PlaybackResource{URLs: []string{srv.URL + "/a2.m4s"}}},
				},
			},
		},
	}

	if !p.ChooseAudioTrack("a2") {
		t.Fatal("ChooseAudioTrack returned false")
	}
	if !p.state.Switching || p.audioCancel == nil {
		t.Fatalf("expected Switching=true and non-nil audioCancel")
	}
	// Simulate the 15s request deadline expiring without superseding p.audioRequest.
	p.audioCancel()
	waitUntil(t, drain, func() bool { return !p.state.Switching && p.audioCancel == nil })
}

type blockingInlinePlaybackSession struct {
	provider.Session
	started chan struct{}
}

func (b *blockingInlinePlaybackSession) Browse(context.Context, provider.BrowseRequest) (provider.CollectionPage, error) {
	return provider.CollectionPage{}, nil
}

func (b *blockingInlinePlaybackSession) Item(context.Context, provider.MediaRef) (provider.MediaItem, error) {
	return provider.MediaItem{}, nil
}

func (b *blockingInlinePlaybackSession) ResolvePlayback(ctx context.Context, _ provider.PlaybackRequest) (provider.Playback, error) {
	close(b.started)
	<-ctx.Done()
	return provider.Playback{}, ctx.Err()
}

func TestStartInlinePreviewTimeoutTransitionsToFailed(t *testing.T) {
	queue := make(chan func(), 32)
	drain := func() {
		for {
			select {
			case fn := <-queue:
				fn()
			default:
				return
			}
		}
	}

	sess := &blockingInlinePlaybackSession{started: make(chan struct{})}
	cat := &Catalog{session: sess}
	p := NewPlayback(cat, player.Engine{Open: func(string, float64) bool { return true }}, nil, func(fn func()) { queue <- fn }, func() {})
	target := provider.PlaybackTarget{Ref: provider.MediaRef{EntryID: "src", Kind: provider.Video, ID: "v1"}}
	p.StartInlinePreview(target)
	<-sess.started

	if !p.inlineActive || !p.inlineState.Loading || p.inlineCancel == nil {
		t.Fatalf("expected active loading preview before timeout, got active=%v state=%+v", p.inlineActive, p.inlineState)
	}
	// Simulate the 12s deadline expiring without StopInlinePreview superseding inlineRequest.
	p.inlineCancel()
	waitUntil(t, drain, func() bool { return !p.inlineActive && !p.inlineState.Loading && p.inlineState.Failed && p.inlineCancel == nil })
}

type fakePrivateReader struct {
	list func(context.Context, provider.PrivateConversationsRequest) (provider.PrivateConversationsPage, error)
	read func(context.Context, provider.PrivateMessagesRequest) (provider.PrivateMessagesPage, error)
	send func(context.Context, provider.PrivateMessageSendRequest) (provider.PrivateMessageSendResult, error)
}

func (f *fakePrivateReader) ReadPrivateConversations(ctx context.Context, req provider.PrivateConversationsRequest) (provider.PrivateConversationsPage, error) {
	return f.list(ctx, req)
}
func (f *fakePrivateReader) ReadPrivateMessages(ctx context.Context, req provider.PrivateMessagesRequest) (provider.PrivateMessagesPage, error) {
	return f.read(ctx, req)
}
func (f *fakePrivateReader) SendPrivateMessage(ctx context.Context, req provider.PrivateMessageSendRequest) (provider.PrivateMessageSendResult, error) {
	return f.send(ctx, req)
}
func (f *fakePrivateReader) MarkPrivateMessagesRead(context.Context, provider.PrivateMessageReadRequest) (provider.PrivateMessageReadResult, error) {
	return provider.PrivateMessageReadResult{}, nil
}

func waitUntil(t *testing.T, drain func(), cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		drain()
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not met before timeout")
}
