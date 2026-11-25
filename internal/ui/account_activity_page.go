package tvui

import (
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/provider"
)

const favoriteFolderSection = "folder"

var librarySections = [...]provider.AccountSection{
	provider.AccountHistory, provider.AccountFavorites, provider.AccountWatchLater,
}

var libraryTabs = [...]provider.AccountSection{
	provider.AccountHistory, provider.AccountWatchLater, provider.AccountFavorites, provider.AccountTracking, provider.AccountCourses,
}

var noticeFilters = [...]provider.NoticeFilter{
	provider.NoticeAll, provider.NoticeReply, provider.NoticeLike, provider.NoticeSystem,
}

func (l accountActivityLabels) section(section provider.AccountSection) (string, string) {
	switch section {
	case provider.AccountHistory:
		return l.History, "history"
	case provider.AccountFavorites:
		return l.Favorites, "star"
	case provider.AccountWatchLater:
		return l.WatchLater, "library"
	default:
		return l.Messages, "comment"
	}
}

func (l accountActivityLabels) libraryTab(section provider.AccountSection) string {
	switch section {
	case provider.AccountHistory:
		return l.LibraryHistory
	case provider.AccountTracking:
		return l.Tracking
	case provider.AccountCourses:
		return l.Courses
	default:
		label, _ := l.section(section)
		return label
	}
}

func (l accountActivityLabels) filter(filter provider.NoticeFilter) string {
	switch filter {
	case provider.NoticeReply:
		return l.Replies
	case provider.NoticeLike:
		return l.Likes
	case provider.NoticeSystem:
		return l.System
	default:
		return l.All
	}
}

func libraryRoute(section provider.AccountSection) app.Route {
	return app.Route{Page: app.Library, Section: string(section)}
}

func (v *View) selectLibraryTab(next app.FocusID) {
	raw, ok := strings.CutPrefix(string(next), "library/tab/")
	if !ok || raw == "" {
		return
	}
	section := provider.AccountSection(raw)
	for _, candidate := range libraryTabs {
		if candidate == section {
			if v.State.Navigate(libraryRoute(section)) {
				v.favorites.management = favoriteManagement{}
				v.State.Focus, v.State.Area, v.reveal = next, app.ContentArea, true
			}
			return
		}
	}
}

func syncAccountOverview(v *View) {
	for _, section := range librarySections {
		if section == provider.AccountHistory && !v.Activity.HistorySync() {
			continue
		}
		v.Activity.Ensure(provider.AccountQuery{Section: section})
	}
	v.Activity.Ensure(provider.AccountQuery{Section: provider.AccountNotifications, Summary: true})
	if v.PrivateMessages != nil && v.PrivateMessages.Available() {
		v.PrivateMessages.EnsureSummary()
	}
}

func libraryQuery(v *View) provider.AccountQuery {
	return libraryQueryFor(v, v.State.Section)
}

func libraryQueryFor(v *View, section string) provider.AccountQuery {
	if section == favoriteFolderSection {
		if v.Activity != nil {
			return provider.AccountQuery{Section: provider.AccountFavorites, Folder: v.Activity.Folder().ID}
		}
		return provider.AccountQuery{Section: provider.AccountFavorites}
	}
	if section != "" {
		return provider.AccountQuery{Section: provider.AccountSection(section)}
	}
	if v.State != nil {
		if entry, ok := v.State.Current(); ok {
			for _, candidate := range libraryTabs {
				if entry.Capabilities.Has(candidate.Capability()) {
					return provider.AccountQuery{Section: candidate}
				}
			}
		}
	}
	return provider.AccountQuery{Section: provider.AccountHistory}
}

func syncLibrary(v *View) {
	if v.Activity == nil {
		return
	}
	query := libraryQuery(v)
	if query.Section == provider.AccountHistory {
		v.Activity.RefreshHistoryIfDirty(query)
		return
	}
	v.Activity.Ensure(query)
}

func pauseLibraryHistory(v *View) {
	if v.Activity != nil {
		v.Activity.PauseHistoryViews()
	}
}

func messagesQuery(v *View) provider.AccountQuery {
	return provider.AccountQuery{Section: provider.AccountNotifications, Filter: v.Activity.Filter()}
}

func syncAccountMessages(v *View) {
	hasNotifications := v.Activity != nil && v.Activity.Available(provider.AccountNotifications)
	hasPrivate := v.PrivateMessages != nil && v.PrivateMessages.Available()
	if hasNotifications {
		v.Activity.Ensure(messagesQuery(v))
		v.Activity.Ensure(provider.AccountQuery{Section: provider.AccountNotifications, Summary: true})
	}
	if hasPrivate {
		v.PrivateMessages.EnsureSummary()
		if v.messagesTab == messagesTabPrivate || !hasNotifications {
			v.messagesTab = messagesTabPrivate
			v.PrivateMessages.EnsureConversations()
		}
	}
}

type libraryPage struct{}

func (libraryPage) Render(c *ui.Context, p *PageContext) {
	if p.Location.Section != "" {
		p.Outlet(c)
		return
	}
	p.renderLibrary(c, libraryQueryFor(p.host, p.Location.Section))
}

type libraryContent struct{}

func (libraryContent) Render(c *ui.Context, p *PageContext) {
	p.renderLibrary(c, libraryQueryFor(p.host, p.Location.Section))
}

func (p *PageContext) preferenceSwitch(c *ui.Context, enabled bool) {
	colors, d := p.Theme.Colors, p.uiColors()
	fill := d.switchOff
	if enabled {
		fill = paint(colors.Accent.Strong)
	}
	ui.Box(c).Size(49, 27).Shrink(0).Radius(18).Background(fill).
		Border(1, withAlpha(colors.Selected.FocusForeground, 0x38)).Children(func() {
		dot := ui.Box(c).Absolute().Top(3).Size(21, 21).Radius(10.5).Background(paint(colors.Selected.FocusForeground))
		if enabled {
			dot.Right(3)
		} else {
			dot.Left(3)
		}
	})
}
