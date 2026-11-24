package platform

import (
	"bytes"
	"encoding/json"
	"testing"
)

type memoryKeychain struct {
	items map[string]map[string][]byte
}

func newMemoryKeychain() *memoryKeychain {
	return &memoryKeychain{items: make(map[string]map[string][]byte)}
}

func (m *memoryKeychain) get(service, account string) ([]byte, error) {
	if m.items[service] == nil {
		return nil, nil
	}
	return bytes.Clone(m.items[service][account]), nil
}

func (m *memoryKeychain) set(service, account string, data []byte) error {
	if m.items[service] == nil {
		m.items[service] = make(map[string][]byte)
	}
	m.items[service][account] = bytes.Clone(data)
	return nil
}

func (m *memoryKeychain) del(service, account string) error {
	if m.items[service] != nil {
		delete(m.items[service], account)
	}
	return nil
}

func TestIsolatedCredentialStoreSplitsPerAccountAndCleansRemoved(t *testing.T) {
	backend := newMemoryKeychain()
	store := &isolatedCredentialStore{backend: backend}

	payload := []byte(`{"selected":"u1","accounts":[{"account":{"id":"u1","name":"Alice"},"secret":"s1"},{"account":{"id":"u2","name":"Bob"},"secret":"s2"}]}`)
	if err := store.Save("sample-source", "entry-1", payload); err != nil {
		t.Fatalf("Save: %v", err)
	}

	service := backend.items["mikutv.source.sample-source"]
	if len(service) != 3 {
		t.Fatalf("expected 3 keychain items (index + 2 accounts), got %d: %v", len(service), service)
	}
	if _, ok := service["v2:entry-1:account:u1"]; !ok {
		t.Fatal("missing isolated item for u1")
	}
	if _, ok := service["v2:entry-1:account:u2"]; !ok {
		t.Fatal("missing isolated item for u2")
	}

	// Corrupt one account; the other account must still load intact.
	service["v2:entry-1:account:u1"] = []byte("{corrupt")
	loaded, err := store.Load("sample-source", "entry-1")
	if err != nil {
		t.Fatalf("Load with one corrupt account: %v", err)
	}
	var decoded struct {
		Selected string `json:"selected"`
		Accounts []struct {
			Account struct {
				ID string `json:"id"`
			} `json:"account"`
		} `json:"accounts"`
	}
	if err := json.Unmarshal(loaded, &decoded); err != nil {
		t.Fatalf("unmarshal loaded: %v", err)
	}
	if len(decoded.Accounts) != 1 || decoded.Accounts[0].Account.ID != "u2" {
		t.Fatalf("expected surviving account u2, got %+v", decoded.Accounts)
	}
	if decoded.Selected != "" {
		t.Fatalf("expected selected cleared when u1 failed, got %q", decoded.Selected)
	}

	// Save with only u2 should remove u1 item; empty save should remove all items.
	if err := store.Save("sample-source", "entry-1", []byte(`{"selected":"u2","accounts":[{"account":{"id":"u2","name":"Bob"}}]}`)); err != nil {
		t.Fatalf("Save u2 only: %v", err)
	}
	if _, ok := service["v2:entry-1:account:u1"]; ok {
		t.Fatal("removed account u1 still present in keychain")
	}
	if err := store.Save("sample-source", "entry-1", nil); err != nil {
		t.Fatalf("Save empty: %v", err)
	}
	if len(service) != 0 {
		t.Fatalf("expected empty keychain after clear, got %d items", len(service))
	}
}

func TestIsolatedCredentialStoreMigratesLegacyBlobSeamlessly(t *testing.T) {
	backend := newMemoryKeychain()
	legacy := []byte(`{"selected":"12345","accounts":[{"account":{"id":"12345","name":"Miku"},"data":{"SESSDATA":"abc"}}]}`)
	_ = backend.set("mikutv.source.sample-source", "sample-source", legacy)

	store := &isolatedCredentialStore{backend: backend}
	got, err := store.Load("sample-source", "sample-source")
	if err != nil {
		t.Fatalf("Load legacy: %v", err)
	}
	if !bytes.Equal(got, legacy) {
		t.Fatalf("Load legacy returned %s, want %s", got, legacy)
	}

	service := backend.items["mikutv.source.sample-source"]
	if _, ok := service["sample-source"]; ok {
		t.Fatal("legacy blob should have been removed after migration")
	}
	if _, ok := service["v2:sample-source:index"]; !ok {
		t.Fatal("missing migrated v2 index")
	}
	if _, ok := service["v2:sample-source:account:12345"]; !ok {
		t.Fatal("missing migrated per-account item")
	}

	// Subsequent load reads from isolated per-account items.
	got2, err := store.Load("sample-source", "sample-source")
	if err != nil {
		t.Fatalf("Load post-migration: %v", err)
	}
	var parsed rawAccountRecord
	if err := json.Unmarshal(got2, &parsed); err != nil || parsed.Selected != "12345" || len(parsed.Accounts) != 1 {
		t.Fatalf("unexpected post-migration record: %s (err=%v)", got2, err)
	}
}
