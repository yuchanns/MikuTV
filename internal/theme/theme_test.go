package theme

import (
	"math"
	"testing"
)

func syntheticLightTheme() Theme {
	t := Theme{ID: "synthetic-light", Name: "Synthetic Light", Dark: false}
	c := &t.Colors
	c.Surface.Canvas = 0xf6f8fbff
	c.Surface.Sidebar = 0xedf1f7ff
	c.Surface.Panel = 0xffffffff
	c.Surface.Raised = 0xe9eef5ff
	c.Surface.Input = 0xffffffff
	c.Text.Primary = 0x101826ff
	c.Text.Secondary = 0x3b485cff
	c.Text.Muted = 0x525f73ff
	c.Accent.Primary = 0x126e67ff
	c.Accent.Soft = 0x167871ff
	c.Accent.Strong = 0x0d554fff
	c.Accent.Foreground = 0xffffffff
	c.Border.Subtle = 0x10182618
	c.Border.Strong = 0x10182644
	c.Focus.Background = 0x101826ff
	c.Focus.Foreground = 0xffffffff
	c.Focus.Ring = 0x0f5f59ff
	c.Selected.Background = 0xd6ece9ff
	c.Selected.Foreground = 0x0d3633ff
	c.Selected.FocusBackground = 0x0f5f59ff
	c.Selected.FocusForeground = 0xffffffff
	c.Hover = Pair{Background: 0xe3e9f2ff, Foreground: 0x101826ff}
	c.Pressed = Pair{Background: 0xcfdbe9ff, Foreground: 0x101826ff}
	c.Disabled = Pair{Background: 0xe8edf4ff, Foreground: 0x828e9eff}
	c.Status.Error = Pair{Background: 0xfde8ecff, Foreground: 0x8f1d32ff}
	c.Status.Warning = Pair{Background: 0xfef4dcff, Foreground: 0x7a5100ff}
	c.Status.Success = Pair{Background: 0xe2f6ecff, Foreground: 0x125e42ff}
	c.Overlay.Scrim = 0x00000099
	c.Overlay.PlayerTop = 0x080b11d9
	c.Overlay.PlayerBottom = 0x080b11f2
	c.Overlay.Shadow = 0x00000050
	c.Playback.Foreground = 0xffffffff
	c.Playback.Secondary = 0xc0cad8ff
	c.Playback.Track = 0xffffff40
	c.Playback.Progress = 0x39c5bbff
	c.Playback.Subtitle = 0xffffffff
	c.Playback.SubtitleOutline = 0x000000ff
	c.Decoration.Ambient = 0x157a7316
	c.Decoration.CardStart = 0xdfecebff
	c.Decoration.CardEnd = 0xebf2f7ff
	return t
}

func TestThemeContrastGates(t *testing.T) {
	for _, tc := range []Theme{Default(), syntheticLightTheme()} {
		t.Run(tc.ID, func(t *testing.T) {
			verifyThemeContrast(t, tc)
		})
	}
}

func verifyThemeContrast(t *testing.T, th Theme) {
	c := th.Colors
	surfaces := map[string]Color{
		"canvas": c.Surface.Canvas, "sidebar": c.Surface.Sidebar,
		"panel": c.Surface.Panel, "raised": c.Surface.Raised, "input": c.Surface.Input,
	}
	texts := map[string]Color{"primary": c.Text.Primary, "secondary": c.Text.Secondary, "muted": c.Text.Muted}
	for name, surface := range surfaces {
		for textName, text := range texts {
			t.Run(name+"/"+textName, func(t *testing.T) { requireContrast(t, text, surface, 4.5) })
		}
	}
	pairs := map[string]Pair{
		"accent":         {c.Accent.Primary, c.Accent.Foreground},
		"accent-soft":    {c.Accent.Soft, c.Accent.Foreground},
		"accent-strong":  {c.Accent.Strong, c.Accent.Foreground},
		"focus":          {c.Focus.Background, c.Focus.Foreground},
		"selected":       {c.Selected.Background, c.Selected.Foreground},
		"selected-focus": {c.Selected.FocusBackground, c.Selected.FocusForeground},
		"hover":          c.Hover,
		"pressed":        c.Pressed,
		"error":          c.Status.Error,
		"warning":        c.Status.Warning,
		"success":        c.Status.Success,
	}
	for name, pair := range pairs {
		t.Run(name, func(t *testing.T) { requireContrast(t, pair.Foreground, pair.Background, 4.5) })
	}
	for _, name := range []string{"canvas", "panel", "raised"} {
		t.Run("ring/"+name, func(t *testing.T) { requireContrast(t, c.Focus.Ring, surfaces[name], 3) })
	}
	// §7.1: playback foreground/secondary against top/bottom overlays composited over white video,
	// plus subtitle text against its outline.
	const whiteVideo Color = 0xffffffff
	topOnWhite := Over(c.Overlay.PlayerTop, whiteVideo)
	bottomOnWhite := Over(c.Overlay.PlayerBottom, whiteVideo)
	t.Run("playback/top-fg", func(t *testing.T) { requireContrast(t, c.Playback.Foreground, topOnWhite, 4.5) })
	t.Run("playback/top-secondary", func(t *testing.T) { requireContrast(t, c.Playback.Secondary, topOnWhite, 4.5) })
	t.Run("playback/bottom-fg", func(t *testing.T) { requireContrast(t, c.Playback.Foreground, bottomOnWhite, 4.5) })
	t.Run("playback/bottom-secondary", func(t *testing.T) { requireContrast(t, c.Playback.Secondary, bottomOnWhite, 4.5) })
	t.Run("playback/subtitle", func(t *testing.T) { requireContrast(t, c.Playback.Subtitle, c.Playback.SubtitleOutline, 4.5) })
}

func requireContrast(t *testing.T, foreground, background Color, minimum float64) {
	t.Helper()
	luminance := func(c Color) float64 {
		v := c.NRGBA()
		linear := func(channel uint8) float64 {
			x := float64(channel) / 255
			if x <= 0.04045 {
				return x / 12.92
			}
			return math.Pow((x+0.055)/1.055, 2.4)
		}
		return 0.2126*linear(v.R) + 0.7152*linear(v.G) + 0.0722*linear(v.B)
	}
	a, b := luminance(foreground), luminance(background)
	ratio := (max(a, b) + 0.05) / (min(a, b) + 0.05)
	if ratio < minimum {
		t.Fatalf("contrast %.2f is below %.1f for %08x on %08x", ratio, minimum, foreground, background)
	}
}
