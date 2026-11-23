package tvui

import (
	"crypto/sha256"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/provider"
	"github.com/yuchanns/MikuTV/internal/topshelf"
)

func (v *View) SyncTopShelf() {
	if v.TopShelf == nil || v.State == nil {
		return
	}
	if !v.hasTopShelfHash {
		if existing, err := v.TopShelf.Load(); err == nil {
			v.topShelfSnapshot = existing
			if encoded, encErr := topshelf.Encode(existing); encErr == nil {
				v.lastTopShelfHash = sha256.Sum256(encoded)
				v.hasTopShelfHash = true
			}
		}
	}
	entry, ok := v.State.Current()
	if !ok || entry.ID == "" {
		v.publishTopShelf(topshelf.Snapshot{Version: topshelf.SnapshotVersion})
		return
	}
	snapshot := v.buildTopShelfSnapshot(entry)
	// Avoid flashing empty TopShelf while initial catalog fetch for this entry is still in flight.
	if snapshot.ItemCount() == 0 && entry.Capabilities.Has(provider.BrowseCatalog) && v.Catalog != nil {
		phase := v.Catalog.Snapshot().Phase
		if phase == app.CatalogIdle || phase == app.CatalogLoading {
			return
		}
	}
	v.publishTopShelf(snapshot)
}

func (v *View) publishTopShelf(snapshot topshelf.Snapshot) {
	sanitized := topshelf.SanitizeSnapshot(snapshot)
	encoded, err := topshelf.Encode(sanitized)
	if err != nil {
		return
	}
	hash := sha256.Sum256(encoded)
	if v.hasTopShelfHash && hash == v.lastTopShelfHash {
		return
	}
	sanitized.UpdatedAt = time.Now().Unix()
	if err := v.TopShelf.Save(sanitized); err != nil {
		if !v.hasTopShelfHash || hash != v.lastTopShelfHash {
			log.Printf("topshelf: save failed: %v", err)
		}
		v.lastTopShelfHash = hash
		v.hasTopShelfHash = true
		return
	}
	v.lastTopShelfHash = hash
	v.hasTopShelfHash = true
	v.topShelfSnapshot = sanitized
	v.TopShelf.NotifyChanged()
	log.Printf("topshelf: published entry=%s sections=%d items=%d", sanitized.EntryID, len(sanitized.Sections), sanitized.ItemCount())
}

func (v *View) buildTopShelfSnapshot(entry app.Entry) topshelf.Snapshot {
	text := v.Assets.Text.TopShelf
	multiEntry := len(v.State.Entries()) > 1
	seen := make(map[string]bool)

	convert := func(media provider.MediaItem, isContinue bool) (topshelf.Item, bool) {
		if media.Ref.EntryID != "" && media.Ref.EntryID != entry.ID {
			return topshelf.Item{}, false
		}
		if !media.Ref.Kind.Valid() || strings.TrimSpace(media.Ref.ID) == "" || strings.TrimSpace(media.Title) == "" {
			return topshelf.Item{}, false
		}
		key := string(media.Ref.Kind) + "\x00" + media.Ref.ID
		if seen[key] {
			return topshelf.Item{}, false
		}
		converted := v.topShelfItemFromMedia(entry, media, isContinue)
		clean := topshelf.SanitizeSnapshot(topshelf.Snapshot{
			EntryID:   entry.ID,
			EntryName: entry.Name,
			Sections:  []topshelf.Section{{ID: "tmp", Title: "tmp", Items: []topshelf.Item{converted}}},
		})
		if len(clean.Sections) == 0 || len(clean.Sections[0].Items) == 0 {
			return topshelf.Item{}, false
		}
		seen[key] = true
		return clean.Sections[0].Items[0], true
	}

	var sections []topshelf.Section

	// 1. Continue Watching section (up to 2 unfinished items so Home Recommendations start on the first screen too).
	if v.Activity != nil && v.Activity.HistorySync() {
		var continueItems []topshelf.Item
		for _, row := range v.Activity.Snapshot(homeHistory).Page.Rows {
			if len(continueItems) >= 2 {
				break
			}
			item := row.Item
			if item == nil || item.ProgressSeconds == nil || *item.ProgressSeconds <= 0 {
				continue
			}
			if item.DurationSeconds != nil && *item.DurationSeconds > 0 && *item.ProgressSeconds >= *item.DurationSeconds {
				continue
			}
			if converted, ok := convert(*item, true); ok {
				continueItems = append(continueItems, converted)
			}
		}
		if len(continueItems) > 0 {
			title := text.SectionContinue
			if multiEntry && strings.TrimSpace(entry.Name) != "" && text.SectionContinueWithSource != "" {
				title = fmt.Sprintf(text.SectionContinueWithSource, entry.Name)
			}
			sections = append(sections, topshelf.Section{
				ID:    "continue",
				Title: title,
				Items: continueItems,
			})
		}
	}

	// 2. Home Recommendations section (up to 8 cards from current entry's home feed).
	var recItems []topshelf.Item
	addRec := func(media provider.MediaItem) {
		if len(recItems) >= topshelf.MaxItemsPerSection {
			return
		}
		if converted, ok := convert(media, false); ok {
			recItems = append(recItems, converted)
		}
	}
	for _, media := range v.home.featured {
		addRec(media)
	}
	if v.Catalog != nil {
		page := v.Catalog.Snapshot().Page
		for _, group := range page.Collections {
			for _, media := range group.Items {
				addRec(media)
			}
		}
		for _, shelf := range page.Shelves {
			for _, media := range shelf.Items {
				addRec(media)
			}
		}
	}
	if len(recItems) > 0 {
		title := text.SectionRecommended
		if multiEntry && strings.TrimSpace(entry.Name) != "" && text.SectionRecommendedWithSource != "" {
			title = fmt.Sprintf(text.SectionRecommendedWithSource, entry.Name)
		}
		sections = append(sections, topshelf.Section{
			ID:    "recommended",
			Title: title,
			Items: recItems,
		})
	}

	return topshelf.Snapshot{
		Version:   topshelf.SnapshotVersion,
		EntryID:   entry.ID,
		EntryName: entry.Name,
		Sections:  sections,
	}
}

func (v *View) topShelfItemFromMedia(entry app.Entry, media provider.MediaItem, isContinue bool) topshelf.Item {
	duration := 0
	if media.DurationSeconds != nil && *media.DurationSeconds > 0 {
		duration = int(*media.DurationSeconds)
	}
	progress := 0
	if media.ProgressSeconds != nil && *media.ProgressSeconds > 0 {
		progress = int(*media.ProgressSeconds)
	}
	creatorName := ""
	if media.Creator != nil {
		creatorName = strings.TrimSpace(media.Creator.Name)
	}
	imageURL := strings.TrimSpace(media.BackdropURL)
	if imageURL == "" {
		imageURL = strings.TrimSpace(media.CoverURL)
	}
	return topshelf.Item{
		EntryID:          entry.ID,
		EntryName:        entry.Name,
		Kind:             string(media.Ref.Kind),
		ID:               media.Ref.ID,
		Title:            media.Title,
		Genre:            strings.TrimSpace(media.Category),
		Badge:            strings.TrimSpace(media.Quality),
		CreatorName:      creatorName,
		ImageURL:         imageURL,
		DurationSeconds:  duration,
		ProgressSeconds:  progress,
		ContinueWatching: isContinue,
	}
}

func (v *View) OpenTopShelfURL(raw string) bool {
	link, ok := topshelf.ParseURL(raw)
	if !ok || v.State == nil {
		return false
	}
	if raw == v.lastOpenedURL && time.Since(v.lastOpenedAt) < 1500*time.Millisecond {
		return true
	}
	var targetEntry app.Entry
	foundEntry := false
	for _, e := range v.State.Entries() {
		if e.ID == link.EntryID {
			targetEntry = e
			foundEntry = true
			break
		}
	}
	if !foundEntry {
		v.pendingTopShelfURL = ""
		v.State.OpenEntries("")
		v.notice = v.Assets.Text.TopShelf.EntryUnavailable
		v.SyncEntry()
		log.Printf("topshelf: entry unavailable entry=%s", link.EntryID)
		return true
	}

	if v.State.CurrentEntry != targetEntry.ID {
		if !v.State.SelectEntry(targetEntry.ID) {
			return false
		}
		if v.Preferences != nil {
			_ = v.Preferences.Save(app.Preferences{RecentEntry: targetEntry.ID})
		}
		v.SyncEntry()
	}

	if targetEntry.Capabilities.Has(provider.SignIn) && v.Auth != nil && !v.Auth.Accounts().Ready {
		v.pendingTopShelfURL = raw
		return true
	}
	if link.Action == topshelf.ActionPlay && targetEntry.Capabilities.Has(provider.ResolvePlayback) && v.Playback == nil {
		v.pendingTopShelfURL = raw
		return true
	}

	v.pendingTopShelfURL = ""
	v.lastOpenedURL = raw
	v.lastOpenedAt = time.Now()

	seed := v.topShelfSeedItem(link)
	if v.Playback != nil && v.Playback.Active() {
		v.leavePlayback()
	}
	v.closeAllVideoDetails()
	v.closeAllCreators()
	v.State.Navigate(app.Route{Page: app.Home})

	page := v.context(v.State.Location, true, 1920, 1080)
	switch link.Action {
	case topshelf.ActionPlay:
		if seed.Ref.Kind == provider.Live && page.canPlay(seed.Ref) {
			page.openLivePlayback(seed, false)
		} else if page.canPlay(seed.Ref) {
			page.openPlayback([]playbackItem{{item: seed}}, 0, false, nil)
		} else if page.canOpenVideoDetail(seed.Ref) {
			page.openVideoDetail(seed)
		}
	case topshelf.ActionDetail:
		if seed.Ref.Kind == provider.Live && page.canPlay(seed.Ref) {
			page.openLivePlayback(seed, true)
		} else if page.canOpenVideoDetail(seed.Ref) {
			page.openVideoDetail(seed)
		}
	}
	v.SyncEntry()
	log.Printf("topshelf: opened action=%s entry=%s kind=%s id=%s page=%s", link.Action, link.EntryID, link.Kind, link.ID, v.State.Page)
	return true
}

func (v *View) FlushPendingTopShelfURL() {
	if v.pendingTopShelfURL == "" {
		return
	}
	v.OpenTopShelfURL(v.pendingTopShelfURL)
}

func (v *View) topShelfSeedItem(link topshelf.Link) provider.MediaItem {
	seed := provider.MediaItem{
		Ref: provider.MediaRef{
			EntryID: link.EntryID,
			Kind:    provider.MediaKind(link.Kind),
			ID:      link.ID,
		},
	}
	item, ok := v.topShelfSnapshot.Find(link.EntryID, link.Kind, link.ID)
	if !ok && v.TopShelf != nil {
		if loaded, err := v.TopShelf.Load(); err == nil {
			item, ok = loaded.Find(link.EntryID, link.Kind, link.ID)
		}
	}
	if !ok {
		return seed
	}
	seed.Title = item.Title
	seed.CoverURL = item.ImageURL
	seed.BackdropURL = item.ImageURL
	seed.Category = item.Genre
	seed.Quality = item.Badge
	if item.CreatorName != "" {
		seed.Creator = &provider.Creator{Name: item.CreatorName}
	}
	if item.DurationSeconds > 0 {
		d := int64(item.DurationSeconds)
		seed.DurationSeconds = &d
	}
	if item.ProgressSeconds > 0 {
		p := int64(item.ProgressSeconds)
		seed.ProgressSeconds = &p
	}
	return seed
}
