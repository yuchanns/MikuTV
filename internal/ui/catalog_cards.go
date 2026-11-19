package tvui

import (
	"strconv"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/provider"
)

const (
	homeShelfLimit = 12
	homeGridRight  = 83
)

type catalogWindow struct {
	first, end int
	slots      int
	focused    int
}

type catalogCardSize struct {
	width, height, cover, title float32
}

var homeShelfCardSize = catalogCardSize{width: homeShelfCoverWidth, height: homeCardHeight, cover: homeCoverHeight, title: 21}

func (p *PageContext) catalogViewport(loadingTail bool) []catalogWindow {
	layout, y := &p.host.home.layout, p.scroll().Y
	windows := make([]catalogWindow, len(layout.sections))
	focused, hasFocus := layout.byFocus[p.State.Focus]
	for index, section := range layout.sections {
		grid := section.grid
		total := grid.slots(len(section.cards), loadingTail && index == len(layout.sections)-1)
		first, end := section.visibleRows(y, p.Height, total, 1)
		window := catalogWindow{first: first, end: end, slots: total, focused: -1}
		if hasFocus && focused.section == index && focused.item >= grid.podium {
			window.focused = (focused.item - grid.podium) / grid.columns
		}
		windows[index] = window
	}
	return windows
}

func (p *PageContext) catalogGrid(c *ui.Context, section catalogSection, window catalogWindow, last, refresh bool, current app.CatalogSnapshot) {
	ui.Box(c.Key("catalog-grid/" + section.group.ID)).FillWidth().Height(section.height(window.slots)).Children(func() {
		if section.grid.podium != 0 {
			p.catalogPodium(c, section)
		}
		if p.host.home.tab == "" && ((window.first < window.end && window.first == 0) || (refresh && p.State.Focus == homeRefresh)) {
			p.catalogGridHeading(c, section.group.Title, refresh, current)
		}
		for row := window.first; row < window.end; row++ {
			p.catalogGridRow(c, section, window, row, last, refresh, current)
		}
		if window.focused >= 0 {
			for row := max(0, window.focused-1); row < min(window.focused+2, section.grid.rows(window.slots)); row++ {
				if row < window.first || row >= window.end {
					p.catalogGridRow(c, section, window, row, last, refresh, current)
				}
			}
		}
	})
}

func (p *PageContext) catalogGridRow(c *ui.Context, section catalogSection, window catalogWindow, row int, last, refresh bool, current app.CatalogSnapshot) {
	grid := section.grid
	start := grid.firstItem(row)
	top := grid.heading + float32(row)*grid.stride()
	atTail := last && row == grid.rows(len(section.cards))-1
	paging := current.CanLoadMore() || (current.Appending && current.Phase == app.CatalogLoading)
	failed := catalogAppendFailed(current)
	goDown := atTail && (paging || failed || p.catalogHasEndRefresh(current))
	size := catalogCardSize{width: grid.width(p.Width), height: grid.height, cover: grid.cover, title: 21}
	end := min(start+grid.columns, window.slots)
	scrollsAcross := grid.cardWidth != 0
	draw := func() {
		for index := start; index < end; index++ {
			if index >= len(section.cards) {
				p.catalogSkeleton(c, size)
				continue
			}
			var direction func(app.Direction) bool
			goUp := refresh && row == 0 && index == min(len(section.cards), grid.columns)-1
			if scrollsAcross || goUp || goDown {
				direction = func(key app.Direction) bool {
					if scrollsAcross {
						switch key {
						case app.Left:
							if index > start {
								p.host.moveFocus(section.cards[index-1], key, true)
								return true
							}
							if p.left.Focus != "" {
								p.host.moveFocus(p.left.Focus, key, true)
								return true
							}
						case app.Right:
							if index+1 < min(end, len(section.cards)) {
								p.host.moveFocus(section.cards[index+1], key, true)
							}
							return true
						}
					}
					if key == app.Up && goUp {
						p.host.moveFocus(homeRefresh, app.Up, true)
						return true
					}
					if key != app.Down || !goDown {
						return false
					}
					scroll := p.scroll()
					switch {
					case paging:
						p.host.Catalog.LoadMore()
						if (len(section.cards)-grid.podium)%grid.columns == 0 {
							loadingEnd := section.top + section.height(grid.slots(len(section.cards), true))
							scroll.ScrollTo(scroll.X, max(scroll.Y, loadingEnd-p.Height), focusScroll)
						}
					case failed:
						p.host.moveFocus(homeRetry, app.Down, true)
					default:
						p.host.moveFocus(homeEnd, app.Down, true)
					}
					return true
				}
			}
			if grid.podium != 0 {
				p.catalogRankingRow(c, section.cards[index], section.group.Items[index], index+1, size.width, direction)
				continue
			}
			p.catalogCard(c, section.cards[index], homeCards, section.group.Items[index], size, direction)
		}
	}
	key := "catalog-row/" + strconv.Itoa(row)
	if scrollsAcross {
		width := float32(end-start)*size.width + float32(max(0, end-start-1))*grid.gap
		scrollKey := section.group.ID + "/" + key
		home := &p.host.home
		if home.rowScrolls == nil {
			home.rowScrolls = make(map[string]*ui.ScrollState)
		}
		scroll := home.rowScrolls[scrollKey]
		if scroll == nil {
			scroll = &ui.ScrollState{}
			home.rowScrolls[scrollKey] = scroll
		}
		ui.ScrollHorizontal(c.Key(key)).Absolute().Left(grid.left).Right(0).Top(top).Height(grid.height).
			TrackScroll(scroll).Children(func() {
			ui.Row(c).Width(width).Height(grid.height).Gap(grid.gap).Children(draw)
		})
		return
	}
	ui.Row(c.Key(key)).Absolute().Left(grid.left).Right(homeGridRight).Top(top).
		Height(grid.height).Gap(grid.gap).Children(draw)
}

func (p *PageContext) catalogShelfVisible(index int, y float32) bool {
	top := float32(homeHeaderHeight + homeHeroHeight + index*homeShelfHeight)
	return top+homeShelfHeight >= y-homeShelfHeight && top < y+p.Height+homeShelfHeight
}

func (p *PageContext) catalogShelf(c *ui.Context, shelf catalogShelf, idx int, scroll float32) {
	if !p.catalogShelfVisible(idx, scroll) {
		ui.Box(c.Key("home-shelf/" + shelf.id)).Height(homeShelfHeight)
		return
	}
	colors, text := p.Theme.Colors, p.Assets.Text
	ui.Column(c.Key("home-shelf/"+shelf.id)).FillWidth().Height(homeShelfHeight).Padding(homeShelfTop, 0, 0, 103).
		Children(func() {
			ui.Row(c).FillWidth().Padding(0, 79, 0, 0).Height(40).Gap(18).AlignItems(ui.Center).Children(func() {
				ui.Text(c, shelf.title).FontSize(27).FontWeight(550).LetterSpacing(1.5)
				ui.Text(c, shelf.note).FontSize(15).LetterSpacing(0.5).Translate(0, 5).TextColor(paint(colors.Text.Secondary))
				ui.Spacer(c).Grow(1)
				p.styledButton(c, text.Home.SeeAll, "", app.FocusID("home/shelf/"+shelf.id), shelf.id, func() {
					if shelf.id == "continue" && p.State.Allows(app.RouteSpec{RequiresEntry: true, Capability: provider.AccountHistory.Capability()}) {
						p.State.Push(libraryRoute(provider.AccountHistory))
						return
					}
					p.openCatalogAction(shelf.title, shelf.items)
				}, buttonAppearance{
					height: 40, radius: 8, fontSize: 17,
					background: ui.Transparent, foreground: paint(colors.Text.Muted),
				}).Width(92).Center()
			})
			ui.Box(c).Height(homeShelfHeading - 40)
			if len(shelf.items) == 0 {
				ui.Text(c, text.EmptyTitle).Margin(26, 0, 0, 4).FontSize(22).TextColor(paint(colors.Text.Secondary))
				return
			}
			home := &p.host.home
			if home.shelfScrolls == nil {
				home.shelfScrolls = make(map[string]*ui.ScrollState)
			}
			scroll := home.shelfScrolls[shelf.id]
			if scroll == nil {
				scroll = &ui.ScrollState{}
				home.shelfScrolls[shelf.id] = scroll
			}
			p.catalogStrip(c, shelf.id, shelf.items[:min(homeShelfLimit, len(shelf.items))], scroll)
		})
}

func (p *PageContext) catalogStrip(c *ui.Context, shelf string, items []provider.MediaItem, scroll *ui.ScrollState) {
	cardWidth := homeShelfCardSize.width
	region, viewport := shelf, p.Width-103
	top, left, gap := float32(0), float32(0), float32(homeShelfGap)
	if shelf == videoDetailRelatedShelf {
		region, viewport = videoDetailRelatedRegion, p.Width-2*videoDetailInset
		top, left, gap = 6, 4, 27
	}
	stride := cardWidth + gap
	first, end := visibleItemRange(scroll.X-left, viewport, stride, len(items), 2)
	focused := -1
	for i, item := range items {
		if p.State.Focus == catalogCardFocus(shelf, item.Ref) {
			focused = i
			break
		}
	}
	width := left + float32(len(items))*stride - gap + 20
	ui.ScrollHorizontal(c.Key("catalog-strip/"+shelf)).FillWidth().Height(homeCardHeight+top).
		Margin(min(top, 2), 0, 0, 0).TrackScroll(scroll).Children(func() {
		ui.Box(c).Width(width).Height(homeCardHeight + top).Children(func() {
			draw := func(i int) {
				item := items[i]
				id := catalogCardFocus(shelf, item.Ref)
				ui.Box(c.Key("strip-card/"+string(id))).Absolute().Top(top).Left(left+float32(i)*stride).
					Size(cardWidth, homeCardHeight).Children(func() {
					p.catalogCard(c, id, region, item, homeShelfCardSize, nil)
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

func (p *PageContext) catalogCard(c *ui.Context, id app.FocusID, region string, item provider.MediaItem, size catalogCardSize, direction func(app.Direction) bool) {
	colors, d := p.Theme.Colors, p.uiColors()
	related := region == videoDetailRelatedRegion
	card := ui.Button(c.Key(string(id)), "").Size(size.width, size.height).Shrink(0).Padding(0, 0).
		Border(0, ui.Transparent).Background(ui.Transparent).Label(item.Title)
	if p.canOpenVideoDetail(item.Ref) {
		card.OnClick(func() { p.dispatch(func() { p.openVideoDetail(item) }) })
	}
	p.bindDirection(card, id, region, direction)
	card.Children(func() {
		ui.Column(c).Fill().Children(func() {
			scale, stroke := float32(1), ui.Transparent
			focused := p.paintFocus() == id
			if focused {
				scale, stroke = 1.035, paint(colors.Focus.Ring)
			}
			cover := ui.Box(c.Key("card-cover")).FillWidth().Height(size.cover).Radius(13).Clip().
				Background(d.coverFill)
			if related {
				shadow := withAlpha(colors.Overlay.Shadow, 0x22)
				offset, blur := float32(8), float32(24)
				if focused {
					shadow = withAlpha(colors.Overlay.Shadow, 0x66)
					offset, blur = 14, 36
				}
				animation := ui.Animation{Duration: 300 * time.Millisecond}
				cover.Shadow(0, cover.AnimateTo("shadow-offset", offset, animation),
					cover.AnimateTo("shadow-blur", blur, animation), 0, cover.AnimateColor("shadow-ink", shadow, animation))
				cover.Shadow(0, 0, 0, 3, cover.AnimateColor("focus-ring", stroke, animation))
			}
			cover.Children(func() {
				if image := p.catalogCover(item.CoverURL); image != nil {
					photo := ui.Image(c.Key("card-photo"), image).Fill().Fit(ui.Cover)
					zoom := photo.AnimateTo("focus-zoom", scale, ui.Animation{Duration: focusDuration})
					photo.Scale(zoom, zoom)
					ui.Box(c).Absolute().Left(0).Top(0).Fill().LinearGradient(ui.LinearGradient{
						Angle: 0, Stops: []ui.GradientStop{{Color: d.coverShade}, {Color: ui.Transparent, At: 0.55}},
					})
				}
				if item.DurationSeconds != nil {
					duration := ui.Text(c, catalogDuration(*item.DurationSeconds)).Absolute().Right(13).Bottom(12).
						ZIndex(1).FontSize(14).LetterSpacing(0.7).TextColor(paint(colors.Playback.Foreground))
					if related {
						duration.Bottom(14)
					}
				}
				if item.Quality != "" {
					tag := ui.Text(c, item.Quality).Absolute().Left(12).Top(12).ZIndex(1).Padding(3, 7).
						FontSize(11).LetterSpacing(1).Radius(4).Border(1, d.badgeBorder).
						Background(d.badgeFill).TextColor(paint(colors.Playback.Foreground))
					if related {
						tag.Padding(4, 7).FontWeight(400)
					}
				}
				if item.ProgressSeconds != nil && item.DurationSeconds != nil && *item.DurationSeconds > 0 {
					progress := min(1, float32(*item.ProgressSeconds)/float32(*item.DurationSeconds))
					ui.Box(c).Absolute().Bottom(0).Left(0).Right(0).Height(3).ZIndex(1).
						Background(paint(colors.Playback.Track))
					ui.Box(c).Absolute().Bottom(0).Left(0).Size(size.width*progress, 3).ZIndex(1).
						Background(paint(colors.Playback.Progress))
				}
				if !related {
					ring := ui.Box(c.Key("card-focus")).Absolute().Left(0).Top(0).Fill().ZIndex(1).Radius(13)
					ring.Border(4, ring.AnimateColor("focus-ring", stroke, ui.Animation{Duration: focusDuration, NonMotion: true}))
				}
			})
			ui.Text(c, item.Title).FillWidth().Margin(16, 0, 9, 0).FontSize(size.title).FontWeight(500).
				LetterSpacing(0.3).SingleLine()
			p.catalogCardMetadata(c, item, size.width)
		})
	})
}

func (p *PageContext) catalogCardMetadata(c *ui.Context, item provider.MediaItem, width float32) {
	colors, text := p.Theme.Colors, p.Assets.Text.Home
	related := p.Location.Page == app.VideoDetail
	ink := paint(colors.Text.Secondary)
	ui.Row(c).FillWidth().Gap(9).AlignItems(ui.Center).Children(func() {
		if item.Creator != nil {
			if related {
				ui.Box(c).Size(17, 17).Shrink(0).Radius(8.5).Clip().Center().
					Background(paint(colors.Surface.Raised)).Children(func() {
					if portrait := p.catalogCover(item.Creator.AvatarURL); portrait != nil {
						ui.Image(c, portrait).Fill().Fit(ui.Cover)
					} else {
						ui.Icon(c, p.Assets.Icon("user")).Size(10, 10).TextColor(ink)
					}
				})
			} else {
				p.catalogCreatorPortrait(c, item.Creator.AvatarURL)
			}
			ui.Text(c, item.Creator.Name).Shrink(1).MaxWidth(0.62 * width).
				FontSize(15).SingleLine().TextColor(ink)
		}
		if item.Views != nil {
			if item.Creator != nil {
				ui.Text(c, "\u00b7").FontSize(15).TextColor(ink)
			}
			ui.Text(c, catalogViews(*item.Views, text)+text.Plays).FontSize(15).SingleLine().TextColor(ink)
		}
	})
}

func (p *PageContext) catalogCreatorPortrait(c *ui.Context, location string) {
	ui.Box(c).Size(17, 17).Shrink(0).Radius(8.5).Clip().
		Background(withAlpha(p.Theme.Colors.Text.Secondary, 0x40)).Children(func() {
		if avatar := p.catalogCover(location); avatar != nil {
			ui.Image(c, avatar).Fill().Fit(ui.Cover)
		}
	})
}

func (p *PageContext) catalogSkeleton(c *ui.Context, size catalogCardSize) {
	colors := p.Theme.Colors
	ui.Column(c).Size(size.width, size.height).Shrink(0).Children(func() {
		ui.Box(c).FillWidth().Height(size.cover).Radius(13).
			Gradient(paint(colors.Surface.Panel), paint(colors.Surface.Raised), 120)
		ui.Box(c).Size(size.width*0.73, 12).Margin(18, 0, 10, 0).Radius(4).Background(paint(colors.Hover.Background))
		ui.Box(c).Size(size.width*0.43, 9).Radius(4).Background(paint(colors.Surface.Raised))
	})
}
