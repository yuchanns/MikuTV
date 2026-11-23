package app

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/yuchanns/MikuTV/internal/plugin"
)

type installTestFetcher struct {
	mu       sync.Mutex
	resolved map[string]plugin.CatalogItem
	zips     map[string][]byte
	dlErr    error
	blockCh  chan struct{}
}

func (f *installTestFetcher) FetchPage(_ context.Context, _ int, _ string) (plugin.CatalogPage, error) {
	return plugin.CatalogPage{}, nil
}
func (f *installTestFetcher) SeedInspectCache([]plugin.CatalogItem) {}
func (f *installTestFetcher) ResolveRepository(_ context.Context, raw string) (plugin.CatalogItem, error) {
	canon, ok := plugin.ParseRepository(raw)
	if !ok {
		return plugin.CatalogItem{}, plugin.ErrInvalidRepository
	}
	f.mu.Lock()
	item, ok := f.resolved[canon]
	f.mu.Unlock()
	if !ok {
		return plugin.CatalogItem{}, plugin.ErrNoOfficialRelease
	}
	return item, nil
}
func (f *installTestFetcher) DownloadAsset(ctx context.Context, asset plugin.ReleaseAsset) ([]byte, [sha256.Size]byte, error) {
	f.mu.Lock()
	blockCh := f.blockCh
	f.mu.Unlock()
	if blockCh != nil {
		select {
		case <-blockCh:
		case <-ctx.Done():
			return nil, [sha256.Size]byte{}, ctx.Err()
		}
	}
	f.mu.Lock()
	dlErr := f.dlErr
	data := f.zips[asset.DownloadURL]
	f.mu.Unlock()
	if dlErr != nil {
		return nil, [sha256.Size]byte{}, dlErr
	}
	digest := sha256.Sum256(data)
	return data, digest, nil
}

func makeAppTestZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
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

func TestPluginCatalogInstallResolveConflictCancelAndMount(t *testing.T) {
	goodZip := makeAppTestZip(t, map[string]string{
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
	goodSHA := fmt.Sprintf("%x", sha256.Sum256(goodZip))
	goodItem := plugin.CatalogItem{
		Repository: "demo/media-source",
		Release:    plugin.ReleaseAsset{ReleaseID: 1, TagName: "v1.0.0", AssetID: 2, AssetName: "plugin.zip", DownloadURL: "https://example.com/media.zip", SHA256: goodSHA},
		Manifest:   plugin.Manifest{ID: "demo-media", Name: "Demo Media", Description: "Videos", Kind: plugin.SourceKind, Version: "1.0.0", APIVersion: 1, MinAppVersion: "0.1.0"},
		Permissions: []string{"media.example.com"},
		Compatibility: plugin.Compatibility{Compatible: true, Code: plugin.CompatibleOK},
	}

	fetcher := &installTestFetcher{
		resolved: map[string]plugin.CatalogItem{"demo/media-source": goodItem},
		zips:     map[string][]byte{"https://example.com/media.zip": goodZip},
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
	mountedID := ""
	cat := NewPluginCatalog(
		[]InstalledPlugin{{ID: "sample-source", Name: "SampleSource", Kind: plugin.SourceKind, Version: "0.1.0", Bundled: true, Enabled: true}},
		fetcher,
		nil,
		func(fn func()) { queue <- fn },
		func() {},
	)
	cat.SetInstaller(pkgStore, "0.1.0", func(pkg plugin.InstalledPackage) error {
		mountedID = pkg.Source.Manifest().ID + "@" + pkg.Receipt.SHA256
		return nil
	})
	t.Cleanup(cat.Close)

	// 1. Resolve repository by URL -> upserts into catalog.
	var resolvedItem plugin.CatalogItem
	var resolveDone bool
	cat.ResolveRepository("https://github.com/demo/media-source", func(item plugin.CatalogItem, err error) {
		if err != nil {
			t.Fatalf("ResolveRepository failed: %v", err)
		}
		resolvedItem = item
		resolveDone = true
	})
	drain(func() bool { return resolveDone })
	if resolvedItem.Manifest.ID != "demo-media" {
		t.Fatalf("unexpected resolved item: %+v", resolvedItem)
	}

	// 2. Cancel in-flight install cleanly.
	ch := make(chan struct{})
	fetcher.mu.Lock()
	fetcher.blockCh = ch
	fetcher.mu.Unlock()
	var cancelOutcome PluginInstallOutcome
	var cancelDone bool
	cat.Install(resolvedItem, func(out PluginInstallOutcome) {
		cancelOutcome = out
		cancelDone = true
	})
	cat.CancelInstall()
	close(ch)
	fetcher.mu.Lock()
	fetcher.blockCh = nil
	fetcher.mu.Unlock()
	// Cancel increments installGen so stale completion is ignored; verify nothing installed.
	if _, ok := cat.FindInstalled("demo-media"); ok || cancelDone {
		t.Fatalf("canceled install leaked: ok=%v done=%v outcome=%+v", ok, cancelDone, cancelOutcome)
	}

	// 3. Network failure reports retryable error without installing.
	fetcher.mu.Lock()
	fetcher.dlErr = errors.New("connection reset")
	fetcher.mu.Unlock()
	var failOutcome PluginInstallOutcome
	var failDone bool
	cat.Install(resolvedItem, func(out PluginInstallOutcome) {
		failOutcome = out
		failDone = true
	})
	drain(func() bool { return failDone })
	if failOutcome.Err == nil || !failOutcome.Retryable {
		t.Fatalf("expected retryable download failure, got %+v", failOutcome)
	}

	// 4. Retry succeeds, persists receipt, mounts source, and updates installed list!
	fetcher.mu.Lock()
	fetcher.dlErr = nil
	fetcher.mu.Unlock()
	var okOutcome PluginInstallOutcome
	var okDone bool
	cat.Install(resolvedItem, func(out PluginInstallOutcome) {
		okOutcome = out
		okDone = true
	})
	drain(func() bool { return okDone })
	if okOutcome.Err != nil || okOutcome.Receipt.SHA256 != goodSHA || mountedID != "demo-media@"+goodSHA {
		t.Fatalf("install failed: outcome=%+v mounted=%q", okOutcome, mountedID)
	}
	if _, ok := cat.FindInstalled("demo-media"); !ok {
		t.Fatal("newly installed plugin missing from installed list")
	}

	// 5. Same-ID conflict from another repository is blocked!
	conflictItem := resolvedItem
	conflictItem.Repository = "other/media-fork"
	if err := cat.CheckInstallable(conflictItem); !errors.Is(err, plugin.ErrSourceConflict) {
		t.Fatalf("expected ErrSourceConflict, got %v", err)
	}
}
