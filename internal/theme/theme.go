package theme

import (
	"image/color"
	"math"
)

// Color stores sRGB as RRGGBBAA, matching theme package color literals.
type Color uint32

func (c Color) NRGBA() color.NRGBA {
	return color.NRGBA{R: uint8(c >> 24), G: uint8(c >> 16), B: uint8(c >> 8), A: uint8(c)}
}

func (c Color) WithAlpha(alpha uint8) Color {
	return (c & 0xffffff00) | Color(alpha)
}

func FromNRGBA(c color.NRGBA) Color {
	return Color(c.R)<<24 | Color(c.G)<<16 | Color(c.B)<<8 | Color(c.A)
}

// Blend interpolates linearly between a (weight 0) and b (weight 1) in sRGB.
func Blend(a, b Color, weight float64) Color {
	w := min(max(weight, 0), 1)
	ca, cb := a.NRGBA(), b.NRGBA()
	mix := func(x, y uint8) uint8 {
		return uint8(math.Round(float64(x)*(1-w) + float64(y)*w))
	}
	return FromNRGBA(color.NRGBA{
		R: mix(ca.R, cb.R),
		G: mix(ca.G, cb.G),
		B: mix(ca.B, cb.B),
		A: mix(ca.A, cb.A),
	})
}

// Over composites foreground over an opaque background using standard source-over blending.
func Over(foreground, background Color) Color {
	fg, bg := foreground.NRGBA(), background.NRGBA()
	a := float64(fg.A) / 255
	inv := 1 - a
	mix := func(f, b uint8) uint8 {
		return uint8(math.Round(float64(f)*a + float64(b)*inv))
	}
	return FromNRGBA(color.NRGBA{
		R: mix(fg.R, bg.R),
		G: mix(fg.G, bg.G),
		B: mix(fg.B, bg.B),
		A: 0xff,
	})
}

type Pair struct {
	Background Color
	Foreground Color
}

// Colors defines the 46 canonical semantic color tokens specified by theme.json v1.
type Colors struct {
	Surface struct {
		Canvas, Sidebar, Panel, Raised, Input Color
	}
	Text struct {
		Primary, Secondary, Muted Color
	}
	Accent struct {
		Primary, Soft, Strong, Foreground Color
	}
	Border struct {
		Subtle, Strong Color
	}
	Focus struct {
		Background, Foreground, Ring Color
	}
	Selected struct {
		Background, Foreground, FocusBackground, FocusForeground Color
	}
	Hover, Pressed, Disabled Pair
	Status                   struct {
		Error, Warning, Success Pair
	}
	Overlay struct {
		Scrim, PlayerTop, PlayerBottom, Shadow Color
	}
	Playback struct {
		Foreground, Secondary, Track, Progress, Subtitle, SubtitleOutline Color
	}
	Decoration struct {
		Ambient, CardStart, CardEnd Color
	}
}

// Theme is a resolved value. Copies do not share mutable configuration.
type Theme struct {
	ID, Name string
	Dark     bool
	Colors   Colors
}

func Default() Theme {
	t := Theme{ID: "mikutv-dark", Name: "MikuTV Dark", Dark: true}
	c := &t.Colors
	c.Surface.Canvas = 0x080b11ff
	c.Surface.Sidebar = 0x07090eff
	c.Surface.Panel = 0x111723ff
	c.Surface.Raised = 0x192332ff
	c.Surface.Input = 0x101620ff
	c.Text.Primary = 0xf4f5f8ff
	c.Text.Secondary = 0xa7b2c1ff
	c.Text.Muted = 0x9299a8ff
	c.Accent.Primary = 0x39c5bbff
	c.Accent.Soft = 0x96e2d9ff
	c.Accent.Strong = 0x299e95ff
	c.Accent.Foreground = 0x082622ff
	c.Border.Subtle = 0xffffff14
	c.Border.Strong = 0xffffff40
	c.Focus.Background = 0xf4f5f8ff
	c.Focus.Foreground = 0x111722ff
	c.Focus.Ring = 0xf4f6faff
	c.Selected.Background = 0x26303dff
	c.Selected.Foreground = c.Text.Primary
	c.Selected.FocusBackground = 0x166c63ff
	c.Selected.FocusForeground = 0xffffffff
	c.Hover = Pair{Background: 0x1a222dff, Foreground: 0xf4f5f8ff}
	c.Pressed = Pair{Background: 0xd6e6e4ff, Foreground: 0x111722ff}
	c.Disabled = Pair{Background: 0x151b24ff, Foreground: 0x707b8aff}
	c.Status.Error = Pair{Background: 0x3b1c25ff, Foreground: 0xffb4c0ff}
	c.Status.Warning = Pair{Background: 0x3d3018ff, Foreground: 0xf5d48bff}
	c.Status.Success = Pair{Background: 0x133a30ff, Foreground: 0x9ce5c4ff}
	c.Overlay.Scrim = 0x000000b3
	c.Overlay.PlayerTop = 0x080b11d9
	c.Overlay.PlayerBottom = 0x080b11f2
	c.Overlay.Shadow = 0x00000080
	c.Playback.Foreground = 0xffffffff
	c.Playback.Secondary = 0xc0cad8ff
	c.Playback.Track = 0xffffff40
	c.Playback.Progress = 0x39c5bbff
	c.Playback.Subtitle = 0xffffffff
	c.Playback.SubtitleOutline = 0x000000ff
	c.Decoration.Ambient = 0x13365020
	c.Decoration.CardStart = 0x193d42ff
	c.Decoration.CardEnd = 0x0a1b23ff
	return t
}
