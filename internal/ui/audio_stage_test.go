package tvui

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/player"
	"github.com/yuchanns/MikuTV/internal/provider"
	"github.com/yuchanns/MikuTV/internal/theme"
)

type audioStageTestSession struct {
	audioOnly  bool
	subtitles  []provider.PlaybackTrack
	cues       []provider.SubtitleCue
	coverImage image.Image
}

func (s *audioStageTestSession) Close() {}

func (s *audioStageTestSession) Browse(context.Context, provider.BrowseRequest) (provider.CollectionPage, error) {
	return provider.CollectionPage{}, nil
}

func (s *audioStageTestSession) Item(context.Context, provider.MediaRef) (provider.MediaItem, error) {
	return provider.MediaItem{}, nil
}

func (s *audioStageTestSession) LoadCatalogArtwork(context.Context, string) (image.Image, error) {
	return s.coverImage, nil
}

func (s *audioStageTestSession) ResolvePlayback(context.Context, provider.PlaybackRequest) (provider.Playback, error) {
	stream := provider.PlaybackStream{Format: provider.PlaybackDirect}
	if s.audioOnly {
		stream.Audio = &provider.PlaybackTrack{
			ContentType: "audio/mpeg",
			Resource:    provider.PlaybackResource{URLs: []string{"https://cdn.example/song.mp3"}},
		}
	} else {
		stream.Video = provider.PlaybackTrack{
			ContentType: "video/mp4",
			Resource:    provider.PlaybackResource{URLs: []string{"https://cdn.example/mv.mp4"}},
		}
	}
	return provider.Playback{
		Plan: provider.PlaybackPlan{
			Duration:  240,
			Subtitles: s.subtitles,
			Streams:   []provider.PlaybackStream{stream},
		},
		Transport: audioStageMockTransport{},
	}, nil
}

func (s *audioStageTestSession) PlaybackSubtitles(context.Context, provider.PlaybackSubtitleQuery) ([]provider.SubtitleCue, error) {
	return s.cues, nil
}

type audioStageMockTransport struct{}

func (audioStageMockTransport) OpenResource(context.Context, provider.PlaybackResource, string, string) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"audio/mpeg"}},
		Body:       io.NopCloser(bytes.NewReader([]byte("ID3"))),
	}, nil
}

func solidTestCover(col color.NRGBA) image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			img.SetNRGBA(x, y, col)
		}
	}
	return img
}

func TestAudioStagePlanALyricsNoLyricsLightAndOLEDPauseDim(t *testing.T) {
	assets, err := LoadAssets()
	if err != nil {
		t.Fatal(err)
	}
	pages, err := NewPages(assets)
	if err != nil {
		t.Fatal(err)
	}

	buildEnv := func(th theme.Theme, sess *audioStageTestSession) (*View, *app.State, *player.Status, func()) {
		entries := []app.Entry{{
			ID: "demo", SourceID: "demo", Name: "Demo",
			Capabilities: provider.Capabilities{provider.BrowseCatalog, provider.ResolvePlayback, provider.ReadSubtitles},
		}}
		state, err := app.NewState(entries, "demo", pages.Routes)
		if err != nil {
			t.Fatal(err)
		}
		queue := make(chan func(), 32)
		drain := func() {
			for {
				select {
				case fn := <-queue:
					fn()
				default:
					return
				}
			}
		}
		catalog := app.NewCatalog(func(string) provider.Session { return sess }, func(fn func()) { queue <- fn }, func() {})
		catalog.Open(entries[0])

		clock := &player.Status{HasFrame: true, AudioOnly: sess.audioOnly, Position: 5.0, Duration: 240}
		pb := app.NewPlayback(catalog, player.Engine{
			Open:     func(string, float64) bool { return true },
			SetRate:  func(float64) {},
			Stop:     func() {},
			Snapshot: func() player.Status { return *clock },
		}, nil, func(fn func()) { queue <- fn }, func() {})

		prefs := app.NewPlaybackSettings(nil, nil) // Default Subtitle pref is Off!
		view := newTestView(t, Options{
			State:            state,
			Catalog:          catalog,
			PlaybackSettings: prefs,
			Pages:            pages,
			Assets:           assets,
			Theme:            th,
		})
		view.Playback = pb
		t.Cleanup(func() {
			pb.Close()
			catalog.Close()
		})
		return view, state, clock, drain
	}

	cues := []provider.SubtitleCue{
		{Start: 0, End: 4, Text: "Line 1\n第一句"},
		{Start: 4, End: 9, Text: "Active Lyric\n当前双语歌词"},
		{Start: 9, End: 15, Text: "Line 3\n第三句"},
	}

	// 1. Dark theme with bilingual lyrics auto-selected even when Subtitle pref is Off, plus OLED pause dim opacity safety.
	{
		sess := &audioStageTestSession{
			audioOnly:  true,
			subtitles:  []provider.PlaybackTrack{{ID: "lrc", Label: "Lyrics", Default: true}},
			cues:       cues,
			coverImage: solidTestCover(color.NRGBA{32, 110, 150, 255}),
		}
		view, state, _, drain := buildEnv(theme.Default(), sess)
		state.Push(app.Route{Page: app.Player})
		view.player = playbackView{
			items: []playbackItem{{item: provider.MediaItem{
				Ref:      provider.MediaRef{EntryID: "demo", Kind: provider.Video, ID: "s1"},
				Title:    "Nocturne in Blue",
				Category: "Midnight Press",
				CoverURL: "mock://cover",
				Creator:  &provider.Creator{ID: "c1", Name: "Studio Trio"},
			}}},
			visible: false,
		}
		view.startPlaybackItem(0, nil)
		view.player.visible = false
		tester := ui.NewTester(view.Render, 1920, 1080)
		for range 8 {
			time.Sleep(25 * time.Millisecond)
			drain()
			tester.Frame()
		}
		if view.player.content.subtitle != "lrc" || len(view.player.content.cues) != 3 {
			t.Fatalf("expected auto-selected lyrics track, got id=%q cues=%d", view.player.content.subtitle, len(view.player.content.cues))
		}
		darkImg := tester.Image()
		if darkImg == nil {
			t.Fatal("expected non-nil rendered frame")
		}
		// Showing the HUD must overlay on top without shrinking or shifting the stage cover.
		titleHidden, okHidden := tester.Find("Nocturne in Blue")
		if !okHidden {
			t.Fatal("expected song title to render on audio stage when HUD hidden")
		}
		// Stage layout must stay stationary over time (no foreground pixel drift).
		tester.Advance(15 * time.Second)
		tester.Frame()
		titleLater, okLater := tester.Find("Nocturne in Blue")
		if !okLater || titleLater.X != titleHidden.X || titleLater.Y != titleHidden.Y {
			t.Fatalf("expected stationary audio stage without drift, before=%+v after=%+v", titleHidden, titleLater)
		}
		view.player.visible = true
		tester.Frame()
		tester.Advance(300 * time.Millisecond)
		tester.Frame()
		// Audio-only player HUD must present audio-native labels (Track Info / Lyrics / Audio Quality / Prev/Next Track).
		if !tester.HasText(assets.Text.Playback.AudioInfo) {
			t.Fatalf("expected audio info label %q when AudioOnly=true", assets.Text.Playback.AudioInfo)
		}
		// Compare center of the 490x490 album cover (x=385, y=460, well above bottom scrim)
		// across HUD hidden vs visible (and with right drawer open) to prove 0 resize/shift.
		visImg := tester.Image()
		if darkImg.RGBAAt(385, 460) != visImg.RGBAAt(385, 460) {
			t.Fatalf("album cover center pixel shifted when HUD appeared: hidden=%v visible=%v titleHidden=%+v",
				darkImg.RGBAAt(385, 460), visImg.RGBAAt(385, 460), titleHidden)
		}
		view.player.panel = playerInfo
		tester.Frame()
		drawerImg := tester.Image()
		if darkImg.RGBAAt(385, 460) != drawerImg.RGBAAt(385, 460) {
			t.Fatalf("album cover shifted/resized when right drawer opened in audio mode: hidden=%v drawer=%v",
				darkImg.RGBAAt(385, 460), drawerImg.RGBAAt(385, 460))
		}
		view.player.panel = ""
		view.player.visible = false
		tester.Frame()
		tester.Advance(300 * time.Millisecond)
		tester.Frame()

		// Pause for 5+ minutes -> AudioDimmed=true; outer corner must remain opaque dark canvas
		// (no transparent bleed!) and strictly dimmer than the undimmed ambient wash.
		undimmedCorner := darkImg.RGBAAt(2, 2)
		view.Playback.Toggle()
		past := time.Now().Add(-6 * time.Minute)
		view.Playback.Tick(past)
		view.Playback.Tick(time.Now())
		if !view.Playback.State().AudioDimmed {
			t.Fatal("expected AudioDimmed=true after 5 minutes paused")
		}
		tester.Frame()
		dimmedImg := tester.Image()
		corner := dimmedImg.RGBAAt(2, 2)
		if corner.A != 255 || corner.B >= undimmedCorner.B {
			t.Fatalf("expected opaque dimmed corner darker than %+v, got %+v", undimmedCorner, corner)
		}
	}

	// 2. Light theme with bilingual lyrics renders bright MikuTV Light canvas.
	{
		sess := &audioStageTestSession{
			audioOnly:  true,
			subtitles:  []provider.PlaybackTrack{{ID: "lrc", Label: "Lyrics", Default: true}},
			cues:       cues,
			coverImage: solidTestCover(color.NRGBA{60, 140, 130, 255}),
		}
		view, state, _, drain := buildEnv(theme.DefaultLight(), sess)
		state.Push(app.Route{Page: app.Player})
		view.player = playbackView{
			items: []playbackItem{{item: provider.MediaItem{
				Ref:      provider.MediaRef{EntryID: "demo", Kind: provider.Video, ID: "s2"},
				Title:    "Rainlight Prelude",
				CoverURL: "mock://cover",
			}}},
			visible: false,
		}
		view.startPlaybackItem(0, nil)
		view.player.visible = false
		tester := ui.NewTester(view.Render, 1920, 1080)
		for range 6 {
			time.Sleep(25 * time.Millisecond)
			drain()
			tester.Frame()
		}
		corner := tester.Image().RGBAAt(10, 10)
		if corner.R < 200 || corner.G < 205 || corner.B < 210 {
			t.Fatalf("expected MikuTV Light bright canvas, got corner %+v", corner)
		}
	}

	// 3. No lyrics track -> cleanly renders centered pure-artwork Plan A stage without panicking.
	{
		sess := &audioStageTestSession{
			audioOnly:  true,
			coverImage: solidTestCover(color.NRGBA{180, 130, 80, 255}),
		}
		view, state, _, drain := buildEnv(theme.Default(), sess)
		state.Push(app.Route{Page: app.Player})
		view.player = playbackView{
			items: []playbackItem{{item: provider.MediaItem{
				Ref:      provider.MediaRef{EntryID: "demo", Kind: provider.Video, ID: "s3"},
				Title:    "Solitude (Instrumental)",
				CoverURL: "mock://cover",
			}}},
			visible: false,
		}
		view.startPlaybackItem(0, nil)
		view.player.visible = false
		tester := ui.NewTester(view.Render, 1920, 1080)
		for range 5 {
			time.Sleep(25 * time.Millisecond)
			drain()
			tester.Frame()
		}
		if len(view.player.content.cues) != 0 {
			t.Fatalf("expected 0 cues for instrumental, got %d", len(view.player.content.cues))
		}
		if tester.Image() == nil {
			t.Fatal("expected rendered centered artwork frame")
		}
	}

	// 4. Switching media kind Video -> Audio -> Video reconciles subtitle selection
	// (loads lyrics on switch to Audio even with Subtitle=Off, clears stale lyrics on switch back to Video).
	{
		sess := &audioStageSwitchSession{
			coverImage: solidTestCover(color.NRGBA{40, 120, 150, 255}),
			videoSubs:  []provider.PlaybackTrack{{ID: "cc-en", Label: "English CC", Language: "en"}},
			audioSubs:  []provider.PlaybackTrack{{ID: "lrc", Label: "Lyrics", Default: true}},
			videoCues:  []provider.SubtitleCue{{Start: 0, End: 5, Text: "Video caption"}},
			audioCues:  cues,
		}
		entries := []app.Entry{{
			ID: "demo", SourceID: "demo", Name: "Demo",
			Capabilities: provider.Capabilities{provider.BrowseCatalog, provider.ResolvePlayback, provider.ReadSubtitles},
		}}
		state, err := app.NewState(entries, "demo", pages.Routes)
		if err != nil {
			t.Fatal(err)
		}
		queue := make(chan func(), 32)
		drain := func() {
			for {
				select {
				case fn := <-queue:
					fn()
				default:
					return
				}
			}
		}
		catalog := app.NewCatalog(func(string) provider.Session { return sess }, func(fn func()) { queue <- fn }, func() {})
		catalog.Open(entries[0])
		clock := &player.Status{HasFrame: true, Position: 5.0, Duration: 240}
		pb := app.NewPlayback(catalog, player.Engine{
			Open:         func(string, float64) bool { return true },
			SetRate:      func(float64) {},
			SetAudioOnly: func(a bool) { clock.AudioOnly = a },
			Stop:         func() {},
			Snapshot:     func() player.Status { return *clock },
		}, nil, func(fn func()) { queue <- fn }, func() {})
		prefs := app.NewPlaybackSettings(nil, nil) // Default Subtitle=Off
		view := newTestView(t, Options{
			State:            state,
			Catalog:          catalog,
			PlaybackSettings: prefs,
			Pages:            pages,
			Assets:           assets,
			Theme:            theme.Default(),
		})
		view.Playback = pb
		view.SyncEntry()
		t.Cleanup(func() {
			pb.Close()
			catalog.Close()
		})

		state.Push(app.Route{Page: app.Player})
		view.player = playbackView{
			items: []playbackItem{{item: provider.MediaItem{
				Ref:      provider.MediaRef{EntryID: "demo", Kind: provider.Video, ID: "mv-1"},
				Title:    "Dual Mode Song",
				CoverURL: "mock://cover",
			}}},
			visible: false,
		}
		view.startPlaybackItem(0, nil)
		view.player.visible = false
		tester := ui.NewTester(view.Render, 1920, 1080)
		for range 6 {
			time.Sleep(20 * time.Millisecond)
			drain()
			tester.Frame()
		}
		if pb.State().AudioOnly || view.player.content.subtitle != "" {
			t.Fatalf("initial video mode should respect Subtitle=Off: audioOnly=%v sub=%q", pb.State().AudioOnly, view.player.content.subtitle)
		}

		// Switch Video -> Audio: must clear previous subtitleChosen and auto-load lyrics track!
		if !pb.SwitchMediaKind(provider.PlaybackMediaAudio) {
			t.Fatal("SwitchMediaKind(audio) failed")
		}
		for range 8 {
			time.Sleep(20 * time.Millisecond)
			drain()
			tester.Frame()
		}
		if !pb.State().AudioOnly || view.player.content.subtitle != "lrc" || len(view.player.content.cues) != 3 {
			t.Fatalf("expected lyrics auto-loaded after Video->Audio switch: audioOnly=%v sub=%q cues=%d",
				pb.State().AudioOnly, view.player.content.subtitle, len(view.player.content.cues))
		}

		// Switch Audio -> Video: must clear stale lyrics cues and respect Video Subtitle=Off!
		if !pb.SwitchMediaKind(provider.PlaybackMediaVideo) {
			t.Fatal("SwitchMediaKind(video) failed")
		}
		for range 8 {
			time.Sleep(20 * time.Millisecond)
			drain()
			tester.Frame()
		}
		if pb.State().AudioOnly || view.player.content.subtitle != "" || len(view.player.content.cues) != 0 {
			t.Fatalf("expected stale lyrics cleared after Audio->Video switch: audioOnly=%v sub=%q cues=%d",
				pb.State().AudioOnly, view.player.content.subtitle, len(view.player.content.cues))
		}
	}
}

type audioStageSwitchSession struct {
	coverImage image.Image
	videoSubs  []provider.PlaybackTrack
	audioSubs  []provider.PlaybackTrack
	videoCues  []provider.SubtitleCue
	audioCues  []provider.SubtitleCue
}

func (s *audioStageSwitchSession) Close() {}
func (s *audioStageSwitchSession) Browse(context.Context, provider.BrowseRequest) (provider.CollectionPage, error) {
	return provider.CollectionPage{}, nil
}
func (s *audioStageSwitchSession) Item(context.Context, provider.MediaRef) (provider.MediaItem, error) {
	return provider.MediaItem{}, nil
}
func (s *audioStageSwitchSession) LoadCatalogArtwork(context.Context, string) (image.Image, error) {
	return s.coverImage, nil
}
func (s *audioStageSwitchSession) ResolvePlayback(_ context.Context, req provider.PlaybackRequest) (provider.Playback, error) {
	if req.MediaKind == provider.PlaybackMediaAudio {
		return provider.Playback{
			Plan: provider.PlaybackPlan{
				Duration:       240,
				AvailableKinds: []string{provider.PlaybackMediaAudio, provider.PlaybackMediaVideo},
				Subtitles:      s.audioSubs,
				Streams: []provider.PlaybackStream{{
					Format: provider.PlaybackDirect,
					Audio: &provider.PlaybackTrack{
						ContentType: "audio/mpeg",
						Resource:    provider.PlaybackResource{URLs: []string{"https://cdn.example/song.mp3"}},
					},
				}},
			},
			Transport: audioStageMockTransport{},
		}, nil
	}
	return provider.Playback{
		Plan: provider.PlaybackPlan{
			Duration:       240,
			AvailableKinds: []string{provider.PlaybackMediaAudio, provider.PlaybackMediaVideo},
			Subtitles:      s.videoSubs,
			Streams: []provider.PlaybackStream{{
				Format: provider.PlaybackDirect,
				Video: provider.PlaybackTrack{
					ContentType: "video/mp4",
					Resource:    provider.PlaybackResource{URLs: []string{"https://cdn.example/mv.mp4"}},
				},
			}},
		},
		Transport: audioStageMockTransport{},
	}, nil
}
func (s *audioStageSwitchSession) PlaybackSubtitles(_ context.Context, q provider.PlaybackSubtitleQuery) ([]provider.SubtitleCue, error) {
	if q.Track.ID == "lrc" {
		return s.audioCues, nil
	}
	return s.videoCues, nil
}

func TestPlaybackDrawerVirtualizesLargePlaylistAndRelatedAutoplay(t *testing.T) {
	assets, err := LoadAssets()
	if err != nil {
		t.Fatal(err)
	}
	pages, err := NewPages(assets)
	if err != nil {
		t.Fatal(err)
	}
	entries := []app.Entry{{
		ID: "demo", SourceID: "demo", Name: "Demo",
		Capabilities: provider.Capabilities{provider.BrowseCatalog, provider.ResolvePlayback, provider.PlaybackModes},
	}}
	state, err := app.NewState(entries, "demo", pages.Routes)
	if err != nil {
		t.Fatal(err)
	}
	sess := &audioStageTestSession{audioOnly: true}
	queue := make(chan func(), 64)
	drain := func() {
		for {
			select {
			case fn := <-queue:
				fn()
			default:
				return
			}
		}
	}
	catalog := app.NewCatalog(func(string) provider.Session { return sess }, func(fn func()) { queue <- fn }, func() {})
	defer catalog.Close()
	catalog.Open(entries[0])

	clock := &player.Status{HasFrame: true, AudioOnly: true, Position: 10, Duration: 180}
	pb := app.NewPlayback(catalog, player.Engine{
		Open:        func(string, float64) bool { return true },
		SetRate:     func(float64) {},
		Stop:        func() {},
		SetAudioOnly: func(a bool) { clock.AudioOnly = a },
		Snapshot:    func() player.Status { return *clock },
	}, nil, func(fn func()) { queue <- fn }, func() {})
	defer pb.Close()

	view := newTestView(t, Options{State: state, Catalog: catalog, Pages: pages, Assets: assets, Theme: theme.Default()})
	view.Playback = pb
	state.Push(app.Route{Page: app.Player})

	// 1. Virtualized playlist drawer: 250 items must NOT mount all offscreen rows.
	items := make([]playbackItem, 250)
	for i := range items {
		items[i] = playbackItem{item: provider.MediaItem{
			Ref:   provider.MediaRef{EntryID: "demo", Kind: provider.Video, ID: fmt.Sprintf("track-%d", i)},
			Title: fmt.Sprintf("Track %d", i),
		}}
	}
	view.player = playbackView{items: items, keepQueue: true, visible: true}
	view.startPlaybackItem(120, nil)
	view.player.openPanel(view, playerInfo)
	tester := ui.NewTester(view.Render, 1920, 1080)
	drain()
	tester.Frame()
	if view.focus.item(playerEpisodeID(0)) == nil || view.focus.item(playerEpisodeID(120)) == nil || view.focus.item(playerEpisodeID(249)) == nil {
		t.Fatal("expected pinned head (0), active (120), and tail (249) playlist rows to be mounted")
	}
	if view.focus.item(playerEpisodeID(180)) != nil {
		t.Fatal("expected far-offscreen playlist row 180 to be virtualized (not mounted)")
	}

	// 2. Single-track autoplay continuation via VideoDetails.Related when track ends.
	relRef := provider.MediaRef{EntryID: "demo", Kind: provider.Video, ID: "related-next"}
	view.player = playbackView{
		items: []playbackItem{{item: provider.MediaItem{
			Ref:   provider.MediaRef{EntryID: "demo", Kind: provider.Video, ID: "seed-track"},
			Title: "Seed Track",
			VideoDetails: &provider.VideoDetails{
				Related: []provider.MediaItem{{Ref: relRef, Title: "Related Next"}},
			},
		}}},
		visible: false,
	}
	view.startPlaybackItem(0, nil)
	for range 5 {
		time.Sleep(15 * time.Millisecond)
		drain()
		tester.Frame()
	}
	clock.Ended = true
	pb.Tick(time.Now())
	tester.Frame()
	drain()
	if len(view.player.items) != 2 || view.player.index != 1 || view.player.items[1].item.Ref != relRef {
		t.Fatalf("expected single-track end to auto-continue via Related, got len=%d idx=%d items=%+v",
			len(view.player.items), view.player.index, view.player.items)
	}
}

func TestRelatedVideoDetailPreservesAndUpdatesPlaybackQueue(t *testing.T) {
	assets, err := LoadAssets()
	if err != nil {
		t.Fatal(err)
	}
	pages, err := NewPages(assets)
	if err != nil {
		t.Fatal(err)
	}
	entries := []app.Entry{{
		ID: "music", SourceID: "music", Name: "Music",
		Capabilities: provider.Capabilities{provider.BrowseCatalog, provider.GetCatalogItem, provider.ResolvePlayback, provider.PlaybackModes},
	}}
	state, err := app.NewState(entries, "music", pages.Routes)
	if err != nil {
		t.Fatal(err)
	}
	sess := &audioStageTestSession{audioOnly: true}
	catalog := app.NewCatalog(func(string) provider.Session { return sess }, func(fn func()) { fn() }, func() {})
	defer catalog.Close()
	catalog.Open(entries[0])
	pb := app.NewPlayback(catalog, player.Engine{
		Open:     func(string, float64) bool { return true },
		Stop:     func() {},
		Snapshot: func() player.Status { return player.Status{} },
	}, nil, func(fn func()) { fn() }, func() {})
	defer pb.Close()
	view := newTestView(t, Options{State: state, Catalog: catalog, Pages: pages, Assets: assets, Theme: theme.Default()})
	view.Playback = pb
	ctx := PageContext{host: view, State: state, Assets: assets, Theme: theme.Default()}

	songA := provider.MediaItem{Ref: provider.MediaRef{EntryID: "music", Kind: provider.Video, ID: "a"}, Title: "A"}
	songB := provider.MediaItem{Ref: provider.MediaRef{EntryID: "music", Kind: provider.Video, ID: "b"}, Title: "B"}
	rel1 := provider.MediaItem{Ref: provider.MediaRef{EntryID: "music", Kind: provider.Video, ID: "r1"}, Title: "R1"}
	rel2 := provider.MediaItem{Ref: provider.MediaRef{EntryID: "music", Kind: provider.Video, ID: "r2"}, Title: "R2"}

	// Hero/list queue builder includes all playable tracks centered on selected index.
	if q, idx := ctx.playbackQueueFromList(songB, []provider.MediaItem{songA, songB}, 1); len(q) != 2 || idx != 1 || q[idx].item.Ref != songB.Ref {
		t.Fatalf("playbackQueueFromList = %+v idx=%d", q, idx)
	}

	// Open songA with playlist queue [songA, songB]
	ctx.openMediaFromList(songA, []provider.MediaItem{songA, songB}, 0)
	if len(view.videoDetail.queue) != 2 || view.videoDetail.queueIndex != 0 || view.videoDetail.queue[0].item.Ref != songA.Ref {
		t.Fatalf("initial queue = %+v idx=%d", view.videoDetail.queue, view.videoDetail.queueIndex)
	}
	// Open rel2 from Related list [rel1, rel2] while on VideoDetail
	ctx.openMediaFromList(rel2, []provider.MediaItem{rel1, rel2}, 1)
	if len(view.videoDetail.queue) != 2 || view.videoDetail.queueIndex != 1 || view.videoDetail.queue[1].item.Ref != rel2.Ref {
		t.Fatalf("related queue = %+v idx=%d", view.videoDetail.queue, view.videoDetail.queueIndex)
	}
	// Back restores songA's original playlist queue [songA, songB]
	if !view.backFromVideoDetail() {
		t.Fatal("expected backFromVideoDetail to restore previous detail")
	}
	if len(view.videoDetail.queue) != 2 || view.videoDetail.queueIndex != 0 || view.videoDetail.queue[0].item.Ref != songA.Ref {
		t.Fatalf("restored queue = %+v idx=%d", view.videoDetail.queue, view.videoDetail.queueIndex)
	}

	// Non-removable rows in a removable section must not claim a header action button.
	if hasRemovableLibraryRows([]provider.AccountRow{{ID: "h1", Title: "Played"}}, false) {
		t.Fatal("expected rows without RemovalID not to advertise Manage header action")
	}
	if !hasRemovableLibraryRows([]provider.AccountRow{{ID: "h1", Title: "Played", RemovalID: "h1"}}, false) {
		t.Fatal("expected row with RemovalID to advertise Manage header action")
	}

	// Hero queue playback with keepQueue=true must keep featured queue even if plan returns Episodes.
	view.player = playbackView{
		items:     []playbackItem{{item: songA}, {item: songB}},
		index:     0,
		keepQueue: true,
	}
	view.player.updatePlan(view, app.PlaybackState{
		Revision: 1,
		Resolution: provider.Playback{
			Plan: provider.PlaybackPlan{
				PartID: "p1",
				Episodes: []provider.VideoSegment{
					{PartID: "p1", Title: "Ep 1"},
					{PartID: "p2", Title: "Ep 2"},
				},
			},
		},
	})
	if len(view.player.items) != 2 || view.player.items[1].item.Ref != songB.Ref {
		t.Fatalf("keepQueue=true Hero queue was overwritten by Episodes: %+v", view.player.items)
	}
}
