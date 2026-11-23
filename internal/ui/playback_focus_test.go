package tvui

import (
	"context"
	"fmt"
	"image"
	"math"
	"net/http"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/player"
	"github.com/yuchanns/MikuTV/internal/provider"
	"github.com/yuchanns/MikuTV/internal/theme"
)

func TestPlaybackPopoverColors(t *testing.T) {
	assets, err := LoadAssets()
	if err != nil {
		t.Fatal(err)
	}
	pages, err := NewPages(assets)
	if err != nil {
		t.Fatal(err)
	}
	state, err := app.NewState(testEntries(), "test-source", pages.Routes)
	if err != nil {
		t.Fatal(err)
	}
	view := newTestView(t, Options{State: state, Pages: pages, Assets: assets, Theme: theme.Default()})
	view.player.panel = playerQuality
	density := 60
	tester := ui.NewTester(func(c *ui.Context) {
		view.focus.begin(c)
		page := view.context(state.Location, true, 320, 160)
		ui.Column(c).FillWidth().Children(func() {
			page.playbackOption(c, playbackOptionID(playerQuality, "hdr"), "HDR",
				page.playbackLimitLabel(provider.PlaybackMembership), false, func() {})
			page.playbackSlider(c, "density", "Density", density, 10, func(next int) { density = next })
		})
	}, 320, 160)
	requireColor := func(frame *image.RGBA, area image.Rectangle, color theme.Color) {
		t.Helper()
		ink := color.NRGBA()
		for y := area.Min.Y; y < area.Max.Y; y++ {
			for x := area.Min.X; x < area.Max.X; x++ {
				got := frame.RGBAAt(x, y)
				if closeColorChannel(got.R, ink.R) && closeColorChannel(got.G, ink.G) && closeColorChannel(got.B, ink.B) {
					return
				}
			}
		}
		t.Fatalf("color %08x missing in %v", uint32(color), area)
	}
	tester.Frame()
	frame := tester.Image()
	derived := deriveColors(view.Theme)
	lock := image.Rect(278, 0, 306, 55)
	requireColor(frame, lock, view.Theme.Colors.Accent.Primary)
	requireColor(frame, image.Rect(35, 60, 135, 128), view.Theme.Colors.Accent.Soft)
	requireColor(frame, image.Rect(35, 60, 135, 128), derived.sliderFillEdge)
	requireColor(frame, image.Rect(210, 60, 285, 128), derived.sliderTrack)
	requireColor(frame, image.Rect(210, 60, 285, 128), derived.sliderEdge)

	updated := view.Theme
	updated.Colors.Accent.Primary = 0xEC6EA6FF
	view.SetTheme(updated)
	tester.Frame()
	requireColor(tester.Image(), lock, view.Theme.Colors.Accent.Primary)
}

func closeColorChannel(a, b uint8) bool {
	return max(a, b)-min(a, b) <= 10
}

func TestSubtitleKeepsPlayerControlFocus(t *testing.T) {
	assets, err := LoadAssets()
	if err != nil {
		t.Fatal(err)
	}
	pages, err := NewPages(assets)
	if err != nil {
		t.Fatal(err)
	}
	entries := testEntries()
	entries[0].Capabilities = append(entries[0].Capabilities, provider.ReadSubtitles)
	state, err := app.NewState(entries, "test-source", pages.Routes)
	if err != nil {
		t.Fatal(err)
	}
	view := newTestView(t, Options{State: state, Pages: pages, Assets: assets, Theme: theme.Default()})
	view.Playback = app.NewPlayback(view.Catalog, player.Engine{}, nil, func(func()) {}, func() {})
	view.player.visible = true
	state.Focus = playerSubtitles
	clock := app.PlaybackState{
		Clock: player.Status{HasFrame: true}, Position: 5, Duration: 60,
		Resolution: provider.Playback{Plan: provider.PlaybackPlan{
			Subtitles: []provider.PlaybackTrack{{ID: "zh", Label: "ZH"}},
		}},
	}

	var frame *ui.Context
	tester := ui.NewTester(func(c *ui.Context) {
		frame = c
		view.focus.begin(c)
		page := view.context(state.Location, true, 1920, 1080)
		page.playbackTextLayers(c, clock)
		page.playbackControls(c, clock, playbackItem{})
	}, 1920, 1080)
	tester.Frame()
	if focus := ui.FocusedValue(frame, &state.Focus); focus != playerSubtitles {
		t.Fatalf("focus before the cue = %q", focus)
	}
	control := view.focus.item(playerSubtitles).elementID

	view.player.content.cues = []provider.SubtitleCue{{Start: 2, End: 8, Text: "Subtitle preview"}}
	tester.Frame()
	if focus := ui.FocusedValue(frame, &state.Focus); focus != playerSubtitles {
		t.Fatalf("focus while showing the cue = %q", focus)
	}
	if got := view.focus.item(playerSubtitles).elementID; got != control {
		t.Fatalf("subtitle remounted the player's control: %d to %d", control, got)
	}
}

func TestDanmakuSchedulePreventsLeftEdgeStarvationAndOverlap(t *testing.T) {
	items := make([]provider.Danmaku, 0, 60)
	for i := range 60 {
		items = append(items, provider.Danmaku{
			Seconds: float64(i) * 0.25, // 4 items/sec for 15 seconds (dense stream)
			Mode:    provider.DanmakuScroll,
			Text:    fmt.Sprintf("\u5f39\u5e55\u6d4b\u8bd5\u6d88\u606f%02d", i),
		})
	}
	const width float32 = 1920
	scheduled := scheduleDanmaku(items, 60, width)
	if len(scheduled) < 15 {
		t.Fatalf("expected continuous scheduling under dense load, got only %d items", len(scheduled))
	}

	// At t=10s (after initial items reached the left edge at t=8s), new items must
	// actively be entering from the right half of the screen (not starved at x<=0).
	const pos = 10.0
	var rightHalf, visible int
	for _, item := range scheduled {
		if item.start <= pos && pos < item.end {
			visible++
			x := width - (width+item.width)*float32((pos-item.start)/danmakuMovingTime)
			if x > width*0.5 {
				rightHalf++
			}
		}
	}
	if visible < 8 || rightHalf == 0 {
		t.Fatalf("danmaku starved at t=%.1f: visible=%d rightHalf=%d", pos, visible, rightHalf)
	}

	// Verify zero horizontal overlap on any scroll lane across their full lifetimes.
	byLane := make(map[int][]scheduledDanmaku)
	for _, item := range scheduled {
		byLane[item.lane] = append(byLane[item.lane], item)
	}
	for lane, list := range byLane {
		for i := 1; i < len(list); i++ {
			prev, curr := list[i-1], list[i]
			for _, checkAt := range []float64{curr.start, prev.end} {
				prevLeft := float64(width) - (float64(width)+float64(prev.width))*((checkAt-prev.start)/danmakuMovingTime)
				prevRight := prevLeft + float64(prev.width)
				currLeft := float64(width) - (float64(width)+float64(curr.width))*((checkAt-curr.start)/danmakuMovingTime)
				if prevRight > currLeft+1e-3 {
					t.Fatalf("lane %d overlap at t=%.3f: prevRight=%.1f > currLeft=%.1f", lane, checkAt, prevRight, currLeft)
				}
			}
		}
	}

	// Higher density schedules more items than lower density on a heavy burst.
	burst := make([]provider.Danmaku, 0, 120)
	for i := range 120 {
		burst = append(burst, provider.Danmaku{
			Seconds: float64(i) * 0.05,
			Mode:    provider.DanmakuScroll,
			Text:    fmt.Sprintf("\u9ad8\u5bc6\u5ea6\u6d4b\u8bd5\u5f39\u5e55%03d", i),
		})
	}
	sparse := scheduleDanmaku(burst, 10, width)
	medium := scheduleDanmaku(burst, 60, width)
	dense := scheduleDanmaku(burst, 100, width)
	if len(sparse) >= len(medium) || len(medium) >= len(dense) {
		t.Fatalf("density did not scale monotonically: 10%%=%d, 60%%=%d, 100%%=%d", len(sparse), len(medium), len(dense))
	}
}

func TestDanmakuSmoothClockAndAnimationFrame(t *testing.T) {
	var content playbackContent
	base := time.Unix(1000, 0)
	frame := time.Second / 60

	// Small sampling jitter in AVPlayer.currentTime should produce smooth monotonic steps.
	state := app.PlaybackState{
		Revision: 1, Playing: true, Position: 10.0,
		Clock: player.Status{HasFrame: true},
	}
	prev := content.danmakuPosition(base, state, 1.0)
	for i := 1; i <= 10; i++ {
		now := base.Add(time.Duration(i) * frame)
		jitter := 0.004
		if i%2 == 0 {
			jitter = -0.004
		}
		state.Position = 10.0 + float64(i)/60.0 + jitter
		got := content.danmakuPosition(now, state, 1.0)
		step := got - prev
		if step <= 0 || math.Abs(step-1.0/60.0) > 0.003 {
			t.Fatalf("frame %d step=%.5f not smooth (want ~%.5f)", i, step, 1.0/60.0)
		}
		prev = got
	}

	// Seek (>0.12s jump) snaps immediately.
	state.Position = 42.5
	if got := content.danmakuPosition(base.Add(11*frame), state, 1.0); math.Abs(got-42.5) > 1e-6 {
		t.Fatalf("seek did not snap: got %.4f, want 42.5", got)
	}

	// Pause freezes cleanly on exact position.
	state.Playing = false
	state.Position = 42.5
	if got := content.danmakuPosition(base.Add(12*frame), state, 1.0); got != 42.5 {
		t.Fatalf("paused position = %.4f, want 42.5", got)
	}
}

type autoNextPlaybackSession struct {
	plans chan provider.PlaybackPlan
}

func (s *autoNextPlaybackSession) Close() {}
func (s *autoNextPlaybackSession) Browse(context.Context, provider.BrowseRequest) (provider.CollectionPage, error) {
	return provider.CollectionPage{}, nil
}
func (s *autoNextPlaybackSession) Item(context.Context, provider.MediaRef) (provider.MediaItem, error) {
	return provider.MediaItem{}, nil
}
func (s *autoNextPlaybackSession) ResolvePlayback(ctx context.Context, req provider.PlaybackRequest) (provider.Playback, error) {
	select {
	case plan := <-s.plans:
		if plan.PartID == "" {
			plan.PartID = req.PartID
		}
		plan.Streams = []provider.PlaybackStream{{Format: provider.PlaybackDirect}}
		return provider.Playback{Plan: plan, Transport: autoNextTransport{}}, nil
	case <-ctx.Done():
		return provider.Playback{}, ctx.Err()
	}
}

type autoNextTransport struct{}

func (autoNextTransport) OpenResource(context.Context, provider.PlaybackResource, string, string) (*http.Response, error) {
	return nil, fmt.Errorf("unused")
}

func TestPlaybackAutoAdvancesNextEpisodeAndStopsOnLastAndPreview(t *testing.T) {
	assets, err := LoadAssets()
	if err != nil {
		t.Fatal(err)
	}
	pages, err := NewPages(assets)
	if err != nil {
		t.Fatal(err)
	}
	entries := testEntries()
	entries[0].Capabilities = append(entries[0].Capabilities, provider.GetCatalogItem, provider.ResolvePlayback)
	state, err := app.NewState(entries, entries[0].ID, pages.Routes)
	if err != nil {
		t.Fatal(err)
	}
	deliveries := make(chan func(), 8)
	deliver := func() {
		t.Helper()
		select {
		case fn := <-deliveries:
			fn()
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for playback resolution")
		}
	}
	source := &autoNextPlaybackSession{plans: make(chan provider.PlaybackPlan, 8)}
	clock := player.Status{HasFrame: true, Duration: 100}
	view := newTestView(t, Options{State: state, Pages: pages, Assets: assets, Theme: theme.Default()})
	catalogEvents := make(chan func(), 8)
	view.Catalog = app.NewCatalog(func(string) provider.Session {
		return source
	}, func(fn func()) { catalogEvents <- fn }, func() {})
	view.Catalog.Select(entries[0])
	select {
	case fn := <-catalogEvents:
		fn()
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for initial catalog browse")
	}
	view.Playback = app.NewPlayback(view.Catalog, player.Engine{
		Open:     func(string, float64) bool { return true },
		Snapshot: func() player.Status { return clock },
	}, nil, func(fn func()) { deliveries <- fn }, func() {})
	defer view.Playback.Close()

	ref := provider.MediaRef{EntryID: entries[0].ID, Kind: provider.Video, ID: "BV1multi"}
	item := provider.MediaItem{
		Ref: ref, Title: "Multi Part Video",
		VideoDetails: &provider.VideoDetails{
			SegmentKind: provider.VideoParts,
			Segments: []provider.VideoSegment{
				{PartID: "p1", Title: "Episode 1"},
				{PartID: "p2", Title: "Episode 2"},
			},
		},
	}
	view.videoDetail.item = item
	view.videoDetail.part = 0
	pageCtx := view.context(state.Location, true, 1920, 1080)
	source.plans <- provider.PlaybackPlan{PartID: "p1", Duration: 100, Access: provider.VideoAccessGranted}
	pageCtx.startDetailPlayback(item, nil)
	deliver()

	tester := ui.NewTester(func(c *ui.Context) {
		view.focus.begin(c)
		p := view.context(state.Location, true, 1920, 1080)
		playbackPage{}.Render(c, p)
	}, 1920, 1080)

	// Part 1 ends while a modal overlay is open: must defer until overlay closes.
	view.player.content.chooser = &playerFavoriteChooser{}
	clock.Ended, clock.Position = true, 100
	tester.Frame()
	if view.player.index != 0 {
		t.Fatalf("advanced while favorite overlay was open: index=%d", view.player.index)
	}

	// Closing overlay lets part 1 auto-advance to part 2 and syncs detail part.
	view.player.content.chooser = nil
	view.player.panel = playerInfo
	source.plans <- provider.PlaybackPlan{PartID: "p2", Duration: 120, Access: provider.VideoAccessGranted}
	tester.Frame()
	if view.player.index != 1 || view.player.panel != "" || view.videoDetail.part != 1 {
		t.Fatalf("did not auto-advance to episode 2 cleanly: index=%d panel=%q detailPart=%d",
			view.player.index, view.player.panel, view.videoDetail.part)
	}
	if got := view.Playback.State().Target.PartID; got != "p2" {
		t.Fatalf("auto-advanced target part = %q, want p2", got)
	}
	clock.Ended, clock.Position, clock.Duration = false, 0, 120
	deliver()

	// Last episode ending must stop on ended feedback instead of advancing.
	clock.Ended, clock.Position = true, 120
	tester.Frame()
	if view.player.index != 1 || view.player.feedback != playbackFeedbackEnded {
		t.Fatalf("last episode did not stay on ended screen: index=%d feedback=%v", view.player.index, view.player.feedback)
	}

	// Trial preview ending on a non-last episode must NOT skip to next episode.
	source.plans <- provider.PlaybackPlan{PartID: "p1", Duration: 180, Access: provider.VideoAccessPreview}
	clock.Ended, clock.Position = false, 0
	view.startPlaybackItem(0, nil)
	deliver()
	view.Playback.StartPreview()
	clock.Ended, clock.Position = true, 180
	tester.Frame()
	if view.player.index != 0 || !playbackPreviewEnded(view.Playback.State()) {
		t.Fatalf("trial preview end skipped episode: index=%d ended=%t", view.player.index, playbackPreviewEnded(view.Playback.State()))
	}
}

func TestPlaybackNetworkSpeedFormattingAndTopRightRender(t *testing.T) {
	cases := []struct {
		bps  float64
		want string
	}{
		{0, "0 KB/s"},
		{128 * 1024, "128 KB/s"},
		{1023 * 1024, "1023 KB/s"},
		{1023.6 * 1024, "1.0 MB/s"},
		{8.6 * 1024 * 1024, "8.6 MB/s"},
		{120 * 1024 * 1024, "120 MB/s"},
	}
	for _, tc := range cases {
		if got := formatNetworkSpeed(tc.bps); got != tc.want {
			t.Fatalf("formatNetworkSpeed(%v) = %q, want %q", tc.bps, got, tc.want)
		}
	}

	// Stall grace & throttle semantics.
	var pv playbackView
	t0 := time.Unix(2000, 0)
	if bps, low := pv.displayNetworkSpeed(t0, app.PlaybackState{BytesPerSecond: 0}); bps != 0 || low {
		t.Fatalf("initial zero speed should not warn immediately: bps=%v low=%t", bps, low)
	}
	if bps, low := pv.displayNetworkSpeed(t0.Add(1600*time.Millisecond), app.PlaybackState{BytesPerSecond: 0}); bps != 0 || !low {
		t.Fatalf("persistent zero speed after 1.5s should warn: bps=%v low=%t", bps, low)
	}
	// Instant recovery from 0 to >0 bypasses 400ms throttle and clears warning.
	if bps, low := pv.displayNetworkSpeed(t0.Add(1700*time.Millisecond), app.PlaybackState{BytesPerSecond: 4 * 1024 * 1024}); bps != 4*1024*1024 || low {
		t.Fatalf("recovery did not immediately clear warning: bps=%v low=%t", bps, low)
	}

	assets, err := LoadAssets()
	if err != nil {
		t.Fatal(err)
	}
	pages, err := NewPages(assets)
	if err != nil {
		t.Fatal(err)
	}
	state, err := app.NewState(testEntries(), "test-source", pages.Routes)
	if err != nil {
		t.Fatal(err)
	}
	view := newTestView(t, Options{State: state, Pages: pages, Assets: assets, Theme: theme.Default()})
	view.Playback = app.NewPlayback(view.Catalog, player.Engine{}, nil, func(func()) {}, func() {})

	hasColor := func(frame *image.RGBA, area image.Rectangle, color theme.Color) bool {
		ink := color.NRGBA()
		for y := area.Min.Y; y < area.Max.Y; y++ {
			for x := area.Min.X; x < area.Max.X; x++ {
				got := frame.RGBAAt(x, y)
				if closeColorChannel(got.R, ink.R) && closeColorChannel(got.G, ink.G) && closeColorChannel(got.B, ink.B) {
					return true
				}
			}
		}
		return false
	}

	topRight := image.Rect(1600, 55, 1850, 115)
	pbState := app.PlaybackState{Loading: true, BytesPerSecond: 8.6 * 1024 * 1024}
	tester := ui.NewTester(func(c *ui.Context) {
		view.focus.begin(c)
		p := view.context(state.Location, true, 1920, 1080)
		p.playbackFeedbackScreen(c, playbackFeedbackBuffering, pbState, "")
	}, 1920, 1080)
	tester.Frame()
	wantNormal := theme.Over(view.Theme.Colors.Accent.Soft.WithAlpha(0xe6), view.Theme.Colors.Surface.Canvas)
	if !hasColor(tester.Image(), topRight, wantNormal) {
		t.Fatal("top-right wifi speed indicator did not render with Accent.Soft during normal buffering")
	}

	// Simulate prolonged stall -> warning amber color in top-right.
	view.player.speedSince = view.player.speedSince.Add(-3 * time.Second)
	view.player.speedSampleAt = time.Time{}
	pbState.BytesPerSecond = 0
	tester.Frame()
	wantWarn := theme.Over(view.Theme.Colors.Status.Warning.Foreground.WithAlpha(0xe6), view.Theme.Colors.Surface.Canvas)
	if !hasColor(tester.Image(), topRight, wantWarn) {
		t.Fatal("top-right wifi speed indicator did not switch to Warning.Foreground during stall")
	}
}

func TestPlaybackQualityMenuAutoVsLockedAndPreloadNextTrigger(t *testing.T) {
	assets, err := LoadAssets()
	if err != nil {
		t.Fatal(err)
	}
	pages, err := NewPages(assets)
	if err != nil {
		t.Fatal(err)
	}
	entries := testEntries()
	entries[0].Capabilities = append(entries[0].Capabilities, provider.GetCatalogItem, provider.ResolvePlayback)
	state, err := app.NewState(entries, entries[0].ID, pages.Routes)
	if err != nil {
		t.Fatal(err)
	}
	deliveries := make(chan func(), 8)
	deliver := func() {
		t.Helper()
		select {
		case fn := <-deliveries:
			fn()
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for playback delivery")
		}
	}
	source := &autoNextPlaybackSession{plans: make(chan provider.PlaybackPlan, 8)}
	clock := player.Status{HasFrame: true, Duration: 100, Position: 10}
	view := newTestView(t, Options{State: state, Pages: pages, Assets: assets, Theme: theme.Default()})
	catalogEvents := make(chan func(), 8)
	view.Catalog = app.NewCatalog(func(string) provider.Session {
		return source
	}, func(fn func()) { catalogEvents <- fn }, func() {})
	view.Catalog.Select(entries[0])
	select {
	case fn := <-catalogEvents:
		fn()
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for catalog browse")
	}
	view.Playback = app.NewPlayback(view.Catalog, player.Engine{
		Open:     func(string, float64) bool { return true },
		Snapshot: func() player.Status { return clock },
		SetRate:  func(float64) {},
	}, nil, func(fn func()) { deliveries <- fn }, func() {})
	defer view.Playback.Close()

	plan4K := provider.PlaybackPlan{
		PartID: "p1", Quality: "120", Duration: 100, Access: provider.VideoAccessGranted,
		Qualities: []provider.PlaybackQuality{
			{ID: "120", Label: "4K", Badge: "4K", Bandwidth: 12_000_000},
			{ID: "80", Label: "1080P", Bandwidth: 4_000_000},
		},
	}
	plan1080 := plan4K
	plan1080.Quality = "80"
	planEp2 := provider.PlaybackPlan{PartID: "p2", Quality: "120", Duration: 100, Access: provider.VideoAccessGranted}

	ref := provider.MediaRef{EntryID: entries[0].ID, Kind: provider.Video, ID: "BV1qual"}
	item := provider.MediaItem{
		Ref: ref, Title: "Quality Test",
		VideoDetails: &provider.VideoDetails{
			SegmentKind: provider.VideoParts,
			Segments: []provider.VideoSegment{
				{PartID: "p1", Title: "Episode 1"},
				{PartID: "p2", Title: "Episode 2"},
			},
		},
	}
	source.plans <- plan4K
	view.context(state.Location, true, 1920, 1080).startDetailPlayback(item, nil)
	deliver()

	// Auto mode: openPanel focuses Auto ("player/quality/").
	view.player.openPanel(view, playerQuality)
	if got := view.State.Focus; got != playbackOptionID(playerQuality, "") {
		t.Fatalf("Auto quality panel focus = %q, want %q", got, playbackOptionID(playerQuality, ""))
	}
	tester := ui.NewTester(func(c *ui.Context) {
		view.focus.begin(c)
		p := view.context(state.Location, true, 1920, 1080)
		playbackPage{}.Render(c, p)
	}, 1920, 1080)
	tester.Frame()
	if !tester.Focused(assets.Text.Playback.Auto) {
		t.Fatalf("expected Auto option %q to be focused in Auto mode", assets.Text.Playback.Auto)
	}

	// Lock concrete 1080P ("80"): openPanel focuses "80".
	source.plans <- plan1080
	view.Playback.ChooseQuality("80")
	deliver()
	view.player.openPanel(view, playerQuality)
	if got := view.State.Focus; got != playbackOptionID(playerQuality, "80") {
		t.Fatalf("Locked quality panel focus = %q, want %q", got, playbackOptionID(playerQuality, "80"))
	}
	tester.Frame()
	if !tester.Focused("1080P") {
		t.Fatal("expected 1080P option to be focused in Locked mode")
	}
	view.player.closePanel(view)

	// Near end of Episode 1 (15s remaining), Render triggers PreloadNext for Episode 2.
	source.plans <- planEp2
	clock.Position = 85
	tester.Frame()
	deliver() // delivers PreloadNext completion for p2

	// When Episode 1 finishes naturally, auto-advance consumes preloaded p2 synchronously with no extra resolve.
	clock.Ended, clock.Position = true, 100
	tester.Frame()
	if view.player.index != 1 || view.Playback.State().Target.PartID != "p2" || view.Playback.State().Loading {
		t.Fatalf("auto-advance did not immediately consume preloaded episode 2: index=%d state=%+v",
			view.player.index, view.Playback.State())
	}
}
