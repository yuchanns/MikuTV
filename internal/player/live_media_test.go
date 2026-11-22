package player

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/yuchanns/MikuTV/internal/provider"
)

type liveHLSTransport struct {
	seq atomic.Int64
}

func (l *liveHLSTransport) OpenResource(ctx context.Context, resource provider.PlaybackResource, method, byteRange string) (*http.Response, error) {
	u := resource.URLs[0]
	if strings.Contains(u, "broken.example.com") {
		return nil, &provider.Error{Code: provider.Network}
	}
	rec := httptest.NewRecorder()
	switch {
	case strings.HasSuffix(u, "/index.m3u8"):
		n := l.seq.Add(1)
		rec.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		fmt.Fprintf(rec, "#EXTM3U\n#EXT-X-VERSION:7\n#EXT-X-MEDIA-SEQUENCE:%d\n#EXT-X-TARGETDURATION:1\n#EXT-X-MAP:URI=\"h0.m4s\"\n#EXTINF:1.0,\n%d.m4s\n#EXTINF:1.0,\n%d.m4s\n", n, n*2, n*2+1)
	case strings.HasSuffix(u, "/h0.m4s"):
		rec.Header().Set("Content-Type", "video/mp4")
		_, _ = io.WriteString(rec, "INIT-FMP4")
	default:
		rec.Header().Set("Content-Type", "video/mp4")
		_, _ = io.WriteString(rec, "SEG:"+u[strings.LastIndex(u, "/")+1:])
	}
	res := rec.Result()
	req, _ := http.NewRequestWithContext(ctx, method, u, nil)
	res.Request = req
	return res, nil
}

func TestLiveHLSChildEvictionAndPreflightFallback(t *testing.T) {
	transport := &liveHLSTransport{}
	playback := provider.Playback{
		Transport: transport,
		Plan: provider.PlaybackPlan{
			Live: true,
			Streams: []provider.PlaybackStream{
				{
					Format: provider.PlaybackHLS,
					Live:   true,
					Video:  provider.PlaybackTrack{Resource: provider.PlaybackResource{URLs: []string{"https://broken.example.com/live/index.m3u8"}}},
				},
				{
					Format: provider.PlaybackHLS,
					Live:   true,
					Video:  provider.PlaybackTrack{Resource: provider.PlaybackResource{URLs: []string{"https://live.example.com/live/index.m3u8"}}},
				},
			},
		},
	}
	media, err := OpenMedia(context.Background(), playback)
	if err != nil {
		t.Fatalf("OpenMedia failed to fall back to healthy HLS stream: %v", err)
	}
	defer media.Close()

	resp, err := http.Get(media.URL)
	if err != nil {
		t.Fatalf("GET live playlist failed: %v", err)
	}
	bodyBytes, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if got := resp.Header.Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	body := string(bodyBytes)
	if !strings.Contains(body, media.prefix+"c/") {
		t.Fatalf("expected rewritten child paths with %q, got:\n%s", media.prefix+"c/", body)
	}

	// Extract init URI and verify it serves properly.
	start := strings.Index(body, `URI="`) + len(`URI="`)
	end := strings.Index(body[start:], `"`)
	initPath := body[start : start+end]
	origin := strings.TrimSuffix(media.URL, media.prefix+"stream.m3u8")
	if got := readFixture(t, origin+initPath, ""); got != "INIT-FMP4" {
		t.Fatalf("init segment = %q, want INIT-FMP4", got)
	}

	// Rotate 350 playlists (>700 unique segments) and verify hlsChildren stays bounded at hlsMaxChildren
	// while the frequently referenced init segment h0.m4s is retained.
	for range 350 {
		r, err := http.Get(media.URL)
		if err != nil {
			t.Fatalf("playlist reload failed: %v", err)
		}
		_, _ = io.Copy(io.Discard, r.Body)
		r.Body.Close()
	}
	media.mu.Lock()
	childCount := len(media.hlsChildren)
	media.mu.Unlock()
	if childCount > hlsMaxChildren {
		t.Fatalf("hlsChildren leaked: got %d > %d", childCount, hlsMaxChildren)
	}
	if got := readFixture(t, origin+initPath, ""); got != "INIT-FMP4" {
		t.Fatalf("hot init segment was evicted unexpectedly: %q", got)
	}
}
