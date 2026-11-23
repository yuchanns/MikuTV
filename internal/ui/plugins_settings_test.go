package tvui

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/plugin"
	"github.com/yuchanns/MikuTV/internal/provider"
	"github.com/yuchanns/MikuTV/internal/theme"
)

type uiPluginFetcher struct {
	pages map[int]plugin.CatalogPage
	err   error
}

func (f *uiPluginFetcher) FetchPage(_ context.Context, page int, etag string) (plugin.CatalogPage, error) {
	if f.err != nil {
		return plugin.CatalogPage{}, f.err
	}
	p := f.pages[page]
	if page == 1 && etag != "" && etag == p.ETag {
		return plugin.CatalogPage{Page: 1, ETag: etag, NotModified: true}, nil
	}
	return p, nil
}

func (f *uiPluginFetcher) SeedInspectCache([]plugin.CatalogItem) {}

func TestPluginsSettingsCatalogDiscoverySearchFilterDetailRestoreAndEntriesIsolation(t *testing.T) {
	assets, err := LoadAssets()
	if err != nil {
		t.Fatal(err)
	}
	pages, err := NewPages(assets)
	if err != nil {
		t.Fatal(err)
	}
	entry := app.Entry{
		ID:           "sample-source",
		SourceID:     "sample-source",
		Name:         "SampleSource Source",
		Description:  "Built-in video source",
		Capabilities: provider.Capabilities{provider.BrowseCatalog},
	}
	state, err := app.NewState([]app.Entry{entry}, "sample-source", pages.Routes)
	if err != nil {
		t.Fatal(err)
	}

	itemSource := plugin.CatalogItem{
		Repository: "demo/media-source",
		RepoID:     11,
		Owner:      "demo",
		Release:    plugin.ReleaseAsset{ReleaseID: 101, TagName: "v1.0.0", AssetID: 201, AssetName: "plugin.zip"},
		Manifest: plugin.Manifest{
			ID: "media", Name: "Demo Media", Description: "Playlists and videos",
			Kind: plugin.SourceKind, Version: "1.0.0", APIVersion: 1, MinAppVersion: "0.1.0",
		},
		Compatibility: plugin.Compatibility{Compatible: true, Code: plugin.CompatibleOK},
	}
	itemIncompatible := plugin.CatalogItem{
		Repository: "demo/channel-source",
		RepoID:     12,
		Owner:      "demo",
		Release:    plugin.ReleaseAsset{ReleaseID: 102, TagName: "v2.0.0", AssetID: 202, AssetName: "plugin.zip"},
		Manifest: plugin.Manifest{
			ID: "channel", Name: "Demo Channel", Description: "Requires newer app",
			Kind: plugin.SourceKind, Version: "2.0.0", APIVersion: 1, MinAppVersion: "2.0.0",
		},
		Compatibility: plugin.Compatibility{Compatible: false, Code: plugin.IncompatibleAppVersion, RequiredAppVersion: "2.0.0"},
	}
	itemTheme := plugin.CatalogItem{
		Repository: "demo/ocean-theme",
		RepoID:     13,
		Owner:      "demo",
		Release:    plugin.ReleaseAsset{ReleaseID: 103, TagName: "v1.0.0", AssetID: 203, AssetName: "plugin.zip"},
		Manifest: plugin.Manifest{
			ID: "ocean", Name: "Ocean Blue", Description: "Cool blue palette",
			Kind: plugin.ThemeKind, Version: "1.0.0", APIVersion: 1, MinAppVersion: "0.1.0",
		},
		Compatibility: plugin.Compatibility{Compatible: false, Code: plugin.IncompatibleThemeUnavailable},
	}

	fetcher := &uiPluginFetcher{
		pages: map[int]plugin.CatalogPage{
			1: {Items: []plugin.CatalogItem{itemSource, itemIncompatible}, Page: 1, TotalCount: 3, HasMore: true, ETag: `"p1"`},
			2: {Items: []plugin.CatalogItem{itemTheme}, Page: 2, TotalCount: 3, HasMore: false},
		},
	}
	queue := make(chan func(), 32)
	drain := func(cond func() bool) {
		t.Helper()
		deadline := time.After(2 * time.Second)
		for !cond() {
			select {
			case fn := <-queue:
				fn()
			case <-deadline:
				t.Fatal("timed out waiting for plugin catalog async state")
			}
		}
	}

	pluginCat := app.NewPluginCatalog(
		[]app.InstalledPlugin{{
			ID: "sample-source", Name: "SampleSource Source", Description: "Built-in video source",
			Kind: plugin.SourceKind, Version: "0.1.0", Bundled: true, Enabled: true,
		}},
		fetcher,
		nil,
		func(fn func()) { queue <- fn },
		func() {},
	)
	t.Cleanup(pluginCat.Close)

	view := newTestView(t, Options{
		State:         state,
		PluginCatalog: pluginCat,
		Pages:         pages,
		Assets:        assets,
		Theme:         theme.Default(),
		Version:       "0.1.0",
	})
	state.Navigate(app.Route{Page: app.Settings, Section: "plugins"})
	tester := ui.NewTester(view.Render, 1920, 1080)
	tester.Frame()

	// 1. Open Discover view -> fetches page 1.
	view.State.Focus = pluginsDiscoverBtn
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	drain(func() bool { return pluginCat.Snapshot().Status == app.PluginCatalogReady })
	tester.Frame()
	if view.pluginsUI.view != pluginsViewDiscover {
		t.Fatalf("expected discover view, got %v", view.pluginsUI.view)
	}

	// 2. Load page 2 so Theme item joins the catalog -> focus recovers from disappeared load-more button to newly loaded item!
	view.State.Focus = pluginsLoadMoreBtn
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	drain(func() bool { return !pluginCat.Snapshot().LoadingMore && pluginCat.Snapshot().Page == 2 })
	tester.Frame()
	if view.State.Focus != pluginsCatalogFocus(itemTheme.Repository) {
		t.Fatalf("expected focus recovered from disappeared load-more button to %q, got %q",
			pluginsCatalogFocus(itemTheme.Repository), view.State.Focus)
	}

	view.State.Focus = pluginsFilterTheme
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	if view.pluginsUI.filter != plugin.ThemeKind {
		t.Fatalf("expected theme filter, got %q", view.pluginsUI.filter)
	}

	// Simulate search query narrowing to ocean-theme.
	view.pluginsUI.query = "ocean-theme"
	view.pluginsUI.input = "ocean-theme"
	tester.Frame()

	// 3. Open Ocean Blue detail, verify detail state, then press Back -> restores query, filter, and exact card focus!
	oceanFocus := pluginsCatalogFocus(itemTheme.Repository)
	view.State.Focus = oceanFocus
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	if view.pluginsUI.selectedCatalogRepo != itemTheme.Repository {
		t.Fatalf("expected detail opened for %q, got %q", itemTheme.Repository, view.pluginsUI.selectedCatalogRepo)
	}
	if !view.backFromPluginsSettings() {
		t.Fatal("backFromPluginsSettings did not close detail")
	}
	tester.Frame()
	if view.pluginsUI.selectedCatalogRepo != "" {
		t.Fatal("detail did not close")
	}
	if view.pluginsUI.filter != plugin.ThemeKind || view.pluginsUI.query != "ocean-theme" || view.State.Focus != oceanFocus {
		t.Fatalf("back from detail lost state: filter=%q query=%q focus=%q wantFocus=%q",
			view.pluginsUI.filter, view.pluginsUI.query, view.State.Focus, oceanFocus)
	}

	// 4. Empty search result offers Clear Search exit returning focus to search input.
	view.pluginsUI.query = "nonexistent-plugin-xyz"
	tester.Frame()
	view.State.Focus = pluginsEmptyExitBtn
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	if view.pluginsUI.query != "" || view.State.Focus != pluginsSearchInput {
		t.Fatalf("clear search exit failed: query=%q focus=%q", view.pluginsUI.query, view.State.Focus)
	}

	// 5. Offline failure keeps cached items visible and allows recovery via top refresh button.
	fetcher.err = errors.New("offline")
	view.State.Focus = pluginsRefreshTopBtn
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	drain(func() bool { return pluginCat.Snapshot().Status == app.PluginCatalogOffline })
	tester.Frame()
	if !pluginCat.Snapshot().HasCache || len(pluginCat.Snapshot().UninstalledItems("", "")) != 3 {
		t.Fatal("offline state did not preserve cached discovery items")
	}

	// 6. Entries picker invariant: catalog items and themes NEVER automatically become entries!
	if got := state.Entries(); len(got) != 1 || got[0].ID != "sample-source" {
		t.Fatalf("catalog items leaked into entries picker: %+v", got)
	}
}
