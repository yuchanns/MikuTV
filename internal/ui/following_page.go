package tvui

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/provider"
)

type followingTab string

const (
	followingViewAll  followingTab = "all"
	followingViewLive followingTab = "live"
)

type followingAllKind string

const (
	followingAllUpdates followingAllKind = "updates"
	followingAllLive    followingAllKind = "live"
)

const (
	followingTabAll         app.FocusID = app.ContentAction
	followingTabLive        app.FocusID = "following/tab/live"
	followingSignInFocus    app.FocusID = "following/sign-in"
	followingRetryFocus     app.FocusID = "following/retry"
	followingCancelFocus    app.FocusID = "following/cancel"
	followingDiscoverFocus  app.FocusID = "following/discover"
	followingUpdatesAll     app.FocusID = "following/all/updates"
	followingLiveAll        app.FocusID = "following/all/live"
	followingAllMoreFocus   app.FocusID = "following-all/more"

	followingHeaderRegion   = "following/header"
	followingCreatorsRegion = "following/creators"
	followingUpdatesRegion  = "following/updates"
	followingLiveRegion     = "following/live"
	followingStatusRegion   = "following/status"

	followingHeaderHeight     float32 = 102
	followingTitleHeight      float32 = 256
	followingCreatorsHeight   float32 = 230
	followingAvatarWidth      float32 = 138
	followingAvatarHeight     float32 = 151
	followingAvatarGap        float32 = 40
	followingAvatarStride     float32 = followingAvatarWidth + followingAvatarGap
	followingAvatarCircle     float32 = 92
	followingAvatarInner      float32 = 76
	followingShelfHeaderH     float32 = 40
	followingShelfHeaderGap   float32 = 25
	followingShelfBottomGap   float32 = 65
	followingShelfCardTopPad  float32 = 6
	followingShelfHeight      float32 = followingShelfHeaderH + followingShelfHeaderGap + followingShelfCardTopPad + homeCardHeight + followingShelfBottomGap
)

type followingViewState struct {
	tab              followingTab
	creatorScroll    ui.ScrollState
	updatesScroll    ui.ScrollState
	liveScroll       ui.ScrollState
	lastCreatorIndex int
	allSection       followingAllKind
	allList          ui.ListState
}

func (s *followingViewState) activeTab(supportsLive bool) followingTab {
	if s.tab == followingViewLive && supportsLive {
		return followingViewLive
	}
	return followingViewAll
}

func followingTabFocus(tab followingTab) app.FocusID {
	if tab == followingViewLive {
		return followingTabLive
	}
	return followingTabAll
}

func followingCreatorFocus(creatorID string) app.FocusID {
	return app.FocusID("following/creator/" + url.PathEscape(creatorID))
}

func followingUpdateFocus(ref provider.MediaRef) app.FocusID {
	return app.FocusID("following/update/" + string(ref.Kind) + "/" + url.PathEscape(ref.ID))
}

func followingLiveFocus(ref provider.MediaRef) app.FocusID {
	return app.FocusID("following/live/" + string(ref.Kind) + "/" + url.PathEscape(ref.ID))
}

func followingAllItemFocus(ref provider.MediaRef) app.FocusID {
	return app.FocusID("following-all/item/" + string(ref.Kind) + "/" + url.PathEscape(ref.ID))
}

func syncFollowing(v *View) {
	if v.Following != nil {
		v.Following.EnsureLoaded()
	}
}

func (v *View) showFollowingTab(tab followingTab) {
	if tab == followingViewLive && v.Following != nil {
		snap := v.Following.Snapshot()
		if snap.Loaded && !snap.SupportsLive {
			tab = followingViewAll
		}
	}
	if v.following.tab == tab {
		return
	}
	v.following.tab = tab
	v.setCatalogFocus(v.activeScroll, followingTabFocus(tab), true)
}

func (v *View) backFromFollowing() bool {
	scroll := v.activeScroll
	if v.State.Area != app.ContentArea || scroll == nil {
		return false
	}
	supportsLive := true
	if v.Following != nil {
		snap := v.Following.Snapshot()
		if snap.Loaded {
			supportsLive = snap.SupportsLive
		}
	}
	tab := followingTabFocus(v.following.activeTab(supportsLive))
	if target := v.focus.item(tab); target == nil || target.group != followingHeaderRegion {
		return false
	}
	onTab := v.State.Focus == tab
	if onTab && scroll.Y == 0 {
		return false
	}
	if scroll.Y > 0 && (!onTab || !scroll.Scrolling()) {
		scroll.ScrollTo(0, 0, homeReturnScroll)
	}
	if !onTab {
		v.moveFocus(tab, app.Up, false)
	}
	return true
}

type followingPage struct{}

func (followingPage) Render(c *ui.Context, p *PageContext) {
	feed := p.host.Following
	entry, _ := p.State.Current()
	if feed == nil || !entry.Capabilities.Has(provider.BrowseFollowing) {
		ui.Column(c).FillWidth().Padding(148, 83, 0, 103).Gap(44).
			Children(func() { contentPage{}.Render(c, p) })
		return
	}
	snapshot := feed.Snapshot()
	signedIn := feed.SignedIn()
	supportsLive := !snapshot.Loaded || snapshot.SupportsLive
	view := &p.host.following
	activeTab := view.activeTab(supportsLive)
	if view.tab != activeTab {
		view.tab = activeTab
	}

	p.restoreFollowingFocus(snapshot, signedIn, activeTab, supportsLive)
	p.prepareFollowingArtwork(snapshot)
	p.followingRegions(snapshot, signedIn, activeTab, supportsLive)

	ui.Column(c.Key("following-page")).FillWidth().Padding(0, 0, screenBottomInset, 0).
		Lang("zh-CN").Font("system-ui, PingFang SC").Children(func() {
		p.followingHeader(c, activeTab, supportsLive)
		if !signedIn {
			p.followingGuestState(c)
			return
		}
		p.followingTitle(c)
		switch {
		case snapshot.Phase == app.CatalogIdle || (snapshot.Phase == app.CatalogLoading && !snapshot.Loaded):
			p.followingLoadingState(c)
			return
		case snapshot.Phase == app.CatalogFailed && !snapshot.Loaded:
			p.followingFailedState(c, snapshot.Error)
			return
		case snapshot.Phase == app.CatalogCanceled && !snapshot.Loaded:
			p.followingCanceledState(c)
			return
		case len(snapshot.Creators) == 0 && len(snapshot.Updates) == 0 && len(snapshot.LiveRooms) == 0:
			p.followingEmptyCreators(c)
			return
		}
		if len(snapshot.Creators) != 0 {
			p.followingAvatarStrip(c, snapshot)
		}
		if activeTab == followingViewLive {
			p.followingLiveShelf(c, snapshot)
			return
		}
		p.followingUpdatesShelf(c, snapshot)
		if supportsLive {
			p.followingLiveShelf(c, snapshot)
		}
	})
}

func (p *PageContext) restoreFollowingFocus(snapshot app.FollowingSnapshot, signedIn bool, activeTab followingTab, supportsLive bool) {
	if !p.active {
		return
	}
	focus := p.State.Focus
	tabFocus := followingTabFocus(activeTab)
	if !signedIn {
		if focus != followingTabAll && focus != followingTabLive && focus != followingSignInFocus && focus != app.RailFocus(app.Following) {
			p.setFollowingFocus(followingSignInFocus)
		}
		return
	}
	if focus == followingTabLive && !supportsLive {
		p.setFollowingFocus(followingTabAll)
		return
	}
	if focus == followingSignInFocus {
		p.setFollowingFocus(tabFocus)
		return
	}
	if focus == followingRetryFocus && snapshot.Phase != app.CatalogFailed && snapshot.Phase != app.CatalogCanceled {
		p.setFollowingFocus(tabFocus)
		return
	}
	if focus == followingCancelFocus && snapshot.Phase != app.CatalogLoading {
		p.setFollowingFocus(tabFocus)
		return
	}
	if focus == followingDiscoverFocus && (len(snapshot.Creators) != 0 || len(snapshot.Updates) != 0 || len(snapshot.LiveRooms) != 0) {
		p.setFollowingFocus(tabFocus)
		return
	}
	if strings.HasPrefix(string(focus), "following/creator/") {
		for i, entry := range snapshot.Creators {
			if followingCreatorFocus(entry.Creator.ID) == focus {
				p.host.following.lastCreatorIndex = i
				return
			}
		}
		if len(snapshot.Creators) != 0 {
			idx := min(p.host.following.lastCreatorIndex, len(snapshot.Creators)-1)
			if idx < 0 {
				idx = 0
			}
			p.setFollowingFocus(followingCreatorFocus(snapshot.Creators[idx].Creator.ID))
			return
		}
		p.setFollowingFocus(tabFocus)
		return
	}
	if strings.HasPrefix(string(focus), "following/update/") {
		if activeTab != followingViewAll {
			p.setFollowingFocus(tabFocus)
			return
		}
		for _, item := range snapshot.Updates {
			if followingUpdateFocus(item.Ref) == focus {
				return
			}
		}
		if len(snapshot.Updates) != 0 {
			p.setFollowingFocus(followingUpdateFocus(snapshot.Updates[0].Ref))
			return
		}
		p.setFollowingFocus(tabFocus)
		return
	}
	if strings.HasPrefix(string(focus), "following/live/") {
		if !supportsLive {
			p.setFollowingFocus(tabFocus)
			return
		}
		for _, room := range snapshot.LiveRooms {
			if followingLiveFocus(room.Ref) == focus {
				return
			}
		}
		if len(snapshot.LiveRooms) != 0 {
			p.setFollowingFocus(followingLiveFocus(snapshot.LiveRooms[0].Ref))
			return
		}
		p.setFollowingFocus(tabFocus)
	}
}

func (p *PageContext) setFollowingFocus(focus app.FocusID) {
	p.State.Focus = focus
	p.host.reveal = true
}

func (p *PageContext) followingRegions(snapshot app.FollowingSnapshot, signedIn bool, activeTab followingTab, supportsLive bool) {
	tabFocus := followingTabFocus(activeTab)
	hasStatus := !signedIn ||
		snapshot.Phase == app.CatalogIdle ||
		(snapshot.Phase == app.CatalogLoading && !snapshot.Loaded) ||
		(snapshot.Phase == app.CatalogFailed && !snapshot.Loaded) ||
		(snapshot.Phase == app.CatalogCanceled && !snapshot.Loaded) ||
		(len(snapshot.Creators) == 0 && len(snapshot.Updates) == 0 && len(snapshot.LiveRooms) == 0)
	if hasStatus {
		p.region(followingHeaderRegion, focusEdges{app.Down: {Region: followingStatusRegion}})
		p.region(followingStatusRegion, focusEdges{app.Up: {Focus: tabFocus}})
		return
	}
	hasCreators := len(snapshot.Creators) != 0
	visibleCard := func(items []provider.MediaItem, scrollX float32, focusOf func(provider.MediaRef) app.FocusID) app.FocusID {
		if len(items) == 0 {
			return ""
		}
		stride := homeShelfCardSize.width + homeShelfGap
		first, _ := visibleItemRange(scrollX-4, p.Width-103, stride, len(items), 0)
		if first >= len(items) {
			first = len(items) - 1
		}
		return focusOf(items[first].Ref)
	}
	var firstShelf focusDestination
	if activeTab == followingViewLive {
		if target := visibleCard(snapshot.LiveRooms, p.host.following.liveScroll.X, followingLiveFocus); target != "" {
			firstShelf = focusDestination{Focus: target, Region: followingLiveRegion}
		} else {
			firstShelf = focusDestination{Region: followingLiveRegion}
		}
	} else {
		if target := visibleCard(snapshot.Updates, p.host.following.updatesScroll.X, followingUpdateFocus); target != "" {
			firstShelf = focusDestination{Focus: target, Region: followingUpdatesRegion}
		} else {
			firstShelf = focusDestination{Region: followingUpdatesRegion}
		}
	}
	if hasCreators {
		p.region(followingHeaderRegion, focusEdges{app.Down: {Region: followingCreatorsRegion}})
		p.region(followingCreatorsRegion, focusEdges{
			app.Up:   {Focus: tabFocus},
			app.Down: firstShelf,
		})
	} else {
		p.region(followingHeaderRegion, focusEdges{app.Down: firstShelf})
	}
	aboveFirstShelf := focusDestination{Focus: tabFocus}
	if hasCreators {
		aboveFirstShelf = focusDestination{Region: followingCreatorsRegion}
	}
	if activeTab == followingViewLive {
		p.region(followingLiveRegion, focusEdges{app.Up: aboveFirstShelf})
		return
	}
	updatesEdges := focusEdges{app.Up: aboveFirstShelf}
	if supportsLive {
		if target := visibleCard(snapshot.LiveRooms, p.host.following.liveScroll.X, followingLiveFocus); target != "" {
			updatesEdges[app.Down] = focusDestination{Focus: target, Region: followingLiveRegion}
		} else {
			updatesEdges[app.Down] = focusDestination{Region: followingLiveRegion}
		}
		p.region(followingLiveRegion, focusEdges{app.Up: {Region: followingUpdatesRegion}})
	}
	p.region(followingUpdatesRegion, updatesEdges)
}

func (p *PageContext) prepareFollowingArtwork(snapshot app.FollowingSnapshot) {
	if p.host.Catalog == nil {
		return
	}
	view := &p.host.following
	viewport := p.Width - 103
	urls := make([]string, 0, 24)
	if len(snapshot.Creators) != 0 {
		first, end := visibleItemRange(view.creatorScroll.X, viewport, followingAvatarStride, len(snapshot.Creators), 3)
		for i := first; i < end; i++ {
			if u := snapshot.Creators[i].Creator.AvatarURL; u != "" {
				urls = append(urls, u)
			}
		}
	}
	stride := homeShelfCardSize.width + homeShelfGap
	if len(snapshot.Updates) != 0 {
		first, end := visibleItemRange(view.updatesScroll.X, viewport, stride, len(snapshot.Updates), 2)
		for i := first; i < end; i++ {
			item := snapshot.Updates[i]
			if item.CoverURL != "" {
				urls = append(urls, item.CoverURL)
			}
			if item.Creator != nil && item.Creator.AvatarURL != "" {
				urls = append(urls, item.Creator.AvatarURL)
			}
		}
	}
	if len(snapshot.LiveRooms) != 0 {
		first, end := visibleItemRange(view.liveScroll.X, viewport, stride, len(snapshot.LiveRooms), 2)
		for i := first; i < end; i++ {
			room := snapshot.LiveRooms[i]
			if room.CoverURL != "" {
				urls = append(urls, room.CoverURL)
			}
		}
	}
	p.host.Catalog.PrepareArtwork(urls)
}

func (p *PageContext) followingHeader(c *ui.Context, activeTab followingTab, supportsLive bool) {
	text, colors := p.Assets.Text.Following, p.Theme.Colors
	type tabEntry struct {
		tab   followingTab
		id    app.FocusID
		label string
	}
	tabs := []tabEntry{{followingViewAll, followingTabAll, text.TabAll}}
	if supportsLive {
		tabs = append(tabs, tabEntry{followingViewLive, followingTabLive, text.TabLive})
	}
	ui.Box(c.Key("following-header")).FillWidth().Height(followingHeaderHeight).Children(func() {
		ui.Row(c).Absolute().Top(45).Left(78).Height(61).Gap(18).AlignItems(ui.Center).Children(func() {
			for _, item := range tabs {
				selected := item.tab == activeTab
				focused := p.paintFocus() == item.id
				background, ink := ui.Transparent, paint(colors.Text.Muted)
				if selected {
					background, ink = withAlpha(colors.Text.Primary, 0x12), paint(colors.Text.Primary)
				}
				if focused {
					background, ink = withAlpha(colors.Focus.Background, 0x13), paint(colors.Text.Primary)
				}
				const tabWidth float32 = 148
				button := ui.Button(c.Key(string(item.id)), "").Label(item.label).Size(tabWidth, 61).Padding(0, 0).
					Radius(30).Background(background).TextColor(ink).Center().Role(ui.RoleTab).AccessSelected(selected).
					OnClick(func() { p.dispatch(func() { p.host.showFollowingTab(item.tab) }) })
				p.animateFocus(button, background, ink)
				p.bind(button, item.id, followingHeaderRegion)
				button.Children(func() {
					ui.Text(c, item.label).FontSize(24).LetterSpacing(1)
					if selected || focused {
						ui.Box(c).Absolute().Bottom(2).Left((tabWidth-15)/2).Size(15, 3).
							Radius(2).Background(paint(colors.Accent.Primary))
					}
				})
			}
		})
	})
}

func (p *PageContext) followingTitle(c *ui.Context) {
	text, colors := p.Assets.Text.Following, p.Theme.Colors
	ui.Box(c.Key("following-title")).FillWidth().Height(followingTitleHeight).Children(func() {
		ui.Row(c).Absolute().Top(67).Left(103).Height(22).Gap(15).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(24, 1).Background(paint(colors.Accent.Soft))
			ui.Text(c, text.Eyebrow).FontSize(16).FontWeight(500).LetterSpacing(3).TextColor(paint(colors.Text.Secondary))
		})
		ui.Text(c, text.Title).Absolute().Top(112).Left(103).FontSize(54).FontWeight(550).LetterSpacing(2)
		ui.Text(c, text.Subtitle).Absolute().Top(208).Left(103).FontSize(23).TextColor(paint(colors.Text.Muted))
	})
}

func (p *PageContext) followingAvatarStrip(c *ui.Context, snapshot app.FollowingSnapshot) {
	view := &p.host.following
	creators := snapshot.Creators
	viewport := p.Width - 103
	scroll := &view.creatorScroll
	first, end := visibleItemRange(scroll.X-6, viewport, followingAvatarStride, len(creators), 2)
	focusedIndex := -1
	for i, entry := range creators {
		if p.State.Focus == followingCreatorFocus(entry.Creator.ID) {
			focusedIndex = i
			view.lastCreatorIndex = i
			break
		}
	}
	if p.active && snapshot.CanLoadMoreCreators() {
		nearEnd := (focusedIndex >= 0 && focusedIndex >= len(creators)-5) ||
			(scroll.X+viewport >= float32(len(creators))*followingAvatarStride-2*followingAvatarStride)
		if nearEnd {
			p.host.Following.LoadMoreCreators()
		}
	}
	totalWidth := float32(6) + float32(len(creators))*followingAvatarStride - followingAvatarGap + 83
	ui.Box(c.Key("following-creators")).FillWidth().Height(followingCreatorsHeight).Children(func() {
		ui.ScrollHorizontal(c.Key("following-avatar-strip")).Absolute().Top(27).Left(103).Right(0).Height(177).
			TrackScroll(scroll).Children(func() {
			ui.Box(c).Width(max(totalWidth, viewport)).Height(177).Children(func() {
				draw := func(i int) {
					entry := creators[i]
					id := followingCreatorFocus(entry.Creator.ID)
					ui.Box(c.Key("following-avatar-slot/"+entry.Creator.ID)).Absolute().
						Top(7).Left(6+float32(i)*followingAvatarStride).
						Size(followingAvatarWidth, followingAvatarHeight).Children(func() {
						p.followingAvatarButton(c, id, entry)
					})
				}
				for i := first; i < end; i++ {
					draw(i)
				}
				if focusedIndex >= 0 {
					for i := max(0, focusedIndex-1); i < min(len(creators), focusedIndex+2); i++ {
						if i < first || i >= end {
							draw(i)
						}
					}
				}
			})
		})
	})
}

func (p *PageContext) followingAvatarButton(c *ui.Context, id app.FocusID, entry provider.FollowingCreator) {
	colors, d := p.Theme.Colors, p.uiColors()
	focused := p.paintFocus() == id
	ringWidth, ringColor := float32(3), withAlpha(colors.Text.Primary, 0x15)
	nameColor := d.secondaryText
	if focused {
		ringWidth, ringColor = 4, paint(colors.Focus.Ring)
		nameColor = paint(colors.Text.Primary)
	}
	creator := entry.Creator
	creator.Following = true
	button := ui.Button(c.Key(string(id)), "").Label(creator.Name).
		Size(followingAvatarWidth, followingAvatarHeight).Padding(6, 0, 13, 0).
		Border(0, ui.Transparent).Background(ui.Transparent).
		OnClick(func() { p.dispatch(func() { p.openCreator(creator) }) })
	p.bind(button, id, followingCreatorsRegion)
	button.Children(func() {
		ui.Column(c).Fill().AlignItems(ui.Center).Children(func() {
			ring := ui.Box(c.Key("avatar-ring/"+creator.ID)).Size(followingAvatarCircle, followingAvatarCircle).
				Radius(followingAvatarCircle/2).Center().Margin(0, 0, 15, 0)
			ring.Border(ringWidth, ring.AnimateColor("focus-ring", ringColor, ui.Animation{Duration: focusDuration, NonMotion: true}))
			ring.Children(func() {
				ui.Box(c).Size(followingAvatarInner, followingAvatarInner).Radius(followingAvatarInner/2).
					Clip().Center().Background(d.avatarStart).Children(func() {
					if portrait := p.catalogCover(creator.AvatarURL); portrait != nil {
						ui.Image(c.Key("following-avatar-img/"+creator.ID), portrait).Fill().Fit(ui.Cover)
					} else {
						ui.Icon(c, p.Assets.Icon("user")).Size(34, 34).TextColor(paint(colors.Text.Secondary))
					}
				})
			})
			ui.Text(c, creator.Name).FillWidth().TextAlign(ui.Center).SingleLine().
				FontSize(18).TextColor(nameColor)
		})
	})
}

func (p *PageContext) followingUpdatesShelf(c *ui.Context, snapshot app.FollowingSnapshot) {
	text, colors := p.Assets.Text, p.Theme.Colors
	updates := snapshot.Updates
	scroll := &p.host.following.updatesScroll
	ui.Column(c.Key("following-updates-shelf")).FillWidth().Padding(0, 0, followingShelfBottomGap, 103).Children(func() {
		p.followingShelfHeader(c, text.Following.UpdatesTitle, p.sourceCreatorGroupTitle(), followingUpdatesAll, followingUpdatesRegion, func() {
			p.openFollowingAll(followingAllUpdates)
		})
		ui.Box(c).Height(followingShelfHeaderGap)
		if len(updates) == 0 {
			ui.Text(c, text.Following.EmptyUpdates).Margin(20, 0, 0, 4).FontSize(22).TextColor(paint(colors.Text.Secondary))
			return
		}
		p.followingMediaStrip(c, "updates", followingUpdatesRegion, updates, scroll, false, snapshot.CanLoadMoreUpdates(), func() {
			p.host.Following.LoadMoreUpdates()
		})
		if snapshot.UpdatesError != "" {
			ui.Text(c, text.Following.MoreFailed).Margin(8, 0, 0, 4).FontSize(18).TextColor(paint(colors.Status.Error.Foreground))
		}
	})
}

func (p *PageContext) followingLiveShelf(c *ui.Context, snapshot app.FollowingSnapshot) {
	text, colors := p.Assets.Text, p.Theme.Colors
	rooms := snapshot.LiveRooms
	scroll := &p.host.following.liveScroll
	ui.Column(c.Key("following-live-shelf")).FillWidth().Padding(0, 0, followingShelfBottomGap, 103).Children(func() {
		p.followingShelfHeader(c, text.Following.LiveTitle, text.Following.LiveNote, followingLiveAll, followingLiveRegion, func() {
			p.openFollowingAll(followingAllLive)
		})
		ui.Box(c).Height(followingShelfHeaderGap)
		if len(rooms) == 0 {
			ui.Text(c, text.Following.EmptyLive).Margin(20, 0, 0, 4).FontSize(22).TextColor(paint(colors.Text.Secondary))
			return
		}
		p.followingMediaStrip(c, "live", followingLiveRegion, rooms, scroll, true, snapshot.CanLoadMoreLive(), func() {
			p.host.Following.LoadMoreLive()
		})
		if snapshot.LiveError != "" {
			ui.Text(c, text.Following.MoreFailed).Margin(8, 0, 0, 4).FontSize(18).TextColor(paint(colors.Status.Error.Foreground))
		}
	})
}

func (p *PageContext) followingShelfHeader(c *ui.Context, title, note string, seeAllFocus app.FocusID, region string, onSeeAll func()) {
	colors, text := p.Theme.Colors, p.Assets.Text
	ui.Row(c).FillWidth().Padding(0, 79, 0, 0).Height(followingShelfHeaderH).Gap(18).AlignItems(ui.Center).Children(func() {
		ui.Text(c, title).FontSize(27).FontWeight(550).LetterSpacing(1.5)
		if note != "" {
			ui.Text(c, note).FontSize(15).LetterSpacing(0.5).Translate(0, 5).TextColor(paint(colors.Text.Secondary))
		}
		ui.Spacer(c).Grow(1)
		p.styledButton(c, text.Home.SeeAll, "", seeAllFocus, region, onSeeAll, buttonAppearance{
			height: 40, radius: 8, fontSize: 17,
			background: ui.Transparent, foreground: paint(colors.Text.Muted),
		}).Width(92).Center()
	})
}

func (p *PageContext) followingMediaStrip(c *ui.Context, key, region string, items []provider.MediaItem, scroll *ui.ScrollState, live, canLoadMore bool, loadMore func()) {
	cardWidth := homeShelfCardSize.width
	viewport := p.Width - 103
	stride := cardWidth + homeShelfGap
	first, end := visibleItemRange(scroll.X-4, viewport, stride, len(items), 2)
	focused := -1
	focusOf := followingUpdateFocus
	if live {
		focusOf = followingLiveFocus
	}
	for i, item := range items {
		if p.State.Focus == focusOf(item.Ref) {
			focused = i
			break
		}
	}
	if p.active && canLoadMore && loadMore != nil {
		nearEnd := (focused >= 0 && focused >= len(items)-3) ||
			(scroll.X+viewport >= float32(len(items))*stride-2*stride)
		if nearEnd {
			loadMore()
		}
	}
	totalWidth := float32(4) + float32(len(items))*stride - homeShelfGap + 83
	ui.ScrollHorizontal(c.Key("following-strip/"+key)).FillWidth().Height(homeCardHeight+followingShelfCardTopPad).
		TrackScroll(scroll).Children(func() {
		ui.Box(c).Width(max(totalWidth, viewport)).Height(homeCardHeight + followingShelfCardTopPad).Children(func() {
			draw := func(i int) {
				item := items[i]
				id := focusOf(item.Ref)
				ui.Box(c.Key("following-card/"+string(id))).Absolute().
					Top(followingShelfCardTopPad).Left(4+float32(i)*stride).
					Size(cardWidth, homeCardHeight).Children(func() {
					if live {
						p.followingLiveCard(c, id, region, item, homeShelfCardSize)
					} else {
						p.catalogCard(c, id, region, item, homeShelfCardSize, nil)
					}
				})
			}
			for i := first; i < end; i++ {
				draw(i)
			}
			if focused >= 0 {
				for i := max(0, focused-1); i < min(len(items), focused+2); i++ {
					if i < first || i >= end {
						draw(i)
					}
				}
			}
		})
	})
}

func (p *PageContext) followingLiveCard(c *ui.Context, id app.FocusID, region string, item provider.MediaItem, size catalogCardSize) {
	colors, d, text := p.Theme.Colors, p.uiColors(), p.Assets.Text
	card := ui.Button(c.Key(string(id)), "").Size(size.width, size.height).Shrink(0).Padding(0, 0).
		Border(0, ui.Transparent).Background(ui.Transparent).Label(item.Title).
		OnClick(func() { p.dispatch(func() { p.host.notice = text.Search.DetailPending }) })
	p.bind(card, id, region)
	card.Children(func() {
		ui.Column(c).Fill().Children(func() {
			scale, stroke := float32(1), ui.Transparent
			if p.paintFocus() == id {
				scale, stroke = 1.035, paint(colors.Focus.Ring)
			}
			cover := ui.Box(c.Key("live-cover")).FillWidth().Height(size.cover).Radius(13).Clip().
				Background(d.coverFill)
			cover.Children(func() {
				if image := p.catalogCover(item.CoverURL); image != nil {
					photo := ui.Image(c.Key("live-photo"), image).Fill().Fit(ui.Cover)
					zoom := photo.AnimateTo("focus-zoom", scale, ui.Animation{Duration: focusDuration})
					photo.Scale(zoom, zoom)
					ui.Box(c).Absolute().Left(0).Top(0).Fill().LinearGradient(ui.LinearGradient{
						Angle: 0, Stops: []ui.GradientStop{{Color: d.coverShade}, {Color: ui.Transparent, At: 0.55}},
					})
				}
				ui.Row(c).Absolute().Left(12).Top(12).ZIndex(1).Padding(4, 8).Gap(6).AlignItems(ui.Center).
					Radius(4).Background(paint(colors.Accent.Primary)).Children(func() {
					ui.Box(c).Size(6, 6).Radius(3).Background(paint(colors.Accent.Foreground))
					ui.Text(c, text.Following.LiveBadge).FontSize(11).FontWeight(650).LetterSpacing(1).
						TextColor(paint(colors.Accent.Foreground))
				})
				if item.Views != nil {
					ui.Text(c, catalogViews(*item.Views, text.Home)).Absolute().Right(13).Bottom(12).
						ZIndex(1).FontSize(14).LetterSpacing(0.7).TextColor(paint(colors.Playback.Foreground))
				}
				ring := ui.Box(c.Key("live-focus")).Absolute().Left(0).Top(0).Fill().ZIndex(1).Radius(13)
				ring.Border(4, ring.AnimateColor("focus-ring", stroke, ui.Animation{Duration: focusDuration, NonMotion: true}))
			})
			ui.Text(c, item.Title).FillWidth().Margin(16, 0, 9, 0).FontSize(size.title).FontWeight(500).
				LetterSpacing(0.3).SingleLine()
			p.followingLiveMetadata(c, item)
		})
	})
}

func (p *PageContext) followingLiveMetadata(c *ui.Context, item provider.MediaItem) {
	ink := paint(p.Theme.Colors.Text.Secondary)
	var parts []string
	if item.Creator != nil && strings.TrimSpace(item.Creator.Name) != "" {
		parts = append(parts, item.Creator.Name)
	}
	if cat := strings.TrimSpace(item.Category); cat != "" {
		parts = append(parts, cat)
	} else if sub := strings.TrimSpace(item.Subtitle); sub != "" {
		parts = append(parts, sub)
	}
	if len(parts) == 0 {
		return
	}
	ui.Text(c, strings.Join(parts, " \u00b7 ")).FillWidth().FontSize(15).SingleLine().TextColor(ink)
}

func (p *PageContext) followingGuestState(c *ui.Context) {
	text := p.Assets.Text
	p.followingStateBlock(c, "following", text.Following.GuestTitle, text.Following.GuestNote,
		text.Activity.SignInAction, "user", followingSignInFocus, func() {
			p.host.openSignIn()
		})
}

func (p *PageContext) followingLoadingState(c *ui.Context) {
	text := p.Assets.Text
	p.followingStateBlock(c, "following", text.Following.Loading, "", text.Cancel, "", followingCancelFocus, func() {
		p.host.Following.Cancel()
	})
}

func (p *PageContext) followingFailedState(c *ui.Context, code provider.ErrorCode) {
	text := p.Assets.Text
	if code == provider.NeedsSignIn {
		p.followingStateBlock(c, "following", text.Auth.AccountExpired, text.Following.GuestNote,
			text.Activity.SignInAction, "user", followingSignInFocus, func() {
				p.host.openSignIn()
			})
		return
	}
	p.followingStateBlock(c, "following", text.Following.LoadFailed, p.authError(code), text.Retry, "refresh", followingRetryFocus, func() {
		p.host.Following.Retry()
	})
}

func (p *PageContext) followingCanceledState(c *ui.Context) {
	text := p.Assets.Text
	p.followingStateBlock(c, "following", text.CanceledTitle, "", text.Retry, "refresh", followingRetryFocus, func() {
		p.host.Following.Retry()
	})
}

func (p *PageContext) followingEmptyCreators(c *ui.Context) {
	text := p.Assets.Text
	p.followingStateBlock(c, "following", text.Following.EmptyCreatorsTitle, text.Following.EmptyCreatorsNote,
		text.Activity.DiscoverContent, "", followingDiscoverFocus, func() {
			p.State.Navigate(app.Route{Page: app.Home})
		})
}

func (p *PageContext) followingStateBlock(c *ui.Context, icon, title, note, action, actionIcon string, focus app.FocusID, run func()) {
	colors, d := p.Theme.Colors, p.uiColors()
	style := buttonAppearance{
		height: 62, padding: 27, radius: 12, fontSize: 22, fontWeight: 550, iconSize: 24, iconGap: 13,
		background: d.secondaryFill, foreground: d.secondaryText, border: d.secondaryBorder,
	}
	ui.Column(c).FillWidth().Padding(80, 83, 40, 103).AlignItems(ui.Center).Children(func() {
		ui.Icon(c, p.Assets.Icon(icon)).Size(65, 65).TextColor(paint(colors.Accent.Soft))
		ui.Text(c, title).Margin(28, 0, 16, 0).FontSize(36).FontWeight(450).LineHeight(1.39)
		if note != "" {
			ui.Text(c, note).Margin(0, 0, 32, 0).FontSize(21).LineHeight(1.5).TextColor(paint(colors.Text.Muted))
		}
		p.styledButton(c, action, actionIcon, focus, followingStatusRegion, run, style).AlignSelf(ui.Center)
	})
}

func (p *PageContext) openFollowingAll(kind followingAllKind) {
	if p.State.Push(app.Route{Page: app.FollowingAll}) {
		p.host.following.allSection = kind
		p.host.following.allList = ui.ListState{}
	}
}

type followingAllPage struct{}

func (followingAllPage) Render(c *ui.Context, p *PageContext) {
	feed := p.host.Following
	text, colors := p.Assets.Text, p.Theme.Colors
	kind := p.host.following.allSection
	title := text.Following.UpdatesTitle
	if kind == followingAllLive {
		title = text.Following.LiveTitle
	}
	var items []provider.MediaItem
	var canLoadMore, loading bool
	var loadMore func()
	if feed != nil {
		snap := feed.Snapshot()
		if kind == followingAllLive {
			items = snap.LiveRooms
			canLoadMore = snap.CanLoadMoreLive()
			loading = snap.LiveLoading
			loadMore = feed.LoadMoreLive
		} else {
			items = snap.Updates
			canLoadMore = snap.CanLoadMoreUpdates()
			loading = snap.UpdatesLoading
			loadMore = feed.LoadMoreUpdates
		}
	}
	const region = "following-all"
	p.region(region, nil)
	meta := ""
	if len(items) != 0 {
		meta = fmt.Sprintf(text.Modal.ItemsCount, len(items))
	}
	eyebrow := "FOLLOWING \u00b7 UPDATES"
	if kind == followingAllLive {
		eyebrow = "FOLLOWING \u00b7 LIVE"
	}
	ui.Column(c).Fill().Lang("zh-CN").Font("system-ui, PingFang SC").Children(func() {
		p.drawerHeader(c, eyebrow, title, meta, "")
		if len(items) == 0 {
			ui.Column(c).FillWidth().Grow(1).Center().Gap(20).Children(func() {
				emptyNote := text.Following.EmptyUpdates
				if kind == followingAllLive {
					emptyNote = text.Following.EmptyLive
				}
				p.emptyDescription(c, text.EmptyTitle, emptyNote)
			})
		} else {
			list := &p.host.following.allList
			totalRows := len(items)
			if canLoadMore || loading {
				totalRows++
			}
			list.Key = func(index int) any {
				if index >= len(items) {
					return "more"
				}
				return items[index].Ref
			}
			ui.List(c.Key("following-all-list"), list, totalRows).FillWidth().Grow(1).
				Padding(0, 0, focusBottomMargin, 0).Rows(func(row ui.ListRow) {
				rc := row.Context
				if row.Index >= len(items) {
					label := text.Activity.More
					if loading {
						label = text.LoadingTitle
					}
					p.drawerOptionRow(rc, followingAllMoreFocus, region, label, "", false, func() {
						if canLoadMore && loadMore != nil {
							loadMore()
						}
					})
					return
				}
				item := items[row.Index]
				id := followingAllItemFocus(item.Ref)
				sub := ""
				if item.Creator != nil {
					sub = item.Creator.Name
				}
				if kind != followingAllLive && p.canOpenVideoDetail(item.Ref) {
					p.drawerOptionRow(rc, id, region, item.Title, sub, false, func() { p.openVideoDetail(item) })
				} else {
					p.drawerOptionRow(rc, id, region, item.Title, sub, false, func() { p.host.notice = text.Search.DetailPending })
				}
			})
		}
		ui.Row(c).Margin(16, 0, 0, 0).Children(func() {
			p.styledButton(c, text.Close, "", app.CloseModal, region, func() { p.State.Back() }, p.drawerButtonAppearance())
		})
		p.drawerFooter(c, text.Modal.HintSelect, text.Modal.HintOpen)
	})
	_ = colors
}
