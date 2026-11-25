package player

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/yuchanns/MikuTV/internal/provider"
)

type audioRangeTransport struct {
	payload     []byte
	contentType string
	rangeHeader string
}

func (t *audioRangeTransport) OpenResource(_ context.Context, res provider.PlaybackResource, method, rng string) (*http.Response, error) {
	t.rangeHeader = rng
	var req *http.Request
	if len(res.URLs) > 0 {
		if u, err := url.Parse(res.URLs[0]); err == nil {
			req = &http.Request{Method: method, URL: u}
		}
	}
	if method == http.MethodHead {
		return &http.Response{
			StatusCode:    http.StatusOK,
			ContentLength: int64(len(t.payload)),
			Request:       req,
			Header: http.Header{
				"Content-Type":  []string{t.contentType},
				"Accept-Ranges": []string{"bytes"},
			},
			Body: io.NopCloser(bytes.NewReader(nil)),
		}, nil
	}
	if rng == "bytes=0-3" && len(t.payload) >= 4 {
		return &http.Response{
			StatusCode:    http.StatusPartialContent,
			ContentLength: 4,
			Request:       req,
			Header: http.Header{
				"Content-Type":  []string{t.contentType},
				"Content-Range": []string{"bytes 0-3/12"},
				"Accept-Ranges": []string{"bytes"},
			},
			Body: io.NopCloser(bytes.NewReader(t.payload[:4])),
		}, nil
	}
	return &http.Response{
		StatusCode:    http.StatusOK,
		ContentLength: int64(len(t.payload)),
		Request:       req,
		Header: http.Header{
			"Content-Type":  []string{t.contentType},
			"Accept-Ranges": []string{"bytes"},
		},
		Body: io.NopCloser(bytes.NewReader(t.payload)),
	}, nil
}

func TestOpenMediaDirectAudioContainersAndRangeProxy(t *testing.T) {
	cases := []struct {
		name     string
		url      string
		ct       string
		wantSuff string
		wantCT   string
	}{
		{"mp3", "https://cdn.example/track.mp3", "audio/mpeg", "/audio.mp3", "audio/mpeg"},
		{"flac", "https://cdn.example/track.flac", "audio/flac", "/audio.flac", "audio/flac"},
		{"m4a", "https://cdn.example/track.m4a", "audio/mp4", "/audio.m4a", "audio/mp4"},
		{"aac", "https://cdn.example/track.aac", "audio/aac", "/audio.aac", "audio/aac"},
		{"wav", "https://cdn.example/track.wav", "audio/wav", "/audio.wav", "audio/wav"},
		{"caf", "https://cdn.example/track.caf", "audio/x-caf", "/audio.caf", "audio/x-caf"},
		{"ext-fallback", "https://cdn.example/master.flac?token=1", "", "/audio.flac", "audio/flac"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr := &audioRangeTransport{payload: []byte("fLaC01234567"), contentType: tc.ct}
			media, err := OpenMedia(context.Background(), provider.Playback{
				Plan: provider.PlaybackPlan{
					Duration: 240,
					Streams: []provider.PlaybackStream{{
						Format: provider.PlaybackDirect,
						Audio: &provider.PlaybackTrack{
							ContentType: tc.ct,
							Resource:    provider.PlaybackResource{URLs: []string{tc.url}},
						},
					}},
				},
				Transport: tr,
			})
			if err != nil {
				t.Fatalf("OpenMedia failed: %v", err)
			}
			defer media.Close()
			if !media.AudioOnly {
				t.Fatal("expected AudioOnly=true")
			}
			if !strings.HasSuffix(media.URL, tc.wantSuff) {
				t.Fatalf("URL = %q, want suffix %q", media.URL, tc.wantSuff)
			}
			req, err := http.NewRequest(http.MethodGet, media.URL, nil)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Range", "bytes=0-3")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != http.StatusPartialContent {
				t.Fatalf("status = %d, want 206", resp.StatusCode)
			}
			if string(body) != "fLaC" {
				t.Fatalf("body = %q, want fLaC", body)
			}
			if tr.rangeHeader != "bytes=0-" {
				t.Fatalf("upstream Range = %q, want bytes=0-", tr.rangeHeader)
			}
			if cr := resp.Header.Get("Content-Range"); cr != "bytes 0-3/12" {
				t.Fatalf("Content-Range = %q, want bytes 0-3/12", cr)
			}
			if gotCT := resp.Header.Get("Content-Type"); gotCT != tc.wantCT {
				t.Fatalf("Content-Type = %q, want %q", gotCT, tc.wantCT)
			}
		})
	}
}

func TestOpenMediaRejectsOggAudio(t *testing.T) {
	tr := &audioRangeTransport{payload: []byte("OggS"), contentType: "audio/ogg"}
	_, err := OpenMedia(context.Background(), provider.Playback{
		Plan: provider.PlaybackPlan{
			Duration: 180,
			Streams: []provider.PlaybackStream{{
				Format: provider.PlaybackDirect,
				Audio: &provider.PlaybackTrack{
					ContentType: "audio/ogg",
					Resource:    provider.PlaybackResource{URLs: []string{"https://cdn.example/track.ogg"}},
				},
			}},
		},
		Transport: tr,
	})
	var perr *provider.Error
	if !errors.As(err, &perr) || perr.Code != provider.Unsupported {
		t.Fatalf("err = %v, want Unsupported", err)
	}
}

func TestOpenMediaAudioOnlyHLS(t *testing.T) {
	tr := &audioRangeTransport{
		payload:     []byte("#EXTM3U\n#EXT-X-VERSION:3\n#EXTINF:4.0,\nseg0.aac\n#EXT-X-ENDLIST\n"),
		contentType: "application/vnd.apple.mpegurl",
	}
	media, err := OpenMedia(context.Background(), provider.Playback{
		Plan: provider.PlaybackPlan{
			Duration: 60,
			Streams: []provider.PlaybackStream{{
				Format: provider.PlaybackHLS,
				Audio: &provider.PlaybackTrack{
					ContentType: "application/vnd.apple.mpegurl",
					Resource:    provider.PlaybackResource{URLs: []string{"https://cdn.example/audio.m3u8"}},
				},
			}},
		},
		Transport: tr,
	})
	if err != nil {
		t.Fatalf("OpenMedia audio HLS failed: %v", err)
	}
	defer media.Close()
	if !media.AudioOnly {
		t.Fatal("expected AudioOnly=true for audio-only HLS")
	}
	if !strings.HasSuffix(media.URL, "/stream.m3u8") {
		t.Fatalf("URL = %q, want /stream.m3u8", media.URL)
	}
}
