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
	ui.Column(c).Fill().Lang("zh-CN").Font("system-ui, PingFang SC").Children(func() {
		p.drawerHeader(c, "LEGAL \u00b7 OPEN SOURCE", p.Title, "", "")
		e := ui.Scroll(c.Key(string(app.LicenseText))).FillWidth().Grow(1).TrackScroll(scroll).
			Label(p.Title).Padding(20, 22).Radius(14).Background(d.subtleFill)
		e.Children(func() {
			ui.Text(c, p.Assets.License).FillWidth().Font("monospace").FontSize(17).LineHeight(1.8).
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
		p.drawerFooter(c, p.Assets.Text.Modal.HintSelect)
	})
}
