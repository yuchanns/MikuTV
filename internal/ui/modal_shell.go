package tvui

import (
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
)

type modalPresentation uint8

const (
	modalDrawer modalPresentation = iota
	modalFrameless
)

const (
	drawerBlurRadius    float32 = 9
	framelessBlurRadius float32 = 34
)

var drawerTransition = ui.ElementTransition{
	Duration: 220 * time.Millisecond, Ease: ui.EaseOut, Position: true,
	Enter: &ui.Motion{X: 72}, Exit: &ui.Motion{X: 48},
}

var framelessTransition = ui.ElementTransition{
	Duration: 220 * time.Millisecond, Ease: ui.EaseOut, Position: true,
	Enter: &ui.Motion{Y: 16}, Exit: &ui.Motion{Y: -10},
}

func (v *View) modalStyle(route app.Route) modalPresentation {
	reg := v.Pages.components[route]
	if reg.ResolveModalPresentation != nil {
		return reg.ResolveModalPresentation(v)
	}
	return reg.ModalPresentation
}

func (v *View) modalBlur(route app.Route, fromMedia bool) float32 {
	reg, ok := v.Pages.components[route]
	if !ok || !reg.Policy.Modal {
		return 0
	}
	if reg.ModalBlur != 0 {
		return reg.ModalBlur
	}
	if v.modalStyle(route) == modalFrameless {
		return framelessBlurRadius
	}
	return drawerBlurRadius
}

func (v *View) renderModal(c *ui.Context, page *PageContext, _ bool) {
	page.left = focusDestination{}
	reg := v.Pages.components[page.Location.Route]
	page.Title, page.Node = reg.Title, reg.Policy.Route
	style := v.modalStyle(page.Location.Route)
	colors, d := v.Theme.Colors, deriveColors(v.Theme)

	ui.Overlay(c, func() {
		shadeKey := struct{ Shade app.Route }{page.Location.Route}
		fTrans := framelessTransition
		dTrans := drawerTransition
		if v.reduceMotionEnabled(c) {
			fTrans = ui.ElementTransition{}
			dTrans = ui.ElementTransition{}
		}
		if style == modalFrameless {
			contentW := reg.ModalWidth
			if contentW == 0 {
				contentW = 840
			}
			contentW = min(contentW, page.Width-240)
			ui.Box(c.Key(shadeKey)).Absolute().Left(0).Top(0).Size(page.Width, page.Height).
				Background(d.framelessScrim).Transition(fTrans)
			ui.Box(c).Absolute().Left(0).Top(0).Size(page.Width, page.Height).LinearGradient(ui.LinearGradient{
				Angle: 155,
				Stops: []ui.GradientStop{
					{Color: d.framelessGlow, At: 0},
					{Color: ui.Transparent, At: 0.55},
				},
			})
			ui.Column(c.Key(page.Location.Route)).Absolute().Left(0).Top(0).Size(page.Width, page.Height).
				Center().Modal().Transition(fTrans).Padding(80, 120).Children(func() {
				ui.Column(c).Width(contentW).Children(func() {
					reg.Component.Render(c, page)
				})
			})
			return
		}

		drawerW := reg.ModalWidth
		if drawerW == 0 {
			drawerW = 620
		}
		drawerW = min(drawerW, page.Width-220)
		padX := float32(52)
		if drawerW >= 620 {
			padX = 56
		}
		ui.Box(c.Key(shadeKey)).Absolute().Left(0).Top(0).Size(page.Width, page.Height).
			LinearGradient(ui.LinearGradient{
				Angle: 90,
				Stops: []ui.GradientStop{
					{Color: d.drawerScrimLeft, At: 0},
					{Color: d.drawerScrimMid, At: 0.54},
					{Color: d.drawerScrimRight, At: 1},
				},
			}).Transition(dTrans)

		panel := ui.Box(c.Key(page.Location.Route)).Absolute().Right(0).Top(0).
			Size(drawerW, page.Height).Modal().Transition(dTrans).
			Gradient(d.drawerStart, d.drawerEnd, 180).
			Shadow(-36, 0, 90, 0, paint(colors.Overlay.Shadow))
		panel.Children(func() {
			ui.Box(c).Absolute().Left(0).Top(0).Bottom(0).Width(1).Background(d.drawerBorder)
			ui.Box(c).Absolute().Left(1).Top(0).Bottom(0).Width(1).Background(d.drawerAccentRim)
			ui.Column(c).Fill().Padding(66, padX, 46, padX).Children(func() {
				reg.Component.Render(c, page)
			})
		})
	})
}

func (p *PageContext) renderPlayerDrawer(c *ui.Context, key string, width float32, body func()) {
	colors, d := p.Theme.Colors, p.uiColors()
	drawerW := min(width, p.Width-220)
	padX := float32(52)
	if drawerW >= 620 {
		padX = 56
	}
	ui.Box(c.Key(key+"-scrim")).Absolute().Left(0).Top(0).Fill().LinearGradient(ui.LinearGradient{
		Angle: 90,
		Stops: []ui.GradientStop{
			{Color: d.drawerScrimLeft, At: 0},
			{Color: d.drawerScrimMid, At: 0.54},
			{Color: d.drawerScrimRight, At: 1},
		},
	})
	ui.Box(c.Key(key)).Absolute().Right(0).Top(0).Size(drawerW, p.Height).Modal().
		Gradient(d.drawerStart, d.drawerEnd, 180).
		Shadow(-36, 0, 90, 0, paint(colors.Overlay.Shadow)).Children(func() {
		ui.Box(c).Absolute().Left(0).Top(0).Bottom(0).Width(1).Background(d.drawerBorder)
		ui.Box(c).Absolute().Left(1).Top(0).Bottom(0).Width(1).Background(d.drawerAccentRim)
		ui.Column(c).Fill().Padding(66, padX, 46, padX).Children(body)
	})
}

func (p *PageContext) renderPlayerFrameless(c *ui.Context, key string, width float32, body func()) {
	d := p.uiColors()
	contentW := min(width, p.Width-240)
	ui.Box(c.Key(key + "-scrim")).Absolute().Left(0).Top(0).Fill().Background(d.framelessScrim)
	ui.Box(c).Absolute().Left(0).Top(0).Fill().LinearGradient(ui.LinearGradient{
		Angle: 155,
		Stops: []ui.GradientStop{
			{Color: d.framelessGlow, At: 0},
			{Color: ui.Transparent, At: 0.55},
		},
	})
	ui.Column(c.Key(key)).Absolute().Left(0).Top(0).Fill().Center().Modal().Padding(80, 120).Children(func() {
		ui.Column(c).Width(contentW).Children(body)
	})
}

func (p *PageContext) drawerHeader(c *ui.Context, eyebrow, title, meta, subtitle string) {
	colors, d := p.Theme.Colors, p.uiColors()
	if eyebrow != "" {
		ui.Row(c).Height(20).Gap(14).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(22, 2).Radius(1).Background(paint(colors.Accent.Soft))
			ui.Text(c, eyebrow).FontSize(13).FontWeight(500).LetterSpacing(3.4).
				TextColor(paint(colors.Text.Secondary))
		})
	}
	ui.Row(c).FillWidth().Margin(16, 0, 0, 0).Gap(16).AlignItems(ui.Center).Children(func() {
		ui.Text(c, title).Grow(1).MinWidth(0).FontSize(40).FontWeight(600).LetterSpacing(0.8).
			SingleLine().Role(ui.RoleHeading).Level(2)
		if meta != "" {
			ui.Text(c, meta).Shrink(0).FontSize(16).FontWeight(450).LetterSpacing(0.5).
				TextColor(paint(colors.Accent.Soft))
		}
	})
	if subtitle != "" {
		ui.Text(c, subtitle).FillWidth().Margin(10, 0, 0, 0).FontSize(18).LineHeight(1.5).
			MaxLines(2).TextColor(paint(colors.Text.Muted))
	}
	ui.Box(c).FillWidth().Height(1).Margin(26, 0, 26, 0).LinearGradient(ui.LinearGradient{
		Angle: 90,
		Stops: []ui.GradientStop{
			{Color: d.drawerHairlineStart, At: 0},
			{Color: d.drawerHairlineEnd, At: 1},
		},
	})
}

func (p *PageContext) drawerFooter(c *ui.Context, hints ...string) {
	colors, d := p.Theme.Colors, p.uiColors()
	ui.Column(c).FillWidth().Margin(22, 0, 0, 0).Children(func() {
		ui.Box(c).FillWidth().Height(1).Background(d.drawerHairlineEnd)
		ui.Row(c).FillWidth().Margin(20, 0, 0, 0).AlignItems(ui.Center).Children(func() {
			ui.Row(c).Gap(22).AlignItems(ui.Center).Children(func() {
				for _, hint := range hints {
					if hint != "" {
						ui.Text(c, hint).FontSize(15).LetterSpacing(0.5).TextColor(paint(colors.Text.Muted))
					}
				}
			})
			ui.Spacer(c).Grow(1)
			ui.Text(c, p.Assets.Text.Modal.HintClose).FontSize(15).LetterSpacing(0.5).
				TextColor(paint(colors.Text.Muted))
		})
	})
}

func (p *PageContext) drawerOptionRow(c *ui.Context, id app.FocusID, region, label, sub string, selected bool, action func()) ui.Element {
	colors, d := p.Theme.Colors, p.uiColors()
	focused := p.paintFocus() == id
	bg, fg, subFg := ui.Transparent, paint(colors.Text.Secondary), paint(colors.Text.Muted)
	if selected {
		bg, fg = withAlpha(colors.Accent.Primary, 0x16), paint(colors.Accent.Soft)
	}
	if focused {
		bg, fg, subFg = paint(colors.Focus.Background), paint(colors.Focus.Foreground), d.focusSubtext
	}
	btn := ui.Button(c.Key(string(id)), "").Label(label).FillWidth().Height(64).
		Margin(0, 0, 6, 0).Padding(0, 22).Radius(14).Border(0, ui.Transparent).
		Background(bg).TextColor(fg).
		OnClick(func() { p.dispatch(action) })
	p.addVideoFocusShadow(btn, focused)
	p.animateFocus(btn, bg, fg)
	p.bind(btn, id, region)
	btn.Children(func() {
		ui.Row(c).Fill().Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Text(c, label).FontSize(23).FontWeight(500).SingleLine()
			if sub != "" {
				ui.Text(c, sub).FontSize(16).SingleLine().TextColor(subFg)
			}
			ui.Spacer(c).Grow(1)
			if selected {
				ui.Icon(c, p.Assets.Icon("check")).Size(22, 22)
			}
		})
	})
	return btn
}

func (p *PageContext) framelessHeader(c *ui.Context, eyebrow, icon, title, body string) {
	colors := p.Theme.Colors
	if eyebrow != "" || icon != "" {
		ui.Row(c).Height(26).Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(28, 2).Radius(1).Background(paint(colors.Accent.Soft))
			if icon != "" {
				ui.Icon(c, p.Assets.Icon(icon)).Size(19, 19).TextColor(paint(colors.Accent.Soft))
			}
			if eyebrow != "" {
				ui.Text(c, eyebrow).FontSize(16).FontWeight(500).LetterSpacing(2.6).
					TextColor(paint(colors.Accent.Soft)).MaxLines(1)
			}
		})
	}
	ui.Text(c, title).FillWidth().Margin(22, 0, 0, 0).FontSize(56).FontWeight(600).
		LetterSpacing(1.2).LineHeight(1.24).MaxLines(2).Role(ui.RoleHeading).Level(2)
	ui.Box(c).Size(56, 2).Radius(1).Margin(28, 0, 26, 0).LinearGradient(ui.LinearGradient{
		Angle: 90,
		Stops: []ui.GradientStop{
			{Color: paint(colors.Accent.Soft), At: 0},
			{Color: ui.Transparent, At: 1},
		},
	})
	if body != "" {
		ui.Text(c, body).FillWidth().FontSize(23).LineHeight(1.8).
			TextColor(paint(colors.Text.Secondary))
	}
}

func (p *PageContext) framelessButtonAppearance() buttonAppearance {
	colors, d := p.Theme.Colors, p.uiColors()
	return buttonAppearance{
		height: 62, padding: 30, radius: 14, fontSize: 21, fontWeight: 550,
		iconSize: 21, iconGap: 12, focusShadow: true,
		background: d.framelessButtonFill, foreground: paint(colors.Text.Primary),
		border: d.framelessButtonBorder,
	}
}

func (p *PageContext) drawerButtonAppearance() buttonAppearance {
	d := p.uiColors()
	return buttonAppearance{
		height: 56, padding: 26, radius: 13, fontSize: 20, fontWeight: 550,
		iconSize: 20, iconGap: 11, focusShadow: true,
		background: d.secondaryFill, foreground: d.secondaryText, border: d.secondaryBorder,
	}
}
