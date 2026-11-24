package plugin

import (
	"context"
	"testing"

	"github.com/yuchanns/MikuTV/internal/provider"
)

func TestLiveChatSecurityBoundariesAndManifestValidation(t *testing.T) {
	source := openForTest(t, `
export function browse() { return { collections: [] }; }
export function connectLiveChat() { return {}; }
`, func(m *sourceManifest) {
		m.Capabilities = provider.Capabilities{provider.BrowseCatalog, provider.LiveChat}
		m.Permissions.Network = []string{"*.chat.media.example"}
	})

	// WebSocket security boundaries: unauthorized host and insecure ws:// rejected.
	bridge := newWSBridge(context.Background(), nil, nil, source.allowRequest(true), true)
	if _, _, err := bridge.validateURL("wss://evil.example.com/sub"); err == nil {
		t.Fatal("expected unauthorized wss host to be rejected")
	}
	if _, _, err := bridge.validateURL("ws://edge.chat.media.example:2244/sub"); err == nil {
		t.Fatal("expected insecure ws:// to be rejected in secure mode")
	}
	if _, _, err := bridge.validateURL("wss://edge.chat.media.example:2245/sub"); err != nil {
		t.Fatalf("expected allowed wss host to pass, got %v", err)
	}

	// Manifest capability dependency: live.chatWrite without live.chat must be rejected.
	badManifest := []byte(`{
		"id": "bad-chat",
		"name": "BadChat",
		"kind": "source",
		"version": "1.0.0",
		"apiVersion": 1,
		"minAppVersion": "1.0.0",
		"main": "index.js",
		"permissions": {"network": ["api.example.com"]},
		"capabilities": ["account.signIn", "live.chatWrite"]
	}`)
	if _, err := decodeSource(badManifest, "1.0.0"); err == nil {
		t.Fatal("expected manifest declaring live.chatWrite without live.chat to fail validation")
	}
}
