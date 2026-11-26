package app

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yuchanns/MikuTV/internal/player"
	"github.com/yuchanns/MikuTV/internal/provider"
)

type hlsPlanAudioTransport struct{}

func (hlsPlanAudioTransport) OpenResource(_ context.Context, res provider.PlaybackResource, _, _ string) (*http.Response, error) {
	body := "#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:6\n#EXTINF:6.0,\nseg0.ts\n#EXT-X-ENDLIST\n"
	raw := "https://example.test/master.m3u8"
	if len(res.URLs) > 0 {
		raw = res.URLs[0]
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/vnd.apple.mpegurl"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    httptest.NewRequest(http.MethodGet, raw, nil),
	}, nil
}

type hlsPlanAudioSession struct {
	provider.Session
	requests      chan provider.PlaybackRequest
	reports       chan provider.PlaybackProgress
	failAudio     string
	restrictAudio string
}

func (s *hlsPlanAudioSession) ResolvePlayback(_ context.Context, req provider.PlaybackRequest) (provider.Playback, error) {
	if s.requests != nil {
		s.requests <- req
	}
	if s.failAudio != "" && req.AudioTrack == s.failAudio {
		return provider.Playback{}, errors.New("audio switch failed")
	}
	activeAudio := req.AudioTrack
	if activeAudio == "" {
		activeAudio = "1"
	}
	quality := req.Quality
	if quality == "" {
		quality = "original"
	}
	var restriction provider.PlaybackLimit
	if s.restrictAudio != "" && req.AudioTrack == s.restrictAudio {
		restriction = provider.PlaybackMembership
	}
	return provider.Playback{
		Plan: provider.PlaybackPlan{
			ItemID:      req.Ref.ID,
			Duration:    300,
			Access:      provider.VideoAccessGranted,
			Restriction: restriction,
			Quality:     quality,
			Qualities: []provider.PlaybackQuality{
				{ID: "original", Label: "1080p · Original", Bandwidth: 8_000_000},
				{ID: "720p-4m", Label: "720p · 4 Mbps", Bandwidth: 4_000_000},
			},
			AudioTrack: activeAudio,
			AudioTracks: []provider.PlaybackTrack{
				{ID: "1", Label: "English 7.1", Language: "eng", Default: true},
				{ID: "2", Label: "Chinese 7.1", Language: "zho"},
				{ID: "3", Label: "Commentary", Language: "eng"},
				{ID: "4", Label: "VIP Atmos", Language: "eng"},
			},
			Streams: []provider.PlaybackStream{{
				Format: provider.PlaybackHLS,
				Video: provider.PlaybackTrack{
					Resource:    provider.PlaybackResource{URLs: []string{"https://example.test/master.m3u8?audio=" + activeAudio}},
					ContentType: "application/vnd.apple.mpegurl",
					Bandwidth:   8_000_000,
				},
			}},
		},
		Transport: hlsPlanAudioTransport{},
	}, nil
}

func (s *hlsPlanAudioSession) ReportPlayback(_ context.Context, report provider.PlaybackProgress) error {
	if s.reports != nil {
		s.reports <- report
	}
	return nil
}

func TestPlaybackPlanAudioSwitchingStoppedReportAndNoFastLANAutoDowngrade(t *testing.T) {
	deliveries := make(chan func(), 16)
	deliver := func() {
		t.Helper()
		select {
		case fn := <-deliveries:
			fn()
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for playback delivery")
		}
	}

	source := &hlsPlanAudioSession{
		requests:      make(chan provider.PlaybackRequest, 8),
		reports:       make(chan provider.PlaybackProgress, 8),
		failAudio:     "3",
		restrictAudio: "4",
	}
	clock := player.Status{HasFrame: true, Duration: 300, Position: 42, BufferEnd: 55}
	var openedAt []float64
	cat := &Catalog{
		entryID:   "entry",
		session:   source,
		canReport: true,
	}
	pb := NewPlayback(cat, player.Engine{
		Open:         func(_ string, at float64) bool { openedAt = append(openedAt, at); return true },
		Snapshot:     func() player.Status { return clock },
		SetRate:      func(float64) {},
		Capabilities: func() player.Capabilities { return player.Capabilities{HDR: true, HEVC: true} },
	}, nil, func(fn func()) { deliveries <- fn }, func() {})

	target := provider.PlaybackTarget{Ref: provider.MediaRef{EntryID: "entry", Kind: provider.Video, ID: "avengers"}}
	pb.Start(target, "user", true, nil)
	deliver()
	req := <-source.requests
	if req.AudioTrack != "" {
		t.Fatalf("initial AudioTrack = %q, want empty", req.AudioTrack)
	}
	st := pb.State()
	if len(st.AudioTracks) != 4 || st.DefaultAudio != "1" || st.ActiveAudio != "1" {
		t.Fatalf("unexpected plan audio state: tracks=%d default=%q active=%q", len(st.AudioTracks), st.DefaultAudio, st.ActiveAudio)
	}

	// Regression 1: brief buffering blips within startup grace must NOT trigger
	// repeated-stall downgrade (e.g. HDMI display-mode switch on Apple TV).
	bootAt := pb.startedAt
	for i := 0; i < 3; i++ {
		clock.Buffering = true
		pb.Tick(bootAt.Add(time.Duration(i*300+100) * time.Millisecond))
		clock.Buffering = false
		pb.Tick(bootAt.Add(time.Duration(i*300+250) * time.Millisecond))
	}
	if pb.State().Switching || pb.State().Resolution.Plan.Quality != "original" {
		t.Fatalf("startup buffering flickers falsely downgraded: switching=%v quality=%q", pb.State().Switching, pb.State().Resolution.Plan.Quality)
	}

	// Regression 2: repeated brief buffering blips on fast LAN (!underfed) after
	// startup grace must NEVER trigger SDR Auto downgrade.
	now := bootAt.Add(5 * time.Second)
	pb.state.BytesPerSecond = 8_000_000 // 64 Mbps >> 8 Mbps stream bitrate
	for i := 0; i < 4; i++ {
		clock.Buffering = true
		if pb.stepAutoQuality(now.Add(time.Duration(i*2)*time.Second), clock) {
			t.Fatal("fast LAN SDR stream falsely downgraded on repeated buffering blip")
		}
		clock.Buffering = false
		pb.stepAutoQuality(now.Add(time.Duration(i*2+1)*time.Second), clock)
	}
	if pb.State().Switching || pb.State().Resolution.Plan.Quality != "original" {
		t.Fatalf("fast LAN SDR stream falsely downgraded: switching=%v quality=%q", pb.State().Switching, pb.State().Resolution.Plan.Quality)
	}
	clock.Buffering = false
	pb.Tick(now.Add(9 * time.Second))

	// Switch to plan-level audio track "2" in place.
	if !pb.ChooseAudioTrack("2") {
		t.Fatal("ChooseAudioTrack(2) returned false")
	}
	deliver()
	req = <-source.requests
	if req.AudioTrack != "2" {
		t.Fatalf("expected resolve request with AudioTrack=2, got %+v", req)
	}
	st = pb.State()
	if st.ActiveAudio != "2" || st.SelectedAudio != "2" {
		t.Fatalf("expected ActiveAudio=2 SelectedAudio=2, got active=%q selected=%q", st.ActiveAudio, st.SelectedAudio)
	}
	if len(openedAt) < 2 || openedAt[len(openedAt)-1] != 42 {
		t.Fatalf("expected in-place reopen at position 42, got %v", openedAt)
	}

	// Failed switch to track "3" must keep active audio on "2" and roll back selectedAudio.
	if !pb.ChooseAudioTrack("3") {
		t.Fatal("ChooseAudioTrack(3) returned false")
	}
	deliver()
	<-source.requests
	if pb.State().ActiveAudio != "2" || pb.selectedAudio != "2" {
		t.Fatalf("expected rollback to audio 2 after failed switch, got active=%q selected=%q", pb.State().ActiveAudio, pb.selectedAudio)
	}

	// Restricted replacement plan for track "4" must also roll back selectedAudio
	// so a subsequent quality change keeps requesting active track "2".
	if !pb.ChooseAudioTrack("4") {
		t.Fatal("ChooseAudioTrack(4) returned false")
	}
	deliver()
	<-source.requests
	if pb.State().ActiveAudio != "2" || pb.selectedAudio != "2" {
		t.Fatalf("expected rollback to audio 2 after restricted switch, got active=%q selected=%q", pb.State().ActiveAudio, pb.selectedAudio)
	}
	pb.ChooseQuality("720p-4m")
	deliver()
	qReq := <-source.requests
	if qReq.AudioTrack != "2" || qReq.Quality != "720p-4m" {
		t.Fatalf("subsequent quality change requested audio=%q quality=%q, want audio=2 quality=720p-4m", qReq.AudioTrack, qReq.Quality)
	}

	// Closing playback must emit a final Stopped=true report even if position was already saved.
	pb.save()
	select {
	case rep := <-source.reports:
		if rep.Stopped || rep.Seconds != 42 {
			t.Fatalf("unexpected progress report before close: %+v", rep)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for progress report")
	}
	deliver() // report completion callback

	pb.Close()
	select {
	case rep := <-source.reports:
		if !rep.Stopped || rep.Seconds != 42 || rep.ItemID != "avengers" {
			t.Fatalf("expected final Stopped=true report at 42s, got %+v", rep)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for Stopped=true report on Close")
	}
}

func TestQueueReportPreservesStoppedBeforeNewStart(t *testing.T) {
	gate := make(chan struct{})
	delivered := make(chan provider.PlaybackProgress, 8)
	deliveries := make(chan func(), 16)
	reporter := &blockingReportSession{
		gate:      gate,
		delivered: delivered,
	}
	cat := &Catalog{entryID: "entry", session: reporter, canReport: true}
	pb := NewPlayback(cat, player.Engine{}, nil, func(fn func()) { deliveries <- fn }, func() {})
	pb.owner = "user"
	pb.syncSource = true
	pb.state.Resolution.Plan.ItemID = "track-1"

	target := provider.PlaybackTarget{Ref: provider.MediaRef{EntryID: "entry", Kind: provider.Video, ID: "track-1"}}
	// First report enters in-flight state (blocked on gate).
	pb.queueReport(target, 10, false, false)
	// While in-flight, close queues Stopped=true, then immediate replay queues start (0s, Stopped=false),
	// followed by a newer progress update (15s, Stopped=false) that coalesces with the replay's progress only.
	pb.queueReport(target, 12, false, true)
	pb.queueReport(target, 0, false, false)
	pb.queueReport(target, 15, false, false)

	close(gate)
	var got []provider.PlaybackProgress
	for i := 0; i < 3; i++ {
		select {
		case r := <-delivered:
			got = append(got, r)
		case <-time.After(3 * time.Second):
			t.Fatalf("timed out waiting for report %d, got %+v", i, got)
		}
		select {
		case fn := <-deliveries:
			fn()
		case <-time.After(3 * time.Second):
			t.Fatalf("timed out waiting for completion callback %d", i)
		}
	}
	if len(got) != 3 ||
		got[0].Seconds != 10 || got[0].Stopped ||
		got[1].Seconds != 12 || !got[1].Stopped ||
		got[2].Seconds != 15 || got[2].Stopped {
		t.Fatalf("unexpected report sequence: %+v", got)
	}
}

type blockingReportSession struct {
	provider.Session
	gate      chan struct{}
	delivered chan provider.PlaybackProgress
}

func (s *blockingReportSession) ReportPlayback(_ context.Context, report provider.PlaybackProgress) error {
	<-s.gate
	s.delivered <- report
	return nil
}
