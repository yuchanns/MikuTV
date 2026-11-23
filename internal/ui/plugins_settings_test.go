package tvui

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/plugin"
	"github.com/yuchanns/MikuTV/internal/provider"
	"github.com/yuchanns/MikuTV/internal/theme"
)

type uiPluginFetcher struct {
	mu          sync.Mutex
	pages       map[int]plugin.CatalogPage
	err         error
	resolved    map[string]plugin.CatalogItem
	resolveErr  map[string]error
	assets      map[int64][]byte
	downloadErr error
	blockCh     chan struct{}
}

func (f *uiPluginFetcher) FetchPage(_ context.Context, page int, etag string) (plugin.CatalogPage, error) {
	f.mu.Lock()
	err := f.err
	p := f.pages[page]
	f.mu.Unlock()
	if err != nil {
		return plugin.CatalogPage{}, err
	}
	if page == 1 && etag != "" && etag == p.ETag {
		return plugin.CatalogPage{Page: 1, ETag: etag, NotModified: true}, nil
	}
	return p, nil
}

func (f *uiPluginFetcher) SeedInspectCache([]plugin.CatalogItem) {}

func (f *uiPluginFetcher) ResolveRepository(_ context.Context, rawRepo string) (plugin.CatalogItem, error) {
	slug, ok := plugin.ParseRepository(rawRepo)
	if !ok {
		return plugin.CatalogItem{}, plugin.ErrInvalidRepository
	}
	f.mu.Lock()
	resErr, hasErr := f.resolveErr[slug]
	item, hasItem := f.resolved[slug]
	f.mu.Unlock()
	if hasErr {
		return plugin.CatalogItem{}, resErr
	}
	if hasItem {
		return item, nil
	}
	return plugin.CatalogItem{}, plugin.ErrNoOfficialRelease
}

func (f *uiPluginFetcher) DownloadAsset(ctx context.Context, asset plugin.ReleaseAsset) ([]byte, [sha256.Size]byte, error) {
	f.mu.Lock()
	blockCh := f.blockCh
	f.mu.Unlock()
	if blockCh != nil {
		select {
		case <-ctx.Done():
			return nil, [sha256.Size]byte{}, ctx.Err()
		case <-blockCh:
		}
	}
	f.mu.Lock()
	dlErr := f.downloadErr
	data, ok := f.assets[asset.AssetID]
	f.mu.Unlock()
	if dlErr != nil {
		return nil, [sha256.Size]byte{}, dlErr
	}
	if !ok {
		return nil, [sha256.Size]byte{}, errors.New("asset missing")
	}
	return data, sha256.Sum256(data), nil
}

func buildUIPluginZip(t *testing.T, id, name, version string, domains []string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	manifest, err := json.Marshal(map[string]any{
		"id":            id,
		"name":          name,
		"description":   "External test source",
		"kind":          "source",
		"version":       version,
		"apiVersion":    1,
		"minAppVersion": "0.1.0",
		"main":          "index.js",
		"license":       "MIT",
		"permissions":   map[string]any{"network": domains},
		"capabilities":  []string{"catalog.browse"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for filename, content := range map[string][]byte{
		"plugin.json": manifest,
		"index.js":    []byte(`export function browse() { return { items: [] }; }`),
	} {
		w, err := zw.Create(filename)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

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
	fetcher.mu.Lock()
	fetcher.err = errors.New("offline")
	fetcher.mu.Unlock()
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

func TestPluginsInstallWorkflowRepoConfirmFailRetrySuccessAndEntries(t *testing.T) {
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

	mediaZip := buildUIPluginZip(t, "media", "Demo Media", "1.0.0", []string{"api.demo.example", "*.cdn.demo.example"})
	mediaSHA := fmt.Sprintf("%x", sha256.Sum256(mediaZip))
	itemMedia := plugin.CatalogItem{
		Repository:  "demo/media-source",
		RepoID:      11,
		Owner:       "demo",
		Permissions: []string{"api.demo.example", "*.cdn.demo.example"},
		Release: plugin.ReleaseAsset{
			ReleaseID: 101, TagName: "v1.0.0", AssetID: 201, AssetName: "plugin.zip", SHA256: mediaSHA,
		},
		Manifest: plugin.Manifest{
			ID: "media", Name: "Demo Media", Description: "External test source",
			Kind: plugin.SourceKind, Version: "1.0.0", APIVersion: 1, MinAppVersion: "0.1.0", License: "MIT",
		},
		Compatibility: plugin.Compatibility{Compatible: true, Code: plugin.CompatibleOK},
	}
	itemConflict := plugin.CatalogItem{
		Repository: "other/sample-source-clone",
		RepoID:     12,
		Owner:      "other",
		Release:    plugin.ReleaseAsset{ReleaseID: 102, TagName: "v1.0.0", AssetID: 202, AssetName: "plugin.zip"},
		Manifest: plugin.Manifest{
			ID: "sample-source", Name: "Conflicting SampleSource", Kind: plugin.SourceKind, Version: "1.0.0", APIVersion: 1, MinAppVersion: "0.1.0",
		},
		Compatibility: plugin.Compatibility{Compatible: true, Code: plugin.CompatibleOK},
	}
	itemTheme := plugin.CatalogItem{
		Repository: "demo/ocean-theme",
		RepoID:     13,
		Owner:      "demo",
		Release:    plugin.ReleaseAsset{ReleaseID: 103, TagName: "v1.0.0", AssetID: 203, AssetName: "plugin.zip"},
		Manifest: plugin.Manifest{
			ID: "ocean", Name: "Ocean Theme", Kind: plugin.ThemeKind, Version: "1.0.0", APIVersion: 1, MinAppVersion: "0.1.0",
		},
		Compatibility: plugin.Compatibility{Compatible: false, Code: plugin.IncompatibleThemeUnavailable},
	}

	fetcher := &uiPluginFetcher{
		resolved: map[string]plugin.CatalogItem{
			"demo/media-source":   itemMedia,
			"other/sample-source-clone": itemConflict,
			"demo/ocean-theme":    itemTheme,
		},
		assets: map[int64][]byte{201: mediaZip},
	}
	queue := make(chan func(), 64)
	drain := func(cond func() bool) {
		t.Helper()
		deadline := time.After(3 * time.Second)
		for !cond() {
			select {
			case fn := <-queue:
				fn()
			case <-deadline:
				t.Fatal("timed out waiting for async plugin install state")
			}
		}
	}

	pkgRoot := filepath.Join(t.TempDir(), "plugins")
	pkgStore := plugin.NewPackageStore(pkgRoot)
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

	var view *View
	pluginCat.SetInstaller(pkgStore, "0.1.0", func(src *plugin.Source, receipt plugin.InstalledReceipt) error {
		info := src.Manifest()
		next := append(append([]app.Entry(nil), state.Entries()...), app.Entry{
			ID: info.ID, SourceID: info.ID, Name: info.Name, Description: info.Description,
			Artwork: info.ID, Capabilities: src.Capabilities(),
		})
		state.UpdateEntries(next)
		view.About.Plugins = append(view.About.Plugins, app.AboutPlugin{
			ID: info.ID, Name: info.Name, Kind: string(info.Kind), Version: info.Version, License: info.License,
		})
		return nil
	})

	view = newTestView(t, Options{
		State:         state,
		PluginCatalog: pluginCat,
		Pages:         pages,
		Assets:        assets,
		Theme:         theme.Default(),
		Version:       "0.1.0",
		About: app.AboutInfo{
			Version: "0.1.0",
			Plugins: []app.AboutPlugin{{ID: "sample-source", Name: "SampleSource Source", Kind: "source", Version: "0.1.0", Bundled: true}},
		},
	})
	state.Navigate(app.Route{Page: app.Settings, Section: "plugins"})
	tester := ui.NewTester(view.Render, 1920, 1080)
	tester.Frame()

	// 1. Open "Install from Repository" modal from Installed page.
	view.State.Focus = pluginsAddRepoBtn
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	if state.Page != app.PluginRepoInput {
		t.Fatalf("expected PluginRepoInput modal, got %v", state.Page)
	}

	// 2. Invalid slug shows inline validation error without closing modal.
	view.pluginsUI.repo.text = "not a valid repo"
	view.State.Focus = pluginRepoSubmit
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	if view.pluginsUI.repo.failure == "" || state.Page != app.PluginRepoInput {
		t.Fatalf("expected inline invalid repo error, got failure=%q page=%v", view.pluginsUI.repo.failure, state.Page)
	}

	// 3. Same-ID conflict against bundled sample-source is rejected cleanly.
	view.pluginsUI.repo.text = "https://github.com/other/sample-source-clone"
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	drain(func() bool { return !view.pluginsUI.repo.busy })
	tester.Frame()
	if view.pluginsUI.repo.failure != assets.Text.Plugins.RepoConflictErr {
		t.Fatalf("expected conflict error %q, got %q", assets.Text.Plugins.RepoConflictErr, view.pluginsUI.repo.failure)
	}

	// 4. Valid repository URL resolves and opens the catalog detail view.
	view.pluginsUI.repo.text = "https://github.com/demo/media-source"
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	drain(func() bool { return state.Page == app.Settings && view.pluginsUI.selectedCatalogRepo == "demo/media-source" })
	tester.Frame()
	if state.Focus != pluginsDetailInstall {
		t.Fatalf("expected focus on install row, got %q", state.Focus)
	}

	// 5. Incompatible theme item cannot open install confirmation.
	view.openPluginInstallConfirm(itemTheme)
	if state.Page != app.Settings {
		t.Fatalf("theme plugin should not open install dialog, got page=%v", state.Page)
	}

	// 6. Click Install -> opens confirm modal with permissions.
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	if state.Page != app.PluginInstallDialog || view.pluginsUI.install.phase != pluginInstallConfirm {
		t.Fatalf("expected confirm install modal, got page=%v phase=%v", state.Page, view.pluginsUI.install.phase)
	}

	// 7. Confirm while download fails -> transitions to retryable error phase with zero staging residue.
	fetcher.mu.Lock()
	fetcher.downloadErr = errors.New("network timeout")
	fetcher.mu.Unlock()
	view.State.Focus = pluginInstallConfirmBtn
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	drain(func() bool { return view.pluginsUI.install.phase == pluginInstallError })
	tester.Frame()
	if !view.pluginsUI.install.retryable || state.Focus != pluginInstallRetryBtn {
		t.Fatalf("expected retryable failure focused on Retry, retryable=%v focus=%q",
			view.pluginsUI.install.retryable, state.Focus)
	}
	if len(state.Entries()) != 1 {
		t.Fatalf("failed install must not mount entry: %+v", state.Entries())
	}

	// 8. Clear network error and press Retry -> installs, mounts dynamically, preserves CurrentEntry!
	fetcher.mu.Lock()
	fetcher.downloadErr = nil
	fetcher.mu.Unlock()
	tester.Key(0, ui.KeyEnter)
	drain(func() bool { return view.pluginsUI.install.phase == pluginInstallSuccess })
	tester.Frame()
	if state.Focus != pluginInstallOpenEntryBtn {
		t.Fatalf("expected focus on Open Entries button, got %q", state.Focus)
	}
	if state.CurrentEntry != "sample-source" {
		t.Fatalf("install must NOT switch CurrentEntry; got %q", state.CurrentEntry)
	}
	if got := state.Entries(); len(got) != 2 || got[1].ID != "media" {
		t.Fatalf("expected dynamically mounted media entry, got %+v", got)
	}
	if len(view.About.Plugins) != 2 || view.About.Plugins[1].ID != "media" {
		t.Fatalf("expected About.Plugins updated, got %+v", view.About.Plugins)
	}
	if staging, err := os.ReadDir(filepath.Join(pkgRoot, ".staging")); err == nil && len(staging) != 0 {
		t.Fatalf("expected empty staging directory, got %d files", len(staging))
	}

	// 9. Click [选择入口] -> navigates to Entries focused on new entry while sample-source stays CurrentEntry;
	// pressing Back returns cleanly to Settings/plugins focused on the newly installed plugin!
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	if state.Page != app.Entries || state.Focus != app.EntryFocus("media") || state.CurrentEntry != "sample-source" {
		t.Fatalf("expected Entries focused on media while keeping sample-source current, got page=%v focus=%q current=%q",
			state.Page, state.Focus, state.CurrentEntry)
	}
	if !state.Back() {
		t.Fatal("expected Back from Entries to return to Settings/plugins")
	}
	tester.Frame()
	if state.Page != app.Settings || state.Section != "plugins" || state.Focus != pluginsInstalledFocus("media") {
		t.Fatalf("expected clean return to Settings/plugins focused on media, got route=%+v focus=%q",
			state.Route, state.Focus)
	}
}

func TestPluginsUpdateWorkflowCheckExpandedPermissionsFailPreserveRetryAndHotReload(t *testing.T) {
	assets, err := LoadAssets()
	if err != nil {
		t.Fatal(err)
	}
	pages, err := NewPages(assets)
	if err != nil {
		t.Fatal(err)
	}
	state, err := app.NewState([]app.Entry{{
		ID: "sample-source", SourceID: "sample-source", Name: "SampleSource Source", Description: "Built-in",
		Capabilities: provider.Capabilities{provider.BrowseCatalog},
	}}, "sample-source", pages.Routes)
	if err != nil {
		t.Fatal(err)
	}

	v1Zip := buildUIPluginZip(t, "media", "Demo Media", "1.0.0", []string{"api.demo.example"})
	v1SHA := fmt.Sprintf("%x", sha256.Sum256(v1Zip))
	v1Item := plugin.CatalogItem{
		Repository:    "demo/media-source",
		Permissions:   []string{"api.demo.example"},
		Release:       plugin.ReleaseAsset{ReleaseID: 1, TagName: "v1.0.0", AssetID: 101, AssetName: "plugin.zip", SHA256: v1SHA},
		Manifest:      plugin.Manifest{ID: "media", Name: "Demo Media", Description: "v1", Kind: plugin.SourceKind, Version: "1.0.0", APIVersion: 1, MinAppVersion: "0.1.0", License: "MIT"},
		Compatibility: plugin.Compatibility{Compatible: true, Code: plugin.CompatibleOK},
	}

	v2Zip := buildUIPluginZip(t, "media", "Demo Media", "1.1.0", []string{"api.demo.example", "images.demo.example"})
	v2SHA := fmt.Sprintf("%x", sha256.Sum256(v2Zip))
	v2Item := plugin.CatalogItem{
		Repository:    "demo/media-source",
		Permissions:   []string{"api.demo.example", "images.demo.example"},
		Release:       plugin.ReleaseAsset{ReleaseID: 2, TagName: "v1.1.0", AssetID: 102, AssetName: "plugin.zip", SHA256: v2SHA},
		Manifest:      plugin.Manifest{ID: "media", Name: "Demo Media", Description: "v2", Kind: plugin.SourceKind, Version: "1.1.0", APIVersion: 1, MinAppVersion: "0.1.0", License: "MIT"},
		Compatibility: plugin.Compatibility{Compatible: true, Code: plugin.CompatibleOK},
	}

	fetcher := &uiPluginFetcher{
		resolved: map[string]plugin.CatalogItem{"demo/media-source": v1Item},
		assets:   map[int64][]byte{101: v1Zip, 102: v2Zip},
	}
	queue := make(chan func(), 64)
	drain := func(cond func() bool) {
		t.Helper()
		deadline := time.After(3 * time.Second)
		for !cond() {
			select {
			case fn := <-queue:
				fn()
			case <-deadline:
				t.Fatal("timed out waiting for async update state")
			}
		}
	}

	pkgRoot := filepath.Join(t.TempDir(), "plugins")
	pkgStore := plugin.NewPackageStore(pkgRoot)
	pluginCat := app.NewPluginCatalog(
		[]app.InstalledPlugin{{
			ID: "sample-source", Name: "SampleSource Source", Kind: plugin.SourceKind, Version: "0.1.0", Bundled: true, Enabled: true,
		}},
		fetcher,
		nil,
		func(fn func()) { queue <- fn },
		func() {},
	)
	t.Cleanup(pluginCat.Close)

	var view *View
	var stoppedSourceID string
	var appliedGen uint64
	pluginCat.SetInstaller(pkgStore, "0.1.0", func(src *plugin.Source, receipt plugin.InstalledReceipt) error {
		info := src.Manifest()
		next := append(append([]app.Entry(nil), state.Entries()...), app.Entry{
			ID: info.ID, SourceID: info.ID, Name: info.Name, Description: info.Description,
			Artwork: info.ID, Capabilities: src.Capabilities(),
		})
		state.UpdateEntries(next)
		view.About.Plugins = append(view.About.Plugins, app.AboutPlugin{
			ID: info.ID, Name: info.Name, Kind: string(info.Kind), Version: info.Version, License: info.License,
		})
		return nil
	})
	pluginCat.SetUpdateHooks(app.PluginUpdateHooks{
		BeforeSourceUpdate: func(pluginID string) { stoppedSourceID = pluginID },
		ApplySourceUpdate: func(src *plugin.Source, receipt plugin.InstalledReceipt) error {
			info := src.Manifest()
			cur := state.Entries()
			next := make([]app.Entry, len(cur))
			for i, e := range cur {
				if e.ID == info.ID {
					e.Description = info.Description
					e.Generation++
					appliedGen = e.Generation
				}
				next[i] = e
			}
			state.UpdateEntries(next)
			for i := range view.About.Plugins {
				if view.About.Plugins[i].ID == info.ID {
					view.About.Plugins[i].Version = info.Version
				}
			}
			return nil
		},
	})

	view = newTestView(t, Options{
		State:         state,
		PluginCatalog: pluginCat,
		Pages:         pages,
		Assets:        assets,
		Theme:         theme.Default(),
		Version:       "0.1.0",
		About: app.AboutInfo{
			Version: "0.1.0",
			Plugins: []app.AboutPlugin{{ID: "sample-source", Name: "SampleSource Source", Kind: "source", Version: "0.1.0", Bundled: true}},
		},
	})

	// Install v1 first.
	installed := false
	pluginCat.Install(v1Item, func(out app.PluginInstallOutcome) {
		if out.Err != nil {
			t.Fatalf("install v1: %v", out.Err)
		}
		installed = true
	})
	drain(func() bool { return installed })

	// Publish v2 with expanded permissions on mock server and trigger Check Updates from UI.
	fetcher.mu.Lock()
	fetcher.resolved["demo/media-source"] = v2Item
	fetcher.mu.Unlock()

	state.Navigate(app.Route{Page: app.Settings, Section: "plugins"})
	tester := ui.NewTester(view.Render, 1920, 1080)
	tester.Frame()

	view.State.Focus = pluginsCheckUpdatesBtn
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	drain(func() bool {
		inst, ok := pluginCat.FindInstalled("media")
		return ok && inst.AvailableUpdate != nil
	})
	tester.Frame()

	// Open media detail -> click Version row -> opens PluginUpdateDialog in confirm phase with expanded permission.
	view.State.Focus = pluginsInstalledFocus("media")
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	if view.pluginsUI.selectedInstalledID != "media" {
		t.Fatalf("expected installed detail for media, got %q", view.pluginsUI.selectedInstalledID)
	}
	view.State.Focus = pluginsDetailVersion
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	if state.Page != app.PluginUpdateDialog || view.pluginsUI.update.phase != pluginUpdateConfirm || !view.pluginsUI.update.offer.HasPermissionExpansion() {
		t.Fatalf("expected update confirm modal with permission expansion, got page=%v phase=%v", state.Page, view.pluginsUI.update.phase)
	}

	// Simulate network failure -> preserves v1.0.0, does NOT stop source, focuses Retry.
	fetcher.mu.Lock()
	fetcher.downloadErr = errors.New("connection dropped")
	fetcher.mu.Unlock()
	view.State.Focus = pluginUpdateConfirmBtn
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	drain(func() bool { return view.pluginsUI.update.phase == pluginUpdateError })
	tester.Frame()
	if state.Focus != pluginUpdateRetryBtn || stoppedSourceID != "" {
		t.Fatalf("expected retry focus without stopping source, got focus=%q stopped=%q", state.Focus, stoppedSourceID)
	}
	if cur, _ := pluginCat.FindInstalled("media"); cur.Version != "1.0.0" {
		t.Fatalf("failed update altered installed version: %+v", cur)
	}

	// Clear network failure and press Retry -> updates cleanly to 1.1.0, bumps Generation, closes modal to detail!
	fetcher.mu.Lock()
	fetcher.downloadErr = nil
	fetcher.mu.Unlock()
	tester.Key(0, ui.KeyEnter)
	drain(func() bool {
		cur, ok := pluginCat.FindInstalled("media")
		return ok && cur.Version == "1.1.0"
	})
	tester.Frame()
	if state.Page != app.Settings || state.Focus != pluginsDetailVersion {
		t.Fatalf("expected return to detail version row after update, got page=%v focus=%q", state.Page, state.Focus)
	}
	if stoppedSourceID != "media" || appliedGen == 0 {
		t.Fatalf("expected source stop hook and bumped entry generation, got stopped=%q gen=%d", stoppedSourceID, appliedGen)
	}
	if view.About.Plugins[1].Version != "1.1.0" {
		t.Fatalf("expected About.Plugins updated to 1.1.0, got %+v", view.About.Plugins)
	}
	if staging, err := os.ReadDir(filepath.Join(pkgRoot, ".staging")); err == nil && len(staging) != 0 {
		t.Fatalf("expected empty staging directory, got %d files", len(staging))
	}
}

func TestPluginsRemoveUninstallDisableEmptyEntriesAndReEnable(t *testing.T) {
	assets, err := LoadAssets()
	if err != nil {
		t.Fatal(err)
	}
	pages, err := NewPages(assets)
	if err != nil {
		t.Fatal(err)
	}
	miniEntry := app.Entry{
		ID: "sample-source", SourceID: "sample-source", Name: "SampleSource Source", Description: "Built-in",
		Capabilities: provider.Capabilities{provider.BrowseCatalog},
	}
	state, err := app.NewState([]app.Entry{miniEntry}, "sample-source", pages.Routes)
	if err != nil {
		t.Fatal(err)
	}

	mediaZip := buildUIPluginZip(t, "media", "Demo Media", "1.0.0", []string{"api.demo.example"})
	mediaItem := plugin.CatalogItem{
		Repository:    "demo/media-source",
		Permissions:   []string{"api.demo.example"},
		Release:       plugin.ReleaseAsset{ReleaseID: 1, TagName: "v1.0.0", AssetID: 101, AssetName: "plugin.zip", SHA256: fmt.Sprintf("%x", sha256.Sum256(mediaZip))},
		Manifest:      plugin.Manifest{ID: "media", Name: "Demo Media", Description: "External", Kind: plugin.SourceKind, Version: "1.0.0", APIVersion: 1, MinAppVersion: "0.1.0", License: "MIT"},
		Compatibility: plugin.Compatibility{Compatible: true, Code: plugin.CompatibleOK},
	}
	fetcher := &uiPluginFetcher{
		resolved: map[string]plugin.CatalogItem{"demo/media-source": mediaItem},
		assets:   map[int64][]byte{101: mediaZip},
	}
	queue := make(chan func(), 64)
	drain := func(cond func() bool) {
		t.Helper()
		deadline := time.After(3 * time.Second)
		for !cond() {
			select {
			case fn := <-queue:
				fn()
			case <-deadline:
				t.Fatal("timed out waiting for async callback")
			}
		}
	}

	pkgRoot := filepath.Join(t.TempDir(), "plugins")
	pkgStore := plugin.NewPackageStore(pkgRoot)
	pluginCat := app.NewPluginCatalog(
		[]app.InstalledPlugin{{
			ID: "sample-source", Name: "SampleSource Source", Kind: plugin.SourceKind, Version: "0.1.0", Bundled: true, Enabled: true,
		}},
		fetcher,
		nil,
		func(fn func()) { queue <- fn },
		func() {},
	)
	t.Cleanup(pluginCat.Close)

	entriesByID := map[string]app.Entry{"sample-source": miniEntry}
	var view *View
	var lastRemovedClearData *bool
	removeEntry := func(id string) {
		cur := state.Entries()
		next := make([]app.Entry, 0, len(cur))
		for _, e := range cur {
			if e.ID != id {
				next = append(next, e)
			}
		}
		state.UpdateEntries(next)
	}
	pluginCat.SetInstaller(pkgStore, "0.1.0", func(src *plugin.Source, receipt plugin.InstalledReceipt) error {
		info := src.Manifest()
		e := app.Entry{ID: info.ID, SourceID: info.ID, Name: info.Name, Description: info.Description, Artwork: info.ID, Capabilities: src.Capabilities()}
		entriesByID[e.ID] = e
		state.UpdateEntries(append(append([]app.Entry(nil), state.Entries()...), e))
		return nil
	})
	pluginCat.SetLifecycleHooks(app.PluginLifecycleHooks{
		DisableSource: func(id string) { removeEntry(id) },
		EnableSource: func(id string) error {
			e := entriesByID[id]
			state.UpdateEntries(append(append([]app.Entry(nil), state.Entries()...), e))
			return nil
		},
		RemoveSource: func(id string, clearData bool) {
			cd := clearData
			lastRemovedClearData = &cd
			delete(entriesByID, id)
			removeEntry(id)
		},
	})

	view = newTestView(t, Options{
		State: state, PluginCatalog: pluginCat, Pages: pages, Assets: assets, Theme: theme.Default(), Version: "0.1.0",
	})

	// Install external media plugin.
	installed := false
	pluginCat.Install(mediaItem, func(out app.PluginInstallOutcome) {
		if out.Err != nil {
			t.Fatal(out.Err)
		}
		installed = true
	})
	drain(func() bool { return installed })

	state.Navigate(app.Route{Page: app.Settings, Section: "plugins"})
	tester := ui.NewTester(view.Render, 1920, 1080)
	tester.Frame()

	// 1. Open external media detail -> click Remove -> opens PluginRemoveDialog -> Confirm uninstalls preserving data!
	view.State.Focus = pluginsInstalledFocus("media")
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	view.State.Focus = pluginsDetailRemove
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	if state.Page != app.PluginRemoveDialog || state.Focus != pluginRemoveConfirmBtn {
		t.Fatalf("expected PluginRemoveDialog focused on confirm, got page=%v focus=%q", state.Page, state.Focus)
	}
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	if state.Page != app.Settings || lastRemovedClearData == nil || *lastRemovedClearData {
		t.Fatalf("expected clean uninstall keeping account/settings data, got page=%v clearData=%v", state.Page, lastRemovedClearData)
	}
	if _, ok := pluginCat.FindInstalled("media"); ok || len(state.Entries()) != 1 {
		t.Fatalf("removed external plugin still present: entries=%+v", state.Entries())
	}

	// 2. Open bundled sample-source detail (current entry) -> toggle Enable -> opens PluginDisableDialog -> Confirm!
	view.State.Focus = pluginsInstalledFocus("sample-source")
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	view.State.Focus = pluginsDetailToggle
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	if state.Page != app.PluginDisableDialog || state.Focus != pluginDisableConfirmBtn {
		t.Fatalf("expected PluginDisableDialog focused on confirm, got page=%v focus=%q", state.Page, state.Focus)
	}
	tester.Key(0, ui.KeyEnter)
	tester.Frame()

	// Disabling the sole/current entry MUST route to Entries empty state focused on [管理插件]!
	if state.Page != app.Entries || state.CurrentEntry != "" || len(state.Entries()) != 0 || state.Focus != app.ManagePlugins {
		t.Fatalf("expected empty Entries focused on ManagePlugins after disabling current/last entry, got page=%v current=%q entries=%d focus=%q",
			state.Page, state.CurrentEntry, len(state.Entries()), state.Focus)
	}

	// 3. Click [管理插件] -> opens Settings/plugins -> open sample-source -> re-enable -> entry restored!
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	if state.Page != app.Settings || state.Section != "plugins" {
		t.Fatalf("ManagePlugins did not open Settings/plugins: %+v", state.Route)
	}
	view.State.Focus = pluginsInstalledFocus("sample-source")
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	view.State.Focus = pluginsDetailToggle
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	if inst, _ := pluginCat.FindInstalled("sample-source"); !inst.Enabled || len(state.Entries()) != 1 {
		t.Fatalf("re-enable bundled plugin failed: inst=%+v entries=%d", inst, len(state.Entries()))
	}
}
