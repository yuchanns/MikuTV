package plugin

import (
	"errors"
	"testing"

	"github.com/yuchanns/MikuTV/internal/provider"
)

func TestFollowingManifestRequiresSignIn(t *testing.T) {
	manifest := `{
		"id": "no-auth-following",
		"name": "No Auth Following",
		"kind": "source",
		"version": "0.1.0",
		"apiVersion": 1,
		"minAppVersion": "0.1.0",
		"main": "index.js",
		"capabilities": ["catalog.following"]
	}`
	pkg := zipFiles(t, map[string][]byte{
		"plugin.json": []byte(manifest),
		"index.js":    []byte(`export async function readFollowing() { return {}; }`),
	})
	_, err := Open(t.Context(), pkg, testVersion)
	if !errors.Is(err, ErrIncompatible) {
		t.Fatalf("expected ErrIncompatible when catalog.following lacks account.signIn, got %v", err)
	}
}

func TestFollowingRuntimeSanitizesOutput(t *testing.T) {
	manifest := `{
		"id": "custom-following",
		"name": "Custom Following",
		"kind": "source",
		"version": "0.1.0",
		"apiVersion": 1,
		"minAppVersion": "0.1.0",
		"main": "index.js",
		"authentication": {"methods": ["qrCode"]},
		"capabilities": ["account.signIn", "catalog.following"]
	}`
	index := `
export async function beginSignIn() { return { url: "https://example.com/qr", key: "k", expiresAt: Date.now() + 60000 }; }
export async function pollSignIn() { return { status: "waiting" }; }
export async function restoreAccount() { throw { code: "signIn" }; }
export async function signOut() {}
export async function readFollowing({ section }) {
  if (section === "creators") {
    return { creators: [{ creator: { id: "", name: "Bad" } }] };
  }
  return {
    creators: [{ creator: { id: "c1", name: "Alice", avatarURL: "http://insecure.example/a.png" } }],
    updates: [{ ref: { kind: "video", id: "v1" }, title: "Hello", coverURL: "http://insecure.example/c.png" }],
    supportsLive: false
  };
}`
	pkg := zipFiles(t, map[string][]byte{
		"plugin.json": []byte(manifest),
		"index.js":    []byte(index),
	})
	src, err := Open(t.Context(), pkg, testVersion)
	if err != nil {
		t.Fatal(err)
	}
	sess := src.NewSession("custom-following", memoryStorageForTest(t))
	defer sess.Close()
	sess.requests.Lock()
	sess.accountID = "u1"
	sess.requests.Unlock()

	page, err := sess.ReadFollowing(t.Context(), provider.FollowingRequest{Section: provider.FollowingOverview})
	if err != nil {
		t.Fatalf("overview failed: %v", err)
	}
	if page.SupportsLive {
		t.Fatal("expected SupportsLive=false")
	}
	if len(page.Creators) != 1 || page.Creators[0].Creator.AvatarURL != "" || !page.Creators[0].Creator.Following {
		t.Fatalf("expected non-https avatar stripped and Following=true, got %+v", page.Creators)
	}
	if len(page.Updates) != 1 || page.Updates[0].CoverURL != "" || page.Updates[0].Ref.EntryID != "custom-following" {
		t.Fatalf("expected non-https cover stripped and EntryID owned, got %+v", page.Updates)
	}
	if _, err := sess.ReadFollowing(t.Context(), provider.FollowingRequest{Section: provider.FollowingCreators}); provider.CodeOf(err) != provider.PluginFailed {
		t.Fatalf("expected PluginFailed for empty creator ID, got %v", err)
	}
	if _, err := sess.ReadFollowing(t.Context(), provider.FollowingRequest{Section: "invalid"}); provider.CodeOf(err) != provider.Denied {
		t.Fatalf("expected Denied for invalid section, got %v", err)
	}
}
