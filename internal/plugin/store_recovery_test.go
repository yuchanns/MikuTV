package plugin

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

type memoryStoreBackup struct {
	mu       sync.Mutex
	receipts []byte
	state    []byte
}

func (m *memoryStoreBackup) LoadPluginReceiptsBackup() []byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]byte(nil), m.receipts...)
}

func (m *memoryStoreBackup) SavePluginReceiptsBackup(data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.receipts = append([]byte(nil), data...)
	return nil
}

func (m *memoryStoreBackup) LoadPluginStateBackup() []byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]byte(nil), m.state...)
}

func (m *memoryStoreBackup) SavePluginStateBackup(data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state = append([]byte(nil), data...)
	return nil
}

func buildTestThemeZip(t *testing.T, id, name, version string) ([]byte, string) {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	manifest := map[string]any{
		"id":            id,
		"name":          name,
		"description":   "test theme",
		"kind":          "theme",
		"version":       version,
		"apiVersion":    1,
		"minAppVersion": "0.1.0",
		"license":       "Apache-2.0",
		"theme":         "theme.json",
	}
	mBytes, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	themeJSON := `{"schemaVersion":1,"base":"mikutv-dark","appearance":"dark","colors":{"accent":{"primary":"#7AA2F7","soft":"#9ECE6A","strong":"#7DCFFF","foreground":"#15161E"}}}`
	for k, v := range map[string][]byte{
		"plugin.json": mBytes,
		"theme.json":  []byte(themeJSON),
	} {
		hdr := &zip.FileHeader{Name: k, Method: zip.Deflate}
		hdr.SetMode(0o644)
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(v); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	raw := buf.Bytes()
	sum := sha256.Sum256(raw)
	return raw, fmt.Sprintf("%x", sum)
}

func TestPackageStoreCachePurgeBackupAndRestore(t *testing.T) {
	ctx := context.Background()
	cacheDir := filepath.Join(t.TempDir(), "plugins")
	backup := &memoryStoreBackup{}

	store := NewPackageStore(cacheDir)
	store.SetBackup(backup)

	zipBytes, shaHex := buildTestThemeZip(t, "test-night", "Test Night", "1.0.0")
	item := CatalogItem{
		Repository: "example/test-night",
		Release: ReleaseAsset{
			ReleaseID: 101,
			TagName:   "v1.0.0",
			AssetID:   202,
			AssetName: "plugin.zip",
			SHA256:    shaHex,
		},
		Manifest: Manifest{
			ID:            "test-night",
			Name:          "Test Night",
			Kind:          ThemeKind,
			Version:       "1.0.0",
			APIVersion:    1,
			MinAppVersion: "0.1.0",
		},
	}

	if _, err := store.StageAndInstall(ctx, zipBytes, item, "0.1.0", nil); err != nil {
		t.Fatalf("StageAndInstall failed: %v", err)
	}
	if err := store.SaveAppearanceConfig("system", "mikutv-light", "test-night"); err != nil {
		t.Fatalf("SaveAppearanceConfig failed: %v", err)
	}
	if len(backup.LoadPluginReceiptsBackup()) == 0 {
		t.Fatalf("expected durable receipts backup to be populated")
	}
	if len(backup.LoadPluginStateBackup()) == 0 {
		t.Fatalf("expected durable state backup to be populated")
	}

	// Simulate tvOS purging Library/Caches completely under low-storage pressure.
	if err := os.RemoveAll(cacheDir); err != nil {
		t.Fatalf("RemoveAll cacheDir: %v", err)
	}

	fresh := NewPackageStore(cacheDir)
	fresh.SetBackup(backup)

	// State should self-heal immediately from durable backup.
	st, err := fresh.LoadAppearanceState()
	if err != nil {
		t.Fatalf("LoadAppearanceState after purge: %v", err)
	}
	if st.AppearanceMode != "system" || st.DarkTheme != "test-night" {
		t.Fatalf("expected restored appearance state, got %+v", st)
	}

	// LoadAllWithMissing should flag cachePurged and return the missing receipt.
	loaded, missing, purged, _ := fresh.LoadAllWithMissing(ctx, "0.1.0")
	if !purged {
		t.Fatalf("expected cachePurged=true after Library/Caches wipe")
	}
	if len(loaded) != 0 || len(missing) != 1 || missing[0].ID != "test-night" {
		t.Fatalf("unexpected recovery scan: loaded=%d missing=%+v", len(loaded), missing)
	}

	// Tampered bytes must be rejected by strict SHA-256 verification.
	if _, err := fresh.RestoreReceipt(ctx, missing[0], []byte("tampered"), "0.1.0"); !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("expected ErrDigestMismatch for tampered bytes, got %v", err)
	}

	// Authentic bytes matching backed-up SHA-256 must restore cleanly.
	restored, err := fresh.RestoreReceipt(ctx, missing[0], zipBytes, "0.1.0")
	if err != nil {
		t.Fatalf("RestoreReceipt failed: %v", err)
	}
	if restored.Theme == nil || restored.Receipt.ID != "test-night" {
		t.Fatalf("unexpected restored package: %+v", restored)
	}

	// Next launch sees zero missing plugins.
	loaded2, missing2, purged2, failures2 := fresh.LoadAllWithMissing(ctx, "0.1.0")
	if purged2 || len(missing2) != 0 || len(failures2) != 0 || len(loaded2) != 1 {
		t.Fatalf("expected clean state after restore: loaded=%d missing=%d purged=%v failures=%v",
			len(loaded2), len(missing2), purged2, failures2)
	}
}

func TestPackageStoreRestoreReceiptPreservesOtherPendingReceipts(t *testing.T) {
	ctx := context.Background()
	cacheDir := filepath.Join(t.TempDir(), "plugins")
	backup := &memoryStoreBackup{}

	store := NewPackageStore(cacheDir)
	store.SetBackup(backup)

	zipA, shaA := buildTestThemeZip(t, "theme-alpha", "Theme Alpha", "1.0.0")
	zipB, shaB := buildTestThemeZip(t, "theme-beta", "Theme Beta", "1.0.0")
	for _, tc := range []struct {
		id, name, repo, sha string
		raw                 []byte
	}{
		{"theme-alpha", "Theme Alpha", "example/theme-alpha", shaA, zipA},
		{"theme-beta", "Theme Beta", "example/theme-beta", shaB, zipB},
	} {
		item := CatalogItem{
			Repository: tc.repo,
			Release: ReleaseAsset{
				ReleaseID: 1, TagName: "v1.0.0", AssetID: 2, AssetName: "plugin.zip", SHA256: tc.sha,
			},
			Manifest: Manifest{
				ID: tc.id, Name: tc.name, Kind: ThemeKind, Version: "1.0.0", APIVersion: 1, MinAppVersion: "0.1.0",
			},
		}
		if _, err := store.StageAndInstall(ctx, tc.raw, item, "0.1.0", nil); err != nil {
			t.Fatalf("StageAndInstall %s: %v", tc.id, err)
		}
	}

	// Simulate tvOS purging Library/Caches completely.
	if err := os.RemoveAll(cacheDir); err != nil {
		t.Fatalf("RemoveAll cacheDir: %v", err)
	}

	fresh := NewPackageStore(cacheDir)
	fresh.SetBackup(backup)
	loaded, missing, purged, _ := fresh.LoadAllWithMissing(ctx, "0.1.0")
	if !purged || len(loaded) != 0 || len(missing) != 2 {
		t.Fatalf("expected 2 missing receipts after purge, got loaded=%d missing=%d purged=%v", len(loaded), len(missing), purged)
	}

	// Simulate receipts.json absent again before restoring only the first plugin.
	_ = os.Remove(filepath.Join(cacheDir, "receipts.json"))
	if _, err := fresh.RestoreReceipt(ctx, missing[0], zipA, "0.1.0"); err != nil {
		t.Fatalf("RestoreReceipt alpha: %v", err)
	}

	// Next launch must still have alpha loaded AND beta discoverable in missing for retry.
	next := NewPackageStore(cacheDir)
	next.SetBackup(backup)
	loaded2, missing2, _, _ := next.LoadAllWithMissing(ctx, "0.1.0")
	if len(loaded2) != 1 || loaded2[0].Receipt.ID != "theme-alpha" {
		t.Fatalf("expected theme-alpha loaded, got %+v", loaded2)
	}
	if len(missing2) != 1 || missing2[0].ID != "theme-beta" {
		t.Fatalf("expected theme-beta preserved in missing receipts, got %+v", missing2)
	}
}

func TestCatalogClientDownloadReleaseAsset(t *testing.T) {
	zipBytes, shaHex := buildTestThemeZip(t, "test-night", "Test Night", "1.0.0")
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/example/test-night/releases/tags/v1.0.0":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":       101,
				"tag_name": "v1.0.0",
				"assets": []map[string]any{{
					"id":                   202,
					"name":                 "plugin.zip",
					"size":                 len(zipBytes),
					"browser_download_url": srv.URL + "/download/plugin.zip",
				}},
			})
		case "/download/plugin.zip":
			_, _ = w.Write(zipBytes)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := NewCatalogClient("0.1.0", srv.URL)
	client.AllowInsecureHTTP = true
	data, sum, err := client.DownloadReleaseAsset(context.Background(), "example/test-night", "v1.0.0", 202, shaHex)
	if err != nil {
		t.Fatalf("DownloadReleaseAsset failed: %v", err)
	}
	if fmt.Sprintf("%x", sum) != shaHex || !bytes.Equal(data, zipBytes) {
		t.Fatalf("downloaded bytes mismatch")
	}

	if _, _, err := client.DownloadReleaseAsset(context.Background(), "example/test-night", "v1.0.0", 202, "0000000000000000000000000000000000000000000000000000000000000000"); !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("expected ErrDigestMismatch on bad digest, got %v", err)
	}
}
