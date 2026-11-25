package tvui

import (
	"fmt"
	"log"
	"maps"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/provider"
)

const (
	homeRefresh           app.FocusID = "home/refresh"
	homeRetry             app.FocusID = "home/retry"
	homeTabRetry          app.FocusID = "home/tab-retry"
	homeEnd               app.FocusID = "home/end"
	homeHeader                        = "home/header"
	homeFeedRefresh                   = "home/feed-refresh"
	homeHero                          = "home/hero"
	homePager                         = "home/pager"
	homeFilters                       = "home/filters"
	homeCards                         = "home/cards"
	homeHeaderHeight                  = 102
	homeHeroHeight                    = 598
	homeArtHeight                     = 875
	homeShelfTop                      = 18
	homeShelfHeading                  = 71
	homeShelfGap                      = 27
	homeRefreshWidth                  = 102
	homeRefreshHeight                 = 40
	homeRefreshRight                  = 79
	homeFeedHeadingHeight             = 45
	homeCoverHeight                   = 184
	homeShelfCoverWidth               = 329
	homeCardHeight                    = 260
	homeShelfHeight                   = homeShelfTop + homeShelfHeading + homeCardHeight + 33
	homeDiscoveryHeight               = 360
	homeColumns                       = 5
	homeFeatureInterval               = 8 * time.Second
	maxCatalogBitmaps                 = 128
)

var homeTabs = [...]string{"", "popular", "ranking", "regions"}
var homeHistory = provider.AccountQuery{Section: provider.AccountHistory}
var homeReturnScroll = ui.Animation{Duration: 550 * time.Millisecond, Ease: ui.EaseInOut}

type catalogPage struct{}

type catalogImageKey struct {
	location string
	backdrop bool
}

type catalogImage struct {
	art      *app.CatalogArtwork
	source   *ui.Bitmap
	lastUsed uint64
}

type catalogHome struct {
	entry        string
	tab          string
	filter       string
	filters      []provider.CatalogFilter
	feedRevision uint64
	layout       catalogLayout
	featured     []provider.MediaItem
	selected     int
	featureStep  float32
	featureInfo  catalogFeatureInfo
	heroPreview  catalogHeroPreview
	lastCard     app.FocusID
	rotateAt     time.Time
	hold         catalogHold
	images       map[catalogImageKey]catalogImage
	imageClock   uint64
	shelfScrolls map[string]*ui.ScrollState
	rowScrolls   map[string]*ui.ScrollState
	action       catalogAction
}

type catalogShelf struct {
	id, title, note string
	items           []provider.MediaItem
}

func syncHome(v *View) {
	if v.Activity.HistorySync() {
		v.Activity.Ensure(homeHistory)
	}
}

func (v *View) backFromHome() bool {
	scroll := v.activeScroll
	if v.State.Area != app.ContentArea || scroll == nil {
		return false
	}
	tab := homeTabID(v.home.tab)
	if target := v.focus.item(tab); target == nil || target.group != homeHeader {
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

func (catalogPage) Render(c *ui.Context, p *PageContext) {
	if p.Location.Page != app.Home {
		p.Outlet(c)
		return
	}
	catalog := p.host.Catalog
	entry, _ := p.State.Current()
	if !entry.Capabilities.Has(provider.BrowseCatalog) {
		p.renderUnavailableContent(c)
		return
	}
	home := &p.host.home
	current := catalog.Snapshot()
	if current.Phase == app.CatalogIdle {
		current.Phase = app.CatalogLoading
	}
	if current.Page.Filters != nil {
		home.filters = current.Page.Filters
	}
	if home.feedRevision != current.FeedRevision {
		home.feedRevision, home.featured = current.FeedRevision, nil
		home.rowScrolls = nil
		home.featureInfo.reset()
		p.host.stopHomePreview()
		home.heroPreview.reset()
		home.rotateAt = time.Time{}
	}
	picks := firstCatalogItems(current.Page.Collections)
	var shelves []catalogShelf
	top := float32(homeHeaderHeight + homeDiscoveryHeight)
	grid := discoveryGridMetrics
	if home.tab == "" {
		if !(current.Phase == app.CatalogFailed && current.Error == provider.NeedsSignIn) {
			shelves = p.homeShelves(current.Page.Shelves, picks)
		}
		top = homeHeaderHeight + homeHeroHeight + float32(len(shelves))*homeShelfHeight
		grid = homeGridMetrics(p.Width)
		if grid.columns != home.layout.grid.columns {
			home.rowScrolls = nil
		}
	} else if home.tab == "ranking" {
		grid = rankingGridMetrics
	}
	home.layout.update(current, top, grid)
	p.restoreCatalogFocus(current, shelves)
	prefetch := grid.stride()
	if home.hold.key == ui.KeyUnknown {
		p.selectCatalogFeature(c, current, shelves, picks)
		p.syncCatalogFeatureInfo(c, current)
		p.syncCatalogHeroPreview(c, current)
		p.host.SyncTopShelf()
	} else {
		// Keep the current artwork while the user is moving through rows.
		home.featureInfo.stop()
		p.host.stopHomePreview()
		prefetch *= 4
	}

	scroll := p.scroll().Y
	_, onCard := home.layout.byFocus[p.State.Focus]
	nearEnd := (scroll > 0 || onCard) && scroll+p.Height >= home.layout.end-prefetch
	if p.active && current.CanLoadMore() && (nearEnd || len(home.layout.sections) == 0) {
		catalog.LoadMore()
		current = catalog.Snapshot()
	}
	loadingTail := current.Appending && current.Phase == app.CatalogLoading
	viewport := p.catalogViewport(loadingTail)
	p.prepareCatalogArtwork(viewport, shelves, scroll)
	emptyFeed := home.tab == "" && len(home.layout.sections) == 0 && !(current.Phase == app.CatalogFailed && current.Error == provider.NeedsSignIn)
	p.catalogRegions(shelves, emptyFeed)

	ui.Box(c.Key("home-feed")).FillWidth().Lang(p.langTag()).Font(p.uiFont()).Children(func() {
		if home.tab == "" && len(home.featured) != 0 && scroll < homeArtHeight {
			p.catalogBackdrop(c, home.featured[home.selected])
		}
		ui.Column(c.Key("home-feed-content")).FillWidth().ZIndex(1).Children(func() {
			p.catalogHeader(c)
			ui.Column(c.Key("home-tab/" + home.tab)).FillWidth().Transition(menuTransition).Children(func() {
				if home.tab == "" {
					p.catalogHero(c, current)
					for idx, shelf := range shelves {
						p.catalogShelf(c, shelf, idx, scroll)
					}
				} else {
					p.catalogDiscovery(c, current)
				}
				if emptyFeed {
					ui.Box(c.Key("catalog-empty-feed")).FillWidth().Height(homeShelfTop + homeShelfHeading).Children(func() {
						p.catalogGridHeading(c, p.Assets.Text.Home.Recommendations, true, current)
					})
				}
				for idx, section := range home.layout.sections {
					refresh := home.tab == "" && idx == 0
					p.catalogGrid(c, section, viewport[idx], idx == len(home.layout.sections)-1, refresh, current)
				}
				p.catalogFooter(c, current)
			})
		})
	})
}

func firstCatalogGroup(groups []provider.Collection) (provider.Collection, bool) {
	for _, group := range groups {
		if len(group.Items) > 0 {
			return group, true
		}
	}
	return provider.Collection{}, false
}

func firstCatalogItems(groups []provider.Collection) []provider.MediaItem {
	group, _ := firstCatalogGroup(groups)
	return group.Items
}

func homeTabID(tab string) app.FocusID {
	if tab == "" {
		return app.ContentAction
	}
	return app.FocusID("home/tab/" + tab)
}

func homeFilterID(filter string) app.FocusID {
	return app.FocusID("home/filter/" + filter)
}

func homeFeatureID(index int) app.FocusID {
	return app.FocusID("home/feature/" + strconv.Itoa(index))
}

func catalogCardFocus(collection string, ref provider.MediaRef) app.FocusID {
	return app.FocusID("home/card/" + url.PathEscape(collection) + "/" + string(ref.Kind) + "/" + url.PathEscape(ref.ID))
}

func (p *PageContext) homeShelves(sourceShelves []provider.Collection, picks []provider.MediaItem) []catalogShelf {
	text := p.Assets.Text.Home
	var history []provider.MediaItem
	if activity := p.host.Activity; activity != nil && activity.HistorySync() {
		for _, row := range activity.Snapshot(homeHistory).Page.Rows {
			if item := row.Item; item != nil && item.ProgressSeconds != nil && *item.ProgressSeconds > 0 &&
				(item.DurationSeconds == nil || *item.ProgressSeconds < *item.DurationSeconds) {
				history = append(history, *item)
			}
		}
	}
	followingTitle := p.sourceCreatorGroupTitle()
	var followingItems []provider.MediaItem
	if group, ok := firstCatalogGroup(sourceShelves); ok {
		followingItems = group.Items
		if title := strings.TrimSpace(group.Title); title != "" {
			followingTitle = title
		}
	}
	picksTitle, picksNote := p.sourceHomePicks()
	return []catalogShelf{
		{id: "continue", title: p.sourceHomeContinueTitle(), note: text.ContinueNote, items: history},
		{id: "picks", title: picksTitle, note: picksNote, items: picks},
		{id: "following", title: followingTitle, note: text.FollowingNote, items: followingItems},
	}
}

func (p *PageContext) catalogRegions(shelves []catalogShelf, emptyFeed bool) {
	home := &p.host.home
	if home.tab != "" {
		next := focusDestination{Region: homeCards}
		previous := focusDestination{Focus: homeTabID(home.tab)}
		if len(home.filters) != 0 {
			filter := home.filter
			if filter == "" {
				filter = home.filters[0].ID
			}
			next = focusDestination{Focus: homeFilterID(filter), Region: homeFilters}
			previous = next
		}
		p.region(homeHeader, focusEdges{app.Down: next})
		p.region(homeFilters, focusEdges{app.Up: {Focus: homeTabID(home.tab)}, app.Down: {Region: homeCards}})
		p.region(homeCards, focusEdges{app.Up: previous})
		return
	}
	shelfDest := func(s catalogShelf) focusDestination {
		d := focusDestination{Region: s.id}
		if len(s.items) > 0 {
			d.Focus = catalogCardFocus(s.id, s.items[0].Ref)
		}
		return d
	}
	var firstShelf focusDestination
	if len(shelves) > 0 {
		firstShelf = shelfDest(shelves[0])
	}
	p.region(homeHeader, focusEdges{app.Down: {Focus: homePlayAction, Region: homeHero}})
	p.region(homeHero, focusEdges{
		app.Up: {Focus: app.ContentAction}, app.Down: firstShelf, app.Right: {Region: homePager},
	})
	p.region(homePager, focusEdges{
		app.Up: {Focus: app.ContentAction}, app.Down: firstShelf, app.Left: {Focus: homeLaterAction, Region: homeHero},
	})
	previous := focusDestination{Focus: homePlayAction, Region: homeHero}
	for idx, shelf := range shelves {
		next := focusDestination{Region: homeCards}
		if idx+1 < len(shelves) {
			next = shelfDest(shelves[idx+1])
		} else if emptyFeed {
			next = focusDestination{Region: homeFeedRefresh}
		}
		p.region(shelf.id, focusEdges{app.Up: previous, app.Down: next})
		previous = shelfDest(shelf)
	}
	p.region(homeCards, focusEdges{app.Up: previous})
	refresh := focusEdges{app.Up: previous}
	if len(home.layout.sections) != 0 {
		cards := home.layout.sections[0].cards
		refresh[app.Down] = focusDestination{Focus: cards[min(len(cards), home.layout.grid.columns)-1]}
	}
	p.region(homeFeedRefresh, refresh)
}

func (p *PageContext) restoreCatalogFocus(current app.CatalogSnapshot, shelves []catalogShelf) {
	if !p.active {
		return
	}
	home := &p.host.home
	focus := p.State.Focus
	switch {
	case focus == homeEnd && !p.catalogHasEndRefresh(current), focus == homeRetry && !catalogAppendFailed(current):
		if _, exists := home.layout.byFocus[home.lastCard]; exists {
			p.setHomeFocus(home.lastCard, false)
		} else {
			p.setHomeFocus(homeTabID(home.tab), true)
		}
	case strings.HasPrefix(string(focus), "home/card/"):
		if _, exists := home.layout.byFocus[focus]; exists {
			home.lastCard = focus
			return
		}
		for _, shelf := range shelves {
			for _, item := range shelf.items[:min(homeShelfLimit, len(shelf.items))] {
				if catalogCardFocus(shelf.id, item.Ref) == focus {
					return
				}
			}
		}
		p.setHomeFocus(homeTabID(home.tab), true)
	case strings.HasPrefix(string(focus), "home/feature/") && home.tab != "":
		p.setHomeFocus(homeTabID(home.tab), true)
	}
}

func (p *PageContext) setHomeFocus(focus app.FocusID, top bool) {
	p.host.setCatalogFocus(p.scroll(), focus, top)
}

func (v *View) setCatalogFocus(scroll *ui.ScrollState, focus app.FocusID, top bool) {
	position := app.Position{Focus: focus}
	if scroll != nil {
		if top {
			scroll.X, scroll.Y = 0, 0
		}
		position.X, position.Y = scroll.X, scroll.Y
	}
	v.State.RecordPosition(position)
	v.reveal = true
}

func (p *PageContext) selectCatalogFeature(c *ui.Context, current app.CatalogSnapshot, shelves []catalogShelf, picks []provider.MediaItem) {
	home := &p.host.home
	if home.tab != "" || len(picks) == 0 {
		home.featured, home.selected = nil, 0
		home.featureInfo.reset()
		home.rotateAt = time.Time{}
		return
	}
	selectGroup := func(items []provider.MediaItem, index int) {
		start := min(index/3*3, max(0, len(items)-3))
		home.featured = items[start:min(start+3, len(items))]
		for ref := range home.featureInfo.loaded {
			if !slices.ContainsFunc(home.featured, func(item provider.MediaItem) bool { return item.Ref == ref }) {
				delete(home.featureInfo.loaded, ref)
			}
		}
		home.selected = index - start
	}
	if len(home.featured) == 0 {
		selectGroup(picks, 0)
	}
	if !p.active {
		home.rotateAt = time.Time{}
		return
	}
	if p.host.hoverPreviewEnabled() {
		if position, ok := home.layout.byFocus[p.State.Focus]; ok {
			selectGroup(home.layout.sections[position.section].group.Items, position.item)
		} else if slot, ok := strings.CutPrefix(string(p.State.Focus), "home/feature/"); ok {
			if index, err := strconv.Atoi(slot); err == nil && index >= 0 && index < len(home.featured) {
				home.selected = index
			}
		} else {
			for _, shelf := range shelves {
				for idx, item := range shelf.items[:min(homeShelfLimit, len(shelf.items))] {
					if p.State.Focus == catalogCardFocus(shelf.id, item.Ref) {
						selectGroup(shelf.items, idx)
					}
				}
			}
		}
	} else if slot, ok := strings.CutPrefix(string(p.State.Focus), "home/feature/"); ok {
		if index, err := strconv.Atoi(slot); err == nil && index >= 0 && index < len(home.featured) {
			home.selected = index
		}
	}
	now := c.Now()
	previewActive := p.host.Playback != nil && p.host.Playback.InlinePreviewState().Active
	if catalogSelectionPinned(p.State.Focus) || previewActive || p.scroll().Y > 8 || len(home.featured) < 2 ||
		current.Phase != app.CatalogReady || p.host.reduceMotionEnabled(c) {
		home.rotateAt = time.Time{}
		return
	}
	if home.rotateAt.IsZero() {
		home.rotateAt = now.Add(homeFeatureInterval)
	} else if !now.Before(home.rotateAt) {
		home.featureStep = 1
		home.selected = (home.selected + 1) % len(home.featured)
		home.rotateAt = now.Add(homeFeatureInterval)
	}
	c.After(home.rotateAt.Sub(now))
}

func catalogSelectionPinned(focus app.FocusID) bool {
	key := string(focus)
	return strings.HasPrefix(key, "home/card/") || strings.HasPrefix(key, "home/feature/")
}

func (p *PageContext) catalogHeader(c *ui.Context) {
	home, text, colors, d := &p.host.home, p.Assets.Text, p.Theme.Colors, p.uiColors()
	titles := [...]string{text.Home.ForYou, text.Home.Popular, text.Home.Ranking, text.Home.Regions}
	widths := [...]float32{148, 148, 123, 148}
	ui.Box(c.Key("home-navigation")).FillWidth().Height(homeHeaderHeight).Children(func() {
		ui.Row(c).Absolute().Top(45).Left(78).Height(61).Gap(18).AlignItems(ui.Center).Children(func() {
			for idx, tab := range homeTabs {
				id := homeTabID(tab)
				background, ink := ui.Transparent, paint(colors.Text.Muted)
				borderW, borderColor := float32(0), ui.Transparent
				selected, focused := tab == home.tab, p.State.Focus == id
				if selected {
					background, ink = withAlpha(colors.Text.Primary, 0x12), paint(colors.Text.Primary)
				}
				if focused {
					background, ink = d.tabSubtleFocusFill, paint(colors.Text.Primary)
					if !p.Theme.Dark {
						borderW, borderColor = 1, d.focusBorder
					}
				}
				button := ui.Button(c.Key(string(id)), "").Label(titles[idx]).Size(widths[idx], 61).Padding(0, 0).
					Radius(30).Border(borderW, borderColor).Background(background).TextColor(ink).Center().Role(ui.RoleTab).AccessSelected(selected).
					OnClick(func() { p.dispatch(func() { p.host.showHomeTab(tab, "") }) })
				p.animateFocus(button, background, ink)
				p.bind(button, id, homeHeader)
				button.Children(func() {
					ui.Text(c, titles[idx]).FontSize(24).LetterSpacing(1)
					p.tabUnderline(c, selected, focused, 0)
				})
			}
		})
	})
}

func (p *PageContext) catalogGridHeading(c *ui.Context, title string, refresh bool, current app.CatalogSnapshot) {
	ui.Row(c.Key("catalog-grid-heading")).Absolute().Top(homeShelfTop).Left(103).Right(homeRefreshRight).
		Height(homeFeedHeadingHeight).Gap(16).AlignItems(ui.Center).Children(func() {
		ui.Text(c, title).Grow(1).MinWidth(0).SingleLine().FontSize(32).FontWeight(600).LetterSpacing(1.5)
		if refresh {
			p.catalogRefreshControl(c, current)
		}
	})
}

func (p *PageContext) catalogRefreshControl(c *ui.Context, current app.CatalogSnapshot) {
	text, colors, catalog := p.Assets.Text, p.Theme.Colors, p.host.Catalog
	label, icon, action := text.Home.Refresh, "refresh", func() { p.refreshHome(homeRefresh) }
	if !current.Appending {
		switch current.Phase {
		case app.CatalogLoading:
			label, icon, action = text.LoadingTitle, "", func() {}
		case app.CatalogFailed, app.CatalogCanceled:
			label, action = text.Retry, catalog.Retry
		}
	}
	ink := paint(colors.Text.Secondary)
	if p.State.Focus == homeRefresh {
		ink = paint(colors.Accent.Soft)
	}
	if current.Phase == app.CatalogFailed && !current.Appending {
		p.catalogError(c, text.Home.RefreshFailed, current.Error)
	}
	button := ui.Button(c.Key(string(homeRefresh)), "").Label(label).Size(homeRefreshWidth, homeRefreshHeight).Shrink(0).Padding(0, 12).
		Border(0, ui.Transparent).Background(ui.Transparent).TextColor(ink).Center().
		OnClick(func() { p.dispatch(action) })
	p.animateFocus(button, ui.Transparent, ink)
	p.bind(button, homeRefresh, homeFeedRefresh)
	button.Children(func() {
		ui.Row(c).FillWidth().Justify(ui.End).Gap(9).AlignItems(ui.Center).Children(func() {
			if icon != "" {
				ui.Icon(c, p.Assets.Icon(icon)).Size(18, 18)
			}
			ui.Text(c, label).SingleLine().Shrink(0).FontSize(17).FontWeight(400)
		})
	})
}

func (v *View) showHomeTab(tab, filter string) {
	home := &v.home
	if home.tab == tab && home.filter == filter {
		return
	}
	if home.tab != tab {
		home.filters = nil
	}
	v.stopHomePreview()
	home.heroPreview.reset()
	home.tab, home.filter, home.featured = tab, filter, nil
	home.lastCard = ""
	home.rotateAt = time.Time{}
	if filter == "" {
		v.setCatalogFocus(v.activeScroll, homeTabID(tab), true)
	}
	v.Catalog.Show(tab, filter)
}

func (p *PageContext) refreshHome(focus app.FocusID) {
	p.host.home.featured = nil
	p.host.home.featureStep = 1
	p.host.home.rotateAt = time.Time{}
	p.setHomeFocus(focus, focus == homeTabID(""))
	p.host.Catalog.Refresh()
}

func (p *PageContext) catalogDiscovery(c *ui.Context, current app.CatalogSnapshot) {
	home, text := &p.host.home, p.Assets.Text
	colors := p.Theme.Colors
	_, picksNote := p.sourceHomePicks()
	eyebrow, title, note := text.Home.PopularEyebrow, text.Home.Popular, picksNote
	switch home.tab {
	case "ranking":
		eyebrow, title, note = text.Home.RankingEyebrow, text.Home.Ranking, p.sourceHomeRankingNote()
	case "regions":
		eyebrow, title = text.Home.RegionsEyebrow, text.Home.Regions
	}
	ui.Box(c.Key("catalog-discovery")).FillWidth().Height(homeDiscoveryHeight).Children(func() {
		ui.Row(c).Absolute().Top(69).Left(103).Height(22).Gap(15).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(24, 1).Background(paint(colors.Accent.Soft))
			ui.Text(c, eyebrow).FontSize(16).FontWeight(500).LetterSpacing(3).TextColor(paint(colors.Text.Secondary))
		})
		ui.Text(c, title).Absolute().Top(110).Left(103).FontSize(54).FontWeight(550).LetterSpacing(2)
		ui.Text(c, note).Absolute().Top(208).Left(103).FontSize(23).TextColor(paint(colors.Text.Muted))
		ui.ScrollHorizontal(c.Key("catalog-filters")).Absolute().Top(277).Left(103).Right(homeGridRight).
			Height(49).Children(func() {
			ui.Row(c).Gap(15).AlignItems(ui.Center).Children(func() {
				for _, filter := range home.filters {
					p.catalogFilter(c, filter)
				}
			})
		})
	})
	if len(current.Page.Collections) == 0 {
		ui.Column(c.Key("catalog-discovery-status")).FillWidth().Padding(homeShelfTop, 83, 0, 103).Gap(20).Children(func() {
			entry, _ := p.State.Current()
			if current.Phase == app.CatalogFailed && current.Error == provider.NeedsSignIn && entry.Capabilities.Has(provider.SignIn) {
				ui.Text(c, text.Activity.SignInNeeded).FontSize(25).TextColor(paint(colors.Text.Secondary))
				p.button(c, p.signInActionLabel(), homeTabRetry, homeCards, func() {
					p.host.openSignIn()
				})
				return
			}
			p.catalogStatus(c, current)
			if catalogPageFailed(current) {
				p.button(c, text.Retry, homeTabRetry, homeCards, func() {
					p.setHomeFocus(homeTabID(home.tab), true)
					p.host.Catalog.Retry()
				})
			}
		})
	}
}

func (p *PageContext) catalogFilter(c *ui.Context, filter provider.CatalogFilter) {
	home, colors, d := &p.host.home, p.Theme.Colors, p.uiColors()
	id := homeFilterID(filter.ID)
	selected := filter.ID == home.filter || (home.filter == "" && filter.ID == home.filters[0].ID)
	focused := p.paintFocus() == id
	background, ink, border := d.subtleFill, paint(colors.Text.Secondary), paint(colors.Border.Subtle)
	if selected {
		background, ink, border = paint(colors.Focus.Background), paint(colors.Focus.Foreground), ui.Transparent
	}
	if focused {
		background, ink, border = paint(colors.Focus.Background), paint(colors.Focus.Foreground), ui.Transparent
	}
	button := ui.Button(c.Key(string(id)), filter.Title).Label(filter.Title).Role(ui.RoleTab).AccessSelected(selected).
		Height(49).MinWidth(90).Padding(0, 25).Radius(25).FontSize(19).Center().Shrink(0).
		Background(background).TextColor(ink).Border(1, border).
		OnClick(func() { p.dispatch(func() { p.host.showHomeTab(home.tab, filter.ID) }) })
	p.animateFocus(button, background, ink)
	p.bind(button, id, homeFilters)
}

func (p *PageContext) catalogStatus(c *ui.Context, current app.CatalogSnapshot) {
	text := p.Assets.Text
	title := text.EmptyTitle
	switch current.Phase {
	case app.CatalogLoading:
		title = text.LoadingTitle
	case app.CatalogFailed:
		title = text.FailedTitle
	case app.CatalogCanceled:
		title = text.CanceledTitle
	}
	ui.Text(c, title).FontSize(25).TextColor(paint(p.Theme.Colors.Text.Secondary))
}

func (p *PageContext) catalogError(c *ui.Context, label string, code provider.ErrorCode) {
	if code == provider.RateLimited {
		label = p.Assets.Text.Home.RateLimited
	}
	ui.Text(c, label).FontSize(20).TextColor(paint(p.Theme.Colors.Status.Error.Foreground))
}

func catalogAppendFailed(current app.CatalogSnapshot) bool {
	return current.Appending && (current.Phase == app.CatalogFailed || current.Phase == app.CatalogCanceled)
}

func catalogPageFailed(current app.CatalogSnapshot) bool {
	return !current.Appending && (current.Phase == app.CatalogFailed || current.Phase == app.CatalogCanceled)
}

func (p *PageContext) catalogHasEndRefresh(current app.CatalogSnapshot) bool {
	home := &p.host.home
	return home.tab == "" && current.Phase == app.CatalogReady && !current.CanLoadMore() && len(home.layout.sections) != 0
}

func (p *PageContext) catalogFooter(c *ui.Context, current app.CatalogSnapshot) {
	text, colors := p.Assets.Text, p.Theme.Colors
	failed := catalogAppendFailed(current)
	if !failed && !p.catalogHasEndRefresh(current) {
		return
	}
	id, label, action := homeEnd, text.Refresh, func() { p.refreshHome(homeTabID("")) }
	if failed {
		id, label, action = homeRetry, text.Retry, p.host.Catalog.Retry
	}
	grid := p.host.home.layout.grid
	ui.Row(c.Key("catalog-tail")).FillWidth().Padding(27, homeGridRight, 0, grid.left).Gap(23).AlignItems(ui.Center).Children(func() {
		background, border := paint(colors.Surface.Panel), ui.Transparent
		if p.State.Focus == id {
			background, border = paint(colors.Surface.Raised), paint(colors.Focus.Ring)
		}
		card := ui.Button(c.Key(string(id)), "").Label(label).Size(grid.width(p.Width), grid.cover).Radius(13).
			Background(background).Border(3, border).Center().OnClick(func() { p.dispatch(action) })
		p.bind(card, id, homeCards)
		card.Children(func() {
			ui.Row(c).Gap(13).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, p.Assets.Icon("refresh")).Size(24, 24)
				ui.Text(c, label).FontSize(24).FontWeight(550)
			})
		})
		if failed && current.Phase == app.CatalogFailed {
			p.catalogError(c, text.Home.MoreFailed, current.Error)
		}
	})
}

func (p *PageContext) prepareCatalogArtwork(rows []catalogWindow, shelves []catalogShelf, scroll float32) {
	home := &p.host.home
	locations := make([]string, 0, 64)
	seen := make(map[string]struct{}, 64)
	includeLocation := func(location string) {
		if _, ok := seen[location]; location != "" && !ok {
			seen[location] = struct{}{}
			locations = append(locations, location)
		}
	}
	includeCovers := func(items []provider.MediaItem) {
		for _, item := range items {
			includeLocation(item.CoverURL)
		}
	}
	includeAvatars := func(items []provider.MediaItem) {
		for _, item := range items {
			if item.Creator != nil {
				includeLocation(item.Creator.AvatarURL)
			}
		}
	}
	var backdrops []string
	heroVisible := home.tab == "" && scroll < homeArtHeight && len(home.featured) != 0
	var visibleSlices [][]provider.MediaItem
	if heroVisible {
		selected := home.featured[home.selected]
		visibleSlices = append(visibleSlices, home.featured[home.selected:home.selected+1])
		if selected.BackdropURL != "" {
			backdrops = append(backdrops, selected.BackdropURL)
		}
		next := home.featured[(home.selected+1)%len(home.featured)].BackdropURL
		if next != "" && next != selected.BackdropURL {
			backdrops = append(backdrops, next)
		}
	}
	for idx, shelf := range shelves {
		top := float32(homeHeaderHeight + homeHeroHeight + idx*homeShelfHeight)
		if top+homeShelfHeight >= scroll && top < scroll+p.Height {
			items := shelf.items[:min(homeShelfLimit, len(shelf.items))]
			first := 0
			for idx, item := range items {
				if catalogCardFocus(shelf.id, item.Ref) == p.State.Focus {
					first = max(0, idx-homeColumns+2)
					break
				}
			}
			visibleSlices = append(visibleSlices, items[first:min(first+homeColumns+1, len(items))])
		}
	}
	for _, section := range home.layout.sections {
		items := section.group.Items
		grid := section.grid
		if grid.podium != 0 && section.top+grid.heading >= scroll && section.top < scroll+p.Height {
			visibleSlices = append(visibleSlices, items[:min(grid.podium, len(items))])
		}
		first, end := section.visibleRows(scroll, p.Height, len(items), 0)
		visibleSlices = append(visibleSlices, items[min(grid.firstItem(first), len(items)):min(grid.firstItem(end), len(items))])
	}
	// Prioritize all visible covers before tiny creator avatars, then overscan covers/avatars.
	for _, batch := range visibleSlices {
		includeCovers(batch)
	}
	for _, batch := range visibleSlices {
		includeAvatars(batch)
	}
	var overscanSlices [][]provider.MediaItem
	if heroVisible {
		overscanSlices = append(overscanSlices, home.featured)
	}
	for idx, window := range rows {
		section := home.layout.sections[idx]
		items, grid := section.group.Items, section.grid
		if window.focused >= 0 {
			start := min(grid.firstItem(max(0, window.focused-1)), len(items))
			end := min(grid.firstItem(window.focused+2), len(items))
			overscanSlices = append(overscanSlices, items[start:end])
		}
		overscanSlices = append(overscanSlices, items[min(grid.firstItem(window.first), len(items)):min(grid.firstItem(window.end), len(items))])
	}
	for idx, shelf := range shelves {
		if p.catalogShelfVisible(idx, scroll) {
			overscanSlices = append(overscanSlices, shelf.items[:min(homeShelfLimit, len(shelf.items))])
		}
	}
	for _, batch := range overscanSlices {
		includeCovers(batch)
	}
	for _, batch := range overscanSlices {
		includeAvatars(batch)
	}
	p.host.Catalog.PrepareArtwork(locations)
	p.host.Catalog.PrepareBackdrops(backdrops)
	p.pruneCatalogBitmaps()
}

func (p *PageContext) pruneCatalogBitmaps() {
	home := &p.host.home
	if home.images == nil {
		return
	}
	maps.DeleteFunc(home.images, func(key catalogImageKey, cached catalogImage) bool {
		return p.catalogArtwork(key) != cached.art
	})
	for len(home.images) > maxCatalogBitmaps {
		var oldestKey catalogImageKey
		var oldestUse uint64
		first := true
		for k, entry := range home.images {
			if first || entry.lastUsed < oldestUse {
				oldestKey, oldestUse, first = k, entry.lastUsed, false
			}
		}
		delete(home.images, oldestKey)
	}
}

func (p *PageContext) catalogCover(location string) *ui.Bitmap {
	return p.catalogBitmap(catalogImageKey{location: location})
}

func (p *PageContext) catalogArtwork(key catalogImageKey) *app.CatalogArtwork {
	if p.host.Catalog == nil || key.location == "" {
		return nil
	}
	if key.backdrop {
		return p.host.Catalog.BackdropArtwork(key.location)
	}
	return p.host.Catalog.Artwork(key.location)
}

func (p *PageContext) catalogBitmap(key catalogImageKey) *ui.Bitmap {
	art := p.catalogArtwork(key)
	if art == nil {
		return nil
	}
	home := &p.host.home
	if home.images == nil {
		home.images = make(map[catalogImageKey]catalogImage)
	}
	home.imageClock++
	cached := home.images[key]
	if cached.art == art {
		cached.lastUsed = home.imageClock
		home.images[key] = cached
		return cached.source
	}
	cached = catalogImage{art: art, source: ui.NewBitmap(art.Image), lastUsed: home.imageClock}
	home.images[key] = cached
	if key.backdrop && p.host.Trace {
		size := art.Image.Bounds().Size()
		log.Printf("catalog backdrop: %dx%d", size.X, size.Y)
	}
	if len(home.images) > maxCatalogBitmaps {
		p.pruneCatalogBitmaps()
	}
	return cached.source
}

func catalogDuration(seconds int64) string {
	if seconds >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", seconds/3600, seconds/60%60, seconds%60)
	}
	return fmt.Sprintf("%02d:%02d", seconds/60, seconds%60)
}

func catalogViews(count int64, text homeLabels) string {
	scale, unit := int64(10000), text.TenThousand
	if count >= 100000000 {
		scale, unit = 100000000, text.HundredMillion
	}
	if count < scale {
		return strconv.FormatInt(count, 10)
	}
	return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(count)/float64(scale)), ".0") + " " + unit
}
