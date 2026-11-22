package tvui

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/player"
	"github.com/yuchanns/MikuTV/internal/provider"
	"github.com/yuchanns/MikuTV/internal/theme"
)

type uiLiveChatSub struct {
	events chan provider.LiveChatEvent
	once   sync.Once
	closed bool
	mu     sync.Mutex
}

func (s *uiLiveChatSub) Events() <-chan provider.LiveChatEvent { return s.events }
func (s *uiLiveChatSub) Close() {
	s.once.Do(func() {
		s.mu.Lock()
		s.closed = true
		s.mu.Unlock()
		close(s.events)
	})
}

type uiLiveChatSession struct {
	uiLiveSession
	chatMu   sync.Mutex
	sub      *uiLiveChatSub
	sent     []provider.LiveChatPost
	sendMute bool
}

func (s *uiLiveChatSession) OpenLiveChat(ctx context.Context, _ provider.LiveChatRequest) (provider.LiveChatSubscription, error) {
	sub := &uiLiveChatSub{events: make(chan provider.LiveChatEvent, 32)}
	s.chatMu.Lock()
	s.sub = sub
	s.chatMu.Unlock()
	context.AfterFunc(ctx, sub.Close)
	return sub, nil
}

func (s *uiLiveChatSession) SendLiveChat(_ context.Context, post provider.LiveChatPost) (provider.LiveChatSendResult, error) {
	s.chatMu.Lock()
	s.sent = append(s.sent, post)
	mute := s.sendMute
	s.chatMu.Unlock()
	if mute {
		return provider.LiveChatSendResult{Muted: true, MuteReason: "Muted"}, nil
	}
	return provider.LiveChatSendResult{}, nil
}

func (s *uiLiveChatSession) currentSub() *uiLiveChatSub {
	for range 100 {
		s.chatMu.Lock()
		sub := s.sub
		s.chatMu.Unlock()
		if sub != nil {
			return sub
		}
		time.Sleep(5 * time.Millisecond)
	}
	return nil
}

func TestLiveChatDrawerReadOnlyWriteComposeMutedAndDanmaku(t *testing.T) {
	assets, err := LoadAssets()
	if err != nil {
		t.Fatal(err)
	}
	pages, err := NewPages(assets)
	if err != nil {
		t.Fatal(err)
	}

	// Build a view with two entries: read-only live chat, and full read+write live chat.
	sess := &uiLiveChatSession{uiLiveSession: uiLiveSession{followed: map[string]bool{"host-1": false}}}
	readOnlyEntry := app.Entry{
		ID:   "live-ro",
		Name: "LiveReadOnly",
		Capabilities: provider.Capabilities{
			provider.BrowseCatalog, provider.BrowseLive, provider.GetCatalogItem,
			provider.ResolvePlayback, provider.LiveChat,
		},
	}
	fullEntry := app.Entry{
		ID:   "live-src",
		Name: "LiveFull",
		Capabilities: provider.Capabilities{
			provider.BrowseCatalog, provider.BrowseLive, provider.GetCatalogItem,
			provider.ResolvePlayback, provider.SignIn, provider.FollowCreator,
			provider.LiveChat, provider.WriteLiveChat,
		},
	}
	state, err := app.NewState([]app.Entry{readOnlyEntry, fullEntry}, "live-ro", pages.Routes)
	if err != nil {
		t.Fatal(err)
	}

	queue := make(chan func(), 128)
	post := func(fn func()) { queue <- fn }
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

	cat := app.NewCatalog(func(string) provider.Session { return sess }, post, func() {})
	live := app.NewLiveCatalog(post, func() {})
	liveChat := app.NewLiveChat(post, func() {})
	view := newTestView(t, Options{
		State: state, Catalog: cat, Live: live, LiveChat: liveChat,
		Pages: pages, Assets: assets, Theme: theme.Default(),
	})
	view.Auth = app.NewAuth(nil, post, func() {}, func() {}, func() {})
	var clock player.Status
	view.Playback = app.NewPlayback(cat, player.Engine{
		Open:     func(string, float64) bool { clock = player.Status{HasFrame: true}; return true },
		SetRate:  func(float64) {},
		Stop:     func() {},
		Snapshot: func() player.Status { return clock },
	}, nil, post, func() {})
	view.SyncEntry()
	drain()

	tester := ui.NewTester(view.Render, 1920, 1080)

	// 1. Read-only live chat entry shows chat & danmaku controls but HIDES compose bar and send action.
	roRoom := provider.MediaItem{
		Ref:   provider.MediaRef{EntryID: "live-ro", Kind: provider.Live, ID: "392731"},
		Title: "ReadOnly Stage",
	}
	view.context(state.Location, true, 1920, 1080).openLivePlayback(roRoom, false)
	drain()
	tester.Frame()

	if view.focus.item(playerComments) == nil || view.focus.item(playerDanmaku) == nil {
		t.Fatal("expected chat and danmaku controls when entry declares live.chat")
	}
	for _, forbidden := range []app.FocusID{playerTimeline, playerPrevious, playerNext, playerSpeed, playerSubtitles, playerLike, playerFavorite} {
		if view.focus.item(forbidden) != nil {
			t.Fatalf("VOD-only control %q must remain hidden in live playback", forbidden)
		}
	}

	view.player.openPanel(view, playerComments)
	tester.Frame()
	if view.focus.item(playerLiveChatScroll) == nil {
		t.Fatal("expected live chat scroll feed when drawer is open")
	}
	if view.focus.item(playerLiveCompose) != nil {
		t.Fatal("compose button must be hidden when source lacks live.chatWrite")
	}
	view.player.openPanel(view, playerDanmaku)
	tester.Frame()
	if view.focus.item(playbackOptionID(playerDanmaku, "send")) != nil {
		t.Fatal("danmaku popover compose button must be hidden when source lacks live.chatWrite")
	}

	// 2. Switch to full live.chat + live.chatWrite entry and verify drawer, real-time messages, danmaku, compose & muted state.
	state.SelectEntry("live-src")
	view.SyncEntry()
	drain()
	fullRoom := provider.MediaItem{
		Ref:     provider.MediaRef{EntryID: "live-src", Kind: provider.Live, ID: "392731"},
		Title:   "Full Live Stage",
		Creator: &provider.Creator{ID: "host-1", Name: "CityStage"},
	}
	view.context(state.Location, true, 1920, 1080).openLivePlayback(fullRoom, false)
	drain()
	sub := sess.currentSub()
	if sub == nil {
		t.Fatal("expected live chat subscription to open on live playback entry")
	}
	viewers := int64(13200)
	sub.events <- provider.LiveChatEvent{Type: provider.LiveChatEventStatus, Status: provider.LiveChatConnected}
	sub.events <- provider.LiveChatEvent{Type: provider.LiveChatEventViewers, Viewers: &viewers}
	sub.events <- provider.LiveChatEvent{
		Type:    provider.LiveChatEventMessage,
		Message: &provider.LiveChatMessage{ID: "m1", Author: "NightListener", Body: "Sounds great tonight!"},
	}
	drain()
	tester.Frame()

	// Live scrolling danmaku should be active while show=true and stop when toggled off.
	if !view.player.content.needsLiveDanmakuFrame(view, view.Playback.State(), time.Now()) {
		t.Fatal("expected live scrolling danmaku frame needed after incoming chat message")
	}
	view.playbackSettings.show = false
	if view.player.content.needsLiveDanmakuFrame(view, view.Playback.State(), time.Now()) {
		t.Fatal("expected live danmaku hidden when show=false")
	}
	view.playbackSettings.show = true

	// Open Live Chat drawer -> compose button present.
	view.player.openPanel(view, playerComments)
	tester.Frame()
	if view.focus.item(playerLiveCompose) == nil {
		t.Fatal("expected live compose button when entry declares live.chatWrite")
	}

	// Open frameless live compose overlay directly (bypassing sign-in gate by setting draft as signed-in flow does).
	view.player.content.draft = &danmakuDraft{
		mode:        provider.DanmakuScroll,
		live:        true,
		returnFocus: playerLiveCompose,
		body:        "Good evening!",
		edit:        false,
	}
	tester.Frame()
	if view.focus.item(danmakuEdit) == nil || view.focus.item(danmakuSend) == nil {
		t.Fatal("expected frameless live compose edit and send controls")
	}
	for _, mode := range []provider.DanmakuMode{provider.DanmakuScroll, provider.DanmakuTop, provider.DanmakuBottom} {
		vodChip := app.FocusID("player/danmaku-mode/" + mode)
		if view.focus.item(vodChip) != nil {
			t.Fatalf("VOD danmaku mode chip %q must not appear in live chat compose", vodChip)
		}
	}

	// Trigger sendLiveChat and verify it sends, records local message, and closes overlay.
	view.context(state.Location, true, 1920, 1080).sendLiveChat(view.player.content.draft)
	drain()
	tester.Frame()
	sess.chatMu.Lock()
	sentCount := len(sess.sent)
	sess.chatMu.Unlock()
	if sentCount != 1 {
		t.Fatalf("expected 1 SendLiveChat call, got %d", sentCount)
	}
	if view.player.content.draft != nil {
		t.Fatal("expected draft overlay to close after successful live chat send")
	}
	if got := len(liveChat.Snapshot().Messages); got != 2 {
		t.Fatalf("expected 2 chat messages (1 incoming + 1 local sent), got %d", got)
	}

	// Muted state removes playerLiveCompose from focus tree and disables sending.
	muted := true
	sub.events <- provider.LiveChatEvent{Type: provider.LiveChatEventMute, Muted: &muted}
	drain()
	tester.Frame()
	if view.focus.item(playerLiveCompose) != nil {
		t.Fatal("compose button must be disabled/unfocusable when live room is muted")
	}

	// Leaving live playback via Back closes panel -> hides HUD -> exits playback and closes live chat subscription.
	view.goBack() // close live chat panel
	view.goBack() // hide player controls
	view.goBack() // exit live playback
	drain()
	if liveChat.Snapshot().Active {
		t.Fatal("expected LiveChat to disconnect when leaving playback")
	}
}

func TestLiveHubBackdropFollowsFocusedRoom(t *testing.T) {
	assets, err := LoadAssets()
	if err != nil {
		t.Fatal(err)
	}
	pages, err := NewPages(assets)
	if err != nil {
		t.Fatal(err)
	}
	entry := app.Entry{
		ID:           "live-src",
		Name:         "LiveSource",
		Capabilities: provider.Capabilities{provider.BrowseCatalog, provider.BrowseLive},
	}
	state, err := app.NewState([]app.Entry{entry}, "live-src", pages.Routes)
	if err != nil {
		t.Fatal(err)
	}
	r1 := provider.MediaItem{Ref: provider.MediaRef{EntryID: "live-src", Kind: provider.Live, ID: "101"}, Title: "Room One", CoverURL: "https://cdn.media.example/101.jpg"}
	r2 := provider.MediaItem{Ref: provider.MediaRef{EntryID: "live-src", Kind: provider.Live, ID: "202"}, Title: "Room Two", CoverURL: "https://cdn.media.example/202.jpg"}
	view := newTestView(t, Options{State: state, Pages: pages, Assets: assets, Theme: theme.Default()})
	snap := app.LiveSnapshot{Hero: &r1, Rooms: []provider.MediaItem{r1, r2}}

	page := view.context(state.Location, true, 1920, 1080)
	state.Focus = liveRoomCardFocus("rooms", r2.Ref)
	got := page.liveFeaturedItem(snap)
	if got == nil || got.Ref != r2.Ref || view.live.featuredRef != r2.Ref {
		t.Fatalf("expected focused room 202 to become featured hero/backdrop, got %+v", got)
	}

	// Moving focus up to Hero Play keeps remembered room 202 so clicking Enter opens room 202.
	state.Focus = liveHeroPlayFocus
	got = page.liveFeaturedItem(snap)
	if got == nil || got.Ref != r2.Ref {
		t.Fatalf("expected hero play to preserve remembered room 202, got %+v", got)
	}
}
