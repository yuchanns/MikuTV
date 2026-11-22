package tvui

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/provider"
)

const (
	seriesCalendarBackFocus    app.FocusID = "series-calendar/back"
	seriesCalendarRetryFocus   app.FocusID = "series-calendar/retry"
	seriesCalendarCancelFocus  app.FocusID = "series-calendar/cancel"
	seriesCalendarToIndexFocus app.FocusID = "series-calendar/to-index"

	seriesCalendarHeaderRegion = "series-calendar/header"
	seriesCalendarDaysRegion   = "series-calendar/days"
	seriesCalendarListRegion   = "series-calendar/list"
	seriesCalendarStatusRegion = "series-calendar/status"
)

func seriesCalendarDayFocus(index int) app.FocusID {
	return app.FocusID("series-calendar/day/" + strconv.Itoa(index))
}

func seriesCalendarShowFocus(ref provider.MediaRef) app.FocusID {
	return app.FocusID("series-calendar/show/" + string(ref.Kind) + "/" + url.PathEscape(ref.ID))
}

func seriesCalendarTrackFocus(ref provider.MediaRef) app.FocusID {
	return app.FocusID("series-calendar/track/" + string(ref.Kind) + "/" + url.PathEscape(ref.ID))
}

func syncSeriesCalendar(v *View) {
	if v.Series != nil {
		v.Series.EnsureCalendar()
	}
}

func (v *View) backFromSeriesCalendar() bool {
	scroll := v.activeScroll
	if v.State.Area != app.ContentArea || scroll == nil {
		return false
	}
	if scroll.Y > 0 {
		scroll.ScrollTo(0, 0, homeReturnScroll)
		selected := 0
		if v.Series != nil {
			selected = v.Series.Calendar().Selected
		}
		v.moveFocus(seriesCalendarDayFocus(selected), app.Up, false)
		return true
	}
	return false
}

type seriesCalendarPage struct{}

func (seriesCalendarPage) Render(c *ui.Context, p *PageContext) {
	catalog := p.host.Series
	if catalog == nil {
		return
	}
	snap := catalog.Calendar()
	p.restoreSeriesCalendarFocus(snap)
	p.prepareSeriesCalendarArtwork(snap)
	p.seriesCalendarRegions(snap)

	text, colors := p.Assets.Text, p.Theme.Colors
	ui.Column(c.Key("series-calendar-page")).FillWidth().Padding(seriesHeaderHeight, 0, screenBottomInset, 0).
		Lang("zh-CN").Font("system-ui, PingFang SC").Children(func() {
		p.seriesSubpageHeading(c, seriesCalendarBackFocus, seriesCalendarHeaderRegion, text.Series.Calendar, text.Series.CalendarNote)
		switch {
		case snap.Phase == app.CatalogIdle || (snap.Phase == app.CatalogLoading && !snap.Loaded):
			p.seriesStateBlock(c, "history", text.Series.Loading, "", text.Cancel, "", seriesCalendarCancelFocus, seriesCalendarStatusRegion, func() {
				catalog.CancelCalendar()
			})
			return
		case snap.Phase == app.CatalogFailed && !snap.Loaded:
			p.seriesStateBlock(c, "history", text.Series.LoadFailed, p.authError(snap.Error), text.Retry, "refresh", seriesCalendarRetryFocus, seriesCalendarStatusRegion, func() {
				catalog.RetryCalendar()
			})
			return
		case snap.Phase == app.CatalogCanceled && !snap.Loaded:
			p.seriesStateBlock(c, "history", text.CanceledTitle, "", text.Retry, "refresh", seriesCalendarRetryFocus, seriesCalendarStatusRegion, func() {
				catalog.RetryCalendar()
			})
			return
		case len(snap.Days) == 0:
			p.seriesStateBlock(c, "history", text.Series.NoUpdates, text.Series.NoUpdatesNote,
				text.Series.Index, "search", seriesCalendarToIndexFocus, seriesCalendarStatusRegion, func() {
					p.State.Navigate(app.Route{Page: app.SeriesIndex})
				})
			return
		}

		p.seriesCalendarDayStrip(c, snap)
		day := snap.Days[min(max(snap.Selected, 0), len(snap.Days)-1)]
		ui.Row(c).FillWidth().Padding(54, 110, 20, 140).Gap(20).AlignItems(ui.Center).Children(func() {
			ui.Text(c, fmt.Sprintf("%d.%02d", day.Month, day.Day)).FontSize(24).TextColor(paint(colors.Accent.Soft))
			ui.Text(c, text.Series.Scheduled).FontSize(27).FontWeight(450)
			ui.Spacer(c).Grow(1)
			ui.Text(c, fmt.Sprintf(text.Series.WorksCount, len(day.Entries))).FontSize(17).TextColor(paint(colors.Text.Muted))
		})
		if len(day.Entries) == 0 {
			p.seriesStateBlock(c, "history", text.Series.NoUpdates, text.Series.NoUpdatesNote,
				text.Series.Index, "search", seriesCalendarToIndexFocus, seriesCalendarStatusRegion, func() {
					p.State.Navigate(app.Route{Page: app.SeriesIndex})
				})
			return
		}
		ui.Column(c.Key("series-calendar-schedule")).FillWidth().Padding(0, 115, 40, 140).Children(func() {
			for _, item := range day.Entries {
				p.seriesScheduleRow(c, item)
			}
		})
	})
}

func (p *PageContext) restoreSeriesCalendarFocus(snap app.SeriesCalendarSnapshot) {
	if !p.active {
		return
	}
	focus := p.State.Focus
	dayFocus := seriesCalendarDayFocus(min(max(snap.Selected, 0), max(0, len(snap.Days)-1)))
	if (focus == seriesCalendarRetryFocus && snap.Phase != app.CatalogFailed && snap.Phase != app.CatalogCanceled) ||
		(focus == seriesCalendarCancelFocus && snap.Phase != app.CatalogLoading) {
		if len(snap.Days) != 0 {
			p.State.Focus = dayFocus
		} else {
			p.State.Focus = seriesCalendarBackFocus
		}
		p.host.reveal = true
		return
	}
	if len(snap.Days) == 0 {
		return
	}
	day := snap.Days[min(max(snap.Selected, 0), len(snap.Days)-1)]
	if strings.HasPrefix(string(focus), "series-calendar/show/") || strings.HasPrefix(string(focus), "series-calendar/track/") {
		for _, item := range day.Entries {
			if seriesCalendarShowFocus(item.Entry.Item.Ref) == focus || seriesCalendarTrackFocus(item.Entry.Item.Ref) == focus {
				return
			}
		}
		p.State.Focus = dayFocus
		p.host.reveal = true
	}
}

func (p *PageContext) seriesCalendarRegions(snap app.SeriesCalendarSnapshot) {
	if len(snap.Days) == 0 {
		p.region(seriesCalendarHeaderRegion, focusEdges{app.Down: {Region: seriesCalendarStatusRegion}})
		p.region(seriesCalendarStatusRegion, focusEdges{app.Up: {Focus: seriesCalendarBackFocus, Region: seriesCalendarHeaderRegion}})
		return
	}
	sel := min(max(snap.Selected, 0), len(snap.Days)-1)
	day := snap.Days[sel]
	p.region(seriesCalendarHeaderRegion, focusEdges{app.Down: {Focus: seriesCalendarDayFocus(sel), Region: seriesCalendarDaysRegion}})
	belowDays := focusDestination{Region: seriesCalendarStatusRegion}
	if len(day.Entries) != 0 {
		belowDays = focusDestination{Focus: seriesCalendarShowFocus(day.Entries[0].Entry.Item.Ref), Region: seriesCalendarListRegion}
	}
	p.region(seriesCalendarDaysRegion, focusEdges{
		app.Up:   {Focus: seriesCalendarBackFocus, Region: seriesCalendarHeaderRegion},
		app.Down: belowDays,
	})
	if len(day.Entries) != 0 {
		p.region(seriesCalendarListRegion, focusEdges{
			app.Up: {Focus: seriesCalendarDayFocus(sel), Region: seriesCalendarDaysRegion},
		})
	} else {
		p.region(seriesCalendarStatusRegion, focusEdges{
			app.Up: {Focus: seriesCalendarDayFocus(sel), Region: seriesCalendarDaysRegion},
		})
	}
}

func (p *PageContext) prepareSeriesCalendarArtwork(snap app.SeriesCalendarSnapshot) {
	if p.host.Catalog == nil || len(snap.Days) == 0 {
		return
	}
	day := snap.Days[min(max(snap.Selected, 0), len(snap.Days)-1)]
	urls := make([]string, 0, len(day.Entries))
	for _, entry := range day.Entries {
		if u := entry.Entry.Item.CoverURL; u != "" {
			urls = append(urls, u)
		}
	}
	p.host.Catalog.PrepareArtwork(urls)
}

func (p *PageContext) seriesCalendarDayStrip(c *ui.Context, snap app.SeriesCalendarSnapshot) {
	colors, text := p.Theme.Colors, p.Assets.Text.Series
	dayCount := len(snap.Days)
	tileWidth := (p.Width - 140 - 110 - float32(max(0, dayCount-1))*16) / float32(max(1, dayCount))
	ui.Row(c.Key("series-calendar-days")).FillWidth().Padding(32, 110, 0, 140).Gap(16).Children(func() {
		for i, day := range snap.Days {
			id := seriesCalendarDayFocus(i)
			selected := i == snap.Selected
			focused := p.paintFocus() == id
			bg := withAlpha(colors.Text.Primary, 0x05)
			border := paint(colors.Border.Subtle)
			fg := paint(colors.Text.Muted)
			if selected {
				bg = withAlpha(colors.Accent.Primary, 0x10)
				border = withAlpha(colors.Accent.Primary, 0x70)
				fg = paint(colors.Accent.Soft)
			}
			if focused {
				bg = paint(colors.Focus.Background)
				border = paint(colors.Focus.Ring)
				fg = paint(colors.Focus.Foreground)
			}
			weekdayLabel := ""
			if day.IsToday {
				weekdayLabel = text.Today
			} else if day.Weekday >= 0 && day.Weekday < len(text.Weekdays) {
				weekdayLabel = text.Weekdays[day.Weekday]
			}
			dayIdx := i
			btn := ui.Button(c.Key(string(id)), "").Label(fmt.Sprintf("%s %d.%02d", weekdayLabel, day.Month, day.Day)).
				Size(tileWidth, 137).Radius(17).Border(1, border).Background(bg).TextColor(fg).
				Center().Role(ui.RoleTab).AccessSelected(selected).
				OnClick(func() { p.dispatch(func() { p.host.Series.SelectCalendarDay(dayIdx) }) })
			p.animateFocus(btn, bg, fg)
			p.bind(btn, id, seriesCalendarDaysRegion)
			btn.Children(func() {
				ui.Column(c).Fill().Center().Gap(10).Children(func() {
					ui.Text(c, weekdayLabel).FontSize(18)
					ui.Text(c, fmt.Sprintf("%02d", day.Day)).FontSize(34).FontWeight(450)
					ui.Text(c, fmt.Sprintf("%d%s", day.Month, text.MonthSuffix)).FontSize(14)
				})
			})
		}
	})
}

func (p *PageContext) seriesScheduleRow(c *ui.Context, item provider.SeriesScheduleEntry) {
	colors, d := p.Theme.Colors, p.uiColors()
	showID := seriesCalendarShowFocus(item.Entry.Item.Ref)
	trackID := seriesCalendarTrackFocus(item.Entry.Item.Ref)
	showFocused := p.paintFocus() == showID
	bg, fg, subFg, epFg := withAlpha(colors.Text.Primary, 0x05), paint(colors.Text.Primary), paint(colors.Text.Muted), paint(colors.Accent.Soft)
	if showFocused {
		bg, fg, subFg, epFg = paint(colors.Focus.Background), paint(colors.Focus.Foreground), d.focusSubtext, d.focusSubtext
	}
	ui.Row(c.Key("schedule-row/"+item.Entry.Item.Ref.ID)).FillWidth().Margin(0, 0, 14, 0).Gap(35).AlignItems(ui.Center).Children(func() {
		ui.Text(c, item.Time).Width(90).Shrink(0).FontSize(23).TextColor(paint(colors.Accent.Soft))
		btn := ui.Button(c.Key(string(showID)), "").Label(item.Entry.Item.Title).Grow(1).MinWidth(0).
			Height(116).Padding(10, 15).Radius(15).Border(0, ui.Transparent).Background(bg).TextColor(fg).
			OnClick(func() { p.dispatch(func() { p.openVideoDetail(item.Entry.Item) }) })
		p.animateFocus(btn, bg, fg)
		p.bind(btn, showID, seriesCalendarListRegion)
		btn.Children(func() {
			ui.Row(c).Fill().Gap(26).AlignItems(ui.Center).Children(func() {
				ui.Box(c).Size(164, 92).Shrink(0).Radius(9).Clip().Background(d.coverFill).Children(func() {
					if img := p.catalogCover(item.Entry.Item.CoverURL); img != nil {
						ui.Image(c, img).Fill().Fit(ui.Cover)
					}
				})
				ui.Column(c).Grow(1).MinWidth(0).Gap(6).Children(func() {
					if item.Entry.Item.Category != "" {
						ui.Text(c, item.Entry.Item.Category).FontSize(16).SingleLine().TextColor(subFg)
					}
					ui.Text(c, item.Entry.Item.Title).FontSize(25).FontWeight(450).SingleLine()
					if item.EpisodeLabel != "" {
						ui.Text(c, item.EpisodeLabel).FontSize(18).SingleLine().TextColor(epFg)
					}
				})
			})
		})
		trackIcon := "plus"
		if item.Entry.Tracked {
			trackIcon = "check"
		}
		p.styledButton(c, p.sourceSeriesTrackLabel(item.Entry.Tracked), trackIcon, trackID, seriesCalendarListRegion, func() {
			p.toggleSeriesTrack(item.Entry)
		}, buttonAppearance{
			height: 50, minWidth: 125, padding: 20, radius: 12, fontSize: 19, iconSize: 19, iconGap: 10,
			background: d.secondaryFill, foreground: d.secondaryText, border: d.secondaryBorder,
		})
	})
}
