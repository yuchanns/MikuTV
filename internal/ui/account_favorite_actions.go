package tvui

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/provider"
)

const (
	favoriteFolderDialog  app.Page    = "favorite-folder-dialog"
	favoriteManage        app.FocusID = "favorites/manage"
	favoriteSelectAll     app.FocusID = "favorites/select-all"
	favoriteRemove        app.FocusID = "favorites/remove"
	favoriteDialogEdit    app.FocusID = "favorites/dialog/edit"
	favoriteDialogDelete  app.FocusID = "favorites/dialog/delete"
	favoriteDialogName    app.FocusID = "favorites/dialog/name"
	favoriteDialogInput   app.FocusID = "favorites/dialog/input"
	favoriteDialogPrivacy app.FocusID = "favorites/dialog/privacy"
	favoriteDialogSave    app.FocusID = "favorites/dialog/save"
	favoriteDialogRegion              = "favorite-dialog"
	favoriteNameRegion                = "favorite-dialog-name"
	favoritePrivacyRegion             = "favorite-dialog-privacy"
	favoriteDialogActions             = "favorite-dialog-actions"
)

type favoriteDialogMode uint8

const (
	favoriteCreate favoriteDialogMode = iota
	favoriteSettings
	favoriteEdit
	favoriteDelete
	favoriteConfirmRemove
)

type favoritesUI struct {
	management favoriteManagement
	dialog     favoriteDialog
}

type favoriteManagement struct {
	query   provider.AccountQuery
	picked  map[string]bool
	all     bool
	pending bool
	err     provider.ErrorCode
}

func (m favoriteManagement) selected(resource string) bool {
	return resource != "" && (m.all || m.picked[resource])
}

func (m favoriteManagement) resources(rows []provider.AccountRow) []string {
	var resources []string
	for _, row := range rows {
		if m.selected(row.RemovalID) {
			resources = append(resources, row.RemovalID)
		}
	}
	return resources
}

type favoriteDialog struct {
	mode                      favoriteDialogMode
	title, input, errorText   string
	private, editing, pending bool
	resources                 []string
	saved                     func()
}

func favoriteDialogPresentation(v *View) modalPresentation {
	switch v.favorites.dialog.mode {
	case favoriteDelete, favoriteConfirmRemove:
		return modalFrameless
	default:
		return modalDrawer
	}
}

func (p *PageContext) favoriteAction(c *ui.Context, label, icon string, id app.FocusID, region string, action func()) ui.Element {
	d := p.uiColors()
	style := buttonAppearance{height: 56, padding: 23, radius: 12, fontSize: 20, fontWeight: 550, iconSize: 21,
		background: d.secondaryFill, foreground: d.secondaryText}
	if region == favoriteHeader {
		style.iconSize, style.iconGap = 28, 13
		style.border = d.secondaryBorder
	}
	if region == favoriteDialogActions {
		style.padding, style.fontSize, style.iconSize, style.iconGap = 20, 19, 24, 13
		style.border = d.secondaryBorder
	}
	return p.styledButton(c, label, icon, id, region, action, style)
}

func (p *PageContext) canManageLibrary(query provider.AccountQuery) bool {
	activity := p.host.Activity
	switch query.Section {
	case provider.AccountFavorites:
		return activity.CanEditFavoriteFolders()
	default:
		return activity.CanRemoveAccountItems(query.Section)
	}
}

func libraryManageID(inFolder bool) app.FocusID {
	if inFolder {
		return favoriteManage
	}
	return app.ContentAction
}

func (p *PageContext) libraryHeaderActions(c *ui.Context, query provider.AccountQuery, rows []provider.AccountRow, managing bool) {
	text := p.Assets.Text.Activity
	if !p.canManageLibrary(query) {
		if query.Section == provider.AccountFavorites || len(rows) != 0 {
			activity := p.host.Activity
			p.favoriteAction(c, p.Assets.Text.Refresh, "", app.ContentAction, favoriteHeader, func() { activity.Refresh(query) })
		}
		return
	}
	directory := query.Section == provider.AccountFavorites && query.Folder == ""
	if directory {
		p.favoriteAction(c, text.CreateFolder, "plus", app.ContentAction, favoriteHeader, func() {
			p.openFavoriteDialog(favoriteDialog{mode: favoriteCreate}, favoriteDialogName)
		})
		return
	}
	if managing || len(rows) != 0 {
		label, icon := text.ManageFolder, "settings"
		if managing {
			label, icon = text.DoneManaging, "check"
		}
		p.favoriteAction(c, label, icon, libraryManageID(query.Folder != ""), favoriteHeader, func() {
			if p.host.favorites.management.pending {
				return
			}
			if managing {
				p.host.favorites.management = favoriteManagement{}
			} else {
				p.host.favorites.management = favoriteManagement{query: query, picked: make(map[string]bool)}
			}
		})
	}
	if query.Folder != "" {
		p.favoriteAction(c, text.FolderSettings, "folder", app.ContentAction, favoriteHeader, func() {
			p.openFavoriteDialog(favoriteDialog{mode: favoriteSettings}, favoriteDialogEdit)
		})
	}
}

func (p *PageContext) favoritePageStatus(c *ui.Context, query provider.AccountQuery, snapshot app.AccountSnapshot) {
	text := p.Assets.Text
	if snapshot.Phase == app.CatalogFailed || snapshot.Phase == app.CatalogCanceled {
		ui.Column(c).FillWidth().Padding(0, 83, 0, 103).Gap(15).Children(func() {
			if snapshot.Error != "" {
				ui.Text(c, p.authError(snapshot.Error)).FontSize(19).TextColor(paint(p.Theme.Colors.Text.Secondary))
			}
			p.favoriteAction(c, text.Retry, "", "favorites/retry", favoriteStatus, func() {
				p.State.Focus = app.ContentAction
				p.host.Activity.Retry(query)
			})
		})
		return
	}
	if snapshot.Phase == app.CatalogLoading && len(snapshot.Page.Rows) != 0 {
		ui.Text(c, text.LoadingTitle).Margin(0, 83, 0, 103).FontSize(19).TextColor(paint(p.Theme.Colors.Text.Secondary))
	}
}

func (p *PageContext) favoriteManagementBar(c *ui.Context, snapshot app.AccountSnapshot, top float32) {
	text := p.Assets.Text.Activity
	m := &p.host.favorites.management
	resources := m.resources(snapshot.Page.Rows)
	canRemove := !m.pending && len(resources) != 0 && snapshot.Phase == app.CatalogReady && (!m.all || snapshot.Page.NextCursor == "")
	all := text.SelectAll
	if m.all {
		all = text.ClearSelection
	}
	label := text.RemoveSelected
	if m.pending {
		label = text.SavingFolder
	}
	ui.Row(c).Absolute().Top(top).Left(103).Right(83).Height(58).Gap(18).AlignItems(ui.Center).Children(func() {
		ui.Text(c, fmt.Sprintf(text.SelectedFavorites, len(resources))).FontSize(21).TextColor(paint(p.Theme.Colors.Text.Secondary))
		if m.err != "" {
			ui.Text(c, p.authError(m.err)).FontSize(19).TextColor(paint(p.Theme.Colors.Status.Error.Foreground))
		}
		ui.Box(c).Grow(1)
		p.libraryManagementButton(c, all, "", favoriteSelectAll, func() {
			if m.pending {
				return
			}
			m.all = !m.all
			clear(m.picked)
		})
		if canRemove {
			p.libraryManagementButton(c, label, "trash", favoriteRemove, func() {
				if m.query.Section == provider.AccountFavorites {
					p.openFavoriteDialog(favoriteDialog{mode: favoriteConfirmRemove, resources: resources}, app.CloseModal)
				} else {
					p.removeLibraryItems(m.query, resources)
				}
			})
		} else {
			p.libraryManagementButton(c, label, "trash", favoriteRemove, nil)
		}
	})
}

func (p *PageContext) libraryManagementButton(c *ui.Context, label, icon string, id app.FocusID, action func()) {
	d := p.uiColors()
	style := buttonAppearance{height: 49, padding: 25, radius: 25, fontSize: 19, iconSize: 19, iconGap: 10,
		background: d.subtleFill, foreground: paint(p.Theme.Colors.Text.Secondary), border: d.subtleBorder}
	if action != nil {
		p.styledButton(c, label, icon, id, favoriteTools, action, style).AlignSelf(ui.Center)
		return
	}
	ui.Button(c.Key(string(id)), "").Label(label).Disabled(true).Opacity(.35).Height(style.height).Padding(0, style.padding).
		Radius(style.radius).Border(1, style.border).Background(style.background).TextColor(style.foreground).Children(func() {
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, p.Assets.Icon(icon)).Size(style.iconSize, style.iconSize)
			ui.Text(c, label).FontSize(style.fontSize)
		})
	})
}

func (p *PageContext) removeLibraryItems(query provider.AccountQuery, resources []string) {
	activity, management := p.host.Activity, &p.host.favorites.management
	management.err = ""
	done := func(completed int, err error) {
		if management.query != query {
			return
		}
		management.pending = false
		if err != nil {
			management.err = provider.CodeOf(err)
		}
		management.all = false
		clear(management.picked)
		for _, id := range resources[completed:] {
			management.picked[id] = true
		}
		if query.Section == provider.AccountTracking && completed > 0 {
			for _, id := range resources[:completed] {
				refID := id
				if !strings.HasPrefix(refID, "ss") {
					refID = "ss" + refID
				}
				p.host.syncSeriesTracked(provider.SeriesEntry{
					Item: provider.MediaItem{Ref: provider.MediaRef{EntryID: p.State.CurrentEntry, Kind: provider.Series, ID: refID}},
				}, false, false)
			}
		}
		if len(activity.Snapshot(query).Page.Rows) == 0 {
			*management = favoriteManagement{}
		}
		if p.State.Page == app.Library && libraryQuery(p.host) == query {
			p.dispatch(func() { p.State.Focus, p.host.reveal = app.ContentAction, true })
		}
	}
	management.pending = activity.RemoveAccountItems(query, resources, done)
	if management.pending {
		p.State.Focus = app.ContentAction
	} else {
		management.err = provider.Denied
	}
}

func (p *PageContext) toggleFavoriteSelection(resource string) {
	if resource == "" {
		return
	}
	m := &p.host.favorites.management
	if m.pending {
		return
	}
	if m.all {
		snapshot := p.host.Activity.Snapshot(m.query)
		for _, resource := range m.resources(snapshot.Page.Rows) {
			m.picked[resource] = true
		}
		m.all = false
	}
	if m.picked[resource] {
		delete(m.picked, resource)
	} else {
		m.picked[resource] = true
	}
}

func (p *PageContext) favoriteSelectionMark(c *ui.Context, selected bool) {
	colors := p.Theme.Colors
	fill, border := withAlpha(colors.Overlay.PlayerBottom, 0xb3), withAlpha(colors.Playback.Foreground, 0xbb)
	if selected {
		fill, border = paint(colors.Accent.Primary), paint(colors.Accent.Primary)
	}
	ui.Box(c).Absolute().Right(16).Top(16).Size(29, 29).Radius(15).Border(2, border).Background(fill).Center().Children(func() {
		if selected {
			ui.Icon(c, p.Assets.Icon("check")).Size(19, 19).TextColor(paint(colors.Accent.Foreground))
		}
	})
}

func (v *View) backFromLibraryManagement() bool {
	if v.favorites.management.query != libraryQuery(v) {
		return false
	}
	if v.favorites.management.pending {
		return true
	}
	v.favorites.management = favoriteManagement{}
	v.State.Focus, v.reveal = libraryManageID(v.State.Section == favoriteFolderSection), true
	return true
}

func (p *PageContext) openFavoriteDialog(dialog favoriteDialog, focus app.FocusID) {
	if p.State.Push(app.Route{Page: favoriteFolderDialog}) {
		if dialog.mode == favoriteCreate {
			dialog.private = true
		}
		p.host.favorites.dialog = dialog
		p.State.Focus = focus
	}
}

type favoriteDialogPage struct{}

func (favoriteDialogPage) Render(c *ui.Context, p *PageContext) {
	text := p.Assets.Text
	dialog := &p.host.favorites.dialog
	folder := p.host.Activity.Folder()
	p.region(favoriteDialogRegion, focusEdges{app.Down: {Focus: app.CloseModal}})
	p.region(favoriteNameRegion, focusEdges{app.Down: {Focus: favoriteDialogPrivacy}})
	p.region(favoritePrivacyRegion, focusEdges{app.Up: {Focus: favoriteDialogName}, app.Down: {Focus: favoriteDialogSave}})
	aboveActions := focusDestination{Region: favoriteDialogRegion}
	if dialog.mode == favoriteCreate || dialog.mode == favoriteEdit {
		aboveActions = focusDestination{Focus: favoriteDialogPrivacy}
	}
	p.region(favoriteDialogActions, focusEdges{app.Up: aboveActions})
	title := folder.Title
	switch dialog.mode {
	case favoriteCreate:
		title = text.Activity.CreateFolder
	case favoriteEdit:
		title = text.Activity.EditFolder
	case favoriteDelete:
		title = text.Activity.DeleteFolderTitle
	case favoriteConfirmRemove:
		title = text.Activity.RemoveSelected
	}
	if dialog.mode == favoriteDelete || dialog.mode == favoriteConfirmRemove {
		note := text.Activity.DeleteFolderNote
		if dialog.mode == favoriteConfirmRemove {
			note = fmt.Sprintf(text.Activity.RemoveFavoritesNote, len(dialog.resources))
		}
		ui.Column(c).FillWidth().Lang(p.langTag()).Font(p.uiFont()).Children(func() {
			p.framelessHeader(c, "COLLECTION \u00b7 CONFIRM", "trash", title, note)
			if dialog.errorText != "" {
				ui.Text(c, dialog.errorText).FillWidth().Margin(18, 0, 0, 0).
					FontSize(18).LineHeight(1.6).TextColor(paint(p.Theme.Colors.Status.Warning.Foreground))
			}
			ui.Box(c).Height(38)
			p.favoriteDialogButtons(c, dialog)
		})
		return
	}
	ui.Column(c).Fill().Lang(p.langTag()).Font(p.uiFont()).Children(func() {
		p.drawerHeader(c, "COLLECTION", title, "", "")
		p.favoriteDialogContent(c, dialog, folder)
		ui.Box(c).Height(28)
		p.favoriteDialogButtons(c, dialog)
		ui.Spacer(c).Grow(1)
		p.drawerFooter(c, text.Modal.HintSelect, text.Modal.HintSave)
	})
}

func (p *PageContext) favoriteDialogContent(c *ui.Context, dialog *favoriteDialog, folder provider.AccountRow) {
	text, colors := p.Assets.Text, p.Theme.Colors
	switch dialog.mode {
	case favoriteSettings:
		ui.Column(c).FillWidth().Margin(24, 0, 0, 0).Gap(6).Children(func() {
			p.favoriteFolderOption(c, text.Activity.EditFolder, "settings", favoriteDialogEdit, func() {
				*dialog = favoriteDialog{mode: favoriteEdit, title: folder.Title, private: folder.Private != nil && *folder.Private}
				p.State.Focus = favoriteDialogName
			})
			if folder.CanDelete {
				p.favoriteFolderOption(c, text.Activity.DeleteFolder, "trash", favoriteDialogDelete, func() {
					dialog.mode = favoriteDelete
					p.State.Focus = app.CloseModal
				})
			}
		})
	case favoriteCreate, favoriteEdit:
		p.favoriteNameFields(c, dialog)
	case favoriteDelete, favoriteConfirmRemove:
		note := text.Activity.DeleteFolderNote
		if dialog.mode == favoriteConfirmRemove {
			note = fmt.Sprintf(text.Activity.RemoveFavoritesNote, len(dialog.resources))
		}
		ui.Text(c, note).Margin(21, 0, 0, 0).FontSize(21).TextColor(paint(colors.Text.Secondary)).LineHeight(1.8)
	}
	errorHeight := float32(28)
	if dialog.errorText != "" {
		errorHeight = 32
	}
	if dialog.mode == favoriteCreate || dialog.mode == favoriteEdit || dialog.errorText != "" {
		ui.Text(c, dialog.errorText).FillWidth().Height(errorHeight).Margin(15, 0, 0, 0).
			FontSize(18).LineHeight(1.8).TextColor(paint(colors.Status.Warning.Foreground))
	}
}

func (p *PageContext) favoriteFolderOption(c *ui.Context, label, icon string, id app.FocusID, action func()) {
	colors := p.Theme.Colors
	fill, ink := ui.Transparent, paint(colors.Text.Secondary)
	if p.State.Focus == id {
		fill, ink = paint(colors.Focus.Background), paint(colors.Focus.Foreground)
	}
	button := ui.Button(c.Key(string(id)), "").Label(label).FillWidth().Height(62).
		Radius(10).Border(0, ui.Transparent).Padding(0).OnClick(func() { p.dispatch(action) })
	p.animateFocus(button, fill, ink)
	p.bind(button, id, favoriteDialogRegion)
	button.Children(func() {
		ui.Row(c).Fill().Padding(0, 18).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, p.Assets.Icon(icon)).Size(23, 23)
			ui.Spacer(c).Grow(1)
			ui.Text(c, label).FontSize(23)
		})
	})
}

func (p *PageContext) favoriteDialogButtons(c *ui.Context, dialog *favoriteDialog) {
	text := p.Assets.Text
	ui.Row(c).Gap(6).Children(func() {
		if dialog.mode != favoriteSettings {
			label, icon := text.Activity.SaveFolder, ""
			switch dialog.mode {
			case favoriteDelete:
				label, icon = text.Activity.DeleteFolder, "trash"
			case favoriteConfirmRemove:
				label, icon = text.Activity.ConfirmRemove, "trash"
			}
			if dialog.pending {
				label = text.Activity.SavingFolder
			}
			p.favoriteAction(c, label, icon, favoriteDialogSave, favoriteDialogActions, p.saveFavoriteDialog).
				Disabled(dialog.pending || dialog.editing)
		}
		label := text.Activity.CancelFolder
		if dialog.mode == favoriteSettings {
			label = text.Close
		}
		p.favoriteAction(c, label, "", app.CloseModal, favoriteDialogActions, func() { p.State.Back() }).Disabled(dialog.pending)
	})
}

func (p *PageContext) favoriteNameFields(c *ui.Context, dialog *favoriteDialog) {
	text, colors, d := p.Assets.Text.Activity, p.Theme.Colors, p.uiColors()
	ui.Text(c, text.FolderName).Height(28).Margin(28, 0, 15, 0).FontSize(20).LineHeight(1.4).
		TextColor(paint(colors.Text.Secondary))
	if dialog.editing {
		field := ui.TextInput(c.Key("favorite-folder-name"), &dialog.input).FillWidth().Height(71).
			Radius(12).Padding(0, 22).Background(paint(colors.Surface.Input)).
			Border(1, d.strongBorder).TextColor(paint(colors.Text.Primary)).
			Label(text.FolderName).Placeholder(text.FolderNamePlaceholder).PlaceholderColor(paint(colors.Text.Muted)).
			MaxLength(provider.FavoriteFolderTitleLimit).
			FontSize(25).ReturnKey(ui.ReturnDone).OnTextKey(func(key ui.TextKey) bool {
			switch key.Key {
			case ui.KeyEnter:
				dialog.title = key.Text()
			case ui.KeyEscape:
			default:
				return false
			}
			dialog.editing = false
			dialog.errorText = ""
			p.State.Focus = favoriteDialogName
			return true
		})
		if p.active {
			field.FocusRing(false).Shadow(0, 0, 0, 2, paint(colors.Focus.Ring))
			field.FocusBind(&p.State.Focus, favoriteDialogInput)
			field.Focus(ui.FocusOptions{PreventScroll: true})
		}
	} else {
		name, ink := dialog.title, paint(colors.Text.Primary)
		if name == "" {
			name, ink = text.FolderNamePlaceholder, paint(colors.Text.Muted)
		}
		field := ui.Button(c.Key(string(favoriteDialogName)), "").Label(name).FillWidth().Height(71).
			Radius(12).Padding(0).Background(paint(colors.Surface.Input)).Border(1, d.strongBorder).
			TextColor(ink).Disabled(dialog.pending).OnClick(func() {
			p.dispatch(func() {
				dialog.input, dialog.editing = dialog.title, true
				p.State.Focus = favoriteDialogInput
			})
		})
		if p.State.Focus == favoriteDialogName {
			field.Shadow(0, 0, 0, 2, paint(colors.Focus.Ring))
		}
		p.bind(field, favoriteDialogName, favoriteNameRegion)
		field.Children(func() {
			ui.Row(c).Fill().Padding(0, 22).AlignItems(ui.Center).Children(func() {
				ui.Text(c, name).SingleLine().FontSize(25).TextColor(ink)
			})
		})
	}
	fill, ink := ui.Transparent, paint(colors.Text.Primary)
	if p.State.Focus == favoriteDialogPrivacy {
		fill, ink = paint(colors.Focus.Background), paint(colors.Focus.Foreground)
	}
	status := text.FolderPublic
	if dialog.private {
		status = text.FolderPrivate
	}
	toggle := ui.Button(c.Key(string(favoriteDialogPrivacy)), "").Label(text.PrivateFolder+" "+status).
		Role(ui.RoleSwitch).Checked(dialog.private).FillWidth().Margin(25, 0, 0, 0).Height(70).Radius(10).
		Border(0, ui.Transparent).Padding(0).Disabled(dialog.pending).
		OnClick(func() { p.dispatch(func() { dialog.private = !dialog.private }) })
	p.animateFocus(toggle, fill, ink)
	p.bind(toggle, favoriteDialogPrivacy, favoritePrivacyRegion)
	toggle.Children(func() {
		ui.Row(c).Fill().Padding(0, 15).Gap(14).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, p.Assets.Icon("lock")).Size(22, 22)
			ui.Text(c, text.PrivateFolder).FontSize(22)
			ui.Spacer(c).Grow(1)
			p.preferenceSwitch(c, dialog.private)
		})
	})
}

func (p *PageContext) saveFavoriteDialog() {
	view, activity := p.host, p.host.Activity
	dialog := &view.favorites.dialog
	if dialog.pending || dialog.editing {
		return
	}
	folder := activity.Folder()
	edit := provider.FavoriteFolderEdit{Folder: folder.ID, Title: strings.TrimSpace(dialog.title), Private: dialog.private}
	switch dialog.mode {
	case favoriteCreate:
		edit.Action, edit.Folder = provider.FavoriteFolderCreate, ""
	case favoriteEdit:
		edit.Action = provider.FavoriteFolderUpdate
	case favoriteDelete:
		edit.Action = provider.FavoriteFolderDelete
	case favoriteConfirmRemove:
		edit.Action, edit.Resources = provider.FavoriteFolderRemove, dialog.resources
	}
	if dialog.mode == favoriteCreate || dialog.mode == favoriteEdit {
		if edit.Title == "" {
			dialog.errorText = p.Assets.Text.Activity.FolderNameRequired
			p.State.Focus = favoriteDialogName
			return
		}
		directory := activity.Snapshot(provider.AccountQuery{Section: provider.AccountFavorites})
		for _, existing := range directory.Page.Rows {
			if existing.ID != edit.Folder && strings.EqualFold(existing.Title, edit.Title) {
				dialog.errorText = p.Assets.Text.Activity.FolderDuplicate
				p.State.Focus = favoriteDialogName
				return
			}
		}
	}
	dialog.errorText = ""
	dialog.pending = activity.EditFavoriteFolder(edit, func(completed int, err error) {
		dialog.pending = false
		if err != nil {
			dialog.errorText = p.authError(provider.CodeOf(err))
			dialog.resources = dialog.resources[completed:]
			return
		}
		view.dispatch(func() {
			p.State.Back()
			if dialog.saved != nil {
				dialog.saved()
			}
			if edit.Action == provider.FavoriteFolderDelete {
				p.State.Back()
				p.State.Focus = app.ContentAction
			}
			if edit.Action == provider.FavoriteFolderRemove {
				view.favorites.management = favoriteManagement{}
				p.State.Focus = app.ContentAction
				view.reveal = true
			}
		})
	})
	if !dialog.pending {
		dialog.errorText = p.Assets.Text.FailedTitle
	}
}
