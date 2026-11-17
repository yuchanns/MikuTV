package tvui

import (
	"log"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
)

type entriesPage struct{}

func (entriesPage) Render(c *ui.Context, p *PageContext) {
	width := p.Width - 186
	p.region("entries", focusEdges{})
	text := p.Assets.Text
	note := text.EntriesNote
	if entry, ok := p.State.Current(); ok {
		note = text.CurrentEntry + " · " + entry.Name
	}
	p.heading(c, text.EntriesTitle, note)
	entries := p.State.AvailableEntries()
	if len(entries) == 0 {
		p.empty(c, text.EmptyEntries, text.EmptyEntryNote, text.Manage, app.ManagePlugins, "entries", func() {
			p.State.Navigate(app.Route{Page: app.Settings, Section: "plugins"})
		})
		return
	}
	columns := max(1, min(3, int(width/430)))
	cardWidth := (width - float32(columns-1)*30) / float32(columns)
	ui.Grid(c).FillWidth().Columns(columns).Gap(30).Children(func() {
		for _, entry := range entries {
			p.entryCard(c, entry, cardWidth)
		}
	})
}

func (p *PageContext) entryCard(c *ui.Context, entry app.Entry, width float32) {
	id := app.EntryFocus(entry.ID)
	colors := p.Theme.Colors
	border := ui.Transparent
	if p.State.Focus == id {
		border = paint(colors.Focus.Ring)
	}
	e := ui.Column(c.Key(string(id))).FillWidth().Gap(14).Role(ui.RoleButton).Label(entry.Name).
		OnClick(func() {
			p.dispatch(func() {
				if p.State.SelectEntry(entry.ID) {
					if err := p.Preferences.Save(app.Preferences{RecentEntry: entry.ID}); err != nil {
						log.Print(err)
						p.host.notice = p.Assets.Text.SaveFailed
					}
				}
			})
		})
	e.Children(func() {
		ui.Box(c).FillWidth().Height(width/2).Radius(14).Border(4, border).
			Background(paint(colors.Decoration.CardStart)).Clip().
			Transition(ui.ElementTransition{Colors: true, Duration: focusDuration}).Children(func() {
			if image := p.Assets.Artwork(entry.Artwork); image != nil {
				ui.Image(c, image).Fill().Fit(ui.Cover)
			} else {
				ui.Box(c).Fill().Center().Children(func() { ui.Icon(c, p.Assets.Icon("tv")).Size(96, 96).TextColor(paint(colors.Accent.Soft)) })
			}
			if entry.ID == p.State.CurrentEntry {
				ui.Row(c).Absolute().Right(16).Top(16).Gap(8).Padding(8, 12).Radius(8).
					Background(paint(colors.Selected.Background)).TextColor(paint(colors.Selected.Foreground)).Children(func() {
					ui.Icon(c, p.Assets.Icon("check")).Size(18, 18)
					ui.Text(c, p.Assets.Text.CurrentEntry).FontSize(16)
				})
			}
		})
		ui.Text(c, entry.Name).FontSize(27).Bold()
		ui.Text(c, entry.Description).FontSize(19).TextColor(paint(colors.Text.Secondary))
	})
	p.bind(e, id, "entries")
}
