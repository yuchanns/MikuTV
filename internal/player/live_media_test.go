package player

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
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

	// Register more than hlsMaxChildren unique segments while keeping the init segment hot,
	// and verify hlsChildren stays bounded at hlsMaxChildren without evicting h0.m4s.
	base, _ := url.Parse("https://live.example.com/live/index.m3u8")
	for i := range hlsMaxChildren + 32 {
		if i%500 == 0 {
			media.playlistChild(base, "h0.m4s", nil)
		}
		media.playlistChild(base, fmt.Sprintf("rot-%d.m4s", i), nil)
	}
	media.mu.Lock()
	childCount := len(media.hlsChildren)
	media.mu.Unlock()
	if childCount != hlsMaxChildren {
		t.Fatalf("hlsChildren count = %d, want bounded at %d", childCount, hlsMaxChildren)
	}
	if got := readFixture(t, origin+initPath, ""); got != "INIT-FMP4" {
		t.Fatalf("hot init segment was evicted unexpectedly: %q", got)
	}
}

type vodHTTPHLSTransport struct {
	segments int
}

func (v *vodHTTPHLSTransport) OpenResource(ctx context.Context, resource provider.PlaybackResource, method, byteRange string) (*http.Response, error) {
	u := resource.URLs[0]
	rec := httptest.NewRecorder()
	switch {
	case strings.HasSuffix(u, "/master.m3u8"):
		rec.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		_, _ = io.WriteString(rec, "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=8000000\nmain.m3u8?token=abc\n")
	case strings.Contains(u, "/main.m3u8"):
		rec.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		var b strings.Builder
		b.WriteString("#EXTM3U\n#EXT-X-VERSION:7\n#EXT-X-TARGETDURATION:6\n#EXT-X-MAP:URI=\"hls1/main/-1.mp4?token=abc\"\n")
		for i := range v.segments {
			fmt.Fprintf(&b, "#EXTINF:6.0,\nhls1/main/%d.mp4?token=abc\n", i)
		}
		b.WriteString("#EXT-X-ENDLIST\n")
		_, _ = io.WriteString(rec, b.String())
	case strings.Contains(u, "/-1.mp4"):
		rec.Header().Set("Content-Type", "video/mp4")
		_, _ = io.WriteString(rec, "VOD-INIT-FMP4")
	default:
		rec.Header().Set("Content-Type", "video/mp4")
		_, _ = io.WriteString(rec, "VOD-SEG:"+u[strings.LastIndex(u, "/")+1:])
	}
	res := rec.Result()
	req, _ := http.NewRequestWithContext(ctx, method, u, nil)
	res.Request = req
	return res, nil
}

func TestHLSLocalHTTPOriginAndLongVOD(t *testing.T) {
	const totalSegments = 1200 // > 512 legacy limit (~2h movie at 6s/segment)
	playback := provider.Playback{
		Transport: &vodHTTPHLSTransport{segments: totalSegments},
		Plan: provider.PlaybackPlan{
			Streams: []provider.PlaybackStream{
				{
					Format: provider.PlaybackHLS,
					Video: provider.PlaybackTrack{
						Resource: provider.PlaybackResource{
							URLs: []string{"http://192.168.1.50:8096/videos/1/master.m3u8"},
						},
					},
				},
			},
		},
	}
	media, err := OpenMedia(context.Background(), playback)
	if err != nil {
		t.Fatalf("OpenMedia for same-origin HTTP HLS failed: %v", err)
	}
	defer media.Close()

	origin := strings.TrimSuffix(media.URL, media.prefix+"stream.m3u8")
	master := readFixture(t, media.URL, "")
	if strings.Contains(master, "unavailable") {
		t.Fatalf("master playlist rejected same-origin HTTP child:\n%s", master)
	}
	var variantPath string
	for _, line := range strings.Split(master, "\n") {
		if strings.HasPrefix(line, media.prefix+"c/") {
			variantPath = strings.TrimSpace(line)
			break
		}
	}
	if variantPath == "" {
		t.Fatalf("expected rewritten variant playlist path, got:\n%s", master)
	}

	variant := readFixture(t, origin+variantPath, "")
	if strings.Contains(variant, "unavailable") {
		t.Fatalf("variant playlist rejected same-origin HTTP segments:\n%s", variant[:min(len(variant), 400)])
	}
	start := strings.Index(variant, `URI="`) + len(`URI="`)
	end := strings.Index(variant[start:], `"`)
	initPath := variant[start : start+end]
	if got := readFixture(t, origin+initPath, ""); got != "VOD-INIT-FMP4" {
		t.Fatalf("init map after 1200 segments = %q, want VOD-INIT-FMP4", got)
	}

	var segPaths []string
	for _, line := range strings.Split(variant, "\n") {
		if strings.HasPrefix(line, media.prefix+"c/") {
			segPaths = append(segPaths, strings.TrimSpace(line))
		}
	}
	if len(segPaths) != totalSegments {
		t.Fatalf("segment count = %d, want %d", len(segPaths), totalSegments)
	}
	if got := readFixture(t, origin+segPaths[0], ""); got != "VOD-SEG:0.mp4?token=abc" {
		t.Fatalf("first segment = %q, want VOD-SEG:0.mp4?token=abc", got)
	}
	if got := readFixture(t, origin+segPaths[len(segPaths)-1], ""); got != "VOD-SEG:1199.mp4?token=abc" {
		t.Fatalf("last segment = %q, want VOD-SEG:1199.mp4?token=abc", got)
	}

	// Verify scheme downgrade and cross-origin HTTP children remain blocked.
	httpsBase, _ := url.Parse("https://media.example.com/videos/1/master.m3u8")
	if got := media.playlistChild(httpsBase, "http://media.example.com/videos/1/main.m3u8", nil); got != "unavailable" {
		t.Fatalf("https->http downgrade allowed: %q", got)
	}
	httpBase, _ := url.Parse("http://192.168.1.50:8096/videos/1/master.m3u8")
	if got := media.playlistChild(httpBase, "http://192.168.1.51:8096/videos/1/main.m3u8", nil); got != "unavailable" {
		t.Fatalf("cross-host http child allowed: %q", got)
	}
	if got := media.playlistChild(httpBase, "http://192.168.1.50:8080/videos/1/main.m3u8", nil); got != "unavailable" {
		t.Fatalf("cross-port http child allowed: %q", got)
	}
}
