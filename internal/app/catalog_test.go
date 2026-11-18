package app

import (
	"context"
	"testing"

	"github.com/yuchanns/MikuTV/internal/provider"
)

type delayedSession struct {
	started chan context.Context
	result  chan provider.CollectionPage
	closed  bool
}

func (s *delayedSession) Browse(ctx context.Context, _ provider.BrowseRequest) (provider.CollectionPage, error) {
	s.started <- ctx
	return <-s.result, nil
}

func (*delayedSession) Item(context.Context, provider.MediaRef) (provider.MediaItem, error) {
	return provider.MediaItem{}, nil
}

func (s *delayedSession) Close() { s.closed = true }

func TestCatalogRejectsLateResultsAfterEntrySwitchAndCancel(t *testing.T) {
	makeSession := func() *delayedSession {
		return &delayedSession{started: make(chan context.Context, 1), result: make(chan provider.CollectionPage, 1)}
	}
	a, b := makeSession(), makeSession()
	sessions := map[string]*delayedSession{"a": a, "b": b}
	callbacks := make(chan func(), 2)
	catalog := NewCatalog(func(id string) provider.Session { return sessions[id] }, func(fn func()) { callbacks <- fn }, func() {})
	defer catalog.Close()
	enabled := provider.Capabilities{provider.BrowseCatalog}
	catalog.Select(Entry{ID: "a", Capabilities: enabled})
	first := <-a.started
	catalog.Select(Entry{ID: "b", Capabilities: enabled})
	<-b.started
	if first.Err() == nil || !a.closed {
		t.Fatal("inactive entry kept its work or session")
	}
	b.result <- provider.CollectionPage{Collections: []provider.Collection{{ID: "b"}}}
	(<-callbacks)()
	a.result <- provider.CollectionPage{Collections: []provider.Collection{{ID: "stale"}}}
	(<-callbacks)()
	if snapshot := catalog.Snapshot(); snapshot.Phase != CatalogReady || snapshot.Page.Collections[0].ID != "b" {
		t.Fatalf("late entry result replaced the current result: %+v", snapshot)
	}
	catalog.Reload()
	request := <-b.started
	catalog.Cancel()
	b.result <- provider.CollectionPage{Collections: []provider.Collection{{ID: "canceled"}}}
	(<-callbacks)()
	if request.Err() == nil || catalog.Snapshot().Phase != CatalogCanceled {
		t.Fatal("late canceled result changed the current state")
	}
}

type catalogCall struct {
	context.Context
	request provider.BrowseRequest
	reply   chan catalogReply
}

type catalogReply struct {
	page provider.CollectionPage
	err  error
}

type catalogScript chan catalogCall

func (s catalogScript) Browse(ctx context.Context, request provider.BrowseRequest) (provider.CollectionPage, error) {
	reply := make(chan catalogReply, 1)
	s <- catalogCall{Context: ctx, request: request, reply: reply}
	result := <-reply
	return result.page, result.err
}

func (catalogScript) Item(context.Context, provider.MediaRef) (provider.MediaItem, error) {
	return provider.MediaItem{}, nil
}

func (catalogScript) Close() {}

func TestCatalogRefreshAndPaginationKeepPublishedPagesConsistent(t *testing.T) {
	source := make(catalogScript, 1)
	callbacks := make(chan func(), 1)
	catalog := NewCatalog(func(string) provider.Session { return source }, func(fn func()) { callbacks <- fn }, func() {})
	defer catalog.Close()
	catalog.Select(Entry{ID: "source", Capabilities: provider.Capabilities{provider.BrowseCatalog}})
	page := func(cursor string, ids ...string) provider.CollectionPage {
		group := provider.Collection{ID: "source-feed", Title: "Recommendations"}
		for _, id := range ids {
			group.Items = append(group.Items, provider.MediaItem{Ref: provider.MediaRef{Kind: provider.Video, ID: id}, Title: id})
		}
		return provider.CollectionPage{Collections: []provider.Collection{group}, NextCursor: cursor}
	}
	complete := func(call catalogCall, result catalogReply) {
		call.reply <- result
		(<-callbacks)()
	}

	complete(<-source, catalogReply{page: page("next", "first", "first")})
	first := catalog.Snapshot()
	catalog.LoadMore()
	failed := <-source
	if failed.request != (provider.BrowseRequest{Cursor: "next"}) {
		t.Fatalf("invalid pagination request: %+v", failed.request)
	}
	complete(failed, catalogReply{err: &provider.Error{Code: provider.Network}})
	if snapshot := catalog.Snapshot(); snapshot.Phase != CatalogFailed || !snapshot.Appending ||
		snapshot.Page.NextCursor != "next" || len(snapshot.Page.Collections[0].Items) != 1 {
		t.Fatalf("pagination failure replaced the first page: %+v", snapshot)
	}
	catalog.Retry()
	retry := <-source
	if retry.request != failed.request {
		t.Fatalf("pagination retry changed cursor: %+v", retry.request)
	}
	complete(retry, catalogReply{page: page("later", "first", "second")})
	appended := catalog.Snapshot()
	if items := appended.Page.Collections[0].Items; len(items) != 2 || items[1].Ref.ID != "second" || appended.AppendStalled {
		t.Fatalf("pagination lost order or duplicated cards: %+v", appended)
	}
	if items := first.Page.Collections[0].Items; len(items) != 1 || items[0].Ref.ID != "first" {
		t.Fatalf("pagination changed the published first page: %+v", first)
	}

	catalog.LoadMore()
	stale := <-source
	catalog.Refresh()
	refresh := <-source
	if stale.Err() == nil || refresh.request != (provider.BrowseRequest{Refresh: true, Cursor: "later"}) ||
		len(catalog.Snapshot().Page.Collections[0].Items) != 2 {
		t.Fatal("refresh neither isolated the older call nor retained the feed")
	}
	complete(refresh, catalogReply{err: &provider.Error{Code: provider.RateLimited}})
	if snapshot := catalog.Snapshot(); snapshot.Error != provider.RateLimited || len(snapshot.Page.Collections[0].Items) != 2 {
		t.Fatalf("refresh failure replaced the feed: %+v", snapshot)
	}
	catalog.Retry()
	refreshRetry := <-source
	if refreshRetry.request != refresh.request {
		t.Fatal("refresh retry changed the operation")
	}
	complete(refreshRetry, catalogReply{page: page("again", "new")})
	complete(stale, catalogReply{page: page("bad", "stale")})
	if snapshot := catalog.Snapshot(); snapshot.Page.Collections[0].Items[0].Ref.ID != "new" ||
		snapshot.Page.NextCursor != "again" || snapshot.Phase != CatalogReady {
		t.Fatalf("a superseded append changed the refreshed list: %+v", snapshot)
	}

	catalog.LoadMore()
	complete(<-source, catalogReply{page: page("again", "new")})
	if snapshot := catalog.Snapshot(); !snapshot.AppendStalled || snapshot.Page.NextCursor != "" {
		t.Fatalf("a repeated cursor kept pagination active: %+v", snapshot)
	}
}
