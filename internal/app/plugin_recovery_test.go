package app

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/yuchanns/MikuTV/internal/plugin"
)

type recoveryTestFetcher struct {
	payloads map[string][]byte
	failRepo string
}

func (f *recoveryTestFetcher) FetchPage(context.Context, int, string) (plugin.CatalogPage, error) {
	return plugin.CatalogPage{}, nil
}
func (f *recoveryTestFetcher) SeedInspectCache([]plugin.CatalogItem) {}
func (f *recoveryTestFetcher) ResolveRepository(context.Context, string) (plugin.CatalogItem, error) {
	return plugin.CatalogItem{}, plugin.ErrNoOfficialRelease
}
func (f *recoveryTestFetcher) DownloadAsset(context.Context, plugin.ReleaseAsset) ([]byte, [sha256.Size]byte, error) {
	return nil, [sha256.Size]byte{}, errors.New("unused")
}
func (f *recoveryTestFetcher) DownloadReleaseAsset(_ context.Context, repo, _ string, _ int64, expectedSHA string) ([]byte, [sha256.Size]byte, error) {
	if repo == f.failRepo {
		return nil, [sha256.Size]byte{}, errors.New("offline")
	}
	data, ok := f.payloads[repo]
	if !ok {
		return nil, [sha256.Size]byte{}, plugin.ErrNoOfficialRelease
	}
	sum := sha256.Sum256(data)
	if expectedSHA != "" && fmt.Sprintf("%x", sum) != expectedSHA {
		return nil, [sha256.Size]byte{}, plugin.ErrDigestMismatch
	}
	return data, sum, nil
}

func makeRecoveryThemeZip(t *testing.T, id, name string) ([]byte, string) {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	manifest, _ := json.Marshal(map[string]any{
		"id":            id,
		"name":          name,
		"description":   "recovery test theme",
		"kind":          "theme",
		"version":       "1.0.0",
		"apiVersion":    1,
		"minAppVersion": "0.1.0",
		"theme":         "theme.json",
	})
	themeJSON := []byte(`{"schemaVersion":1,"base":"mikutv-dark","appearance":"dark","colors":{"accent":{"primary":"#7AA2F7","soft":"#9ECE6A","strong":"#7DCFFF","foreground":"#15161E"}}}`)
	for name, body := range map[string][]byte{"plugin.json": manifest, "theme.json": themeJSON} {
		hdr := &zip.FileHeader{Name: name, Method: zip.Deflate}
		hdr.SetMode(0o644)
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(body); err != nil {
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

func TestPluginCatalogStartupRecoveryDoneAndPartial(t *testing.T) {
	zipA, shaA := makeRecoveryThemeZip(t, "theme-a", "Theme A")
	zipB, shaB := makeRecoveryThemeZip(t, "theme-b", "Theme B")

	// 1. Full recovery succeeds -> Phase transitions Running -> Done.
	store := plugin.NewPackageStore(filepath.Join(t.TempDir(), "plugins"))
	fetcher := &recoveryTestFetcher{payloads: map[string][]byte{
		"example/theme-a": zipA,
		"example/theme-b": zipB,
	}}
	cat := NewPluginCatalog(nil, fetcher, nil, func(fn func()) { fn() }, nil)
	cat.SetInstaller(store, "0.1.0", nil)

	missing := []plugin.InstalledReceipt{
		{ID: "theme-a", Name: "Theme A", Kind: plugin.ThemeKind, Version: "1.0.0", Repository: "example/theme-a", TagName: "v1.0.0", SHA256: shaA},
		{ID: "theme-b", Name: "Theme B", Kind: plugin.ThemeKind, Version: "1.0.0", Repository: "example/theme-b", TagName: "v1.0.0", SHA256: shaB},
	}

	done := make(chan [2]int, 1)
	mounted := 0
	cat.StartStartupRecovery(missing, func(pkg plugin.InstalledPackage) error {
		if pkg.Theme != nil {
			mounted++
		}
		return nil
	}, func(restored, failed int) {
		done <- [2]int{restored, failed}
	})

	select {
	case res := <-done:
		if res[0] != 2 || res[1] != 0 || mounted != 2 {
			t.Fatalf("unexpected recovery result: restored=%d failed=%d mounted=%d", res[0], res[1], mounted)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("startup recovery timed out")
	}

	rec := cat.RecoveryState()
	if rec.Phase != PluginRecoveryDone || rec.Restored != 2 || rec.Failed != 0 {
		t.Fatalf("unexpected final recovery state: %+v", rec)
	}
	if len(cat.Snapshot().Installed) != 2 {
		t.Fatalf("expected 2 installed plugins after recovery, got %d", len(cat.Snapshot().Installed))
	}

	// 2. Partial offline failure -> Phase transitions Running -> Partial.
	store2 := plugin.NewPackageStore(filepath.Join(t.TempDir(), "plugins2"))
	fetcher2 := &recoveryTestFetcher{
		payloads: map[string][]byte{"example/theme-a": zipA},
		failRepo: "example/theme-b",
	}
	cat2 := NewPluginCatalog(nil, fetcher2, nil, func(fn func()) { fn() }, nil)
	cat2.SetInstaller(store2, "0.1.0", nil)

	done2 := make(chan [2]int, 1)
	cat2.StartStartupRecovery(missing, nil, func(restored, failed int) {
		done2 <- [2]int{restored, failed}
	})
	select {
	case res := <-done2:
		if res[0] != 1 || res[1] != 1 {
			t.Fatalf("expected 1 restored / 1 failed, got %v", res)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("partial recovery timed out")
	}
	if st := cat2.RecoveryState(); st.Phase != PluginRecoveryPartial || st.Restored != 1 || st.Failed != 1 {
		t.Fatalf("unexpected partial recovery state: %+v", st)
	}
}
