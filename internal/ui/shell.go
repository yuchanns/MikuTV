package tvui

import (
	"github.com/egoist/mygo/plugins/glass"
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
	railAnim := ui.Animation{Duration: railDuration}
	if p.host.reduceMotionEnabled(c) {
		railAnim = ui.Animation{}
	}
	width := rail.AnimateTo("width", railWidth(p.State), railAnim)
	p.host.railSpan = width
	buttonWidth := width - 48
	if p.active {
		rightRegion := "content"
		if p.Location.Page == app.Library {
			rightRegion = favoriteTabs
		}
		p.host.focus.groups["rail"] = focusRegion{navigation: true, edges: focusEdges{app.Right: {Focus: p.State.ContentFocus(), Region: rightRegion}}}
	}
	colors, d := p.Theme.Colors, p.uiColors()
	railBg := paint(colors.Surface.Sidebar)
	if !p.Theme.Dark {
		railBg = withAlpha(colors.Surface.Sidebar, 0xcc)
		rail.Material(glass.Blur{Radius: 28})
	}
	rail.Width(width).Height(p.Height).Shrink(0).
		Padding(40, 24, 36, 24).Background(railBg).Clip()
	opacity := float32(0)
	if expanded {
		opacity = 1
	}
	opacity = rail.AnimateTo("brand", opacity, ui.Animation{Duration: railDuration, NonMotion: true})
	rail.Children(func() {
		if !p.Theme.Dark {
			ui.Box(c).Absolute().Right(0).Top(0).Bottom(0).Width(1).Background(d.subtleBorder)
		}
		ui.Box(c).Width(240).Height(103).Children(func() {
			ui.Row(c).Width(240).Height(48).Gap(18).AlignItems(ui.Center).Children(func() {
				ui.Box(c).Size(68, 48).Center().Children(func() { ui.Image(c, p.Assets.Brand).Size(48, 48) })
				ui.Text(c, "MikuTV").FontSize(26).Bold().TextColor(paint(colors.Text.Primary)).Opacity(opacity)
			})
		})
		ui.Box(c).Height(55)
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
	label := link.Title
	if link.Label != nil {
		label = link.Label(p)
	}
	page := registration.Policy.Route.Page
	id := app.RailFocus(page)
	selected := p.Pages.navigationOwner(p.Location.Route) == page
	colors, d := p.Theme.Colors, p.uiColors()
	background, foreground := ui.Transparent, paint(colors.Text.Muted)
	borderW, borderColor := float32(0), ui.Transparent
	if selected {
		background, foreground = withAlpha(colors.Text.Primary, 0x0e), paint(colors.Text.Primary)
	}
	focused := p.State.Focus == id
	if focused {
		background, foreground = paint(colors.Focus.Background), paint(colors.Focus.Foreground)
		if !p.Theme.Dark {
			borderW, borderColor = 1, d.focusBorder
		}
	}
	e := ui.Button(c.Key(string(id)), "").Width(width).Height(66).Padding(0, 12).
		Radius(16).Border(borderW, borderColor).Background(background).TextColor(foreground).
		Label(label).
		OnClick(func() {
			p.dispatch(func() {
				if p.State.Navigate(registration.Policy.Route) {
					p.State.Focus, p.State.Area = p.State.ContentFocus(), app.ContentArea
				}
			})
		})
	p.animateFocus(e, background, foreground)
	p.applyFocusElevation(e, focused)
	opacity := float32(0)
	if expanded {
		opacity = 1
	}
	opacity = e.AnimateTo("label", opacity, ui.Animation{Duration: focusDuration, NonMotion: true})
	e.Children(func() {
		if selected {
			ui.Box(c).Absolute().Left(-23).Top(21).Size(4, 24).Radius(4).Background(paint(colors.Accent.Soft))
		}
		ui.Row(c).Fill().Gap(16).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(40, 40).Center().Children(func() {
				if link.Symbol != nil {
					link.Symbol(c, p)
				} else {
					ui.Icon(c, p.Assets.Icon(link.Icon)).Size(26, 26)
				}
			})
			ui.Text(c, label).Width(180).MaxLines(1).FontSize(22).Opacity(opacity)
		})
	})
	p.bind(e, id, "rail")
}
