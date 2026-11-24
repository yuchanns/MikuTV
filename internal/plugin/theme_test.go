package plugin

import (
	"errors"
	"testing"

	"github.com/yuchanns/MikuTV/internal/theme"
)

func TestOpenThemeValidatesSchemaRejectsSourceFieldsAndLoadsWithoutJS(t *testing.T) {
	validZip := buildTestPluginZip(t, map[string]string{
		"plugin.json": `{
			"id": "tokyonight",
			"name": "Tokyo Night",
			"description": "Night city indigo palette",
			"kind": "theme",
			"version": "1.0.0",
			"apiVersion": 1,
			"minAppVersion": "0.1.0",
			"license": "MIT",
			"theme": "theme.json"
		}`,
		"theme.json": `{
			"schemaVersion": 1,
			"base": "mikutv-dark",
			"appearance": "dark",
			"colors": {
				"surface": {
					"canvas": "#1A1B26",
					"sidebar": "#16161E",
					"panel": "#1F2335",
					"raised": "#292E42",
					"input": "#1B1E2E"
				},
				"accent": {
					"primary": "#7AA2F7",
					"soft": "#9ECE6A",
					"strong": "#7DCFFF",
					"foreground": "#15161E"
				},
				"decoration": {
					"cardStart": "#24283B",
					"cardEnd": "#16161E"
				}
			}
		}`,
		"LICENSE": "MIT License\nCopyright 2026 Demo",
	})
	pkg, err := OpenTheme(validZip, "0.1.0")
	if err != nil {
		t.Fatalf("OpenTheme valid: %v", err)
	}
	if pkg.Manifest().ID != "tokyonight" || pkg.Theme().Colors.Accent.Primary != 0x7aa2f7ff || pkg.LicenseText() == "" {
		t.Fatalf("unexpected opened theme: manifest=%+v accent=%08x license=%q",
			pkg.Manifest(), pkg.Theme().Colors.Accent.Primary, pkg.LicenseText())
	}

	// InspectPackage now marks valid themes CompatibleOK and extracts swatches!
	inspected, err := InspectPackage(validZip, "0.1.0")
	if err != nil {
		t.Fatalf("InspectPackage valid theme: %v", err)
	}
	if !inspected.Compatibility.Compatible || inspected.Compatibility.Code != CompatibleOK || inspected.Swatches[3] != 0x7aa2f7ff {
		t.Fatalf("expected compatible theme with swatches, got %+v", inspected)
	}

	// Reject theme declaring content-source fields (main / permissions / capabilities).
	badSourceFieldZip := buildTestPluginZip(t, map[string]string{
		"plugin.json": `{
			"id": "bad-theme",
			"name": "Bad Theme",
			"kind": "theme",
			"version": "1.0.0",
			"apiVersion": 1,
			"minAppVersion": "0.1.0",
			"theme": "theme.json",
			"main": "index.js"
		}`,
		"theme.json": `{"schemaVersion":1,"base":"mikutv-dark","appearance":"dark","colors":{"border":{"subtle":"#FFFFFF22"}}}`,
	})
	if _, err := OpenTheme(badSourceFieldZip, "0.1.0"); err == nil {
		t.Fatal("expected theme with main field to be rejected")
	}

	// Reject theme with path traversal.
	badPathZip := buildTestPluginZip(t, map[string]string{
		"plugin.json": `{
			"id": "bad-path",
			"name": "Bad Path",
			"kind": "theme",
			"version": "1.0.0",
			"apiVersion": 1,
			"minAppVersion": "0.1.0",
			"theme": "../escape.json"
		}`,
	})
	if _, err := OpenTheme(badPathZip, "0.1.0"); err == nil {
		t.Fatal("expected path traversal theme path to be rejected")
	}

	// Reject theme failing contrast gate.
	badContrastZip := buildTestPluginZip(t, map[string]string{
		"plugin.json": `{
			"id": "low-contrast",
			"name": "Low Contrast",
			"kind": "theme",
			"version": "1.0.0",
			"apiVersion": 1,
			"minAppVersion": "0.1.0",
			"theme": "theme.json"
		}`,
		"theme.json": `{"schemaVersion":1,"base":"mikutv-dark","appearance":"dark","colors":{"text":{"primary":"#111111"}}}`,
	})
	if _, err := OpenTheme(badContrastZip, "0.1.0"); !errors.Is(err, theme.ErrContrastFailed) {
		t.Fatalf("expected ErrContrastFailed, got %v", err)
	}
}
