//go:build ios && tvos && arm64

package main

import (
	"context"
	"log"
	"os"
	"path/filepath"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/platform"
	"github.com/yuchanns/MikuTV/internal/plugin"
	"github.com/yuchanns/MikuTV/internal/provider"
	"github.com/yuchanns/MikuTV/internal/theme"
	tvui "github.com/yuchanns/MikuTV/internal/ui"
)

var appVersion = "0.1.0"

func main() {
	executable, err := os.Executable()
	if err != nil {
		log.Fatal(err)
	}
	sources, failures := plugin.LoadBundled(context.Background(),
		os.DirFS(filepath.Join(filepath.Dir(executable), "assets", "plugins")), appVersion)
	for _, err := range failures {
		log.Print(err)
	}
	mygo.App.WhenReady(func() {
		assets, err := tvui.LoadAssets()
		if err != nil {
			log.Fatal(err)
		}
		preferences, err := platform.NewPreferences()
		if err != nil {
			log.Fatal(err)
		}
		saved, err := preferences.Load()
		if err != nil {
			log.Print(err)
		}
		pages, err := tvui.NewPages(assets)
		if err != nil {
			log.Fatal(err)
		}
		var entries []app.Entry
		installed := make(map[string]*plugin.Source)
		stores := make(map[string]*plugin.Storage)
		for _, source := range sources {
			info := source.Manifest()
			entry := app.Entry{ID: info.ID, SourceID: info.ID, Name: info.Name,
				Description: info.Description, Artwork: info.ID, Available: true, Capabilities: source.Capabilities()}
			store, err := plugin.NewStorage(preferences.LoadPluginStorage(entry.SourceID, entry.ID), func(data []byte) error {
				return preferences.SavePluginStorage(entry.SourceID, entry.ID, data)
			})
			if err != nil {
				log.Printf("plugin %s: storage is unavailable", info.ID)
				store, _ = plugin.NewStorage(nil, nil)
			}
			installed[entry.ID], stores[entry.ID] = source, store
			entries = append(entries, entry)
			assets.PrepareArtwork(entry.Artwork, source.Cover())
		}
		state, err := app.NewState(entries, saved.RecentEntry, pages.Routes)
		if err != nil {
			log.Fatal(err)
		}
		var window *mygo.Window
		catalog := app.NewCatalog(func(entryID string) provider.Session {
			if source := installed[entryID]; source != nil {
				return source.NewSession(entryID, stores[entryID])
			}
			return nil
		}, mygo.RunOnMain, func() {
			if window != nil {
				window.Invalidate()
			}
		})
		view := tvui.NewView(tvui.Options{State: state, Catalog: catalog, Pages: pages, Assets: assets,
			Preferences: preferences, Theme: theme.Default(), Version: appVersion})
		view.Trace = os.Getenv("MIKUTV_NAV_TRACE") == "1"
		if view.Trace {
			log.SetFlags(log.LstdFlags | log.Lmicroseconds)
		}
		window = mygo.NewWindow(mygo.WindowOptions{
			Title:   "MikuTV",
			Content: ui.View(view.Render),
		})
		window.OnReadyToShow(func() { log.Print("MikuTV: first frame ready") })
		entry, _ := state.Current()
		catalog.Select(entry)
		mygo.App.OnQuit(catalog.Close)
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
