package tvui

import (
	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/theme"
)

func nativeTheme(palette theme.Theme) ui.Theme {
	c := palette.Colors
	return ui.Theme{
		Dark: palette.Dark, Background: paint(c.Surface.Canvas), Surface: paint(c.Surface.Raised),
		SurfaceHover: paint(c.Hover.Background), SurfacePressed: paint(c.Pressed.Background),
		Border: paint(c.Border.Subtle), Text: paint(c.Text.Primary), TextMuted: paint(c.Text.Muted),
		Accent: paint(c.Accent.Primary), AccentText: paint(c.Accent.Foreground),
		AccentHover: paint(c.Accent.Soft), AccentPressed: paint(c.Accent.Strong),
		Focus: paint(c.Focus.Ring), FocusRingWidth: 4, FocusRingOffset: 0,
		Danger: paint(c.Status.Error.Foreground), Warning: paint(c.Status.Warning.Foreground),
		Success: paint(c.Status.Success.Foreground), Selection: paint(c.Selected.Background),
		Radius: 12, Spacing: 4, FontSize: 24,
	}
}

func paint(color theme.Color) ui.Color {
	c := color.NRGBA()
	return ui.Color{R: c.R, G: c.G, B: c.B, A: c.A}
}

func withAlpha(color theme.Color, alpha uint8) ui.Color {
	c := paint(color)
	c.A = alpha
	return c
}

// uiColors derives deterministic component tints from the 46 canonical §7.1 theme tokens,
// preserving MikuTV Dark parity while adapting all surfaces cleanly to light or custom themes.
type uiColors struct {
	secondaryFill   ui.Color
	secondaryText   ui.Color
	secondaryBorder ui.Color
	subtleFill      ui.Color
	subtleHoverFill ui.Color
	subtleBorder    ui.Color
	strongBorder    ui.Color
	focusSubtext    ui.Color
	dialogStart           ui.Color
	dialogEnd             ui.Color
	dialogBorder          ui.Color
	drawerStart           ui.Color
	drawerEnd             ui.Color
	drawerBorder          ui.Color
	drawerAccentRim       ui.Color
	drawerHairlineStart   ui.Color
	drawerHairlineEnd     ui.Color
	drawerScrimLeft       ui.Color
	drawerScrimMid        ui.Color
	drawerScrimRight      ui.Color
	framelessScrim        ui.Color
	framelessGlow         ui.Color
	framelessButtonFill   ui.Color
	framelessButtonBorder ui.Color
	toastFill             ui.Color
	toastText       ui.Color
	toastBorder     ui.Color
	coverFill       ui.Color
	coverShade      ui.Color
	badgeFill       ui.Color
	badgeBorder     ui.Color
	switchOff       ui.Color
	playbackMenu    ui.Color
	sliderTrack     theme.Color
	sliderEdge      theme.Color
	sliderFillEdge  theme.Color
	spinnerTrack    ui.Color
	avatarStart     ui.Color
	avatarEnd       ui.Color
	avatarBorder    ui.Color
	accountCardFrom     ui.Color
	accountCardTo       ui.Color
	profileGlow         ui.Color
	privateOwnBubble    ui.Color
	privateOwnBorder    ui.Color
	privateSelectedFill ui.Color
}

func deriveColors(t theme.Theme) uiColors {
	c := t.Colors
	secondaryText := theme.Blend(c.Text.Primary, c.Text.Secondary, 0.28)
	focusSubtext := theme.Blend(c.Focus.Foreground, c.Focus.Background, 0.25)
	sliderTrack := theme.Blend(c.Overlay.PlayerBottom.WithAlpha(0xff), c.Playback.Foreground, 0.22)
	sliderEdge := theme.Blend(c.Overlay.PlayerBottom.WithAlpha(0xff), c.Playback.Foreground, 0.36)
	sliderFillEdge := theme.Blend(c.Accent.Soft, c.Overlay.PlayerBottom.WithAlpha(0xff), 0.14)
	dialogStart := c.Surface.Raised.WithAlpha(0xf0)
	dialogEnd := c.Surface.Panel.WithAlpha(0xf5)
	drawerStart := c.Surface.Raised.WithAlpha(0xe6)
	drawerEnd := c.Surface.Canvas.WithAlpha(0xf6)
	drawerBorder := withAlpha(c.Text.Primary, 0x18)
	framelessScrim := withAlpha(c.Surface.Canvas, 0xd8)
	if !t.Dark {
		dialogStart = c.Surface.Panel.WithAlpha(0xfa)
		dialogEnd = c.Surface.Raised.WithAlpha(0xf8)
		drawerStart = c.Surface.Panel.WithAlpha(0xf8)
		drawerEnd = c.Surface.Canvas.WithAlpha(0xfc)
		drawerBorder = paint(c.Border.Subtle)
		framelessScrim = withAlpha(c.Surface.Canvas, 0xe8)
	}
	return uiColors{
		secondaryFill:         withAlpha(c.Text.Primary, 0x13),
		secondaryText:         paint(secondaryText),
		secondaryBorder:       paint(c.Border.Subtle),
		subtleFill:            withAlpha(c.Text.Primary, 0x06),
		subtleHoverFill:       withAlpha(c.Text.Primary, 0x0c),
		subtleBorder:          paint(c.Border.Subtle),
		strongBorder:          paint(theme.Blend(c.Border.Subtle, c.Border.Strong, 0.45)),
		focusSubtext:          paint(focusSubtext),
		dialogStart:           paint(dialogStart),
		dialogEnd:             paint(dialogEnd),
		dialogBorder:          paint(theme.Blend(c.Border.Subtle, c.Border.Strong, 0.25)),
		drawerStart:           paint(drawerStart),
		drawerEnd:             paint(drawerEnd),
		drawerBorder:          drawerBorder,
		drawerAccentRim:       withAlpha(c.Accent.Soft, 0x16),
		drawerHairlineStart:   withAlpha(c.Text.Primary, 0x1f),
		drawerHairlineEnd:     withAlpha(c.Text.Primary, 0x06),
		drawerScrimLeft:       withAlpha(c.Surface.Canvas, 0x38),
		drawerScrimMid:        withAlpha(c.Surface.Canvas, 0x8c),
		drawerScrimRight:      withAlpha(c.Surface.Canvas, 0xe8),
		framelessScrim:        framelessScrim,
		framelessGlow:         withAlpha(c.Accent.Primary, 0x18),
		framelessButtonFill:   withAlpha(c.Text.Primary, 0x14),
		framelessButtonBorder: withAlpha(c.Text.Primary, 0x24),
		toastFill:             withAlpha(c.Focus.Background, 0xee),
		toastText:       paint(c.Focus.Foreground),
		toastBorder:     withAlpha(c.Focus.Background, 0x88),
		coverFill:       paint(theme.Blend(c.Surface.Panel, c.Surface.Raised, 0.35)),
		coverShade:      withAlpha(c.Overlay.PlayerBottom, 0xcc),
		badgeFill:       withAlpha(c.Overlay.PlayerBottom, 0xb8),
		badgeBorder:     withAlpha(c.Playback.Foreground, 0x26),
		switchOff:       paint(theme.Blend(c.Surface.Raised, c.Text.Muted, 0.45)),
		playbackMenu:    withAlpha(theme.Blend(c.Overlay.PlayerBottom.WithAlpha(0xff), c.Playback.Foreground, 0.08), 0xf6),
		sliderTrack:     sliderTrack,
		sliderEdge:      sliderEdge,
		sliderFillEdge:  sliderFillEdge,
		spinnerTrack:    withAlpha(c.Accent.Soft, 0x28),
		avatarStart:     paint(theme.Blend(c.Surface.Raised, c.Text.Secondary, 0.32)),
		avatarEnd:       paint(theme.Blend(c.Surface.Panel, c.Surface.Raised, 0.65)),
		avatarBorder:    withAlpha(c.Text.Secondary, 0x45),
		accountCardFrom:     paint(theme.Blend(c.Decoration.CardStart, c.Surface.Raised, 0.22)),
		accountCardTo:       paint(theme.Blend(c.Decoration.CardEnd, c.Surface.Panel, 0.35)),
		profileGlow:         withAlpha(c.Decoration.Ambient, 0x28),
		privateOwnBubble:    paint(theme.Blend(c.Surface.Panel, c.Accent.Primary, 0.22)),
		privateOwnBorder:    withAlpha(c.Accent.Primary, 0x38),
		privateSelectedFill: withAlpha(c.Accent.Primary, 0x14),
	}
}

func (p *PageContext) uiColors() uiColors {
	return deriveColors(p.Theme)
}
