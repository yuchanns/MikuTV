package tvui

import (
	"time"

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
	return p.actionButton(c, label, "", id, group, action)
}

type buttonAppearance struct {
	background, foreground, border                                 ui.Color
	focusBackground, focusForeground                               ui.Color
	height, padding, radius, fontSize, iconSize, iconGap, minWidth float32
	textStroke                                                     float32
	fontWeight                                                     int
	focusShadow, tab, selected                                     bool
}

func (p *PageContext) playButtonAppearance() buttonAppearance {
	d := p.uiColors()
	return buttonAppearance{
		height: 62, radius: 12, fontSize: 22, fontWeight: 550, iconSize: 24, iconGap: 13,
		background: d.secondaryFill, foreground: d.secondaryText, border: d.secondaryBorder,
	}
}

func (p *PageContext) dialogButtonAppearance() buttonAppearance {
	d := p.uiColors()
	return buttonAppearance{
		height: 56, padding: 20, radius: 12, fontSize: 19,
		background: d.secondaryFill, foreground: d.secondaryText, border: d.secondaryBorder,
	}
}

func (p *PageContext) actionButton(c *ui.Context, label, icon string, id app.FocusID, group string, action func()) ui.Element {
	colors := p.Theme.Colors
	return p.styledButton(c, label, icon, id, group, action, buttonAppearance{
		background: paint(colors.Surface.Raised), foreground: paint(colors.Text.Primary),
		height: 62, padding: 27, radius: 12, fontSize: 22, iconSize: 22, minWidth: 112,
	})
}

func (p *PageContext) styledButton(c *ui.Context, label, icon string, id app.FocusID, group string, action func(), style buttonAppearance) ui.Element {
	colors, d := p.Theme.Colors, p.uiColors()
	background, foreground, borderColor := style.background, style.foreground, style.border
	focused := p.paintFocus() == id
	if focused {
		background, foreground = paint(colors.Focus.Background), paint(colors.Focus.Foreground)
		if style.focusBackground.A != 0 {
			background, foreground = style.focusBackground, style.focusForeground
		}
		if style.tab {
			background, foreground = d.tabSubtleFocusFill, paint(colors.Text.Primary)
		}
		if !p.Theme.Dark && d.focusBorder.A != 0 {
			borderColor = d.focusBorder
		}
	}
	text := label
	if icon != "" || style.tab {
		text = ""
	}
	border := float32(0)
	if borderColor.A != 0 {
		border = 1
	}
	e := ui.Button(c.Key(string(id)), text).Label(label).Height(style.height).MinWidth(style.minWidth).
		Padding(0, style.padding).Radius(style.radius).
		Border(border, borderColor).Background(background).TextColor(foreground).FontSize(style.fontSize).
		AlignSelf(ui.Start).OnClick(func() { p.dispatch(action) })
	if style.focusShadow {
		p.addVideoFocusShadow(e, focused)
	} else {
		p.applyFocusElevation(e, focused)
	}
	if style.fontWeight > 0 {
		e.FontWeight(style.fontWeight)
	}
	if style.tab {
		e.Role(ui.RoleTab).AccessSelected(style.selected)
	}
	if icon != "" || style.tab {
		gap := style.iconGap
		if gap == 0 {
			gap = 10
		}
		e.Children(func() {
			ui.Row(c).Gap(gap).AlignItems(ui.Center).Children(func() {
				if icon != "" {
					ui.Icon(c, p.Assets.Icon(icon)).Size(style.iconSize, style.iconSize)
				}
				ui.Text(c, label).FontSize(style.fontSize)
			})
			if style.tab {
				p.tabUnderline(c, style.selected, focused, 2)
			}
		})
	}
	color := p.animateFocus(e, background, foreground)
	if style.textStroke > 0 {
		e.TextStroke(style.textStroke, color)
	}
	p.bind(e, id, group)
	return e
}

func (p *PageContext) tabUnderline(c *ui.Context, selected, focused bool, bottom float32) {
	if !selected && !focused {
		return
	}
	colors := p.Theme.Colors
	width, ink := float32(15), withAlpha(colors.Text.Primary, 0x66)
	if focused {
		width, ink = 18, paint(colors.Accent.Soft)
	}
	ui.Row(c).Absolute().Left(0).Right(0).Bottom(bottom).Height(3).Center().Children(func() {
		ui.Box(c).Size(width, 3).Radius(2).Background(ink)
	})
}

func (p *PageContext) animateFocus(element ui.Element, background, foreground ui.Color) ui.Color {
	animation := ui.Animation{Duration: focusDuration, NonMotion: true}
	color := element.AnimateColor("foreground", foreground, animation)
	element.Background(element.AnimateColor("background", background, animation)).TextColor(color)
	return color
}

func (p *PageContext) applyFocusElevation(_ ui.Element, _ bool) {
	// Light themes preserve the same flat frosted-glass surface language as Dark themes without heavy drop shadows.
}

func (p *PageContext) loadingSpinner(c *ui.Context, size float32) {
	colors, d := p.Theme.Colors, p.uiColors()
	var degrees float32
	if !p.host.reduceMotionEnabled(c) {
		degrees = float32(c.Now().UnixNano()%int64(time.Second)) / float32(time.Second) * 360
		c.AnimationFrame()
	}
	stroke := max(float32(2.5), size*0.06)
	ui.Box(c.Key("spinner")).Size(size, size).Shrink(0).Children(func() {
		ui.Box(c.Key("track")).Absolute().Top(0).Left(0).Fill().Radius(size/2).Border(stroke, d.spinnerTrack)
		ui.Icon(c.Key("arc"), p.Assets.Icon("playback-spinner")).Absolute().Top(0).Left(0).Size(size, size).
			TextColor(paint(colors.Accent.Soft)).Rotate(degrees)
	})
}
