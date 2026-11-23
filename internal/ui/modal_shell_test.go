package tvui

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/provider"
	"github.com/yuchanns/MikuTV/internal/theme"
)

func TestModalShellPresentationAndRender(t *testing.T) {
	assets, err := LoadAssets()
	if err != nil {
		t.Fatalf("LoadAssets: %v", err)
	}
	pages, err := NewPages(assets)
	if err != nil {
		t.Fatalf("NewPages: %v", err)
	}
	entries := []app.Entry{{
		ID: "test", Name: "Test Source",
		Capabilities: provider.Capabilities{
			provider.SignIn, provider.SearchCatalog, provider.BrowseSeries,
			provider.BrowseFollowing, provider.EditFavoriteFolders,
		},
	}}
	state, err := app.NewState(entries, "test", pages.Routes)
	if err != nil {
		t.Fatalf("NewState: %v", err)
	}
	view := newTestView(t, Options{
		State: state, Pages: pages, Assets: assets, Theme: theme.Default(),
	})

	drawerRoutes := []app.Route{
		{Page: app.SeriesIndexFilter},
		{Page: app.SearchFilters},
		{Page: app.AccountPicker},
		{Page: app.PlaybackSettingPicker, Section: "playback"},
		{Page: app.DanmakuSettingPicker, Section: "danmaku"},
		{Page: app.Licenses, Section: "about"},
	}
	for _, route := range drawerRoutes {
		if got := view.modalStyle(route); got != modalDrawer {
			t.Errorf("route %+v: want modalDrawer, got %v", route, got)
		}
		if blur := view.modalBlur(route, false); blur != drawerBlurRadius {
			t.Errorf("route %+v: want drawer blur %v, got %v", route, drawerBlurRadius, blur)
		}
	}

	// Shelf "See All" pages must be full-screen non-modal collection routes, never drawers.
	for _, route := range []app.Route{
		{Page: app.HomeAction},
		{Page: app.FollowingAll},
		{Page: app.SeriesAll},
		{Page: app.LiveAll},
	} {
		reg, ok := pages.components[route]
		if !ok {
			t.Fatalf("missing registration for %+v", route)
		}
		if reg.Policy.Modal {
			t.Errorf("route %+v: want non-modal full-screen collection, got Modal=true", route)
		}
		if !reg.RawScreen {
			t.Errorf("route %+v: want RawScreen=true", route)
		}
		if blur := view.modalBlur(route, false); blur != 0 {
			t.Errorf("route %+v: want blur 0, got %v", route, blur)
		}
	}

	framelessRoutes := []app.Route{
		{Page: app.VideoConfirm},
		{Page: app.VideoCommentCompose},
		{Page: app.DanmakuRuleEditor, Section: "danmaku"},
		{Page: app.PluginRepoInput, Section: "plugins"},
		{Page: app.PluginInstallDialog, Section: "plugins"},
		{Page: app.PrivacyConfirm, Section: "privacy"},
		{Page: app.PrivacyDetails, Section: "about"},
		{Page: app.AboutProject, Section: "about"},
		{Page: app.SignInQR},
		{Page: app.SignOut},
		{Page: app.AccountDetails},
		{Page: noticePage},
	}
	for _, route := range framelessRoutes {
		if got := view.modalStyle(route); got != modalFrameless {
			t.Errorf("route %+v: want modalFrameless, got %v", route, got)
		}
		if blur := view.modalBlur(route, false); blur != framelessBlurRadius {
			t.Errorf("route %+v: want frameless blur %v, got %v", route, framelessBlurRadius, blur)
		}
	}

	// Favorite dialog adapts dynamically: settings/edit -> drawer, delete/remove -> frameless.
	favRoute := app.Route{Page: favoriteFolderDialog}
	view.favorites.dialog.mode = favoriteEdit
	if got := view.modalStyle(favRoute); got != modalDrawer {
		t.Fatalf("favoriteEdit: want modalDrawer, got %v", got)
	}
	view.favorites.dialog.mode = favoriteDelete
	if got := view.modalStyle(favRoute); got != modalFrameless {
		t.Fatalf("favoriteDelete: want modalFrameless, got %v", got)
	}

	// Smoke-render drawer + frameless modals in both Dark and Light themes.
	light := theme.Default()
	light.Dark = false
	tester := ui.NewTester(view.Render, 1920, 1080)
	for _, th := range []theme.Theme{theme.Default(), light} {
		view.SetTheme(th)
		state.Navigate(app.Route{Page: app.Account})
		for _, modal := range []app.Route{{Page: app.AccountPicker}, {Page: app.SignOut}} {
			state.Push(modal)
			tester.Frame()
			state.Back()
		}
	}
}
