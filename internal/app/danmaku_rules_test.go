package app

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/yuchanns/MikuTV/internal/provider"
)

type memoryDanmakuStore struct {
	mu      sync.Mutex
	hasDisp bool
	display DanmakuDisplay
	rules   map[string][]provider.DanmakuRule
}

func newMemoryDanmakuStore() *memoryDanmakuStore {
	return &memoryDanmakuStore{rules: make(map[string][]provider.DanmakuRule)}
}

func (m *memoryDanmakuStore) DanmakuDisplay() (DanmakuDisplay, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.display, m.hasDisp
}

func (m *memoryDanmakuStore) SaveDanmakuDisplay(d DanmakuDisplay) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.display, m.hasDisp = d, true
	return nil
}

func (m *memoryDanmakuStore) DanmakuRules(entryID, owner string) []provider.DanmakuRule {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]provider.DanmakuRule(nil), m.rules[entryID+"\x00"+owner]...)
}

func (m *memoryDanmakuStore) SaveDanmakuRules(entryID, owner string, rules []provider.DanmakuRule) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rules[entryID+"\x00"+owner] = append([]provider.DanmakuRule(nil), rules...)
	return nil
}

type fakeDanmakuRulesSession struct {
	mu      sync.Mutex
	rules   []provider.DanmakuRule
	nextID  int
	added   []provider.DanmakuRuleDraft
	removed []provider.DanmakuRuleRemoval
}

func (s *fakeDanmakuRulesSession) Browse(context.Context, provider.BrowseRequest) (provider.CollectionPage, error) {
	return provider.CollectionPage{}, nil
}

func (s *fakeDanmakuRulesSession) Item(context.Context, provider.MediaRef) (provider.MediaItem, error) {
	return provider.MediaItem{}, nil
}

func (s *fakeDanmakuRulesSession) Close() {}

func (s *fakeDanmakuRulesSession) DanmakuRules(context.Context) ([]provider.DanmakuRule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]provider.DanmakuRule(nil), s.rules...), nil
}

func (s *fakeDanmakuRulesSession) AddDanmakuRule(_ context.Context, draft provider.DanmakuRuleDraft) (provider.DanmakuRule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	s.added = append(s.added, draft)
	rule := provider.DanmakuRule{
		ID:       "remote:" + string(rune('a'+s.nextID)),
		RemoteID: "r-" + draft.Pattern,
		Kind:     draft.Kind,
		Pattern:  draft.Pattern,
		Enabled:  true,
		Synced:   true,
	}
	s.rules = append(s.rules, rule)
	return rule, nil
}

func (s *fakeDanmakuRulesSession) RemoveDanmakuRule(_ context.Context, removal provider.DanmakuRuleRemoval) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.removed = append(s.removed, removal)
	return nil
}

func TestDanmakuRulesPersistenceCloudSyncScopeAndMatching(t *testing.T) {
	store := newMemoryDanmakuStore()
	queue := make(chan func(), 32)
	post := func(fn func()) { queue <- fn }
	drain := func() {
		select {
		case fn := <-queue:
			fn()
		case <-time.After(3 * time.Second):
			t.Fatal("timed out waiting for async callback")
		}
		for len(queue) > 0 {
			(<-queue)()
		}
	}

	uidHash := DanmakuUserHash("1024386")
	sess := &fakeDanmakuRulesSession{
		rules: []provider.DanmakuRule{
			{RemoteID: "1", Kind: provider.DanmakuRuleKeyword, Pattern: "Spoiler", Enabled: true, Synced: true},
			{RemoteID: "2", Kind: provider.DanmakuRuleUser, Pattern: uidHash, Enabled: true, Synced: true},
		},
	}
	catalog := NewCatalog(func(string) provider.Session { return sess }, post, func() {})
	defer catalog.Close()
	caps := provider.Capabilities{provider.BrowseCatalog, provider.ReadDanmaku, provider.SignIn, provider.DanmakuRules}
	catalog.Open(Entry{ID: "sample-source", SourceID: "sample-source", Capabilities: caps})

	mgr := NewDanmakuRules(store, post, func() {})
	defer mgr.Close()

	// 1. Display preference persistence.
	if err := mgr.UpdateDisplay(func(d *DanmakuDisplay) {
		d.Density = 30
		d.Area = DanmakuAreaTop
		d.Opacity = 50
		d.FontSize = DanmakuFontLarge
	}); err != nil {
		t.Fatal(err)
	}
	reloaded := NewDanmakuRules(store, post, func() {})
	defer reloaded.Close()
	if d := reloaded.Display(); d.Density != 30 || d.Area != DanmakuAreaTop || d.Opacity != 50 || d.FontSize != DanmakuFontLarge {
		t.Fatalf("expected persisted display settings, got %+v", d)
	}

	// 2. Guest scope saves locally without touching cloud.
	mgr.SetScope("sample-source", caps, "", catalog)
	var addErr error
	mgr.Add(provider.DanmakuRuleKeyword, "guest-word", func(err error) { addErr = err })
	if addErr != nil {
		t.Fatalf("unexpected guest add err: %v", addErr)
	}
	if !mgr.Blocks("this is a GUEST-WORD line", "", "") {
		t.Fatal("expected case-insensitive keyword match")
	}
	if len(sess.added) != 0 {
		t.Fatalf("guest add should not call cloud, got %+v", sess.added)
	}

	// Validation checks.
	if _, err := mgr.ValidateDraft(provider.DanmakuRuleKeyword, "   "); !errors.Is(err, ErrDanmakuRuleEmpty) {
		t.Fatalf("expected empty error, got %v", err)
	}
	if _, err := mgr.ValidateDraft(provider.DanmakuRuleUser, "12a3"); !errors.Is(err, ErrDanmakuRuleInvalidUser) {
		t.Fatalf("expected invalid user error, got %v", err)
	}
	if _, err := mgr.ValidateDraft(provider.DanmakuRuleKeyword, "GUEST-WORD"); !errors.Is(err, ErrDanmakuRuleDuplicate) {
		t.Fatalf("expected duplicate error, got %v", err)
	}

	// 3. Switch to signed-in account "u1": guest rule must NOT leak; cloud sync fetches account rules.
	mgr.SetScope("sample-source", caps, "u1", catalog)
	if mgr.Blocks("guest-word", "", "") {
		t.Fatal("guest rule leaked into signed-in account u1")
	}
	mgr.EnsureSynced()
	drain()
	snap := mgr.Snapshot()
	if snap.SyncState != DanmakuRulesSynced || len(snap.Rules) != 2 {
		t.Fatalf("expected 2 synced cloud rules, got state=%v rules=%+v", snap.SyncState, snap.Rules)
	}
	// Cloud user rule stores CRC32 hash; adding same UID "1024386" must detect duplicate!
	if _, err := mgr.ValidateDraft(provider.DanmakuRuleUser, "1024386"); !errors.Is(err, ErrDanmakuRuleDuplicate) {
		t.Fatalf("expected UID matching cloud hash to be detected as duplicate, got %v", err)
	}
	// Matching works for BOTH VOD CRC32 hash (padded/unpadded) AND live senderID!
	if !mgr.Blocks("clean text", "", "0"+uidHash) {
		t.Fatal("expected user hash match with leading zero normalization")
	}
	if !mgr.Blocks("spoiler ahead", "", "") {
		t.Fatal("expected cloud keyword match")
	}

	// Toggle off keyword rule -> stops blocking and persists local disabled override across re-sync!
	keywordID := snap.Rules[0].ID
	if err := mgr.Toggle(keywordID); err != nil {
		t.Fatal(err)
	}
	if mgr.Blocks("spoiler ahead", "", "") {
		t.Fatal("disabled rule should not block")
	}
	mgr.Sync()
	drain()
	if mgr.Blocks("spoiler ahead", "", "") {
		t.Fatal("local disabled override should survive cloud re-sync")
	}

	// Cloud add + remove.
	var cloudAddErr error
	mgr.Add(provider.DanmakuRuleUser, "998877", func(err error) { cloudAddErr = err })
	drain()
	if cloudAddErr != nil || !mgr.Blocks("hi", "998877", "") || !mgr.Blocks("hi", "", DanmakuUserHash("998877")) {
		t.Fatalf("expected added user rule 998877 to block by senderID and hash, err=%v", cloudAddErr)
	}
	var targetID string
	for _, r := range mgr.Snapshot().Rules {
		if r.Pattern == "998877" {
			targetID = r.ID
		}
	}
	var rmErr error
	mgr.Remove(targetID, func(err error) { rmErr = err })
	drain()
	if rmErr != nil || mgr.Blocks("hi", "998877", "") || len(sess.removed) != 1 {
		t.Fatalf("expected cloud rule removal to succeed, err=%v removed=%+v", rmErr, sess.removed)
	}
}
