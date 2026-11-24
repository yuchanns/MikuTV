package app

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/yuchanns/MikuTV/internal/player"
	"github.com/yuchanns/MikuTV/internal/provider"
)

type playbackTestSession struct {
	requests chan provider.PlaybackRequest
}

func (s *playbackTestSession) Close() {}
func (s *playbackTestSession) Browse(context.Context, provider.BrowseRequest) (provider.CollectionPage, error) {
	return provider.CollectionPage{}, nil
}
func (s *playbackTestSession) Item(context.Context, provider.MediaRef) (provider.MediaItem, error) {
	return provider.MediaItem{}, nil
}

func (s *playbackTestSession) ResolvePlayback(ctx context.Context, request provider.PlaybackRequest) (provider.Playback, error) {
	s.requests <- request
	if request.Quality == "hdr" {
		return provider.Playback{Plan: provider.PlaybackPlan{Restriction: provider.PlaybackDevice}}, nil
	}
	return provider.Playback{
		Plan: provider.PlaybackPlan{
			PartID: request.PartID, Duration: 95, ResumeSeconds: 11,
			Streams: []provider.PlaybackStream{{Format: provider.PlaybackDirect}},
		},
		Transport: playbackTestTransport{},
	}, nil
}

type playbackTestTransport struct{}

func (playbackTestTransport) OpenResource(context.Context, provider.PlaybackResource, string, string) (*http.Response, error) {
	return nil, fmt.Errorf("the fake native engine doesn't request bytes")
}

type playbackTestStore struct {
	saved float64
}

func (s *playbackTestStore) PlaybackPosition(_ string, target provider.PlaybackTarget) (float64, bool) {
	return 27, target.PartID == "part-two"
}
func (s *playbackTestStore) SavePlaybackPosition(_ string, _ provider.PlaybackTarget, seconds float64) error {
	s.saved = seconds
	return nil
}

func TestPlaybackExplicitStartTakesPriorityOverResume(t *testing.T) {
	for _, start := range []float64{0, 42} {
		t.Run(fmt.Sprint(start), func(t *testing.T) {
			source := &playbackTestSession{requests: make(chan provider.PlaybackRequest, 1)}
			catalog := &Catalog{session: source, entryID: "entry"}
			events := make(chan func(), 1)
			position := -1.0
			controller := NewPlayback(catalog, player.Engine{
				Open: func(_ string, start float64) bool {
					position = start
					return true
				},
			}, new(playbackTestStore), func(f func()) { events <- f }, func() {})
			defer controller.Close()
			target := provider.PlaybackTarget{
				Ref:    provider.MediaRef{EntryID: "entry", Kind: provider.Video, ID: "upload"},
				PartID: "part-two",
			}
			controller.Start(target, "account", true, &start)
			select {
			case deliver := <-events:
				deliver()
			case <-time.After(5 * time.Second):
				t.Fatal("the playback controller didn't load the selected part")
			}
			if position != start {
				t.Fatalf("native start = %.2f, want %.2f", position, start)
			}
		})
	}
}

func TestPlaybackKeepsSelectedPartAndPositionOnQualityRestriction(t *testing.T) {
	events := make(chan func(), 4)
	wait := func() {
		t.Helper()
		select {
		case f := <-events:
			f()
		case <-time.After(5 * time.Second):
			t.Fatal("the playback controller didn't deliver the resource")
		}
	}
	source := &playbackTestSession{requests: make(chan provider.PlaybackRequest, 4)}
	catalog := &Catalog{session: source, entryID: "entry"}
	var positions []float64
	var rate float64
	var fullscreen bool
	clock := player.Status{HasFrame: true, Position: 40, Duration: 95}
	store := new(playbackTestStore)
	controller := NewPlayback(catalog, player.Engine{
		Open:       func(_ string, position float64) bool { positions = append(positions, position); return true },
		Seek:       func(seconds float64) { clock.Position = seconds },
		Snapshot:   func() player.Status { return clock },
		Fullscreen: func(value bool) { fullscreen = value },
		SetRate:    func(value float64) { rate = value },
	}, store, func(f func()) { events <- f }, func() {})
	defer controller.Close()
	target := provider.PlaybackTarget{
		Ref:    provider.MediaRef{EntryID: "entry", Kind: provider.Video, ID: "upload"},
		PartID: "part-two",
	}
	controller.Start(target, "account", true, nil)
	wait()
	if request := <-source.requests; request.PlaybackTarget != target || len(positions) != 1 || positions[0] != 27 || rate != 1 || !fullscreen {
		t.Fatalf("the selected P and saved resume point did not reach playback: %+v %v", request, positions)
	}

	controller.Tick(time.Now())
	controller.Toggle()
	controller.ChooseQuality("hdr")
	wait()
	<-source.requests
	state := controller.State()
	if state.Target != target || state.Limit != provider.PlaybackDevice || state.Position != 40 || len(positions) != 1 || state.Playing || rate != 0 {
		t.Fatalf("a rejected quality changed the active paused playback: %+v", state)
	}

	controller.DismissLimit()
	controller.state.Resolution.Plan.ExpiresAt = time.Now().Add(-time.Minute).Unix()
	controller.Seek(55.25)
	wait()
	if request := <-source.requests; request.PlaybackTarget != target || len(positions) != 2 || positions[1] != 55.25 || rate != 0 {
		t.Fatalf("refreshing expired resources didn't preserve the seek and pause: %+v %v", request, positions)
	}
	clock.Buffering, clock.Position = true, 55.25
	now := time.Now()
	controller.Tick(now)
	controller.Tick(now.Add(playbackStallTimeout + time.Second))
	if state := controller.State(); state.Failure != provider.Timeout || state.Position != 55.25 {
		t.Fatalf("a stall with a visible frame did not become retryable: %+v", state)
	}
	controller.Close()
	if fullscreen || store.saved != 55 {
		t.Fatalf("exiting left the native view active or lost the saved position: fullscreen=%v saved=%v", fullscreen, store.saved)
	}
}

func TestPlaybackTrialKeepsProgressWithoutCompletingTheVideo(t *testing.T) {
	store := new(playbackTestStore)
	controller := NewPlayback(&Catalog{}, player.Engine{}, store, nil, nil)
	controller.loaded, controller.didEnd = true, true
	controller.state.Target.Ref = provider.MediaRef{EntryID: "entry", Kind: provider.Video, ID: "upload"}
	controller.state.Position = 180.3
	controller.state.Resolution.Plan.Access = provider.VideoAccessPreview
	controller.save()
	if store.saved != 180 || controller.savedAsEnd {
		t.Fatalf("trial completion changed full video history: seconds=%f ended=%t", store.saved, controller.savedAsEnd)
	}
	controller.state.Resolution.Plan.Access = provider.VideoAccessGranted
	controller.save()
	if store.saved != 0 || !controller.savedAsEnd {
		t.Fatalf("full completion did not reset the saved position: seconds=%f ended=%t", store.saved, controller.savedAsEnd)
	}
}

type trialPlaybackSession struct {
	playbackTestSession
	plans chan provider.PlaybackPlan
}

func (s *trialPlaybackSession) ResolvePlayback(ctx context.Context, _ provider.PlaybackRequest) (provider.Playback, error) {
	select {
	case plan := <-s.plans:
		plan.Streams = []provider.PlaybackStream{{Format: provider.PlaybackDirect}}
		return provider.Playback{Plan: plan, Transport: playbackTestTransport{}}, nil
	case <-ctx.Done():
		return provider.Playback{}, ctx.Err()
	}
}

func TestPreviewRequiresConfirmationAndResumesAtEndAfterUpgrade(t *testing.T) {
	deliveries := make(chan func(), 4)
	deliver := func() {
		t.Helper()
		select {
		case callback := <-deliveries:
			callback()
		case <-time.After(5 * time.Second):
			t.Fatal("timed out resolving preview")
		}
	}
	source := &trialPlaybackSession{plans: make(chan provider.PlaybackPlan, 3)}
	preview := provider.PlaybackPlan{Access: provider.VideoAccessPreview, Duration: 180.3, ResumeSeconds: 180}
	source.plans <- preview
	source.plans <- preview
	source.plans <- provider.PlaybackPlan{Access: provider.VideoAccessGranted, Duration: 2100}
	clock := player.Status{HasFrame: true, Duration: preview.Duration}
	var positions []float64
	var rate float64
	playback := NewPlayback(&Catalog{session: source}, player.Engine{
		Open:     func(_ string, at float64) bool { positions = append(positions, at); return true },
		SetRate:  func(value float64) { rate = value },
		Snapshot: func() player.Status { return clock },
	}, nil, func(work func()) { deliveries <- work }, func() {})
	defer playback.Close()

	playback.Start(provider.PlaybackTarget{Ref: provider.MediaRef{Kind: provider.Video, ID: "video"}}, "", true, nil)
	deliver()
	if !playback.State().PreviewPending || playback.State().Playing || rate != 0 {
		t.Fatalf("preview started without confirmation: %+v, rate %v", playback.State(), rate)
	}
	if len(positions) != 1 || positions[0] != 0 {
		t.Fatalf("an exhausted saved preview did not restart at zero: %v", positions)
	}
	playback.StartPreview()
	if playback.State().PreviewPending || !playback.State().Playing || rate != 1 {
		t.Fatalf("confirmation did not start the preview: %+v, rate %v", playback.State(), rate)
	}

	clock.Ended, clock.Position = true, preview.Duration
	playback.Tick(time.Now())
	playback.RefreshAccess()
	deliver()
	if !playback.State().Clock.Ended || len(positions) != 1 {
		t.Fatalf("the same entitlement reopened the expired preview: %+v, positions %v", playback.State(), positions)
	}

	playback.RefreshAccess()
	deliver()
	if playback.State().Clock.Ended || !playback.State().Playing || len(positions) != 2 || positions[1] != preview.Duration || rate != 1 {
		t.Fatalf("full playback did not resume from the finished preview: %+v, positions %v, rate %v",
			playback.State(), positions, rate)
	}
	if playback.State().BytesPerSecond != 0 {
		t.Fatalf("idle fake media reported non-zero speed: %v", playback.State().BytesPerSecond)
	}
}

type autoQualitySession struct {
	playbackTestSession
	requests chan provider.PlaybackRequest
}

func (s *autoQualitySession) ResolvePlayback(_ context.Context, req provider.PlaybackRequest) (provider.Playback, error) {
	s.requests <- req
	chosen := req.Quality
	if chosen == "" {
		if req.HDR {
			chosen = "125"
		} else {
			chosen = "120"
		}
	}
	bw := map[string]int{"125": 24_000_000, "120": 12_000_000, "80": 4_000_000}[chosen]
	return provider.Playback{
		Plan: provider.PlaybackPlan{
			PartID:   req.PartID,
			Quality:  chosen,
			Duration: 120,
			Access:   provider.VideoAccessGranted,
			Qualities: []provider.PlaybackQuality{
				{ID: "125", Label: "HDR", HDR: true, Badge: "HDR", Bandwidth: 24_000_000},
				{ID: "120", Label: "4K", Badge: "4K", Bandwidth: 12_000_000},
				{ID: "80", Label: "1080P", Bandwidth: 4_000_000},
			},
			Streams: []provider.PlaybackStream{{
				Format: provider.PlaybackDirect,
				Video:  provider.PlaybackTrack{Bandwidth: bw},
			}},
		},
		Transport: playbackTestTransport{},
	}, nil
}

func TestPlaybackAutoQualityDowngradesAndProtectsHDRWhileLockedNeverDowngrades(t *testing.T) {
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
	source := &autoQualitySession{requests: make(chan provider.PlaybackRequest, 8)}
	clock := player.Status{HasFrame: true, Duration: 120, Position: 15, BufferEnd: 20, HDRVideo: true}
	var opened []float64
	pb := NewPlayback(&Catalog{session: source}, player.Engine{
		Open:         func(_ string, at float64) bool { opened = append(opened, at); return true },
		Snapshot:     func() player.Status { return clock },
		SetRate:      func(float64) {},
		Capabilities: func() player.Capabilities { return player.Capabilities{HDR: true, HEVC: true} },
	}, nil, func(fn func()) { deliveries <- fn }, func() {})
	defer pb.Close()

	target := provider.PlaybackTarget{Ref: provider.MediaRef{EntryID: "entry", Kind: provider.Video, ID: "hdr1"}}
	pb.Start(target, "user", false, nil)
	deliver()
	if req := <-source.requests; req.Quality != "" || !req.HDR {
		t.Fatalf("expected Auto start with HDR enabled, got %+v", req)
	}
	if st := pb.State(); st.Selected != "" || st.Resolution.Plan.Quality != "125" {
		t.Fatalf("expected Auto active on 125, got selected=%q quality=%q", st.Selected, st.Resolution.Plan.Quality)
	}

	// Rebuffering for 6s on HDR must NOT yet cross HDR->SDR (needs 8s).
	t0 := time.Now()
	clock.Buffering = true
	pb.Tick(t0)
	pb.Tick(t0.Add(6 * time.Second))
	if pb.State().Switching {
		t.Fatal("HDR downgraded too early before 8s cross-dynamic-range grace")
	}

	// At 8.5s of sustained rebuffering on HDR, Auto steps down to 4K SDR (120) while keeping Selected=="".
	pb.Tick(t0.Add(8500 * time.Millisecond))
	if !pb.State().Switching {
		t.Fatal("expected Auto HDR->SDR downgrade after 8.5s rebuffer")
	}
	deliver()
	if req := <-source.requests; req.Quality != "120" {
		t.Fatalf("expected downgrade request for 120, got %+v", req)
	}
	if st := pb.State(); st.Selected != "" || st.Resolution.Plan.Quality != "120" || !pb.autoHDRBlocked {
		t.Fatalf("unexpected state after HDR->SDR downgrade: selected=%q quality=%q hdrBlocked=%v",
			st.Selected, st.Resolution.Plan.Quality, pb.autoHDRBlocked)
	}

	// SDR->SDR downgrade from 120 to 80 triggers after cooldown (8s) + 5s rebuffering.
	clock.HDRVideo = false
	clock.Buffering = false
	t1 := t0.Add(20 * time.Second)
	pb.Tick(t1)
	clock.Buffering = true
	pb.Tick(t1.Add(time.Second))
	pb.Tick(t1.Add(6500 * time.Millisecond))
	if !pb.State().Switching {
		t.Fatal("expected Auto SDR 4K->1080P downgrade after 5.5s rebuffer")
	}
	deliver()
	if req := <-source.requests; req.Quality != "80" {
		t.Fatalf("expected downgrade request for 80, got %+v", req)
	}
	if st := pb.State(); st.Selected != "" || st.Resolution.Plan.Quality != "80" {
		t.Fatalf("expected Auto active on 80, got selected=%q quality=%q", st.Selected, st.Resolution.Plan.Quality)
	}

	// Once network stays healthy (buffer>=20s, speed>=1.6x 4K bitrate) for >=15s after 30s cooldown,
	// Auto steps back up to 4K SDR (120), but must NEVER auto-re-enter HDR (125).
	clock.Buffering = false
	clock.BufferEnd = clock.Position + 25
	pb.state.BytesPerSecond = 3_000_000 // > 1.6 * (12Mbps/8 = 1.5 MB/s)
	// Keep autoUpgrade checking with simulated fast speed via state override after media returns 0.
	t2 := t1.Add(45 * time.Second)
	pb.autoLastSwitch = t1.Add(6500 * time.Millisecond)
	pb.state.Clock = clock
	pb.stepAutoQuality(t2, clock) // sets autoHealthyAt = t2
	if pb.autoHealthyAt.IsZero() {
		t.Fatal("expected healthy window to begin on fast SDR connection")
	}
	if !pb.stepAutoQuality(t2.Add(16*time.Second), clock) {
		t.Fatal("expected Auto to step back up from 80 to 120")
	}
	deliver()
	if req := <-source.requests; req.Quality != "120" {
		t.Fatalf("expected upgrade request for 120, got %+v", req)
	}
	// Verify Auto does NOT re-enter HDR even after long healthy window.
	t3 := t2.Add(80 * time.Second)
	pb.state.BytesPerSecond = 10_000_000
	if pb.stepAutoQuality(t3, clock) || pb.stepAutoQuality(t3.Add(20*time.Second), clock) {
		t.Fatal("Auto re-entered HDR after autoHDRBlocked was set")
	}

	// Now manually lock HDR (125): sustained rebuffering must NEVER auto-downgrade.
	pb.ChooseQuality("125")
	deliver()
	if req := <-source.requests; req.Quality != "125" || !req.HDR {
		t.Fatalf("manual HDR lock should request 125 with HDR=true, got %+v", req)
	}
	if pb.State().Selected != "125" {
		t.Fatalf("Selected = %q, want 125", pb.State().Selected)
	}
	clock.HDRVideo = true
	clock.Buffering = true
	t4 := t3.Add(40 * time.Second)
	pb.Tick(t4)
	pb.Tick(t4.Add(12 * time.Second))
	if pb.State().Switching || pb.State().Resolution.Plan.Quality != "125" {
		t.Fatalf("locked quality unexpectedly auto-downgraded: %+v", pb.State())
	}
}

func TestPlaybackPreloadNextReusesWarmedMediaAndCleansUpOnClose(t *testing.T) {
	deliveries := make(chan func(), 8)
	deliver := func() {
		t.Helper()
		select {
		case fn := <-deliveries:
			fn()
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for preload/start delivery")
		}
	}
	source := &autoQualitySession{requests: make(chan provider.PlaybackRequest, 8)}
	var openedURLs []string
	pb := NewPlayback(&Catalog{session: source}, player.Engine{
		Open:     func(u string, _ float64) bool { openedURLs = append(openedURLs, u); return true },
		Snapshot: func() player.Status { return player.Status{HasFrame: true, Duration: 120, Position: 105} },
		SetRate:  func(float64) {},
	}, nil, func(fn func()) { deliveries <- fn }, func() {})

	ep1 := provider.PlaybackTarget{Ref: provider.MediaRef{EntryID: "entry", Kind: provider.Video, ID: "ep1"}}
	ep2 := provider.PlaybackTarget{Ref: provider.MediaRef{EntryID: "entry", Kind: provider.Video, ID: "ep2"}}
	ep3 := provider.PlaybackTarget{Ref: provider.MediaRef{EntryID: "entry", Kind: provider.Video, ID: "ep3"}}

	pb.Start(ep1, "user", false, nil)
	deliver()
	<-source.requests

	// Preload ep2 in background.
	pb.PreloadNext(ep2, "user")
	deliver()
	if req := <-source.requests; req.PlaybackTarget != ep2 {
		t.Fatalf("expected preload request for ep2, got %+v", req)
	}
	if pb.preloaded == nil || pb.preloaded.target != ep2 {
		t.Fatalf("expected preloaded ep2 ready, got %+v", pb.preloaded)
	}
	preloadedURL := pb.preloaded.media.URL

	// Starting ep2 must hit preloaded cache synchronously with ZERO additional ResolvePlayback call.
	pb.Start(ep2, "user", false, nil)
	select {
	case unexpected := <-source.requests:
		t.Fatalf("Start(ep2) triggered redundant ResolvePlayback: %+v", unexpected)
	default:
	}
	if len(openedURLs) != 2 || openedURLs[1] != preloadedURL {
		t.Fatalf("Start(ep2) did not reuse preloaded media URL: got %v want %q", openedURLs, preloadedURL)
	}
	if pb.preloaded != nil {
		t.Fatal("preloaded slot was not consumed by Start(ep2)")
	}

	// Preload ep3, then Close() must clean up preloaded media cleanly.
	pb.PreloadNext(ep3, "user")
	deliver()
	<-source.requests
	if pb.preloaded == nil {
		t.Fatal("expected ep3 preloaded before Close")
	}
	pb.Close()
	if pb.preloaded != nil || pb.preloadCancel != nil {
		t.Fatal("Close did not clear preloaded episode resources")
	}
}

func TestPlaybackInlinePreviewLifecycleAndFullscreenHandoff(t *testing.T) {
	source := &playbackTestSession{requests: make(chan provider.PlaybackRequest, 4)}
	catalog := &Catalog{session: source, entryID: "entry"}
	store := &playbackTestStore{}
	deliveries := make(chan func(), 8)
	deliver := func() {
		t.Helper()
		select {
		case fn := <-deliveries:
			fn()
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for inline preview delivery")
		}
	}

	var (
		previewCalls    []bool
		fullscreenCalls []bool
		openPositions   []float64
		seekCalls       []float64
		rates           []float64
		stopCount       int
		lastRect        player.ViewportRect
		clock           player.Status
	)
	pb := NewPlayback(catalog, player.Engine{
		Open: func(_ string, pos float64) bool {
			openPositions = append(openPositions, pos)
			return true
		},
		Seek:         func(sec float64) { seekCalls = append(seekCalls, sec) },
		SetRate:      func(rate float64) { rates = append(rates, rate) },
		Stop:         func() { stopCount++ },
		Snapshot:     func() player.Status { return clock },
		Fullscreen:   func(on bool) { fullscreenCalls = append(fullscreenCalls, on) },
		Preview:      func(on bool) { previewCalls = append(previewCalls, on) },
		ViewportRect: func(r player.ViewportRect) { lastRect = r },
		Capabilities: func() player.Capabilities { return player.Capabilities{HDR: true, HEVC: true} },
	}, store, func(fn func()) { deliveries <- fn }, func() {})
	defer pb.Close()

	target := provider.PlaybackTarget{Ref: provider.MediaRef{EntryID: "entry", Kind: provider.Video, ID: "hero-1"}}
	pb.StartInlinePreview(target)
	if !pb.InlinePreviewState().Active || !pb.InlinePreviewState().Loading {
		t.Fatalf("expected active loading preview, got %+v", pb.InlinePreviewState())
	}
	req := <-source.requests
	if req.HDR || req.PreferredQuality != "1080p" {
		t.Fatalf("inline preview must request SDR 1080p fast-start, got HDR=%t pref=%q", req.HDR, req.PreferredQuality)
	}
	deliver()

	if len(fullscreenCalls) != 0 {
		t.Fatalf("inline preview must never enter fullscreen: %v", fullscreenCalls)
	}
	if len(previewCalls) != 1 || !previewCalls[0] {
		t.Fatalf("expected Preview(true), got %v", previewCalls)
	}
	if len(openPositions) != 1 || openPositions[0] != 0 {
		t.Fatalf("expected preview open at 0, got %v", openPositions)
	}
	if len(rates) == 0 || rates[len(rates)-1] != 1 {
		t.Fatalf("expected preview rate 1, got %v", rates)
	}

	wantRect := player.ViewportRect{X: 116, Y: 0, W: 1804, H: 875, Cover: true}
	pb.SetInlinePreviewRect(wantRect)
	if lastRect != wantRect {
		t.Fatalf("viewport rect = %+v, want %+v", lastRect, wantRect)
	}

	clock = player.Status{HasFrame: true}
	pb.TickInlinePreview(time.Now())
	if st := pb.InlinePreviewState(); !st.HasFrame || !st.Playing {
		t.Fatalf("expected HasFrame & Playing after tick, got %+v", st)
	}

	// Ending inline preview loops cleanly back to 0 without saving history.
	clock = player.Status{HasFrame: true, Ended: true}
	pb.TickInlinePreview(time.Now())
	if len(seekCalls) != 1 || seekCalls[0] != 0 {
		t.Fatalf("expected loop seek to 0 on end, got %v", seekCalls)
	}
	if store.saved != 0 {
		t.Fatalf("inline preview must never persist watch history, saved=%.2f", store.saved)
	}

	// Entering full-screen Start() automatically tears down inline preview first.
	startAt := 0.0
	pb.Start(target, "user", false, &startAt)
	if pb.InlinePreviewState().Active {
		t.Fatal("fullscreen Start did not stop inline preview")
	}
	if stopCount == 0 || len(previewCalls) < 2 || previewCalls[len(previewCalls)-1] {
		t.Fatalf("expected Stop + Preview(false) on handoff, stops=%d preview=%v", stopCount, previewCalls)
	}
	if len(fullscreenCalls) != 1 || !fullscreenCalls[0] {
		t.Fatalf("expected Fullscreen(true) after handoff, got %v", fullscreenCalls)
	}
	<-source.requests
	deliver()

	// Inline preview must be ignored while fullscreen playback is active.
	pb.StartInlinePreview(provider.PlaybackTarget{Ref: provider.MediaRef{EntryID: "entry", Kind: provider.Video, ID: "hero-2"}})
	if pb.InlinePreviewState().Active {
		t.Fatal("inline preview started while fullscreen playback was active")
	}
}
