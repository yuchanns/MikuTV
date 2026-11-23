package theme

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
)

const (
	SchemaVersion     = 1
	BaseMikuTVDark    = "mikutv-dark"
	BaseMikuTVLight   = "mikutv-light"
	MaxThemeJSONBytes = 64 << 10
)

// IsBuiltin reports whether id refers to a built-in MikuTV baseline theme.
func IsBuiltin(id string) bool {
	trimmed := strings.TrimSpace(id)
	return trimmed == "" || strings.EqualFold(trimmed, BaseMikuTVDark) || strings.EqualFold(trimmed, BaseMikuTVLight)
}

var (
	ErrInvalidTheme      = errors.New("invalid theme configuration")
	ErrUnsupportedSchema = errors.New("unsupported theme schema version")
	ErrContrastFailed    = errors.New("theme failed contrast gate")
)

// Swatches returns 6 representative colors in a 2x3 grid matching the UI
// palette preview: [canvas, panel, accentSurface, accentPrimary, accentSoft, textPrimary].
func (t Theme) Swatches() [6]Color {
	c := t.Colors
	third := c.Decoration.CardStart
	if !t.Dark || (c.Selected.Background != Default().Colors.Selected.Background &&
		c.Selected.Background != DefaultLight().Colors.Selected.Background) {
		third = c.Selected.Background
	}
	return [6]Color{
		c.Surface.Canvas,
		c.Surface.Panel,
		third,
		c.Accent.Primary,
		c.Accent.Soft,
		c.Text.Primary,
	}
}

type rawThemeFile struct {
	SchemaVersion *int            `json:"schemaVersion"`
	Base          *string         `json:"base"`
	Appearance    *string         `json:"appearance"`
	Colors        *rawThemeColors `json:"colors"`
}

type rawThemeColors struct {
	Surface    *rawSurfaceColors    `json:"surface"`
	Text       *rawTextColors       `json:"text"`
	Accent     *rawAccentColors     `json:"accent"`
	Border     *rawBorderColors     `json:"border"`
	Focus      *rawFocusColors      `json:"focus"`
	Selected   *rawSelectedColors   `json:"selected"`
	Hover      *rawPairColors       `json:"hover"`
	Pressed    *rawPairColors       `json:"pressed"`
	Disabled   *rawPairColors       `json:"disabled"`
	Status     *rawStatusColors     `json:"status"`
	Overlay    *rawOverlayColors    `json:"overlay"`
	Playback   *rawPlaybackColors   `json:"playback"`
	Decoration *rawDecorationColors `json:"decoration"`
}

type rawSurfaceColors struct {
	Canvas  json.RawMessage `json:"canvas"`
	Sidebar json.RawMessage `json:"sidebar"`
	Panel   json.RawMessage `json:"panel"`
	Raised  json.RawMessage `json:"raised"`
	Input   json.RawMessage `json:"input"`
}

type rawTextColors struct {
	Primary   json.RawMessage `json:"primary"`
	Secondary json.RawMessage `json:"secondary"`
	Muted     json.RawMessage `json:"muted"`
}

type rawAccentColors struct {
	Primary    json.RawMessage `json:"primary"`
	Soft       json.RawMessage `json:"soft"`
	Strong     json.RawMessage `json:"strong"`
	Foreground json.RawMessage `json:"foreground"`
}

type rawBorderColors struct {
	Subtle json.RawMessage `json:"subtle"`
	Strong json.RawMessage `json:"strong"`
}

type rawFocusColors struct {
	Background json.RawMessage `json:"background"`
	Foreground json.RawMessage `json:"foreground"`
	Ring       json.RawMessage `json:"ring"`
}

type rawSelectedColors struct {
	Background      json.RawMessage `json:"background"`
	Foreground      json.RawMessage `json:"foreground"`
	FocusBackground json.RawMessage `json:"focusBackground"`
	FocusForeground json.RawMessage `json:"focusForeground"`
}

type rawPairColors struct {
	Background json.RawMessage `json:"background"`
	Foreground json.RawMessage `json:"foreground"`
}

type rawStatusColors struct {
	Error   *rawPairColors `json:"error"`
	Warning *rawPairColors `json:"warning"`
	Success *rawPairColors `json:"success"`
}

type rawOverlayColors struct {
	Scrim        json.RawMessage `json:"scrim"`
	PlayerTop    json.RawMessage `json:"playerTop"`
	PlayerBottom json.RawMessage `json:"playerBottom"`
	Shadow       json.RawMessage `json:"shadow"`
}

type rawPlaybackColors struct {
	Foreground      json.RawMessage `json:"foreground"`
	Secondary       json.RawMessage `json:"secondary"`
	Track           json.RawMessage `json:"track"`
	Progress        json.RawMessage `json:"progress"`
	Subtitle        json.RawMessage `json:"subtitle"`
	SubtitleOutline json.RawMessage `json:"subtitleOutline"`
}

type rawDecorationColors struct {
	Ambient   json.RawMessage `json:"ambient"`
	CardStart json.RawMessage `json:"cardStart"`
	CardEnd   json.RawMessage `json:"cardEnd"`
}

// Parse validates a theme.json payload against §7.1, merges sparse overrides
// onto the built-in baseline, and enforces all opacity, pairing, and WCAG
// contrast gates before returning an immutable Theme value.
func Parse(id, name string, data []byte) (Theme, error) {
	if len(data) == 0 {
		return Theme{}, fmt.Errorf("%w: empty theme file", ErrInvalidTheme)
	}
	if len(data) > MaxThemeJSONBytes {
		return Theme{}, fmt.Errorf("%w: theme file exceeds %d bytes", ErrInvalidTheme, MaxThemeJSONBytes)
	}
	if err := rejectExplicitNulls(data); err != nil {
		return Theme{}, err
	}
	var raw rawThemeFile
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&raw); err != nil {
		return Theme{}, fmt.Errorf("%w: %v", ErrInvalidTheme, err)
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		return Theme{}, fmt.Errorf("%w: unexpected trailing data", ErrInvalidTheme)
	}

	if raw.SchemaVersion == nil {
		return Theme{}, fmt.Errorf("%w: schemaVersion is required", ErrInvalidTheme)
	}
	if *raw.SchemaVersion != SchemaVersion {
		return Theme{}, fmt.Errorf("%w: got %d want %d", ErrUnsupportedSchema, *raw.SchemaVersion, SchemaVersion)
	}
	if raw.Base == nil || (*raw.Base != BaseMikuTVDark && *raw.Base != BaseMikuTVLight) {
		return Theme{}, fmt.Errorf("%w: unsupported base theme", ErrInvalidTheme)
	}
	if raw.Appearance == nil || (*raw.Appearance != "dark" && *raw.Appearance != "light") {
		return Theme{}, fmt.Errorf("%w: appearance must be dark or light", ErrInvalidTheme)
	}
	if raw.Colors == nil {
		return Theme{}, fmt.Errorf("%w: colors object is required", ErrInvalidTheme)
	}

	resolved := Default()
	if *raw.Base == BaseMikuTVLight {
		resolved = DefaultLight()
	}
	if strings.TrimSpace(id) != "" {
		resolved.ID = strings.TrimSpace(id)
	}
	if strings.TrimSpace(name) != "" {
		resolved.Name = strings.TrimSpace(name)
	}
	resolved.Dark = *raw.Appearance == "dark"

	overrides := 0
	applyColor := func(field string, msg json.RawMessage, allowAlpha bool, dst *Color) (bool, error) {
		if len(msg) == 0 {
			return false, nil
		}
		var lit string
		if err := json.Unmarshal(msg, &lit); err != nil {
			return false, fmt.Errorf("%w: %s must be a color string", ErrInvalidTheme, field)
		}
		parsed, err := ParseColorLiteral(lit, allowAlpha)
		if err != nil {
			return false, fmt.Errorf("%w: %s: %v", ErrInvalidTheme, field, err)
		}
		*dst = parsed
		overrides++
		return true, nil
	}
	applyPair := func(prefix string, pair *rawPairColors, dst *Pair) error {
		if pair == nil {
			return nil
		}
		hasBg, err := applyColor(prefix+".background", pair.Background, false, &dst.Background)
		if err != nil {
			return err
		}
		hasFg, err := applyColor(prefix+".foreground", pair.Foreground, false, &dst.Foreground)
		if err != nil {
			return err
		}
		if hasBg != hasFg {
			return fmt.Errorf("%w: %s background and foreground must be provided together", ErrInvalidTheme, prefix)
		}
		return nil
	}

	rc := raw.Colors
	c := &resolved.Colors
	if rc.Surface != nil {
		for _, item := range []struct {
			name string
			raw  json.RawMessage
			dst  *Color
		}{
			{"surface.canvas", rc.Surface.Canvas, &c.Surface.Canvas},
			{"surface.sidebar", rc.Surface.Sidebar, &c.Surface.Sidebar},
			{"surface.panel", rc.Surface.Panel, &c.Surface.Panel},
			{"surface.raised", rc.Surface.Raised, &c.Surface.Raised},
			{"surface.input", rc.Surface.Input, &c.Surface.Input},
		} {
			if _, err := applyColor(item.name, item.raw, false, item.dst); err != nil {
				return Theme{}, err
			}
		}
	}
	if rc.Text != nil {
		for _, item := range []struct {
			name string
			raw  json.RawMessage
			dst  *Color
		}{
			{"text.primary", rc.Text.Primary, &c.Text.Primary},
			{"text.secondary", rc.Text.Secondary, &c.Text.Secondary},
			{"text.muted", rc.Text.Muted, &c.Text.Muted},
		} {
			if _, err := applyColor(item.name, item.raw, false, item.dst); err != nil {
				return Theme{}, err
			}
		}
	}
	if rc.Accent != nil {
		hasPrimary, err := applyColor("accent.primary", rc.Accent.Primary, false, &c.Accent.Primary)
		if err != nil {
			return Theme{}, err
		}
		if _, err := applyColor("accent.soft", rc.Accent.Soft, false, &c.Accent.Soft); err != nil {
			return Theme{}, err
		}
		if _, err := applyColor("accent.strong", rc.Accent.Strong, false, &c.Accent.Strong); err != nil {
			return Theme{}, err
		}
		hasFg, err := applyColor("accent.foreground", rc.Accent.Foreground, false, &c.Accent.Foreground)
		if err != nil {
			return Theme{}, err
		}
		if hasPrimary != hasFg {
			return Theme{}, fmt.Errorf("%w: accent.primary and accent.foreground must be provided together", ErrInvalidTheme)
		}
	}
	if rc.Border != nil {
		if _, err := applyColor("border.subtle", rc.Border.Subtle, true, &c.Border.Subtle); err != nil {
			return Theme{}, err
		}
		if _, err := applyColor("border.strong", rc.Border.Strong, true, &c.Border.Strong); err != nil {
			return Theme{}, err
		}
	}
	if rc.Focus != nil {
		hasBg, err := applyColor("focus.background", rc.Focus.Background, false, &c.Focus.Background)
		if err != nil {
			return Theme{}, err
		}
		hasFg, err := applyColor("focus.foreground", rc.Focus.Foreground, false, &c.Focus.Foreground)
		if err != nil {
			return Theme{}, err
		}
		if hasBg != hasFg {
			return Theme{}, fmt.Errorf("%w: focus.background and focus.foreground must be provided together", ErrInvalidTheme)
		}
		if _, err := applyColor("focus.ring", rc.Focus.Ring, false, &c.Focus.Ring); err != nil {
			return Theme{}, err
		}
	}
	if rc.Selected != nil {
		hasBg, err := applyColor("selected.background", rc.Selected.Background, false, &c.Selected.Background)
		if err != nil {
			return Theme{}, err
		}
		hasFg, err := applyColor("selected.foreground", rc.Selected.Foreground, false, &c.Selected.Foreground)
		if err != nil {
			return Theme{}, err
		}
		if hasBg != hasFg {
			return Theme{}, fmt.Errorf("%w: selected.background and selected.foreground must be provided together", ErrInvalidTheme)
		}
		hasFBg, err := applyColor("selected.focusBackground", rc.Selected.FocusBackground, false, &c.Selected.FocusBackground)
		if err != nil {
			return Theme{}, err
		}
		hasFFg, err := applyColor("selected.focusForeground", rc.Selected.FocusForeground, false, &c.Selected.FocusForeground)
		if err != nil {
			return Theme{}, err
		}
		if hasFBg != hasFFg {
			return Theme{}, fmt.Errorf("%w: selected.focusBackground and selected.focusForeground must be provided together", ErrInvalidTheme)
		}
	}
	if err := applyPair("hover", rc.Hover, &c.Hover); err != nil {
		return Theme{}, err
	}
	if err := applyPair("pressed", rc.Pressed, &c.Pressed); err != nil {
		return Theme{}, err
	}
	if err := applyPair("disabled", rc.Disabled, &c.Disabled); err != nil {
		return Theme{}, err
	}
	if rc.Status != nil {
		if err := applyPair("status.error", rc.Status.Error, &c.Status.Error); err != nil {
			return Theme{}, err
		}
		if err := applyPair("status.warning", rc.Status.Warning, &c.Status.Warning); err != nil {
			return Theme{}, err
		}
		if err := applyPair("status.success", rc.Status.Success, &c.Status.Success); err != nil {
			return Theme{}, err
		}
	}
	if rc.Overlay != nil {
		for _, item := range []struct {
			name string
			raw  json.RawMessage
			dst  *Color
		}{
			{"overlay.scrim", rc.Overlay.Scrim, &c.Overlay.Scrim},
			{"overlay.playerTop", rc.Overlay.PlayerTop, &c.Overlay.PlayerTop},
			{"overlay.playerBottom", rc.Overlay.PlayerBottom, &c.Overlay.PlayerBottom},
			{"overlay.shadow", rc.Overlay.Shadow, &c.Overlay.Shadow},
		} {
			if _, err := applyColor(item.name, item.raw, true, item.dst); err != nil {
				return Theme{}, err
			}
		}
	}
	if rc.Playback != nil {
		for _, item := range []struct {
			name       string
			raw        json.RawMessage
			allowAlpha bool
			dst        *Color
		}{
			{"playback.foreground", rc.Playback.Foreground, false, &c.Playback.Foreground},
			{"playback.secondary", rc.Playback.Secondary, false, &c.Playback.Secondary},
			{"playback.track", rc.Playback.Track, true, &c.Playback.Track},
			{"playback.progress", rc.Playback.Progress, false, &c.Playback.Progress},
			{"playback.subtitle", rc.Playback.Subtitle, false, &c.Playback.Subtitle},
			{"playback.subtitleOutline", rc.Playback.SubtitleOutline, false, &c.Playback.SubtitleOutline},
		} {
			if _, err := applyColor(item.name, item.raw, item.allowAlpha, item.dst); err != nil {
				return Theme{}, err
			}
		}
	}
	if rc.Decoration != nil {
		if _, err := applyColor("decoration.ambient", rc.Decoration.Ambient, true, &c.Decoration.Ambient); err != nil {
			return Theme{}, err
		}
		if _, err := applyColor("decoration.cardStart", rc.Decoration.CardStart, false, &c.Decoration.CardStart); err != nil {
			return Theme{}, err
		}
		if _, err := applyColor("decoration.cardEnd", rc.Decoration.CardEnd, false, &c.Decoration.CardEnd); err != nil {
			return Theme{}, err
		}
	}

	if overrides == 0 {
		return Theme{}, fmt.Errorf("%w: colors must override at least one field", ErrInvalidTheme)
	}
	if err := ValidateContrast(resolved); err != nil {
		return Theme{}, err
	}
	return resolved, nil
}

// ParseColorLiteral parses a strict #RRGGBB or #RRGGBBAA sRGB literal.
func ParseColorLiteral(raw string, allowAlpha bool) (Color, error) {
	if len(raw) != 7 && len(raw) != 9 || raw[0] != '#' {
		return 0, fmt.Errorf("expected #RRGGBB or #RRGGBBAA")
	}
	var bytesOut [4]uint8
	bytesOut[3] = 0xff
	pairs := (len(raw) - 1) / 2
	for i := range pairs {
		hi, ok1 := hexNibble(raw[1+i*2])
		lo, ok2 := hexNibble(raw[2+i*2])
		if !ok1 || !ok2 {
			return 0, fmt.Errorf("invalid hex digit")
		}
		bytesOut[i] = (hi << 4) | lo
	}
	if !allowAlpha && bytesOut[3] != 0xff {
		return 0, fmt.Errorf("translucent color is not allowed for this field")
	}
	return Color(bytesOut[0])<<24 | Color(bytesOut[1])<<16 | Color(bytesOut[2])<<8 | Color(bytesOut[3]), nil
}

func hexNibble(ch byte) (uint8, bool) {
	switch {
	case ch >= '0' && ch <= '9':
		return ch - '0', true
	case ch >= 'a' && ch <= 'f':
		return ch - 'a' + 10, true
	case ch >= 'A' && ch <= 'F':
		return ch - 'A' + 10, true
	default:
		return 0, false
	}
}

func rejectExplicitNulls(data []byte) error {
	var generic any
	if err := json.Unmarshal(data, &generic); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidTheme, err)
	}
	var walk func(v any, path string) error
	walk = func(v any, path string) error {
		switch node := v.(type) {
		case nil:
			return fmt.Errorf("%w: explicit null at %s", ErrInvalidTheme, path)
		case map[string]any:
			for k, child := range node {
				next := k
				if path != "" {
					next = path + "." + k
				}
				if err := walk(child, next); err != nil {
					return err
				}
			}
		case []any:
			for i, child := range node {
				if err := walk(child, fmt.Sprintf("%s[%d]", path, i)); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return walk(generic, "")
}

// ContrastRatio returns the WCAG 2.x luminance contrast ratio between two opaque colors.
func ContrastRatio(a, b Color) float64 {
	lum := func(c Color) float64 {
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
	la, lb := lum(a), lum(b)
	return (max(la, lb) + 0.05) / (min(la, lb) + 0.05)
}

// ValidateContrast enforces all §7.1 contrast gates on a resolved Theme.
func ValidateContrast(th Theme) error {
	c := th.Colors
	check := func(label string, fg, bg Color, minimum float64) error {
		if ratio := ContrastRatio(fg, bg); ratio < minimum {
			return fmt.Errorf("%w: %s contrast %.2f < %.1f", ErrContrastFailed, label, ratio, minimum)
		}
		return nil
	}

	surfaces := [...]struct {
		name string
		val  Color
	}{
		{"canvas", c.Surface.Canvas},
		{"sidebar", c.Surface.Sidebar},
		{"panel", c.Surface.Panel},
		{"raised", c.Surface.Raised},
		{"input", c.Surface.Input},
	}
	texts := [...]struct {
		name string
		val  Color
	}{
		{"primary", c.Text.Primary},
		{"secondary", c.Text.Secondary},
		{"muted", c.Text.Muted},
	}
	for _, s := range surfaces {
		for _, tx := range texts {
			if err := check("text."+tx.name+"/surface."+s.name, tx.val, s.val, 4.5); err != nil {
				return err
			}
		}
	}

	pairs := [...]struct {
		name string
		fg   Color
		bg   Color
	}{
		{"accent.primary", c.Accent.Foreground, c.Accent.Primary},
		{"accent.soft", c.Accent.Foreground, c.Accent.Soft},
		{"accent.strong", c.Accent.Foreground, c.Accent.Strong},
		{"focus", c.Focus.Foreground, c.Focus.Background},
		{"selected", c.Selected.Foreground, c.Selected.Background},
		{"selected.focus", c.Selected.FocusForeground, c.Selected.FocusBackground},
		{"hover", c.Hover.Foreground, c.Hover.Background},
		{"pressed", c.Pressed.Foreground, c.Pressed.Background},
		{"status.error", c.Status.Error.Foreground, c.Status.Error.Background},
		{"status.warning", c.Status.Warning.Foreground, c.Status.Warning.Background},
		{"status.success", c.Status.Success.Foreground, c.Status.Success.Background},
	}
	for _, p := range pairs {
		if err := check(p.name, p.fg, p.bg, 4.5); err != nil {
			return err
		}
	}

	for _, s := range [...]struct {
		name string
		val  Color
	}{
		{"canvas", c.Surface.Canvas},
		{"panel", c.Surface.Panel},
		{"raised", c.Surface.Raised},
	} {
		if err := check("focus.ring/surface."+s.name, c.Focus.Ring, s.val, 3.0); err != nil {
			return err
		}
	}

	const whiteVideo Color = 0xffffffff
	topOnWhite := Over(c.Overlay.PlayerTop, whiteVideo)
	bottomOnWhite := Over(c.Overlay.PlayerBottom, whiteVideo)
	for _, item := range [...]struct {
		name string
		fg   Color
		bg   Color
	}{
		{"playback.foreground/playerTop", c.Playback.Foreground, topOnWhite},
		{"playback.secondary/playerTop", c.Playback.Secondary, topOnWhite},
		{"playback.foreground/playerBottom", c.Playback.Foreground, bottomOnWhite},
		{"playback.secondary/playerBottom", c.Playback.Secondary, bottomOnWhite},
		{"playback.subtitle/outline", c.Playback.Subtitle, c.Playback.SubtitleOutline},
	} {
		if err := check(item.name, item.fg, item.bg, 4.5); err != nil {
			return err
		}
	}
	return nil
}
