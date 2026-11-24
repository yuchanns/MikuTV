package tvui

import (
	"math"

	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/provider"
)

type catalogGridMetrics struct {
	columns, podium                                      int
	left, gap, heading, height, rowGap, cover, cardWidth float32
}

var (
	discoveryGridMetrics = catalogGridMetrics{
		columns: 4, left: 103, gap: 30, height: 284, rowGap: 35, cover: 208,
	}
	rankingGridMetrics = catalogGridMetrics{
		columns: 2, podium: 3, left: 103, gap: 35, heading: 369, height: 115, rowGap: 5, cover: 72,
	}
)

func homeGridMetrics(viewport float32) catalogGridMetrics {
	const left = 103
	const stride = homeShelfCoverWidth + homeShelfGap
	columns := max(1, int(math.Ceil(float64((viewport-left)/stride))))
	return catalogGridMetrics{
		columns: columns, left: left, gap: homeShelfGap, heading: homeShelfTop + homeShelfHeading,
		height: homeCardHeight, rowGap: homeShelfGap, cover: homeCoverHeight, cardWidth: homeShelfCoverWidth,
	}
}

func (grid catalogGridMetrics) rows(slots int) int {
	return (max(0, slots-grid.podium) + grid.columns - 1) / grid.columns
}

func (grid catalogGridMetrics) stride() float32 { return grid.height + grid.rowGap }

func (grid catalogGridMetrics) width(viewport float32) float32 {
	if grid.cardWidth != 0 {
		return grid.cardWidth
	}
	return max(1, (viewport-grid.left-homeGridRight-float32(grid.columns-1)*grid.gap)/float32(grid.columns))
}

func (grid catalogGridMetrics) slots(items int, loading bool) int {
	if loading {
		return max(items, grid.podium) + grid.columns - max(0, items-grid.podium)%grid.columns
	}
	return items
}

func (grid catalogGridMetrics) firstItem(row int) int { return grid.podium + row*grid.columns }

type catalogPosition struct {
	section int
	item    int
}

type catalogSection struct {
	group provider.Collection
	grid  catalogGridMetrics
	top   float32
	cards []app.FocusID
}

type catalogLayout struct {
	revision uint64
	grid     catalogGridMetrics
	top      float32
	end      float32
	sections []catalogSection
	byFocus  map[app.FocusID]catalogPosition
}

func (layout *catalogLayout) update(snapshot app.CatalogSnapshot, top float32, grid catalogGridMetrics) {
	if layout.byFocus != nil && layout.revision == snapshot.PageRevision && layout.top == top && layout.grid == grid {
		return
	}
	layout.revision, layout.grid, layout.top, layout.end = snapshot.PageRevision, grid, top, top
	layout.sections = layout.sections[:0]
	if layout.byFocus == nil {
		layout.byFocus = make(map[app.FocusID]catalogPosition)
	} else {
		clear(layout.byFocus)
	}
	for _, group := range snapshot.Page.Collections {
		if len(group.Items) == 0 {
			continue
		}
		section := catalogSection{group: group, grid: grid, top: layout.end, cards: make([]app.FocusID, len(group.Items))}
		for index, item := range group.Items {
			focus := catalogCardFocus(group.ID, item.Ref)
			section.cards[index] = focus
			layout.byFocus[focus] = catalogPosition{section: len(layout.sections), item: index}
		}
		layout.sections = append(layout.sections, section)
		layout.end += section.height(len(section.cards))
	}
}

func (section catalogSection) height(slots int) float32 {
	grid := section.grid
	return grid.heading + max(0, float32(grid.rows(slots))*grid.stride()-grid.rowGap)
}

func (section catalogSection) visibleRows(scroll, viewport float32, slots, overscan int) (first, end int) {
	grid := section.grid
	return visibleItemRange(scroll-section.top-grid.heading, viewport, grid.stride(), grid.rows(slots), overscan)
}

func visibleItemRange(offset, viewport, stride float32, count, overscan int) (first, end int) {
	above := int(math.Floor(float64(offset / stride)))
	below := int(math.Floor(float64((offset + viewport) / stride)))
	return min(count, max(0, above-overscan)), min(count, max(0, below+1+overscan))
}
