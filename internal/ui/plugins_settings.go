package tvui

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/plugin"
	"github.com/yuchanns/MikuTV/internal/theme"
)

type pluginsViewMode uint8

const (
	pluginsViewInstalled pluginsViewMode = iota
	pluginsViewDiscover
)

const (
	pluginsDiscoverBtn     app.FocusID = "settings/plugins/discover"
	pluginsAddRepoBtn      app.FocusID = "settings/plugins/add-repo"
	pluginsCheckUpdatesBtn app.FocusID = "settings/plugins/check-updates"

	pluginsBackBtn         app.FocusID = "settings/plugins/back"
	pluginsRefreshTopBtn   app.FocusID = "settings/plugins/refresh-top"
	pluginsSearchInput     app.FocusID = "settings/plugins/search"
	pluginsSearchEdit      app.FocusID = "settings/plugins/search-edit"
	pluginsFilterAll       app.FocusID = "settings/plugins/filter/all"
	pluginsFilterSource    app.FocusID = "settings/plugins/filter/source"
	pluginsFilterTheme     app.FocusID = "settings/plugins/filter/theme"
	pluginsRetryBtn        app.FocusID = "settings/plugins/retry"
	pluginsEmptyAddRepoBtn app.FocusID = "settings/plugins/empty-add-repo"
	pluginsEmptyExitBtn    app.FocusID = "settings/plugins/empty-exit"
	pluginsLoadMoreBtn     app.FocusID = "settings/plugins/load-more"

	pluginsDetailToggle  app.FocusID = "settings/plugins/detail-toggle"
	pluginsDetailVersion app.FocusID = "settings/plugins/detail-version"
	pluginsDetailSource  app.FocusID = "settings/plugins/detail-source"
	pluginsDetailRemove  app.FocusID = "settings/plugins/detail-remove"
	pluginsDetailCompat  app.FocusID = "settings/plugins/detail-compat"
	pluginsDetailInstall app.FocusID = "settings/plugins/detail-install"

	pluginSearchMaxLength = 120
)

func pluginsInstalledFocus(id string) app.FocusID {
	return app.FocusID("settings/plugins/installed/" + id)
}

func pluginsCatalogFocus(repo string) app.FocusID {
	return app.FocusID("settings/plugins/catalog/" + strings.ToLower(strings.TrimSpace(repo)))
}

type pluginsSettingsUI struct {
	view                 pluginsViewMode
	selectedInstalledID  string
	selectedCatalogRepo  string
	filter               plugin.Kind
	query                string
	input                string
	editing              bool
	listScroll           float32
	pendingRestoreScroll bool
	returnFocus          app.FocusID
	returnToThemePicker  bool
	repo                 pluginRepoDraft
	install              pluginInstallState
	update               pluginUpdateState
	pendingAction        app.InstalledPlugin
}

func syncPluginsSettings(v *View) {
	if v.pluginsUI.view == pluginsViewDiscover && v.PluginCatalog != nil {
		v.PluginCatalog.EnsureLoaded()
	}
}

func (v *View) backFromPluginsSettings() bool {
	s := &v.pluginsUI
	if s.editing {
		s.editing = false
		s.input = s.query
		v.State.Focus, v.reveal = pluginsSearchInput, true
		return true
	}
	if s.selectedInstalledID != "" || s.selectedCatalogRepo != "" {
		v.closePluginDetail()
		return true
	}
	if s.view == pluginsViewDiscover {
		if s.returnToThemePicker {
			v.returnToAppearanceThemePicker("")
			return true
		}
		s.view = pluginsViewInstalled
		v.State.Focus, v.reveal = pluginsDiscoverBtn, true
		return true
	}
	return false
}

func (v *View) closePluginDetail() {
	s := &v.pluginsUI
	focus := s.returnFocus
	if focus == "" {
		if s.selectedInstalledID != "" {
			focus = pluginsInstalledFocus(s.selectedInstalledID)
		} else if s.selectedCatalogRepo != "" {
			focus = pluginsCatalogFocus(s.selectedCatalogRepo)
		} else {
			focus = pluginsDiscoverBtn
		}
	}
	s.selectedInstalledID = ""
	s.selectedCatalogRepo = ""
	s.pendingRestoreScroll = s.listScroll > 0
	v.State.Focus, v.reveal = focus, true
}

func (v *View) installedPlugins() []app.InstalledPlugin {
	if v.PluginCatalog != nil {
		snap := v.PluginCatalog.Snapshot()
		if len(snap.Installed) > 0 {
			return snap.Installed
		}
	}
	if v.State == nil {
		return nil
	}
	entries := v.State.Entries()
	out := make([]app.InstalledPlugin, 0, len(entries))
	for _, e := range entries {
		out = append(out, app.InstalledPlugin{
			ID:          e.ID,
			Name:        e.Name,
			Description: e.Description,
			Kind:        plugin.SourceKind,
			Version:     v.Version,
			Bundled:     true,
			Enabled:     true,
		})
	}
	return out
}

type pluginsSettingsPage struct{}

func (pluginsSettingsPage) Render(c *ui.Context, p *PageContext) {
	v := p.host
	s := &v.pluginsUI
	if s.pendingRestoreScroll && v.activeScroll != nil {
		s.pendingRestoreScroll = false
		v.activeScroll.ScrollTo(0, s.listScroll, ui.Animation{})
	}
	if s.selectedInstalledID != "" {
		if inst, ok := p.findInstalledPlugin(s.selectedInstalledID); ok {
			p.renderInstalledPluginDetail(c, inst)
			return
		}
		s.selectedInstalledID = ""
	}
	if s.selectedCatalogRepo != "" && v.PluginCatalog != nil {
		if item, ok := v.PluginCatalog.FindCatalog(s.selectedCatalogRepo); ok {
			p.renderCatalogPluginDetail(c, item)
			return
		}
		s.selectedCatalogRepo = ""
	}
	if s.view == pluginsViewDiscover {
		p.renderPluginsDiscover(c)
		return
	}
	p.renderPluginsInstalled(c)
}

func (p *PageContext) findInstalledPlugin(id string) (app.InstalledPlugin, bool) {
	for _, item := range p.host.installedPlugins() {
		if strings.EqualFold(item.ID, id) {
			return item, true
		}
	}
	return app.InstalledPlugin{}, false
}

func (p *PageContext) renderPluginsInstalled(c *ui.Context) {
	v := p.host
	l, colors, d := p.Assets.Text.Plugins, p.Theme.Colors, p.uiColors()
	installed := v.installedPlugins()

	firstListFocus := pluginsCheckUpdatesBtn
	if len(installed) > 0 {
		firstListFocus = pluginsInstalledFocus(installed[0].ID)
	}
	p.region("plugins-toolbar", focusEdges{
		app.Left: p.left,
		app.Down: {Focus: firstListFocus},
	})
	p.region("content", focusEdges{
		app.Left: p.left,
		app.Up:   {Focus: pluginsDiscoverBtn},
	})

	ui.Row(c.Key("plugins-installed-head")).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(24).Margin(0, 0, 26, 0).Children(func() {
		ui.Column(c).Grow(1).Children(func() {
			ui.Text(c, l.InstalledTitle).FontSize(32).FontWeight(550).LineHeight(1.3)
			ui.Text(c, l.InstalledNote).FontSize(19).LineHeight(1.7).Margin(10, 0, 0, 0).TextColor(paint(colors.Text.Muted))
		})
		ui.Row(c).Gap(12).Shrink(0).AlignItems(ui.Center).Children(func() {
			btnStyle := buttonAppearance{
				height: 52, padding: 24, radius: 13, fontSize: 19, fontWeight: 500, iconSize: 20, iconGap: 10,
				focusShadow: true,
				background:  d.secondaryFill, foreground: paint(colors.Text.Primary), border: d.secondaryBorder,
			}
			p.styledButton(c, l.DiscoverButton, "search", pluginsDiscoverBtn, "plugins-toolbar", func() {
				v.pluginsUI.view = pluginsViewDiscover
				v.pluginsUI.returnToThemePicker = false
				v.pluginsUI.selectedInstalledID = ""
				v.pluginsUI.selectedCatalogRepo = ""
				if v.PluginCatalog != nil {
					v.PluginCatalog.EnsureLoaded()
				}
				p.State.Focus, v.reveal = pluginsFilterAll, true
			}, btnStyle)
			p.styledButton(c, l.AddRepoButton, "repository", pluginsAddRepoBtn, "plugins-toolbar", func() {
				v.openPluginRepoInput()
			}, btnStyle)
		})
	})

	ui.Box(c).FillWidth().Height(1).Background(d.subtleBorder)
	for _, item := range installed {
		inst := item
		focusID := pluginsInstalledFocus(inst.ID)
		note := p.installedPluginSubtitle(inst)
		status := p.installedPluginStatus(inst)
		var swatches *[6]theme.Color
		if inst.Kind == plugin.ThemeKind {
			sw := inst.Swatches
			swatches = &sw
		}
		p.pluginRowWithSwatches(c, focusID, "content", p.pluginKindIcon(inst.Kind), inst.Name, note, status, swatches, func() {
			if v.activeScroll != nil {
				v.pluginsUI.listScroll = v.activeScroll.Y
			}
			v.pluginsUI.returnFocus = focusID
			v.pluginsUI.selectedInstalledID = inst.ID
			v.pluginsUI.selectedCatalogRepo = ""
			p.State.Focus, v.reveal = pluginsDetailToggle, true
		})
	}
	checkLabel := l.CheckUpdates
	if v.PluginCatalog != nil && v.PluginCatalog.Snapshot().CheckingUpdates {
		checkLabel = l.CheckingUpdates
	}
	p.pluginRow(c, pluginsCheckUpdatesBtn, "content", "refresh", checkLabel, "", "", func() {
		v.checkPluginUpdates()
	})
}

func (p *PageContext) renderPluginsDiscover(c *ui.Context) {
	v := p.host
	s := &v.pluginsUI
	l, colors, d := p.Assets.Text.Plugins, p.Theme.Colors, p.uiColors()

	snap := app.PluginCatalogSnapshot{Status: app.PluginCatalogIdle}
	if v.PluginCatalog != nil {
		snap = v.PluginCatalog.Snapshot()
	}
	visible := snap.UninstalledItems(s.filter, s.query)
	hasQuery := strings.TrimSpace(s.query) != ""
	totalUninstalled := snap.TotalUninstalled()

	selectedTab := p.pluginFilterFocus(s.filter)
	var bodyFirstFocus app.FocusID
	switch {
	case snap.Status == app.PluginCatalogLoading && !snap.HasCache:
		bodyFirstFocus = ""
	case (snap.Status == app.PluginCatalogFailed || snap.Status == app.PluginCatalogOffline || snap.Status == app.PluginCatalogRateLimited) && !snap.HasCache:
		bodyFirstFocus = pluginsRetryBtn
	case totalUninstalled == 0 && !hasQuery:
		bodyFirstFocus = pluginsEmptyAddRepoBtn
	case len(visible) == 0:
		bodyFirstFocus = pluginsEmptyExitBtn
	default:
		bodyFirstFocus = pluginsCatalogFocus(visible[0].Repository)
	}

	p.restorePluginsDiscoverFocus(snap, visible, selectedTab, bodyFirstFocus)

	showTopRefresh := snap.Status != app.PluginCatalogLoading &&
		(snap.Status == app.PluginCatalogOffline || snap.Status == app.PluginCatalogRateLimited || snap.HasCache)

	topEdges := focusEdges{
		app.Left: p.left,
		app.Down: {Focus: pluginsSearchInput},
	}
	searchEdges := focusEdges{
		app.Left: p.left,
		app.Up:   {Focus: pluginsBackBtn},
		app.Down: {Focus: selectedTab},
	}
	tabEdges := focusEdges{
		app.Left: p.left,
		app.Up:   {Focus: pluginsSearchInput},
	}
	if bodyFirstFocus != "" {
		tabEdges[app.Down] = focusDestination{Focus: bodyFirstFocus}
	}
	contentEdges := focusEdges{
		app.Left: p.left,
		app.Up:   {Focus: selectedTab},
	}

	searchRegion := "plugins-search"
	if bodyFirstFocus == "" {
		searchRegion = "content"
		p.region("content", searchEdges)
	} else {
		p.region("plugins-search", searchEdges)
		p.region("content", contentEdges)
	}
	p.region("plugins-top", topEdges)
	p.region("plugins-tabs", tabEdges)

	// Breadcrumb row
	backLabel := l.BackInstalled
	if s.returnToThemePicker {
		backLabel = l.BackThemePicker
	}
	ui.Row(c.Key("plugins-discover-top")).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Margin(0, 0, 22, 0).Children(func() {
		p.pluginBackButton(c, backLabel, "plugins-top", func() {
			s.editing = false
			if s.returnToThemePicker {
				v.returnToAppearanceThemePicker("")
				return
			}
			s.view = pluginsViewInstalled
			p.State.Focus, v.reveal = pluginsDiscoverBtn, true
		})
		if showTopRefresh {
			refreshFocused := p.paintFocus() == pluginsRefreshTopBtn
			bg, ink := d.subtleFill, paint(colors.Text.Secondary)
			if snap.Status == app.PluginCatalogOffline || snap.Status == app.PluginCatalogRateLimited {
				ink = paint(colors.Status.Warning.Foreground)
			}
			if refreshFocused {
				bg, ink = paint(colors.Focus.Background), paint(colors.Focus.Foreground)
			}
			btn := ui.Button(c.Key(string(pluginsRefreshTopBtn)), "").Label(l.RefreshAction).
				Height(40).Padding(0, 16).Radius(10).Background(bg).Border(0, ui.Transparent).TextColor(ink).
				OnClick(func() {
					p.dispatch(func() {
						if v.PluginCatalog != nil {
							v.PluginCatalog.Refresh()
						}
					})
				})
			p.animateFocus(btn, bg, ink)
			p.bind(btn, pluginsRefreshTopBtn, "plugins-top")
			btn.Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Icon(c, p.Assets.Icon("refresh")).Size(18, 18)
					ui.Text(c, l.RefreshAction).FontSize(17)
				})
			})
		} else if snap.Status == app.PluginCatalogLoading && snap.HasCache {
			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				p.loadingSpinner(c, 22)
				ui.Text(c, l.LoadingTitle).FontSize(17).TextColor(paint(colors.Text.Muted))
			})
		}
	})

	// Heading + intro
	intro := l.DiscoverNote
	if snap.HasCache {
		switch snap.Status {
		case app.PluginCatalogOffline:
			intro = l.OfflineCachedNote
		case app.PluginCatalogRateLimited:
			intro = l.RateLimitedCachedNote
		}
	}
	ui.Text(c, l.DiscoverTitle).FontSize(32).FontWeight(550).LineHeight(1.3)
	ui.Text(c, intro).FontSize(19).LineHeight(1.7).Margin(10, 0, 22, 0).TextColor(paint(colors.Text.Muted))

	// Search input (F07 native tvOS TextInput)
	p.renderPluginSearchBox(c, searchRegion)

	// Kind filter tabs: 全部 / 内容源 / 主题
	p.renderPluginKindTabs(c)

	// Results / status body
	switch {
	case snap.Status == app.PluginCatalogLoading && !snap.HasCache:
		p.renderPluginStatusBox(c, "spinner", l.LoadingTitle, "", "", "", "", nil)
	case (snap.Status == app.PluginCatalogFailed || snap.Status == app.PluginCatalogOffline || snap.Status == app.PluginCatalogRateLimited) && !snap.HasCache:
		note := l.ErrorNote
		if snap.Status == app.PluginCatalogRateLimited {
			note = l.RateLimitErrorNote
		}
		p.renderPluginStatusBox(c, "wifi", l.ErrorTitle, note, l.ReloadAction, "refresh", pluginsRetryBtn, func() {
			if v.PluginCatalog != nil {
				v.PluginCatalog.Refresh()
			}
		})
	case totalUninstalled == 0 && !hasQuery:
		p.renderPluginStatusBox(c, "plug", l.EmptyTitle, l.EmptyNote, l.AddRepoButton, "repository", pluginsEmptyAddRepoBtn, func() {
			v.openPluginRepoInput()
		})
	case len(visible) == 0:
		if hasQuery {
			p.renderPluginStatusBox(c, "search", l.NoMatchTitle, l.NoMatchNote, l.ClearSearch, "", pluginsEmptyExitBtn, func() {
				s.query = ""
				s.input = ""
				p.State.Focus, v.reveal = pluginsSearchInput, true
			})
		} else if s.filter != "" {
			p.renderPluginStatusBox(c, "search", l.NoMoreTitle, l.NoMoreNote, l.ShowAll, "", pluginsEmptyExitBtn, func() {
				s.filter = ""
				p.State.Focus, v.reveal = pluginsFilterAll, true
			})
		} else {
			p.renderPluginStatusBox(c, "search", l.NoMoreTitle, l.NoMoreNote, l.BackInstalled, "", pluginsEmptyExitBtn, func() {
				s.view = pluginsViewInstalled
				p.State.Focus, v.reveal = pluginsDiscoverBtn, true
			})
		}
	default:
		ui.Box(c).FillWidth().Height(1).Background(d.subtleBorder)
		for _, entry := range visible {
			item := entry
			focusID := pluginsCatalogFocus(item.Repository)
			note := p.catalogPluginSubtitle(item)
			status := p.catalogPluginStatus(item)
			var swatches *[6]theme.Color
			if item.Manifest.Kind == plugin.ThemeKind {
				sw := item.Swatches
				swatches = &sw
			}
			p.pluginRowWithSwatches(c, focusID, "content", p.pluginKindIcon(item.Manifest.Kind), item.Manifest.Name, note, status, swatches, func() {
				if v.activeScroll != nil {
					s.listScroll = v.activeScroll.Y
				}
				s.returnFocus = focusID
				s.selectedInstalledID = ""
				s.selectedCatalogRepo = item.Repository
				if item.Compatibility.Compatible {
					p.State.Focus, v.reveal = pluginsDetailInstall, true
				} else {
					p.State.Focus, v.reveal = pluginsBackBtn, true
				}
			})
		}
		if snap.HasMore {
			label := l.LoadMoreAction
			if snap.LoadingMore {
				label = l.LoadingMore
			}
			p.pluginRow(c, pluginsLoadMoreBtn, "content", "refresh", label, "", "", func() {
				if v.PluginCatalog != nil {
					v.PluginCatalog.LoadMore()
				}
			})
		}
	}
}

func (p *PageContext) renderPluginSearchBox(c *ui.Context, region string) {
	v := p.host
	s := &v.pluginsUI
	l, colors, d := p.Assets.Text.Plugins, p.Theme.Colors, p.uiColors()

	if s.editing {
		ui.Row(c.Key("plugins-search-edit-box")).FillWidth().Height(64).Padding(0, 22).Gap(16).
			AlignItems(ui.Center).Radius(12).Background(paint(colors.Surface.Input)).
			Border(1, paint(colors.Focus.Ring)).Shadow(0, 0, 0, 3, paint(colors.Focus.Ring)).
			Margin(0, 0, 24, 0).Children(func() {
			ui.Icon(c, p.Assets.Icon("search")).Size(24, 24).TextColor(paint(colors.Text.Secondary))
			field := ui.TextInput(c.Key("plugins-search-input"), &s.input).Grow(1).MinWidth(0).Height(44).
				Padding(0, 0).Border(0, ui.Transparent).Background(ui.Transparent).
				FontSize(22).TextColor(paint(colors.Text.Primary)).
				Label(l.SearchPlaceholder).Placeholder(l.SearchPlaceholder).PlaceholderColor(paint(colors.Text.Muted)).
				MaxLength(pluginSearchMaxLength).ReturnKey(ui.ReturnSearch).
				OnChange(func() {
					s.query = strings.TrimSpace(s.input)
				}).
				OnTextKey(func(key ui.TextKey) bool {
					switch key.Key {
					case ui.KeyEnter:
						s.query = strings.TrimSpace(key.Text())
						s.input = s.query
						s.editing = false
						v.moveFocus(pluginsSearchInput, app.Left, false)
						return true
					case ui.KeyEscape:
						s.editing = false
						s.input = s.query
						v.moveFocus(pluginsSearchInput, app.Left, false)
						return true
					default:
						return false
					}
				})
			if p.active {
				field.FocusRing(false)
				field.FocusBind(&p.State.Focus, pluginsSearchEdit)
				field.Focus(ui.FocusOptions{PreventScroll: true})
			}
		})
		return
	}

	display := s.query
	ink := paint(colors.Text.Primary)
	if display == "" {
		display = l.SearchPlaceholder
		ink = paint(colors.Text.Muted)
	}
	focused := p.paintFocus() == pluginsSearchInput
	border := d.strongBorder
	if focused {
		border = paint(colors.Focus.Ring)
	}
	btn := ui.Button(c.Key(string(pluginsSearchInput)), "").Label(display).
		FillWidth().Height(64).Padding(0, 22).Radius(12).
		Background(paint(colors.Surface.Input)).Border(1, border).TextColor(ink).
		Margin(0, 0, 24, 0).
		OnClick(func() {
			p.dispatch(func() {
				s.input = s.query
				s.editing = true
				p.State.Focus = pluginsSearchEdit
			})
		})
	if focused {
		btn.Shadow(0, 0, 0, 3, paint(colors.Focus.Ring))
	}
	p.bind(btn, pluginsSearchInput, region)
	btn.Children(func() {
		ui.Row(c).Fill().Gap(16).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, p.Assets.Icon("search")).Size(24, 24).TextColor(paint(colors.Text.Secondary))
			ui.Text(c, display).Grow(1).MinWidth(0).SingleLine().FontSize(22).TextColor(ink)
		})
	})
}

func (p *PageContext) renderPluginKindTabs(c *ui.Context) {
	v := p.host
	s := &v.pluginsUI
	l, colors := p.Assets.Text.Plugins, p.Theme.Colors

	tabs := [...]struct {
		kind  plugin.Kind
		id    app.FocusID
		label string
	}{
		{"", pluginsFilterAll, l.FilterAll},
		{plugin.SourceKind, pluginsFilterSource, l.FilterSource},
		{plugin.ThemeKind, pluginsFilterTheme, l.FilterTheme},
	}

	ui.Row(c.Key("plugins-kind-tabs")).Gap(24).Margin(0, 0, 24, 0).AlignItems(ui.Center).Children(func() {
		for _, tab := range tabs {
			kindVal := tab.kind
			selected := s.filter == tab.kind
			focused := p.paintFocus() == tab.id
			bg, ink := ui.Transparent, paint(colors.Text.Secondary)
			if selected {
				ink = paint(colors.Accent.Soft)
			}
			if focused {
				bg, ink = paint(colors.Focus.Background), paint(colors.Focus.Foreground)
			}
			btn := ui.Button(c.Key(string(tab.id)), "").Label(tab.label).Height(48).Padding(0, 12).Radius(10).
				Background(bg).Border(0, ui.Transparent).TextColor(ink).
				AccessSelected(selected).OnClick(func() {
				p.dispatch(func() {
					s.filter = kindVal
				})
			})
			p.animateFocus(btn, bg, ink)
			p.bind(btn, tab.id, "plugins-tabs")
			btn.Children(func() {
				ui.Column(c).FillHeight().AlignItems(ui.Center).Justify(ui.Center).Gap(6).Children(func() {
					ui.Text(c, tab.label).FontSize(21).FontWeight(500)
					if selected && !focused {
						ui.Box(c).FillWidth().Height(3).Radius(1.5).Background(paint(colors.Accent.Primary))
					} else {
						ui.Box(c).FillWidth().Height(3).Background(ui.Transparent)
					}
				})
			})
		}
	})
}

func (p *PageContext) renderInstalledPluginDetail(c *ui.Context, item app.InstalledPlugin) {
	v := p.host
	l, colors, d := p.Assets.Text.Plugins, p.Theme.Colors, p.uiColors()

	p.region("plugins-top", focusEdges{
		app.Left: p.left,
		app.Down: {Focus: pluginsDetailToggle},
	})
	p.region("content", focusEdges{
		app.Left: p.left,
		app.Up:   {Focus: pluginsBackBtn},
	})

	ui.Row(c.Key("plugins-detail-top")).FillWidth().AlignItems(ui.Center).Margin(0, 0, 22, 0).Children(func() {
		p.pluginBackButton(c, l.BackManage, "plugins-top", v.closePluginDetail)
	})

	subtitle := p.pluginKindLabel(item.Kind)
	if desc := strings.TrimSpace(item.Description); desc != "" {
		subtitle += " \u00b7 " + desc
	}
	ui.Text(c, item.Name).FontSize(32).FontWeight(550).LineHeight(1.3)
	ui.Text(c, subtitle).FontSize(19).LineHeight(1.7).Margin(10, 0, 26, 0).TextColor(paint(colors.Text.Muted))
	ui.Box(c).FillWidth().Height(1).Background(d.subtleBorder)

	toggleIcon := "plug"
	toggleNote := l.EnableReadyNote
	if item.Kind == plugin.ThemeKind {
		toggleIcon = "palette"
		if item.Enabled {
			toggleNote = l.ThemeEnableActiveNote
		} else {
			toggleNote = l.ThemeEnableReadyNote
		}
	} else if !item.Enabled {
		toggleNote = l.EnableDisabledNote
	} else if p.State != nil && p.State.CurrentEntry == item.ID {
		toggleNote = l.EnableCurrentNote
	}
	instItem := item
	p.danmakuToggleRow(c, pluginsDetailToggle, toggleIcon, l.EnableLabel, toggleNote, item.Enabled, func() {
		if instItem.Enabled {
			v.openPluginDisableDialog(instItem)
		} else {
			v.enablePlugin(instItem)
		}
	})

	versionIcon := "info"
	versionNote := l.BuiltinUpdateNote
	if !item.Bundled {
		versionIcon = "refresh"
		switch {
		case v.PluginCatalog != nil && v.PluginCatalog.Snapshot().CheckingUpdates:
			versionNote = l.CheckingUpdates
		case item.AvailableUpdate != nil && item.AvailableUpdate.IncompatibleKind:
			versionNote = l.UpdateErrKindChanged
		case item.AvailableUpdate != nil && !item.AvailableUpdate.Item.Compatibility.Compatible:
			versionNote = p.compatibilityDetailNote(item.AvailableUpdate.Item.Compatibility)
		case item.AvailableUpdate != nil:
			versionNote = fmt.Sprintf(l.UpdateAvailableNote, item.AvailableUpdate.Item.Manifest.Version)
		default:
			versionNote = l.CheckSingleUpdateNote
		}
	}
	p.pluginRow(c, pluginsDetailVersion, "content", versionIcon, l.VersionLabel, versionNote, item.Version, func() {
		v.openPluginUpdateDialog(instItem)
	})

	repo := item.Repository
	if item.Bundled || repo == "" {
		repo = l.BuiltinRepo
	}
	p.pluginRow(c, pluginsDetailSource, "content", "repository", l.SourceLabel, "", repo, func() {})

	if !item.Bundled {
		removeNote := l.RemoveNote
		if item.Kind == plugin.ThemeKind {
			removeNote = l.ThemeRemoveNote
		}
		p.pluginRow(c, pluginsDetailRemove, "content", "trash", l.RemoveLabel, removeNote, "", func() {
			v.openPluginRemoveDialog(instItem)
		})
	}
}

func (p *PageContext) renderCatalogPluginDetail(c *ui.Context, item plugin.CatalogItem) {
	v := p.host
	l, colors, d := p.Assets.Text.Plugins, p.Theme.Colors, p.uiColors()

	downFocus := pluginsDetailVersion
	if item.Compatibility.Compatible {
		downFocus = pluginsDetailInstall
	}
	p.region("plugins-top", focusEdges{
		app.Left: p.left,
		app.Down: {Focus: downFocus},
	})
	p.region("content", focusEdges{
		app.Left: p.left,
		app.Up:   {Focus: pluginsBackBtn},
	})

	ui.Row(c.Key("plugins-catalog-detail-top")).FillWidth().AlignItems(ui.Center).Margin(0, 0, 22, 0).Children(func() {
		p.pluginBackButton(c, l.BackDiscover, "plugins-top", v.closePluginDetail)
	})

	desc := strings.TrimSpace(item.Manifest.Description)
	if desc == "" {
		desc = strings.TrimSpace(item.RepoDescription)
	}
	if !item.Compatibility.Compatible {
		desc = p.compatibilityDetailNote(item.Compatibility)
	}
	subtitle := p.pluginKindLabel(item.Manifest.Kind)
	if desc != "" {
		subtitle += " \u00b7 " + desc
	}
	ui.Text(c, item.Manifest.Name).FontSize(32).FontWeight(550).LineHeight(1.3)
	ui.Text(c, subtitle).FontSize(19).LineHeight(1.7).Margin(10, 0, 26, 0).TextColor(paint(colors.Text.Muted))
	ui.Box(c).FillWidth().Height(1).Background(d.subtleBorder)

	versionNote := ""
	if tag := strings.TrimSpace(item.Release.TagName); tag != "" && strings.TrimPrefix(tag, "v") != item.Manifest.Version {
		versionNote = tag
	}
	p.pluginRow(c, pluginsDetailVersion, "content", "info", l.VersionLabel, versionNote, item.Manifest.Version, func() {})
	p.pluginRow(c, pluginsDetailSource, "content", "repository", l.SourceLabel, "", item.Repository, func() {})

	compatIcon := "lock"
	compatValue := p.catalogPluginStatus(item)
	if item.Compatibility.Compatible {
		compatIcon = "sparkles"
		compatValue = l.CompatOK
	}
	p.pluginRow(c, pluginsDetailCompat, "content", compatIcon, l.CompatLabel, p.compatibilityDetailNote(item.Compatibility), compatValue, func() {})

	if item.Compatibility.Compatible {
		installNote := l.InstallNote
		if item.Manifest.Kind == plugin.ThemeKind {
			installNote = l.ThemeInstallNote
		}
		p.pluginRow(c, pluginsDetailInstall, "content", "plus", l.InstallLabel, installNote, "", func() {
			v.openPluginInstallConfirm(item)
		})
	}
}

func (p *PageContext) renderPluginStatusBox(c *ui.Context, iconName, title, note, btnLabel, btnIcon string, btnID app.FocusID, onAction func()) {
	colors, d := p.Theme.Colors, p.uiColors()
	ui.Column(c.Key("plugins-status-box")).FillWidth().Padding(64, 24).AlignItems(ui.Center).Gap(14).Children(func() {
		if iconName == "spinner" {
			p.loadingSpinner(c, 48)
		} else {
			ui.Icon(c, p.Assets.Icon(iconName)).Size(44, 44).TextColor(paint(colors.Accent.Soft))
		}
		ui.Text(c, title).FontSize(26).FontWeight(500).TextColor(paint(colors.Text.Primary))
		if note != "" {
			ui.Text(c, note).FontSize(18).LineHeight(1.6).TextColor(paint(colors.Text.Muted))
		}
		if btnLabel != "" && btnID != "" && onAction != nil {
			ui.Box(c).Height(10)
			p.styledButton(c, btnLabel, btnIcon, btnID, "content", onAction, buttonAppearance{
				height: 54, padding: 28, radius: 13, fontSize: 20, fontWeight: 500, iconSize: 20, iconGap: 10,
				focusShadow: true,
				background:  d.secondaryFill, foreground: paint(colors.Text.Primary), border: d.secondaryBorder,
			}).AlignSelf(ui.Center)
		}
	})
}

func (p *PageContext) pluginBackButton(c *ui.Context, label, region string, action func()) {
	colors := p.Theme.Colors
	backInk := paint(colors.Text.Secondary)
	if p.paintFocus() == pluginsBackBtn {
		backInk = paint(colors.Text.Primary)
	}
	btn := ui.Button(c.Key(string(pluginsBackBtn)), "").Label(label).Height(40).
		Padding(0, 4).Radius(8).Background(ui.Transparent).Border(0, ui.Transparent).TextColor(backInk).
		OnClick(func() { p.dispatch(action) })
	p.animateFocus(btn, ui.Transparent, backInk)
	p.bind(btn, pluginsBackBtn, region)
	btn.Children(func() {
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, p.Assets.Icon("back")).Size(20, 20)
			ui.Text(c, label).FontSize(19)
		})
	})
}

func (p *PageContext) pluginRow(c *ui.Context, id app.FocusID, region, iconName, label, note, value string, action func()) {
	p.pluginRowWithSwatches(c, id, region, iconName, label, note, value, nil, action)
}

func (p *PageContext) pluginRowWithSwatches(c *ui.Context, id app.FocusID, region, iconName, label, note, value string, swatches *[6]theme.Color, action func()) {
	colors, d := p.Theme.Colors, p.uiColors()
	focused := p.paintFocus() == id
	fill, ink, noteColor, iconColor, valColor := ui.Transparent, paint(colors.Text.Primary), paint(colors.Text.Muted), paint(colors.Text.Secondary), paint(colors.Text.Secondary)
	borderW, borderColor := float32(0), ui.Transparent
	if focused {
		fill, ink = paint(colors.Focus.Background), paint(colors.Focus.Foreground)
		noteColor, iconColor, valColor = d.focusSubtext, d.focusSubtext, d.focusSubtext
		if !p.Theme.Dark {
			borderW, borderColor = 1, d.focusBorder
		}
	}
	fullLabel := label
	if value != "" {
		fullLabel += " " + value
	}
	control := ui.Button(c.Key(string(id)), "").FillWidth().MinHeight(88).
		Radius(7).Border(borderW, borderColor).Padding(0).Label(fullLabel).
		OnClick(func() { p.dispatch(action) })
	p.animateFocus(control, fill, ink)
	p.applyFocusElevation(control, focused)
	p.bind(control, id, region)
	control.Children(func() {
		ui.Row(c).Fill().Padding(14, 20).Gap(20).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(36, 36).Shrink(0).Center().Children(func() {
				ui.Icon(c, p.Assets.Icon(iconName)).Size(24, 24).TextColor(iconColor)
			})
			ui.Column(c).Grow(1).MinWidth(0).Gap(6).Children(func() {
				ui.Text(c, label).FontSize(23).FontWeight(450).LineHeight(1.4).SingleLine()
				if note != "" {
					ui.Text(c, note).FontSize(16).LineHeight(1.6).TextColor(noteColor).SingleLine()
				}
			})
			if swatches != nil {
				p.renderThemeSwatches(c, *swatches, 76, 46, 8)
			}
			if value != "" {
				ui.Text(c, value).FontSize(21).Shrink(0).TextColor(valColor)
			}
		})
		if !focused || p.Theme.Dark {
			ui.Box(c).Absolute().Left(0).Right(0).Bottom(0).Height(1).Background(d.subtleBorder)
		}
	})
}

func (p *PageContext) restorePluginsDiscoverFocus(snap app.PluginCatalogSnapshot, visible []plugin.CatalogItem, selectedTab, bodyFirst app.FocusID) {
	if !p.active || p.host.pluginsUI.editing {
		return
	}
	fallback := bodyFirst
	if fallback == "" {
		fallback = selectedTab
	}
	focus := p.State.Focus
	switch {
	case focus == pluginsLoadMoreBtn && (!snap.HasMore || len(visible) == 0):
		if len(visible) > 0 {
			p.State.Focus = pluginsCatalogFocus(visible[len(visible)-1].Repository)
		} else {
			p.State.Focus = fallback
		}
	case focus == pluginsRetryBtn && bodyFirst != pluginsRetryBtn:
		p.State.Focus = fallback
	case focus == pluginsEmptyAddRepoBtn && bodyFirst != pluginsEmptyAddRepoBtn:
		p.State.Focus = fallback
	case focus == pluginsEmptyExitBtn && bodyFirst != pluginsEmptyExitBtn:
		p.State.Focus = fallback
	case strings.HasPrefix(string(focus), "settings/plugins/catalog/"):
		found := false
		for _, item := range visible {
			if pluginsCatalogFocus(item.Repository) == focus {
				found = true
				break
			}
		}
		if !found {
			p.State.Focus = fallback
		}
	}
}

func (p *PageContext) pluginFilterFocus(kind plugin.Kind) app.FocusID {
	switch kind {
	case plugin.SourceKind:
		return pluginsFilterSource
	case plugin.ThemeKind:
		return pluginsFilterTheme
	default:
		return pluginsFilterAll
	}
}

func (p *PageContext) pluginKindIcon(kind plugin.Kind) string {
	switch kind {
	case plugin.ThemeKind:
		return "palette"
	case plugin.SourceKind:
		return "tv"
	default:
		return "plug"
	}
}

func (p *PageContext) pluginKindLabel(kind plugin.Kind) string {
	l := p.Assets.Text.Plugins
	switch kind {
	case plugin.SourceKind:
		return l.KindSource
	case plugin.ThemeKind:
		return l.KindTheme
	default:
		return l.KindUnknown
	}
}

func (p *PageContext) installedPluginSubtitle(item app.InstalledPlugin) string {
	l := p.Assets.Text.Plugins
	parts := []string{p.pluginKindLabel(item.Kind)}
	if item.Bundled {
		parts = append(parts, l.BuiltinTag)
	} else if desc := strings.TrimSpace(item.Description); desc != "" {
		parts = append(parts, desc)
	}
	if item.Version != "" {
		parts = append(parts, item.Version)
	}
	if !item.Bundled && item.AvailableUpdate != nil && item.AvailableUpdate.Applicable() {
		parts = append(parts, l.UpdateAvailableTag)
	}
	return strings.Join(parts, " \u00b7 ")
}

func (p *PageContext) installedPluginStatus(item app.InstalledPlugin) string {
	l := p.Assets.Text.Plugins
	if item.Kind == plugin.ThemeKind {
		if item.Enabled {
			return l.StatusCurrentTheme
		}
		return l.StatusInstalled
	}
	if !item.Enabled {
		return l.StatusOff
	}
	if p.State != nil && item.Kind == plugin.SourceKind && p.State.CurrentEntry == item.ID {
		return l.StatusCurrentEntry
	}
	return l.StatusEnabled
}

func (p *PageContext) catalogPluginSubtitle(item plugin.CatalogItem) string {
	parts := []string{p.pluginKindLabel(item.Manifest.Kind)}
	desc := strings.TrimSpace(item.Manifest.Description)
	if desc == "" {
		desc = strings.TrimSpace(item.RepoDescription)
	}
	if desc == "" {
		desc = item.Repository
	}
	if desc != "" {
		parts = append(parts, desc)
	}
	if item.Manifest.Version != "" {
		parts = append(parts, item.Manifest.Version)
	}
	return strings.Join(parts, " \u00b7 ")
}

func (p *PageContext) catalogPluginStatus(item plugin.CatalogItem) string {
	l := p.Assets.Text.Plugins
	if item.Compatibility.Compatible {
		return l.StatusUninstalled
	}
	switch item.Compatibility.Code {
	case plugin.IncompatibleAppVersion:
		return l.StatusNeedAppUpdate
	case plugin.IncompatibleThemeUnavailable:
		return l.StatusThemeUnavailable
	default:
		return l.StatusIncompatible
	}
}

func (p *PageContext) compatibilityDetailNote(compat plugin.Compatibility) string {
	l := p.Assets.Text.Plugins
	if compat.Compatible {
		return l.CompatOKNote
	}
	switch compat.Code {
	case plugin.IncompatibleAppVersion:
		if compat.RequiredAppVersion != "" {
			return fmt.Sprintf(l.CompatNeedAppNote, compat.RequiredAppVersion)
		}
		return l.CompatProtocolNote
	case plugin.IncompatibleThemeUnavailable:
		return l.CompatThemeNote
	case plugin.IncompatibleProtocol:
		return l.CompatProtocolNote
	case plugin.IncompatibleAuth:
		return l.CompatAuthNote
	default:
		return l.CompatInvalidNote
	}
}
