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
