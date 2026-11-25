package plugin

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yuchanns/MikuTV/internal/provider"
)

func TestSessionCacheLRUExpiryAndQuota(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	cache := newSessionCache(64, 32, 4, func() time.Time { return now })

	if err := cache.Set("", "v", 0); err == nil {
		t.Fatal("expected error for empty key")
	}
	if err := cache.Set("huge", strings.Repeat("x", 33), 0); err == nil {
		t.Fatal("expected error for entry exceeding maxEntry")
	}

	if err := cache.Set("k1", "11111111111111111111", 10*time.Second); err != nil { // 22 bytes
		t.Fatal(err)
	}
	if err := cache.Set("k2", "22222222222222222222", 0); err != nil { // 22 bytes (total 44)
		t.Fatal(err)
	}
	// Access k1 so k2 becomes LRU.
	if got, ok := cache.Get("k1"); !ok || got != "11111111111111111111" {
		t.Fatalf("Get(k1) = %q,%t", got, ok)
	}
	// Adding k3 (22 bytes) pushes total to 66 > 64 -> must evict k2 (LRU), keeping k1 and k3.
	if err := cache.Set("k3", "33333333333333333333", 0); err != nil {
		t.Fatal(err)
	}
	if _, ok := cache.Get("k2"); ok {
		t.Fatal("expected LRU k2 evicted")
	}
	if _, ok := cache.Get("k1"); !ok {
		t.Fatal("expected MRU k1 preserved")
	}

	// Advance clock past k1's 10s TTL -> k1 must expire lazily and free bytes.
	now = now.Add(11 * time.Second)
	if _, ok := cache.Get("k1"); ok {
		t.Fatal("expected expired k1 to miss")
	}
	if cache.Len() != 1 || cache.Bytes() != 22 {
		t.Fatalf("after expiry len=%d bytes=%d, want 1, 22", cache.Len(), cache.Bytes())
	}

	cache.Delete("k3")
	if cache.Len() != 0 || cache.Bytes() != 0 {
		t.Fatalf("after delete len=%d bytes=%d, want 0, 0", cache.Len(), cache.Bytes())
	}
}

func TestSessionCacheConcurrentAccess(t *testing.T) {
	cache := NewSessionCache()
	var wg sync.WaitGroup
	for g := range 8 {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := range 200 {
				key := fmt.Sprintf("k-%d-%d", id, i%32)
				_ = cache.Set(key, fmt.Sprintf("val-%d", i), time.Minute)
				_, _ = cache.Get(key)
				if i%17 == 0 {
					cache.Delete(key)
				}
			}
		}(g)
	}
	wg.Wait()
	cache.Clear()
	if cache.Len() != 0 || cache.Bytes() != 0 {
		t.Fatalf("expected empty cache after Clear, got len=%d bytes=%d", cache.Len(), cache.Bytes())
	}
}

func TestHostCachePersistsAcrossCallsAndClearsOnAccountSwitch(t *testing.T) {
	manifest := `{
		"id": "cache-test",
		"name": "Cache Test",
		"kind": "source",
		"version": "1.0.0",
		"apiVersion": 1,
		"minAppVersion": "0.1.0",
		"main": "index.js",
		"authentication": {"methods": ["qrCode"]},
		"capabilities": ["account.signIn", "catalog.browse"]
	}`
	index := `
export async function beginSignIn() {
  host.cache.set("leak", "should-not-persist", 60);
  return { url: "https://example.com/qr", key: "k", expiresAt: Date.now() + 60000 };
}
export async function pollSignIn() { return { status: "waiting" }; }
export async function restoreAccount() { throw { code: "signIn" }; }
export async function signOut() {}
export async function browse({ collection }) {
  if (collection === "popular") {
    host.cache.delete("hits");
    return { collections: [{ id: "c", title: host.cache.get("hits") ?? "deleted", items: [] }] };
  }
  const prev = Number(host.cache.get("hits") ?? "0");
  host.cache.set("hits", String(prev + 1), 60);
  const leak = host.cache.get("leak") ?? "none";
  return { collections: [{ id: "c", title: String(prev + 1) + ":" + leak, items: [] }] };
}`
	pkg := zipFiles(t, map[string][]byte{
		"plugin.json": []byte(manifest),
		"index.js":    []byte(index),
	})
	src, err := Open(t.Context(), pkg, testVersion)
	if err != nil {
		t.Fatal(err)
	}
	sess := src.NewSession("cache-test", memoryStorageForTest(t))
	defer sess.Close()

	// Sign-in calls receive isolated nil cache and cannot write into active session cache.
	if _, err := sess.BeginSignIn(t.Context()); err != nil {
		t.Fatalf("BeginSignIn: %v", err)
	}
	page1, err := sess.Browse(t.Context(), provider.BrowseRequest{})
	if err != nil {
		t.Fatalf("Browse #1: %v", err)
	}
	if got := page1.Collections[0].Title; got != "1:none" {
		t.Fatalf("Browse #1 title = %q, want 1:none", got)
	}

	// Second call in a brand-new moejs runtime still hits session cache!
	page2, err := sess.Browse(t.Context(), provider.BrowseRequest{})
	if err != nil {
		t.Fatalf("Browse #2: %v", err)
	}
	if got := page2.Collections[0].Title; got != "2:none" {
		t.Fatalf("Browse #2 title = %q, want 2:none", got)
	}

	// Switching account clears session cache so Account A's data never leaks into Account B.
	credA := &provider.Credential{Account: provider.Account{ID: "acc-a", Name: "A"}, Data: []byte(`{"secret":{"uid":"a"}}`)}
	credB := &provider.Credential{Account: provider.Account{ID: "acc-b", Name: "B"}, Data: []byte(`{"secret":{"uid":"b"}}`)}
	if err := sess.SetAccount(credA); err != nil {
		t.Fatalf("SetAccount(A): %v", err)
	}
	pageA, err := sess.Browse(t.Context(), provider.BrowseRequest{})
	if err != nil {
		t.Fatalf("Browse(A): %v", err)
	}
	if got := pageA.Collections[0].Title; got != "1:none" {
		t.Fatalf("Browse(A) after sign-in title = %q, want 1:none", got)
	}
	// Same-account refresh preserves cache.
	if err := sess.SetAccount(credA); err != nil {
		t.Fatalf("SetAccount(A refresh): %v", err)
	}
	pageA2, err := sess.Browse(t.Context(), provider.BrowseRequest{})
	if err != nil {
		t.Fatalf("Browse(A2): %v", err)
	}
	if got := pageA2.Collections[0].Title; got != "2:none" {
		t.Fatalf("Browse(A2) same-account title = %q, want 2:none", got)
	}
	// Switching to Account B wipes Account A's cache.
	if err := sess.SetAccount(credB); err != nil {
		t.Fatalf("SetAccount(B): %v", err)
	}
	pageB, err := sess.Browse(t.Context(), provider.BrowseRequest{})
	if err != nil {
		t.Fatalf("Browse(B): %v", err)
	}
	if got := pageB.Collections[0].Title; got != "1:none" {
		t.Fatalf("Browse(B) after account switch title = %q, want 1:none", got)
	}

	// Delete removes the cached entry.
	pageDel, err := sess.Browse(t.Context(), provider.BrowseRequest{Collection: "popular"})
	if err != nil {
		t.Fatalf("Browse(popular delete): %v", err)
	}
	if got := pageDel.Collections[0].Title; got != "deleted" {
		t.Fatalf("Browse(popular delete) title = %q, want deleted", got)
	}
}
