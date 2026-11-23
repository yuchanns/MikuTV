package theme

import (
	"errors"
	"testing"
)

func TestParseSparseThemeAndStrictValidation(t *testing.T) {
	// 1. Valid sparse override (Twilight dark theme) merges onto MikuTV Dark baseline and passes all contrast gates.
	validSparse := `{
		"schemaVersion": 1,
		"base": "mikutv-dark",
		"appearance": "dark",
		"colors": {
			"surface": {
				"canvas": "#0E0B1A",
				"sidebar": "#0A0814",
				"panel": "#171229",
				"raised": "#211A3A",
				"input": "#151026"
			},
			"accent": {
				"primary": "#A78BFA",
				"soft": "#C4B5FD",
				"strong": "#8B6DF0",
				"foreground": "#120B24"
			},
			"selected": {
				"background": "#342657",
				"foreground": "#F4F5F8",
				"focusBackground": "#5B3FA6",
				"focusForeground": "#FFFFFF"
			},
			"decoration": {
				"ambient": "#5B3FA626",
				"cardStart": "#2B1E4A",
				"cardEnd": "#120D22"
			}
		}
	}`
	parsed, err := Parse("twilight", "Twilight", []byte(validSparse))
	if err != nil {
		t.Fatalf("Parse valid sparse theme: %v", err)
	}
	if parsed.ID != "twilight" || parsed.Name != "Twilight" || !parsed.Dark {
		t.Fatalf("unexpected theme metadata: %+v", parsed)
	}
	if parsed.Colors.Accent.Primary != 0xa78bfaff || parsed.Colors.Text.Primary != Default().Colors.Text.Primary {
		t.Fatalf("sparse merge failed: accent=%08x text=%08x", parsed.Colors.Accent.Primary, parsed.Colors.Text.Primary)
	}
	swatches := parsed.Swatches()
	if swatches[0] != 0x0e0b1aff || swatches[3] != 0xa78bfaff {
		t.Fatalf("unexpected swatches: %+v", swatches)
	}

	// 2. Reject empty colors override.
	if _, err := Parse("empty", "Empty", []byte(`{"schemaVersion":1,"base":"mikutv-dark","appearance":"dark","colors":{}}`)); !errors.Is(err, ErrInvalidTheme) {
		t.Fatalf("expected ErrInvalidTheme for empty colors, got %v", err)
	}

	// 3. Reject unknown field (no silent typos).
	if _, err := Parse("typo", "Typo", []byte(`{"schemaVersion":1,"base":"mikutv-dark","appearance":"dark","colors":{"surface":{"canvass":"#000000"}}}`)); !errors.Is(err, ErrInvalidTheme) {
		t.Fatalf("expected ErrInvalidTheme for unknown field, got %v", err)
	}

	// 4. Reject explicit null.
	if _, err := Parse("null", "Null", []byte(`{"schemaVersion":1,"base":"mikutv-dark","appearance":"dark","colors":{"surface":{"canvas":null}}}`)); !errors.Is(err, ErrInvalidTheme) {
		t.Fatalf("expected ErrInvalidTheme for explicit null, got %v", err)
	}

	// 5. Reject unsupported schemaVersion and unknown base.
	if _, err := Parse("v2", "V2", []byte(`{"schemaVersion":2,"base":"mikutv-dark","appearance":"dark","colors":{"surface":{"canvas":"#000000"}}}`)); !errors.Is(err, ErrUnsupportedSchema) {
		t.Fatalf("expected ErrUnsupportedSchema, got %v", err)
	}
	if _, err := Parse("badbase", "BadBase", []byte(`{"schemaVersion":1,"base":"other-theme","appearance":"dark","colors":{"surface":{"canvas":"#000000"}}}`)); !errors.Is(err, ErrInvalidTheme) {
		t.Fatalf("expected ErrInvalidTheme for unknown base, got %v", err)
	}

	// 6. Reject translucent color on opaque-only field (surface.canvas), accept translucent on border.subtle!
	if _, err := Parse("alpha", "Alpha", []byte(`{"schemaVersion":1,"base":"mikutv-dark","appearance":"dark","colors":{"surface":{"canvas":"#080B1180"}}}`)); !errors.Is(err, ErrInvalidTheme) {
		t.Fatalf("expected ErrInvalidTheme for translucent canvas, got %v", err)
	}
	if _, err := Parse("border-alpha", "BorderAlpha", []byte(`{"schemaVersion":1,"base":"mikutv-dark","appearance":"dark","colors":{"border":{"subtle":"#FFFFFF22"}}}`)); err != nil {
		t.Fatalf("translucent border.subtle should be allowed, got %v", err)
	}

	// 7. Reject unpaired foreground/background override (e.g. accent.primary without accent.foreground, hover.background without hover.foreground).
	if _, err := Parse("unpaired-accent", "Unpaired", []byte(`{"schemaVersion":1,"base":"mikutv-dark","appearance":"dark","colors":{"accent":{"primary":"#39C5BB"}}}`)); !errors.Is(err, ErrInvalidTheme) {
		t.Fatalf("expected ErrInvalidTheme for unpaired accent.primary, got %v", err)
	}
	if _, err := Parse("unpaired-hover", "Unpaired", []byte(`{"schemaVersion":1,"base":"mikutv-dark","appearance":"dark","colors":{"hover":{"background":"#1A222D"}}}`)); !errors.Is(err, ErrInvalidTheme) {
		t.Fatalf("expected ErrInvalidTheme for unpaired hover.background, got %v", err)
	}

	// 8. Reject failing contrast gate (dark text on dark surface).
	if _, err := Parse("low-contrast", "LowContrast", []byte(`{"schemaVersion":1,"base":"mikutv-dark","appearance":"dark","colors":{"text":{"muted":"#222630"}}}`)); !errors.Is(err, ErrContrastFailed) {
		t.Fatalf("expected ErrContrastFailed for low contrast text.muted, got %v", err)
	}

	// 9. Reject oversized > 64 KiB payload.
	big := make([]byte, MaxThemeJSONBytes+1)
	if _, err := Parse("big", "Big", big); !errors.Is(err, ErrInvalidTheme) {
		t.Fatalf("expected ErrInvalidTheme for >64 KiB payload, got %v", err)
	}
}
