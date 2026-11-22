package app

import (
	"cmp"
	"context"
	"slices"

	"github.com/yuchanns/MikuTV/internal/provider"
)

type HistorySyncStore interface {
	HistorySync(entry, owner string) bool
	SaveHistorySync(entry, owner string, enabled bool) error
}

type AccountSnapshot struct {
	Phase CatalogPhase
	Page  provider.AccountPage
	Error provider.ErrorCode
}

type accountRead struct {
	cancel context.CancelFunc
	cursor string
	AccountSnapshot
}

type accountWrite struct {
	cancel context.CancelFunc
}

// AccountActivity owns private account activity on the application thread.
type AccountActivity struct {
	store        HistorySyncStore
	post         func(func())
	changed      func()
	entry        Entry
	owner        string
	reader       provider.AccountReader
	reads        map[provider.AccountQuery]*accountRead
	sync         bool
	folder       provider.AccountRow
	filter       provider.NoticeFilter
	pendingWrite *accountWrite
	historyViews *historyViews
}

func NewAccountActivity(store HistorySyncStore, post func(func()), changed func()) *AccountActivity {
	return &AccountActivity{store: store, post: post, changed: changed}
}

func (a *AccountActivity) Bind(entry Entry, owner string, reader provider.AccountReader) bool {
	if a.entry.ID == entry.ID && a.entry.Generation == entry.Generation && a.owner == owner &&
		slices.Equal(a.entry.Capabilities, entry.Capabilities) && a.reader == reader {
		return false
	}
	a.Close()
	a.entry, a.owner, a.reader = entry, owner, reader
	a.reads = make(map[provider.AccountQuery]*accountRead)
	a.filter = provider.NoticeAll
	a.sync = owner != "" && (a.store == nil || a.store.HistorySync(entry.ID, owner))
	return true
}

func (a *AccountActivity) Available(section provider.AccountSection) bool {
	if a == nil {
		return false
	}
	return a.owner != "" && a.reader != nil && a.entry.Capabilities.Has(section.Capability())
}

func (a *AccountActivity) SignedIn() bool { return a.owner != "" }

func (a *AccountActivity) Snapshot(query provider.AccountQuery) AccountSnapshot {
	if read := a.reads[query]; read != nil {
		return read.AccountSnapshot
	}
	return AccountSnapshot{}
}

func (a *AccountActivity) Ensure(query provider.AccountQuery) {
	if a.reads[query] == nil {
		a.start(query, "", provider.AccountPage{})
	}
}

func (a *AccountActivity) Refresh(query provider.AccountQuery) {
	var previous provider.AccountPage
	if read := a.reads[query]; read != nil {
		previous = read.Page
	}
	a.start(query, "", previous)
}

func (a *AccountActivity) Retry(query provider.AccountQuery) {
	if read := a.reads[query]; read != nil && (read.Phase == CatalogFailed || read.Phase == CatalogCanceled) {
		a.start(query, read.cursor, read.Page)
	}
}

func (a *AccountActivity) Cancel(query provider.AccountQuery) {
	if read := a.reads[query]; read != nil && read.Phase == CatalogLoading {
		read.cancel()
		read.Phase = CatalogCanceled
		a.changed()
	}
}

func (a *AccountActivity) More(query provider.AccountQuery) {
	read := a.reads[query]
	if read != nil && read.Phase != CatalogLoading && read.Page.NextCursor != "" {
		a.start(query, read.Page.NextCursor, read.Page)
	}
}

func (a *AccountActivity) start(query provider.AccountQuery, cursor string, previous provider.AccountPage) {
	if !query.Valid() || !a.Available(query.Section) {
		return
	}
	a.remove(query)
	ctx, cancel := context.WithCancel(context.Background())
	read := &accountRead{cancel: cancel, cursor: cursor, AccountSnapshot: AccountSnapshot{Phase: CatalogLoading, Page: previous}}
	a.reads[query] = read
	reader := a.reader
	a.changed()
	go func() {
		page, err := reader.ReadAccount(ctx, provider.AccountRequest{AccountQuery: query, Cursor: cursor})
		a.post(func() {
			if ctx.Err() != nil {
				return
			}
			cancel()
			if err != nil {
				read.AccountSnapshot = AccountSnapshot{Phase: CatalogFailed, Page: previous, Error: provider.CodeOf(err)}
			} else {
				if cursor != "" {
					page.Rows = mergeAccountRows(previous.Rows, page.Rows)
					if page.Total == nil {
						page.Total = previous.Total
					}
					if page.Paused == nil {
						page.Paused = previous.Paused
					}
				} else {
					page.Rows = mergeAccountRows(nil, page.Rows)
				}
				if query.Section == provider.AccountNotifications {
					slices.SortStableFunc(page.Rows, func(a, b provider.AccountRow) int { return cmp.Compare(b.Time, a.Time) })
				}
				if page.NextCursor == cursor {
					page.NextCursor = ""
				}
				read.AccountSnapshot = AccountSnapshot{Phase: CatalogReady, Page: page}
			}
			a.changed()
		})
	}()
}

func mergeAccountRows(previous, next []provider.AccountRow) []provider.AccountRow {
	seen := make(map[string]bool, len(previous)+len(next))
	merged := make([]provider.AccountRow, 0, len(previous)+len(next))
	for _, batch := range [][]provider.AccountRow{previous, next} {
		for _, row := range batch {
			if !seen[row.ID] {
				seen[row.ID] = true
				merged = append(merged, row)
			}
		}
	}
	return merged
}

func (a *AccountActivity) remove(query provider.AccountQuery) {
	if read := a.reads[query]; read != nil {
		read.cancel()
		delete(a.reads, query)
	}
}

func (a *AccountActivity) Close() {
	if views := a.historyViews; views != nil {
		views.cancel()
		a.historyViews = nil
	}
	if write := a.pendingWrite; write != nil {
		write.cancel()
		a.pendingWrite = nil
	}
	for query := range a.reads {
		a.remove(query)
	}
	a.entry, a.owner, a.reader = Entry{}, "", nil
	a.reads, a.folder, a.filter = nil, provider.AccountRow{}, provider.NoticeAll
	a.sync = false
}

func (a *AccountActivity) HistorySync() bool { return a.sync }

func (a *AccountActivity) ToggleHistorySync() error {
	if !a.Available(provider.AccountHistory) {
		return &provider.Error{Code: provider.Unsupported}
	}
	next := !a.sync
	if err := a.store.SaveHistorySync(a.entry.ID, a.owner, next); err != nil {
		return err
	}
	a.sync = next
	a.remove(provider.AccountQuery{Section: provider.AccountHistory})
	a.changed()
	return nil
}

func (a *AccountActivity) SetFolder(row provider.AccountRow)      { a.folder = row }
func (a *AccountActivity) Folder() provider.AccountRow            { return a.folder }
func (a *AccountActivity) SetFilter(filter provider.NoticeFilter) { a.filter = filter }
func (a *AccountActivity) Filter() provider.NoticeFilter          { return a.filter }

func (a *AccountActivity) CanEditFavoriteFolders() bool {
	_, supported := a.reader.(provider.FavoriteFolderEditor)
	return supported && a.Available(provider.AccountFavorites) && a.entry.Capabilities.Has(provider.EditFavoriteFolders)
}

func (a *AccountActivity) EditFavoriteFolder(edit provider.FavoriteFolderEdit, done func(int, error)) bool {
	if !a.CanEditFavoriteFolders() || !edit.Valid() ||
		(edit.Action != provider.FavoriteFolderCreate && edit.Folder != a.folder.ID) ||
		(edit.Action == provider.FavoriteFolderDelete && !a.folder.CanDelete) {
		return false
	}
	editor := a.reader.(provider.FavoriteFolderEditor)
	directory := provider.AccountQuery{Section: provider.AccountFavorites}
	folder := provider.AccountQuery{Section: provider.AccountFavorites, Folder: edit.Folder}
	if edit.Action == provider.FavoriteFolderRemove {
		return a.removeSavedItems(folder, edit.Resources, func(ctx context.Context, resources []string) error {
			request := edit
			request.Resources = resources
			return editor.EditFavoriteFolder(ctx, request)
		}, func(completed int, err error) {
			a.start(directory, "", a.Snapshot(directory).Page)
			done(completed, err)
		})
	}
	return a.write(func(ctx context.Context) (int, error) {
		return 0, editor.EditFavoriteFolder(ctx, edit)
	}, func(completed int, err error) {
		if err == nil {
			switch edit.Action {
			case provider.FavoriteFolderUpdate:
				if a.folder.ID == edit.Folder {
					a.folder.Title, a.folder.Private = edit.Title, &edit.Private
				}
			case provider.FavoriteFolderDelete:
				a.remove(folder)
			}
		}
		a.start(directory, "", a.Snapshot(directory).Page)
		done(completed, err)
	})
}

func (a *AccountActivity) CanRemoveAccountItems(section provider.AccountSection) bool {
	_, supported := a.reader.(provider.AccountItemsEditor)
	capability := section.RemoveCapability()
	return capability != "" && supported && a.Available(section) && a.entry.Capabilities.Has(capability)
}

func (a *AccountActivity) RemoveAccountItems(query provider.AccountQuery, ids []string, done func(int, error)) bool {
	if !a.CanRemoveAccountItems(query.Section) {
		return false
	}
	editor := a.reader.(provider.AccountItemsEditor)
	return a.removeSavedItems(query, ids,
		func(ctx context.Context, ids []string) error {
			return editor.RemoveAccountItems(ctx, query.Section, ids)
		}, done)
}

func (a *AccountActivity) removeSavedItems(query provider.AccountQuery, ids []string,
	remove func(context.Context, []string) error, done func(int, error),
) bool {
	snapshot := a.Snapshot(query)
	if snapshot.Phase != CatalogReady || len(ids) == 0 {
		return false
	}
	available := make(map[string]bool, len(snapshot.Page.Rows))
	for _, row := range snapshot.Page.Rows {
		if row.RemovalID != "" {
			available[row.RemovalID] = true
		}
	}
	for _, id := range ids {
		if !available[id] {
			return false
		}
		delete(available, id)
	}
	return a.write(func(ctx context.Context) (int, error) {
		if err := remove(ctx, ids); err != nil {
			return 0, err
		}
		return len(ids), nil
	}, func(completed int, err error) {
		page := a.Snapshot(query).Page
		removed := make(map[string]bool, completed)
		for _, id := range ids[:completed] {
			removed[id] = true
		}
		page.Rows = slices.DeleteFunc(slices.Clone(page.Rows), func(row provider.AccountRow) bool {
			return removed[row.RemovalID]
		})
		if page.Total != nil {
			total := max(0, *page.Total-completed)
			page.Total = &total
		}
		a.start(query, "", page)
		done(completed, err)
	})
}

func (a *AccountActivity) write(work func(context.Context) (int, error), done func(int, error)) bool {
	if a.pendingWrite != nil {
		return false
	}
	ctx, cancel := context.WithCancel(context.Background())
	request := &accountWrite{cancel: cancel}
	a.pendingWrite = request
	go func() {
		completed, err := work(ctx)
		a.post(func() {
			if a.pendingWrite != request {
				return
			}
			cancel()
			a.pendingWrite = nil
			done(completed, err)
		})
	}()
	return true
}
