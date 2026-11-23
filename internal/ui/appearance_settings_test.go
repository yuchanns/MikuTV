package tvui

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/plugin"
	"github.com/yuchanns/MikuTV/internal/provider"
	"github.com/yuchanns/MikuTV/internal/theme"
)

func buildUIThemeZip(t *testing.T, id, name, desc, version, themeJSON string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	manifest := fmt.Sprintf(`{
		"id": %q,
		"name": %q,
		"description": %q,
		"kind": "theme",
		"version": %q,
		"apiVersion": 1,
		"minAppVersion": "0.1.0",
		"license": "MIT",
		"theme": "theme.json"
	}`, id, name, desc, version)
	for name, body := range map[string]string{
		"plugin.json": manifest,
		"theme.json":  themeJSON,
	} {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

const uiTwilightThemeJSON = `{
	"schemaVersion": 1,
	"base": "mikutv-dark",
	"appearance": "dark",
	"colors": {
		"surface": {
			"canvas": "#10101C",
			"sidebar": "#0C0C16",
			"panel": "#1A1829",
			"raised": "#242036",
			"input": "#161425"
		},
		"text": {
			"primary": "#F5F0FC",
			"secondary": "#BDB3CD",
			"muted": "#A59BB8"
		},
		"accent": {
			"primary": "#BDA0EC",
			"soft": "#DAC6F7",
			"strong": "#9F7CCC",
			"foreground": "#221332"
		},
		"focus": {
			"background": "#F5F0FC",
			"foreground": "#21172E",
			"ring": "#F4F6FA"
		},
		"selected": {
			"background": "#332442",
			"foreground": "#DAC6F7",
			"focusBackground": "#654682",
			"focusForeground": "#FFFFFF"
		},
		"decoration": {
			"ambient": "#8B60C920",
			"cardStart": "#2B1E42",
			"cardEnd": "#140F24"
		}
	}
}`

const uiOceanThemeJSON = `{
	"schemaVersion": 1,
	"base": "mikutv-dark",
	"appearance": "dark",
	"colors": {
		"surface": {
			"canvas": "#08151C",
			"sidebar": "#071017",
			"panel": "#102630",
			"raised": "#18323D",
			"input": "#0D1E27"
		},
		"text": {
			"primary": "#EEFAFF",
			"secondary": "#B1CBD5",
			"muted": "#91B4C2"
		},
		"accent": {
			"primary": "#60BFEC",
			"soft": "#B2E5FF",
			"strong": "#4AA4D2",
			"foreground": "#072537"
		},
		"selected": {
			"background": "#153D53",
			"foreground": "#B2E5FF",
			"focusBackground": "#245C7A",
			"focusForeground": "#FFFFFF"
		}
	}
}`

func TestAppearanceThemePickerDiscoverInstallApplyErrorAndTeardown(t *testing.T) {
	assets, err := LoadAssets()
	if err != nil {
		t.Fatal(err)
	}
	pages, err := NewPages(assets)
	if err != nil {
		t.Fatal(err)
	}
	miniEntry := app.Entry{
		ID:           "sample-source",
		SourceID:     "sample-source",
		Name:         "SampleSource Source",
		Description:  "Built-in video source",
		Capabilities: provider.Capabilities{provider.BrowseCatalog},
	}
	state, err := app.NewState([]app.Entry{miniEntry}, "sample-source", pages.Routes)
	if err != nil {
		t.Fatal(err)
	}

	twilightZip := buildUIThemeZip(t, "twilight", "Twilight Purple", "Soft purple dark theme", "1.0.0", uiTwilightThemeJSON)
	twilightInsp, err := plugin.InspectPackage(twilightZip, "0.1.0")
	if err != nil || !twilightInsp.Compatibility.Compatible {
		t.Fatalf("inspect twilight: err=%v compat=%+v", err, twilightInsp.Compatibility)
	}
	twilightItem := plugin.CatalogItem{
		Repository:    "demo/twilight-theme",
		RepoID:        201,
		Owner:         "demo",
		Release:       plugin.ReleaseAsset{ReleaseID: 1, TagName: "v1.0.0", AssetID: 301, AssetName: "plugin.zip", SHA256: fmt.Sprintf("%x", sha256.Sum256(twilightZip))},
		Manifest:      twilightInsp.Manifest,
		Swatches:      twilightInsp.Swatches,
		Compatibility: twilightInsp.Compatibility,
	}

	oceanZip := buildUIThemeZip(t, "ocean", "Ocean Blue", "Deep ocean palette", "1.0.0", uiOceanThemeJSON)
	oceanInsp, err := plugin.InspectPackage(oceanZip, "0.1.0")
	if err != nil || !oceanInsp.Compatibility.Compatible {
		t.Fatalf("inspect ocean: err=%v compat=%+v", err, oceanInsp.Compatibility)
	}
	oceanItem := plugin.CatalogItem{
		Repository:    "demo/ocean-theme",
		RepoID:        202,
		Owner:         "demo",
		Release:       plugin.ReleaseAsset{ReleaseID: 2, TagName: "v1.0.0", AssetID: 302, AssetName: "plugin.zip", SHA256: fmt.Sprintf("%x", sha256.Sum256(oceanZip))},
		Manifest:      oceanInsp.Manifest,
		Swatches:      oceanInsp.Swatches,
		Compatibility: oceanInsp.Compatibility,
	}

	fetcher := &uiPluginFetcher{
		pages: map[int]plugin.CatalogPage{
			1: {Items: []plugin.CatalogItem{twilightItem, oceanItem}, Page: 1, TotalCount: 2, HasMore: false},
		},
		resolved: map[string]plugin.CatalogItem{
			"demo/twilight-theme": twilightItem,
			"demo/ocean-theme":    oceanItem,
		},
		assets: map[int64][]byte{301: twilightZip, 302: oceanZip},
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
				t.Fatal("timed out waiting for async UI callback")
			}
		}
	}

	pkgStore := plugin.NewPackageStore(filepath.Join(t.TempDir(), "plugins"))
	var view *View
	var pluginCat *app.PluginCatalog
	appearance := app.NewAppearanceSettings(
		pkgStore,
		nil,
		func(next theme.Theme) {
			if view != nil {
				view.SetTheme(next)
			}
		},
		func(activeID string) {
			if pluginCat != nil {
				pluginCat.SyncActiveTheme(activeID)
			}
		},
		func() {},
	)
	pluginCat = app.NewPluginCatalog(
		[]app.InstalledPlugin{{
			ID: "sample-source", Name: "SampleSource Source", Kind: plugin.SourceKind, Version: "0.1.0", Bundled: true, Enabled: true,
		}},
		fetcher,
		nil,
		func(fn func()) { queue <- fn },
		func() {},
	)
	t.Cleanup(pluginCat.Close)

	pluginCat.SetInstaller(pkgStore, "0.1.0", func(pkg plugin.InstalledPackage) error {
		if pkg.Theme != nil {
			appearance.RegisterTheme(pkg.Theme)
		}
		return nil
	})
	pluginCat.SetLifecycleHooks(app.PluginLifecycleHooks{
		EnableTheme: func(id string) error {
			_, err := appearance.SelectTheme(id)
			return err
		},
		DisableTheme: func(id string) { appearance.DisableTheme(id) },
		RemoveTheme:  func(id string) { appearance.RemoveTheme(id) },
	})

	view = newTestView(t, Options{
		State:         state,
		Appearance:    appearance,
		PluginCatalog: pluginCat,
		Pages:         pages,
		Assets:        assets,
		Theme:         appearance.ActiveTheme(),
		Version:       "0.1.0",
	})

	state.Navigate(app.Route{Page: app.Settings, Section: "appearance"})
	tester := ui.NewTester(view.Render, 1920, 1080)
	tester.Frame()

	// 1. Toggle HoverPreview & ReduceMotion in Settings/appearance and verify persistence + helpers.
	view.State.Focus = appearanceHoverPreviewRow
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	if view.hoverPreviewEnabled() {
		t.Fatal("expected hoverPreview disabled after toggle")
	}
	view.State.Focus = appearanceReduceMotionRow
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	if !view.reduceMotionEnabled(nil) {
		t.Fatal("expected reduceMotion enabled after toggle")
	}

	// 2. Open ThemePicker -> built-in only initially; click [发现更多主题] -> Discover pre-filtered to ThemeKind!
	view.State.Focus = appearanceThemeRow
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	if state.Page != app.ThemePicker || state.Focus != themePickerOptionID(theme.BaseMikuTVDark) {
		t.Fatalf("expected ThemePicker focused on mikutv-dark, got page=%v focus=%q", state.Page, state.Focus)
	}
	view.State.Focus = themePickerDiscoverBtn
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	drain(func() bool { return pluginCat.Snapshot().Status == app.PluginCatalogReady })
	tester.Frame()
	if state.Page != app.Settings || state.Section != "plugins" || view.pluginsUI.view != pluginsViewDiscover ||
		view.pluginsUI.filter != plugin.ThemeKind || !view.pluginsUI.returnToThemePicker {
		t.Fatalf("Discover More Themes did not open filtered discover flow: route=%+v ui=%+v", state.Route, view.pluginsUI)
	}

	// 3. Back from Discover without installing returns cleanly to ThemePicker!
	if !view.goBack() {
		t.Fatal("Back from theme discover failed")
	}
	tester.Frame()
	if state.Page != app.ThemePicker || state.Section != "appearance" {
		t.Fatalf("Back from picker-launched Discover should return to ThemePicker, got route=%+v", state.Route)
	}

	// 4. Re-enter Discover from ThemePicker -> open Twilight detail -> confirm install ->
	// MUST return directly to ThemePicker focused on Twilight while KEEPING MikuTV Dark active!
	view.State.Focus = themePickerDiscoverBtn
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	tester.Frame()

	view.State.Focus = pluginsCatalogFocus("demo/twilight-theme")
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	if view.pluginsUI.selectedCatalogRepo != "demo/twilight-theme" || state.Focus != pluginsDetailInstall {
		t.Fatalf("expected Twilight catalog detail focused on install, got repo=%q focus=%q",
			view.pluginsUI.selectedCatalogRepo, state.Focus)
	}
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	if state.Page != app.PluginInstallDialog || view.pluginsUI.install.phase != pluginInstallConfirm {
		t.Fatalf("expected confirm install modal, got page=%v phase=%v", state.Page, view.pluginsUI.install.phase)
	}
	view.State.Focus = pluginInstallConfirmBtn
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	drain(func() bool { return state.Page == app.ThemePicker })
	tester.Frame()

	if state.Focus != themePickerOptionID("twilight") {
		t.Fatalf("expected ThemePicker focused on newly installed twilight, got %q", state.Focus)
	}
	if appearance.ActiveThemeID() != theme.BaseMikuTVDark || view.Theme.ID != theme.BaseMikuTVDark {
		t.Fatalf("installing theme from picker must NOT auto-apply: active=%q viewTheme=%q",
			appearance.ActiveThemeID(), view.Theme.ID)
	}
	if got := state.Entries(); len(got) != 1 || got[0].ID != "sample-source" {
		t.Fatalf("theme plugin leaked into entries picker: %+v", got)
	}

	// 5. Confirm focused Twilight -> atomic theme switch, closes picker, keeps CurrentEntry & focus!
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	if state.Page != app.Settings || state.Section != "appearance" || state.Focus != appearanceThemeRow {
		t.Fatalf("expected return to Settings/appearance theme row after apply, got route=%+v focus=%q",
			state.Route, state.Focus)
	}
	if view.Theme.ID != "twilight" || view.Theme.Colors.Accent.Primary != 0xbda0ecff {
		t.Fatalf("view.Theme not updated atomically to twilight: %+v", view.Theme)
	}
	if state.CurrentEntry != "sample-source" {
		t.Fatalf("switching theme altered CurrentEntry: %q", state.CurrentEntry)
	}

	// 6. Theme apply error opens Scheme B ThemeError preserving Twilight; [重新选择] reopens ThemePicker.
	view.openThemePicker("")
	tester.Frame()
	view.selectThemeOption(app.ThemeOption{ID: "missing-theme", Name: "Broken Theme"})
	tester.Frame()
	if state.Page != app.ThemeError || state.Focus != themeErrorReselectBtn || view.Theme.ID != "twilight" {
		t.Fatalf("expected ThemeError preserving twilight, got page=%v focus=%q theme=%q",
			state.Page, state.Focus, view.Theme.ID)
	}
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	if state.Page != app.ThemePicker || state.Focus != themePickerOptionID("twilight") {
		t.Fatalf("ThemeError Reselect did not reopen ThemePicker focused on active theme: page=%v focus=%q",
			state.Page, state.Focus)
	}
	view.goBack()
	tester.Frame()

	// 7. Install Ocean from normal Settings/plugins (not from ThemePicker) -> shows success modal with [前往外观设置], keeps Twilight active!
	state.Navigate(app.Route{Page: app.Settings, Section: "plugins"})
	view.pluginsUI.view = pluginsViewDiscover
	view.pluginsUI.filter = plugin.ThemeKind
	view.pluginsUI.returnToThemePicker = false
	tester.Frame()
	view.State.Focus = pluginsCatalogFocus("demo/ocean-theme")
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	view.State.Focus = pluginInstallConfirmBtn
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	drain(func() bool { return view.pluginsUI.install.phase == pluginInstallSuccess })
	tester.Frame()
	if state.Page != app.PluginInstallDialog || state.Focus != pluginInstallOpenAppearanceBtn || view.Theme.ID != "twilight" {
		t.Fatalf("expected normal theme install success focused on OpenAppearance keeping twilight, got page=%v focus=%q theme=%q",
			state.Page, state.Focus, view.Theme.ID)
	}
	// Click [前往外观设置] -> opens ThemePicker focused on ocean while twilight stays active!
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	if state.Page != app.ThemePicker || state.Focus != themePickerOptionID("ocean") || view.Theme.ID != "twilight" {
		t.Fatalf("OpenAppearance did not focus ocean while keeping twilight: page=%v focus=%q theme=%q",
			state.Page, state.Focus, view.Theme.ID)
	}
	view.goBack()
	tester.Frame()

	// 8. Uninstall active Twilight from Settings/plugins -> restores MikuTV Dark without touching sample-source entry!
	state.Navigate(app.Route{Page: app.Settings, Section: "plugins"})
	view.pluginsUI.view = pluginsViewInstalled
	tester.Frame()
	view.State.Focus = pluginsInstalledFocus("twilight")
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	view.State.Focus = pluginsDetailRemove
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	if state.Page != app.PluginRemoveDialog || state.Focus != pluginRemoveConfirmBtn {
		t.Fatalf("expected PluginRemoveDialog for active theme, got page=%v focus=%q", state.Page, state.Focus)
	}
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	if view.Theme.ID != theme.BaseMikuTVDark || appearance.ActiveThemeID() != theme.BaseMikuTVDark {
		t.Fatalf("uninstalling active theme must restore MikuTV Dark: view=%q active=%q", view.Theme.ID, appearance.ActiveThemeID())
	}
	if state.CurrentEntry != "sample-source" || len(state.Entries()) != 1 {
		t.Fatalf("uninstalling theme affected content source entries: current=%q entries=%+v", state.CurrentEntry, state.Entries())
	}
}
