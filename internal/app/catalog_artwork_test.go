package app

import (
	"context"
	"image"
	"strconv"
	"testing"
)

func TestArtworkStaysAvailableAcrossPriorityChanges(t *testing.T) {
	callbacks := make(chan func(), catalogImageWorkers)
	catalog := NewCatalog(nil, func(fn func()) { callbacks <- fn }, func() {})
	defer catalog.Close()
	catalog.artwork = newCatalogArtwork(func(context.Context, string) (image.Image, error) {
		return image.NewRGBA(image.Rect(0, 0, 16, 9)), nil
	}, catalogArtworkCache, catalogImageWorkers)

	load := func(locations []string, count int) {
		t.Helper()
		catalog.PrepareArtwork(locations)
		for range count {
			select {
			case callback := <-callbacks:
				callback()
			case <-t.Context().Done():
				t.Fatal("artwork load did not complete")
			}
		}
	}
	load([]string{"prior-row"}, 1)
	previous := catalog.Artwork("prior-row")
	load([]string{"next-row"}, 1)
	if previous == nil || catalog.Artwork("prior-row") != previous {
		t.Fatal("switching rows replaced a cover that had already loaded")
	}

	for index := range catalogArtworkCache {
		load([]string{"next-row", strconv.Itoa(index)}, 1)
	}
	if catalog.Artwork("prior-row") != nil || catalog.Artwork("next-row") == nil ||
		len(catalog.artwork.loaded) != catalogArtworkCache {
		t.Fatal("cache did not evict the oldest offscreen cover while protecting the wanted row")
	}
}
