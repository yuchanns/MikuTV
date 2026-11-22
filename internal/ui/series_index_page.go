package tvui

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/provider"
)

const (
	seriesIndexBackFocus   app.FocusID = "series-index/back"
	seriesIndexResetFocus  app.FocusID = "series-index/reset"
	seriesIndexEmptyReset  app.FocusID = "series-index/empty-reset"
	seriesIndexRetryFocus  app.FocusID = "series-index/retry"
	seriesIndexCancelFocus app.FocusID = "series-index/cancel"
	seriesIndexMoreRetry   app.FocusID = "series-index/more-retry"

	seriesIndexHeaderRegion  = "series-index/header"
	seriesIndexFiltersRegion = "series-index/filters"
	seriesIndexGridRegion    = "series-index/grid"
	seriesIndexStatusRegion  = "series-index/status"

	seriesIndexCols      = 4
	seriesIndexGapX      = 30
	seriesIndexGapY      = 35
	seriesIndexCoverH    = 218
	seriesIndexCardH     = 294
	seriesIndexRowStride = seriesIndexCardH + seriesIndexGapY
	seriesIndexGridTop   = 515
)

func seriesIndexFilterFocus(key string) app.FocusID {
	return app.FocusID("series-index/filter/" + url.PathEscape(key))
}

func seriesIndexCardFocus(ref provider.MediaRef) app.FocusID {
	return app.FocusID("series-index/card/" + string(ref.Kind) + "/" + url.PathEscape(ref.ID))
}

func seriesFilterOptionFocus(index int) app.FocusID {
	return app.FocusID("series-filter-opt/" + strconv.Itoa(index))
}

func syncSeriesIndex(v *View) {
	if v.Series != nil {
		v.Series.EnsureIndex()
	}
}

func (v *View) backFromSeriesIndex() bool {
	scroll := v.activeScroll
	if v.State.Area != app.ContentArea || scroll == nil {
		return false
	}
	if scroll.Y > 0 {
		scroll.ScrollTo(0, 0, homeReturnScroll)
		if v.State.Focus != seriesIndexBackFocus {
			v.moveFocus(seriesIndexBackFocus, app.Up, false)
		}
		return true
	}
	return false
}

type seriesIndexPage struct{}

func (seriesIndexPage) Render(c *ui.Context, p *PageContext) {
	catalog := p.host.Series
	if catalog == nil {
		return
	}
	snap := catalog.Index()
	p.restoreSeriesIndexFocus(snap)
	p.seriesIndexRegions(snap)

	text, colors := p.Assets.Text, p.Theme.Colors
	rows := (len(snap.Items) + seriesIndexCols - 1) / seriesIndexCols
	scrollY := p.scroll().Y
	if p.active && snap.CanLoadMore() && rows > 0 {
		gridEnd := float32(seriesIndexGridTop) + float32(rows)*seriesIndexRowStride
		if scrollY+p.Height >= gridEnd-seriesIndexRowStride*2 {
			catalog.LoadMoreIndex()
			snap = catalog.Index()
			rows = (len(snap.Items) + seriesIndexCols - 1) / seriesIndexCols
		}
	}
	p.prepareSeriesIndexArtwork(snap, scrollY)

	ui.Column(c.Key("series-index-page")).FillWidth().Padding(seriesHeaderHeight, 0, screenBottomInset, 0).
		Lang("zh-CN").Font("system-ui, PingFang SC").Children(func() {
		p.seriesSubpageHeading(c, seriesIndexBackFocus, seriesIndexHeaderRegion, text.Series.Index, text.Series.IndexNote)
		p.seriesIndexFilterBar(c, snap)
		ui.Row(c).FillWidth().Padding(42, 83, 25, 103).Gap(20).AlignItems(ui.Center).Children(func() {
			ui.Text(c, text.Series.Results).FontSize(26).FontWeight(500)
			if snap.Loaded {
				ui.Text(c, fmt.Sprintf(text.Series.WorksCount, snap.DisplayTotal())).FontSize(18).
					Translate(0, 2).TextColor(paint(colors.Text.Muted))
			}
		})
		switch {
		case snap.Phase == app.CatalogIdle || (snap.Phase == app.CatalogLoading && len(snap.Items) == 0):
			p.seriesStateBlock(c, "search", text.Series.Loading, "", text.Cancel, "", seriesIndexCancelFocus, seriesIndexStatusRegion, func() {
				catalog.CancelIndex()
			})
			return
		case snap.Phase == app.CatalogFailed && len(snap.Items) == 0:
			p.seriesStateBlock(c, "search", text.Series.LoadFailed, p.authError(snap.Error), text.Retry, "refresh", seriesIndexRetryFocus, seriesIndexStatusRegion, func() {
				catalog.RetryIndex()
			})
			return
		case snap.Phase == app.CatalogCanceled && len(snap.Items) == 0:
			p.seriesStateBlock(c, "search", text.CanceledTitle, "", text.Retry, "refresh", seriesIndexRetryFocus, seriesIndexStatusRegion, func() {
				catalog.RetryIndex()
			})
			return
		case len(snap.Items) == 0:
			p.seriesStateBlock(c, "search", text.Series.NoFilterResults, text.Series.NoFilterNote,
				text.Series.Reset, "refresh", seriesIndexEmptyReset, seriesIndexStatusRegion, func() {
					catalog.ResetIndexFilters()
				})
			return
		}
		p.seriesIndexGrid(c, snap, scrollY)
		if snap.Appending && snap.Phase == app.CatalogLoading {
			ui.Row(c).FillWidth().Padding(20, 83, 12, 103).Center().Children(func() {
				ui.Text(c, text.LoadingTitle).FontSize(20).TextColor(paint(colors.Text.Muted))
			})
		} else if snap.Appending && snap.Phase == app.CatalogFailed {
			ui.Row(c).FillWidth().Padding(20, 83, 12, 103).Gap(18).AlignItems(ui.Center).Children(func() {
				ui.Text(c, text.Series.MoreFailed).FontSize(20).TextColor(paint(colors.Status.Error.Foreground))
				p.button(c, text.Retry, seriesIndexMoreRetry, seriesIndexStatusRegion, func() {
					catalog.RetryIndex()
				})
			})
		}
	})
}

func (p *PageContext) seriesSubpageHeading(c *ui.Context, backFocus app.FocusID, region, title, note string) {
	colors := p.Theme.Colors
	focused := p.paintFocus() == backFocus
	bg, fg := ui.Transparent, paint(colors.Text.Secondary)
	if focused {
		bg, fg = paint(colors.Focus.Background), paint(colors.Focus.Foreground)
	}
	ui.Column(c).FillWidth().Padding(37, 83, 0, 103).Children(func() {
		backBtn := ui.Button(c.Key(string(backFocus)), "").Label(p.sourceSearchSeriesLabel()).
			Height(40).Padding(0, 10).Radius(8).Border(0, ui.Transparent).Background(bg).TextColor(fg).AlignSelf(ui.Start).
			OnClick(func() { p.dispatch(func() { p.State.Back() }) })
		p.animateFocus(backBtn, bg, fg)
		p.bind(backBtn, backFocus, region)
		backBtn.Children(func() {
			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, p.Assets.Icon("back")).Size(20, 20)
				ui.Text(c, p.sourceSearchSeriesLabel()).FontSize(19)
			})
		})
		ui.Text(c, title).Margin(14, 0, 16, 0).FontSize(51).FontWeight(550).LetterSpacing(1)
		ui.Text(c, note).FontSize(22).LineHeight(1.6).TextColor(paint(colors.Text.Muted))
	})
}

func (p *PageContext) seriesIndexFilterBar(c *ui.Context, snap app.SeriesIndexSnapshot) {
	colors, d, text := p.Theme.Colors, p.uiColors(), p.Assets.Text.Series
	ui.Row(c.Key("series-index-filters")).FillWidth().Padding(37, 83, 0, 103).Gap(14).AlignItems(ui.Center).Children(func() {
		for _, group := range snap.FilterGroups {
			activeLabel := seriesActiveFilterLabel(group, snap.Filters[group.Key])
			label := group.Label + " \u00b7 " + activeLabel
			id := seriesIndexFilterFocus(group.Key)
			focused := p.paintFocus() == id
			bg, fg, border := d.subtleFill, paint(colors.Text.Secondary), paint(colors.Border.Subtle)
			if snap.Filters[group.Key] != "" {
				bg = withAlpha(colors.Accent.Primary, 0x18)
				fg = paint(colors.Accent.Soft)
				border = withAlpha(colors.Accent.Primary, 0x55)
			}
			if focused {
				bg, fg, border = paint(colors.Focus.Background), paint(colors.Focus.Foreground), ui.Transparent
			}
			groupKey := group.Key
			btn := ui.Button(c.Key(string(id)), "").Label(label).Height(51).Padding(0, 19).Radius(25).
				Border(1, border).Background(bg).TextColor(fg).Shrink(0).
				OnClick(func() {
					p.dispatch(func() {
						p.openSeriesFilterModal(groupKey)
					})
				})
			p.animateFocus(btn, bg, fg)
			p.bind(btn, id, seriesIndexFiltersRegion)
			btn.Children(func() {
				ui.Row(c).Gap(11).AlignItems(ui.Center).Children(func() {
					ui.Text(c, label).FontSize(19).SingleLine()
					ui.Icon(c, p.Assets.Icon("down")).Size(18, 18)
				})
			})
		}
		resetFocused := p.paintFocus() == seriesIndexResetFocus
		rBg, rFg := ui.Transparent, paint(colors.Text.Secondary)
		if resetFocused {
			rBg, rFg = paint(colors.Focus.Background), paint(colors.Focus.Foreground)
		}
		resetBtn := ui.Button(c.Key(string(seriesIndexResetFocus)), "").Label(text.Reset).
			Height(46).Padding(0, 14).Margin(0, 0, 0, 12).Radius(10).Border(0, ui.Transparent).
			Background(rBg).TextColor(rFg).Shrink(0).
			OnClick(func() {
				p.dispatch(func() {
					p.host.Series.ResetIndexFilters()
				})
			})
		p.animateFocus(resetBtn, rBg, rFg)
		p.bind(resetBtn, seriesIndexResetFocus, seriesIndexFiltersRegion)
		resetBtn.Children(func() {
			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, p.Assets.Icon("refresh")).Size(18, 18)
				ui.Text(c, text.Reset).FontSize(18).SingleLine()
			})
		})
	})
}

func seriesActiveFilterLabel(group provider.SeriesFilterGroup, value string) string {
	for _, opt := range group.Options {
		if opt.Value == value {
			return opt.Label
		}
	}
	if len(group.Options) != 0 {
		return group.Options[0].Label
	}
	return value
}

func (p *PageContext) restoreSeriesIndexFocus(snap app.SeriesIndexSnapshot) {
	if !p.active {
		return
	}
	focus := p.State.Focus
	fallback := seriesIndexBackFocus
	if len(snap.FilterGroups) != 0 {
		fallback = seriesIndexFilterFocus(snap.FilterGroups[0].Key)
	}
	if (focus == seriesIndexRetryFocus && snap.Phase != app.CatalogFailed && snap.Phase != app.CatalogCanceled) ||
		(focus == seriesIndexCancelFocus && snap.Phase != app.CatalogLoading) ||
		(focus == seriesIndexEmptyReset && len(snap.Items) != 0) {
		p.State.Focus = fallback
		p.host.reveal = true
		return
	}
	if strings.HasPrefix(string(focus), "series-index/card/") {
		for _, entry := range snap.Items {
			if seriesIndexCardFocus(entry.Item.Ref) == focus {
				return
			}
		}
		if len(snap.Items) != 0 {
			p.State.Focus = seriesIndexCardFocus(snap.Items[0].Item.Ref)
		} else {
			p.State.Focus = fallback
		}
		p.host.reveal = true
	}
}

func (p *PageContext) seriesIndexRegions(snap app.SeriesIndexSnapshot) {
	hasFilters := len(snap.FilterGroups) != 0
	hasGrid := len(snap.Items) != 0
	belowHeader := focusDestination{Region: seriesIndexStatusRegion}
	if hasFilters {
		belowHeader = focusDestination{Focus: seriesIndexFilterFocus(snap.FilterGroups[0].Key), Region: seriesIndexFiltersRegion}
	} else if hasGrid {
		belowHeader = focusDestination{Focus: seriesIndexCardFocus(snap.Items[0].Item.Ref), Region: seriesIndexGridRegion}
	}
	p.region(seriesIndexHeaderRegion, focusEdges{app.Down: belowHeader})
	belowFilters := focusDestination{Region: seriesIndexStatusRegion}
	if hasGrid {
		belowFilters = focusDestination{Focus: seriesIndexCardFocus(snap.Items[0].Item.Ref), Region: seriesIndexGridRegion}
	}
	if hasFilters {
		p.region(seriesIndexFiltersRegion, focusEdges{
			app.Up:   {Focus: seriesIndexBackFocus, Region: seriesIndexHeaderRegion},
			app.Down: belowFilters,
		})
	}
	aboveGrid := focusDestination{Focus: seriesIndexBackFocus, Region: seriesIndexHeaderRegion}
	if hasFilters {
		aboveGrid = focusDestination{Region: seriesIndexFiltersRegion}
	}
	if hasGrid {
		p.region(seriesIndexGridRegion, focusEdges{
			app.Up:   aboveGrid,
			app.Down: {Region: seriesIndexStatusRegion},
		})
		p.region(seriesIndexStatusRegion, focusEdges{app.Up: {Region: seriesIndexGridRegion}})
	} else {
		p.region(seriesIndexStatusRegion, focusEdges{app.Up: aboveGrid})
	}
}

func (p *PageContext) prepareSeriesIndexArtwork(snap app.SeriesIndexSnapshot, scrollY float32) {
	if p.host.Catalog == nil || len(snap.Items) == 0 {
		return
	}
	rows := (len(snap.Items) + seriesIndexCols - 1) / seriesIndexCols
	first, end := visibleItemRange(scrollY-seriesIndexGridTop, p.Height, seriesIndexRowStride, rows, 1)
	urls := make([]string, 0, (end-first)*seriesIndexCols)
	for row := first; row < end; row++ {
		for col := 0; col < seriesIndexCols; col++ {
			idx := row*seriesIndexCols + col
			if idx >= len(snap.Items) {
				break
			}
			if u := snap.Items[idx].Item.CoverURL; u != "" {
				urls = append(urls, u)
			}
		}
	}
	p.host.Catalog.PrepareArtwork(urls)
}

func (p *PageContext) seriesIndexGrid(c *ui.Context, snap app.SeriesIndexSnapshot, scrollY float32) {
	total := len(snap.Items)
	rows := (total + seriesIndexCols - 1) / seriesIndexCols
	first, end := visibleItemRange(scrollY-seriesIndexGridTop, p.Height, seriesIndexRowStride, rows, 1)
	focusedRow := -1
	for i, entry := range snap.Items {
		if p.State.Focus == seriesIndexCardFocus(entry.Item.Ref) {
			focusedRow = i / seriesIndexCols
			break
		}
	}
	cardWidth := (p.Width - 103 - 83 - float32(seriesIndexCols-1)*seriesIndexGapX) / seriesIndexCols
	gridHeight := float32(rows)*seriesIndexRowStride - seriesIndexGapY
	ui.Box(c.Key("series-index-grid")).FillWidth().Height(max(gridHeight, 0)).Children(func() {
		drawRow := func(row int) {
			start := row * seriesIndexCols
			limit := min(start+seriesIndexCols, total)
			top := float32(row) * seriesIndexRowStride
			ui.Row(c.Key("series-index-row/"+strconv.Itoa(row))).Absolute().
				Top(top).Left(103).Right(83).Height(seriesIndexCardH).Gap(seriesIndexGapX).Children(func() {
				for i := start; i < limit; i++ {
					entry := snap.Items[i]
					p.seriesCard(c, seriesIndexCardFocus(entry.Item.Ref), seriesIndexGridRegion, entry,
						cardWidth, seriesIndexCoverH, seriesIndexCardH, 21, 15)
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

func (p *PageContext) openSeriesFilterModal(key string) {
	if p.host.Series == nil {
		return
	}
	p.host.series.filterKey = key
	p.host.series.filterList = ui.ListState{}
	snap := p.host.Series.Index()
	selectedIdx := 0
	for _, group := range snap.FilterGroups {
		if group.Key != key {
			continue
		}
		cur := snap.Filters[key]
		for i, opt := range group.Options {
			if opt.Value == cur {
				selectedIdx = i
				break
			}
		}
		break
	}
	if p.State.Push(app.Route{Page: app.SeriesIndexFilter}) {
		p.State.Focus = seriesFilterOptionFocus(selectedIdx)
		p.host.reveal = true
	}
}

func seriesIndexFilterModalHeight(v *View) float32 {
	count := 4
	if v.Series != nil {
		for _, g := range v.Series.Index().FilterGroups {
			if g.Key == v.series.filterKey {
				count = len(g.Options)
				break
			}
		}
	}
	h := float32(190 + min(count, 6)*66)
	return min(max(h, 340), 620)
}

type seriesIndexFilterPage struct{}

func (seriesIndexFilterPage) Render(c *ui.Context, p *PageContext) {
	catalog := p.host.Series
	if catalog == nil {
		return
	}
	snap := catalog.Index()
	var group provider.SeriesFilterGroup
	found := false
	for _, g := range snap.FilterGroups {
		if g.Key == p.host.series.filterKey {
			group = g
			found = true
			break
		}
	}
	const region = "series-index-filter-modal"
	p.region(region, nil)
	if !found || len(group.Options) == 0 {
		p.button(c, p.Assets.Text.Close, app.CloseModal, region, func() { p.State.Back() })
		return
	}
	colors := p.Theme.Colors
	ui.Text(c, group.Label).FontSize(36).FontWeight(550)
	curVal := snap.Filters[group.Key]
	if curVal == "" && len(group.Options) != 0 {
		curVal = group.Options[0].Value
	}
	list := &p.host.series.filterList
	list.Key = func(index int) any { return group.Options[index].Value }
	ui.List(c.Key("series-filter-options"), list, len(group.Options)).FillWidth().Grow(1).
		Padding(0, 0, 12, 0).Rows(func(row ui.ListRow) {
		rc := row.Context
		opt := group.Options[row.Index]
		id := seriesFilterOptionFocus(row.Index)
		selected := opt.Value == curVal
		focused := p.paintFocus() == id
		bg, fg := ui.Transparent, paint(colors.Text.Secondary)
		if selected {
			bg, fg = withAlpha(colors.Text.Primary, 0x06), paint(colors.Accent.Soft)
		}
		if focused {
			bg, fg = paint(colors.Focus.Background), paint(colors.Focus.Foreground)
		}
		val := opt.Value
		btn := ui.Button(rc.Key(string(id)), "").Label(opt.Label).FillWidth().Height(60).
			Margin(0, 0, 6, 0).Padding(0, 18).Radius(10).Border(0, ui.Transparent).
			Background(bg).TextColor(fg).
			OnClick(func() {
				p.dispatch(func() {
					p.State.Back()
					catalog.SetIndexFilter(group.Key, val)
				})
			})
		p.animateFocus(btn, bg, fg)
		p.bind(btn, id, region)
		btn.Children(func() {
			ui.Row(rc).Fill().AlignItems(ui.Center).Children(func() {
				ui.Text(rc, opt.Label).Grow(1).FontSize(23).SingleLine()
				if selected {
					ui.Icon(rc, p.Assets.Icon("check")).Size(23, 23)
				}
			})
		})
	})
}
