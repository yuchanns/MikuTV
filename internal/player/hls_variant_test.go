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

type httpPlaybackTransport struct{}

func (httpPlaybackTransport) OpenResource(ctx context.Context, resource provider.PlaybackResource, method, byteRange string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, resource.URLs[0], nil)
	if err != nil {
		return nil, err
	}
	if byteRange != "" {
		req.Header.Set("Range", byteRange)
	}
	return http.DefaultClient.Do(req)
}

func TestHLSExtensionlessVariantPlaylistRewritten(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/master.m3u8":
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			_, _ = io.WriteString(w, "#EXTM3U\n#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID=\"a\",NAME=\"ja\",URI=\"audio_track\"\n#EXT-X-STREAM-INF:BANDWIDTH=4000000,AUDIO=\"a\"\nvariant_1080\n")
		case "/variant_1080":
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			_, _ = io.WriteString(w, "#EXTM3U\n#EXT-X-TARGETDURATION:4\n#EXTINF:4.0,\nseg0.ts\n#EXT-X-ENDLIST\n")
		case "/audio_track":
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			_, _ = io.WriteString(w, "#EXTM3U\n#EXT-X-TARGETDURATION:4\n#EXTINF:4.0,\naudio0.aac\n#EXT-X-ENDLIST\n")
		case "/seg0.ts":
			_, _ = io.WriteString(w, "VIDEO-SEG-0")
		case "/audio0.aac":
			_, _ = io.WriteString(w, "AUDIO-SEG-0")
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	media, err := OpenMedia(context.Background(), provider.Playback{
		Transport: httpPlaybackTransport{},
		Plan: provider.PlaybackPlan{
			Streams: []provider.PlaybackStream{
				{
					Format: provider.PlaybackHLS,
					Video:  provider.PlaybackTrack{Resource: provider.PlaybackResource{URLs: []string{srv.URL + "/master.m3u8"}}},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("OpenMedia: %v", err)
	}
	defer media.Close()

	masterResp, err := http.Get(media.URL)
	if err != nil {
		t.Fatalf("GET master: %v", err)
	}
	masterBytes, _ := io.ReadAll(masterResp.Body)
	masterResp.Body.Close()
	masterBody := string(masterBytes)

	var variantURL, audioURL string
	for _, line := range strings.Split(masterBody, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#EXT-X-MEDIA:") {
			if m := playlistURI.FindStringSubmatch(line); len(m) == 2 {
				audioURL = m[1]
			}
		} else if line != "" && !strings.HasPrefix(line, "#") {
			variantURL = line
		}
	}
	if variantURL == "" || audioURL == "" {
		t.Fatalf("missing rewritten child URLs in master:\n%s", masterBody)
	}

	parsedMedia, err := url.Parse(media.URL)
	if err != nil {
		t.Fatalf("parse media URL: %v", err)
	}
	origin := parsedMedia.Scheme + "://" + parsedMedia.Host

	for _, tc := range []struct {
		name     string
		childURL string
		wantBody string
	}{
		{"variant", origin + variantURL, "VIDEO-SEG-0"},
		{"audio", origin + audioURL, "AUDIO-SEG-0"},
	} {
		resp, err := http.Get(tc.childURL)
		if err != nil {
			t.Fatalf("GET %s playlist: %v", tc.name, err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		var segPath string
		for _, line := range strings.Split(string(b), "\n") {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "#") {
				segPath = line
				break
			}
		}
		if !strings.HasPrefix(segPath, "/") {
			t.Fatalf("%s playlist segment was not rewritten to proxy path: %q\nbody:\n%s", tc.name, segPath, string(b))
		}
		segResp, err := http.Get(origin + segPath)
		if err != nil {
			t.Fatalf("GET %s segment: %v", tc.name, err)
		}
		sb, _ := io.ReadAll(segResp.Body)
		segResp.Body.Close()
		if segResp.StatusCode != http.StatusOK || string(sb) != tc.wantBody {
			t.Fatalf("%s segment got %d %q, want 200 %q", tc.name, segResp.StatusCode, string(sb), tc.wantBody)
		}
	}
}

func TestHLSVODPlaylistRetainsAllSegmentsBeyondCacheCap(t *testing.T) {
	total := hlsMaxChildren + 64
	var vod strings.Builder
	vod.WriteString("#EXTM3U\n#EXT-X-TARGETDURATION:2\n")
	for i := range total {
		fmt.Fprintf(&vod, "#EXTINF:2.0,\nseg-%d.ts\n", i)
	}
	vod.WriteString("#EXT-X-ENDLIST\n")
	vodBody := vod.String()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/vod.m3u8":
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			_, _ = io.WriteString(w, vodBody)
		default:
			_, _ = io.WriteString(w, "OK:"+r.URL.Path)
		}
	}))
	defer srv.Close()

	media, err := OpenMedia(context.Background(), provider.Playback{
		Transport: httpPlaybackTransport{},
		Plan: provider.PlaybackPlan{
			Streams: []provider.PlaybackStream{
				{
					Format: provider.PlaybackHLS,
					Video:  provider.PlaybackTrack{Resource: provider.PlaybackResource{URLs: []string{srv.URL + "/vod.m3u8"}}},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("OpenMedia: %v", err)
	}
	defer media.Close()

	resp, err := http.Get(media.URL)
	if err != nil {
		t.Fatalf("GET vod playlist: %v", err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	var segPaths []string
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			segPaths = append(segPaths, line)
		}
	}
	if len(segPaths) != total {
		t.Fatalf("got %d segment paths, want %d", len(segPaths), total)
	}

	parsedMedia, _ := url.Parse(media.URL)
	origin := parsedMedia.Scheme + "://" + parsedMedia.Host
	for _, idx := range []int{0, 1, total / 2, total - 1} {
		segResp, err := http.Get(origin + segPaths[idx])
		if err != nil {
			t.Fatalf("GET segment %d: %v", idx, err)
		}
		sb, _ := io.ReadAll(segResp.Body)
		segResp.Body.Close()
		want := fmt.Sprintf("OK:/seg-%d.ts", idx)
		if segResp.StatusCode != http.StatusOK || string(sb) != want {
			t.Fatalf("segment %d got %d %q, want 200 %q", idx, segResp.StatusCode, string(sb), want)
		}
	}
}

func TestHLSLivePlaylistReleasesDroppedSegmentsForEviction(t *testing.T) {
	var round atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/live.m3u8" {
			_, _ = io.WriteString(w, "OK")
			return
		}
		n := int(round.Add(1))
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		var b strings.Builder
		b.WriteString("#EXTM3U\n#EXT-X-TARGETDURATION:2\n")
		start := (n - 1) * 1500
		for i := start; i < start+1500; i++ {
			fmt.Fprintf(&b, "#EXTINF:2.0,\nlive-%d.ts\n", i)
		}
		_, _ = io.WriteString(w, b.String())
	}))
	defer srv.Close()

	media, err := OpenMedia(context.Background(), provider.Playback{
		Transport: httpPlaybackTransport{},
		Plan: provider.PlaybackPlan{
			Streams: []provider.PlaybackStream{
				{
					Format: provider.PlaybackHLS,
					Video:  provider.PlaybackTrack{Resource: provider.PlaybackResource{URLs: []string{srv.URL + "/live.m3u8"}}},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("OpenMedia: %v", err)
	}
	defer media.Close()

	for range 4 {
		resp, err := http.Get(media.URL)
		if err != nil {
			t.Fatalf("GET live playlist: %v", err)
		}
		_, _ = io.ReadAll(resp.Body)
		resp.Body.Close()
	}

	media.mu.Lock()
	count := len(media.hlsChildren)
	media.mu.Unlock()
	if count != hlsMaxChildren {
		t.Fatalf("live hlsChildren = %d, want bounded at %d", count, hlsMaxChildren)
	}
}
