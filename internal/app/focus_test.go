package app

import "testing"

func TestSpatialFocusKeepsColumnsAndReachesOffscreenRows(t *testing.T) {
	targets := []FocusTarget{
		{ID: "a", X: 0, Y: 0, W: 300, H: 200},
		{ID: "b", X: 330, Y: 0, W: 300, H: 200},
		{ID: "c", X: 660, Y: 0, W: 300, H: 200},
		{ID: "d", X: 0, Y: 240, W: 300, H: 200},
		{ID: "e", X: 330, Y: 240, W: 300, H: 200},
		{ID: "offscreen", X: 330, Y: 1200, W: 300, H: 200},
	}
	for _, test := range []struct {
		from, want FocusID
		direction  Direction
	}{
		{"a", "b", Right}, {"b", "a", Left}, {"b", "e", Down},
		{"e", "b", Up}, {"e", "offscreen", Down}, {"a", "a", Left},
	} {
		if got := Neighbor(targets, test.from, test.direction); got != test.want {
			t.Fatalf("from %q toward %v: got %q, want %q", test.from, test.direction, got, test.want)
		}
	}
}
