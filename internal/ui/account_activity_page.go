package tvui

import (
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

func syncAccountOverview(v *View) {
	for _, section := range librarySections {
		if section == provider.AccountHistory && !v.Activity.HistorySync() {
			continue
		}
		v.Activity.Ensure(provider.AccountQuery{Section: section})
	}
	v.Activity.Ensure(provider.AccountQuery{Section: provider.AccountNotifications, Summary: true})
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
	if v.Activity != nil {
		v.Activity.Ensure(libraryQuery(v))
	}
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
	v.Activity.Ensure(messagesQuery(v))
	v.Activity.Ensure(provider.AccountQuery{Section: provider.AccountNotifications, Summary: true})
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

type privacySettingsPage struct{}

func (privacySettingsPage) Render(c *ui.Context, p *PageContext) {
	if !p.host.Activity.Available(provider.AccountHistory) {
		settingsEmptyPage{}.Render(c, p)
		return
	}
	l, colors, d := p.Assets.Text.Activity, p.Theme.Colors, p.uiColors()
	p.region("content", nil)
	ui.Text(c, p.Title).Height(45).FontSize(32).FontWeight(550).LineHeight(1.4).Margin(0, 0, 13, 0)
	ui.Text(c, l.PrivacyIntro).Height(32).FontSize(19).LineHeight(1.7).TextColor(paint(colors.Text.Muted)).Margin(0, 0, 27, 0)
	ui.Box(c).FillWidth().Height(1).Background(d.subtleBorder)
	enabled := p.host.Activity.HistorySync()
	status := l.Disabled
	if enabled {
		status = l.Enabled
	}
	fill, ink, note, icon := ui.Transparent, paint(colors.Text.Primary), paint(colors.Text.Muted), paint(colors.Text.Secondary)
	if p.State.Focus == app.HistorySync {
		fill, ink = paint(colors.Focus.Background), paint(colors.Focus.Foreground)
		note, icon = d.focusSubtext, d.focusSubtext
	}
	control := ui.Button(c.Key(string(app.HistorySync)), "").FillWidth().Height(110).
		Radius(7).Border(0, ui.Transparent).Padding(0).Role(ui.RoleSwitch).
		Label(l.HistorySync + " " + status).OnClick(func() {
		p.dispatch(func() {
			if err := p.host.Activity.ToggleHistorySync(); err != nil {
				p.host.notice = p.Assets.Text.SaveFailed
			}
		})
	})
	p.animateFocus(control, fill, ink)
	p.bind(control, app.HistorySync, "content")
	control.Children(func() {
		ui.Row(c).Fill().Padding(0, 20).Gap(20).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(36, 36).Shrink(0).Center().Children(func() {
				ui.Icon(c, p.Assets.Icon("history")).Size(24, 24).TextColor(icon)
			})
			ui.Column(c).Grow(1).Gap(9).Children(func() {
				ui.Text(c, l.HistorySync).FontSize(23).FontWeight(450).LineHeight(1.4)
				ui.Text(c, l.HistorySyncNote).FontSize(16).LineHeight(1.6).TextColor(note)
			})
			p.preferenceSwitch(c, enabled)
		})
		ui.Box(c).Absolute().Left(0).Right(0).Bottom(0).Height(1).Background(d.subtleBorder)
	})
}

func (p *PageContext) preferenceSwitch(c *ui.Context, enabled bool) {
	colors, d := p.Theme.Colors, p.uiColors()
	fill := d.switchOff
	if enabled {
		fill = paint(colors.Accent.Strong)
	}
	ui.Box(c).Size(49, 27).Shrink(0).Radius(18).Background(fill).Children(func() {
		dot := ui.Box(c).Absolute().Top(3).Size(21, 21).Radius(10.5).Background(paint(colors.Selected.FocusForeground))
		if enabled {
			dot.Right(3)
		} else {
			dot.Left(3)
		}
	})
}
