package platform

import (
	"fmt"
	"testing"

	"github.com/yuchanns/MikuTV/internal/provider"
)

func TestPlaybackPositionStoreBoundsLRUAndClearsScopes(t *testing.T) {
	var store playbackPositionStore
	for i := range maxPlaybackPositions + 25 {
		target := provider.PlaybackTarget{
			Ref:    provider.MediaRef{EntryID: "sample-source", Kind: provider.Video, ID: fmt.Sprintf("BV%d", i)},
			PartID: "p1",
		}
		store.put("user-a", target, float64(i+10))
	}
	if len(store.Items) != maxPlaybackPositions {
		t.Fatalf("expected %d items, got %d", maxPlaybackPositions, len(store.Items))
	}

	// Oldest 25 entries must be evicted; newest must survive.
	oldest := provider.PlaybackTarget{Ref: provider.MediaRef{EntryID: "sample-source", Kind: provider.Video, ID: "BV0"}, PartID: "p1"}
	if _, ok := store.get("user-a", oldest); ok {
		t.Fatal("oldest item was not evicted")
	}
	newest := provider.PlaybackTarget{Ref: provider.MediaRef{EntryID: "sample-source", Kind: provider.Video, ID: fmt.Sprintf("BV%d", maxPlaybackPositions+24)}, PartID: "p1"}
	if sec, ok := store.get("user-a", newest); !ok || sec != float64(maxPlaybackPositions+34) {
		t.Fatalf("newest item = (%v, %v)", sec, ok)
	}

	// Round-trip through JSON and verify ClearAccount and ClearEntry.
	other := provider.PlaybackTarget{Ref: provider.MediaRef{EntryID: "sample-source", Kind: provider.Video, ID: "BV-other"}, PartID: "p1"}
	store.put("user-b", other, 42)
	secondEntry := provider.PlaybackTarget{Ref: provider.MediaRef{EntryID: "other-entry", Kind: provider.Episode, ID: "ep1"}, PartID: "1"}
	store.put("user-a", secondEntry, 99)

	encoded, err := store.encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	restored := parsePlaybackPositionStore(encoded)
	if !restored.clearAccount("sample-source", "user-a") {
		t.Fatal("clearAccount reported no changes")
	}
	if _, ok := restored.get("user-a", newest); ok {
		t.Fatal("user-a entry survived clearAccount")
	}
	if sec, ok := restored.get("user-b", other); !ok || sec != 42 {
		t.Fatalf("user-b entry damaged by clearAccount: (%v, %v)", sec, ok)
	}
	if sec, ok := restored.get("user-a", secondEntry); !ok || sec != 99 {
		t.Fatalf("other-entry damaged by clearAccount: (%v, %v)", sec, ok)
	}
	if !restored.clearEntry("sample-source") {
		t.Fatal("clearEntry reported no changes")
	}
	if _, ok := restored.get("user-b", other); ok {
		t.Fatal("sample-source entry survived clearEntry")
	}
	if sec, ok := restored.get("user-a", secondEntry); !ok || sec != 99 {
		t.Fatalf("other-entry damaged by clearEntry: (%v, %v)", sec, ok)
	}
}
