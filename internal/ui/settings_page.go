package tvui

import (
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/theme"
)

func (v *View) selectSettingsSection(next app.FocusID) {
	section, ok := strings.CutPrefix(string(next), "section/")
	if !ok || section == "" {
		return
	}
	route := app.Route{Page: app.Settings, Section: section}
	if v.State.Route == route {
		return
	}
	if v.State.Navigate(route) {
		v.State.Focus, v.State.Area, v.reveal = next, app.ContentArea, true
	}
}

type settingsLayout struct{}

func (settingsLayout) Render(c *ui.Context, p *PageContext) {
	text := p.Assets.Text
	p.region("sections", focusEdges{app.Right: {Region: "content"}})
	sections := p.Pages.children(p.Node)
	colors, d := p.Theme.Colors, p.uiColors()
	ui.Column(c).FillWidth().Lang(p.langTag()).Font(p.uiFont()).Children(func() {
		ui.Box(c).FillWidth().Height(187).Children(func() {
			ui.Text(c, text.SettingsTitle).Absolute().Top(2).Left(0).FontSize(54).FontWeight(550).LetterSpacing(2)
			ui.Text(c, text.SettingsNote).Absolute().Top(94).Left(0).FontSize(22).LineHeight(1.7).
				TextColor(paint(colors.Text.Muted))
		})
		ui.Row(c).FillWidth().Gap(83).AlignItems(ui.Start).Children(func() {
			ui.Column(c).Width(265).Shrink(0).Gap(10).Children(func() {
				for _, section := range sections {
					id := section.Policy.Initial.Focus
					selected := p.Pages.contains(section.Policy.Route, p.Location.Route)
					background, foreground := ui.Transparent, paint(colors.Text.Secondary)
					borderW, borderColor := float32(0), ui.Transparent
					if selected {
						background, foreground = paint(colors.Selected.Background), paint(colors.Selected.Foreground)
						p.left = focusDestination{Focus: id}
					}
					focused := p.State.Focus == id
					if focused {
						if !p.Theme.Dark {
							background, foreground = paint(colors.Focus.Background), paint(colors.Focus.Foreground)
							borderW, borderColor = 1, d.focusBorder
						} else {
							background, foreground = d.subtleHoverFill, paint(colors.Text.Primary)
						}
					}
					e := ui.Button(c.Key(string(id)), "").FillWidth().Height(68).Padding(0, 19).Radius(13).
						Border(borderW, borderColor).Background(background).TextColor(foreground).Label(section.Title).
						OnClick(func() {
							p.dispatch(func() { p.State.Navigate(section.Policy.Route); p.State.Focus = id; p.State.Area = app.ContentArea })
						})
					p.animateFocus(e, background, foreground)
					e.Children(func() {
						ui.Row(c).Fill().Gap(16).AlignItems(ui.Center).Children(func() {
							ui.Icon(c, p.Assets.Icon(section.Icon)).Size(24, 24)
							ui.Text(c, section.Title).FontSize(23)
						})
					})
					p.bind(e, id, "sections")
				}
			})
			ui.Column(c.Key("settings-section/" + p.Location.Section)).Grow(1).MaxWidth(1145).Padding(8, 0, 0, 0).Children(func() { p.Outlet(c) })
		})
	})
}

type settingsEmptyPage struct{}

func (settingsEmptyPage) Render(c *ui.Context, p *PageContext) {
	ui.Text(c, p.Title).FontSize(32).FontWeight(550).LineHeight(1.4).Margin(0, 0, 13, 0)
	ui.Text(c, p.Assets.Text.EmptySettings).FontSize(19).TextColor(paint(p.Theme.Colors.Text.Muted))
}

func (p *PageContext) settingsSectionHeader(c *ui.Context, title, intro string) {
	colors, d := p.Theme.Colors, p.uiColors()
	ui.Text(c, title).Height(45).FontSize(32).FontWeight(550).LineHeight(1.4).Margin(0, 0, 13, 0)
	ui.Text(c, intro).Height(32).FontSize(19).LineHeight(1.7).TextColor(paint(colors.Text.Muted)).Margin(0, 0, 27, 0)
	ui.Box(c).FillWidth().Height(1).Background(d.subtleBorder)
}

type settingsRowOptions struct {
	id         app.FocusID
	region     string
	icon       string
	label      string
	note       string
	value      string
	accessName string
	role       ui.Role
	singleLine bool
	swatches   *[6]theme.Color
	trailing   func(ui.Color)
	action     func()
}

func (p *PageContext) settingsRow(c *ui.Context, opts settingsRowOptions) {
	colors, d := p.Theme.Colors, p.uiColors()
	region := opts.region
	if region == "" {
		region = "content"
	}
	focused := p.paintFocus() == opts.id
	fill, ink := ui.Transparent, paint(colors.Text.Primary)
	noteColor, iconColor, valColor := paint(colors.Text.Muted), paint(colors.Text.Secondary), paint(colors.Text.Secondary)
	borderW, borderColor := float32(0), ui.Transparent
	if focused {
		fill, ink = paint(colors.Focus.Background), paint(colors.Focus.Foreground)
		noteColor, iconColor, valColor = d.focusSubtext, d.focusSubtext, d.focusSubtext
		if !p.Theme.Dark {
			borderW, borderColor = 1, d.focusBorder
		}
	}
	accessLabel := opts.accessName
	if accessLabel == "" {
		accessLabel = opts.label
		if opts.value != "" {
			accessLabel += " " + opts.value
		}
	}
	control := ui.Button(c.Key(string(opts.id)), "").FillWidth().MinHeight(88).
		Radius(7).Border(borderW, borderColor).Padding(0).Label(accessLabel).
		OnClick(func() { p.dispatch(opts.action) })
	if opts.role != 0 {
		control.Role(opts.role)
	}
	p.animateFocus(control, fill, ink)
	p.bind(control, opts.id, region)
	control.Children(func() {
		ui.Row(c).Fill().Padding(14, 20).Gap(20).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(36, 36).Shrink(0).Center().Children(func() {
				ui.Icon(c, p.Assets.Icon(opts.icon)).Size(24, 24).TextColor(iconColor)
			})
			col := ui.Column(c).Grow(1).Gap(6)
			if opts.singleLine {
				col.MinWidth(0)
			}
			col.Children(func() {
				titleEl := ui.Text(c, opts.label).FontSize(23).FontWeight(450).LineHeight(1.4)
				if opts.singleLine {
					titleEl.SingleLine()
				}
				if opts.note != "" {
					noteEl := ui.Text(c, opts.note).FontSize(16).LineHeight(1.6).TextColor(noteColor)
					if opts.singleLine {
						noteEl.SingleLine()
					}
				}
			})
			if opts.swatches != nil {
				p.renderThemeSwatches(c, *opts.swatches, 76, 46, 8)
			}
			if opts.trailing != nil {
				opts.trailing(valColor)
			} else if opts.value != "" {
				ui.Text(c, opts.value).FontSize(21).Shrink(0).TextColor(valColor)
			}
		})
		if !focused || p.Theme.Dark {
			ui.Box(c).Absolute().Left(0).Right(0).Bottom(0).Height(1).Background(d.subtleBorder)
		}
	})
}

func (p *PageContext) settingsToggleRow(c *ui.Context, id app.FocusID, iconName, label, note string, enabled bool, action func()) {
	status := p.Assets.Text.Activity.Disabled
	if enabled {
		status = p.Assets.Text.Activity.Enabled
	}
	p.settingsRow(c, settingsRowOptions{
		id:         id,
		icon:       iconName,
		label:      label,
		note:       note,
		accessName: label + " " + status,
		role:       ui.RoleSwitch,
		trailing:   func(ui.Color) { p.preferenceSwitch(c, enabled) },
		action:     action,
	})
}

func (p *PageContext) settingsValueRow(c *ui.Context, id app.FocusID, iconName, label, note, value string, action func()) {
	p.settingsRow(c, settingsRowOptions{
		id:     id,
		icon:   iconName,
		label:  label,
		note:   note,
		value:  value,
		action: action,
	})
}

type settingsPickerChoice struct {
	id       app.FocusID
	label    string
	selected bool
	onSelect func()
}

func (p *PageContext) renderSettingsPickerDrawer(c *ui.Context, eyebrow, title string, choices []settingsPickerChoice) {
	p.region("modal", nil)
	ui.Column(c).Fill().Lang(p.langTag()).Font(p.uiFont()).Children(func() {
		p.drawerHeader(c, eyebrow, title, "", "")
		ui.Column(c).FillWidth().Grow(1).Children(func() {
			for _, choice := range choices {
				ch := choice
				p.drawerOptionRow(c, ch.id, "modal", ch.label, "", ch.selected, ch.onSelect)
			}
		})
		p.drawerFooter(c, p.Assets.Text.Modal.HintSelect, p.Assets.Text.Modal.HintApply)
	})
}
