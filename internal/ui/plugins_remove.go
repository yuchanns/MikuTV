package tvui

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/plugin"
)

const (
	pluginDisableConfirmBtn app.FocusID = "plugins/disable/confirm"
	pluginRemoveConfirmBtn  app.FocusID = "plugins/remove/confirm"
)

func (v *View) openPluginDisableDialog(item app.InstalledPlugin) {
	v.pluginsUI.pendingAction = item
	v.State.Push(app.Route{Page: app.PluginDisableDialog, Section: "plugins"})
	v.State.Focus, v.reveal = pluginDisableConfirmBtn, true
}

func (v *View) openPluginRemoveDialog(item app.InstalledPlugin) {
	if item.Bundled {
		return
	}
	v.pluginsUI.pendingAction = item
	v.State.Push(app.Route{Page: app.PluginRemoveDialog, Section: "plugins"})
	v.State.Focus, v.reveal = pluginRemoveConfirmBtn, true
}

func (v *View) enablePlugin(item app.InstalledPlugin) {
	l := v.Assets.Text.Plugins
	if v.PluginCatalog == nil {
		v.showSettingsFeedback(l.ToggleFailedToast)
		return
	}
	if err := v.PluginCatalog.SetEnabled(item.ID, true); err != nil {
		v.showSettingsFeedback(l.ToggleFailedToast)
		return
	}
	if item.Kind == plugin.ThemeKind {
		v.showSettingsFeedback(fmt.Sprintf(v.Assets.Text.Appearance.ThemeAppliedToast, item.Name))
		return
	}
	if item.Kind == plugin.LocaleKind {
		name := item.NativeName
		if name == "" {
			name = item.Name
		}
		v.showSettingsFeedback(fmt.Sprintf(v.Assets.Text.Appearance.LanguageAppliedToast, name))
		return
	}
	v.showSettingsFeedback(fmt.Sprintf(v.Assets.Text.Plugins.EnabledToast, item.Name))
}

func (v *View) confirmDisablePlugin() {
	l := v.Assets.Text.Plugins
	item := v.pluginsUI.pendingAction
	wasCurrent := item.Kind == plugin.SourceKind && v.State != nil && strings.EqualFold(v.State.CurrentEntry, item.ID)
	v.State.Back()
	if v.PluginCatalog == nil {
		v.showSettingsFeedback(l.ToggleFailedToast)
		return
	}
	if err := v.PluginCatalog.SetEnabled(item.ID, false); err != nil {
		v.showSettingsFeedback(l.ToggleFailedToast)
		return
	}
	if wasCurrent {
		v.pluginsUI.selectedInstalledID = ""
		v.pluginsUI.view = pluginsViewInstalled
		v.reveal = true
		return
	}
	v.State.Focus, v.reveal = pluginsDetailToggle, true
	v.showSettingsFeedback(fmt.Sprintf(v.Assets.Text.Plugins.DisabledToast, item.Name))
}

func (v *View) confirmRemovePlugin() {
	l := v.Assets.Text.Plugins
	item := v.pluginsUI.pendingAction
	wasCurrent := item.Kind == plugin.SourceKind && v.State != nil && strings.EqualFold(v.State.CurrentEntry, item.ID)
	v.State.Back()
	if v.PluginCatalog == nil {
		v.showSettingsFeedback(l.RemoveFailedToast)
		return
	}
	if err := v.PluginCatalog.Remove(item.ID, false); err != nil {
		v.showSettingsFeedback(l.RemoveFailedToast)
		return
	}
	v.pluginsUI.selectedInstalledID = ""
	v.pluginsUI.selectedCatalogRepo = ""
	v.pluginsUI.view = pluginsViewInstalled
	if wasCurrent {
		v.reveal = true
		return
	}
	installed := v.installedPlugins()
	if len(installed) > 0 {
		v.State.Focus, v.reveal = pluginsInstalledFocus(installed[0].ID), true
	} else {
		v.State.Focus, v.reveal = pluginsDiscoverBtn, true
	}
	v.showSettingsFeedback(fmt.Sprintf(v.Assets.Text.Plugins.RemovedToast, item.Name))
}

type pluginDisableDialogPage struct{}

func (pluginDisableDialogPage) Render(c *ui.Context, p *PageContext) {
	v := p.host
	item := v.pluginsUI.pendingAction
	l, colors := p.Assets.Text.Plugins, p.Theme.Colors

	p.region("plugin-disable-actions", nil)

	ctxLine := item.Name
	if item.Version != "" {
		ctxLine += " \u00b7 " + item.Version
	}
	body := l.DisableDialogBody
	icon := "plug"
	if item.Kind == plugin.ThemeKind {
		body = l.ThemeDisableActiveBody
		icon = "palette"
	} else if item.Kind == plugin.LocaleKind {
		body = l.LocaleDisableActiveBody
		icon = "globe"
	} else if p.State != nil && strings.EqualFold(p.State.CurrentEntry, item.ID) {
		body += "\n" + l.DisableCurrentNote
	}

	ui.Column(c).FillWidth().Lang(p.langTag()).Font(p.uiFont()).Children(func() {
		p.framelessHeader(c, "PLUGINS \u00b7 DISABLE", icon, l.DisableDialogTitle, ctxLine)
		ui.Text(c, body).FillWidth().Margin(22, 0, 0, 0).
			FontSize(20).LineHeight(1.75).TextColor(paint(colors.Text.Secondary))
		ui.Row(c.Key("plugin-disable-actions")).Margin(36, 0, 0, 0).Gap(16).Children(func() {
			p.styledButton(c, l.DisableConfirmAction, "", pluginDisableConfirmBtn, "plugin-disable-actions", func() {
				v.confirmDisablePlugin()
			}, p.framelessButtonAppearance())
			p.styledButton(c, p.Assets.Text.Cancel, "", app.CloseModal, "plugin-disable-actions", func() {
				p.State.Back()
			}, p.framelessButtonAppearance())
		})
	})
}

type pluginRemoveDialogPage struct{}

func (pluginRemoveDialogPage) Render(c *ui.Context, p *PageContext) {
	v := p.host
	item := v.pluginsUI.pendingAction
	l, colors := p.Assets.Text.Plugins, p.Theme.Colors

	p.region("plugin-remove-actions", nil)

	ctxLine := item.Name
	if item.Version != "" {
		ctxLine += " \u00b7 " + item.Version
	}
	body := l.RemoveDialogBody
	if item.Kind == plugin.ThemeKind {
		if item.Enabled {
			body = l.ThemeRemoveActiveBody
		} else {
			body = l.ThemeRemoveInactiveBody
		}
	} else if item.Kind == plugin.LocaleKind {
		if item.Enabled {
			body = l.LocaleRemoveActiveBody
		} else {
			body = l.LocaleRemoveInactiveBody
		}
	} else if p.State != nil && strings.EqualFold(p.State.CurrentEntry, item.ID) {
		body += "\n" + l.DisableCurrentNote
	}

	ui.Column(c).FillWidth().Lang(p.langTag()).Font(p.uiFont()).Children(func() {
		p.framelessHeader(c, "PLUGINS \u00b7 REMOVE", "trash", l.RemoveDialogTitle, ctxLine)
		ui.Text(c, body).FillWidth().Margin(22, 0, 0, 0).
			FontSize(20).LineHeight(1.75).TextColor(paint(colors.Text.Secondary))
		ui.Row(c.Key("plugin-remove-actions")).Margin(36, 0, 0, 0).Gap(16).Children(func() {
			p.styledButton(c, l.RemoveConfirmAction, "", pluginRemoveConfirmBtn, "plugin-remove-actions", func() {
				v.confirmRemovePlugin()
			}, p.framelessButtonAppearance())
			p.styledButton(c, p.Assets.Text.Cancel, "", app.CloseModal, "plugin-remove-actions", func() {
				p.State.Back()
			}, p.framelessButtonAppearance())
		})
	})
}
