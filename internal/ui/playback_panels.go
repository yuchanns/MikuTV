package tvui

import (
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/provider"
)

var playbackRates = [...]float64{0.5, 0.75, 1, 1.25, 1.5, 2}

func (s *playbackView) openPanel(v *View, panel app.FocusID) {
	s.panel, s.returnFocus, s.panelScroll = panel, panel, ui.ScrollState{}
	s.controlFocus = ""
	switch panel {
	case playerInfo:
		v.State.Focus = playerPanelExit
	case playerComments:
		detail := v.discussionDetail()
		d := detail.videoDiscussion()
		v.State.Focus = d.firstFocus(true, detail.item.VideoDetails)
		if !d.hotLoaded && !d.showLatest && d.cancel == nil && d.root == nil {
			d.listRows.ScrollTo(0, ui.Start)
			p := v.context(v.State.Location, true, playerDrawerWidth, 1080)
			p.loadVideoDiscussion(d, 0)
		}
	case playerSpeed:
		v.State.Focus = playbackOptionID(panel, playbackRateLabel(v.Playback.Rate()))
	case playerSubtitles:
		v.State.Focus = playbackOptionID(panel, s.content.subtitle)
	case playerQuality:
		state := v.Playback.State()
		key := state.Resolution.Plan.Quality
		if state.Selected == "" {
			key = ""
		}
		v.State.Focus = playbackOptionID(panel, key)
	case playerDanmaku:
		key := "disabled"
		if v.playbackSettings.show {
			key = "enabled"
		}
		v.State.Focus = playbackOptionID(panel, key)
	}
	if v.State.Focus == "" {
		v.State.Focus = playerPanelExit
	}
	v.reveal = true
}

func (s *playbackView) closePanel(v *View) {
	s.panel = ""
	s.restoreControl(v, s.returnFocus)
}

const playerPanelExit app.FocusID = "player/panel-exit"

func playbackOptionID(panel app.FocusID, key string) app.FocusID {
	return panel + "/" + app.FocusID(key)
}

func playerEpisodeID(index int) app.FocusID {
	return app.FocusID(fmt.Sprintf("player/episode/%d", index))
}

func playerChapterID(index int) app.FocusID {
	return app.FocusID(fmt.Sprintf("player/chapter/%d", index))
}

func currentPlaybackChapter(chapters []provider.PlaybackChapter, position float64) int {
	for i := len(chapters) - 1; i > 0; i-- {
		if position >= chapters[i].Start {
			return i
		}
	}
	return 0
}

func (p *PageContext) playbackChapters(c *ui.Context, state app.PlaybackState) {
	v, s := p.host, &p.host.player
	chapters := state.Resolution.Plan.Chapters
	position := state.Position
	if s.scrubbing {
		position = s.scrubPosition
	}
	current := currentPlaybackChapter(chapters, position)
	focused := current
	if s.selectingChapter {
		focused = s.chapterIndex
	}
	first := max(0, min(focused-1, len(chapters)-4))
	last := min(first+4, len(chapters))
	cardWidth := (p.Width - 2*playerInset - 72) / 4
	cardHeight := cardWidth * 9 / 16
	colors, text := p.Theme.Colors, p.Assets.Text.Playback
	p.region(playerChapterGroup, focusEdges{})
	ui.Column(c).Absolute().Bottom(237).Left(playerInset).Right(playerInset).Gap(22).Children(func() {
		ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Children(func() {
			ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
				ui.Text(c, text.Chapters).FontSize(25).FontWeight(500).LetterSpacing(1).Translate(0, -2).
					TextColor(paint(colors.Playback.Foreground))
				if !s.selectingChapter {
					ui.Icon(c, p.Assets.Icon("detail/back")).Size(24, 24).Rotate(90).TextColor(paint(colors.Accent.Soft))
				}
			})
			ui.RichText(c,
				ui.Span{Text: fmt.Sprintf("%02d", focused+1), Color: paint(colors.Playback.Foreground)},
				ui.Span{Text: fmt.Sprintf(" / %02d", len(chapters)), Color: paint(colors.Playback.Secondary)},
			).FontSize(17).FontFeatures("tnum")
		})
		ui.Row(c).FillWidth().Padding(6, 0, 8, 0).Gap(24).Children(func() {
			for i := first; i < last; i++ {
				chapter := chapters[i]
				id := playerChapterID(i)
				button := ui.Button(c.Key(string(id)), "").Label(chapter.Title).Padding(0, 0).
					Width(cardWidth).Shrink(0).Background(ui.Transparent).Border(0, ui.Transparent).
					IgnoreInput(!s.selectingChapter).
					OnClick(func() {
						p.dispatch(func() {
							if s.scrubbing {
								s.scrubPosition = chapter.Start
								s.finishScrub(v, true)
							} else {
								v.Playback.Seek(chapter.Start)
							}
							s.selectingChapter = false
							p.State.Focus, v.reveal = playerTimeline, true
						})
					})
				if s.selectingChapter {
					p.bindDirection(button, id, playerChapterGroup, func(direction app.Direction) bool {
						switch direction {
						case app.Left:
							s.chapterIndex = max(0, s.chapterIndex-1)
						case app.Right:
							s.chapterIndex = min(len(chapters)-1, s.chapterIndex+1)
						case app.Down:
							s.selectingChapter = false
							p.State.Focus, v.reveal = playerTimeline, true
							return true
						default:
							return true
						}
						p.State.Focus, v.reveal = playerChapterID(s.chapterIndex), true
						return true
					})
				}
				button.Children(func() {
					ui.Column(c).FillWidth().Gap(15).Children(func() {
						card := ui.Box(c).FillWidth().Height(cardHeight).Radius(12).Background(withAlpha(colors.Overlay.PlayerBottom, 0xe8))
						focusedCard := s.selectingChapter && p.paintFocus() == id
						if focusedCard {
							card.Shadow(0, 0, 0, 4, paint(colors.Focus.Ring))
						}
						card.Children(func() {
							ui.Box(c).Fill().Radius(12).Clip().Children(func() {
								art := p.catalogCover(chapter.ImageURL)
								if art == nil {
									art = p.catalogCover(s.current().item.CoverURL)
								}
								if art != nil {
									ui.Image(c, art).Fill().Fit(ui.Cover)
								}
								ui.Box(c).Absolute().Left(0).Bottom(0).FillWidth().Height(cardHeight/2).
									Gradient(ui.Transparent, withAlpha(colors.Overlay.PlayerBottom, 0xcc), 180)
								ui.Text(c, playbackTime(chapter.Start)).Absolute().Left(18).Bottom(16).
									FontSize(19).FontFeatures("tnum").TextColor(paint(colors.Playback.Foreground))
							})
						})
						ink := paint(colors.Playback.Secondary)
						if focusedCard {
							ink = paint(colors.Playback.Foreground)
						} else if i == current {
							ink = paint(colors.Accent.Soft)
						}
						ui.Text(c, chapter.Title).Margin(0, 2).Height(32).FontSize(22).LineHeight(32.0 / 22).
							MaxLines(1).TextColor(ink)
					})
				})
			}
		})
	})
}

func (p *PageContext) playbackSeekPreview(c *ui.Context, state app.PlaybackState, poster string) {
	s, colors := &p.host.player, p.Theme.Colors
	progress := playbackFraction(s.scrubPosition, playbackTimelineDuration(state, s.current().item))
	left := min(max(playerInset, playerInset+(p.Width-2*playerInset)*progress-122), p.Width-playerInset-245)
	ui.Column(c).Absolute().Bottom(249).Left(left).Width(245).Padding(8).Gap(8).Radius(13).
		Background(paint(colors.Surface.Raised)).Children(func() {
		if image := s.content.thumbnail(p.host, s.scrubPosition); image != nil {
			ui.Image(c, image).FillWidth().Height(119).Fit(ui.Cover).Radius(8)
		} else if image := p.catalogCover(poster); image != nil {
			ui.Image(c, image).FillWidth().Height(119).Fit(ui.Cover).Radius(8)
		}
		ui.Row(c).FillWidth().Padding(4, 8).Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Text(c, playbackTime(s.scrubPosition)).Shrink(0).FontSize(16).TextColor(paint(colors.Playback.Foreground))
			ui.Text(c, s.current().item.Title).Shrink(1).MaxLines(1).FontSize(16).TextColor(paint(colors.Playback.Foreground))
		})
	})
}

func (p *PageContext) playbackPanel(c *ui.Context, state app.PlaybackState, item playbackItem) {
	s, text := &p.host.player, p.Assets.Text.Playback
	pane := *p
	pane.active = p.active && !playbackBlocking(state) && !s.content.overlay()
	if s.panel == playerInfo || s.panel == playerComments {
		pane.playbackDrawer(c, item)
		return
	}
	var title, current string
	switch s.panel {
	case playerDanmaku:
		title = text.Danmaku
	case playerSubtitles:
		title = text.Subtitles
	case playerSpeed:
		title, current = text.Speed, playbackRateLabel(p.host.Playback.Rate())
	case playerQuality:
		title, current = text.Quality, playbackQualityLabel(state)
		if state.Selected == "" && current != "" {
			current = text.Auto + " · " + current
		}
	}
	colors, d := p.Theme.Colors, p.uiColors()
	pane.region(playerPanelGroup, focusEdges{})
	ui.Column(c.Key("player-popover")).Absolute().Right(88).Bottom(164).Width(392).MaxHeight(680).
		Radius(20).Background(d.playbackMenu).Border(1, withAlpha(colors.Playback.Foreground, 0x14)).
		Shadow(0, 16, 60, 0, paint(colors.Overlay.Shadow)).Clip().Children(func() {
		ui.Row(c).FillWidth().Height(66).Padding(0, 16).Gap(12).AlignItems(ui.Center).
			Border(0, ui.Transparent).Children(func() {
			heading := ui.Text(c, title).FontSize(24).FontWeight(550).TextColor(paint(colors.Playback.Foreground))
			if s.panel == playerSubtitles {
				heading.Translate(0, 4)
			}
			if current != "" {
				ui.Spacer(c).Grow(1)
				ui.Text(c, current).FontSize(15).TextColor(paint(colors.Playback.Secondary))
			}
		})
		ui.Box(c).FillWidth().Height(1).Background(withAlpha(colors.Playback.Foreground, 0x14))
		ui.Scroll(c).FillWidth().MaxHeight(607).TrackScroll(&s.panelScroll).Children(func() {
			ui.Column(c).FillWidth().Padding(10, 12).Gap(4).Children(func() {
				if s.panel == playerQuality {
					hint := text.QualityGuest
					if account, signedIn := currentAccount(p.host.Auth.Accounts()); signedIn {
						hint = text.QualitySignedIn
						if account.Card.Highlight {
							if account.Card.Title != "" {
								hint = account.Card.Title
							} else {
								hint = p.sourceMembership()
							}
						}
					}
					ui.Text(c, hint).FontSize(14).Margin(2, 12, 8, 12).
						TextColor(paint(colors.Playback.Secondary))
				}
				pane.playbackOptions(c, state)
			})
		})
	})
}

func (p *PageContext) playbackOptions(c *ui.Context, state app.PlaybackState) {
	v, s, text := p.host, &p.host.player, p.Assets.Text.Playback
	plan := state.Resolution.Plan
	option := func(key, label, note string, selected bool, action func()) {
		p.playbackOption(c, playbackOptionID(s.panel, key), label, note, selected, action)
	}
	switch s.panel {
	case playerSpeed:
		for _, rate := range playbackRates {
			value := playbackRateLabel(rate)
			option(value, value, "", v.Playback.Rate() == rate, func() {
				v.Playback.SetRate(rate)
				s.closePanel(v)
				s.content.notice(text.Speed + " " + value)
			})
		}
	case playerSubtitles:
		option("", text.Off, "", s.content.subtitle == "", func() { s.content.setSubtitle(v, provider.PlaybackTrack{}); s.closePanel(v) })
		for _, track := range plan.Subtitles {
			option(track.ID, track.Label, "", s.content.subtitle == track.ID, func() {
				s.content.setSubtitle(v, track)
				s.closePanel(v)
			})
		}
	case playerQuality:
		isAuto := state.Selected == ""
		for _, quality := range plan.Qualities {
			option(quality.ID, quality.Label, p.playbackLimitLabel(quality.Limit), !isAuto && plan.Quality == quality.ID, func() {
				s.content.requestQuality = quality.ID
				if quality.Limit == provider.PlaybackLogin && v.openPlaybackSignIn() {
					return
				}
				v.Playback.ChooseQuality(quality.ID)
				s.closePanel(v)
			})
		}
		autoNote := ""
		if isAuto {
			autoNote = playbackQualityLabel(state)
		}
		option("", text.Auto, autoNote, isAuto, func() { v.Playback.ChooseQuality(""); s.closePanel(v) })
	case playerDanmaku:
		p.playbackDanmakuOptions(c)
	}
}

func (p *PageContext) playbackOption(c *ui.Context, id app.FocusID, label, note string, selected bool, action func()) ui.Element {
	colors := p.Theme.Colors
	focus := p.paintFocus() == id
	fill, ink, sub := ui.Transparent, paint(colors.Playback.Foreground), paint(colors.Playback.Secondary)
	if selected {
		fill, ink = withAlpha(colors.Playback.Foreground, 0x04), paint(colors.Accent.Soft)
	}
	if focus {
		fill, ink, sub = paint(colors.Focus.Background), paint(colors.Focus.Foreground), paint(colors.Focus.Foreground)
	}
	button := ui.Button(c.Key(string(id)), "").Label(label).FillWidth().MinHeight(52).Padding(9, 16).Radius(11).
		AccessSelected(selected).Background(fill).Border(0, ui.Transparent).
		OnClick(func() { p.dispatch(action) })
	p.bind(button, id, playerPanelGroup)
	button.Children(func() {
		ui.Row(c).FillWidth().Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Column(c).Grow(1).Gap(4).Children(func() {
				ui.Text(c, label).FontSize(21).TextColor(ink).MaxLines(2)
				if note != "" {
					ui.Text(c, note).FontSize(13).TextColor(sub).MaxLines(2)
				}
			})
			if selected {
				ui.Icon(c, p.Assets.Icon("check")).Size(22, 22).TextColor(ink)
			} else if p.host.player.panel == playerQuality && note != "" {
				icon, iconInk := "info", sub
				if note == p.playbackLimitLabel(provider.PlaybackLogin) || note == p.playbackLimitLabel(provider.PlaybackMembership) {
					icon, iconInk = "lock", paint(colors.Accent.Primary)
				}
				ui.Icon(c, p.Assets.Icon(icon)).Size(22, 22).TextColor(iconInk)
			}
		})
	})
	return button
}

func (p *PageContext) playbackDrawer(c *ui.Context, current playbackItem) {
	v, s, colors, d := p.host, &p.host.player, p.Theme.Colors, p.uiColors()
	text := p.Assets.Text.Playback
	drawer := ui.Column(c.Key("player-drawer")).Absolute().Top(0).Bottom(0).Right(0).Width(playerDrawerWidth).
		Background(paint(colors.Surface.Panel)).Clip().Transition(menuTransition)
	ui.Box(c.Key("player-drawer-edge")).Absolute().Top(0).Bottom(0).Right(playerDrawerWidth - 1).Width(1).
		Background(paint(colors.Border.Subtle))
	if s.panel == playerComments {
		p.playbackDiscussion(c, drawer)
		return
	}
	p.region(playerPanelGroup, focusEdges{})
	hasEpisodes := !s.keepQueue && len(s.items) > 1
	if !hasEpisodes {
		p.bind(drawer.Focusable().Label(text.Info), playerPanelExit, playerPanelGroup)
	}
	drawer.Children(func() {
		ui.Scroll(c).Fill().TrackScroll(&s.panelScroll).Children(func() {
			ui.Column(c).FillWidth().Padding(65, 42).Gap(22).Children(func() {
				ui.Text(c, text.Info).FontSize(32).FontWeight(550)
				if creator := current.item.Creator; creator != nil {
					ui.Text(c, creator.Name).FontSize(16).Margin(5, 0, 0, 0).TextColor(paint(colors.Text.Secondary))
				}
				if current.item.Subtitle != "" {
					ui.Text(c, current.item.Subtitle).FontSize(22).Margin(12, 0, 0, 0).LineHeight(1.8).
						TextColor(paint(colors.Text.Secondary))
				}
				if hasEpisodes {
					p.styledButton(c, text.Episodes, "following", playerPanelExit, playerPanelGroup,
						func() { v.moveFocus(playerEpisodeID(s.index), app.Down, true) }, buttonAppearance{
							height: 62, padding: 27, radius: 12, fontSize: 22, iconSize: 28, iconGap: 13,
							background: d.secondaryFill, foreground: d.secondaryText,
						}).Margin(8, 0, 0, 0)
					ui.Row(c).FillWidth().Margin(30, 0, 4, 0).AlignItems(ui.Baseline).Justify(ui.SpaceBetween).Children(func() {
						ui.Text(c, text.Episodes).FontSize(24).TextColor(paint(colors.Text.Secondary))
						ui.Text(c, fmt.Sprintf("%d / %d", min(max(1, s.index+1), len(s.items)), len(s.items))).FontSize(17).
							TextColor(paint(colors.Text.Muted))
					})
					ui.Column(c).FillWidth().Gap(16).Children(func() {
						for i, row := range s.items {
							p.playbackEpisodeRow(c, row, i)
						}
					})
				}
			})
		})
	})
}

func (p *PageContext) playbackEpisodeRow(c *ui.Context, row playbackItem, index int) {
	v, s, colors, d := p.host, &p.host.player, p.Theme.Colors, p.uiColors()
	id := playerEpisodeID(index)
	title := row.label
	if title == "" {
		title = row.item.Title
	}
	number := fmt.Sprintf("%02d", index+1)
	selected, focused := index == s.index, p.paintFocus() == id
	fill, ink := d.subtleFill, paint(colors.Text.Primary)
	meta, icon := paint(colors.Text.Muted), "play"
	if selected {
		fill, icon = d.subtleHoverFill, "check"
	}
	if focused {
		fill, ink, meta = paint(colors.Focus.Background), paint(colors.Focus.Foreground), d.focusSubtext
	}
	button := ui.Button(c.Key(string(id)), "").Label(number+" "+title).FillWidth().Height(80).Radius(13).
		Padding(0, 20).Background(fill).Border(1, d.subtleBorder).
		AccessSelected(selected).OnClick(func() {
		p.dispatch(func() {
			if !selected {
				v.startPlaybackItem(index, nil)
			}
			s.closePanel(v)
		})
	})
	p.bind(button, id, playerPanelGroup)
	button.Children(func() {
		ui.Row(c).FillWidth().Gap(17).AlignItems(ui.Center).Children(func() {
			ui.Text(c, number).FontSize(25).FontWeight(450).TextColor(meta)
			ui.Text(c, title).Grow(1).Shrink(1).FontSize(20).MaxLines(1).TextColor(ink)
			ui.Icon(c, p.Assets.Icon(icon)).Size(21, 21).TextColor(meta)
		})
	})
}

func (p *PageContext) playbackDiscussion(c *ui.Context, drawer ui.Element) {
	detail := p.host.discussionDetail()
	d := detail.videoDiscussion()
	fill := paint(p.Theme.Colors.Surface.Panel)
	p.region(videoDiscussionRegion, focusEdges{})
	drawer.Children(func() {
		p.videoDiscussionHeader(c, d, detail.item.VideoDetails, fill, playerDiscussionLayout)
		p.videoDiscussionRows(c, d, detail.item.VideoDetails, playerDiscussionLayout)
		p.videoDiscussionComposer(c, d, fill)
	})
}

func playbackBlocking(state app.PlaybackState) bool {
	return playbackHasAccessDialog(state) || state.Failure != "" || state.Clock.Ended
}

func (p *PageContext) playbackLimitLabel(limit provider.PlaybackLimit) string {
	text := p.Assets.Text.Playback
	switch limit {
	case provider.PlaybackLogin:
		return text.LoginLimit
	case provider.PlaybackMembership:
		return p.sourceText(text.MembershipLimit)
	case provider.PlaybackPurchase:
		return text.PurchaseLimit
	case provider.PlaybackRegion:
		return text.RegionLimit
	case provider.PlaybackDevice:
		return text.DeviceLimit
	case provider.PlaybackSource:
		return text.SourceLimit
	}
	return ""
}

func (p *PageContext) playbackToast(c *ui.Context, message string, expires time.Time) {
	if message == "" || !c.Now().Before(expires) {
		return
	}
	colors := p.Theme.Colors
	ui.Column(c).Absolute().Top(154).Left(p.Width/2 - 180).Width(360).AlignItems(ui.Center).Children(func() {
		ui.Text(c, message).FontSize(20).Padding(12, 22).Radius(11).
			Background(paint(colors.Surface.Panel)).TextColor(paint(colors.Playback.Foreground))
	})
}
