package tvui

import (
	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/provider"
)

const (
	searchFilterApply        app.FocusID = "search-filters/apply"
	searchFilterSortRegion               = "search-filters/sort"
	searchFilterDurRegion                = "search-filters/duration"
	searchFilterActionRegion             = "search-filters/actions"
)

var (
	searchSortOptions = [...]provider.SearchSort{
		provider.SearchSortDefault,
		provider.SearchSortNewest,
		provider.SearchSortViews,
	}
	searchDurationOptions = [...]provider.SearchDuration{
		provider.SearchDurationAll,
		provider.SearchDurationShort,
		provider.SearchDurationMedium,
		provider.SearchDurationLong,
	}
)

type searchFilterDraft struct {
	sort     provider.SearchSort
	duration provider.SearchDuration
}

type searchFiltersPage struct{}

func searchFilterSortFocus(sort provider.SearchSort) app.FocusID {
	if sort == "" {
		sort = provider.SearchSortDefault
	}
	return app.FocusID("search-filters/sort/" + string(sort))
}

func searchFilterDurationFocus(duration provider.SearchDuration) app.FocusID {
	if duration == "" {
		duration = provider.SearchDurationAll
	}
	return app.FocusID("search-filters/duration/" + string(duration))
}

func (p *PageContext) searchSortLabel(sort provider.SearchSort) string {
	text := p.Assets.Text.Search
	switch sort {
	case provider.SearchSortNewest:
		return text.SortNewest
	case provider.SearchSortViews:
		return text.SortViews
	default:
		return text.SortDefault
	}
}

func (p *PageContext) searchDurationLabel(duration provider.SearchDuration) string {
	text := p.Assets.Text.Search
	switch duration {
	case provider.SearchDurationShort:
		return text.DurationShort
	case provider.SearchDurationMedium:
		return text.DurationMedium
	case provider.SearchDurationLong:
		return text.DurationLong
	default:
		return text.DurationAll
	}
}

func (p *PageContext) openSearchFilters() {
	current := p.host.Search.Snapshot().Request
	sort, duration := current.Sort, current.Duration
	if sort == "" {
		sort = provider.SearchSortDefault
	}
	if duration == "" {
		duration = provider.SearchDurationAll
	}
	p.host.search.filterDraft = searchFilterDraft{sort: sort, duration: duration}
	if p.State.Push(app.Route{Page: app.SearchFilters}) {
		p.State.Focus = searchFilterSortFocus(sort)
	}
}

func (searchFiltersPage) Render(c *ui.Context, p *PageContext) {
	text, colors := p.Assets.Text.Search, p.Theme.Colors
	draft := &p.host.search.filterDraft
	if draft.sort == "" {
		draft.sort = provider.SearchSortDefault
	}
	if draft.duration == "" {
		draft.duration = provider.SearchDurationAll
	}
	p.region(searchFilterSortRegion, focusEdges{
		app.Down: {Focus: searchFilterDurationFocus(draft.duration), Region: searchFilterDurRegion},
	})
	p.region(searchFilterDurRegion, focusEdges{
		app.Up:   {Focus: searchFilterSortFocus(draft.sort), Region: searchFilterSortRegion},
		app.Down: {Focus: searchFilterApply, Region: searchFilterActionRegion},
	})
	p.region(searchFilterActionRegion, focusEdges{
		app.Up: {Focus: searchFilterDurationFocus(draft.duration), Region: searchFilterDurRegion},
	})

	ui.Column(c).Fill().Lang("zh-CN").Font("system-ui, PingFang SC").Children(func() {
		ui.Text(c, text.FiltersTitle).FontSize(36).FontWeight(500).Role(ui.RoleHeading).Level(2)
		ui.Text(c, text.SortLabel).Margin(30, 0, 18, 0).FontSize(21).FontWeight(400).TextColor(paint(colors.Text.Secondary))
		ui.Row(c).Gap(13).Children(func() {
			for _, sort := range searchSortOptions {
				p.searchFilterChip(c, p.searchSortLabel(sort), searchFilterSortFocus(sort), searchFilterSortRegion, draft.sort == sort, func() {
					draft.sort = sort
				})
			}
		})
		ui.Text(c, text.DurationLabel).Margin(30, 0, 18, 0).FontSize(21).FontWeight(400).TextColor(paint(colors.Text.Secondary))
		ui.Row(c).Gap(13).Children(func() {
			for _, duration := range searchDurationOptions {
				p.searchFilterChip(c, p.searchDurationLabel(duration), searchFilterDurationFocus(duration), searchFilterDurRegion, draft.duration == duration, func() {
					draft.duration = duration
				})
			}
		})
		ui.Spacer(c).Grow(1)
		ui.Row(c).Margin(32, 0, 0, 0).Gap(12).Children(func() {
			p.styledButton(c, text.ApplyFilters, "", searchFilterApply, searchFilterActionRegion, func() {
				applied := *draft
				p.State.Back()
				p.host.Search.ApplyFilters(applied.sort, applied.duration)
			}, p.dialogButtonAppearance())
		})
	})
}

func (p *PageContext) searchFilterChip(c *ui.Context, label string, id app.FocusID, region string, selected bool, action func()) {
	colors, d := p.Theme.Colors, p.uiColors()
	focused := p.paintFocus() == id
	background, ink, border := d.subtleFill, paint(colors.Text.Secondary), paint(colors.Border.Subtle)
	if selected {
		background = withAlpha(colors.Accent.Primary, 0x25)
		ink = paint(colors.Accent.Soft)
		border = withAlpha(colors.Accent.Primary, 0x80)
	}
	if focused {
		background = paint(colors.Focus.Background)
		ink = paint(colors.Focus.Foreground)
		border = ui.Transparent
	}
	button := ui.Button(c.Key(string(id)), label).Label(label).Role(ui.RoleRadio).AccessSelected(selected).
		Height(51).Padding(0, 20).Radius(26).FontSize(20).Center().Shrink(0).
		Background(background).TextColor(ink).Border(1, border).
		OnClick(func() { p.dispatch(action) })
	p.animateFocus(button, background, ink)
	p.bind(button, id, region)
}
