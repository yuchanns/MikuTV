package tvui

import (
	"context"
	"sync"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/provider"
	"github.com/yuchanns/MikuTV/internal/theme"
)

type uiFollowingSession struct {
	mu           sync.Mutex
	owner        string
	pagesByOwner map[string]provider.FollowingPage
	creatorPages map[string]provider.CreatorPage
	items        map[string]provider.MediaItem
	followCalls  []provider.CreatorFollowRequest
	readCalls    []provider.FollowingRequest
}

func (s *uiFollowingSession) Browse(context.Context, provider.BrowseRequest) (provider.CollectionPage, error) {
	return provider.CollectionPage{}, nil
}

func (s *uiFollowingSession) Item(_ context.Context, ref provider.MediaRef) (provider.MediaItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if item, ok := s.items[ref.ID]; ok {
		return item, nil
	}
	return provider.MediaItem{}, &provider.Error{Code: provider.Expired}
}

func (s *uiFollowingSession) VideoDetail(ctx context.Context, ref provider.MediaRef) (provider.MediaItem, error) {
	return s.Item(ctx, ref)
}

func (s *uiFollowingSession) ReadFollowing(_ context.Context, req provider.FollowingRequest) (provider.FollowingPage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.readCalls = append(s.readCalls, req)
	if s.owner == "" {
		return provider.FollowingPage{}, &provider.Error{Code: provider.NeedsSignIn}
	}
	return s.pagesByOwner[s.owner], nil
}

func (s *uiFollowingSession) ReadCreator(_ context.Context, req provider.CreatorRequest) (provider.CreatorPage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.creatorPages[req.CreatorID], nil
}

func (s *uiFollowingSession) FollowCreator(_ context.Context, req provider.CreatorFollowRequest) (provider.CreatorFollowResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.followCalls = append(s.followCalls, req)
	return provider.CreatorFollowResult{Following: req.Enabled}, nil
}

func (s *uiFollowingSession) BeginSignIn(context.Context) (provider.QRAttempt, error) {
	return nil, &provider.Error{Code: provider.Unsupported}
}
func (s *uiFollowingSession) RestoreAccount(_ context.Context, cred provider.Credential) (provider.Credential, error) {
	return cred, nil
}
func (s *uiFollowingSession) SetAccount(cred *provider.Credential) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cred == nil {
		s.owner = ""
	} else {
		s.owner = cred.Account.ID
	}
	return nil
}
func (s *uiFollowingSession) SignOut(context.Context, provider.Credential) error { return nil }
func (s *uiFollowingSession) Close()                                             {}

func TestFollowingGuestTabsCreatorUnfollowSyncAndAccountSwitch(t *testing.T) {
	assets, err := LoadAssets()
	if err != nil {
		t.Fatal(err)
	}
	pages, err := NewPages(assets)
	if err != nil {
		t.Fatal(err)
	}
	entry := app.Entry{
		ID: "sample-source", SourceID: "sample-source", Name: "SampleSource",
		Capabilities: provider.Capabilities{
			provider.BrowseCatalog, provider.GetCatalogItem,
			provider.BrowseFollowing, provider.ReadCreator,
			provider.FollowCreator, provider.SignIn,
		},
	}
	state, err := app.NewState([]app.Entry{entry}, entry.ID, pages.Routes)
	if err != nil {
		t.Fatal(err)
	}

	dur := int64(420)
	views := int64(25000)
	liveViews := int64(13000)
	ref1 := provider.MediaRef{EntryID: entry.ID, Kind: provider.Video, ID: "BV1UpA"}
	ref2 := provider.MediaRef{EntryID: entry.ID, Kind: provider.Video, ID: "BV1UpB"}
	liveRef := provider.MediaRef{EntryID: entry.ID, Kind: provider.Live, ID: "room-100"}

	sess := &uiFollowingSession{
		pagesByOwner: map[string]provider.FollowingPage{
			"acc-a": {
				SupportsLive: true,
				Creators: []provider.FollowingCreator{
					{Creator: provider.Creator{ID: "c1", Name: "Creator Alpha", Following: true}, HasUpdate: true, Live: true},
					{Creator: provider.Creator{ID: "c2", Name: "Creator Beta", Following: true}, HasUpdate: true},
				},
				Updates: []provider.MediaItem{
					{Ref: ref1, Title: "Alpha Film", Creator: &provider.Creator{ID: "c1", Name: "Creator Alpha", Following: true}, DurationSeconds: &dur, Views: &views},
					{Ref: ref2, Title: "Beta Film", Creator: &provider.Creator{ID: "c2", Name: "Creator Beta", Following: true}, DurationSeconds: &dur, Views: &views},
				},
				LiveRooms: []provider.MediaItem{
					{Ref: liveRef, Title: "Alpha Live Stream", Creator: &provider.Creator{ID: "c1", Name: "Creator Alpha", Following: true}, Category: "Music", Views: &liveViews, Quality: "LIVE"},
				},
			},
			"acc-b": {
				SupportsLive: false,
				Creators: []provider.FollowingCreator{
					{Creator: provider.Creator{ID: "c9", Name: "Account B Creator", Following: true}},
				},
			},
		},
		creatorPages: map[string]provider.CreatorPage{
			"c1": {
				Profile: &provider.CreatorProfile{
					Creator: provider.Creator{ID: "c1", Name: "Creator Alpha", Following: true},
					Tabs:    []provider.CreatorTab{provider.CreatorTabVideos, provider.CreatorTabAbout},
				},
			},
		},
		items: map[string]provider.MediaItem{
			"BV1UpB": {
				Ref: ref2, Title: "Beta Film",
				Creator: &provider.Creator{ID: "c2", Name: "Creator Beta", Following: true},
			},
		},
	}

	queue := make(chan func(), 64)
	post := func(fn func()) { queue <- fn }
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

	catalog := app.NewCatalog(func(string) provider.Session { return sess }, post, func() {})
	t.Cleanup(catalog.Close)
	creds := newMemoryCreds()
	var view *View
	auth := app.NewAuth(creds, post, func() {}, func() {
		if view != nil {
			view.SyncAccountActivity()
		}
	}, func() {})
	t.Cleanup(auth.Close)
	following := app.NewFollowingFeed(post, func() {})
	t.Cleanup(following.Close)

	view = newTestView(t, Options{
		State: state, Catalog: catalog, Auth: auth, Following: following,
		Pages: pages, Assets: assets, Theme: theme.Default(),
	})
	view.SyncEntry()
	for len(queue) > 0 || !auth.Accounts().Ready {
		(<-queue)()
	}
	drain()

	tester := ui.NewTester(view.Render, 1920, 1080)
	if !state.Navigate(app.Route{Page: app.Following}) {
		t.Fatal("failed to navigate to Following")
	}
	view.SyncEntry()
	tester.Frame()

	// Signed out: guest prompt is shown and SignIn focus is active.
	if following.SignedIn() {
		t.Fatal("expected guest state initially")
	}
	if view.focus.item(followingSignInFocus) == nil {
		t.Fatal("expected guest sign-in CTA on Following page")
	}

	// Load Account A & Account B into credential store and switch cleanly via auth.
	_ = creds.Save(entry.SourceID, entry.ID, []byte(`{"selected":"acc-a","accounts":[{"account":{"id":"acc-a","name":"Account A"},"data":{"csrf":"a"}},{"account":{"id":"acc-b","name":"Account B"},"data":{"csrf":"b"}}]}`))
	auth.Close()
	auth = app.NewAuth(creds, post, func() {}, func() {
		if view != nil {
			view.SyncAccountActivity()
		}
	}, func() {})
	t.Cleanup(auth.Close)
	view.Auth = auth
	view.SyncEntry()
	for !auth.Accounts().Ready || auth.Accounts().CurrentID != "acc-a" || !following.Snapshot().Loaded {
		(<-queue)()
	}
	drain()
	tester.Frame()

	if len(following.Snapshot().Creators) != 2 {
		t.Fatalf("expected 2 creators for acc-a, got %d", len(following.Snapshot().Creators))
	}
	if view.focus.item(followingTabLive) == nil {
		t.Fatal("expected live tab visible when SupportsLive=true")
	}

	// Move right on top tabs -> immediately switches to live tab.
	state.Focus = followingTabAll
	tester.Frame()
	tester.Key(0, ui.KeyRight)
	tester.Frame()
	if view.following.tab != followingViewLive || state.Focus != followingTabLive {
		t.Fatalf("expected Right on header to switch to live tab, got tab=%q focus=%q", view.following.tab, state.Focus)
	}
	// Move left -> immediately back to all tab.
	tester.Key(0, ui.KeyLeft)
	tester.Frame()
	if view.following.tab != followingViewAll || state.Focus != followingTabAll {
		t.Fatalf("expected Left on header to switch back to all tab, got tab=%q focus=%q", view.following.tab, state.Focus)
	}

	// Open Creator Alpha from avatar strip -> unfollow -> return -> Alpha pruned from Following!
	state.Focus = followingCreatorFocus("c1")
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	drain()
	for view.creator != nil && !view.creator.profileLoaded {
		(<-queue)()
	}
	tester.Frame()
	if state.Page != app.Creator || view.creator == nil || view.creator.seed.ID != "c1" {
		t.Fatalf("expected Creator page for c1, got page=%s creator=%v", state.Page, view.creator)
	}
	state.Focus = creatorFollowFocus
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	for len(sess.followCalls) == 0 {
		(<-queue)()
	}
	drain()
	tester.Frame()
	if sess.followCalls[0].Enabled {
		t.Fatalf("expected unfollow call, got %+v", sess.followCalls[0])
	}

	// Return to Following page.
	tester.Key(0, ui.KeyEscape)
	tester.Frame()
	if state.Page != app.Following {
		t.Fatalf("expected return to Following, got %s", state.Page)
	}
	snap := following.Snapshot()
	if len(snap.Creators) != 1 || snap.Creators[0].Creator.ID != "c2" ||
		len(snap.Updates) != 1 || snap.Updates[0].Ref.ID != "BV1UpB" ||
		len(snap.LiveRooms) != 0 {
		t.Fatalf("expected unfollowed c1 removed across creators/updates/live, got %+v", snap)
	}
	// Focus should cleanly recover from deleted c1 to remaining c2!
	if state.Focus != followingCreatorFocus("c2") {
		t.Fatalf("expected focus recovery to remaining creator c2, got %q", state.Focus)
	}

	// Open VideoDetail from remaining update card -> Back restores exact update card focus!
	state.Focus = followingUpdateFocus(ref2)
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	drain()
	tester.Frame()
	if state.Page != app.VideoDetail {
		t.Fatalf("expected VideoDetail page, got %s", state.Page)
	}
	tester.Key(0, ui.KeyEscape)
	tester.Frame()
	if state.Page != app.Following || state.Focus != followingUpdateFocus(ref2) {
		t.Fatalf("expected Back from VideoDetail to restore Following card focus, got page=%s focus=%q", state.Page, state.Focus)
	}

	// Scroll updates shelf horizontally so card 0 would be scrolled off-screen; Down from avatar still targets visible update card.
	view.following.updatesScroll.X = 1200
	state.Focus = followingCreatorFocus("c2")
	tester.Frame()
	tester.Key(0, ui.KeyDown)
	tester.Frame()
	if state.Focus != followingUpdateFocus(ref2) {
		t.Fatalf("expected Down from avatar after horizontal scroll to focus visible update card, got %q", state.Focus)
	}

	// Open See All modal and close it cleanly.
	state.Focus = followingUpdatesAll
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	if state.Page != app.FollowingAll {
		t.Fatalf("expected FollowingAll modal, got %s", state.Page)
	}
	tester.Key(0, ui.KeyEscape)
	tester.Frame()
	if state.Page != app.Following {
		t.Fatalf("expected Back from FollowingAll to return to Following, got %s", state.Page)
	}

	// Switch to Account B (SupportsLive=false) -> isolates data and hides live tab!
	auth.SelectAccount("acc-b", nil)
	for auth.Accounts().CurrentID != "acc-b" || !following.Snapshot().Loaded || len(following.Snapshot().Creators) != 1 || following.Snapshot().Creators[0].Creator.ID != "c9" {
		(<-queue)()
	}
	drain()
	tester.Frame()
	if len(following.Snapshot().Creators) != 1 || following.Snapshot().Creators[0].Creator.ID != "c9" {
		t.Fatalf("expected Account B isolated creator c9, got %+v", following.Snapshot().Creators)
	}
	if view.focus.item(followingTabLive) != nil {
		t.Fatal("expected live tab hidden when SupportsLive=false")
	}
}

type failingFollowingSession struct {
	uiFollowingSession
	err error
}

func (s *failingFollowingSession) ReadFollowing(_ context.Context, _ provider.FollowingRequest) (provider.FollowingPage, error) {
	return provider.FollowingPage{}, s.err
}

func TestFollowingHidesLiveTabWithoutLiveCapabilityAndUsesRequestError(t *testing.T) {
	assets, err := LoadAssets()
	if err != nil {
		t.Fatal(err)
	}
	pages, err := NewPages(assets)
	if err != nil {
		t.Fatal(err)
	}
	entry := app.Entry{
		ID: "music-source", SourceID: "music-source", Name: "MusicSource",
		Capabilities: provider.Capabilities{
			provider.BrowseCatalog, provider.BrowseFollowing, provider.SignIn,
		},
	}
	state, err := app.NewState([]app.Entry{entry}, entry.ID, pages.Routes)
	if err != nil {
		t.Fatal(err)
	}
	sess := &failingFollowingSession{err: &provider.Error{Code: provider.RateLimited}}
	queue := make(chan func(), 32)
	post := func(fn func()) { queue <- fn }
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
	catalog := app.NewCatalog(func(string) provider.Session { return sess }, post, func() {})
	t.Cleanup(catalog.Close)
	creds := newMemoryCreds()
	_ = creds.Save(entry.SourceID, entry.ID, []byte(`{"selected":"u1","accounts":[{"account":{"id":"u1","name":"Listener"},"data":{}}]}`))
	var view *View
	auth := app.NewAuth(creds, post, func() {}, func() {
		if view != nil {
			view.SyncAccountActivity()
		}
	}, func() {})
	t.Cleanup(auth.Close)
	following := app.NewFollowingFeed(post, func() {})
	t.Cleanup(following.Close)

	view = newTestView(t, Options{
		State: state, Catalog: catalog, Auth: auth, Following: following,
		Pages: pages, Assets: assets, Theme: theme.Default(),
	})
	if !state.Navigate(app.Route{Page: app.Following}) {
		t.Fatal("failed to navigate to Following")
	}
	view.SyncEntry()
	for !auth.Accounts().Ready || following.Snapshot().Phase != app.CatalogFailed {
		(<-queue)()
	}
	drain()
	tester := ui.NewTester(view.Render, 1920, 1080)
	tester.Frame()

	// Without BrowseLive, Live tab must stay hidden even when Following fails before load.
	if view.focus.item(followingTabLive) != nil {
		t.Fatal("expected live tab hidden when entry lacks BrowseLive and Following failed")
	}
	if view.focus.item(followingRetryFocus) == nil {
		t.Fatal("expected retry CTA on Following failure")
	}
	// requestError must NOT surface sign-in error text on content failures.
	p := PageContext{Assets: assets}
	if got := p.requestError(provider.RateLimited); got != assets.Text.Home.RateLimited || got == assets.Text.Auth.NetworkFailed {
		t.Fatalf("requestError(RateLimited) = %q, want %q", got, assets.Text.Home.RateLimited)
	}
	if got := p.requestError(provider.Network); got != assets.Text.Plugins.InstallErrNetwork || got == assets.Text.Auth.NetworkFailed {
		t.Fatalf("requestError(Network) = %q, want %q", got, assets.Text.Plugins.InstallErrNetwork)
	}
}
