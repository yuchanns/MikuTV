package app

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/yuchanns/MikuTV/internal/plugin"
)

func TestPluginCatalogDisableEnableRemoveAndThemeLifecycleIsolation(t *testing.T) {
	v1Zip := makeAppTestZip(t, map[string]string{
		"plugin.json": `{
			"id": "demo-media",
			"name": "Demo Media",
			"description": "v1",
			"kind": "source",
			"version": "1.0.0",
			"apiVersion": 1,
			"minAppVersion": "0.1.0",
			"main": "index.js",
			"permissions": {"network": ["api.media.example"]},
			"capabilities": ["catalog.browse"]
		}`,
		"index.js": `export function browse() { return { collections: [] }; }`,
	})
	v1Item := plugin.CatalogItem{
		Repository:    "demo/media-source",
		Release:       plugin.ReleaseAsset{ReleaseID: 1, TagName: "v1.0.0", AssetID: 11, AssetName: "plugin.zip", DownloadURL: "https://example.com/v1.zip", SHA256: fmt.Sprintf("%x", sha256.Sum256(v1Zip))},
		Manifest:      plugin.Manifest{ID: "demo-media", Name: "Demo Media", Kind: plugin.SourceKind, Version: "1.0.0", APIVersion: 1, MinAppVersion: "0.1.0"},
		Compatibility: plugin.Compatibility{Compatible: true, Code: plugin.CompatibleOK},
	}

	fetcher := &installTestFetcher{
		resolved: map[string]plugin.CatalogItem{"demo/media-source": v1Item},
		zips:     map[string][]byte{"https://example.com/v1.zip": v1Zip},
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
				t.Fatal("timed out waiting for async callback")
			}
		}
	}

	pkgStore := plugin.NewPackageStore(t.TempDir())
	cat := NewPluginCatalog(
		[]InstalledPlugin{
			{ID: "sample-source", Name: "SampleSource", Kind: plugin.SourceKind, Version: "0.1.0", Bundled: true, Enabled: true},
			{ID: "tokyonight", Name: "TokyoNight", Kind: plugin.ThemeKind, Version: "1.0.0", Repository: "demo/tokyonight", Bundled: false, Enabled: true},
		},
		fetcher,
		nil,
		func(fn func()) { queue <- fn },
		func() {},
	)
	cat.SetInstaller(pkgStore, "0.1.0", func(plugin.InstalledPackage) error { return nil })

	var disabledSources []string
	var enabledSources []string
	var removedSources []string
	var removedClearData []bool
	var disabledThemes []string
	var removedThemes []string
	cat.SetLifecycleHooks(PluginLifecycleHooks{
		DisableSource: func(id string) { disabledSources = append(disabledSources, id) },
		EnableSource:  func(id string) error { enabledSources = append(enabledSources, id); return nil },
		RemoveSource: func(id string, clearData bool) {
			removedSources = append(removedSources, id)
			removedClearData = append(removedClearData, clearData)
		},
		DisableTheme: func(id string) { disabledThemes = append(disabledThemes, id) },
		RemoveTheme:  func(id string) { removedThemes = append(removedThemes, id) },
	})
	t.Cleanup(cat.Close)

	// Install external media source first.
	done := false
	cat.Install(v1Item, func(out PluginInstallOutcome) {
		if out.Err != nil {
			t.Fatalf("install failed: %v", out.Err)
		}
		done = true
	})
	drain(func() bool { return done })

	// 1. Bundled plugin cannot be uninstalled, only disabled & re-enabled with persistence!
	if err := cat.Remove("sample-source", false); !errors.Is(err, plugin.ErrIncompatible) {
		t.Fatalf("expected ErrIncompatible uninstalling bundled plugin, got %v", err)
	}
	if err := cat.SetEnabled("sample-source", false); err != nil {
		t.Fatalf("disable bundled sample-source: %v", err)
	}
	if inst, ok := cat.FindInstalled("sample-source"); !ok || inst.Enabled {
		t.Fatalf("expected sample-source disabled, got %+v", inst)
	}
	disabledPersisted, err := pkgStore.LoadDisabledBundled()
	if err != nil || len(disabledPersisted) != 1 || disabledPersisted[0] != "sample-source" {
		t.Fatalf("expected sample-source persisted in disabled list, got %+v err=%v", disabledPersisted, err)
	}
	if err := cat.SetEnabled("sample-source", true); err != nil {
		t.Fatalf("re-enable bundled sample-source: %v", err)
	}
	if inst, ok := cat.FindInstalled("sample-source"); !ok || !inst.Enabled {
		t.Fatalf("expected sample-source re-enabled, got %+v", inst)
	}

	// 2. External source disable & uninstall preserves account data (clearData=false).
	if err := cat.SetEnabled("demo-media", false); err != nil {
		t.Fatalf("disable external source: %v", err)
	}
	if err := cat.Remove("demo-media", false); err != nil {
		t.Fatalf("remove external source: %v", err)
	}
	if _, ok := cat.FindInstalled("demo-media"); ok {
		t.Fatal("removed plugin still present in installed list")
	}
	if loaded, failures := pkgStore.LoadAll(context.Background(), "0.1.0"); len(loaded) != 0 || len(failures) != 0 {
		t.Fatalf("expected empty package store after removal, got loaded=%d failures=%v", len(loaded), failures)
	}
	if len(removedSources) != 1 || removedSources[0] != "demo-media" || removedClearData[0] {
		t.Fatalf("unexpected source removal callback: ids=%v clearData=%v", removedSources, removedClearData)
	}

	// 3. Theme disable & remove NEVER invokes source teardown hooks!
	prevSourceDisables := len(disabledSources)
	prevSourceRemoves := len(removedSources)
	if err := dispatchPluginToggle(cat.lifecycleHooks, plugin.ThemeKind, "tokyonight", false); err != nil {
		t.Fatalf("disable theme dispatch: %v", err)
	}
	dispatchPluginRemove(cat.lifecycleHooks, plugin.ThemeKind, "tokyonight", false)
	if len(disabledSources) != prevSourceDisables || len(removedSources) != prevSourceRemoves {
		t.Fatal("theme disable/remove wrongly triggered content-source hooks")
	}
	if len(disabledThemes) != 1 || disabledThemes[0] != "tokyonight" || len(removedThemes) != 1 || removedThemes[0] != "tokyonight" {
		t.Fatalf("theme hooks not invoked: disabled=%v removed=%v", disabledThemes, removedThemes)
	}
}
