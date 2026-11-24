package plugin

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/yuchanns/MikuTV/internal/provider"
)

type testRoundTripper func(*http.Request) (*http.Response, error)

func (fn testRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestManifestRejectsWildcardTLDsPrivateIPsAndBadIDs(t *testing.T) {
	for _, host := range []string{
		"*.com", "*.cn", "*.co.uk", "*.0.0.1", "*.127.0.0.1",
		"127.0.0.1", "::1", "192.168.1.1", "10.0.0.1", "169.254.169.254",
		"100.64.0.1", "localhost", "api.localhost",
	} {
		if validHostPattern(host) {
			t.Errorf("validHostPattern(%q) = true, want false", host)
		}
	}
	for _, host := range []string{
		"api.media.example", "*.cdn-a.media.example", "*.cdn-b.media.example", "8.8.8.8",
	} {
		if !validHostPattern(host) {
			t.Errorf("validHostPattern(%q) = false, want true", host)
		}
	}
	for _, badID := range []string{"", "BadID", "bad id", "-leading", "a/b", "\u4e2d\u6587"} {
		if validPluginID(badID) {
			t.Errorf("validPluginID(%q) = true, want false", badID)
		}
	}
	// BrowseLibrary must require SignIn.
	data := zipFiles(t, map[string][]byte{
		"plugin.json": manifestForTest(t, func(m *sourceManifest) {
			m.Capabilities = provider.Capabilities{provider.BrowseCatalog, provider.BrowseLibrary}
		}),
		"index.js": []byte("export function browse() {} export function readAccount() {}"),
	})
	if _, err := Open(t.Context(), data, testVersion); !errors.Is(err, ErrIncompatible) {
		t.Fatalf("BrowseLibrary without SignIn accepted: %v", err)
	}
}

func TestCrossOriginRedirectChainStripsSensitiveHeaders(t *testing.T) {
	source := openForTest(t, "export function browse() { return {collections: []}; }", func(m *sourceManifest) {
		m.Permissions.Network = []string{"origin.example", "other.example"}
	})
	client := source.httpClient()
	originURL, _ := url.Parse("https://origin.example/start")
	other1, _ := url.Parse("https://other.example/hop1")
	other2, _ := url.Parse("https://other.example/hop2")

	req := &http.Request{Method: http.MethodGet, URL: other2, Header: http.Header{"Authorization": {"Bearer secret"}, "Origin": {"https://origin.example"}}}
	via := []*http.Request{
		{Method: http.MethodGet, URL: originURL, Header: http.Header{"Authorization": {"Bearer secret"}}},
		{Method: http.MethodGet, URL: other1, Header: http.Header{}},
	}
	if err := client.CheckRedirect(req, via); err != nil {
		t.Fatalf("CheckRedirect: %v", err)
	}
	if req.Header.Get("Authorization") != "" || req.Header.Get("Origin") != "" {
		t.Fatalf("cross-origin A->B->B leaked headers: %+v", req.Header)
	}
}

func TestMediaRedirectPreservesAllowedMediaHeadersAndFailsOverOnTimeout(t *testing.T) {
	source := openForTest(t, `
export function resolvePlayback() {
  return {
    partID: "1",
    streams: [{
      format: "direct",
      video: { resource: { urls: ["https://edge1.cdn-a.media.example/v.mp4", "https://backup.cdn-b.media.example/v.mp4"], headers: { Referer: "https://www.media.example/", "User-Agent": "MikuTV-Test" } } }
    }]
  };
}
`, func(m *sourceManifest) {
		m.Capabilities = provider.Capabilities{provider.ResolvePlayback}
		m.Permissions.Network = []string{"*.cdn-a.media.example", "*.cdn-b.media.example"}
	})
	session := source.NewSession("entry", memoryStorageForTest(t))
	defer session.Close()

	resolved, err := session.ResolvePlayback(t.Context(), provider.PlaybackRequest{
		PlaybackTarget: provider.PlaybackTarget{Ref: provider.MediaRef{EntryID: "entry", Kind: provider.Movie, ID: "m1"}},
	})
	if err != nil {
		t.Fatalf("ResolvePlayback non-video kind: %v", err)
	}
	pt := resolved.Transport.(*playbackTransport)
	if tr, ok := pt.client.Transport.(*http.Transport); !ok || tr.ResponseHeaderTimeout <= 0 {
		t.Fatalf("mediaClient missing ResponseHeaderTimeout for backup CDN failover")
	}

	// Verify authorized cross-host CDN redirect keeps Referer/UA/Range and strips Cookie/Authorization.
	u0, _ := url.Parse("https://edge1.cdn-a.media.example/v.mp4")
	u1, _ := url.Parse("https://edge2.cdn-b.media.example/edge.mp4")
	u2, _ := url.Parse("https://edge2.cdn-b.media.example/edge2.mp4")
	req := &http.Request{Method: http.MethodGet, URL: u2, Header: http.Header{
		"Referer":       {"https://www.media.example/"},
		"User-Agent":    {"MikuTV-Test"},
		"Range":         {"bytes=0-100"},
		"Cookie":        {"SESSION=secret"},
		"Authorization": {"Bearer secret"},
	}}
	via := []*http.Request{
		{Method: http.MethodGet, URL: u0, Header: req.Header.Clone()},
		{Method: http.MethodGet, URL: u1, Header: http.Header{}},
	}
	if err := pt.client.CheckRedirect(req, via); err != nil {
		t.Fatalf("authorized CDN redirect failed: %v", err)
	}
	if req.Header.Get("Referer") != "https://www.media.example/" || req.Header.Get("User-Agent") != "MikuTV-Test" || req.Header.Get("Range") != "bytes=0-100" {
		t.Fatalf("authorized CDN redirect lost safe headers: %+v", req.Header)
	}
	if req.Header.Get("Cookie") != "" || req.Header.Get("Authorization") != "" {
		t.Fatalf("authorized CDN redirect leaked auth headers: %+v", req.Header)
	}
}

func TestArtworkDecodesToTightRGBAOffMainThread(t *testing.T) {
	allowLocalhostForTest(t)
	var jpg bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	img.Set(0, 0, color.RGBA{R: 200, G: 100, B: 50, A: 255})
	if err := jpeg.Encode(&jpg, img, nil); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(jpg.Bytes())
	}))
	defer srv.Close()

	source := openForTest(t, "export function browse() { return {collections:[]}; }", func(m *sourceManifest) {
		m.Permissions.Network = []string{"127.0.0.1"}
	})
	session := source.NewSession("entry", memoryStorageForTest(t))
	defer session.Close()
	session.client.Transport = srv.Client().Transport

	decoded, err := session.LoadCatalogArtwork(t.Context(), srv.URL+"/cover.jpg")
	if err != nil {
		t.Fatalf("LoadCatalogArtwork: %v", err)
	}
	rgba, ok := decoded.(*image.RGBA)
	if !ok || rgba.Stride != 16*4 || rgba.Rect != image.Rect(0, 0, 16, 16) {
		t.Fatalf("expected tight *image.RGBA, got %T (%+v)", decoded, decoded.Bounds())
	}
}

func TestHostStorageDeleteRecoversQuota(t *testing.T) {
	source := openForTest(t, `
export function browse() {
  host.storage.set("temp", "x".repeat(6000));
  host.storage.delete("temp");
  host.storage.set("next", "y".repeat(6000));
  return { collections: [{ id: "ok", title: host.storage.get("temp") ?? "cleared", items: [] }] };
}
`, nil)
	session := source.NewSession("entry", memoryStorageForTest(t))
	defer session.Close()
	page, err := session.Browse(t.Context(), provider.BrowseRequest{})
	if err != nil {
		t.Fatalf("storage delete did not free quota: %v (%v)", err, errors.Unwrap(err))
	}
	if page.Collections[0].Title != "cleared" {
		t.Fatalf("unexpected title: %q", page.Collections[0].Title)
	}
}

func TestPlaybackTransportStickyCDNAndSlowFailover(t *testing.T) {
	source := openForTest(t, `
export function resolvePlayback() {
  return {
    partID: "1",
    streams: [{
      format: "direct",
      video: { resource: { urls: ["https://cdn1.cdn-a.media.example/v.mp4", "https://cdn2.cdn-b.media.example/v.mp4"] } }
    }]
  };
}
`, func(m *sourceManifest) {
		m.Capabilities = provider.Capabilities{provider.ResolvePlayback}
		m.Permissions.Network = []string{"*.cdn-a.media.example", "*.cdn-b.media.example"}
	})
	session := source.NewSession("entry", memoryStorageForTest(t))
	defer session.Close()

	var hosts []string
	failCDN1 := true
	session.client.Transport = testRoundTripper(func(req *http.Request) (*http.Response, error) {
		hosts = append(hosts, req.URL.Host)
		if req.URL.Host == "cdn1.cdn-a.media.example" && failCDN1 {
			return &http.Response{
				StatusCode: http.StatusServiceUnavailable,
				Header:     make(http.Header),
				Body:       http.NoBody,
				Request:    req,
			}, nil
		}
		payload := bytes.Repeat([]byte("A"), 128<<10)
		return &http.Response{
			StatusCode: http.StatusPartialContent,
			Header:     http.Header{"Content-Range": {"bytes 0-131071/262144"}},
			Body:       &readCloserBuffer{Reader: bytes.NewReader(payload)},
			Request:    req,
		}, nil
	})

	resolved, err := session.ResolvePlayback(t.Context(), provider.PlaybackRequest{
		PlaybackTarget: provider.PlaybackTarget{Ref: provider.MediaRef{EntryID: "entry", Kind: provider.Video, ID: "v1"}},
	})
	if err != nil {
		t.Fatalf("ResolvePlayback: %v", err)
	}
	resource := resolved.Plan.Streams[0].Video.Resource
	// First request hits failing cdn1, fails over to cdn2, and records cdn2 healthy.
	resp, err := resolved.Transport.OpenResource(t.Context(), resource, http.MethodGet, "bytes=0-131071")
	if err != nil {
		t.Fatalf("first OpenResource: %v", err)
	}
	_, _ = bytes.NewBuffer(nil).ReadFrom(resp.Body)
	_ = resp.Body.Close()
	if len(hosts) != 2 || hosts[0] != "cdn1.cdn-a.media.example" || hosts[1] != "cdn2.cdn-b.media.example" {
		t.Fatalf("expected failover cdn1->cdn2, got %v", hosts)
	}

	// Second request should go straight to sticky healthy cdn2 without hitting cdn1.
	hosts = nil
	resp2, err := resolved.Transport.OpenResource(t.Context(), resource, http.MethodGet, "bytes=0-131071")
	if err != nil {
		t.Fatalf("second OpenResource: %v", err)
	}
	_, _ = bytes.NewBuffer(nil).ReadFrom(resp2.Body)
	_ = resp2.Body.Close()
	if len(hosts) != 1 || hosts[0] != "cdn2.cdn-b.media.example" {
		t.Fatalf("expected sticky cdn2, got %v", hosts)
	}

	// Now cdn1 recovers and cdn2 is reported slow; next request should rotate back to cdn1.
	reporter := resolved.Transport.(provider.PlaybackHealthReporter)
	if reporter.HasHealthyAlternative(resource.URLs, "https://cdn2.cdn-b.media.example/v.mp4") {
		t.Fatalf("cdn1 is still in penalty window; expected no healthy alternative yet")
	}
	// Clear cdn1 penalty by simulating recovery, then report cdn2 slow.
	failCDN1 = false
	session.cdn.mu.Lock()
	session.cdn.hosts["cdn1.cdn-a.media.example"].penaltyUntil = time.Now().Add(-time.Second)
	session.cdn.mu.Unlock()
	if !reporter.HasHealthyAlternative(resource.URLs, "https://cdn2.cdn-b.media.example/v.mp4") {
		t.Fatalf("expected cdn1 to be a healthy alternative once cooldown expired")
	}
	reporter.ReportSlow("https://cdn2.cdn-b.media.example/v.mp4")
	hosts = nil
	resp3, err := resolved.Transport.OpenResource(t.Context(), resource, http.MethodGet, "bytes=0-131071")
	if err != nil {
		t.Fatalf("third OpenResource: %v", err)
	}
	_, _ = bytes.NewBuffer(nil).ReadFrom(resp3.Body)
	_ = resp3.Body.Close()
	if len(hosts) != 1 || hosts[0] != "cdn1.cdn-a.media.example" {
		t.Fatalf("expected rotation to cdn1 after reporting cdn2 slow, got %v", hosts)
	}
}

type readCloserBuffer struct {
	*bytes.Reader
}

func (readCloserBuffer) Close() error { return nil }
