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

type uiCreatorSession struct {
	mu           sync.Mutex
	items        map[string]provider.MediaItem
	readCalls    []provider.CreatorRequest
	followCalls  []provider.CreatorFollowRequest
	creatorPages map[provider.CreatorTab]provider.CreatorPage
	creatorsByID map[string]provider.CreatorPage
	searchPage   provider.SearchPage
}

func (s *uiCreatorSession) Browse(context.Context, provider.BrowseRequest) (provider.CollectionPage, error) {
	return provider.CollectionPage{}, nil
}

func (s *uiCreatorSession) Item(_ context.Context, ref provider.MediaRef) (provider.MediaItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if item, ok := s.items[ref.ID]; ok {
		return item, nil
	}
	return provider.MediaItem{}, &provider.Error{Code: provider.Expired}
}

func (s *uiCreatorSession) VideoDetail(ctx context.Context, ref provider.MediaRef) (provider.MediaItem, error) {
	return s.Item(ctx, ref)
}

func (s *uiCreatorSession) ReadCreator(_ context.Context, req provider.CreatorRequest) (provider.CreatorPage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.readCalls = append(s.readCalls, req)
	if page, ok := s.creatorsByID[req.CreatorID]; ok {
		return page, nil
	}
	tab := req.Tab
	if tab == "" {
		tab = provider.CreatorTabVideos
	}
	return s.creatorPages[tab], nil
}

func (s *uiCreatorSession) FollowCreator(_ context.Context, req provider.CreatorFollowRequest) (provider.CreatorFollowResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.followCalls = append(s.followCalls, req)
	return provider.CreatorFollowResult{Following: req.Enabled}, nil
}

func (s *uiCreatorSession) Search(context.Context, provider.SearchRequest) (provider.SearchPage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.searchPage, nil
}

func (s *uiCreatorSession) BeginSignIn(context.Context) (provider.QRAttempt, error) {
	return nil, &provider.Error{Code: provider.Unsupported}
}
func (s *uiCreatorSession) RestoreAccount(_ context.Context, cred provider.Credential) (provider.Credential, error) {
	return cred, nil
}
func (s *uiCreatorSession) SetAccount(*provider.Credential) error              { return nil }
func (s *uiCreatorSession) SignOut(context.Context, provider.Credential) error { return nil }
func (s *uiCreatorSession) Close()                                             {}

type memoryCreds struct {
	data map[string][]byte
}

func newMemoryCreds() *memoryCreds {
	return &memoryCreds{data: make(map[string][]byte)}
}

func (m *memoryCreds) Load(pluginID, entryID string) ([]byte, error) {
	return m.data[pluginID+"/"+entryID], nil
}

func (m *memoryCreds) Save(pluginID, entryID string, data []byte) error {
	m.data[pluginID+"/"+entryID] = append([]byte(nil), data...)
	return nil
}

func TestCreatorNavigationRoundTripFollowSyncAndTabs(t *testing.T) {
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
			provider.ReadCreator, provider.FollowCreator,
			provider.EditVideo, provider.SignIn,
		},
	}
	state, err := app.NewState([]app.Entry{entry}, entry.ID, pages.Routes)
	if err != nil {
		t.Fatal(err)
	}

	followers := int64(128000)
	works := int64(24)
	colCount := int64(6)
	views := int64(45000)
	dur := int64(320)
	refA := provider.MediaRef{EntryID: entry.ID, Kind: provider.Video, ID: "BVVideoA"}
	refB := provider.MediaRef{EntryID: entry.ID, Kind: provider.Video, ID: "BVVideoB"}
	refCol := provider.MediaRef{EntryID: entry.ID, Kind: provider.Video, ID: "BVCol1"}
	creatorSeed := provider.Creator{ID: "946974", Name: "Deep Space Studio", Following: false}

	session := &uiCreatorSession{
		items: map[string]provider.MediaItem{
			"BVVideoA": {
				Ref: refA, Title: "Original Video A", Creator: &provider.Creator{ID: "946974", Name: "Deep Space Studio", Following: false},
				VideoDetails: &provider.VideoDetails{},
			},
			"BVVideoB": {
				Ref: refB, Title: "Creator Video B", Creator: &provider.Creator{ID: "946974", Name: "Deep Space Studio", Following: true},
				VideoDetails: &provider.VideoDetails{},
			},
		},
		creatorPages: map[provider.CreatorTab]provider.CreatorPage{
			provider.CreatorTabVideos: {
				Profile: &provider.CreatorProfile{
					Creator:   creatorSeed,
					Bio:       "Recording details of the world.",
					Followers: &followers,
					Works:     &works,
					Category:  "Science",
					Tabs:      []provider.CreatorTab{provider.CreatorTabVideos, provider.CreatorTabPosts, provider.CreatorTabCollections, provider.CreatorTabAbout},
				},
				Items: []provider.MediaItem{
					{Ref: refB, Title: "Creator Video B", Views: &views, DurationSeconds: &dur},
				},
			},
			provider.CreatorTabPosts: {
				Posts: []provider.CreatorPost{
					{
						ID: "post-1", PublishedLabel: "Today", Body: "New episode is out!",
						Item: &provider.MediaItem{Ref: refB, Title: "Creator Video B", Views: &views, DurationSeconds: &dur},
					},
				},
			},
			provider.CreatorTabCollections: {
				Collections: []provider.CreatorCollection{
					{
						ID: "season:10", Title: "World Through Lens", Count: &colCount,
						Items: []provider.MediaItem{{Ref: refCol, Title: "Episode 1", Views: &views, DurationSeconds: &dur}},
					},
				},
			},
		},
	}

	callbacks := make(chan func(), 64)
	drain := func() {
		for {
			select {
			case fn := <-callbacks:
				fn()
			default:
				return
			}
		}
	}
	catalog := app.NewCatalog(func(string) provider.Session { return session }, func(fn func()) { callbacks <- fn }, func() {})
	defer catalog.Close()

	store := newMemoryCreds()
	_ = store.Save(entry.SourceID, entry.ID, []byte(`{"selected":"42","accounts":[{"account":{"id":"42","name":"Viewer"},"data":{"csrf":"ok"}}]}`))
	auth := app.NewAuth(store, func(fn func()) { callbacks <- fn }, func() {}, func() {}, func() {})
	defer auth.Close()

	view := newTestView(t, Options{State: state, Catalog: catalog, Auth: auth, Pages: pages, Assets: assets, Theme: theme.Default()})
	view.SyncEntry()
	for len(callbacks) > 0 || !auth.Accounts().Ready {
		(<-callbacks)()
	}
	drain()

	tester := ui.NewTester(view.Render, 1920, 1080)
	tester.Frame()

	// 1. Open VideoDetail(A), focus creator button, press Enter to open Creator(946974).
	view.dispatch(func() {
		p := view.context(view.State.Location, true, 1920, 1080)
		p.openVideoDetail(session.items["BVVideoA"])
	})
	(<-callbacks)()
	drain()
	tester.Frame()
	if view.State.Page != app.VideoDetail || view.videoDetail.item.Ref.ID != "BVVideoA" {
		t.Fatalf("expected VideoDetail(A), got page=%s item=%s", view.State.Page, view.videoDetail.item.Ref.ID)
	}

	view.dispatch(func() { view.moveFocus(videoDetailCreatorID, app.Down, false) })
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	(<-callbacks)()
	drain()
	tester.Frame()
	if view.State.Page != app.Creator || view.creator == nil || view.creator.profile.Creator.Name != "Deep Space Studio" {
		t.Fatalf("expected Creator page loaded, got page=%s creator=%+v", view.State.Page, view.creator)
	}
	if view.State.Focus != creatorFollowFocus {
		t.Fatalf("expected initial focus on follow button, got %s", view.State.Focus)
	}

	// 2. Toggle follow via Enter on Creator page and verify sync to stacked VideoDetail(A).
	tester.Key(0, ui.KeyEnter)
	(<-callbacks)()
	drain()
	tester.Frame()
	if !view.creator.profile.Creator.Following {
		t.Fatal("expected creator profile Following=true")
	}
	if !view.videoDetail.item.Creator.Following {
		t.Fatal("expected stacked VideoDetail(A) creator Following=true")
	}

	// 3. Move down to Videos tab, then Right across Posts -> Collections -> About -> Left back to Videos.
	tester.Key(0, ui.KeyDown)
	if view.State.Focus != creatorTabFocus(provider.CreatorTabVideos) {
		t.Fatalf("expected focus on Videos tab, got %s", view.State.Focus)
	}
	tester.Key(0, ui.KeyRight)
	(<-callbacks)()
	drain()
	tester.Frame()
	if view.creator.tab != provider.CreatorTabPosts || len(view.creator.posts.posts) != 1 {
		t.Fatalf("expected Posts tab loaded, got tab=%s len=%d", view.creator.tab, len(view.creator.posts.posts))
	}
	if _, ok := tester.Find("New episode is out!"); !ok {
		t.Fatal("expected dynamic post body rendered")
	}

	tester.Key(0, ui.KeyRight)
	(<-callbacks)()
	drain()
	tester.Frame()
	if view.creator.tab != provider.CreatorTabCollections || len(view.creator.collections.collections) != 1 {
		t.Fatalf("expected Collections tab loaded, got tab=%s len=%d", view.creator.tab, len(view.creator.collections.collections))
	}
	if _, ok := tester.Find("World Through Lens"); !ok {
		t.Fatal("expected collection title rendered")
	}

	tester.Key(0, ui.KeyRight)
	tester.Frame()
	if view.creator.tab != provider.CreatorTabAbout {
		t.Fatalf("expected About tab, got %s", view.creator.tab)
	}
	if _, ok := tester.Find("Science"); !ok {
		t.Fatal("expected creator category rendered in About tab")
	}

	// Return to Videos tab (3x Left).
	tester.Key(0, ui.KeyLeft)
	tester.Key(0, ui.KeyLeft)
	tester.Key(0, ui.KeyLeft)
	tester.Frame()
	if view.creator.tab != provider.CreatorTabVideos {
		t.Fatalf("expected Videos tab, got %s", view.creator.tab)
	}

	// 4. Move Down onto Video B and press Enter -> opens VideoDetail(B).
	videoBFocus := creatorVideoFocus(refB)
	tester.Key(0, ui.KeyDown)
	tester.Frame()
	if view.State.Focus != videoBFocus {
		t.Fatalf("expected focus on video card B (%s), got %s", videoBFocus, view.State.Focus)
	}
	tester.Key(0, ui.KeyEnter)
	(<-callbacks)()
	drain()
	tester.Frame()
	if view.State.Page != app.VideoDetail || view.videoDetail.item.Ref.ID != "BVVideoB" {
		t.Fatalf("expected VideoDetail(B), got page=%s item=%s", view.State.Page, view.videoDetail.item.Ref.ID)
	}
	if len(view.videoDetailStack) != 1 || view.videoDetailStack[0].item.Ref.ID != "BVVideoA" {
		t.Fatalf("expected VideoDetail(A) preserved on stack, got %+v", view.videoDetailStack)
	}

	// 5. Back from VideoDetail(B) -> Creator(946974) with exact video focus preserved.
	if !view.goBack() {
		t.Fatal("expected Back from VideoDetail(B) to succeed")
	}
	tester.Frame()
	if view.State.Page != app.Creator || view.State.Focus != videoBFocus || view.creator == nil || view.creator.tab != provider.CreatorTabVideos {
		t.Fatalf("expected Creator restored at %s, got page=%s focus=%s", videoBFocus, view.State.Page, view.State.Focus)
	}
	if len(view.videoDetailStack) != 0 || view.videoDetail.item.Ref.ID != "BVVideoA" {
		t.Fatalf("expected VideoDetail(A) popped back into videoDetail, got stack=%d current=%s", len(view.videoDetailStack), view.videoDetail.item.Ref.ID)
	}

	// 6. Back from Creator -> VideoDetail(A) with focus restored to creator button and Following=true.
	if !view.goBack() {
		t.Fatal("expected Back from Creator to succeed")
	}
	tester.Frame()
	if view.State.Page != app.VideoDetail || view.State.Focus != videoDetailCreatorID ||
		view.videoDetail.item.Ref.ID != "BVVideoA" || !view.videoDetail.item.Creator.Following {
		t.Fatalf("expected VideoDetail(A) restored with Following=true at creator button, got page=%s focus=%s item=%s following=%v",
			view.State.Page, view.State.Focus, view.videoDetail.item.Ref.ID, view.videoDetail.item.Creator.Following)
	}
	if view.creator != nil {
		t.Fatal("expected creator state closed after leaving")
	}

	// 7. Back from VideoDetail(A) -> Home.
	if !view.goBack() {
		t.Fatal("expected Back from VideoDetail(A) to succeed")
	}
	if view.State.Page != app.Home {
		t.Fatalf("expected Home, got %s", view.State.Page)
	}
}

func TestCreatorCapabilityHidingAndSearchEntry(t *testing.T) {
	assets, err := LoadAssets()
	if err != nil {
		t.Fatal(err)
	}
	pages, err := NewPages(assets)
	if err != nil {
		t.Fatal(err)
	}
	// Source declares Search + ReadCreator, but NO FollowCreator.
	entry := app.Entry{
		ID: "neutral", SourceID: "neutral", Name: "Neutral",
		Capabilities: provider.Capabilities{
			provider.BrowseCatalog, provider.SearchCatalog, provider.ReadCreator,
		},
	}
	state, err := app.NewState([]app.Entry{entry}, entry.ID, pages.Routes)
	if err != nil {
		t.Fatal(err)
	}
	creator := provider.Creator{ID: "c1", Name: "Indie Filmmaker"}
	session := &uiCreatorSession{
		searchPage: provider.SearchPage{
			Creators: []provider.SearchCreatorItem{{Creator: creator}},
			Kinds:    []provider.SearchKind{provider.SearchCreator},
		},
		creatorPages: map[provider.CreatorTab]provider.CreatorPage{
			provider.CreatorTabVideos: {
				Profile: &provider.CreatorProfile{
					Creator: creator,
					Tabs:    []provider.CreatorTab{provider.CreatorTabVideos, provider.CreatorTabAbout},
				},
			},
		},
	}

	catalogCallbacks := make(chan func(), 32)
	searchCallbacks := make(chan func(), 32)
	catalog := app.NewCatalog(func(string) provider.Session { return session }, func(fn func()) { catalogCallbacks <- fn }, func() {})
	defer catalog.Close()
	search := app.NewCatalogSearch(nil, func(fn func()) { searchCallbacks <- fn }, func() {})
	defer search.Close()

	view := newTestView(t, Options{State: state, Catalog: catalog, Search: search, Pages: pages, Assets: assets, Theme: theme.Default()})
	tester := ui.NewTester(view.Render, 1920, 1080)
	view.SyncEntry()
	(<-catalogCallbacks)()

	// Navigate to Search, switch to creator kind, select creator card via Enter.
	view.dispatch(func() { view.State.Navigate(app.Route{Page: app.Search}) })
	(<-searchCallbacks)()
	view.dispatch(func() { view.Search.SelectKind(provider.SearchCreator) })
	(<-searchCallbacks)()
	tester.Frame()

	cardID := searchCreatorFocus("c1")
	view.dispatch(func() { view.moveFocus(cardID, app.Down, false) })
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	(<-catalogCallbacks)()
	tester.Frame()

	if view.State.Page != app.Creator {
		t.Fatalf("expected Creator page from search card, got %s", view.State.Page)
	}
	// Follow button must be hidden and initial focus must be on first tab.
	if view.focus.item(creatorFollowFocus) != nil {
		t.Fatal("follow button must be hidden when FollowCreator capability is absent")
	}
	if view.State.Focus != creatorTabFocus(provider.CreatorTabVideos) {
		t.Fatalf("expected focus on first visible tab, got %s", view.State.Focus)
	}
	if view.focus.item(creatorTabFocus(provider.CreatorTabPosts)) != nil ||
		view.focus.item(creatorTabFocus(provider.CreatorTabCollections)) != nil {
		t.Fatal("unsupported tabs must be hidden")
	}
	if view.focus.item(creatorTabFocus(provider.CreatorTabAbout)) == nil {
		t.Fatal("expected About tab visible")
	}

	// Back returns to Search at exact creator card focus.
	if !view.goBack() {
		t.Fatal("expected Back to Search to succeed")
	}
	tester.Frame()
	if view.State.Page != app.Search || view.State.Focus != cardID {
		t.Fatalf("expected Search restored at %s, got page=%s focus=%s", cardID, view.State.Page, view.State.Focus)
	}
}

func TestVideoDetailMultiCreatorsAvatarHoverAndHiddenNames(t *testing.T) {
	assets, err := LoadAssets()
	if err != nil {
		t.Fatal(err)
	}
	pages, err := NewPages(assets)
	if err != nil {
		t.Fatal(err)
	}

	// 1. Cast avatar row without FollowCreator: hidden/omitted names must not render
	// inline text, focus must target circular 50x50 avatars, and stepping Right/Enter
	// must open the focused actor's creator page and restore focus on Back.
	cinemaEntry := app.Entry{
		ID: "cinema", SourceID: "cinema", Name: "Cinema",
		Capabilities: provider.Capabilities{
			provider.BrowseCatalog, provider.GetCatalogItem, provider.ResolvePlayback, provider.ReadCreator,
		},
	}
	cinemaState, err := app.NewState([]app.Entry{cinemaEntry}, cinemaEntry.ID, pages.Routes)
	if err != nil {
		t.Fatal(err)
	}
	movieRef := provider.MediaRef{EntryID: cinemaEntry.ID, Kind: provider.Movie, ID: "m1"}
	cinemaSession := &uiCreatorSession{
		items: map[string]provider.MediaItem{
			"m1": {
				Ref:   movieRef,
				Title: "Ensemble Film",
				Creators: []provider.Creator{
					{ID: "actor-1", Name: "Actor One", HideName: true},
					{ID: "actor-2"},
					{ID: "actor-3", Name: "Actor Three", HideName: true},
				},
				VideoDetails: &provider.VideoDetails{Access: provider.VideoAccessGranted},
			},
		},
		creatorsByID: map[string]provider.CreatorPage{
			"actor-3": {
				Profile: &provider.CreatorProfile{
					Creator: provider.Creator{ID: "actor-3", Name: "Actor Three"},
					Tabs:    []provider.CreatorTab{provider.CreatorTabVideos},
				},
			},
		},
	}
	cinemaCB := make(chan func(), 32)
	drainCinema := func() {
		for {
			select {
			case fn := <-cinemaCB:
				fn()
			default:
				return
			}
		}
	}
	cinemaCatalog := app.NewCatalog(func(string) provider.Session { return cinemaSession }, func(fn func()) { cinemaCB <- fn }, func() {})
	defer cinemaCatalog.Close()

	cinemaView := newTestView(t, Options{State: cinemaState, Catalog: cinemaCatalog, Pages: pages, Assets: assets, Theme: theme.Default()})
	cinemaView.SyncEntry()
	drainCinema()
	cinemaTester := ui.NewTester(cinemaView.Render, 1920, 1080)
	cinemaTester.Frame()

	cinemaView.dispatch(func() {
		p := cinemaView.context(cinemaView.State.Location, true, 1920, 1080)
		p.openVideoDetail(cinemaSession.items["m1"])
	})
	(<-cinemaCB)()
	drainCinema()
	cinemaTester.Frame()
	cinemaTester.Advance(2 * videoDetailTransition.Duration)
	cinemaTester.Frame()

	if cinemaView.focus.item(videoDetailFollow) != nil {
		t.Fatal("expected follow button hidden without FollowCreator capability")
	}
	var avatarRects [3]app.FocusTarget
	for i := range 3 {
		id := videoDetailCreatorFocus(i)
		ctrl := cinemaView.focus.item(id)
		if ctrl == nil {
			t.Fatalf("missing focusable circular avatar %s", id)
		}
		if ctrl.target.W != 50 || ctrl.target.H != 50 {
			t.Fatalf("expected circular 50x50 avatar button for %s, got %vx%v", id, ctrl.target.W, ctrl.target.H)
		}
		avatarRects[i] = ctrl.target
	}
	// Hidden/omitted names render an avatar-only row: 50px avatar + 14px gap = 64px pitch.
	if gap := avatarRects[1].X - avatarRects[0].X; gap != 64 {
		t.Fatalf("expected avatar-only pitch of 64px (no inline name text), got %v", gap)
	}
	if rect, ok := cinemaTester.Find("Actor One"); !ok || rect.W != 50 || rect.H != 50 {
		t.Fatalf("expected Actor One accessibility label only on the 50x50 avatar button, got ok=%v rect=%+v", ok, rect)
	}

	// Up from Play moves to first circular avatar; Right twice reaches actor-3.
	cinemaTester.Key(0, ui.KeyUp)
	cinemaTester.Frame()
	if cinemaState.Focus != videoDetailCreatorID {
		t.Fatalf("expected Up from Play to focus %s, got %s", videoDetailCreatorID, cinemaState.Focus)
	}
	cinemaTester.Key(0, ui.KeyRight)
	cinemaTester.Frame()
	if cinemaState.Focus != videoDetailCreatorFocus(1) {
		t.Fatalf("expected focus on creator/1, got %s", cinemaState.Focus)
	}
	cinemaTester.Key(0, ui.KeyRight)
	cinemaTester.Frame()
	if cinemaState.Focus != videoDetailCreatorFocus(2) {
		t.Fatalf("expected focus on creator/2, got %s", cinemaState.Focus)
	}
	cinemaTester.Key(0, ui.KeyEnter)
	(<-cinemaCB)()
	drainCinema()
	cinemaTester.Frame()
	cinemaTester.Advance(2 * videoDetailTransition.Duration)
	cinemaTester.Frame()
	if cinemaState.Page != app.Creator || cinemaView.creator == nil || cinemaView.creator.profile.Creator.Name != "Actor Three" {
		t.Fatalf("expected Actor Three creator page, got page=%s creator=%+v", cinemaState.Page, cinemaView.creator)
	}
	if !cinemaView.goBack() {
		t.Fatal("expected Back from actor page to succeed")
	}
	cinemaTester.Frame()
	if cinemaState.Page != app.VideoDetail || cinemaState.Focus != videoDetailCreatorFocus(2) {
		t.Fatalf("expected VideoDetail restored at creator/2, got page=%s focus=%s", cinemaState.Page, cinemaState.Focus)
	}

	// 2. Named co-authors with FollowCreator: follow sits next to primary creator,
	// Right steps primary avatar -> follow -> co-author avatar, and following the
	// co-author from their creator page syncs Creators[1].Following independently.
	collabEntry := app.Entry{
		ID: "collab", SourceID: "collab", Name: "Collab",
		Capabilities: provider.Capabilities{
			provider.BrowseCatalog, provider.GetCatalogItem, provider.ResolvePlayback,
			provider.ReadCreator, provider.FollowCreator, provider.EditVideo, provider.SignIn,
		},
	}
	collabState, err := app.NewState([]app.Entry{collabEntry}, collabEntry.ID, pages.Routes)
	if err != nil {
		t.Fatal(err)
	}
	collabRef := provider.MediaRef{EntryID: collabEntry.ID, Kind: provider.Video, ID: "v-collab"}
	owner := provider.Creator{ID: "u1", Name: "Primary Author"}
	coauthor := provider.Creator{ID: "u2", Name: "Guest CoAuthor"}
	collabSession := &uiCreatorSession{
		items: map[string]provider.MediaItem{
			"v-collab": {
				Ref:          collabRef,
				Title:        "Joint Video",
				Creator:      &owner,
				Creators:     []provider.Creator{owner, coauthor},
				VideoDetails: &provider.VideoDetails{Access: provider.VideoAccessGranted},
			},
		},
		creatorsByID: map[string]provider.CreatorPage{
			"u2": {
				Profile: &provider.CreatorProfile{
					Creator: coauthor,
					Tabs:    []provider.CreatorTab{provider.CreatorTabVideos},
				},
			},
		},
	}
	collabCB := make(chan func(), 32)
	drainCollab := func() {
		for {
			select {
			case fn := <-collabCB:
				fn()
			default:
				return
			}
		}
	}
	collabCatalog := app.NewCatalog(func(string) provider.Session { return collabSession }, func(fn func()) { collabCB <- fn }, func() {})
	defer collabCatalog.Close()
	creds := newMemoryCreds()
	_ = creds.Save(collabEntry.SourceID, collabEntry.ID, []byte(`{"selected":"42","accounts":[{"account":{"id":"42","name":"Viewer"},"data":{"csrf":"ok"}}]}`))
	collabAuth := app.NewAuth(creds, func(fn func()) { collabCB <- fn }, func() {}, func() {}, func() {})
	defer collabAuth.Close()

	collabView := newTestView(t, Options{State: collabState, Catalog: collabCatalog, Auth: collabAuth, Pages: pages, Assets: assets, Theme: theme.Default()})
	collabView.SyncEntry()
	for len(collabCB) > 0 || !collabAuth.Accounts().Ready {
		(<-collabCB)()
	}
	drainCollab()
	collabTester := ui.NewTester(collabView.Render, 1920, 1080)
	collabTester.Frame()

	collabView.dispatch(func() {
		p := collabView.context(collabView.State.Location, true, 1920, 1080)
		p.openVideoDetail(collabSession.items["v-collab"])
	})
	(<-collabCB)()
	drainCollab()
	collabTester.Frame()
	collabTester.Advance(2 * videoDetailTransition.Duration)
	collabTester.Frame()

	if !collabTester.HasText("Primary Author") || !collabTester.HasText("Guest CoAuthor") {
		t.Fatal("expected named co-authors to render both names beside their avatars")
	}
	primaryAvatar := collabView.focus.item(videoDetailCreatorID)
	followBtn := collabView.focus.item(videoDetailFollow)
	if primaryAvatar == nil || followBtn == nil || followBtn.target.X-primaryAvatar.target.X <= 80 {
		t.Fatalf("expected inline author name space between primary avatar and follow button, avatar=%+v follow=%+v", primaryAvatar, followBtn)
	}

	collabView.dispatch(func() { collabView.moveFocus(videoDetailCreatorID, app.Up, false) })
	collabTester.Frame()
	collabTester.Key(0, ui.KeyRight)
	collabTester.Frame()
	if collabState.Focus != videoDetailFollow {
		t.Fatalf("expected Right from primary avatar to focus follow button, got %s", collabState.Focus)
	}
	collabTester.Key(0, ui.KeyRight)
	collabTester.Frame()
	if collabState.Focus != videoDetailCreatorFocus(1) {
		t.Fatalf("expected Right from follow button to focus co-author avatar, got %s", collabState.Focus)
	}
	collabTester.Key(0, ui.KeyLeft)
	collabTester.Frame()
	if collabState.Focus != videoDetailFollow {
		t.Fatalf("expected Left from co-author avatar back to follow button, got %s", collabState.Focus)
	}
	collabTester.Key(0, ui.KeyRight)
	collabTester.Frame()
	collabTester.Key(0, ui.KeyEnter)
	(<-collabCB)()
	drainCollab()
	collabTester.Frame()
	collabTester.Advance(2 * videoDetailTransition.Duration)
	collabTester.Frame()
	if collabState.Page != app.Creator || collabView.creator.profile.Creator.ID != "u2" {
		t.Fatalf("expected co-author Creator(u2) page, got page=%s creator=%+v", collabState.Page, collabView.creator)
	}
	// Follow co-author u2 on their Creator page.
	collabTester.Key(0, ui.KeyEnter)
	(<-collabCB)()
	drainCollab()
	collabTester.Frame()
	collabTester.Advance(2 * videoDetailTransition.Duration)
	collabTester.Frame()
	if !collabView.videoDetail.item.Creators[1].Following || collabView.videoDetail.item.Creators[0].Following {
		t.Fatalf("expected Creators[1].Following=true and Creators[0].Following=false, got %+v", collabView.videoDetail.item.Creators)
	}
}
