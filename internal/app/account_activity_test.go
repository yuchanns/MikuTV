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

type noticeActivityReader struct {
	pages map[provider.AccountQuery]provider.AccountPage
	mark  func(context.Context, provider.NotificationReadRequest) (provider.NotificationReadResult, error)
}

func (r *noticeActivityReader) ReadAccount(_ context.Context, req provider.AccountRequest) (provider.AccountPage, error) {
	return r.pages[req.AccountQuery], nil
}

func (r *noticeActivityReader) MarkNotificationsRead(ctx context.Context, req provider.NotificationReadRequest) (provider.NotificationReadResult, error) {
	return r.mark(ctx, req)
}

func TestAccountActivityNotificationsMarkReadSyncFailureAndOwnerIsolation(t *testing.T) {
	entry := Entry{
		ID: "sample-source",
		Capabilities: provider.Capabilities{
			provider.AccountNotifications.Capability(),
			provider.EditNotifications,
		},
	}
	unreadTotal := 3
	replyRow := provider.AccountRow{
		ID: "reply:1", Title: "CreatorA", Time: 300,
		Notice: &provider.AccountNotice{Actor: "CreatorA", Unread: true, Filter: provider.NoticeReply, Target: provider.NoticeTargetComments},
	}
	sysRow := provider.AccountRow{
		ID: "system:99", Title: "System Notice", Time: 200,
		Notice: &provider.AccountNotice{Unread: true, Filter: provider.NoticeSystem, Cursor: "9000000000000000005"},
	}
	allQ := provider.AccountQuery{Section: provider.AccountNotifications, Filter: provider.NoticeAll}
	replyQ := provider.AccountQuery{Section: provider.AccountNotifications, Filter: provider.NoticeReply}
	summaryQ := provider.AccountQuery{Section: provider.AccountNotifications, Summary: true}

	var gotRequests []provider.NotificationReadRequest
	failNext := false
	var markGate chan struct{}
	reader := &noticeActivityReader{
		pages: map[provider.AccountQuery]provider.AccountPage{
			allQ:     {Rows: []provider.AccountRow{replyRow, sysRow}},
			replyQ:   {Rows: []provider.AccountRow{replyRow}},
			summaryQ: {Unread: &unreadTotal},
		},
		mark: func(ctx context.Context, req provider.NotificationReadRequest) (provider.NotificationReadResult, error) {
			gotRequests = append(gotRequests, req)
			if markGate != nil {
				select {
				case <-markGate:
				case <-ctx.Done():
					return provider.NotificationReadResult{}, ctx.Err()
				}
			}
			if failNext {
				return provider.NotificationReadResult{}, &provider.Error{Code: provider.Network}
			}
			if req.All {
				zero := 0
				return provider.NotificationReadResult{Unread: &zero}, nil
			}
			// Omit Unread on single mark to also verify exact distinct local decrement.
			return provider.NotificationReadResult{}, nil
		},
	}

	callbacks := make(chan func(), 16)
	activity := NewAccountActivity(testHistorySync{}, func(fn func()) { callbacks <- fn }, func() {})
	defer activity.Close()
	activity.Bind(entry, "owner-1", reader)
	if !activity.CanMarkNotificationsRead() {
		t.Fatal("expected CanMarkNotificationsRead to be true")
	}

	activity.Ensure(allQ)
	activity.Ensure(replyQ)
	activity.Ensure(summaryQ)
	for range 3 {
		waitAccountRead(t, callbacks)()
	}

	// 1. Write failure preserves unread status and summary count.
	failNext = true
	var writeErr error
	if !activity.MarkNotificationsRead(provider.NotificationReadRequest{IDs: []string{replyRow.ID}, Filter: provider.NoticeReply}, func(err error) { writeErr = err }) {
		t.Fatal("expected MarkNotificationsRead to start")
	}
	waitAccountRead(t, callbacks)()
	if provider.CodeOf(writeErr) != provider.Network {
		t.Fatalf("expected Network, got %v", writeErr)
	}
	if !activity.Snapshot(allQ).Page.Rows[0].Notice.Unread || *activity.Snapshot(summaryQ).Page.Unread != 3 {
		t.Fatal("failed mark-read mutated local unread state")
	}

	// 2. Single mark-read clears unread in both NoticeAll and NoticeReply and decrements summary once (3 -> 2).
	failNext = false
	writeErr = nil
	if !activity.MarkNotificationsRead(provider.NotificationReadRequest{IDs: []string{replyRow.ID}, Filter: provider.NoticeReply}, func(err error) { writeErr = err }) {
		t.Fatal("expected single MarkNotificationsRead to start")
	}
	waitAccountRead(t, callbacks)()
	if writeErr != nil {
		t.Fatalf("single mark-read failed: %v", writeErr)
	}
	if activity.Snapshot(allQ).Page.Rows[0].Notice.Unread || activity.Snapshot(replyQ).Page.Rows[0].Notice.Unread {
		t.Fatal("single mark-read did not sync across cached notification tabs")
	}
	if !activity.Snapshot(allQ).Page.Rows[1].Notice.Unread {
		t.Fatal("single mark-read cleared an unrelated notification")
	}
	if got := *activity.Snapshot(summaryQ).Page.Unread; got != 2 {
		t.Fatalf("expected summary unread to decrement once from 3 to 2, got %d", got)
	}

	// 3. Mark-all forwards cached latest system cursor, clears all remaining unread rows, and sets summary unread to 0.
	if !activity.MarkNotificationsRead(provider.NotificationReadRequest{All: true, Filter: provider.NoticeAll}, func(err error) { writeErr = err }) {
		t.Fatal("expected mark-all to start")
	}
	waitAccountRead(t, callbacks)()
	if writeErr != nil || gotRequests[len(gotRequests)-1].Cursor != "9000000000000000005" {
		t.Fatalf("mark-all did not forward cached system cursor: err=%v req=%+v", writeErr, gotRequests[len(gotRequests)-1])
	}
	if activity.Snapshot(allQ).Page.Rows[1].Notice.Unread || *activity.Snapshot(summaryQ).Page.Unread != 0 {
		t.Fatalf("mark-all did not clear remaining rows and summary badge")
	}

	// 4. Switching account cancels in-flight mark-read and discards its callback.
	markGate = make(chan struct{})
	doneCalled := false
	activity.MarkNotificationsRead(provider.NotificationReadRequest{All: true, Filter: provider.NoticeAll}, func(error) { doneCalled = true })
	activity.Bind(entry, "owner-2", reader)
	close(markGate)
	waitAccountRead(t, callbacks)()
	if doneCalled {
		t.Fatal("stale mark-read callback ran after owner switch")
	}
}
