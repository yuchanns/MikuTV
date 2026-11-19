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

func TestHorizontalFocusRejectsControlsAboveAndBelow(t *testing.T) {
	targets := []FocusTarget{
		{ID: "play", X: 100, Y: 500, W: 180, H: 62},
		{ID: "later", X: 720, Y: 506, W: 180, H: 62},
		{ID: "creator", X: 150, Y: 430, W: 110, H: 40},
		{ID: "follow", X: 230, Y: 430, W: 100, H: 40},
		{ID: "overview", X: 190, Y: 585, W: 100, H: 40},
	}
	for _, move := range []struct {
		from, want FocusID
		direction  Direction
	}{
		{"play", "later", Right},
		{"later", "play", Left},
		{"play", "creator", Up},
		{"play", "play", Left},
		{"later", "later", Right},
	} {
		if got := Neighbor(targets, move.from, move.direction); got != move.want {
			t.Fatalf("from %q toward %v: got %q, want %q", move.from, move.direction, got, move.want)
		}
	}
	targets[1].W = 0
	if got := Neighbor(targets, "play", Right); got != "play" {
		t.Fatalf("Right with an unlaid neighbor jumped to %q", got)
	}
}
