package platform

import (
	"fmt"
	"slices"
	"testing"
)

func TestSearchHistoryStoreIsolatesEntriesAndAccounts(t *testing.T) {
	var store searchHistoryStore
	store.put("sample-source", "", []string{"guest-a", "guest-b"})
	store.put("sample-source", "acct-1", []string{"miku", "travel", "miku", "  "})
	store.put("sample-source", "acct-2", []string{"science"})
	store.put("other", "acct-1", []string{"other-term"})

	encoded, err := store.encode()
	if err != nil {
		t.Fatal(err)
	}
	reloaded := parseSearchHistoryStore(encoded)
	if got := reloaded.get("sample-source", ""); !slices.Equal(got, []string{"guest-a", "guest-b"}) {
		t.Fatalf("guest history = %v", got)
	}
	if got := reloaded.get("sample-source", "acct-1"); !slices.Equal(got, []string{"miku", "travel"}) {
		t.Fatalf("acct-1 history = %v", got)
	}
	if got := reloaded.get("sample-source", "acct-2"); !slices.Equal(got, []string{"science"}) {
		t.Fatalf("acct-2 history = %v", got)
	}
	if got := reloaded.get("other", "acct-1"); !slices.Equal(got, []string{"other-term"}) {
		t.Fatalf("other entry history = %v", got)
	}

	if !reloaded.clearAccount("sample-source", "acct-1") {
		t.Fatal("clearAccount did not remove acct-1")
	}
	if got := reloaded.get("sample-source", "acct-1"); len(got) != 0 {
		t.Fatalf("acct-1 history remained: %v", got)
	}
	if got := reloaded.get("sample-source", ""); !slices.Equal(got, []string{"guest-a", "guest-b"}) {
		t.Fatalf("clearing acct-1 modified guest history: %v", got)
	}
	if got := reloaded.get("other", "acct-1"); !slices.Equal(got, []string{"other-term"}) {
		t.Fatalf("clearing sample-source/acct-1 modified other entry: %v", got)
	}

	if !reloaded.clearEntry("sample-source") {
		t.Fatal("clearEntry did not remove sample-source buckets")
	}
	if len(reloaded.get("sample-source", "")) != 0 || len(reloaded.get("sample-source", "acct-2")) != 0 {
		t.Fatal("clearEntry left sample-source history behind")
	}
	if got := reloaded.get("other", "acct-1"); !slices.Equal(got, []string{"other-term"}) {
		t.Fatalf("clearEntry removed another entry: %v", got)
	}
}

func TestSearchHistoryStoreBoundsQueriesAndBuckets(t *testing.T) {
	var store searchHistoryStore
	var many []string
	for i := range maxSearchHistoryQueries + 5 {
		many = append(many, fmt.Sprintf("q-%d", i))
	}
	store.put("entry", "owner", many)
	if got := store.get("entry", "owner"); len(got) != maxSearchHistoryQueries {
		t.Fatalf("query count = %d, want %d", len(got), maxSearchHistoryQueries)
	}

	for i := range maxSearchHistoryBuckets + 12 {
		store.put(fmt.Sprintf("entry-%d", i), "owner", []string{"term"})
	}
	if len(store.Buckets) != maxSearchHistoryBuckets {
		t.Fatalf("bucket count = %d, want %d", len(store.Buckets), maxSearchHistoryBuckets)
	}
	if got := store.get("entry-0", "owner"); len(got) != 0 {
		t.Fatalf("oldest bucket was not evicted: %v", got)
	}
	latest := fmt.Sprintf("entry-%d", maxSearchHistoryBuckets+11)
	if got := store.get(latest, "owner"); !slices.Equal(got, []string{"term"}) {
		t.Fatalf("newest bucket missing: %v", got)
	}
}
