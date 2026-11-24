package tvui

import (
	"context"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/provider"
	"github.com/yuchanns/MikuTV/internal/theme"
)

type noticesTestSource struct {
	uiAuthSource
	pages     map[provider.AccountQuery]provider.AccountPage
	details   map[string]provider.MediaItem
	markCalls chan provider.NotificationReadRequest
	unread    int
}

func (s *noticesTestSource) ReadAccount(_ context.Context, req provider.AccountRequest) (provider.AccountPage, error) {
	if req.Summary {
		u := s.unread
		return provider.AccountPage{Unread: &u}, nil
	}
	return s.pages[req.AccountQuery], nil
}

func (s *noticesTestSource) MarkNotificationsRead(_ context.Context, req provider.NotificationReadRequest) (provider.NotificationReadResult, error) {
	s.markCalls <- req
	if req.All {
		s.unread = 0
	} else if s.unread > 0 {
		s.unread--
	}
	u := s.unread
	return provider.NotificationReadResult{Unread: &u}, nil
}

func (s *noticesTestSource) VideoDetail(_ context.Context, ref provider.MediaRef) (provider.MediaItem, error) {
	if item, ok := s.details[ref.ID]; ok {
		return item, nil
	}
	return provider.MediaItem{Ref: ref, Title: "Target Content", VideoDetails: &provider.VideoDetails{}}, nil
}

func (s *noticesTestSource) VideoComments(_ context.Context, _ provider.VideoCommentQuery) (provider.VideoCommentPage, error) {
	return provider.VideoCommentPage{
		Comments: []provider.VideoComment{{ID: "c1", Author: "CreatorA", Body: "Reply body"}},
	}, nil
}

func TestNotificationsReadStatusContentJumpReturnAndCapabilityHiding(t *testing.T) {
	assets, err := LoadAssets()
	if err != nil {
		t.Fatal(err)
	}
	pages, err := NewPages(assets)
	if err != nil {
		t.Fatal(err)
	}
	entries := testEntries()
	entries[0].Capabilities = provider.Capabilities{
		provider.SignIn,
		provider.BrowseCatalog,
		provider.GetCatalogItem,
		provider.ReadVideoComments,
		provider.AccountNotifications.Capability(),
		provider.EditNotifications,
	}
	entries = append(entries, app.Entry{
		ID: "readonly", SourceID: "readonly", Name: "ReadOnly",
		Capabilities: provider.Capabilities{
			provider.SignIn,
			provider.AccountNotifications.Capability(),
			// Intentionally omit EditNotifications to verify capability hiding.
		},
	})

	videoRef := provider.MediaRef{EntryID: entries[0].ID, Kind: provider.Video, ID: "BV1NoticeTest"}
	seriesRef := provider.MediaRef{EntryID: entries[0].ID, Kind: provider.Series, ID: "ss47836"}
	replyRow := provider.AccountRow{
		ID: "reply:10", Title: "CreatorA", Subtitle: "Replied to your comment", Time: 1700000300,
		Ref:  &videoRef,
		Item: &provider.MediaItem{Ref: videoRef, Title: "Target Video"},
		Notice: &provider.AccountNotice{
			Actor: "CreatorA", Action: "replied", Unread: true,
			Filter: provider.NoticeReply, Target: provider.NoticeTargetComments,
		},
	}
	sysRow := provider.AccountRow{
		ID: "system:20", Title: "System Update", Subtitle: "New season available now", Time: 1700000200,
		Ref:  &seriesRef,
		Item: &provider.MediaItem{Ref: seriesRef, Title: "Demon Blade Arc"},
		Notice: &provider.AccountNotice{
			Unread: true, Filter: provider.NoticeSystem, Cursor: "9000000000000000005",
		},
	}
	likeRow := provider.AccountRow{
		ID: "like:30", Title: "FanB", Subtitle: "Liked your comment", Time: 1700000100,
		Notice: &provider.AccountNotice{Actor: "FanB", Action: "liked", Unread: true, Filter: provider.NoticeLike},
	}

	allQ := provider.AccountQuery{Section: provider.AccountNotifications, Filter: provider.NoticeAll}
	source := &noticesTestSource{
		unread:    3,
		markCalls: make(chan provider.NotificationReadRequest, 8),
		pages: map[provider.AccountQuery]provider.AccountPage{
			allQ: {Rows: []provider.AccountRow{replyRow, sysRow, likeRow}},
		},
		details: map[string]provider.MediaItem{
			videoRef.ID: {
				Ref: videoRef, Title: "Target Video",
				VideoDetails: &provider.VideoDetails{
					Discussion: []provider.VideoComment{{ID: "c1", Author: "CreatorA", Body: "Reply body"}},
				},
			},
			seriesRef.ID: {
				Ref: seriesRef, Title: "Demon Blade Arc",
				VideoDetails: &provider.VideoDetails{},
			},
		},
	}

	state, err := app.NewState(entries, entries[0].ID, pages.Routes)
	if err != nil {
		t.Fatal(err)
	}
	queued := make(chan func(), 64)
	post := func(fn func()) { queued <- fn }
	drain := func(done func() bool) {
		t.Helper()
		timeout := time.After(3 * time.Second)
		for !done() {
			select {
			case fn := <-queued:
				fn()
			case <-timeout:
				t.Fatal("timed out waiting for async notification work")
			}
		}
	}
	drainPending := func() {
		for {
			select {
			case fn := <-queued:
				fn()
			default:
				return
			}
		}
	}

	catalog := app.NewCatalog(func(string) provider.Session { return source }, post, func() {})
	defer catalog.Close()
	activity := app.NewAccountActivity(nil, post, func() {})
	defer activity.Close()
	store := uiTestKeychain{data: []byte(`{"selected":"owner","accounts":[{"account":{"id":"owner","name":"Viewer"},"data":"e30="}]}`)}
	auth := app.NewAuth(store, post, func() {}, func() {}, func() {})
	defer auth.Close()

	state.Navigate(app.Route{Page: app.Account})
	view := newTestView(t, Options{
		State: state, Auth: auth, Activity: activity, Catalog: catalog,
		Pages: pages, Assets: assets, Theme: theme.Default(),
	})
	view.SyncEntry()
	drain(func() bool { return auth.Accounts().CurrentID == "owner" })
	view.SyncEntry()
	summaryQ := provider.AccountQuery{Section: provider.AccountNotifications, Summary: true}
	drain(func() bool { return activity.Snapshot(summaryQ).Phase == app.CatalogReady })
	tester := ui.NewTester(view.Render, 1920, 1080)
	tester.Frame()

	// 1. Account overview shows unread badge "消息 · 3".
	if !tester.HasText(assets.Text.Activity.Messages + " · 3") {
		t.Fatal("account overview did not show initial unread count 3")
	}

	// 2. Open Messages page -> ReadAll button is visible and focusable via Right from tab.
	state.Focus = app.AccountInbox
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	drain(func() bool { return activity.Snapshot(allQ).Phase == app.CatalogReady })
	tester.Frame()
	if state.Page != app.AccountMessages || !tester.HasText(assets.Text.Activity.ReadAllNotices) {
		t.Fatalf("messages page missing ReadAllNotices button: page=%s", state.Page)
	}
	tester.Key(0, ui.KeyRight)
	tester.Frame()
	if state.Focus != noticeReadAll {
		t.Fatalf("pressing Right from noticeTab focused %q, want %q", state.Focus, noticeReadAll)
	}
	tester.Key(0, ui.KeyLeft)
	tester.Frame()
	if state.Focus != noticeTab {
		t.Fatalf("pressing Left from noticeReadAll focused %q, want %q", state.Focus, noticeTab)
	}

	// 3. Click unread reply notification -> marks read (3 -> 2), jumps directly to VideoDetail Comments tab, and Back restores row focus.
	tester.Key(0, ui.KeyDown) // account/messages/all
	tester.Key(0, ui.KeyDown) // reply:10
	tester.Frame()
	if state.Focus != noticeRowFocus(replyRow.ID) {
		t.Fatalf("expected focus on reply row, got %q", state.Focus)
	}
	tester.Key(0, ui.KeyEnter)
	req1 := <-source.markCalls
	if req1.All || len(req1.IDs) != 1 || req1.IDs[0] != replyRow.ID {
		t.Fatalf("unexpected single mark-read request: %+v", req1)
	}
	drain(func() bool { return !view.videoDetail.loading && !activity.MarkingNotificationsRead() })
	drainPending()
	tester.Frame()
	if state.Page != app.VideoDetail || view.videoDetail.item.Ref.ID != videoRef.ID || view.videoDetail.tab != videoDetailComments {
		t.Fatalf("reply notification did not jump to VideoDetail comments tab: page=%s ref=%+v tab=%v",
			state.Page, view.videoDetail.item.Ref, view.videoDetail.tab)
	}
	if activity.Snapshot(allQ).Page.Rows[0].Notice.Unread || *activity.Snapshot(summaryQ).Page.Unread != 2 {
		t.Fatalf("reply mark-read did not update list/summary state: unread=%v summary=%d",
			activity.Snapshot(allQ).Page.Rows[0].Notice.Unread, *activity.Snapshot(summaryQ).Page.Unread)
	}
	// Return from VideoDetail -> restores AccountMessages focused on reply:10.
	tester.Key(0, ui.KeyEscape)
	tester.Frame()
	if state.Page != app.AccountMessages || state.Focus != noticeRowFocus(replyRow.ID) {
		t.Fatalf("returning from VideoDetail did not restore notification row focus: page=%s focus=%q", state.Page, state.Focus)
	}

	// 4. Click unread system notification with linked series -> marks read (2 -> 1), opens notice dialog with "查看内容", jumps to series detail, returns to system row.
	tester.Key(0, ui.KeyDown) // system:20
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	req2 := <-source.markCalls
	if len(req2.IDs) != 1 || req2.IDs[0] != sysRow.ID || req2.Cursor != sysRow.Notice.Cursor {
		t.Fatalf("unexpected system mark-read request: %+v", req2)
	}
	drain(func() bool { return !activity.MarkingNotificationsRead() })
	drainPending()
	tester.Frame()
	if state.Page != noticePage || !tester.HasText(assets.Text.Activity.OpenNoticeTarget) {
		t.Fatalf("system notice dialog missing jump target action: page=%s", state.Page)
	}
	tester.Key(0, ui.KeyLeft)
	tester.Frame()
	if state.Focus != noticeOpenTarget {
		t.Fatalf("expected focus on noticeOpenTarget, got %q", state.Focus)
	}
	tester.Key(0, ui.KeyEnter)
	drain(func() bool { return !view.videoDetail.loading })
	drainPending()
	tester.Frame()
	if state.Page != app.VideoDetail || view.videoDetail.item.Ref.ID != seriesRef.ID {
		t.Fatalf("system notice OpenNoticeTarget did not jump to series detail: page=%s ref=%+v", state.Page, view.videoDetail.item.Ref)
	}
	tester.Key(0, ui.KeyEscape)
	tester.Frame()
	if state.Page != app.AccountMessages || state.Focus != noticeRowFocus(sysRow.ID) {
		t.Fatalf("returning from series detail did not restore system row focus: page=%s focus=%q", state.Page, state.Focus)
	}

	// 5. Click "全部已读" -> clears remaining unread like row and zeroes Account unread badge.
	state.Focus = noticeReadAll
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	reqAll := <-source.markCalls
	if !reqAll.All || reqAll.Filter != provider.NoticeAll || reqAll.Cursor != sysRow.Notice.Cursor {
		t.Fatalf("unexpected mark-all request: %+v", reqAll)
	}
	drain(func() bool { return !activity.MarkingNotificationsRead() })
	drainPending()
	tester.Frame()
	if view.noticeFeedback.text != assets.Text.Activity.MarkedAllRead {
		t.Fatalf("expected MarkedAllRead feedback, got %q", view.noticeFeedback.text)
	}
	for _, row := range activity.Snapshot(allQ).Page.Rows {
		if row.Notice != nil && row.Notice.Unread {
			t.Fatalf("row %q still unread after ReadAll", row.ID)
		}
	}
	tester.Key(0, ui.KeyEscape)
	tester.Frame()
	if state.Page != app.Account || tester.HasText(assets.Text.Activity.Messages+" · ") {
		t.Fatalf("account overview still showed unread badge after ReadAll: page=%s", state.Page)
	}

	// 6. Source without EditNotifications capability hides ReadAllNotices cleanly.
	state.SelectEntry(entries[1].ID)
	view.SyncEntry()
	drain(func() bool { return auth.Accounts().CurrentID == "owner" })
	state.Navigate(app.Route{Page: app.AccountMessages})
	view.SyncEntry()
	drain(func() bool { return activity.Snapshot(allQ).Phase == app.CatalogReady })
	tester.Frame()
	if tester.HasText(assets.Text.Activity.ReadAllNotices) {
		t.Fatal("ReadAllNotices must be hidden when source lacks account.editNotifications")
	}
	tester.Key(0, ui.KeyRight)
	tester.Frame()
	if state.Focus == noticeReadAll {
		t.Fatal("focus moved to hidden noticeReadAll button")
	}
}
