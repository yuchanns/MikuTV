package tvui

import (
	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/provider"
)

type catalogActionPage struct{}

type catalogAction struct {
	title string
	items []provider.MediaItem
	list  ui.ListState
}

func (p *PageContext) openCatalogAction(title string, items []provider.MediaItem) {
	if p.State.Push(app.Route{Page: app.HomeAction}) {
		p.host.home.action = catalogAction{title: title, items: items}
	}
}

func (catalogActionPage) Render(c *ui.Context, p *PageContext) {
	action := &p.host.home.action
	text := p.Assets.Text
	const region = "catalog-action"
	p.region(region, nil)
	ui.Row(c).FillWidth().Gap(18).AlignItems(ui.Center).Children(func() {
		ui.Text(c, action.title).FontSize(35).FontWeight(600)
		ui.Spacer(c).Grow(1)
		p.button(c, text.Close, app.CloseModal, region, func() { p.State.Back() })
	})
	if len(action.items) != 0 {
		action.list.Key = func(index int) any { return action.items[index].Ref }
		ui.List(c.Key("shelf-results"), &action.list, len(action.items)).FillWidth().Grow(1).
			Padding(0, 0, focusBottomMargin, 0).Rows(func(row ui.ListRow) {
			c, item := row.Context, action.items[row.Index]
			if p.canOpenVideoDetail(item.Ref) {
				id := catalogCardFocus("shelf-results", item.Ref)
				p.button(c, item.Title, id, region, func() { p.openVideoDetail(item) }).Margin(0, 0, 13, 0)
			} else {
				ui.Text(c, item.Title).FontSize(22).Padding(14, 27).Margin(0, 0, 13, 0)
			}
		})
		return
	}
	ui.Column(c).Fill().Center().Gap(20).Children(func() {
		p.emptyDescription(c, text.EmptyTitle, text.EmptyNote)
	})
}
