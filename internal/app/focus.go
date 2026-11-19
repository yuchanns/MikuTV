package app

import "math"

type Direction uint8

const (
	Left Direction = iota
	Right
	Up
	Down
)

type FocusTarget struct {
	ID         FocusID
	X, Y, W, H float32
}

// Neighbor uses committed geometry, including offscreen scroll content.
// Horizontal moves stay within the vertical span of the current control.
func Neighbor(targets []FocusTarget, current FocusID, direction Direction) FocusID {
	var origin FocusTarget
	found := false
	for _, target := range targets {
		if target.ID == current {
			origin, found = target, true
			break
		}
	}
	if !found {
		if len(targets) > 0 {
			return targets[0].ID
		}
		return current
	}
	if origin.W <= 0 || origin.H <= 0 {
		return current
	}
	horizontal := direction == Left || direction == Right
	best, score := current, math.Inf(1)
	for _, target := range targets {
		if target.ID == current || target.W <= 0 || target.H <= 0 {
			continue
		}
		if horizontal && min(origin.Y+origin.H, target.Y+target.H) <= max(origin.Y, target.Y) {
			continue
		}
		dx := float64(target.X + target.W/2 - origin.X - origin.W/2)
		dy := float64(target.Y + target.H/2 - origin.Y - origin.H/2)
		primary, cross := dx, math.Abs(dy)
		switch direction {
		case Left:
			primary = -dx
		case Up:
			primary, cross = -dy, math.Abs(dx)
		case Down:
			primary, cross = dy, math.Abs(dx)
		}
		if primary <= 1 {
			continue
		}
		distance := primary + cross*3.2
		if cross > primary*1.4 {
			distance += 350
		}
		if distance < score {
			best, score = target.ID, distance
		}
	}
	return best
}
