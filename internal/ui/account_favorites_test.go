package tvui

import (
	"context"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/provider"
	"github.com/yuchanns/MikuTV/internal/theme"
)

type favoriteTestReader map[string][]provider.AccountRow

func (rows favoriteTestReader) ReadAccount(_ context.Context, request provider.AccountRequest) (provider.AccountPage, error) {
	return provider.AccountPage{Rows: rows[request.Folder]}, nil
}

func TestFavoriteCardsNavigateBetweenFolderAndVideos(t *testing.T) {
	assets, err := LoadAssets()
	if err != nil {
		t.Fatal(err)
	}
	pages, err := NewPages(assets)
	if err != nil {
		t.Fatal(err)
	}
	entries := testEntries()
	entries[0].Capabilities = append(entries[0].Capabilities, provider.BrowseLibrary, provider.AccountFavorites.Capability())
	state, err := app.NewState(entries, entries[0].ID, pages.Routes)
	if err != nil {
		t.Fatal(err)
	}
	folders := []provider.AccountRow{
		{ID: "first", Title: "First folder", Ref: &provider.MediaRef{EntryID: entries[0].ID, Kind: provider.CollectionKind, ID: "first"}},
		{ID: "second", Title: "Second folder", Ref: &provider.MediaRef{EntryID: entries[0].ID, Kind: provider.CollectionKind, ID: "second"}},
	}
	videos := []provider.AccountRow{{ID: "one", Title: "First saved video"}, {ID: "two", Title: "Second saved video"}}
	completed := make(chan func(), 1)
	activity := app.NewAccountActivity(nil, func(fn func()) { completed <- fn }, func() {})
	defer activity.Close()
	activity.Bind(entries[0], "owner", favoriteTestReader{"": folders, "second": videos})
	state.Navigate(libraryRoute(provider.AccountFavorites))
	activity.Ensure(provider.AccountQuery{Section: provider.AccountFavorites})
	(<-completed)()
	view := NewView(Options{State: state, Activity: activity, Pages: pages, Assets: assets,
		Preferences: &memoryPreferences{}, Theme: theme.Default()})
	tester := ui.NewTester(view.Render, 1920, 1080)
	tester.Frame()
	tester.Key(0, ui.KeyDown)
	tester.Key(0, ui.KeyRight)
	if state.Focus != favoriteFocus(folders[1], true) {
		t.Fatalf("moving across the folder grid focused %s", state.Focus)
	}
	tester.Key(0, ui.KeyEnter)
	if state.Section != favoriteFolderSection || activity.Folder().ID != folders[1].ID {
		t.Fatal("the focused folder was not opened")
	}
	activity.Ensure(libraryQuery(view))
	(<-completed)()
	tester.Frame()
	tester.Key(0, ui.KeyDown)
	tester.Key(0, ui.KeyRight)
	if state.Focus != favoriteFocus(videos[1], false) {
		t.Fatalf("moving across saved videos focused %s", state.Focus)
	}
	view.dispatch(func() { state.Back() })
	tester.Frame()
	if state.Focus != favoriteFocus(folders[1], true) {
		t.Fatalf("returning to the directory focused %s", state.Focus)
	}
}

type accountReviewSource struct {
	uiAuthSource
	favoriteTestReader
	edits chan provider.FavoriteFolderEdit
}

func (s *accountReviewSource) EditFavoriteFolder(_ context.Context, edit provider.FavoriteFolderEdit) error {
	s.edits <- edit
	return nil
}

func TestAccountShortcutsAndFavoriteManagement(t *testing.T) {
	assets, err := LoadAssets()
	if err != nil {
		t.Fatal(err)
	}
	pages, err := NewPages(assets)
	if err != nil {
		t.Fatal(err)
	}
	entries := testEntries()
	entries[0].Capabilities = append(entries[0].Capabilities, provider.SignIn, provider.BrowseLibrary,
		provider.AccountHistory.Capability(), provider.AccountWatchLater.Capability(), provider.AccountFavorites.Capability(),
		provider.AccountNotifications.Capability(), provider.EditFavoriteFolders)
	state, err := app.NewState(entries, entries[0].ID, pages.Routes)
	if err != nil {
		t.Fatal(err)
	}
	folder := provider.AccountRow{ID: "folder", Title: "Review folder", CanDelete: true,
		Ref: &provider.MediaRef{EntryID: entries[0].ID, Kind: provider.CollectionKind, ID: "folder"}}
	video := provider.AccountRow{ID: "video", Title: "Saved video", RemovalID: "123:2"}
	source := &accountReviewSource{
		favoriteTestReader: favoriteTestReader{"": {folder}, folder.ID: {video}},
		edits:              make(chan provider.FavoriteFolderEdit, 2),
	}
	queued := make(chan func(), 30)
	post := func(f func()) { queued <- f }
	catalog := app.NewCatalog(func(string) provider.Session { return source }, post, func() {})
	defer catalog.Close()
	activity := app.NewAccountActivity(nil, post, func() {})
	defer activity.Close()
	store := uiTestKeychain{data: []byte(`{"selected":"owner","accounts":[{"account":{"id":"owner","name":"Viewer"},"data":{}}]}`)}
	var view *View
	auth := app.NewAuth(store, post, func() {}, func() {
		catalog.Reload()
		view.SyncAccountActivity()
	}, func() {})
	defer auth.Close()
	view = NewView(Options{State: state, Activity: activity, Auth: auth, Catalog: catalog, Pages: pages, Assets: assets,
		Preferences: &memoryPreferences{}, Theme: theme.Default()})
	tester := ui.NewTester(view.Render, 1920, 1080)
	await := func(done func() bool) {
		t.Helper()
		alarm := time.NewTimer(4 * time.Second)
		defer alarm.Stop()
		for !done() {
			select {
			case result := <-queued:
				result()
				tester.Frame()
			case <-alarm.C:
				t.Fatal("the account did not reach the expected state")
			}
		}
	}
	move := func(key ui.Key, want app.FocusID) {
		t.Helper()
		previous := state.Focus
		tester.Key(0, key)
		if state.Focus != want {
			t.Fatalf("key %v moved %s to %s on %s, want %s (%+v)", key, previous, state.Focus, state.Page, want, view.focus.controls)
		}
	}
	state.Navigate(app.Route{Page: app.Account})
	view.SyncEntry()
	await(func() bool { return auth.Accounts().CurrentID == "owner" })
	tester.Frame()
	move(ui.KeyLeft, app.RailFocus(app.Account))
	move(ui.KeyRight, app.AccountAction)
	move(ui.KeyUp, app.AccountInbox)
	move(ui.KeyDown, app.AccountAction)
	tester.Key(0, ui.KeyEnter)
	if state.Page != app.AccountPicker {
		t.Fatal("the account picker was not opened")
	}
	move(ui.KeyEscape, app.AccountAction)
	history := app.FocusID("account/shortcut/" + provider.AccountHistory)
	favorites := app.FocusID("account/shortcut/" + provider.AccountFavorites)
	later := app.FocusID("account/shortcut/" + provider.AccountWatchLater)
	move(ui.KeyDown, history)
	tester.Key(0, ui.KeyEnter)
	if state.Route != libraryRoute(provider.AccountHistory) {
		t.Fatal("the history shortcut did not open its list")
	}
	move(ui.KeyEscape, history)
	move(ui.KeyRight, favorites)
	move(ui.KeyRight, later)
	tester.Key(0, ui.KeyEnter)
	if state.Route != libraryRoute(provider.AccountWatchLater) {
		t.Fatal("the watch-later shortcut did not open its list")
	}
	move(ui.KeyEscape, later)
	move(ui.KeyLeft, favorites)
	tester.Key(0, ui.KeyEnter)
	if state.Route != libraryRoute(provider.AccountFavorites) {
		t.Fatal("the favorites shortcut did not open the directory")
	}
	await(func() bool { return activity.Snapshot(libraryQuery(view)).Phase == app.CatalogReady })
	tester.Frame()
	if !tester.Focused(assets.Text.Activity.CreateFolder) {
		t.Fatal("the directory did not focus the create action")
	}
	tester.Key(0, ui.KeyEnter)
	if state.Page != favoriteFolderDialog {
		t.Fatal("the create action did not open its editor")
	}
	tester.Key(0, ui.KeyEnter)
	tester.Type("New folder")
	move(ui.KeyEnter, favoriteDialogName)
	move(ui.KeyDown, favoriteDialogPrivacy)
	move(ui.KeyDown, favoriteDialogSave)
	tester.Key(0, ui.KeyEnter)
	await(func() bool { return state.Page == app.Library })
	if edit := <-source.edits; edit.Action != provider.FavoriteFolderCreate || edit.Title != "New folder" || !edit.Private {
		t.Fatalf("the create editor submitted %+v", edit)
	}
	await(func() bool { return activity.Snapshot(libraryQuery(view)).Phase == app.CatalogReady })
	move(ui.KeyDown, favoriteFocus(folder, true))
	tester.Key(0, ui.KeyEnter)
	await(func() bool { return activity.Snapshot(libraryQuery(view)).Phase == app.CatalogReady })
	tester.Frame()
	if !tester.Focused(assets.Text.Activity.FolderSettings) {
		t.Fatal("folder contents did not focus the settings action")
	}
	move(ui.KeyLeft, favoriteManage)
	tester.Key(0, ui.KeyEnter)
	move(ui.KeyDown, favoriteSelectAll)
	tester.Key(0, ui.KeyEnter)
	move(ui.KeyRight, favoriteRemove)
	tester.Key(0, ui.KeyEnter)
	if !tester.Focused(assets.Text.Activity.CancelFolder) {
		t.Fatal("the confirmation did not focus the folder dismissal action")
	}
	move(ui.KeyLeft, favoriteDialogSave)
	if !tester.Focused(assets.Text.Activity.ConfirmRemove) {
		t.Fatal("the selection did not focus the remove confirmation action")
	}
	tester.Key(0, ui.KeyEnter)
	await(func() bool { return state.Page == app.Library })
	if edit := <-source.edits; edit.Action != provider.FavoriteFolderRemove || edit.Folder != folder.ID ||
		len(edit.Resources) != 1 || edit.Resources[0] != video.RemovalID {
		t.Fatalf("the selection submitted %+v", edit)
	}
	move(ui.KeyEscape, favoriteFocus(folder, true))
	move(ui.KeyEscape, favorites)
	move(ui.KeyUp, app.AccountAction)
	move(ui.KeyUp, app.AccountInbox)
	move(ui.KeyEnter, noticeTab)
	await(func() bool { return activity.Snapshot(messagesQuery(view)).Phase == app.CatalogReady })
	if tester.HasText(assets.Text.Refresh) || tester.HasText(assets.Text.Activity.More) {
		t.Fatal("notification pagination must not occupy the page header")
	}
	move(ui.KeyDown, "account/messages/all")
	move(ui.KeyDown, noticeRowFocus(folder.ID))
	move(ui.KeyEnter, app.CloseModal)
	if state.Page != noticePage {
		t.Fatal("the focused notification did not open")
	}
	move(ui.KeyEscape, noticeRowFocus(folder.ID))
	move(ui.KeyEscape, app.AccountInbox)
}
