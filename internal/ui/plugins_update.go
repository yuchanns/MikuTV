package tvui

import (
	"errors"
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/plugin"
)

type pluginUpdatePhase uint8

const (
	pluginUpdateConfirm pluginUpdatePhase = iota
	pluginUpdatePending
	pluginUpdateError
)

const (
	pluginUpdateConfirmBtn app.FocusID = "plugins/update/confirm"
	pluginUpdateRetryBtn   app.FocusID = "plugins/update/retry"
)

type pluginUpdateState struct {
	phase     pluginUpdatePhase
	installed app.InstalledPlugin
	offer     app.PluginUpdateOffer
	failure   string
	retryable bool
}

func (v *View) checkPluginUpdates() {
	l := v.Assets.Text.Plugins
	if v.PluginCatalog == nil {
		v.showSettingsFeedback(l.UpdatesBuiltinOnly)
		return
	}
	snap := v.PluginCatalog.Snapshot()
	if snap.CheckingUpdates {
		return
	}
	hasExternal := false
	for _, item := range snap.Installed {
		if !item.Bundled && strings.TrimSpace(item.Repository) != "" {
			hasExternal = true
			break
		}
	}
	if !hasExternal {
		v.showSettingsFeedback(l.UpdatesBuiltinOnly)
		return
	}
	v.PluginCatalog.CheckUpdates(func(found, _ int, err error) {
		switch {
		case found > 0:
			v.showSettingsFeedback(fmt.Sprintf(l.UpdatesFound, found))
		case err != nil:
			v.showSettingsFeedback(l.UpdatesCheckFailed)
		default:
			v.showSettingsFeedback(l.UpdatesUpToDate)
		}
	})
}

func (v *View) checkSinglePluginUpdate(pluginID string) {
	l := v.Assets.Text.Plugins
	if v.PluginCatalog == nil {
		v.showSettingsFeedback(l.UpdatesCheckFailed)
		return
	}
	if v.PluginCatalog.Snapshot().CheckingUpdates {
		return
	}
	v.PluginCatalog.CheckPluginUpdate(pluginID, func(offer *app.PluginUpdateOffer, err error) {
		switch {
		case err != nil:
			v.showSettingsFeedback(l.UpdatesCheckFailed)
		case offer != nil && offer.IncompatibleKind:
			v.showSettingsFeedback(l.UpdateErrKindChanged)
		case offer != nil && !offer.Item.Compatibility.Compatible:
			v.showSettingsFeedback(l.InstallErrIncompatible)
		case offer != nil:
			v.showSettingsFeedback(fmt.Sprintf(l.UpdateAvailableNote, offer.Item.Manifest.Version))
		default:
			v.showSettingsFeedback(l.UpdatesUpToDate)
		}
	})
}

func (v *View) openPluginUpdateDialog(inst app.InstalledPlugin) {
	l := v.Assets.Text.Plugins
	if inst.Bundled {
		v.showSettingsFeedback(l.UpdatesBuiltinOnly)
		return
	}
	if inst.AvailableUpdate == nil {
		v.checkSinglePluginUpdate(inst.ID)
		return
	}
	offer := *inst.AvailableUpdate
	if offer.IncompatibleKind {
		v.showSettingsFeedback(l.UpdateErrKindChanged)
		return
	}
	if !offer.Item.Compatibility.Compatible {
		v.showSettingsFeedback(l.InstallErrIncompatible)
		return
	}
	v.pluginsUI.update = pluginUpdateState{
		phase:     pluginUpdateConfirm,
		installed: inst,
		offer:     offer,
	}
	v.State.Push(app.Route{Page: app.PluginUpdateDialog, Section: "plugins"})
	v.State.Focus, v.reveal = pluginUpdateConfirmBtn, true
}

func (v *View) startPluginUpdate() {
	st := &v.pluginsUI.update
	st.phase = pluginUpdatePending
	st.failure = ""
	st.retryable = false
	v.State.Focus, v.reveal = app.CloseModal, true

	if v.PluginCatalog == nil {
		st.phase = pluginUpdateError
		st.failure = v.Assets.Text.Plugins.InstallErrNetwork
		st.retryable = false
		return
	}
	pluginID := st.installed.ID
	offer := st.offer
	v.PluginCatalog.Update(pluginID, offer, func(out app.PluginUpdateOutcome) {
		if out.Canceled {
			return
		}
		if out.Err != nil {
			st.phase = pluginUpdateError
			st.failure = v.pluginUpdateErrorMessage(out.Err)
			st.retryable = out.Retryable
			if out.Retryable {
				v.State.Focus, v.reveal = pluginUpdateRetryBtn, true
			} else {
				v.State.Focus, v.reveal = app.CloseModal, true
			}
			return
		}
		l := v.Assets.Text.Plugins
		name := out.Receipt.Name
		if name == "" {
			name = st.installed.Name
		}
		v.State.Back()
		if v.pluginsUI.selectedInstalledID != "" {
			v.State.Focus, v.reveal = pluginsDetailVersion, true
		} else {
			v.State.Focus, v.reveal = pluginsInstalledFocus(out.Receipt.ID), true
		}
		v.showSettingsFeedback(fmt.Sprintf(l.UpdateSuccessToast, name, out.Receipt.Version))
	})
}

func (v *View) pluginUpdateErrorMessage(err error) string {
	l := v.Assets.Text.Plugins
	switch {
	case errors.Is(err, plugin.ErrKindChanged):
		return l.UpdateErrKindChanged
	case errors.Is(err, plugin.ErrDigestMismatch):
		return l.InstallErrDigest
	case errors.Is(err, plugin.ErrSourceConflict):
		return l.InstallErrConflict
	case errors.Is(err, plugin.ErrVersionTagMismatch):
		return l.RepoVersionMismatchErr
	case errors.Is(err, plugin.ErrIncompatible):
		return l.InstallErrIncompatible
	default:
		return l.InstallErrNetwork
	}
}

type pluginUpdateDialogPage struct{}

func (pluginUpdateDialogPage) Render(c *ui.Context, p *PageContext) {
	v := p.host
	st := &v.pluginsUI.update
	l, colors, d := p.Assets.Text.Plugins, p.Theme.Colors, p.uiColors()

	p.region("plugin-update-actions", nil)

	name := st.installed.Name
	if name == "" {
		name = st.offer.Item.Manifest.Name
	}
	versionLine := name
	if st.installed.Version != "" && st.offer.Item.Manifest.Version != "" {
		versionLine += " \u00b7 " + st.installed.Version + " \u2192 " + st.offer.Item.Manifest.Version
	}

	ui.Column(c).FillWidth().Lang(p.langTag()).Font(p.uiFont()).Children(func() {
		switch st.phase {
		case pluginUpdateConfirm:
			body := versionLine
			if st.offer.Item.Repository != "" {
				body += "\n" + l.SourceLabel + " \u00b7 " + st.offer.Item.Repository
				if st.offer.Item.Release.TagName != "" {
					body += " (" + st.offer.Item.Release.TagName + ")"
				}
			}
			p.framelessHeader(c, "PLUGINS \u00b7 UPDATE", "refresh", l.UpdateDialogTitle, body)

			addedSet := make(map[string]bool, len(st.offer.AddedPermissions))
			for _, host := range st.offer.AddedPermissions {
				addedSet[strings.ToLower(strings.TrimSpace(host))] = true
			}
			expanded := len(st.offer.AddedPermissions) > 0
			domains := st.offer.Item.Permissions

			if st.installed.Kind == plugin.ThemeKind {
				expanded = false
				ui.Text(c, l.ThemeUpdateConfirmBody).FillWidth().Margin(24, 0, 0, 0).
					FontSize(19).LineHeight(1.6).TextColor(paint(colors.Text.Secondary))
			} else if st.installed.Kind == plugin.LocaleKind {
				expanded = false
				ui.Text(c, l.LocaleUpdateConfirmBody).FillWidth().Margin(24, 0, 0, 0).
					FontSize(19).LineHeight(1.6).TextColor(paint(colors.Text.Secondary))
			} else if expanded && len(domains) > 0 {
				ui.Text(c, l.InstallPermissionsIntro).FillWidth().Margin(24, 0, 14, 0).
					FontSize(19).LineHeight(1.6).TextColor(paint(colors.Text.Secondary))
				ui.Column(c).FillWidth().Gap(8).Children(func() {
					limit := min(len(domains), 6)
					for _, domain := range domains[:limit] {
						isNew := addedSet[strings.ToLower(strings.TrimSpace(domain))]
						ui.Row(c).FillWidth().MinHeight(54).Padding(12, 18).Radius(10).
							Background(d.subtleFill).AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(16).Children(func() {
							ui.Text(c, domain).Grow(1).MinWidth(0).SingleLine().FontSize(19).TextColor(paint(colors.Text.Primary))
							if isNew {
								ui.Text(c, l.UpdateAddedPermission).Shrink(0).FontSize(17).FontWeight(500).TextColor(paint(colors.Accent.Soft))
							} else {
								ui.Icon(c, p.Assets.Icon("lock")).Size(20, 20).TextColor(paint(colors.Text.Secondary))
							}
						})
					}
				})
			} else {
				ui.Text(c, l.UpdateNoNewPermissions).FillWidth().Margin(24, 0, 0, 0).
					FontSize(19).LineHeight(1.6).TextColor(paint(colors.Text.Secondary))
			}

			confirmLabel := l.UpdateConfirmAction
			if expanded {
				confirmLabel = l.UpdateConfirmAllowAction
			}
			ui.Row(c.Key("plugin-update-actions")).Margin(36, 0, 0, 0).Gap(16).Children(func() {
				p.styledButton(c, confirmLabel, "", pluginUpdateConfirmBtn, "plugin-update-actions", func() {
					v.startPluginUpdate()
				}, p.framelessButtonAppearance())
				p.styledButton(c, p.Assets.Text.Cancel, "", app.CloseModal, "plugin-update-actions", func() {
					p.State.Back()
				}, p.framelessButtonAppearance())
			})

		case pluginUpdatePending:
			p.framelessHeader(c, "PLUGINS \u00b7 UPDATING", "refresh", l.UpdatePendingTitle, versionLine)
			ui.Row(c).FillWidth().Margin(28, 0, 0, 0).Gap(16).AlignItems(ui.Center).Children(func() {
				p.loadingSpinner(c, 32)
				ui.Text(c, l.UpdatePendingBody).Grow(1).MinWidth(0).FontSize(19).LineHeight(1.6).TextColor(paint(colors.Text.Muted))
			})
			ui.Row(c.Key("plugin-update-actions")).Margin(36, 0, 0, 0).Gap(16).Children(func() {
				p.styledButton(c, p.Assets.Text.Cancel, "", app.CloseModal, "plugin-update-actions", func() {
					if v.PluginCatalog != nil {
						v.PluginCatalog.CancelUpdate()
					}
					p.State.Back()
				}, p.framelessButtonAppearance())
			})

		case pluginUpdateError:
			body := st.failure
			if body == "" {
				body = l.InstallErrNetwork
			}
			body += "\n" + l.UpdateFailedPreserved
			p.framelessHeader(c, "PLUGINS \u00b7 ERROR", "wifi", l.UpdateFailedTitle, body)
			ui.Row(c.Key("plugin-update-actions")).Margin(36, 0, 0, 0).Gap(16).Children(func() {
				if st.retryable {
					p.styledButton(c, l.InstallRetryAction, "refresh", pluginUpdateRetryBtn, "plugin-update-actions", func() {
						v.startPluginUpdate()
					}, p.framelessButtonAppearance())
				}
				p.styledButton(c, l.InstallRetryLater, "", app.CloseModal, "plugin-update-actions", func() {
					p.State.Back()
				}, p.framelessButtonAppearance())
			})
		}
	})
}
