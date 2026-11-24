package tvui

import (
	"errors"
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/provider"
)

type danmakuSettingKind string

const (
	danmakuSettingDensity danmakuSettingKind = "density"
	danmakuSettingArea    danmakuSettingKind = "area"
	danmakuSettingOpacity danmakuSettingKind = "opacity"
	danmakuSettingSize    danmakuSettingKind = "size"
)

type danmakuRuleFilter string

const (
	danmakuFilterAll     danmakuRuleFilter = "all"
	danmakuFilterKeyword danmakuRuleFilter = "keyword"
	danmakuFilterUser    danmakuRuleFilter = "user"
)

const (
	danmakuPrefShow      app.FocusID = "settings/danmaku/show"
	danmakuPrefDensity   app.FocusID = "settings/danmaku/density"
	danmakuPrefArea      app.FocusID = "settings/danmaku/area"
	danmakuPrefOpacity   app.FocusID = "settings/danmaku/opacity"
	danmakuPrefSize      app.FocusID = "settings/danmaku/size"
	danmakuPrefBlocklist app.FocusID = "settings/danmaku/blocklist"

	danmakuRulesBack       app.FocusID = "settings/danmaku/rules-back"
	danmakuRulesSyncRetry  app.FocusID = "settings/danmaku/rules-sync-retry"
	danmakuRulesAdd        app.FocusID = "settings/danmaku/rule-add"
	danmakuFilterAllID     app.FocusID = "settings/danmaku/filter/all"
	danmakuFilterKeywordID app.FocusID = "settings/danmaku/filter/keyword"
	danmakuFilterUserID    app.FocusID = "settings/danmaku/filter/user"

	danmakuEditorKindKeyword app.FocusID = "danmaku-rule/kind/keyword"
	danmakuEditorKindUser    app.FocusID = "danmaku-rule/kind/user"
	danmakuEditorField       app.FocusID = "danmaku-rule/field"
	danmakuEditorInput       app.FocusID = "danmaku-rule/input"
	danmakuEditorSave        app.FocusID = "danmaku-rule/save"
	danmakuEditorCancel      app.FocusID = "danmaku-rule/cancel"
)

func danmakuPickerOptionID(index int) app.FocusID {
	return app.FocusID(fmt.Sprintf("settings/danmaku/choice/%d", index))
}

func danmakuRuleToggleID(id string) app.FocusID {
	return app.FocusID("settings/danmaku/rule-toggle/" + id)
}

func danmakuRuleRemoveID(id string) app.FocusID {
	return app.FocusID("settings/danmaku/rule-remove/" + id)
}

type danmakuRuleDraft struct {
	kind    provider.DanmakuRuleKind
	body    string
	input   string
	editing bool
	failure string
}

type danmakuSettingsUI struct {
	rulesOpen  bool
	filter     danmakuRuleFilter
	pickerKind danmakuSettingKind
	draft      danmakuRuleDraft
}

func (v *View) danmakuDisplay() app.DanmakuDisplay {
	if v.DanmakuRules == nil {
		return app.DefaultDanmakuDisplay()
	}
	return v.DanmakuRules.Display()
}

func (v *View) hasDanmakuSettingsCapability() bool {
	entry, ok := v.State.Current()
	if !ok {
		return false
	}
	return entry.Capabilities.Has(provider.ReadDanmaku) || entry.Capabilities.Has(provider.LiveChat)
}

func (v *View) showSettingsFeedback(message string) {
	v.settingsFeedback = videoDetailFeedback{text: message}
}

func (v *View) backFromDanmakuSettings() bool {
	if v.danmakuUI.rulesOpen {
		v.danmakuUI.rulesOpen = false
		v.State.Focus, v.reveal = danmakuPrefBlocklist, true
		return true
	}
	return false
}

func syncDanmakuSettings(v *View) {
	if v.DanmakuRules != nil && v.hasDanmakuSettingsCapability() {
		v.DanmakuRules.EnsureSynced()
	}
}

type danmakuChoice struct {
	label    string
	selected bool
	apply    func(*app.DanmakuDisplay)
}

func (v *View) danmakuChoices(kind danmakuSettingKind) (string, []danmakuChoice) {
	l := v.Assets.Text.Danmaku
	cur := v.danmakuDisplay()
	switch kind {
	case danmakuSettingDensity:
		return l.Density, []danmakuChoice{
			{label: "30%", selected: cur.Density <= 45, apply: func(d *app.DanmakuDisplay) { d.Density = 30 }},
			{label: "60%", selected: cur.Density > 45 && cur.Density < 85, apply: func(d *app.DanmakuDisplay) { d.Density = 60 }},
			{label: "100%", selected: cur.Density >= 85, apply: func(d *app.DanmakuDisplay) { d.Density = 100 }},
		}
	case danmakuSettingArea:
		return l.Area, []danmakuChoice{
			{label: l.AreaUpper, selected: cur.Area == app.DanmakuAreaUpper, apply: func(d *app.DanmakuDisplay) { d.Area = app.DanmakuAreaUpper }},
			{label: l.AreaFull, selected: cur.Area == app.DanmakuAreaFull, apply: func(d *app.DanmakuDisplay) { d.Area = app.DanmakuAreaFull }},
			{label: l.AreaTop, selected: cur.Area == app.DanmakuAreaTop, apply: func(d *app.DanmakuDisplay) { d.Area = app.DanmakuAreaTop }},
		}
	case danmakuSettingOpacity:
		return l.Opacity, []danmakuChoice{
			{label: "50%", selected: cur.Opacity <= 62, apply: func(d *app.DanmakuDisplay) { d.Opacity = 50 }},
			{label: "75%", selected: cur.Opacity > 62 && cur.Opacity < 88, apply: func(d *app.DanmakuDisplay) { d.Opacity = 75 }},
			{label: "100%", selected: cur.Opacity >= 88, apply: func(d *app.DanmakuDisplay) { d.Opacity = 100 }},
		}
	case danmakuSettingSize:
		return l.FontSize, []danmakuChoice{
			{label: l.SizeNormal, selected: cur.FontSize == app.DanmakuFontNormal, apply: func(d *app.DanmakuDisplay) { d.FontSize = app.DanmakuFontNormal }},
			{label: l.SizeLarge, selected: cur.FontSize == app.DanmakuFontLarge, apply: func(d *app.DanmakuDisplay) { d.FontSize = app.DanmakuFontLarge }},
		}
	default:
		return "", nil
	}
}

func (v *View) danmakuPickerInitialFocus() app.FocusID {
	_, choices := v.danmakuChoices(v.danmakuUI.pickerKind)
	for i, ch := range choices {
		if ch.selected {
			return danmakuPickerOptionID(i)
		}
	}
	return danmakuPickerOptionID(0)
}

func (p *PageContext) danmakuAreaLabel(area app.DanmakuArea) string {
	l := p.Assets.Text.Danmaku
	switch area {
	case app.DanmakuAreaFull:
		return l.AreaFull
	case app.DanmakuAreaTop:
		return l.AreaTop
	default:
		return l.AreaUpper
	}
}

func (p *PageContext) danmakuSizeLabel(size app.DanmakuFontSize) string {
	l := p.Assets.Text.Danmaku
	if size == app.DanmakuFontLarge {
		return l.SizeLarge
	}
	return l.SizeNormal
}

type danmakuSettingsPage struct{}

func (danmakuSettingsPage) Render(c *ui.Context, p *PageContext) {
	v := p.host
	if !v.hasDanmakuSettingsCapability() {
		settingsEmptyPage{}.Render(c, p)
		return
	}
	if v.DanmakuRules != nil {
		v.DanmakuRules.EnsureSynced()
	}
	if v.danmakuUI.rulesOpen {
		p.renderDanmakuRules(c)
		return
	}
	p.renderDanmakuPreferences(c)
}

func (p *PageContext) renderDanmakuPreferences(c *ui.Context) {
	v := p.host
	l := p.Assets.Text.Danmaku
	display := v.danmakuDisplay()
	p.region("content", nil)

	p.settingsSectionHeader(c, l.Title, l.Intro)

	p.settingsToggleRow(c, danmakuPrefShow, "comment", l.DefaultShow, "", display.Show, func() {
		if v.DanmakuRules != nil {
			if err := v.DanmakuRules.UpdateDisplay(func(cfg *app.DanmakuDisplay) { cfg.Show = !cfg.Show }); err != nil {
				v.notice = p.Assets.Text.SaveFailed
			}
		}
	})
	p.settingsValueRow(c, danmakuPrefDensity, "comment", l.Density, "", fmt.Sprintf("%d%%", display.Density), func() {
		v.danmakuUI.pickerKind = danmakuSettingDensity
		p.State.Push(app.Route{Page: app.DanmakuSettingPicker, Section: "danmaku"})
		p.State.Focus, v.reveal = v.danmakuPickerInitialFocus(), true
	})
	p.settingsValueRow(c, danmakuPrefArea, "following", l.Area, "", p.danmakuAreaLabel(display.Area), func() {
		v.danmakuUI.pickerKind = danmakuSettingArea
		p.State.Push(app.Route{Page: app.DanmakuSettingPicker, Section: "danmaku"})
		p.State.Focus, v.reveal = v.danmakuPickerInitialFocus(), true
	})
	p.settingsValueRow(c, danmakuPrefOpacity, "comment", l.Opacity, "", fmt.Sprintf("%d%%", display.Opacity), func() {
		v.danmakuUI.pickerKind = danmakuSettingOpacity
		p.State.Push(app.Route{Page: app.DanmakuSettingPicker, Section: "danmaku"})
		p.State.Focus, v.reveal = v.danmakuPickerInitialFocus(), true
	})
	p.settingsValueRow(c, danmakuPrefSize, "captions", l.FontSize, "", p.danmakuSizeLabel(display.FontSize), func() {
		v.danmakuUI.pickerKind = danmakuSettingSize
		p.State.Push(app.Route{Page: app.DanmakuSettingPicker, Section: "danmaku"})
		p.State.Focus, v.reveal = v.danmakuPickerInitialFocus(), true
	})
	p.settingsValueRow(c, danmakuPrefBlocklist, "lock", l.Blocklist, l.BlocklistNote, "", func() {
		v.danmakuUI.rulesOpen = true
		if v.danmakuUI.filter == "" {
			v.danmakuUI.filter = danmakuFilterAll
		}
		if v.DanmakuRules != nil {
			v.DanmakuRules.EnsureSynced()
		}
		p.State.Focus, v.reveal = danmakuRulesAdd, true
	})
}

func (p *PageContext) renderDanmakuRules(c *ui.Context) {
	v := p.host
	l, colors, d := p.Assets.Text.Danmaku, p.Theme.Colors, p.uiColors()
	snap := app.DanmakuRulesSnapshot{}
	if v.DanmakuRules != nil {
		snap = v.DanmakuRules.Snapshot()
	}
	if v.danmakuUI.filter == "" {
		v.danmakuUI.filter = danmakuFilterAll
	}
	visible := make([]provider.DanmakuRule, 0, len(snap.Rules))
	for _, rule := range snap.Rules {
		switch v.danmakuUI.filter {
		case danmakuFilterKeyword:
			if rule.Kind != provider.DanmakuRuleKeyword {
				continue
			}
		case danmakuFilterUser:
			if rule.Kind != provider.DanmakuRuleUser {
				continue
			}
		}
		visible = append(visible, rule)
	}

	selectedFilterID := danmakuFilterAllID
	switch v.danmakuUI.filter {
	case danmakuFilterKeyword:
		selectedFilterID = danmakuFilterKeywordID
	case danmakuFilterUser:
		selectedFilterID = danmakuFilterUserID
	}
	listEdges := focusEdges{
		app.Up:   {Focus: selectedFilterID},
		app.Left: p.left,
	}
	tabEdges := focusEdges{
		app.Up:   {Focus: danmakuRulesAdd},
		app.Left: p.left,
	}
	if len(visible) > 0 {
		tabEdges[app.Down] = focusDestination{Focus: danmakuRuleToggleID(visible[0].ID)}
	}
	p.region("rules-top", focusEdges{
		app.Left: p.left,
		app.Down: {Focus: danmakuRulesAdd},
	})
	p.region("content", focusEdges{
		app.Up:   {Focus: danmakuRulesBack},
		app.Left: {Focus: selectedFilterID},
		app.Down: {Focus: selectedFilterID},
	})
	p.region("rules-tabs", tabEdges)
	p.region("rules-list", listEdges)

	// Top row: Back to danmaku settings + sync status indicator
	ui.Row(c.Key("danmaku-rules-top")).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Margin(0, 0, 25, 0).Children(func() {
		backFocused := p.paintFocus() == danmakuRulesBack
		backInk := paint(colors.Text.Secondary)
		if backFocused {
			backInk = paint(colors.Text.Primary)
		}
		backBtn := ui.Button(c.Key(string(danmakuRulesBack)), "").Label(l.RulesBack).Height(40).
			Padding(0, 4).Radius(8).Background(ui.Transparent).Border(0, ui.Transparent).TextColor(backInk).
			OnClick(func() {
				p.dispatch(func() {
					v.danmakuUI.rulesOpen = false
					p.State.Focus, v.reveal = danmakuPrefBlocklist, true
				})
			})
		p.animateFocus(backBtn, ui.Transparent, backInk)
		p.bind(backBtn, danmakuRulesBack, "rules-top")
		backBtn.Children(func() {
			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, p.Assets.Icon("back")).Size(20, 20)
				ui.Text(c, l.RulesBack).FontSize(19)
			})
		})

		switch {
		case !snap.CloudAvailable:
			ui.Text(c, l.RulesLocalOnly).FontSize(16).TextColor(paint(colors.Text.Muted))
		case !snap.SignedIn:
			ui.Text(c, l.RulesGuest).FontSize(16).TextColor(paint(colors.Text.Muted))
		case snap.SyncState == app.DanmakuRulesSyncing:
			ui.Text(c, l.RulesSyncing).FontSize(16).TextColor(paint(colors.Accent.Soft))
		case snap.SyncState == app.DanmakuRulesFailed:
			retryInk := paint(colors.Status.Warning.Foreground)
			retryBg := ui.Transparent
			if p.paintFocus() == danmakuRulesSyncRetry {
				retryBg = paint(colors.Focus.Background)
				retryInk = paint(colors.Focus.Foreground)
			}
			retryBtn := ui.Button(c.Key(string(danmakuRulesSyncRetry)), "").Label(l.RulesSyncFailed).
				Height(36).Padding(0, 14).Radius(10).Background(retryBg).Border(0, ui.Transparent).TextColor(retryInk).
				OnClick(func() {
					p.dispatch(func() {
						if v.DanmakuRules != nil {
							v.DanmakuRules.Sync()
						}
					})
				})
			p.animateFocus(retryBtn, retryBg, retryInk)
			p.bind(retryBtn, danmakuRulesSyncRetry, "rules-top")
			retryBtn.Children(func() {
				ui.Text(c, l.RulesSyncFailed).FontSize(16)
			})
		default:
			ui.Text(c, l.RulesSynced).FontSize(16).TextColor(paint(colors.Text.Muted))
		}
	})

	// Section heading + "+ 添加规则"
	ui.Row(c.Key("danmaku-rules-heading")).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(25).Children(func() {
		ui.Column(c).Grow(1).Children(func() {
			ui.Text(c, l.RulesTitle).FontSize(32).FontWeight(550).LineHeight(1.3)
			ui.Text(c, l.RulesNote).FontSize(19).LineHeight(1.7).Margin(12, 0, 20, 0).TextColor(paint(colors.Text.Muted))
		})
		p.styledButton(c, l.AddRule, "plus", danmakuRulesAdd, "content", func() {
			defaultKind := provider.DanmakuRuleKeyword
			if v.danmakuUI.filter == danmakuFilterUser {
				defaultKind = provider.DanmakuRuleUser
			}
			v.danmakuUI.draft = danmakuRuleDraft{kind: defaultKind}
			p.State.Push(app.Route{Page: app.DanmakuRuleEditor, Section: "danmaku"})
		}, buttonAppearance{
			height: 52, padding: 24, radius: 13, fontSize: 19, fontWeight: 500, iconSize: 20, iconGap: 10,
			focusShadow: true,
			background:  d.secondaryFill, foreground: paint(colors.Text.Primary), border: d.secondaryBorder,
		})
	})

	// Filter tabs: 全部 / 关键词 / 用户
	ui.Row(c.Key("danmaku-rule-tabs")).Gap(16).Margin(15, 0, 25, 0).AlignItems(ui.Center).Children(func() {
		for _, tab := range []struct {
			filter danmakuRuleFilter
			id     app.FocusID
			label  string
		}{
			{danmakuFilterAll, danmakuFilterAllID, l.FilterAll},
			{danmakuFilterKeyword, danmakuFilterKeywordID, l.FilterKeyword},
			{danmakuFilterUser, danmakuFilterUserID, l.FilterUser},
		} {
			selected := v.danmakuUI.filter == tab.filter
			focused := p.paintFocus() == tab.id
			bg, ink := ui.Transparent, paint(colors.Text.Muted)
			if selected {
				bg, ink = d.subtleFill, paint(colors.Text.Primary)
			}
			if focused {
				bg, ink = paint(colors.Focus.Background), paint(colors.Focus.Foreground)
			}
			filterVal := tab.filter
			btn := ui.Button(c.Key(string(tab.id)), "").Label(tab.label).Height(48).Padding(0, 14).Radius(10).
				Background(bg).Border(0, ui.Transparent).TextColor(ink).
				AccessSelected(selected).OnClick(func() {
				p.dispatch(func() { v.danmakuUI.filter = filterVal })
			})
			p.animateFocus(btn, bg, ink)
			p.bind(btn, tab.id, "rules-tabs")
			btn.Children(func() {
				ui.Column(c).FillHeight().AlignItems(ui.Center).Justify(ui.Center).Gap(4).Children(func() {
					ui.Text(c, tab.label).FontSize(21).FontWeight(500)
					if selected && !focused {
						ui.Box(c).Size(18, 3).Radius(1.5).Background(paint(colors.Accent.Primary))
					} else {
						ui.Box(c).Size(18, 3).Background(ui.Transparent)
					}
				})
			})
		}
	})

	// Rule list
	ui.Box(c).FillWidth().Height(1).Background(d.subtleBorder)
	if len(visible) == 0 {
		ui.Column(c.Key("danmaku-rules-empty")).FillWidth().Padding(60, 20).AlignItems(ui.Center).Gap(14).Children(func() {
			ui.Icon(c, p.Assets.Icon("lock")).Size(44, 44).TextColor(paint(colors.Accent.Soft))
			ui.Text(c, l.EmptyRules).FontSize(25).FontWeight(450).TextColor(paint(colors.Text.Primary))
			ui.Text(c, l.EmptyRulesNote).FontSize(18).TextColor(paint(colors.Text.Muted))
		})
		return
	}
	for _, rule := range visible {
		p.renderDanmakuRuleItem(c, rule, snap.Busy)
	}
}

func (p *PageContext) renderDanmakuRuleItem(c *ui.Context, rule provider.DanmakuRule, busy bool) {
	v := p.host
	l, colors, d := p.Assets.Text.Danmaku, p.Theme.Colors, p.uiColors()
	toggleID := danmakuRuleToggleID(rule.ID)
	removeID := danmakuRuleRemoveID(rule.ID)

	kindLabel := l.FilterKeyword
	if rule.Kind == provider.DanmakuRuleUser {
		kindLabel = l.FilterUser
	}

	ui.Box(c.Key("rule-row-" + rule.ID)).FillWidth().MinHeight(93).Children(func() {
		ui.Row(c).FillWidth().MinHeight(93).Gap(14).AlignItems(ui.Center).Children(func() {
			toggleFocused := p.paintFocus() == toggleID
			fill, ink := ui.Transparent, paint(colors.Text.Primary)
			tagInk := paint(colors.Text.Muted)
			tagBorder := d.secondaryBorder
			if toggleFocused {
				fill, ink = paint(colors.Focus.Background), paint(colors.Focus.Foreground)
				tagInk = d.focusSubtext
				tagBorder = withAlpha(colors.Focus.Foreground, 0x38)
			}
			ruleID := rule.ID
			toggleBtn := ui.Button(c.Key(string(toggleID)), "").Label(kindLabel + " " + rule.Pattern).
				Grow(1).MinHeight(80).Radius(8).Padding(16, 18).Border(0, ui.Transparent).
				Role(ui.RoleSwitch).OnClick(func() {
				p.dispatch(func() {
					if v.DanmakuRules != nil {
						if err := v.DanmakuRules.Toggle(ruleID); err != nil {
							v.showSettingsFeedback(p.Assets.Text.SaveFailed)
						}
					}
				})
			})
			p.animateFocus(toggleBtn, fill, ink)
			p.bind(toggleBtn, toggleID, "rules-list")
			toggleBtn.Children(func() {
				ui.Row(c).FillWidth().Gap(25).AlignItems(ui.Center).Children(func() {
					ui.Box(c).MinWidth(66).Padding(6, 10).Radius(6).Border(1, tagBorder).Center().Children(func() {
						ui.Text(c, kindLabel).FontSize(15).TextColor(tagInk)
					})
					ui.Text(c, rule.Pattern).Grow(1).MinWidth(0).FontSize(23).FontWeight(400).SingleLine()
					p.preferenceSwitch(c, rule.Enabled)
				})
			})

			removeFocused := p.paintFocus() == removeID
			rmFill, rmInk := ui.Transparent, paint(colors.Text.Muted)
			if removeFocused {
				rmFill, rmInk = paint(colors.Focus.Background), paint(colors.Focus.Foreground)
			}
			removeBtn := ui.Button(c.Key(string(removeID)), "").Label(l.RuleRemove + " " + rule.Pattern).
				Size(48, 48).Shrink(0).Radius(24).Border(0, ui.Transparent).Center().
				Disabled(busy).OnClick(func() {
				p.dispatch(func() {
					if v.DanmakuRules == nil {
						return
					}
					v.DanmakuRules.Remove(ruleID, func(err error) {
						if err != nil {
							v.showSettingsFeedback(l.RuleRemoveFailed)
							return
						}
						v.showSettingsFeedback(l.RuleDeleted)
						p.State.Focus, v.reveal = danmakuRulesAdd, true
					})
				})
			})
			p.animateFocus(removeBtn, rmFill, rmInk)
			p.bind(removeBtn, removeID, "rules-list")
			removeBtn.Children(func() {
				ui.Icon(c, p.Assets.Icon("trash")).Size(20, 20)
			})
		})
		ui.Box(c).Absolute().Left(0).Right(0).Bottom(0).Height(1).Background(d.subtleBorder)
	})
}

type danmakuSettingPickerPage struct{}

func (danmakuSettingPickerPage) Render(c *ui.Context, p *PageContext) {
	v := p.host
	title, rawChoices := v.danmakuChoices(v.danmakuUI.pickerKind)
	choices := make([]settingsPickerChoice, len(rawChoices))
	for i, choice := range rawChoices {
		apply := choice.apply
		choices[i] = settingsPickerChoice{
			id:       danmakuPickerOptionID(i),
			label:    choice.label,
			selected: choice.selected,
			onSelect: func() {
				if v.DanmakuRules != nil {
					if err := v.DanmakuRules.UpdateDisplay(apply); err != nil {
						v.showSettingsFeedback(p.Assets.Text.SaveFailed)
					}
				}
				p.State.Back()
			},
		}
	}
	p.renderSettingsPickerDrawer(c, "DANMAKU \u00b7 PREFERENCES", title, choices)
}

type danmakuRuleEditorPage struct{}

func (danmakuRuleEditorPage) Render(c *ui.Context, p *PageContext) {
	v := p.host
	l, colors, d := p.Assets.Text.Danmaku, p.Theme.Colors, p.uiColors()
	draft := &v.danmakuUI.draft
	if draft.kind == "" {
		draft.kind = provider.DanmakuRuleKeyword
	}
	busy := v.DanmakuRules != nil && v.DanmakuRules.Snapshot().Busy

	selectedKindID := danmakuEditorKindKeyword
	if draft.kind == provider.DanmakuRuleUser {
		selectedKindID = danmakuEditorKindUser
	}
	p.region("modal-kinds", focusEdges{
		app.Down: {Focus: danmakuEditorField},
	})
	p.region("modal-field", focusEdges{
		app.Up:   {Focus: selectedKindID},
		app.Down: {Focus: danmakuEditorSave},
	})
	p.region("modal-actions", focusEdges{
		app.Up: {Focus: danmakuEditorField},
	})
	ui.Column(c).FillWidth().Lang(p.langTag()).Font(p.uiFont()).Children(func() {
		p.framelessHeader(c, "DANMAKU \u00b7 BLOCK RULE", "lock", l.AddRuleTitle, "")

		// Kind tabs: 关键词 / 用户
		ui.Row(c.Key("danmaku-editor-kinds")).Gap(14).Margin(0, 0, 24, 0).Children(func() {
			for _, opt := range []struct {
				kind  provider.DanmakuRuleKind
				id    app.FocusID
				label string
			}{
				{provider.DanmakuRuleKeyword, danmakuEditorKindKeyword, l.FilterKeyword},
				{provider.DanmakuRuleUser, danmakuEditorKindUser, l.FilterUser},
			} {
				selected := draft.kind == opt.kind
				focused := p.paintFocus() == opt.id
				bg, ink := ui.Transparent, paint(colors.Text.Muted)
				if selected {
					bg, ink = d.subtleFill, paint(colors.Text.Primary)
				}
				if focused {
					bg, ink = paint(colors.Focus.Background), paint(colors.Focus.Foreground)
				}
				kindVal := opt.kind
				btn := ui.Button(c.Key(string(opt.id)), "").Label(opt.label).Height(50).Padding(0, 18).Radius(10).
					Background(bg).Border(0, ui.Transparent).TextColor(ink).
					AccessSelected(selected).Disabled(busy).OnClick(func() {
					p.dispatch(func() {
						draft.kind = kindVal
						draft.failure = ""
					})
				})
				p.animateFocus(btn, bg, ink)
				p.bind(btn, opt.id, "modal-kinds")
				btn.Children(func() {
					ui.Column(c).FillHeight().AlignItems(ui.Center).Justify(ui.Center).Gap(4).Children(func() {
						ui.Text(c, opt.label).FontSize(20).FontWeight(500)
						if selected && !focused {
							ui.Box(c).Size(18, 3).Radius(1.5).Background(paint(colors.Accent.Primary))
						} else {
							ui.Box(c).Size(18, 3).Background(ui.Transparent)
						}
					})
				})
			}
		})

		placeholder := l.KeywordPlaceholder
		if draft.kind == provider.DanmakuRuleUser {
			placeholder = l.UserPlaceholder
		}
		if draft.editing {
			field := ui.TextInput(c.Key("danmaku-rule-text-input"), &draft.input).FillWidth().Height(96).FontSize(26).
				Label(l.AddRuleTitle).Placeholder(placeholder).MaxLength(provider.DanmakuRuleLimit).
				ReturnKey(ui.ReturnDone).OnTextKey(func(key ui.TextKey) bool {
				switch key.Key {
				case ui.KeyEnter:
					draft.body = key.Text()
					draft.failure = ""
				case ui.KeyEscape:
				default:
					return false
				}
				draft.editing = false
				if key.Key == ui.KeyEnter && strings.TrimSpace(draft.body) != "" {
					p.State.Focus, v.reveal = danmakuEditorSave, true
				} else {
					p.State.Focus, v.reveal = danmakuEditorField, true
				}
				return true
			})
			if p.active {
				field.FocusBind(&p.State.Focus, danmakuEditorInput)
				field.Focus(ui.FocusOptions{PreventScroll: true})
			}
		} else {
			fieldFocused := p.paintFocus() == danmakuEditorField
			displayText := strings.TrimSpace(draft.body)
			ink := paint(colors.Text.Primary)
			if displayText == "" {
				displayText = placeholder
				ink = paint(colors.Text.Muted)
			}
			bg := d.subtleFill
			border := d.secondaryBorder
			borderW := float32(1.5)
			if fieldFocused {
				border = paint(colors.Focus.Ring)
				borderW = 3
			}
			fieldBtn := ui.Button(c.Key(string(danmakuEditorField)), "").Label(placeholder).
				FillWidth().Height(82).Padding(0, 22).Radius(14).
				Background(bg).Border(borderW, border).TextColor(ink).
				Disabled(busy).OnClick(func() {
				p.dispatch(func() {
					draft.editing = true
					draft.input = draft.body
					draft.failure = ""
					p.State.Focus, v.reveal = danmakuEditorInput, true
				})
			})
			p.bind(fieldBtn, danmakuEditorField, "modal-field")
			fieldBtn.Children(func() {
				ui.Row(c).Fill().AlignItems(ui.Center).Children(func() {
					ui.Text(c, displayText).Grow(1).FontSize(25).SingleLine().TextColor(ink)
				})
			})
		}

		if draft.failure != "" {
			ui.Text(c.Key("danmaku-rule-error"), draft.failure).Margin(14, 0, 0, 0).FontSize(18).
				TextColor(paint(colors.Status.Warning.Foreground))
		}

		ui.Row(c.Key("danmaku-editor-actions")).Gap(16).Margin(32, 0, 0, 0).AlignSelf(ui.Start).Children(func() {
			saveLabel := l.SaveRule
			if busy {
				saveLabel = l.SavingRule
			}
			p.styledButton(c, saveLabel, "check", danmakuEditorSave, "modal-actions", func() {
				if v.DanmakuRules == nil || busy {
					return
				}
				v.DanmakuRules.Add(draft.kind, draft.body, func(err error) {
					if err != nil {
						switch {
						case errors.Is(err, app.ErrDanmakuRuleEmpty):
							draft.failure = l.RuleRequired
						case errors.Is(err, app.ErrDanmakuRuleInvalidUser):
							draft.failure = l.RuleInvalidUser
						case errors.Is(err, app.ErrDanmakuRuleDuplicate):
							draft.failure = l.RuleDuplicate
						default:
							draft.failure = l.RuleSaveFailed
						}
						p.State.Focus, v.reveal = danmakuEditorField, true
						return
					}
					v.danmakuUI.draft = danmakuRuleDraft{}
					if p.State.Page == app.DanmakuRuleEditor {
						p.State.Back()
					}
					v.showSettingsFeedback(l.RuleAdded)
					p.State.Focus, v.reveal = danmakuRulesAdd, true
				})
			}, p.framelessButtonAppearance()).Disabled(busy)
			p.styledButton(c, p.Assets.Text.Cancel, "", danmakuEditorCancel, "modal-actions", func() {
				if !busy {
					p.State.Back()
				}
			}, p.framelessButtonAppearance()).Disabled(busy)
		})
	})
}
