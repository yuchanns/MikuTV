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
