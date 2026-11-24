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

type uiCourseSession struct {
	uiAuthSource
	mu     sync.Mutex
	owned  bool
	rows   []provider.AccountRow
	detail map[string]provider.MediaItem
	calls  []provider.PlaybackRequest
}

func (s *uiCourseSession) Browse(context.Context, provider.BrowseRequest) (provider.CollectionPage, error) {
	return provider.CollectionPage{}, nil
}

func (s *uiCourseSession) ReadAccount(_ context.Context, req provider.AccountRequest) (provider.AccountPage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if req.Section == provider.AccountCourses {
		return provider.AccountPage{Rows: s.rows}, nil
	}
	return provider.AccountPage{}, nil
}

func (s *uiCourseSession) Item(_ context.Context, ref provider.MediaRef) (provider.MediaItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.detail[ref.ID]
	if !ok {
		return provider.MediaItem{}, &provider.Error{Code: provider.Expired}
	}
	if item.VideoDetails != nil {
		copied := *item.VideoDetails
		copied.Segments = append([]provider.VideoSegment(nil), copied.Segments...)
		if s.owned {
			copied.Access = provider.VideoAccessGranted
			for i := range copied.Segments {
				if !copied.Segments[i].Unavailable {
					copied.Segments[i].Locked = false
				}
			}
		}
		item.VideoDetails = &copied
	}
	return item, nil
}

func (s *uiCourseSession) VideoDetail(ctx context.Context, ref provider.MediaRef) (provider.MediaItem, error) {
	return s.Item(ctx, ref)
}

func (s *uiCourseSession) ResolvePlayback(_ context.Context, req provider.PlaybackRequest) (provider.Playback, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, req)
	item := s.detail[req.Ref.ID]
	var segments []provider.VideoSegment
	if item.VideoDetails != nil {
		segments = append([]provider.VideoSegment(nil), item.VideoDetails.Segments...)
	}
	stream := provider.PlaybackStream{
		Format: provider.PlaybackDirect,
		Video:  provider.PlaybackTrack{Resource: provider.PlaybackResource{URLs: []string{"https://example.test/course.mp4"}}, ContentType: "video/mp4"},
	}
	part := req.PartID
	if part == "" && len(segments) > 0 {
		for _, seg := range segments {
			if !seg.Unavailable && !seg.Locked {
				part = seg.PartID
				break
			}
		}
		if part == "" {
			part = segments[0].PartID
		}
	}
	locked := false
	for i := range segments {
		if s.owned && !segments[i].Unavailable {
			segments[i].Locked = false
		}
		if segments[i].PartID == part {
			locked = segments[i].Locked
		}
	}
	if locked && !s.owned {
		return provider.Playback{
			Plan: provider.PlaybackPlan{
				ItemID: "aid-locked", PartID: part, Access: provider.VideoAccessRestricted,
				Restriction: provider.PlaybackPurchase, Episodes: segments,
			},
			Transport: fakeMediaTransport{},
		}, nil
	}
	return provider.Playback{
		Plan: provider.PlaybackPlan{
			ItemID: "aid-ok", PartID: part, Duration: 600, Access: provider.VideoAccessGranted,
			Streams: []provider.PlaybackStream{stream}, Episodes: segments,
		},
		Transport: fakeMediaTransport{},
	}, nil
}

func (s *uiCourseSession) Close() {}

func TestCourseLibraryDetailPlaybackAndPurchaseGate(t *testing.T) {
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
			provider.SignIn, provider.BrowseLibrary, provider.BrowseCatalog, provider.GetCatalogItem, provider.ResolvePlayback,
			provider.AccountCourses.Capability(),
		},
	}
	state, err := app.NewState([]app.Entry{entry}, entry.ID, pages.Routes)
	if err != nil {
		t.Fatal(err)
	}

	courseRef := provider.MediaRef{EntryID: entry.ID, Kind: provider.Course, ID: "cs268"}
	dur1, dur2 := int64(600), int64(900)
	segments := []provider.VideoSegment{
		{Title: "Locked Lesson 1", PartID: "cid-locked-0", DurationSeconds: &dur1, Locked: true},
		{Title: "Free Preview Lesson 2", PartID: "cid-free-1", DurationSeconds: &dur2, Status: "Full Preview"},
		{Title: "Locked Lesson 3", PartID: "cid-locked-2", DurationSeconds: &dur2, Locked: true},
		{Title: "Upcoming Lesson 4", Unavailable: true},
	}

	sess := &uiCourseSession{
		rows: []provider.AccountRow{
			{ID: "268", Title: "Structured Math Course", Subtitle: "4 lessons", Ref: &courseRef},
		},
		detail: map[string]provider.MediaItem{
			"cs268": {
				Ref: courseRef, Title: "Structured Math Course", Subtitle: "Complete syllabus.", Category: "Course",
				DurationSeconds: &dur2,
				VideoDetails: &provider.VideoDetails{
					Access:      provider.VideoAccessPreview,
					SegmentKind: provider.VideoParts,
					Segments:    segments,
				},
			},
		},
	}

	queue := make(chan func(), 64)
	post := func(fn func()) { queue <- fn }
	drainOne := func() {
		t.Helper()
		select {
		case fn := <-queue:
			fn()
		case <-time.After(3 * time.Second):
			t.Fatal("timed out waiting for async delivery")
		}
	}
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
	auth := app.NewAuth(uiTestKeychain{data: []byte(`{"selected":"owner","accounts":[{"account":{"id":"owner","name":"Owner"},"data":"e30="}]}`)},
		post, func() {}, func() {}, func() {})
	t.Cleanup(auth.Close)
	preferences := &memoryPreferences{}
	activity := app.NewAccountActivity(preferences, post, func() {})
	t.Cleanup(activity.Close)
	clock := player.Status{HasFrame: true, Duration: 600}
	playback := app.NewPlayback(catalog, player.Engine{
		Open:     func(string, float64) bool { return true },
		Snapshot: func() player.Status { return clock },
	}, nil, post, func() {})
	t.Cleanup(playback.Close)

	view := newTestView(t, Options{
		State: state, Auth: auth, Catalog: catalog, Activity: activity, Preferences: preferences,
		Pages: pages, Assets: assets, Theme: theme.Default(),
	})
	view.Playback = playback
	view.SyncEntry()
	for auth.Accounts().CurrentID != "owner" {
		drainOne()
	}
	view.SyncEntry()
	for catalog.Snapshot().Phase == app.CatalogLoading {
		drainOne()
	}
	drain()

	tester := ui.NewTester(view.Render, 1920, 1080)

	// 1. Open Library -> Courses and click the course row to open Course Detail.
	if !state.Navigate(libraryRoute(provider.AccountCourses)) {
		t.Fatal("failed to navigate to Library Courses")
	}
	activity.Ensure(provider.AccountQuery{Section: provider.AccountCourses})
	for len(activity.Snapshot(provider.AccountQuery{Section: provider.AccountCourses}).Page.Rows) == 0 {
		drainOne()
	}
	drain()
	tester.Frame()

	state.Focus = libraryCardFocus(provider.AccountQuery{Section: provider.AccountCourses}, sess.rows[0])
	view.reveal = true
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	for view.videoDetail.loading {
		drainOne()
	}
	drain()
	tester.Frame()
	if state.Page != app.VideoDetail || view.videoDetail.item.Ref != courseRef {
		t.Fatalf("expected clicking Library course card to open Course Detail, got page=%s ref=%+v", state.Page, view.videoDetail.item.Ref)
	}
	// Lesson 0 is locked and lesson 1 is unlocked, so initial part picks lesson 1.
	if view.videoDetail.part != 1 {
		t.Fatalf("expected initial course part to skip locked lesson 0 and select free lesson 1, got %d", view.videoDetail.part)
	}

	// 2. Clicking Play starts the unlocked preview lesson 2 with full playback and Course drawer items.
	state.Focus = videoDetailPlay
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	for playback.State().Loading {
		drainOne()
	}
	drain()
	tester.Frame()
	if state.Page != app.Player || playback.State().Target.PartID != "cid-free-1" || playback.State().Resolution.Plan.Access != provider.VideoAccessGranted || view.player.accessVisible {
		t.Fatalf("expected unlocked course lesson playback, got page=%s part=%q access=%s overlay=%t",
			state.Page, playback.State().Target.PartID, playback.State().Resolution.Plan.Access, view.player.accessVisible)
	}
	if len(view.player.items) != 3 || !view.player.items[0].locked || view.player.items[1].locked || !view.player.items[2].locked {
		t.Fatalf("expected player queue to preserve locked flags across 3 released lessons, got %+v", view.player.items)
	}

	// Open Info drawer in player to verify it renders Course lessons without panicking.
	view.player.openPanel(view, playerInfo)
	tester.Frame()
	view.player.closePanel(view)
	tester.Key(0, ui.KeyEscape) // hides controls
	tester.Key(0, ui.KeyEscape) // returns to Course Detail
	tester.Frame()
	if state.Page != app.VideoDetail {
		t.Fatalf("expected Escape to return to Course Detail, got %s", state.Page)
	}

	// 3. Switch to Episodes tab: clicking upcoming shows toast; clicking locked opens frameless Purchase Permission Overlay!
	view.videoDetail.tab = videoDetailEpisodes
	tester.Frame()

	state.Focus = videoSegmentFocus(3)
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	if state.Page != app.VideoDetail || view.videoDetail.feedback.text != assets.Text.Detail.EpisodeUpcomingNotice {
		t.Fatalf("expected upcoming course lesson to show notice toast, got page=%s feedback=%q", state.Page, view.videoDetail.feedback.text)
	}

	state.Focus = videoSegmentFocus(2)
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	for playback.State().Loading {
		drainOne()
	}
	drain()
	tester.Frame()
	if state.Page != app.Player || playback.State().Target.PartID != "cid-locked-2" ||
		playback.State().Resolution.Plan.Access != provider.VideoAccessRestricted ||
		playback.State().Limit != provider.PlaybackPurchase || !view.player.accessVisible {
		t.Fatalf("expected locked course lesson to display purchase gate, got page=%s part=%q access=%s limit=%s visible=%t",
			state.Page, playback.State().Target.PartID, playback.State().Resolution.Plan.Access, playback.State().Limit, view.player.accessVisible)
	}

	// Verify Purchase Permission Overlay has Return-to-Detail button and purchase eyebrow flag.
	page := view.context(state.Location, true, 1920, 1080)
	dialog := page.playbackAccessDialogFor(playback.State(), view.player.current())
	if !dialog.purchase || !dialog.returnToDetail {
		t.Fatalf("expected purchase dialog to set purchase=true and returnToDetail=true, got %+v", dialog)
	}

	// 4. RefreshAccess promotes playback once course entitlement is granted!
	sess.mu.Lock()
	sess.owned = true
	sess.mu.Unlock()
	playback.RefreshAccess()
	for playback.State().Loading {
		drainOne()
	}
	drain()
	tester.Frame()
	if playback.State().Resolution.Plan.Access != provider.VideoAccessGranted || view.player.accessVisible {
		t.Fatalf("expected RefreshAccess to unlock purchased course lesson, got access=%s visible=%t",
			playback.State().Resolution.Plan.Access, view.player.accessVisible)
	}

	// 5. Return-to-detail button works cleanly from Purchase Permission Overlay when restricted again.
	sess.mu.Lock()
	sess.owned = false
	sess.mu.Unlock()
	view.startPlaybackItem(0, nil)
	for playback.State().Loading {
		drainOne()
	}
	drain()
	tester.Frame()
	if !view.player.accessVisible {
		t.Fatal("expected restricted overlay for locked lesson 0")
	}
	state.Focus = playerStatus + "/detail"
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	if state.Page != app.VideoDetail {
		t.Fatalf("expected Return button on purchase gate to return to Course Detail, got %s", state.Page)
	}
}
