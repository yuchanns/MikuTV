package tvui

import (
	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/provider"
)

type catalogPage struct{}

func (catalogPage) Render(c *ui.Context, p *PageContext) {
	catalog := p.host.Catalog
	if catalog == nil {
		contentPage{}.Render(c, p)
		return
	}
	p.region("content", nil)
	p.heading(c, p.Title, "")
	text := p.Assets.Text
	current := catalog.Snapshot()
	title, note, label := text.EmptyTitle, text.EmptyNote, text.SwitchEntry
	action := func() { p.State.OpenEntries("") }
	switch current.Phase {
	case app.CatalogLoading:
		title, note, label, action = text.LoadingTitle, text.LoadingNote, text.Cancel, catalog.Cancel
	case app.CatalogFailed:
		title, note, label, action = text.FailedTitle, text.FailedNote, text.Retry, catalog.Reload
	case app.CatalogCanceled:
		title, note, label, action = text.CanceledTitle, "", text.Retry, catalog.Reload
	}
	body := ui.Column(c.Key("catalog-body")).FillWidth().Gap(24)
	empty := len(current.Page.Collections) == 0
	if empty {
		body.Height(530).Center()
	}
	// Keep the action in one keyed layout when loading and content swap places.
	body.Children(func() {
		if empty {
			p.emptyDescription(c, title, note)
		}
		button := p.button(c, label, app.ContentAction, "content", action)
		if empty {
			button.AlignSelf(ui.Center)
		}
		for _, group := range current.Page.Collections {
			renderCollection(c, p, group)
		}
	})
}

func renderCollection(c *ui.Context, p *PageContext, group provider.Collection) {
	colors := p.Theme.Colors
	ui.Column(c).FillWidth().Gap(18).Children(func() {
		ui.Text(c, group.Title).FontSize(32).Bold()
		ui.Grid(c).FillWidth().Columns(3).Gap(18).Children(func() {
			for _, item := range group.Items {
				ui.Column(c).Height(120).Padding(22).Gap(10).
					Background(paint(colors.Surface.Raised)).Radius(14).Children(func() {
					ui.Text(c, item.Title).FontSize(24).Bold().MaxLines(2)
					if item.Subtitle != "" {
						ui.Text(c, item.Subtitle).FontSize(18).MaxLines(1).TextColor(paint(colors.Text.Secondary))
					}
				})
			}
		})
	})
}
