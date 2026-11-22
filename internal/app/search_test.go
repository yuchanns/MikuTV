package app

import (
	"context"
	"slices"
	"testing"

	"github.com/yuchanns/MikuTV/internal/provider"
)

type searchCall struct {
	ctx     context.Context
	request provider.SearchRequest
	reply   chan searchReply
}

type searchReply struct {
	page provider.SearchPage
	err  error
}

type suggestCall struct {
	ctx   context.Context
	query string
	reply chan suggestReply
}

type suggestReply struct {
	result provider.SearchSuggestResult
	err    error
}

type scriptedSearcher struct {
	searches chan searchCall
	suggests chan suggestCall
}

func (s *scriptedSearcher) Search(ctx context.Context, request provider.SearchRequest) (provider.SearchPage, error) {
	reply := make(chan searchReply, 1)
	s.searches <- searchCall{ctx: ctx, request: request, reply: reply}
	res := <-reply
	return res.page, res.err
}

func (s *scriptedSearcher) SearchSuggest(ctx context.Context, query string) (provider.SearchSuggestResult, error) {
	reply := make(chan suggestReply, 1)
	s.suggests <- suggestCall{ctx: ctx, query: query, reply: reply}
	res := <-reply
	return res.result, res.err
}

type memorySearchHistory struct {
	buckets map[string][]string
}

func (m *memorySearchHistory) SearchHistory(entryID, owner string) []string {
	return slices.Clone(m.buckets[entryID+"\x00"+owner])
}

func (m *memorySearchHistory) SaveSearchHistory(entryID, owner string, queries []string) error {
	if m.buckets == nil {
		m.buckets = make(map[string][]string)
	}
	m.buckets[entryID+"\x00"+owner] = slices.Clone(queries)
	return nil
}

func TestSearchCancelsStaleQueriesFiltersAndPaginationWithoutCrossTalk(t *testing.T) {
	source := &scriptedSearcher{
		searches: make(chan searchCall, 8),
		suggests: make(chan suggestCall, 8),
	}
	callbacks := make(chan func(), 16)
	history := &memorySearchHistory{}
	search := NewCatalogSearch(history, func(fn func()) { callbacks <- fn }, func() {})
	defer search.Close()

	entry := Entry{ID: "sample-source", Capabilities: provider.Capabilities{provider.SearchCatalog, provider.SearchSuggest}}
	search.Bind(entry, "acct-1", source, source)
	search.EnsureLoaded()
	initial := <-source.searches
	if initial.request.Query != "" || initial.request.Kind != provider.SearchAll {
		t.Fatalf("unexpected initial discovery request: %+v", initial.request)
	}
	initial.reply <- searchReply{page: provider.SearchPage{
		Items:       []provider.MediaItem{{Ref: provider.MediaRef{EntryID: "sample-source", Kind: provider.Video, ID: "BVInit"}, Title: "Popular"}},
		HotKeywords: []string{"miku", "space"},
	}}
	(<-callbacks)()
	if snap := search.Snapshot(); snap.Phase != CatalogReady || !slices.Equal(snap.HotKeywords, []string{"miku", "space"}) {
		t.Fatalf("discovery snapshot = %+v", snap)
	}

	// Rapidly submit first query then immediately supersede it before the first completes.
	search.Submit("old")
	firstCall := <-source.searches
	firstSuggest := <-source.suggests
	search.Submit("miku")
	secondCall := <-source.searches
	secondSuggest := <-source.suggests
	if firstCall.ctx.Err() == nil || firstSuggest.ctx.Err() == nil {
		t.Fatal("superseding search did not cancel previous search and suggest")
	}

	total := int64(2)
	secondCall.reply <- searchReply{page: provider.SearchPage{
		Items:      []provider.MediaItem{{Ref: provider.MediaRef{EntryID: "sample-source", Kind: provider.Video, ID: "BV1"}, Title: "Miku 1"}},
		Total:      &total,
		NextCursor: "2",
	}}
	(<-callbacks)()
	secondSuggest.reply <- suggestReply{result: provider.SearchSuggestResult{Suggestions: []string{"miku live"}}}
	(<-callbacks)()

	// Late replies from the superseded query must be ignored.
	firstCall.reply <- searchReply{page: provider.SearchPage{
		Items: []provider.MediaItem{{Ref: provider.MediaRef{EntryID: "sample-source", Kind: provider.Video, ID: "BVStale"}, Title: "Stale"}},
	}}
	(<-callbacks)()
	firstSuggest.reply <- suggestReply{result: provider.SearchSuggestResult{Suggestions: []string{"old suggest"}}}
	(<-callbacks)()

	snap := search.Snapshot()
	if snap.Request.Query != "miku" || len(snap.Page.Items) != 1 || snap.Page.Items[0].Ref.ID != "BV1" ||
		!slices.Equal(snap.Suggestions, []string{"miku live"}) || !slices.Equal(snap.HotKeywords, []string{"miku", "space"}) {
		t.Fatalf("stale result contaminated current search: %+v", snap)
	}

	// Start pagination, fail, retry, deduplicate, and stall on repeated cursor.
	search.LoadMore()
	page2Fail := <-source.searches
	if page2Fail.request.Cursor != "2" || page2Fail.request.Query != "miku" {
		t.Fatalf("bad page 2 request: %+v", page2Fail.request)
	}
	page2Fail.reply <- searchReply{err: &provider.Error{Code: provider.Network}}
	(<-callbacks)()
	if snap := search.Snapshot(); snap.Phase != CatalogFailed || !snap.Appending || len(snap.Page.Items) != 1 {
		t.Fatalf("append failure lost page 1: %+v", snap)
	}
	search.Retry()
	page2Retry := <-source.searches
	if page2Retry.request != page2Fail.request {
		t.Fatalf("retry altered request: %+v", page2Retry.request)
	}
	page2Retry.reply <- searchReply{page: provider.SearchPage{
		Items: []provider.MediaItem{
			{Ref: provider.MediaRef{EntryID: "sample-source", Kind: provider.Video, ID: "BV1"}, Title: "Miku 1"},
			{Ref: provider.MediaRef{EntryID: "sample-source", Kind: provider.Video, ID: "BV2"}, Title: "Miku 2"},
		},
		NextCursor: "3",
	}}
	(<-callbacks)()
	if snap := search.Snapshot(); len(snap.Page.Items) != 2 || snap.Page.Items[1].Ref.ID != "BV2" || snap.AppendStalled {
		t.Fatalf("pagination dedup failed: %+v", snap)
	}

	// Start page 3, then change filters before page 3 returns; page 3 must not pollute filtered page 1.
	search.LoadMore()
	page3Stale := <-source.searches
	search.ApplyFilters(provider.SearchSortViews, provider.SearchDurationShort)
	filtered := <-source.searches
	if page3Stale.ctx.Err() == nil {
		t.Fatal("filter change did not cancel pending pagination")
	}
	if filtered.request.Cursor != "" || filtered.request.Sort != provider.SearchSortViews || filtered.request.Duration != provider.SearchDurationShort {
		t.Fatalf("filter change did not reset pagination: %+v", filtered.request)
	}
	filtered.reply <- searchReply{page: provider.SearchPage{
		Items: []provider.MediaItem{{Ref: provider.MediaRef{EntryID: "sample-source", Kind: provider.Video, ID: "BVFiltered"}, Title: "Short Top"}},
	}}
	(<-callbacks)()
	page3Stale.reply <- searchReply{page: provider.SearchPage{
		Items: []provider.MediaItem{{Ref: provider.MediaRef{EntryID: "sample-source", Kind: provider.Video, ID: "BVPage3"}, Title: "Late Page 3"}},
	}}
	(<-callbacks)()
	if snap := search.Snapshot(); len(snap.Page.Items) != 1 || snap.Page.Items[0].Ref.ID != "BVFiltered" {
		t.Fatalf("stale page leaked across filter switch: %+v", snap)
	}

	// Switch kind to creator and verify history isolation on account/entry switch.
	search.SelectKind(provider.SearchCreator)
	creatorCall := <-source.searches
	if creatorCall.request.Kind != provider.SearchCreator || creatorCall.request.Cursor != "" {
		t.Fatalf("unexpected creator search request: %+v", creatorCall.request)
	}
	creatorCall.reply <- searchReply{page: provider.SearchPage{
		Creators: []provider.SearchCreatorItem{{Creator: provider.Creator{ID: "39", Name: "Miku"}}},
	}}
	(<-callbacks)()
	if snap := search.Snapshot(); len(snap.Page.Creators) != 1 || len(snap.Page.Items) != 0 {
		t.Fatalf("kind switch did not isolate results: %+v", snap)
	}

	// History saved under acct-1 is isolated from guest and other entries.
	if got := search.Snapshot().History; !slices.Equal(got, []string{"miku", "old"}) {
		t.Fatalf("acct-1 history = %v", got)
	}
	search.Bind(entry, "", source, source)
	if snap := search.Snapshot(); len(snap.History) != 0 || snap.Request.Query != "" || len(snap.Page.Creators) != 0 {
		t.Fatalf("switching to guest leaked signed-in search state: %+v", snap)
	}
	search.Bind(entry, "acct-1", source, source)
	if got := search.Snapshot().History; !slices.Equal(got, []string{"miku", "old"}) {
		t.Fatalf("restoring acct-1 lost history: %v", got)
	}
}
