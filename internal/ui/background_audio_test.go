package tvui

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"io"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/player"
	"github.com/yuchanns/MikuTV/internal/provider"
	"github.com/yuchanns/MikuTV/internal/theme"
)

type bgAudioTestSession struct {
	audioOnlyByID map[string]bool
	resolveCount  atomic.Int32
	coverImage    image.Image
}

func (s *bgAudioTestSession) Close() {}

func (s *bgAudioTestSession) Browse(context.Context, provider.BrowseRequest) (provider.CollectionPage, error) {
	return provider.CollectionPage{
		Collections: []provider.Collection{{
			ID: "featured",
			Items: []provider.MediaItem{{
				Ref:      provider.MediaRef{EntryID: "demo", Kind: provider.Video, ID: "hero-vid"},
				Title:    "Hero Feature",
				CoverURL: "mock://hero",
			}},
		}},
	}, nil
}

func (s *bgAudioTestSession) Item(context.Context, provider.MediaRef) (provider.MediaItem, error) {
	return provider.MediaItem{}, nil
}

func (s *bgAudioTestSession) LoadCatalogArtwork(context.Context, string) (image.Image, error) {
	return s.coverImage, nil
}

func (s *bgAudioTestSession) ResolvePlayback(_ context.Context, req provider.PlaybackRequest) (provider.Playback, error) {
	s.resolveCount.Add(1)
	stream := provider.PlaybackStream{Format: provider.PlaybackDirect}
	if s.audioOnlyByID[req.Ref.ID] {
		stream.Audio = &provider.PlaybackTrack{
			ContentType: "audio/mpeg",
			Resource:    provider.PlaybackResource{URLs: []string{"https://cdn.example/" + req.Ref.ID + ".mp3"}},
		}
	} else {
		stream.Video = provider.PlaybackTrack{
			ContentType: "video/mp4",
			Resource:    provider.PlaybackResource{URLs: []string{"https://cdn.example/" + req.Ref.ID + ".mp4"}},
		}
	}
	return provider.Playback{
		Plan: provider.PlaybackPlan{
			Duration: 180,
			Streams:  []provider.PlaybackStream{stream},
		},
		Transport: bgAudioMockTransport{},
	}, nil
}

type bgAudioMockTransport struct{}

func (bgAudioMockTransport) OpenResource(context.Context, provider.PlaybackResource, string, string) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"audio/mpeg"}},
		Body:       io.NopCloser(bytes.NewReader([]byte("ID3"))),
	}, nil
}

func TestBackgroundAudioListeningAndRailNowPlaying(t *testing.T) {
	assets, err := LoadAssets()
	if err != nil {
		t.Fatal(err)
	}
	pages, err := NewPages(assets)
	if err != nil {
		t.Fatal(err)
	}
	entries := []app.Entry{
		{
			ID: "demo", SourceID: "demo", Name: "Demo",
			Capabilities: provider.Capabilities{provider.BrowseCatalog, provider.ResolvePlayback, provider.PlaybackModes},
		},
		{
			ID: "other", SourceID: "other", Name: "Other",
			Capabilities: provider.Capabilities{provider.BrowseCatalog, provider.ResolvePlayback},
		},
	}
	state, err := app.NewState(entries, "demo", pages.Routes)
	if err != nil {
		t.Fatal(err)
	}

	sess := &bgAudioTestSession{
		audioOnlyByID: map[string]bool{"track-1": true, "track-2": true, "vid-1": false},
		coverImage:    solidTestCover(color.NRGBA{40, 150, 145, 255}),
	}
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
	catalog.Open(entries[0])

	var fullscreenLog []bool
	var lastNowPlaying player.NowPlayingInfo
	var remoteHandlers player.RemoteHandlers
	clock := player.Status{HasFrame: true, AudioOnly: true, Position: 12, Duration: 180}

	pb := app.NewPlayback(catalog, player.Engine{
		Open:              func(string, float64) bool { return true },
		SetRate:           func(float64) {},
		Stop:              func() {},
		Snapshot:          func() player.Status { return clock },
		Fullscreen:        func(active bool) { fullscreenLog = append(fullscreenLog, active) },
		SetNowPlaying:     func(info player.NowPlayingInfo) { lastNowPlaying = info },
		SetRemoteHandlers: func(h player.RemoteHandlers) { remoteHandlers = h },
	}, nil, func(fn func()) { queue <- fn }, func() {})

	prefs := app.NewPlaybackSettings(nil, nil)
	view := newTestView(t, Options{
		State:            state,
		Catalog:          catalog,
		PlaybackSettings: prefs,
		Pages:            pages,
		Assets:           assets,
		Theme:            theme.Default(),
	})
	view.Playback = pb
	pb.SetPreferences(prefs)
	pb.SetRemoteHandlers(view.RemoteHandlers(nil, nil))
	view.SyncEntry()
	t.Cleanup(func() {
		pb.Close()
		catalog.Close()
	})

	tester := ui.NewTester(view.Render, 1920, 1080)
	pump := func(n int) {
		for range n {
			time.Sleep(20 * time.Millisecond)
			drain()
			tester.Frame()
		}
	}

	// Open a 2-track audio playlist.
	page := view.context(state.Location, true, 1920, 1080)
	tracks := []playbackItem{
		{item: provider.MediaItem{
			Ref:      provider.MediaRef{EntryID: "demo", Kind: provider.Video, ID: "track-1"},
			Title:    "Nocturne in Blue",
			Category: "Midnight Press",
			CoverURL: "mock://track-1",
			Creator:  &provider.Creator{ID: "a1", Name: "Studio Trio"},
		}},
		{item: provider.MediaItem{
			Ref:      provider.MediaRef{EntryID: "demo", Kind: provider.Video, ID: "track-2"},
			Title:    "Rainlight Prelude",
			Category: "Morning Tape",
			CoverURL: "mock://track-2",
			Creator:  &provider.Creator{ID: "a2", Name: "Ensemble Two"},
		}},
	}
	view.dispatch(func() { page.openPlayback(tracks, 0, true, nil) })
	pump(6)

	if !pb.State().AudioOnly || !pb.Active() {
		t.Fatalf("expected active audio-only playback, got active=%v audioOnly=%v", pb.Active(), pb.State().AudioOnly)
	}
	if !lastNowPlaying.Active || lastNowPlaying.Title != "Nocturne in Blue" || lastNowPlaying.Artist != "Studio Trio" || len(lastNowPlaying.Artwork) == 0 {
		t.Fatalf("expected NowPlaying metadata and artwork synced, got %+v (art=%d)", lastNowPlaying, len(lastNowPlaying.Artwork))
	}

	// 1st Back hides HUD; 2nd Back leaves Player while keeping audio active in background!
	if !view.goBack() {
		t.Fatal("1st Back should hide HUD")
	}
	if state.Page != app.Player || view.player.visible {
		t.Fatalf("expected still on Player with hidden HUD, got page=%s visible=%v", state.Page, view.player.visible)
	}
	if !view.goBack() {
		t.Fatal("2nd Back should leave Player into background listening")
	}
	if state.Page != app.Home {
		t.Fatalf("expected return to Home, got %s", state.Page)
	}
	if !pb.Active() || !view.HasBackgroundAudio() {
		t.Fatalf("expected background audio active after leaving Player, active=%v bg=%v", pb.Active(), view.HasBackgroundAudio())
	}
	if len(fullscreenLog) == 0 || fullscreenLog[len(fullscreenLog)-1] != false {
		t.Fatalf("expected Fullscreen(false) on soft-leave, got %v", fullscreenLog)
	}

	// Render Home (collapsed + expanded rail): Now Playing rail button must be bound,
	// Home Hero inline preview must stay suppressed while background audio is active.
	pump(3)
	if view.focus.item(railNowPlaying) == nil {
		t.Fatal("expected rail/now-playing button registered on Home rail")
	}
	if view.Playback.InlinePreviewState().Active || view.homePreviewShowing(state.Layers()) {
		t.Fatal("Home Hero inline preview must be suppressed while background audio is playing")
	}

	// Expanding rail and scrolling onto rail/now-playing must NOT navigate away.
	state.Focus, state.Area, view.reveal = railNowPlaying, app.NavigationArea, true
	view.selectRailFocus(railNowPlaying)
	if state.Page != app.Home {
		t.Fatalf("scrolling over rail/now-playing must not navigate, got %s", state.Page)
	}
	tester.Frame()
	if tester.Image() == nil {
		t.Fatal("expected rendered Home frame with focused rail/now-playing")
	}

	// Light theme renders focused rail/now-playing cleanly too.
	view.SetTheme(theme.DefaultLight())
	tester.Frame()
	if tester.Image() == nil {
		t.Fatal("expected rendered Light theme Home frame with rail/now-playing")
	}
	view.SetTheme(theme.Default())

	// Pressing KeySpace (tvOS Siri Remote Play/Pause) toggles background playback cleanly.
	state.Focus, state.Area, view.reveal = homePlayAction, app.ContentArea, true
	tester.Frame()
	if !pb.State().Playing {
		t.Fatal("expected audio playing before Play/Pause press")
	}
	tester.Key(0, ui.KeySpace)
	drain()
	tester.Frame()
	if pb.State().Playing || state.Page != app.Home {
		t.Fatalf("expected KeySpace to pause background audio without leaving Home, playing=%v page=%s", pb.State().Playing, state.Page)
	}

	// Remote Command Center Play/Next/Previous/Seek work while in background.
	remoteHandlers.Play()
	if !pb.State().Playing {
		t.Fatal("expected RemoteHandlers.Play to resume playback")
	}
	remoteHandlers.Seek(45)
	if pb.State().Position != 45 {
		t.Fatalf("expected RemoteHandlers.Seek(45), got %.1f", pb.State().Position)
	}

	// Track 1 ends in background -> auto-advances to Track 2 and updates NowPlaying.
	clock.Ended = true
	pump(6)
	clock.Ended = false
	clock.Position = 3
	pump(4)
	if view.player.index != 1 || pb.State().Target.Ref.ID != "track-2" {
		t.Fatalf("expected background auto-advance to track-2, got idx=%d ref=%s", view.player.index, pb.State().Target.Ref.ID)
	}
	if lastNowPlaying.Title != "Rainlight Prelude" {
		t.Fatalf("expected NowPlaying title updated to Rainlight Prelude, got %+v", lastNowPlaying)
	}

	// Clicking rail/now-playing resumes fullscreen Player with ZERO re-resolve!
	resolvesBefore := sess.resolveCount.Load()
	view.dispatch(func() { view.ResumeBackgroundAudio() })
	tester.Frame()
	if state.Page != app.Player || view.HasBackgroundAudio() {
		t.Fatalf("expected seamless return to Player, page=%s bg=%v", state.Page, view.HasBackgroundAudio())
	}
	if sess.resolveCount.Load() != resolvesBefore {
		t.Fatalf("ResumeBackgroundAudio must not re-resolve stream: before=%d after=%d", resolvesBefore, sess.resolveCount.Load())
	}
	if len(fullscreenLog) == 0 || fullscreenLog[len(fullscreenLog)-1] != true {
		t.Fatalf("expected Fullscreen(true) on resume, got %v", fullscreenLog)
	}

	// Leave to background again, then opening a regular video hard-closes background audio
	// and transitions to video playback cleanly with Fullscreen(true).
	view.goBack() // hide HUD
	view.goBack() // leave to Home in background
	if !view.HasBackgroundAudio() {
		t.Fatal("expected background audio active before starting new video")
	}
	clock.AudioOnly = false
	page = view.context(state.Location, true, 1920, 1080)
	view.dispatch(func() {
		page.openPlayback([]playbackItem{{item: provider.MediaItem{
			Ref:   provider.MediaRef{EntryID: "demo", Kind: provider.Video, ID: "vid-1"},
			Title: "Regular Video",
		}}}, 0, false, nil)
	})
	pump(6)
	if pb.State().AudioOnly || !pb.Active() {
		t.Fatalf("expected active video-only playback, active=%v audioOnly=%v", pb.Active(), pb.State().AudioOnly)
	}
	if len(fullscreenLog) == 0 || fullscreenLog[len(fullscreenLog)-1] != true {
		t.Fatalf("expected Fullscreen(true) when replacing background audio with video, got %v", fullscreenLog)
	}
	// Regression check: leaving video playback MUST still hard-close immediately (no background mode).
	view.goBack() // hide HUD
	view.goBack() // leave Player
	if pb.Active() || view.HasBackgroundAudio() {
		t.Fatalf("video playback must hard-close on Back, active=%v bg=%v", pb.Active(), view.HasBackgroundAudio())
	}

	// Verify entry switch also hard-closes background audio and clears NowPlaying.
	clock.AudioOnly = true
	page = view.context(state.Location, true, 1920, 1080)
	view.dispatch(func() { page.openPlayback(tracks, 0, true, nil) })
	pump(6)
	view.goBack()
	view.goBack()
	if !view.HasBackgroundAudio() {
		t.Fatal("expected background audio active before entry switch")
	}
	state.SelectEntry("other")
	view.SyncEntry()
	if pb.Active() || view.HasBackgroundAudio() {
		t.Fatalf("expected hard-close on entry switch, active=%v bg=%v", pb.Active(), view.HasBackgroundAudio())
	}
	if lastNowPlaying.Active {
		t.Fatal("expected NowPlaying cleared on close")
	}

	// Mixed playlist auto-advancing from audio -> video in background must cleanly
	// close the incompatible video session instead of stranding an active non-fullscreen player.
	state.SelectEntry("demo")
	view.SyncEntry()
	clock = player.Status{HasFrame: true, AudioOnly: true, Position: 10, Duration: 180}
	mixed := []playbackItem{
		tracks[0],
		{item: provider.MediaItem{
			Ref:   provider.MediaRef{EntryID: "demo", Kind: provider.Video, ID: "vid-1"},
			Title: "Mixed Video Item",
		}},
	}
	page = view.context(state.Location, true, 1920, 1080)
	view.dispatch(func() { page.openPlayback(mixed, 0, true, nil) })
	pump(6)
	view.goBack() // hide HUD
	view.goBack() // enter background listening on Home
	if !view.HasBackgroundAudio() {
		t.Fatal("expected background audio active before mixed queue advance")
	}
	clock.Ended = true
	pump(3)
	clock.Ended = false
	clock.AudioOnly = false
	pump(6)
	if pb.Active() || view.HasBackgroundAudio() {
		t.Fatalf("mixed background advance into video must close cleanly, active=%v bg=%v", pb.Active(), view.HasBackgroundAudio())
	}
}
