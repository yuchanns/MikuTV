package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/yuchanns/MikuTV/internal/plugin"
)

type stubPluginCatalogFetcher struct {
	pages map[int]plugin.CatalogPage
	err   error
	calls int
}

func (f *stubPluginCatalogFetcher) FetchPage(_ context.Context, page int, etag string) (plugin.CatalogPage, error) {
	f.calls++
	if f.err != nil {
		return plugin.CatalogPage{}, f.err
	}
	p := f.pages[page]
	if page == 1 && etag != "" && etag == p.ETag {
		return plugin.CatalogPage{Page: 1, ETag: etag, NotModified: true}, nil
	}
	return p, nil
}

func (f *stubPluginCatalogFetcher) SeedInspectCache([]plugin.CatalogItem) {}
func (f *stubPluginCatalogFetcher) ResolveRepository(_ context.Context, _ string) (plugin.CatalogItem, error) {
	return plugin.CatalogItem{}, plugin.ErrNoOfficialRelease
}
func (f *stubPluginCatalogFetcher) DownloadAsset(_ context.Context, _ plugin.ReleaseAsset) ([]byte, [32]byte, error) {
	return nil, [32]byte{}, errors.New("not implemented")
}

type memoryPluginCatalogCache struct {
	data PluginCatalogCacheData
}

func (m *memoryPluginCatalogCache) Load() (PluginCatalogCacheData, error) { return m.data, nil }
func (m *memoryPluginCatalogCache) Save(d PluginCatalogCacheData) error   { m.data = d; return nil }

func TestPluginCatalogPaginationDedupCachingOfflineAndRateLimitRecovery(t *testing.T) {
	itemInstalled := plugin.CatalogItem{
		Repository: "yuchanns/sample-source", RepoID: 1,
		Manifest:      plugin.Manifest{ID: "sample-source", Name: "SampleSource", Kind: plugin.SourceKind, Version: "0.1.0"},
		Compatibility: plugin.Compatibility{Compatible: true, Code: plugin.CompatibleOK},
	}
	itemSource := plugin.CatalogItem{
		Repository: "demo/media-source", RepoID: 2,
		Manifest:      plugin.Manifest{ID: "media", Name: "Demo Media", Description: "Playlists", Kind: plugin.SourceKind, Version: "1.0.0"},
		Compatibility: plugin.Compatibility{Compatible: true, Code: plugin.CompatibleOK},
	}
	itemTheme := plugin.CatalogItem{
		Repository: "demo/ocean-theme", RepoID: 3,
		Manifest:      plugin.Manifest{ID: "ocean", Name: "Ocean Blue", Description: "Blue palette", Kind: plugin.ThemeKind, Version: "1.0.0"},
		Compatibility: plugin.Compatibility{Compatible: false, Code: plugin.IncompatibleThemeUnavailable},
	}

	fetcher := &stubPluginCatalogFetcher{
		pages: map[int]plugin.CatalogPage{
			1: {
				Items:      []plugin.CatalogItem{itemInstalled, itemSource},
				Page:       1,
				TotalCount: 3,
				HasMore:    true,
				ETag:       `"v1"`,
			},
			2: {
				Items:      []plugin.CatalogItem{itemSource, itemTheme},
				Page:       2,
				TotalCount: 3,
				HasMore:    false,
			},
		},
	}
	cache := &memoryPluginCatalogCache{}
	queue := make(chan func(), 32)
	drain := func(cond func() bool) {
		t.Helper()
		deadline := time.After(2 * time.Second)
		for !cond() {
			select {
			case fn := <-queue:
				fn()
			case <-deadline:
				t.Fatal("timed out waiting for plugin catalog update")
			}
		}
	}

	cat := NewPluginCatalog(
		[]InstalledPlugin{{ID: "sample-source", Name: "SampleSource", Kind: plugin.SourceKind, Version: "0.1.0", Bundled: true, Enabled: true}},
		fetcher,
		cache,
		func(fn func()) { queue <- fn },
		func() {},
	)
	t.Cleanup(cat.Close)

	cat.EnsureLoaded()
	drain(func() bool { return cat.Snapshot().Status == PluginCatalogReady })

	snap := cat.Snapshot()
	if !snap.HasMore || len(snap.Items) != 2 {
		t.Fatalf("unexpected page 1 snapshot: %+v", snap)
	}
	// Installed plugin excluded from UninstalledItems!
	uninstalled := snap.UninstalledItems("", "")
	if len(uninstalled) != 1 || uninstalled[0].Manifest.ID != "media" {
		t.Fatalf("expected installed sample-source filtered out, got %+v", uninstalled)
	}

	// Page 2 appends ocean theme and deduplicates itemSource.
	cat.LoadMore()
	drain(func() bool { return !cat.Snapshot().LoadingMore && cat.Snapshot().Page == 2 })
	snap = cat.Snapshot()
	if snap.HasMore || len(snap.UninstalledItems("", "")) != 2 {
		t.Fatalf("expected 2 deduplicated uninstalled items after page 2, got %+v", snap.UninstalledItems("", ""))
	}
	if len(snap.UninstalledItems(plugin.ThemeKind, "")) != 1 || snap.UninstalledItems(plugin.ThemeKind, "")[0].Manifest.ID != "ocean" {
		t.Fatalf("theme kind filter failed: %+v", snap.UninstalledItems(plugin.ThemeKind, ""))
	}
	if len(snap.UninstalledItems("", "media-source")) != 1 {
		t.Fatalf("repository query search failed")
	}

	// Offline failure preserves cached items!
	fetcher.err = errors.New("network unreachable")
	cat.Refresh()
	drain(func() bool { return cat.Snapshot().Status == PluginCatalogOffline })
	if len(cat.Snapshot().UninstalledItems("", "")) != 2 {
		t.Fatal("offline refresh lost cached items")
	}

	// Rate limit error preserves cached items and exposes reset time, then recovers!
	fetcher.err = &plugin.RateLimitError{ResetAt: time.Now().Add(5 * time.Minute)}
	cat.Refresh()
	drain(func() bool { return cat.Snapshot().Status == PluginCatalogRateLimited })
	if !cat.Snapshot().HasCache || cat.Snapshot().RateLimitResetAt.IsZero() {
		t.Fatalf("rate-limited snapshot missing cache or reset time: %+v", cat.Snapshot())
	}

	fetcher.err = nil
	cat.Refresh()
	drain(func() bool { return cat.Snapshot().Status == PluginCatalogReady })
	if !cat.Snapshot().RateLimitResetAt.IsZero() {
		t.Fatal("rate limit state did not clear on recovery")
	}
}
