package platform

import (
	"fmt"
	"testing"

	"github.com/yuchanns/MikuTV/internal/app"
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

func TestPlaybackPositionStorePersistsTrackSelectionsIndependently(t *testing.T) {
	var store playbackPositionStore
	target := provider.PlaybackTarget{Ref: provider.MediaRef{EntryID: "jellyfin", Kind: provider.Movie, ID: "avengers"}}

	// Saving a track selection before any position must not create a fake 0s resume.
	store.putTracks("u1", target, app.PlaybackTrackSelection{Subtitle: "sub-5", SubtitleSet: true})
	if sec, ok := store.get("u1", target); ok {
		t.Fatalf("expected no saved resume position yet, got (%v, %v)", sec, ok)
	}
	if sel, ok := store.getTracks("u1", target); !ok || !sel.SubtitleSet || sel.Subtitle != "sub-5" || sel.Audio != "" {
		t.Fatalf("unexpected saved tracks: %+v ok=%v", sel, ok)
	}

	// Saving audio track preserves subtitle; saving position preserves both tracks.
	store.putTracks("u1", target, app.PlaybackTrackSelection{Audio: "audio-2"})
	store.put("u1", target, 125)
	encoded, err := store.encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	restored := parsePlaybackPositionStore(encoded)
	if sec, ok := restored.get("u1", target); !ok || sec != 125 {
		t.Fatalf("expected preserved position 125, got (%v, %v)", sec, ok)
	}
	if sel, ok := restored.getTracks("u1", target); !ok || !sel.SubtitleSet || sel.Subtitle != "sub-5" || sel.Audio != "audio-2" {
		t.Fatalf("expected preserved tracks after position save, got %+v ok=%v", sel, ok)
	}

	// Explicit Off ("") overrides previous subtitle while keeping audio intact.
	restored.putTracks("u1", target, app.PlaybackTrackSelection{Subtitle: "", SubtitleSet: true})
	if sel, ok := restored.getTracks("u1", target); !ok || !sel.SubtitleSet || sel.Subtitle != "" || sel.Audio != "audio-2" {
		t.Fatalf("expected explicit off subtitle with preserved audio, got %+v ok=%v", sel, ok)
	}
}
