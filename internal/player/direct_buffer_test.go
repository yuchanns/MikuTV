package player

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yuchanns/MikuTV/internal/provider"
)

type directMockTransport struct {
	data         []byte
	contentType  string
	dropFirstAt  int64 // if > 0, first upstream response drops after this many bytes
	unknownLen   bool
	mu           sync.Mutex
	openCount    int
	openRanges   []string
	droppedFirst atomic.Bool
}

func (t *directMockTransport) OpenResource(_ context.Context, _ provider.PlaybackResource, method, byteRange string) (*http.Response, error) {
	t.mu.Lock()
	t.openCount++
	t.openRanges = append(t.openRanges, method+" "+byteRange)
	t.mu.Unlock()

	if t.unknownLen {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{t.contentType}},
			Body:       io.NopCloser(bytes.NewReader(t.data)),
		}, nil
	}

	total := int64(len(t.data))
	first, last, partial, ok := parseDirectByteRange(byteRange, total)
	if !ok {
		return &http.Response{
			StatusCode: http.StatusRequestedRangeNotSatisfiable,
			Header:     http.Header{"Content-Range": []string{fmt.Sprintf("bytes */%d", total)}},
			Body:       io.NopCloser(bytes.NewReader(nil)),
		}, nil
	}
	slice := t.data[first : last+1]
	if t.dropFirstAt > 0 && t.droppedFirst.CompareAndSwap(false, true) && int64(len(slice)) > t.dropFirstAt {
		slice = slice[:t.dropFirstAt]
	}
	hdr := http.Header{
		"Content-Type":   []string{t.contentType},
		"Accept-Ranges":  []string{"bytes"},
		"Content-Length": []string{strconv.FormatInt(last-first+1, 10)},
	}
	status := http.StatusOK
	if partial {
		status = http.StatusPartialContent
		hdr.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", first, last, total))
	}
	return &http.Response{
		StatusCode:    status,
		ContentLength: last - first + 1,
		Header:        hdr,
		Body:          io.NopCloser(bytes.NewReader(slice)),
	}, nil
}

func (t *directMockTransport) opens() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.openCount
}

func makePatternBytes(size int) []byte {
	b := make([]byte, size)
	for i := range b {
		b[i] = byte((i*31 + 17) & 0xff)
	}
	return b
}

func TestDirectAudioBufferSingleUpstreamAndReconnect(t *testing.T) {
	payload := makePatternBytes(900 << 10) // 900 KiB (~3.5 chunks)

	// 1. Single continuous upstream fetch serves probe, sequential Range reads, and full replay from RAM.
	tr := &directMockTransport{data: payload, contentType: "audio/flac"}
	media, err := OpenMedia(context.Background(), provider.Playback{
		Plan: provider.PlaybackPlan{
			Streams: []provider.PlaybackStream{{
				Format: provider.PlaybackDirect,
				Audio: &provider.PlaybackTrack{
					ContentType: "audio/flac",
					Resource:    provider.PlaybackResource{URLs: []string{"https://cdn.example/track.flac"}},
				},
			}},
		},
		Transport: tr,
	})
	if err != nil {
		t.Fatalf("OpenMedia: %v", err)
	}
	defer media.Close()
	if !media.AudioOnly {
		t.Fatal("expected AudioOnly=true")
	}

	readRange := func(rng string) ([]byte, *http.Response) {
		t.Helper()
		req, _ := http.NewRequest(http.MethodGet, media.URL, nil)
		if rng != "" {
			req.Header.Set("Range", rng)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("GET %q: %v", rng, err)
		}
		defer resp.Body.Close()
		got, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("read %q: %v", rng, err)
		}
		return got, resp
	}

	// Typical AVPlayer probe sequence: 0-1, 0-65535, full sequential ranges, then RepeatOne replay.
	b01, r01 := readRange("bytes=0-1")
	if r01.StatusCode != http.StatusPartialContent || !bytes.Equal(b01, payload[:2]) {
		t.Fatalf("probe 0-1 failed: status=%d len=%d", r01.StatusCode, len(b01))
	}
	if cr := r01.Header.Get("Content-Range"); cr != fmt.Sprintf("bytes 0-1/%d", len(payload)) {
		t.Fatalf("unexpected Content-Range %q", cr)
	}
	bHdr, _ := readRange("bytes=0-65535")
	if !bytes.Equal(bHdr, payload[:65536]) {
		t.Fatal("header range mismatch")
	}
	bMid, _ := readRange("bytes=200000-550000")
	if !bytes.Equal(bMid, payload[200000:550001]) {
		t.Fatal("mid range mismatch")
	}
	bFull, rFull := readRange("")
	if rFull.StatusCode != http.StatusOK || !bytes.Equal(bFull, payload) {
		t.Fatalf("full replay mismatch: status=%d len=%d", rFull.StatusCode, len(bFull))
	}
	if opens := tr.opens(); opens != 1 {
		t.Fatalf("expected 1 single upstream open for buffered track, got %d (%v)", opens, tr.openRanges)
	}

	// 2. Mid-stream CDN drop automatically resumes with Range without failing the reader.
	dropTr := &directMockTransport{
		data:        payload,
		contentType: "audio/mpeg",
		dropFirstAt: 300 << 10,
	}
	media2, err := OpenMedia(context.Background(), provider.Playback{
		Plan: provider.PlaybackPlan{
			Streams: []provider.PlaybackStream{{
				Format: provider.PlaybackDirect,
				Audio: &provider.PlaybackTrack{
					ContentType: "audio/mpeg",
					Resource:    provider.PlaybackResource{URLs: []string{"https://cdn.example/drop.mp3"}},
				},
			}},
		},
		Transport: dropTr,
	})
	if err != nil {
		t.Fatalf("OpenMedia(drop): %v", err)
	}
	defer media2.Close()

	resp, err := http.Get(media2.URL)
	if err != nil {
		t.Fatalf("GET drop stream: %v", err)
	}
	got, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("expected seamless resume across upstream drop: err=%v len=%d/%d", err, len(got), len(payload))
	}
	if opens := dropTr.opens(); opens != 2 {
		t.Fatalf("expected 2 upstream opens (initial + resume), got %d", opens)
	}
}

func TestDirectBufferWarmupPromoteAndUnknownLengthFallback(t *testing.T) {
	payload := makePatternBytes(1500 << 10) // 1.5 MiB
	tr := &directMockTransport{data: payload, contentType: "audio/mpeg"}
	media, err := OpenMedia(context.Background(), provider.Playback{
		Plan: provider.PlaybackPlan{
			Streams: []provider.PlaybackStream{{
				Format: provider.PlaybackDirect,
				Audio: &provider.PlaybackTrack{
					ContentType: "audio/mpeg",
					Resource:    provider.PlaybackResource{URLs: []string{"https://cdn.example/warm.mp3"}},
				},
			}},
		},
		Transport: tr,
	})
	if err != nil {
		t.Fatalf("OpenMedia: %v", err)
	}
	defer media.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := media.Warmup(ctx); err != nil {
		t.Fatalf("Warmup: %v", err)
	}
	media.Promote()

	resp, err := http.Get(media.URL)
	if err != nil {
		t.Fatalf("GET warmed track: %v", err)
	}
	got, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("warmed payload mismatch: err=%v len=%d/%d", err, len(got), len(payload))
	}
	if opens := tr.opens(); opens != 1 {
		t.Fatalf("expected Warmup + Promote + GET to reuse 1 upstream open, got %d", opens)
	}

	// Unknown Content-Length cleanly falls back to passthrough without hanging.
	uk := &directMockTransport{data: []byte("ID3-stream"), contentType: "audio/mpeg", unknownLen: true}
	media3, err := OpenMedia(context.Background(), provider.Playback{
		Plan: provider.PlaybackPlan{
			Streams: []provider.PlaybackStream{{
				Format: provider.PlaybackDirect,
				Audio: &provider.PlaybackTrack{
					ContentType: "audio/mpeg",
					Resource:    provider.PlaybackResource{URLs: []string{"https://cdn.example/live.mp3"}},
				},
			}},
		},
		Transport: uk,
	})
	if err != nil {
		t.Fatalf("OpenMedia(unknownLen): %v", err)
	}
	defer media3.Close()
	resp3, err := http.Get(media3.URL)
	if err != nil {
		t.Fatalf("GET unknownLen: %v", err)
	}
	got3, err := io.ReadAll(resp3.Body)
	resp3.Body.Close()
	if err != nil || !strings.HasPrefix(string(got3), "ID3-stream") {
		t.Fatalf("unknownLen passthrough failed: err=%v got=%q", err, string(got3))
	}
}

type directReadTrackingTransport struct {
	directMockTransport
	readBytes int64
}

type trackingReadCloser struct {
	rc    io.ReadCloser
	count *int64
}

func (t *trackingReadCloser) Read(p []byte) (int, error) {
	n, err := t.rc.Read(p)
	if n > 0 {
		atomic.AddInt64(t.count, int64(n))
	}
	return n, err
}

func (t *trackingReadCloser) Close() error { return t.rc.Close() }

func (tr *directReadTrackingTransport) OpenResource(ctx context.Context, res provider.PlaybackResource, method, byteRange string) (*http.Response, error) {
	resp, err := tr.directMockTransport.OpenResource(ctx, res, method, byteRange)
	if err != nil || resp == nil || resp.Body == nil || method == http.MethodHead {
		return resp, err
	}
	resp.Body = &trackingReadCloser{rc: resp.Body, count: &tr.readBytes}
	return resp, nil
}

func TestDirectBufferForwardBackpressureAndBackwardSeekIntoEvictedBytes(t *testing.T) {
	// 24 MiB video exceeds the 16 MiB directVideoBudget so both forward backpressure
	// and backward-seek recovery into evicted chunks are exercised deterministically.
	const total = 24 << 20
	payload := makePatternBytes(total)
	tr := &directReadTrackingTransport{
		directMockTransport: directMockTransport{data: payload, contentType: "video/mp4"},
	}
	media, err := OpenMedia(context.Background(), provider.Playback{
		Plan: provider.PlaybackPlan{
			Streams: []provider.PlaybackStream{{
				Format: provider.PlaybackDirect,
				Video: provider.PlaybackTrack{
					ContentType: "video/mp4",
					Resource:    provider.PlaybackResource{URLs: []string{"https://cdn.example/movie.mp4"}},
				},
			}},
		},
		Transport: tr,
	})
	if err != nil {
		t.Fatalf("OpenMedia: %v", err)
	}
	defer media.Close()

	fetchRange := func(start, end int64) []byte {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, media.URL, nil)
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("GET range %d-%d: %v", start, end, err)
		}
		defer resp.Body.Close()
		b, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("ReadAll range %d-%d: %v", start, end, err)
		}
		if !bytes.Equal(b, payload[start:end+1]) {
			t.Fatalf("range %d-%d content mismatch", start, end)
		}
		return b
	}

	// 1. Read only the first 1 MiB and let the pump run briefly; backpressure must keep
	// upstream downloads bounded by the 16 MiB budget instead of draining all 24 MiB.
	fetchRange(0, (1<<20)-1)
	time.Sleep(150 * time.Millisecond)
	if downloaded := atomic.LoadInt64(&tr.readBytes); downloaded >= total-directChunkSize {
		t.Fatalf("forward backpressure broken: downloaded %d of %d bytes after reading only 1 MiB", downloaded, total)
	}

	// 2. Advance sequentially past 20 MiB so early chunks (e.g. offset 2 MiB) are evicted.
	fetchRange(1<<20, (20<<20)-1)

	// 3. Seek backward into an evicted chunk between pumpStart (0) and pumpOffset (>=20 MiB).
	// The pump must restart cleanly and serve exact bytes within timeout.
	fetchRange(2<<20, (3<<20)-1)
}

type directExpireOnResumeTransport struct {
	directMockTransport
	expireAfterFirstOpen atomic.Bool
}

func (tr *directExpireOnResumeTransport) OpenResource(ctx context.Context, res provider.PlaybackResource, method, byteRange string) (*http.Response, error) {
	if method != http.MethodHead && tr.expireAfterFirstOpen.Swap(true) {
		return nil, &provider.Error{Code: provider.Expired}
	}
	return tr.directMockTransport.OpenResource(ctx, res, method, byteRange)
}

func TestDirectBufferPropagatesPostProbeExpiredFailure(t *testing.T) {
	payload := makePatternBytes(2 << 20)
	tr := &directExpireOnResumeTransport{
		directMockTransport: directMockTransport{
			data:        payload,
			contentType: "audio/mpeg",
			dropFirstAt: 512 << 10, // drop first open after 512 KiB; resume returns provider.Expired
		},
	}
	media, err := OpenMedia(context.Background(), provider.Playback{
		Plan: provider.PlaybackPlan{
			Streams: []provider.PlaybackStream{{
				Format: provider.PlaybackDirect,
				Audio: &provider.PlaybackTrack{
					ContentType: "audio/mpeg",
					Resource:    provider.PlaybackResource{URLs: []string{"https://cdn.example/expiring.mp3"}},
				},
			}},
		},
		Transport: tr,
	})
	if err != nil {
		t.Fatalf("OpenMedia: %v", err)
	}
	defer media.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, media.URL, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET expiring stream: %v", err)
	}
	_, _ = io.ReadAll(resp.Body)
	resp.Body.Close()

	if got := media.Failure(); got != provider.Expired {
		t.Fatalf("media.Failure() = %q, want %q", got, provider.Expired)
	}
}

type directIgnoreRangeTransport struct {
	data  []byte
	opens atomic.Int32
}

func (tr *directIgnoreRangeTransport) OpenResource(_ context.Context, _ provider.PlaybackResource, method, _ string) (*http.Response, error) {
	if method != http.MethodHead {
		tr.opens.Add(1)
	}
	return &http.Response{
		StatusCode:    http.StatusOK,
		ContentLength: int64(len(tr.data)),
		Header: http.Header{
			"Content-Type":   []string{"audio/mpeg"},
			"Content-Length": []string{strconv.Itoa(len(tr.data))},
		},
		Body: io.NopCloser(bytes.NewReader(tr.data)),
	}, nil
}

func TestDirectBufferHandlesHTTP200OnNonZeroRangeSeek(t *testing.T) {
	// Server ignores Range on a >4 MiB seek and returns 200 OK with full Content-Length.
	// Pump must recognize totalSize, restart cleanly from byte 0, and serve the sought range
	// without looping upstream opens.
	const total = 6 << 20
	payload := makePatternBytes(total)
	tr := &directIgnoreRangeTransport{data: payload}
	media, err := OpenMedia(context.Background(), provider.Playback{
		Plan: provider.PlaybackPlan{
			Streams: []provider.PlaybackStream{{
				Format: provider.PlaybackDirect,
				Audio: &provider.PlaybackTrack{
					ContentType: "audio/mpeg",
					Resource:    provider.PlaybackResource{URLs: []string{"https://cdn.example/full200.mp3"}},
				},
			}},
		},
		Transport: tr,
	})
	if err != nil {
		t.Fatalf("OpenMedia: %v", err)
	}
	defer media.Close()

	// Jump directly to 5 MiB (> directSeekJumpThreshold = 4 MiB) before byte 0 finishes.
	start, end := int64(5<<20), int64(total-1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, media.URL, nil)
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET range on full-200 server: %v", err)
	}
	got, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil || !bytes.Equal(got, payload[start:end+1]) {
		t.Fatalf("full-200 seek mismatch: err=%v len=%d/%d", err, len(got), end-start+1)
	}
	if opens := tr.opens.Load(); opens > 3 {
		t.Fatalf("expected bounded upstream opens on full-200 seek, got %d", opens)
	}
}
