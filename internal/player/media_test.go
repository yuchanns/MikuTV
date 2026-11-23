package player

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yuchanns/MikuTV/internal/provider"
)

func TestDASHPlaylistPreservesHDRAndByteRanges(t *testing.T) {
	// The DASH initialization range is followed by a v1 sidx. The first
	// referenced fragment starts four bytes after that box.
	initial := []byte{0, 0, 0, 19, 'c', 'o', 'l', 'r', 'n', 'c', 'l', 'x', 0, 9, 0, 16, 0, 9, 0}
	box := make([]byte, 52)
	binary.BigEndian.PutUint32(box, uint32(len(box)))
	copy(box[4:], "sidx")
	box[8] = 1
	binary.BigEndian.PutUint32(box[16:], 1000)
	binary.BigEndian.PutUint64(box[28:], 4)
	binary.BigEndian.PutUint16(box[38:], 1)
	binary.BigEndian.PutUint32(box[40:], 12)
	binary.BigEndian.PutUint32(box[44:], 3501)
	movie := append(append(append(bytes.Clone(initial), box...), make([]byte, 4)...), []byte("abcdefghijkl")...)
	resource := provider.PlaybackResource{URLs: []string{"https://media.example.com/track"}}
	video := provider.PlaybackTrack{
		Resource: resource, Bandwidth: 16384, Width: 3840, Height: 2160, Codec: "hvc1.2.4.L150.B0",
		Init: &provider.PlaybackRange{First: 0, Last: 18}, Index: &provider.PlaybackRange{First: 19, Last: 70},
	}
	presentation := provider.Playback{
		Transport: testResource(movie),
		Plan:      provider.PlaybackPlan{Streams: []provider.PlaybackStream{{Format: provider.PlaybackDASH, Video: video}}},
	}
	media, err := OpenMedia(context.Background(), presentation)
	if err != nil {
		t.Fatal(err)
	}
	defer media.Close()
	master := readFixture(t, media.URL, "")
	if !strings.Contains(master, "#EXT-X-INDEPENDENT-SEGMENTS") ||
		!strings.Contains(master, "VIDEO-RANGE=PQ") || !strings.Contains(master, "RESOLUTION=3840x2160") {
		t.Fatalf("HDR variant lost metadata: %s", master)
	}
	base := strings.TrimSuffix(media.URL, "stream.m3u8")
	playlist := readFixture(t, base+"video.m3u8", "")
	for _, expected := range []string{"#EXT-X-TARGETDURATION:4", "BYTERANGE=\"19@0\"", "#EXT-X-BYTERANGE:12@75"} {
		if !strings.Contains(playlist, expected) {
			t.Fatalf("%q is missing from %s", expected, playlist)
		}
	}
	if got := readFixture(t, base+"video.mp4", "bytes=75-86"); got != "abcdefghijkl" {
		t.Fatalf("fragment contents = %q", got)
	}
	if rate := media.BytesPerSecond(time.Now()); rate <= 0 {
		t.Fatalf("media BytesPerSecond = %f, want > 0 after reads", rate)
	}
}

func TestMediaSpeedMeterTracksTransferAndDecaysToZero(t *testing.T) {
	var nilMedia *Media
	if got := nilMedia.BytesPerSecond(time.Now()); got != 0 {
		t.Fatalf("nil media speed = %f, want 0", got)
	}
	base := time.Unix(1000, 0)
	meter := speedMeter{origin: base}
	// Initial burst over 100ms clamps denominator to speedMinWindow (250ms).
	meter.record(512*1024, base)
	if got := meter.bytesPerSecond(base.Add(100 * time.Millisecond)); got < 1.9*1024*1024 || got > 2.1*1024*1024 {
		t.Fatalf("initial burst rate = %.0f B/s, want ~2 MiB/s", got)
	}
	// Steady 1 MiB/s over 1.5s (100 KiB every 100ms).
	steady := speedMeter{origin: base}
	for i := range 15 {
		steady.record(102400, base.Add(time.Duration(i)*100*time.Millisecond))
	}
	atSteady := steady.bytesPerSecond(base.Add(1450 * time.Millisecond))
	if atSteady < 950*1024 || atSteady > 1100*1024 {
		t.Fatalf("steady rate = %.0f B/s, want ~1024 KiB/s", atSteady)
	}
	// After 1.6s with no new bytes, sliding window expires cleanly to 0.
	if stalled := steady.bytesPerSecond(base.Add(3100 * time.Millisecond)); stalled != 0 {
		t.Fatalf("stalled rate = %.0f B/s, want 0", stalled)
	}
}

func TestSegmentIndexRejectsNestedReferenceAndTruncation(t *testing.T) {
	box := make([]byte, 44)
	binary.BigEndian.PutUint32(box, uint32(len(box)))
	copy(box[4:], "sidx")
	binary.BigEndian.PutUint32(box[16:], 1000)
	binary.BigEndian.PutUint16(box[30:], 1)
	binary.BigEndian.PutUint32(box[32:], 0x80000004)
	binary.BigEndian.PutUint32(box[36:], 1000)
	if _, err := segmentIndex(box, 0); err == nil {
		t.Fatal("expected an error for an unsupported nested reference")
	}
	if _, err := segmentIndex(box[:40], 0); err == nil {
		t.Fatal("expected an error for a truncated box")
	}
}

func TestVideoRangeDetectsNCLCAndHVCCAndDolbyVision(t *testing.T) {
	nclcPQ := []byte{0, 0, 0, 18, 'c', 'o', 'l', 'r', 'n', 'c', 'l', 'c', 0, 9, 0, 16, 0, 9}
	if got := videoRange(nclcPQ, "hvc1.2.4.L153.0"); got != "PQ" {
		t.Fatalf("nclc PQ = %q, want PQ", got)
	}
	nclcHLG := []byte{0, 0, 0, 18, 'c', 'o', 'l', 'r', 'n', 'c', 'l', 'c', 0, 9, 0, 18, 0, 9}
	if got := videoRange(nclcHLG, "hvc1.2.4.L153.0"); got != "HLG" {
		t.Fatalf("nclc HLG = %q, want HLG", got)
	}
	if got := videoRange([]byte("xxxxdvcCxxxx"), "hvc1.2.4.L153.0"); got != "PQ" {
		t.Fatalf("dvcC = %q, want PQ", got)
	}

	makeHVCC := func(transfer byte) []byte {
		// Minimal valid HEVC SPS RBSP with VUI colour_description_present_flag=1.
		var bits []byte
		addBits := func(v uint32, n int) {
			for i := n - 1; i >= 0; i-- {
				bits = append(bits, byte((v>>i)&1))
			}
		}
		addUE := func(v uint32) {
			code := v + 1
			length := 0
			for c := code; c > 1; c >>= 1 {
				length++
			}
			addBits(0, length)
			addBits(code, length+1)
		}
		addBits(0, 4)  // vps_id
		addBits(0, 3)  // max_sub_layers_minus1
		addBits(1, 1)  // temporal_id_nesting
		addBits(0, 32) // PTL 96 bits
		addBits(0, 32)
		addBits(0, 32)
		addUE(0)       // sps_id
		addUE(1)       // chroma_format_idc = 4:2:0
		addUE(3840)    // width
		addUE(2160)    // height
		addBits(0, 1)  // conformance_window_flag
		addUE(2)       // bit_depth_luma_minus8 = 10-bit
		addUE(2)       // bit_depth_chroma_minus8 = 10-bit
		addUE(4)       // log2_max_pic_order_cnt_lsb_minus4
		addBits(0, 1)  // sub_layer_ordering_info_present_flag
		addUE(1)       // max_dec_pic_buffering
		addUE(0)       // max_num_reorder_pics
		addUE(0)       // max_latency_increase
		for range 6 {
			addUE(0)
		}
		addBits(0, 1) // scaling_list_enabled
		addBits(0, 1) // amp_enabled
		addBits(0, 1) // sao_enabled
		addBits(0, 1) // pcm_enabled
		addUE(0)      // num_short_term_ref_pic_sets
		addBits(0, 1) // long_term_ref_pics_present
		addBits(0, 1) // temporal_mvp
		addBits(0, 1) // strong_intra_smoothing
		addBits(1, 1) // vui_parameters_present_flag
		addBits(0, 1) // aspect_ratio_info_present
		addBits(0, 1) // overscan_info_present
		addBits(1, 1) // video_signal_type_present_flag
		addBits(5, 3) // video_format
		addBits(0, 1) // video_full_range_flag
		addBits(1, 1) // colour_description_present_flag
		addBits(9, 8) // colour_primaries = BT.2020
		addBits(uint32(transfer), 8)
		addBits(9, 8) // matrix_coeffs = BT.2020

		for len(bits)%8 != 0 {
			bits = append(bits, 0)
		}
		rbsp := make([]byte, len(bits)/8)
		for i, b := range bits {
			if b != 0 {
				rbsp[i/8] |= 1 << (7 - (i % 8))
			}
		}
		nal := append([]byte{33 << 1, 1}, rbsp...)
		body := make([]byte, 23)
		body[0] = 1  // configurationVersion
		body[22] = 1 // numOfArrays
		body = append(body, 33)
		body = binary.BigEndian.AppendUint16(body, 1)
		body = binary.BigEndian.AppendUint16(body, uint16(len(nal)))
		body = append(body, nal...)
		box := make([]byte, 8+len(body))
		binary.BigEndian.PutUint32(box, uint32(len(box)))
		copy(box[4:8], "hvcC")
		copy(box[8:], body)
		return box
	}

	if got := videoRange(makeHVCC(16), "hvc1.2.4.L153.0"); got != "PQ" {
		t.Fatalf("hvcC PQ = %q, want PQ", got)
	}
	if got := videoRange(makeHVCC(18), "hvc1.2.4.L153.0"); got != "HLG" {
		t.Fatalf("hvcC HLG = %q, want HLG", got)
	}
	// Unspecified colr (transfer=2) should fall through to hvcC VUI.
	unspecColr := []byte{0, 0, 0, 19, 'c', 'o', 'l', 'r', 'n', 'c', 'l', 'x', 0, 2, 0, 2, 0, 2, 0}
	if got := videoRange(append(unspecColr, makeHVCC(16)...), "hvc1.2.4.L153.0"); got != "PQ" {
		t.Fatalf("unspecified colr + hvcC PQ = %q, want PQ", got)
	}
}

func TestMasterPlaylistDisambiguatesDuplicateAudioNames(t *testing.T) {
	stream := provider.PlaybackStream{
		Format: provider.PlaybackDASH,
		Video:  provider.PlaybackTrack{Codec: "hvc1.2.4.L153.90", Bandwidth: 8000000, Width: 3840, Height: 2160, FrameRate: 60},
	}
	tracks := []provider.PlaybackTrack{
		{Codec: "mp4a.40.2", Bandwidth: 96280, Label: "AAC 96k"},
		{Codec: "mp4a.40.2", Bandwidth: 96280, Label: "AAC 96k"},
		{Codec: "mp4a.40.2", Bandwidth: 65552, Label: "AAC 66k"},
	}
	got := masterPlaylist(stream, tracks, "PQ")
	if !strings.Contains(got, `NAME="AAC 96k",DEFAULT=YES,AUTOSELECT=YES,URI="audio.m3u8"`) ||
		!strings.Contains(got, `NAME="AAC 96k (2)",DEFAULT=NO,AUTOSELECT=NO,URI="audio-1.m3u8"`) ||
		!strings.Contains(got, `NAME="AAC 66k",DEFAULT=NO,AUTOSELECT=NO,URI="audio-2.m3u8"`) {
		t.Fatalf("unexpected master playlist:\n%s", got)
	}
}

type testResource []byte

func (data testResource) OpenResource(ctx context.Context, resource provider.PlaybackResource, method, byteRange string) (*http.Response, error) {
	request, _ := http.NewRequestWithContext(ctx, method, resource.URLs[0], nil)
	request.Header.Set("Range", byteRange)
	recorder := httptest.NewRecorder()
	http.ServeContent(recorder, request, "video.mp4", time.Time{}, bytes.NewReader(data))
	return recorder.Result(), nil
}

func readFixture(t *testing.T, address, byteRange string) string {
	t.Helper()
	request, _ := http.NewRequest(http.MethodGet, address, nil)
	if byteRange != "" {
		request.Header.Set("Range", byteRange)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	expected := http.StatusOK
	if byteRange != "" {
		expected = http.StatusPartialContent
	}
	if response.StatusCode != expected {
		t.Fatalf("expected HTTP %d, got %d", expected, response.StatusCode)
	}
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func buildMultiFragmentDASH(fragPayloads [][]byte, fragMillis uint32) ([]byte, provider.PlaybackRange, provider.PlaybackRange, []provider.PlaybackRange) {
	initial := []byte{0, 0, 0, 19, 'c', 'o', 'l', 'r', 'n', 'c', 'l', 'x', 0, 9, 0, 16, 0, 9, 0}
	box := make([]byte, 40+len(fragPayloads)*12)
	binary.BigEndian.PutUint32(box, uint32(len(box)))
	copy(box[4:], "sidx")
	box[8] = 1
	binary.BigEndian.PutUint32(box[16:], 1000)
	binary.BigEndian.PutUint64(box[28:], 0)
	binary.BigEndian.PutUint16(box[38:], uint16(len(fragPayloads)))
	for i, p := range fragPayloads {
		off := 40 + i*12
		binary.BigEndian.PutUint32(box[off:], uint32(len(p)))
		binary.BigEndian.PutUint32(box[off+4:], fragMillis)
	}
	initRange := provider.PlaybackRange{First: 0, Last: int64(len(initial) - 1)}
	indexRange := provider.PlaybackRange{First: initRange.Last + 1, Last: initRange.Last + int64(len(box))}
	movie := append(bytes.Clone(initial), box...)
	ranges := make([]provider.PlaybackRange, len(fragPayloads))
	cursor := indexRange.Last + 1
	for i, p := range fragPayloads {
		ranges[i] = provider.PlaybackRange{First: cursor, Last: cursor + int64(len(p)) - 1}
		movie = append(movie, p...)
		cursor += int64(len(p))
	}
	return movie, initRange, indexRange, ranges
}

type countingDASHTransport struct {
	data     []byte
	mu       sync.Mutex
	requests map[string]int
}

func (c *countingDASHTransport) OpenResource(ctx context.Context, resource provider.PlaybackResource, method, byteRange string) (*http.Response, error) {
	c.mu.Lock()
	if c.requests == nil {
		c.requests = make(map[string]int)
	}
	c.requests[byteRange]++
	c.mu.Unlock()
	request, _ := http.NewRequestWithContext(ctx, method, resource.URLs[0], nil)
	request.Header.Set("Range", byteRange)
	recorder := httptest.NewRecorder()
	http.ServeContent(recorder, request, "video.mp4", time.Time{}, bytes.NewReader(c.data))
	resp := recorder.Result()
	resp.Request = request
	return resp, nil
}

func (c *countingDASHTransport) count(byteRange string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.requests[byteRange]
}

func TestDASHPrefetchCachesForwardSegmentsAndWarmupPromotes(t *testing.T) {
	frags := [][]byte{
		bytes.Repeat([]byte("0"), 4096),
		bytes.Repeat([]byte("1"), 4096),
		bytes.Repeat([]byte("2"), 4096),
		bytes.Repeat([]byte("3"), 4096),
		bytes.Repeat([]byte("4"), 4096),
	}
	movie, initR, idxR, ranges := buildMultiFragmentDASH(frags, 4000)
	transport := &countingDASHTransport{data: movie}
	video := provider.PlaybackTrack{
		Resource: provider.PlaybackResource{URLs: []string{"https://cdn1.example.com/v.m4s"}},
		Bandwidth: 8_000_000, Width: 3840, Height: 2160, Codec: "hvc1.2.4.L150.B0",
		Init: &initR, Index: &idxR,
	}
	media, err := OpenMedia(context.Background(), provider.Playback{
		Transport: transport,
		Plan:      provider.PlaybackPlan{Streams: []provider.PlaybackStream{{Format: provider.PlaybackDASH, Video: video}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer media.Close()

	// Warmup should pre-seed init + opening fragments into cache.
	if err := media.Warmup(context.Background()); err != nil {
		t.Fatalf("Warmup: %v", err)
	}
	if budget, _ := media.cache.limits(); budget != warmupCacheBudget {
		t.Fatalf("warmup budget = %d, want %d", budget, warmupCacheBudget)
	}
	media.Promote()
	if budget, _ := media.cache.limits(); budget != hdrCacheBudget {
		t.Fatalf("promoted HDR budget = %d, want %d", budget, hdrCacheBudget)
	}

	base := strings.TrimSuffix(media.URL, "stream.m3u8")
	// Init was already seeded by OpenMedia -> total upstream init reads stays 1.
	initSpec := fmt.Sprintf("bytes=%d-%d", initR.First, initR.Last)
	if got := readFixture(t, base+"video.mp4", initSpec); len(got) != int(initR.Last-initR.First+1) {
		t.Fatalf("init len = %d", len(got))
	}
	if got := transport.count(initSpec); got != 1 {
		t.Fatalf("init fetched %d times, want 1 (seeded from OpenMedia)", got)
	}

	// Reading fragment 0 should serve from warmup cache and trigger background prefetch of fragments 2..4.
	for i := range 3 {
		spec := fmt.Sprintf("bytes=%d-%d", ranges[i].First, ranges[i].Last)
		if got := readFixture(t, base+"video.mp4", spec); got != string(frags[i]) {
			t.Fatalf("fragment %d payload mismatch", i)
		}
	}
	// Wait briefly for background prefetch of fragments 3 and 4 to complete.
	deadline := time.Now().Add(2 * time.Second)
	spec4 := fmt.Sprintf("bytes=%d-%d", ranges[4].First, ranges[4].Last)
	for time.Now().Before(deadline) && transport.count(spec4) == 0 {
		time.Sleep(15 * time.Millisecond)
	}
	if transport.count(spec4) != 1 {
		t.Fatalf("expected fragment 4 to be prefetched in background, count=%d", transport.count(spec4))
	}
	// Foreground read of fragment 4 must hit cache without a second upstream fetch.
	if got := readFixture(t, base+"video.mp4", spec4); got != string(frags[4]) {
		t.Fatalf("fragment 4 payload mismatch")
	}
	if transport.count(spec4) != 1 {
		t.Fatalf("fragment 4 fetched %d times, want 1 (cache hit)", transport.count(spec4))
	}
}

type slowPrimaryTransport struct {
	data       []byte
	slowHost   string
	mu         sync.Mutex
	penalized  map[string]bool
	slowHits   atomic.Int32
	backupHits atomic.Int32
}

func (s *slowPrimaryTransport) ReportSlow(rawURL string) {
	s.mu.Lock()
	if s.penalized == nil {
		s.penalized = make(map[string]bool)
	}
	s.penalized[rawURL] = true
	s.mu.Unlock()
}

func (s *slowPrimaryTransport) HasHealthyAlternative(urls []string, currentURL string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, u := range urls {
		if u != currentURL && !s.penalized[u] {
			return true
		}
	}
	return false
}

func (s *slowPrimaryTransport) OpenResource(ctx context.Context, resource provider.PlaybackResource, method, byteRange string) (*http.Response, error) {
	chosen := resource.URLs[0]
	s.mu.Lock()
	if s.penalized[chosen] && len(resource.URLs) > 1 {
		chosen = resource.URLs[1]
	}
	s.mu.Unlock()

	rng, ok := parseSingleByteRange(byteRange)
	if !ok {
		return nil, fmt.Errorf("unexpected range %q", byteRange)
	}
	slice := s.data[rng.First : rng.Last+1]
	req, _ := http.NewRequestWithContext(ctx, method, chosen, nil)
	hdr := http.Header{
		"Accept-Ranges":  {"bytes"},
		"Content-Type":   {"video/mp4"},
		"Content-Length": {fmt.Sprint(len(slice))},
		"Content-Range":  {fmt.Sprintf("bytes %d-%d/%d", rng.First, rng.Last, len(s.data))},
	}
	if strings.Contains(chosen, s.slowHost) && len(slice) >= 4096 {
		s.slowHits.Add(1)
		pr, pw := io.Pipe()
		go func() {
			// Deliver first half, then stall until attempt context is canceled.
			half := len(slice) / 2
			_, _ = pw.Write(slice[:half])
			<-ctx.Done()
			_ = pw.CloseWithError(ctx.Err())
		}()
		return &http.Response{StatusCode: http.StatusPartialContent, Header: hdr, Body: pr, Request: req}, nil
	}
	if !strings.Contains(chosen, s.slowHost) {
		s.backupHits.Add(1)
	}
	return &http.Response{StatusCode: http.StatusPartialContent, Header: hdr, Body: io.NopCloser(bytes.NewReader(slice)), Request: req}, nil
}

func TestDASHMidSegmentSlowCDNFailoverResumesSeamlessly(t *testing.T) {
	oldMin, oldStall, oldInterval := slowReadMinAge, slowStallTimeout, slowStallCheckInterval
	slowReadMinAge = 40 * time.Millisecond
	slowStallTimeout = 80 * time.Millisecond
	slowStallCheckInterval = 10 * time.Millisecond
	t.Cleanup(func() {
		slowReadMinAge = oldMin
		slowStallTimeout = oldStall
		slowStallCheckInterval = oldInterval
	})

	payload := make([]byte, 8192)
	for i := range payload {
		payload[i] = byte('A' + (i % 26))
	}
	movie, initR, idxR, ranges := buildMultiFragmentDASH([][]byte{payload}, 4000)
	transport := &slowPrimaryTransport{data: movie, slowHost: "slow.example.com"}
	video := provider.PlaybackTrack{
		Resource: provider.PlaybackResource{URLs: []string{
			"https://slow.example.com/v.m4s",
			"https://fast.example.com/v.m4s",
		}},
		Bandwidth: 16_000_000, Width: 3840, Height: 2160, Codec: "hvc1.2.4.L150.B0",
		Init: &initR, Index: &idxR,
	}
	media, err := OpenMedia(context.Background(), provider.Playback{
		Transport: transport,
		Plan:      provider.PlaybackPlan{Streams: []provider.PlaybackStream{{Format: provider.PlaybackDASH, Video: video}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer media.Close()

	base := strings.TrimSuffix(media.URL, "stream.m3u8")
	spec := fmt.Sprintf("bytes=%d-%d", ranges[0].First, ranges[0].Last)
	got := readFixture(t, base+"video.mp4", spec)
	if got != string(payload) {
		t.Fatalf("failover segment corrupted: got len=%d want len=%d", len(got), len(payload))
	}
	if transport.slowHits.Load() != 1 || transport.backupHits.Load() != 1 {
		t.Fatalf("expected 1 slow hit + 1 backup resume hit, got slow=%d backup=%d",
			transport.slowHits.Load(), transport.backupHits.Load())
	}
}

type failingTrackTransport struct {
	data    []byte
	failURL string
}

func (f *failingTrackTransport) OpenResource(ctx context.Context, resource provider.PlaybackResource, method, byteRange string) (*http.Response, error) {
	if len(resource.URLs) > 0 && resource.URLs[0] == f.failURL {
		return nil, &provider.Error{Code: provider.Network}
	}
	return testResource(f.data).OpenResource(ctx, resource, method, byteRange)
}

func TestDASHMultiAudioSelectionCodecIsolationAndSecondaryResilience(t *testing.T) {
	movie, initR, idxR, _ := buildMultiFragmentDASH([][]byte{[]byte("abcdefghijkl")}, 3000)
	transport := &failingTrackTransport{data: movie, failURL: "https://media.example.com/broken.m4s"}
	res := func(name string) provider.PlaybackResource {
		return provider.PlaybackResource{URLs: []string{"https://media.example.com/" + name}}
	}
	audioTracks := []provider.PlaybackTrack{
		{ID: "aac-192", Label: "AAC 192k", Default: true, Codec: "mp4a.40.2", Bandwidth: 192000, Resource: res("a192.m4s"), Init: &initR, Index: &idxR},
		{ID: "dolby", Label: "Dolby Atmos", Codec: "ec-3", Bandwidth: 448000, Resource: res("dolby.m4s"), Init: &initR, Index: &idxR},
		{ID: "aac-132", Label: "AAC 132k", Codec: "mp4a.40.2", Bandwidth: 132000, Resource: res("a132.m4s"), Init: &initR, Index: &idxR},
		{ID: "opus", Label: "Opus", Codec: "opus", Bandwidth: 128000, Resource: res("opus.m4s"), Init: &initR, Index: &idxR},
		{ID: "broken", Label: "Broken AAC", Codec: "mp4a.40.2", Bandwidth: 64000, Resource: res("broken.m4s"), Init: &initR, Index: &idxR},
	}
	playback := provider.Playback{
		Transport: transport,
		Plan: provider.PlaybackPlan{
			Streams: []provider.PlaybackStream{{
				Format:      provider.PlaybackDASH,
				Video:       provider.PlaybackTrack{Codec: "hvc1.1.6.L150.90", Bandwidth: 4000000, Width: 1920, Height: 1080, Resource: res("v.m4s"), Init: &initR, Index: &idxR},
				Audio:       &audioTracks[0],
				AudioTracks: audioTracks,
			}},
		},
	}

	media, err := OpenMediaWithAudio(context.Background(), playback, "")
	if err != nil {
		t.Fatal(err)
	}
	defer media.Close()

	// Unsupported opus and failing secondary track are cleanly omitted; default is aac-192.
	tracks := media.AudioTracks()
	if len(tracks) != 3 || tracks[0].ID != "aac-192" || tracks[1].ID != "dolby" || tracks[2].ID != "aac-132" {
		t.Fatalf("unexpected prepared audio tracks: %+v", tracks)
	}
	if media.DefaultAudioID() != "aac-192" || media.ActiveAudioID() != "aac-192" {
		t.Fatalf("default=%q active=%q, want aac-192", media.DefaultAudioID(), media.ActiveAudioID())
	}
	// Same codec (AAC <-> AAC) supports in-place switching; cross-codec (AAC <-> Dolby ec-3) does not.
	if !media.CanSwitchAudioInPlace("aac-192", "aac-132") {
		t.Fatal("expected same-codec AAC tracks to allow in-place switching")
	}
	if media.CanSwitchAudioInPlace("aac-192", "dolby") {
		t.Fatal("expected cross-codec AAC <-> ec-3 to require reload rather than in-place switching")
	}
	master := readFixture(t, media.URL, "")
	if !strings.Contains(master, `NAME="AAC 192k",DEFAULT=YES,AUTOSELECT=YES`) ||
		!strings.Contains(master, `NAME="AAC 132k",DEFAULT=NO,AUTOSELECT=NO`) ||
		strings.Contains(master, `Dolby Atmos`) ||
		!strings.Contains(master, `CODECS="hvc1.1.6.L150.90,mp4a.40.2"`) {
		t.Fatalf("master playlist should isolate AAC codec group cleanly:\n%s", master)
	}

	// Opening with Dolby selected emits ec-3 codec group cleanly.
	dolbyMedia, err := OpenMediaWithAudio(context.Background(), playback, "dolby")
	if err != nil {
		t.Fatal(err)
	}
	defer dolbyMedia.Close()
	if dolbyMedia.ActiveAudioID() != "dolby" || dolbyMedia.DefaultAudioID() != "aac-192" {
		t.Fatalf("dolby media active=%q default=%q", dolbyMedia.ActiveAudioID(), dolbyMedia.DefaultAudioID())
	}
	dolbyMaster := readFixture(t, dolbyMedia.URL, "")
	if !strings.Contains(dolbyMaster, `NAME="Dolby Atmos",DEFAULT=YES,AUTOSELECT=YES`) ||
		!strings.Contains(dolbyMaster, `CODECS="hvc1.1.6.L150.90,ec-3"`) ||
		strings.Contains(dolbyMaster, `AAC 192k`) {
		t.Fatalf("dolby master playlist should isolate ec-3 codec group cleanly:\n%s", dolbyMaster)
	}
}
