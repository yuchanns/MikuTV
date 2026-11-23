//go:build ios && tvos && arm64

package main

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/platform"
	"github.com/yuchanns/MikuTV/internal/player"
	"github.com/yuchanns/MikuTV/internal/plugin"
	"github.com/yuchanns/MikuTV/internal/provider"
	"github.com/yuchanns/MikuTV/internal/theme"
	"github.com/yuchanns/MikuTV/internal/topshelf"
	tvui "github.com/yuchanns/MikuTV/internal/ui"
)

var (
	appVersion = "0.1.0"
	appBuild   = ""
	appCommit  = ""
)

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
	aboutDeps, err := app.LoadBundledDependencies(filepath.Join(filepath.Dir(executable), "assets", "licenses.json"))
	if err != nil {
		log.Printf("bundled licenses unavailable: %v", err)
		aboutDeps = app.RuntimeDependencies()
	}
	var topShelfStore topshelf.Store
	if shared, err := topshelf.NewSharedStore(); err == nil {
		topShelfStore = shared
		log.Printf("topshelf: host store mode=%s", shared.Mode())
	} else {
		log.Printf("topshelf store unavailable: %v", err)
	}
	var pendingOpenURL string
	var activeView *tvui.View
	var invalidateWindow func()
	mygo.App.OnOpenURL(func(raw string) {
		log.Printf("topshelf: received openURL=%s", raw)
		mygo.RunOnMain(func() {
			if activeView == nil {
				pendingOpenURL = raw
				return
			}
			if activeView.OpenTopShelfURL(raw) && invalidateWindow != nil {
				invalidateWindow()
			}
		})
	})
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
		var aboutPlugins []app.AboutPlugin
		var installedPlugins []app.InstalledPlugin
		var sourcesMu sync.RWMutex
		sourcesByID := make(map[string]*plugin.Source)
		entrySource := make(map[string]string)
		stores := make(map[string]*plugin.Storage)

		mountSource := func(source *plugin.Source, receipt *plugin.InstalledReceipt) (app.Entry, app.AboutPlugin, app.InstalledPlugin, error) {
			info := source.Manifest()
			entry := app.Entry{ID: info.ID, SourceID: info.ID, Name: info.Name,
				Description: info.Description, Artwork: info.ID,
				Capabilities: source.Capabilities(), Auth: source.AuthPresentation(),
				Presentation: source.Presentation()}
			sourcesMu.RLock()
			store := stores[entry.ID]
			sourcesMu.RUnlock()
			if store == nil {
				var err error
				store, err = plugin.NewStorage(preferences.LoadPluginStorage(entry.SourceID, entry.ID), func(data []byte) error {
					return preferences.SavePluginStorage(entry.SourceID, entry.ID, data)
				})
				if err != nil {
					return app.Entry{}, app.AboutPlugin{}, app.InstalledPlugin{}, err
				}
			}
			sourcesMu.Lock()
			sourcesByID[entry.SourceID] = source
			entrySource[entry.ID] = entry.SourceID
			stores[entry.ID] = store
			sourcesMu.Unlock()
			assets.PrepareArtwork(entry.Artwork, source.Cover())
			assets.PrepareAccountArtwork(entry.ID, source.AuthArtwork())

			aboutPl := app.AboutPlugin{
				ID:           info.ID,
				Name:         info.Name,
				Kind:         string(info.Kind),
				Version:      info.Version,
				Bundled:      source.Bundled(),
				SourceCommit: source.SourceCommit(),
				License:      info.License,
				LicenseText:  source.LicenseText(),
			}
			installedPl := app.InstalledPlugin{
				ID:           info.ID,
				Name:         info.Name,
				Description:  info.Description,
				Kind:         info.Kind,
				Version:      info.Version,
				Bundled:      source.Bundled(),
				Enabled:      true,
				License:      info.License,
				SourceCommit: source.SourceCommit(),
				Capabilities: source.Capabilities(),
			}
			if receipt != nil {
				installedPl.Bundled = false
				installedPl.Repository = receipt.Repository
				installedPl.ReleaseID = receipt.ReleaseID
				installedPl.TagName = receipt.TagName
				installedPl.AssetID = receipt.AssetID
				installedPl.SHA256 = receipt.SHA256
				installedPl.Enabled = receipt.Enabled
				installedPl.InstalledAt = receipt.InstalledAt
				installedPl.UpdatedAt = receipt.UpdatedAt
				installedPl.Permissions = append([]string(nil), receipt.Permissions...)
				aboutPl.Bundled = false
			}
			return entry, aboutPl, installedPl, nil
		}

		pluginCatalogBase := os.Getenv("MIKUTV_PLUGIN_CATALOG_BASE_URL")
		packageStore := platform.NewPluginPackageStore()
		if pluginCatalogBase != "" {
			packageStore = platform.NewPluginPackageStoreNamed("plugins-override")
		}
		disabledBundledList, err := packageStore.LoadDisabledBundled()
		if err != nil {
			log.Printf("load disabled bundled plugins: %v", err)
		}
		disabledBundled := make(map[string]bool, len(disabledBundledList))
		for _, id := range disabledBundledList {
			disabledBundled[strings.ToLower(strings.TrimSpace(id))] = true
		}

		mountedIDs := make(map[string]bool)
		for _, source := range sources {
			entry, aboutPl, installedPl, err := mountSource(source, nil)
			if err != nil {
				log.Fatalf("open plugin %s storage: %v", source.Manifest().ID, err)
			}
			mountedIDs[strings.ToLower(entry.ID)] = true
			if disabledBundled[strings.ToLower(entry.ID)] {
				installedPl.Enabled = false
			} else {
				entries = append(entries, entry)
			}
			aboutPlugins = append(aboutPlugins, aboutPl)
			installedPlugins = append(installedPlugins, installedPl)
		}

		externalPackages, externalFailures := packageStore.LoadAll(context.Background(), appVersion)
		for _, err := range externalFailures {
			log.Printf("external plugin load: %v", err)
		}
		for _, pkg := range externalPackages {
			if mountedIDs[strings.ToLower(pkg.Receipt.ID)] {
				log.Printf("skipping external plugin %s: conflicts with bundled plugin", pkg.Receipt.ID)
				continue
			}
			receipt := pkg.Receipt
			entry, aboutPl, installedPl, err := mountSource(pkg.Source, &receipt)
			if err != nil {
				log.Printf("open external plugin %s storage: %v", receipt.ID, err)
				continue
			}
			mountedIDs[strings.ToLower(entry.ID)] = true
			if receipt.Enabled {
				entries = append(entries, entry)
			}
			aboutPlugins = append(aboutPlugins, aboutPl)
			installedPlugins = append(installedPlugins, installedPl)
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
			sourcesMu.RLock()
			src := sourcesByID[entrySource[entryID]]
			store := stores[entryID]
			sourcesMu.RUnlock()
			if src == nil {
				return nil
			}
			return src.NewSession(entryID, store)
		}, mygo.RunOnMain, invalidate)
		activity := app.NewAccountActivity(preferences, mygo.RunOnMain, invalidate)
		privateMessages := app.NewPrivateMessages(mygo.RunOnMain, invalidate)
		search := app.NewCatalogSearch(preferences, mygo.RunOnMain, invalidate)
		following := app.NewFollowingFeed(mygo.RunOnMain, invalidate)
		series := app.NewSeriesCatalog(mygo.RunOnMain, invalidate)
		live := app.NewLiveCatalog(mygo.RunOnMain, invalidate)
		liveChat := app.NewLiveChat(mygo.RunOnMain, invalidate)
		danmakuRules := app.NewDanmakuRules(preferences, mygo.RunOnMain, invalidate)
		playbackSettings := app.NewPlaybackSettings(preferences, invalidate)
		pluginCatalogCache := platform.NewPluginCatalogCache()
		if pluginCatalogBase != "" {
			pluginCatalogCache = platform.NewPluginCatalogCacheNamed("plugin-catalog-override-v1.json")
		}
		pluginCatalog := app.NewPluginCatalog(
			installedPlugins,
			plugin.NewCatalogClient(appVersion, pluginCatalogBase),
			pluginCatalogCache,
			mygo.RunOnMain,
			invalidate,
		)
		var view *tvui.View
		upsertRuntimeSource := func(source *plugin.Source, receipt plugin.InstalledReceipt, bumpGeneration bool) error {
			entry, aboutPl, _, err := mountSource(source, &receipt)
			if err != nil {
				return err
			}
			if receipt.Enabled {
				current := state.Entries()
				next := make([]app.Entry, 0, len(current)+1)
				replaced := false
				for _, existing := range current {
					if strings.EqualFold(existing.ID, entry.ID) {
						if bumpGeneration {
							entry.Generation = existing.Generation + 1
						}
						next = append(next, entry)
						replaced = true
					} else {
						next = append(next, existing)
					}
				}
				if !replaced {
					next = append(next, entry)
				}
				state.UpdateEntries(next)
			}
			if view != nil {
				replaced := false
				for i, existing := range view.About.Plugins {
					if strings.EqualFold(existing.ID, aboutPl.ID) {
						view.About.Plugins[i] = aboutPl
						replaced = true
						break
					}
				}
				if !replaced {
					view.About.Plugins = append(view.About.Plugins, aboutPl)
				}
			}
			return nil
		}
		pluginCatalog.SetInstaller(packageStore, appVersion, func(source *plugin.Source, receipt plugin.InstalledReceipt) error {
			return upsertRuntimeSource(source, receipt, false)
		})
		var auth *app.Auth
		stopActiveSourceIfCurrent := func(pluginID string) {
			if !strings.EqualFold(state.CurrentEntry, pluginID) {
				return
			}
			if view != nil && view.Playback != nil {
				view.Playback.Close()
			}
			liveChat.Close()
			live.Close()
			series.Close()
			following.Close()
			search.Close()
			privateMessages.Close()
			activity.Close()
			catalog.Close()
			if auth != nil {
				auth.Close()
			}
		}
		removeEntryByID := func(pluginID string) {
			current := state.Entries()
			next := make([]app.Entry, 0, len(current))
			for _, existing := range current {
				if strings.EqualFold(existing.ID, pluginID) {
					continue
				}
				next = append(next, existing)
			}
			state.UpdateEntries(next)
		}
		pluginCatalog.SetUpdateHooks(app.PluginUpdateHooks{
			BeforeSourceUpdate: func(pluginID string) {
				stopActiveSourceIfCurrent(pluginID)
			},
			ApplySourceUpdate: func(source *plugin.Source, receipt plugin.InstalledReceipt) error {
				if err := upsertRuntimeSource(source, receipt, true); err != nil {
					return err
				}
				if view != nil && strings.EqualFold(state.CurrentEntry, receipt.ID) {
					view.SyncEntry()
				}
				return nil
			},
			ApplyThemeUpdate: func(plugin.InstalledReceipt, []byte) error {
				return plugin.ErrIncompatible
			},
		})
		pluginCatalog.SetLifecycleHooks(app.PluginLifecycleHooks{
			DisableSource: func(pluginID string) {
				stopActiveSourceIfCurrent(pluginID)
				removeEntryByID(pluginID)
				if view != nil {
					view.SyncEntry()
				}
			},
			EnableSource: func(pluginID string) error {
				sourcesMu.RLock()
				src := sourcesByID[pluginID]
				sourcesMu.RUnlock()
				if src == nil {
					return plugin.ErrIncompatible
				}
				info := src.Manifest()
				entry := app.Entry{
					ID: info.ID, SourceID: info.ID, Name: info.Name,
					Description: info.Description, Artwork: info.ID,
					Capabilities: src.Capabilities(), Auth: src.AuthPresentation(),
					Presentation: src.Presentation(),
				}
				current := state.Entries()
				for _, existing := range current {
					if strings.EqualFold(existing.ID, entry.ID) {
						return nil
					}
				}
				var next []app.Entry
				if src.Bundled() {
					next = append([]app.Entry{entry}, current...)
				} else {
					next = append(append([]app.Entry(nil), current...), entry)
				}
				state.UpdateEntries(next)
				return nil
			},
			RemoveSource: func(pluginID string, clearData bool) {
				stopActiveSourceIfCurrent(pluginID)
				sourcesMu.Lock()
				delete(sourcesByID, pluginID)
				delete(entrySource, pluginID)
				if clearData {
					delete(stores, pluginID)
				}
				sourcesMu.Unlock()
				removeEntryByID(pluginID)
				if view != nil {
					nextAbout := make([]app.AboutPlugin, 0, len(view.About.Plugins))
					for _, pl := range view.About.Plugins {
						if strings.EqualFold(pl.ID, pluginID) {
							continue
						}
						nextAbout = append(nextAbout, pl)
					}
					view.About.Plugins = nextAbout
				}
				if clearData {
					_ = preferences.ClearEntry(pluginID)
					_ = preferences.ClearPluginStorage(pluginID, pluginID)
					if credentials != nil {
						_ = credentials.Save(pluginID, pluginID, nil)
					}
				}
				if view != nil {
					view.SyncEntry()
				}
			},
			DisableTheme: func(string) {},
			RemoveTheme:  func(string) {},
		})
		auth = app.NewAuth(credentials, mygo.RunOnMain, invalidate, func() {
			catalog.Reload()
			activity.Close()
			privateMessages.Close()
			if view != nil {
				view.SyncAccountActivity()
			}
		}, func() { view.PromptSignIn() })
		auth.SetCleaner(preferences)
		view = tvui.NewView(tvui.Options{State: state, Catalog: catalog, Search: search, Following: following, Series: series, Live: live, LiveChat: liveChat, Auth: auth, Activity: activity, PrivateMessages: privateMessages, DanmakuRules: danmakuRules, PlaybackSettings: playbackSettings, PluginCatalog: pluginCatalog, Pages: pages, Assets: assets,
			Preferences: preferences, Theme: theme.Default(), Version: appVersion,
			About: app.AboutInfo{Version: appVersion, Build: appBuild, Commit: appCommit, Plugins: aboutPlugins, Dependencies: aboutDeps},
			TopShelf: topShelfStore})
		activeView = view
		invalidateWindow = invalidate
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
			view.Playback.SetPreferences(playbackSettings)
			if pendingOpenURL != "" {
				raw := pendingOpenURL
				pendingOpenURL = ""
				view.OpenTopShelfURL(raw)
			}
			view.FlushPendingTopShelfURL()
			invalidate()
		})
		mygo.Power.OnSuspend(func() {
			view.SyncTopShelf()
			if view.Playback != nil {
				view.Playback.Suspend()
			}
		})
		window.OnReadyToShow(func() { log.Print("MikuTV: first frame ready") })
		view.SyncEntry()
		if pendingOpenURL != "" {
			raw := pendingOpenURL
			pendingOpenURL = ""
			view.OpenTopShelfURL(raw)
		}
		mygo.App.OnQuit(func() {
			if view.Playback != nil {
				view.Playback.Close()
				disposePlayer()
			}
			pluginCatalog.Close()
			danmakuRules.Close()
			liveChat.Close()
			live.Close()
			series.Close()
			following.Close()
			search.Close()
			privateMessages.Close()
			activity.Close()
			auth.Close()
			catalog.Close()
		})
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
