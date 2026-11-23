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

type hostRuntime struct {
	assets       *tvui.Assets
	pages        *tvui.Pages
	preferences  *platform.Preferences
	credentials  app.CredentialStore
	packageStore *plugin.PackageStore
	topShelf     topshelf.Store
	aboutDeps    []app.AboutDependency

	state            *app.State
	catalog          *app.Catalog
	activity         *app.AccountActivity
	privateMessages  *app.PrivateMessages
	search           *app.CatalogSearch
	following        *app.FollowingFeed
	series           *app.SeriesCatalog
	live             *app.LiveCatalog
	liveChat         *app.LiveChat
	danmakuRules     *app.DanmakuRules
	playbackSettings *app.PlaybackSettings
	localeSettings   *app.LocaleSettings
	appearance       *app.AppearanceSettings
	pluginCatalog    *app.PluginCatalog
	auth             *app.Auth

	view           *tvui.View
	window         *mygo.Window
	disposePlayer  func()
	pendingOpenURL string

	sourcesMu   sync.RWMutex
	sourcesByID map[string]*plugin.Source
	entrySource map[string]string
	stores      map[string]*plugin.Storage
}

func main() {
	executable, err := os.Executable()
	if err != nil {
		log.Fatal(err)
	}
	execDir := filepath.Dir(executable)
	sources, failures := plugin.LoadBundled(context.Background(),
		os.DirFS(filepath.Join(execDir, "assets", "plugins")), appVersion)
	for _, err := range failures {
		log.Print(err)
	}
	aboutDeps, err := app.LoadBundledDependencies(filepath.Join(execDir, "assets", "licenses.json"))
	if err != nil {
		log.Printf("bundled licenses unavailable: %v", err)
		aboutDeps = app.RuntimeDependencies()
	}
	rt := &hostRuntime{
		aboutDeps:   aboutDeps,
		sourcesByID: make(map[string]*plugin.Source),
		entrySource: make(map[string]string),
		stores:      make(map[string]*plugin.Storage),
	}
	if shared, err := topshelf.NewSharedStore(); err == nil {
		rt.topShelf = shared
		log.Printf("topshelf: host store mode=%s", shared.Mode())
	} else {
		log.Printf("topshelf store unavailable: %v", err)
	}
	mygo.App.OnOpenURL(func(raw string) {
		log.Printf("topshelf: received openURL=%s", raw)
		mygo.RunOnMain(func() {
			if rt.view == nil {
				rt.pendingOpenURL = raw
				return
			}
			if rt.view.OpenTopShelfURL(raw) {
				rt.invalidate()
			}
		})
	})
	mygo.App.WhenReady(func() { rt.start(sources) })
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

func (rt *hostRuntime) invalidate() {
	if rt.window != nil {
		rt.window.Invalidate()
	}
}

func (rt *hostRuntime) flushPendingOpenURL() {
	if rt.pendingOpenURL == "" || rt.view == nil {
		return
	}
	raw := rt.pendingOpenURL
	rt.pendingOpenURL = ""
	rt.view.OpenTopShelfURL(raw)
}

func entryFromSource(source *plugin.Source) app.Entry {
	info := source.Manifest()
	return app.Entry{
		ID:            info.ID,
		SourceID:      info.ID,
		Name:          info.Name,
		Description:   info.Description,
		Artwork:       info.ID,
		Capabilities:  source.Capabilities(),
		Auth:          source.AuthPresentation(),
		AuthMethods:   source.AuthMethods(),
		ServerURLMode: source.ServerURLMode(),
		Presentation:  source.Presentation(),
	}
}

func buildPluginMetadata(info plugin.Manifest, receipt *plugin.InstalledReceipt, bundled bool, licenseText, sourceCommit string) (app.AboutPlugin, app.InstalledPlugin) {
	aboutPl := app.AboutPlugin{
		ID:           info.ID,
		Name:         info.Name,
		Kind:         string(info.Kind),
		Version:      info.Version,
		Bundled:      bundled,
		SourceCommit: sourceCommit,
		License:      info.License,
		LicenseText:  licenseText,
	}
	installedPl := app.InstalledPlugin{
		ID:           info.ID,
		Name:         info.Name,
		Description:  info.Description,
		Kind:         info.Kind,
		Version:      info.Version,
		Bundled:      bundled,
		Enabled:      bundled,
		License:      info.License,
		SourceCommit: sourceCommit,
	}
	if receipt != nil {
		aboutPl.Bundled = false
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
	}
	return aboutPl, installedPl
}

func buildThemeMetadata(pkg *plugin.ThemePackage, receipt plugin.InstalledReceipt) (app.AboutPlugin, app.InstalledPlugin) {
	aboutPl, installedPl := buildPluginMetadata(pkg.Manifest(), &receipt, false, pkg.LicenseText(), "")
	installedPl.Enabled = false
	installedPl.Swatches = pkg.Theme().Swatches()
	return aboutPl, installedPl
}

func buildLocaleMetadata(pkg *plugin.LocalePackage, receipt plugin.InstalledReceipt) (app.AboutPlugin, app.InstalledPlugin) {
	aboutPl, installedPl := buildPluginMetadata(pkg.Manifest(), &receipt, false, pkg.LicenseText(), "")
	installedPl.Enabled = false
	installedPl.Language = pkg.Language()
	installedPl.NativeName = pkg.NativeName()
	return aboutPl, installedPl
}

func (rt *hostRuntime) mountSource(source *plugin.Source, receipt *plugin.InstalledReceipt) (app.Entry, app.AboutPlugin, app.InstalledPlugin, error) {
	entry := entryFromSource(source)
	rt.sourcesMu.RLock()
	store := rt.stores[entry.ID]
	rt.sourcesMu.RUnlock()
	if store == nil {
		var err error
		store, err = plugin.NewStorage(rt.preferences.LoadPluginStorage(entry.SourceID, entry.ID), func(data []byte) error {
			return rt.preferences.SavePluginStorage(entry.SourceID, entry.ID, data)
		})
		if err != nil {
			return app.Entry{}, app.AboutPlugin{}, app.InstalledPlugin{}, err
		}
	}
	rt.sourcesMu.Lock()
	rt.sourcesByID[entry.SourceID] = source
	rt.entrySource[entry.ID] = entry.SourceID
	rt.stores[entry.ID] = store
	rt.sourcesMu.Unlock()
	rt.assets.PrepareArtwork(entry.Artwork, source.Cover())
	rt.assets.PrepareAccountArtwork(entry.ID, source.AuthArtwork())

	aboutPl, installedPl := buildPluginMetadata(source.Manifest(), receipt, source.Bundled(), source.LicenseText(), source.SourceCommit())
	installedPl.Capabilities = source.Capabilities()
	if receipt == nil {
		installedPl.Enabled = true
	}
	return entry, aboutPl, installedPl, nil
}

func (rt *hostRuntime) refreshEntryLocalizations(tag string) {
	current := rt.state.Entries()
	if len(current) == 0 {
		return
	}
	next := make([]app.Entry, len(current))
	rt.sourcesMu.RLock()
	for i, entry := range current {
		if src := rt.sourcesByID[entry.SourceID]; src != nil {
			entry.Name, entry.Description, entry.Auth, entry.Presentation = src.LocalizedMetadata(tag)
		}
		next[i] = entry
	}
	rt.sourcesMu.RUnlock()
	rt.state.UpdateEntries(next)
}

func (rt *hostRuntime) upsertAboutPlugin(aboutPl app.AboutPlugin) {
	if rt.view == nil {
		return
	}
	for i, existing := range rt.view.About.Plugins {
		if strings.EqualFold(existing.ID, aboutPl.ID) {
			rt.view.About.Plugins[i] = aboutPl
			return
		}
	}
	rt.view.About.Plugins = append(rt.view.About.Plugins, aboutPl)
}

func (rt *hostRuntime) removeAboutPlugin(pluginID string) {
	if rt.view == nil {
		return
	}
	nextAbout := make([]app.AboutPlugin, 0, len(rt.view.About.Plugins))
	for _, pl := range rt.view.About.Plugins {
		if !strings.EqualFold(pl.ID, pluginID) {
			nextAbout = append(nextAbout, pl)
		}
	}
	rt.view.About.Plugins = nextAbout
}

func (rt *hostRuntime) upsertRuntimeSource(source *plugin.Source, receipt plugin.InstalledReceipt, bumpGeneration bool) error {
	entry, aboutPl, _, err := rt.mountSource(source, &receipt)
	if err != nil {
		return err
	}
	entry.Name, entry.Description, entry.Auth, entry.Presentation = source.LocalizedMetadata(rt.localeSettings.ActiveTag())
	if receipt.Enabled {
		current := rt.state.Entries()
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
		rt.state.UpdateEntries(next)
	}
	rt.upsertAboutPlugin(aboutPl)
	return nil
}

func (rt *hostRuntime) removeEntryByID(pluginID string) {
	current := rt.state.Entries()
	next := make([]app.Entry, 0, len(current))
	for _, existing := range current {
		if !strings.EqualFold(existing.ID, pluginID) {
			next = append(next, existing)
		}
	}
	rt.state.UpdateEntries(next)
}

func (rt *hostRuntime) closeSourceControllers() {
	if rt.view != nil && rt.view.Playback != nil {
		rt.view.Playback.Close()
	}
	rt.liveChat.Close()
	rt.live.Close()
	rt.series.Close()
	rt.following.Close()
	rt.search.Close()
	rt.privateMessages.Close()
	rt.activity.Close()
	if rt.auth != nil {
		rt.auth.Close()
	}
	rt.catalog.Close()
}

func (rt *hostRuntime) stopActiveSourceIfCurrent(pluginID string) {
	if strings.EqualFold(rt.state.CurrentEntry, pluginID) {
		rt.closeSourceControllers()
	}
}

func (rt *hostRuntime) mountInstalledPackage(pkg plugin.InstalledPackage, reapply bool) error {
	switch {
	case pkg.Theme != nil:
		aboutPl, _ := buildThemeMetadata(pkg.Theme, pkg.Receipt)
		rt.appearance.RegisterTheme(pkg.Theme)
		if reapply {
			rt.appearance.ReapplyStoredThemes()
		}
		rt.upsertAboutPlugin(aboutPl)
		return nil
	case pkg.Locale != nil:
		aboutPl, _ := buildLocaleMetadata(pkg.Locale, pkg.Receipt)
		rt.localeSettings.RegisterLocale(pkg.Locale)
		if reapply {
			rt.localeSettings.ReapplyStoredLocale()
		}
		rt.upsertAboutPlugin(aboutPl)
		return nil
	case pkg.Source != nil:
		if err := rt.upsertRuntimeSource(pkg.Source, pkg.Receipt, false); err != nil {
			return err
		}
		if reapply && rt.view != nil {
			rt.view.SyncEntry()
		}
		return nil
	default:
		return plugin.ErrIncompatible
	}
}

func (rt *hostRuntime) start(bundledSources []*plugin.Source) {
	var err error
	if rt.assets, err = tvui.LoadAssets(); err != nil {
		log.Fatal(err)
	}
	if rt.preferences, err = platform.NewPreferences(); err != nil {
		log.Fatal(err)
	}
	if keychain, err := platform.NewCredentialKeychain(); err == nil {
		rt.credentials = keychain
	} else {
		log.Printf("credential store unavailable: %v", err)
	}
	saved := rt.preferences.Load()
	if rt.pages, err = tvui.NewPages(rt.assets); err != nil {
		log.Fatal(err)
	}

	pluginCatalogBase := os.Getenv("MIKUTV_PLUGIN_CATALOG_BASE_URL")
	rt.packageStore = platform.NewPluginPackageStore()
	if pluginCatalogBase != "" {
		rt.packageStore = platform.NewPluginPackageStoreNamed("plugins-override")
	} else {
		rt.packageStore.SetBackup(rt.preferences)
	}
	disabledBundledList, err := rt.packageStore.LoadDisabledBundled()
	if err != nil {
		log.Printf("load disabled bundled plugins: %v", err)
	}
	disabledBundled := make(map[string]bool, len(disabledBundledList))
	for _, id := range disabledBundledList {
		disabledBundled[strings.ToLower(strings.TrimSpace(id))] = true
	}

	var entries []app.Entry
	var aboutPlugins []app.AboutPlugin
	var installedPlugins []app.InstalledPlugin
	var loadedThemes []*plugin.ThemePackage
	var loadedLocales []*plugin.LocalePackage
	mountedIDs := make(map[string]bool)

	for _, source := range bundledSources {
		entry, aboutPl, installedPl, err := rt.mountSource(source, nil)
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

	externalPackages, missingReceipts, cachePurged, externalFailures := rt.packageStore.LoadAllWithMissing(context.Background(), appVersion)
	if cachePurged {
		log.Printf("plugin recovery: detected tvOS cache purge (%d missing plugins queued for self-healing)", len(missingReceipts))
	}
	for _, err := range externalFailures {
		log.Printf("external plugin load: %v", err)
	}
	for _, pkg := range externalPackages {
		if mountedIDs[strings.ToLower(pkg.Receipt.ID)] {
			log.Printf("skipping external plugin %s: conflicts with bundled plugin", pkg.Receipt.ID)
			continue
		}
		receipt := pkg.Receipt
		switch {
		case pkg.Theme != nil:
			aboutPl, installedPl := buildThemeMetadata(pkg.Theme, receipt)
			mountedIDs[strings.ToLower(receipt.ID)] = true
			loadedThemes = append(loadedThemes, pkg.Theme)
			aboutPlugins = append(aboutPlugins, aboutPl)
			installedPlugins = append(installedPlugins, installedPl)
		case pkg.Locale != nil:
			aboutPl, installedPl := buildLocaleMetadata(pkg.Locale, receipt)
			mountedIDs[strings.ToLower(receipt.ID)] = true
			loadedLocales = append(loadedLocales, pkg.Locale)
			aboutPlugins = append(aboutPlugins, aboutPl)
			installedPlugins = append(installedPlugins, installedPl)
		case pkg.Source != nil:
			entry, aboutPl, installedPl, err := rt.mountSource(pkg.Source, &receipt)
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
	}

	if rt.state, err = app.NewState(entries, saved.RecentEntry, rt.pages.Routes); err != nil {
		log.Fatal(err)
	}
	rt.catalog = app.NewCatalog(func(entryID string) provider.Session {
		rt.sourcesMu.RLock()
		src := rt.sourcesByID[rt.entrySource[entryID]]
		store := rt.stores[entryID]
		rt.sourcesMu.RUnlock()
		if src == nil {
			return nil
		}
		return src.NewSession(entryID, store)
	}, mygo.RunOnMain, rt.invalidate)
	rt.activity = app.NewAccountActivity(rt.preferences, mygo.RunOnMain, rt.invalidate)
	rt.privateMessages = app.NewPrivateMessages(mygo.RunOnMain, rt.invalidate)
	rt.search = app.NewCatalogSearch(rt.preferences, mygo.RunOnMain, rt.invalidate)
	rt.following = app.NewFollowingFeed(mygo.RunOnMain, rt.invalidate)
	rt.series = app.NewSeriesCatalog(mygo.RunOnMain, rt.invalidate)
	rt.live = app.NewLiveCatalog(mygo.RunOnMain, rt.invalidate)
	rt.liveChat = app.NewLiveChat(mygo.RunOnMain, rt.invalidate)
	rt.danmakuRules = app.NewDanmakuRules(rt.preferences, mygo.RunOnMain, rt.invalidate)
	rt.playbackSettings = app.NewPlaybackSettings(rt.preferences, rt.invalidate)

	pluginCatalogCache := platform.NewPluginCatalogCache()
	if pluginCatalogBase != "" {
		pluginCatalogCache = platform.NewPluginCatalogCacheNamed("plugin-catalog-override-v1.json")
	}

	systemLanguages := platform.PreferredLanguages()
	rt.localeSettings = app.NewLocaleSettings(
		rt.packageStore,
		loadedLocales,
		systemLanguages,
		func(tag string, overrideJSON []byte) {
			if rt.view != nil {
				if err := rt.view.SetLocale(tag, overrideJSON); err != nil {
					log.Printf("locale: apply %s failed: %v", tag, err)
				}
			} else {
				if err := rt.assets.ApplyLocale(tag, overrideJSON); err != nil {
					log.Printf("locale: apply %s failed: %v", tag, err)
				}
				rt.pages.RefreshLabels(rt.assets)
			}
			rt.refreshEntryLocalizations(tag)
			rt.catalog.Reload()
		},
		func(activeLocaleID string) {
			if rt.pluginCatalog != nil {
				rt.pluginCatalog.SyncActiveLocale(activeLocaleID)
			}
		},
		rt.invalidate,
	)
	plugin.SetHostLocaleProvider(rt.localeSettings.ActiveTag)
	initialLocaleTag, initialLocaleJSON := rt.localeSettings.ActivePayload()
	log.Printf("locale: preferred=%v active=%s", systemLanguages, initialLocaleTag)
	if err := rt.assets.ApplyLocale(initialLocaleTag, initialLocaleJSON); err != nil {
		log.Printf("locale: initial apply %s failed: %v", initialLocaleTag, err)
	}
	rt.pages.RefreshLabels(rt.assets)
	rt.refreshEntryLocalizations(initialLocaleTag)

	rt.appearance = app.NewAppearanceSettings(
		rt.packageStore,
		loadedThemes,
		func(next theme.Theme) {
			if rt.view != nil {
				rt.view.SetTheme(next)
			}
		},
		func(activeThemeID string) {
			if rt.pluginCatalog != nil {
				rt.pluginCatalog.SyncActiveTheme(activeThemeID)
			}
		},
		rt.invalidate,
	)
	systemDark := mygo.Theme.IsDark()
	overrideAppearance := strings.ToLower(strings.TrimSpace(os.Getenv("MIKUTV_SYSTEM_APPEARANCE_OVERRIDE")))
	switch overrideAppearance {
	case "light":
		systemDark = false
	case "dark":
		systemDark = true
	}
	log.Printf("appearance: systemDark=%v override=%q", systemDark, overrideAppearance)
	rt.appearance.SetSystemDark(systemDark)
	if overrideAppearance == "" {
		mygo.Theme.OnUpdated(func() {
			dark := mygo.Theme.IsDark()
			log.Printf("appearance: system theme updated systemDark=%v", dark)
			rt.appearance.SetSystemDark(dark)
		})
	}

	activeThemeID := rt.appearance.ActiveThemeID()
	activeLocaleID := rt.localeSettings.ActiveID()
	for i := range installedPlugins {
		switch installedPlugins[i].Kind {
		case plugin.ThemeKind:
			installedPlugins[i].Enabled = !theme.IsBuiltin(activeThemeID) && strings.EqualFold(installedPlugins[i].ID, activeThemeID)
		case plugin.LocaleKind:
			installedPlugins[i].Enabled = strings.EqualFold(installedPlugins[i].ID, activeLocaleID)
		}
	}

	rt.pluginCatalog = app.NewPluginCatalog(
		installedPlugins,
		plugin.NewCatalogClient(appVersion, pluginCatalogBase),
		pluginCatalogCache,
		mygo.RunOnMain,
		rt.invalidate,
	)
	rt.configurePluginHooks()

	rt.auth = app.NewAuth(rt.credentials, mygo.RunOnMain, rt.invalidate, func() {
		rt.catalog.Reload()
		rt.activity.Close()
		rt.privateMessages.Close()
		if rt.view != nil {
			rt.view.SyncAccountActivity()
		}
	}, func() { rt.view.PromptSignIn() })
	rt.auth.SetCleaner(rt.preferences)

	rt.view = tvui.NewView(tvui.Options{
		State:            rt.state,
		Catalog:          rt.catalog,
		Search:           rt.search,
		Following:        rt.following,
		Series:           rt.series,
		Live:             rt.live,
		LiveChat:         rt.liveChat,
		Auth:             rt.auth,
		Activity:         rt.activity,
		PrivateMessages:  rt.privateMessages,
		DanmakuRules:     rt.danmakuRules,
		PlaybackSettings: rt.playbackSettings,
		Appearance:       rt.appearance,
		Locale:           rt.localeSettings,
		PluginCatalog:    rt.pluginCatalog,
		Pages:            rt.pages,
		Assets:           rt.assets,
		Preferences:      rt.preferences,
		Theme:            rt.appearance.ActiveTheme(),
		Version:          appVersion,
		About: app.AboutInfo{
			Version:      appVersion,
			Build:        appBuild,
			Commit:       appCommit,
			Plugins:      aboutPlugins,
			Dependencies: rt.aboutDeps,
		},
		TopShelf: rt.topShelf,
	})
	rt.view.Trace = os.Getenv("MIKUTV_NAV_TRACE") == "1"
	if rt.view.Trace {
		log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	}

	rt.window = mygo.NewWindow(mygo.WindowOptions{
		Title:       "MikuTV",
		Content:     ui.View(rt.view.Render),
		Transparent: true,
	})
	rt.window.OnReadyToShow(func() {
		log.Print("MikuTV: first frame ready")
		engine, dispose := player.NewNative(rt.window.NativeHandle())
		rt.disposePlayer = dispose
		rt.view.Playback = app.NewPlayback(rt.catalog, engine, rt.preferences, mygo.RunOnMain, func() {
			rt.view.PlaybackChanged()
			rt.invalidate()
		})
		rt.view.Playback.SetPreferences(rt.playbackSettings)
		rt.flushPendingOpenURL()
		rt.view.FlushPendingTopShelfURL()
		rt.invalidate()
	})
	mygo.Power.OnSuspend(func() {
		rt.view.SyncTopShelf()
		if rt.view.Playback != nil {
			rt.view.Playback.Suspend()
		}
	})
	rt.view.SyncEntry()
	if len(missingReceipts) > 0 {
		rt.pluginCatalog.StartStartupRecovery(missingReceipts, func(pkg plugin.InstalledPackage) error {
			return rt.mountInstalledPackage(pkg, true)
		}, func(restored, failed int) {
			log.Printf("plugin recovery: finished restored=%d failed=%d", restored, failed)
			rt.appearance.ReapplyStoredThemes()
			rt.localeSettings.ReapplyStoredLocale()
			rt.view.SyncEntry()
		})
	}
	rt.flushPendingOpenURL()
	mygo.App.OnQuit(func() {
		rt.closeSourceControllers()
		if rt.disposePlayer != nil {
			rt.disposePlayer()
		}
		rt.pluginCatalog.Close()
		rt.danmakuRules.Close()
	})
}

func (rt *hostRuntime) configurePluginHooks() {
	rt.pluginCatalog.SetInstaller(rt.packageStore, appVersion, func(pkg plugin.InstalledPackage) error {
		return rt.mountInstalledPackage(pkg, false)
	})
	rt.pluginCatalog.SetUpdateHooks(app.PluginUpdateHooks{
		BeforeSourceUpdate: rt.stopActiveSourceIfCurrent,
		ApplySourceUpdate: func(source *plugin.Source, receipt plugin.InstalledReceipt) error {
			if err := rt.upsertRuntimeSource(source, receipt, true); err != nil {
				return err
			}
			if rt.view != nil && strings.EqualFold(rt.state.CurrentEntry, receipt.ID) {
				rt.view.SyncEntry()
			}
			return nil
		},
		ApplyThemeUpdate: func(themePkg *plugin.ThemePackage, receipt plugin.InstalledReceipt) error {
			if err := rt.appearance.UpdateTheme(themePkg); err != nil {
				return err
			}
			aboutPl, _ := buildThemeMetadata(themePkg, receipt)
			rt.upsertAboutPlugin(aboutPl)
			return nil
		},
		ApplyLocaleUpdate: func(localePkg *plugin.LocalePackage, receipt plugin.InstalledReceipt) error {
			if err := rt.localeSettings.UpdateLocale(localePkg); err != nil {
				return err
			}
			aboutPl, _ := buildLocaleMetadata(localePkg, receipt)
			rt.upsertAboutPlugin(aboutPl)
			return nil
		},
	})
	rt.pluginCatalog.SetLifecycleHooks(app.PluginLifecycleHooks{
		DisableSource: func(pluginID string) {
			rt.stopActiveSourceIfCurrent(pluginID)
			rt.removeEntryByID(pluginID)
			if rt.view != nil {
				rt.view.SyncEntry()
			}
		},
		EnableSource: func(pluginID string) error {
			rt.sourcesMu.RLock()
			src := rt.sourcesByID[pluginID]
			rt.sourcesMu.RUnlock()
			if src == nil {
				return plugin.ErrIncompatible
			}
			entry := entryFromSource(src)
			current := rt.state.Entries()
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
			rt.state.UpdateEntries(next)
			return nil
		},
		RemoveSource: func(pluginID string, clearData bool) {
			rt.stopActiveSourceIfCurrent(pluginID)
			rt.sourcesMu.Lock()
			delete(rt.sourcesByID, pluginID)
			delete(rt.entrySource, pluginID)
			if clearData {
				delete(rt.stores, pluginID)
			}
			rt.sourcesMu.Unlock()
			rt.removeEntryByID(pluginID)
			rt.removeAboutPlugin(pluginID)
			if clearData {
				_ = rt.preferences.ClearEntry(pluginID)
				_ = rt.preferences.ClearPluginStorage(pluginID, pluginID)
				if rt.credentials != nil {
					_ = rt.credentials.Save(pluginID, pluginID, nil)
				}
			}
			if rt.view != nil {
				rt.view.SyncEntry()
			}
		},
		EnableTheme: func(pluginID string) error {
			_, err := rt.appearance.SelectTheme(pluginID)
			return err
		},
		DisableTheme: func(pluginID string) { rt.appearance.DisableTheme(pluginID) },
		RemoveTheme: func(pluginID string) {
			rt.appearance.RemoveTheme(pluginID)
			rt.removeAboutPlugin(pluginID)
		},
		EnableLocale:  func(pluginID string) error { return rt.localeSettings.Select(pluginID) },
		DisableLocale: func(pluginID string) { rt.localeSettings.DisableLocale(pluginID) },
		RemoveLocale: func(pluginID string) {
			rt.localeSettings.RemoveLocale(pluginID)
			rt.removeAboutPlugin(pluginID)
		},
	})
}
