package tvui

import (
	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
)

type contentPage struct{}

func (contentPage) Render(c *ui.Context, p *PageContext) {
	p.region("content", nil)
	p.heading(c, p.Title, "")
	p.empty(c, p.Assets.Text.EmptyTitle, p.Assets.Text.EmptyNote, p.Assets.Text.SwitchEntry, app.ContentAction, "content", func() { p.State.OpenEntries("") })
}
