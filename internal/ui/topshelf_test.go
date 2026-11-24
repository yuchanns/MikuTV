package tvui

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/provider"
	"github.com/yuchanns/MikuTV/internal/theme"
	"github.com/yuchanns/MikuTV/internal/topshelf"
)

type stubTopShelfStore struct {
	mu       sync.Mutex
	snapshot topshelf.Snapshot
	saves    int
	notifies int
}

func (s *stubTopShelfStore) Load() (topshelf.Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return topshelf.SanitizeSnapshot(s.snapshot), nil
}

func (s *stubTopShelfStore) Save(snap topshelf.Snapshot) error {
	s.mu.Lock()
	s.snapshot = topshelf.SanitizeSnapshot(snap)
	s.saves++
	s.mu.Unlock()
	return nil
}

func (s *stubTopShelfStore) NotifyChanged() {
	s.mu.Lock()
	s.notifies++
	s.mu.Unlock()
}

type topShelfTestSession struct {
	uiAuthSource
	entryID string
	items   []provider.MediaItem
	history []provider.AccountRow
}

func (s *topShelfTestSession) Browse(_ context.Context, _ provider.BrowseRequest) (provider.CollectionPage, error) {
	return provider.CollectionPage{
		Collections: []provider.Collection{{ID: "feed", Items: s.items}},
	}, nil
}

func (s *topShelfTestSession) Item(_ context.Context, ref provider.MediaRef) (provider.MediaItem, error) {
	for _, it := range s.items {
		if it.Ref == ref {
			return it, nil
		}
	}
	return provider.MediaItem{Ref: ref, Title: "Resolved " + ref.ID}, nil
}

func (s *topShelfTestSession) Close() {}

func (s *topShelfTestSession) ReadAccount(_ context.Context, req provider.AccountRequest) (provider.AccountPage, error) {
	if req.Section == provider.AccountHistory {
		return provider.AccountPage{Rows: s.history}, nil
	}
	return provider.AccountPage{}, nil
}

type stubTopShelfPrefs struct {
	recent      string
	historySync map[string]bool
}

func (p *stubTopShelfPrefs) Load() app.Preferences { return app.Preferences{RecentEntry: p.recent} }
func (p *stubTopShelfPrefs) Save(pref app.Preferences) error {
	p.recent = pref.RecentEntry
	return nil
}
func (p *stubTopShelfPrefs) HistorySync(entry, owner string) bool {
	if p.historySync == nil {
		return true
	}
	v, ok := p.historySync[entry+"/"+owner]
	return !ok || v
}
func (p *stubTopShelfPrefs) SaveHistorySync(entry, owner string, enabled bool) error {
	if p.historySync == nil {
		p.historySync = make(map[string]bool)
	}
	p.historySync[entry+"/"+owner] = enabled
	return nil
}

func TestTopShelfPublishingAndEntryRecovery(t *testing.T) {
	assets, err := LoadAssets()
	if err != nil {
		t.Fatal(err)
	}
	pages, err := NewPages(assets)
	if err != nil {
		t.Fatal(err)
	}
	capsA := provider.Capabilities{
		provider.BrowseCatalog,
		provider.GetCatalogItem,
		provider.ResolvePlayback,
		provider.SignIn,
		provider.AccountHistory.Capability(),
	}
	capsB := provider.Capabilities{
		provider.BrowseCatalog,
		provider.GetCatalogItem,
		provider.ResolvePlayback,
	}
	entryA := app.Entry{ID: "src-a", SourceID: "src-a", Name: "Source Alpha", Capabilities: capsA}
	entryB := app.Entry{ID: "src-b", SourceID: "src-b", Name: "Source Beta", Capabilities: capsB}

	dur := int64(1800)
	prog := int64(540)
	histItem := provider.MediaItem{
		Ref:             provider.MediaRef{EntryID: "src-a", Kind: provider.Video, ID: "v-hist"},
		Title:           "Continue Episode",
		CoverURL:        "https://img.example.com/hist.jpg",
		DurationSeconds: &dur,
		ProgressSeconds: &prog,
		Creator:         &provider.Creator{Name: "CreatorA"},
	}
	feat1 := provider.MediaItem{
		Ref:      provider.MediaRef{EntryID: "src-a", Kind: provider.Video, ID: "v-feat-1"},
		Title:    "Featured One",
		CoverURL: "https://img.example.com/feat1.jpg",
		Quality:  "4K HDR",
		Category: "Science",
		Creator:  &provider.Creator{Name: "CreatorB"},
	}
	featB := provider.MediaItem{
		Ref:      provider.MediaRef{EntryID: "src-b", Kind: provider.Series, ID: "s-beta-1"},
		Title:    "Beta Show",
		CoverURL: "https://img.example.com/beta1.jpg",
	}

	sessions := map[string]*topShelfTestSession{
		"src-a": {
			entryID: "src-a",
			items:   []provider.MediaItem{feat1},
			history: []provider.AccountRow{{Item: &histItem}},
		},
		"src-b": {
			entryID: "src-b",
			items:   []provider.MediaItem{featB},
		},
	}

	queue := make(chan func(), 64)
	post := func(fn func()) { queue <- fn }

	prefs := &stubTopShelfPrefs{recent: "src-a"}
	store := &stubTopShelfStore{}

	state, err := app.NewState([]app.Entry{entryA, entryB}, "src-a", pages.Routes)
	if err != nil {
		t.Fatal(err)
	}
	catalog := app.NewCatalog(func(entryID string) provider.Session {
		return sessions[entryID]
	}, post, func() {})
	t.Cleanup(catalog.Close)
	var view *View
	creds := uiTestKeychain{data: []byte(`{"selected":"user-1","accounts":[{"account":{"id":"user-1","name":"Alice"},"data":"e30="}]}`)}
	auth := app.NewAuth(creds, post, func() {}, func() {
		catalog.Reload()
		if view != nil {
			view.SyncAccountActivity()
		}
	}, func() {})
	t.Cleanup(auth.Close)
	activity := app.NewAccountActivity(prefs, post, func() {})
	t.Cleanup(activity.Close)
	waitUntil := func(cond func() bool, label string) {
		t.Helper()
		deadline := time.After(2 * time.Second)
		for !cond() {
			select {
			case fn := <-queue:
				fn()
			case <-deadline:
				t.Fatalf("timed out waiting for %s (auth=%+v cat=%d act=%d sync=%v avail=%v)", label, auth.Accounts(), catalog.Snapshot().Phase, activity.Snapshot(homeHistory).Phase, activity.HistorySync(), activity.Available(provider.AccountHistory))
			}
		}
	}

	view = newTestView(t, Options{
		State:       state,
		Catalog:     catalog,
		Auth:        auth,
		Activity:    activity,
		Pages:       pages,
		Assets:      assets,
		Preferences: prefs,
		Theme:       theme.Default(),
		TopShelf:    store,
	})
	view.SyncEntry()
	waitUntil(func() bool { return auth.Accounts().Ready && auth.Accounts().CurrentID == "user-1" }, "auth restore")
	view.SyncEntry()
	waitUntil(func() bool {
		return catalog.Snapshot().Phase == app.CatalogReady &&
			activity.Snapshot(homeHistory).Phase == app.CatalogReady
	}, "catalog and history load")

	tester := ui.NewTester(view.Render, 1920, 1080)
	tester.Frame()
	tester.Frame()
	view.flushTopShelf()

	// 1. Verify TopShelf snapshot contains Continue Watching section + Home Recommendations section, attributed to src-a.
	snap, _ := store.Load()
	if snap.EntryID != "src-a" {
		t.Fatalf("snapshot EntryID = %q, want src-a", snap.EntryID)
	}
	if len(snap.Sections) != 2 {
		t.Fatalf("snapshot sections = %d, want 2: %+v", len(snap.Sections), snap.Sections)
	}
	if snap.Sections[0].ID != "continue" || len(snap.Sections[0].Items) != 1 || snap.Sections[0].Items[0].ID != "v-hist" || !snap.Sections[0].Items[0].ContinueWatching {
		t.Fatalf("expected continue watching section first, got %+v", snap.Sections[0])
	}
	if !strings.Contains(snap.Sections[0].Title, "Source Alpha") {
		t.Errorf("multi-entry continue section title should include source name, got %q", snap.Sections[0].Title)
	}
	if snap.Sections[0].Items[0].PlaybackProgress() <= 0 {
		t.Errorf("expected positive playback progress for continue item, got %v", snap.Sections[0].Items[0].PlaybackProgress())
	}
	if snap.Sections[1].ID != "recommended" || len(snap.Sections[1].Items) != 1 || snap.Sections[1].Items[0].ID != "v-feat-1" || snap.Sections[1].Items[0].ContinueWatching {
		t.Fatalf("expected recommended section second, got %+v", snap.Sections[1])
	}
	if !strings.Contains(snap.Sections[1].Title, "Source Alpha") {
		t.Errorf("multi-entry recommended section title should include source name, got %q", snap.Sections[1].Title)
	}

	// 2. Disabling HistorySync immediately strips Continue Watching section while preserving Home Recommendations.
	if err := activity.ToggleHistorySync(); err != nil {
		t.Fatal(err)
	}
	view.SyncTopShelf()
	view.flushTopShelf()
	snap, _ = store.Load()
	if len(snap.Sections) != 1 || snap.Sections[0].ID != "recommended" || len(snap.Sections[0].Items) != 1 || snap.Sections[0].Items[0].ID != "v-feat-1" {
		t.Fatalf("expected only recommended section when history sync off, got %+v", snap.Sections)
	}

	// 3. Switch to src-b, then open a TopShelf detail link owned by src-a -> restores src-a entry & opens VideoDetail!
	state.SelectEntry("src-b")
	view.SyncEntry()
	waitUntil(func() bool { return catalog.Snapshot().Phase == app.CatalogReady }, "src-b catalog")
	tester.Frame()
	if state.CurrentEntry != "src-b" {
		t.Fatalf("expected current entry src-b, got %q", state.CurrentEntry)
	}
	detailURL := topshelf.BuildURL(topshelf.Link{
		Action:  topshelf.ActionDetail,
		EntryID: "src-a",
		Kind:    "video",
		ID:      "v-feat-1",
	})
	if !view.OpenTopShelfURL(detailURL) {
		t.Fatal("OpenTopShelfURL(detail) returned false")
	}
	if state.CurrentEntry != "src-a" {
		t.Fatalf("entry recovery failed: CurrentEntry = %q, want src-a", state.CurrentEntry)
	}
	if prefs.recent != "src-a" {
		t.Fatalf("entry recovery did not persist RecentEntry, got %q", prefs.recent)
	}
	waitUntil(func() bool { return auth.Accounts().Ready && state.Page == app.VideoDetail }, "detail after auth settled")

	// 4. Cold-launch play link queued when Playback is nil, then stays queued on src-b until Playback ready.
	playURL := topshelf.BuildURL(topshelf.Link{
		Action:  topshelf.ActionPlay,
		EntryID: "src-b",
		Kind:    "series",
		ID:      "s-beta-1",
	})
	if !view.OpenTopShelfURL(playURL) {
		t.Fatal("OpenTopShelfURL(play) returned false")
	}
	if state.CurrentEntry != "src-b" {
		t.Fatalf("play link did not restore entry src-b, got %q", state.CurrentEntry)
	}
	if view.pendingTopShelfURL != playURL {
		t.Fatalf("expected play link queued while Playback is nil, got %q", view.pendingTopShelfURL)
	}

	// 5. Clicking a TopShelf link for an unavailable/removed entry falls back safely to Entries picker with notice.
	missingURL := topshelf.BuildURL(topshelf.Link{
		Action:  topshelf.ActionDetail,
		EntryID: "removed-plugin",
		Kind:    "video",
		ID:      "x1",
	})
	if !view.OpenTopShelfURL(missingURL) {
		t.Fatal("OpenTopShelfURL(missing) returned false")
	}
	if state.Page != app.Entries {
		t.Fatalf("expected Entries picker fallback, got %s", state.Page)
	}
	if view.notice == "" {
		t.Fatal("expected user-visible notice for unavailable TopShelf entry")
	}
}

type blockingTopShelfStore struct {
	mu      sync.Mutex
	started chan struct{}
	release chan struct{}
	saved   []string
	notifies int
}

func (b *blockingTopShelfStore) Load() (topshelf.Snapshot, error) {
	return topshelf.Snapshot{}, nil
}

func (b *blockingTopShelfStore) Save(s topshelf.Snapshot) error {
	b.mu.Lock()
	started := b.started
	release := b.release
	b.started = nil
	b.release = nil
	b.mu.Unlock()
	if started != nil {
		close(started)
	}
	if release != nil {
		<-release
	}
	b.mu.Lock()
	b.saved = append(b.saved, s.EntryID)
	b.mu.Unlock()
	return nil
}

func (b *blockingTopShelfStore) NotifyChanged() {
	b.mu.Lock()
	b.notifies++
	b.mu.Unlock()
}

func TestSyncTopShelfRunsOffMainThreadAndCoalescesWrites(t *testing.T) {
	assets, err := LoadAssets()
	if err != nil {
		t.Fatal(err)
	}
	pages, err := NewPages(assets)
	if err != nil {
		t.Fatal(err)
	}
	state, err := app.NewState(testEntries(), "test-source", pages.Routes)
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	store := &blockingTopShelfStore{started: started, release: release}
	view := newTestView(t, Options{State: state, Pages: pages, Assets: assets, Theme: theme.Default(), TopShelf: store})

	t0 := time.Now()
	view.publishTopShelf(topshelf.Snapshot{EntryID: "first"})
	if elapsed := time.Since(t0); elapsed > 100*time.Millisecond {
		t.Fatalf("publishTopShelf blocked caller for %s", elapsed)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("background TopShelf writer did not start")
	}

	// Enqueue multiple updates while first save is blocked; only the latest should land next.
	view.publishTopShelf(topshelf.Snapshot{EntryID: "second"})
	view.publishTopShelf(topshelf.Snapshot{EntryID: "third"})
	close(release)
	view.flushTopShelf()

	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.saved) != 2 || store.saved[0] != "first" || store.saved[1] != "third" {
		t.Fatalf("saved order = %v, want [first third]", store.saved)
	}
	if store.notifies != 2 {
		t.Fatalf("notifies = %d, want 2", store.notifies)
	}
}
