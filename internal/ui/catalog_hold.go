package tvui

import (
	"log"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/provider"
)

const (
	catalogHoldSpeed = 1900.0 // DIPs per second in either direction.
)

type catalogHold struct {
	key   ui.Key
	frame time.Time
}

func (v *View) startCatalogHold(key ui.Key, group string) bool {
	scroll := v.activeScroll
	if (key != ui.KeyUp && key != ui.KeyDown) || v.State.Page != app.Home ||
		v.focus.groups[group].navigation || scroll == nil || scroll.MaxY == 0 {
		return false
	}
	entry, _ := v.State.Current()
	if !entry.Capabilities.Has(provider.BrowseCatalog) {
		return false
	}
	if v.home.hold.key != key {
		v.home.hold = catalogHold{key: key}
		if scroll.Scrolling() {
			scroll.ScrollTo(scroll.X, scroll.Y, ui.Animation{Duration: time.Nanosecond})
		}
		v.home.rotateAt = time.Time{}
	}
	return true
}

func (v *View) advanceCatalogHold(c *ui.Context, height float32, active bool) {
	hold, scroll := &v.home.hold, v.activeScroll
	if hold.key == ui.KeyUnknown {
		return
	}
	if !active || scroll == nil || !c.KeyDown(hold.key) {
		*hold = catalogHold{}
		v.reveal = active && scroll != nil
		return
	}
	now := c.Now()
	if hold.frame.IsZero() {
		hold.frame = now
		c.AnimationFrame()
		return
	}
	distance := float32(now.Sub(hold.frame).Seconds() * catalogHoldSpeed)
	hold.frame = now
	direction := app.Down
	if hold.key == ui.KeyUp {
		direction, distance = app.Up, -distance
	}
	next := max(0, min(scroll.Y+distance, scroll.MaxY))
	boundary := next == 0 || next == scroll.MaxY
	movedFocus := v.followCatalogHold(direction, next-scroll.Y, height, boundary)
	if v.Trace && next != scroll.Y {
		log.Printf("catalog hold: y=%.2f delta=%.2f frame=%s", next, next-scroll.Y, now.Format("15:04:05.000000"))
	}
	scroll.Y = next
	if !boundary || movedFocus {
		c.AnimationFrame()
	} else {
		hold.frame = time.Time{}
	}
}

func (v *View) followCatalogHold(direction app.Direction, delta, height float32, boundary bool) bool {
	current := v.focus.item(v.State.Focus)
	if current == nil {
		return false
	}
	center := current.target.Y + current.target.H/2 - delta
	const margin = 0.32
	if !boundary && ((direction == app.Down && center > height*margin) ||
		(direction == app.Up && center < height*(1-margin))) {
		return false
	}
	next := v.focus.neighbor(current, direction)
	if next == v.State.Focus {
		return false
	}
	item := v.focus.item(next)
	if item == nil {
		return false
	}
	y := item.target.Y + item.target.H/2 - delta
	if y <= 0 || y >= height {
		return false
	}
	v.moveFocus(next, direction, false)
	return true
}
