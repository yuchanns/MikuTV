package tvui

import (
	"context"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/provider"
)

type seriesAllKind string

const (
	seriesAllPicks   seriesAllKind = "picks"
	seriesAllTracked seriesAllKind = "tracked"
)

const (
	seriesToolIndexFocus    app.FocusID = "series/tool/index"
	seriesToolCalendarFocus app.FocusID = "series/tool/calendar"
	seriesHeroPlayFocus     app.FocusID = "series/hero/play"
	seriesHeroDetailFocus   app.FocusID = "series/hero/detail"
	seriesHeroTrackFocus    app.FocusID = "series/hero/track"
	seriesPicksAllFocus     app.FocusID = "series/all/picks"
	seriesTrackedAllFocus   app.FocusID = "series/all/tracked"
	seriesRetryFocus        app.FocusID = "series/retry"
	seriesAllBackFocus      app.FocusID = "series-all/back"
	seriesAllRetryFocus     app.FocusID = "series-all/retry"

	seriesHeaderRegion  = "series/header"
	seriesToolsRegion   = "series/tools"
	seriesHeroRegion    = "series/hero"
	seriesPicksRegion   = "series/picks"
	seriesTrackedRegion = "series/tracked"
	seriesStatusRegion  = "series/status"

	seriesHeaderHeight   float32 = 102
	seriesHeroHeight     float32 = 440
	seriesArtHeight      float32 = 780
	seriesPosterWidth    float32 = 245
	seriesPosterCoverH   float32 = 290
	seriesPosterHeight   float32 = 372
	seriesPosterGap      float32 = 27
	seriesPosterStride   float32 = seriesPosterWidth + seriesPosterGap
	seriesShelfHeaderH   float32 = 40
	seriesShelfHeaderGap float32 = 25
)

type seriesViewState struct {
	picksScroll   ui.ScrollState
	trackedScroll ui.ScrollState
	featuredRef   provider.MediaRef
	allSection    seriesAllKind
	filterKey     string
	filterList    ui.ListState
	trackPending  context.CancelFunc
	signInAction  func()
	feedback      videoDetailFeedback
}

func seriesGenreFocus(value string) app.FocusID {
	if value == "" {
		return app.ContentAction
	}
	return app.FocusID("series/genre/" + url.PathEscape(value))
}

func seriesCardFocus(shelf string, ref provider.MediaRef) app.FocusID {
	return app.FocusID("series/" + shelf + "/" + string(ref.Kind) + "/" + url.PathEscape(ref.ID))
}

func seriesAllItemFocus(ref provider.MediaRef) app.FocusID {
	return app.FocusID("series-all/item/" + string(ref.Kind) + "/" + url.PathEscape(ref.ID))
}

func syncSeriesHub(v *View) {
	if v.Series != nil {
		v.Series.EnsureHub()
	}
}

func (v *View) selectSeriesGenre(genre string) {
	if v.Series == nil {
		return
	}
	v.series.picksScroll = ui.ScrollState{}
	v.series.featuredRef = provider.MediaRef{}
	v.setCatalogFocus(v.activeScroll, seriesGenreFocus(genre), true)
	v.Series.SelectGenre(genre)
}

func (v *View) backFromSeries() bool {
	scroll := v.activeScroll
	if v.State.Area != app.ContentArea || scroll == nil {
		return false
	}
	genre := ""
	if v.Series != nil {
		genre = v.Series.Hub().Genre
	}
	tab := seriesGenreFocus(genre)
	if target := v.focus.item(tab); target == nil || target.group != seriesHeaderRegion {
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

type seriesPage struct{}

func (seriesPage) Render(c *ui.Context, p *PageContext) {
	if p.Location.Page != app.Series {
		p.Outlet(c)
		return
	}
	catalog := p.host.Series
	entry, _ := p.State.Current()
	if catalog == nil || !entry.Capabilities.Has(provider.BrowseSeries) {
		ui.Column(c).FillWidth().Padding(148, 83, 0, 103).Gap(44).
			Children(func() { contentPage{}.Render(c, p) })
		return
	}
	snap := catalog.Hub()
	p.restoreSeriesHubFocus(snap)
	featured := p.seriesFeaturedEntry(snap)
	p.prepareSeriesHubArtwork(snap, featured)
	p.seriesHubRegions(snap, featured != nil)

	ui.Box(c.Key("series-page")).FillWidth().Lang("zh-CN").Font("system-ui, PingFang SC").Children(func() {
		if featured != nil && p.scroll().Y < seriesArtHeight {
			p.seriesBackdrop(c, *featured)
		}
		ui.Column(c.Key("series-content")).FillWidth().Padding(0, 0, screenBottomInset, 0).ZIndex(1).Children(func() {
			p.seriesHeader(c, snap)
			switch {
			case snap.Phase == app.CatalogIdle || (snap.Phase == app.CatalogLoading && !snap.Loaded):
				p.seriesStateBlock(c, "series", p.Assets.Text.Series.Loading, "", "", "", "", seriesStatusRegion, nil)
				return
			case snap.Phase == app.CatalogFailed && !snap.Loaded:
				p.seriesStateBlock(c, "series", p.Assets.Text.Series.LoadFailed, p.authError(snap.Error), p.Assets.Text.Retry, "refresh", seriesRetryFocus, seriesStatusRegion, func() {
					catalog.RetryHub()
				})
				return
			case snap.Phase == app.CatalogCanceled && !snap.Loaded:
				p.seriesStateBlock(c, "series", p.Assets.Text.CanceledTitle, "", p.Assets.Text.Retry, "refresh", seriesRetryFocus, seriesStatusRegion, func() {
					catalog.RetryHub()
				})
				return
			}
			if featured != nil {
				p.seriesHero(c, *featured)
			}
			p.seriesPosterShelf(c, "picks", p.Assets.Text.Series.PicksTitle, p.Assets.Text.Series.PicksNote,
				snap.Items, &p.host.series.picksScroll, seriesPicksAllFocus, seriesPicksRegion,
				snap.CanLoadMore(), catalog.LoadMoreHub, func() { p.openSeriesAll(seriesAllPicks) })
			if len(snap.Tracked) != 0 {
				p.seriesPosterShelf(c, "tracked", p.Assets.Text.Series.TrackedTitle, p.Assets.Text.Series.TrackedNote,
					snap.Tracked, &p.host.series.trackedScroll, seriesTrackedAllFocus, seriesTrackedRegion,
					false, nil, func() { p.openSeriesAll(seriesAllTracked) })
			}
		})
	})
}

func (p *PageContext) restoreSeriesHubFocus(snap app.SeriesHubSnapshot) {
	if !p.active {
		return
	}
	focus := p.State.Focus
	tab := seriesGenreFocus(snap.Genre)
	if focus == seriesRetryFocus && snap.Phase != app.CatalogFailed && snap.Phase != app.CatalogCanceled {
		p.State.Focus = tab
		p.host.reveal = true
		return
	}
	if strings.HasPrefix(string(focus), "series/picks/") {
		for _, entry := range snap.Items {
			if seriesCardFocus("picks", entry.Item.Ref) == focus {
				return
			}
		}
		if len(snap.Items) != 0 {
			p.State.Focus = seriesCardFocus("picks", snap.Items[0].Item.Ref)
		} else {
			p.State.Focus = tab
		}
		p.host.reveal = true
		return
	}
	if strings.HasPrefix(string(focus), "series/tracked/") {
		for _, entry := range snap.Tracked {
			if seriesCardFocus("tracked", entry.Item.Ref) == focus {
				return
			}
		}
		p.State.Focus = tab
		p.host.reveal = true
	}
}

func (p *PageContext) seriesHubRegions(snap app.SeriesHubSnapshot, hasHero bool) {
	tab := seriesGenreFocus(snap.Genre)
	hasTools := !snap.Loaded || snap.SupportsIndex || snap.SupportsCalendar
	headerEdges := focusEdges{}
	if hasTools {
		toolTarget := seriesToolIndexFocus
		if snap.Loaded && !snap.SupportsIndex {
			toolTarget = seriesToolCalendarFocus
		}
		headerEdges[app.Right] = focusDestination{Focus: toolTarget, Region: seriesToolsRegion}
	}
	hasStatus := snap.Phase == app.CatalogIdle ||
		(snap.Phase == app.CatalogLoading && !snap.Loaded) ||
		(snap.Phase == app.CatalogFailed && !snap.Loaded) ||
		(snap.Phase == app.CatalogCanceled && !snap.Loaded)
	if hasStatus {
		headerEdges[app.Down] = focusDestination{Region: seriesStatusRegion}
		p.region(seriesHeaderRegion, headerEdges)
		if hasTools {
			p.region(seriesToolsRegion, focusEdges{app.Left: {Focus: tab, Region: seriesHeaderRegion}, app.Down: {Region: seriesStatusRegion}})
		}
		p.region(seriesStatusRegion, focusEdges{app.Up: {Focus: tab, Region: seriesHeaderRegion}})
		return
	}

	visibleSeriesCard := func(shelf string, items []provider.SeriesEntry, scrollX float32) app.FocusID {
		if len(items) == 0 {
			return ""
		}
		first, _ := visibleItemRange(scrollX-4, p.Width-103, seriesPosterStride, len(items), 0)
		if first >= len(items) {
			first = len(items) - 1
		}
		return seriesCardFocus(shelf, items[first].Item.Ref)
	}

	picksDest := focusDestination{Region: seriesPicksRegion}
	if target := visibleSeriesCard("picks", snap.Items, p.host.series.picksScroll.X); target != "" {
		picksDest.Focus = target
	}
	belowHeader := picksDest
	if hasHero {
		belowHeader = focusDestination{Focus: seriesHeroPlayFocus, Region: seriesHeroRegion}
	}
	headerEdges[app.Down] = belowHeader
	p.region(seriesHeaderRegion, headerEdges)
	if hasTools {
		p.region(seriesToolsRegion, focusEdges{
			app.Left: {Focus: tab, Region: seriesHeaderRegion},
			app.Down: belowHeader,
		})
	}
	if hasHero {
		p.region(seriesHeroRegion, focusEdges{
			app.Up:   {Focus: tab, Region: seriesHeaderRegion},
			app.Down: picksDest,
		})
	}
	abovePicks := focusDestination{Focus: tab, Region: seriesHeaderRegion}
	if hasHero {
		abovePicks = focusDestination{Focus: seriesHeroPlayFocus, Region: seriesHeroRegion}
	}
	picksEdges := focusEdges{app.Up: abovePicks}
	if len(snap.Tracked) != 0 {
		trackedDest := focusDestination{Region: seriesTrackedRegion}
		if target := visibleSeriesCard("tracked", snap.Tracked, p.host.series.trackedScroll.X); target != "" {
			trackedDest.Focus = target
		}
		picksEdges[app.Down] = trackedDest
		p.region(seriesTrackedRegion, focusEdges{app.Up: picksDest})
	}
	p.region(seriesPicksRegion, picksEdges)
}

func (p *PageContext) seriesFeaturedEntry(snap app.SeriesHubSnapshot) *provider.SeriesEntry {
	focus := p.State.Focus
	find := func(ref provider.MediaRef) *provider.SeriesEntry {
		if ref.ID == "" {
			return nil
		}
		if snap.Hero != nil && snap.Hero.Item.Ref == ref {
			copyEntry := *snap.Hero
			return &copyEntry
		}
		for i := range snap.Items {
			if snap.Items[i].Item.Ref == ref {
				copyEntry := snap.Items[i]
				return &copyEntry
			}
		}
		for i := range snap.Tracked {
			if snap.Tracked[i].Item.Ref == ref {
				copyEntry := snap.Tracked[i]
				return &copyEntry
			}
		}
		return nil
	}
	if strings.HasPrefix(string(focus), "series/picks/") {
		for i := range snap.Items {
			if seriesCardFocus("picks", snap.Items[i].Item.Ref) == focus {
				p.host.series.featuredRef = snap.Items[i].Item.Ref
				if snap.Hero != nil && snap.Hero.Item.Ref == snap.Items[i].Item.Ref {
					copyEntry := *snap.Hero
					return &copyEntry
				}
				copyEntry := snap.Items[i]
				return &copyEntry
			}
		}
	}
	if strings.HasPrefix(string(focus), "series/tracked/") {
		for i := range snap.Tracked {
			if seriesCardFocus("tracked", snap.Tracked[i].Item.Ref) == focus {
				p.host.series.featuredRef = snap.Tracked[i].Item.Ref
				if snap.Hero != nil && snap.Hero.Item.Ref == snap.Tracked[i].Item.Ref {
					copyEntry := *snap.Hero
					return &copyEntry
				}
				copyEntry := snap.Tracked[i]
				return &copyEntry
			}
		}
	}
	if entry := find(p.host.series.featuredRef); entry != nil {
		return entry
	}
	if snap.Hero != nil {
		copyEntry := *snap.Hero
		return &copyEntry
	}
	if len(snap.Items) != 0 {
		copyEntry := snap.Items[0]
		return &copyEntry
	}
	return nil
}

func (p *PageContext) prepareSeriesHubArtwork(snap app.SeriesHubSnapshot, featured *provider.SeriesEntry) {
	if p.host.Catalog == nil {
		return
	}
	backdrops := make([]string, 0, 6)
	addBackdrop := func(entry *provider.SeriesEntry) {
		if entry == nil {
			return
		}
		if entry.Item.BackdropURL != "" {
			backdrops = append(backdrops, entry.Item.BackdropURL)
		} else if entry.Item.CoverURL != "" {
			backdrops = append(backdrops, entry.Item.CoverURL)
		}
	}
	addBackdrop(featured)
	if snap.Hero != nil && (featured == nil || snap.Hero.Item.Ref != featured.Item.Ref) {
		addBackdrop(snap.Hero)
	}
	addNeighbors := func(items []provider.SeriesEntry) {
		if featured == nil {
			return
		}
		for i := range items {
			if items[i].Item.Ref == featured.Item.Ref {
				if i+1 < len(items) {
					addBackdrop(&items[i+1])
				}
				if i > 0 {
					addBackdrop(&items[i-1])
				}
				return
			}
		}
	}
	addNeighbors(snap.Items)
	addNeighbors(snap.Tracked)
	if len(backdrops) != 0 {
		p.host.Catalog.PrepareBackdrops(backdrops)
	}
	urls := make([]string, 0, 24)
	if featured != nil && featured.Item.CoverURL != "" {
		urls = append(urls, featured.Item.CoverURL)
	}
	if snap.Hero != nil && snap.Hero.Item.CoverURL != "" {
		urls = append(urls, snap.Hero.Item.CoverURL)
	}
	viewport := p.Width - 103
	if len(snap.Items) != 0 {
		first, end := visibleItemRange(p.host.series.picksScroll.X, viewport, seriesPosterStride, len(snap.Items), 2)
		for i := first; i < end; i++ {
			if u := snap.Items[i].Item.CoverURL; u != "" {
				urls = append(urls, u)
			}
		}
	}
	if len(snap.Tracked) != 0 {
		first, end := visibleItemRange(p.host.series.trackedScroll.X, viewport, seriesPosterStride, len(snap.Tracked), 2)
		for i := first; i < end; i++ {
			if u := snap.Tracked[i].Item.CoverURL; u != "" {
				urls = append(urls, u)
			}
		}
	}
	p.host.Catalog.PrepareArtwork(urls)
}

func (p *PageContext) seriesBackdrop(c *ui.Context, hero provider.SeriesEntry) {
	canvas := p.Theme.Colors.Surface.Canvas
	var art ui.ImageSource
	backdropLoc := hero.Item.BackdropURL
	if backdropLoc == "" {
		backdropLoc = hero.Item.CoverURL
	}
	if backdropLoc != "" {
		art = p.catalogBitmap(catalogImageKey{location: backdropLoc, backdrop: true})
	}
	if art == nil && hero.Item.CoverURL != "" {
		art = p.catalogCover(hero.Item.CoverURL)
	}
	ui.Box(c.Key("series-backdrop")).Absolute().Top(0).Left(0).Right(0).Height(seriesArtHeight).Clip().Children(func() {
		if art != nil {
			ui.Image(c.Key("series-backdrop-img"), art).Fill().Fit(ui.Cover).ObjectPosition(0.6, 0.4).Opacity(0.88)
		}
		ui.Box(c).Absolute().Top(0).Left(0).Fill().LinearGradient(ui.LinearGradient{
			Angle: 90,
			Stops: []ui.GradientStop{
				{Color: paint(canvas), At: 0},
				{Color: withAlpha(canvas, 0xdd), At: 0.28},
				{Color: withAlpha(canvas, 0x22), At: 0.70},
			},
		})
		ui.Box(c).Absolute().Top(0).Left(0).Fill().LinearGradient(ui.LinearGradient{
			Angle: 0,
			Stops: []ui.GradientStop{
				{Color: paint(canvas), At: 0},
				{Color: withAlpha(canvas, 0xee), At: 0.12},
				{Color: ui.Transparent, At: 0.65},
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

func seriesGenreWidth(label string) float32 {
	runes := utf8.RuneCountInString(label)
	switch {
	case runes <= 2:
		return 98
	case runes == 3:
		return 123
	default:
		return 148
	}
}

func (p *PageContext) seriesHeader(c *ui.Context, snap app.SeriesHubSnapshot) {
	colors, text := p.Theme.Colors, p.Assets.Text.Series
	genres := snap.Genres
	if len(genres) == 0 {
		genres = []provider.SeriesFilterOption{{Value: "", Label: p.Assets.Text.Search.KindAll}}
	}
	ui.Box(c.Key("series-header")).FillWidth().Height(seriesHeaderHeight).Children(func() {
		ui.Row(c).Absolute().Top(45).Left(78).Height(61).Gap(18).AlignItems(ui.Center).Children(func() {
			d := p.uiColors()
			for _, genre := range genres {
				id := seriesGenreFocus(genre.Value)
				selected := genre.Value == snap.Genre
				focused := p.paintFocus() == id
				background, ink := ui.Transparent, paint(colors.Text.Muted)
				borderW, borderColor := float32(0), ui.Transparent
				if selected {
					background, ink = withAlpha(colors.Text.Primary, 0x12), paint(colors.Text.Primary)
				}
				if focused {
					background, ink = d.tabSubtleFocusFill, paint(colors.Text.Primary)
					if !p.Theme.Dark {
						borderW, borderColor = 1, d.focusBorder
					}
				}
				w := seriesGenreWidth(genre.Label)
				val := genre.Value
				btn := ui.Button(c.Key(string(id)), "").Label(genre.Label).Size(w, 61).Padding(0, 0).
					Radius(30).Border(borderW, borderColor).Background(background).TextColor(ink).Center().Role(ui.RoleTab).AccessSelected(selected).
					OnClick(func() { p.dispatch(func() { p.host.selectSeriesGenre(val) }) })
				p.animateFocus(btn, background, ink)
				p.applyFocusElevation(btn, focused)
				p.bind(btn, id, seriesHeaderRegion)
				btn.Children(func() {
					ui.Text(c, genre.Label).FontSize(24).LetterSpacing(1)
					if selected || focused {
						ui.Box(c).Absolute().Bottom(2).Left((w-15)/2).Size(15, 3).
							Radius(2).Background(paint(colors.Accent.Primary))
					}
				})
			}
		})
		showIndex := !snap.Loaded || snap.SupportsIndex
		showCalendar := !snap.Loaded || snap.SupportsCalendar
		if showIndex || showCalendar {
			ui.Row(c).Absolute().Top(51).Right(69).Height(44).Gap(6).AlignItems(ui.Center).Children(func() {
				if showIndex {
					p.seriesToolButton(c, text.Index, "search", seriesToolIndexFocus, func() {
						p.State.Push(app.Route{Page: app.SeriesIndex})
					})
				}
				if showCalendar {
					p.seriesToolButton(c, text.Calendar, "history", seriesToolCalendarFocus, func() {
						p.State.Push(app.Route{Page: app.SeriesCalendar})
					})
				}
			})
		}
	})
}

func (p *PageContext) seriesToolButton(c *ui.Context, label, icon string, id app.FocusID, action func()) {
	colors := p.Theme.Colors
	focused := p.paintFocus() == id
	bg, fg := ui.Transparent, paint(colors.Accent.Soft)
	if focused {
		bg, fg = paint(colors.Focus.Background), paint(colors.Focus.Foreground)
	}
	btn := ui.Button(c.Key(string(id)), "").Label(label).Height(44).Padding(0, 14).Radius(10).
		Border(0, ui.Transparent).Background(bg).TextColor(fg).Center().
		OnClick(func() { p.dispatch(action) })
	p.animateFocus(btn, bg, fg)
	p.bind(btn, id, seriesToolsRegion)
	btn.Children(func() {
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, p.Assets.Icon(icon)).Size(20, 20)
			ui.Text(c, label).FontSize(19).FontWeight(450).SingleLine()
		})
	})
}

func (p *PageContext) seriesHero(c *ui.Context, hero provider.SeriesEntry) {
	colors, text, d := p.Theme.Colors, p.Assets.Text, p.uiColors()
	eyebrow := hero.Eyebrow
	if eyebrow == "" {
		eyebrow = text.Series.Eyebrow
	}
	ui.Box(c.Key("series-hero")).FillWidth().Height(seriesHeroHeight).Children(func() {
		ui.Column(c).Absolute().Top(46).Left(103).Width(860).Children(func() {
			ui.Row(c).Height(22).Gap(15).AlignItems(ui.Center).Children(func() {
				ui.Box(c).Size(24, 1).Background(paint(colors.Accent.Soft))
				ui.Text(c, eyebrow).FontSize(16).FontWeight(500).LetterSpacing(4).TextColor(paint(colors.Text.Secondary))
			})
			ui.Text(c, hero.Item.Title).Margin(14, 0, 14, 0).FontSize(72).FontWeight(650).LetterSpacing(2).
				LineHeight(1.18).SingleLine()
			ui.Row(c).Height(30).Gap(16).AlignItems(ui.Center).Children(func() {
				badge := hero.Status
				if badge == "" {
					badge = hero.Badge
				}
				if badge != "" {
					ui.Text(c, badge).Padding(3, 8).Radius(4).Border(1, withAlpha(colors.Text.Primary, 0x40)).
						FontSize(13).FontWeight(600).LetterSpacing(1).TextColor(paint(colors.Text.Primary))
				}
				if hero.Item.Category != "" {
					ui.Text(c, hero.Item.Category).FontSize(19).TextColor(d.secondaryText)
				}
				if hero.Score != "" {
					if hero.Item.Category != "" {
						ui.Box(c).Size(3, 3).Radius(1.5).Background(d.secondaryText)
					}
					ui.Text(c, hero.Score+text.Series.RatingSuffix).FontSize(19).TextColor(d.secondaryText)
				}
			})
			if hero.Item.Subtitle != "" {
				ui.Text(c, hero.Item.Subtitle).Margin(16, 0, 24, 0).MaxWidth(690).
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
				p.styledButton(c, text.Home.Play, "play", seriesHeroPlayFocus, seriesHeroRegion, func() {
					if p.canPlay(hero.Item.Ref) {
						p.startDetailPlayback(hero.Item, nil)
					} else {
						p.openVideoDetail(hero.Item)
					}
				}, primaryStyle)
				p.styledButton(c, text.Home.Details, "info", seriesHeroDetailFocus, seriesHeroRegion, func() {
					p.openVideoDetail(hero.Item)
				}, secondaryStyle)
				trackIcon := "plus"
				if hero.Tracked {
					trackIcon = "check"
				}
				p.styledButton(c, p.sourceSeriesTrackLabel(hero.Tracked), trackIcon, seriesHeroTrackFocus, seriesHeroRegion, func() {
					p.toggleSeriesTrack(hero)
				}, secondaryStyle)
			})
		})
	})
}

func (p *PageContext) seriesPosterShelf(c *ui.Context, key, title, note string, items []provider.SeriesEntry, scroll *ui.ScrollState, seeAllFocus app.FocusID, region string, canLoadMore bool, loadMore, onSeeAll func()) {
	colors, text := p.Theme.Colors, p.Assets.Text
	ui.Column(c.Key("series-shelf/"+key)).FillWidth().Padding(25, 0, 34, 103).Children(func() {
		ui.Row(c).FillWidth().Padding(0, 79, 0, 0).Height(seriesShelfHeaderH).Gap(18).AlignItems(ui.Center).Children(func() {
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
		ui.Box(c).Height(seriesShelfHeaderGap)
		if len(items) == 0 {
			ui.Text(c, text.EmptyTitle).Margin(20, 0, 0, 4).FontSize(22).TextColor(paint(colors.Text.Secondary))
			return
		}
		viewport := p.Width - 103
		first, end := visibleItemRange(scroll.X-4, viewport, seriesPosterStride, len(items), 2)
		focused := -1
		for i, entry := range items {
			if p.State.Focus == seriesCardFocus(key, entry.Item.Ref) {
				focused = i
				break
			}
		}
		if p.active && canLoadMore && loadMore != nil {
			nearEnd := (focused >= 0 && focused >= len(items)-4) ||
				(scroll.X+viewport >= float32(len(items))*seriesPosterStride-2*seriesPosterStride)
			if nearEnd {
				loadMore()
			}
		}
		totalWidth := float32(4) + float32(len(items))*seriesPosterStride - seriesPosterGap + 83
		ui.ScrollHorizontal(c.Key("series-strip/"+key)).FillWidth().Height(seriesPosterHeight+6).
			TrackScroll(scroll).Children(func() {
			ui.Box(c).Width(max(totalWidth, viewport)).Height(seriesPosterHeight + 6).Children(func() {
				draw := func(i int) {
					entry := items[i]
					id := seriesCardFocus(key, entry.Item.Ref)
					ui.Box(c.Key("series-slot/"+string(id))).Absolute().
						Top(6).Left(4+float32(i)*seriesPosterStride).
						Size(seriesPosterWidth, seriesPosterHeight).Children(func() {
						p.seriesCard(c, id, region, entry, seriesPosterWidth, seriesPosterCoverH, seriesPosterHeight, 23, 16)
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
	})
}

func (p *PageContext) seriesCard(c *ui.Context, id app.FocusID, region string, entry provider.SeriesEntry, width, coverH, height, titleSize, metaSize float32) {
	colors, d, text := p.Theme.Colors, p.uiColors(), p.Assets.Text.Series
	card := ui.Button(c.Key(string(id)), "").Size(width, height).Shrink(0).Padding(0, 0).
		Border(0, ui.Transparent).Background(ui.Transparent).Label(entry.Item.Title).
		OnClick(func() { p.dispatch(func() { p.openVideoDetail(entry.Item) }) })
	p.bind(card, id, region)
	card.Children(func() {
		ui.Column(c).Fill().Children(func() {
			scale, stroke := float32(1), ui.Transparent
			if p.paintFocus() == id {
				scale, stroke = 1.035, paint(colors.Focus.Ring)
			}
			coverBox := ui.Box(c.Key("series-cover")).FillWidth().Height(coverH).Radius(13).Clip().
				Background(d.coverFill)
			coverBox.Children(func() {
				if img := p.catalogCover(entry.Item.CoverURL); img != nil {
					photo := ui.Image(c.Key("series-photo"), img).Fill().Fit(ui.Cover)
					zoom := photo.AnimateTo("focus-zoom", scale, ui.Animation{Duration: focusDuration})
					photo.Scale(zoom, zoom)
					ui.Box(c).Absolute().Left(0).Top(0).Fill().LinearGradient(ui.LinearGradient{
						Angle: 0, Stops: []ui.GradientStop{{Color: d.coverShade}, {Color: ui.Transparent, At: 0.55}},
					})
				}
				tag := entry.Badge
				if tag == "" {
					tag = entry.Status
				}
				if tag != "" {
					ui.Text(c, tag).Absolute().Left(12).Top(12).ZIndex(1).Padding(4, 7).
						FontSize(11).LetterSpacing(1).Radius(4).Border(1, d.badgeBorder).
						Background(d.badgeFill).TextColor(paint(colors.Playback.Foreground))
				}
				corner := entry.Status
				if corner == "" || corner == tag {
					if entry.Score != "" {
						corner = entry.Score + text.RatingSuffix
					} else {
						corner = ""
					}
				}
				if corner != "" {
					ui.Text(c, corner).Absolute().Right(13).Bottom(12).ZIndex(1).
						FontSize(14).LetterSpacing(0.5).TextColor(paint(colors.Playback.Foreground))
				}
				ring := ui.Box(c.Key("series-focus")).Absolute().Left(0).Top(0).Fill().ZIndex(1).Radius(13)
				ring.Border(4, ring.AnimateColor("focus-ring", stroke, ui.Animation{Duration: focusDuration, NonMotion: true}))
			})
			ui.Text(c, entry.Item.Title).FillWidth().Margin(16, 0, 9, 0).
				FontSize(titleSize).FontWeight(500).LetterSpacing(0.3).SingleLine()
			p.seriesCardMeta(c, entry, metaSize)
		})
	})
}

func (p *PageContext) seriesCardMeta(c *ui.Context, entry provider.SeriesEntry, fontSize float32) {
	ink := paint(p.Theme.Colors.Text.Secondary)
	var parts []string
	if entry.Item.Category != "" {
		parts = append(parts, entry.Item.Category)
	}
	if entry.Score != "" {
		parts = append(parts, entry.Score+p.Assets.Text.Series.RatingSuffix)
	} else if entry.Status != "" {
		parts = append(parts, entry.Status)
	}
	if len(parts) == 0 {
		return
	}
	ui.Text(c, strings.Join(parts, " \u00b7 ")).FillWidth().FontSize(fontSize).SingleLine().TextColor(ink)
}

func (p *PageContext) seriesStateBlock(c *ui.Context, icon, title, note, action, actionIcon string, focus app.FocusID, region string, run func()) {
	colors, d := p.Theme.Colors, p.uiColors()
	style := buttonAppearance{
		height: 62, padding: 27, radius: 12, fontSize: 22, fontWeight: 550, iconSize: 22, iconGap: 13,
		background: d.secondaryFill, foreground: d.secondaryText, border: d.secondaryBorder,
	}
	ui.Column(c).FillWidth().Padding(110, 83, 40, 103).AlignItems(ui.Center).Children(func() {
		ui.Icon(c, p.Assets.Icon(icon)).Size(65, 65).TextColor(paint(colors.Accent.Soft))
		ui.Text(c, title).Margin(28, 0, 16, 0).FontSize(36).FontWeight(450).LineHeight(1.39)
		if note != "" {
			ui.Text(c, note).Margin(0, 0, 32, 0).FontSize(21).LineHeight(1.5).TextColor(paint(colors.Text.Muted))
		}
		if action != "" && run != nil {
			p.styledButton(c, action, actionIcon, focus, region, run, style).AlignSelf(ui.Center)
		}
	})
}

func (p *PageContext) openSeriesAll(kind seriesAllKind) {
	if p.State.Push(app.Route{Page: app.SeriesAll}) {
		p.host.series.allSection = kind
		if catalog := p.host.Series; catalog != nil {
			snap := catalog.Hub()
			items := snap.Items
			if kind == seriesAllTracked {
				items = snap.Tracked
			}
			if len(items) != 0 {
				p.State.Focus = seriesAllItemFocus(items[0].Item.Ref)
				p.host.reveal = true
			}
		}
	}
}

type seriesAllPage struct{}

func (seriesAllPage) Render(c *ui.Context, p *PageContext) {
	catalog := p.host.Series
	text := p.Assets.Text
	kind := p.host.series.allSection
	title := text.Series.PicksTitle
	if kind == seriesAllTracked {
		title = text.Series.TrackedTitle
	}
	var items []provider.SeriesEntry
	var canLoadMore, loading bool
	var loadErr string
	var loadMore, retryMore func()
	if catalog != nil {
		snap := catalog.Hub()
		if kind == seriesAllTracked {
			items = snap.Tracked
		} else {
			items = snap.Items
			canLoadMore = snap.CanLoadMore()
			loading = snap.Appending && snap.Phase == app.CatalogLoading
			if snap.Appending && snap.Phase == app.CatalogFailed {
				loadErr = string(snap.Error)
			}
			loadMore = catalog.LoadMoreHub
			retryMore = catalog.RetryHub
		}
	}
	p.renderShelfCollection(c, shelfCollectionSpec{
		key:         "series-all/" + string(kind),
		title:       title,
		countFormat: text.Series.WorksCount,
		count:       len(items),
		grid:        shelfPosterGrid,
		backFocus:   seriesAllBackFocus,
		retryFocus:  seriesAllRetryFocus,
		itemPrefix:  "series-all/item/",
		itemFocus: func(index int) app.FocusID {
			return seriesAllItemFocus(items[index].Item.Ref)
		},
		artworkURLs: func(index int) []string {
			return []string{items[index].Item.CoverURL}
		},
		renderCard: func(c *ui.Context, index int, id app.FocusID, region string, cardWidth float32) {
			p.seriesCard(c, id, region, items[index], cardWidth, shelfPosterGrid.coverH, shelfPosterGrid.cardH, 21, 15)
		},
		emptyIcon:   "series",
		emptyNote:   text.EmptyNote,
		canLoadMore: canLoadMore,
		loading:     loading,
		loadErr:     loadErr,
		moreFailed:  text.Series.MoreFailed,
		loadMore:    loadMore,
		retryMore:   retryMore,
	})
}
