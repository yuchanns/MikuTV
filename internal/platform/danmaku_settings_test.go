package platform

import (
	"testing"

	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/provider"
)

func TestDanmakuDisplayAndRulesStoreIsolation(t *testing.T) {
	encoded, err := encodeDanmakuDisplay(app.DanmakuDisplay{
		Show: false, Density: 30, Area: app.DanmakuAreaTop, Opacity: 50, FontSize: app.DanmakuFontLarge,
	})
	if err != nil {
		t.Fatal(err)
	}
	got, ok := parseDanmakuDisplay(encoded)
	if !ok || got.Show || got.Density != 30 || got.Area != app.DanmakuAreaTop || got.Opacity != 50 || got.FontSize != app.DanmakuFontLarge {
		t.Fatalf("unexpected decoded display: %+v ok=%t", got, ok)
	}

	var store danmakuRulesStore
	store.put("sample-source", "", []provider.DanmakuRule{
		{ID: "local:1", Kind: provider.DanmakuRuleKeyword, Pattern: "spoiler", Enabled: true},
	})
	store.put("sample-source", "u1", []provider.DanmakuRule{
		{ID: "remote:10", Kind: provider.DanmakuRuleUser, Pattern: "1024386", Enabled: true, Synced: true, RemoteID: "10"},
	})
	store.put("other", "u1", []provider.DanmakuRule{
		{ID: "local:2", Kind: provider.DanmakuRuleKeyword, Pattern: "ads", Enabled: false},
	})

	raw, err := store.encode()
	if err != nil {
		t.Fatal(err)
	}
	reloaded := parseDanmakuRulesStore(raw)
	if len(reloaded.get("sample-source", "")) != 1 || reloaded.get("sample-source", "")[0].Pattern != "spoiler" {
		t.Fatalf("expected guest rules isolated, got %+v", reloaded.get("sample-source", ""))
	}
	if len(reloaded.get("sample-source", "u1")) != 1 || reloaded.get("sample-source", "u1")[0].RemoteID != "10" {
		t.Fatalf("expected account u1 rules isolated, got %+v", reloaded.get("sample-source", "u1"))
	}
	if len(reloaded.get("other", "u1")) != 1 || reloaded.get("other", "u1")[0].Enabled {
		t.Fatalf("expected other entry rules isolated, got %+v", reloaded.get("other", "u1"))
	}

	if !reloaded.clearAccount("sample-source", "u1") {
		t.Fatal("expected clearAccount to remove u1 bucket")
	}
	if len(reloaded.get("sample-source", "u1")) != 0 || len(reloaded.get("sample-source", "")) != 1 || len(reloaded.get("other", "u1")) != 1 {
		t.Fatalf("clearAccount should only clear target entry+owner")
	}
	if !reloaded.clearEntry("sample-source") || len(reloaded.get("sample-source", "")) != 0 || len(reloaded.get("other", "u1")) != 1 {
		t.Fatalf("clearEntry should only clear target entry")
	}
}
