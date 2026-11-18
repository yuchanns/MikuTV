package app

import (
	"context"
	"image"
	"slices"

	"github.com/yuchanns/MikuTV/internal/provider"
)

const (
	maxCatalogArtwork   = 32
	catalogArtworkCache = 48
	catalogImageWorkers = 4
	maxCatalogBackdrops = 2
)

type CatalogArtwork struct {
	Image image.Image
}

type artworkLoader func(context.Context, string) (image.Image, error)

type artworkResult struct {
	art  *CatalogArtwork
	used uint64
}

type catalogArtwork struct {
	ctx           context.Context
	cancel        context.CancelFunc
	load          artworkLoader
	size, workers int

	wanted []string
	active map[string]struct{}
	loaded map[string]artworkResult
	clock  uint64
}

func (c *Catalog) Artwork(location string) *CatalogArtwork {
	return c.artwork.get(location)
}

func (c *Catalog) BackdropArtwork(location string) *CatalogArtwork {
	return c.backdrops.get(location)
}

func (a *catalogArtwork) get(location string) *CatalogArtwork {
	if a == nil {
		return nil
	}
	return a.loaded[location].art
}

func (c *Catalog) PrepareArtwork(locations []string) {
	if c.artwork == nil {
		loader, ok := c.session.(provider.CatalogArtworkLoader)
		if !ok || len(locations) == 0 {
			return
		}
		c.artwork = newCatalogArtwork(loader.LoadCatalogArtwork, catalogArtworkCache, catalogImageWorkers)
	}
	c.prepareArtwork(c.artwork, locations[:min(len(locations), maxCatalogArtwork)])
}

func (c *Catalog) PrepareBackdrops(locations []string) {
	if c.backdrops == nil {
		loader, ok := c.session.(provider.CatalogBackdropLoader)
		if !ok || len(locations) == 0 {
			return
		}
		c.backdrops = newCatalogArtwork(loader.LoadCatalogBackdrop, maxCatalogBackdrops, 1)
	}
	c.prepareArtwork(c.backdrops, locations[:min(len(locations), maxCatalogBackdrops)])
}

func newCatalogArtwork(load artworkLoader, size, workers int) *catalogArtwork {
	ctx, cancel := context.WithCancel(context.Background())
	return &catalogArtwork{
		ctx: ctx, cancel: cancel, load: load, size: size, workers: workers,
		active: make(map[string]struct{}), loaded: make(map[string]artworkResult),
	}
}

// The viewport sets fetch priority. Recently drawn images stay available while
// the viewport moves, within a separate, bounded cache.
func (c *Catalog) prepareArtwork(work *catalogArtwork, locations []string) {
	if slices.Equal(work.wanted, locations) {
		return
	}
	work.wanted = append(work.wanted[:0], locations...)
	work.clock++
	for location, result := range work.loaded {
		if slices.Contains(work.wanted, location) {
			result.used = work.clock
			work.loaded[location] = result
		} else if result.art == nil {
			delete(work.loaded, location)
		}
	}
	c.loadArtwork(work)
}

func (c *Catalog) loadArtwork(work *catalogArtwork) {
	for _, location := range work.wanted {
		if len(work.active) >= work.workers {
			return
		}
		if _, ok := work.loaded[location]; ok || location == "" {
			continue
		}
		if _, ok := work.active[location]; ok {
			continue
		}
		work.active[location] = struct{}{}
		started := work.clock
		go func() {
			cover, err := work.load(work.ctx, location)
			c.post(func() {
				if work.ctx.Err() != nil {
					return
				}
				delete(work.active, location)
				wanted := slices.Contains(work.wanted, location)
				ready := err == nil && cover != nil
				if ready {
					result := artworkResult{art: &CatalogArtwork{Image: cover}, used: started}
					if wanted {
						result.used = work.clock
					}
					work.loaded[location] = result
				} else if wanted {
					work.loaded[location] = artworkResult{}
				}
				work.trim()
				c.loadArtwork(work)
				if ready && wanted {
					c.changed()
				}
			})
		}()
	}
}

func (a *catalogArtwork) trim() {
	if len(a.loaded) <= a.size {
		return
	}
	var oldest string
	var lastUse uint64
	for location, result := range a.loaded {
		if !slices.Contains(a.wanted, location) && (oldest == "" || result.used < lastUse) {
			oldest, lastUse = location, result.used
		}
	}
	delete(a.loaded, oldest)
}

func (c *Catalog) clearArtwork() {
	for _, work := range []*catalogArtwork{c.artwork, c.backdrops} {
		if work != nil {
			work.cancel()
		}
	}
	c.artwork, c.backdrops = nil, nil
}
