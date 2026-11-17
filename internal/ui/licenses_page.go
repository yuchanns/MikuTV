package tvui

import (
	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
)

type licensesPage struct{}

func (licensesPage) Render(c *ui.Context, p *PageContext) {
	p.region("modal", nil)
	colors, d := p.Theme.Colors, p.uiColors()
	scroll := p.scroll()
	ui.Column(c).Fill().Gap(26).Children(func() {
		ui.Text(c, p.Title).Height(50).FontSize(36).FontWeight(500).LineHeight(1.4)
		e := ui.Scroll(c.Key(string(app.LicenseText))).FillWidth().Height(510).TrackScroll(scroll).
			Label(p.Title).Padding(22).Radius(12).Background(d.subtleFill)
		e.Children(func() {
			ui.Text(c, p.Assets.License).FillWidth().Font("monospace").FontSize(18).LineHeight(1.8).
				TextColor(paint(colors.Text.Secondary))
		})
		p.bindDirection(e, app.LicenseText, "modal", func(direction app.Direction) bool {
			step := float32(220)
			switch direction {
			case app.Up:
				step = -step
			case app.Down:
			default:
				return false
			}
			next := max(0, min(scroll.MaxY, scroll.Y+step))
			if next == scroll.Y {
				return false
			}
			scroll.Y = next
			return true
		})
	})
}
