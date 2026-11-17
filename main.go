//go:build ios && tvos && arm64

package main

import (
	"log"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func main() {
	count := 0
	focus := "hello"
	mygo.App.WhenReady(func() {
		window := mygo.NewWindow(mygo.WindowOptions{
			Title: "MikuTV",
			Content: ui.View(func(c *ui.Context) {
				ui.Column(c).Fill().Center().Gap(28).Children(func() {
					ui.Text(c, "Hello World").FontSize(76).Bold()
					ui.Text(c, "MikuTV / MyGo / tvOS").FontSize(32)
					button := ui.PrimaryButton(c.Key("hello"), "Say hello").Height(72).Width(320)
					button.FocusBind(&focus, "hello")
					if button.Clicked() {
						count++
						log.Printf("hello: selected %d", count)
					}
					ui.Textf(c, "Selected: %d", count).FontSize(28)
				})
			}),
		})
		window.OnReadyToShow(func() { log.Print("hello: first frame ready") })
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
