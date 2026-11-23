package plugin

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func buildTestPluginZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestInspectPackageUsesReleaseManifestNotRepoNameAndChecksCompatibility(t *testing.T) {
	compatibleSource := buildTestPluginZip(t, map[string]string{
		"plugin.json": `{
			"id": "demo-video",
			"name": "Demo Video",
			"description": "Videos",
			"kind": "source",
			"version": "1.0.0",
			"apiVersion": 1,
			"minAppVersion": "0.1.0",
			"main": "index.js",
			"permissions": {"network": ["api.example.com"]},
			"capabilities": ["catalog.browse"]
		}`,
		"index.js": `export function browse() { return {}; }`,
	})
	got, err := InspectPackage(compatibleSource, "0.1.0")
	if err != nil {
		t.Fatalf("InspectPackage compatible source: %v", err)
	}
	if got.Manifest.Kind != SourceKind || !got.Compatibility.Compatible || got.Compatibility.Code != CompatibleOK {
		t.Fatalf("unexpected compatible inspection: %+v", got)
	}

	futureApp := buildTestPluginZip(t, map[string]string{
		"plugin.json": `{
			"id": "future-source",
			"name": "Future Source",
			"kind": "source",
			"version": "2.0.0",
			"apiVersion": 1,
			"minAppVersion": "1.2.0",
			"main": "index.js"
		}`,
	})
	gotFuture, err := InspectPackage(futureApp, "0.1.0")
	if err != nil {
		t.Fatalf("InspectPackage future source: %v", err)
	}
	if gotFuture.Compatibility.Compatible || gotFuture.Compatibility.Code != IncompatibleAppVersion || gotFuture.Compatibility.RequiredAppVersion != "1.2.0" {
		t.Fatalf("expected app_version incompatibility, got %+v", gotFuture.Compatibility)
	}

	themeZip := buildTestPluginZip(t, map[string]string{
		"plugin.json": `{
			"id": "ocean-theme",
			"name": "Ocean Blue",
			"kind": "theme",
			"version": "1.0.0",
			"apiVersion": 1,
			"minAppVersion": "0.1.0",
			"theme": "theme.json"
		}`,
		"theme.json": `{"schemaVersion":1}`,
	})
	gotTheme, err := InspectPackage(themeZip, "0.1.0")
	if err != nil {
		t.Fatalf("InspectPackage theme: %v", err)
	}
	if gotTheme.Manifest.Kind != ThemeKind || gotTheme.Compatibility.Compatible || gotTheme.Compatibility.Code != IncompatibleThemeUnavailable {
		t.Fatalf("expected theme kind unavailable until F45, got %+v", gotTheme)
	}

	badAuth := buildTestPluginZip(t, map[string]string{
		"plugin.json": `{
			"id": "bad-auth",
			"name": "Password Source",
			"kind": "source",
			"version": "1.0.0",
			"apiVersion": 1,
			"minAppVersion": "0.1.0",
			"main": "index.js",
			"authentication": {"methods": ["password"]},
			"capabilities": ["account.signIn"]
		}`,
	})
	gotAuth, err := InspectPackage(badAuth, "0.1.0")
	if err != nil {
		t.Fatalf("InspectPackage bad auth: %v", err)
	}
	if gotAuth.Compatibility.Compatible || gotAuth.Compatibility.Code != IncompatibleAuth {
		t.Fatalf("expected auth incompatibility, got %+v", gotAuth.Compatibility)
	}
}

func TestCatalogClientPaginationDedupETagRateLimitAndAuthoritativeKind(t *testing.T) {
	// Deliberately name a theme repo "acme/video-source" to prove kind comes from Release's plugin.json!
	themeZip := buildTestPluginZip(t, map[string]string{
		"plugin.json": `{
			"id": "deep-ocean",
			"name": "Deep Ocean",
			"description": "Cool ocean palette",
			"kind": "theme",
			"version": "1.0.0",
			"apiVersion": 1,
			"minAppVersion": "0.1.0",
			"theme": "theme.json"
		}`,
	})
	sourceZip := buildTestPluginZip(t, map[string]string{
		"plugin.json": `{
			"id": "demo-media",
			"name": "Demo Media",
			"description": "Videos and playlists",
			"kind": "source",
			"version": "1.1.0",
			"apiVersion": 1,
			"minAppVersion": "0.1.0",
			"main": "index.js",
			"permissions": {"network": ["media.example.com"]},
			"capabilities": ["catalog.browse"]
		}`,
		"index.js": `export function browse() { return {}; }`,
	})

	var zipDownloads atomic.Int32
	var forceRateLimit atomic.Bool
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if forceRateLimit.Load() {
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.Header().Set("X-RateLimit-Reset", fmt.Sprintf("%d", time.Now().Add(2*time.Minute).Unix()))
			w.WriteHeader(http.StatusForbidden)
			return
		}
		switch r.URL.Path {
		case "/search/repositories":
			if !strings.Contains(r.URL.Query().Get("q"), "topic:mikutv-plugin") {
				t.Errorf("unexpected search query: %q", r.URL.Query().Get("q"))
			}
			if r.URL.Query().Get("page") == "1" && r.Header.Get("If-None-Match") == `"etag-v1"` {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			w.Header().Set("ETag", `"etag-v1"`)
			if r.URL.Query().Get("page") == "2" {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"total_count": 3,
					"items": []map[string]any{
						{"id": 101, "full_name": "acme/video-source", "description": "dup page 2", "owner": map[string]any{"login": "acme"}},
						{"id": 102, "full_name": "acme/media-hub", "description": "media", "owner": map[string]any{"login": "acme"}},
					},
				})
				return
			}
			w.Header().Set("Link", fmt.Sprintf(`<%s/search/repositories?page=2>; rel="next"`, srv.URL))
			_ = json.NewEncoder(w).Encode(map[string]any{
				"total_count": 3,
				"items": []map[string]any{
					{"id": 101, "full_name": "acme/video-source", "description": "misleading repo name", "owner": map[string]any{"login": "acme"}},
					{"id": 101, "full_name": "ACME/VIDEO-SOURCE", "description": "same-page dup", "owner": map[string]any{"login": "acme"}},
					{"id": 103, "full_name": "acme/prerelease-only", "description": "only draft/prerelease", "owner": map[string]any{"login": "acme"}},
					{"id": 104, "full_name": "acme/archived-repo", "archived": true, "owner": map[string]any{"login": "acme"}},
				},
			})
		case "/repos/acme/video-source/releases":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"id": 1, "tag_name": "v2.0.0-beta.1", "prerelease": true, "assets": []map[string]any{{"id": 11, "name": "plugin.zip", "size": len(sourceZip), "browser_download_url": srv.URL + "/assets/source.zip"}}},
				{"id": 2, "tag_name": "v1.0.0", "draft": false, "prerelease": false, "assets": []map[string]any{{"id": 12, "name": "plugin.zip", "size": len(themeZip), "browser_download_url": srv.URL + "/assets/theme.zip"}}},
			})
		case "/repos/acme/media-hub/releases":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"id": 3, "tag_name": "v1.1.0", "draft": false, "prerelease": false, "assets": []map[string]any{{"id": 13, "name": "plugin.zip", "size": len(sourceZip), "browser_download_url": srv.URL + "/assets/source.zip"}}},
			})
		case "/repos/acme/prerelease-only/releases":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"id": 4, "tag_name": "v0.1.0-rc1", "prerelease": true, "assets": []map[string]any{{"id": 14, "name": "plugin.zip", "size": len(sourceZip), "browser_download_url": srv.URL + "/assets/source.zip"}}},
				{"id": 5, "tag_name": "v0.1.0-draft", "draft": true, "assets": []map[string]any{{"id": 15, "name": "plugin.zip", "size": len(sourceZip), "browser_download_url": srv.URL + "/assets/source.zip"}}},
			})
		case "/assets/theme.zip":
			zipDownloads.Add(1)
			_, _ = w.Write(themeZip)
		case "/assets/source.zip":
			zipDownloads.Add(1)
			_, _ = w.Write(sourceZip)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	client := NewCatalogClient("0.1.0", srv.URL)
	p1, err := client.FetchPage(context.Background(), 1, "")
	if err != nil {
		t.Fatalf("FetchPage 1: %v", err)
	}
	if !p1.HasMore || p1.ETag != `"etag-v1"` {
		t.Fatalf("unexpected page 1 metadata: %+v", p1)
	}
	if len(p1.Items) != 1 {
		t.Fatalf("expected 1 official release item (deduped + skipped prerelease/archived), got %d: %+v", len(p1.Items), p1.Items)
	}
	if p1.Items[0].Manifest.Kind != ThemeKind {
		t.Fatalf("kind must come from official release plugin.json (theme), not repo name; got %q", p1.Items[0].Manifest.Kind)
	}
	if p1.Items[0].Release.TagName != "v1.0.0" {
		t.Fatalf("should skip prerelease and select v1.0.0, got %q", p1.Items[0].Release.TagName)
	}

	// ETag conditional request returns NotModified without re-downloading.
	p1Cached, err := client.FetchPage(context.Background(), 1, p1.ETag)
	if err != nil || !p1Cached.NotModified {
		t.Fatalf("expected 304 NotModified, got err=%v page=%+v", err, p1Cached)
	}

	// Page 2 reuses inspect cache for acme/video-source and fetches acme/media-hub.
	p2, err := client.FetchPage(context.Background(), 2, "")
	if err != nil {
		t.Fatalf("FetchPage 2: %v", err)
	}
	if p2.HasMore || len(p2.Items) != 2 {
		t.Fatalf("unexpected page 2: hasMore=%v len=%d", p2.HasMore, len(p2.Items))
	}
	if zipDownloads.Load() != 2 {
		t.Fatalf("expected exactly 2 zip downloads (cached acme/video-source reused), got %d", zipDownloads.Load())
	}

	// Rate limit error is cleanly typed.
	forceRateLimit.Store(true)
	_, err = client.FetchPage(context.Background(), 1, "")
	var rlErr *RateLimitError
	if !errors.As(err, &rlErr) || rlErr.ResetAt.IsZero() {
		t.Fatalf("expected typed RateLimitError with ResetAt, got %v", err)
	}
}

func TestParseRepositoryReleaseVersionResolveAndDownloadAsset(t *testing.T) {
	validCases := map[string]string{
		"demo/media-source":                         "demo/media-source",
		"https://github.com/demo/media-source":      "demo/media-source",
		"https://github.com/demo/media-source.git":  "demo/media-source",
		"http://www.github.com/demo/media-source/":  "demo/media-source",
		"github.com/Demo/Media-Source/releases/tag": "Demo/Media-Source",
	}
	for raw, want := range validCases {
		got, ok := ParseRepository(raw)
		if !ok || got != want {
			t.Errorf("ParseRepository(%q) = (%q, %v), want (%q, true)", raw, got, ok, want)
		}
	}
	for _, bad := range []string{"", "onlyowner", "https://gitlab.com/a/b", "bad repo/name", "a/b c"} {
		if got, ok := ParseRepository(bad); ok {
			t.Errorf("ParseRepository(%q) unexpectedly succeeded: %q", bad, got)
		}
	}

	if !ReleaseVersionMatches("v1.2.3", "1.2.3") || !ReleaseVersionMatches("1.2.3", "1.2.3") {
		t.Fatal("expected matching release tag and manifest version")
	}
	if ReleaseVersionMatches("v1.2.4", "1.2.3") || ReleaseVersionMatches("v2.0.0", "1.2.3") {
		t.Fatal("expected mismatched release tag and manifest version to fail")
	}

	goodZip := buildTestPluginZip(t, map[string]string{
		"plugin.json": `{
			"id": "demo-media",
			"name": "Demo Media",
			"description": "Videos",
			"kind": "source",
			"version": "1.2.0",
			"apiVersion": 1,
			"minAppVersion": "0.1.0",
			"main": "index.js",
			"permissions": {"network": ["api.demo.example"]},
			"capabilities": ["catalog.browse"]
		}`,
		"index.js": `export function browse() { return { collections: [] }; }`,
	})
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/demo/good":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 10, "full_name": "demo/good", "description": "Good repo", "owner": map[string]any{"login": "demo"}})
		case "/repos/demo/good/releases":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"id": 20, "tag_name": "v2.0.0-rc1", "prerelease": true, "assets": []map[string]any{{"id": 30, "name": "plugin.zip", "size": len(goodZip), "browser_download_url": srv.URL + "/good.zip"}}},
				{"id": 21, "tag_name": "v1.2.0", "draft": false, "prerelease": false, "assets": []map[string]any{{"id": 31, "name": "plugin.zip", "size": len(goodZip), "browser_download_url": srv.URL + "/good.zip"}}},
			})
		case "/repos/demo/mismatch":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 11, "full_name": "demo/mismatch", "owner": map[string]any{"login": "demo"}})
		case "/repos/demo/mismatch/releases":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"id": 22, "tag_name": "v9.9.9", "draft": false, "prerelease": false, "assets": []map[string]any{{"id": 32, "name": "plugin.zip", "size": len(goodZip), "browser_download_url": srv.URL + "/good.zip"}}},
			})
		case "/repos/demo/prerelease-only":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 12, "full_name": "demo/prerelease-only", "owner": map[string]any{"login": "demo"}})
		case "/repos/demo/prerelease-only/releases":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"id": 23, "tag_name": "v1.2.0-beta.1", "prerelease": true, "assets": []map[string]any{{"id": 33, "name": "plugin.zip", "size": len(goodZip), "browser_download_url": srv.URL + "/good.zip"}}},
			})
		case "/good.zip":
			_, _ = w.Write(goodZip)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	client := NewCatalogClient("0.1.0", srv.URL)
	item, err := client.ResolveRepository(context.Background(), "https://github.com/demo/good")
	if err != nil {
		t.Fatalf("ResolveRepository good: %v", err)
	}
	if item.Manifest.ID != "demo-media" || item.Release.TagName != "v1.2.0" || item.Release.SHA256 == "" {
		t.Fatalf("unexpected resolved item: %+v", item)
	}

	if _, err := client.ResolveRepository(context.Background(), "demo/mismatch"); !errors.Is(err, ErrVersionTagMismatch) {
		t.Fatalf("expected ErrVersionTagMismatch, got %v", err)
	}
	if _, err := client.ResolveRepository(context.Background(), "demo/prerelease-only"); !errors.Is(err, ErrNoOfficialRelease) {
		t.Fatalf("expected ErrNoOfficialRelease for prerelease-only, got %v", err)
	}

	// DownloadAsset checks SHA-256 when provided.
	if _, _, err := client.DownloadAsset(context.Background(), item.Release); err != nil {
		t.Fatalf("DownloadAsset valid: %v", err)
	}
	badAsset := item.Release
	badAsset.SHA256 = strings.Repeat("0", 64)
	if _, _, err := client.DownloadAsset(context.Background(), badAsset); !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("expected ErrDigestMismatch, got %v", err)
	}
}
