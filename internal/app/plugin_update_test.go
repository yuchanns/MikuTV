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

func TestPluginCatalogUpdateCheckPermissionsKindGuardRollbackAndDispatch(t *testing.T) {
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
	v1SHA := fmt.Sprintf("%x", sha256.Sum256(v1Zip))
	v1Item := plugin.CatalogItem{
		Repository:    "demo/media-source",
		Release:       plugin.ReleaseAsset{ReleaseID: 1, TagName: "v1.0.0", AssetID: 11, AssetName: "plugin.zip", DownloadURL: "https://example.com/v1.zip", SHA256: v1SHA},
		Manifest:      plugin.Manifest{ID: "demo-media", Name: "Demo Media", Description: "v1", Kind: plugin.SourceKind, Version: "1.0.0", APIVersion: 1, MinAppVersion: "0.1.0"},
		Permissions:   []string{"api.media.example"},
		Compatibility: plugin.Compatibility{Compatible: true, Code: plugin.CompatibleOK},
	}

	v2Zip := makeAppTestZip(t, map[string]string{
		"plugin.json": `{
			"id": "demo-media",
			"name": "Demo Media",
			"description": "v2 expanded perms",
			"kind": "source",
			"version": "1.1.0",
			"apiVersion": 1,
			"minAppVersion": "0.1.0",
			"main": "index.js",
			"permissions": {"network": ["api.media.example", "images.media.example"]},
			"capabilities": ["catalog.browse"]
		}`,
		"index.js": `export function browse() { return { collections: [] }; }`,
	})
	v2SHA := fmt.Sprintf("%x", sha256.Sum256(v2Zip))
	v2Item := plugin.CatalogItem{
		Repository:    "demo/media-source",
		Release:       plugin.ReleaseAsset{ReleaseID: 2, TagName: "v1.1.0", AssetID: 12, AssetName: "plugin.zip", DownloadURL: "https://example.com/v2.zip", SHA256: v2SHA},
		Manifest:      plugin.Manifest{ID: "demo-media", Name: "Demo Media", Description: "v2 expanded perms", Kind: plugin.SourceKind, Version: "1.1.0", APIVersion: 1, MinAppVersion: "0.1.0"},
		Permissions:   []string{"api.media.example", "images.media.example"},
		Compatibility: plugin.Compatibility{Compatible: true, Code: plugin.CompatibleOK},
	}

	kindChangeItem := plugin.CatalogItem{
		Repository:    "demo/media-source",
		Release:       plugin.ReleaseAsset{ReleaseID: 3, TagName: "v2.0.0", AssetID: 13, AssetName: "plugin.zip", DownloadURL: "https://example.com/v2theme.zip"},
		Manifest:      plugin.Manifest{ID: "demo-media", Name: "Demo Media", Kind: plugin.ThemeKind, Version: "2.0.0", APIVersion: 1, MinAppVersion: "0.1.0"},
		Compatibility: plugin.Compatibility{Compatible: false, Code: plugin.IncompatibleThemeUnavailable},
	}

	fetcher := &installTestFetcher{
		resolved: map[string]plugin.CatalogItem{"demo/media-source": v1Item},
		zips: map[string][]byte{
			"https://example.com/v1.zip": v1Zip,
			"https://example.com/v2.zip": v2Zip,
		},
	}
	queue := make(chan func(), 64)
	drain := func(cond func() bool) {
		t.Helper()
		deadline := time.After(2 * time.Second)
		for !cond() {
			select {
			case fn := <-queue:
				fn()
			case <-deadline:
				t.Fatal("timed out waiting for async update callback")
			}
		}
	}

	pkgStore := plugin.NewPackageStore(t.TempDir())
	cat := NewPluginCatalog(
		[]InstalledPlugin{{ID: "sample-source", Name: "SampleSource", Kind: plugin.SourceKind, Version: "0.1.0", Bundled: true, Enabled: true}},
		fetcher,
		nil,
		func(fn func()) { queue <- fn },
		func() {},
	)
	cat.SetInstaller(pkgStore, "0.1.0", func(plugin.InstalledPackage) error { return nil })
	var beforeStoppedID string
	var appliedVersion string
	var themeHookCalled bool
	cat.SetUpdateHooks(PluginUpdateHooks{
		BeforeSourceUpdate: func(id string) { beforeStoppedID = id },
		ApplySourceUpdate: func(src *plugin.Source, receipt plugin.InstalledReceipt) error {
			appliedVersion = src.Manifest().Version + "@" + receipt.SHA256
			return nil
		},
		ApplyThemeUpdate: func(*plugin.ThemePackage, plugin.InstalledReceipt) error {
			themeHookCalled = true
			return nil
		},
	})
	t.Cleanup(cat.Close)

	// Install v1 first.
	installed := false
	cat.Install(v1Item, func(out PluginInstallOutcome) {
		if out.Err != nil {
			t.Fatalf("install v1 failed: %v", out.Err)
		}
		installed = true
	})
	drain(func() bool { return installed })

	// 1. CheckUpdates detects kind-change release and marks IncompatibleKind = true.
	fetcher.mu.Lock()
	fetcher.resolved["demo/media-source"] = kindChangeItem
	fetcher.mu.Unlock()
	checked := false
	cat.CheckUpdates(func(found, total int, err error) {
		if err != nil || total != 1 || found != 1 {
			t.Fatalf("expected 1 checked external plugin (skipping bundled), got found=%d total=%d err=%v", found, total, err)
		}
		checked = true
	})
	drain(func() bool { return checked })
	inst, ok := cat.FindInstalled("demo-media")
	if !ok || inst.AvailableUpdate == nil || !inst.AvailableUpdate.IncompatibleKind || inst.AvailableUpdate.Applicable() {
		t.Fatalf("expected kind-change offer marked incompatible, got %+v", inst.AvailableUpdate)
	}
	// Attempting to apply kind-change update is rejected immediately with ErrKindChanged.
	var kindErr error
	cat.Update("demo-media", *inst.AvailableUpdate, func(out PluginUpdateOutcome) { kindErr = out.Err })
	if !errors.Is(kindErr, plugin.ErrKindChanged) {
		t.Fatalf("expected ErrKindChanged, got %v", kindErr)
	}

	// 2. CheckUpdates detects valid v1.1.0 with expanded permission ("images.media.example").
	fetcher.mu.Lock()
	fetcher.resolved["demo/media-source"] = v2Item
	fetcher.mu.Unlock()
	checked = false
	cat.CheckUpdates(func(found, total int, err error) {
		if err != nil || found != 1 || total != 1 {
			t.Fatalf("CheckUpdates v2 failed: found=%d total=%d err=%v", found, total, err)
		}
		checked = true
	})
	drain(func() bool { return checked })
	inst, _ = cat.FindInstalled("demo-media")
	if inst.AvailableUpdate == nil || !inst.AvailableUpdate.Applicable() || !inst.AvailableUpdate.HasPermissionExpansion() {
		t.Fatalf("expected applicable update with expanded permission, got %+v", inst.AvailableUpdate)
	}
	if len(inst.AvailableUpdate.AddedPermissions) != 1 || inst.AvailableUpdate.AddedPermissions[0] != "images.media.example" {
		t.Fatalf("unexpected AddedPermissions: %+v", inst.AvailableUpdate.AddedPermissions)
	}
	offer := *inst.AvailableUpdate

	// 3. Download failure preserves v1.0.0 and does NOT invoke BeforeSourceUpdate!
	fetcher.mu.Lock()
	fetcher.dlErr = errors.New("connection dropped")
	fetcher.mu.Unlock()
	var failOut PluginUpdateOutcome
	failDone := false
	cat.Update("demo-media", offer, func(out PluginUpdateOutcome) {
		failOut = out
		failDone = true
	})
	drain(func() bool { return failDone })
	if failOut.Err == nil || !failOut.Retryable || beforeStoppedID != "" {
		t.Fatalf("failed update should be retryable without stopping active source: out=%+v stopped=%q", failOut, beforeStoppedID)
	}
	if cur, _ := cat.FindInstalled("demo-media"); cur.Version != "1.0.0" || cur.SHA256 != v1SHA {
		t.Fatalf("failed update corrupted installed v1: %+v", cur)
	}

	// 4. Retry succeeds: pins v1.1.0 + SHA256, calls BeforeSourceUpdate + ApplySourceUpdate, clears AvailableUpdate!
	fetcher.mu.Lock()
	fetcher.dlErr = nil
	fetcher.mu.Unlock()
	var okOut PluginUpdateOutcome
	okDone := false
	cat.Update("demo-media", offer, func(out PluginUpdateOutcome) {
		okOut = out
		okDone = true
	})
	drain(func() bool { return okDone })
	if okOut.Err != nil || okOut.Receipt.Version != "1.1.0" || okOut.Receipt.SHA256 != v2SHA {
		t.Fatalf("update failed: %+v", okOut)
	}
	if beforeStoppedID != "demo-media" || appliedVersion != "1.1.0@"+v2SHA || themeHookCalled {
		t.Fatalf("unexpected update hook dispatch: stopped=%q applied=%q theme=%v", beforeStoppedID, appliedVersion, themeHookCalled)
	}
	if cur, _ := cat.FindInstalled("demo-media"); cur.Version != "1.1.0" || cur.AvailableUpdate != nil || len(cur.Permissions) != 2 {
		t.Fatalf("installed record not updated cleanly: %+v", cur)
	}
	loaded, failures := pkgStore.LoadAll(context.Background(), "0.1.0")
	if len(failures) != 0 || len(loaded) != 1 || loaded[0].Receipt.Version != "1.1.0" {
		t.Fatalf("persisted store did not update cleanly: loaded=%+v failures=%v", loaded, failures)
	}

	// 5. Kind-dispatched callback guarantee: theme updates NEVER invoke BeforeSourceUpdate or ApplySourceUpdate!
	sourceStopCalled := false
	sourceApplyCalled := false
	themeAppliedID := ""
	err := dispatchPackageUpdate(
		PluginUpdateHooks{
			BeforeSourceUpdate: func(string) { sourceStopCalled = true },
			ApplySourceUpdate:  func(*plugin.Source, plugin.InstalledReceipt) error { sourceApplyCalled = true; return nil },
			ApplyThemeUpdate:   func(_ *plugin.ThemePackage, r plugin.InstalledReceipt) error { themeAppliedID = r.ID; return nil },
		},
		plugin.InstalledPackage{
			Receipt: plugin.InstalledReceipt{ID: "tokyonight", Kind: plugin.ThemeKind, Version: "1.2.0"},
		},
	)
	if err != nil || sourceStopCalled || sourceApplyCalled || themeAppliedID != "tokyonight" {
		t.Fatalf("theme update wrongly entered source stop/apply path: err=%v stop=%v apply=%v theme=%q",
			err, sourceStopCalled, sourceApplyCalled, themeAppliedID)
	}
}
