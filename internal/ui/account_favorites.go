package tvui

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/provider"
)

const (
	favoriteTabs   = "favorite-tabs"
	favoriteHeader = "favorite-header"
	favoriteCards  = "favorite-cards"
	favoriteTools  = "favorite-tools"
	favoriteStatus = "favorite-status"
)

func (p *PageContext) renderLibrary(c *ui.Context, query provider.AccountQuery) {
	if p.active && p.State.Focus == "" {
		p.State.Focus = app.ContentAction
	}
	activity, text := p.host.Activity, p.Assets.Text
	var snapshot app.AccountSnapshot
	guest := activity == nil || !activity.SignedIn()
	available := !guest && activity.Available(query.Section)
	if available {
		snapshot = activity.Snapshot(query)
	}
	directory := query.Section == provider.AccountFavorites && query.Folder == ""
	rows := snapshot.Page.Rows
	managing := len(rows) != 0 && p.host.favorites.management.query == query
	total := len(snapshot.Page.Rows)
	if snapshot.Page.Total != nil {
		total = *snapshot.Page.Total
	}
	activeTab := app.FocusID("library/tab/" + query.Section)
	hasHeaderAction := available && p.canManageLibrary(query) && (directory || query.Folder != "" || managing || len(rows) != 0)
	if p.active && !guest && available && !hasHeaderAction && p.State.Focus == app.ContentAction {
		switch {
		case query.Folder != "":
			p.State.Focus = "favorites/back"
		case query.Section == provider.AccountWatchLater && len(rows) > 0:
			p.State.Focus = libraryPlayAll
		default:
			p.State.Focus = activeTab
		}
	}
	belowHeader := focusDestination{Focus: libraryPlayAll, Region: favoriteCards}
	if query.Section != provider.AccountWatchLater {
		belowHeader = focusDestination{Region: favoriteCards}
	}
	aboveCards := focusDestination{Focus: app.ContentAction, Region: favoriteHeader}
	if !hasHeaderAction {
		switch {
		case query.Section == provider.AccountWatchLater && len(rows) > 0:
			aboveCards = focusDestination{Focus: libraryPlayAll, Region: libraryMeta}
		case query.Folder != "":
			aboveCards = focusDestination{Focus: "favorites/back", Region: favoriteHeader}
		default:
			aboveCards = focusDestination{Focus: activeTab, Region: favoriteTabs}
		}
	}
	if managing {
		belowHeader = focusDestination{Region: favoriteTools}
		aboveCards = belowHeader
	}
	if len(rows) == 0 {
		belowHeader = focusDestination{Region: favoriteStatus}
	}
	if p.active && available && !directory && !managing && snapshot.Phase == app.CatalogReady &&
		strings.HasPrefix(string(p.State.Focus), "account/") {
		found := false
		for _, row := range rows {
			if libraryCardFocus(query, row) == p.State.Focus {
				found = true
				break
			}
		}
		if !found {
			if len(rows) > 0 {
				p.State.Focus = libraryCardFocus(query, rows[0])
			} else {
				p.State.Focus = activeTab
			}
			p.host.reveal = true
		}
	}
	emptyFocus := app.ContentAction
	if query.Folder != "" {
		emptyFocus = "library/empty-action"
	}
	tabsDown := focusDestination{Focus: app.ContentAction, Region: belowHeader.Region}
	if !hasHeaderAction {
		if query.Folder != "" {
			tabsDown = focusDestination{Focus: "favorites/back", Region: favoriteHeader}
		} else {
			tabsDown = belowHeader
		}
	}
	p.region(favoriteTabs, focusEdges{app.Down: tabsDown})
	p.region(favoriteHeader, focusEdges{app.Up: {Focus: activeTab}, app.Down: belowHeader})
	p.region(favoriteTools, focusEdges{app.Up: {Focus: libraryManageID(query.Folder != "")}, app.Down: {Region: favoriteCards}})
	metaUp := focusDestination{Focus: activeTab, Region: favoriteTabs}
	if hasHeaderAction {
		metaUp = focusDestination{Focus: app.ContentAction, Region: favoriteHeader}
	} else if query.Folder != "" {
		metaUp = focusDestination{Focus: "favorites/back", Region: favoriteHeader}
	}
	p.region(libraryMeta, focusEdges{app.Up: metaUp, app.Down: {Region: favoriteCards}})
	p.region(favoriteCards, focusEdges{app.Up: aboveCards, app.Down: {Region: favoriteStatus}})
	p.region(favoriteStatus, focusEdges{app.Up: {Focus: activeTab, Region: favoriteCards}})
	headingHeight, actionTop, metaTop := float32(266), float32(110), float32(203)
	switch {
	case query.Folder != "":
		headingHeight, actionTop, metaTop = 318, 160, 244
		if managing {
			headingHeight, metaTop = 334, 251
		}
	case directory || len(rows) == 0:
		headingHeight = 216
	case managing:
		headingHeight, metaTop = 282, 201
	}
	ui.Column(c.Key("library")).FillWidth().Padding(0, 0, 64, 0).Lang(p.langTag()).Font(p.uiFont()).Children(func() {
		p.renderLibraryTabs(c, query.Section)
		ui.Column(c.Key("library-body/" + string(query.Section) + "/" + query.Folder)).FillWidth().Children(func() {
			if guest {
				guestTitle, guestNote := p.libraryGuestText()
				p.libraryEmpty(c, "library", guestTitle, guestNote,
					p.signInActionLabel(), "user", emptyFocus, func() { p.State.Push(app.Route{Page: app.Account}) })
				return
			}
			ui.Box(c.Key("library-heading")).FillWidth().Height(headingHeight).Children(func() {
				ui.Row(c).Absolute().Top(actionTop).Right(83).Gap(16).Children(func() {
					if available {
						p.libraryHeaderActions(c, query, rows, managing)
					}
				})
				p.libraryHeading(c, activity.Folder(), query.Folder != "", total)
				switch {
				case managing:
					p.favoriteManagementBar(c, snapshot, metaTop)
				case !directory && len(rows) != 0:
					p.libraryCollectionMeta(c, query, total, metaTop)
				}
			})
			if !available {
				p.libraryEmpty(c, "library", text.Activity.LibraryUnavailable, "", text.Activity.DiscoverContent, "", emptyFocus, p.openLibraryHome)
				return
			}
			if len(rows) == 0 && !directory {
				switch snapshot.Phase {
				case app.CatalogIdle, app.CatalogLoading:
					p.libraryEmpty(c, "spinner", text.LoadingTitle, "", "", "", "", nil)
				case app.CatalogFailed:
					p.libraryEmpty(c, "", text.FailedTitle, p.requestError(snapshot.Error), text.Retry, "", emptyFocus, func() { activity.Retry(query) })
				case app.CatalogCanceled:
					p.libraryEmpty(c, "", text.CanceledTitle, "", text.Retry, "", emptyFocus, func() { activity.Retry(query) })
				default:
					t := text.Activity
					emptyTitle, emptyNote := p.libraryEmptyText(query.Section)
					icon := "library"
					if query.Section == provider.AccountTracking {
						icon = "series"
					}
					p.libraryEmpty(c, icon, emptyTitle, emptyNote, t.DiscoverContent, "", emptyFocus, p.openLibraryHome)
				}
				return
			}
			p.libraryGrid(c, query, snapshot, 102+headingHeight, managing)
			p.favoritePageStatus(c, query, snapshot)
		})
	})
}

const libraryMeta = "library-meta"
const libraryPlayAll app.FocusID = "library/play-all"

func (p *PageContext) openLibraryHome() { p.State.Navigate(app.Route{Page: app.Home}) }

func (p *PageContext) libraryEmpty(c *ui.Context, icon, title, note, action, actionIcon string, focus app.FocusID, run func()) {
	colors, d := p.Theme.Colors, p.uiColors()
	if icon == "" {
		icon = "library"
	}
	top := float32(55)
	style := buttonAppearance{height: 62, padding: 27, radius: 12, fontSize: 22, fontWeight: 550, iconSize: 24, iconGap: 13,
		background: d.secondaryFill, foreground: d.secondaryText,
		border: d.secondaryBorder}
	if actionIcon != "" {
		top = 105
	}
	ui.Column(c).FillWidth().Padding(top, 0, 40, 0).AlignItems(ui.Center).Children(func() {
		if icon == "spinner" {
			p.loadingSpinner(c, 52)
		} else {
			ui.Icon(c, p.Assets.Icon(icon)).Size(65, 65).TextColor(paint(colors.Accent.Soft))
		}
		ui.Text(c, title).Margin(32, 0, 18, 0).FontSize(36).FontWeight(450).LineHeight(1.39)
		if note != "" {
			ui.Text(c, note).Margin(0, 0, 34, 0).FontSize(21).LineHeight(1.38).TextColor(paint(colors.Text.Muted))
		}
		if action != "" && run != nil {
			p.styledButton(c, action, actionIcon, focus, favoriteStatus, run, style).AlignSelf(ui.Center)
		}
	})
}

func (p *PageContext) renderLibraryTabs(c *ui.Context, active provider.AccountSection) {
	colors, d := p.Theme.Colors, p.uiColors()
	entry, _ := p.State.Current()
	ui.Box(c.Key("favorite-tabs")).FillWidth().Height(102).Children(func() {
		ui.Row(c).Absolute().Top(45).Left(78).Height(61).Gap(18).Children(func() {
			for _, section := range libraryTabs {
				if !entry.Capabilities.Has(section.Capability()) {
					continue
				}
				title := p.librarySectionLabel(section)
				id := app.FocusID("library/tab/" + section)
				selected := section == active
				background, foreground, width := ui.Transparent, paint(colors.Text.Muted), float32(148)
				borderW, borderColor := float32(0), ui.Transparent
				if section == provider.AccountFavorites && utf8.RuneCountInString(title) <= 3 {
					width = 123
				}
				if selected {
					background, foreground = withAlpha(colors.Text.Primary, 0x12), paint(colors.Text.Primary)
				}
				focused := p.paintFocus() == id
				if focused {
					background, foreground = d.tabFocusFill, paint(colors.Text.Primary)
					if !p.Theme.Dark {
						borderW, borderColor = 1, d.focusBorder
					}
				}
				button := ui.Button(c.Key(string(id)), "").Label(title).Role(ui.RoleTab).AccessSelected(selected).
					Size(width, 61).Padding(0, 0).Center().Radius(30).Border(borderW, borderColor).
					Background(background).TextColor(foreground).
					OnClick(func() {
						p.dispatch(func() {
							if p.State.Navigate(libraryRoute(section)) {
								p.host.favorites.management = favoriteManagement{}
								p.State.Focus = id
							}
						})
					})
				p.animateFocus(button, background, foreground)
				p.bind(button, id, favoriteTabs)
				button.Children(func() {
					ui.Text(c, title).FontSize(24).LetterSpacing(1)
					if selected || focused {
						ui.Box(c).Absolute().Bottom(2).Left((width-15)/2).Size(15, 3).
							Radius(2).Background(paint(colors.Accent.Primary))
					}
				})
			}
		})
	})
}

func (p *PageContext) libraryHeading(c *ui.Context, folder provider.AccountRow, inFolder bool, total int) {
	text, colors := p.Assets.Text.Activity, p.Theme.Colors
	secondary := paint(colors.Text.Muted)
	if !inFolder {
		ui.Text(c, text.FavoritesTitle).Absolute().Top(39).Left(103).FontSize(51).FontWeight(550).LetterSpacing(1)
		ui.Text(c, text.FavoritesNote).Absolute().Top(132).Left(103).FontSize(22).TextColor(secondary)
		return
	}
	ui.Box(c).Absolute().Top(42).Left(103).Children(func() {
		p.libraryTextLink(c, p.libraryFoldersBackLabel(), "back", "favorites/back", favoriteHeader, func() {
			p.State.Back()
		})
	})
	ui.Text(c, folder.Title).Absolute().Top(88).Left(103).FontSize(51).FontWeight(550).LetterSpacing(1)
	count := fmt.Sprintf(text.FolderCount, total)
	note := count
	if visibility, _ := p.favoriteVisibility(folder); visibility != "" {
		note += " \u00b7 " + visibility
	}
	ui.Text(c, note).Absolute().Top(180).Left(103).FontSize(22).TextColor(secondary)
}

func (p *PageContext) libraryCollectionMeta(c *ui.Context, query provider.AccountQuery, total int, top float32) {
	text, colors := p.Assets.Text.Activity, p.Theme.Colors
	label := p.librarySectionLabel(query.Section)
	if query.Folder != "" {
		label = text.FolderContents
		top += 8
	}
	ui.Row(c).Absolute().Top(top).Left(103).Right(83).Height(48).Gap(20).AlignItems(ui.Baseline).Children(func() {
		ui.Text(c, label).FontSize(26).FontWeight(400)
		ui.Text(c, fmt.Sprintf(text.FolderCount, total)).FontSize(18).TextColor(paint(colors.Text.Muted))
	})
	rows := p.host.Activity.Snapshot(query).Page.Rows
	if query.Section == provider.AccountWatchLater {
		ui.Box(c).Absolute().Top(top + 2).Right(83).Children(func() {
			p.libraryTextLink(c, text.PlayAll, "play", libraryPlayAll, libraryMeta, func() {
				p.playWatchLater(rows)
			})
		})
	}
}

func (p *PageContext) libraryTextLink(c *ui.Context, label, icon string, id app.FocusID, region string, action func()) {
	colors := p.Theme.Colors
	focused := p.paintFocus() == id
	bg, ink := ui.Transparent, paint(colors.Text.Secondary)
	borderW, borderCol := float32(0), ui.Transparent
	if focused {
		bg, ink = paint(colors.Focus.Background), paint(colors.Focus.Foreground)
		borderW, borderCol = 2, paint(colors.Focus.Ring)
	}
	button := ui.Button(c.Key(string(id)), "").Label(label).Height(38).Padding(0, 14).Radius(19).
		Border(borderW, borderCol).Background(bg).TextColor(ink).
		OnClick(func() { p.dispatch(action) })
	p.animateFocus(button, bg, ink)
	p.bind(button, id, region)
	button.Children(func() {
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, p.Assets.Icon(icon)).Size(20, 20).TextColor(ink)
			ui.Text(c, label).FontSize(19).TextColor(ink)
		})
	})
}

func (p *PageContext) favoriteVisibility(row provider.AccountRow) (string, string) {
	if row.Private == nil {
		return "", ""
	}
	if *row.Private {
		return p.Assets.Text.Activity.FolderPrivate, "lock"
	}
	return p.Assets.Text.Activity.FolderPublic, "user"
}

func favoriteFocus(row provider.AccountRow, directory bool) app.FocusID {
	if directory {
		return app.FocusID("account/folder/" + row.ID)
	}
	return app.FocusID("account/favorite/" + row.ID)
}

func libraryCardFocus(query provider.AccountQuery, item provider.AccountRow) app.FocusID {
	if query.Section == provider.AccountFavorites {
		return favoriteFocus(item, query.Folder == "")
	}
	return app.FocusID("account/" + string(query.Section) + "/" + item.ID)
}

func (p *PageContext) libraryGrid(c *ui.Context, query provider.AccountQuery, snapshot app.AccountSnapshot, top float32, managing bool) {
	items := snapshot.Page.Rows
	directory := query.Section == provider.AccountFavorites && query.Folder == ""
	columns, gap, cover, stride := 4, float32(30), float32(208), float32(319)
	if directory {
		columns, gap, cover, stride = 3, 40, 260, 385
	}
	rows := (len(items) + columns - 1) / columns
	width := max(1, (p.Width-186-float32(columns-1)*gap)/float32(columns))
	y := p.scroll().Y - top
	if p.active && snapshot.Phase == app.CatalogReady && snapshot.Page.NextCursor != "" &&
		(y+p.Height+stride >= float32(rows)*stride || managing && p.host.favorites.management.all) {
		p.host.Activity.More(query)
	}
	first := min(rows, max(0, int(y/stride)-1))
	end := min(rows, max(first, int((y+p.Height)/stride)+2))
	focused := -1
	for index, item := range items {
		if p.State.Focus == libraryCardFocus(query, item) {
			focused = index / columns
			break
		}
	}
	window := items[min(len(items), first*columns):min(len(items), end*columns)]
	p.prepareAccountArtwork(window)
	if p.active && query.Section == provider.AccountHistory {
		p.host.Activity.PrepareHistoryViews(window)
	}
	ui.Box(c.Key("favorite-grid")).FillWidth().Height(float32(rows) * stride).Children(func() {
		draw := func(row int) {
			ui.Row(c.Key(row)).Absolute().Top(float32(row) * stride).Left(103).Right(83).Height(stride).Gap(gap).Children(func() {
				for _, item := range items[row*columns : min((row+1)*columns, len(items))] {
					p.libraryCard(c, query, items, item, directory, width, cover, managing)
				}
			})
		}
		for row := first; row < end; row++ {
			draw(row)
		}
		if focused >= 0 && (focused < first || focused >= end) {
			draw(focused)
		}
	})
}

func (p *PageContext) prepareAccountArtwork(items []provider.AccountRow) {
	if p.host.Catalog == nil {
		return
	}
	locations := make([]string, 0, 2*len(items))
	for _, item := range items {
		if item.CoverURL != "" {
			locations = append(locations, item.CoverURL)
		}
	}
	for _, item := range items {
		if item.CreatorAvatarURL != "" {
			locations = append(locations, item.CreatorAvatarURL)
		}
	}
	p.host.Catalog.PrepareArtwork(locations)
	p.pruneCatalogBitmaps()
}

func (p *PageContext) accountRowImage(location string) *ui.Bitmap {
	return p.catalogCover(location)
}

func (p *PageContext) libraryCard(c *ui.Context, query provider.AccountQuery, allRows []provider.AccountRow, item provider.AccountRow, directory bool, width, cover float32, managing bool) {
	colors := p.Theme.Colors
	var quality string
	if query.Section == provider.AccountHistory && item.Ref != nil {
		views, itemQuality := p.host.Activity.HistoryMetadata(*item.Ref)
		quality = itemQuality
		if item.Views == nil {
			item.Views = views
		}
	}
	id := libraryCardFocus(query, item)
	focus := p.paintFocus() == id
	button := ui.Button(c.Key(string(id)), "").Label(item.Title).Width(width).Height(cover+107).Padding(0, 0).
		Border(0, ui.Transparent).Background(ui.Transparent).Shrink(0)
	selected := managing && p.host.favorites.management.selected(item.RemovalID)
	if managing {
		button.AccessSelected(selected).OnClick(func() {
			p.dispatch(func() { p.toggleFavoriteSelection(item.RemovalID) })
		})
	} else if directory && item.Ref != nil && item.Ref.Kind == provider.CollectionKind && item.Ref.EntryID == p.State.CurrentEntry {
		button.OnClick(func() {
			p.dispatch(func() {
				p.host.favorites.management = favoriteManagement{}
				p.host.Activity.SetFolder(item)
				p.State.Push(app.Route{Page: app.Library, Section: favoriteFolderSection})
			})
		})
	}
	if !managing && !directory && item.Ref != nil && item.Ref.EntryID == p.State.CurrentEntry && p.canOpenVideoDetail(*item.Ref) {
		media := provider.MediaItem{Ref: *item.Ref, Title: item.Title, CoverURL: item.CoverURL,
			DurationSeconds: item.DurationSeconds, Views: item.Views}
		if item.Item != nil {
			media = *item.Item
		}
		button.OnClick(func() {
			p.dispatch(func() {
				entry, _ := p.State.Current()
				queueSection := (query.Section == provider.AccountFavorites && query.Folder != "") ||
					query.Section == provider.AccountWatchLater ||
					(query.Section == provider.AccountHistory && entry.Capabilities.Has(provider.PlaybackModes))
				if queueSection {
					queue := make([]playbackItem, 0, len(allRows))
					clickedIdx := -1
					for _, row := range allRows {
						if row.Ref == nil || !p.canPlay(*row.Ref) {
							continue
						}
						m := provider.MediaItem{Ref: *row.Ref, Title: row.Title, CoverURL: row.CoverURL,
							DurationSeconds: row.DurationSeconds, Views: row.Views}
						if row.Item != nil {
							m = *row.Item
						}
						if row.ID == item.ID && *row.Ref == *item.Ref && clickedIdx < 0 {
							clickedIdx = len(queue)
						}
						queue = append(queue, playbackItem{item: m})
					}
					if clickedIdx >= 0 && len(queue) > 1 {
						p.openVideoDetailInQueue(media, queue, clickedIdx)
						return
					}
				}
				p.openVideoDetail(media)
			})
		})
	}
	p.bind(button, id, favoriteCards)
	button.Children(func() {
		radius, title, margin, spacing := float32(13), float32(21), float32(16), float32(.3)
		weight := 500
		if directory {
			radius, title, margin, spacing, weight = 18, 28, 25, 0, 450
		}
		ui.Column(c).Fill().Children(func() {
			d := p.uiColors()
			poster := ui.Box(c).FillWidth().Height(cover).Radius(radius).Background(d.coverFill)
			if directory {
				poster.Gradient(paint(colors.Decoration.CardStart), paint(colors.Decoration.CardEnd), 130)
			}
			if focus && !directory {
				poster.Shadow(0, 0, 0, 4, paint(colors.Focus.Ring))
			}
			poster.Children(func() {
				if focus && directory {
					ui.Box(c).Absolute().Top(-7).Right(-7).Bottom(-7).Left(-7).
						Radius(radius+7).Border(3, paint(colors.Focus.Ring))
				}
				ui.Box(c).Fill().Radius(radius).Clip().Children(func() {
					shade, fade := d.coverShade, float32(.55)
					if directory {
						fade = .8
					}
					if image := p.accountRowImage(item.CoverURL); image != nil {
						coverImage := ui.Image(c, image).Fill().Fit(ui.Cover)
						if directory {
							coverImage.Opacity(.8)
						}
					} else if directory {
						ui.Box(c).Fill().Center().Children(func() {
							ui.Icon(c, p.Assets.Icon("folder")).Size(65, 65).TextColor(paint(colors.Accent.Soft))
						})
					} else {
						ui.Box(c).Fill().Center().Gradient(paint(colors.Decoration.CardStart), paint(colors.Decoration.CardEnd), 110).Children(func() {
							ui.Icon(c, p.Assets.Icon("play")).Size(50, 50).TextColor(paint(colors.Text.Muted))
						})
					}
					ui.Box(c).Absolute().Top(0).Left(0).Fill().LinearGradient(ui.LinearGradient{
						Angle: 0, Stops: []ui.GradientStop{{Color: shade}, {Color: ui.Transparent, At: fade}},
					})
					if query.Section == provider.AccountHistory {
						var progress float32
						if item.Finished {
							progress = 1
						} else if item.ProgressSeconds != nil && item.DurationSeconds != nil && *item.DurationSeconds > 0 {
							progress = min(1, float32(*item.ProgressSeconds)/float32(*item.DurationSeconds))
						}
						if progress > 0 {
							ui.Box(c).Absolute().Bottom(0).Left(0).Right(0).Height(3).Background(paint(colors.Playback.Track)).Children(func() {
								ui.Box(c).Width(progress * width).Height(3).Background(paint(colors.Accent.Primary))
							})
						}
					}
				})
				if directory {
					ui.Box(c).Absolute().Top(0).Right(0).Bottom(0).Left(0).Radius(radius).
						Border(1, paint(colors.Border.Subtle))
					if item.Count != nil {
						ui.Text(c, strconv.Itoa(*item.Count)).Absolute().Right(22).Bottom(20).FontSize(26).
							TextColor(paint(colors.Playback.Foreground))
					}
				}
				if quality != "" {
					ui.Text(c, quality).Absolute().Top(12).Left(12).Padding(4, 7).Radius(4).
						Background(d.badgeFill).Border(1, d.badgeBorder).TextColor(paint(colors.Playback.Foreground)).
						FontSize(11).LetterSpacing(1)
				}
				if !directory && item.DurationSeconds != nil {
					ui.Text(c, catalogDuration(*item.DurationSeconds)).Absolute().
						Right(13).Bottom(14).FontSize(14).LetterSpacing(.7).TextColor(paint(colors.Playback.Foreground))
				}
				if managing && item.RemovalID != "" {
					p.favoriteSelectionMark(c, selected)
				}
			})
			titleBottom := float32(10)
			if directory {
				titleBottom = 17
			}
			ui.Text(c, item.Title).FillWidth().SingleLine().Margin(margin, 0, titleBottom, 0).
				FontSize(title).FontWeight(weight).LetterSpacing(spacing)
			p.favoriteCardMetadata(c, item, directory)
		})
	})
}

func (p *PageContext) favoriteCardMetadata(c *ui.Context, item provider.AccountRow, directory bool) {
	colors := p.Theme.Colors
	if directory {
		if label, icon := p.favoriteVisibility(item); label != "" {
			ink := paint(colors.Text.Muted)
			ui.Row(c).Margin(0, 0, 0, 3).Gap(9).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, p.Assets.Icon(icon)).Size(17, 17).TextColor(ink)
				ui.Text(c, label).FontSize(17).TextColor(ink)
			})
		}
		return
	}
	ink := paint(colors.Text.Secondary)
	ui.Row(c).FillWidth().Gap(9).AlignItems(ui.Center).Children(func() {
		if item.CreatorAvatarURL != "" && item.Subtitle != "" {
			ui.Box(c).Size(17, 17).Shrink(0).Radius(8.5).Clip().Background(withAlpha(p.Theme.Colors.Text.Secondary, 0x40)).Children(func() {
				if image := p.accountRowImage(item.CreatorAvatarURL); image != nil {
					ui.Image(c, image).Fill().Fit(ui.Cover)
				}
			})
		}
		if item.Subtitle != "" {
			ui.Text(c, item.Subtitle).Shrink(1).SingleLine().FontSize(15).TextColor(ink)
		}
		if item.Views != nil {
			if item.Subtitle != "" {
				ui.Text(c, "\u00b7").FontSize(15).TextColor(ink)
			}
			ui.Text(c, catalogViews(*item.Views, p.Assets.Text.Home)+p.Assets.Text.Home.Plays).FontSize(15).TextColor(ink)
		}
	})
}
