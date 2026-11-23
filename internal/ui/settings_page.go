package tvui

import (
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
)

func (v *View) selectSettingsSection(next app.FocusID) {
	section, ok := strings.CutPrefix(string(next), "section/")
	if !ok || section == "" {
		return
	}
	route := app.Route{Page: app.Settings, Section: section}
	if v.State.Route == route {
		return
	}
	if v.State.Navigate(route) {
		v.State.Focus, v.State.Area, v.reveal = next, app.ContentArea, true
	}
}

type settingsLayout struct{}

func (settingsLayout) Render(c *ui.Context, p *PageContext) {
	text := p.Assets.Text
	p.region("sections", focusEdges{app.Right: {Region: "content"}})
	sections := p.Pages.children(p.Node)
	colors, d := p.Theme.Colors, p.uiColors()
	ui.Column(c).FillWidth().Lang(p.langTag()).Font(p.uiFont()).Children(func() {
		ui.Box(c).FillWidth().Height(187).Children(func() {
			ui.Text(c, text.SettingsTitle).Absolute().Top(2).Left(0).FontSize(54).FontWeight(550).LetterSpacing(2)
			ui.Text(c, text.SettingsNote).Absolute().Top(94).Left(0).FontSize(22).LineHeight(1.7).
				TextColor(paint(colors.Text.Muted))
		})
		ui.Row(c).FillWidth().Gap(83).AlignItems(ui.Start).Children(func() {
			ui.Column(c).Width(265).Shrink(0).Gap(10).Children(func() {
				for _, section := range sections {
					id := section.Policy.Initial.Focus
					selected := p.Pages.contains(section.Policy.Route, p.Location.Route)
					background, foreground := ui.Transparent, paint(colors.Text.Secondary)
					borderW, borderColor := float32(0), ui.Transparent
					if selected {
						background, foreground = paint(colors.Selected.Background), paint(colors.Selected.Foreground)
						p.left = focusDestination{Focus: id}
					}
					focused := p.State.Focus == id
					if focused {
						if !p.Theme.Dark {
							background, foreground = paint(colors.Focus.Background), paint(colors.Focus.Foreground)
							borderW, borderColor = 1, d.focusBorder
						} else {
							background, foreground = d.subtleHoverFill, paint(colors.Text.Primary)
						}
					}
					e := ui.Button(c.Key(string(id)), "").FillWidth().Height(68).Padding(0, 19).Radius(13).
						Border(borderW, borderColor).Background(background).TextColor(foreground).Label(section.Title).
						OnClick(func() {
							p.dispatch(func() { p.State.Navigate(section.Policy.Route); p.State.Focus = id; p.State.Area = app.ContentArea })
						})
					p.animateFocus(e, background, foreground)
					p.applyFocusElevation(e, focused)
					e.Children(func() {
						ui.Row(c).Fill().Gap(16).AlignItems(ui.Center).Children(func() {
							ui.Icon(c, p.Assets.Icon(section.Icon)).Size(24, 24)
							ui.Text(c, section.Title).FontSize(23)
						})
					})
					p.bind(e, id, "sections")
				}
			})
			ui.Column(c.Key("settings-section/" + p.Location.Section)).Grow(1).MaxWidth(1145).Padding(8, 0, 0, 0).Children(func() { p.Outlet(c) })
		})
	})
}

type settingsEmptyPage struct{}

func (settingsEmptyPage) Render(c *ui.Context, p *PageContext) {
	ui.Text(c, p.Title).FontSize(32).FontWeight(550).LineHeight(1.4).Margin(0, 0, 13, 0)
	ui.Text(c, p.Assets.Text.EmptySettings).FontSize(19).TextColor(paint(p.Theme.Colors.Text.Muted))
}
