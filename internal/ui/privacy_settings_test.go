package tvui

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/provider"
	"github.com/yuchanns/MikuTV/internal/theme"
)

type uiPrivacyPrefsStore struct {
	syncDisabled map[string]bool
	searches     map[string][]string
	positions    map[string]map[provider.PlaybackTarget]float64
}

func newUIPrivacyPrefsStore() *uiPrivacyPrefsStore {
	return &uiPrivacyPrefsStore{
		syncDisabled: make(map[string]bool),
		searches:     make(map[string][]string),
		positions:    make(map[string]map[provider.PlaybackTarget]float64),
	}
}

func uiPrivacyKey(entryID, owner string) string { return entryID + "\x00" + owner }

func (s *uiPrivacyPrefsStore) Save(app.Preferences) error { return nil }

func (s *uiPrivacyPrefsStore) HistorySync(entry, owner string) bool {
	return !s.syncDisabled[uiPrivacyKey(entry, owner)]
}

func (s *uiPrivacyPrefsStore) SaveHistorySync(entry, owner string, enabled bool) error {
	s.syncDisabled[uiPrivacyKey(entry, owner)] = !enabled
	return nil
}

func (s *uiPrivacyPrefsStore) SearchHistory(entryID, owner string) []string {
	return slices.Clone(s.searches[uiPrivacyKey(entryID, owner)])
}

func (s *uiPrivacyPrefsStore) SaveSearchHistory(entryID, owner string, queries []string) error {
	if len(queries) == 0 {
		delete(s.searches, uiPrivacyKey(entryID, owner))
		return nil
	}
	s.searches[uiPrivacyKey(entryID, owner)] = slices.Clone(queries)
	return nil
}

func (s *uiPrivacyPrefsStore) PlaybackPosition(owner string, target provider.PlaybackTarget) (float64, bool) {
	bucket := s.positions[uiPrivacyKey(target.Ref.EntryID, owner)]
	if bucket == nil {
		return 0, false
	}
	sec, ok := bucket[target]
	return sec, ok
}

func (s *uiPrivacyPrefsStore) SavePlaybackPosition(owner string, target provider.PlaybackTarget, seconds float64) error {
	key := uiPrivacyKey(target.Ref.EntryID, owner)
	if s.positions[key] == nil {
		s.positions[key] = make(map[provider.PlaybackTarget]float64)
	}
	s.positions[key][target] = seconds
	return nil
}

func (s *uiPrivacyPrefsStore) ClearPlaybackPositions(entryID, owner string) error {
	delete(s.positions, uiPrivacyKey(entryID, owner))
	return nil
}

type uiPrivacySession struct {
	uiAuthSource
}

func (s *uiPrivacySession) Browse(context.Context, provider.BrowseRequest) (provider.CollectionPage, error) {
	return provider.CollectionPage{}, nil
}
func (s *uiPrivacySession) Close() {}
func (s *uiPrivacySession) ReadAccount(context.Context, provider.AccountRequest) (provider.AccountPage, error) {
	return provider.AccountPage{Rows: []provider.AccountRow{{ID: "row-1", Title: "Sample History"}}}, nil
}

func TestPrivacySettingsClearConfirmIsolationGuestAndAboutDetails(t *testing.T) {
	assets, err := LoadAssets()
	if err != nil {
		t.Fatal(err)
	}
	pages, err := NewPages(assets)
	if err != nil {
		t.Fatal(err)
	}
	entryFull := app.Entry{
		ID: "entry-a", SourceID: "entry-a", Name: "Source A",
		Capabilities: provider.Capabilities{
			provider.BrowseCatalog, provider.SignIn, provider.AccountHistory.Capability(),
			provider.SearchCatalog, provider.ResolvePlayback,
		},
	}
	entryMinimal := app.Entry{
		ID: "entry-minimal", SourceID: "entry-minimal", Name: "Minimal Source",
		Capabilities: provider.Capabilities{provider.BrowseCatalog},
	}
	state, err := app.NewState([]app.Entry{entryFull, entryMinimal}, entryFull.ID, pages.Routes)
	if err != nil {
		t.Fatal(err)
	}
	store := newUIPrivacyPrefsStore()
	_ = store.SaveSearchHistory("entry-a", "user-1", []string{"alpha", "beta"})
	_ = store.SaveSearchHistory("entry-a", "user-2", []string{"other-account"})
	_ = store.SaveSearchHistory("entry-a", "", []string{"guest-term"})
	_ = store.SaveSearchHistory("entry-minimal", "user-1", []string{"other-entry"})

	targetA1 := provider.PlaybackTarget{Ref: provider.MediaRef{EntryID: "entry-a", Kind: provider.Video, ID: "v1"}, PartID: "p1"}
	targetA2 := provider.PlaybackTarget{Ref: provider.MediaRef{EntryID: "entry-a", Kind: provider.Video, ID: "v2"}, PartID: "p1"}
	targetGuest := provider.PlaybackTarget{Ref: provider.MediaRef{EntryID: "entry-a", Kind: provider.Video, ID: "vg"}, PartID: "p1"}
	_ = store.SavePlaybackPosition("user-1", targetA1, 55)
	_ = store.SavePlaybackPosition("user-2", targetA2, 77)
	_ = store.SavePlaybackPosition("", targetGuest, 21)

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

	sess := &uiPrivacySession{}
	catalog := app.NewCatalog(func(string) provider.Session { return sess }, post, func() {})
	t.Cleanup(catalog.Close)
	creds := uiTestKeychain{data: []byte(`{"selected":"user-1","accounts":[{"account":{"id":"user-1","name":"Alice"},"data":"e30="}]}`)}
	auth := app.NewAuth(creds, post, func() {}, func() {}, func() {})
	t.Cleanup(auth.Close)
	activity := app.NewAccountActivity(store, post, func() {})
	t.Cleanup(activity.Close)
	search := app.NewCatalogSearch(store, post, func() {})
	t.Cleanup(search.Close)

	view := newTestView(t, Options{
		State: state, Catalog: catalog, Auth: auth, Activity: activity, Search: search,
		Pages: pages, Assets: assets, Preferences: store, Theme: theme.Default(),
	})
	view.SyncEntry()
	for auth.Accounts().CurrentID != "user-1" {
		select {
		case fn := <-queue:
			fn()
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for auth restore")
		}
	}
	view.SyncAccountActivity()
	drain()

	tester := ui.NewTester(view.Render, 1920, 1080)

	// 1. Open About -> Privacy & Data modal -> click "管理隐私设置" -> navigates to Settings/privacy focused on HistorySync.
	state.Navigate(app.Route{Page: app.Settings, Section: "about"})
	tester.Frame()
	if view.focus.item(app.ShowPrivacyDetails) == nil {
		t.Fatal("expected Privacy & Data button on About page")
	}
	state.Focus = app.ShowPrivacyDetails
	view.reveal = true
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	if state.Page != app.PrivacyDetails || state.Focus != privacyManageAction {
		t.Fatalf("expected PrivacyDetails focused on manage action, got page=%s focus=%q", state.Page, state.Focus)
	}
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	if state.Page != app.Settings || state.Section != "privacy" || state.Focus != app.HistorySync {
		t.Fatalf("expected navigation to settings/privacy focused on HistorySync, got page=%s section=%s focus=%q", state.Page, state.Section, state.Focus)
	}

	// 2. Toggle HistorySync OFF and verify persistence.
	if !activity.HistorySync() {
		t.Fatal("expected HistorySync initially ON")
	}
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	if activity.HistorySync() || store.HistorySync("entry-a", "user-1") {
		t.Fatal("expected HistorySync toggled OFF and saved")
	}

	// 3. Click "清除搜索记录" -> opens PrivacyConfirm focused on Cancel -> Cancel keeps data intact.
	state.Focus = privacyClearSearch
	view.reveal = true
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	if state.Page != app.PrivacyConfirm || state.Focus != app.CloseModal {
		t.Fatalf("expected PrivacyConfirm focused on CloseModal (Cancel), got page=%s focus=%q", state.Page, state.Focus)
	}
	tester.Key(0, ui.KeyEnter) // Cancel
	tester.Frame()
	if state.Page != app.Settings || len(store.SearchHistory("entry-a", "user-1")) != 2 {
		t.Fatalf("cancel should preserve search history, got page=%s history=%v", state.Page, store.SearchHistory("entry-a", "user-1"))
	}

	// 4. Re-open "清除搜索记录" -> Confirm clears ONLY (entry-a, user-1) search history + shows toast.
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	state.Focus = privacyConfirmAccept
	view.reveal = true
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	if state.Page != app.Settings || state.Focus != privacyClearSearch {
		t.Fatalf("expected return to settings/privacy focus=%q, got page=%s focus=%q", privacyClearSearch, state.Page, state.Focus)
	}
	if len(store.SearchHistory("entry-a", "user-1")) != 0 || len(search.Snapshot().History) != 0 {
		t.Fatalf("expected (entry-a, user-1) search cleared, store=%v mem=%v", store.SearchHistory("entry-a", "user-1"), search.Snapshot().History)
	}
	if !slices.Equal(store.SearchHistory("entry-a", "user-2"), []string{"other-account"}) ||
		!slices.Equal(store.SearchHistory("entry-a", ""), []string{"guest-term"}) ||
		!slices.Equal(store.SearchHistory("entry-minimal", "user-1"), []string{"other-entry"}) {
		t.Fatal("clearing user-1 search history affected another entry or account")
	}
	if view.settingsFeedback.text != assets.Text.Privacy.Cleared {
		t.Fatalf("expected Cleared toast, got %q", view.settingsFeedback.text)
	}

	// 5. Click "清除观看记录" -> Confirm clears ONLY (entry-a, user-1) local playback positions.
	state.Focus = privacyClearHistory
	view.reveal = true
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	state.Focus = privacyConfirmAccept
	view.reveal = true
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	if _, ok := store.PlaybackPosition("user-1", targetA1); ok {
		t.Fatal("expected (entry-a, user-1) playback position cleared")
	}
	if sec, ok := store.PlaybackPosition("user-2", targetA2); !ok || sec != 77 {
		t.Fatalf("(entry-a, user-2) position damaged: (%v, %v)", sec, ok)
	}
	if sec, ok := store.PlaybackPosition("", targetGuest); !ok || sec != 21 {
		t.Fatalf("(entry-a, guest) position damaged: (%v, %v)", sec, ok)
	}

	// 6. Mid-confirm entry switch auto-dismisses PrivacyConfirm so stale confirm cannot fire on wrong entry.
	state.Focus = privacyClearHistory
	view.reveal = true
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	if state.Page != app.PrivacyConfirm {
		t.Fatalf("expected PrivacyConfirm open, got %s", state.Page)
	}
	state.SelectEntry(entryMinimal.ID)
	view.SyncEntry()
	drain()
	tester.Frame()
	if state.Page == app.PrivacyConfirm {
		t.Fatal("expected PrivacyConfirm auto-dismissed on entry switch")
	}

	// 7. Minimal source without AccountHistory/SearchCatalog/ResolvePlayback hides all unavailable controls and falls back to empty state.
	state.Navigate(app.Route{Page: app.Settings, Section: "privacy"})
	tester.Frame()
	if view.focus.item(app.HistorySync) != nil || view.focus.item(privacyClearSearch) != nil || view.focus.item(privacyClearHistory) != nil {
		t.Fatal("minimal source without privacy capabilities should hide all privacy controls")
	}

	// 8. Guest mode on entryFull hides HistorySync, shows ClearSearch + ClearHistory, clears ONLY guest bucket.
	state.SelectEntry(entryFull.ID)
	auth.SignOut("user-1")
	drain()
	view.SyncEntry()
	drain()
	state.Navigate(app.Route{Page: app.Settings, Section: "privacy"})
	tester.Frame()
	if view.focus.item(app.HistorySync) != nil {
		t.Fatal("guest mode should hide HistorySync toggle")
	}
	if view.focus.item(privacyClearSearch) == nil || view.focus.item(privacyClearHistory) == nil {
		t.Fatal("guest mode should show ClearSearch and ClearHistory")
	}
	state.Focus = privacyClearSearch
	view.reveal = true
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	state.Focus = privacyConfirmAccept
	view.reveal = true
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	if len(store.SearchHistory("entry-a", "")) != 0 {
		t.Fatal("guest search history not cleared")
	}
	if !slices.Equal(store.SearchHistory("entry-a", "user-2"), []string{"other-account"}) {
		t.Fatal("guest clear damaged user-2 search history")
	}
}
