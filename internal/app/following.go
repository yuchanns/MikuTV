package app

import (
	"context"
	"slices"

	"github.com/yuchanns/MikuTV/internal/provider"
)

type FollowingSnapshot struct {
	Phase           CatalogPhase
	Error           provider.ErrorCode
	Revision        uint64
	Loaded          bool
	SupportsLive    bool
	Creators        []provider.FollowingCreator
	CreatorsCursor  string
	CreatorsLoading bool
	CreatorsError   provider.ErrorCode
	CreatorsStalled bool
	Updates         []provider.MediaItem
	UpdatesCursor   string
	UpdatesLoading  bool
	UpdatesError    provider.ErrorCode
	UpdatesStalled  bool
	LiveRooms       []provider.MediaItem
	LiveCursor      string
	LiveLoading     bool
	LiveError       provider.ErrorCode
	LiveStalled     bool
}

func (s FollowingSnapshot) CanLoadMoreCreators() bool {
	return s.Phase == CatalogReady && !s.CreatorsLoading && s.CreatorsCursor != "" && !s.CreatorsStalled
}

func (s FollowingSnapshot) CanLoadMoreUpdates() bool {
	return s.Phase == CatalogReady && !s.UpdatesLoading && s.UpdatesCursor != "" && !s.UpdatesStalled
}

func (s FollowingSnapshot) CanLoadMoreLive() bool {
	return s.Phase == CatalogReady && !s.LiveLoading && s.LiveCursor != "" && !s.LiveStalled
}

// FollowingFeed is owned by the UI thread. Provider calls run in workers and re-enter via post.
type FollowingFeed struct {
	post    func(func())
	changed func()

	entryID   string
	entryGen  uint64
	owner     string
	canBrowse bool
	reader    provider.FollowingReader

	sessionGen uint64
	overview   pendingFollowingCall
	creators   pendingFollowingCall
	updates    pendingFollowingCall
	live       pendingFollowingCall
	stale      bool
	snapshot   FollowingSnapshot
}

type pendingFollowingCall struct {
	gen    uint64
	cancel context.CancelFunc
}

func (p *pendingFollowingCall) stop() bool {
	if p.cancel == nil {
		return false
	}
	p.cancel()
	p.cancel = nil
	return true
}

func NewFollowingFeed(post func(func()), changed func()) *FollowingFeed {
	return &FollowingFeed{post: post, changed: changed}
}

func (f *FollowingFeed) Snapshot() FollowingSnapshot {
	if f == nil {
		return FollowingSnapshot{}
	}
	return f.snapshot
}

func (f *FollowingFeed) SignedIn() bool {
	return f != nil && f.owner != ""
}

func (f *FollowingFeed) Available() bool {
	return f != nil && f.canBrowse && f.reader != nil
}

// Bind attaches the active entry and account. Switching entry or account cancels
// all pending calls and isolates following data strictly per (entry, owner).
func (f *FollowingFeed) Bind(entry Entry, owner string, reader provider.FollowingReader) bool {
	if f == nil {
		return false
	}
	canBrowse := entry.ID != "" && entry.Capabilities.Has(provider.BrowseFollowing) && reader != nil
	if f.entryID == entry.ID && f.entryGen == entry.Generation && f.owner == owner &&
		f.canBrowse == canBrowse && f.reader == reader {
		return false
	}
	f.sessionGen++
	f.cancelAll()
	f.entryID, f.entryGen, f.owner = entry.ID, entry.Generation, owner
	f.canBrowse, f.reader = canBrowse, reader
	f.stale = false
	f.snapshot = FollowingSnapshot{Revision: f.snapshot.Revision + 1}
	f.changed()
	return true
}

func (f *FollowingFeed) EnsureLoaded() {
	if f == nil || !f.canBrowse || f.reader == nil || f.owner == "" {
		return
	}
	if f.stale && f.snapshot.Phase != CatalogLoading {
		f.stale = false
		f.loadOverview()
		return
	}
	if f.snapshot.Phase == CatalogIdle {
		f.loadOverview()
	}
}

func (f *FollowingFeed) Reload() {
	if f == nil || !f.canBrowse || f.reader == nil || f.owner == "" {
		return
	}
	f.stale = false
	f.loadOverview()
}

func (f *FollowingFeed) Retry() {
	if f == nil || !f.canBrowse || f.reader == nil || f.owner == "" {
		return
	}
	if f.snapshot.Phase == CatalogFailed || f.snapshot.Phase == CatalogCanceled {
		f.loadOverview()
	}
}

func (f *FollowingFeed) Cancel() {
	if f == nil {
		return
	}
	if f.overview.stop() {
		f.snapshot.Phase = CatalogCanceled
		f.snapshot.Revision++
		f.changed()
	}
}

func (f *FollowingFeed) LoadMoreCreators() {
	if f == nil || !f.canBrowse || f.reader == nil || f.owner == "" || !f.snapshot.CanLoadMoreCreators() {
		return
	}
	cursor := f.snapshot.CreatorsCursor
	f.creators.stop()
	f.creators.gen++
	gen := f.creators.gen
	sessionGen := f.sessionGen
	reader := f.reader
	f.snapshot.CreatorsLoading = true
	f.snapshot.CreatorsError = ""
	ctx, cancel := context.WithCancel(context.Background())
	f.creators.cancel = cancel
	f.changed()
	go func() {
		page, err := reader.ReadFollowing(ctx, provider.FollowingRequest{
			Section: provider.FollowingCreators,
			Cursor:  cursor,
		})
		f.post(func() {
			if ctx.Err() != nil || f.sessionGen != sessionGen || f.creators.gen != gen {
				return
			}
			cancel()
			f.creators.cancel = nil
			f.snapshot.CreatorsLoading = false
			if err != nil {
				f.snapshot.CreatorsError = provider.CodeOf(err)
			} else {
				added := f.appendCreators(page.Creators)
				nextCursor := page.CreatorsCursor
				if nextCursor == cursor || !added {
					nextCursor = ""
				}
				f.snapshot.CreatorsCursor = nextCursor
				f.snapshot.CreatorsStalled = !added
				f.snapshot.Revision++
			}
			f.changed()
		})
	}()
}

func (f *FollowingFeed) LoadMoreUpdates() {
	if f == nil || !f.canBrowse || f.reader == nil || f.owner == "" || !f.snapshot.CanLoadMoreUpdates() {
		return
	}
	cursor := f.snapshot.UpdatesCursor
	f.updates.stop()
	f.updates.gen++
	gen := f.updates.gen
	sessionGen := f.sessionGen
	reader := f.reader
	f.snapshot.UpdatesLoading = true
	f.snapshot.UpdatesError = ""
	ctx, cancel := context.WithCancel(context.Background())
	f.updates.cancel = cancel
	f.changed()
	go func() {
		page, err := reader.ReadFollowing(ctx, provider.FollowingRequest{
			Section: provider.FollowingUpdates,
			Cursor:  cursor,
		})
		f.post(func() {
			if ctx.Err() != nil || f.sessionGen != sessionGen || f.updates.gen != gen {
				return
			}
			cancel()
			f.updates.cancel = nil
			f.snapshot.UpdatesLoading = false
			if err != nil {
				f.snapshot.UpdatesError = provider.CodeOf(err)
			} else {
				added := f.appendUpdates(page.Updates)
				nextCursor := page.UpdatesCursor
				if nextCursor == cursor || !added {
					nextCursor = ""
				}
				f.snapshot.UpdatesCursor = nextCursor
				f.snapshot.UpdatesStalled = !added
				f.snapshot.Revision++
			}
			f.changed()
		})
	}()
}

func (f *FollowingFeed) LoadMoreLive() {
	if f == nil || !f.canBrowse || f.reader == nil || f.owner == "" || !f.snapshot.CanLoadMoreLive() {
		return
	}
	cursor := f.snapshot.LiveCursor
	f.live.stop()
	f.live.gen++
	gen := f.live.gen
	sessionGen := f.sessionGen
	reader := f.reader
	f.snapshot.LiveLoading = true
	f.snapshot.LiveError = ""
	ctx, cancel := context.WithCancel(context.Background())
	f.live.cancel = cancel
	f.changed()
	go func() {
		page, err := reader.ReadFollowing(ctx, provider.FollowingRequest{
			Section: provider.FollowingLive,
			Cursor:  cursor,
		})
		f.post(func() {
			if ctx.Err() != nil || f.sessionGen != sessionGen || f.live.gen != gen {
				return
			}
			cancel()
			f.live.cancel = nil
			f.snapshot.LiveLoading = false
			if err != nil {
				f.snapshot.LiveError = provider.CodeOf(err)
			} else {
				added := f.appendLiveRooms(page.LiveRooms)
				nextCursor := page.LiveCursor
				if nextCursor == cursor || !added {
					nextCursor = ""
				}
				f.snapshot.LiveCursor = nextCursor
				f.snapshot.LiveStalled = !added
				f.snapshot.Revision++
			}
			f.changed()
		})
	}()
}

// SyncFollow reflects a follow/unfollow mutation immediately in the Following list
// without waiting for a full network round-trip.
func (f *FollowingFeed) SyncFollow(creator provider.Creator, following bool) {
	if f == nil || creator.ID == "" || f.owner == "" {
		return
	}
	if !following {
		changed := false
		if len(f.snapshot.Creators) != 0 {
			filtered := make([]provider.FollowingCreator, 0, len(f.snapshot.Creators))
			for _, entry := range f.snapshot.Creators {
				if entry.Creator.ID == creator.ID {
					changed = true
					continue
				}
				filtered = append(filtered, entry)
			}
			f.snapshot.Creators = filtered
		}
		if len(f.snapshot.Updates) != 0 {
			filtered := make([]provider.MediaItem, 0, len(f.snapshot.Updates))
			for _, item := range f.snapshot.Updates {
				if item.Creator != nil && item.Creator.ID == creator.ID {
					changed = true
					continue
				}
				filtered = append(filtered, item)
			}
			f.snapshot.Updates = filtered
		}
		if len(f.snapshot.LiveRooms) != 0 {
			filtered := make([]provider.MediaItem, 0, len(f.snapshot.LiveRooms))
			for _, room := range f.snapshot.LiveRooms {
				if room.Creator != nil && room.Creator.ID == creator.ID {
					changed = true
					continue
				}
				filtered = append(filtered, room)
			}
			f.snapshot.LiveRooms = filtered
		}
		if changed {
			f.snapshot.Revision++
			f.changed()
		}
		return
	}
	creator.Following = true
	found := false
	for i := range f.snapshot.Creators {
		if f.snapshot.Creators[i].Creator.ID == creator.ID {
			if creator.Name != "" {
				f.snapshot.Creators[i].Creator.Name = creator.Name
			}
			if creator.AvatarURL != "" {
				f.snapshot.Creators[i].Creator.AvatarURL = creator.AvatarURL
			}
			f.snapshot.Creators[i].Creator.Following = true
			found = true
			break
		}
	}
	if !found && creator.Name != "" && f.snapshot.Loaded {
		next := make([]provider.FollowingCreator, 0, len(f.snapshot.Creators)+1)
		next = append(next, provider.FollowingCreator{Creator: creator})
		next = append(next, f.snapshot.Creators...)
		f.snapshot.Creators = next
	}
	if f.snapshot.Loaded {
		f.stale = true
	}
	f.snapshot.Revision++
	f.changed()
}

func (f *FollowingFeed) Close() {
	if f == nil {
		return
	}
	f.sessionGen++
	f.cancelAll()
	f.entryID, f.entryGen, f.owner = "", 0, ""
	f.canBrowse, f.reader = false, nil
	f.stale = false
	f.snapshot = FollowingSnapshot{Revision: f.snapshot.Revision + 1}
}

func (f *FollowingFeed) loadOverview() {
	f.cancelAll()
	f.overview.gen++
	gen := f.overview.gen
	sessionGen := f.sessionGen
	reader := f.reader
	f.snapshot.Phase = CatalogLoading
	f.snapshot.Error = ""
	f.snapshot.CreatorsLoading = false
	f.snapshot.UpdatesLoading = false
	f.snapshot.LiveLoading = false
	f.snapshot.CreatorsError = ""
	f.snapshot.UpdatesError = ""
	f.snapshot.LiveError = ""
	f.snapshot.CreatorsStalled = false
	f.snapshot.UpdatesStalled = false
	f.snapshot.LiveStalled = false
	ctx, cancel := context.WithCancel(context.Background())
	f.overview.cancel = cancel
	f.changed()
	go func() {
		page, err := reader.ReadFollowing(ctx, provider.FollowingRequest{Section: provider.FollowingOverview})
		f.post(func() {
			if ctx.Err() != nil || f.sessionGen != sessionGen || f.overview.gen != gen {
				return
			}
			cancel()
			f.overview.cancel = nil
			if err != nil {
				f.snapshot.Phase = CatalogFailed
				f.snapshot.Error = provider.CodeOf(err)
			} else {
				f.snapshot = FollowingSnapshot{
					Phase:          CatalogReady,
					Revision:       f.snapshot.Revision + 1,
					Loaded:         true,
					SupportsLive:   page.SupportsLive,
					Creators:       dedupFollowingCreators(nil, page.Creators),
					CreatorsCursor: page.CreatorsCursor,
					Updates:        dedupMediaItems(nil, page.Updates),
					UpdatesCursor:  page.UpdatesCursor,
					LiveRooms:      dedupMediaItems(nil, page.LiveRooms),
					LiveCursor:     page.LiveCursor,
				}
			}
			f.changed()
		})
	}()
}

func (f *FollowingFeed) appendCreators(incoming []provider.FollowingCreator) bool {
	before := len(f.snapshot.Creators)
	f.snapshot.Creators = dedupFollowingCreators(f.snapshot.Creators, incoming)
	return len(f.snapshot.Creators) > before
}

func (f *FollowingFeed) appendUpdates(incoming []provider.MediaItem) bool {
	before := len(f.snapshot.Updates)
	f.snapshot.Updates = dedupMediaItems(f.snapshot.Updates, incoming)
	return len(f.snapshot.Updates) > before
}

func (f *FollowingFeed) appendLiveRooms(incoming []provider.MediaItem) bool {
	before := len(f.snapshot.LiveRooms)
	f.snapshot.LiveRooms = dedupMediaItems(f.snapshot.LiveRooms, incoming)
	return len(f.snapshot.LiveRooms) > before
}

func (f *FollowingFeed) cancelAll() {
	f.overview.stop()
	f.creators.stop()
	f.updates.stop()
	f.live.stop()
}

func dedupFollowingCreators(existing, incoming []provider.FollowingCreator) []provider.FollowingCreator {
	out := slices.Clone(existing)
	seen := make(map[string]struct{}, len(out)+len(incoming))
	for _, entry := range out {
		seen[entry.Creator.ID] = struct{}{}
	}
	for _, entry := range incoming {
		if entry.Creator.ID == "" {
			continue
		}
		if _, dup := seen[entry.Creator.ID]; dup {
			continue
		}
		seen[entry.Creator.ID] = struct{}{}
		out = append(out, entry)
	}
	return out
}

func dedupMediaItems(existing, incoming []provider.MediaItem) []provider.MediaItem {
	out := slices.Clone(existing)
	seen := make(map[provider.MediaRef]struct{}, len(out)+len(incoming))
	for _, item := range out {
		seen[item.Ref] = struct{}{}
	}
	for _, item := range incoming {
		if _, dup := seen[item.Ref]; dup {
			continue
		}
		seen[item.Ref] = struct{}{}
		out = append(out, item)
	}
	return out
}
