package app

import (
	"context"
	"slices"

	"github.com/yuchanns/MikuTV/internal/provider"
)

type LiveSnapshot struct {
	Phase            CatalogPhase
	Error            provider.ErrorCode
	Revision         uint64
	Loaded           bool
	Area             string
	Areas            []provider.LiveArea
	Hero             *provider.MediaItem
	Rooms            []provider.MediaItem
	RoomsCursor      string
	RoomsLoading     bool
	RoomsStalled     bool
	RoomsError       provider.ErrorCode
	FollowingRooms   []provider.MediaItem
	FollowingCursor  string
	FollowingLoading bool
	FollowingStalled bool
	FollowingError   provider.ErrorCode
}

func (s LiveSnapshot) CanLoadMoreRooms() bool {
	return s.Phase == CatalogReady && !s.RoomsLoading && s.RoomsCursor != "" && !s.RoomsStalled
}

func (s LiveSnapshot) CanLoadMoreFollowing() bool {
	return s.Phase == CatalogReady && !s.FollowingLoading && s.FollowingCursor != "" && !s.FollowingStalled
}

// LiveCatalog is owned by the UI thread. Provider calls run in workers and re-enter via post.
type LiveCatalog struct {
	post    func(func())
	changed func()

	entryID   string
	entryGen  uint64
	owner     string
	canBrowse bool
	reader    provider.LiveReader

	sessionGen    uint64
	hubCall       pendingCall
	roomsCall     pendingCall
	followingCall pendingCall
	snapshot      LiveSnapshot
}

func NewLiveCatalog(post func(func()), changed func()) *LiveCatalog {
	return &LiveCatalog{post: post, changed: changed}
}

func (l *LiveCatalog) Available() bool {
	return l != nil && l.canBrowse && l.reader != nil
}

func (l *LiveCatalog) Snapshot() LiveSnapshot {
	if l == nil {
		return LiveSnapshot{}
	}
	return l.snapshot
}

func (l *LiveCatalog) Bind(entry Entry, owner string, reader provider.LiveReader) bool {
	if l == nil {
		return false
	}
	canBrowse := entry.ID != "" && entry.Capabilities.Has(provider.BrowseLive) && reader != nil
	if l.entryID == entry.ID && l.entryGen == entry.Generation && l.owner == owner &&
		l.canBrowse == canBrowse && l.reader == reader {
		return false
	}
	l.sessionGen++
	l.cancelAll()
	l.entryID, l.entryGen, l.owner = entry.ID, entry.Generation, owner
	l.canBrowse, l.reader = canBrowse, reader
	l.snapshot = LiveSnapshot{Revision: l.snapshot.Revision + 1}
	l.changed()
	return true
}

func (l *LiveCatalog) EnsureLoaded() {
	if l == nil || !l.canBrowse || l.reader == nil {
		return
	}
	if l.snapshot.Phase == CatalogIdle {
		l.loadHub(l.snapshot.Area)
	}
}

func (l *LiveCatalog) SelectArea(area string) {
	if l == nil || !l.canBrowse || l.reader == nil {
		return
	}
	if l.snapshot.Area == area && l.snapshot.Phase != CatalogIdle && l.snapshot.Phase != CatalogFailed && l.snapshot.Phase != CatalogCanceled {
		return
	}
	l.snapshot.Area = area
	l.snapshot.Hero = nil
	l.snapshot.Rooms = nil
	l.snapshot.RoomsCursor = ""
	l.snapshot.RoomsStalled = false
	l.snapshot.RoomsError = ""
	l.snapshot.Revision++
	l.loadHub(area)
}

func (l *LiveCatalog) LoadMoreRooms() {
	if l == nil || !l.canBrowse || l.reader == nil || !l.snapshot.CanLoadMoreRooms() {
		return
	}
	l.roomsCall.stop()
	l.roomsCall.gen++
	callGen := l.roomsCall.gen
	sessionGen := l.sessionGen
	reader := l.reader
	area := l.snapshot.Area
	cursor := l.snapshot.RoomsCursor
	req := provider.LiveRequest{Section: provider.LiveSectionRooms, Area: area, Cursor: cursor}
	l.snapshot.RoomsLoading = true
	l.snapshot.RoomsError = ""
	l.snapshot.Revision++
	ctx, cancel := context.WithCancel(context.Background())
	l.roomsCall.cancel = cancel
	l.changed()
	go func() {
		page, err := reader.ReadLive(ctx, req)
		l.post(func() {
			if ctx.Err() != nil || l.sessionGen != sessionGen || l.roomsCall.gen != callGen {
				return
			}
			cancel()
			l.roomsCall.cancel = nil
			l.snapshot.RoomsLoading = false
			if err != nil {
				l.snapshot.RoomsError = provider.CodeOf(err)
			} else {
				nextCursor := page.NextCursor
				if nextCursor == cursor {
					nextCursor = ""
				}
				merged, added := mergeLiveRooms(l.snapshot.Rooms, page.Rooms)
				l.snapshot.Rooms = merged
				l.snapshot.RoomsCursor = nextCursor
				l.snapshot.RoomsStalled = !added && nextCursor != ""
			}
			l.snapshot.Revision++
			l.changed()
		})
	}()
}

func (l *LiveCatalog) LoadMoreFollowing() {
	if l == nil || !l.canBrowse || l.reader == nil || !l.snapshot.CanLoadMoreFollowing() {
		return
	}
	l.followingCall.stop()
	l.followingCall.gen++
	callGen := l.followingCall.gen
	sessionGen := l.sessionGen
	reader := l.reader
	cursor := l.snapshot.FollowingCursor
	req := provider.LiveRequest{Section: provider.LiveSectionFollowing, Cursor: cursor}
	l.snapshot.FollowingLoading = true
	l.snapshot.FollowingError = ""
	l.snapshot.Revision++
	ctx, cancel := context.WithCancel(context.Background())
	l.followingCall.cancel = cancel
	l.changed()
	go func() {
		page, err := reader.ReadLive(ctx, req)
		l.post(func() {
			if ctx.Err() != nil || l.sessionGen != sessionGen || l.followingCall.gen != callGen {
				return
			}
			cancel()
			l.followingCall.cancel = nil
			l.snapshot.FollowingLoading = false
			if err != nil {
				l.snapshot.FollowingError = provider.CodeOf(err)
			} else {
				nextCursor := page.FollowingCursor
				if nextCursor == "" {
					nextCursor = page.NextCursor
				}
				if nextCursor == cursor {
					nextCursor = ""
				}
				merged, added := mergeLiveRooms(l.snapshot.FollowingRooms, page.FollowingRooms)
				l.snapshot.FollowingRooms = merged
				l.snapshot.FollowingCursor = nextCursor
				l.snapshot.FollowingStalled = !added && nextCursor != ""
			}
			l.snapshot.Revision++
			l.changed()
		})
	}()
}

func (l *LiveCatalog) Retry() {
	if l == nil || !l.canBrowse || l.reader == nil {
		return
	}
	if l.snapshot.Phase == CatalogFailed || l.snapshot.Phase == CatalogCanceled {
		l.loadHub(l.snapshot.Area)
	}
}

func (l *LiveCatalog) Cancel() {
	if l == nil {
		return
	}
	if l.hubCall.stop() {
		l.snapshot.Phase = CatalogCanceled
		l.snapshot.Revision++
		l.changed()
	}
}

func (l *LiveCatalog) SyncFollow(creatorID string, following bool) {
	if l == nil || creatorID == "" {
		return
	}
	changed := false
	update := func(item *provider.MediaItem) {
		if item != nil && item.Creator != nil && item.Creator.ID == creatorID && item.Creator.Following != following {
			copyCreator := *item.Creator
			copyCreator.Following = following
			item.Creator = &copyCreator
			changed = true
		}
	}
	if l.snapshot.Hero != nil {
		hero := *l.snapshot.Hero
		update(&hero)
		l.snapshot.Hero = &hero
	}
	for i := range l.snapshot.Rooms {
		update(&l.snapshot.Rooms[i])
	}
	if !following && len(l.snapshot.FollowingRooms) != 0 {
		filtered := l.snapshot.FollowingRooms[:0]
		for _, room := range l.snapshot.FollowingRooms {
			if room.Creator != nil && room.Creator.ID == creatorID {
				changed = true
				continue
			}
			filtered = append(filtered, room)
		}
		l.snapshot.FollowingRooms = filtered
	} else {
		for i := range l.snapshot.FollowingRooms {
			update(&l.snapshot.FollowingRooms[i])
		}
	}
	if changed {
		l.snapshot.Revision++
		l.changed()
	}
}

func (l *LiveCatalog) Close() {
	if l == nil {
		return
	}
	l.sessionGen++
	l.cancelAll()
	l.entryID, l.entryGen, l.owner = "", 0, ""
	l.canBrowse, l.reader = false, nil
	l.snapshot = LiveSnapshot{}
}

func (l *LiveCatalog) cancelAll() {
	l.hubCall.stop()
	l.roomsCall.stop()
	l.followingCall.stop()
}

func (l *LiveCatalog) loadHub(area string) {
	l.hubCall.stop()
	l.roomsCall.stop()
	l.followingCall.stop()
	l.hubCall.gen++
	callGen := l.hubCall.gen
	sessionGen := l.sessionGen
	reader := l.reader
	req := provider.LiveRequest{Section: provider.LiveSectionHub, Area: area}
	l.snapshot.Phase = CatalogLoading
	l.snapshot.Error = ""
	l.snapshot.RoomsLoading = false
	l.snapshot.RoomsError = ""
	l.snapshot.FollowingLoading = false
	l.snapshot.FollowingError = ""
	l.snapshot.Revision++
	ctx, cancel := context.WithCancel(context.Background())
	l.hubCall.cancel = cancel
	l.changed()
	go func() {
		page, err := reader.ReadLive(ctx, req)
		l.post(func() {
			if ctx.Err() != nil || l.sessionGen != sessionGen || l.hubCall.gen != callGen {
				return
			}
			cancel()
			l.hubCall.cancel = nil
			if err != nil {
				l.snapshot.Phase = CatalogFailed
				l.snapshot.Error = provider.CodeOf(err)
			} else {
				if len(page.Areas) != 0 {
					l.snapshot.Areas = slices.Clone(page.Areas)
				}
				rooms, _ := mergeLiveRooms(nil, page.Rooms)
				hero := page.Hero
				if hero == nil && len(rooms) != 0 {
					first := rooms[0]
					hero = &first
				}
				following, _ := mergeLiveRooms(nil, page.FollowingRooms)
				l.snapshot.Phase = CatalogReady
				l.snapshot.Loaded = true
				l.snapshot.Hero = hero
				l.snapshot.Rooms = rooms
				l.snapshot.RoomsCursor = page.NextCursor
				l.snapshot.RoomsStalled = false
				if page.FollowingRooms != nil || l.owner == "" {
					l.snapshot.FollowingRooms = following
					l.snapshot.FollowingCursor = page.FollowingCursor
					l.snapshot.FollowingStalled = false
				}
			}
			l.snapshot.Revision++
			l.changed()
		})
	}()
}

func mergeLiveRooms(existing, incoming []provider.MediaItem) ([]provider.MediaItem, bool) {
	out := slices.Clone(existing)
	seen := make(map[provider.MediaRef]bool, len(out)+len(incoming))
	for _, item := range out {
		seen[item.Ref] = true
	}
	added := false
	for _, item := range incoming {
		if seen[item.Ref] {
			continue
		}
		seen[item.Ref] = true
		out = append(out, item)
		added = true
	}
	return out, added
}
