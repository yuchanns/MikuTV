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

type uiDanmakuStore struct {
	mu      sync.Mutex
	hasDisp bool
	display app.DanmakuDisplay
	rules   map[string][]provider.DanmakuRule
}

func newUIDanmakuStore() *uiDanmakuStore {
	return &uiDanmakuStore{rules: make(map[string][]provider.DanmakuRule)}
}

func (s *uiDanmakuStore) Save(app.Preferences) error { return nil }
func (s *uiDanmakuStore) DanmakuDisplay() (app.DanmakuDisplay, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.display, s.hasDisp
}
func (s *uiDanmakuStore) SaveDanmakuDisplay(d app.DanmakuDisplay) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.display, s.hasDisp = d, true
	return nil
}
func (s *uiDanmakuStore) DanmakuRules(entryID, owner string) []provider.DanmakuRule {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]provider.DanmakuRule(nil), s.rules[entryID+"\x00"+owner]...)
}
func (s *uiDanmakuStore) SaveDanmakuRules(entryID, owner string, rules []provider.DanmakuRule) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rules[entryID+"\x00"+owner] = append([]provider.DanmakuRule(nil), rules...)
	return nil
}

type uiDanmakuSession struct {
	uiAuthSource
	rules []provider.DanmakuRule
}

func (s *uiDanmakuSession) Browse(context.Context, provider.BrowseRequest) (provider.CollectionPage, error) {
	return provider.CollectionPage{}, nil
}
func (s *uiDanmakuSession) Close() {}
func (s *uiDanmakuSession) DanmakuRules(context.Context) ([]provider.DanmakuRule, error) {
	return append([]provider.DanmakuRule(nil), s.rules...), nil
}
func (s *uiDanmakuSession) AddDanmakuRule(_ context.Context, draft provider.DanmakuRuleDraft) (provider.DanmakuRule, error) {
	r := provider.DanmakuRule{ID: "remote:" + draft.Pattern, RemoteID: "r-" + draft.Pattern, Kind: draft.Kind, Pattern: draft.Pattern, Enabled: true, Synced: true}
	s.rules = append(s.rules, r)
	return r, nil
}
func (s *uiDanmakuSession) RemoveDanmakuRule(_ context.Context, _ provider.DanmakuRuleRemoval) error {
	return nil
}

func TestDanmakuSettingsRulesEditorFilteringAndCapabilityHiding(t *testing.T) {
	assets, err := LoadAssets()
	if err != nil {
		t.Fatal(err)
	}
	pages, err := NewPages(assets)
	if err != nil {
		t.Fatal(err)
	}

	fullEntry := app.Entry{
		ID: "sample-source", SourceID: "sample-source", Name: "SampleSource",
		Capabilities: provider.Capabilities{provider.BrowseCatalog, provider.ReadDanmaku, provider.SignIn, provider.DanmakuRules},
	}
	noDanmakuEntry := app.Entry{
		ID: "plain", SourceID: "plain", Name: "Plain",
		Capabilities: provider.Capabilities{provider.BrowseCatalog},
	}
	state, err := app.NewState([]app.Entry{fullEntry, noDanmakuEntry}, fullEntry.ID, pages.Routes)
	if err != nil {
		t.Fatal(err)
	}

	store := newUIDanmakuStore()
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

	sess := &uiDanmakuSession{}
	catalog := app.NewCatalog(func(string) provider.Session { return sess }, post, func() {})
	t.Cleanup(catalog.Close)
	rulesMgr := app.NewDanmakuRules(store, post, func() {})
	t.Cleanup(rulesMgr.Close)

	view := newTestView(t, Options{
		State: state, Catalog: catalog, DanmakuRules: rulesMgr,
		Pages: pages, Assets: assets, Preferences: store, Theme: theme.Default(),
	})
	view.SyncEntry()
	drain()

	tester := ui.NewTester(view.Render, 1920, 1080)
	state.Navigate(app.Route{Page: app.Settings, Section: "danmaku"})
	view.SyncEntry()
	tester.Frame()

	// 1. Toggle default show danmaku -> persists in store.
	state.Focus = danmakuPrefShow
	view.reveal = true
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	if view.danmakuDisplay().Show {
		t.Fatal("expected default show toggled off")
	}
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	if !view.danmakuDisplay().Show {
		t.Fatal("expected default show toggled back on")
	}

	// 2. Open Area picker (modalDrawer) -> initial focus on current selection (index 0 = upper), select Top (index 2).
	state.Focus = danmakuPrefArea
	view.reveal = true
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	if state.Page != app.DanmakuSettingPicker || state.Focus != danmakuPickerOptionID(0) {
		t.Fatalf("expected Area picker focused on option 0 (upper), got page=%s focus=%q", state.Page, state.Focus)
	}
	state.Focus = danmakuPickerOptionID(2)
	view.reveal = true
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	if state.Page != app.Settings || view.danmakuDisplay().Area != app.DanmakuAreaTop {
		t.Fatalf("expected Area updated to top and returned to Settings, got page=%s area=%q", state.Page, view.danmakuDisplay().Area)
	}
	// Re-open Area picker -> MUST default focus to index 2 (currently selected Top)!
	state.Focus = danmakuPrefArea
	view.reveal = true
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	if state.Focus != danmakuPickerOptionID(2) {
		t.Fatalf("expected reopened Area picker to focus current option 2, got %q", state.Focus)
	}
	tester.Key(0, ui.KeyEscape)
	tester.Frame()

	// 3. Enter Block Rules subview and open Add Rule editor (modalFrameless).
	state.Focus = danmakuPrefBlocklist
	view.reveal = true
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	if !view.danmakuUI.rulesOpen || state.Focus != danmakuRulesAdd {
		t.Fatalf("expected rules subview focused on Add button, got open=%t focus=%q", view.danmakuUI.rulesOpen, state.Focus)
	}
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	if state.Page != app.DanmakuRuleEditor {
		t.Fatalf("expected DanmakuRuleEditor modal, got %s", state.Page)
	}

	// Validation: empty submit -> RuleRequired.
	state.Focus = danmakuEditorSave
	view.reveal = true
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	if view.danmakuUI.draft.failure != assets.Text.Danmaku.RuleRequired {
		t.Fatalf("expected empty validation message, got %q", view.danmakuUI.draft.failure)
	}

	// Validation: user kind with non-digits -> RuleInvalidUser.
	view.danmakuUI.draft.kind = provider.DanmakuRuleUser
	view.danmakuUI.draft.body = "not-a-uid"
	state.Focus = danmakuEditorSave
	view.reveal = true
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	if view.danmakuUI.draft.failure != assets.Text.Danmaku.RuleInvalidUser {
		t.Fatalf("expected invalid user validation message, got %q", view.danmakuUI.draft.failure)
	}

	// Save valid keyword rule -> closes modal and filters VOD danmaku immediately!
	view.danmakuUI.draft.kind = provider.DanmakuRuleKeyword
	view.danmakuUI.draft.body = "spoiler"
	state.Focus = danmakuEditorSave
	view.reveal = true
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	drain()
	tester.Frame()
	if state.Page != app.Settings || len(rulesMgr.Snapshot().Rules) != 1 {
		t.Fatalf("expected saved keyword rule and return to Settings, got page=%s rules=%+v", state.Page, rulesMgr.Snapshot().Rules)
	}

	// Add user UID rule and verify live filtering on scheduleDanmakuWithOptions.
	rulesMgr.Add(provider.DanmakuRuleUser, "1024386", nil)
	snap := rulesMgr.Snapshot()
	scheduled := scheduleDanmakuWithOptions([]provider.Danmaku{
		{Seconds: 1, Mode: provider.DanmakuScroll, Text: "major SPOILER here"},
		{Seconds: 2, Mode: provider.DanmakuScroll, Text: "from blocked uid", SenderHash: app.DanmakuUserHash("1024386")},
		{Seconds: 3, Mode: provider.DanmakuScroll, Text: "safe comment", SenderHash: "deadbeef"},
	}, snap.Display, snap.Rules, 1920)
	if len(scheduled) != 1 || scheduled[0].text != "safe comment" {
		t.Fatalf("expected keyword and UID-hash danmaku filtered out, got %+v", scheduled)
	}

	// Back from rules subview returns to Danmaku settings at blocklist row.
	tester.Key(0, ui.KeyEscape)
	tester.Frame()
	if view.danmakuUI.rulesOpen || state.Focus != danmakuPrefBlocklist {
		t.Fatalf("expected Escape from rules subview to focus blocklist row, got open=%t focus=%q", view.danmakuUI.rulesOpen, state.Focus)
	}

	// 4. Switch to no-danmaku source -> Settings hides the Danmaku sidebar entry and redirects off it instead of showing an empty section.
	state.SelectEntry(noDanmakuEntry.ID)
	view.SyncEntry()
	if view.hasDanmakuSettingsCapability() {
		t.Fatal("expected no-danmaku source to report danmaku settings unavailable")
	}
	// Direct navigation to Settings/danmaku must redirect cleanly to playback without showing EmptySettings.
	state.Navigate(app.Route{Page: app.Settings, Section: "danmaku"})
	tester.Frame()
	if state.Section != "playback" || view.focus.item(app.SectionFocus("danmaku")) != nil {
		t.Fatalf("expected direct navigation to hidden danmaku section to redirect to playback, got section=%q", state.Section)
	}
	if _, found := tester.Find(assets.Text.EmptySettings); found {
		t.Fatal("hidden settings section should not render EmptySettings placeholder")
	}
	// Pressing Down from playback in the settings sidebar skips hidden danmaku and lands directly on appearance.
	tester.Key(0, ui.KeyDown)
	tester.Frame()
	if state.Section != "appearance" || state.Focus != app.SectionFocus("appearance") {
		t.Fatalf("expected Down from playback to skip hidden danmaku and reach appearance, got section=%q focus=%q", state.Section, state.Focus)
	}
	_ = time.Second
}
