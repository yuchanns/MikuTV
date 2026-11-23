package tvui

import (
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/provider"
)

type liveAllKind string

const (
	liveAllRooms     liveAllKind = "rooms"
	liveAllFollowing liveAllKind = "following"
)

const (
	liveHeroPlayFocus     app.FocusID = "live/hero/play"
	liveHeroInfoFocus     app.FocusID = "live/hero/info"
	liveRoomsAllFocus     app.FocusID = "live/all/rooms"
	liveFollowingAllFocus app.FocusID = "live/all/following"
	liveRetryFocus        app.FocusID = "live/retry"
	liveAllBackFocus      app.FocusID = "live-all/back"
	liveAllRetryFocus     app.FocusID = "live-all/retry"

	liveHeaderRegion    = "live/header"
	liveHeroRegion      = "live/hero"
	liveRoomsRegion     = "live/rooms"
	liveFollowingRegion = "live/following"
	liveStatusRegion    = "live/status"

	liveHeaderHeight   float32 = 102
	liveHeroHeight     float32 = 440
	liveArtHeight      float32 = 780
	liveShelfHeaderH   float32 = 40
	liveShelfHeaderGap float32 = 25
	liveShelfCardPad   float32 = 6
)

type liveViewState struct {
	headerScroll    ui.ScrollState
	roomsScroll     ui.ScrollState
	followingScroll ui.ScrollState
	featuredRef     provider.MediaRef
	allSection      liveAllKind
}

func liveAreaFocus(id string) app.FocusID {
	if id == "" {
		return app.ContentAction
	}
	return app.FocusID("live/area/" + url.PathEscape(id))
}

func liveRoomCardFocus(shelf string, ref provider.MediaRef) app.FocusID {
	return app.FocusID("live/" + shelf + "/" + string(ref.Kind) + "/" + url.PathEscape(ref.ID))
}

func liveAllItemFocus(ref provider.MediaRef) app.FocusID {
	return app.FocusID("live-all/item/" + string(ref.Kind) + "/" + url.PathEscape(ref.ID))
}

func syncLive(v *View) {
	if v.Live != nil {
		v.Live.EnsureLoaded()
	}
}

func (v *View) selectLiveArea(area string) {
	if v.Live == nil {
		return
	}
	v.live.roomsScroll = ui.ScrollState{}
	v.live.featuredRef = provider.MediaRef{}
	v.setCatalogFocus(v.activeScroll, liveAreaFocus(area), true)
	v.Live.SelectArea(area)
}

func (v *View) backFromLive() bool {
	scroll := v.activeScroll
	if v.State.Area != app.ContentArea || scroll == nil {
		return false
	}
	area := ""
	if v.Live != nil {
		area = v.Live.Snapshot().Area
	}
	tab := liveAreaFocus(area)
	if target := v.focus.item(tab); target == nil || target.group != liveHeaderRegion {
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

type livePage struct{}

func (livePage) Render(c *ui.Context, p *PageContext) {
	if p.Location.Page != app.Live {
		p.Outlet(c)
		return
	}
	catalog := p.host.Live
	entry, _ := p.State.Current()
	if catalog == nil || !entry.Capabilities.Has(provider.BrowseLive) {
		p.renderUnavailableContent(c)
		return
	}
	snap := catalog.Snapshot()
	p.restoreLiveFocus(snap)
	featured := p.liveFeaturedItem(snap)
	p.prepareLiveArtwork(snap, featured)
	p.liveRegions(snap)

	ui.Box(c.Key("live-page")).FillWidth().Lang(p.langTag()).Font(p.uiFont()).Children(func() {
		if featured != nil && p.scroll().Y < liveArtHeight {
			p.liveBackdrop(c, *featured)
		}
		ui.Column(c.Key("live-content")).FillWidth().Padding(0, 0, screenBottomInset, 0).ZIndex(1).Children(func() {
			p.liveHeader(c, snap)
			switch {
			case snap.Phase == app.CatalogIdle || (snap.Phase == app.CatalogLoading && !snap.Loaded):
				p.liveStateBlock(c, "live", p.Assets.Text.Live.Loading, "", "", "", "", nil)
				return
			case snap.Phase == app.CatalogFailed && !snap.Loaded:
				p.liveStateBlock(c, "live", p.Assets.Text.Live.LoadFailed, p.authError(snap.Error), p.Assets.Text.Retry, "refresh", liveRetryFocus, func() {
					catalog.Retry()
				})
				return
			case snap.Phase == app.CatalogCanceled && !snap.Loaded:
				p.liveStateBlock(c, "live", p.Assets.Text.CanceledTitle, "", p.Assets.Text.Retry, "refresh", liveRetryFocus, func() {
					catalog.Retry()
				})
				return
			}
			if featured != nil {
				p.liveHero(c, *featured)
			}
			if len(snap.Rooms) == 0 && snap.Phase == app.CatalogReady {
				p.liveEmptyArea(c)
			} else {
				p.liveRoomShelf(c, "rooms", p.Assets.Text.Live.RecommendedTitle, p.Assets.Text.Live.RecommendedNote,
					snap.Rooms, &p.host.live.roomsScroll, liveRoomsAllFocus, liveRoomsRegion,
					snap.CanLoadMoreRooms(), snap.RoomsError, catalog.LoadMoreRooms, func() { p.openLiveAll(liveAllRooms) })
			}
			if len(snap.FollowingRooms) != 0 {
				p.liveRoomShelf(c, "following", p.Assets.Text.Live.FollowingTitle, p.Assets.Text.Live.FollowingNote,
					snap.FollowingRooms, &p.host.live.followingScroll, liveFollowingAllFocus, liveFollowingRegion,
					snap.CanLoadMoreFollowing(), snap.FollowingError, catalog.LoadMoreFollowing, func() { p.openLiveAll(liveAllFollowing) })
			}
		})
	})
}

func (p *PageContext) restoreLiveFocus(snap app.LiveSnapshot) {
	if !p.active {
		return
	}
	focus := p.State.Focus
	tab := liveAreaFocus(snap.Area)
	if focus == liveRetryFocus && snap.Phase != app.CatalogFailed && snap.Phase != app.CatalogCanceled {
		p.State.Focus = tab
		p.host.reveal = true
		return
	}
	if strings.HasPrefix(string(focus), "live/rooms/") {
		for _, room := range snap.Rooms {
			if liveRoomCardFocus("rooms", room.Ref) == focus {
				return
			}
		}
		if len(snap.Rooms) != 0 {
			p.State.Focus = liveRoomCardFocus("rooms", snap.Rooms[0].Ref)
		} else {
			p.State.Focus = tab
		}
		p.host.reveal = true
		return
	}
	if strings.HasPrefix(string(focus), "live/following/") {
		for _, room := range snap.FollowingRooms {
			if liveRoomCardFocus("following", room.Ref) == focus {
				return
			}
		}
		if len(snap.FollowingRooms) != 0 {
			p.State.Focus = liveRoomCardFocus("following", snap.FollowingRooms[0].Ref)
		} else {
			p.State.Focus = tab
		}
		p.host.reveal = true
	}
}

func (p *PageContext) liveRegions(snap app.LiveSnapshot) {
	tab := liveAreaFocus(snap.Area)
	hasStatus := snap.Phase == app.CatalogIdle ||
		(snap.Phase == app.CatalogLoading && !snap.Loaded) ||
		(snap.Phase == app.CatalogFailed && !snap.Loaded) ||
		(snap.Phase == app.CatalogCanceled && !snap.Loaded)
	if hasStatus {
		p.region(liveHeaderRegion, focusEdges{app.Down: {Region: liveStatusRegion}})
		p.region(liveStatusRegion, focusEdges{app.Up: {Focus: tab, Region: liveHeaderRegion}})
		return
	}

	stride := homeShelfCardSize.width + homeShelfGap
	visibleRoomCard := func(shelf string, items []provider.MediaItem, scrollX float32) app.FocusID {
		if len(items) == 0 {
			return ""
		}
		first, _ := visibleItemRange(scrollX-4, p.Width-103, stride, len(items), 0)
		if first >= len(items) {
			first = len(items) - 1
		}
		return liveRoomCardFocus(shelf, items[first].Ref)
	}

	roomsDest := focusDestination{}
	if len(snap.Rooms) != 0 {
		roomsDest = focusDestination{Focus: visibleRoomCard("rooms", snap.Rooms, p.host.live.roomsScroll.X), Region: liveRoomsRegion}
	}
	followingDest := focusDestination{}
	if len(snap.FollowingRooms) != 0 {
		followingDest = focusDestination{Focus: visibleRoomCard("following", snap.FollowingRooms, p.host.live.followingScroll.X), Region: liveFollowingRegion}
	}
	belowHeader := roomsDest
	if belowHeader.Region == "" {
		belowHeader = followingDest
	}
	if snap.Hero != nil {
		belowHeader = focusDestination{Focus: liveHeroPlayFocus, Region: liveHeroRegion}
	}
	headerEdges := focusEdges{}
	if belowHeader.Region != "" {
		headerEdges[app.Down] = belowHeader
	}
	p.region(liveHeaderRegion, headerEdges)

	if snap.Hero != nil {
		heroEdges := focusEdges{app.Up: {Focus: tab, Region: liveHeaderRegion}}
		if roomsDest.Region != "" {
			heroEdges[app.Down] = roomsDest
		} else if followingDest.Region != "" {
			heroEdges[app.Down] = followingDest
		}
		p.region(liveHeroRegion, heroEdges)
	}

	if roomsDest.Region != "" {
		aboveRooms := focusDestination{Focus: tab, Region: liveHeaderRegion}
		if snap.Hero != nil {
			aboveRooms = focusDestination{Focus: liveHeroPlayFocus, Region: liveHeroRegion}
		}
		roomsEdges := focusEdges{app.Up: aboveRooms}
		if followingDest.Region != "" {
			roomsEdges[app.Down] = followingDest
		}
		p.region(liveRoomsRegion, roomsEdges)
	}

	if followingDest.Region != "" {
		aboveFollowing := roomsDest
		if aboveFollowing.Region == "" {
			if snap.Hero != nil {
				aboveFollowing = focusDestination{Focus: liveHeroPlayFocus, Region: liveHeroRegion}
			} else {
				aboveFollowing = focusDestination{Focus: tab, Region: liveHeaderRegion}
			}
		}
		p.region(liveFollowingRegion, focusEdges{app.Up: aboveFollowing})
	}
}

func (p *PageContext) liveFeaturedItem(snap app.LiveSnapshot) *provider.MediaItem {
	focus := p.State.Focus
	find := func(ref provider.MediaRef) *provider.MediaItem {
		if ref.ID == "" {
			return nil
		}
		if snap.Hero != nil && snap.Hero.Ref == ref {
			copyItem := *snap.Hero
			return &copyItem
		}
		for i := range snap.Rooms {
			if snap.Rooms[i].Ref == ref {
				copyItem := snap.Rooms[i]
				return &copyItem
			}
		}
		for i := range snap.FollowingRooms {
			if snap.FollowingRooms[i].Ref == ref {
				copyItem := snap.FollowingRooms[i]
				return &copyItem
			}
		}
		return nil
	}
	if strings.HasPrefix(string(focus), "live/rooms/") {
		for i := range snap.Rooms {
			if liveRoomCardFocus("rooms", snap.Rooms[i].Ref) == focus {
				p.host.live.featuredRef = snap.Rooms[i].Ref
				if snap.Hero != nil && snap.Hero.Ref == snap.Rooms[i].Ref {
					copyItem := *snap.Hero
					return &copyItem
				}
				copyItem := snap.Rooms[i]
				return &copyItem
			}
		}
	}
	if strings.HasPrefix(string(focus), "live/following/") {
		for i := range snap.FollowingRooms {
			if liveRoomCardFocus("following", snap.FollowingRooms[i].Ref) == focus {
				p.host.live.featuredRef = snap.FollowingRooms[i].Ref
				if snap.Hero != nil && snap.Hero.Ref == snap.FollowingRooms[i].Ref {
					copyItem := *snap.Hero
					return &copyItem
				}
				copyItem := snap.FollowingRooms[i]
				return &copyItem
			}
		}
	}
	if item := find(p.host.live.featuredRef); item != nil {
		return item
	}
	if snap.Hero != nil {
		copyItem := *snap.Hero
		return &copyItem
	}
	if len(snap.Rooms) != 0 {
		copyItem := snap.Rooms[0]
		return &copyItem
	}
	return nil
}

func (p *PageContext) prepareLiveArtwork(snap app.LiveSnapshot, featured *provider.MediaItem) {
	if p.host.Catalog == nil {
		return
	}
	var backdrops []string
	addBackdrop := func(item *provider.MediaItem) {
		if item == nil {
			return
		}
		if item.BackdropURL != "" {
			backdrops = append(backdrops, item.BackdropURL)
		} else if item.CoverURL != "" {
			backdrops = append(backdrops, item.CoverURL)
		}
	}
	addBackdrop(featured)
	if snap.Hero != nil && (featured == nil || snap.Hero.Ref != featured.Ref) {
		addBackdrop(snap.Hero)
	}
	if len(backdrops) != 0 {
		p.host.Catalog.PrepareBackdrops(backdrops)
	}
	if featured != nil && featured.CoverURL != "" {
		p.host.Catalog.PrepareArtwork([]string{featured.CoverURL})
	}
	if snap.Hero != nil && snap.Hero.CoverURL != "" {
		p.host.Catalog.PrepareArtwork([]string{snap.Hero.CoverURL})
	}
	urls := make([]string, 0, 20)
	viewport := p.Width - 103
	stride := homeShelfCardSize.width + homeShelfGap
	if len(snap.Rooms) != 0 {
		first, end := visibleItemRange(p.host.live.roomsScroll.X, viewport, stride, len(snap.Rooms), 2)
		for i := first; i < end; i++ {
			if u := snap.Rooms[i].CoverURL; u != "" {
				urls = append(urls, u)
			}
		}
	}
	if len(snap.FollowingRooms) != 0 {
		first, end := visibleItemRange(p.host.live.followingScroll.X, viewport, stride, len(snap.FollowingRooms), 2)
		for i := first; i < end; i++ {
			if u := snap.FollowingRooms[i].CoverURL; u != "" {
				urls = append(urls, u)
			}
		}
	}
	p.host.Catalog.PrepareArtwork(urls)
}

func (p *PageContext) liveBackdrop(c *ui.Context, hero provider.MediaItem) {
	canvas := p.Theme.Colors.Surface.Canvas
	var art ui.ImageSource
	backdropLoc := hero.BackdropURL
	if backdropLoc == "" {
		backdropLoc = hero.CoverURL
	}
	if backdropLoc != "" {
		art = p.catalogBitmap(catalogImageKey{location: backdropLoc, backdrop: true})
	}
	if art == nil && hero.CoverURL != "" {
		art = p.catalogCover(hero.CoverURL)
	}
	ui.Box(c.Key("live-backdrop")).Absolute().Top(0).Left(0).Right(0).Height(liveArtHeight).Clip().Children(func() {
		if art != nil {
			ui.Image(c.Key("live-backdrop-img"), art).Fill().Fit(ui.Cover).ObjectPosition(0.6, 0.35).Opacity(0.86)
		}
		ui.Box(c).Absolute().Top(0).Left(0).Fill().LinearGradient(ui.LinearGradient{
			Angle: 90,
			Stops: []ui.GradientStop{
				{Color: paint(canvas), At: 0},
				{Color: withAlpha(canvas, 0xdd), At: 0.32},
				{Color: ui.Transparent, At: 0.78},
			},
		})
		ui.Box(c).Absolute().Top(0).Left(0).Fill().LinearGradient(ui.LinearGradient{
			Angle: 0,
			Stops: []ui.GradientStop{
				{Color: paint(canvas), At: 0},
				{Color: withAlpha(canvas, 0xee), At: 0.14},
				{Color: ui.Transparent, At: 0.75},
			},
		})
		ui.Box(c).Absolute().Top(0).Left(0).Fill().LinearGradient(ui.LinearGradient{
			Angle: 180,
			Stops: []ui.GradientStop{
				{Color: withAlpha(canvas, 0x80), At: 0},
				{Color: ui.Transparent, At: 0.24},
			},
		})
	})
}

func liveAreaWidth(label string) float32 {
	runes := utf8.RuneCountInString(label)
	switch {
	case runes <= 2:
		return 98
	case runes == 3:
		return 123
	case runes == 4:
		return 148
	default:
		return 168
	}
}

func (p *PageContext) liveHeader(c *ui.Context, snap app.LiveSnapshot) {
	colors := p.Theme.Colors
	areas := snap.Areas
	if len(areas) == 0 {
		areas = []provider.LiveArea{{ID: "", Label: p.Assets.Text.Search.KindAll}}
	}
	ui.Box(c.Key("live-header")).FillWidth().Height(liveHeaderHeight).Children(func() {
		ui.ScrollHorizontal(c.Key("live-area-tabs")).Absolute().Top(45).Left(78).Right(48).Height(61).
			TrackScroll(&p.host.live.headerScroll).Children(func() {
			ui.Row(c).Height(61).Gap(18).Padding(0, 24, 0, 0).AlignItems(ui.Center).Children(func() {
				for _, area := range areas {
					id := liveAreaFocus(area.ID)
					selected := area.ID == snap.Area
					focused := p.paintFocus() == id
					background, ink := ui.Transparent, paint(colors.Text.Muted)
					borderW, borderColor := float32(0), ui.Transparent
					if selected {
						background, ink = withAlpha(colors.Text.Primary, 0x12), paint(colors.Text.Primary)
					}
					if focused {
						background, ink = p.uiColors().tabSubtleFocusFill, paint(colors.Text.Primary)
						if !p.Theme.Dark {
							borderW, borderColor = 1, p.uiColors().focusBorder
						}
					}
					w := liveAreaWidth(area.Label)
					val := area.ID
					btn := ui.Button(c.Key(string(id)), "").Label(area.Label).Size(w, 61).Shrink(0).Padding(0, 0).
						Radius(30).Border(borderW, borderColor).Background(background).TextColor(ink).Center().Role(ui.RoleTab).AccessSelected(selected).
						OnClick(func() { p.dispatch(func() { p.host.selectLiveArea(val) }) })
					p.animateFocus(btn, background, ink)
					p.bind(btn, id, liveHeaderRegion)
					btn.Children(func() {
						ui.Text(c, area.Label).FontSize(24).LetterSpacing(1).SingleLine()
						if selected || focused {
							ui.Box(c).Absolute().Bottom(2).Left((w-15)/2).Size(15, 3).
								Radius(2).Background(paint(colors.Accent.Primary))
						}
					})
				}
			})
		})
	})
}

func (p *PageContext) liveHero(c *ui.Context, hero provider.MediaItem) {
	colors, text, d := p.Theme.Colors, p.Assets.Text, p.uiColors()
	ui.Box(c.Key("live-hero")).FillWidth().Height(liveHeroHeight).Children(func() {
		ui.Column(c).Absolute().Top(46).Left(103).Width(920).Children(func() {
			ui.Row(c).Height(22).Gap(15).AlignItems(ui.Center).Children(func() {
				ui.Box(c).Size(24, 1).Background(paint(colors.Accent.Soft))
				ui.Text(c, text.Live.Eyebrow).FontSize(16).FontWeight(500).LetterSpacing(4).TextColor(paint(colors.Text.Secondary))
			})
			ui.Text(c, hero.Title).Margin(14, 0, 14, 0).FontSize(72).FontWeight(650).LetterSpacing(2).
				LineHeight(1.18).SingleLine()
			ui.Row(c).Height(32).Gap(16).AlignItems(ui.Center).Children(func() {
				ui.Row(c).Padding(5, 10).Gap(6).AlignItems(ui.Center).Radius(5).
					Background(paint(colors.Accent.Primary)).Children(func() {
					ui.Box(c).Size(6, 6).Radius(3).Background(paint(colors.Accent.Foreground))
					ui.Text(c, text.Live.LiveNow).FontSize(13).FontWeight(650).LetterSpacing(0.8).
						TextColor(paint(colors.Accent.Foreground))
				})
				if hero.Category != "" {
					ui.Text(c, hero.Category).FontSize(19).TextColor(d.secondaryText)
				}
				if hero.Views != nil && *hero.Views > 0 {
					if hero.Category != "" {
						ui.Box(c).Size(3, 3).Radius(1.5).Background(d.secondaryText)
					}
					ui.Text(c, catalogViews(*hero.Views, text.Home)+" "+text.Live.Viewers).FontSize(19).TextColor(d.secondaryText)
				}
			})
			subtitle := hero.Subtitle
			if subtitle == "" && hero.Creator != nil {
				subtitle = hero.Creator.Name
			}
			if subtitle != "" {
				ui.Text(c, subtitle).Margin(16, 0, 24, 0).MaxWidth(690).
					FontSize(21).LineHeight(1.7).MaxLines(2).TextColor(paint(colors.Text.Secondary))
			} else {
				ui.Box(c).Height(24)
			}
			ui.Row(c).Gap(16).AlignItems(ui.Center).Children(func() {
				primaryStyle := p.playButtonAppearance()
				primaryStyle.padding = 27
				secondaryStyle := buttonAppearance{
					height: 62, padding: 27, radius: 12, fontSize: 22, fontWeight: 550, iconSize: 22, iconGap: 13,
					background: d.secondaryFill, foreground: d.secondaryText, border: d.secondaryBorder,
				}
				p.styledButton(c, text.Live.EnterRoom, "play", liveHeroPlayFocus, liveHeroRegion, func() {
					p.openLivePlayback(hero, false)
				}, primaryStyle)
				p.styledButton(c, text.Home.Details, "info", liveHeroInfoFocus, liveHeroRegion, func() {
					p.openLivePlayback(hero, true)
				}, secondaryStyle)
			})
		})
	})
}

func (p *PageContext) liveRoomShelf(c *ui.Context, key, title, note string, items []provider.MediaItem, scroll *ui.ScrollState, seeAllFocus app.FocusID, region string, canLoadMore bool, loadErr provider.ErrorCode, loadMore, onSeeAll func()) {
	colors, text := p.Theme.Colors, p.Assets.Text
	ui.Column(c.Key("live-shelf/"+key)).FillWidth().Padding(25, 0, 34, 103).Children(func() {
		ui.Row(c).FillWidth().Padding(0, 79, 0, 0).Height(liveShelfHeaderH).Gap(18).AlignItems(ui.Center).Children(func() {
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
		ui.Box(c).Height(liveShelfHeaderGap)
		cardWidth := homeShelfCardSize.width
		viewport := p.Width - 103
		stride := cardWidth + homeShelfGap
		first, end := visibleItemRange(scroll.X-4, viewport, stride, len(items), 2)
		focused := -1
		for i, item := range items {
			if p.State.Focus == liveRoomCardFocus(key, item.Ref) {
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
		ui.ScrollHorizontal(c.Key("live-strip/"+key)).FillWidth().Height(homeCardHeight+liveShelfCardPad).
			TrackScroll(scroll).Children(func() {
			ui.Box(c).Width(max(totalWidth, viewport)).Height(homeCardHeight + liveShelfCardPad).Children(func() {
				draw := func(i int) {
					item := items[i]
					id := liveRoomCardFocus(key, item.Ref)
					ui.Box(c.Key("live-slot/"+string(id))).Absolute().
						Top(liveShelfCardPad).Left(4+float32(i)*stride).
						Size(cardWidth, homeCardHeight).Children(func() {
						p.liveRoomCard(c, id, region, item, homeShelfCardSize)
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
		if loadErr != "" {
			ui.Text(c, text.Live.MoreFailed).Margin(8, 0, 0, 4).FontSize(18).TextColor(paint(colors.Status.Error.Foreground))
		}
	})
}

func (p *PageContext) liveRoomCard(c *ui.Context, id app.FocusID, region string, item provider.MediaItem, size catalogCardSize) {
	colors, d, text := p.Theme.Colors, p.uiColors(), p.Assets.Text
	card := ui.Button(c.Key(string(id)), "").Size(size.width, size.height).Shrink(0).Padding(0, 0).
		Border(0, ui.Transparent).Background(ui.Transparent).Label(item.Title).
		OnClick(func() { p.dispatch(func() { p.openLivePlayback(item, false) }) })
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
				if item.Views != nil && *item.Views > 0 {
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

func (p *PageContext) liveEmptyArea(c *ui.Context) {
	colors, text := p.Theme.Colors, p.Assets.Text.Live
	ui.Column(c).FillWidth().Padding(80, 83, 40, 103).AlignItems(ui.Center).Children(func() {
		ui.Icon(c, p.Assets.Icon("live")).Size(65, 65).TextColor(paint(colors.Accent.Soft))
		ui.Text(c, text.EmptyArea).Margin(28, 0, 14, 0).FontSize(34).FontWeight(450).LineHeight(1.39)
		ui.Text(c, text.EmptyAreaNote).FontSize(21).LineHeight(1.5).TextColor(paint(colors.Text.Muted))
	})
}

func (p *PageContext) liveStateBlock(c *ui.Context, icon, title, note, action, actionIcon string, focus app.FocusID, run func()) {
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
		if action != "" && run != nil {
			p.styledButton(c, action, actionIcon, focus, liveStatusRegion, run, style).AlignSelf(ui.Center)
		}
	})
}

func (p *PageContext) openLiveAll(kind liveAllKind) {
	if p.State.Push(app.Route{Page: app.LiveAll}) {
		p.host.live.allSection = kind
		if catalog := p.host.Live; catalog != nil {
			snap := catalog.Snapshot()
			items := snap.Rooms
			if kind == liveAllFollowing {
				items = snap.FollowingRooms
			}
			if len(items) != 0 {
				p.State.Focus = liveAllItemFocus(items[0].Ref)
				p.host.reveal = true
			}
		}
	}
}

type liveAllPage struct{}

func (liveAllPage) Render(c *ui.Context, p *PageContext) {
	catalog := p.host.Live
	text := p.Assets.Text
	kind := p.host.live.allSection
	title := text.Live.RecommendedTitle
	if kind == liveAllFollowing {
		title = text.Live.FollowingTitle
	}
	var items []provider.MediaItem
	var canLoadMore, loading bool
	var loadErr string
	var loadMore func()
	if catalog != nil {
		snap := catalog.Snapshot()
		if kind == liveAllFollowing {
			items = snap.FollowingRooms
			canLoadMore = snap.CanLoadMoreFollowing()
			loading = snap.FollowingLoading
			loadErr = string(snap.FollowingError)
			loadMore = catalog.LoadMoreFollowing
		} else {
			items = snap.Rooms
			canLoadMore = snap.CanLoadMoreRooms()
			loading = snap.RoomsLoading
			loadErr = string(snap.RoomsError)
			loadMore = catalog.LoadMoreRooms
		}
	}
	p.renderShelfCollection(c, shelfCollectionSpec{
		key:         "live-all/" + string(kind),
		title:       title,
		countFormat: text.Activity.FolderCount,
		count:       len(items),
		grid:        shelfVideoGrid,
		backFocus:   liveAllBackFocus,
		retryFocus:  liveAllRetryFocus,
		itemPrefix:  "live-all/item/",
		itemFocus: func(index int) app.FocusID {
			return liveAllItemFocus(items[index].Ref)
		},
		artworkURLs: func(index int) []string {
			return []string{items[index].CoverURL}
		},
		renderCard: func(c *ui.Context, index int, id app.FocusID, region string, cardWidth float32) {
			size := catalogCardSize{
				width:  cardWidth,
				height: shelfVideoGrid.cardH,
				cover:  shelfVideoGrid.coverH,
				title:  21,
			}
			p.liveRoomCard(c, id, region, items[index], size)
		},
		emptyIcon:   "live",
		emptyNote:   text.Live.EmptyArea,
		canLoadMore: canLoadMore,
		loading:     loading,
		loadErr:     loadErr,
		moreFailed:  text.Live.MoreFailed,
		loadMore:    loadMore,
		retryMore:   loadMore,
	})
}
