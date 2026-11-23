package plugin

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestParseRepositoryAndVersionTagMatching(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"demo/media-source", "demo/media-source", true},
		{"https://github.com/demo/media-source", "demo/media-source", true},
		{"https://github.com/demo/media-source.git/", "demo/media-source", true},
		{"github.com/demo/my.plugin_1", "demo/my.plugin_1", true},
		{"not-a-repo", "", false},
		{"demo/a/b", "", false},
		{"../evil/repo", "", false},
	}
	for _, tc := range cases {
		got, ok := ParseRepository(tc.in)
		if ok != tc.ok || got != tc.want {
			t.Errorf("ParseRepository(%q) = (%q, %v), want (%q, %v)", tc.in, got, ok, tc.want, tc.ok)
		}
	}
	if !ReleaseVersionMatches("v1.2.3", "1.2.3") || !ReleaseVersionMatches("1.2.3", "1.2.3") {
		t.Fatal("ReleaseVersionMatches rejected matching version")
	}
	if ReleaseVersionMatches("v1.2.4", "1.2.3") {
		t.Fatal("ReleaseVersionMatches accepted mismatched version")
	}
}

func TestPackageStoreStageInstallReloadConflictCancelAndCleanup(t *testing.T) {
	root := t.TempDir()
	store := NewPackageStore(root)

	validZip := buildTestPluginZip(t, map[string]string{
		"plugin.json": `{
			"id": "demo-media",
			"name": "Demo Media",
			"description": "Videos",
			"kind": "source",
			"version": "1.0.0",
			"apiVersion": 1,
			"minAppVersion": "0.1.0",
			"main": "index.js",
			"permissions": {"network": ["media.example.com"]},
			"capabilities": ["catalog.browse"]
		}`,
		"index.js": `export function browse() { return { collections: [] }; }`,
	})
	digestHex := fmt.Sprintf("%x", sha256.Sum256(validZip))
	item := CatalogItem{
		Repository: "demo/media-source",
		Release:    ReleaseAsset{ReleaseID: 10, TagName: "v1.0.0", AssetID: 20, AssetName: "plugin.zip", SHA256: digestHex},
		Manifest:   Manifest{ID: "demo-media", Name: "Demo Media", Kind: SourceKind, Version: "1.0.0", APIVersion: 1, MinAppVersion: "0.1.0"},
	}

	// 1. Digest mismatch cleans up staging dir!
	badDigestItem := item
	badDigestItem.Release.SHA256 = "0000000000000000000000000000000000000000000000000000000000000000"
	if _, _, err := store.StageAndInstall(context.Background(), validZip, badDigestItem, "0.1.0", nil); !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("expected ErrDigestMismatch, got %v", err)
	}
	assertStagingEmpty(t, root)

	// 2. Same-ID conflict against bundled plugin (bound repo "") is rejected!
	if _, _, err := store.StageAndInstall(context.Background(), validZip, item, "0.1.0", map[string]string{"demo-media": ""}); !errors.Is(err, ErrSourceConflict) {
		t.Fatalf("expected ErrSourceConflict against bundled id, got %v", err)
	}
	assertStagingEmpty(t, root)

	// 3. Canceled context cleans up staging!
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := store.StageAndInstall(canceledCtx, validZip, item, "0.1.0", nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	assertStagingEmpty(t, root)

	// 4. Unsupported kind (theme before F45) is rejected cleanly!
	themeZip := buildTestPluginZip(t, map[string]string{
		"plugin.json": `{
			"id": "ocean", "name": "Ocean", "kind": "theme", "version": "1.0.0",
			"apiVersion": 1, "minAppVersion": "0.1.0", "theme": "theme.json"
		}`,
	})
	themeItem := CatalogItem{
		Repository: "demo/ocean-theme",
		Release:    ReleaseAsset{ReleaseID: 11, TagName: "v1.0.0", AssetID: 21, SHA256: fmt.Sprintf("%x", sha256.Sum256(themeZip))},
		Manifest:   Manifest{ID: "ocean", Name: "Ocean", Kind: ThemeKind, Version: "1.0.0"},
	}
	if _, _, err := store.StageAndInstall(context.Background(), themeZip, themeItem, "0.1.0", nil); !errors.Is(err, ErrIncompatible) {
		t.Fatalf("expected ErrIncompatible for theme install, got %v", err)
	}
	assertStagingEmpty(t, root)

	// 5. Valid source installs atomically, persists receipt & zip, and reloads cleanly!
	src, receipt, err := store.StageAndInstall(context.Background(), validZip, item, "0.1.0", nil)
	if err != nil {
		t.Fatalf("StageAndInstall valid source: %v", err)
	}
	if src.Manifest().ID != "demo-media" || receipt.SHA256 != digestHex || receipt.ReleaseID != 10 || receipt.AssetID != 20 {
		t.Fatalf("unexpected receipt/source: %+v", receipt)
	}
	assertStagingEmpty(t, root)

	// 6. Same-ID conflict against a DIFFERENT repository is rejected!
	otherRepoItem := item
	otherRepoItem.Repository = "attacker/media-fork"
	if _, _, err := store.StageAndInstall(context.Background(), validZip, otherRepoItem, "0.1.0", nil); !errors.Is(err, ErrSourceConflict) {
		t.Fatalf("expected ErrSourceConflict from different repository, got %v", err)
	}

	// 7. Reloading from disk verifies digest and restores source!
	loaded, failures := store.LoadAll(context.Background(), "0.1.0")
	if len(failures) != 0 || len(loaded) != 1 || loaded[0].Receipt.ID != "demo-media" {
		t.Fatalf("LoadAll failed: loaded=%d failures=%v", len(loaded), failures)
	}

	// 8. Tampering with stored plugin.zip is caught on reload!
	if err := os.WriteFile(filepath.Join(root, "packages", "demo-media", "plugin.zip"), []byte("corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, failures = store.LoadAll(context.Background(), "0.1.0")
	if len(loaded) != 0 || len(failures) != 1 {
		t.Fatalf("expected tampered archive rejected on LoadAll, got loaded=%d failures=%v", len(loaded), failures)
	}
}

func assertStagingEmpty(t *testing.T, root string) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(root, ".staging"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("read staging dir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("staging dir not cleaned up: %d entries left", len(entries))
	}
}

func TestPackageStoreStageUpdatePreservesOriginalOnFailureAndBlocksKindChange(t *testing.T) {
	if CompareVersions("1.0.0", "1.1.0") <= 0 || CompareVersions("1.1.0", "1.1.0") != 0 || CompareVersions("1.2.0", "1.1.0") >= 0 {
		t.Fatal("CompareVersions returned unexpected ordering")
	}
	added := AddedPermissions([]string{"api.video.example"}, []string{"API.video.example", "images.video.example", "images.video.example"})
	if len(added) != 1 || added[0] != "images.video.example" {
		t.Fatalf("unexpected AddedPermissions: %+v", added)
	}

	root := t.TempDir()
	store := NewPackageStore(root)

	v1Zip := buildTestPluginZip(t, map[string]string{
		"plugin.json": `{
			"id": "demo-video",
			"name": "Demo Video",
			"description": "v1",
			"kind": "source",
			"version": "1.0.0",
			"apiVersion": 1,
			"minAppVersion": "0.1.0",
			"main": "index.js",
			"permissions": {"network": ["api.video.example"]},
			"capabilities": ["catalog.browse"]
		}`,
		"index.js": `export function browse() { return { collections: [] }; }`,
	})
	v1SHA := fmt.Sprintf("%x", sha256.Sum256(v1Zip))
	v1Item := CatalogItem{
		Repository: "demo/video-source",
		Release:    ReleaseAsset{ReleaseID: 1, TagName: "v1.0.0", AssetID: 11, AssetName: "plugin.zip", SHA256: v1SHA},
		Manifest:   Manifest{ID: "demo-video", Name: "Demo Video", Kind: SourceKind, Version: "1.0.0", APIVersion: 1, MinAppVersion: "0.1.0"},
	}
	_, receiptV1, err := store.StageAndInstall(context.Background(), v1Zip, v1Item, "0.1.0", nil)
	if err != nil {
		t.Fatalf("install v1: %v", err)
	}

	verifyOriginalIntact := func() {
		t.Helper()
		loaded, failures := store.LoadAll(context.Background(), "0.1.0")
		if len(failures) != 0 || len(loaded) != 1 || loaded[0].Receipt.Version != "1.0.0" || loaded[0].Receipt.SHA256 != v1SHA {
			t.Fatalf("original v1 package was damaged: loaded=%+v failures=%v", loaded, failures)
		}
		assertStagingEmpty(t, root)
	}

	// 1. Kind change (source -> theme) is strictly forbidden and preserves v1!
	kindChangeZip := buildTestPluginZip(t, map[string]string{
		"plugin.json": `{
			"id": "demo-video",
			"name": "Demo Video Theme",
			"kind": "theme",
			"version": "1.1.0",
			"apiVersion": 1,
			"minAppVersion": "0.1.0",
			"theme": "theme.json"
		}`,
		"theme.json": `{"schemaVersion":1}`,
	})
	kindChangeItem := CatalogItem{
		Repository: "demo/video-source",
		Release:    ReleaseAsset{ReleaseID: 2, TagName: "v1.1.0", AssetID: 12, AssetName: "plugin.zip", SHA256: fmt.Sprintf("%x", sha256.Sum256(kindChangeZip))},
		Manifest:   Manifest{ID: "demo-video", Name: "Demo Video Theme", Kind: ThemeKind, Version: "1.1.0"},
	}
	if _, _, err := store.StageAndUpdate(context.Background(), kindChangeZip, receiptV1, kindChangeItem, "0.1.0"); !errors.Is(err, ErrKindChanged) {
		t.Fatalf("expected ErrKindChanged, got %v", err)
	}
	verifyOriginalIntact()

	// 2. Digest mismatch preserves v1!
	v2Zip := buildTestPluginZip(t, map[string]string{
		"plugin.json": `{
			"id": "demo-video",
			"name": "Demo Video",
			"description": "v2 with expanded permissions",
			"kind": "source",
			"version": "1.1.0",
			"apiVersion": 1,
			"minAppVersion": "0.1.0",
			"main": "index.js",
			"permissions": {"network": ["api.video.example", "images.video.example"]},
			"capabilities": ["catalog.browse"]
		}`,
		"index.js": `export function browse() { return { collections: [] }; }`,
	})
	v2SHA := fmt.Sprintf("%x", sha256.Sum256(v2Zip))
	v2Item := CatalogItem{
		Repository:  "demo/video-source",
		Release:     ReleaseAsset{ReleaseID: 3, TagName: "v1.1.0", AssetID: 13, AssetName: "plugin.zip", SHA256: v2SHA},
		Manifest:    Manifest{ID: "demo-video", Name: "Demo Video", Kind: SourceKind, Version: "1.1.0", APIVersion: 1, MinAppVersion: "0.1.0"},
		Permissions: []string{"api.video.example", "images.video.example"},
	}
	badDigest := v2Item
	badDigest.Release.SHA256 = "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	if _, _, err := store.StageAndUpdate(context.Background(), v2Zip, receiptV1, badDigest, "0.1.0"); !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("expected ErrDigestMismatch, got %v", err)
	}
	verifyOriginalIntact()

	// 3. Repository hijack preserves v1!
	badRepo := v2Item
	badRepo.Repository = "other/video-fork"
	if _, _, err := store.StageAndUpdate(context.Background(), v2Zip, receiptV1, badRepo, "0.1.0"); !errors.Is(err, ErrSourceConflict) {
		t.Fatalf("expected ErrSourceConflict, got %v", err)
	}
	verifyOriginalIntact()

	// 4. Valid update replaces atomically, preserves InstalledAt/Enabled, records UpdatedAt and new SHA256!
	srcV2, receiptV2, err := store.StageAndUpdate(context.Background(), v2Zip, receiptV1, v2Item, "0.1.0")
	if err != nil {
		t.Fatalf("StageAndUpdate valid v2: %v", err)
	}
	if srcV2.Manifest().Version != "1.1.0" || receiptV2.SHA256 != v2SHA || receiptV2.ReleaseID != 3 || receiptV2.AssetID != 13 {
		t.Fatalf("unexpected v2 receipt: %+v", receiptV2)
	}
	if receiptV2.InstalledAt != receiptV1.InstalledAt || !receiptV2.Enabled || receiptV2.UpdatedAt == 0 {
		t.Fatalf("expected InstalledAt/Enabled preserved and UpdatedAt recorded: %+v", receiptV2)
	}
	if len(receiptV2.Permissions) != 2 {
		t.Fatalf("expected updated permissions persisted, got %+v", receiptV2.Permissions)
	}
	assertStagingEmpty(t, root)
	if _, err := os.Stat(filepath.Join(root, "packages", "demo-video", "plugin.zip.bak")); !os.IsNotExist(err) {
		t.Fatalf("expected backup file cleaned up, stat err=%v", err)
	}
	loaded, failures := store.LoadAll(context.Background(), "0.1.0")
	if len(failures) != 0 || len(loaded) != 1 || loaded[0].Receipt.Version != "1.1.0" || loaded[0].Receipt.SHA256 != v2SHA {
		t.Fatalf("LoadAll after update failed: loaded=%+v failures=%v", loaded, failures)
	}
}

func TestPackageStoreDisableBundledExternalAndRemove(t *testing.T) {
	root := t.TempDir()
	store := NewPackageStore(root)

	// 1. Bundled disable/re-enable persistence in state.json.
	if err := store.SetBundledEnabled("sample-source", false); err != nil {
		t.Fatalf("disable bundled: %v", err)
	}
	disabled, err := store.LoadDisabledBundled()
	if err != nil || len(disabled) != 1 || disabled[0] != "sample-source" {
		t.Fatalf("expected sample-source disabled, got %+v err=%v", disabled, err)
	}
	if err := store.SetBundledEnabled("sample-source", true); err != nil {
		t.Fatalf("re-enable bundled: %v", err)
	}
	disabled, err = store.LoadDisabledBundled()
	if err != nil || len(disabled) != 0 {
		t.Fatalf("expected empty disabled list after re-enable, got %+v err=%v", disabled, err)
	}

	// 2. External enable/disable persistence & clean removal without affecting sibling plugins.
	zipA := buildTestPluginZip(t, map[string]string{
		"plugin.json": `{"id":"src-a","name":"Source A","kind":"source","version":"1.0.0","apiVersion":1,"minAppVersion":"0.1.0","main":"index.js","capabilities":["catalog.browse"]}`,
		"index.js":    `export function browse() { return { collections: [] }; }`,
	})
	zipB := buildTestPluginZip(t, map[string]string{
		"plugin.json": `{"id":"src-b","name":"Source B","kind":"source","version":"1.0.0","apiVersion":1,"minAppVersion":"0.1.0","main":"index.js","capabilities":["catalog.browse"]}`,
		"index.js":    `export function browse() { return { collections: [] }; }`,
	})
	itemA := CatalogItem{
		Repository: "demo/src-a",
		Release:    ReleaseAsset{ReleaseID: 1, TagName: "v1.0.0", AssetID: 10, SHA256: fmt.Sprintf("%x", sha256.Sum256(zipA))},
		Manifest:   Manifest{ID: "src-a", Name: "Source A", Kind: SourceKind, Version: "1.0.0"},
	}
	itemB := CatalogItem{
		Repository: "demo/src-b",
		Release:    ReleaseAsset{ReleaseID: 2, TagName: "v1.0.0", AssetID: 20, SHA256: fmt.Sprintf("%x", sha256.Sum256(zipB))},
		Manifest:   Manifest{ID: "src-b", Name: "Source B", Kind: SourceKind, Version: "1.0.0"},
	}
	if _, _, err := store.StageAndInstall(context.Background(), zipA, itemA, "0.1.0", nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.StageAndInstall(context.Background(), zipB, itemB, "0.1.0", nil); err != nil {
		t.Fatal(err)
	}

	recA, err := store.SetExternalEnabled("src-a", false)
	if err != nil || recA.Enabled {
		t.Fatalf("disable src-a failed: rec=%+v err=%v", recA, err)
	}
	loaded, failures := store.LoadAll(context.Background(), "0.1.0")
	if len(failures) != 0 || len(loaded) != 2 || loaded[0].Receipt.Enabled || !loaded[1].Receipt.Enabled {
		t.Fatalf("expected src-a disabled and src-b enabled on reload: %+v failures=%v", loaded, failures)
	}

	removed, err := store.Remove("src-a")
	if err != nil || removed.ID != "src-a" {
		t.Fatalf("Remove src-a failed: removed=%+v err=%v", removed, err)
	}
	if _, err := os.Stat(filepath.Join(root, "packages", "src-a")); !os.IsNotExist(err) {
		t.Fatalf("expected src-a package dir removed, stat err=%v", err)
	}
	loaded, failures = store.LoadAll(context.Background(), "0.1.0")
	if len(failures) != 0 || len(loaded) != 1 || loaded[0].Receipt.ID != "src-b" {
		t.Fatalf("expected only src-b remaining after remove, got loaded=%+v failures=%v", loaded, failures)
	}
}
