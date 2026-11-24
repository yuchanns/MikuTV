package player

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yuchanns/MikuTV/internal/provider"
)

const (
	sdrCacheBudget        int64 = 48 << 20
	hdrCacheBudget        int64 = 64 << 20
	warmupCacheBudget     int64 = 12 << 20
	maxCachedSegmentBytes int64 = 24 << 20
	sdrPrefetchSeconds          = 30.0
	hdrPrefetchSeconds          = 45.0
	warmupPrefetchSeconds       = 8.0
	seekCancelDistance          = 3
	minSlowFloorBps             = 256 << 10
	slowRemainingSeconds        = 3.0
)

var (
	slowReadMinAge         = 2500 * time.Millisecond
	slowStallTimeout       = 3500 * time.Millisecond
	slowStallCheckInterval = 500 * time.Millisecond
)

type rangeKey struct {
	track string
	first int64
	last  int64
}

type dashTrack struct {
	name         string
	resource     provider.PlaybackResource
	mediaType    string
	bandwidthBps float64
	isVideo      bool
	init         provider.PlaybackRange
	fragments    []fragment
	fragIndex    map[provider.PlaybackRange]int

	mu            sync.Mutex
	cursor        int
	hasCursor     bool
	workerStarted bool
	epochCtx      context.Context
	epochCancel   context.CancelFunc
	wake          chan struct{}
}

func newDashTrack(parent context.Context, name string, resource provider.PlaybackResource, mediaType string, bandwidthBits int, isVideo bool, initRange provider.PlaybackRange, fragments []fragment) *dashTrack {
	idx := make(map[provider.PlaybackRange]int, len(fragments))
	for i, part := range fragments {
		idx[part.PlaybackRange] = i
	}
	ectx, ecancel := context.WithCancel(parent)
	return &dashTrack{
		name:         name,
		resource:     resource,
		mediaType:    mediaType,
		bandwidthBps: float64(max(bandwidthBits, 0)) / 8.0,
		isVideo:      isVideo,
		init:         initRange,
		fragments:    fragments,
		fragIndex:    idx,
		epochCtx:     ectx,
		epochCancel:  ecancel,
		wake:         make(chan struct{}, 1),
	}
}

func (t *dashTrack) close() {
	t.mu.Lock()
	cancel := t.epochCancel
	t.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (t *dashTrack) currentEpoch() (context.Context, int, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.epochCtx, t.cursor, t.hasCursor
}

func (t *dashTrack) advanceCursor(parent context.Context, fragIdx int) (context.Context, bool) {
	t.mu.Lock()
	jumped := t.hasCursor && (fragIdx < t.cursor-1 || fragIdx-t.cursor > seekCancelDistance)
	if jumped && t.epochCancel != nil {
		t.epochCancel()
		t.epochCtx, t.epochCancel = context.WithCancel(parent)
	}
	if jumped || !t.hasCursor || fragIdx > t.cursor {
		t.cursor = fragIdx
		t.hasCursor = true
	}
	ectx := t.epochCtx
	t.mu.Unlock()
	return ectx, jumped
}

func (t *dashTrack) notifyWorker() {
	select {
	case t.wake <- struct{}{}:
	default:
	}
}

func (t *dashTrack) updateCursor(parent context.Context, fragIdx int) (context.Context, bool) {
	ectx, jumped := t.advanceCursor(parent, fragIdx)
	t.notifyWorker()
	return ectx, jumped
}

func (t *dashTrack) fragmentAtByte(offset int64) int {
	lo, hi := 0, len(t.fragments)-1
	for lo <= hi {
		mid := lo + (hi-lo)/2
		f := t.fragments[mid]
		if offset < f.First {
			hi = mid - 1
		} else if offset > f.Last {
			lo = mid + 1
		} else {
			return mid
		}
	}
	return -1
}

type cachedSegment struct {
	key      rangeKey
	pinned   bool
	track    *dashTrack
	fragIdx  int
	expected int64

	// accounted is owned exclusively by segmentCache.mu.
	accounted int64

	mu       sync.Mutex
	cond     *sync.Cond
	buf      []byte
	status   int
	headers  http.Header
	done     bool
	err      error
	readers  int
	fetching bool
	lastUse  int64
}

type segmentCache struct {
	mu            sync.Mutex
	budget        int64
	activeBudget  int64
	targetSeconds float64
	activeSeconds float64
	used          int64
	seq           int64
	entries       map[rangeKey]*cachedSegment
	closed        bool
}

func newSegmentCache(hdr bool) *segmentCache {
	budget := sdrCacheBudget
	seconds := sdrPrefetchSeconds
	if hdr {
		budget = hdrCacheBudget
		seconds = hdrPrefetchSeconds
	}
	return &segmentCache{
		budget:        budget,
		activeBudget:  budget,
		targetSeconds: seconds,
		activeSeconds: seconds,
		entries:       make(map[rangeKey]*cachedSegment),
	}
}

func (c *segmentCache) setWarmupMode(warmup bool) {
	c.mu.Lock()
	if warmup {
		c.budget = warmupCacheBudget
		c.targetSeconds = warmupPrefetchSeconds
	} else {
		c.budget = c.activeBudget
		c.targetSeconds = c.activeSeconds
	}
	c.evictLocked()
	c.mu.Unlock()
}

func (c *segmentCache) limits() (int64, float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.budget, c.targetSeconds
}

func (c *segmentCache) close() {
	c.mu.Lock()
	c.closed = true
	all := make([]*cachedSegment, 0, len(c.entries))
	for _, seg := range c.entries {
		all = append(all, seg)
	}
	clear(c.entries)
	c.used = 0
	c.mu.Unlock()
	for _, seg := range all {
		seg.mu.Lock()
		if !seg.done {
			seg.err = context.Canceled
			seg.done = true
			seg.fetching = false
		}
		seg.cond.Broadcast()
		seg.mu.Unlock()
	}
}

func (c *segmentCache) seedInit(track *dashTrack, r provider.PlaybackRange, data []byte, contentRange string) {
	if len(data) == 0 || int64(len(data)) > maxCachedSegmentBytes {
		return
	}
	key := rangeKey{track: track.name, first: r.First, last: r.Last}
	hdr := make(http.Header)
	hdr.Set("Accept-Ranges", "bytes")
	hdr.Set("Content-Length", strconv.Itoa(len(data)))
	if contentRange != "" {
		hdr.Set("Content-Range", contentRange)
	}
	if track.mediaType != "" {
		hdr.Set("Content-Type", track.mediaType)
	}
	seg := &cachedSegment{
		key:       key,
		pinned:    true,
		track:     track,
		fragIdx:   -1,
		expected:  int64(len(data)),
		accounted: int64(len(data)),
		buf:       append([]byte(nil), data...),
		status:    http.StatusPartialContent,
		headers:   hdr,
		done:      true,
	}
	seg.cond = sync.NewCond(&seg.mu)
	c.mu.Lock()
	if !c.closed {
		if prev, ok := c.entries[key]; ok {
			c.used -= prev.accounted
		}
		c.seq++
		seg.lastUse = c.seq
		c.entries[key] = seg
		c.used += seg.accounted
		c.evictLocked()
	}
	c.mu.Unlock()
}

// lookupOrCreate returns an existing live entry or inserts a new entry with its
// full expected byte size reserved up front in c.used. When evictNow is true,
// idle entries are evicted immediately to fit within c.budget; fragment reads in
// serveDashTrack pass false and call evict() right after advancing the track
// cursor so eviction reflects the new playback position.
func (c *segmentCache) lookupOrCreate(track *dashTrack, r provider.PlaybackRange, fragIdx int, pinned, evictNow bool) (seg *cachedSegment, startFetch bool) {
	key := rangeKey{track: track.name, first: r.First, last: r.Last}
	expected := r.Last - r.First + 1
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, false
	}
	c.seq++
	seq := c.seq
	if existing, ok := c.entries[key]; ok {
		existing.mu.Lock()
		if !existing.done || existing.err == nil {
			existing.lastUse = seq
			existing.mu.Unlock()
			return existing, false
		}
		existing.mu.Unlock()
		c.used -= existing.accounted
		existing.accounted = 0
		delete(c.entries, key)
	}
	seg = &cachedSegment{
		key:       key,
		pinned:    pinned,
		track:     track,
		fragIdx:   fragIdx,
		expected:  expected,
		accounted: expected,
		fetching:  true,
		lastUse:   seq,
	}
	seg.cond = sync.NewCond(&seg.mu)
	c.entries[key] = seg
	c.used += expected
	if evictNow {
		c.evictLocked()
	}
	return seg, true
}

func (c *segmentCache) dropEntry(seg *cachedSegment) {
	c.mu.Lock()
	if cur, ok := c.entries[seg.key]; ok && cur == seg {
		delete(c.entries, seg.key)
		c.used -= seg.accounted
		seg.accounted = 0
	}
	c.mu.Unlock()
}

// tryStartPrefetch atomically checks cache presence and budget headroom under
// c.mu and reserves the segment's full expected size before starting a
// background fetch.
func (c *segmentCache) tryStartPrefetch(track *dashTrack, r provider.PlaybackRange, fragIdx int) (seg *cachedSegment, start bool, hasRoom bool) {
	key := rangeKey{track: track.name, first: r.First, last: r.Last}
	size := r.Last - r.First + 1
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, false, false
	}
	if existing, ok := c.entries[key]; ok {
		existing.mu.Lock()
		usable := !existing.done || existing.err == nil
		existing.mu.Unlock()
		if usable {
			return existing, false, true
		}
		c.used -= existing.accounted
		existing.accounted = 0
		delete(c.entries, key)
	}
	if c.used+size > c.budget {
		c.evictToLocked(c.budget-size, track, fragIdx)
		if c.used+size > c.budget {
			return nil, false, false
		}
	}
	c.seq++
	seg = &cachedSegment{
		key:       key,
		pinned:    false,
		track:     track,
		fragIdx:   fragIdx,
		expected:  size,
		accounted: size,
		fetching:  true,
		lastUse:   c.seq,
	}
	seg.cond = sync.NewCond(&seg.mu)
	c.entries[key] = seg
	c.used += size
	return seg, true, true
}

func (c *segmentCache) evict() {
	c.mu.Lock()
	c.evictLocked()
	c.mu.Unlock()
}

func (c *segmentCache) evictBehind(track *dashTrack, cursor int) {
	c.mu.Lock()
	for k, seg := range c.entries {
		if seg.pinned || seg.track != track || seg.fragIdx >= cursor {
			continue
		}
		seg.mu.Lock()
		canDrop := seg.done && seg.readers == 0
		seg.mu.Unlock()
		if canDrop {
			delete(c.entries, k)
			c.used -= seg.accounted
			seg.accounted = 0
		}
	}
	c.mu.Unlock()
}

func (c *segmentCache) evictLocked() {
	c.evictToLocked(c.budget, nil, -1)
}

// evictToLocked frees completed idle entries until c.used <= target.
// Behind-cursor segments are always evicted first (oldest lastUse first).
// Ahead-of-cursor segments are evicted farthest-from-cursor first so the
// imminent next segment (cursor+1) is preserved. When called by a prefetch
// worker (protectTrack != nil), ahead segments at or before protectMaxAhead
// on that track (and ahead segments on other tracks) are never evicted to
// make room for a farther prefetch.
func (c *segmentCache) evictToLocked(target int64, protectTrack *dashTrack, protectMaxAhead int) {
	if target < 0 {
		target = 0
	}
	for c.used > target {
		var victim *cachedSegment
		victimBehind := false
		victimAheadDist := -1
		var victimSeq int64
		for _, seg := range c.entries {
			if seg.pinned {
				continue
			}
			seg.mu.Lock()
			canDrop := seg.done && seg.readers == 0
			seq := seg.lastUse
			seg.mu.Unlock()
			if !canDrop {
				continue
			}
			_, cur, hasCur := seg.track.currentEpoch()
			behind := hasCur && seg.fragIdx >= 0 && seg.fragIdx < cur
			if !behind && protectTrack != nil {
				if seg.track != protectTrack || seg.fragIdx < 0 || seg.fragIdx <= protectMaxAhead {
					continue
				}
			}
			aheadDist := 0
			if !behind && seg.fragIdx >= 0 {
				if hasCur {
					aheadDist = seg.fragIdx - cur
				} else {
					aheadDist = seg.fragIdx
				}
			}
			better := false
			switch {
			case victim == nil:
				better = true
			case behind != victimBehind:
				better = behind
			case behind:
				better = seq < victimSeq
			case aheadDist != victimAheadDist:
				better = aheadDist > victimAheadDist
			default:
				better = seq < victimSeq
			}
			if better {
				victim = seg
				victimBehind = behind
				victimAheadDist = aheadDist
				victimSeq = seq
			}
		}
		if victim == nil {
			return
		}
		delete(c.entries, victim.key)
		c.used -= victim.accounted
		victim.accounted = 0
	}
}

func parseSingleByteRange(header string) (provider.PlaybackRange, bool) {
	if !strings.HasPrefix(header, "bytes=") {
		return provider.PlaybackRange{}, false
	}
	spec := strings.TrimSpace(strings.TrimPrefix(header, "bytes="))
	if spec == "" || strings.Contains(spec, ",") {
		return provider.PlaybackRange{}, false
	}
	loStr, hiStr, ok := strings.Cut(spec, "-")
	if !ok || loStr == "" || hiStr == "" {
		return provider.PlaybackRange{}, false
	}
	first, err1 := strconv.ParseInt(loStr, 10, 64)
	last, err2 := strconv.ParseInt(hiStr, 10, 64)
	if err1 != nil || err2 != nil || first < 0 || last < first {
		return provider.PlaybackRange{}, false
	}
	return provider.PlaybackRange{First: first, Last: last}, true
}

func (m *Media) ensurePrefetchWorker(track *dashTrack) {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	track.mu.Lock()
	if track.workerStarted {
		track.mu.Unlock()
		m.mu.Unlock()
		return
	}
	track.workerStarted = true
	track.mu.Unlock()
	m.wg.Add(1)
	m.mu.Unlock()
	go func() {
		defer m.wg.Done()
		m.runPrefetchWorker(track)
	}()
}

func (m *Media) runPrefetchWorker(track *dashTrack) {
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-track.wake:
		}
		for {
			if m.ctx.Err() != nil {
				return
			}
			ectx, cursor, hasCursor := track.currentEpoch()
			if !hasCursor {
				break
			}
			_, targetSeconds := m.cache.limits()
			var buffered float64
			for idx := cursor + 1; idx < len(track.fragments); idx++ {
				if ectx.Err() != nil || m.ctx.Err() != nil {
					break
				}
				if buffered >= targetSeconds {
					break
				}
				frag := track.fragments[idx]
				buffered += frag.duration
				segSize := frag.Last - frag.First + 1
				if segSize > maxCachedSegmentBytes {
					break
				}
				seg, start, hasRoom := m.cache.tryStartPrefetch(track, frag.PlaybackRange, idx)
				if !hasRoom {
					break
				}
				if !start {
					continue
				}
				m.fetchSegment(ectx, track, seg, false)
			}
			if ectx.Err() == nil {
				break
			}
		}
	}
}

func (m *Media) registerDashTrack(track *dashTrack) {
	m.tracks = append(m.tracks, track)
	m.mux.HandleFunc(m.prefix+track.name, func(w http.ResponseWriter, r *http.Request) {
		m.serveDashTrack(w, r, track)
	})
}

func (m *Media) serveDashTrack(w http.ResponseWriter, r *http.Request, track *dashTrack) {
	if r.Method != http.MethodGet {
		m.serveDirectResource(w, r, track.resource, track.mediaType)
		return
	}
	rng, ok := parseSingleByteRange(r.Header.Get("Range"))
	if !ok || rng.Last-rng.First+1 > maxCachedSegmentBytes {
		if ok {
			if idx := track.fragmentAtByte(rng.First); idx >= 0 {
				m.ensurePrefetchWorker(track)
				_, jumped := track.updateCursor(m.ctx, idx)
				if jumped {
					m.cache.evictBehind(track, idx)
				}
			}
		}
		m.serveDirectResource(w, r, track.resource, track.mediaType)
		return
	}

	isInit := rng == track.init
	fragIdx := -1
	if !isInit {
		if idx, found := track.fragIndex[rng]; found {
			fragIdx = idx
		} else {
			if idx := track.fragmentAtByte(rng.First); idx >= 0 {
				m.ensurePrefetchWorker(track)
				_, jumped := track.updateCursor(m.ctx, idx)
				if jumped {
					m.cache.evictBehind(track, idx)
				}
			}
			m.serveDirectResource(w, r, track.resource, track.mediaType)
			return
		}
	}

	// Reserve the requested segment in cache before updating the track cursor or
	// waking the background prefetch worker so in-flight foreground bytes are
	// always counted against the cache budget.
	seg, start := m.cache.lookupOrCreate(track, rng, fragIdx, isInit, fragIdx < 0)
	if seg == nil {
		http.Error(w, "Media unavailable", http.StatusBadGateway)
		return
	}
	if fragIdx >= 0 {
		_, jumped := track.advanceCursor(m.ctx, fragIdx)
		if jumped {
			m.cache.evictBehind(track, fragIdx)
		} else {
			m.cache.evictBehind(track, max(0, fragIdx-1))
		}
		if start {
			m.cache.evict()
		}
	}
	if start {
		m.mu.Lock()
		closed := m.closed
		if !closed {
			m.wg.Add(1)
		}
		m.mu.Unlock()
		if closed {
			m.cache.dropEntry(seg)
			http.Error(w, "Media unavailable", http.StatusBadGateway)
			return
		}
		go func() {
			defer m.wg.Done()
			m.fetchSegment(m.ctx, track, seg, true)
		}()
	}
	if fragIdx >= 0 {
		m.ensurePrefetchWorker(track)
		track.notifyWorker()
	}

	m.streamCachedSegment(w, r, seg)
}

func (m *Media) streamCachedSegment(w http.ResponseWriter, r *http.Request, seg *cachedSegment) {
	seg.mu.Lock()
	seg.readers++
	seg.mu.Unlock()
	defer func() {
		seg.mu.Lock()
		seg.readers--
		seg.mu.Unlock()
	}()

	stopWake := context.AfterFunc(r.Context(), func() {
		seg.mu.Lock()
		seg.cond.Broadcast()
		seg.mu.Unlock()
	})
	defer stopWake()

	headersSent := false
	sent := 0
	var out []byte
	for {
		seg.mu.Lock()
		for !headersSent && seg.status == 0 && !seg.done && r.Context().Err() == nil {
			seg.cond.Wait()
		}
		if r.Context().Err() != nil {
			seg.mu.Unlock()
			return
		}
		if !headersSent {
			if seg.status == 0 {
				seg.mu.Unlock()
				http.Error(w, "Media unavailable", http.StatusBadGateway)
				return
			}
			for k, vals := range seg.headers {
				for _, v := range vals {
					w.Header().Add(k, v)
				}
			}
			w.WriteHeader(seg.status)
			headersSent = true
		}
		for sent == len(seg.buf) && !seg.done && r.Context().Err() == nil {
			seg.cond.Wait()
		}
		if r.Context().Err() != nil {
			seg.mu.Unlock()
			return
		}
		avail := len(seg.buf)
		done := seg.done
		var chunk []byte
		if avail > sent {
			if done {
				// seg.buf is immutable once done; write directly without copying.
				chunk = seg.buf[sent:avail]
			} else {
				limit := min(avail, sent+256*1024)
				out = append(out[:0], seg.buf[sent:limit]...)
				chunk = out
			}
		}
		seg.mu.Unlock()

		if len(chunk) > 0 {
			if _, err := w.Write(chunk); err != nil {
				return
			}
			sent += len(chunk)
		}
		if done && sent >= avail {
			return
		}
	}
}

func (m *Media) fetchSegment(ctx context.Context, track *dashTrack, seg *cachedSegment, foreground bool) {
	total := seg.expected
	offset := int64(0)
	failovers := 0
	reporter, _ := m.transport.(provider.PlaybackHealthReporter)
	var finalErr error

	for offset < total {
		if ctx.Err() != nil {
			finalErr = ctx.Err()
			break
		}
		byteRange := fmt.Sprintf("bytes=%d-%d", seg.key.first+offset, seg.key.last)
		attemptCtx, attemptCancel := context.WithCancel(ctx)
		attemptStart := time.Now()
		resp, err := m.transport.OpenResource(attemptCtx, track.resource, http.MethodGet, byteRange)
		if err != nil {
			attemptCancel()
			finalErr = err
			break
		}
		respURL := ""
		if resp.Request != nil && resp.Request.URL != nil {
			respURL = resp.Request.URL.String()
			u := respURL
			m.lastURL.Store(&u)
		}
		if offset > 0 {
			expectedPrefix := fmt.Sprintf("bytes %d-%d/", seg.key.first+offset, seg.key.last)
			if resp.StatusCode != http.StatusPartialContent || !strings.HasPrefix(resp.Header.Get("Content-Range"), expectedPrefix) {
				resp.Body.Close()
				attemptCancel()
				finalErr = io.ErrUnexpectedEOF
				break
			}
		} else {
			expectedPrefix := fmt.Sprintf("bytes %d-%d/", seg.key.first, seg.key.last)
			if resp.StatusCode != http.StatusPartialContent || !strings.HasPrefix(resp.Header.Get("Content-Range"), expectedPrefix) {
				resp.Body.Close()
				attemptCancel()
				finalErr = &provider.Error{Code: provider.Unsupported}
				break
			}
			hdr := make(http.Header)
			for _, name := range []string{"Accept-Ranges", "Content-Length", "Content-Range", "Content-Type"} {
				if value := resp.Header.Get(name); value != "" {
					hdr.Set(name, value)
				}
			}
			if track.mediaType != "" {
				hdr.Set("Content-Type", track.mediaType)
			}
			seg.mu.Lock()
			if seg.status == 0 {
				seg.status = resp.StatusCode
				seg.headers = hdr
				if seg.buf == nil && total > 0 && total <= maxCachedSegmentBytes {
					seg.buf = make([]byte, 0, total)
				}
				seg.cond.Broadcast()
			}
			seg.mu.Unlock()
		}

		canFailover := failovers == 0 && len(track.resource.URLs) > 1 && reporter != nil && reporter.HasHealthyAlternative(track.resource.URLs, respURL)
		var slowAborted atomic.Bool
		var lastProgress atomic.Int64
		lastProgress.Store(time.Now().UnixNano())
		watchDone := make(chan struct{})
		if canFailover {
			go func() {
				ticker := time.NewTicker(slowStallCheckInterval)
				defer ticker.Stop()
				for {
					select {
					case <-watchDone:
						return
					case <-attemptCtx.Done():
						return
					case now := <-ticker.C:
						idle := time.Duration(now.UnixNano() - lastProgress.Load())
						if idle >= slowStallTimeout {
							slowAborted.Store(true)
							attemptCancel()
							return
						}
					}
				}
			}()
		}

		buf := make([]byte, 128*1024)
		var attemptBytes int64
		var readErr error
		for offset < total {
			n, rerr := resp.Body.Read(buf)
			if n > 0 {
				now := time.Now()
				lastProgress.Store(now.UnixNano())
				m.speed.record(int64(n), now)
				if int64(n) > total-offset {
					n = int(total - offset)
				}
				seg.mu.Lock()
				seg.buf = append(seg.buf, buf[:n]...)
				seg.cond.Broadcast()
				seg.mu.Unlock()
				attemptBytes += int64(n)
				offset += int64(n)
			}
			if canFailover && !slowAborted.Load() && offset < total {
				elapsed := time.Since(attemptStart)
				if elapsed >= slowReadMinAge {
					bps := float64(attemptBytes) / elapsed.Seconds()
					minBps := max(float64(minSlowFloorBps), track.bandwidthBps*0.45)
					remaining := float64(total - offset)
					if bps < minBps && (bps <= 1 || remaining/bps > slowRemainingSeconds) {
						slowAborted.Store(true)
						attemptCancel()
						break
					}
				}
			}
			if rerr != nil {
				if !errors.Is(rerr, io.EOF) {
					readErr = rerr
				}
				break
			}
		}

		close(watchDone)
		resp.Body.Close()
		attemptCancel()

		elapsed := time.Since(attemptStart)
		if attemptBytes > 0 {
			m.speed.recordTransfer(attemptBytes, elapsed)
		}

		if slowAborted.Load() && ctx.Err() == nil && failovers == 0 {
			if mediaTrace {
				log.Printf("player-media: dash slow failover track=%s frag=%d offset=%d/%d elapsed=%s",
					track.name, seg.fragIdx, offset, total, elapsed.Round(time.Millisecond))
			}
			reporter.ReportSlow(respURL)
			failovers++
			continue
		}
		if readErr != nil {
			if ctx.Err() == nil && failovers == 0 && len(track.resource.URLs) > 1 && reporter != nil && reporter.HasHealthyAlternative(track.resource.URLs, respURL) {
				if mediaTrace {
					log.Printf("player-media: dash read failover track=%s frag=%d err=%v", track.name, seg.fragIdx, readErr)
				}
				reporter.ReportSlow(respURL)
				failovers++
				continue
			}
			finalErr = readErr
			break
		}
		if offset < total {
			finalErr = io.ErrUnexpectedEOF
			break
		}
	}

	if finalErr != nil {
		if foreground && ctx.Err() == nil {
			if mediaTrace {
				log.Printf("player-media: dash foreground fetch failed track=%s frag=%d err=%v", track.name, seg.fragIdx, finalErr)
			}
			m.recordFailure(finalErr)
		}
		seg.mu.Lock()
		seg.err = finalErr
		seg.done = true
		seg.fetching = false
		seg.cond.Broadcast()
		seg.mu.Unlock()
		m.cache.dropEntry(seg)
		return
	}

	seg.mu.Lock()
	seg.done = true
	seg.fetching = false
	seg.cond.Broadcast()
	seg.mu.Unlock()
}

// Warmup prefetches the initialization bytes and opening segments into a capped
// cache budget so a preloaded next episode can start immediately when promoted.
func (m *Media) Warmup(ctx context.Context) error {
	if m == nil || m.cache == nil || len(m.tracks) == 0 {
		return nil
	}
	m.cache.setWarmupMode(true)
	warmCtx, cancel := context.WithCancel(ctx)
	stopParent := context.AfterFunc(m.ctx, cancel)
	defer func() {
		stopParent()
		cancel()
	}()

	for _, track := range m.tracks {
		if !track.isVideo && m.activeAudioName != "" && track.name != m.activeAudioName {
			continue
		}
		if warmCtx.Err() != nil {
			return warmCtx.Err()
		}
		// Ensure init range is cached (video init is already seeded by OpenMedia).
		if track.init.Last >= track.init.First && track.init.Last-track.init.First+1 <= maxCachedSegmentBytes {
			if seg, start := m.cache.lookupOrCreate(track, track.init, -1, true, true); seg != nil && start {
				m.fetchSegment(warmCtx, track, seg, false)
			}
		}
		var dur float64
		for i := 0; i < len(track.fragments) && i < 2; i++ {
			if warmCtx.Err() != nil {
				return warmCtx.Err()
			}
			if dur >= warmupPrefetchSeconds {
				break
			}
			frag := track.fragments[i]
			segSize := frag.Last - frag.First + 1
			if segSize > maxCachedSegmentBytes {
				break
			}
			seg, start, hasRoom := m.cache.tryStartPrefetch(track, frag.PlaybackRange, i)
			if !hasRoom {
				break
			}
			if start {
				m.fetchSegment(warmCtx, track, seg, false)
			}
			dur += frag.duration
		}
	}
	return warmCtx.Err()
}

// Promote lifts a warmed-up Media instance to its full active playback cache budget.
func (m *Media) Promote() {
	if m == nil || m.cache == nil {
		return
	}
	m.cache.setWarmupMode(false)
	for _, track := range m.tracks {
		select {
		case track.wake <- struct{}{}:
		default:
		}
	}
}
