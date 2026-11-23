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
