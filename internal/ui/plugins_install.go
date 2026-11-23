package tvui

import (
	"errors"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/plugin"
)

type pluginInstallPhase uint8

const (
	pluginInstallConfirm pluginInstallPhase = iota
	pluginInstallPending
	pluginInstallError
	pluginInstallSuccess
)

const (
	pluginRepoField  app.FocusID = "plugins/repo/field"
	pluginRepoEdit   app.FocusID = "plugins/repo/edit"
	pluginRepoSubmit app.FocusID = "plugins/repo/submit"

	pluginInstallConfirmBtn        app.FocusID = "plugins/install/confirm"
	pluginInstallRetryBtn          app.FocusID = "plugins/install/retry"
	pluginInstallOpenEntryBtn      app.FocusID = "plugins/install/open-entry"
	pluginInstallOpenAppearanceBtn app.FocusID = "plugins/install/open-appearance"
	pluginInstallOpenLanguageBtn   app.FocusID = "plugins/install/open-language"

	pluginRepoMaxLength = 120
)

type pluginRepoDraft struct {
	text    string
	input   string
	editing bool
	busy    bool
	failure string
}

type pluginInstallState struct {
	phase     pluginInstallPhase
	item      plugin.CatalogItem
	receipt   plugin.InstalledReceipt
	failure   string
	retryable bool
}

func (v *View) openPluginRepoInput() {
	v.pluginsUI.repo = pluginRepoDraft{}
	v.State.Push(app.Route{Page: app.PluginRepoInput, Section: "plugins"})
	v.State.Focus, v.reveal = pluginRepoField, true
}

func (v *View) openPluginInstallConfirm(item plugin.CatalogItem) {
	l := v.Assets.Text.Plugins
	if v.PluginCatalog != nil {
		if err := v.PluginCatalog.CheckInstallable(item); err != nil {
			switch {
			case errors.Is(err, app.ErrPluginAlreadyInstalled):
				v.showSettingsFeedback(l.InstallAlreadyInstalled)
			case errors.Is(err, plugin.ErrSourceConflict):
				v.showSettingsFeedback(l.InstallErrConflict)
			default:
				v.showSettingsFeedback(l.InstallErrIncompatible)
			}
			return
		}
	}
	v.pluginsUI.install = pluginInstallState{
		phase: pluginInstallConfirm,
		item:  item,
	}
	v.State.Push(app.Route{Page: app.PluginInstallDialog, Section: "plugins"})
	v.State.Focus, v.reveal = pluginInstallConfirmBtn, true
}

func (v *View) startPluginInstall() {
	st := &v.pluginsUI.install
	st.phase = pluginInstallPending
	st.failure = ""
	st.retryable = false
	v.State.Focus, v.reveal = app.CloseModal, true

	if v.PluginCatalog == nil {
		st.phase = pluginInstallError
		st.failure = v.Assets.Text.Plugins.InstallErrNetwork
		st.retryable = false
		return
	}
	item := st.item
	v.PluginCatalog.Install(item, func(out app.PluginInstallOutcome) {
		if out.Canceled {
			return
		}
		if out.Err != nil {
			st.phase = pluginInstallError
			st.failure = v.pluginTransferErrorMessage(out.Err)
			st.retryable = out.Retryable
			if out.Retryable {
				v.State.Focus, v.reveal = pluginInstallRetryBtn, true
			} else {
				v.State.Focus, v.reveal = app.CloseModal, true
			}
			return
		}
		st.phase = pluginInstallSuccess
		st.receipt = out.Receipt
		st.failure = ""
		st.retryable = false
		if out.Receipt.Kind == plugin.ThemeKind && v.pluginsUI.returnToThemePicker {
			v.State.Back()
			v.returnToAppearanceThemePicker(out.Receipt.ID)
			return
		}
		if out.Receipt.Kind == plugin.LocaleKind && v.pluginsUI.returnToLanguagePicker {
			v.State.Back()
			v.returnToAppearanceLanguagePicker(out.Receipt.ID)
			return
		}
		// Prepare the underlying settings page on Installed focused at the new plugin.
		v.pluginsUI.view = pluginsViewInstalled
		v.pluginsUI.selectedInstalledID = ""
		v.pluginsUI.selectedCatalogRepo = ""
		v.pluginsUI.returnFocus = pluginsInstalledFocus(out.Receipt.ID)
		switch out.Receipt.Kind {
		case plugin.ThemeKind:
			v.State.Focus, v.reveal = pluginInstallOpenAppearanceBtn, true
		case plugin.LocaleKind:
			v.State.Focus, v.reveal = pluginInstallOpenLanguageBtn, true
		default:
			v.State.Focus, v.reveal = pluginInstallOpenEntryBtn, true
		}
	})
}

func (v *View) pluginTransferErrorMessage(err error) string {
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

func (p *PageContext) renderPluginPermissionList(c *ui.Context, domains []string, addedSet map[string]bool) {
	colors, d, l := p.Theme.Colors, p.uiColors(), p.Assets.Text.Plugins
	ui.Column(c).FillWidth().Gap(8).Children(func() {
		limit := min(len(domains), 6)
		for _, domain := range domains[:limit] {
			isNew := addedSet != nil && addedSet[strings.ToLower(strings.TrimSpace(domain))]
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
}

func (v *View) submitPluginRepository() {
	draft := &v.pluginsUI.repo
	l := v.Assets.Text.Plugins
	if draft.busy {
		return
	}
	raw := strings.TrimSpace(draft.text)
	if _, ok := plugin.ParseRepository(raw); !ok {
		draft.failure = l.RepoInvalidErr
		v.reveal = true
		return
	}
	if v.PluginCatalog == nil {
		draft.failure = l.RepoResolveFailedErr
		v.reveal = true
		return
	}
	draft.busy = true
	draft.failure = ""
	v.PluginCatalog.ResolveRepository(raw, func(item plugin.CatalogItem, err error) {
		draft.busy = false
		if err != nil {
			switch {
			case errors.Is(err, plugin.ErrInvalidRepository):
				draft.failure = l.RepoInvalidErr
			case errors.Is(err, plugin.ErrNoOfficialRelease):
				draft.failure = l.RepoNotFoundErr
			case errors.Is(err, plugin.ErrVersionTagMismatch):
				draft.failure = l.RepoVersionMismatchErr
			default:
				draft.failure = l.RepoResolveFailedErr
			}
			v.reveal = true
			return
		}
		if existing, ok := v.PluginCatalog.FindInstalled(item.Manifest.ID); ok {
			if existing.Bundled || !strings.EqualFold(existing.Repository, item.Repository) {
				draft.failure = l.RepoConflictErr
				v.reveal = true
				return
			}
			v.State.Back()
			v.pluginsUI.view = pluginsViewInstalled
			v.pluginsUI.selectedInstalledID = existing.ID
			v.pluginsUI.selectedCatalogRepo = ""
			v.pluginsUI.returnFocus = pluginsInstalledFocus(existing.ID)
			v.State.Focus, v.reveal = pluginsDetailToggle, true
			return
		}
		v.State.Back()
		v.pluginsUI.view = pluginsViewDiscover
		v.pluginsUI.filter = ""
		v.pluginsUI.query = ""
		v.pluginsUI.input = ""
		v.pluginsUI.selectedInstalledID = ""
		v.pluginsUI.selectedCatalogRepo = item.Repository
		v.pluginsUI.returnFocus = pluginsCatalogFocus(item.Repository)
		if item.Compatibility.Compatible {
			v.State.Focus, v.reveal = pluginsDetailInstall, true
		} else {
			v.State.Focus, v.reveal = pluginsBackBtn, true
		}
	})
}

type pluginRepoInputPage struct{}

func (pluginRepoInputPage) Render(c *ui.Context, p *PageContext) {
	v := p.host
	draft := &v.pluginsUI.repo
	l, colors, d := p.Assets.Text.Plugins, p.Theme.Colors, p.uiColors()

	p.region("plugin-repo-field", focusEdges{
		app.Down: {Focus: pluginRepoSubmit},
	})
	p.region("plugin-repo-actions", focusEdges{
		app.Up: {Focus: pluginRepoField},
	})

	ui.Column(c).FillWidth().Lang(p.langTag()).Font(p.uiFont()).Children(func() {
		p.framelessHeader(c, "PLUGINS \u00b7 REPOSITORY", "repository", l.RepoDialogTitle, l.RepoFieldLabel)

		if draft.editing {
			ui.Row(c.Key("plugin-repo-edit-box")).FillWidth().Height(64).Margin(28, 0, 0, 0).Padding(0, 22).
				AlignItems(ui.Center).Radius(12).Background(paint(colors.Surface.Input)).
				Border(1, paint(colors.Focus.Ring)).Shadow(0, 0, 0, 3, paint(colors.Focus.Ring)).Children(func() {
				field := ui.TextInput(c.Key("plugin-repo-input"), &draft.input).Grow(1).MinWidth(0).Height(44).
					Padding(0, 0).Border(0, ui.Transparent).Background(ui.Transparent).
					FontSize(22).TextColor(paint(colors.Text.Primary)).
					Label(l.RepoFieldLabel).Placeholder(l.RepoPlaceholder).PlaceholderColor(paint(colors.Text.Muted)).
					MaxLength(pluginRepoMaxLength).ReturnKey(ui.ReturnDone).
					OnChange(func() {
						draft.text = strings.TrimSpace(draft.input)
						draft.failure = ""
					}).
					OnTextKey(func(key ui.TextKey) bool {
						switch key.Key {
						case ui.KeyEnter:
							draft.text = strings.TrimSpace(key.Text())
							draft.input = draft.text
							draft.editing = false
							draft.failure = ""
							v.State.Focus, v.reveal = pluginRepoSubmit, true
							return true
						case ui.KeyEscape:
							draft.editing = false
							draft.input = draft.text
							v.State.Focus, v.reveal = pluginRepoField, true
							return true
						default:
							return false
						}
					})
				if p.active {
					field.FocusRing(false)
					field.FocusBind(&p.State.Focus, pluginRepoEdit)
					field.Focus(ui.FocusOptions{PreventScroll: true})
				}
			})
		} else {
			display := draft.text
			ink := paint(colors.Text.Primary)
			if display == "" {
				display = l.RepoPlaceholder
				ink = paint(colors.Text.Muted)
			}
			focused := p.paintFocus() == pluginRepoField
			border := d.strongBorder
			if focused {
				border = paint(colors.Focus.Ring)
			}
			btn := ui.Button(c.Key(string(pluginRepoField)), "").Label(display).
				FillWidth().Height(64).Margin(28, 0, 0, 0).Padding(0, 22).Radius(12).
				Background(paint(colors.Surface.Input)).Border(1, border).TextColor(ink).
				OnClick(func() {
					p.dispatch(func() {
						if draft.busy {
							return
						}
						draft.input = draft.text
						draft.editing = true
						p.State.Focus = pluginRepoEdit
					})
				})
			if focused {
				btn.Shadow(0, 0, 0, 3, paint(colors.Focus.Ring))
			}
			p.bind(btn, pluginRepoField, "plugin-repo-field")
			btn.Children(func() {
				ui.Row(c).Fill().AlignItems(ui.Center).Children(func() {
					ui.Text(c, display).Grow(1).MinWidth(0).SingleLine().FontSize(22).TextColor(ink)
				})
			})
		}

		if draft.failure != "" {
			ui.Text(c.Key("plugin-repo-error"), draft.failure).FillWidth().Margin(16, 0, 0, 0).
				FontSize(18).LineHeight(1.5).TextColor(paint(colors.Status.Error.Foreground))
		}

		submitLabel := l.RepoInspectAction
		if draft.busy {
			submitLabel = l.RepoInspecting
		}
		ui.Row(c.Key("plugin-repo-actions")).Margin(36, 0, 0, 0).Gap(16).Children(func() {
			p.styledButton(c, submitLabel, "", pluginRepoSubmit, "plugin-repo-actions", func() {
				v.submitPluginRepository()
			}, p.framelessButtonAppearance())
			p.styledButton(c, p.Assets.Text.Cancel, "", app.CloseModal, "plugin-repo-actions", func() {
				if v.PluginCatalog != nil {
					v.PluginCatalog.CancelResolve()
				}
				p.State.Back()
			}, p.framelessButtonAppearance())
		})
	})
}

type pluginInstallDialogPage struct{}

func (pluginInstallDialogPage) Render(c *ui.Context, p *PageContext) {
	v := p.host
	st := &v.pluginsUI.install
	l, colors := p.Assets.Text.Plugins, p.Theme.Colors

	p.region("plugin-install-actions", nil)
	ctxLine := st.item.Manifest.Name
	if st.item.Manifest.Version != "" {
		ctxLine += " \u00b7 " + p.pluginKindLabel(st.item.Manifest.Kind) + " \u00b7 " + st.item.Manifest.Version
	}

	ui.Column(c).FillWidth().Lang(p.langTag()).Font(p.uiFont()).Children(func() {
		switch st.phase {
		case pluginInstallConfirm:
			body := ctxLine
			if st.item.Repository != "" {
				body += "\n" + l.SourceLabel + " \u00b7 " + st.item.Repository
				if st.item.Release.TagName != "" {
					body += " (" + st.item.Release.TagName + ")"
				}
			}
			if st.item.Manifest.Kind == plugin.ThemeKind {
				p.framelessHeader(c, "PLUGINS \u00b7 INSTALL", "palette", l.InstallConfirmTitle, body)
				ui.Text(c, l.ThemeInstallConfirmBody).FillWidth().Margin(24, 0, 0, 0).
					FontSize(20).LineHeight(1.7).TextColor(paint(colors.Text.Secondary))
			} else if st.item.Manifest.Kind == plugin.LocaleKind {
				p.framelessHeader(c, "PLUGINS \u00b7 INSTALL", "globe", l.InstallConfirmTitle, body)
				ui.Text(c, l.LocaleInstallConfirmBody).FillWidth().Margin(24, 0, 0, 0).
					FontSize(20).LineHeight(1.7).TextColor(paint(colors.Text.Secondary))
			} else {
				p.framelessHeader(c, "PLUGINS \u00b7 INSTALL", "plus", l.InstallConfirmTitle, body)
				permIntro := l.InstallPermissionsIntro
				if len(st.item.Permissions) == 0 {
					if st.item.UserServerOrigin {
						permIntro = l.InstallUserServerPermissions
					} else {
						permIntro = l.InstallNoPermissions
					}
				}
				ui.Text(c, permIntro).FillWidth().Margin(24, 0, 14, 0).
					FontSize(19).LineHeight(1.6).TextColor(paint(colors.Text.Secondary))
				if len(st.item.Permissions) > 0 {
					p.renderPluginPermissionList(c, st.item.Permissions, nil)
				}
			}
			ui.Row(c.Key("plugin-install-actions")).Margin(36, 0, 0, 0).Gap(16).Children(func() {
				p.styledButton(c, l.InstallConfirmAction, "", pluginInstallConfirmBtn, "plugin-install-actions", func() {
					v.startPluginInstall()
				}, p.framelessButtonAppearance())
				p.styledButton(c, p.Assets.Text.Cancel, "", app.CloseModal, "plugin-install-actions", func() {
					p.State.Back()
				}, p.framelessButtonAppearance())
			})

		case pluginInstallPending:
			p.framelessHeader(c, "PLUGINS \u00b7 INSTALLING", "refresh", l.InstallPendingTitle, ctxLine)
			ui.Row(c).FillWidth().Margin(28, 0, 0, 0).Gap(16).AlignItems(ui.Center).Children(func() {
				p.loadingSpinner(c, 32)
				ui.Text(c, l.InstallPendingBody).Grow(1).MinWidth(0).FontSize(19).LineHeight(1.6).TextColor(paint(colors.Text.Muted))
			})
			ui.Row(c).Margin(36, 0, 0, 0).Gap(16).Children(func() {
				p.styledButton(c, p.Assets.Text.Cancel, "", app.CloseModal, "plugin-install-actions", func() {
					if v.PluginCatalog != nil {
						v.PluginCatalog.CancelInstall()
					}
					p.State.Back()
				}, p.framelessButtonAppearance())
			})

		case pluginInstallError:
			body := st.failure
			if body == "" {
				body = l.InstallFailedDefault
			}
			body += "\n" + l.InstallFailedNotInstalled
			p.framelessHeader(c, "PLUGINS \u00b7 ERROR", "wifi", l.InstallFailedTitle, body)
			ui.Row(c).Margin(36, 0, 0, 0).Gap(16).Children(func() {
				if st.retryable {
					p.styledButton(c, l.InstallRetryAction, "refresh", pluginInstallRetryBtn, "plugin-install-actions", func() {
						v.startPluginInstall()
					}, p.framelessButtonAppearance())
				}
				p.styledButton(c, l.InstallRetryLater, "", app.CloseModal, "plugin-install-actions", func() {
					p.State.Back()
				}, p.framelessButtonAppearance())
			})

		case pluginInstallSuccess:
			name := st.receipt.Name
			if name == "" {
				name = st.item.Manifest.Name
			}
			isTheme := st.receipt.Kind == plugin.ThemeKind || st.item.Manifest.Kind == plugin.ThemeKind
			isLocale := st.receipt.Kind == plugin.LocaleKind || st.item.Manifest.Kind == plugin.LocaleKind
			body := name + "\n" + l.InstallSuccessBody
			if isTheme {
				body = name + "\n" + l.ThemeInstallSuccessBody
			} else if isLocale {
				body = name + "\n" + l.LocaleInstallSuccessBody
			}
			p.framelessHeader(c, "PLUGINS \u00b7 READY", "check", l.InstallSuccessTitle, body)
			ui.Row(c.Key("plugin-install-actions")).Margin(36, 0, 0, 0).Gap(16).Children(func() {
				if isTheme {
					p.styledButton(c, l.ThemeOpenAppearance, "palette", pluginInstallOpenAppearanceBtn, "plugin-install-actions", func() {
						newID := st.receipt.ID
						p.State.Back()
						v.returnToAppearanceThemePicker(newID)
					}, p.framelessButtonAppearance())
				} else if isLocale {
					p.styledButton(c, l.ThemeOpenAppearance, "globe", pluginInstallOpenLanguageBtn, "plugin-install-actions", func() {
						newID := st.receipt.ID
						p.State.Back()
						v.returnToAppearanceLanguagePicker(newID)
					}, p.framelessButtonAppearance())
				} else {
					p.styledButton(c, l.InstallOpenEntries, "switch", pluginInstallOpenEntryBtn, "plugin-install-actions", func() {
						newID := st.receipt.ID
						p.State.Back()
						if newID != "" {
							p.State.Focus = pluginsInstalledFocus(newID)
							p.State.OpenEntries(newID)
						} else {
							p.State.OpenEntries("")
						}
						v.reveal = true
					}, p.framelessButtonAppearance())
				}
				p.styledButton(c, l.InstallDone, "", app.CloseModal, "plugin-install-actions", func() {
					newID := st.receipt.ID
					p.State.Back()
					if newID != "" {
						p.State.Focus, v.reveal = pluginsInstalledFocus(newID), true
					}
				}, p.framelessButtonAppearance())
			})
		}
	})
}
