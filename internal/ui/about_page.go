package tvui

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/skip2/go-qrcode"
	"github.com/yuchanns/MikuTV/internal/app"
)

type aboutPage struct{}

func (p *PageContext) resolvedAboutInfo() app.AboutInfo {
	info := p.host.About.Normalized(p.Version)
	if len(info.Plugins) == 0 && p.State != nil {
		for _, entry := range p.State.Entries() {
			info.Plugins = append(info.Plugins, app.AboutPlugin{
				ID:      entry.ID,
				Name:    entry.Name,
				Kind:    "source",
				Bundled: true,
			})
		}
	}
	if len(info.Dependencies) == 0 {
		info.Dependencies = app.RuntimeDependencies()
	}
	return info
}

func (p *PageContext) aboutPluginsSummary(info app.AboutInfo) string {
	about := p.Assets.Text.About
	if len(info.Plugins) == 0 {
		return about.NoPlugins
	}
	first := info.Plugins[0]
	name := strings.TrimSpace(first.Name)
	if name == "" {
		name = first.ID
	}
	if first.Version != "" {
		name += " v" + first.Version
	}
	if len(info.Plugins) == 1 {
		return name
	}
	return fmt.Sprintf(about.PluginsMore, name, len(info.Plugins))
}

func (aboutPage) Render(c *ui.Context, p *PageContext) {
	p.region("content", nil)
	text, colors, d := p.Assets.Text, p.Theme.Colors, p.uiColors()
	info := p.resolvedAboutInfo()
	ui.Row(c).FillWidth().Padding(24, 0, 0, 0).Gap(65).AlignItems(ui.Start).Children(func() {
		ui.Column(c).Width(320).Shrink(0).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(280, 168).Radius(25).Clip().Children(func() {
				ui.Image(c, p.Assets.AboutArtwork).Fill()
			})
			ui.Text(c, "MikuTV").FontSize(40).FontWeight(500).LetterSpacing(3).Margin(26, 0, 0, 0)
			ui.Text(c, text.AboutNote).MaxWidth(300).TextAlign(ui.Center).FontSize(17).LineHeight(1.8).
				Margin(30, 0, 0, 0).TextColor(paint(colors.Text.Muted))
		})
		ui.Column(c).Grow(1).Children(func() {
			facts := [][2]string{
				{text.Version, info.DisplayVersion()},
				{text.Licenses, info.License},
				{text.Platform, info.Platform},
				{text.About.Plugins, p.aboutPluginsSummary(info)},
			}
			for _, row := range facts {
				ui.Box(c).FillWidth().Height(76).Children(func() {
					ui.Row(c).Fill().Padding(0, 19).AlignItems(ui.Center).Children(func() {
						ui.Text(c, row[0]).FontSize(21).TextColor(paint(colors.Text.Muted))
						ui.Spacer(c).Grow(1)
						ui.Text(c, row[1]).FontSize(20).TextColor(paint(colors.Text.Secondary))
					})
					ui.Box(c).Absolute().Left(0).Right(0).Bottom(0).Height(1).Background(d.subtleBorder)
				})
			}
			p.aboutActionRow(c, app.ShowLicenses, text.Licenses, func() {
				p.State.Push(app.Route{Page: app.Licenses, Section: "about"})
			})
			p.aboutActionRow(c, app.ShowPrivacyDetails, text.Privacy.DetailsTitle, func() {
				p.State.Push(app.Route{Page: app.PrivacyDetails, Section: "about"})
			})
			p.aboutActionRow(c, app.ShowProjectHome, text.About.ProjectHome, func() {
				p.State.Push(app.Route{Page: app.AboutProject, Section: "about"})
			})
		})
	})
}

func (p *PageContext) aboutActionRow(c *ui.Context, id app.FocusID, label string, action func()) {
	colors, d := p.Theme.Colors, p.uiColors()
	focused := p.paintFocus() == id
	background, ink := ui.Transparent, d.secondaryText
	borderW, borderColor := float32(0), ui.Transparent
	if focused {
		background, ink = paint(colors.Focus.Background), paint(colors.Focus.Foreground)
		if !p.Theme.Dark {
			borderW, borderColor = 1, d.focusBorder
		}
	}
	button := ui.Button(c.Key(string(id)), "").Label(label).
		FillWidth().Height(76).Radius(8).Border(borderW, borderColor).Padding(0).
		OnClick(func() { p.dispatch(action) })
	p.animateFocus(button, background, ink)
	p.bind(button, id, "content")
	button.Children(func() {
		ui.Row(c).Fill().Padding(0, 19).AlignItems(ui.Center).Children(func() {
			ui.Text(c, label).FontSize(21)
		})
		if !focused || p.Theme.Dark {
			ui.Box(c).Absolute().Left(0).Right(0).Bottom(0).Height(1).Background(d.subtleBorder)
		}
	})
}

func (v *View) projectQRCode(payload string) ui.ImageSource {
	if payload == "" {
		return nil
	}
	if payload == v.projectQRURL && v.projectQRImage != nil {
		return v.projectQRImage
	}
	code, err := qrcode.New(payload, qrcode.Medium)
	if err != nil {
		return nil
	}
	code.DisableBorder = true
	v.projectQRURL = payload
	v.projectQRImage = ui.NewBitmap(code.Image(220))
	return v.projectQRImage
}

type aboutProjectPage struct{}

func (aboutProjectPage) Render(c *ui.Context, p *PageContext) {
	info := p.resolvedAboutInfo()
	about, colors := p.Assets.Text.About, p.Theme.Colors
	p.region("about-project", nil)
	ui.Column(c).FillWidth().Lang(p.langTag()).Font(p.uiFont()).Children(func() {
		p.framelessHeader(c, "ABOUT \u00b7 PROJECT", "info", about.ProjectHome, about.ProjectBody)
		ui.Row(c).Margin(32, 0, 0, 0).Gap(34).AlignItems(ui.Center).Children(func() {
			if qr := p.host.projectQRCode(info.ProjectURL); qr != nil {
				ui.Box(c).Size(196, 196).Radius(18).Padding(10).Center().
					Background(ui.Color{R: 255, G: 255, B: 255, A: 255}).
					Shadow(0, 20, 56, 0, paint(colors.Overlay.Shadow)).Children(func() {
					ui.Box(c).Size(176, 176).Radius(10).Clip().Children(func() {
						ui.Image(c, qr).Size(176, 176).Label(about.ProjectHome)
					})
				})
			}
			ui.Column(c).Grow(1).MinWidth(0).Gap(12).Children(func() {
				ui.Text(c, info.ProjectURL).Font("monospace").FontSize(22).FontWeight(500).
					TextColor(paint(colors.Accent.Soft)).LineHeight(1.5)
				meta := "MikuTV " + info.DisplayVersion() + " \u00b7 " + info.License
				if info.Commit != "" {
					meta += " \u00b7 " + info.Commit
				}
				ui.Text(c, meta).FontSize(17).TextColor(paint(colors.Text.Muted)).LineHeight(1.6)
			})
		})
		ui.Row(c).Margin(38, 0, 0, 0).Gap(16).Children(func() {
			p.styledButton(c, p.Assets.Text.Close, "", app.CloseModal, "about-project", func() {
				p.State.Back()
			}, p.framelessButtonAppearance())
		})
	})
}
