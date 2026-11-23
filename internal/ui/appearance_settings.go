package tvui

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/plugin"
	"github.com/yuchanns/MikuTV/internal/theme"
)

const (
	appearanceThemeRow        app.FocusID = "settings/appearance/theme"
	appearanceHoverPreviewRow app.FocusID = "settings/appearance/hover-preview"
	appearanceReduceMotionRow app.FocusID = "settings/appearance/reduce-motion"

	themePickerDiscoverBtn app.FocusID = "appearance/theme-picker/discover"
	themeErrorReselectBtn  app.FocusID = "appearance/theme-error/reselect"
)

func themePickerOptionID(id string) app.FocusID {
	return app.FocusID("appearance/theme/" + strings.ToLower(strings.TrimSpace(id)))
}

type appearanceSettingsUI struct {
	failedThemeID   string
	failedThemeName string
}

func (v *View) appearanceSnapshot() app.AppearanceSnapshot {
	if v.Appearance != nil {
		return v.Appearance.Snapshot()
	}
	def := theme.Default()
	return app.AppearanceSnapshot{
		ActiveID:     def.ID,
		ActiveTheme:  v.Theme,
		HoverPreview: true,
		ReduceMotion: false,
		Options: []app.ThemeOption{{
			ID:       def.ID,
			Name:     def.Name,
			Bundled:  true,
			Active:   true,
			Swatches: def.Swatches(),
			Theme:    def,
		}},
	}
}

func (v *View) hoverPreviewEnabled() bool {
	if v.Appearance == nil {
		return true
	}
	return v.Appearance.HoverPreview()
}

func (v *View) reduceMotionEnabled(c *ui.Context) bool {
	if v.Appearance != nil && v.Appearance.ReduceMotion() {
		return true
	}
	return c != nil && c.Preferences().ReduceMotion
}

func (v *View) openThemePicker(focusThemeID string) {
	snap := v.appearanceSnapshot()
	target := focusThemeID
	if target == "" {
		target = snap.ActiveID
	}
	focus := themePickerOptionID(target)
	found := false
	for _, opt := range snap.Options {
		if strings.EqualFold(opt.ID, target) {
			focus = themePickerOptionID(opt.ID)
			found = true
			break
		}
	}
	if !found && len(snap.Options) > 0 {
		focus = themePickerOptionID(snap.Options[0].ID)
	}
	v.State.Push(app.Route{Page: app.ThemePicker, Section: "appearance"})
	v.State.Focus, v.reveal = focus, true
}

func (v *View) openThemeError(themeID, themeName string) {
	v.appearanceUI.failedThemeID = themeID
	v.appearanceUI.failedThemeName = themeName
	v.State.Push(app.Route{Page: app.ThemeError, Section: "appearance"})
	v.State.Focus, v.reveal = themeErrorReselectBtn, true
}

func (v *View) openDiscoverThemesFromPicker() {
	// Close ThemePicker modal, navigate to Settings/plugins Discover pre-filtered to ThemeKind.
	if v.State.Page == app.ThemePicker {
		v.State.Back()
	}
	s := &v.pluginsUI
	s.view = pluginsViewDiscover
	s.filter = plugin.ThemeKind
	s.query = ""
	s.input = ""
	s.editing = false
	s.selectedInstalledID = ""
	s.selectedCatalogRepo = ""
	s.returnToThemePicker = true
	if v.PluginCatalog != nil {
		v.PluginCatalog.EnsureLoaded()
	}
	v.State.Navigate(app.Route{Page: app.Settings, Section: "plugins"})
	v.State.Focus, v.reveal = pluginsFilterTheme, true
}

func (v *View) returnToAppearanceThemePicker(focusThemeID string) {
	v.pluginsUI.returnToThemePicker = false
	v.pluginsUI.selectedInstalledID = ""
	v.pluginsUI.selectedCatalogRepo = ""
	v.pluginsUI.editing = false
	v.State.Navigate(app.Route{Page: app.Settings, Section: "appearance"})
	v.State.Focus = appearanceThemeRow
	v.openThemePicker(focusThemeID)
}

func (v *View) selectThemeOption(opt app.ThemeOption) {
	if v.Appearance == nil {
		v.SetTheme(opt.Theme)
		v.State.Back()
		return
	}
	if _, err := v.Appearance.SelectTheme(opt.ID); err != nil {
		// Retain current active theme and open Scheme B ThemeError modal (§7.2).
		v.State.Back()
		v.openThemeError(opt.ID, opt.Name)
		return
	}
	v.State.Back()
	v.State.Focus, v.reveal = appearanceThemeRow, true
	v.showSettingsFeedback(fmt.Sprintf(v.Assets.Text.Appearance.ThemeAppliedToast, opt.Name))
}

type appearanceSettingsPage struct{}

func (appearanceSettingsPage) Render(c *ui.Context, p *PageContext) {
	v := p.host
	l, colors, d := p.Assets.Text.Appearance, p.Theme.Colors, p.uiColors()
	snap := v.appearanceSnapshot()
	p.region("content", nil)

	activeName := snap.ActiveTheme.Name
	if activeName == "" {
		activeName = theme.Default().Name
	}

	ui.Text(c, l.Title).Height(45).FontSize(32).FontWeight(550).LineHeight(1.4).Margin(0, 0, 13, 0)
	ui.Text(c, l.Intro).Height(32).FontSize(19).LineHeight(1.7).TextColor(paint(colors.Text.Muted)).Margin(0, 0, 27, 0)
	ui.Box(c).FillWidth().Height(1).Background(d.subtleBorder)

	p.danmakuValueRow(c, appearanceThemeRow, "palette", l.ThemeLabel, l.ThemeNote, activeName, func() {
		v.openThemePicker("")
	})
	p.danmakuToggleRow(c, appearanceHoverPreviewRow, "series", l.HoverPreviewLabel, l.HoverPreviewNote, snap.HoverPreview, func() {
		if v.Appearance != nil {
			if err := v.Appearance.SetHoverPreview(!snap.HoverPreview); err != nil {
				v.showSettingsFeedback(p.Assets.Text.SaveFailed)
			}
		}
	})
	p.danmakuToggleRow(c, appearanceReduceMotionRow, "brand", l.ReduceMotionLabel, l.ReduceMotionNote, snap.ReduceMotion, func() {
		if v.Appearance != nil {
			if err := v.Appearance.SetReduceMotion(!snap.ReduceMotion); err != nil {
				v.showSettingsFeedback(p.Assets.Text.SaveFailed)
			}
		}
	})
}

type themePickerPage struct{}

func (themePickerPage) Render(c *ui.Context, p *PageContext) {
	v := p.host
	l, colors, d := p.Assets.Text.Appearance, p.Theme.Colors, p.uiColors()
	snap := v.appearanceSnapshot()

	p.region("modal", nil)

	ui.Column(c).Fill().Lang("zh-CN").Font("system-ui, PingFang SC").Children(func() {
		p.drawerHeader(c, "APPEARANCE \u00b7 THEME", l.PickerTitle, "", l.PickerSubtitle)

		ui.Column(c).FillWidth().Grow(1).Children(func() {
			for _, option := range snap.Options {
				opt := option
				id := themePickerOptionID(opt.ID)
				sub := l.InstalledThemeNote
				if opt.Bundled {
					sub = l.BuiltinThemeNote
				}
				p.renderThemeOptionRow(c, id, "modal", opt, sub, func() {
					v.selectThemeOption(opt)
				})
			}

			if len(snap.Options) <= 1 {
				ui.Text(c, l.BuiltinOnlyHint).FillWidth().Margin(14, 4, 6, 4).
					FontSize(17).LineHeight(1.6).TextColor(paint(colors.Text.Muted))
			}

			ui.Box(c).Height(18)
			p.styledButton(c, l.DiscoverMoreThemes, "search", themePickerDiscoverBtn, "modal", func() {
				v.openDiscoverThemesFromPicker()
			}, buttonAppearance{
				height: 54, padding: 24, radius: 13, fontSize: 19, fontWeight: 500, iconSize: 20, iconGap: 10,
				focusShadow: true,
				background:  d.secondaryFill, foreground: paint(colors.Text.Primary), border: d.secondaryBorder,
			})
		})

		p.drawerFooter(c, p.Assets.Text.Modal.HintSelect, p.Assets.Text.Modal.HintApply)
	})
}

func (p *PageContext) renderThemeOptionRow(c *ui.Context, id app.FocusID, region string, opt app.ThemeOption, sub string, action func()) {
	colors, d := p.Theme.Colors, p.uiColors()
	focused := p.paintFocus() == id
	bg, fg, subFg := ui.Transparent, paint(colors.Text.Primary), paint(colors.Text.Muted)
	border := d.subtleBorder
	if opt.Active {
		bg = withAlpha(colors.Accent.Primary, 0x16)
		border = withAlpha(colors.Accent.Primary, 0x44)
	}
	if focused {
		bg, fg, subFg = paint(colors.Focus.Background), paint(colors.Focus.Foreground), d.focusSubtext
		border = ui.Transparent
	}
	btn := ui.Button(c.Key(string(id)), "").Label(opt.Name).FillWidth().Height(94).
		Margin(0, 0, 12, 0).Padding(0, 20).Radius(14).Border(1, border).
		Background(bg).TextColor(fg).
		OnClick(func() { p.dispatch(action) })
	p.addVideoFocusShadow(btn, focused)
	p.animateFocus(btn, bg, fg)
	p.bind(btn, id, region)
	btn.Children(func() {
		ui.Row(c).Fill().Gap(18).AlignItems(ui.Center).Children(func() {
			p.renderThemeSwatches(c, opt.Swatches, 84, 52, 9)
			ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
				ui.Text(c, opt.Name).FontSize(23).FontWeight(500).SingleLine()
				if sub != "" {
					ui.Text(c, sub).FontSize(16).SingleLine().TextColor(subFg)
				}
			})
			if opt.Active {
				checkColor := paint(colors.Accent.Soft)
				if focused {
					checkColor = paint(colors.Focus.Foreground)
				}
				ui.Icon(c, p.Assets.Icon("check")).Size(22, 22).Shrink(0).TextColor(checkColor)
			}
		})
	})
}

// renderThemeSwatches draws a 2x3 palette swatch grid matching the visual spec
// ([Canvas, Panel, CardStart] on top, [Accent.Primary, Accent.Soft, Text.Primary] on bottom).
func (p *PageContext) renderThemeSwatches(c *ui.Context, swatches [6]theme.Color, width, height, radius float32) {
	if swatches[0] == 0 && swatches[3] == 0 {
		return
	}
	d := p.uiColors()
	ui.Column(c).Size(width, height).Shrink(0).Radius(radius).Clip().Border(1, d.subtleBorder).Children(func() {
		ui.Row(c).FillWidth().Grow(1).Children(func() {
			ui.Box(c).Grow(1).FillHeight().Background(paint(swatches[0]))
			ui.Box(c).Grow(1).FillHeight().Background(paint(swatches[1]))
			ui.Box(c).Grow(1).FillHeight().Background(paint(swatches[2]))
		})
		ui.Row(c).FillWidth().Grow(1).Children(func() {
			ui.Box(c).Grow(1).FillHeight().Background(paint(swatches[3]))
			ui.Box(c).Grow(1).FillHeight().Background(paint(swatches[4]))
			ui.Box(c).Grow(1).FillHeight().Background(paint(swatches[5]))
		})
	})
}

type themeErrorPage struct{}

func (themeErrorPage) Render(c *ui.Context, p *PageContext) {
	v := p.host
	l := p.Assets.Text.Appearance

	p.region("theme-error-actions", nil)

	ui.Column(c).FillWidth().Lang("zh-CN").Font("system-ui, PingFang SC").Children(func() {
		p.framelessHeader(c, "APPEARANCE \u00b7 ERROR", "palette", l.ErrorTitle, l.ErrorBody)
		ui.Row(c.Key("theme-error-actions")).Margin(36, 0, 0, 0).Gap(16).Children(func() {
			p.styledButton(c, l.ErrorReselect, "", themeErrorReselectBtn, "theme-error-actions", func() {
				p.State.Back()
				v.openThemePicker("")
			}, p.framelessButtonAppearance())
			p.styledButton(c, l.ErrorReturn, "", app.CloseModal, "theme-error-actions", func() {
				p.State.Back()
			}, p.framelessButtonAppearance())
		})
	})
}
