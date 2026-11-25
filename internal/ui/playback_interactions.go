package tvui

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/provider"
)

const (
	playerFavoritesNew  app.FocusID = "player/favorites-new"
	playerFavoritesSave app.FocusID = "player/favorites-save"
	playerFavoritesBack app.FocusID = "player/favorites-back"
	playerFavoritesArea             = "player-favorites-chooser"
	playerFavoritesRows             = "player-favorites-rows"
)

type playerFavoriteChooser struct {
	folders []provider.VideoFavoriteFolder
	checked map[string]bool
	cancel  context.CancelFunc
	scroll  ui.ScrollState
	failure provider.ErrorCode
	writing bool
}

func (s *playbackContent) overlay() bool { return s.chooser != nil || s.draft != nil }

func (s *playbackContent) closeChooser() {
	if s.chooser != nil && s.chooser.cancel != nil {
		s.chooser.cancel()
	}
	s.chooser = nil
}

func (s *playbackView) backFromContent(v *View) bool {
	if draft := s.content.draft; draft != nil {
		if draft.cancel != nil {
			return true
		}
		target := draft.returnFocus
		if target == "" {
			target = playbackOptionID(playerDanmaku, "send")
		}
		if target == playerLiveCompose && s.panel != playerComments {
			target = playerComments
		}
		s.content.draft = nil
		v.State.Focus, v.reveal = target, true
		return true
	}
	if chooser := s.content.chooser; chooser != nil {
		if chooser.writing {
			return true
		}
		s.content.closeChooser()
		s.restoreControl(v, playerFavorite)
		return true
	}
	d := s.discussion.discussion
	if s.panel == playerComments && d != nil && d.root != nil {
		v.closeVideoThread()
		return true
	}
	return false
}

func (p *PageContext) withPlayerAccount(action func()) {
	v := p.host
	if v.Auth.Accounts().CurrentID != "" {
		action()
		return
	}
	v.openSignIn()
	if isSignInPage(v.State.Page) {
		v.player.content.afterLogin = action
	}
}

func (v *View) discussionDetail() *videoDetailState {
	if v.Playback != nil && v.Playback.Active() && v.State.Page != app.VideoDetail {
		return &v.player.discussion
	}
	return &v.videoDetail
}

func (p *PageContext) togglePlayerLike() {
	p.withPlayerAccount(func() {
		detail := p.host.discussionDetail()
		if detail.operation != nil {
			return
		}
		liked := detail.item.VideoDetails != nil && detail.item.VideoDetails.Engagement.Liked
		p.editVideo(provider.VideoEdit{Ref: detail.item.Ref, Interaction: provider.VideoLike, Enabled: !liked})
	})
}

func (p *PageContext) toggleLiveFollow() {
	p.withPlayerAccount(func() {
		v, s := p.host, &p.host.player
		if s.followPending != nil {
			return
		}
		item := s.current().item
		if item.Creator == nil || item.Creator.ID == "" {
			return
		}
		creator := *item.Creator
		want := !creator.Following
		text := p.Assets.Text.Detail
		s.followPending = v.Catalog.FollowCreator(provider.CreatorFollowRequest{
			CreatorID: creator.ID,
			Enabled:   want,
		}, func(result provider.CreatorFollowResult, err error) {
			s.followPending = nil
			if err != nil {
				if provider.CodeOf(err) == provider.NeedsSignIn {
					v.openSignIn()
					if isSignInPage(v.State.Page) {
						s.content.afterLogin = p.toggleLiveFollow
					}
					return
				}
				s.content.notice(text.UpdateFailed)
				return
			}
			v.syncCreatorFollowing(creator, result.Following)
			if result.Following {
				s.content.notice(text.Followed)
			} else {
				s.content.notice(text.Unfollowed)
			}
		})
	})
}

func (v *View) syncPlaybackEngagement(request provider.VideoEdit, result provider.VideoEditResult) {
	s := &v.player
	item := s.discussion.item
	if s.current().item.Ref == request.Ref {
		s.items[s.index].item = item
	}
	if v.videoDetail.item.Ref == request.Ref && (result.Liked != nil || result.Favorite != nil || len(result.ActionStates) != 0) {
		v.videoDetail.applyVideoEdit(request, result)
	}
}

func (p *PageContext) openPlayerFavorites() {
	p.withPlayerAccount(func() {
		s := &p.host.player.content
		s.chooser = &playerFavoriteChooser{checked: make(map[string]bool)}
		p.State.Focus = playerFavoritesBack
		p.loadPlayerFavorites(false)
	})
}

func (p *PageContext) loadPlayerFavorites(preserve bool) {
	s := &p.host.player
	chooser := s.content.chooser
	if chooser == nil || chooser.writing {
		return
	}
	if chooser.cancel != nil {
		chooser.cancel()
	}
	chooser.failure = ""
	part := s.current()
	chooser.cancel = p.host.Catalog.ReadVideoFavoriteFolders(part.item.Ref, func(folders []provider.VideoFavoriteFolder, err error) {
		chooser.cancel = nil
		if err != nil {
			chooser.failure = provider.CodeOf(err)
			return
		}
		checks := make(map[string]bool, len(folders))
		for _, folder := range folders {
			checked, exists := chooser.checked[folder.ID]
			if !exists || !preserve {
				checked = folder.Selected
			}
			checks[folder.ID] = checked
		}
		chooser.folders, chooser.checked = folders, checks
		if len(folders) > 0 && p.State.Page == app.Player {
			p.State.Focus, p.host.reveal = playerFolderID(folders[0].ID), true
		}
	})
}

func (p *PageContext) savePlayerFavorites() {
	v, s := p.host, &p.host.player
	chooser := s.content.chooser
	if chooser == nil || chooser.writing || chooser.cancel != nil || chooser.failure != "" {
		return
	}
	edit := provider.VideoFavoriteEdit{Ref: s.current().item.Ref}
	var favorite bool
	for _, folder := range chooser.folders {
		checked := chooser.checked[folder.ID]
		favorite = favorite || checked
		if checked == folder.Selected {
			continue
		}
		if checked {
			edit.Add = append(edit.Add, folder.ID)
		} else {
			edit.Remove = append(edit.Remove, folder.ID)
		}
	}
	if len(edit.Add)+len(edit.Remove) == 0 {
		s.backFromContent(v)
		return
	}
	chooser.writing = true
	chooser.cancel = v.Catalog.EditVideoFavoriteFolders(edit, func(err error) {
		chooser.cancel, chooser.writing = nil, false
		if err != nil {
			chooser.failure = provider.CodeOf(err)
			return
		}
		request := provider.VideoEdit{Ref: edit.Ref, Interaction: provider.VideoFavorite}
		result := provider.VideoEditResult{Favorite: &favorite}
		v.discussionDetail().applyVideoEdit(request, result)
		v.syncPlaybackEngagement(request, result)
		v.Activity.Refresh(provider.AccountQuery{Section: provider.AccountFavorites})
		s.backFromContent(v)
		s.content.notice(p.Assets.Text.Playback.FavoriteSaved)
	})
}

func playerFolderID(id string) app.FocusID { return "player/folder/" + app.FocusID(id) }

func (p *PageContext) drawFavoriteChooser(c *ui.Context, chooser *playerFavoriteChooser) {
	colors := p.Theme.Colors
	text, library := p.Assets.Text.Playback, p.Assets.Text.Activity
	entry, _ := p.State.Current()
	canCreate := entry.Capabilities.Has(provider.EditFavoriteFolders)
	firstAction := playerFavoritesSave
	if canCreate {
		firstAction = playerFavoritesNew
	}
	p.region(playerFavoritesRows, focusEdges{app.Down: {Focus: firstAction}})
	var actionEdges focusEdges
	if len(chooser.folders) > 0 {
		last := chooser.folders[len(chooser.folders)-1]
		actionEdges = focusEdges{app.Up: {Focus: playerFolderID(last.ID)}}
	}
	p.region(playerFavoritesArea, actionEdges)
	modalText := p.Assets.Text.Modal
	p.renderPlayerDrawer(c, "player-favorites", 620, func() {
		p.drawerHeader(c, "COLLECTION \u00b7 SAVE", p.sourceFolderText(text.PickFavoriteFolders), "", p.host.player.current().item.Title)
		switch {
		case chooser.failure != "":
			ui.Text(c, p.requestError(chooser.failure)).FontSize(18).TextColor(paint(colors.Status.Error.Foreground))
		case chooser.cancel != nil && !chooser.writing:
			ui.Text(c, p.Assets.Text.LoadingTitle).FontSize(18).TextColor(paint(colors.Text.Secondary))
		default:
			ui.Scroll(c).FillWidth().Grow(1).Margin(0, 0, 18, 0).TrackScroll(&chooser.scroll).Children(func() {
				ui.Column(c).Gap(10).FillWidth().Children(func() {
					for _, folder := range chooser.folders {
						p.drawFavoriteFolder(c, folder, chooser)
					}
				})
			})
		}
		if canCreate {
			p.styledButton(c, p.sourceFolderText(library.CreateFolder), "plus", playerFavoritesNew, playerFavoritesArea, func() {
				p.openFavoriteDialog(favoriteDialog{mode: favoriteCreate, saved: func() { p.loadPlayerFavorites(true) }}, favoriteDialogName)
			}, buttonAppearance{
				height: 46, padding: 12, radius: 10, fontSize: 19, iconSize: 21,
				foreground: paint(colors.Text.Secondary), focusShadow: true,
			}).Margin(0, 0, 18, 0).Disabled(chooser.writing)
		}
		ui.Row(c).FillWidth().Gap(12).Children(func() {
			if chooser.failure != "" {
				p.styledButton(c, p.Assets.Text.Retry, "", playerFavoritesSave, playerFavoritesArea, func() { p.loadPlayerFavorites(false) }, p.drawerButtonAppearance())
			} else {
				p.styledButton(c, library.SaveFolder, "", playerFavoritesSave, playerFavoritesArea, p.savePlayerFavorites, p.drawerButtonAppearance()).
					Disabled(chooser.cancel != nil)
			}
			p.styledButton(c, p.Assets.Text.Detail.Reconsider, "", playerFavoritesBack, playerFavoritesArea, func() {
				p.host.player.backFromContent(p.host)
			}, p.drawerButtonAppearance()).Disabled(chooser.writing)
		})
		p.drawerFooter(c, modalText.HintSelect, modalText.HintSave)
	})
}

func (p *PageContext) drawFavoriteFolder(c *ui.Context, folder provider.VideoFavoriteFolder, chooser *playerFavoriteChooser) {
	id := playerFolderID(folder.ID)
	selected := chooser.checked[folder.ID]
	colors, d, text := p.Theme.Colors, p.uiColors(), p.Assets.Text.Activity
	background, ink := d.subtleFill, paint(colors.Text.Primary)
	secondary, glyph := paint(colors.Text.Muted), paint(colors.Accent.Soft)
	checkBorder := withAlpha(colors.Text.Primary, 0x50)
	if p.paintFocus() == id {
		background, ink, secondary = paint(colors.Focus.Background), paint(colors.Focus.Foreground), paint(colors.Focus.Foreground)
		glyph = ink
		checkBorder = withAlpha(colors.Focus.Foreground, 0x50)
	}
	button := ui.Button(c.Key(string(id)), "").Label(folder.Title).FillWidth().Height(91).Radius(12).Padding(16, 20).
		Background(background).Border(0, ui.Transparent).Role(ui.RoleCheckBox).Checked(selected).
		OnClick(func() {
			p.dispatch(func() {
				if !chooser.writing {
					chooser.checked[folder.ID] = !selected
				}
			})
		})
	p.bind(button, id, playerFavoritesRows)
	button.Children(func() {
		ui.Row(c).Fill().Gap(20).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, p.Assets.Icon("folder")).Size(27, 27).TextColor(glyph)
			ui.Column(c).Grow(1).Gap(7).Children(func() {
				ui.Text(c, folder.Title).FontSize(23).FontWeight(450).TextColor(ink).MaxLines(1)
				privacy := text.FolderPublic
				if folder.Private {
					privacy = text.FolderPrivate
				}
				ui.Text(c, videoDetailNumbers.Sprintf(text.FolderCount, folder.Count)+" · "+privacy).FontSize(16).TextColor(secondary)
			})
			check := ui.Box(c).Size(27, 27).Radius(7).Center().Border(1, checkBorder)
			if selected {
				check.Background(paint(colors.Accent.Primary)).Border(1, paint(colors.Accent.Primary)).Children(func() {
					ui.Icon(c, p.Assets.Icon("check")).Size(19, 19).TextColor(paint(colors.Accent.Foreground))
				})
			}
		})
	})
}

var commentTimestamp = regexp.MustCompile(`\b(?:[0-9]{1,2}:)?[0-9]{1,3}:[0-5][0-9]\b`)

func commentTimeSeconds(label string) float64 {
	seconds := 0
	for field := range strings.SplitSeq(label, ":") {
		value, _ := strconv.Atoi(field)
		seconds = 60*seconds + value
	}
	return float64(seconds)
}

func (p *PageContext) videoCommentTimestamp(c *ui.Context, comment provider.VideoComment, inPlayer bool) bool {
	match := commentTimestamp.FindStringIndex(comment.Body)
	if match == nil || match[0] != 0 {
		return false
	}
	label := comment.Body[:match[1]]
	seconds := commentTimeSeconds(label)
	item := p.host.videoDetail.item
	duration := item.DurationSeconds
	action := func() { p.startDetailPlayback(item, &seconds) }
	if inPlayer {
		state := p.host.Playback.State()
		if state.Duration <= 0 || seconds >= state.Duration {
			return false
		}
		action = func() { p.seekPlaybackComment(seconds) }
	} else {
		if !p.canPlay(item.Ref) {
			return false
		}
		if index := p.host.videoDetail.episodeIndex(); index >= 0 {
			if episode := videoEpisodes(item.VideoDetails)[index]; episode.DurationSeconds != nil {
				duration = episode.DurationSeconds
			}
		}
		if duration != nil && seconds >= float64(*duration) {
			return false
		}
	}
	ui.Row(c).FillWidth().Gap(8).Margin(11, 0, 9, 0).AlignItems(ui.Start).Children(func() {
		p.commentTimeButton(c, comment.ID, label, 22, action).Shrink(0)
		trimmed := comment
		trimmed.Body = strings.TrimSpace(comment.Body[match[1]:])
		p.videoCommentText(c, trimmed).Grow(1).Shrink(1).FontSize(22).FontWeight(400).LineHeight(1.65).
			TextColor(paint(p.Theme.Colors.Text.Primary))
	})
	return true
}

func (p *PageContext) seekPlaybackComment(seconds float64) {
	p.host.Playback.Seek(seconds)
	p.host.player.closePanel(p.host)
}

func (p *PageContext) commentTimeButton(c *ui.Context, comment, label string, size float32, action func()) ui.Element {
	id := app.FocusID("video-discussion/time/" + comment + "/" + label)
	colors := p.Theme.Colors
	fill, ink := ui.Transparent, paint(colors.Accent.Soft)
	if p.paintFocus() == id {
		fill, ink = paint(colors.Focus.Background), paint(colors.Focus.Foreground)
	}
	button := ui.Button(c.Key(string(id)), label).Label(fmt.Sprintf(p.Assets.Text.Playback.SeekTo, label)).
		Height(36).Padding(0, 3).Radius(4).FontSize(size).TextColor(ink).Background(fill).Border(0, ui.Transparent).
		OnClick(func() { p.dispatch(action) })
	p.bind(button, id, videoDiscussionRegion)
	return button
}

func (p *PageContext) playbackCommentLinks(c *ui.Context, comment provider.VideoComment) {
	state := p.host.Playback.State()
	if state.Duration <= 0 {
		return
	}
	matches := commentTimestamp.FindAllString(comment.Body, -1)
	if len(matches) == 0 {
		return
	}
	ui.Row(c).Gap(8).Children(func() {
		seen := make(map[string]bool)
		for _, label := range matches {
			if seen[label] {
				continue
			}
			seen[label] = true
			seconds := commentTimeSeconds(label)
			if seconds >= state.Duration {
				continue
			}
			p.commentTimeButton(c, comment.ID, label, 17, func() { p.seekPlaybackComment(seconds) })
		}
	})
}
