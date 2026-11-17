package tvui

import (
	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
)

type navigationRail struct{}

const (
	collapsedRailWidth float32 = 116
	expandedRailWidth  float32 = 288
)

func railWidth(state *app.State) float32 {
	if state.Area == app.NavigationArea {
		return expandedRailWidth
	}
	return collapsedRailWidth
}

func (navigationRail) Render(c *ui.Context, p *PageContext) {
	expanded := p.State.Area == app.NavigationArea
	rail := ui.Column(c.Key("rail"))
	width := rail.AnimateTo("width", railWidth(p.State), ui.Animation{Duration: railDuration})
	buttonWidth := width - 48
	if p.active {
		p.host.focus.groups["rail"] = focusRegion{navigation: true, edges: focusEdges{app.Right: {Focus: p.State.ContentFocus()}}}
	}
	colors := p.Theme.Colors
	rail.Width(width).Height(p.Height).Shrink(0).
		Padding(40, 24, 36, 24).Background(paint(colors.Surface.Sidebar)).Clip()
	opacity := float32(0)
	if expanded {
		opacity = 1
	}
	opacity = rail.AnimateTo("brand", opacity, ui.Animation{Duration: railDuration, NonMotion: true})
	rail.Children(func() {
		ui.Box(c).Width(240).Height(103).Children(func() {
			ui.Row(c).Width(240).Height(48).Gap(18).AlignItems(ui.Center).Children(func() {
				ui.Box(c).Size(68, 48).Center().Children(func() { ui.Image(c, p.Assets.Brand).Size(48, 48) })
				ui.Text(c, "MikuTV").FontSize(26).Bold().TextColor(paint(colors.Text.Primary)).Opacity(opacity)
			})
		})
		ui.Spacer(c).Height(55)
		ui.Column(c).Gap(20).Children(func() {
			for _, link := range p.Pages.navigation(p.State, primaryNavigation) {
				p.railButton(c, link, buttonWidth, expanded)
			}
		})
		ui.Spacer(c).Grow(1)
		ui.Column(c).Gap(16).Children(func() {
			for _, link := range p.Pages.navigation(p.State, utilityNavigation) {
				p.railButton(c, link, buttonWidth, expanded)
			}
		})
	})
}

func (p *PageContext) railButton(c *ui.Context, registration Registration, width float32, expanded bool) {
	link := registration.Navigation
	page := registration.Policy.Route.Page
	id := app.RailFocus(page)
	colors := p.Theme.Colors
	background, foreground := ui.Transparent, paint(colors.Text.Muted)
	if p.Pages.navigationOwner(p.Location.Route) == page {
		background, foreground = paint(colors.Hover.Background), paint(colors.Text.Primary)
	}
	if p.State.Focus == id {
		background, foreground = paint(colors.Focus.Background), paint(colors.Focus.Foreground)
	}
	e := ui.Button(c.Key(string(id)), "").Width(width).Height(66).Padding(0, 12).
		Radius(16).Border(0, ui.Transparent).Background(background).TextColor(foreground).
		Label(link.Title).
		OnClick(func() {
			p.dispatch(func() {
				if p.State.Navigate(registration.Policy.Route) {
					p.State.Focus, p.State.Area = p.State.ContentFocus(), app.ContentArea
				}
			})
		})
	p.animateFocus(e, background, foreground)
	opacity := float32(0)
	if expanded {
		opacity = 1
	}
	opacity = e.AnimateTo("label", opacity, ui.Animation{Duration: focusDuration, NonMotion: true})
	e.Children(func() {
		ui.Row(c).Fill().Gap(16).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(40, 40).Center().Children(func() { ui.Icon(c, p.Assets.Icon(link.Icon)).Size(26, 26) })
			ui.Text(c, link.Title).Width(180).MaxLines(1).FontSize(22).Opacity(opacity)
		})
	})
	p.bind(e, id, "rail")
}
