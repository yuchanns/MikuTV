package app

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yuchanns/MikuTV/internal/player"
	"github.com/yuchanns/MikuTV/internal/provider"
)

type audioPlaybackSession struct {
	audioOnly      bool
	availableKinds []string
	lastReq        provider.PlaybackRequest
	resolveCount   atomic.Int32
}

func (s *audioPlaybackSession) Close() {}

func (s *audioPlaybackSession) Browse(context.Context, provider.BrowseRequest) (provider.CollectionPage, error) {
	return provider.CollectionPage{}, nil
}

func (s *audioPlaybackSession) Item(context.Context, provider.MediaRef) (provider.MediaItem, error) {
	return provider.MediaItem{}, nil
}

func (s *audioPlaybackSession) ResolvePlayback(_ context.Context, req provider.PlaybackRequest) (provider.Playback, error) {
	s.resolveCount.Add(1)
	s.lastReq = req
	wantAudio := s.audioOnly
	if req.MediaKind == provider.PlaybackMediaAudio {
		wantAudio = true
	} else if req.MediaKind == provider.PlaybackMediaVideo {
		wantAudio = false
	}
	stream := provider.PlaybackStream{Format: provider.PlaybackDirect}
	if wantAudio {
		stream.Audio = &provider.PlaybackTrack{
			ContentType: "audio/mpeg",
			Resource:    provider.PlaybackResource{URLs: []string{"https://cdn.example/track.mp3"}},
		}
	} else {
		stream.Video = provider.PlaybackTrack{
			ContentType: "video/mp4",
			Resource:    provider.PlaybackResource{URLs: []string{"https://cdn.example/video.mp4"}},
		}
	}
	return provider.Playback{
		Plan: provider.PlaybackPlan{
			Duration:       240,
			AvailableKinds: s.availableKinds,
			Streams:        []provider.PlaybackStream{stream},
		},
		Transport: audioMockTransport{},
	}, nil
}

type audioMockTransport struct{}

func (audioMockTransport) OpenResource(context.Context, provider.PlaybackResource, string, string) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"audio/mpeg"}},
		Body:       io.NopCloser(bytes.NewReader([]byte("ID3"))),
	}, nil
}

func TestAudioPlaybackStatePauseDimAndInlinePreviewSafety(t *testing.T) {
	sess := &audioPlaybackSession{audioOnly: true}
	queue := make(chan func(), 16)
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
	catalog := NewCatalog(func(string) provider.Session { return sess }, func(fn func()) { queue <- fn }, func() {})
	defer catalog.Close()
	catalog.Open(Entry{
		ID:           "demo",
		SourceID:     "demo",
		Capabilities: provider.Capabilities{provider.BrowseCatalog, provider.ResolvePlayback},
	})

	var (
		gotAudioOnly   bool
		gotScreensaver bool
		gotNowPlaying  player.NowPlayingInfo
		clock          = player.Status{HasFrame: true, AudioOnly: true, Position: 12, Duration: 240}
	)
	pb := NewPlayback(catalog, player.Engine{
		Open:                func(string, float64) bool { return true },
		SetRate:             func(float64) {},
		Stop:                func() {},
		SetAudioOnly:        func(v bool) { gotAudioOnly = v },
		SetAllowScreensaver: func(v bool) { gotScreensaver = v },
		SetNowPlaying:       func(info player.NowPlayingInfo) { gotNowPlaying = info },
		Preview:             func(bool) {},
		Snapshot:            func() player.Status { return clock },
	}, nil, func(fn func()) { queue <- fn }, func() {})
	defer pb.Close()

	prefs := NewPlaybackSettings(nil, nil)
	_ = prefs.Update(func(cfg *PlaybackPreferences) { cfg.AllowAudioScreensaver = true })
	pb.SetPreferences(prefs)
	if !gotScreensaver {
		t.Fatal("expected SetAllowScreensaver(true) on SetPreferences")
	}

	target := provider.PlaybackTarget{Ref: provider.MediaRef{EntryID: "demo", Kind: provider.Video, ID: "track-1"}}
	pb.Start(target, "", false, nil)
	select {
	case fn := <-queue:
		fn()
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for playback accept")
	}
	drain()

	if !gotAudioOnly || !pb.State().AudioOnly {
		t.Fatalf("expected AudioOnly=true, engine=%t state=%t", gotAudioOnly, pb.State().AudioOnly)
	}

	// Pause and advance 5 minutes -> AudioDimmed becomes true; resume clears it.
	pb.Toggle()
	base := time.Now()
	pb.Tick(base)
	if pb.State().AudioDimmed {
		t.Fatal("AudioDimmed should not trigger immediately on pause")
	}
	pb.Tick(base.Add(5*time.Minute + time.Second))
	if !pb.State().AudioDimmed {
		t.Fatal("AudioDimmed should become true after 5 minutes paused")
	}
	pb.Toggle()
	pb.Tick(base.Add(5*time.Minute + 2*time.Second))
	if pb.State().AudioDimmed {
		t.Fatal("AudioDimmed should clear on resume")
	}

	// NowPlaying pass-through and Close cleanup
	pb.SetNowPlaying(player.NowPlayingInfo{Active: true, Title: "Nocturne"})
	if !gotNowPlaying.Active || gotNowPlaying.Title != "Nocturne" {
		t.Fatalf("unexpected NowPlaying: %+v", gotNowPlaying)
	}
	pb.Close()
	if gotNowPlaying.Active || gotAudioOnly {
		t.Fatalf("expected Close to clear NowPlaying and AudioOnly, got active=%t audioOnly=%t", gotNowPlaying.Active, gotAudioOnly)
	}

	// Audio-only inline preview must keep HasFrame=false so Hero art never clears
	pb.StartInlinePreview(target)
	select {
	case fn := <-queue:
		fn()
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for inline preview")
	}
	drain()
	pb.TickInlinePreview(time.Now())
	ps := pb.InlinePreviewState()
	if !ps.Active || !ps.AudioOnly || ps.HasFrame {
		t.Fatalf("audio inline preview state = %+v, want Active=true AudioOnly=true HasFrame=false", ps)
	}
}

func TestSwitchMediaKindPreservesPosition(t *testing.T) {
	sess := &audioPlaybackSession{
		audioOnly:      true,
		availableKinds: []string{provider.PlaybackMediaAudio, provider.PlaybackMediaVideo},
	}
	queue := make(chan func(), 16)
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
	catalog := NewCatalog(func(string) provider.Session { return sess }, func(fn func()) { queue <- fn }, func() {})
	defer catalog.Close()
	catalog.Open(Entry{
		ID:           "demo",
		SourceID:     "demo",
		Capabilities: provider.Capabilities{provider.BrowseCatalog, provider.ResolvePlayback},
	})

	var (
		openedPos float64
		clock     = player.Status{HasFrame: true, AudioOnly: true, Position: 88.5, Duration: 240}
	)
	pb := NewPlayback(catalog, player.Engine{
		Open:     func(_ string, pos float64) bool { openedPos = pos; return true },
		SetRate:  func(float64) {},
		Stop:     func() {},
		Snapshot: func() player.Status { return clock },
	}, nil, func(fn func()) { queue <- fn }, func() {})
	defer pb.Close()

	waitReady := func(wantAudio bool) {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			drain()
			st := pb.State()
			if !st.Loading && !st.Switching && st.AudioOnly == wantAudio {
				return
			}
			select {
			case fn := <-queue:
				fn()
			case <-time.After(10 * time.Millisecond):
			}
		}
		t.Fatalf("timed out waiting for AudioOnly=%t (state=%+v)", wantAudio, pb.State())
	}

	target := provider.PlaybackTarget{Ref: provider.MediaRef{EntryID: "demo", Kind: provider.Video, ID: "track-mv"}}
	pb.Start(target, "", false, nil)
	waitReady(true)
	pb.Tick(time.Now())

	if !pb.SwitchMediaKind(provider.PlaybackMediaVideo) {
		t.Fatal("SwitchMediaKind(video) returned false")
	}
	waitReady(false)
	if sess.lastReq.MediaKind != provider.PlaybackMediaVideo {
		t.Fatalf("request MediaKind = %q, want video", sess.lastReq.MediaKind)
	}
	if openedPos < 88.0 || openedPos > 89.0 {
		t.Fatalf("opened position = %.2f, want ~88.5 preserved", openedPos)
	}

	// Switch back to audio preserving new position (reuses cached alternate Media without extra ResolvePlayback!)
	beforeBack := sess.resolveCount.Load()
	clock.AudioOnly = false
	clock.Position = 123.0
	pb.Tick(time.Now())
	if !pb.SwitchMediaKind(provider.PlaybackMediaAudio) {
		t.Fatal("SwitchMediaKind(audio) returned false")
	}
	waitReady(true)
	if openedPos < 122.5 || openedPos > 123.5 {
		t.Fatalf("opened position = %.2f, want ~123.0 preserved", openedPos)
	}
	if got := sess.resolveCount.Load(); got != beforeBack {
		t.Fatalf("expected cached alternate audio reuse without ResolvePlayback, got %d -> %d", beforeBack, got)
	}
}
