package tvui

import (
	"fmt"

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
	meta := ""
	if len(action.items) != 0 {
		meta = fmt.Sprintf(text.Modal.ItemsCount, len(action.items))
	}
	ui.Column(c).Fill().Lang("zh-CN").Font("system-ui, PingFang SC").Children(func() {
		p.drawerHeader(c, "COLLECTION", action.title, meta, "")
		if len(action.items) != 0 {
			action.list.Key = func(index int) any { return action.items[index].Ref }
			ui.List(c.Key("shelf-results"), &action.list, len(action.items)).FillWidth().Grow(1).
				Padding(0, 0, focusBottomMargin, 0).Rows(func(row ui.ListRow) {
				rc, item := row.Context, action.items[row.Index]
				sub := ""
				if item.Creator != nil {
					sub = item.Creator.Name
				}
				if p.canOpenVideoDetail(item.Ref) {
					id := catalogCardFocus("shelf-results", item.Ref)
					p.drawerOptionRow(rc, id, region, item.Title, sub, false, func() { p.openVideoDetail(item) })
				} else {
					ui.Text(rc, item.Title).FontSize(22).Padding(14, 22).Margin(0, 0, 8, 0)
				}
			})
		} else {
			ui.Column(c).FillWidth().Grow(1).Center().Gap(20).Children(func() {
				p.emptyDescription(c, text.EmptyTitle, text.EmptyNote)
			})
		}
		ui.Row(c).Margin(16, 0, 0, 0).Children(func() {
			p.styledButton(c, text.Close, "", app.CloseModal, region, func() { p.State.Back() }, p.drawerButtonAppearance())
		})
		p.drawerFooter(c, text.Modal.HintSelect, text.Modal.HintOpen)
	})
}
