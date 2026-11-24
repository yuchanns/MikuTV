package plugin

import (
	"errors"
	"testing"

	"github.com/yuchanns/MikuTV/internal/provider"
)

func TestSearchManifestRequiresSearchForSuggestAndValidatesOutput(t *testing.T) {
	badManifest := zipFiles(t, map[string][]byte{
		"plugin.json": manifestForTest(t, func(m *sourceManifest) {
			m.Capabilities = provider.Capabilities{provider.BrowseCatalog, provider.SearchSuggest}
		}),
		"index.js": []byte("export function browse() {} export function searchSuggest() { return {}; }"),
	})
	if _, err := Open(t.Context(), badManifest, testVersion); !errors.Is(err, ErrIncompatible) {
		t.Fatalf("searchSuggest without search was accepted: %v", err)
	}

	badSource := openForTest(t, `
export function browse() { return { collections: [] }; }
export function search() { return { total: -1 }; }
`, func(m *sourceManifest) {
		m.Capabilities = provider.Capabilities{provider.BrowseCatalog, provider.SearchCatalog}
	})
	session := badSource.NewSession("bad", nil)
	defer session.Close()
	if _, err := session.Search(t.Context(), provider.SearchRequest{Query: "test"}); provider.CodeOf(err) != provider.PluginFailed {
		t.Fatalf("negative total was accepted: %v", err)
	}
	if _, err := session.Search(t.Context(), provider.SearchRequest{Kind: "invalid"}); provider.CodeOf(err) != provider.Denied {
		t.Fatalf("invalid search kind was accepted: %v", err)
	}
}
