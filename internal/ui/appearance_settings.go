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
	appearanceLanguageRow     app.FocusID = "settings/appearance/language"
	appearanceModeRow         app.FocusID = "settings/appearance/mode"
	appearanceLightThemeRow   app.FocusID = "settings/appearance/light-theme"
	appearanceDarkThemeRow    app.FocusID = "settings/appearance/dark-theme"
	appearanceHoverPreviewRow app.FocusID = "settings/appearance/hover-preview"
	appearanceReduceMotionRow app.FocusID = "settings/appearance/reduce-motion"

	languagePickerDiscoverBtn app.FocusID = "appearance/language-picker/discover"
	themePickerDiscoverBtn    app.FocusID = "appearance/theme-picker/discover"
	themeErrorReselectBtn     app.FocusID = "appearance/theme-error/reselect"
)

func languagePickerOptionID(id string) app.FocusID {
	return app.FocusID("appearance/language/" + strings.ToLower(strings.TrimSpace(id)))
}

func appearanceModeOptionID(mode app.AppearanceMode) app.FocusID {
	return app.FocusID("appearance/mode/" + string(mode))
}

func themePickerOptionID(id string) app.FocusID {
	return app.FocusID("appearance/theme/" + strings.ToLower(strings.TrimSpace(id)))
}

type appearanceSettingsUI struct {
	pickerSlot      app.ThemeSlot
	pickerScroll    ui.ScrollState
	languageScroll  ui.ScrollState
	failedThemeID   string
	failedThemeName string
}

func (v *View) appearanceSnapshot() app.AppearanceSnapshot {
	if v.Appearance != nil {
		return v.Appearance.Snapshot()
	}
	darkDef := theme.Default()
	lightDef := theme.DefaultLight()
	return app.AppearanceSnapshot{
		ActiveID:     darkDef.ID,
		ActiveTheme:  v.Theme,
		ActiveSlot:   app.ThemeSlotDark,
		Mode:         app.AppearanceModeDark,
		SystemDark:   true,
		LightID:      lightDef.ID,
		LightName:    lightDef.Name,
		DarkID:       darkDef.ID,
		DarkName:     darkDef.Name,
		HoverPreview: true,
		ReduceMotion: false,
		Options: []app.ThemeOption{{
			ID:       darkDef.ID,
			Name:     darkDef.Name,
			Dark:     true,
			Bundled:  true,
			Active:   true,
			Swatches: darkDef.Swatches(),
			Theme:    darkDef,
		}, {
			ID:       lightDef.ID,
			Name:     lightDef.Name,
			Dark:     false,
			Bundled:  true,
			Active:   false,
			Swatches: lightDef.Swatches(),
			Theme:    lightDef,
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

func appearanceRowForSlot(slot app.ThemeSlot) app.FocusID {
	if slot == app.ThemeSlotLight {
		return appearanceLightThemeRow
	}
	return appearanceDarkThemeRow
}

func selectedIDForSlot(snap app.AppearanceSnapshot, slot app.ThemeSlot) string {
	if slot == app.ThemeSlotLight {
		return snap.LightID
	}
	return snap.DarkID
}

func (p *PageContext) appearanceModeLabel(mode app.AppearanceMode) string {
	l := p.Assets.Text.Appearance
	switch mode {
	case app.AppearanceModeSystem:
		return l.ModeSystem
	case app.AppearanceModeLight:
		return l.ModeLight
	default:
		return l.ModeDark
	}
}

func (p *PageContext) appearanceModeNote(mode app.AppearanceMode) string {
	l := p.Assets.Text.Appearance
	switch mode {
	case app.AppearanceModeSystem:
		return l.ModeSystemNote
	case app.AppearanceModeLight:
		return l.ModeLightNote
	default:
		return l.ModeDarkNote
	}
}

func (v *View) localeSnapshot() app.LocaleSnapshot {
	if v.Locale != nil {
		return v.Locale.Snapshot()
	}
	activeID, activeName := app.LocaleBuiltinZH, "简体中文"
	if v.Assets != nil && strings.HasPrefix(strings.ToLower(v.Assets.LocaleTag), "en") {
		activeID, activeName = app.LocaleBuiltinEN, "English"
	}
	return app.LocaleSnapshot{
		Selection:  app.LocaleModeSystem,
		ActiveID:   activeID,
		ActiveTag:  activeID,
		ActiveName: activeName,
		Options: []app.LocaleOption{
			{ID: app.LocaleBuiltinZH, Name: "简体中文", Language: app.LocaleBuiltinZH, Bundled: true, Active: activeID == app.LocaleBuiltinZH},
			{ID: app.LocaleBuiltinEN, Name: "English", Language: app.LocaleBuiltinEN, Bundled: true, Active: activeID == app.LocaleBuiltinEN},
		},
	}
}

func (v *View) openLanguagePicker(focusID string) {
	snap := v.localeSnapshot()
	v.appearanceUI.languageScroll = ui.ScrollState{}
	target := strings.TrimSpace(focusID)
	if target == "" {
		target = snap.Selection
	}
	if target == "" {
		target = app.LocaleModeSystem
	}
	v.State.Push(app.Route{Page: app.LanguagePicker, Section: "appearance"})
	v.State.Focus, v.reveal = languagePickerOptionID(target), true
}

func (v *View) openDiscoverLanguagesFromPicker() {
	if v.State.Page == app.LanguagePicker {
		v.State.Back()
	}
	s := &v.pluginsUI
	s.view = pluginsViewDiscover
	s.filter = plugin.LocaleKind
	s.query = ""
	s.input = ""
	s.editing = false
	s.selectedInstalledID = ""
	s.selectedCatalogRepo = ""
	s.returnToThemePicker = false
	s.returnToLanguagePicker = true
	if v.PluginCatalog != nil {
		v.PluginCatalog.EnsureLoaded()
	}
	v.State.Navigate(app.Route{Page: app.Settings, Section: "plugins"})
	v.State.Focus, v.reveal = pluginsFilterLocale, true
}

func (v *View) returnToAppearanceLanguagePicker(focusID string) {
	v.pluginsUI.returnToLanguagePicker = false
	v.pluginsUI.selectedInstalledID = ""
	v.pluginsUI.selectedCatalogRepo = ""
	v.pluginsUI.editing = false
	v.State.Navigate(app.Route{Page: app.Settings, Section: "appearance"})
	v.State.Focus = appearanceLanguageRow
	v.openLanguagePicker(focusID)
}

func (v *View) selectLanguageOption(id, displayName string) {
	if v.Locale != nil {
		if err := v.Locale.Select(id); err != nil {
			v.showSettingsFeedback(v.Assets.Text.SaveFailed)
			v.State.Back()
			return
		}
	} else if v.Assets != nil {
		targetTag := id
		if targetTag == app.LocaleModeSystem {
			targetTag = app.LocaleBuiltinZH
		}
		_ = v.SetLocale(targetTag, nil)
	}
	v.State.Back()
	v.State.Focus, v.reveal = appearanceLanguageRow, true
	l := v.Assets.Text.Appearance
	toastName := displayName
	if strings.EqualFold(id, app.LocaleModeSystem) {
		snap := v.localeSnapshot()
		toastName = l.LanguageSystem + " \u00b7 " + snap.ActiveName
	}
	v.showSettingsFeedback(fmt.Sprintf(l.LanguageAppliedToast, toastName))
}

func (v *View) openAppearanceModePicker() {
	snap := v.appearanceSnapshot()
	v.State.Push(app.Route{Page: app.AppearanceModePicker, Section: "appearance"})
	v.State.Focus, v.reveal = appearanceModeOptionID(snap.Mode), true
}

func (v *View) openThemePicker(focusThemeID string) {
	snap := v.appearanceSnapshot()
	slot := snap.ActiveSlot
	if focusThemeID != "" {
		for _, opt := range snap.Options {
			if strings.EqualFold(opt.ID, focusThemeID) {
				if opt.Dark {
					slot = app.ThemeSlotDark
				} else {
					slot = app.ThemeSlotLight
				}
				break
			}
		}
	}
	v.openThemePickerForSlot(slot, focusThemeID)
}

func (v *View) openThemePickerForSlot(slot app.ThemeSlot, focusThemeID string) {
	snap := v.appearanceSnapshot()
	v.appearanceUI.pickerSlot = slot
	v.appearanceUI.pickerScroll = ui.ScrollState{}
	target := focusThemeID
	if target == "" {
		target = selectedIDForSlot(snap, slot)
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
	fromPicker := v.pluginsUI.returnToThemePicker
	v.pluginsUI.returnToThemePicker = false
	v.pluginsUI.selectedInstalledID = ""
	v.pluginsUI.selectedCatalogRepo = ""
	v.pluginsUI.editing = false
	snap := v.appearanceSnapshot()
	slot := v.appearanceUI.pickerSlot
	if !fromPicker {
		slot = snap.ActiveSlot
		for _, opt := range snap.Options {
			if strings.EqualFold(opt.ID, focusThemeID) {
				if opt.Dark {
					slot = app.ThemeSlotDark
				} else {
					slot = app.ThemeSlotLight
				}
				break
			}
		}
	}
	v.State.Navigate(app.Route{Page: app.Settings, Section: "appearance"})
	v.State.Focus = appearanceRowForSlot(slot)
	v.openThemePickerForSlot(slot, focusThemeID)
}

func (v *View) selectThemeOption(opt app.ThemeOption) {
	slot := v.appearanceUI.pickerSlot
	l := v.Assets.Text.Appearance
	if v.Appearance == nil {
		v.SetTheme(opt.Theme)
		v.State.Back()
		return
	}
	if _, err := v.Appearance.SelectThemeForSlot(slot, opt.ID); err != nil {
		// Retain current active theme and open Scheme B ThemeError modal (§7.2).
		v.State.Back()
		v.openThemeError(opt.ID, opt.Name)
		return
	}
	snap := v.appearanceSnapshot()
	v.State.Back()
	v.State.Focus, v.reveal = appearanceRowForSlot(slot), true
	if slot == snap.ActiveSlot {
		v.showSettingsFeedback(fmt.Sprintf(l.ThemeAppliedToast, opt.Name))
	} else if slot == app.ThemeSlotLight {
		v.showSettingsFeedback(fmt.Sprintf(l.LightThemeSavedToast, opt.Name))
	} else {
		v.showSettingsFeedback(fmt.Sprintf(l.DarkThemeSavedToast, opt.Name))
	}
}

type appearanceSettingsPage struct{}

func (appearanceSettingsPage) Render(c *ui.Context, p *PageContext) {
	v := p.host
	l, colors, d := p.Assets.Text.Appearance, p.Theme.Colors, p.uiColors()
	snap := v.appearanceSnapshot()
	localeSnap := v.localeSnapshot()
	p.region("content", nil)

	langNote := l.LanguageCustomNote
	langValue := localeSnap.ActiveName
	if strings.EqualFold(localeSnap.Selection, app.LocaleModeSystem) || localeSnap.Selection == "" {
		langNote = l.LanguageSystemNote
		if localeSnap.ActiveName != "" {
			langValue = l.LanguageSystem + " \u00b7 " + localeSnap.ActiveName
		} else {
			langValue = l.LanguageSystem
		}
	}

	lightName := snap.LightName
	if lightName == "" {
		lightName = theme.DefaultLight().Name
	}
	darkName := snap.DarkName
	if darkName == "" {
		darkName = theme.Default().Name
	}

	lightNote := l.LightThemeNote
	darkNote := l.DarkThemeNote
	if snap.ActiveSlot == app.ThemeSlotLight {
		lightNote = l.LightThemeActiveNote
	} else {
		darkNote = l.DarkThemeActiveNote
	}

	ui.Text(c, l.Title).Height(45).FontSize(32).FontWeight(550).LineHeight(1.4).Margin(0, 0, 13, 0)
	ui.Text(c, l.Intro).Height(32).FontSize(19).LineHeight(1.7).TextColor(paint(colors.Text.Muted)).Margin(0, 0, 27, 0)
	ui.Box(c).FillWidth().Height(1).Background(d.subtleBorder)

	p.danmakuValueRow(c, appearanceLanguageRow, "globe", l.LanguageLabel, langNote, langValue, func() {
		v.openLanguagePicker("")
	})
	p.danmakuValueRow(c, appearanceModeRow, "sparkles", l.ModeLabel, p.appearanceModeNote(snap.Mode), p.appearanceModeLabel(snap.Mode), func() {
		v.openAppearanceModePicker()
	})
	p.danmakuValueRow(c, appearanceLightThemeRow, "sun", l.LightThemeLabel, lightNote, lightName, func() {
		v.openThemePickerForSlot(app.ThemeSlotLight, "")
	})
	p.danmakuValueRow(c, appearanceDarkThemeRow, "moon", l.DarkThemeLabel, darkNote, darkName, func() {
		v.openThemePickerForSlot(app.ThemeSlotDark, "")
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

type languagePickerPage struct{}

func (languagePickerPage) Render(c *ui.Context, p *PageContext) {
	v := p.host
	l, colors, d := p.Assets.Text.Appearance, p.Theme.Colors, p.uiColors()
	snap := v.localeSnapshot()
	p.region("modal", nil)

	ui.Column(c).Fill().Lang(p.langTag()).Font(p.uiFont()).Children(func() {
		p.drawerHeader(c, "APPEARANCE \u00b7 LANGUAGE", l.LanguagePickerTitle, "", l.LanguagePickerSubtitle)

		ui.Scroll(c.Key("language-picker-scroll")).FillWidth().Grow(1).MinHeight(0).
			TrackScroll(&v.appearanceUI.languageScroll).Padding(4, 6, 12, 6).Children(func() {
			ui.Column(c).FillWidth().Children(func() {
				systemSelected := strings.EqualFold(snap.Selection, app.LocaleModeSystem) || snap.Selection == ""
				systemMeta := ""
				if snap.ActiveName != "" && systemSelected {
					systemMeta = snap.ActiveName
				}
				p.drawerOptionRow(c, languagePickerOptionID(app.LocaleModeSystem), "modal", l.LanguageSystem, systemMeta, systemSelected, func() {
					v.selectLanguageOption(app.LocaleModeSystem, l.LanguageSystem)
				})

				for _, option := range snap.Options {
					opt := option
					id := languagePickerOptionID(opt.ID)
					selected := !systemSelected && strings.EqualFold(snap.Selection, opt.ID)
					meta := l.LanguageBuiltinNote
					if !opt.Bundled {
						meta = l.LanguagePluginNote
						if opt.Language != "" {
							meta = opt.Language + " \u00b7 " + l.LanguagePluginNote
						}
					}
					p.drawerOptionRow(c, id, "modal", opt.Name, meta, selected, func() {
						v.selectLanguageOption(opt.ID, opt.Name)
					})
				}

				if len(snap.Options) <= 2 {
					ui.Text(c, l.LanguageBuiltinOnlyHint).FillWidth().Margin(14, 4, 6, 4).
						FontSize(17).LineHeight(1.6).TextColor(paint(colors.Text.Muted))
				}

				ui.Box(c).Height(18)
				p.styledButton(c, l.DiscoverMoreLanguages, "search", languagePickerDiscoverBtn, "modal", func() {
					v.openDiscoverLanguagesFromPicker()
				}, buttonAppearance{
					height: 54, padding: 24, radius: 13, fontSize: 19, fontWeight: 500, iconSize: 20, iconGap: 10,
					focusShadow: true,
					background:  d.secondaryFill, foreground: paint(colors.Text.Primary), border: d.secondaryBorder,
				})
			})
		})

		p.drawerFooter(c, p.Assets.Text.Modal.HintSelect, p.Assets.Text.Modal.HintApply)
	})
}

type appearanceModePickerPage struct{}

func (appearanceModePickerPage) Render(c *ui.Context, p *PageContext) {
	v := p.host
	l := p.Assets.Text.Appearance
	snap := v.appearanceSnapshot()
	p.region("modal", nil)

	choices := [...]struct {
		mode  app.AppearanceMode
		label string
		note  string
	}{
		{app.AppearanceModeSystem, l.ModeSystem, l.ModeSystemNote},
		{app.AppearanceModeLight, l.ModeLight, l.ModeLightNote},
		{app.AppearanceModeDark, l.ModeDark, l.ModeDarkNote},
	}

	ui.Column(c).Fill().Lang(p.langTag()).Font(p.uiFont()).Children(func() {
		p.drawerHeader(c, "APPEARANCE \u00b7 MODE", l.ModePickerTitle, "", l.ModePickerSubtitle)
		ui.Column(c).FillWidth().Grow(1).Children(func() {
			for _, choice := range choices {
				ch := choice
				p.drawerOptionRow(c, appearanceModeOptionID(ch.mode), "modal", ch.label, "", snap.Mode == ch.mode, func() {
					if v.Appearance != nil {
						if err := v.Appearance.SetMode(ch.mode); err != nil {
							v.showSettingsFeedback(p.Assets.Text.SaveFailed)
							p.State.Back()
							return
						}
					}
					p.State.Back()
					p.State.Focus, v.reveal = appearanceModeRow, true
					v.showSettingsFeedback(fmt.Sprintf(l.ModeAppliedToast, ch.label))
				})
			}
		})
		p.drawerFooter(c, p.Assets.Text.Modal.HintSelect, p.Assets.Text.Modal.HintApply)
	})
}

type themePickerPage struct{}

func (themePickerPage) Render(c *ui.Context, p *PageContext) {
	v := p.host
	l, colors, d := p.Assets.Text.Appearance, p.Theme.Colors, p.uiColors()
	snap := v.appearanceSnapshot()
	slot := v.appearanceUI.pickerSlot
	selectedID := selectedIDForSlot(snap, slot)

	eyebrow, title, subtitle := "APPEARANCE \u00b7 NIGHT THEME", l.DarkPickerTitle, l.DarkPickerSubtitle
	if slot == app.ThemeSlotLight {
		eyebrow, title, subtitle = "APPEARANCE \u00b7 DAY THEME", l.LightPickerTitle, l.LightPickerSubtitle
	}

	p.region("modal", nil)

	ui.Column(c).Fill().Lang(p.langTag()).Font(p.uiFont()).Children(func() {
		p.drawerHeader(c, eyebrow, title, "", subtitle)

		ui.Scroll(c.Key("theme-picker-scroll")).FillWidth().Grow(1).MinHeight(0).
			TrackScroll(&v.appearanceUI.pickerScroll).Padding(4, 6, 12, 6).Children(func() {
			ui.Column(c).FillWidth().Children(func() {
				for _, option := range snap.Options {
					opt := option
					id := themePickerOptionID(opt.ID)
					selected := strings.EqualFold(selectedID, opt.ID)
					sub := l.InstalledDarkThemeNote
					if opt.Bundled {
						if opt.Dark {
							sub = l.BuiltinDarkThemeNote
						} else {
							sub = l.BuiltinLightThemeNote
						}
					} else if !opt.Dark {
						sub = l.InstalledLightThemeNote
					}
					p.renderThemeOptionRow(c, id, "modal", opt, sub, selected, func() {
						v.selectThemeOption(opt)
					})
				}

				if len(snap.Options) <= 2 {
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
		})

		p.drawerFooter(c, p.Assets.Text.Modal.HintSelect, p.Assets.Text.Modal.HintApply)
	})
}

func (p *PageContext) renderThemeOptionRow(c *ui.Context, id app.FocusID, region string, opt app.ThemeOption, sub string, selected bool, action func()) {
	colors, d := p.Theme.Colors, p.uiColors()
	focused := p.paintFocus() == id
	bg, fg, subFg := ui.Transparent, paint(colors.Text.Primary), paint(colors.Text.Muted)
	border := d.subtleBorder
	if selected {
		bg = withAlpha(colors.Accent.Primary, 0x16)
		border = withAlpha(colors.Accent.Primary, 0x44)
	}
	if focused {
		bg, fg, subFg = paint(colors.Focus.Background), paint(colors.Focus.Foreground), d.focusSubtext
		if !p.Theme.Dark {
			border = d.focusBorder
		} else {
			border = ui.Transparent
		}
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
			if selected {
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

	ui.Column(c).FillWidth().Lang(p.langTag()).Font(p.uiFont()).Children(func() {
		p.framelessHeader(c, "APPEARANCE \u00b7 ERROR", "palette", l.ErrorTitle, l.ErrorBody)
		ui.Row(c.Key("theme-error-actions")).Margin(36, 0, 0, 0).Gap(16).Children(func() {
			p.styledButton(c, l.ErrorReselect, "", themeErrorReselectBtn, "theme-error-actions", func() {
				p.State.Back()
				v.openThemePickerForSlot(v.appearanceUI.pickerSlot, "")
			}, p.framelessButtonAppearance())
			p.styledButton(c, l.ErrorReturn, "", app.CloseModal, "theme-error-actions", func() {
				p.State.Back()
			}, p.framelessButtonAppearance())
		})
	})
}
