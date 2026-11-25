package tvui

import (
	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/provider"
)

type privacyClearKind uint8

const (
	privacyClearNone privacyClearKind = iota
	privacyClearKindSearch
	privacyClearKindHistory
)

const (
	privacyClearSearch  app.FocusID = "settings/privacy/clear-search"
	privacyClearHistory app.FocusID = "settings/privacy/clear-history"
	privacyConfirmAccept app.FocusID = "privacy/confirm-clear"
	privacyManageAction  app.FocusID = "privacy/manage-settings"
)

type privacySettingsUI struct {
	confirmKind  privacyClearKind
	confirmEntry string
	confirmOwner string
	confirmError string
}

func (v *View) currentPrivacyScope() (app.Entry, string) {
	entry, _ := v.State.Current()
	owner := ""
	if v.Auth != nil {
		owner = v.Auth.Accounts().CurrentID
	}
	return entry, owner
}

func (v *View) hasLocalSearchHistory(entryID, owner string) bool {
	if entryID == "" {
		return false
	}
	if v.Search != nil && v.Search.MatchesScope(entryID, owner) && len(v.Search.Snapshot().History) > 0 {
		return true
	}
	if store, ok := v.Preferences.(app.SearchHistoryStore); ok {
		return len(store.SearchHistory(entryID, owner)) > 0
	}
	return false
}

func (v *View) privacyRowAvailability() (showSync, showSearch, showHistory bool) {
	entry, owner := v.currentPrivacyScope()
	if entry.ID == "" {
		return false, false, false
	}
	showSync = v.Activity != nil && v.Activity.Available(provider.AccountHistory)
	showSearch = entry.Capabilities.Has(provider.SearchCatalog) || v.hasLocalSearchHistory(entry.ID, owner)
	showHistory = entry.Capabilities.Has(provider.ResolvePlayback) || entry.Capabilities.Has(provider.AccountHistory.Capability())
	return showSync, showSearch, showHistory
}

func (v *View) firstPrivacyFocus() app.FocusID {
	showSync, showSearch, showHistory := v.privacyRowAvailability()
	switch {
	case showSync:
		return app.HistorySync
	case showSearch:
		return privacyClearSearch
	case showHistory:
		return privacyClearHistory
	default:
		return app.SectionFocus("privacy")
	}
}

func (v *View) openPrivacyConfirm(kind privacyClearKind) {
	entry, owner := v.currentPrivacyScope()
	v.privacyUI = privacySettingsUI{
		confirmKind:  kind,
		confirmEntry: entry.ID,
		confirmOwner: owner,
	}
	v.State.Push(app.Route{Page: app.PrivacyConfirm, Section: "privacy"})
}

type privacySettingsPage struct{}

func (privacySettingsPage) Render(c *ui.Context, p *PageContext) {
	v := p.host
	showSync, showSearch, showHistory := v.privacyRowAvailability()
	if !showSync && !showSearch && !showHistory {
		settingsEmptyPage{}.Render(c, p)
		return
	}
	act, priv := p.Assets.Text.Activity, p.Assets.Text.Privacy
	p.region("content", nil)
	p.settingsSectionHeader(c, p.Title, act.PrivacyIntro)

	if showSync {
		enabled := v.Activity.HistorySync()
		p.settingsToggleRow(c, app.HistorySync, "history", act.HistorySync, act.HistorySyncNote, enabled, func() {
			if err := v.Activity.ToggleHistorySync(); err != nil {
				v.showSettingsFeedback(priv.SyncSaveFailed)
				return
			}
			v.SyncTopShelf()
		})
	}
	if showSearch {
		p.settingsValueRow(c, privacyClearSearch, "trash", priv.ClearSearch, "", "", func() {
			v.openPrivacyConfirm(privacyClearKindSearch)
		})
	}
	if showHistory {
		p.settingsValueRow(c, privacyClearHistory, "trash", priv.ClearHistory, "", "", func() {
			v.openPrivacyConfirm(privacyClearKindHistory)
		})
	}
}

type privacyConfirmPage struct{}

func (privacyConfirmPage) Render(c *ui.Context, p *PageContext) {
	v := p.host
	priv, colors := p.Assets.Text.Privacy, p.Theme.Colors
	title, body := priv.ClearSearchTitle, priv.ClearSearchBody
	if v.privacyUI.confirmKind == privacyClearKindHistory {
		title, body = priv.ClearHistoryTitle, priv.ClearHistoryBody
	}
	p.region("privacy-confirm", nil)
	ui.Column(c).FillWidth().Lang(p.langTag()).Font(p.uiFont()).Children(func() {
		p.framelessHeader(c, "PRIVACY \u00b7 CONFIRM", "trash", title, body)
		if v.privacyUI.confirmError != "" {
			ui.Text(c, v.privacyUI.confirmError).FillWidth().Margin(18, 0, 0, 0).
				FontSize(18).LineHeight(1.6).TextColor(paint(colors.Status.Warning.Foreground))
		}
		ui.Row(c).Margin(40, 0, 0, 0).Gap(16).Children(func() {
			p.styledButton(c, priv.ConfirmClear, "", privacyConfirmAccept, "privacy-confirm", func() {
				entry, owner := v.currentPrivacyScope()
				if entry.ID == "" || entry.ID != v.privacyUI.confirmEntry || owner != v.privacyUI.confirmOwner {
					p.State.Back()
					return
				}
				var err error
				switch v.privacyUI.confirmKind {
				case privacyClearKindSearch:
					store, _ := v.Preferences.(app.SearchHistoryStore)
					err = app.ClearLocalSearchHistory(store, v.Search, entry.ID, owner)
				case privacyClearKindHistory:
					cleaner, _ := v.Preferences.(app.LocalPlaybackCleaner)
					err = app.ClearLocalWatchHistory(cleaner, v.Activity, v.Playback, entry.ID, owner)
				}
				if err != nil {
					v.privacyUI.confirmError = priv.ClearFailed
					return
				}
				clearedKind := v.privacyUI.confirmKind
				p.State.Back()
				if clearedKind == privacyClearKindSearch && !entry.Capabilities.Has(provider.SearchCatalog) {
					p.State.Focus, v.reveal = v.firstPrivacyFocus(), true
				}
				if clearedKind == privacyClearKindHistory {
					v.SyncTopShelf()
				}
				v.showSettingsFeedback(priv.Cleared)
			}, p.framelessButtonAppearance())
			p.styledButton(c, p.Assets.Text.Cancel, "", app.CloseModal, "privacy-confirm", func() {
				p.State.Back()
			}, p.framelessButtonAppearance())
		})
	})
}

type privacyDetailsPage struct{}

func (privacyDetailsPage) Render(c *ui.Context, p *PageContext) {
	v := p.host
	priv := p.Assets.Text.Privacy
	canManage := v.settingsSectionAvailable("privacy")
	if p.active && !canManage && p.State.Focus == privacyManageAction {
		p.State.Focus, v.reveal = app.CloseModal, true
	}
	p.region("privacy-details", nil)
	ui.Column(c).FillWidth().Lang(p.langTag()).Font(p.uiFont()).Children(func() {
		p.framelessHeader(c, "PRIVACY \u00b7 DATA", "lock", priv.DetailsTitle, priv.DetailsBody)
		ui.Row(c).Margin(40, 0, 0, 0).Gap(16).Children(func() {
			if canManage {
				p.styledButton(c, priv.ManageAction, "", privacyManageAction, "privacy-details", func() {
					p.State.Back()
					p.State.Navigate(app.Route{Page: app.Settings, Section: "privacy"})
					p.State.Focus, p.State.Area, v.reveal = v.firstPrivacyFocus(), app.ContentArea, true
				}, p.framelessButtonAppearance())
			}
			p.styledButton(c, p.Assets.Text.Close, "", app.CloseModal, "privacy-details", func() {
				p.State.Back()
			}, p.framelessButtonAppearance())
		})
	})
}
