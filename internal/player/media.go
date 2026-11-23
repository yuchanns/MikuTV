package player

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yuchanns/MikuTV/internal/provider"
)

const (
	speedBucketWidth = 100 * time.Millisecond
	speedBucketCount = 15
	speedMinWindow   = 250 * time.Millisecond
	hlsMaxChildren   = 4096
)

type hlsChildEntry struct {
	resource provider.PlaybackResource
	playlist bool
	usedAt   uint64
}

type speedBucket struct {
	slot  int64
	bytes int64
}

type speedMeter struct {
	mu          sync.Mutex
	origin      time.Time
	firstAt     time.Time
	buckets     [speedBucketCount]speedBucket
	transferBps float64
}

func (s *speedMeter) recordTransfer(bytes int64, elapsed time.Duration) {
	if bytes < 32<<10 && elapsed < 250*time.Millisecond {
		return
	}
	secs := elapsed.Seconds()
	if secs < 0.001 {
		secs = 0.001
	}
	bps := float64(bytes) / secs
	s.mu.Lock()
	if s.transferBps == 0 {
		s.transferBps = bps
	} else {
		s.transferBps = 0.35*bps + 0.65*s.transferBps
	}
	s.mu.Unlock()
}

func (s *speedMeter) activeTransferBps() float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.transferBps
}

func (s *speedMeter) record(n int64, now time.Time) {
	if n <= 0 {
		return
	}
	s.mu.Lock()
	if s.origin.IsZero() {
		s.origin = now
	}
	if s.firstAt.IsZero() {
		s.firstAt = now
	}
	elapsed := now.Sub(s.origin)
	if elapsed < 0 {
		elapsed = 0
	}
	slot := int64(elapsed/speedBucketWidth) + 1
	b := &s.buckets[slot%speedBucketCount]
	if b.slot == slot {
		b.bytes += n
	} else {
		b.slot = slot
		b.bytes = n
	}
	s.mu.Unlock()
}

func (s *speedMeter) bytesPerSecond(now time.Time) float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.firstAt.IsZero() || s.origin.IsZero() {
		return 0
	}
	elapsed := now.Sub(s.origin)
	if elapsed < 0 {
		return 0
	}
	curSlot := int64(elapsed/speedBucketWidth) + 1
	minSlot := curSlot - speedBucketCount + 1
	var total int64
	for _, b := range s.buckets {
		if b.slot >= minSlot && b.slot <= curSlot {
			total += b.bytes
		}
	}
	if total == 0 {
		return 0
	}
	window := now.Sub(s.firstAt)
	maxWindow := time.Duration(speedBucketCount) * speedBucketWidth
	if window < speedMinWindow {
		window = speedMinWindow
	} else if window > maxWindow {
		window = maxWindow
	}
	return float64(total) / window.Seconds()
}

type countingReader struct {
	r     io.Reader
	meter *speedMeter
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	if n > 0 {
		c.meter.record(int64(n), time.Now())
	}
	return n, err
}

// Media publishes only the resources of one resolved playback over loopback.
// The native HLS client can seek in those resources without proxying a CDN URL
// or receiving the source account's request headers.
type Media struct {
	URL          string
	VideoRange   string
	server       *http.Server
	transport    provider.PlaybackTransport
	prefix       string
	mux          *http.ServeMux
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup
	mu               sync.Mutex
	closed           bool
	hlsChildren      map[string]hlsChildEntry
	hlsSeq           uint64
	hlsPrefetchBody  string
	hlsPrefetchUntil time.Time
	hlsLive          bool
	cache            *segmentCache
	tracks           []*dashTrack
	audioTracks      []provider.PlaybackTrack
	audioOptionNames map[string]string
	audioCodecs      map[string]string
	defaultAudioID   string
	activeAudioID    string
	activeAudioName  string
	bandwidthBps     float64
	primaryURLs      []string
	lastURL          atomic.Pointer[string]
	failure          atomic.Pointer[provider.ErrorCode]
	speed            speedMeter
}

func OpenMedia(ctx context.Context, playback provider.Playback) (*Media, error) {
	return OpenMediaWithAudio(ctx, playback, "")
}

func OpenMediaWithAudio(ctx context.Context, playback provider.Playback, selectedAudioID string) (*Media, error) {
	if playback.Transport == nil {
		return nil, &provider.Error{Code: provider.Unsupported}
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	mctx, mcancel := context.WithCancel(context.Background())
	m := &Media{
		transport: playback.Transport, prefix: "/" + rand.Text() + "/",
		mux: http.NewServeMux(), ctx: mctx, cancel: mcancel,
		hlsChildren: make(map[string]hlsChildEntry),
		speed:       speedMeter{origin: time.Now()},
	}
	m.mux.HandleFunc(m.prefix+"c/", m.serveHLSChild)
	for _, stream := range playback.Plan.Streams {
		streamCtx := ctx
		var streamStop context.CancelFunc
		if len(playback.Plan.Streams) > 1 && stream.Format == provider.PlaybackHLS {
			streamCtx, streamStop = context.WithTimeout(ctx, 4*time.Second)
		}
		name, err := m.prepareStream(streamCtx, stream, selectedAudioID)
		if streamStop != nil {
			streamStop()
		}
		if err != nil {
			if ctx.Err() != nil {
				mcancel()
				listener.Close()
				return nil, err
			}
			m.recordFailure(err)
			continue
		}
		m.URL = "http://" + listener.Addr().String() + m.prefix + name
		m.failure.Store(nil)
		m.server = &http.Server{
			ReadHeaderTimeout: 5 * time.Second,
			IdleTimeout:       30 * time.Second,
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Host != listener.Addr().String() || r.Method != http.MethodGet && r.Method != http.MethodHead {
					http.NotFound(w, r)
					return
				}
				m.mux.ServeHTTP(w, r)
			}),
		}
		go m.server.Serve(listener)
		return m, nil
	}
	mcancel()
	listener.Close()
	if code := m.Failure(); code != "" {
		return nil, &provider.Error{Code: code}
	}
	return nil, &provider.Error{Code: provider.Unsupported}
}

func (m *Media) Failure() provider.ErrorCode {
	if code := m.failure.Load(); code != nil {
		return *code
	}
	return ""
}

func (m *Media) recordFailure(err error) {
	code := provider.CodeOf(err)
	if code != provider.Canceled {
		m.failure.Store(&code)
	}
}

func (m *Media) Close() {
	if m == nil {
		return
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.closed = true
	tracks := m.tracks
	cache := m.cache
	m.mu.Unlock()
	if m.cancel != nil {
		m.cancel()
	}
	for _, track := range tracks {
		track.close()
	}
	if cache != nil {
		cache.close()
	}
	if m.server != nil {
		_ = m.server.Close()
	}
	m.wg.Wait()
}

// BytesPerSecond returns the recent downstream transfer rate observed by this
// media proxy across video, audio, and playlist fetches.
func (m *Media) BytesPerSecond(now time.Time) float64 {
	if m == nil {
		return 0
	}
	return m.speed.bytesPerSecond(now)
}

// TransferBps returns the EWMA active transfer rate observed while downloading
// segments, unaffected by idle periods when the forward buffer is full.
func (m *Media) TransferBps() float64 {
	if m == nil {
		return 0
	}
	return m.speed.activeTransferBps()
}

// BandwidthBps returns the nominal byte rate required by the active stream.
func (m *Media) BandwidthBps() float64 {
	if m == nil {
		return 0
	}
	return m.bandwidthBps
}

// HasHealthyBackup reports whether the primary stream has another unpenalized CDN host.
func (m *Media) HasHealthyBackup() bool {
	if m == nil || len(m.primaryURLs) < 2 {
		return false
	}
	reporter, ok := m.transport.(provider.PlaybackHealthReporter)
	if !ok {
		return false
	}
	current := ""
	if p := m.lastURL.Load(); p != nil {
		current = *p
	}
	return reporter.HasHealthyAlternative(m.primaryURLs, current)
}

func (m *Media) AudioTracks() []provider.PlaybackTrack {
	if m == nil || len(m.audioTracks) == 0 {
		return nil
	}
	return slices.Clone(m.audioTracks)
}

func (m *Media) DefaultAudioID() string {
	if m == nil {
		return ""
	}
	return m.defaultAudioID
}

func (m *Media) ActiveAudioID() string {
	if m == nil {
		return ""
	}
	return m.activeAudioID
}

func (m *Media) AudioOptionName(trackID string) (string, bool) {
	if m == nil || len(m.audioOptionNames) == 0 {
		return "", false
	}
	name, ok := m.audioOptionNames[trackID]
	return name, ok
}

func (m *Media) CanSwitchAudioInPlace(fromID, toID string) bool {
	if m == nil || fromID == "" || toID == "" {
		return false
	}
	if _, ok := m.audioOptionNames[fromID]; !ok {
		return false
	}
	if _, ok := m.audioOptionNames[toID]; !ok {
		return false
	}
	return m.audioCodecs[fromID] != "" && m.audioCodecs[fromID] == m.audioCodecs[toID]
}

func supportedAudioCodec(codec string) bool {
	c := strings.ToLower(strings.TrimSpace(codec))
	if c == "" {
		return true
	}
	return strings.HasPrefix(c, "mp4a.") || c == "ec-3" || c == "ac-3"
}

func audioCodecFamily(codec string) string {
	c := strings.ToLower(strings.TrimSpace(codec))
	switch {
	case strings.HasPrefix(c, "mp4a."):
		return "mp4a"
	case c == "ec-3":
		return "ec-3"
	case c == "ac-3":
		return "ac-3"
	default:
		return c
	}
}

func (m *Media) prepareStream(ctx context.Context, stream provider.PlaybackStream, selectedAudioID string) (string, error) {
	switch stream.Format {
	case provider.PlaybackDirect:
		m.primaryURLs = stream.Video.Resource.URLs
		m.bandwidthBps = float64(max(stream.Video.Bandwidth, 0)) / 8.0
		m.registerResource("video.mp4", stream.Video.Resource, stream.Video.ContentType, false)
		return "video.mp4", nil
	case provider.PlaybackHLS:
		body, err := m.fetchPlaylist(ctx, stream.Video.Resource)
		if err != nil {
			return "", err
		}
		m.primaryURLs = stream.Video.Resource.URLs
		m.bandwidthBps = float64(max(stream.Video.Bandwidth, 0)) / 8.0
		m.mu.Lock()
		m.hlsLive = stream.Live
		m.hlsPrefetchBody = body
		m.hlsPrefetchUntil = time.Now().Add(750 * time.Millisecond)
		m.mu.Unlock()
		m.registerResource("stream.m3u8", stream.Video.Resource, stream.Video.ContentType, true)
		return "stream.m3u8", nil
	case provider.PlaybackDASH:
		video, initData, initCR, dynamicRange, err := m.readTrack(ctx, stream.Video, true)
		if err != nil {
			return "", err
		}
		rawAudio := stream.AudioTracks
		if len(rawAudio) == 0 && stream.Audio != nil {
			rawAudio = []provider.PlaybackTrack{*stream.Audio}
		}
		candidates := make([]provider.PlaybackTrack, 0, len(rawAudio))
		seenIDs := make(map[string]bool, len(rawAudio))
		for i, track := range rawAudio {
			if !supportedAudioCodec(track.Codec) {
				continue
			}
			track.ID = strings.TrimSpace(track.ID)
			if track.ID == "" {
				track.ID = fmt.Sprintf("audio-%d", i)
			}
			if seenIDs[track.ID] {
				continue
			}
			seenIDs[track.ID] = true
			track.Label = strings.TrimSpace(track.Label)
			if track.Label == "" {
				if len(candidates) == 0 {
					track.Label = "Audio"
				} else {
					track.Label = fmt.Sprintf("Audio %d", len(candidates)+1)
				}
			}
			candidates = append(candidates, track)
		}
		desiredIdx := 0
		if selectedAudioID != "" {
			if idx := slices.IndexFunc(candidates, func(t provider.PlaybackTrack) bool { return t.ID == selectedAudioID }); idx >= 0 {
				desiredIdx = idx
			}
		} else if idx := slices.IndexFunc(candidates, func(t provider.PlaybackTrack) bool { return t.Default }); idx >= 0 {
			desiredIdx = idx
		}

		type audioReadResult struct {
			fragments []fragment
			err       error
		}
		results := make([]audioReadResult, len(candidates))
		if len(candidates) == 1 {
			frags, _, _, _, rerr := m.readTrack(ctx, candidates[0], false)
			results[0] = audioReadResult{fragments: frags, err: rerr}
		} else if len(candidates) > 1 {
			var wg sync.WaitGroup
			for i, track := range candidates {
				wg.Add(1)
				go func(idx int, tr provider.PlaybackTrack) {
					defer wg.Done()
					frags, _, _, _, rerr := m.readTrack(ctx, tr, false)
					results[idx] = audioReadResult{fragments: frags, err: rerr}
				}(i, track)
			}
			wg.Wait()
		}

		audioTracks := make([]provider.PlaybackTrack, 0, len(candidates))
		audioFragments := make([][]fragment, 0, len(candidates))
		activeIdx := -1
		for i, track := range candidates {
			if results[i].err != nil {
				if i == desiredIdx {
					return "", results[i].err
				}
				continue
			}
			if i == desiredIdx {
				activeIdx = len(audioTracks)
			}
			audioTracks = append(audioTracks, track)
			audioFragments = append(audioFragments, results[i].fragments)
		}
		if len(candidates) > 0 && len(audioTracks) == 0 {
			return "", results[desiredIdx].err
		}
		if activeIdx < 0 && len(audioTracks) > 0 {
			activeIdx = 0
		}

		m.VideoRange = dynamicRange
		m.primaryURLs = stream.Video.Resource.URLs
		totalBandwidth := stream.Video.Bandwidth
		if activeIdx >= 0 {
			totalBandwidth += audioTracks[activeIdx].Bandwidth
		}
		m.bandwidthBps = float64(max(totalBandwidth, 0)) / 8.0
		m.cache = newSegmentCache(dynamicRange == "PQ" || dynamicRange == "HLG")
		// Register handlers only once all indexes have been read. An alternate
		// representation can be tried without leaving partial handlers behind.
		m.registerPlaylist("video.m3u8", mediaPlaylist("video.mp4", *stream.Video.Init, video, stream.Live))
		videoTrack := newDashTrack(m.ctx, "video.mp4", stream.Video.Resource, "video/mp4", stream.Video.Bandwidth, true, *stream.Video.Init, video)
		m.cache.seedInit(videoTrack, *stream.Video.Init, initData, initCR)
		m.registerDashTrack(videoTrack)

		activeFamily := ""
		if activeIdx >= 0 {
			activeFamily = audioCodecFamily(audioTracks[activeIdx].Codec)
		}
		m.audioTracks = audioTracks
		m.audioCodecs = make(map[string]string, len(audioTracks))
		for i, track := range audioTracks {
			m.audioCodecs[track.ID] = audioCodecFamily(track.Codec)
			if i == 0 || track.Default {
				if m.defaultAudioID == "" || track.Default {
					m.defaultAudioID = track.ID
				}
			}
			if activeFamily != "" && audioCodecFamily(track.Codec) != activeFamily {
				continue
			}
			playlistName, mediaName := audioNames(i)
			if i == activeIdx {
				m.activeAudioID = track.ID
				m.activeAudioName = mediaName
			}
			m.registerPlaylist(playlistName, mediaPlaylist(mediaName, *track.Init, audioFragments[i], stream.Live))
			audioTrack := newDashTrack(m.ctx, mediaName, track.Resource, "audio/mp4", track.Bandwidth, false, *track.Init, audioFragments[i])
			m.registerDashTrack(audioTrack)
		}
		playlistBody, optionNames := buildMasterPlaylist(stream, audioTracks, activeIdx, dynamicRange)
		m.audioOptionNames = optionNames
		m.registerPlaylist("stream.m3u8", playlistBody)
		return "stream.m3u8", nil
	}
	return "", &provider.Error{Code: provider.Unsupported}
}

func (m *Media) readTrack(ctx context.Context, track provider.PlaybackTrack, video bool) ([]fragment, []byte, string, string, error) {
	if track.Index == nil || track.Init == nil {
		return nil, nil, "", "", &provider.Error{Code: provider.Unsupported}
	}
	index, _, err := m.readRange(ctx, track.Resource, *track.Index)
	if err != nil {
		return nil, nil, "", "", err
	}
	fragments, err := segmentIndex(index, track.Index.First)
	if err != nil {
		return nil, nil, "", "", err
	}
	if !video {
		return fragments, nil, "", "", nil
	}
	initial, contentRange, err := m.readRange(ctx, track.Resource, *track.Init)
	if err != nil {
		return nil, nil, "", "", err
	}
	return fragments, initial, contentRange, videoRange(initial, track.Codec), nil
}

func (m *Media) readRange(ctx context.Context, resource provider.PlaybackResource, r provider.PlaybackRange) ([]byte, string, error) {
	// A sidx has at most 65535 12-byte entries. Allow room for other initialization
	// boxes, and reject a range that is too large to be playback metadata.
	const maxMetadata = 8 << 20
	if r.First < 0 || r.Last < r.First || r.Last-r.First >= maxMetadata {
		return nil, "", &provider.Error{Code: provider.Unsupported}
	}
	byteRange := fmt.Sprintf("bytes=%d-%d", r.First, r.Last)
	response, err := m.transport.OpenResource(ctx, resource, http.MethodGet, byteRange)
	if err != nil {
		return nil, "", err
	}
	defer response.Body.Close()
	contentRange := response.Header.Get("Content-Range")
	expected := fmt.Sprintf("bytes %d-%d/", r.First, r.Last)
	if response.StatusCode != http.StatusPartialContent || !strings.HasPrefix(contentRange, expected) {
		return nil, "", &provider.Error{Code: provider.Unsupported}
	}
	result := make([]byte, int(r.Last-r.First+1))
	started := time.Now()
	_, err = io.ReadFull(&countingReader{r: response.Body, meter: &m.speed}, result)
	if err != nil {
		return nil, "", &provider.Error{Code: provider.Network, Cause: err}
	}
	m.speed.recordTransfer(int64(len(result)), time.Since(started))
	return result, contentRange, nil
}

func audioNames(index int) (playlist, media string) {
	if index == 0 {
		return "audio.m3u8", "audio.mp4"
	}
	return fmt.Sprintf("audio-%d.m3u8", index), fmt.Sprintf("audio-%d.mp4", index)
}

func mediaPlaylist(name string, init provider.PlaybackRange, fragments []fragment, live bool) string {
	var text strings.Builder
	var target float64
	for _, part := range fragments {
		target = max(target, part.duration)
	}
	fmt.Fprintf(&text, "#EXTM3U\n#EXT-X-VERSION:7\n#EXT-X-TARGETDURATION:%d\n#EXT-X-MEDIA-SEQUENCE:0\n",
		int(math.Ceil(target)))
	if !live {
		text.WriteString("#EXT-X-PLAYLIST-TYPE:VOD\n")
	}
	fmt.Fprintf(&text, "#EXT-X-MAP:URI=%q,BYTERANGE=%q\n", name, fmt.Sprintf("%d@%d", init.Last-init.First+1, init.First))
	for _, part := range fragments {
		fmt.Fprintf(&text, "#EXTINF:%.6f,\n#EXT-X-BYTERANGE:%d@%d\n%s\n", part.duration, part.Last-part.First+1, part.First, name)
	}
	if !live {
		text.WriteString("#EXT-X-ENDLIST\n")
	}
	return text.String()
}

var codecName = regexp.MustCompile("^[A-Za-z0-9.-]+$")

func masterPlaylist(stream provider.PlaybackStream, audioTracks []provider.PlaybackTrack, dynamicRange string) string {
	body, _ := buildMasterPlaylist(stream, audioTracks, 0, dynamicRange)
	return body
}

func buildMasterPlaylist(stream provider.PlaybackStream, audioTracks []provider.PlaybackTrack, activeIdx int, dynamicRange string) (string, map[string]string) {
	var text strings.Builder
	text.WriteString("#EXTM3U\n#EXT-X-VERSION:7\n#EXT-X-INDEPENDENT-SEGMENTS\n")
	codecs := stream.Video.Codec
	bandwidth := stream.Video.Bandwidth
	if !codecName.MatchString(codecs) {
		codecs = ""
	}
	if activeIdx < 0 || activeIdx >= len(audioTracks) {
		activeIdx = 0
	}
	activeFamily := ""
	if len(audioTracks) > 0 {
		activeFamily = audioCodecFamily(audioTracks[activeIdx].Codec)
	}
	maxAudioBandwidth := 0
	seenNames := make(map[string]int, len(audioTracks))
	optionNames := make(map[string]string, len(audioTracks))
	emittedAudio := 0
	for i, sound := range audioTracks {
		if activeFamily != "" && audioCodecFamily(sound.Codec) != activeFamily {
			continue
		}
		playlistName, _ := audioNames(i)
		name := strings.TrimSpace(sound.Label)
		if name == "" {
			if i == 0 {
				name = "Audio"
			} else {
				name = fmt.Sprintf("Audio %d", i+1)
			}
		}
		seenNames[name]++
		if seenNames[name] > 1 {
			name = fmt.Sprintf("%s (%d)", name, seenNames[name])
		}
		if sound.ID != "" {
			optionNames[sound.ID] = name
		}
		def := "NO"
		if i == activeIdx {
			def = "YES"
			if codecName.MatchString(sound.Codec) {
				if codecs != "" {
					codecs += ","
				}
				codecs += sound.Codec
			}
		}
		if sound.Bandwidth > maxAudioBandwidth {
			maxAudioBandwidth = sound.Bandwidth
		}
		fmt.Fprintf(&text, "#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID=\"audio\",NAME=%q,DEFAULT=%s,AUTOSELECT=%s,URI=%q\n",
			name, def, def, playlistName)
		emittedAudio++
	}
	bandwidth += maxAudioBandwidth
	fmt.Fprintf(&text, "#EXT-X-STREAM-INF:BANDWIDTH=%d,RESOLUTION=%dx%d,CODECS=%q,VIDEO-RANGE=%s",
		bandwidth, stream.Video.Width, stream.Video.Height, codecs, dynamicRange)
	if stream.Video.FrameRate > 0 {
		fmt.Fprintf(&text, ",FRAME-RATE=%.3f", stream.Video.FrameRate)
	}
	if emittedAudio > 0 {
		text.WriteString(",AUDIO=\"audio\"")
	}
	text.WriteString("\nvideo.m3u8\n")
	return text.String(), optionNames
}

func (m *Media) registerPlaylist(name, body string) {
	m.mux.HandleFunc(m.prefix+name, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		if r.Method == http.MethodGet {
			io.WriteString(w, body)
		}
	})
}

func (m *Media) registerResource(name string, resource provider.PlaybackResource, mediaType string, playlist bool) {
	m.mux.HandleFunc(m.prefix+name, func(w http.ResponseWriter, request *http.Request) {
		if playlist {
			m.servePlaylist(w, request, resource)
			return
		}
		m.serveDirectResource(w, request, resource, mediaType)
	})
}

func (m *Media) serveDirectResource(w http.ResponseWriter, request *http.Request, resource provider.PlaybackResource, mediaType string) {
	started := time.Now()
	response, err := m.transport.OpenResource(request.Context(), resource, request.Method, request.Header.Get("Range"))
	if err != nil {
		m.recordFailure(err)
		http.Error(w, "Media unavailable", http.StatusBadGateway)
		return
	}
	defer response.Body.Close()
	if response.Request != nil && response.Request.URL != nil {
		u := response.Request.URL.String()
		m.lastURL.Store(&u)
	}
	for _, name := range []string{"Accept-Ranges", "Content-Length", "Content-Range", "Content-Type"} {
		if value := response.Header.Get(name); value != "" {
			w.Header().Set(name, value)
		}
	}
	if mediaType != "" {
		w.Header().Set("Content-Type", mediaType)
	}
	w.WriteHeader(response.StatusCode)
	if request.Method == http.MethodGet {
		n, _ := io.Copy(w, &countingReader{r: response.Body, meter: &m.speed})
		m.speed.recordTransfer(n, time.Since(started))
	}
}

var playlistURI = regexp.MustCompile("URI=\"([^\"]+)\"")

func (m *Media) fetchPlaylist(ctx context.Context, resource provider.PlaybackResource) (string, error) {
	res, err := m.transport.OpenResource(ctx, resource, http.MethodGet, "")
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.Request != nil && res.Request.URL != nil {
		u := res.Request.URL.String()
		m.lastURL.Store(&u)
	}
	var body strings.Builder
	scanner := bufio.NewScanner(&countingReader{r: res.Body, meter: &m.speed})
	scanner.Buffer(make([]byte, 4096), 1<<20)
	nextIsPlaylist := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch {
		case strings.HasPrefix(line, "#"):
			if strings.HasPrefix(line, "#EXT-X-STREAM-INF") {
				nextIsPlaylist = true
			}
			uriIsPlaylist := strings.HasPrefix(line, "#EXT-X-MEDIA:") || strings.HasPrefix(line, "#EXT-X-I-FRAME-STREAM-INF:")
			line = playlistURI.ReplaceAllStringFunc(line, func(pattern string) string {
				match := playlistURI.FindStringSubmatch(pattern)
				return "URI=" + strconv.Quote(m.playlistChild(res.Request.URL, match[1], resource.Headers, uriIsPlaylist))
			})
		case line != "":
			line = m.playlistChild(res.Request.URL, line, resource.Headers, nextIsPlaylist)
			nextIsPlaylist = false
		}
		body.WriteString(line)
		body.WriteByte('\n')
	}
	if err := scanner.Err(); err != nil || !strings.HasPrefix(body.String(), "#EXTM3U\n") {
		return "", errors.New("invalid HLS playlist")
	}
	return body.String(), nil
}

func (m *Media) servePlaylist(w http.ResponseWriter, r *http.Request, resource provider.PlaybackResource) {
	var body string
	isRoot := r.URL.Path == m.prefix+"stream.m3u8"
	if isRoot {
		m.mu.Lock()
		if m.hlsPrefetchBody != "" && time.Now().Before(m.hlsPrefetchUntil) {
			body = m.hlsPrefetchBody
		}
		m.hlsPrefetchBody = ""
		m.hlsPrefetchUntil = time.Time{}
		m.mu.Unlock()
	}
	if body == "" {
		var err error
		body, err = m.fetchPlaylist(r.Context(), resource)
		if err != nil {
			m.recordFailure(err)
			http.Error(w, "Playlist unavailable", http.StatusBadGateway)
			return
		}
	}
	m.mu.Lock()
	live := m.hlsLive
	m.mu.Unlock()
	if live {
		w.Header().Set("Cache-Control", "no-store")
	}
	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	if r.Method == http.MethodGet {
		io.WriteString(w, body)
	}
}

func (m *Media) serveHLSChild(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, m.prefix+"c/")
	m.mu.Lock()
	entry, ok := m.hlsChildren[name]
	if ok {
		m.hlsSeq++
		entry.usedAt = m.hlsSeq
		m.hlsChildren[name] = entry
	}
	m.mu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	if entry.playlist {
		m.servePlaylist(w, r, entry.resource)
		return
	}
	m.serveDirectResource(w, r, entry.resource, "")
}

func (m *Media) playlistChild(base *url.URL, value string, headers map[string]string, isPlaylist bool) string {
	ref, err := base.Parse(value)
	allowedScheme := err == nil && (ref.Scheme == "https" ||
		(ref.Scheme == "http" && base != nil && base.Scheme == "http" && strings.EqualFold(ref.Host, base.Host)))
	if err != nil || ref.User != nil || !allowedScheme || ref.Host == "" {
		return "unavailable"
	}
	if !strings.EqualFold(ref.Host, base.Host) {
		forwarded := make(map[string]string, len(headers))
		for k, v := range headers {
			switch strings.ToLower(k) {
			case "accept", "accept-language", "referer", "user-agent":
				forwarded[k] = v
			}
		}
		if len(forwarded) == 0 {
			headers = nil
		} else {
			headers = forwarded
		}
	}
	sum := sha256.Sum256([]byte(ref.String()))
	ext := path.Ext(ref.Path)
	if len(ext) > 10 {
		ext = ""
	}
	playlist := isPlaylist || strings.EqualFold(ext, ".m3u8") || strings.EqualFold(ext, ".m3u")
	name := fmt.Sprintf("%x%s", sum[:16], ext)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.hlsSeq++
	if existing, ok := m.hlsChildren[name]; ok {
		existing.usedAt = m.hlsSeq
		if playlist {
			existing.playlist = true
		}
		m.hlsChildren[name] = existing
		return m.prefix + "c/" + name
	}
	if len(m.hlsChildren) >= hlsMaxChildren {
		var oldestKey string
		var oldestSeq uint64
		for key, item := range m.hlsChildren {
			if oldestKey == "" || item.usedAt < oldestSeq {
				oldestKey, oldestSeq = key, item.usedAt
			}
		}
		if oldestKey != "" {
			delete(m.hlsChildren, oldestKey)
		}
	}
	m.hlsChildren[name] = hlsChildEntry{
		resource: provider.PlaybackResource{URLs: []string{ref.String()}, Headers: headers},
		playlist: playlist,
		usedAt:   m.hlsSeq,
	}
	return m.prefix + "c/" + name
}
