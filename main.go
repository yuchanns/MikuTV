//go:build ios && tvos && arm64

package main

import (
	"log"
	"os"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/platform"
	"github.com/yuchanns/MikuTV/internal/theme"
	tvui "github.com/yuchanns/MikuTV/internal/ui"
)

func main() {
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
		state, err := app.NewState(assets.Entries, saved.RecentEntry, pages.Routes)
		if err != nil {
			log.Fatal(err)
		}
		view := tvui.NewView(tvui.Options{State: state, Pages: pages, Assets: assets, Preferences: preferences, Theme: theme.Default(), Version: "0.1.0"})
		view.Trace = os.Getenv("MIKUTV_NAV_TRACE") == "1"
		if view.Trace {
			log.SetFlags(log.LstdFlags | log.Lmicroseconds)
		}
		window := mygo.NewWindow(mygo.WindowOptions{
			Title:   "MikuTV",
			Content: ui.View(view.Render),
		})
		window.OnReadyToShow(func() { log.Print("MikuTV: first frame ready") })
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
