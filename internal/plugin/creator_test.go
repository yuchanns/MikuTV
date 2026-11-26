package plugin

import (
	"errors"
	"testing"

	"github.com/yuchanns/MikuTV/internal/provider"
)

func TestCreatorManifestAndSanitization(t *testing.T) {
	// FollowCreator requires SignIn.
	manifest := `{
		"id": "no-signin-follow", "name": "Test", "kind": "source",
		"version": "1.0.0", "apiVersion": 1, "minAppVersion": "0.1.0",
		"main": "index.js",
		"capabilities": ["catalog.creator", "account.followCreator"]
	}`
	code := `export async function readCreator() { return {}; } export async function followCreator() { return {}; }`
	if _, err := Open(t.Context(), zipFiles(t, map[string][]byte{
		"plugin.json": []byte(manifest), "index.js": []byte(code),
	}), testVersion); !errors.Is(err, ErrIncompatible) {
		t.Fatalf("expected ErrIncompatible when followCreator declared without signIn, got %v", err)
	}

	// Sanitization rejects mismatched creator ID and strips non-HTTPS URLs.
	goodManifest := `{
		"id": "creator-sanitize", "name": "Sanitize", "kind": "source",
		"version": "1.0.0", "apiVersion": 1, "minAppVersion": "0.1.0",
		"main": "index.js",
		"capabilities": ["catalog.creator"]
	}`
	badIDCode := `export async function readCreator({ creatorID }) {
		if (creatorID === "bad") return { profile: { creator: { id: "other", name: "Wrong" } } };
		return {
			profile: {
				creator: { id: creatorID, name: "Clean", avatarURL: "http://insecure.example/a.jpg" },
				backdropURL: "javascript:alert(1)",
				tabs: ["videos", "videos", "about"]
			},
			posts: [{ id: "p1", imageURL: "http://insecure.example/b.jpg", body: "Hello" }]
		};
	}`
	src, err := Open(t.Context(), zipFiles(t, map[string][]byte{
		"plugin.json": []byte(goodManifest), "index.js": []byte(badIDCode),
	}), testVersion)
	if err != nil {
		t.Fatal(err)
	}
	sess := src.NewSession("creator-sanitize", memoryStorageForTest(t))
	defer sess.Close()

	if _, err := sess.ReadCreator(t.Context(), provider.CreatorRequest{CreatorID: "bad", IncludeProfile: true}); provider.CodeOf(err) != provider.PluginFailed {
		t.Fatalf("expected PluginFailed for mismatched creator id, got %v", err)
	}
	page, err := sess.ReadCreator(t.Context(), provider.CreatorRequest{CreatorID: "good", IncludeProfile: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if page.Profile.Creator.AvatarURL != "" || page.Profile.BackdropURL != "" || page.Posts[0].ImageURL != "" {
		t.Fatalf("expected non-HTTPS URLs stripped, got %+v", page)
	}
	if len(page.Profile.Tabs) != 2 || page.Profile.Tabs[0] != provider.CreatorTabVideos || page.Profile.Tabs[1] != provider.CreatorTabAbout {
		t.Fatalf("expected deduplicated tabs, got %+v", page.Profile.Tabs)
	}
}

func TestCreatorSanitizationAllowsUserOriginHTTP(t *testing.T) {
	manifest := `{
		"id": "creator-lan-http", "name": "LAN Source", "kind": "source",
		"version": "1.0.0", "apiVersion": 1, "minAppVersion": "0.1.0",
		"main": "index.js",
		"authentication": { "methods": ["credentials"], "serverURL": "required" },
		"capabilities": ["account.signIn", "catalog.creator"]
	}`
	code := `
	export async function signInWithCredentials(req) {
		return {
			account: { id: "u1", name: req.username },
			secret: { serverURL: req.serverURL }
		};
	}
	export async function restoreAccount(req) { return { account: { id: "u1", name: "User" }, secret: req }; }
	export async function signOut() { return {}; }
	export async function readCreator({ creatorID }) {
		return {
			profile: {
				creator: { id: creatorID, name: "Director", avatarURL: "http://192.168.1.50:8096/Items/p1/Images/Primary" },
				backdropURL: "http://192.168.1.50:8096/Items/p1/Images/Backdrop/0",
				tabs: ["videos"]
			},
			posts: [
				{ id: "p-ok", imageURL: "http://192.168.1.50:8096/Items/p1/Images/Post" },
				{ id: "p-foreign", imageURL: "http://evil.example/a.jpg" }
			]
		};
	}`
	src, err := Open(t.Context(), zipFiles(t, map[string][]byte{
		"plugin.json": []byte(manifest), "index.js": []byte(code),
	}), testVersion)
	if err != nil {
		t.Fatal(err)
	}
	sess := src.NewSession("creator-lan-http", memoryStorageForTest(t))
	defer sess.Close()

	cred, err := sess.SignInWithCredentials(t.Context(), provider.CredentialsRequest{
		ServerURL: "http://192.168.1.50:8096",
		Username:  "demo",
		Password:  "secret",
	})
	if err != nil {
		t.Fatalf("SignInWithCredentials: %v", err)
	}
	if err := sess.SetAccount(&cred); err != nil {
		t.Fatalf("SetAccount: %v", err)
	}
	page, err := sess.ReadCreator(t.Context(), provider.CreatorRequest{CreatorID: "p1", IncludeProfile: true})
	if err != nil {
		t.Fatalf("ReadCreator: %v", err)
	}
	if page.Profile.Creator.AvatarURL != "http://192.168.1.50:8096/Items/p1/Images/Primary" {
		t.Fatalf("expected user-origin HTTP avatar preserved, got %q", page.Profile.Creator.AvatarURL)
	}
	if page.Profile.BackdropURL != "http://192.168.1.50:8096/Items/p1/Images/Backdrop/0" {
		t.Fatalf("expected user-origin HTTP backdrop preserved, got %q", page.Profile.BackdropURL)
	}
	if page.Posts[0].ImageURL != "http://192.168.1.50:8096/Items/p1/Images/Post" {
		t.Fatalf("expected user-origin HTTP post image preserved, got %q", page.Posts[0].ImageURL)
	}
	if page.Posts[1].ImageURL != "" {
		t.Fatalf("expected foreign HTTP post image stripped, got %q", page.Posts[1].ImageURL)
	}
}
