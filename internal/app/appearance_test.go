package app

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yuchanns/MikuTV/internal/plugin"
	"github.com/yuchanns/MikuTV/internal/theme"
)

func makeThemeAppTestZip(t *testing.T, id, name, version, themeJSON string) []byte {
	t.Helper()
	return makeAppTestZip(t, map[string]string{
		"plugin.json": fmt.Sprintf(`{
			"id": %q,
			"name": %q,
			"description": "Test theme %s",
			"kind": "theme",
			"version": %q,
			"apiVersion": 1,
			"minAppVersion": "0.1.0",
			"license": "MIT",
			"theme": "theme.json"
		}`, id, name, name, version),
		"theme.json": themeJSON,
	})
}

const twilightThemeV1JSON = `{
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

const twilightThemeV2JSON = `{
	"schemaVersion": 1,
	"base": "mikutv-dark",
	"appearance": "dark",
	"colors": {
		"surface": {
			"canvas": "#121020",
			"sidebar": "#0D0B18",
			"panel": "#1D1930",
			"raised": "#282240",
			"input": "#18152A"
		},
		"text": {
			"primary": "#F5F0FC",
			"secondary": "#BDB3CD",
			"muted": "#A59BB8"
		},
		"accent": {
			"primary": "#C9ADFF",
			"soft": "#E3D4FF",
			"strong": "#A987E0",
			"foreground": "#1D102D"
		},
		"selected": {
			"background": "#3A2850",
			"foreground": "#E3D4FF",
			"focusBackground": "#6B498E",
			"focusForeground": "#FFFFFF"
		}
	}
}`

const oceanThemeJSON = `{
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
		"focus": {
			"background": "#EEFAFF",
			"foreground": "#112935",
			"ring": "#F4F6FA"
		},
		"selected": {
			"background": "#153D53",
			"foreground": "#B2E5FF",
			"focusBackground": "#245C7A",
			"focusForeground": "#FFFFFF"
		},
		"decoration": {
			"ambient": "#60BFEC20",
			"cardStart": "#153D53",
			"cardEnd": "#08151C"
		}
	}
}`

func TestAppearanceThemeLifecyclePersistenceUpdateRollbackAndFallback(t *testing.T) {
	twilightV1 := makeThemeAppTestZip(t, "twilight", "Twilight Purple", "1.0.0", twilightThemeV1JSON)
	twilightV1SHA := fmt.Sprintf("%x", sha256.Sum256(twilightV1))
	twilightV1Inspected, err := plugin.InspectPackage(twilightV1, "0.1.0")
	if err != nil || !twilightV1Inspected.Compatibility.Compatible {
		t.Fatalf("inspect twilight v1: err=%v compat=%+v", err, twilightV1Inspected.Compatibility)
	}
	twilightV1Item := plugin.CatalogItem{
		Repository:    "demo/twilight-theme",
		Release:       plugin.ReleaseAsset{ReleaseID: 1, TagName: "v1.0.0", AssetID: 11, AssetName: "plugin.zip", DownloadURL: "https://example.com/twilight-v1.zip", SHA256: twilightV1SHA},
		Manifest:      twilightV1Inspected.Manifest,
		Swatches:      twilightV1Inspected.Swatches,
		Compatibility: twilightV1Inspected.Compatibility,
	}

	oceanV1 := makeThemeAppTestZip(t, "ocean", "Ocean Blue", "1.0.0", oceanThemeJSON)
	oceanV1SHA := fmt.Sprintf("%x", sha256.Sum256(oceanV1))
	oceanV1Inspected, err := plugin.InspectPackage(oceanV1, "0.1.0")
	if err != nil || !oceanV1Inspected.Compatibility.Compatible {
		t.Fatalf("inspect ocean v1: err=%v compat=%+v", err, oceanV1Inspected.Compatibility)
	}
	oceanV1Item := plugin.CatalogItem{
		Repository:    "demo/ocean-theme",
		Release:       plugin.ReleaseAsset{ReleaseID: 2, TagName: "v1.0.0", AssetID: 12, AssetName: "plugin.zip", DownloadURL: "https://example.com/ocean-v1.zip", SHA256: oceanV1SHA},
		Manifest:      oceanV1Inspected.Manifest,
		Swatches:      oceanV1Inspected.Swatches,
		Compatibility: oceanV1Inspected.Compatibility,
	}

	twilightBadContrast := makeThemeAppTestZip(t, "twilight", "Twilight Purple", "1.1.0", `{
		"schemaVersion": 1,
		"base": "mikutv-dark",
		"appearance": "dark",
		"colors": { "text": { "primary": "#121218" } }
	}`)
	twilightBadSHA := fmt.Sprintf("%x", sha256.Sum256(twilightBadContrast))
	// Construct a pinned offer claiming compatibility to verify StageAndUpdate's authoritative contrast gate!
	twilightBadOffer := PluginUpdateOffer{
		Item: plugin.CatalogItem{
			Repository:    "demo/twilight-theme",
			Release:       plugin.ReleaseAsset{ReleaseID: 3, TagName: "v1.1.0", AssetID: 13, AssetName: "plugin.zip", DownloadURL: "https://example.com/twilight-bad.zip", SHA256: twilightBadSHA},
			Manifest:      plugin.Manifest{ID: "twilight", Name: "Twilight Purple", Kind: plugin.ThemeKind, Version: "1.1.0", APIVersion: 1, MinAppVersion: "0.1.0"},
			Compatibility: plugin.Compatibility{Compatible: true, Code: plugin.CompatibleOK},
		},
	}

	twilightV2 := makeThemeAppTestZip(t, "twilight", "Twilight Purple", "1.2.0", twilightThemeV2JSON)
	twilightV2SHA := fmt.Sprintf("%x", sha256.Sum256(twilightV2))
	twilightV2Inspected, err := plugin.InspectPackage(twilightV2, "0.1.0")
	if err != nil || !twilightV2Inspected.Compatibility.Compatible {
		t.Fatalf("inspect twilight v2: err=%v compat=%+v", err, twilightV2Inspected.Compatibility)
	}
	twilightV2Item := plugin.CatalogItem{
		Repository:    "demo/twilight-theme",
		Release:       plugin.ReleaseAsset{ReleaseID: 4, TagName: "v1.2.0", AssetID: 14, AssetName: "plugin.zip", DownloadURL: "https://example.com/twilight-v2.zip", SHA256: twilightV2SHA},
		Manifest:      twilightV2Inspected.Manifest,
		Swatches:      twilightV2Inspected.Swatches,
		Compatibility: twilightV2Inspected.Compatibility,
	}

	fetcher := &installTestFetcher{
		resolved: map[string]plugin.CatalogItem{
			"demo/twilight-theme": twilightV1Item,
			"demo/ocean-theme":    oceanV1Item,
		},
		zips: map[string][]byte{
			"https://example.com/twilight-v1.zip":  twilightV1,
			"https://example.com/ocean-v1.zip":     oceanV1,
			"https://example.com/twilight-bad.zip": twilightBadContrast,
			"https://example.com/twilight-v2.zip":  twilightV2,
		},
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

	var appliedThemes []theme.Theme
	var cat *PluginCatalog
	appearance := NewAppearanceSettings(
		pkgStore,
		nil,
		func(th theme.Theme) { appliedThemes = append(appliedThemes, th) },
		func(activeID string) {
			if cat != nil {
				cat.SyncActiveTheme(activeID)
			}
		},
		func() {},
	)

	cat = NewPluginCatalog(
		[]InstalledPlugin{{ID: "sample-source", Name: "SampleSource", Kind: plugin.SourceKind, Version: "0.1.0", Bundled: true, Enabled: true}},
		fetcher,
		nil,
		func(fn func()) { queue <- fn },
		func() {},
	)
	t.Cleanup(cat.Close)

	sourceTouched := false
	cat.SetInstaller(pkgStore, "0.1.0", func(pkg plugin.InstalledPackage) error {
		if pkg.Theme == nil {
			sourceTouched = true
			return errors.New("unexpected non-theme install")
		}
		appearance.RegisterTheme(pkg.Theme)
		return nil
	})
	cat.SetUpdateHooks(PluginUpdateHooks{
		BeforeSourceUpdate: func(string) { sourceTouched = true },
		ApplySourceUpdate:  func(*plugin.Source, plugin.InstalledReceipt) error { sourceTouched = true; return nil },
		ApplyThemeUpdate: func(pkg *plugin.ThemePackage, _ plugin.InstalledReceipt) error {
			return appearance.UpdateTheme(pkg)
		},
	})
	cat.SetLifecycleHooks(PluginLifecycleHooks{
		DisableSource: func(string) { sourceTouched = true },
		EnableSource:  func(string) error { sourceTouched = true; return nil },
		RemoveSource:  func(string, bool) { sourceTouched = true },
		EnableTheme: func(id string) error {
			_, err := appearance.SelectTheme(id)
			return err
		},
		DisableTheme: func(id string) { appearance.DisableTheme(id) },
		RemoveTheme:  func(id string) { appearance.RemoveTheme(id) },
	})

	// 1. Install two themes: neither auto-applies! Built-in remains active.
	done := false
	cat.Install(twilightV1Item, func(out PluginInstallOutcome) {
		if out.Err != nil {
			t.Fatalf("install twilight: %v", out.Err)
		}
		done = true
	})
	drain(func() bool { return done })
	done = false
	cat.Install(oceanV1Item, func(out PluginInstallOutcome) {
		if out.Err != nil {
			t.Fatalf("install ocean: %v", out.Err)
		}
		done = true
	})
	drain(func() bool { return done })

	if appearance.ActiveThemeID() != theme.BaseMikuTVDark || len(appliedThemes) != 0 {
		t.Fatalf("installing themes must NOT auto-apply: active=%q applied=%d", appearance.ActiveThemeID(), len(appliedThemes))
	}
	if snap := appearance.Snapshot(); len(snap.Options) != 3 {
		t.Fatalf("expected 3 theme options (builtin + 2 installed), got %d", len(snap.Options))
	}
	if inst, ok := cat.FindInstalled("twilight"); !ok || inst.Enabled {
		t.Fatalf("newly installed theme should be inactive: %+v", inst)
	}

	// 2. Select Twilight -> atomic apply, global single selection synced to PluginCatalog, persisted!
	if _, err := appearance.SelectTheme("twilight"); err != nil {
		t.Fatalf("SelectTheme twilight: %v", err)
	}
	if appearance.ActiveThemeID() != "twilight" || appearance.ActiveTheme().Colors.Accent.Primary != 0xbda0ecff {
		t.Fatalf("twilight not active: id=%q accent=%08x", appearance.ActiveThemeID(), appearance.ActiveTheme().Colors.Accent.Primary)
	}
	twInst, _ := cat.FindInstalled("twilight")
	ocInst, _ := cat.FindInstalled("ocean")
	if !twInst.Enabled || ocInst.Enabled {
		t.Fatalf("single-selection sync failed: twilight.Enabled=%v ocean.Enabled=%v", twInst.Enabled, ocInst.Enabled)
	}

	// 3. Toggle motion preferences and verify cold-restart persistence of theme + motion preferences.
	if err := appearance.SetHoverPreview(false); err != nil {
		t.Fatalf("SetHoverPreview: %v", err)
	}
	if err := appearance.SetReduceMotion(true); err != nil {
		t.Fatalf("SetReduceMotion: %v", err)
	}
	loadedPkgs, loadErrs := pkgStore.LoadAll(context.Background(), "0.1.0")
	if len(loadErrs) != 0 || len(loadedPkgs) != 2 {
		t.Fatalf("LoadAll after install: loaded=%d errs=%v", len(loadedPkgs), loadErrs)
	}
	var reloadedThemes []*plugin.ThemePackage
	for _, pkg := range loadedPkgs {
		if pkg.Theme != nil {
			reloadedThemes = append(reloadedThemes, pkg.Theme)
		}
	}
	restarted := NewAppearanceSettings(pkgStore, reloadedThemes, nil, nil, nil)
	if restarted.ActiveThemeID() != "twilight" || restarted.HoverPreview() || !restarted.ReduceMotion() {
		t.Fatalf("cold restart lost appearance state: active=%q hover=%v reduce=%v",
			restarted.ActiveThemeID(), restarted.HoverPreview(), restarted.ReduceMotion())
	}

	// 4. Failed contrast update on active theme rejects cleanly, cleans staging, preserves v1.0.0 & live colors!
	var badOut PluginUpdateOutcome
	badDone := false
	cat.Update("twilight", twilightBadOffer, func(out PluginUpdateOutcome) {
		badOut = out
		badDone = true
	})
	drain(func() bool { return badDone })
	if badOut.Err == nil || !errors.Is(badOut.Err, plugin.ErrIncompatible) {
		t.Fatalf("expected ErrIncompatible for low-contrast theme update, got %+v", badOut)
	}
	if appearance.ActiveTheme().Colors.Accent.Primary != 0xbda0ecff {
		t.Fatal("failed update mutated active theme")
	}
	if cur, _ := cat.FindInstalled("twilight"); cur.Version != "1.0.0" || cur.SHA256 != twilightV1SHA {
		t.Fatalf("failed update corrupted installed theme: %+v", cur)
	}

	// 5. Valid v1.2.0 update on active theme atomically swaps live theme WITHOUT touching content sources!
	fetcher.mu.Lock()
	fetcher.resolved["demo/twilight-theme"] = twilightV2Item
	fetcher.mu.Unlock()
	checked := false
	cat.CheckPluginUpdate("twilight", func(offer *PluginUpdateOffer, err error) {
		if err != nil || offer == nil || !offer.Applicable() {
			t.Fatalf("CheckPluginUpdate twilight v2 failed: offer=%+v err=%v", offer, err)
		}
		checked = true
	})
	drain(func() bool { return checked })
	twCur, _ := cat.FindInstalled("twilight")
	var okOut PluginUpdateOutcome
	okDone := false
	cat.Update("twilight", *twCur.AvailableUpdate, func(out PluginUpdateOutcome) {
		okOut = out
		okDone = true
	})
	drain(func() bool { return okDone })
	if okOut.Err != nil || okOut.Receipt.Version != "1.2.0" {
		t.Fatalf("valid twilight update failed: %+v", okOut)
	}
	if appearance.ActiveTheme().Colors.Accent.Primary != 0xc9adffff {
		t.Fatalf("active theme did not atomically hot-swap to v1.2.0 colors: %08x", appearance.ActiveTheme().Colors.Accent.Primary)
	}

	// 6. Enabling ocean via PluginCatalog switches global single selection from twilight -> ocean.
	if err := cat.SetEnabled("ocean", true); err != nil {
		t.Fatalf("SetEnabled ocean: %v", err)
	}
	if appearance.ActiveThemeID() != "ocean" {
		t.Fatalf("expected ocean active, got %q", appearance.ActiveThemeID())
	}
	twInst, _ = cat.FindInstalled("twilight")
	ocInst, _ = cat.FindInstalled("ocean")
	if twInst.Enabled || !ocInst.Enabled {
		t.Fatalf("single-selection violated after enabling ocean: tw=%v oc=%v", twInst.Enabled, ocInst.Enabled)
	}

	// 7. Disabling active ocean restores built-in baseline MikuTV Dark.
	if err := cat.SetEnabled("ocean", false); err != nil {
		t.Fatalf("SetEnabled ocean false: %v", err)
	}
	if appearance.ActiveThemeID() != theme.BaseMikuTVDark {
		t.Fatalf("disabling active theme must restore baseline, got %q", appearance.ActiveThemeID())
	}

	// 8. Re-select twilight then uninstall active twilight -> restores MikuTV Dark & removes from picker!
	if _, err := appearance.SelectTheme("twilight"); err != nil {
		t.Fatal(err)
	}
	if err := cat.Remove("twilight", false); err != nil {
		t.Fatalf("Remove active twilight: %v", err)
	}
	if appearance.ActiveThemeID() != theme.BaseMikuTVDark || len(appearance.Snapshot().Options) != 2 {
		t.Fatalf("removing active theme did not restore baseline/prune picker: active=%q opts=%d",
			appearance.ActiveThemeID(), len(appearance.Snapshot().Options))
	}

	// 9. Startup fallback when persisted active theme is missing/corrupted on disk.
	if err := pkgStore.SaveActiveTheme("ocean"); err != nil {
		t.Fatal(err)
	}
	// Corrupt ocean's plugin.zip on disk.
	if err := os.WriteFile(filepath.Join(pkgRoot, "packages", "ocean", "plugin.zip"), []byte("corrupted"), 0o600); err != nil {
		t.Fatal(err)
	}
	corruptPkgs, corruptErrs := pkgStore.LoadAll(context.Background(), "0.1.0")
	if len(corruptPkgs) != 0 || len(corruptErrs) != 1 {
		t.Fatalf("expected corrupted theme rejected on LoadAll: pkgs=%d errs=%v", len(corruptPkgs), corruptErrs)
	}
	fallback := NewAppearanceSettings(pkgStore, nil, nil, nil, nil)
	if fallback.ActiveThemeID() != theme.BaseMikuTVDark || fallback.ActiveTheme().ID != theme.BaseMikuTVDark {
		t.Fatalf("missing/corrupted persisted theme must fall back to built-in baseline, got %+v", fallback.ActiveTheme())
	}

	// 10. Content-source lifecycle hooks were NEVER touched across the entire theme lifecycle!
	if sourceTouched {
		t.Fatal("theme lifecycle wrongly invoked content-source hooks")
	}
}
