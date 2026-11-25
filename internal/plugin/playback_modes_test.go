package plugin

import (
	"testing"

	"github.com/yuchanns/MikuTV/internal/provider"
)

func TestOpenPackageAllowsZeroExportPlaybackModesCapability(t *testing.T) {
	manifest := `{
		"id": "modes-test", "name": "Modes Test", "kind": "source",
		"version": "1.0.0", "apiVersion": 1, "minAppVersion": "0.1.0",
		"main": "index.js",
		"capabilities": ["catalog.browse", "playback.resolve", "playback.modes"]
	}`
	code := `
		export async function browse() { return { collections: [] }; }
		export async function resolvePlayback() { return { streams: [] }; }
	`
	src, err := Open(t.Context(), zipFiles(t, map[string][]byte{
		"plugin.json": []byte(manifest),
		"index.js":    []byte(code),
	}), testVersion)
	if err != nil {
		t.Fatalf("Open failed with playback.modes: %v", err)
	}
	if !src.Capabilities().Has(provider.PlaybackModes) {
		t.Fatal("expected PlaybackModes capability on opened source")
	}
}
