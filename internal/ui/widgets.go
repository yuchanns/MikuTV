package tvui

import (
	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
)

func (p *PageContext) heading(c *ui.Context, title, note string) {
	ui.Column(c).Gap(18).Children(func() {
		ui.Text(c, title).FontSize(52).Bold()
		if note != "" {
			ui.Text(c, note).FontSize(23).TextColor(paint(p.Theme.Colors.Text.Secondary))
		}
	})
}

func (p *PageContext) empty(c *ui.Context, title, note, label string, id app.FocusID, group string, action func()) {
	ui.Column(c).FillWidth().Height(530).Center().Gap(24).Children(func() {
		p.emptyDescription(c, title, note)
		p.button(c, label, id, group, action).AlignSelf(ui.Center)
	})
}

func (p *PageContext) emptyDescription(c *ui.Context, title, note string) {
	colors := p.Theme.Colors
	ui.Icon(c, p.Assets.Icon("library")).Size(64, 64).TextColor(paint(colors.Text.Muted))
	ui.Text(c, title).FontSize(36)
	if note != "" {
		ui.Text(c, note).FontSize(23).TextColor(paint(colors.Text.Secondary))
	}
}

func (p *PageContext) button(c *ui.Context, label string, id app.FocusID, group string, action func()) ui.Element {
	colors := p.Theme.Colors
	background, foreground := paint(colors.Surface.Raised), paint(colors.Text.Primary)
	if p.State.Focus == id {
		background, foreground = paint(colors.Focus.Background), paint(colors.Focus.Foreground)
	}
	e := ui.Button(c.Key(string(id)), label).Height(62).MinWidth(112).Padding(0, 27).Radius(12).
		Border(0, ui.Transparent).Background(background).TextColor(foreground).FontSize(22).
		AlignSelf(ui.Start).OnClick(func() { p.dispatch(action) })
	p.animateFocus(e, background, foreground)
	p.bind(e, id, group)
	return e
}

func (p *PageContext) animateFocus(element ui.Element, background, foreground ui.Color) {
	animation := ui.Animation{Duration: focusDuration, NonMotion: true}
	element.Background(element.AnimateColor("background", background, animation)).
		TextColor(element.AnimateColor("foreground", foreground, animation))
}
