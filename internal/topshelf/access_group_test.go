package topshelf

import (
	"slices"
	"testing"
)

func TestSelectSharedAccessGroup(t *testing.T) {
	t.Run("prefers dedicated group when entitled", func(t *testing.T) {
		var probed []string
		got := selectSharedAccessGroup("TEAM123456.*", func(group string) bool {
			probed = append(probed, group)
			return group == "TEAM123456.dev.yuchanns.MikuTV.topshelf"
		})
		if got != "TEAM123456.dev.yuchanns.MikuTV.topshelf" {
			t.Fatalf("got %q, want dedicated group", got)
		}
		wantProbed := []string{"TEAM123456.dev.yuchanns.MikuTV.topshelf"}
		if !slices.Equal(probed, wantProbed) {
			t.Fatalf("probed %v, want %v", probed, wantProbed)
		}
	})

	t.Run("falls back to team wildcard group when dedicated group lacks entitlement", func(t *testing.T) {
		var probed []string
		got := selectSharedAccessGroup("TEAM123456.dev.yuchanns.MikuTV.TEAM123456", func(group string) bool {
			probed = append(probed, group)
			return group == "TEAM123456.*"
		})
		if got != "TEAM123456.*" {
			t.Fatalf("got %q, want TEAM123456.*", got)
		}
		wantProbed := []string{"TEAM123456.dev.yuchanns.MikuTV.topshelf", "TEAM123456.*"}
		if !slices.Equal(probed, wantProbed) {
			t.Fatalf("probed %v, want %v", probed, wantProbed)
		}
	})

	t.Run("falls back to default when no candidate is entitled", func(t *testing.T) {
		got := selectSharedAccessGroup("TEAM123456.*", func(string) bool { return false })
		if got != "" {
			t.Fatalf("got %q, want empty fallback", got)
		}
	})

	t.Run("rejects malformed default group", func(t *testing.T) {
		for _, raw := range []string{"", "no-dot", ".missing-prefix", "   .foo"} {
			if got := selectSharedAccessGroup(raw, func(string) bool { return true }); got != "" {
				t.Errorf("selectSharedAccessGroup(%q) = %q, want empty", raw, got)
			}
		}
	})
}
