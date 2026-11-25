package tvui

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/provider"
	"github.com/yuchanns/MikuTV/internal/theme"
)

type videoDetailReply struct {
	item provider.MediaItem
	err  error
}

type videoDetailCall struct {
	context.Context
	ref   provider.MediaRef
	reply chan videoDetailReply
}

type videoDetailSession struct {
	viewCatalogSession
	calls chan videoDetailCall
}

func (s videoDetailSession) Item(ctx context.Context, ref provider.MediaRef) (provider.MediaItem, error) {
	reply := make(chan videoDetailReply, 1)
	s.calls <- videoDetailCall{ctx, ref, reply}
	result := <-reply
	return result.item, result.err
}

func TestVideoDetailVirtualizesGrowingCollections(t *testing.T) {
	assets, err := LoadAssets()
	if err != nil {
		t.Fatal(err)
	}
	pages, err := NewPages(assets)
	if err != nil {
		t.Fatal(err)
	}
	entries := testEntries()
	entries[0].Capabilities = []provider.Capability{provider.GetCatalogItem, provider.ReadVideoComments, provider.WriteVideoComments}
	state, err := app.NewState(entries, entries[0].ID, pages.Routes)
	if err != nil || !state.Push(app.Route{Page: app.VideoDetail}) {
		t.Fatalf("could not open the test detail screen: %v", err)
	}
	view := newTestView(t, Options{State: state, Pages: pages, Assets: assets, Theme: theme.Default()})
	view.SyncEntry()
	details := &provider.VideoDetails{SegmentKind: provider.VideoParts}
	ref := provider.MediaRef{EntryID: entries[0].ID, Kind: provider.Video, ID: "main"}
	view.videoDetail.item = provider.MediaItem{Ref: ref, Title: "Main video", VideoDetails: details}
	const total = 160
	for index := range total {
		id := fmt.Sprint(index)
		details.Segments = append(details.Segments, provider.VideoSegment{Title: "Segment " + id})
		details.Discussion = append(details.Discussion, provider.VideoComment{ID: id, Author: "Viewer", Body: "Comment " + id})
		details.Related = append(details.Related, provider.MediaItem{
			Ref: provider.MediaRef{EntryID: ref.EntryID, Kind: provider.Video, ID: id}, Title: "Related " + id,
		})
	}
	tester := ui.NewTester(view.Render, 1920, 1080)
	tester.Frame()
	view.videoDetail.tab = videoDetailComments
	view.videoDetail.revealSection = true
	view.moveFocus(videoDetailComments.focusID(), app.Right, false)
	tester.Frame()
	tester.Advance(2 * focusScroll.Duration)
	start := view.activeScroll.Y
	tester.Key(0, ui.KeyDown)
	tester.Key(0, ui.KeyDown)
	for range 8 {
		tester.Key(0, ui.KeyDown)
		tester.Advance(2 * focusScroll.Duration)
	}
	row := view.focus.item(videoCommentFocus("8"))
	if state.Focus != videoCommentFocus("8") || view.activeScroll.Y <= start || row == nil ||
		row.target.Y < 0 || row.target.Y+row.target.H > 1080 {
		t.Fatalf("walking the comments did not scroll the detail page to a whole row, scroll %v -> %v, focus=%q, row=%+v",
			start, view.activeScroll.Y, state.Focus, row)
	}

	for _, test := range []struct {
		tab       videoDetailTab
		group     string
		focus     app.FocusID
		nextFocus app.FocusID
		key       ui.Key
	}{
		{videoDetailEpisodes, videoDetailSegmentsRegion, videoSegmentFocus(95), videoSegmentFocus(99), ui.KeyDown},
		{videoDetailComments, videoDiscussionRegion, videoCommentFocus("95"), videoCommentFocus("96"), ui.KeyDown},
		{videoDetailRelated, videoDetailRelatedRegion, catalogCardFocus(videoDetailRelatedShelf, details.Related[95].Ref),
			catalogCardFocus(videoDetailRelatedShelf, details.Related[96].Ref), ui.KeyRight},
	} {
		view.videoDetail.tab = test.tab
		view.moveFocus(test.focus, app.Down, true)
		tester.Frame()
		tester.Advance(2 * focusScroll.Duration)
		if state.Focus != test.focus || view.focus.item(test.focus) == nil {
			t.Fatalf("%s did not build its focused item outside the earlier viewport: %q", test.group, state.Focus)
		}
		tester.Key(0, test.key)
		if state.Focus != test.nextFocus {
			t.Fatalf("%s focus moved to %q, want %q", test.group, state.Focus, test.nextFocus)
		}
		built := 0
		for _, control := range view.focus.controls {
			if control.group == test.group {
				built++
			}
		}
		if built >= total/2 {
			t.Fatalf("%s built %d of its %d items", test.group, built, total)
		}
	}
}

func TestFocusedFavoriteShowsItsSavedState(t *testing.T) {
	assets, err := LoadAssets()
	if err != nil {
		t.Fatal(err)
	}
	details := &provider.VideoDetails{}
	view := &View{videoDetail: videoDetailState{item: provider.MediaItem{VideoDetails: details}}}
	state := &app.State{Location: app.Location{Position: app.Position{Focus: videoDetailFavorite}}}
	page := &PageContext{State: state, Assets: assets, Theme: theme.Default(), host: view}
	tester := ui.NewTester(func(c *ui.Context) {
		page.videoDetailButton(c, assets.Text.Detail.Favorite, "detail/star", videoDetailFavorite, 141)
	}, 260, 100)
	tester.Frame()
	before := bytes.Clone(tester.Image().Pix)
	details.Engagement.Favorite = true
	tester.Frame()
	if bytes.Equal(tester.Image().Pix, before) {
		t.Fatal("saving a favorite did not change its icon while focused")
	}
}

func TestVideoFollowFeedbackUsesTheResultingState(t *testing.T) {
	assets, err := LoadAssets()
	if err != nil {
		t.Fatal(err)
	}
	following := false
	request := provider.VideoEdit{Interaction: provider.VideoFollow, Enabled: true}
	if got := videoEditFeedback(assets.Text.Detail, request, provider.VideoEditResult{}, nil).text; got != assets.Text.Detail.Followed {
		t.Fatalf("follow feedback = %q", got)
	}
	result := provider.VideoEditResult{Following: &following}
	if got := videoEditFeedback(assets.Text.Detail, request, result, nil).text; got == "" || got != assets.Text.Detail.Unfollowed {
		t.Fatalf("unfollow feedback = %q", got)
	}
}

func TestVideoDetailReturnsToTheCallingCard(t *testing.T) {
	assets, err := LoadAssets()
	if err != nil {
		t.Fatal(err)
	}
	pages, err := NewPages(assets)
	if err != nil {
		t.Fatal(err)
	}
	entries := testEntries()
	entries[0].Capabilities = append(entries[0].Capabilities, provider.GetCatalogItem, provider.ResolvePlayback, provider.EditVideo, provider.AccountWatchLater.Capability(), provider.EditVideoFavorites)
	state, err := app.NewState(entries, entries[0].ID, pages.Routes)
	if err != nil {
		t.Fatal(err)
	}
	session := videoDetailSession{
		viewCatalogSession: viewCatalogSession{page: make(chan provider.CollectionPage, 1)},
		calls:              make(chan videoDetailCall, 1),
	}
	callbacks := make(chan func(), 1)
	var frame *ui.Context
	catalog := app.NewCatalog(func(string) provider.Session { return session }, func(fn func()) { callbacks <- fn }, func() {
		if frame != nil {
			frame.Invalidate()
		}
	})
	defer catalog.Close()
	view := newTestView(t, Options{State: state, Catalog: catalog, Pages: pages, Assets: assets, Theme: theme.Default()})
	tester := ui.NewTester(func(c *ui.Context) { frame = c; view.Render(c) }, 1920, 1080)
	view.SyncEntry()
	tester.Frame()

	feed := provider.Collection{ID: "recommendations", Title: "Recommendations"}
	for index := range 12 {
		ref := provider.MediaRef{EntryID: entries[0].ID, Kind: provider.Video, ID: fmt.Sprint(index)}
		feed.Items = append(feed.Items, provider.MediaItem{Ref: ref, Title: "Video " + ref.ID,
			Category: "Science", Quality: "4K", Subtitle: "Recommendation excerpt"})
	}
	session.page <- provider.CollectionPage{Collections: []provider.Collection{feed}}
	(<-callbacks)()
	tester.Frame()
	selected := feed.Items[6]
	focus := catalogCardFocus(feed.ID, selected.Ref)
	position := app.Position{Focus: focus, Y: min(view.activeScroll.MaxY, view.home.layout.sections[0].top)}
	state.RecordPosition(position)
	view.activeScroll.Y = position.Y
	tester.Frame()

	open := func() videoDetailCall {
		t.Helper()
		tester.Key(0, ui.KeyEnter)
		if state.Page != app.VideoDetail || state.Focus != videoDetailPlay || view.focus.item(app.RailFocus(app.Home)) != nil {
			t.Fatalf("the card opened route %+v with focus %q", state.Route, state.Focus)
		}
		if from := view.videoDetail.entrance.from; from.Page != app.Home || from.Focus != focus || from.Y != position.Y {
			t.Fatalf("the entrance lost the source card or scroll position: %+v", from)
		}
		call := <-session.calls
		if call.ref != selected.Ref {
			t.Fatalf("detail request targeted %+v", call.ref)
		}
		tester.Advance(2 * videoDetailTransition.Duration)
		if view.videoDetail.entrance.from.Page != "" {
			t.Fatal("the source page remained after the transition settled")
		}
		return call
	}
	back := func() {
		t.Helper()
		tester.Key(0, ui.KeyEscape)
		if state.Page != app.Home || state.Focus != focus || view.activeScroll.Y != position.Y {
			t.Fatalf("return restored %+v, focus %q, scroll %v, want %v", state.Route, state.Focus, view.activeScroll.Y, position)
		}
	}
	bounds := func(label string) ui.Rect {
		t.Helper()
		rect, ok := tester.Find(label)
		if !ok {
			t.Fatalf("could not find %q on the detail screen", label)
		}
		return rect
	}

	stale := open()
	shortTitle := bounds(selected.Title)
	shortPlay, shortTab := bounds(assets.Text.Home.Play), bounds(assets.Text.Detail.Overview)
	if shortPlay.Y-shortTitle.Y != 339 || shortTab.Y-shortTitle.Y != 516 {
		t.Fatalf("the single-line title shifted the actions or tabs: title %v, play %v, tab %v", shortTitle, shortPlay, shortTab)
	}
	tester.Key(0, ui.KeyEnter)
	if state.Page != app.VideoDetail || state.Focus != videoDetailPlay {
		t.Fatal("the play entry changed the page before playback is connected")
	}
	route := state.Route
	original := pages.components[route]
	withoutControls := original
	withoutControls.Component = ComponentFunc(func(*ui.Context, *PageContext) {})
	pages.components[route] = withoutControls
	tester.Frame()
	back()
	pages.components[route] = original
	if stale.Context.Err() != context.Canceled {
		t.Fatal("leaving the detail screen left its request active")
	}
	stale.reply <- videoDetailReply{item: provider.MediaItem{Ref: selected.Ref, Title: "Stale result"}}
	(<-callbacks)()
	tester.Frame()
	if view.videoDetail.item.Title != "" || tester.HasText("Stale result") {
		t.Fatal("a late detail response changed the page after Back")
	}

	tall := selected
	tall.Title = strings.Repeat("An exceptionally long source title needs multiple lines. ", 6)
	request := open()
	request.reply <- videoDetailReply{item: tall}
	(<-callbacks)()
	tester.Frame()
	tester.Advance(2 * focusScroll.Duration)
	play := bounds(assets.Text.Home.Play)
	if state.Focus != videoDetailPlay || view.activeScroll.Y <= 0 || play.H != 62 || play.Y+play.H > 1080-48 {
		t.Fatalf("late title moved the initial focus out of view: play %v, focus %q, scroll %v", play, state.Focus, view.activeScroll.Y)
	}
	tester.Key(0, ui.KeyRight)
	if state.Focus != videoDetailLater {
		t.Fatalf("Right on a tall detail page focused %q", state.Focus)
	}
	back()

	active := open()
	active.reply <- videoDetailReply{err: &provider.Error{Code: provider.RateLimited}}
	(<-callbacks)()
	tester.Frame()
	if !tester.HasText(assets.Text.Home.RateLimited) {
		t.Fatal("the detail screen did not explain the rate limit")
	}
	tester.Key(0, ui.KeyDown)
	if state.Focus != videoDetailOverview.focusID() {
		t.Fatalf("the primary action did not lead down to the tabs: %s", state.Focus)
	}
	tester.Key(0, ui.KeyDown)
	if state.Focus != videoDetailRetry {
		t.Fatalf("the retry action was unreachable from the overview: %s", state.Focus)
	}
	tester.Key(0, ui.KeyEnter)
	retry := <-session.calls
	selected.Title = strings.Repeat("A full video title needs room to wrap. ", 2) + "Final words"
	selected.Subtitle = strings.Repeat("Full description from the source.\n", 14)
	selected.VideoDetails = &provider.VideoDetails{
		Access: provider.VideoAccessPreview, SegmentKind: provider.VideoParts,
		Segments: []provider.VideoSegment{{Title: "First video part"}, {Title: "Second video part"}},
		Actions:  []provider.ActionSpec{{ID: "boost", Slot: provider.ActionSlotHero, Label: "Boost", Icon: "like"}},
	}
	retry.reply <- videoDetailReply{item: selected}
	(<-callbacks)()
	tester.Frame()
	longTitle := bounds(selected.Title)
	longPlay, longTab := bounds(assets.Text.Detail.Preview), bounds(assets.Text.Detail.Overview)
	titleGrowth := longTitle.H - shortTitle.H
	if titleGrowth < shortTitle.H ||
		longPlay.Y-longTitle.Y < shortPlay.Y-shortTitle.Y+titleGrowth-1 ||
		longTab.Y-longTitle.Y < shortTab.Y-shortTitle.Y+titleGrowth-1 {
		t.Fatalf("the title did not wrap and push the controls down: title %v, play %v, tab %v", longTitle, longPlay, longTab)
	}
	if title, ok := tester.AccessByRole(ui.RoleHeading, selected.Title); !ok || title.Level != 1 {
		t.Fatal("the complete title was not exposed as the detail heading")
	}
	bounds(assets.Text.Detail.Collection)
	if !tester.HasText(assets.Text.Detail.More) || tester.HasText(assets.Text.Detail.AccessPreview) ||
		!tester.HasText(assets.Text.Detail.Preview) || !tester.HasText("Boost") {
		t.Fatal("the detail screen lost source metadata or declared action controls")
	}
	tester.Key(0, ui.KeyDown)
	if state.Focus != videoDetailMore {
		t.Fatalf("the next action from the overview tab should be More: %s", state.Focus)
	}
	tester.Key(0, ui.KeyEnter)
	if !view.videoDetail.expanded || !tester.HasText(selected.Subtitle) {
		t.Fatal("More did not expand the full description")
	}
	tester.Key(0, ui.KeyDown)
	tester.Advance(2 * focusScroll.Duration)
	if view.activeScroll.Y == 0 {
		t.Fatal("Down did not scroll the long description")
	}
	tester.Key(0, ui.KeyEscape)
	if view.videoDetail.expanded || state.Page != app.VideoDetail || state.Focus != videoDetailMore {
		t.Fatal("Back did not restore the collapsed description and its More action")
	}
	view.videoDetail.item.Title = strings.Repeat("The synopsis follows a tall video heading. ", 7)
	view.moveFocus(videoDetailMore, app.Down, true)
	tester.Frame()
	tester.Advance(2 * focusScroll.Duration)
	tester.Key(0, ui.KeyEnter)
	view.moveFocus(videoDetailMore, app.Down, true)
	tester.Frame()
	tester.Advance(2 * focusScroll.Duration)
	if !view.videoDetail.expanded || !tester.HasText(assets.Text.Detail.Collapse) {
		t.Fatal("the long synopsis did not expose its collapse button")
	}
	tester.Key(0, ui.KeyEnter)
	tester.Advance(2 * focusScroll.Duration)
	more := bounds(assets.Text.Detail.More)
	if view.videoDetail.expanded || state.Focus != videoDetailMore || more.Y < 0 || more.Y+more.H > 1080 {
		t.Fatalf("the collapsed synopsis left More off screen: focus %q, bounds %v, scroll %v/%v",
			state.Focus, more, view.activeScroll.Y, view.activeScroll.MaxY)
	}
	back()
}

type videoPaneSession struct {
	viewCatalogSession
	queries chan provider.VideoCommentQuery
	edits   chan provider.VideoEdit
}

func (s videoPaneSession) VideoComments(_ context.Context, query provider.VideoCommentQuery) (provider.VideoCommentPage, error) {
	s.queries <- query
	count := int64(1)
	rows := []provider.VideoComment{{ID: "newest", Author: "Another viewer", Body: "Most recent comment", ReplyCount: &count}}
	if query.ThreadID != "" {
		rows = []provider.VideoComment{{ID: "reply", Author: "Creator", Body: "Reply from the creator", IsCreator: true}}
	} else if query.Order == provider.VideoCommentsHot {
		rows = []provider.VideoComment{{ID: "hot", Author: "Viewer", Body: "Most liked comment", ReplyCount: &count}}
	}
	return provider.VideoCommentPage{Comments: rows}, nil
}

func (s videoPaneSession) EditVideo(_ context.Context, request provider.VideoEdit) (provider.VideoEditResult, error) {
	s.edits <- request
	switch request.Interaction {
	case provider.VideoWatchLater:
		return provider.VideoEditResult{WatchLater: &request.Enabled}, nil
	case provider.VideoAction:
		if request.ActionID == "triple" {
			return provider.VideoEditResult{
				ActionStates: map[string]bool{"coin": true},
				Message:      "Triple delivered",
				Celebrate:    true,
			}, nil
		}
		return provider.VideoEditResult{}, &provider.Error{Code: provider.Denied, Message: "Insufficient balance"}
	case provider.VideoReplyLike:
		return provider.VideoEditResult{}, nil
	default:
		return provider.VideoEditResult{}, &provider.Error{Code: provider.Denied, Message: "Insufficient balance"}
	}
}

func TestVideoDetailSectionsAndConfirmations(t *testing.T) {
	assets, err := LoadAssets()
	if err != nil {
		t.Fatal(err)
	}
	pages, err := NewPages(assets)
	if err != nil {
		t.Fatal(err)
	}
	entries := testEntries()
	entries[0].Capabilities = []provider.Capability{provider.GetCatalogItem, provider.ResolvePlayback, provider.ReadVideoComments, provider.WriteVideoComments, provider.EditVideo, provider.AccountWatchLater.Capability(), provider.EditVideoFavorites}
	state, err := app.NewState(entries, entries[0].ID, pages.Routes)
	if err != nil {
		t.Fatal(err)
	}
	session := videoPaneSession{queries: make(chan provider.VideoCommentQuery, 1), edits: make(chan provider.VideoEdit, 1)}
	callbacks := make(chan func(), 2)
	catalog := app.NewCatalog(func(string) provider.Session { return session }, func(fn func()) { callbacks <- fn }, func() {})
	defer catalog.Close()
	view := newTestView(t, Options{State: state, Catalog: catalog, Pages: pages, Assets: assets, Theme: theme.Default()})
	view.SyncEntry()
	if !state.Push(app.Route{Page: app.VideoDetail}) {
		t.Fatal("could not open video details")
	}
	ref := provider.MediaRef{EntryID: entries[0].ID, Kind: provider.Video, ID: "video"}
	count := int64(1)
	secondDuration := int64(91)
	coinAction := provider.ActionSpec{
		ID: "coin", Slot: provider.ActionSlotOverview, Label: "Coin", Icon: "coin", ActiveIcon: "coin-filled",
		Confirm:     &provider.ActionConfirm{Title: "Send a coin?", Body: "Cannot be undone.", Accept: "Send coin", Icon: "detail-primary/coin"},
		DoneMessage: "Coin sent",
	}
	tripleAction := provider.ActionSpec{
		ID: "triple", Slot: provider.ActionSlotHero, Label: "Triple", Icon: "like",
		Confirm:        &provider.ActionConfirm{Title: "Send triple?", Body: "Like, coin and save.", Accept: "Send triple", Icon: "detail-primary/like"},
		DoneMessage:    "Triple delivered",
		Celebrate:      true,
		CelebrateIcons: []string{"like", "coin", "detail/star"},
	}
	view.videoDetail.item = provider.MediaItem{
		Ref: ref, Title: strings.Repeat("A full video title can extend across several lines. ", 4),
		VideoDetails: &provider.VideoDetails{
			Comments: &count, SegmentKind: provider.VideoParts,
			Actions: []provider.ActionSpec{tripleAction, coinAction},
			Segments: []provider.VideoSegment{
				{Title: "Episode one"},
				{Title: strings.Repeat("A longer title for the second episode. ", 4), DurationSeconds: &secondDuration},
			},
			Discussion: []provider.VideoComment{
				{ID: "hot", Author: "Viewer", Body: "Most liked comment", Likes: &count, ReplyCount: &count},
				{ID: "second-hot", Author: "Viewer", Body: "Second hot comment"},
			},
			Related: []provider.MediaItem{
				{Ref: provider.MediaRef{EntryID: ref.EntryID, Kind: provider.Video, ID: "first"}, Title: "First related"},
				{Ref: provider.MediaRef{EntryID: ref.EntryID, Kind: provider.Video, ID: "second"}, Title: "Second related"},
			},
		},
	}
	tester := ui.NewTester(view.Render, 1920, 1080)
	view.reveal = true
	tester.Frame()
	tester.Advance(2 * focusScroll.Duration)
	key := func(value ui.Key) {
		tester.Key(0, value)
		tester.Advance(2 * focusScroll.Duration)
	}
	expectFocus := func(want app.FocusID) {
		t.Helper()
		if state.Focus != want {
			t.Fatalf("focused %q, want %q", state.Focus, want)
		}
	}
	expectFocusFill := func(id app.FocusID, want bool) {
		t.Helper()
		item := view.focus.item(id)
		if item == nil {
			t.Fatalf("missing focus target %q", id)
		}
		rect := item.target
		fill := tester.Image().RGBAAt(int(rect.X+10), int(rect.Y+rect.H/2))
		highlight := paint(view.Theme.Colors.Focus.Background)
		focused := fill.R >= highlight.R-8 && fill.G >= highlight.G-8 && fill.B >= highlight.B-8
		if focused != want {
			t.Fatalf("focus fill on %q is %v, want highlighted %t", id, fill, want)
		}
	}
	expectUnfilledSort := func(label string) {
		t.Helper()
		button, ok := tester.Find(label)
		if !ok || button.H != 42 {
			t.Fatalf("could not find the sort button %q: %v", label, button)
		}
		frame := tester.Image()
		y := int(button.Y) + 4
		fill := frame.RGBAAt(int(button.X+button.W/2), y)
		panel := frame.RGBAAt(int(button.X+button.W)+8, y)
		if fill != panel {
			t.Fatalf("sort button %q has fill %v over panel %v", label, fill, panel)
		}
	}
	expectFocus(videoDetailPlay)
	expectFocusFill(videoDetailPlay, true)
	// The view was built before the target's layout was committed.
	view.focus.item(videoDetailLater).target.W = 0
	key(ui.KeyRight)
	expectFocus(videoDetailLater)
	expectFocusFill(videoDetailPlay, false)
	expectFocusFill(videoDetailLater, true)
	key(ui.KeyLeft)
	expectFocus(videoDetailPlay)
	key(ui.KeyDown)
	key(ui.KeyRight)
	expectFocus(videoDetailEpisodes.focusID())
	if view.videoDetail.tab != videoDetailEpisodes {
		t.Fatal("moving right did not select Episodes")
	}
	if !tester.HasText("1 / 2") || !tester.HasText("Episode one") {
		t.Fatal("episode count or title missing")
	}
	if view.activeScroll.Y <= 0 {
		t.Fatal("selecting Episodes did not bring its panel into view")
	}
	key(ui.KeyLeft)
	expectFocus(videoDetailOverview.focusID())
	if view.videoDetail.tab != videoDetailOverview {
		t.Fatal("moving left did not switch back to the overview")
	}
	key(ui.KeyRight)
	key(ui.KeyDown)
	expectFocus(videoSegmentFocus(0))
	if first, second := view.focus.item(videoSegmentFocus(0)), view.focus.item(videoSegmentFocus(1)); first == nil || second == nil ||
		second.target.H <= 77 || first.target.H != second.target.H {
		t.Fatalf("long episode titles did not expand their row: first %+v, second %+v", first, second)
	}
	key(ui.KeyRight)
	expectFocus(videoSegmentFocus(1))
	key(ui.KeyEnter)
	expectFocus(videoSegmentFocus(1))
	if view.videoDetail.episodeIndex() != 1 || view.videoDetail.item.Ref != ref {
		t.Fatalf("selecting the second part changed the wrong detail state: %+v", view.videoDetail.item.Ref)
	}
	if !tester.HasText("2 / 2") {
		t.Fatal("episode count did not update to 2 / 2 after selecting the second part")
	}
	key(ui.KeyUp)
	key(ui.KeyRight)
	expectFocus(videoDetailComments.focusID())
	if tab := view.focus.item(videoDetailComments.focusID()); view.videoDetail.tab != videoDetailComments ||
		tab == nil || tab.target.Y < videoDetailCommentTabTop-1 || tab.target.Y >= videoDetailPanelTabTop {
		t.Fatalf("moving right did not bring comments above the other panels, scroll %.1f/%.1f: %+v",
			view.activeScroll.Y, view.activeScroll.MaxY, tab)
	}
	key(ui.KeyDown)
	expectFocus(videoDiscussionHot)
	expectUnfilledSort(assets.Text.Detail.Hot)
	key(ui.KeyDown)
	expectFocus(videoCommentFocus("hot"))
	key(ui.KeyDown)
	expectFocus(videoCommentFocus("second-hot"))
	key(ui.KeyDown)
	expectFocus(videoDiscussionCompose)
	key(ui.KeyUp)
	expectFocus(videoCommentFocus("second-hot"))
	key(ui.KeyUp)
	expectFocus(videoCommentFocus("hot"))
	key(ui.KeyUp)
	expectFocus(videoDiscussionHot)
	key(ui.KeyRight)
	expectFocus(videoDiscussionLatest)
	expectUnfilledSort(assets.Text.Detail.Hot)
	expectUnfilledSort(assets.Text.Detail.Latest)
	select {
	case callback := <-callbacks:
		callback()
	case <-time.After(time.Second):
		t.Fatal("focusing Latest did not change the comment order")
	}
	tester.Frame()
	expectUnfilledSort(assets.Text.Detail.Latest)
	if query := <-session.queries; query.Ref != ref || query.Order != provider.VideoCommentsLatest {
		t.Fatalf("latest comments query = %+v", query)
	}
	key(ui.KeyDown)
	expectFocus(videoCommentFocus("newest"))
	key(ui.KeyLeft)
	expectFocus(videoCommentFocus("hot"))
	key(ui.KeyRight)
	expectFocus(videoCommentFocus("newest"))
	commentRow := view.focus.item(videoCommentFocus("newest"))
	const commentEdge = videoDetailInset + 1 + 32 - 16
	if commentRow == nil || commentRow.target.X != commentEdge || commentRow.target.X+commentRow.target.W != videoDetailInset+1480-(commentEdge-videoDetailInset) {
		t.Fatalf("the comment focus band is not symmetric inside its panel: %+v", commentRow)
	}
	key(ui.KeyEnter)
	(<-callbacks)()
	tester.Frame()
	if query := <-session.queries; query.ThreadID != "newest" {
		t.Fatalf("thread query = %+v", query)
	}
	expectFocus(videoDiscussionBack)
	if !tester.HasText("Reply from the creator") {
		t.Fatal("thread did not show the returned reply")
	}
	backLabel, hasBackLabel := tester.Find(assets.Text.Detail.AllComments)
	replyCount, hasReplyCount := tester.Find(fmt.Sprintf(assets.Text.Detail.ReplyCount, 1))
	// Find reports the whole button, including its padding, icon, and gap.
	if !hasBackLabel || !hasReplyCount || backLabel.X+37 != replyCount.X {
		t.Fatalf("thread header text is not left-aligned: title %v (%t), count %v (%t)", backLabel, hasBackLabel, replyCount, hasReplyCount)
	}
	key(ui.KeyUp)
	expectFocus(videoDetailComments.focusID())
	key(ui.KeyEnter)
	if view.videoDetail.discussion.root != nil || !tester.HasText("Most recent comment") {
		t.Fatal("reselecting Comments did not return to the sorted comment list")
	}
	key(ui.KeyDown)
	expectFocus(videoDiscussionLatest)
	key(ui.KeyUp)
	expectFocus(videoDetailComments.focusID())
	key(ui.KeyRight)
	expectFocus(videoDetailRelated.focusID())
	key(ui.KeyLeft)
	expectFocus(videoDetailComments.focusID())
	if view.videoDetail.tab != videoDetailComments {
		t.Fatal("moving left did not switch back to comments")
	}
	key(ui.KeyRight)
	key(ui.KeyDown)
	related := catalogCardFocus(videoDetailRelatedShelf, view.videoDetail.item.VideoDetails.Related[0].Ref)
	expectFocus(related)
	if card := view.focus.item(related); card == nil || card.target.Y+card.target.H > 1080-48 {
		t.Fatalf("the focused related card did not leave room below its metadata: %+v", card)
	}

	coinFocus := videoDetailActionFocus("coin")
	page := &PageContext{State: state, Assets: assets, host: view}
	state.RecordPosition(app.Position{Focus: coinFocus})
	page.showVideoActionConfirmation(coinAction)
	tester.Frame()
	tester.Advance(menuTransition.Duration)
	expectFocus(videoConfirmAccept)
	expectFocusFill(videoConfirmAccept, true)
	key(ui.KeyRight)
	expectFocus(app.CloseModal)
	expectFocusFill(videoConfirmAccept, false)
	expectFocusFill(app.CloseModal, true)
	key(ui.KeyLeft)
	key(ui.KeyEnter)
	(<-callbacks)()
	tester.Frame()
	if request := <-session.edits; request.Ref != ref || request.Interaction != provider.VideoAction || request.ActionID != "coin" {
		t.Fatalf("confirmed coin request = %+v", request)
	}
	if !tester.HasText("Insufficient balance") {
		t.Fatal("plugin-provided rejection message did not render as feedback")
	}
	expectFocus(coinFocus)
	page.showVideoActionConfirmation(tripleAction)
	tester.Frame()
	if !tester.HasText(tripleAction.Confirm.Title) || !tester.HasText(tripleAction.Confirm.Accept) {
		t.Fatal("the triple confirmation did not use its own action spec")
	}
	key(ui.KeyRight)
	expectFocus(app.CloseModal)
	expectFocusFill(videoConfirmAccept, false)
	expectFocusFill(app.CloseModal, true)
	key(ui.KeyEscape)
	expectFocus(coinFocus)
	if !state.Push(app.Route{Page: app.VideoCommentCompose}) {
		t.Fatal("could not open the comment composer")
	}
	view.videoDetail.draft = videoCommentDraft{title: assets.Text.Detail.WriteComment, body: "An unsent draft"}
	tester.Frame()
	tester.Advance(menuTransition.Duration)
	expectFocus(videoCommentEdit)
	expectFocusFill(videoCommentEdit, true)
	expectFocusFill(videoCommentSend, false)
	if !tester.HasText("An unsent draft") || !tester.HasText("15 / 500") || !tester.HasText(assets.Text.Detail.PostComment) {
		t.Fatal("the comment composer did not show the draft and its character count")
	}
	key(ui.KeyEnter)
	expectFocus(videoCommentInput)
	tester.Type(" cancelled")
	key(ui.KeyEscape)
	expectFocus(videoCommentEdit)
	if view.videoDetail.draft.body != "An unsent draft" {
		t.Fatal("cancelling text input changed the draft")
	}
	key(ui.KeyEnter)
	expectFocus(videoCommentInput)
	tester.Key(ui.Cmd, ui.KeyA)
	limit := "\u4f60\u597d" + strings.Repeat("\U0001f680", (provider.VideoCommentLimit-2)/2)
	tester.Type(limit + "tail")
	key(ui.KeyEnter)
	expectFocus(videoCommentSend)
	expectFocusFill(videoCommentEdit, false)
	expectFocusFill(videoCommentSend, true)
	if view.videoDetail.draft.body != limit || !tester.HasText("500 / 500") {
		t.Fatal("the confirmed draft lost Unicode text or exceeded the character limit")
	}
	key(ui.KeyEscape)
	expectFocus(coinFocus)
	if len(session.edits) != 0 {
		t.Fatal("closing a confirmation queued a write")
	}

	page.editVideo(provider.VideoEdit{Ref: ref, Interaction: provider.VideoWatchLater, Enabled: true})
	(<-callbacks)()
	<-session.edits
	tester.Frame()
	tester.Advance(400 * time.Millisecond)
	toast, shown := tester.Find(assets.Text.Detail.LaterAdded)
	if !shown || math.Abs(float64(toast.X+toast.W/2-960)) > 1 || math.Abs(float64(toast.Y+toast.H-1020)) > 1 {
		t.Fatalf("watch-later feedback did not appear at the bottom center: %v, visible %t", toast, shown)
	}
	tester.Advance(videoToastDuration + time.Millisecond)
	if tester.HasText(assets.Text.Detail.LaterAdded) {
		t.Fatal("the watch-later toast did not expire")
	}
	page.editVideo(provider.VideoEdit{Ref: ref, Interaction: provider.VideoAction, ActionID: "triple"})
	(<-callbacks)()
	<-session.edits
	tester.Frame()
	if !view.videoDetail.feedback.celebration || !tester.HasText("Triple delivered") {
		t.Fatal("a completed triple action did not show its celebration")
	}
	if updated, ok := view.videoDetail.actionSpec("coin"); !ok || !updated.Active {
		t.Fatalf("actionStates did not mark coin active: %+v", updated)
	}
	tester.Advance(videoCelebrationDuration + time.Millisecond)
	if tester.HasText("Triple delivered") {
		t.Fatal("the triple celebration did not expire")
	}
	page.editVideo(provider.VideoEdit{Ref: ref, Interaction: provider.VideoReplyLike, ReplyID: "newest", Enabled: true})
	(<-callbacks)()
	<-session.edits
	tester.Frame()
	if view.videoDetail.feedback.text != "" {
		t.Fatal("a successful comment vote displayed feedback")
	}

}

func TestVideoDetailSelectsCurrentCollectionUpload(t *testing.T) {
	ref := provider.MediaRef{EntryID: "source", Kind: provider.Video, ID: "current"}
	episode := provider.VideoSegment{Title: "Current upload", Ref: &ref}
	state := videoDetailState{item: provider.MediaItem{
		Ref: ref,
		VideoDetails: &provider.VideoDetails{
			SegmentKind: provider.VideoCollection,
			Segments:    []provider.VideoSegment{{Title: "First upload"}, episode},
		},
	}}
	if state.episodeIndex() != 1 {
		t.Fatalf("current upload = %d, want 1", state.episodeIndex())
	}
	state.item.VideoDetails.SegmentKind = provider.VideoChapters
	if state.episodeIndex() != -1 || videoDetailSectionFocus(videoDetailEpisodes, state.item.VideoDetails) != "" {
		t.Fatal("timeline chapters were exposed as selectable episodes in details")
	}
}

type expiredVideoAccount struct {
	uiAuthSource
	writes chan provider.VideoEdit
}

func (s *expiredVideoAccount) EditVideo(_ context.Context, request provider.VideoEdit) (provider.VideoEditResult, error) {
	s.writes <- request
	return provider.VideoEditResult{}, &provider.Error{Code: provider.NeedsSignIn}
}

func TestVideoDetailOffersSignInAgainWhenAFailedWriteNeedsIt(t *testing.T) {
	assets, err := LoadAssets()
	if err != nil {
		t.Fatal(err)
	}
	pages, err := NewPages(assets)
	if err != nil {
		t.Fatal(err)
	}
	entries := testEntries()
	entries[0].Capabilities = provider.Capabilities{provider.GetCatalogItem, provider.SignIn, provider.EditVideo}
	state, err := app.NewState(entries, entries[0].ID, pages.Routes)
	if err != nil {
		t.Fatal(err)
	}
	source := &expiredVideoAccount{writes: make(chan provider.VideoEdit, 2)}
	queued := make(chan func(), 8)
	post := func(f func()) { queued <- f }
	catalog := app.NewCatalog(func(string) provider.Session { return source }, post, func() {})
	defer catalog.Close()
	keychain := uiTestKeychain{data: []byte(`{"selected":"viewer","accounts":[{"account":{"id":"viewer"},"data":{}}]}`)}
	auth := app.NewAuth(keychain, post, func() {}, catalog.Reload, func() {})
	defer auth.Close()
	view := newTestView(t, Options{Auth: auth, State: state, Catalog: catalog, Pages: pages, Assets: assets, Theme: theme.Default()})
	tester := ui.NewTester(view.Render, 1920, 1080)
	deliver := func() {
		t.Helper()
		select {
		case callback := <-queued:
			callback()
			tester.Frame()
		case <-time.After(time.Second):
			t.Fatal("missing account or video result")
		}
	}
	view.SyncEntry()
	for auth.Accounts().CurrentID != "viewer" {
		deliver()
	}
	if !state.Push(app.Route{Page: app.VideoDetail}) {
		t.Fatal("could not open video detail")
	}
	coinFocus := videoDetailActionFocus("coin")
	view.videoDetail.item = provider.MediaItem{
		Ref: provider.MediaRef{EntryID: entries[0].ID, Kind: provider.Video, ID: "video"}, Title: "Video",
		VideoDetails: &provider.VideoDetails{
			Actions: []provider.ActionSpec{{
				ID: "coin", Slot: provider.ActionSlotOverview, Label: "Coin",
				Confirm: &provider.ActionConfirm{Title: "Send coin?"},
			}},
		},
	}
	page := &PageContext{State: state, Assets: assets, host: view}
	state.RecordPosition(app.Position{Focus: coinFocus})
	page.editVideo(provider.VideoEdit{Ref: view.videoDetail.item.Ref, Interaction: provider.VideoAction, ActionID: "coin"})
	deliver()
	if layers := state.Layers(); len(layers) != 2 || layers[0].Page != app.VideoDetail || state.Page != app.SignInQR {
		t.Fatalf("account recovery was not shown above the video detail: %+v", layers)
	}
	tester.Key(0, ui.KeyEscape)
	if state.Page != app.VideoDetail || state.Focus != coinFocus {
		t.Fatal("Back did not restore the action button")
	}

	if !state.Push(app.Route{Page: app.VideoCommentCompose}) {
		t.Fatal("could not open the comment preview")
	}
	const body = "Keep this comment draft"
	view.videoDetail.draft = videoCommentDraft{title: assets.Text.Detail.WriteComment, body: body}
	state.RecordPosition(app.Position{Focus: videoCommentSend})
	page.postVideoComment()
	for state.Page == app.VideoCommentCompose {
		deliver()
	}
	if layers := state.Layers(); len(layers) != 3 || layers[1].Page != app.VideoCommentCompose || state.Page != app.SignInQR {
		t.Fatalf("account recovery was not shown above the draft: %+v", layers)
	}
	tester.Key(0, ui.KeyEscape)
	if state.Page != app.VideoCommentCompose || state.Focus != videoCommentSend || view.videoDetail.draft.body != body {
		t.Fatal("Back did not restore the draft and its publish action")
	}
	coin, comment := <-source.writes, <-source.writes
	if coin.Interaction != provider.VideoAction || coin.ActionID != "coin" || comment.Interaction != provider.VideoPostComment || comment.Body != body {
		t.Fatalf("unexpected original edits: %+v, %+v", coin, comment)
	}
}

type approvingVideoAccount struct {
	expiredVideoAccount
	reads chan provider.MediaRef
}

type approvedVideoChallenge uiAuthChallenge

func (a approvedVideoChallenge) Challenge() provider.QRChallenge { return provider.QRChallenge(a) }
func (approvedVideoChallenge) Poll(context.Context) (provider.QRProgress, error) {
	return provider.QRProgress{Status: provider.QRApproved, Credential: provider.Credential{
		Account: provider.Account{ID: "viewer", Name: "Viewer"}, Data: []byte("{}"),
	}}, nil
}

func (*approvingVideoAccount) BeginSignIn(context.Context) (provider.QRAttempt, error) {
	return approvedVideoChallenge{URL: "https://source.invalid/approve", ExpiresAt: time.Now().Add(10 * time.Second)}, nil
}

func (s *approvingVideoAccount) Item(_ context.Context, ref provider.MediaRef) (provider.MediaItem, error) {
	s.reads <- ref
	return provider.MediaItem{Ref: ref, Title: "Video"}, nil
}

func TestVideoDetailResumesPendingCoinConfirmationAfterSignIn(t *testing.T) {
	assets, err := LoadAssets()
	if err != nil {
		t.Fatal(err)
	}
	pages, err := NewPages(assets)
	if err != nil {
		t.Fatal(err)
	}
	entries := testEntries()
	entries[0].Capabilities = provider.Capabilities{provider.GetCatalogItem, provider.SignIn, provider.EditVideo}
	state, err := app.NewState(entries, entries[0].ID, pages.Routes)
	if err != nil {
		t.Fatal(err)
	}
	source := &approvingVideoAccount{
		expiredVideoAccount: expiredVideoAccount{writes: make(chan provider.VideoEdit, 1)},
		reads:               make(chan provider.MediaRef, 1),
	}
	queued := make(chan func(), 8)
	post := func(f func()) { queued <- f }
	catalog := app.NewCatalog(func(string) provider.Session { return source }, post, func() {})
	defer catalog.Close()
	auth := app.NewAuth(uiTestKeychain{}, post, func() {}, catalog.Reload, func() {})
	defer auth.Close()
	view := newTestView(t, Options{Auth: auth, State: state, Catalog: catalog, Pages: pages, Assets: assets, Theme: theme.Default()})
	await := func(done func() bool) {
		t.Helper()
		timeout := time.After(6 * time.Second)
		for !done() {
			select {
			case callback := <-queued:
				callback()
			case <-timeout:
				t.Fatal("the pending sign-in action did not reach its expected state")
			}
		}
	}
	view.SyncEntry()
	await(func() bool { return auth.Accounts().Ready })
	if !state.Push(app.Route{Page: app.VideoDetail}) {
		t.Fatal("could not open video detail")
	}
	coinAction := provider.ActionSpec{
		ID: "coin", Slot: provider.ActionSlotOverview, Label: "Coin",
		Confirm: &provider.ActionConfirm{Title: "Send coin?"},
	}
	ref := provider.MediaRef{EntryID: entries[0].ID, Kind: provider.Video, ID: "video"}
	view.videoDetail.seed = provider.MediaItem{Ref: ref, Title: "Video", VideoDetails: &provider.VideoDetails{Actions: []provider.ActionSpec{coinAction}}}
	view.videoDetail.item = view.videoDetail.seed
	page := &PageContext{State: state, Assets: assets, host: view}
	page.runVideoCustomAction(coinAction)
	if state.Page != app.SignInQR || view.videoDetail.signInAction == nil {
		t.Fatal("the coin action did not open sign-in and retain its intent")
	}
	view.dispatch(func() { state.Back() })
	if view.videoDetail.signInAction != nil {
		t.Fatal("cancelling sign-in retained an action")
	}
	page.runVideoCustomAction(coinAction)
	await(func() bool { return state.Page == app.VideoConfirm })
	if got := <-source.reads; got != ref || view.videoDetail.confirm == nil || view.videoDetail.confirm.ID != "coin" || view.videoDetail.item.Ref != ref {
		t.Fatal("sign-in did not restore confirmation for the original video")
	}
	if len(source.writes) != 0 {
		t.Fatal("sign-in sent custom action before confirmation")
	}
}

func TestVideoDetailSynopsisCacheReusesResult(t *testing.T) {
	body := strings.Repeat("Long synopsis line for CoreText memoization check. ", 12)
	var detail videoDetailState
	first := detail.cachedSynopsis(body, 760)
	if !detail.synopsisReady || !first.truncated || !strings.HasSuffix(first.second, "\u2026") {
		t.Fatalf("unexpected initial synopsis: %+v", first)
	}
	second := detail.cachedSynopsis(body, 760)
	if second != first {
		t.Fatalf("cached synopsis changed: %+v vs %+v", second, first)
	}
	narrower := detail.cachedSynopsis(body, 400)
	if detail.synopsisWidth != 400 || !narrower.truncated {
		t.Fatalf("synopsis cache did not recompute on width change: width=%v %+v", detail.synopsisWidth, narrower)
	}
}

func TestVideoDetailCommentsLazyLoadsHotWhenUnpopulated(t *testing.T) {
	assets, err := LoadAssets()
	if err != nil {
		t.Fatal(err)
	}
	pages, err := NewPages(assets)
	if err != nil {
		t.Fatal(err)
	}
	entries := testEntries()
	entries[0].Capabilities = provider.Capabilities{
		provider.GetCatalogItem, provider.ReadVideoComments,
	}
	state, err := app.NewState(entries, entries[0].ID, pages.Routes)
	if err != nil {
		t.Fatal(err)
	}
	session := videoPaneSession{queries: make(chan provider.VideoCommentQuery, 4), edits: make(chan provider.VideoEdit, 1)}
	callbacks := make(chan func(), 4)
	catalog := app.NewCatalog(func(string) provider.Session { return session }, func(fn func()) { callbacks <- fn }, func() {})
	defer catalog.Close()
	view := newTestView(t, Options{State: state, Catalog: catalog, Pages: pages, Assets: assets, Theme: theme.Default()})
	view.SyncEntry()
	if !state.Push(app.Route{Page: app.VideoDetail}) {
		t.Fatal("could not open video details")
	}
	ref := provider.MediaRef{EntryID: entries[0].ID, Kind: provider.Series, ID: "ss45969"}
	commentCount := int64(42)
	view.videoDetail.item = provider.MediaItem{
		Ref:   ref,
		Title: "Series With Lazy Hot Comments",
		VideoDetails: &provider.VideoDetails{
			Comments: &commentCount,
			// Intentionally leave Discussion empty to verify lazy hot-comment loading.
		},
	}
	tester := ui.NewTester(view.Render, 1920, 1080)
	tester.Frame()

	// Switch to Comments tab -> must trigger VideoCommentsHot automatically instead of showing NoComments.
	page := &PageContext{State: state, Assets: assets, host: view}
	page.selectVideoDetailTab(videoDetailComments, app.Right)
	tester.Frame()
	if tester.HasText(assets.Text.Detail.NoComments) {
		t.Fatal("Comments tab showed NoComments while hot comments were loading")
	}
	select {
	case cb := <-callbacks:
		cb()
	case <-time.After(2 * time.Second):
		t.Fatal("selecting Comments tab did not lazy-load hot comments when Discussion was empty")
	}
	query := <-session.queries
	if query.Ref != ref || query.Order != provider.VideoCommentsHot || query.Page != 0 {
		t.Fatalf("unexpected hot query: %+v", query)
	}
	tester.Frame()
	if !tester.HasText("Most liked comment") || tester.HasText(assets.Text.Detail.NoComments) {
		t.Fatalf("lazy-loaded hot comments did not render")
	}

	// Switch to Latest and back to Hot -> Hot stays cached with no redundant query.
	page.setVideoDiscussionOrder(true)
	select {
	case cb := <-callbacks:
		cb()
	case <-time.After(2 * time.Second):
		t.Fatal("switching to Latest did not load latest comments")
	}
	latestQuery := <-session.queries
	if latestQuery.Order != provider.VideoCommentsLatest {
		t.Fatalf("unexpected latest query: %+v", latestQuery)
	}
	page.setVideoDiscussionOrder(false)
	tester.Frame()
	if !tester.HasText("Most liked comment") {
		t.Fatal("switching back to Hot lost loaded hot comments")
	}
	select {
	case extra := <-session.queries:
		t.Fatalf("unexpected redundant query after hot comments were cached: %+v", extra)
	default:
	}

	// Zero comment count skips redundant Hot RPC.
	zero := int64(0)
	view.videoDetail.item.VideoDetails = &provider.VideoDetails{Comments: &zero}
	view.videoDetail.discussion = nil
	view.videoDetail.tab = videoDetailOverview
	page.selectVideoDetailTab(videoDetailComments, app.Right)
	tester.Frame()
	select {
	case extra := <-session.queries:
		t.Fatalf("unexpected query when comment count is explicitly 0: %+v", extra)
	default:
	}
}
