package tvui

import (
	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
)

type aboutPage struct{}

func (aboutPage) Render(c *ui.Context, p *PageContext) {
	p.region("content", nil)
	text, colors, d := p.Assets.Text, p.Theme.Colors, p.uiColors()
	ui.Row(c).FillWidth().Padding(24, 0, 0, 0).Gap(65).AlignItems(ui.Start).Children(func() {
		ui.Column(c).Width(320).Shrink(0).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(280, 168).Radius(25).Clip().Children(func() {
				ui.Image(c, p.Assets.AboutArtwork).Fill()
			})
			ui.Text(c, "MikuTV").FontSize(40).FontWeight(500).LetterSpacing(3).Margin(26, 0, 0, 0)
			ui.Text(c, text.AboutNote).MaxWidth(300).TextAlign(ui.Center).FontSize(17).LineHeight(1.8).
				Margin(30, 0, 0, 0).TextColor(paint(colors.Text.Muted))
		})
		ui.Column(c).Grow(1).Children(func() {
			for _, row := range [][2]string{{text.Version, p.Version}, {text.Licenses, "Apache-2.0"}, {text.Platform, "Apple TV / tvOS"}} {
				ui.Box(c).FillWidth().Height(76).Children(func() {
					ui.Row(c).Fill().Padding(0, 19).AlignItems(ui.Center).Children(func() {
						ui.Text(c, row[0]).FontSize(21).TextColor(paint(colors.Text.Muted))
						ui.Spacer(c).Grow(1)
						ui.Text(c, row[1]).FontSize(20).TextColor(paint(colors.Text.Secondary))
					})
					ui.Box(c).Absolute().Left(0).Right(0).Bottom(0).Height(1).Background(d.subtleBorder)
				})
			}
			p.aboutActionRow(c, app.ShowLicenses, text.Licenses, func() {
				p.State.Push(app.Route{Page: app.Licenses, Section: "about"})
			})
			p.aboutActionRow(c, app.ShowPrivacyDetails, text.Privacy.DetailsTitle, func() {
				p.State.Push(app.Route{Page: app.PrivacyDetails, Section: "about"})
			})
		})
	})
}

func (p *PageContext) aboutActionRow(c *ui.Context, id app.FocusID, label string, action func()) {
	colors, d := p.Theme.Colors, p.uiColors()
	background, ink := ui.Transparent, d.secondaryText
	if p.paintFocus() == id {
		background, ink = paint(colors.Focus.Background), paint(colors.Focus.Foreground)
	}
	button := ui.Button(c.Key(string(id)), "").Label(label).
		FillWidth().Height(76).Radius(8).Border(0, ui.Transparent).Padding(0).
		OnClick(func() { p.dispatch(action) })
	p.animateFocus(button, background, ink)
	p.bind(button, id, "content")
	button.Children(func() {
		ui.Row(c).Fill().Padding(0, 19).AlignItems(ui.Center).Children(func() {
			ui.Text(c, label).FontSize(21)
		})
		ui.Box(c).Absolute().Left(0).Right(0).Bottom(0).Height(1).Background(d.subtleBorder)
	})
}
