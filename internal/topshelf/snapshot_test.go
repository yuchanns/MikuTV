package topshelf

import (
	"fmt"
	"strings"
	"testing"
)

func TestBuildAndParseURL(t *testing.T) {
	link := Link{
		Action:  ActionPlay,
		EntryID: "sample-source",
		Kind:    "video",
		ID:      "BV1xx411c7mD",
	}
	raw := BuildURL(link)
	if !strings.HasPrefix(raw, "mikutv://play?") {
		t.Fatalf("unexpected url: %q", raw)
	}
	got, ok := ParseURL(raw)
	if !ok {
		t.Fatalf("ParseURL(%q) returned false", raw)
	}
	if got != link {
		t.Fatalf("ParseURL = %+v, want %+v", got, link)
	}

	detail := Link{Action: ActionDetail, EntryID: "alt-source", Kind: "course", ID: "cs331"}
	gotDetail, ok := ParseURL(BuildURL(detail))
	if !ok || gotDetail != detail {
		t.Fatalf("detail round-trip failed: ok=%v got=%+v", ok, gotDetail)
	}

	for _, bad := range []string{
		"",
		"https://example.com",
		"mikutv://unknown?entry=sample-source&kind=video&id=1",
		"mikutv://play?entry=&kind=video&id=1",
		"mikutv://play?entry=sample-source&kind=invalid&id=1",
		"mikutv://play?entry=sample-source&kind=video&id=",
	} {
		if _, ok := ParseURL(bad); ok {
			t.Errorf("ParseURL(%q) unexpectedly succeeded", bad)
		}
	}
}

func TestEncodeDecodeSanitizesSectionsDeduplicatesAndCapsLimits(t *testing.T) {
	continueItems := []Item{
		{EntryID: "sample-source", Kind: "video", ID: "c1", Title: "Continue 1", ImageURL: "https://img.example.com/c1.jpg", DurationSeconds: 600, ProgressSeconds: 180, ContinueWatching: true},
		{EntryID: "sample-source", Kind: "video", ID: "c1", Title: "Duplicate", ImageURL: "https://img.example.com/dup.jpg", ContinueWatching: true},
		{EntryID: "sample-source", Kind: "video", ID: "bad-http", Title: "Insecure", ImageURL: "http://img.example.com/bad.jpg", ContinueWatching: true},
		{EntryID: "sample-source", Kind: "video", ID: "c2", Title: "Continue 2", ImageURL: "https://img.example.com/c2.jpg", DurationSeconds: 1000, ProgressSeconds: 5, ContinueWatching: true},
	}
	var recItems []Item
	recItems = append(recItems, Item{EntryID: "sample-source", Kind: "video", ID: "c1", Title: "Cross-Section Dup", ImageURL: "https://img.example.com/c1.jpg"})
	for i := 1; i <= 12; i++ {
		recItems = append(recItems, Item{
			EntryID:  "sample-source",
			Kind:     "video",
			ID:       fmt.Sprintf("r%d", i),
			Title:    fmt.Sprintf("Rec %d", i),
			ImageURL: fmt.Sprintf("https://img.example.com/r%d.jpg", i),
		})
	}
	encoded, err := Encode(Snapshot{
		EntryID:   "sample-source",
		EntryName: "SourceA",
		Sections: []Section{
			{ID: "continue", Title: "Continue Watching", Items: continueItems},
			{ID: "empty", Title: "Empty Section"},
			{ID: "recommended", Title: "Recommended", Items: recItems},
		},
	})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := Decode(encoded)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(got.Sections) != 2 {
		t.Fatalf("len(Sections) = %d, want 2: %+v", len(got.Sections), got.Sections)
	}
	if got.Sections[0].ID != "continue" || len(got.Sections[0].Items) != 2 {
		t.Fatalf("unexpected continue section: %+v", got.Sections[0])
	}
	if got.Sections[1].ID != "recommended" || len(got.Sections[1].Items) != MaxItemsPerSection {
		t.Fatalf("unexpected recommended section: len=%d want=%d", len(got.Sections[1].Items), MaxItemsPerSection)
	}
	if got.ItemCount() != 2+MaxItemsPerSection {
		t.Fatalf("ItemCount = %d, want %d", got.ItemCount(), 2+MaxItemsPerSection)
	}
	if _, ok := got.Find("sample-source", "video", "bad-http"); ok {
		t.Fatal("non-HTTPS image item was not filtered out")
	}
	if got.Sections[0].Items[0].PlaybackProgress() != 0.3 {
		t.Fatalf("PlaybackProgress(c1) = %v, want 0.3", got.Sections[0].Items[0].PlaybackProgress())
	}
	if got.Sections[0].Items[1].PlaybackProgress() != 0.02 {
		t.Fatalf("PlaybackProgress(c2) = %v, want min clamp 0.02", got.Sections[0].Items[1].PlaybackProgress())
	}
	if got.Sections[1].Items[0].PlaybackProgress() != 0 {
		t.Fatalf("non-continue item should have 0 progress, got %v", got.Sections[1].Items[0].PlaybackProgress())
	}
}

func TestSanitizeSnapshotLegacyFlatItemsMigratedToSection(t *testing.T) {
	got := SanitizeSnapshot(Snapshot{
		EntryID:   "sample-source",
		EntryName: "SourceA",
		Items: []Item{
			{EntryID: "sample-source", Kind: "video", ID: "legacy-1", Title: "Legacy Item", ImageURL: "https://img.example.com/l1.jpg"},
		},
	})
	if len(got.Sections) != 1 || len(got.Sections[0].Items) != 1 || got.Sections[0].Items[0].ID != "legacy-1" {
		t.Fatalf("legacy flat items not migrated cleanly: %+v", got)
	}
}

func TestSanitizeImageURLAllowsLocalNetworkHTTPAndRejectsPublicOrLoopbackHTTP(t *testing.T) {
	allowed := []string{
		"https://img.example.com/poster.jpg",
		"http://192.168.1.50:8096/Items/abc/Images/Backdrop/0?maxWidth=1920",
		"http://10.9.60.10:8096/Items/abc/Images/Primary",
		"http://172.16.0.5:8096/Items/abc/Images/Primary",
		"http://100.64.0.10:8096/Items/abc/Images/Primary",
		"http://169.254.1.2:8096/Items/abc/Images/Primary",
		"http://[fd00::1]:8096/Items/abc/Images/Primary",
		"http://nas.local:8096/Items/abc/Images/Primary",
		"http://media.lan:8096/Items/abc/Images/Primary",
		"http://server.home.arpa:8096/Items/abc/Images/Primary",
		"http://media-box:8096/Items/abc/Images/Primary",
	}
	for _, raw := range allowed {
		snap := SanitizeSnapshot(Snapshot{
			EntryID: "media-server",
			Sections: []Section{{
				ID:    "rec",
				Title: "Recommended",
				Items: []Item{{EntryID: "media-server", Kind: "movie", ID: "m1", Title: "Movie", ImageURL: raw}},
			}},
		})
		if snap.ItemCount() != 1 {
			t.Errorf("expected image URL %q to be allowed, got %+v", raw, snap)
		}
	}

	rejected := []string{
		"",
		"https://:443/poster.jpg",
		"http://img.example.com/bad.jpg",
		"http://1.1.1.1/bad.jpg",
		"http://127.0.0.1:8096/Items/abc/Images/Primary",
		"http://localhost:8096/Items/abc/Images/Primary",
		"http://[::1]:8096/Items/abc/Images/Primary",
		"http://[::ffff:127.0.0.1]:8096/Items/abc/Images/Primary",
		"http://0.0.0.0:8096/Items/abc/Images/Primary",
		"http://2130706433:8096/Items/abc/Images/Primary",
		"http://0x7f000001:8096/Items/abc/Images/Primary",
		"http://user:pass@192.168.1.50:8096/Items/abc/Images/Primary",
		"https://user:pass@img.example.com/poster.jpg",
		"file:///var/mobile/poster.jpg",
		"javascript:alert(1)",
	}
	for _, raw := range rejected {
		snap := SanitizeSnapshot(Snapshot{
			EntryID: "media-server",
			Sections: []Section{{
				ID:    "rec",
				Title: "Recommended",
				Items: []Item{{EntryID: "media-server", Kind: "movie", ID: "m1", Title: "Movie", ImageURL: raw}},
			}},
		})
		if snap.ItemCount() != 0 {
			t.Errorf("expected image URL %q to be rejected, got %+v", raw, snap)
		}
	}
}
