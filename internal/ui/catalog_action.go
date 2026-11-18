package tvui

import (
	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/provider"
)

const homeShelfDetailsSection = "shelf-details"

type catalogActionPage struct{}

func (p *PageContext) openCatalogAction(title string, item *provider.MediaItem, details bool) {
	home := &p.host.home
	action := &home.action
	route := app.Route{Page: app.HomeAction}
	if p.State.Route == route && details {
		route.Section = homeShelfDetailsSection
		action = &home.shelfDetails
	}
	if !p.State.Push(route) {
		return
	}
	action.stop()
	*action = catalogAction{title: title, item: item}
	if !details || item == nil {
		return
	}
	action.loading = true
	action.cancel = p.host.Catalog.ReadItem(item.Ref, func(updated provider.MediaItem, err error) {
		action.loading = false
		if err != nil {
			if provider.CodeOf(err) != provider.Unsupported {
				action.errorText = p.Assets.Text.FailedNote
			}
			return
		}
		action.item = &updated
	})
}

func (p *PageContext) openCatalogShelf(shelf catalogShelf) {
	p.openCatalogAction(shelf.title, nil, false)
	p.host.home.action.items = shelf.items
}

func (action *catalogAction) stop() {
	if action.cancel != nil {
		action.cancel()
	}
	*action = catalogAction{}
}

func (catalogActionPage) Render(c *ui.Context, p *PageContext) {
	action := &p.host.home.action
	if p.Location.Route.Section == homeShelfDetailsSection {
		action = &p.host.home.shelfDetails
	}
	text, colors := p.Assets.Text, p.Theme.Colors
	const region = "catalog-action"
	p.region(region, nil)
	ui.Row(c).FillWidth().Gap(18).AlignItems(ui.Center).Children(func() {
		ui.Text(c, action.title).FontSize(35).FontWeight(600)
		ui.Spacer(c).Grow(1)
		p.button(c, text.Close, app.CloseModal, region, func() { p.State.Back() })
	})
	if len(action.items) != 0 {
		ui.Scroll(c).Fill().Children(func() {
			ui.Column(c).FillWidth().Gap(13).Children(func() {
				for _, row := range action.items {
					id := catalogCardFocus("shelf-results", row.Ref)
					p.button(c, row.Title, id, region, func() {
						p.openCatalogAction(text.Home.Details, &row, true)
					})
				}
			})
		})
		return
	}
	if action.item == nil {
		ui.Column(c).Fill().Center().Gap(20).Children(func() {
			p.emptyDescription(c, text.EmptyTitle, text.EmptyNote)
		})
		return
	}
	item := *action.item
	ui.Row(c).FillWidth().Gap(24).AlignItems(ui.Start).Children(func() {
		ui.Box(c).Size(326, 184).Radius(13).Clip().Background(paint(colors.Surface.Raised)).Children(func() {
			if cover := p.catalogCover(item.CoverURL); cover != nil {
				ui.Image(c, cover).Fill().Fit(ui.Cover)
			}
		})
		ui.Column(c).Grow(1).MinWidth(0).Gap(16).Children(func() {
			ui.Text(c, item.Title).FontSize(31).FontWeight(600).MaxLines(3)
			p.catalogHeroMetadata(c, item)
			if item.Creator != nil {
				ui.Text(c, item.Creator.Name).FontSize(20).TextColor(paint(colors.Text.Secondary))
			}
		})
	})
	ui.Scroll(c).Fill().Children(func() {
		ui.Column(c).FillWidth().Gap(16).Children(func() {
			if item.Subtitle != "" {
				ui.Text(c, item.Subtitle).FillWidth().FontSize(22).LineHeight(1.55)
			}
			if action.loading {
				ui.Text(c, text.LoadingTitle).FontSize(20).TextColor(paint(colors.Text.Secondary))
			}
			if action.errorText != "" {
				ui.Text(c, action.errorText).FontSize(20).TextColor(paint(colors.Status.Error.Foreground))
			}
		})
	})
}
