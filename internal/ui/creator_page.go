package tvui

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/provider"
)

const (
	creatorFollowFocus  app.FocusID = "creator/follow"
	creatorPrivateFocus app.FocusID = "creator/private"
	creatorRetryFocus   app.FocusID = "creator/retry"
	creatorMoreRetry    app.FocusID = "creator/more-retry"

	creatorHeaderRegion  = "creator/header"
	creatorTabsRegion    = "creator/tabs"
	creatorContentRegion = "creator/content"

	creatorCoverHeight = 600
	creatorOverviewMin = 490
	creatorInset       = 110
	creatorContentTop  = 603
	creatorVideoCols   = 4
	creatorVideoGapX   = 30
	creatorVideoGapY   = 35
	creatorVideoCover  = 208
	creatorVideoHeight = 281
	creatorPostCols    = 2
	creatorPostGap     = 32
	creatorPostCover   = 235
)

var defaultCreatorTabs = []provider.CreatorTab{
	provider.CreatorTabVideos,
	provider.CreatorTabPosts,
	provider.CreatorTabCollections,
	provider.CreatorTabAbout,
}

func creatorTabFocus(tab provider.CreatorTab) app.FocusID {
	return app.FocusID("creator/tab/" + string(tab))
}

func creatorVideoFocus(ref provider.MediaRef) app.FocusID {
	return app.FocusID("creator/video/" + string(ref.Kind) + "/" + ref.ID)
}

func creatorPostFocus(id string) app.FocusID {
	return app.FocusID("creator/post/" + id)
}

func creatorCollectionCardFocus(collectionID string, ref provider.MediaRef) app.FocusID {
	return app.FocusID("creator/col/" + collectionID + "/" + string(ref.Kind) + "/" + ref.ID)
}

type creatorPage struct{}

type creatorTabData struct {
	loaded      bool
	loading     bool
	appending   bool
	stalled     bool
	failure     provider.ErrorCode
	nextCursor  string
	items       []provider.MediaItem
	posts       []provider.CreatorPost
	collections []provider.CreatorCollection
	cancel      context.CancelFunc
}

func (d *creatorTabData) cancelLoad() {
	if d.cancel != nil {
		d.cancel()
		d.cancel = nil
	}
	d.loading = false
	d.appending = false
}

func (d *creatorTabData) canLoadMore() bool {
	return d.loaded && !d.loading && d.failure == "" && d.nextCursor != "" && !d.stalled
}

type creatorState struct {
	seed              provider.Creator
	profile           provider.CreatorProfile
	profileLoaded     bool
	profileLoading    bool
	profileFailure    provider.ErrorCode
	profileCancel     context.CancelFunc
	account           string
	tab               provider.CreatorTab
	videos            creatorTabData
	posts             creatorTabData
	collections       creatorTabData
	followPending     context.CancelFunc
	signInAction      func()
	afterLoad         func()
	feedback          videoDetailFeedback
	entrance          videoDetailEntrance
	collectionScrolls map[string]*ui.ScrollState
}

func (s *creatorState) close() {
	if s == nil {
		return
	}
	if s.profileCancel != nil {
		s.profileCancel()
		s.profileCancel = nil
	}
	if s.followPending != nil {
		s.followPending()
		s.followPending = nil
	}
	s.videos.cancelLoad()
	s.posts.cancelLoad()
	s.collections.cancelLoad()
	s.signInAction = nil
	s.afterLoad = nil
}

func (s *creatorState) tabData(tab provider.CreatorTab) *creatorTabData {
	switch tab {
	case provider.CreatorTabVideos:
		return &s.videos
	case provider.CreatorTabPosts:
		return &s.posts
	case provider.CreatorTabCollections:
		return &s.collections
	default:
		return nil
	}
}

func (s *creatorState) visibleTabs() []provider.CreatorTab {
	if len(s.profile.Tabs) != 0 {
		return s.profile.Tabs
	}
	return defaultCreatorTabs
}

func (p *PageContext) canOpenCreator(creator *provider.Creator) bool {
	if creator == nil || strings.TrimSpace(creator.ID) == "" {
		return false
	}
	entry, _ := p.State.Current()
	return entry.Capabilities.Has(provider.ReadCreator)
}

func (p *PageContext) openCreator(creator provider.Creator) {
	if !p.canOpenCreator(&creator) {
		return
	}
	view := p.host
	if p.State.Page == app.Creator && view.creator != nil && view.creator.seed.ID == creator.ID {
		return
	}
	layers := p.State.Layers()
	if !p.State.Push(app.Route{Page: app.Creator}) {
		return
	}
	if view.creator != nil {
		view.creatorStack = append(view.creatorStack, view.creator)
	}
	state := &creatorState{
		seed:    creator,
		profile: provider.CreatorProfile{Creator: creator},
		tab:     provider.CreatorTabVideos,
	}
	if len(layers) == 1 {
		state.entrance.from = layers[0]
	}
	view.creator = state
	entry, _ := p.State.Current()
	if !entry.Capabilities.Has(provider.FollowCreator) {
		if entry.Capabilities.Has(provider.ReadPrivateMessages) && view.PrivateMessages != nil {
			view.State.Focus = creatorPrivateFocus
		} else {
			view.State.Focus = creatorTabFocus(provider.CreatorTabVideos)
		}
	}
	view.reveal = true
	view.loadCreatorInitial(state)
}

func (v *View) closeAllCreators() {
	if v.creator != nil {
		v.creator.close()
		v.creator = nil
	}
	for _, saved := range v.creatorStack {
		saved.close()
	}
	v.creatorStack = nil
}

func (v *View) leaveCreator() {
	needed := v.State.RouteCount(app.Creator)
	have := len(v.creatorStack)
	if v.creator != nil {
		have++
	}
	for have > needed {
		if v.creator != nil {
			v.creator.close()
			v.creator = nil
		}
		if len(v.creatorStack) > 0 {
			last := len(v.creatorStack) - 1
			v.creator = v.creatorStack[last]
			v.creatorStack[last] = nil
			v.creatorStack = v.creatorStack[:last]
		}
		have--
	}
}

func syncCreator(v *View) {
	state := v.creator
	if state == nil {
		return
	}
	currentAccount := ""
	if v.Auth != nil {
		currentAccount = v.Auth.Accounts().CurrentID
	}
	if state.account != currentAccount {
		if state.followPending != nil {
			state.followPending()
			state.followPending = nil
		}
		state.afterLoad = nil
		v.loadCreatorInitial(state)
	}
}

func (v *View) loadCreatorInitial(state *creatorState) {
	if state == nil || v.Catalog == nil {
		return
	}
	if state.profileCancel != nil {
		state.profileCancel()
	}
	state.videos.cancelLoad()
	state.posts.cancelLoad()
	state.collections.cancelLoad()
	if v.Auth != nil {
		state.account = v.Auth.Accounts().CurrentID
	}
	state.profileLoading = true
	state.profileFailure = ""
	state.videos = creatorTabData{loading: state.tab == provider.CreatorTabVideos}
	state.posts = creatorTabData{loading: state.tab == provider.CreatorTabPosts}
	state.collections = creatorTabData{loading: state.tab == provider.CreatorTabCollections}
	tab := state.tab
	if tab == "" {
		tab = provider.CreatorTabVideos
		state.tab = tab
	}
	req := provider.CreatorRequest{
		CreatorID:      state.seed.ID,
		Tab:            tab,
		IncludeProfile: true,
	}
	state.profileCancel = v.Catalog.ReadCreator(req, func(page provider.CreatorPage, err error) {
		state.profileCancel = nil
		state.profileLoading = false
		if data := state.tabData(tab); data != nil {
			data.loading = false
		}
		if err != nil {
			code := provider.CodeOf(err)
			state.profileFailure = code
			state.afterLoad = nil
			if data := state.tabData(tab); data != nil {
				data.failure = code
			}
			return
		}
		if page.Profile != nil {
			mergeCreatorProfile(&state.profile, *page.Profile)
			state.profileLoaded = true
		}
		if data := state.tabData(tab); data != nil {
			applyCreatorTabPage(data, tab, page, false, "")
		}
		tabs := state.visibleTabs()
		if !slices.Contains(tabs, state.tab) && len(tabs) != 0 {
			state.tab = tabs[0]
		}
		resume := state.afterLoad
		state.afterLoad = nil
		if resume != nil && v.State.Page == app.Creator && v.creator == state {
			v.dispatch(resume)
		}
	})
}

func mergeCreatorProfile(dst *provider.CreatorProfile, src provider.CreatorProfile) {
	if src.Creator.AvatarURL == "" {
		src.Creator.AvatarURL = dst.Creator.AvatarURL
	}
	if src.BackdropURL == "" {
		src.BackdropURL = dst.BackdropURL
	}
	if src.Category == "" {
		src.Category = dst.Category
	}
	*dst = src
}

func applyCreatorTabPage(data *creatorTabData, tab provider.CreatorTab, page provider.CreatorPage, appendPage bool, requestedCursor string) {
	nextCursor := page.NextCursor
	if nextCursor == requestedCursor {
		nextCursor = ""
	}
	data.loaded = true
	data.loading = false
	data.appending = false
	data.failure = ""
	data.nextCursor = nextCursor
	var added bool
	switch tab {
	case provider.CreatorTabVideos:
		if !appendPage {
			data.items = slices.Clone(page.Items)
			added = len(data.items) > 0
		} else {
			seen := make(map[provider.MediaRef]struct{}, len(data.items)+len(page.Items))
			for _, item := range data.items {
				seen[item.Ref] = struct{}{}
			}
			for _, item := range page.Items {
				if _, exists := seen[item.Ref]; exists {
					continue
				}
				seen[item.Ref] = struct{}{}
				data.items = append(data.items, item)
				added = true
			}
		}
	case provider.CreatorTabPosts:
		if !appendPage {
			data.posts = slices.Clone(page.Posts)
			added = len(data.posts) > 0
		} else {
			seen := make(map[string]struct{}, len(data.posts)+len(page.Posts))
			for _, post := range data.posts {
				seen[post.ID] = struct{}{}
			}
			for _, post := range page.Posts {
				if _, exists := seen[post.ID]; exists {
					continue
				}
				seen[post.ID] = struct{}{}
				data.posts = append(data.posts, post)
				added = true
			}
		}
	case provider.CreatorTabCollections:
		if !appendPage {
			data.collections = slices.Clone(page.Collections)
			added = len(data.collections) > 0
		} else {
			seen := make(map[string]struct{}, len(data.collections)+len(page.Collections))
			for _, col := range data.collections {
				seen[col.ID] = struct{}{}
			}
			for _, col := range page.Collections {
				if _, exists := seen[col.ID]; exists {
					continue
				}
				seen[col.ID] = struct{}{}
				data.collections = append(data.collections, col)
				added = true
			}
		}
	}
	data.stalled = appendPage && !added
}

func (v *View) loadCreatorTab(state *creatorState, tab provider.CreatorTab, appendPage bool) {
	if state == nil || v.Catalog == nil {
		return
	}
	data := state.tabData(tab)
	if data == nil {
		return
	}
	if appendPage && !data.canLoadMore() {
		return
	}
	if !appendPage && data.loading {
		return
	}
	cursor := ""
	if appendPage {
		cursor = data.nextCursor
	}
	data.cancelLoad()
	data.loading = true
	data.appending = appendPage
	data.failure = ""
	data.stalled = false
	req := provider.CreatorRequest{
		CreatorID: state.seed.ID,
		Tab:       tab,
		Cursor:    cursor,
	}
	data.cancel = v.Catalog.ReadCreator(req, func(page provider.CreatorPage, err error) {
		data.cancel = nil
		data.loading = false
		if err != nil {
			data.failure = provider.CodeOf(err)
			return
		}
		if page.Profile != nil {
			mergeCreatorProfile(&state.profile, *page.Profile)
			state.profileLoaded = true
		}
		applyCreatorTabPage(data, tab, page, appendPage, cursor)
	})
}

func (v *View) selectCreatorTab(tab provider.CreatorTab, direction app.Direction) {
	state := v.creator
	if state == nil {
		return
	}
	state.tab = tab
	v.moveFocus(creatorTabFocus(tab), direction, false)
	if data := state.tabData(tab); data != nil && !data.loaded && !data.loading {
		v.loadCreatorTab(state, tab, false)
	}
}

func (p *PageContext) toggleCreatorFollow() {
	state := p.host.creator
	if state == nil {
		return
	}
	entry, _ := p.State.Current()
	if !entry.Capabilities.Has(provider.FollowCreator) {
		return
	}
	if p.host.Auth != nil && p.host.Auth.Accounts().CurrentID == "" {
		p.openCreatorSignIn(func() {
			if p.host.creator == state && !state.profile.Creator.Following {
				p.performCreatorFollow(state, true)
			}
		})
		return
	}
	p.performCreatorFollow(state, !state.profile.Creator.Following)
}

func (p *PageContext) openCreatorSignIn(action func()) bool {
	p.host.openSignIn()
	if !isSignInPage(p.State.Page) || p.host.creator == nil {
		return false
	}
	p.host.creator.signInAction = action
	return true
}

func (v *View) resumeCreatorAfterSignIn(action func()) {
	if v.State.Page != app.Creator || v.creator == nil || v.Auth == nil || v.Auth.Accounts().CurrentID == "" {
		return
	}
	syncCreator(v)
	state := v.creator
	if state.profileLoading {
		state.afterLoad = action
	} else if state.profileFailure == "" {
		v.dispatch(action)
	}
}

func (p *PageContext) performCreatorFollow(state *creatorState, enabled bool) {
	if state.followPending != nil || p.host.Catalog == nil {
		return
	}
	text := p.Assets.Text.Detail
	req := provider.CreatorFollowRequest{CreatorID: state.seed.ID, Enabled: enabled}
	state.followPending = p.host.Catalog.FollowCreator(req, func(result provider.CreatorFollowResult, err error) {
		state.followPending = nil
		if err != nil {
			if provider.CodeOf(err) == provider.NeedsSignIn && p.State.Page == app.Creator {
				p.openCreatorSignIn(func() {
					if p.host.creator == state && state.profile.Creator.Following != enabled {
						p.performCreatorFollow(state, enabled)
					}
				})
				return
			}
			state.feedback = videoDetailFeedback{text: p.videoOperationError(err, text.UpdateFailed)}
			return
		}
		p.host.syncCreatorFollowing(state.profile.Creator, result.Following)
		if result.Following {
			state.feedback = videoDetailFeedback{text: text.Followed}
		} else {
			state.feedback = videoDetailFeedback{text: text.Unfollowed}
		}
	})
}

func (v *View) syncCreatorFollowing(creator provider.Creator, following bool) {
	creatorID := creator.ID
	if creatorID == "" {
		return
	}
	updateCreator := func(s *creatorState) {
		if s != nil && s.seed.ID == creatorID {
			s.seed.Following = following
			s.profile.Creator.Following = following
		}
	}
	updateCreator(v.creator)
	for _, s := range v.creatorStack {
		updateCreator(s)
	}
	updateDetail := func(d *videoDetailState) {
		if d.seed.Creator != nil && d.seed.Creator.ID == creatorID {
			d.seed.Creator.Following = following
		}
		if d.item.Creator != nil && d.item.Creator.ID == creatorID {
			d.item.Creator.Following = following
		}
		for i := range d.previous {
			if d.previous[i].seed.Creator != nil && d.previous[i].seed.Creator.ID == creatorID {
				d.previous[i].seed.Creator.Following = following
			}
			if d.previous[i].item.Creator != nil && d.previous[i].item.Creator.ID == creatorID {
				d.previous[i].item.Creator.Following = following
			}
		}
	}
	updateDetail(&v.videoDetail)
	for i := range v.videoDetailStack {
		updateDetail(&v.videoDetailStack[i])
	}
	if v.Following != nil {
		v.Following.SyncFollow(creator, following)
	}
	if v.Live != nil {
		v.Live.SyncFollow(creatorID, following)
	}
	for i := range v.player.items {
		if c := v.player.items[i].item.Creator; c != nil && c.ID == creatorID {
			copied := *c
			copied.Following = following
			v.player.items[i].item.Creator = &copied
		}
	}
}

func (creatorPage) Render(c *ui.Context, p *PageContext) {
	state := p.host.creator
	if state == nil {
		return
	}
	entry, _ := p.State.Current()
	canFollow := entry.Capabilities.Has(provider.FollowCreator)
	canPrivate := entry.Capabilities.Has(provider.ReadPrivateMessages) && p.host.PrivateMessages != nil
	headerFocus := app.FocusID("")
	if canFollow {
		headerFocus = creatorFollowFocus
	} else if canPrivate {
		headerFocus = creatorPrivateFocus
	}
	tabs := state.visibleTabs()
	if !slices.Contains(tabs, state.tab) && len(tabs) != 0 {
		state.tab = tabs[0]
	}
	if p.active && ((p.State.Focus == creatorFollowFocus && !canFollow) || (p.State.Focus == creatorPrivateFocus && !canPrivate)) {
		if headerFocus != "" {
			p.State.Focus = headerFocus
		} else if len(tabs) != 0 {
			p.State.Focus = creatorTabFocus(state.tab)
		}
		p.host.reveal = true
	}

	p.prepareCreatorArtwork(state)
	p.region(creatorHeaderRegion, focusEdges{app.Down: {Focus: creatorTabFocus(state.tab)}})
	p.region(creatorTabsRegion, focusEdges{app.Up: {Focus: headerFocus}})
	p.region(creatorContentRegion, focusEdges{app.Up: {Focus: creatorTabFocus(state.tab)}})

	ui.Box(c.Key("creator-page")).FillWidth().Lang(p.langTag()).Font(p.uiFont()).Children(func() {
		p.creatorArtwork(c, state)
		ui.Column(c).FillWidth().ZIndex(1).Children(func() {
			p.creatorOverview(c, state, canFollow, canPrivate)
			p.creatorTabs(c, state, tabs, headerFocus)
			ui.Column(c).FillWidth().Margin(35, 0, 0, 0).Children(func() {
				p.creatorSection(c, state)
			})
		})
	})
}

func (p *PageContext) prepareCreatorArtwork(state *creatorState) {
	if p.host.Catalog == nil {
		return
	}
	covers := make([]string, 0, 24)
	if state.profile.Creator.AvatarURL != "" {
		covers = append(covers, state.profile.Creator.AvatarURL)
	}
	switch state.tab {
	case provider.CreatorTabVideos:
		items := state.videos.items
		if len(items) != 0 {
			firstRow, endRow := creatorVisibleVideoRows(p.scroll().Y, p.Height, len(items))
			start := firstRow * creatorVideoCols
			end := min(len(items), endRow*creatorVideoCols)
			for _, item := range items[start:end] {
				if item.CoverURL != "" {
					covers = append(covers, item.CoverURL)
				}
			}
		}
	case provider.CreatorTabPosts:
		for _, post := range state.posts.posts[:min(len(state.posts.posts), 12)] {
			if post.Item != nil && post.Item.CoverURL != "" {
				covers = append(covers, post.Item.CoverURL)
			} else if post.ImageURL != "" {
				covers = append(covers, post.ImageURL)
			}
		}
	case provider.CreatorTabCollections:
		for _, col := range state.collections.collections[:min(len(state.collections.collections), 6)] {
			for _, item := range col.Items[:min(len(col.Items), 6)] {
				if item.CoverURL != "" {
					covers = append(covers, item.CoverURL)
				}
			}
		}
	}
	p.host.Catalog.PrepareArtwork(covers)
	if state.profile.BackdropURL != "" {
		p.host.Catalog.PrepareBackdrops([]string{state.profile.BackdropURL})
	}
}

func (p *PageContext) creatorArtwork(c *ui.Context, state *creatorState) {
	canvas := p.Theme.Colors.Surface.Canvas
	var source ui.ImageSource
	if state.profile.BackdropURL != "" {
		source = p.catalogBitmap(catalogImageKey{location: state.profile.BackdropURL, backdrop: true})
	}
	if source == nil && state.profile.Creator.AvatarURL != "" {
		source = p.catalogCover(state.profile.Creator.AvatarURL)
	}
	ui.Box(c.Key("creator-cover")).Absolute().Top(0).Left(0).Right(0).Height(creatorCoverHeight).Clip().Children(func() {
		if source != nil {
			ui.Image(c, source).Fill().Fit(ui.Cover).ObjectPosition(0.5, 0.4).Opacity(0.55)
		}
		ui.Box(c).Absolute().Top(0).Left(0).Fill().LinearGradient(ui.LinearGradient{
			Angle: 90, Stops: []ui.GradientStop{
				{Color: withAlpha(canvas, 0xd9)},
				{Color: withAlpha(canvas, 0), At: 1},
			},
		})
		ui.Box(c).Absolute().Top(0).Left(0).Fill().LinearGradient(ui.LinearGradient{
			Angle: 180, Stops: []ui.GradientStop{
				{Color: withAlpha(canvas, 0)},
				{Color: withAlpha(canvas, 0), At: 0.05},
				{Color: paint(canvas), At: 1},
			},
		})
	})
}

func (p *PageContext) openCreatorPrivateMessage(creator provider.Creator) {
	if p.host.PrivateMessages == nil || strings.TrimSpace(creator.ID) == "" {
		return
	}
	if p.host.Auth == nil || p.host.Auth.Accounts().CurrentID == "" {
		p.openCreatorSignIn(func() {
			p.host.openCreatorPrivateMessages(creator)
		})
		return
	}
	p.host.openCreatorPrivateMessages(creator)
}

func (p *PageContext) creatorOverview(c *ui.Context, state *creatorState, canFollow, canPrivate bool) {
	profile := state.profile
	colors, d, text := p.Theme.Colors, p.uiColors(), p.Assets.Text
	ui.Row(c.Key("creator-overview")).FillWidth().MinHeight(creatorOverviewMin).
		Padding(135, creatorInset, 65, creatorInset).Gap(35).AlignItems(ui.Center).Children(func() {
		ui.Box(c.Key("creator-avatar")).Size(155, 155).Shrink(0).Radius(77.5).Clip().Center().
			Border(2, withAlpha(colors.Accent.Soft, 0x66)).Background(paint(colors.Surface.Raised)).Children(func() {
			if img := p.catalogCover(profile.Creator.AvatarURL); img != nil {
				ui.Image(c, img).Fill().Fit(ui.Cover)
			} else if r, _ := utf8.DecodeRuneInString(strings.TrimSpace(profile.Creator.Name)); r != utf8.RuneError {
				ui.Text(c, string(r)).FontSize(54).FontWeight(500).TextColor(paint(colors.Text.Primary))
			} else {
				ui.Icon(c, p.Assets.Icon("user")).Size(56, 56).TextColor(paint(colors.Text.Secondary))
			}
		})

		ui.Column(c.Key("creator-heading")).Grow(1).MinWidth(0).Children(func() {
			ui.Text(c, text.Creator.Eyebrow).FontSize(15).LetterSpacing(3).TextColor(paint(colors.Accent.Soft))
			ui.Text(c, profile.Creator.Name).FillWidth().Margin(20, 0, 18, 0).
				FontSize(56).FontWeight(500).LetterSpacing(1).LineHeight(1.2).MaxLines(2).
				Role(ui.RoleHeading).Level(1)
			if profile.Bio != "" {
				ui.Text(c, profile.Bio).FillWidth().Margin(0, 0, 23, 0).
					FontSize(22).LineHeight(1.5).MaxLines(2).TextColor(paint(colors.Text.Secondary))
			}
			if profile.Followers != nil || profile.Works != nil {
				ui.Row(c.Key("creator-metrics")).Gap(32).AlignItems(ui.Baseline).Children(func() {
					if profile.Followers != nil {
						ui.Row(c).Gap(9).AlignItems(ui.Baseline).Children(func() {
							ui.Text(c, catalogViews(*profile.Followers, text.Home)).FontSize(23).FontWeight(450).
								TextColor(paint(colors.Text.Primary))
							ui.Text(c, text.Creator.Followers).FontSize(17).TextColor(paint(colors.Text.Muted))
						})
					}
					if profile.Works != nil {
						ui.Row(c).Gap(9).AlignItems(ui.Baseline).Children(func() {
							ui.Text(c, strconv.FormatInt(*profile.Works, 10)).FontSize(23).FontWeight(450).
								TextColor(paint(colors.Text.Primary))
							ui.Text(c, text.Creator.Works).FontSize(17).TextColor(paint(colors.Text.Muted))
						})
					}
				})
			}
		})

		if canFollow || canPrivate {
			ui.Row(c.Key("creator-actions")).Gap(14).AlignItems(ui.Center).Shrink(0).Children(func() {
				actionStyle := buttonAppearance{
					height: 57, padding: 27, radius: 12, fontSize: 21, fontWeight: 550, iconSize: 24, iconGap: 13,
					focusShadow: true, background: d.secondaryFill, foreground: d.secondaryText, border: d.secondaryBorder,
				}
				if canFollow {
					label, icon := text.Detail.Follow, "detail/plus"
					if profile.Creator.Following {
						label, icon = text.Detail.Followed, "detail/check"
					}
					p.styledButton(c, label, icon, creatorFollowFocus, creatorHeaderRegion, func() {
						p.dispatch(p.toggleCreatorFollow)
					}, actionStyle).Center()
				}
				if canPrivate {
					p.styledButton(c, text.Activity.PrivateTab, "detail/comment", creatorPrivateFocus, creatorHeaderRegion, func() {
						p.dispatch(func() { p.openCreatorPrivateMessage(profile.Creator) })
					}, actionStyle).Center()
				}
			})
		}
	})
}

func (p *PageContext) creatorTabLabel(tab provider.CreatorTab) string {
	text := p.Assets.Text.Creator
	switch tab {
	case provider.CreatorTabVideos:
		return text.Videos
	case provider.CreatorTabPosts:
		return text.Posts
	case provider.CreatorTabCollections:
		return text.Collections
	case provider.CreatorTabAbout:
		return text.About
	default:
		return string(tab)
	}
}

func (p *PageContext) creatorTabs(c *ui.Context, state *creatorState, tabs []provider.CreatorTab, headerFocus app.FocusID) {
	colors, d := p.Theme.Colors, p.uiColors()
	ui.Column(c.Key("creator-tabs-wrap")).FillWidth().Padding(0, creatorInset, 0, creatorInset).Children(func() {
		ui.Row(c.Key("creator-tabs")).FillWidth().Gap(18).Padding(0, 0, 15, 0).Children(func() {
			for pos, tab := range tabs {
				label := p.creatorTabLabel(tab)
				id := creatorTabFocus(tab)
				selected := state.tab == tab
				focused := p.paintFocus() == id
				background, ink := ui.Transparent, paint(colors.Text.Muted)
				if selected {
					background, ink = withAlpha(colors.Text.Primary, 0x12), paint(colors.Text.Primary)
				}
				if focused {
					background, ink = withAlpha(colors.Text.Primary, 0x13), paint(colors.Text.Primary)
				}
				btn := ui.Button(c.Key(string(id)), "").Label(label).Size(98, 62).Padding(0, 0).
					Radius(30).Border(0, ui.Transparent).Center().Role(ui.RoleTab).Selected(selected).
					OnClick(func() {
						p.dispatch(func() { p.host.selectCreatorTab(tab, app.Down) })
					})
				ink = p.animateFocus(btn, background, ink)
				p.bindDirection(btn, id, creatorTabsRegion, func(direction app.Direction) bool {
					switch direction {
					case app.Left, app.Right:
						next := pos + 1
						if direction == app.Left {
							next = pos - 1
						}
						if next >= 0 && next < len(tabs) {
							p.host.selectCreatorTab(tabs[next], direction)
						}
						return true
					case app.Up:
						if headerFocus != "" {
							p.host.moveFocus(headerFocus, app.Up, true)
							return true
						}
						return true
					case app.Down:
						if target := creatorFirstContentFocus(state); target != "" && p.host.focus.item(target) != nil {
							p.host.moveFocus(target, app.Down, true)
							return true
						}
						if state.tab == provider.CreatorTabAbout {
							scroll := p.scroll()
							if scroll.MaxY-scroll.Y > 20 {
								scroll.ScrollTo(scroll.X, min(scroll.MaxY, scroll.Y+p.Height/2), focusScroll)
								return true
							}
						}
						return false
					default:
						return false
					}
				})
				btn.Children(func() {
					stroke := float32(24 * videoDetailTextStroke)
					if selected || focused {
						stroke = 24 * 0.023
					}
					ui.Text(c, label).FontSize(24).FontWeight(400).LetterSpacing(1).Translate(0, -1).
						TextColor(ink).TextStroke(stroke, ink)
					p.tabUnderline(c, selected, focused, 3)
				})
			}
		})
		ui.Box(c).FillWidth().Height(1).Background(d.subtleBorder)
	})
}

func creatorFirstContentFocus(state *creatorState) app.FocusID {
	if state.profileFailure != "" && !state.profileLoaded {
		return creatorRetryFocus
	}
	switch state.tab {
	case provider.CreatorTabVideos:
		if len(state.videos.items) != 0 {
			return creatorVideoFocus(state.videos.items[0].Ref)
		}
		if state.videos.failure != "" {
			return creatorRetryFocus
		}
	case provider.CreatorTabPosts:
		if len(state.posts.posts) != 0 {
			return creatorPostFocus(state.posts.posts[0].ID)
		}
		if state.posts.failure != "" {
			return creatorRetryFocus
		}
	case provider.CreatorTabCollections:
		for _, col := range state.collections.collections {
			if len(col.Items) != 0 {
				return creatorCollectionCardFocus(col.ID, col.Items[0].Ref)
			}
		}
		if state.collections.failure != "" {
			return creatorRetryFocus
		}
	}
	return ""
}

func (p *PageContext) creatorSection(c *ui.Context, state *creatorState) {
	text := p.Assets.Text
	if state.profileLoading && !state.profileLoaded {
		ui.Column(c).FillWidth().Padding(0, creatorInset, 0, creatorInset).Children(func() {
			p.videoDetailText(c, text.Creator.Loading)
		})
		return
	}
	if state.profileFailure != "" && !state.profileLoaded {
		ui.Column(c).FillWidth().Padding(0, creatorInset, 0, creatorInset).Gap(20).Children(func() {
			p.videoDetailText(c, p.creatorFailureMessage(state.profileFailure))
			p.button(c, text.Retry, creatorRetryFocus, creatorContentRegion, func() {
				p.host.loadCreatorInitial(state)
			})
		})
		return
	}
	switch state.tab {
	case provider.CreatorTabVideos:
		p.creatorVideosSection(c, state)
	case provider.CreatorTabPosts:
		p.creatorPostsSection(c, state)
	case provider.CreatorTabCollections:
		p.creatorCollectionsSection(c, state)
	case provider.CreatorTabAbout:
		p.creatorAboutSection(c, state)
	}
}

func (p *PageContext) creatorFailureMessage(code provider.ErrorCode) string {
	switch code {
	case provider.RateLimited:
		return p.Assets.Text.Home.RateLimited
	case provider.NeedsSignIn:
		return p.Assets.Text.Auth.AccountExpired
	default:
		return p.Assets.Text.Creator.LoadFailed
	}
}

func creatorVisibleVideoRows(scrollY, viewportHeight float32, count int) (int, int) {
	if count == 0 {
		return 0, 0
	}
	totalRows := (count + creatorVideoCols - 1) / creatorVideoCols
	return visibleItemRange(max(0, scrollY-creatorContentTop), viewportHeight, float32(creatorVideoHeight+creatorVideoGapY), totalRows, 1)
}

func (p *PageContext) creatorItemWithOwner(state *creatorState, item provider.MediaItem) provider.MediaItem {
	if item.Creator == nil && state.profile.Creator.ID != "" {
		owner := state.profile.Creator
		item.Creator = &owner
	}
	return item
}

func (p *PageContext) creatorVideosSection(c *ui.Context, state *creatorState) {
	data, text, colors := &state.videos, p.Assets.Text, p.Theme.Colors
	ui.Column(c.Key("creator-videos")).FillWidth().Padding(0, creatorInset, 70, creatorInset).Children(func() {
		if len(data.items) == 0 {
			if data.loading {
				p.videoDetailText(c, text.LoadingTitle)
				return
			}
			if data.failure != "" {
				p.videoDetailText(c, p.creatorFailureMessage(data.failure))
				p.button(c, text.Retry, creatorRetryFocus, creatorContentRegion, func() {
					p.host.loadCreatorTab(state, provider.CreatorTabVideos, false)
				}).Margin(20, 0, 0, 0)
				return
			}
			p.videoDetailText(c, text.Creator.NoVideos)
			return
		}

		contentWidth := max(400, p.Width-2*creatorInset)
		cardWidth := (contentWidth - float32(creatorVideoCols-1)*creatorVideoGapX) / creatorVideoCols
		stride := float32(creatorVideoHeight + creatorVideoGapY)
		count := len(data.items)
		slots := count
		if data.appending && data.loading {
			slots += creatorVideoCols
		}
		totalRows := (slots + creatorVideoCols - 1) / creatorVideoCols
		lastRow := (count - 1) / creatorVideoCols
		firstRow, endRow := creatorVisibleVideoRows(p.scroll().Y, p.Height, slots)
		focusedRow := -1
		for i, item := range data.items {
			if p.State.Focus == creatorVideoFocus(item.Ref) {
				focusedRow = i / creatorVideoCols
				break
			}
		}
		if p.active && data.canLoadMore() && (endRow >= lastRow || (focusedRow >= 0 && focusedRow >= lastRow-1)) {
			p.host.loadCreatorTab(state, provider.CreatorTabVideos, true)
		}

		paging := data.canLoadMore() || (data.appending && data.loading)
		appendFailed := data.appending && data.failure != ""
		gridHeight := max(0, float32(totalRows)*stride-creatorVideoGapY)
		size := catalogCardSize{width: cardWidth, height: creatorVideoHeight, cover: creatorVideoCover, title: 21}

		ui.Box(c.Key("creator-video-grid")).Width(contentWidth).Height(gridHeight).Children(func() {
			drawRow := func(row int) {
				start := row * creatorVideoCols
				end := min(start+creatorVideoCols, slots)
				atTop := row == 0
				atTail := row == lastRow
				ui.Row(c.Key("creator-video-row/"+strconv.Itoa(row))).Absolute().Left(0).Top(float32(row)*stride).
					Width(contentWidth).Height(creatorVideoHeight).Gap(creatorVideoGapX).Children(func() {
					for index := start; index < end; index++ {
						if index >= count {
							p.catalogSkeleton(c, size)
							continue
						}
						item := p.creatorItemWithOwner(state, data.items[index])
						var dir func(app.Direction) bool
						if atTop || (atTail && (paging || appendFailed)) {
							dir = func(key app.Direction) bool {
								if key == app.Up && atTop {
									p.host.moveFocus(creatorTabFocus(provider.CreatorTabVideos), app.Up, true)
									return true
								}
								if key == app.Down && atTail {
									if paging {
										p.host.loadCreatorTab(state, provider.CreatorTabVideos, true)
										return true
									}
									if appendFailed {
										p.host.moveFocus(creatorMoreRetry, app.Down, true)
										return true
									}
								}
								return false
							}
						}
						p.catalogCard(c, creatorVideoFocus(item.Ref), creatorContentRegion, item, size, dir)
					}
				})
			}
			for row := firstRow; row < endRow; row++ {
				drawRow(row)
			}
			if focusedRow >= 0 && (focusedRow < firstRow || focusedRow >= endRow) && focusedRow < totalRows {
				drawRow(focusedRow)
			}
		})

		if appendFailed {
			ui.Row(c.Key("creator-videos-more-failed")).FillWidth().Margin(24, 0, 0, 0).Gap(18).AlignItems(ui.Center).Children(func() {
				p.button(c, text.Retry, creatorMoreRetry, creatorContentRegion, func() {
					p.host.loadCreatorTab(state, provider.CreatorTabVideos, true)
				})
				ui.Text(c, text.Creator.MoreFailed).FontSize(19).TextColor(paint(colors.Status.Error.Foreground))
			})
		}
	})
}

func (p *PageContext) creatorPostsSection(c *ui.Context, state *creatorState) {
	data, text, colors := &state.posts, p.Assets.Text, p.Theme.Colors
	ui.Column(c.Key("creator-posts")).FillWidth().Padding(0, creatorInset, 70, creatorInset).Children(func() {
		if len(data.posts) == 0 {
			if data.loading {
				p.videoDetailText(c, text.LoadingTitle)
				return
			}
			if data.failure != "" {
				p.videoDetailText(c, p.creatorFailureMessage(data.failure))
				p.button(c, text.Retry, creatorRetryFocus, creatorContentRegion, func() {
					p.host.loadCreatorTab(state, provider.CreatorTabPosts, false)
				}).Margin(20, 0, 0, 0)
				return
			}
			p.videoDetailText(c, text.Creator.NoPosts)
			return
		}

		contentWidth := max(400, p.Width-2*creatorInset)
		cardWidth := (contentWidth - creatorPostGap) / creatorPostCols
		count := len(data.posts)
		totalRows := (count + creatorPostCols - 1) / creatorPostCols
		lastRow := totalRows - 1
		focusedRow := -1
		for i, post := range data.posts {
			if p.State.Focus == creatorPostFocus(post.ID) {
				focusedRow = i / creatorPostCols
				break
			}
		}
		if p.active && data.canLoadMore() && focusedRow >= 0 && focusedRow >= lastRow-1 {
			p.host.loadCreatorTab(state, provider.CreatorTabPosts, true)
		}

		paging := data.canLoadMore() || (data.appending && data.loading)
		appendFailed := data.appending && data.failure != ""
		ui.Column(c.Key("creator-post-rows")).Width(contentWidth).Gap(creatorPostGap).Children(func() {
			for row := 0; row < totalRows; row++ {
				start := row * creatorPostCols
				end := min(start+creatorPostCols, count)
				atTop := row == 0
				atTail := row == lastRow
				ui.Row(c.Key("creator-post-row/"+strconv.Itoa(row))).Width(contentWidth).Gap(creatorPostGap).
					AlignItems(ui.Stretch).Children(func() {
					for index := start; index < end; index++ {
						var dir func(app.Direction) bool
						if atTop || (atTail && (paging || appendFailed)) {
							dir = func(key app.Direction) bool {
								if key == app.Up && atTop {
									p.host.moveFocus(creatorTabFocus(provider.CreatorTabPosts), app.Up, true)
									return true
								}
								if key == app.Down && atTail {
									if paging {
										p.host.loadCreatorTab(state, provider.CreatorTabPosts, true)
										return true
									}
									if appendFailed {
										p.host.moveFocus(creatorMoreRetry, app.Down, true)
										return true
									}
								}
								return false
							}
						}
						p.creatorPostCard(c, state, data.posts[index], cardWidth, dir)
					}
				})
			}
		})

		if data.appending && data.loading {
			ui.Text(c, text.LoadingTitle).Margin(24, 0, 0, 0).FontSize(20).TextColor(paint(colors.Text.Muted))
		} else if appendFailed {
			ui.Row(c.Key("creator-posts-more-failed")).FillWidth().Margin(24, 0, 0, 0).Gap(18).AlignItems(ui.Center).Children(func() {
				p.button(c, text.Retry, creatorMoreRetry, creatorContentRegion, func() {
					p.host.loadCreatorTab(state, provider.CreatorTabPosts, true)
				})
				ui.Text(c, text.Creator.MoreFailed).FontSize(19).TextColor(paint(colors.Status.Error.Foreground))
			})
		}
	})
}

func (p *PageContext) creatorPostTime(post provider.CreatorPost) string {
	if post.PublishedLabel != "" {
		return post.PublishedLabel
	}
	if post.PublishedAt > 0 {
		return time.Unix(post.PublishedAt, 0).Local().Format("2006.01.02 15:04")
	}
	return ""
}

func (p *PageContext) creatorPostCard(c *ui.Context, state *creatorState, post provider.CreatorPost, width float32, direction func(app.Direction) bool) {
	colors, d := p.Theme.Colors, p.uiColors()
	id := creatorPostFocus(post.ID)
	innerWidth := width - 56
	renderHeaderAndBody := func() {
		ui.Row(c).FillWidth().Gap(14).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(43, 43).Shrink(0).Radius(21.5).Clip().Center().Background(paint(colors.Surface.Raised)).Children(func() {
				if img := p.catalogCover(state.profile.Creator.AvatarURL); img != nil {
					ui.Image(c, img).Fill().Fit(ui.Cover)
				} else {
					ui.Icon(c, p.Assets.Icon("user")).Size(20, 20).TextColor(paint(colors.Text.Secondary))
				}
			})
			ui.Column(c).Grow(1).MinWidth(0).Children(func() {
				ui.Text(c, state.profile.Creator.Name).FontSize(19).FontWeight(400).SingleLine().
					TextColor(paint(colors.Text.Primary))
				if when := p.creatorPostTime(post); when != "" {
					ui.Text(c, when).Margin(7, 0, 0, 0).FontSize(14).SingleLine().
						TextColor(paint(colors.Text.Muted))
				}
			})
		})
		if post.Body != "" {
			maxLines := 2
			if post.Item == nil && post.ImageURL == "" {
				maxLines = 5
			}
			ui.Text(c, post.Body).FillWidth().Margin(24, 0, 24, 0).FontSize(23).LineHeight(1.8).
				MaxLines(maxLines).TextColor(paint(colors.Text.Secondary))
		} else {
			ui.Box(c).Height(20)
		}
	}

	if post.Item != nil {
		item := p.creatorItemWithOwner(state, *post.Item)
		size := catalogCardSize{width: innerWidth, height: creatorPostCover + 73, cover: creatorPostCover, title: 21}
		ui.Column(c.Key("creator-post-box/"+post.ID)).Width(width).Padding(28).Radius(17).
			Background(d.subtleFill).Border(1, d.subtleBorder).Children(func() {
			renderHeaderAndBody()
			p.catalogCard(c, id, creatorContentRegion, item, size, direction)
		})
		return
	}

	focused := p.paintFocus() == id
	bg := d.subtleFill
	border := d.subtleBorder
	borderWidth := float32(1)
	if focused {
		bg = d.subtleHoverFill
		border = paint(colors.Focus.Ring)
		borderWidth = 3
	}
	label := post.Body
	if label == "" {
		label = state.profile.Creator.Name
	}
	btn := ui.Button(c.Key(string(id)), "").Label(label).Width(width).Padding(28).Radius(17).
		Background(bg).Border(borderWidth, border)
	p.bindDirection(btn, id, creatorContentRegion, direction)
	btn.Children(func() {
		ui.Column(c).FillWidth().Children(func() {
			renderHeaderAndBody()
			if post.ImageURL != "" {
				ui.Box(c).FillWidth().Height(creatorPostCover).Radius(13).Clip().Background(d.coverFill).Children(func() {
					if img := p.catalogCover(post.ImageURL); img != nil {
						ui.Image(c, img).Fill().Fit(ui.Cover)
					}
				})
			}
		})
	})
}

func (p *PageContext) creatorCollectionsSection(c *ui.Context, state *creatorState) {
	data, text, colors := &state.collections, p.Assets.Text, p.Theme.Colors
	ui.Column(c.Key("creator-collections")).FillWidth().Padding(0, creatorInset, 70, creatorInset).Gap(44).Children(func() {
		if len(data.collections) == 0 {
			if data.loading {
				p.videoDetailText(c, text.LoadingTitle)
				return
			}
			if data.failure != "" {
				p.videoDetailText(c, p.creatorFailureMessage(data.failure))
				p.button(c, text.Retry, creatorRetryFocus, creatorContentRegion, func() {
					p.host.loadCreatorTab(state, provider.CreatorTabCollections, false)
				}).Margin(20, 0, 0, 0)
				return
			}
			p.videoDetailText(c, text.Creator.NoCollections)
			return
		}

		if state.collectionScrolls == nil {
			state.collectionScrolls = make(map[string]*ui.ScrollState)
		}
		lastIdx := len(data.collections) - 1
		paging := data.canLoadMore() || (data.appending && data.loading)
		appendFailed := data.appending && data.failure != ""

		for colIndex, col := range data.collections {
			if len(col.Items) == 0 {
				continue
			}
			colCount := int64(len(col.Items))
			if col.Count != nil && *col.Count > 0 {
				colCount = *col.Count
			}
			scroll := state.collectionScrolls[col.ID]
			if scroll == nil {
				scroll = &ui.ScrollState{}
				state.collectionScrolls[col.ID] = scroll
			}
			atTop := colIndex == 0
			atTail := colIndex == lastIdx
			ui.Column(c.Key("creator-col/"+col.ID)).FillWidth().Children(func() {
				ui.Text(c, col.Title).FontSize(29).FontWeight(450).SingleLine().Role(ui.RoleHeading).Level(2)
				ui.Text(c, fmt.Sprintf(text.Creator.CollectionWorks, colCount)).Margin(15, 0, 28, 0).
					FontSize(18).TextColor(paint(colors.Text.Muted))
				p.creatorCollectionStrip(c, state, col, scroll, atTop, atTail, paging, appendFailed)
			})
		}

		if data.appending && data.loading {
			ui.Text(c, text.LoadingTitle).FontSize(20).TextColor(paint(colors.Text.Muted))
		} else if appendFailed {
			ui.Row(c.Key("creator-cols-more-failed")).FillWidth().Gap(18).AlignItems(ui.Center).Children(func() {
				p.button(c, text.Retry, creatorMoreRetry, creatorContentRegion, func() {
					p.host.loadCreatorTab(state, provider.CreatorTabCollections, true)
				})
				ui.Text(c, text.Creator.MoreFailed).FontSize(19).TextColor(paint(colors.Status.Error.Foreground))
			})
		}
	})
}

func (p *PageContext) creatorCollectionStrip(c *ui.Context, state *creatorState, col provider.CreatorCollection, scroll *ui.ScrollState, atTop, atTail, paging, appendFailed bool) {
	items := col.Items
	cardWidth := homeShelfCardSize.width
	gap := float32(27)
	stride := cardWidth + gap
	viewport := max(400, p.Width-2*creatorInset)
	first, end := visibleItemRange(scroll.X, viewport, stride, len(items), 2)
	focused := -1
	for i, item := range items {
		if p.State.Focus == creatorCollectionCardFocus(col.ID, item.Ref) {
			focused = i
			break
		}
	}
	if p.active && atTail && focused >= 0 && state.collections.canLoadMore() {
		p.host.loadCreatorTab(state, provider.CreatorTabCollections, true)
	}
	stripWidth := float32(len(items))*stride - gap + 20
	ui.ScrollHorizontal(c.Key("creator-col-strip/"+col.ID)).FillWidth().Height(homeCardHeight+8).
		TrackScroll(scroll).Children(func() {
		ui.Box(c).Width(stripWidth).Height(homeCardHeight + 8).Children(func() {
			draw := func(i int) {
				item := p.creatorItemWithOwner(state, items[i])
				id := creatorCollectionCardFocus(col.ID, item.Ref)
				var dir func(app.Direction) bool
				if atTop || (atTail && (paging || appendFailed)) {
					dir = func(key app.Direction) bool {
						if key == app.Up && atTop {
							p.host.moveFocus(creatorTabFocus(provider.CreatorTabCollections), app.Up, true)
							return true
						}
						if key == app.Down && atTail {
							if paging {
								p.host.loadCreatorTab(state, provider.CreatorTabCollections, true)
								return true
							}
							if appendFailed {
								p.host.moveFocus(creatorMoreRetry, app.Down, true)
								return true
							}
						}
						return false
					}
				}
				ui.Box(c.Key("creator-col-card/"+string(id))).Absolute().Top(4).Left(4+float32(i)*stride).
					Size(cardWidth, homeCardHeight).Children(func() {
					p.catalogCard(c, id, creatorContentRegion, item, homeShelfCardSize, dir)
				})
			}
			for i := first; i < end; i++ {
				draw(i)
			}
			if focused >= 0 {
				for i := max(0, focused-1); i < min(len(items), focused+2); i++ {
					if i < first || i >= end {
						draw(i)
					}
				}
			}
		})
	})
}

func (p *PageContext) creatorAboutSection(c *ui.Context, state *creatorState) {
	profile := state.profile
	text, colors, d := p.Assets.Text.Creator, p.Theme.Colors, p.uiColors()
	bio := profile.Bio
	if bio == "" {
		bio = text.EmptyBio
	}
	var rows []provider.CreatorAttribute
	if profile.Location != "" {
		rows = append(rows, provider.CreatorAttribute{Label: text.Location, Value: profile.Location})
	}
	if profile.Category != "" {
		rows = append(rows, provider.CreatorAttribute{Label: text.Category, Value: profile.Category})
	}
	rows = append(rows, profile.Attributes...)
	if profile.Works != nil {
		rows = append(rows, provider.CreatorAttribute{Label: text.Works, Value: strconv.FormatInt(*profile.Works, 10)})
	}
	width := min(float32(1100), max(400, p.Width-2*creatorInset))
	ui.Column(c.Key("creator-about")).FillWidth().Padding(0, creatorInset, 70, creatorInset).Children(func() {
		ui.Column(c).Width(width).Children(func() {
			ui.Text(c, profile.Creator.Name).FontSize(35).FontWeight(450).Margin(0, 0, 22, 0).
				Role(ui.RoleHeading).Level(2)
			ui.Text(c, bio).FillWidth().FontSize(26).LineHeight(1.8).Margin(0, 0, 32, 0).
				TextColor(paint(colors.Text.Secondary))
			for idx, row := range rows {
				ui.Column(c.Key("creator-about-row/"+strconv.Itoa(idx))).FillWidth().Children(func() {
					ui.Row(c).FillWidth().Padding(24, 10, 24, 10).Gap(24).AlignItems(ui.Center).Children(func() {
						ui.Text(c, row.Label).Shrink(0).FontSize(23).TextColor(paint(colors.Text.Muted))
						ui.Spacer(c).Grow(1)
						ui.Text(c, row.Value).Shrink(1).TextAlign(ui.End).FontSize(22).FontWeight(400).
							TextColor(paint(colors.Text.Primary))
					})
					ui.Box(c).FillWidth().Height(1).Background(d.subtleBorder)
				})
			}
		})
	})
}
