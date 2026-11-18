package tvui

import (
	"strconv"

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
				ui.Text(c, shelf.note).FontSize(15).LetterSpacing(0.5).Translate(0, 5).TextColor(paint(colors.Surface.Raised))
				ui.Spacer(c).Grow(1)
				p.styledButton(c, text.Home.SeeAll, "", app.FocusID("home/shelf/"+shelf.id), shelf.id, func() {
					if shelf.id == "continue" && p.State.Allows(app.RouteSpec{RequiresEntry: true, Capability: provider.AccountHistory.Capability()}) {
						p.State.Push(libraryRoute(provider.AccountHistory))
						return
					}
					p.openCatalogShelf(shelf)
				}, buttonAppearance{
					height: 40, radius: 8, fontSize: 17,
					background: ui.Transparent, foreground: paint(colors.Surface.Raised),
				}).Width(92).Center()
			})
			ui.Box(c).Height(homeShelfHeading - 40)
			if len(shelf.items) == 0 {
				ui.Text(c, text.EmptyTitle).Margin(26, 0, 0, 4).FontSize(22).TextColor(paint(colors.Text.Secondary))
				return
			}
			ui.ScrollHorizontal(c.Key("home-shelf-cards/" + shelf.id)).FillWidth().Height(homeCardHeight).Children(func() {
				ui.Row(c).Gap(homeShelfGap).Padding(0, 20, 0, 0).Children(func() {
					for _, item := range shelf.items[:min(homeShelfLimit, len(shelf.items))] {
						p.catalogCard(c, catalogCardFocus(shelf.id, item.Ref), shelf.id, item, homeShelfCardSize, nil)
					}
				})
			})
		})
}

func (p *PageContext) catalogCard(c *ui.Context, id app.FocusID, region string, item provider.MediaItem, size catalogCardSize, direction func(app.Direction) bool) {
	colors := p.Theme.Colors
	card := ui.Button(c.Key(string(id)), "").Size(size.width, size.height).Shrink(0).Padding(0, 0).
		Border(0, ui.Transparent).Background(ui.Transparent).Label(item.Title).
		OnClick(func() { p.dispatch(func() { p.openCatalogAction(p.Assets.Text.Home.Details, &item, true) }) })
	p.bindDirection(card, id, region, direction)
	card.Children(func() {
		ui.Column(c).Fill().Children(func() {
			ui.Box(c).FillWidth().Height(size.cover).Radius(13).Clip().
				Background(paint(colors.Surface.Raised)).Children(func() {
				scale, stroke := float32(1), ui.Transparent
				if p.State.Focus == id {
					scale, stroke = 1.035, paint(colors.Focus.Ring)
				}
				if image := p.catalogCover(item.CoverURL); image != nil {
					photo := ui.Image(c.Key("card-photo"), image).Fill().Fit(ui.Cover)
					zoom := photo.AnimateTo("focus-zoom", scale, ui.Animation{Duration: focusDuration})
					photo.Scale(zoom, zoom)
					ui.Box(c).Absolute().Left(0).Top(0).Fill().LinearGradient(ui.LinearGradient{
						Angle: 0, Stops: []ui.GradientStop{{Color: paint(colors.Surface.Raised)}, {Color: ui.Transparent, At: 0.55}},
					})
				}
				if item.DurationSeconds != nil {
					ui.Text(c, catalogDuration(*item.DurationSeconds)).Absolute().Right(13).Bottom(12).ZIndex(1).
						FontSize(14).LetterSpacing(0.7)
				}
				if item.Quality != "" {
					ui.Text(c, item.Quality).Absolute().Left(12).Top(12).ZIndex(1).Padding(3, 7).
						FontSize(11).LetterSpacing(1).Radius(4).Border(1, paint(colors.Border.Subtle)).
						Background(paint(colors.Surface.Panel))
				}
				if item.ProgressSeconds != nil && item.DurationSeconds != nil && *item.DurationSeconds > 0 {
					progress := min(1, float32(*item.ProgressSeconds)/float32(*item.DurationSeconds))
					ui.Box(c).Absolute().Bottom(0).Left(0).Right(0).Height(3).ZIndex(1).
						Background(paint(colors.Playback.Track))
					ui.Box(c).Absolute().Bottom(0).Left(0).Size(size.width*progress, 3).ZIndex(1).
						Background(paint(colors.Playback.Progress))
				}
				ring := ui.Box(c.Key("card-focus")).Absolute().Left(0).Top(0).Fill().ZIndex(1).Radius(13)
				ring.Border(4, ring.AnimateColor("focus-ring", stroke, ui.Animation{Duration: focusDuration, NonMotion: true}))
			})
			ui.Text(c, item.Title).FillWidth().Margin(16, 0, 9, 0).FontSize(size.title).FontWeight(500).
				LetterSpacing(0.3).SingleLine()
			p.catalogCardMetadata(c, item, size.width)
		})
	})
}

func (p *PageContext) catalogCardMetadata(c *ui.Context, item provider.MediaItem, width float32) {
	colors, text := p.Theme.Colors, p.Assets.Text.Home
	ui.Row(c).FillWidth().Gap(9).AlignItems(ui.Center).Children(func() {
		if item.Creator != nil {
			p.catalogCreatorPortrait(c, item.Creator.AvatarURL)
			ui.Text(c, item.Creator.Name).Shrink(1).MaxWidth(0.62 * width).
				FontSize(15).SingleLine().TextColor(paint(colors.Surface.Raised))
		}
		if item.Views != nil {
			if item.Creator != nil {
				ui.Text(c, "\u00b7").FontSize(15).TextColor(paint(colors.Surface.Raised))
			}
			ui.Text(c, catalogViews(*item.Views, text)+text.Plays).
				FontSize(15).SingleLine().TextColor(paint(colors.Surface.Raised))
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
