package plugin_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yuchanns/MikuTV/internal/plugin"
	"github.com/yuchanns/MikuTV/internal/provider"
)

func buildCredPluginZip(t *testing.T, manifest string, js string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	add := func(name string, data []byte) {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip create %s: %v", name, err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatalf("zip write %s: %v", name, err)
		}
	}
	add("plugin.json", []byte(manifest))
	add("index.js", []byte(js))
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

func makeTestPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for y := range 16 {
		for x := range 16 {
			img.Set(x, y, color.RGBA{R: 50, G: 180, B: 220, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

func TestCredentialsAuthManifestValidationAndInspection(t *testing.T) {
	ctx := context.Background()
	js := `
export async function signInWithCredentials(req) { return { account: { id: "u1", name: req.username }, secret: { token: "t1" } }; }
export async function restoreAccount(req) { return { account: req.account, secret: req.secret }; }
export async function signOut() { return {}; }
`
	manifest := `{
		"id": "self-hosted",
		"name": "Self Hosted",
		"description": "Self-hosted server",
		"kind": "source",
		"version": "1.0.0",
		"apiVersion": 1,
		"minAppVersion": "0.1.0",
		"main": "index.js",
		"permissions": { "network": [] },
		"capabilities": ["account.signIn"],
		"authentication": {
			"methods": ["credentials"],
			"serverURL": "required",
			"presentation": {
				"credentialsTitle": "Connect Server",
				"serverURLPlaceholder": "http://10.0.0.1:8096"
			}
		}
	}`
	zipData := buildCredPluginZip(t, manifest, js)

	inspected, err := plugin.InspectPackage(zipData, "0.1.0")
	if err != nil {
		t.Fatalf("InspectPackage: %v", err)
	}
	if !inspected.Compatibility.Compatible {
		t.Fatalf("expected compatible package, got %+v", inspected.Compatibility)
	}
	if !inspected.UserServerOrigin {
		t.Fatal("expected UserServerOrigin=true for serverURL=required")
	}

	src, err := plugin.Open(ctx, zipData, "0.1.0")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !src.HasAuthMethod(provider.AuthMethodCredentials) || src.HasAuthMethod(provider.AuthMethodQRCode) {
		t.Fatalf("unexpected auth methods: %v", src.AuthMethods())
	}
	if src.ServerURLMode() != "required" || !src.AllowsUserServerOrigin() {
		t.Fatalf("unexpected serverURL mode: %q", src.ServerURLMode())
	}
	pres := src.AuthPresentation()
	if pres.CredentialsTitle != "Connect Server" || pres.ServerURLPlaceholder != "http://10.0.0.1:8096" {
		t.Fatalf("unexpected AuthPresentation: %+v", pres)
	}

	// Static permissions.network must STILL reject private IPs even with credentials auth.
	badManifest := strings.Replace(manifest, `"network": []`, `"network": ["10.0.0.1"]`, 1)
	if _, err := plugin.Open(ctx, buildCredPluginZip(t, badManifest, js), "0.1.0"); err == nil {
		t.Fatal("expected static private IP in permissions.network to be rejected")
	}

	// Invalid serverURL value must be rejected.
	badMode := strings.Replace(manifest, `"serverURL": "required"`, `"serverURL": "any"`, 1)
	if _, err := plugin.Open(ctx, buildCredPluginZip(t, badMode, js), "0.1.0"); err == nil {
		t.Fatal("expected invalid authentication.serverURL to be rejected")
	}
}

func TestCredentialsAuthEndToEndAndUserOriginIsolation(t *testing.T) {
	ctx := context.Background()
	pngBytes := makeTestPNG(t)

	// Target self-hosted LAN/loopback server.
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth":
			var body struct {
				Username string `json:"username"`
				Password string `json:"password"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body.Username != "alice" || body.Password != "s3cret" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"userId": "u-alice",
				"name":   "Alice",
				"token":  "tok-123",
			})
		case "/me":
			if r.Header.Get("X-Token") != "tok-123" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"userId": "u-alice", "name": "Alice Restored"})
		case "/items":
			if r.Header.Get("X-Token") != "tok-123" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"title": "Homelab Movie",
			})
		case "/poster.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(pngBytes)
		case "/stream.mp4":
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = w.Write([]byte("mp4data"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer primary.Close()

	// Unrelated loopback server on a different port: MUST be blocked even during an active session!
	otherHits := 0
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		otherHits++
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"leaked":true}`))
	}))
	defer other.Close()

	js := `
export async function signInWithCredentials(req) {
	const resp = await fetch(req.serverURL + "/auth", {
		method: "POST",
		headers: { "Content-Type": "application/json" },
		body: JSON.stringify({ username: req.username, password: req.password })
	});
	if (resp.status === 401) {
		throw { code: "signIn", message: "unauthorized" };
	}
	const data = await resp.json();
	return {
		account: {
			id: data.userId,
			name: data.name,
			avatarURL: req.serverURL + "/poster.png"
		},
		secret: {
			serverURL: req.serverURL,
			token: data.token
		}
	};
}

export async function restoreAccount(secret) {
	const base = secret && secret.serverURL;
	const token = secret && secret.token;
	const resp = await fetch(base + "/me", { headers: { "X-Token": token } });
	if (!resp.ok) throw { code: "signIn", message: "expired" };
	const data = await resp.json();
	return {
		account: { id: data.userId, name: data.name },
		secret
	};
}

export async function signOut() { return {}; }

export async function browse(req) {
	if (req.filter === "probe-other") {
		await fetch("` + other.URL + `/secret");
		return { collections: [] };
	}
	const base = req.secret && req.secret.serverURL;
	const token = req.secret && req.secret.token;
	if (!base || !token) throw { code: "signIn", message: "sign in required" };
	const resp = await fetch(base + "/items", { headers: { "X-Token": token } });
	if (!resp.ok) throw { code: "signIn", message: "unauthorized" };
	const data = await resp.json();
	return {
		collections: [{
			id: "movies",
			title: "Movies",
			items: [{
				ref: { kind: "video", id: "m1" },
				title: data.title,
				coverURL: base + "/poster.png"
			}]
		}]
	};
}

export async function resolvePlayback(req) {
	const base = req.secret && req.secret.serverURL;
	const token = req.secret && req.secret.token;
	return {
		quality: "1080p",
		streams: [{
			format: "direct",
			video: {
				resource: {
					urls: [base + "/stream.mp4?api_key=" + encodeURIComponent(token)]
				}
			}
		}],
		qualities: [{ id: "1080p", label: "1080p" }]
	};
}
`
	manifest := `{
		"id": "homelab-test",
		"name": "Homelab",
		"description": "LAN media test",
		"kind": "source",
		"version": "1.0.0",
		"apiVersion": 1,
		"minAppVersion": "0.1.0",
		"main": "index.js",
		"permissions": { "network": [] },
		"capabilities": ["account.signIn", "catalog.browse", "playback.resolve"],
		"authentication": {
			"methods": ["credentials"],
			"serverURL": "required"
		}
	}`

	src, err := plugin.Open(ctx, buildCredPluginZip(t, manifest, js), "0.1.0")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	session := src.NewSession("homelab-test", nil)
	defer session.Close()

	// Wrong password -> NeedsSignIn.
	if _, err := session.SignInWithCredentials(ctx, provider.CredentialsRequest{
		ServerURL: primary.URL,
		Username:  "alice",
		Password:  "wrong",
	}); provider.CodeOf(err) != provider.NeedsSignIn {
		t.Fatalf("wrong password: want NeedsSignIn, got %v", err)
	}

	// Valid credentials against HTTP loopback server!
	cred, err := session.SignInWithCredentials(ctx, provider.CredentialsRequest{
		ServerURL: primary.URL,
		Username:  "alice",
		Password:  "s3cret",
	})
	if err != nil {
		t.Fatalf("SignInWithCredentials: %v", err)
	}
	if cred.Account.ID != "u-alice" || cred.Account.Name != "Alice" {
		t.Fatalf("unexpected account: %+v", cred.Account)
	}

	// RestoreAccount on a brand-new session must reconstruct userOrigin from Keychain data.
	session2 := src.NewSession("homelab-test", nil)
	defer session2.Close()
	restored, err := session2.RestoreAccount(ctx, cred)
	if err != nil {
		t.Fatalf("RestoreAccount: %v", err)
	}
	if restored.Account.Name != "Alice Restored" {
		t.Fatalf("unexpected restored account: %+v", restored.Account)
	}
	if err := session2.SetAccount(&restored); err != nil {
		t.Fatalf("SetAccount: %v", err)
	}

	// Browse over HTTP user origin works and receives secret.
	page, err := session2.Browse(ctx, provider.BrowseRequest{})
	if err != nil {
		t.Fatalf("Browse: %v", err)
	}
	if len(page.Collections) != 1 || len(page.Collections[0].Items) != 1 || page.Collections[0].Items[0].Title != "Homelab Movie" {
		t.Fatalf("unexpected browse result: %+v", page)
	}
	coverURL := page.Collections[0].Items[0].CoverURL
	if coverURL != primary.URL+"/poster.png" {
		t.Fatalf("expected user-origin http coverURL preserved, got %q", coverURL)
	}

	// LoadCatalogArtwork from exact user origin over HTTP works.
	if _, err := session2.LoadCatalogArtwork(ctx, coverURL); err != nil {
		t.Fatalf("LoadCatalogArtwork on user origin: %v", err)
	}
	// LoadCatalogArtwork from another loopback port MUST fail!
	if _, err := session2.LoadCatalogArtwork(ctx, other.URL+"/poster.png"); err == nil {
		t.Fatal("expected LoadCatalogArtwork to different loopback port to be blocked")
	}

	// ResolvePlayback on exact user origin over HTTP preserves stream URL and Transport opens it.
	pb, err := session2.ResolvePlayback(ctx, provider.PlaybackRequest{
		PlaybackTarget: provider.PlaybackTarget{Ref: provider.MediaRef{EntryID: "homelab-test", Kind: provider.Video, ID: "m1"}},
	})
	if err != nil {
		t.Fatalf("ResolvePlayback: %v", err)
	}
	wantStream := primary.URL + "/stream.mp4?api_key=tok-123"
	if len(pb.Plan.Streams) != 1 || len(pb.Plan.Streams[0].Video.Resource.URLs) != 1 || pb.Plan.Streams[0].Video.Resource.URLs[0] != wantStream {
		t.Fatalf("unexpected playback plan: %+v", pb.Plan)
	}
	resp, err := pb.Transport.OpenResource(ctx, pb.Plan.Streams[0].Video.Resource, http.MethodGet, "")
	if err != nil {
		t.Fatalf("OpenResource on user origin: %v", err)
	}
	resp.Body.Close()

	// Confirming rotated credentials for the SAME account must preserve the active playback transport scope.
	if err := session2.SetAccount(&restored); err != nil {
		t.Fatalf("SetAccount same account refresh: %v", err)
	}
	respSame, err := pb.Transport.OpenResource(ctx, pb.Plan.Streams[0].Video.Resource, http.MethodGet, "")
	if err != nil {
		t.Fatalf("OpenResource after same-account SetAccount failed: %v", err)
	}
	respSame.Body.Close()

	// SSRF isolation check: fetch to a DIFFERENT port on 127.0.0.1 MUST be blocked!
	if _, err := session2.Browse(ctx, provider.BrowseRequest{Filter: "probe-other"}); err == nil {
		t.Fatal("expected fetch to unrelated loopback port to be blocked")
	}
	if otherHits != 0 {
		t.Fatalf("unrelated server received %d hits, want 0", otherHits)
	}

	// Clearing account revokes user origin AND cancels active playback transport scope.
	if err := session2.SetAccount(nil); err != nil {
		t.Fatalf("SetAccount(nil): %v", err)
	}
	if _, err := pb.Transport.OpenResource(ctx, pb.Plan.Streams[0].Video.Resource, http.MethodGet, ""); err == nil {
		t.Fatal("expected active playback transport to be canceled after sign-out")
	}
	if _, err := session2.LoadCatalogArtwork(ctx, coverURL); err == nil {
		t.Fatal("expected LoadCatalogArtwork after sign-out to be blocked")
	}
}
