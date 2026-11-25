package tvui

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/player"
	"github.com/yuchanns/MikuTV/internal/provider"
)

const (
	homePlayAction           app.FocusID = "home/play"
	homeDetailAction         app.FocusID = "home/details"
	homeLaterAction          app.FocusID = "home/watch-later"
	homeHeroPreviewDelay                 = 1200 * time.Millisecond
	homeHeroPreviewMaxScroll             = 260
)

type catalogHeroPreview struct {
	ref    provider.MediaRef
	due    time.Time
	failed map[provider.MediaRef]bool
	logged provider.MediaRef
}

func (hp *catalogHeroPreview) reset() {
	hp.ref, hp.due, hp.failed, hp.logged = provider.MediaRef{}, time.Time{}, nil, provider.MediaRef{}
}

func catalogPreviewEligibleFocus(focus app.FocusID) bool {
	switch focus {
	case homePlayAction, homeDetailAction, homeLaterAction:
		return true
	}
	key := string(focus)
	return strings.HasPrefix(key, "home/feature/") || strings.HasPrefix(key, "home/card/")
}

func (v *View) stopHomePreview() {
	v.home.heroPreview.ref = provider.MediaRef{}
	v.home.heroPreview.due = time.Time{}
	v.home.heroPreview.logged = provider.MediaRef{}
	if v.Playback != nil && v.Playback.InlinePreviewState().Active {
		if v.Trace {
			log.Printf("hero-preview: stop")
		}
		v.Playback.StopInlinePreview()
	}
}

func (v *View) homePreviewPlayingFor(ref provider.MediaRef) bool {
	if v.Playback == nil || ref.ID == "" {
		return false
	}
	ps := v.Playback.InlinePreviewState()
	return ps.Active && ps.HasFrame && ps.Target.Ref == ref
}

func (v *View) homePreviewShowing(layers []app.Location) bool {
	if len(layers) != 1 || layers[0].Page != app.Home || v.home.tab != "" || v.State.Area != app.ContentArea ||
		!v.hoverPreviewEnabled() || v.Playback == nil || v.Playback.Active() {
		return false
	}
	if len(v.home.featured) == 0 || v.home.selected < 0 || v.home.selected >= len(v.home.featured) {
		return false
	}
	item := v.home.featured[v.home.selected]
	return v.homePreviewPlayingFor(item.Ref)
}

func (p *PageContext) syncCatalogHeroPreview(c *ui.Context, snapshot app.CatalogSnapshot) {
	v := p.host
	home := &v.home
	hp := &home.heroPreview
	entry, _ := p.State.Current()
	scrollY := p.scroll().Y
	if !p.active || p.State.Area != app.ContentArea || !v.hoverPreviewEnabled() || v.reduceMotionEnabled(c) ||
		home.tab != "" || len(home.featured) == 0 || scrollY >= homeHeroPreviewMaxScroll ||
		snapshot.Phase != app.CatalogReady || v.Playback == nil || v.Playback.Active() ||
		!entry.Capabilities.Has(provider.ResolvePlayback) || !catalogPreviewEligibleFocus(p.State.Focus) {
		v.stopHomePreview()
		return
	}
	item := home.featured[home.selected]
	if !item.Ref.Kind.Valid() || item.Ref.EntryID != entry.ID || hp.failed[item.Ref] {
		v.stopHomePreview()
		return
	}
	if item.Ref != hp.ref {
		v.stopHomePreview()
		hp.ref = item.Ref
		hp.due = c.Now().Add(homeHeroPreviewDelay)
	}
	ps := v.Playback.InlinePreviewState()
	if ps.Target.Ref == item.Ref && ps.Failed {
		if hp.failed == nil {
			hp.failed = make(map[provider.MediaRef]bool)
		}
		hp.failed[item.Ref] = true
		v.stopHomePreview()
		return
	}
	if ps.Active && ps.Target.Ref == item.Ref {
		railW := v.railSpan
		if railW <= 0 {
			railW = collapsedRailWidth
		}
		totalW := p.Width + collapsedRailWidth
		heroH := max(0, float32(homeArtHeight)-scrollY)
		v.Playback.SetInlinePreviewRect(player.ViewportRect{
			X:     float64(railW),
			Y:     0,
			W:     float64(max(0, totalW-railW)),
			H:     float64(heroH),
			Cover: true,
		})
		v.Playback.TickInlinePreview(c.Now())
		ps = v.Playback.InlinePreviewState()
		if ps.HasFrame && hp.logged != item.Ref {
			hp.logged = item.Ref
			if v.Trace {
				log.Printf("hero-preview: ready ref=%s", item.Ref.ID)
			}
		}
		if ps.HasFrame {
			c.After(250 * time.Millisecond)
		} else {
			c.After(120 * time.Millisecond)
		}
		return
	}
	if remaining := hp.due.Sub(c.Now()); remaining > 0 {
		c.After(remaining)
		return
	}
	railW := v.railSpan
	if railW <= 0 {
		railW = collapsedRailWidth
	}
	totalW := p.Width + collapsedRailWidth
	heroH := max(0, float32(homeArtHeight)-scrollY)
	if v.Trace {
		log.Printf("hero-preview: start ref=%s", item.Ref.ID)
	}
	v.Playback.StartInlinePreview(provider.PlaybackTarget{Ref: item.Ref})
	v.Playback.SetInlinePreviewRect(player.ViewportRect{
		X:     float64(railW),
		Y:     0,
		W:     float64(max(0, totalW-railW)),
		H:     float64(heroH),
		Cover: true,
	})
	c.After(120 * time.Millisecond)
}

type catalogFeatureInfo struct {
	ref    provider.MediaRef
	due    time.Time
	cancel context.CancelFunc
	loaded map[provider.MediaRef]provider.MediaItem
}

func (info *catalogFeatureInfo) stop() {
	if info.cancel != nil {
		info.cancel()
	}
	info.ref, info.due, info.cancel = provider.MediaRef{}, time.Time{}, nil
}

func (info *catalogFeatureInfo) reset() {
	info.stop()
	info.loaded = nil
}

func (p *PageContext) syncCatalogFeatureInfo(c *ui.Context, snapshot app.CatalogSnapshot) {
	home := &p.host.home
	info := &home.featureInfo
	entry, _ := p.State.Current()
	if !p.active || !p.host.hoverPreviewEnabled() || home.tab != "" || len(home.featured) == 0 || p.scroll().Y >= homeHeroHeight ||
		snapshot.Phase != app.CatalogReady || !entry.Capabilities.Has(provider.GetCatalogItem) {
		info.stop()
		return
	}
	item := home.featured[home.selected]
	if item.Ref != info.ref {
		info.stop()
		info.ref, info.due = item.Ref, c.Now().Add(450*time.Millisecond)
	}
	if _, seen := info.loaded[item.Ref]; seen || info.cancel != nil ||
		(item.Category != "" && item.Subtitle != "" && item.Quality != "") {
		return
	}
	if remaining := info.due.Sub(c.Now()); remaining > 0 {
		c.After(remaining)
		return
	}
	info.cancel = p.host.Catalog.ReadItem(item.Ref, func(loaded provider.MediaItem, _ error) {
		if info.loaded == nil {
			info.loaded = make(map[provider.MediaRef]provider.MediaItem)
		}
		info.loaded[item.Ref], info.cancel = loaded, nil
	})
}

func (p *PageContext) catalogBackdrop(c *ui.Context, item provider.MediaItem) {
	colors := p.Theme.Colors
	canvas := paint(colors.Surface.Canvas)
	image := p.catalogBitmap(catalogImageKey{location: item.BackdropURL, backdrop: true})
	if image == nil {
		image = p.catalogCover(item.CoverURL)
	}
	ui.Box(c.Key("home-backdrop")).Absolute().Left(0).Right(0).Top(0).Height(homeArtHeight).Clip().Children(func() {
		transition, exit := p.catalogSlide(c)
		artOpacity := float32(1)
		if p.host.homePreviewPlayingFor(item.Ref) {
			artOpacity = 0
		}
		track := ui.Box(c.Key("home-art-track")).Fill().ChildExit(exit)
		fadeAnim := ui.Animation{Duration: 280 * time.Millisecond, Ease: ui.EaseOut, NonMotion: true}
		if p.host.reduceMotionEnabled(c) {
			fadeAnim = ui.Animation{}
		}
		fade := track.AnimateTo("preview-fade", artOpacity, fadeAnim)
		track.Opacity(fade).Children(func() {
			ui.Box(c.Key(item.Ref)).Absolute().Left(0).Top(0).Fill().Transition(transition).Children(func() {
				if image != nil {
					ui.Image(c, image).Fill().Fit(ui.Cover).ObjectPosition(0.6, 0.46).
						Transition(ui.ElementTransition{Duration: 160 * time.Millisecond, Enter: &ui.Motion{Opacity: 0}})
				}
			})
		})
		ui.Box(c).Absolute().Left(0).Top(0).Fill().LinearGradient(ui.LinearGradient{
			Angle: 90, Stops: []ui.GradientStop{
				{Color: canvas, At: 0},
				{Color: withAlpha(colors.Surface.Canvas, 0xed), At: 0.08},
				{Color: withAlpha(colors.Surface.Canvas, 0x77), At: 0.43},
				{Color: withAlpha(colors.Surface.Canvas, 0), At: 0.71},
			},
		})
		ui.Box(c).Absolute().Left(0).Top(0).Fill().LinearGradient(ui.LinearGradient{
			Angle: 180, Stops: []ui.GradientStop{
				{Color: withAlpha(colors.Surface.Canvas, 0x66), At: 0},
				{Color: withAlpha(colors.Surface.Canvas, 0), At: 0.22},
				{Color: withAlpha(colors.Surface.Canvas, 0), At: 0.55},
				{Color: withAlpha(colors.Surface.Canvas, 0xe8), At: 0.93},
				{Color: canvas, At: 1},
			},
		})
	})
}

func (p *PageContext) catalogSlide(c *ui.Context) (ui.ElementTransition, ui.Motion) {
	if p.host.reduceMotionEnabled(c) {
		return ui.ElementTransition{}, ui.Motion{}
	}
	distance := p.Width
	if p.host.home.featureStep < 0 {
		distance = -distance
	}
	exit := ui.Motion{X: -distance, Opacity: 1}
	return ui.ElementTransition{
		Duration: 360 * time.Millisecond, Ease: ui.EaseInOut, Position: true,
		Enter: &ui.Motion{X: distance, Opacity: 1}, Exit: &exit,
	}, exit
}

func (p *PageContext) catalogHero(c *ui.Context, current app.CatalogSnapshot) {
	home, colors := &p.host.home, p.Theme.Colors
	ui.Box(c.Key("catalog-hero")).FillWidth().Height(homeHeroHeight).Children(func() {
		if len(home.featured) == 0 {
			entry, _ := p.State.Current()
			if current.Phase == app.CatalogFailed && current.Error == provider.NeedsSignIn && entry.Capabilities.Has(provider.SignIn) {
				p.catalogGuestHero(c, entry)
				return
			}
			ui.Box(c).Absolute().Top(76).Left(103).Children(func() {
				p.catalogStatus(c, current)
			})
			p.catalogHeroActions(c, nil)
			return
		}
		if p.scroll().Y >= homeHeaderHeight+homeHeroHeight+homeShelfHeight {
			return
		}
		item := home.featured[home.selected]
		if info := home.featureInfo.loaded[item.Ref]; info.Title != "" {
			if item.Category == "" {
				item.Category = info.Category
			}
			if item.Quality == "" {
				item.Quality = info.Quality
			}
			if item.Subtitle == "" {
				item.Subtitle = info.Subtitle
			}
		}
		p.catalogHeroCopy(c, item, home.selected)
		p.catalogHeroActions(c, &item)
		ui.Text(c, p.Assets.Text.Home.HeroCaption).Absolute().Right(83).Bottom(76).
			FontSize(13).LetterSpacing(3).TextColor(withAlpha(colors.Text.Secondary, 0xb3))
		p.catalogPager(c)
	})
}

func (p *PageContext) catalogGuestHero(c *ui.Context, entry app.Entry) {
	colors := p.Theme.Colors
	title := entry.Auth.GuestTitle
	if title == "" {
		title = entry.Name
	}
	if title == "" {
		title = p.Assets.Text.Auth.AccountTitle
	}
	desc := entry.Auth.GuestDescription
	if desc == "" {
		desc = p.Assets.Text.Activity.SignInNeeded
	}
	ui.Column(c.Key("catalog-guest-hero")).Absolute().Top(112).Left(103).Width(min(760, p.Width-186)).Gap(22).Children(func() {
		ui.Row(c).Height(19).Gap(15).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(24, 1).Background(paint(colors.Accent.Soft))
			ui.Text(c, "SIGN IN").FontSize(16).FontWeight(500).LetterSpacing(5).TextColor(paint(colors.Text.Secondary))
		})
		ui.Text(c, title).FontSize(60).FontWeight(650).LineHeight(1.2).MaxLines(2)
		ui.Text(c, desc).FontSize(22).LineHeight(1.7).MaxLines(3).TextColor(paint(colors.Text.Secondary))
	})
	ui.Row(c).Absolute().Left(103).Top(498).Gap(16).AlignItems(ui.Center).Children(func() {
		style := p.playButtonAppearance()
		style.padding = 28
		p.styledButton(c, p.signInActionLabel(), "user", homePlayAction, homeHero, func() {
			p.host.openSignIn()
		}, style).Center()
	})
}

func (p *PageContext) catalogHeroCopy(c *ui.Context, item provider.MediaItem, selected int) {
	colors := p.Theme.Colors
	transition, exit := p.catalogSlide(c)
	ui.Box(c.Key("hero-copy-track")).Absolute().Left(0).Top(0).Right(0).Height(498).Clip().ChildExit(exit).Children(func() {
		ui.Box(c.Key(item.Ref)).Absolute().Left(0).Top(0).Fill().Transition(transition).Children(func() {
			ui.Row(c).Absolute().Top(76).Left(103).Height(19).Gap(15).AlignItems(ui.Center).Children(func() {
				ui.Box(c).Size(24, 1).Background(paint(colors.Accent.Soft))
				ui.Text(c, catalogEyebrow(c.Now(), selected, p.Assets.Text.Home)).FontSize(16).FontWeight(500).
					LetterSpacing(5).TextColor(paint(colors.Text.Secondary))
			})
			p.catalogHeadline(c, item.Title)
			ui.Box(c).Absolute().Top(346).Left(103).Height(27).Children(func() {
				p.catalogHeroMetadata(c, item)
			})
			if item.Subtitle != "" {
				ui.Text(c, item.Subtitle).Absolute().Top(389).Left(103).Width(min(690, p.Width-186)).
					FontSize(21).LineHeight(1.8).MaxLines(2).TextColor(paint(colors.Text.Secondary))
			}
		})
	})
}

func (p *PageContext) catalogHeadline(c *ui.Context, title string) {
	width := min(767, p.Width-186)
	if strings.Contains(title, "\n") {
		for index, line := range strings.SplitN(title, "\n", 3) {
			if index == 2 {
				break
			}
			ui.Text(c, line).Absolute().Top(112 + 101*float32(index)).Left(103).Width(width).
				FontSize(86).FontWeight(650).LetterSpacing(4).LineHeight(1.17).MaxLines(1)
		}
		return
	}
	ui.Text(c, title).Absolute().Top(108).Left(103).Width(width).
		FontSize(84).FontWeight(650).LetterSpacing(4).LineHeight(1.17).MaxLines(2)
}

func catalogEyebrow(now time.Time, index int, labels homeLabels) string {
	label := labels.EyebrowAfterHours
	switch hour := now.Hour(); {
	case hour >= 5 && hour < 11:
		label = labels.EyebrowMorning
	case hour >= 11 && hour < 14:
		label = labels.EyebrowMidday
	case hour >= 14 && hour < 18:
		label = labels.EyebrowAfternoon
	case hour >= 18 && hour < 23:
		label = labels.EyebrowTonight
	}
	return fmt.Sprintf("%s / %02d", label, index+1)
}

func (p *PageContext) catalogHeroMetadata(c *ui.Context, item provider.MediaItem) {
	colors, d := p.Theme.Colors, p.uiColors()
	secondary := d.secondaryText
	ui.Row(c).Gap(16).AlignItems(ui.Center).Children(func() {
		if item.Quality != "" {
			ui.Text(c, item.Quality).Padding(3, 7).Radius(4).FontSize(13).
				LetterSpacing(1).FontWeight(600).Border(1, paint(colors.Border.Strong)).TextColor(secondary)
		}
		if item.Category != "" {
			ui.Text(c, item.Category).FontSize(19).TextColor(secondary)
		}
		if item.DurationSeconds != nil {
			if item.Category != "" {
				ui.Box(c).Size(3, 3).Radius(2).Background(secondary)
			}
			ui.Text(c, catalogDuration(*item.DurationSeconds)).FontSize(19).TextColor(secondary)
		}
	})
}

func (p *PageContext) catalogHeroActions(c *ui.Context, item *provider.MediaItem) {
	text, colors, d := p.Assets.Text.Home, p.Theme.Colors, p.uiColors()
	entry, _ := p.State.Current()
	ui.Row(c).Absolute().Left(103).Top(498).Gap(16).AlignItems(ui.Center).Children(func() {
		p.styledButton(c, text.Play, "play-filled", homePlayAction, homeHero, func() {
			if item != nil && p.canPlay(item.Ref) {
				if queue, idx := p.playbackQueueFromList(*item, p.host.home.featured, p.host.home.selected); len(queue) > 1 {
					p.openPlayback(queue, idx, true, nil)
					return
				}
				p.openPlayback([]playbackItem{{item: *item}}, 0, false, nil)
				return
			}
			p.openCatalogAction("play", text.Play, nil)
		}, p.playButtonAppearance()).Width(179).Center()
		if item != nil && p.canOpenVideoDetail(item.Ref) {
			p.styledButton(c, text.Details, "info", homeDetailAction, homeHero, func() {
				p.openMediaFromList(*item, p.host.home.featured, p.host.home.selected)
			}, buttonAppearance{
				height: 62, padding: 0, radius: 12, fontSize: 22, iconSize: 28, iconGap: 13, fontWeight: 550,
				background: d.secondaryFill, foreground: d.secondaryText,
				border: d.secondaryBorder,
			}).Width(185).Center()
		}
		if item != nil && p.canEditWatchLater(entry) {
			focused := p.State.Focus == homeLaterAction
			background, ink, borderColor := d.secondaryFill, paint(colors.Text.Primary), d.secondaryBorder
			if focused {
				background, ink = paint(colors.Focus.Background), paint(colors.Focus.Foreground)
				if !p.Theme.Dark && d.focusBorder.A != 0 {
					borderColor = d.focusBorder
				}
			}
			featured := *item
			button := ui.Button(c.Key(string(homeLaterAction)), "").Label(text.Later).Size(62, 62).Radius(12).
				Border(1, borderColor).Background(background).TextColor(ink).Center().
				OnClick(func() { p.dispatch(func() { p.addCatalogWatchLater(featured) }) })
			p.animateFocus(button, background, ink)
			p.bind(button, homeLaterAction, homeHero)
			button.Children(func() { ui.Icon(c, p.Assets.Icon("plus")).Size(28, 28) })
		}
	})
}

func (p *PageContext) addCatalogWatchLater(item provider.MediaItem) {
	v := p.host
	if v.Auth == nil || v.Auth.Accounts().CurrentID == "" {
		v.openSignIn()
		return
	}
	v.Catalog.EditVideo(provider.VideoEdit{
		Ref: item.Ref, Interaction: provider.VideoWatchLater, Enabled: true,
	}, func(_ provider.VideoEditResult, err error) {
		if err != nil {
			v.notice = p.videoOperationError(err, p.Assets.Text.Detail.UpdateFailed)
			return
		}
		v.notice = p.Assets.Text.Detail.LaterAdded
		if v.Activity != nil {
			v.Activity.Refresh(provider.AccountQuery{Section: provider.AccountWatchLater})
		}
	})
}

func (p *PageContext) catalogPager(c *ui.Context) {
	home, text, colors := &p.host.home, p.Assets.Text.Home, p.Theme.Colors
	ui.Row(c.Key("hero-pager")).Absolute().Right(83).Bottom(19).Gap(12).AlignItems(ui.Center).Children(func() {
		ui.Text(c, fmt.Sprintf("%02d / %02d", home.selected+1, len(home.featured))).Margin(0, 20, 0, 0).
			FontSize(13).LetterSpacing(3).TextColor(paint(colors.Text.Secondary))
		for idx := range home.featured {
			id := homeFeatureID(idx)
			length, fill := float32(34), withAlpha(colors.Text.Muted, 0x66)
			if idx == home.selected {
				length, fill = 65, paint(colors.Focus.Background)
			}
			button := ui.Button(c.Key(string(id)), "").Label(fmt.Sprintf(text.Feature, idx+1)).
				Size(length, 24).Padding(0, 0).Radius(4).Border(0, ui.Transparent).Background(ui.Transparent).
				OnClick(func() {
					p.dispatch(func() {
						home.featureStep = 1
						if idx < home.selected {
							home.featureStep = -1
						}
						p.setHomeFocus(id, false)
					})
				})
			p.bind(button, id, homePager)
			button.Children(func() {
				ui.Box(c).Absolute().Left(0).Right(0).Top(10.5).Height(3).Radius(2).Background(fill)
				if p.State.Focus == id {
					ui.Box(c).Absolute().Left(0).Top(0).Fill().Radius(4).Border(1, paint(colors.Focus.Ring))
				}
			})
		}
	})
}
