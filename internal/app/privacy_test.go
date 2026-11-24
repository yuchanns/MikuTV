package app

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/yuchanns/MikuTV/internal/player"
	"github.com/yuchanns/MikuTV/internal/provider"
)

type privacyTestStore struct {
	mu          sync.Mutex
	searches    map[string][]string
	positions   map[string]map[provider.PlaybackTarget]float64
	sync        map[string]bool
	failNext    error
	saveStarted chan struct{}
	blockSave   chan struct{}
}

func newPrivacyTestStore() *privacyTestStore {
	return &privacyTestStore{
		searches:  make(map[string][]string),
		positions: make(map[string]map[provider.PlaybackTarget]float64),
		sync:      make(map[string]bool),
	}
}

func privacyKey(entryID, owner string) string { return entryID + "\x00" + owner }

func (s *privacyTestStore) SearchHistory(entryID, owner string) []string {
	return slices.Clone(s.searches[privacyKey(entryID, owner)])
}

func (s *privacyTestStore) SaveSearchHistory(entryID, owner string, queries []string) error {
	if s.failNext != nil {
		err := s.failNext
		s.failNext = nil
		return err
	}
	if len(queries) == 0 {
		delete(s.searches, privacyKey(entryID, owner))
		return nil
	}
	s.searches[privacyKey(entryID, owner)] = slices.Clone(queries)
	return nil
}

func (s *privacyTestStore) PlaybackPosition(owner string, target provider.PlaybackTarget) (float64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	bucket := s.positions[privacyKey(target.Ref.EntryID, owner)]
	if bucket == nil {
		return 0, false
	}
	sec, ok := bucket[target]
	return sec, ok
}

func (s *privacyTestStore) SavePlaybackPosition(owner string, target provider.PlaybackTarget, seconds float64) error {
	s.mu.Lock()
	started := s.saveStarted
	block := s.blockSave
	s.saveStarted = nil
	s.blockSave = nil
	s.mu.Unlock()
	if started != nil {
		close(started)
	}
	if block != nil {
		<-block
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := privacyKey(target.Ref.EntryID, owner)
	if s.positions[key] == nil {
		s.positions[key] = make(map[provider.PlaybackTarget]float64)
	}
	s.positions[key][target] = seconds
	return nil
}

func (s *privacyTestStore) ClearPlaybackPositions(entryID, owner string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failNext != nil {
		err := s.failNext
		s.failNext = nil
		return err
	}
	delete(s.positions, privacyKey(entryID, owner))
	return nil
}

func (s *privacyTestStore) HistorySync(entry, owner string) bool {
	if v, ok := s.sync[privacyKey(entry, owner)]; ok {
		return v
	}
	return true
}

func (s *privacyTestStore) SaveHistorySync(entry, owner string, enabled bool) error {
	s.sync[privacyKey(entry, owner)] = enabled
	return nil
}

type privacyAccountReader struct {
	rows []provider.AccountRow
}

func (r *privacyAccountReader) ReadAccount(context.Context, provider.AccountRequest) (provider.AccountPage, error) {
	return provider.AccountPage{Rows: slices.Clone(r.rows)}, nil
}

func TestClearLocalSearchHistoryIsolatesEntryAccountAndPropagatesErrors(t *testing.T) {
	store := newPrivacyTestStore()
	_ = store.SaveSearchHistory("entry-a", "user-1", []string{"miku", "vocaloid"})
	_ = store.SaveSearchHistory("entry-a", "user-2", []string{"user2-query"})
	_ = store.SaveSearchHistory("entry-a", "", []string{"guest-query"})
	_ = store.SaveSearchHistory("entry-b", "user-1", []string{"entryb-query"})

	targetA1 := provider.PlaybackTarget{Ref: provider.MediaRef{EntryID: "entry-a", Kind: provider.Video, ID: "v1"}, PartID: "p1"}
	_ = store.SavePlaybackPosition("user-1", targetA1, 42)

	search := NewCatalogSearch(store, func(fn func()) { fn() }, func() {})
	entryA := Entry{ID: "entry-a", Capabilities: provider.Capabilities{provider.SearchCatalog}}
	search.Bind(entryA, "user-1", nil, nil)
	if got := search.Snapshot().History; len(got) != 2 {
		t.Fatalf("expected 2 loaded history items, got %v", got)
	}

	// Persistence error must NOT falsely wipe in-memory history.
	store.failNext = errors.New("disk full")
	if err := ClearLocalSearchHistory(store, search, "entry-a", "user-1"); err == nil {
		t.Fatal("expected persistence error")
	}
	if got := search.Snapshot().History; len(got) != 2 {
		t.Fatalf("in-memory history wiped despite store failure: %v", got)
	}

	// Clearing (entry-a, user-1) must ONLY clear that bucket.
	if err := ClearLocalSearchHistory(store, search, "entry-a", "user-1"); err != nil {
		t.Fatalf("ClearLocalSearchHistory: %v", err)
	}
	if got := search.Snapshot().History; len(got) != 0 {
		t.Fatalf("expected in-memory history empty, got %v", got)
	}
	if got := store.SearchHistory("entry-a", "user-1"); len(got) != 0 {
		t.Fatalf("expected (entry-a, user-1) cleared, got %v", got)
	}
	if got := store.SearchHistory("entry-a", "user-2"); !slices.Equal(got, []string{"user2-query"}) {
		t.Fatalf("user-2 search history damaged: %v", got)
	}
	if got := store.SearchHistory("entry-a", ""); !slices.Equal(got, []string{"guest-query"}) {
		t.Fatalf("guest search history damaged: %v", got)
	}
	if got := store.SearchHistory("entry-b", "user-1"); !slices.Equal(got, []string{"entryb-query"}) {
		t.Fatalf("entry-b search history damaged: %v", got)
	}
	if sec, ok := store.PlaybackPosition("user-1", targetA1); !ok || sec != 42 {
		t.Fatalf("playback position damaged by search clear: (%v, %v)", sec, ok)
	}

	// Clearing guest ("") must ONLY clear guest bucket.
	if err := ClearLocalSearchHistory(store, search, "entry-a", ""); err != nil {
		t.Fatalf("ClearLocalSearchHistory guest: %v", err)
	}
	if got := store.SearchHistory("entry-a", ""); len(got) != 0 {
		t.Fatalf("expected guest history cleared, got %v", got)
	}
	if got := store.SearchHistory("entry-a", "user-2"); !slices.Equal(got, []string{"user2-query"}) {
		t.Fatalf("user-2 damaged by guest clear: %v", got)
	}
}

func TestClearLocalWatchHistoryIsolatesScopesClearsCachesAndPreservesOtherData(t *testing.T) {
	store := newPrivacyTestStore()
	targetA1 := provider.PlaybackTarget{Ref: provider.MediaRef{EntryID: "entry-a", Kind: provider.Video, ID: "v1"}, PartID: "p1"}
	targetA2 := provider.PlaybackTarget{Ref: provider.MediaRef{EntryID: "entry-a", Kind: provider.Video, ID: "v2"}, PartID: "p1"}
	targetGuest := provider.PlaybackTarget{Ref: provider.MediaRef{EntryID: "entry-a", Kind: provider.Video, ID: "vg"}, PartID: "p1"}
	targetB1 := provider.PlaybackTarget{Ref: provider.MediaRef{EntryID: "entry-b", Kind: provider.Video, ID: "vb"}, PartID: "p1"}
	_ = store.SavePlaybackPosition("user-1", targetA1, 120)
	_ = store.SavePlaybackPosition("user-2", targetA2, 88)
	_ = store.SavePlaybackPosition("", targetGuest, 33)
	_ = store.SavePlaybackPosition("user-1", targetB1, 99)
	_ = store.SaveSearchHistory("entry-a", "user-1", []string{"keep-me"})

	queue := make(chan func(), 16)
	reader := &privacyAccountReader{rows: []provider.AccountRow{{ID: "h1", Title: "History Row"}}}
	activity := NewAccountActivity(store, func(fn func()) { queue <- fn }, func() {})
	entryA := Entry{ID: "entry-a", Capabilities: provider.Capabilities{provider.AccountHistory.Capability()}}
	activity.Bind(entryA, "user-1", reader)
	activity.Ensure(provider.AccountQuery{Section: provider.AccountHistory})
	(<-queue)()
	if snap := activity.Snapshot(provider.AccountQuery{Section: provider.AccountHistory}); snap.Phase != CatalogReady || len(snap.Page.Rows) != 1 {
		t.Fatalf("expected cached history row, got %+v", snap)
	}

	pb := NewPlayback(&Catalog{}, player.Engine{}, store, func(fn func()) { fn() }, func() {})
	pb.preloaded = &preloadedPlayback{target: targetA1, owner: "user-1"}

	// Failure must leave caches and persisted positions untouched.
	store.failNext = errors.New("defaults error")
	if err := ClearLocalWatchHistory(store, activity, pb, "entry-a", "user-1"); err == nil {
		t.Fatal("expected clear error")
	}
	if snap := activity.Snapshot(provider.AccountQuery{Section: provider.AccountHistory}); snap.Phase != CatalogReady {
		t.Fatalf("activity cache cleared despite store failure: %+v", snap)
	}
	if pb.preloaded == nil {
		t.Fatal("preloaded playback cleared despite store failure")
	}

	// Succeeding clear removes ONLY (entry-a, user-1) positions + matching caches.
	if err := ClearLocalWatchHistory(store, activity, pb, "entry-a", "user-1"); err != nil {
		t.Fatalf("ClearLocalWatchHistory: %v", err)
	}
	if _, ok := store.PlaybackPosition("user-1", targetA1); ok {
		t.Fatal("(entry-a, user-1) position survived clear")
	}
	if snap := activity.Snapshot(provider.AccountQuery{Section: provider.AccountHistory}); snap.Phase != CatalogIdle || len(snap.Page.Rows) != 0 {
		t.Fatalf("activity history cache not cleared: %+v", snap)
	}
	if pb.preloaded != nil {
		t.Fatal("matching preloaded playback not dropped")
	}
	if sec, ok := store.PlaybackPosition("user-2", targetA2); !ok || sec != 88 {
		t.Fatalf("(entry-a, user-2) position damaged: (%v, %v)", sec, ok)
	}
	if sec, ok := store.PlaybackPosition("", targetGuest); !ok || sec != 33 {
		t.Fatalf("(entry-a, guest) position damaged: (%v, %v)", sec, ok)
	}
	if sec, ok := store.PlaybackPosition("user-1", targetB1); !ok || sec != 99 {
		t.Fatalf("(entry-b, user-1) position damaged: (%v, %v)", sec, ok)
	}
	if got := store.SearchHistory("entry-a", "user-1"); !slices.Equal(got, []string{"keep-me"}) {
		t.Fatalf("search history damaged by watch clear: %v", got)
	}
	if !activity.HistorySync() {
		t.Fatal("history sync preference unexpectedly toggled off")
	}
}

func TestClearLocalWatchHistoryDrainsInflightPositionSaveBeforeClearing(t *testing.T) {
	store := newPrivacyTestStore()
	target := provider.PlaybackTarget{Ref: provider.MediaRef{EntryID: "entry-a", Kind: provider.Video, ID: "v1"}, PartID: "p1"}
	otherTarget := provider.PlaybackTarget{Ref: provider.MediaRef{EntryID: "entry-b", Kind: provider.Video, ID: "v2"}, PartID: "p1"}
	_ = store.SavePlaybackPosition("user-1", otherTarget, 77)
	saveStarted := make(chan struct{})
	blockSave := make(chan struct{})
	store.mu.Lock()
	store.saveStarted = saveStarted
	store.blockSave = blockSave
	store.mu.Unlock()

	pb := NewPlayback(&Catalog{}, player.Engine{}, store, func(fn func()) { fn() }, func() {})
	pb.loaded = true
	pb.active = true
	pb.owner = "user-1"
	pb.state.Target = target
	pb.state.Position = 123
	pb.saveAsync()

	select {
	case <-saveStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("in-flight periodic save did not start")
	}

	clearDone := make(chan error, 1)
	go func() {
		clearDone <- ClearLocalWatchHistory(store, nil, pb, "entry-a", "user-1")
	}()

	// Clear must wait for the in-flight save rather than clearing before it lands.
	select {
	case err := <-clearDone:
		t.Fatalf("ClearLocalWatchHistory returned before in-flight save finished: %v", err)
	case <-time.After(30 * time.Millisecond):
	}

	close(blockSave)

	select {
	case err := <-clearDone:
		if err != nil {
			t.Fatalf("ClearLocalWatchHistory: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ClearLocalWatchHistory did not complete after releasing in-flight save")
	}

	if sec, ok := store.PlaybackPosition("user-1", target); ok {
		t.Fatalf("in-flight periodic save resurrected cleared resume position: sec=%v", sec)
	}
	if sec, ok := store.PlaybackPosition("user-1", otherTarget); !ok || sec != 77 {
		t.Fatalf("unrelated scope position damaged: (%v, %v)", sec, ok)
	}
}
