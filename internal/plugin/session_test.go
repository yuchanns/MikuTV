package plugin

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yuchanns/MikuTV/internal/provider"
)

func memoryStorageForTest(t *testing.T) *Storage {
	t.Helper()
	storage, err := NewStorage(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return storage
}

func TestAsyncCollectionAndEntryStorage(t *testing.T) {
	source := openForTest(t, `
export async function browse() {
  const next = Number(host.storage.get("count") ?? "0") + 1;
  host.storage.set("count", String(next));
  await new Promise(resolve => setTimeout(resolve, 10));
  return { collections: [{
    id: "films", title: "Films", items: [
      {ref: { kind: "movie", id: "m1" }, title: String(next)},
      {ref: { kind: "series", id: "s1" }, title: "Series"}
    ]
  }] };
}
export function getItem(ref) {
  return {ref, title: "Details"};
}
`, func(m *sourceManifest) { m.Capabilities = append(m.Capabilities, provider.GetCatalogItem) })
	storeA, _ := NewStorage(nil, nil)
	storeB, _ := NewStorage(nil, nil)
	sessionA := source.NewSession("entry-a", storeA)
	sessionB := source.NewSession("entry-b", storeB)
	defer sessionB.Close()
	for _, tc := range []struct {
		session      *Session
		entry, title string
	}{{sessionA, "entry-a", "1"}, {sessionA, "entry-a", "2"}, {sessionB, "entry-b", "1"}} {
		page, err := tc.session.Browse(t.Context(), provider.BrowseRequest{})
		if err != nil {
			t.Fatalf("collection request: %v (%v)", err, errors.Unwrap(err))
		}
		item := page.Collections[0].Items[0]
		if item.Title != tc.title || item.Ref.EntryID != tc.entry || page.Collections[0].Items[1].Ref.Kind != provider.Series {
			t.Fatalf("unexpected entry collection: %+v", page)
		}
		got, err := tc.session.Item(t.Context(), item.Ref)
		if err != nil || got.Ref != item.Ref {
			t.Fatalf("item request did not preserve ownership: %+v, %v", got, err)
		}
	}
	sessionA.Close()
	reopened := source.NewSession("entry-a", storeA)
	defer reopened.Close()
	page, err := reopened.Browse(t.Context(), provider.BrowseRequest{})
	if err != nil || page.Collections[0].Items[0].Title != "3" {
		t.Fatalf("entry storage did not survive reactivation: %+v, %v", page, err)
	}
	if _, err := sessionB.Item(t.Context(), page.Collections[0].Items[0].Ref); provider.CodeOf(err) != provider.Denied {
		t.Fatalf("foreign entry reference accepted: %v", err)
	}
}

func TestRuntimeCancellationAndErrors(t *testing.T) {
	for _, tc := range []struct {
		name, script string
		code         provider.ErrorCode
		timeout      time.Duration
	}{
		{"async timer", "export async function browse() { await new Promise(r => setTimeout(r, 5000)); }", provider.Timeout, 80 * time.Millisecond},
		{"busy script", "export function browse() { while (true) {} }", provider.Timeout, 80 * time.Millisecond},
		{"exception", "export function browse() { throw { code: 'rateLimited' }; }", provider.RateLimited, time.Second},
		{"invalid response", "export function browse() { return null; }", provider.PluginFailed, time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := openForTest(t, tc.script, nil)
			session := source.NewSession("entry", memoryStorageForTest(t))
			defer session.Close()
			ctx, cancel := context.WithTimeout(t.Context(), tc.timeout)
			defer cancel()
			start := time.Now()
			_, err := session.Browse(ctx, provider.BrowseRequest{})
			if err == nil || provider.CodeOf(err) != tc.code || time.Since(start) > 2*time.Second {
				t.Fatalf("runtime failed to stop or classify error: %v", err)
			}
		})
	}
}

func allowLocalhostForTest(t *testing.T) {
	t.Helper()
	prev := allowLocalNetworkForTest
	allowLocalNetworkForTest = true
	t.Cleanup(func() { allowLocalNetworkForTest = prev })
}

func TestSessionNetworkPermissionAndCookies(t *testing.T) {
	allowLocalhostForTest(t)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/redirect":
			http.Redirect(w, r, "https://example.invalid/", http.StatusFound)
		case "/seed":
			http.SetCookie(w, &http.Cookie{Name: "entry", Value: "one", Path: "/", Secure: true})
			fmt.Fprint(w, "seeded")
		default:
			cookie, err := r.Cookie("entry")
			if err == nil {
				fmt.Fprint(w, cookie.Value)
			} else {
				fmt.Fprint(w, "guest")
			}
		}
	}))
	defer server.Close()
	source := openForTest(t, `
export async function browse({collection}) {
  let result;
  try {
    result = await fetch(collection, {credentials: "include"});
  } catch {
    throw {code: "network"};
  }
  return {collections: [{id: "network", title: await result.text(), items: []}]};
}
`, func(m *sourceManifest) { m.Permissions.Network = []string{"127.0.0.1"} })
	entryA, entryB := source.NewSession("a", memoryStorageForTest(t)), source.NewSession("b", memoryStorageForTest(t))
	defer entryA.Close()
	defer entryB.Close()
	for _, session := range []*Session{entryA, entryB} {
		session.client.Transport = server.Client().Transport
	}
	for _, tc := range []struct {
		session     *Session
		path, title string
	}{
		{entryA, "/seed", "seeded"}, {entryA, "/cookie", "one"}, {entryB, "/cookie", "guest"},
	} {
		page, err := tc.session.Browse(t.Context(), provider.BrowseRequest{Collection: server.URL + tc.path})
		if err != nil || page.Collections[0].Title != tc.title {
			t.Fatalf("incorrect session HTTP result: %+v, %v", page, err)
		}
	}
	if _, err := entryA.Browse(t.Context(), provider.BrowseRequest{Collection: server.URL + "/redirect"}); provider.CodeOf(err) != provider.Network {
		t.Fatalf("redirect escaped the network grant: %v", err)
	}
}

func TestCatalogPreservesOpaqueIDsAndLargerCollections(t *testing.T) {
	source := openForTest(t, `
export function browse({collection, cursor}) {
  return {nextCursor: cursor, collections: Array.from({length: 65}, (_, group) => ({
    id: collection + group, title: "Collection",
    items: Array.from({length: 5}, (_, item) => ({
      ref: {kind: "movie", id: collection + item},
      title: "Title ".repeat(360),
      subtitle: "Subtitle ".repeat(180)
    }))
  }))};
}
`, nil)
	session := source.NewSession("entry", memoryStorageForTest(t))
	defer session.Close()
	collection, cursor := strings.Repeat("id", 600), strings.Repeat("cursor", 900)
	page, err := session.Browse(t.Context(), provider.BrowseRequest{Collection: collection, Cursor: cursor})
	if err != nil {
		t.Fatal(err, errors.Unwrap(err))
	}
	if page.NextCursor != cursor || len(page.Collections) != 65 ||
		page.Collections[64].Items[4].Ref.ID != collection+"4" {
		t.Fatal("catalog data was truncated")
	}
}

func TestNetworkUsesRuntimeMemoryBudgetOnDeclaredHTTPHost(t *testing.T) {
	allowLocalhostForTest(t)
	const size = 5 << 20
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, strings.Repeat("x", size))
	}))
	defer server.Close()
	source := openForTest(t, `
export async function browse({collection}) {
  const result = await fetch(collection);
  return {collections: [{id: "http", title: String((await result.text()).length), items: []}]};
}
`, func(m *sourceManifest) { m.Permissions.Network = []string{"127.0.0.1"} })
	session := source.NewSession("entry", memoryStorageForTest(t))
	defer session.Close()
	page, err := session.Browse(t.Context(), provider.BrowseRequest{Collection: server.URL})
	if err != nil {
		t.Fatal(err, errors.Unwrap(err))
	}
	if page.Collections[0].Title != fmt.Sprint(size) {
		t.Fatal("HTTP response was truncated")
	}
}

func TestStorageEnforcesQuotaAndSupportsDelete(t *testing.T) {
	var persisted []byte
	store, err := NewStorage(nil, func(data []byte) error {
		persisted = data
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	key, value := strings.Repeat("k", 40), strings.Repeat("d", 4<<10)
	if err := store.set(key, value); err != nil {
		t.Fatal(err)
	}
	if err := store.set("too-big", strings.Repeat("x", StorageCapacity)); err == nil {
		t.Fatal("oversized write bypassed 8 KiB storage quota")
	}
	loaded, err := NewStorage(persisted, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := loaded.get(key); !ok || got != value {
		t.Fatal("value did not survive persistence")
	}
	if err := store.delete(key); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.get(key); ok {
		t.Fatal("deleted key remained in storage")
	}
}

func TestNewStorageReportsMalformedValues(t *testing.T) {
	if _, err := NewStorage([]byte(`{"old":42}`), nil); err == nil {
		t.Fatal("non-string value was accepted")
	}
}

func TestPluginCanRecoverFromStorageWriteFailure(t *testing.T) {
	source := openForTest(t, `
export function browse() {
  const valuesAfterFailure = [];
  const oversized = "x".repeat(8192);
  for (const key of ["existing", "new"]) {
    try {
      host.storage.set(key, oversized);
    } catch (error) {
      if (!(error instanceof Error) || error.message !== "storage quota exceeded") throw error;
      valuesAfterFailure.push(host.storage.get(key) ?? "missing");
    }
  }
  host.storage.set("retry", "saved");
  return {collections: [{id: "storage", title: valuesAfterFailure.join(","), items: []}]};
}
`, nil)
	persisted := []byte(`{"existing":"old"}`)
	store, err := NewStorage(persisted, func(candidate []byte) error {
		if len(candidate) > 8<<10 {
			return errors.New("storage quota exceeded")
		}
		persisted = candidate
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	session := source.NewSession("entry", store)
	defer session.Close()
	page, err := session.Browse(t.Context(), provider.BrowseRequest{})
	if err != nil {
		t.Fatalf("plugin could not handle storage failure: %v (%v)", err, errors.Unwrap(err))
	}
	if len(page.Collections) != 1 || page.Collections[0].Title != "old,missing" {
		t.Fatalf("plugin did not observe the previous state after both failed writes: %+v", page)
	}
	if string(persisted) != `{"existing":"old","retry":"saved"}` {
		t.Fatalf("storage did not preserve the old value and accept the retry: %s", persisted)
	}
}
