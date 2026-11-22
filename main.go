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
	"github.com/yuchanns/MikuTV/internal/player"
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
		var credentials app.CredentialStore
		if keychain, err := platform.NewCredentialKeychain(); err == nil {
			credentials = keychain
		} else {
			log.Printf("credential store unavailable: %v", err)
		}
		saved := preferences.Load()
		pages, err := tvui.NewPages(assets)
		if err != nil {
			log.Fatal(err)
		}
		var entries []app.Entry
		sourcesByID := make(map[string]*plugin.Source)
		entrySource := make(map[string]string)
		stores := make(map[string]*plugin.Storage)
		for _, source := range sources {
			info := source.Manifest()
			entry := app.Entry{ID: info.ID, SourceID: info.ID, Name: info.Name,
				Description: info.Description, Artwork: info.ID,
				Capabilities: source.Capabilities(), Auth: source.AuthPresentation(),
				Presentation: source.Presentation()}
			store, err := plugin.NewStorage(preferences.LoadPluginStorage(entry.SourceID, entry.ID), func(data []byte) error {
				return preferences.SavePluginStorage(entry.SourceID, entry.ID, data)
			})
			if err != nil {
				log.Fatalf("open plugin %s storage: %v", info.ID, err)
			}
			sourcesByID[entry.SourceID] = source
			entrySource[entry.ID] = entry.SourceID
			stores[entry.ID] = store
			entries = append(entries, entry)
			assets.PrepareArtwork(entry.Artwork, source.Cover())
			assets.PrepareAccountArtwork(entry.ID, source.AuthArtwork())
		}
		state, err := app.NewState(entries, saved.RecentEntry, pages.Routes)
		if err != nil {
			log.Fatal(err)
		}
		var window *mygo.Window
		invalidate := func() {
			if window != nil {
				window.Invalidate()
			}
		}
		catalog := app.NewCatalog(func(entryID string) provider.Session {
			src := sourcesByID[entrySource[entryID]]
			if src == nil {
				return nil
			}
			return src.NewSession(entryID, stores[entryID])
		}, mygo.RunOnMain, invalidate)
		activity := app.NewAccountActivity(preferences, mygo.RunOnMain, invalidate)
		search := app.NewCatalogSearch(preferences, mygo.RunOnMain, invalidate)
		following := app.NewFollowingFeed(mygo.RunOnMain, invalidate)
		series := app.NewSeriesCatalog(mygo.RunOnMain, invalidate)
		var view *tvui.View
		auth := app.NewAuth(credentials, mygo.RunOnMain, invalidate, func() {
			catalog.Reload()
			activity.Close()
			if view != nil {
				view.SyncAccountActivity()
			}
		}, func() { view.PromptSignIn() })
		auth.SetCleaner(preferences)
		view = tvui.NewView(tvui.Options{State: state, Catalog: catalog, Search: search, Following: following, Series: series, Auth: auth, Activity: activity, Pages: pages, Assets: assets,
			Preferences: preferences, Theme: theme.Default(), Version: appVersion})
		view.Trace = os.Getenv("MIKUTV_NAV_TRACE") == "1"
		if view.Trace {
			log.SetFlags(log.LstdFlags | log.Lmicroseconds)
		}
		window = mygo.NewWindow(mygo.WindowOptions{
			Title:       "MikuTV",
			Content:     ui.View(view.Render),
			Transparent: true,
		})
		var disposePlayer func()
		window.OnReadyToShow(func() {
			engine, dispose := player.NewNative(window.NativeHandle())
			disposePlayer = dispose
			view.Playback = app.NewPlayback(catalog, engine, preferences, mygo.RunOnMain, func() {
				view.PlaybackChanged()
				invalidate()
			})
		})
		mygo.Power.OnSuspend(func() {
			if view.Playback != nil {
				view.Playback.Suspend()
			}
		})
		window.OnReadyToShow(func() { log.Print("MikuTV: first frame ready") })
		view.SyncEntry()
		mygo.App.OnQuit(func() {
			if view.Playback != nil {
				view.Playback.Close()
				disposePlayer()
			}
			series.Close()
			following.Close()
			search.Close()
			activity.Close()
			auth.Close()
			catalog.Close()
		})
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
