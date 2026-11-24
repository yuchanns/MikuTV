package tvui

import (
	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/provider"
)

const (
	homeActionBackFocus  app.FocusID = "home-action/back"
	homeActionRetryFocus app.FocusID = "home-action/retry"
)

type catalogActionPage struct{}

type catalogAction struct {
	id    string
	title string
	items []provider.MediaItem
}

func (p *PageContext) openCatalogAction(id, title string, items []provider.MediaItem) {
	if p.State.Push(app.Route{Page: app.HomeAction}) {
		p.host.home.action = catalogAction{id: id, title: title, items: items}
		if len(items) != 0 {
			p.State.Focus = catalogCardFocus("shelf-results", items[0].Ref)
			p.host.reveal = true
		}
	}
}

func (catalogActionPage) Render(c *ui.Context, p *PageContext) {
	action := &p.host.home.action
	text := p.Assets.Text
	items := action.items
	var canLoadMore, loading bool
	var loadErr string
	var loadMore, retryMore func()
	if action.id == "picks" && p.host.Catalog != nil {
		snap := p.host.Catalog.Snapshot()
		if live := firstCatalogItems(snap.Page.Collections); len(live) != 0 {
			items = live
		}
		canLoadMore = snap.CanLoadMore()
		loading = snap.Appending && snap.Phase == app.CatalogLoading
		if catalogAppendFailed(snap) {
			loadErr = string(snap.Error)
		}
		loadMore = p.host.Catalog.LoadMore
		retryMore = p.host.Catalog.Retry
	}
	p.renderShelfCollection(c, shelfCollectionSpec{
		key:         "home-action/" + action.id,
		title:       action.title,
		countFormat: text.Activity.FolderCount,
		count:       len(items),
		grid:        shelfVideoGrid,
		backFocus:   homeActionBackFocus,
		retryFocus:  homeActionRetryFocus,
		itemPrefix:  "home/card/shelf-results/",
		itemFocus: func(index int) app.FocusID {
			return catalogCardFocus("shelf-results", items[index].Ref)
		},
		artworkURLs: func(index int) []string {
			item := items[index]
			if item.Creator != nil && item.Creator.AvatarURL != "" {
				return []string{item.CoverURL, item.Creator.AvatarURL}
			}
			return []string{item.CoverURL}
		},
		renderCard: func(c *ui.Context, index int, id app.FocusID, region string, cardWidth float32) {
			size := catalogCardSize{
				width:  cardWidth,
				height: shelfVideoGrid.cardH,
				cover:  shelfVideoGrid.coverH,
				title:  21,
			}
			p.catalogCard(c, id, region, items[index], size, nil)
		},
		emptyIcon:   "home",
		emptyNote:   text.EmptyNote,
		canLoadMore: canLoadMore,
		loading:     loading,
		loadErr:     loadErr,
		moreFailed:  text.Home.MoreFailed,
		loadMore:    loadMore,
		retryMore:   retryMore,
	})
}
