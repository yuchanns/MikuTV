package tvui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
)

const (
	shelfCollectionHeaderHeight float32 = 102
	shelfCollectionLeft         float32 = 103
	shelfCollectionRight        float32 = 83
	shelfCollectionGridGapTop   float32 = 35
	shelfCollectionGridTop      float32 = 353
)

type shelfCollectionGrid struct {
	cols   int
	gapX   float32
	gapY   float32
	coverH float32
	cardH  float32
}

func (g shelfCollectionGrid) stride() float32 {
	return g.cardH + g.gapY
}

func (g shelfCollectionGrid) cardWidth(viewport float32) float32 {
	return max(1, (viewport-shelfCollectionLeft-shelfCollectionRight-float32(g.cols-1)*g.gapX)/float32(g.cols))
}

var (
	shelfVideoGrid = shelfCollectionGrid{
		cols:   4,
		gapX:   30,
		gapY:   35,
		coverH: 208,
		cardH:  284,
	}
	shelfPosterGrid = shelfCollectionGrid{
		cols:   5,
		gapX:   28,
		gapY:   35,
		coverH: 360,
		cardH:  442,
	}
)

type shelfCollectionSpec struct {
	key         string
	title       string
	countFormat string
	count       int
	grid        shelfCollectionGrid
	backFocus   app.FocusID
	retryFocus  app.FocusID
	itemPrefix  string
	itemFocus   func(index int) app.FocusID
	artworkURLs func(index int) []string
	renderCard  func(c *ui.Context, index int, id app.FocusID, region string, cardWidth float32)
	emptyIcon   string
	emptyNote   string
	canLoadMore bool
	loading     bool
	loadErr     string
	moreFailed  string
	loadMore    func()
	retryMore   func()
}

func (p *PageContext) renderShelfCollection(c *ui.Context, spec shelfCollectionSpec) {
	headerRegion := spec.key + "/header"
	gridRegion := spec.key + "/grid"
	statusRegion := spec.key + "/status"

	p.restoreShelfCollectionFocus(spec)
	p.shelfCollectionRegions(spec, headerRegion, gridRegion, statusRegion)

	scroll := p.scroll()
	scrollY := scroll.Y
	if p.active && p.host.reveal && p.State.Focus == spec.backFocus && scrollY > 0 {
		scroll.ScrollTo(0, 0, focusScroll)
	}

	grid := spec.grid
	rows := (spec.count + grid.cols - 1) / grid.cols
	focusedRow := -1
	if spec.count > 0 && strings.HasPrefix(string(p.State.Focus), spec.itemPrefix) {
		for i := 0; i < spec.count; i++ {
			if p.State.Focus == spec.itemFocus(i) {
				focusedRow = i / grid.cols
				break
			}
		}
	}

	if p.active && spec.canLoadMore && spec.loadErr == "" && spec.loadMore != nil && rows > 0 {
		gridEnd := shelfCollectionGridTop + float32(rows)*grid.stride()
		nearBottom := scrollY+p.Height >= gridEnd-grid.stride()*2 || (focusedRow >= 0 && focusedRow >= rows-2)
		if nearBottom {
			spec.loadMore()
		}
	}

	p.prepareShelfCollectionArtwork(spec, scrollY, rows)

	text, colors := p.Assets.Text, p.Theme.Colors
	countFormat := spec.countFormat
	if countFormat == "" {
		countFormat = text.Activity.FolderCount
	}
	countLabel := fmt.Sprintf(countFormat, spec.count)

	ui.Column(c.Key("shelf-collection/"+spec.key)).FillWidth().
		Padding(shelfCollectionHeaderHeight, 0, screenBottomInset, 0).
		Lang("zh-CN").Font("system-ui, PingFang SC").Children(func() {
		p.shelfCollectionHeading(c, spec.backFocus, headerRegion, spec.title, countLabel)
		ui.Box(c).Height(shelfCollectionGridGapTop)

		if spec.count == 0 {
			icon := spec.emptyIcon
			if icon == "" {
				icon = "library"
			}
			note := spec.emptyNote
			if note == "" {
				note = text.EmptyNote
			}
			ui.Column(c).FillWidth().Padding(90, shelfCollectionRight, 40, shelfCollectionLeft).
				AlignItems(ui.Center).Children(func() {
				ui.Icon(c, p.Assets.Icon(icon)).Size(65, 65).TextColor(paint(colors.Accent.Soft))
				ui.Text(c, text.EmptyTitle).Margin(28, 0, 16, 0).FontSize(36).FontWeight(450).LineHeight(1.39)
				ui.Text(c, note).FontSize(21).LineHeight(1.5).TextColor(paint(colors.Text.Muted))
			})
			return
		}

		p.shelfCollectionGridBody(c, spec, gridRegion, scrollY, rows, focusedRow)

		if spec.loading {
			ui.Row(c).FillWidth().Padding(20, shelfCollectionRight, 12, shelfCollectionLeft).Center().Children(func() {
				ui.Text(c, text.LoadingTitle).FontSize(20).TextColor(paint(colors.Text.Muted))
			})
		} else if spec.loadErr != "" && spec.retryMore != nil {
			failLabel := spec.moreFailed
			if failLabel == "" {
				failLabel = text.FailedTitle
			}
			ui.Row(c).FillWidth().Padding(20, shelfCollectionRight, 12, shelfCollectionLeft).Gap(18).
				AlignItems(ui.Center).Children(func() {
				ui.Text(c, failLabel).FontSize(20).TextColor(paint(colors.Status.Error.Foreground))
				p.button(c, text.Retry, spec.retryFocus, statusRegion, spec.retryMore)
			})
		}
	})
}

func (p *PageContext) shelfCollectionHeading(c *ui.Context, backFocus app.FocusID, region, title, countLabel string) {
	colors, backLabel := p.Theme.Colors, p.Assets.Text.Back
	focused := p.paintFocus() == backFocus
	bg, fg := ui.Transparent, paint(colors.Text.Secondary)
	if focused {
		bg, fg = paint(colors.Focus.Background), paint(colors.Focus.Foreground)
	}
	ui.Column(c).FillWidth().Padding(37, shelfCollectionRight, 0, shelfCollectionLeft).Children(func() {
		backBtn := ui.Button(c.Key(string(backFocus)), "").Label(backLabel).
			Height(40).Padding(0, 10).Radius(8).Border(0, ui.Transparent).Background(bg).TextColor(fg).AlignSelf(ui.Start).
			OnClick(func() { p.dispatch(func() { p.State.Back() }) })
		p.animateFocus(backBtn, bg, fg)
		p.bind(backBtn, backFocus, region)
		backBtn.Children(func() {
			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, p.Assets.Icon("back")).Size(20, 20)
				ui.Text(c, backLabel).FontSize(19)
			})
		})
		ui.Text(c, title).Margin(14, 0, 16, 0).FontSize(51).FontWeight(550).LetterSpacing(1).
			SingleLine().Role(ui.RoleHeading).Level(1)
		ui.Text(c, countLabel).FontSize(22).LineHeight(1.6).TextColor(paint(colors.Text.Muted))
	})
}

func (p *PageContext) shelfCollectionGridBody(c *ui.Context, spec shelfCollectionSpec, region string, scrollY float32, rows, focusedRow int) {
	grid := spec.grid
	first, end := visibleItemRange(scrollY-shelfCollectionGridTop, p.Height, grid.stride(), rows, 1)
	cardWidth := grid.cardWidth(p.Width)
	gridHeight := float32(rows)*grid.stride() - grid.gapY

	ui.Box(c.Key("shelf-grid/"+spec.key)).FillWidth().Height(max(gridHeight, 0)).Children(func() {
		drawRow := func(row int) {
			start := row * grid.cols
			limit := min(start+grid.cols, spec.count)
			top := float32(row) * grid.stride()
			ui.Row(c.Key("shelf-row/"+spec.key+"/"+strconv.Itoa(row))).Absolute().
				Top(top).Left(shelfCollectionLeft).Right(shelfCollectionRight).
				Height(grid.cardH).Gap(grid.gapX).Children(func() {
				for i := start; i < limit; i++ {
					spec.renderCard(c, i, spec.itemFocus(i), region, cardWidth)
				}
			})
		}
		for row := first; row < end; row++ {
			drawRow(row)
		}
		if focusedRow >= 0 {
			for row := max(0, focusedRow-1); row < min(rows, focusedRow+2); row++ {
				if row < first || row >= end {
					drawRow(row)
				}
			}
		}
	})
}

func (p *PageContext) restoreShelfCollectionFocus(spec shelfCollectionSpec) {
	if !p.active {
		return
	}
	focus := p.State.Focus
	if strings.HasPrefix(string(focus), "rail/") || focus == spec.backFocus {
		return
	}
	if focus == spec.retryFocus {
		if spec.loadErr != "" && spec.retryMore != nil {
			return
		}
		if spec.count > 0 {
			p.State.Focus = spec.itemFocus(spec.count - 1)
		} else {
			p.State.Focus = spec.backFocus
		}
		p.host.reveal = true
		return
	}
	if strings.HasPrefix(string(focus), spec.itemPrefix) {
		for i := 0; i < spec.count; i++ {
			if spec.itemFocus(i) == focus {
				return
			}
		}
	}
	if spec.count > 0 {
		p.State.Focus = spec.itemFocus(0)
	} else {
		p.State.Focus = spec.backFocus
	}
	p.host.reveal = true
}

func (p *PageContext) shelfCollectionRegions(spec shelfCollectionSpec, headerRegion, gridRegion, statusRegion string) {
	if spec.count > 0 {
		p.region(headerRegion, focusEdges{
			app.Down: {Focus: spec.itemFocus(0), Region: gridRegion},
		})
		gridEdges := focusEdges{
			app.Up: {Focus: spec.backFocus, Region: headerRegion},
		}
		if spec.loadErr != "" && spec.retryMore != nil {
			gridEdges[app.Down] = focusDestination{Focus: spec.retryFocus, Region: statusRegion}
			p.region(statusRegion, focusEdges{app.Up: {Region: gridRegion}})
		}
		p.region(gridRegion, gridEdges)
		return
	}
	p.region(headerRegion, nil)
}

func (p *PageContext) prepareShelfCollectionArtwork(spec shelfCollectionSpec, scrollY float32, rows int) {
	if p.host.Catalog == nil || spec.count == 0 || spec.artworkURLs == nil {
		return
	}
	grid := spec.grid
	visFirst, visEnd := visibleItemRange(scrollY-shelfCollectionGridTop, p.Height, grid.stride(), rows, 0)
	overFirst, overEnd := visibleItemRange(scrollY-shelfCollectionGridTop, p.Height, grid.stride(), rows, 1)
	urls := make([]string, 0, (overEnd-overFirst)*grid.cols*2)
	seen := make(map[string]struct{}, (overEnd-overFirst)*grid.cols*2)
	add := func(u string) {
		if u == "" {
			return
		}
		if _, ok := seen[u]; ok {
			return
		}
		seen[u] = struct{}{}
		urls = append(urls, u)
	}
	collectRange := func(first, end int, secondary bool) {
		for row := first; row < end; row++ {
			for col := 0; col < grid.cols; col++ {
				idx := row*grid.cols + col
				if idx >= spec.count {
					break
				}
				list := spec.artworkURLs(idx)
				if !secondary {
					if len(list) > 0 {
						add(list[0])
					}
				} else if len(list) > 1 {
					for _, u := range list[1:] {
						add(u)
					}
				}
			}
		}
	}
	collectRange(visFirst, visEnd, false)
	collectRange(visFirst, visEnd, true)
	collectRange(overFirst, overEnd, false)
	collectRange(overFirst, overEnd, true)
	p.host.Catalog.PrepareArtwork(urls)
}
