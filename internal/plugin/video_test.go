package plugin

import (
	"testing"

	"github.com/yuchanns/MikuTV/internal/provider"
)

func TestOwnVideoActionsValidation(t *testing.T) {
	valid := []provider.ActionSpec{{
		ID: "boost", Slot: provider.ActionSlotHero, Label: "Boost", Icon: "like",
		Confirm: &provider.ActionConfirm{Title: "Boost?", Icon: "detail-primary/like"},
		Celebrate: true, CelebrateIcons: []string{"like", "detail/star"},
	}}
	if err := ownVideoActions(valid); err != nil {
		t.Fatalf("valid action rejected: %v", err)
	}
	invalidCases := [][]provider.ActionSpec{
		{{ID: "BAD_ID", Slot: provider.ActionSlotHero, Label: "Boost"}},
		{{ID: "dup", Slot: provider.ActionSlotHero, Label: "A"}, {ID: "dup", Slot: provider.ActionSlotOverview, Label: "B"}},
		{{ID: "ok", Slot: "unknown", Label: "A"}},
		{{ID: "ok", Slot: provider.ActionSlotHero, Label: "   "}},
		{{ID: "ok", Slot: provider.ActionSlotHero, Label: "A", Icon: "../evil"}},
		{{ID: "ok", Slot: provider.ActionSlotHero, Label: "A", Confirm: &provider.ActionConfirm{Title: " "}}},
		{{ID: "ok", Slot: provider.ActionSlotHero, Label: "A", CelebrateIcons: []string{"a", "b", "c", "d", "e", "f"}}},
	}
	for i, actions := range invalidCases {
		if err := ownVideoActions(actions); provider.CodeOf(err) != provider.PluginFailed {
			t.Fatalf("case %d: got %v, want PluginFailed", i, err)
		}
	}
}
