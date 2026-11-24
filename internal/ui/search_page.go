package tvui

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/provider"
)

const (
	searchInputFocus   app.FocusID = app.ContentAction
	searchInputEdit    app.FocusID = "search/input-edit"
	searchFilterFocus  app.FocusID = "search/filter"
	searchRetryFocus   app.FocusID = "search/retry"
	searchInputRegion              = "search/input"
	searchHistoryRegion            = "search/history"
	searchSuggestRegion            = "search/suggest"
	searchHotRegion                = "search/hot"
	searchSortRegion               = "search/sort"
	searchKindsRegion              = "search/kinds"
	searchResultsRegion            = "search/results"
	searchStatusRegion             = "search/status"

	searchLeftPadding     float32 = 103
	searchRightPadding    float32 = 85
	searchLeftWidth       float32 = 450
	searchColumnGap       float32 = 68
	searchGridGap         float32 = 26
	searchCoverHeight     float32 = 205
	searchCardHeight      float32 = 280
	searchCreatorHeight   float32 = 228
	searchMaxHistoryChips         = 6
	searchHistoryPerRow           = 3
	searchMaxSuggestRows          = 5
	searchMaxHotRows              = 5
	searchQueryMaxLength          = 100
)

var defaultSearchKinds = [...]provider.SearchKind{
	provider.SearchAll,
	provider.SearchVideo,
	provider.SearchCreator,
	provider.SearchSeries,
	provider.SearchLive,
}

type searchViewState struct {
	editing     bool
	input       string
	filterDraft searchFilterDraft
	lastResult  app.FocusID
}

type searchPage struct{}

func syncSearch(v *View) {
	if v.Search != nil {
		v.Search.EnsureLoaded()
	}
}

func (v *View) backFromSearch() bool {
	if v.search.editing {
		v.search.editing = false
		v.moveFocus(searchInputFocus, app.Left, false)
		return true
	}
	if v.State.Area != app.ContentArea {
		return false
	}
	scroll := v.activeScroll
	scrolled := scroll != nil && scroll.Y > 0
	if v.State.Focus == searchInputFocus && !scrolled {
		return false
	}
	if scroll != nil && scroll.Y > 0 {
		scroll.ScrollTo(0, 0, homeReturnScroll)
	}
	v.moveFocus(searchInputFocus, app.Up, false)
	return true
}

func searchHistoryFocus(index int) app.FocusID {
	return app.FocusID("search/history/" + strconv.Itoa(index))
}

func searchSuggestFocus(index int) app.FocusID {
	return app.FocusID("search/suggest/" + strconv.Itoa(index))
}

func searchHotFocus(index int) app.FocusID {
	return app.FocusID("search/hot/" + strconv.Itoa(index))
}

func searchKindFocus(kind provider.SearchKind) app.FocusID {
	return app.FocusID("search/kind/" + string(kind))
}

func searchItemFocus(ref provider.MediaRef) app.FocusID {
	return app.FocusID("search/item/" + string(ref.Kind) + "/" + url.PathEscape(ref.ID))
}

func searchCreatorFocus(id string) app.FocusID {
	return app.FocusID("search/creator/" + url.PathEscape(id))
}

func searchKindsList(snapshot app.SearchSnapshot) []provider.SearchKind {
	if len(snapshot.Kinds) != 0 {
		return snapshot.Kinds
	}
	return defaultSearchKinds[:]
}

func searchKindSupportsFilters(kind provider.SearchKind) bool {
	return kind == "" || kind == provider.SearchAll || kind == provider.SearchVideo
}

func (p *PageContext) searchKindLabel(kind provider.SearchKind) string {
	text := p.Assets.Text.Search
	switch kind {
	case provider.SearchVideo:
		return text.KindVideo
	case provider.SearchCreator:
		return p.sourceSearchCreatorLabel()
	case provider.SearchSeries:
		return p.sourceSearchSeriesLabel()
	case provider.SearchLive:
		return text.KindLive
	default:
		return text.KindAll
	}
}

func (searchPage) Render(c *ui.Context, p *PageContext) {
	entry, _ := p.State.Current()
	if !entry.Capabilities.Has(provider.SearchCatalog) || p.host.Search == nil {
		p.renderUnavailableContent(c)
		return
	}
	current := p.host.Search.Snapshot()
	if current.Phase == app.CatalogIdle {
		current.Phase = app.CatalogLoading
	}
	activeKind := current.Request.Kind
	if activeKind == "" {
		activeKind = provider.SearchAll
	}
	kinds := searchKindsList(current)
	history := current.History[:min(len(current.History), searchMaxHistoryChips)]
	var suggestions []string
	if current.Request.Query != "" && len(current.Suggestions) != 0 {
		suggestions = current.Suggestions[:min(len(current.Suggestions), searchMaxSuggestRows)]
	}
	var hot []string
	if len(suggestions) == 0 && len(current.HotKeywords) != 0 {
		hot = current.HotKeywords[:min(len(current.HotKeywords), searchMaxHotRows)]
	}
	hasFilters := searchKindSupportsFilters(activeKind)
	rightWidth := max(260, p.Width-searchLeftPadding-searchLeftWidth-searchColumnGap-searchRightPadding)
	cardWidth := max(120, (rightWidth-searchGridGap)/2)

	p.restoreSearchFocus(current, history, suggestions, hot, activeKind, hasFilters)
	p.searchRegions(current, history, suggestions, hot, activeKind, hasFilters)

	resultsTop := float32(homeHeaderHeight + 250 + 43 + 19 + 43 + 22)
	rowHeight := searchCardHeight
	if activeKind == provider.SearchCreator {
		rowHeight = searchCreatorHeight
	}
	stride := rowHeight + searchGridGap
	count := current.ResultCount()
	loadingTail := current.Appending && current.Phase == app.CatalogLoading
	slots := count
	if loadingTail {
		slots = count + 2 - count%2
	}
	totalRows := (slots + 1) / 2
	scrollY := p.scroll().Y
	if p.active && current.CanLoadMore() && count > 0 && scrollY+p.Height >= resultsTop+float32(totalRows)*stride-stride {
		p.host.Search.LoadMore()
		current = p.host.Search.Snapshot()
		loadingTail = current.Appending && current.Phase == app.CatalogLoading
		slots = count
		if loadingTail {
			slots = count + 2 - count%2
		}
		totalRows = (slots + 1) / 2
	}
	firstRow, endRow := visibleItemRange(scrollY-resultsTop, p.Height, stride, totalRows, 1)
	focusedRow := p.searchFocusedRow(current, activeKind)
	p.prepareSearchArtwork(current, activeKind, firstRow, endRow, focusedRow)

	text, colors := p.Assets.Text.Search, p.Theme.Colors
	ui.Column(c.Key("search-page")).FillWidth().Padding(0, 0, screenBottomInset, 0).
		Lang(p.langTag()).Font(p.uiFont()).Children(func() {
		ui.Box(c).Height(homeHeaderHeight)
		ui.Column(c.Key("search-title")).FillWidth().Padding(48, searchRightPadding, 8, searchLeftPadding).Children(func() {
			ui.Row(c).Height(22).Gap(15).AlignItems(ui.Center).Children(func() {
				ui.Box(c).Size(24, 1).Background(paint(colors.Accent.Soft))
				ui.Text(c, text.Eyebrow).FontSize(16).FontWeight(500).LetterSpacing(3).TextColor(paint(colors.Text.Secondary))
			})
			ui.Text(c, text.Title).Margin(22, 0, 17, 0).FontSize(48).FontWeight(550).LetterSpacing(2)
			ui.Text(c, p.sourceSearchHint()).FontSize(21).LineHeight(1.7).TextColor(paint(colors.Text.Muted))
		})
		ui.Row(c.Key("search-layout")).FillWidth().Padding(32, searchRightPadding, 0, searchLeftPadding).
			Gap(searchColumnGap).AlignItems(ui.Start).Children(func() {
			ui.Column(c.Key("search-left")).Width(searchLeftWidth).Shrink(0).Children(func() {
				p.searchBox(c, current)
				if len(history) != 0 {
					p.searchHistorySection(c, history)
				}
				if len(suggestions) != 0 {
					p.searchKeywordList(c, text.Suggestions, suggestions, searchSuggestRegion, searchSuggestFocus)
				} else if len(hot) != 0 {
					p.searchKeywordList(c, text.HotSearch, hot, searchHotRegion, searchHotFocus)
				}
			})
			ui.Column(c.Key("search-side")).Width(rightWidth).Shrink(0).Children(func() {
				p.searchSideHeader(c, current, hasFilters)
				p.searchKindTabs(c, kinds, activeKind)
				p.searchResultsBody(c, current, activeKind, rightWidth, cardWidth, rowHeight, stride, totalRows, slots, firstRow, endRow, focusedRow)
			})
		})
	})
}

func (p *PageContext) searchRegions(current app.SearchSnapshot, history, suggestions, hot []string, activeKind provider.SearchKind, hasFilters bool) {
	belowInput := focusDestination{}
	if len(history) != 0 {
		belowInput = focusDestination{Focus: searchHistoryFocus(0), Region: searchHistoryRegion}
	} else if len(suggestions) != 0 {
		belowInput = focusDestination{Focus: searchSuggestFocus(0), Region: searchSuggestRegion}
	} else if len(hot) != 0 {
		belowInput = focusDestination{Focus: searchHotFocus(0), Region: searchHotRegion}
	}
	rightFromLeft := focusDestination{Focus: searchKindFocus(activeKind), Region: searchKindsRegion}
	if current.ResultCount() > 0 {
		if hasFilters {
			rightFromLeft = focusDestination{Focus: searchKindFocus(activeKind), Region: searchKindsRegion}
		}
	}
	p.region(searchInputRegion, focusEdges{
		app.Right: rightFromLeft,
		app.Down:  belowInput,
	})
	belowHistory := focusDestination{}
	if len(suggestions) != 0 {
		belowHistory = focusDestination{Focus: searchSuggestFocus(0), Region: searchSuggestRegion}
	} else if len(hot) != 0 {
		belowHistory = focusDestination{Focus: searchHotFocus(0), Region: searchHotRegion}
	}
	p.region(searchHistoryRegion, focusEdges{
		app.Up:    {Focus: searchInputFocus},
		app.Right: {Focus: searchKindFocus(activeKind), Region: searchKindsRegion},
		app.Down:  belowHistory,
	})
	aboveList := focusDestination{Focus: searchInputFocus}
	if len(history) != 0 {
		aboveList = focusDestination{Focus: searchHistoryFocus(len(history) - 1), Region: searchHistoryRegion}
	}
	rightFromList := focusDestination{Region: searchResultsRegion}
	if current.ResultCount() == 0 {
		rightFromList = focusDestination{Focus: searchKindFocus(activeKind), Region: searchKindsRegion}
	}
	p.region(searchSuggestRegion, focusEdges{
		app.Up:    aboveList,
		app.Right: rightFromList,
	})
	p.region(searchHotRegion, focusEdges{
		app.Up:    aboveList,
		app.Right: rightFromList,
	})
	p.region(searchSortRegion, focusEdges{
		app.Left: {Focus: searchInputFocus},
		app.Down: {Focus: searchKindFocus(activeKind), Region: searchKindsRegion},
	})
	kindsEdges := focusEdges{
		app.Left: {Focus: searchInputFocus},
	}
	if hasFilters {
		kindsEdges[app.Up] = focusDestination{Focus: searchFilterFocus}
	}
	if current.ResultCount() > 0 {
		kindsEdges[app.Down] = focusDestination{Region: searchResultsRegion}
	} else {
		kindsEdges[app.Down] = focusDestination{Region: searchStatusRegion}
	}
	p.region(searchKindsRegion, kindsEdges)
	leftFromResults := focusDestination{Focus: searchInputFocus}
	if len(suggestions) != 0 {
		leftFromResults = focusDestination{Focus: searchSuggestFocus(0), Region: searchSuggestRegion}
	} else if len(hot) != 0 {
		leftFromResults = focusDestination{Focus: searchHotFocus(0), Region: searchHotRegion}
	} else if len(history) != 0 {
		leftFromResults = focusDestination{Focus: searchHistoryFocus(0), Region: searchHistoryRegion}
	}
	p.region(searchResultsRegion, focusEdges{
		app.Left: leftFromResults,
		app.Up:   {Focus: searchKindFocus(activeKind), Region: searchKindsRegion},
		app.Down: {Region: searchStatusRegion},
	})
	upFromStatus := focusDestination{Focus: searchKindFocus(activeKind), Region: searchKindsRegion}
	if current.ResultCount() > 0 {
		upFromStatus = focusDestination{Region: searchResultsRegion}
	}
	p.region(searchStatusRegion, focusEdges{
		app.Left: {Focus: searchInputFocus},
		app.Up:   upFromStatus,
	})
}

func (p *PageContext) restoreSearchFocus(current app.SearchSnapshot, history, suggestions, hot []string, activeKind provider.SearchKind, hasFilters bool) {
	if !p.active || p.host.search.editing {
		return
	}
	focus := p.State.Focus
	switch {
	case focus == searchFilterFocus && !hasFilters:
		p.State.Focus = searchKindFocus(activeKind)
	case focus == searchRetryFocus && current.Phase != app.CatalogFailed && current.Phase != app.CatalogCanceled:
		p.State.Focus = searchInputFocus
	case strings.HasPrefix(string(focus), "search/history/"):
		if idx, err := strconv.Atoi(strings.TrimPrefix(string(focus), "search/history/")); err != nil || idx < 0 || idx >= len(history) {
			p.State.Focus = searchInputFocus
		}
	case strings.HasPrefix(string(focus), "search/suggest/"):
		if idx, err := strconv.Atoi(strings.TrimPrefix(string(focus), "search/suggest/")); err != nil || idx < 0 || idx >= len(suggestions) {
			p.State.Focus = searchInputFocus
		}
	case strings.HasPrefix(string(focus), "search/hot/"):
		if idx, err := strconv.Atoi(strings.TrimPrefix(string(focus), "search/hot/")); err != nil || idx < 0 || idx >= len(hot) {
			p.State.Focus = searchInputFocus
		}
	case strings.HasPrefix(string(focus), "search/item/"):
		found := false
		if activeKind != provider.SearchCreator {
			for _, item := range current.Page.Items {
				if searchItemFocus(item.Ref) == focus {
					found = true
					p.host.search.lastResult = focus
					break
				}
			}
		}
		if !found {
			p.State.Focus = searchKindFocus(activeKind)
		}
	case strings.HasPrefix(string(focus), "search/creator/"):
		found := false
		if activeKind == provider.SearchCreator {
			for _, creator := range current.Page.Creators {
				if searchCreatorFocus(creator.Creator.ID) == focus {
					found = true
					p.host.search.lastResult = focus
					break
				}
			}
		}
		if !found {
			p.State.Focus = searchKindFocus(activeKind)
		}
	}
}

func (p *PageContext) searchBox(c *ui.Context, current app.SearchSnapshot) {
	colors, d, text := p.Theme.Colors, p.uiColors(), p.Assets.Text.Search
	state := &p.host.search
	if state.editing {
		ui.Row(c.Key("search-box-edit")).Width(searchLeftWidth).Height(72).Padding(0, 20).Gap(15).
			AlignItems(ui.Center).Radius(14).Background(paint(colors.Surface.Input)).
			Border(1, paint(colors.Focus.Ring)).Shadow(0, 0, 0, 3, paint(colors.Focus.Ring)).Children(func() {
			ui.Icon(c, p.Assets.Icon("search")).Size(26, 26).TextColor(paint(colors.Text.Muted))
			field := ui.TextInput(c.Key("search-input"), &state.input).Grow(1).MinWidth(0).Height(50).
				Padding(0, 0).Border(0, ui.Transparent).Background(ui.Transparent).
				FontSize(27).TextColor(paint(colors.Text.Primary)).
				Label(text.Placeholder).Placeholder(text.Placeholder).PlaceholderColor(paint(colors.Text.Muted)).
				MaxLength(searchQueryMaxLength).ReturnKey(ui.ReturnSearch).
				OnChange(func() {
					if p.host.Search != nil {
						p.host.Search.Suggest(state.input)
					}
				}).
				OnTextKey(func(key ui.TextKey) bool {
					switch key.Key {
					case ui.KeyEnter:
						query := key.Text()
						state.editing = false
						state.input = strings.TrimSpace(query)
						p.host.moveFocus(searchInputFocus, app.Left, false)
						p.host.Search.Submit(query)
						return true
					case ui.KeyEscape:
						state.editing = false
						p.host.moveFocus(searchInputFocus, app.Left, false)
						return true
					default:
						return false
					}
				})
			if p.active {
				field.FocusRing(false)
				field.FocusBind(&p.State.Focus, searchInputEdit)
				field.Focus(ui.FocusOptions{PreventScroll: true})
			}
		})
		return
	}

	display := current.Request.Query
	ink := paint(colors.Text.Primary)
	if display == "" {
		display = text.Placeholder
		ink = paint(colors.Text.Muted)
	}
	focused := p.paintFocus() == searchInputFocus
	border := d.strongBorder
	if focused {
		border = paint(colors.Focus.Ring)
	}
	button := ui.Button(c.Key(string(searchInputFocus)), "").Label(display).
		Width(searchLeftWidth).Height(72).Padding(0, 20).Radius(14).
		Background(d.subtleFill).Border(1, border).TextColor(ink).
		OnClick(func() {
			p.dispatch(func() {
				state.input = current.Request.Query
				state.editing = true
				p.State.Focus = searchInputEdit
			})
		})
	if focused {
		button.Shadow(0, 0, 0, 3, paint(colors.Focus.Ring))
	}
	p.bind(button, searchInputFocus, searchInputRegion)
	button.Children(func() {
		ui.Row(c).Fill().Gap(15).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, p.Assets.Icon("search")).Size(26, 26).TextColor(paint(colors.Text.Muted))
			ui.Text(c, display).Grow(1).MinWidth(0).SingleLine().FontSize(27).TextColor(ink)
		})
	})
}

func (p *PageContext) searchHistorySection(c *ui.Context, history []string) {
	colors, d, text := p.Theme.Colors, p.uiColors(), p.Assets.Text.Search
	ui.Column(c.Key("search-history")).FillWidth().Margin(23, 0, 0, 0).Children(func() {
		ui.Text(c, text.History).FontSize(16).TextColor(paint(colors.Text.Muted)).Margin(0, 0, 15, 0)
		ui.Column(c).FillWidth().Gap(10).Children(func() {
			for row := 0; row < len(history); row += searchHistoryPerRow {
				end := min(row+searchHistoryPerRow, len(history))
				ui.Row(c.Key(row)).Gap(10).Children(func() {
					for i := row; i < end; i++ {
						term := history[i]
						id := searchHistoryFocus(i)
						focused := p.paintFocus() == id
						bg, fg, bd := d.subtleFill, paint(colors.Text.Secondary), paint(colors.Border.Subtle)
						if focused {
							bg, fg, bd = paint(colors.Focus.Background), paint(colors.Focus.Foreground), ui.Transparent
						}
						chip := ui.Button(c.Key(string(id)), "").Label(term).
							Height(42).MaxWidth(143).Padding(0, 17).Radius(21).
							Background(bg).TextColor(fg).Border(1, bd).Center().Shrink(0).
							OnClick(func() {
								p.dispatch(func() {
									p.host.Search.Submit(term)
								})
							})
						p.animateFocus(chip, bg, fg)
						p.bind(chip, id, searchHistoryRegion)
						chip.Children(func() {
							ui.Text(c, term).SingleLine().FontSize(17)
						})
					}
				})
			}
		})
	})
}

func (p *PageContext) searchKeywordList(c *ui.Context, heading string, keywords []string, region string, idOf func(int) app.FocusID) {
	colors := p.Theme.Colors
	ui.Column(c.Key(region)).FillWidth().Margin(31, 0, 0, 0).Children(func() {
		ui.Text(c, heading).FontSize(16).TextColor(paint(colors.Text.Muted)).Margin(0, 0, 10, 0)
		ui.Column(c).FillWidth().Gap(4).Children(func() {
			for i, term := range keywords {
				id := idOf(i)
				focused := p.paintFocus() == id
				bg, fg := ui.Transparent, paint(colors.Text.Secondary)
				if focused {
					bg, fg = paint(colors.Focus.Background), paint(colors.Focus.Foreground)
				}
				row := ui.Button(c.Key(string(id)), "").Label(term).
					FillWidth().Height(52).Padding(0, 12).Radius(8).
					Background(bg).TextColor(fg).Border(0, ui.Transparent).
					OnClick(func() {
						p.dispatch(func() {
							p.host.Search.Submit(term)
						})
					})
				p.animateFocus(row, bg, fg)
				p.bind(row, id, region)
				row.Children(func() {
					ui.Row(c).Fill().Gap(13).AlignItems(ui.Center).Children(func() {
						ui.Icon(c, p.Assets.Icon("search")).Size(18, 18)
						ui.Text(c, term).Grow(1).MinWidth(0).SingleLine().FontSize(21)
					})
				})
			}
		})
	})
}

func (p *PageContext) searchSideHeader(c *ui.Context, current app.SearchSnapshot, hasFilters bool) {
	text, colors, d := p.Assets.Text.Search, p.Theme.Colors, p.uiColors()
	heading := text.PopularHeading
	if current.Request.Query != "" {
		heading = fmt.Sprintf(text.ResultsHeading, current.DisplayTotal())
	}
	ui.Row(c.Key("search-title-line")).FillWidth().Height(43).Margin(0, 0, 19, 0).AlignItems(ui.Center).Children(func() {
		ui.Text(c, heading).FontSize(25).FontWeight(500).SingleLine().Role(ui.RoleHeading).Level(2)
		ui.Spacer(c).Grow(1)
		if hasFilters {
			label := p.searchSortLabel(current.Request.Sort)
			if current.Request.Duration != "" && current.Request.Duration != provider.SearchDurationAll {
				label += " \u00b7 " + p.searchDurationLabel(current.Request.Duration)
			}
			focused := p.paintFocus() == searchFilterFocus
			bg, fg, bd := d.subtleFill, paint(colors.Text.Secondary), paint(colors.Border.Subtle)
			if focused {
				bg, fg, bd = paint(colors.Focus.Background), paint(colors.Focus.Foreground), ui.Transparent
			}
			btn := ui.Button(c.Key(string(searchFilterFocus)), label).Label(label).
				Height(43).Padding(0, 17).Radius(22).FontSize(17).Center().Shrink(0).
				Background(bg).TextColor(fg).Border(1, bd).
				OnClick(func() { p.dispatch(p.openSearchFilters) })
			p.animateFocus(btn, bg, fg)
			p.bind(btn, searchFilterFocus, searchSortRegion)
		}
	})
}

func (p *PageContext) searchKindTabs(c *ui.Context, kinds []provider.SearchKind, active provider.SearchKind) {
	colors, d := p.Theme.Colors, p.uiColors()
	focusInRow := strings.HasPrefix(string(p.paintFocus()), "search/kind/")
	ui.Row(c.Key("search-kinds")).FillWidth().Height(43).Margin(0, 0, 22, 0).Gap(10).AlignItems(ui.Center).Children(func() {
		for _, kind := range kinds {
			id := searchKindFocus(kind)
			label := p.searchKindLabel(kind)
			selected := kind == active
			focused := p.paintFocus() == id
			bg, fg, bd := d.subtleFill, paint(colors.Text.Secondary), paint(colors.Border.Subtle)
			if selected {
				if focusInRow && !focused {
					bg, fg, bd = paint(colors.Selected.Background), paint(colors.Selected.Foreground), ui.Transparent
				} else {
					bg, fg, bd = withAlpha(colors.Focus.Background, 0xee), paint(colors.Focus.Foreground), ui.Transparent
				}
			}
			if focused {
				bg, fg, bd = paint(colors.Focus.Background), paint(colors.Focus.Foreground), ui.Transparent
			}
			chip := ui.Button(c.Key(string(id)), label).Label(label).Role(ui.RoleTab).AccessSelected(selected).
				Height(43).Padding(0, 17).Radius(22).FontSize(17).Center().Shrink(0).
				Background(bg).TextColor(fg).Border(1, bd).
				OnClick(func() {
					p.dispatch(func() {
						p.host.Search.SelectKind(kind)
					})
				})
			p.animateFocus(chip, bg, fg)
			p.bind(chip, id, searchKindsRegion)
		}
	})
}

func (p *PageContext) searchResultsBody(c *ui.Context, current app.SearchSnapshot, activeKind provider.SearchKind, rightWidth, cardWidth, rowHeight, stride float32, totalRows, slots, firstRow, endRow, focusedRow int) {
	text, colors := p.Assets.Text, p.Theme.Colors
	count := current.ResultCount()
	if count == 0 {
		switch current.Phase {
		case app.CatalogIdle, app.CatalogLoading:
			ui.Column(c.Key("search-loading")).FillWidth().Padding(90, 20, 40, 20).AlignItems(ui.Center).Gap(20).Children(func() {
				ui.Text(c, text.LoadingTitle).FontSize(25).TextColor(paint(colors.Text.Secondary))
			})
		case app.CatalogFailed:
			note := p.authError(current.Error)
			if current.Error == provider.RateLimited {
				note = text.Home.RateLimited
			}
			ui.Column(c.Key("search-failed")).FillWidth().Padding(90, 20, 40, 20).AlignItems(ui.Center).Gap(18).Children(func() {
				ui.Text(c, text.FailedTitle).FontSize(25).TextColor(paint(colors.Text.Secondary))
				if note != "" {
					ui.Text(c, note).FontSize(19).TextColor(paint(colors.Text.Muted))
				}
				p.button(c, text.Retry, searchRetryFocus, searchStatusRegion, p.host.Search.Retry).AlignSelf(ui.Center)
			})
		case app.CatalogCanceled:
			ui.Column(c.Key("search-canceled")).FillWidth().Padding(90, 20, 40, 20).AlignItems(ui.Center).Gap(18).Children(func() {
				ui.Text(c, text.CanceledTitle).FontSize(25).TextColor(paint(colors.Text.Secondary))
				p.button(c, text.Retry, searchRetryFocus, searchStatusRegion, p.host.Search.Retry).AlignSelf(ui.Center)
			})
		default:
			ui.Column(c.Key("search-empty")).FillWidth().Padding(90, 20, 40, 20).AlignItems(ui.Center).Gap(14).Children(func() {
				ui.Icon(c, p.Assets.Icon("library")).Size(65, 65).TextColor(paint(colors.Text.Muted))
				ui.Text(c, text.Search.NoResults).Margin(16, 0, 0, 0).FontSize(25).TextColor(paint(colors.Text.Secondary))
				ui.Text(c, text.Search.NoResultsHint).FontSize(20).TextColor(paint(colors.Text.Muted))
			})
		}
		return
	}

	gridHeight := max(0, float32(totalRows)*stride-searchGridGap)
	paging := current.CanLoadMore() || (current.Appending && current.Phase == app.CatalogLoading)
	appendFailed := current.Appending && (current.Phase == app.CatalogFailed || current.Phase == app.CatalogCanceled)
	lastRow := (count - 1) / 2

	ui.Box(c.Key("search-grid")).Width(rightWidth).Height(gridHeight).Children(func() {
		drawRow := func(row int) {
			start := row * 2
			end := min(start+2, slots)
			atTail := row == lastRow
			ui.Row(c.Key("search-row/"+strconv.Itoa(row))).Absolute().Left(0).Top(float32(row)*stride).
				Width(rightWidth).Height(rowHeight).Gap(searchGridGap).Children(func() {
				for index := start; index < end; index++ {
					var dir func(app.Direction) bool
					if atTail && (paging || appendFailed) {
						dir = func(key app.Direction) bool {
							if key != app.Down {
								return false
							}
							if paging {
								p.host.Search.LoadMore()
								return true
							}
							p.host.moveFocus(searchRetryFocus, app.Down, true)
							return true
						}
					}
					if index >= count {
						p.catalogSkeleton(c, catalogCardSize{width: cardWidth, height: rowHeight, cover: min(searchCoverHeight, rowHeight-60)})
						continue
					}
					if activeKind == provider.SearchCreator {
						p.searchCreatorCard(c, current.Page.Creators[index], cardWidth, dir)
					} else {
						p.searchMediaCard(c, current.Page.Items[index], cardWidth, dir)
					}
				}
			})
		}
		for row := firstRow; row < endRow; row++ {
			drawRow(row)
		}
		if focusedRow >= 0 && (focusedRow < firstRow || focusedRow >= endRow) && focusedRow < totalRows {
			drawRow(focusedRow)
		}
	})

	if appendFailed {
		ui.Row(c.Key("search-append-failed")).FillWidth().Margin(24, 0, 0, 0).Gap(18).AlignItems(ui.Center).Children(func() {
			p.button(c, text.Retry, searchRetryFocus, searchStatusRegion, p.host.Search.Retry)
			label := text.Search.MoreFailed
			if current.Error == provider.RateLimited {
				label = text.Home.RateLimited
			}
			ui.Text(c, label).FontSize(19).TextColor(paint(colors.Status.Error.Foreground))
		})
	}
}

func (p *PageContext) searchMediaCard(c *ui.Context, item provider.MediaItem, width float32, direction func(app.Direction) bool) {
	id := searchItemFocus(item.Ref)
	size := catalogCardSize{width: width, height: searchCardHeight, cover: searchCoverHeight, title: 21}
	if (item.Ref.Kind == provider.Video || item.Ref.Kind == provider.Series || item.Ref.Kind == provider.Course) && p.canOpenVideoDetail(item.Ref) {
		p.catalogCard(c, id, searchResultsRegion, item, size, direction)
		return
	}
	colors, d := p.Theme.Colors, p.uiColors()
	card := ui.Button(c.Key(string(id)), "").Size(size.width, size.height).Shrink(0).Padding(0, 0).
		Border(0, ui.Transparent).Background(ui.Transparent).Label(item.Title).
		OnClick(func() {
			p.dispatch(func() {
				if item.Ref.Kind == provider.Live && p.canPlay(item.Ref) {
					p.openLivePlayback(item, false)
					return
				}
				p.host.notice = p.Assets.Text.Search.DetailPending
			})
		})
	p.bindDirection(card, id, searchResultsRegion, direction)
	card.Children(func() {
		ui.Column(c).Fill().Children(func() {
			scale, stroke := float32(1), ui.Transparent
			if p.paintFocus() == id {
				scale, stroke = 1.035, paint(colors.Focus.Ring)
			}
			cover := ui.Box(c.Key("card-cover")).FillWidth().Height(size.cover).Radius(13).Clip().Background(d.coverFill)
			cover.Children(func() {
				if image := p.catalogCover(item.CoverURL); image != nil {
					photo := ui.Image(c.Key("card-photo"), image).Fill().Fit(ui.Cover)
					zoom := photo.AnimateTo("focus-zoom", scale, ui.Animation{Duration: focusDuration})
					photo.Scale(zoom, zoom)
					ui.Box(c).Absolute().Left(0).Top(0).Fill().LinearGradient(ui.LinearGradient{
						Angle: 0, Stops: []ui.GradientStop{{Color: d.coverShade}, {Color: ui.Transparent, At: 0.55}},
					})
				}
				if item.Quality != "" {
					badgeBg, badgeFg := d.badgeFill, paint(colors.Playback.Foreground)
					if item.Ref.Kind == provider.Live {
						badgeBg, badgeFg = paint(colors.Accent.Primary), paint(colors.Accent.Foreground)
					}
					ui.Text(c, item.Quality).Absolute().Left(12).Top(12).ZIndex(1).Padding(3, 7).
						FontSize(11).LetterSpacing(1).Radius(4).Border(1, d.badgeBorder).
						Background(badgeBg).TextColor(badgeFg)
				}
				if item.DurationSeconds != nil {
					ui.Text(c, catalogDuration(*item.DurationSeconds)).Absolute().Right(13).Bottom(12).
						ZIndex(1).FontSize(14).LetterSpacing(0.7).TextColor(paint(colors.Playback.Foreground))
				}
				ring := ui.Box(c.Key("card-focus")).Absolute().Left(0).Top(0).Fill().ZIndex(1).Radius(13)
				ring.Border(4, ring.AnimateColor("focus-ring", stroke, ui.Animation{Duration: focusDuration, NonMotion: true}))
			})
			ui.Text(c, item.Title).FillWidth().Margin(16, 0, 9, 0).FontSize(size.title).FontWeight(500).
				LetterSpacing(0.3).SingleLine()
			if item.Creator != nil || item.Views != nil {
				p.catalogCardMetadata(c, item, size.width)
			} else if item.Subtitle != "" {
				ui.Text(c, item.Subtitle).FillWidth().SingleLine().FontSize(15).TextColor(paint(colors.Text.Secondary))
			}
		})
	})
}

func (p *PageContext) searchCreatorCard(c *ui.Context, item provider.SearchCreatorItem, width float32, direction func(app.Direction) bool) {
	colors, d, text := p.Theme.Colors, p.uiColors(), p.Assets.Text
	id := searchCreatorFocus(item.Creator.ID)
	focused := p.paintFocus() == id
	bg := d.subtleFill
	if focused {
		bg = d.subtleHoverFill
	}
	card := ui.Button(c.Key(string(id)), "").Label(item.Creator.Name).
		Size(width, searchCreatorHeight).Shrink(0).Padding(24, 24).Radius(20).
		Background(bg).Border(1, paint(colors.Border.Subtle)).
		OnClick(func() {
			p.dispatch(func() {
				if p.canOpenCreator(&item.Creator) {
					p.openCreator(item.Creator)
					return
				}
				p.host.notice = text.Search.DetailPending
			})
		})
	p.bindDirection(card, id, searchResultsRegion, direction)
	card.Children(func() {
		ui.Column(c).Fill().AlignItems(ui.Center).Justify(ui.Center).Gap(14).Children(func() {
			avatar := ui.Box(c).Size(95, 95).Radius(47.5).Background(paint(colors.Surface.Raised)).Center()
			if focused {
				avatar.Shadow(0, 0, 0, 3, paint(colors.Focus.Ring))
			}
			avatar.Children(func() {
				ui.Box(c).Fill().Radius(47.5).Clip().Center().Children(func() {
					if img := p.catalogCover(item.Creator.AvatarURL); img != nil {
						ui.Image(c, img).Fill().Fit(ui.Cover)
					} else {
						initial := "?"
						if r, _ := utf8.DecodeRuneInString(strings.TrimSpace(item.Creator.Name)); r != utf8.RuneError {
							initial = string(r)
						}
						ui.Text(c, initial).FontSize(34).FontWeight(500).TextColor(paint(colors.Text.Primary))
					}
				})
			})
			ui.Text(c, item.Creator.Name).MaxWidth(width-48).SingleLine().FontSize(22).FontWeight(500).
				TextColor(paint(colors.Text.Primary))
			meta := p.searchCreatorMeta(item)
			ui.Text(c, meta).MaxWidth(width-48).SingleLine().FontSize(16).TextColor(paint(colors.Text.Secondary))
		})
	})
}

func (p *PageContext) searchCreatorMeta(item provider.SearchCreatorItem) string {
	text := p.Assets.Text
	var parts []string
	if item.Followers != nil {
		parts = append(parts, catalogViews(*item.Followers, text.Home)+text.Search.Followers)
	}
	if item.Videos != nil {
		parts = append(parts, fmt.Sprintf(text.Search.VideoCount, *item.Videos))
	}
	if len(parts) != 0 {
		return strings.Join(parts, " \u00b7 ")
	}
	if item.Description != "" {
		return item.Description
	}
	return text.Search.CreatorVideos
}

func (p *PageContext) searchFocusedRow(current app.SearchSnapshot, activeKind provider.SearchKind) int {
	focus := p.State.Focus
	if activeKind == provider.SearchCreator {
		for index, creator := range current.Page.Creators {
			if searchCreatorFocus(creator.Creator.ID) == focus {
				return index / 2
			}
		}
		return -1
	}
	for index, item := range current.Page.Items {
		if searchItemFocus(item.Ref) == focus {
			return index / 2
		}
	}
	return -1
}

func (p *PageContext) prepareSearchArtwork(current app.SearchSnapshot, activeKind provider.SearchKind, firstRow, endRow, focusedRow int) {
	if p.host.Catalog == nil {
		return
	}
	locations := make([]string, 0, 24)
	seen := make(map[string]struct{}, 24)
	add := func(location string) {
		if location == "" {
			return
		}
		if _, ok := seen[location]; !ok {
			seen[location] = struct{}{}
			locations = append(locations, location)
		}
	}
	addRow := func(row int, avatars bool) {
		start := row * 2
		if activeKind == provider.SearchCreator {
			if !avatars {
				for i := start; i < min(start+2, len(current.Page.Creators)); i++ {
					add(current.Page.Creators[i].Creator.AvatarURL)
				}
			}
			return
		}
		for i := start; i < min(start+2, len(current.Page.Items)); i++ {
			item := current.Page.Items[i]
			if !avatars {
				add(item.CoverURL)
			} else if item.Creator != nil {
				add(item.Creator.AvatarURL)
			}
		}
	}
	for row := firstRow; row < endRow; row++ {
		addRow(row, false)
	}
	if focusedRow >= 0 && (focusedRow < firstRow || focusedRow >= endRow) {
		addRow(focusedRow, false)
	}
	for row := firstRow; row < endRow; row++ {
		addRow(row, true)
	}
	if focusedRow >= 0 && (focusedRow < firstRow || focusedRow >= endRow) {
		addRow(focusedRow, true)
	}
	p.host.Catalog.PrepareArtwork(locations)
	p.pruneCatalogBitmaps()
}
