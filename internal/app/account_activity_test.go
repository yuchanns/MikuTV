package app

import (
	"context"
	"testing"
	"time"

	"github.com/yuchanns/MikuTV/internal/provider"
)

type accountReadFunc func(context.Context, provider.AccountRequest) (provider.AccountPage, error)

type testHistorySync struct{}

func (testHistorySync) HistorySync(string, string) bool            { return true }
func (testHistorySync) SaveHistorySync(string, string, bool) error { return nil }

func (read accountReadFunc) ReadAccount(ctx context.Context, request provider.AccountRequest) (provider.AccountPage, error) {
	return read(ctx, request)
}

func waitAccountRead[T any](t *testing.T, channel <-chan T) T {
	t.Helper()
	select {
	case value := <-channel:
		return value
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for an account read")
		var zero T
		return zero
	}
}

func TestAccountActivityDiscardsLateResultsFromPreviousOwner(t *testing.T) {
	query := provider.AccountQuery{Section: provider.AccountHistory}
	entry := Entry{ID: "source", Capabilities: provider.Capabilities{query.Section.Capability()}}
	started := make(chan context.Context, 1)
	result := make(chan provider.AccountPage, 1)
	callbacks := make(chan func(), 2)
	activity := NewAccountActivity(testHistorySync{}, func(fn func()) { callbacks <- fn }, func() {})
	defer activity.Close()

	activity.Bind(entry, "first", accountReadFunc(func(ctx context.Context, _ provider.AccountRequest) (provider.AccountPage, error) {
		started <- ctx
		return <-result, nil
	}))
	activity.SetFolder(provider.AccountRow{ID: "private-folder"})
	activity.SetFilter(provider.NoticeReply)
	activity.Ensure(query)
	oldContext := waitAccountRead(t, started)

	activity.Bind(entry, "second", accountReadFunc(func(context.Context, provider.AccountRequest) (provider.AccountPage, error) {
		return provider.AccountPage{Rows: []provider.AccountRow{{ID: "current"}}}, nil
	}))
	if oldContext.Err() != context.Canceled || activity.Folder().ID != "" || activity.Filter() != provider.NoticeAll {
		t.Fatal("switching owners kept private state or work from the previous owner")
	}
	activity.Ensure(query)
	waitAccountRead(t, callbacks)()
	result <- provider.AccountPage{Rows: []provider.AccountRow{{ID: "private-record"}}}
	waitAccountRead(t, callbacks)()

	snapshot := activity.Snapshot(query)
	if snapshot.Phase != CatalogReady || len(snapshot.Page.Rows) != 1 || snapshot.Page.Rows[0].ID != "current" {
		t.Fatalf("previous owner replaced the current result: %+v", snapshot)
	}
}

func TestCanceledAccountReadDoesNotReplaceItsSnapshot(t *testing.T) {
	query := provider.AccountQuery{Section: provider.AccountHistory}
	entry := Entry{ID: "source", Capabilities: provider.Capabilities{query.Section.Capability()}}
	result := make(chan provider.AccountPage, 1)
	callbacks := make(chan func(), 1)
	activity := NewAccountActivity(testHistorySync{}, func(fn func()) { callbacks <- fn }, func() {})
	defer activity.Close()
	activity.Bind(entry, "viewer", accountReadFunc(func(context.Context, provider.AccountRequest) (provider.AccountPage, error) {
		return <-result, nil
	}))
	activity.Ensure(query)
	activity.Cancel(query)
	result <- provider.AccountPage{Rows: []provider.AccountRow{{ID: "late"}}}
	waitAccountRead(t, callbacks)()
	if snapshot := activity.Snapshot(query); snapshot.Phase != CatalogCanceled || len(snapshot.Page.Rows) != 0 {
		t.Fatalf("canceled snapshot was changed: %+v", snapshot)
	}
}
