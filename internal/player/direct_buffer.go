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
	"time"

	"github.com/yuchanns/MikuTV/internal/provider"
)

const (
	directChunkSize         int64 = 256 << 10 // 256 KiB per cached block
	directAudioBudget       int64 = 48 << 20  // 48 MiB full-track RAM buffer for lossless/MP3 audio
	directVideoBudget       int64 = 16 << 20  // 16 MiB bounded read-ahead window for direct MP4
	directWarmupBudget      int64 = 4 << 20   // 4 MiB warmup cap during preload/mode-switch
	directWarmupTarget      int64 = 1 << 20   // 1 MiB minimum pre-buffered before Warmup returns
	directTailProbeMax      int64 = 512 << 10 // Small tail probes (ID3v1/moov) don't reset sequential pump
	directSeekJumpThreshold int64 = 2 << 20   // Jump distance that repositions the sequential pump
	directPumpMaxRetries          = 4
)

// directBuffer buffers a PlaybackDirect media stream in bounded RAM using a
// single continuous sequential upstream fetch with automatic Range resume.
// Repeated AVPlayer Range requests, scrubbing, and single-track loops are
// served directly from memory without round-tripping to the upstream CDN.
type directBuffer struct {
	media     *Media
	resource  provider.PlaybackResource
	mediaType string
	audioOnly bool

	mu           sync.Mutex
	cond         *sync.Cond
	probed       bool
	probeErr     error
	passthrough  bool
	totalSize    int64
	acceptRanges bool
	contentType  string

	chunks       map[int64][]byte
	pinned       map[int64]bool
	used         int64
	budget       int64
	activeBudget int64
	readCursor   int64

	pumpRunning bool
	pumpEpoch   uint64
	pumpStart   int64
	pumpOffset  int64
	pumpCancel  context.CancelFunc
	pumpDone    bool
	pumpErr     error
	closed      bool
}

func newDirectBuffer(m *Media, resource provider.PlaybackResource, mediaType string, audioOnly bool) *directBuffer {
	budget := directVideoBudget
	if audioOnly {
		budget = directAudioBudget
	}
	b := &directBuffer{
		media:        m,
		resource:     resource,
		mediaType:    mediaType,
		audioOnly:    audioOnly,
		chunks:       make(map[int64][]byte),
		pinned:       map[int64]bool{0: true},
		budget:       budget,
		activeBudget: budget,
	}
	b.cond = sync.NewCond(&b.mu)
	return b
}

func (b *directBuffer) close() {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	b.closed = true
	cancel := b.pumpCancel
	b.pumpCancel = nil
	b.pumpEpoch++
	clear(b.chunks)
	b.used = 0
	b.cond.Broadcast()
	b.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (b *directBuffer) setWarmupMode(warmup bool) {
	b.mu.Lock()
	if warmup {
		b.budget = min(b.activeBudget, directWarmupBudget)
	} else {
		b.budget = b.activeBudget
	}
	b.evictLocked(-1)
	b.cond.Broadcast()
	b.mu.Unlock()
}

// warmup opens the sequential pump under the capped warmup budget and waits
// until either the opening bytes are buffered in RAM or ctx expires.
func (b *directBuffer) warmup(ctx context.Context) error {
	b.setWarmupMode(true)
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return context.Canceled
	}
	b.startPumpLocked(0)
	stopWake := context.AfterFunc(ctx, func() {
		b.mu.Lock()
		b.cond.Broadcast()
		b.mu.Unlock()
	})
	defer stopWake()
	for {
		if b.closed || ctx.Err() != nil {
			b.mu.Unlock()
			return ctx.Err()
		}
		if b.probed {
			if b.passthrough || b.probeErr != nil {
				err := b.probeErr
				b.mu.Unlock()
				return err
			}
			target := min(b.totalSize, directWarmupTarget)
			if b.contiguousFromZeroLocked() >= target || b.pumpDone {
				b.mu.Unlock()
				return nil
			}
		}
		b.cond.Wait()
	}
}

func (b *directBuffer) contiguousFromZeroLocked() int64 {
	var total int64
	for idx := int64(0); ; idx++ {
		chunk := b.chunks[idx]
		if len(chunk) == 0 {
			return total
		}
		total += int64(len(chunk))
		want := b.chunkTargetSizeLocked(idx)
		if int64(len(chunk)) < want {
			return total
		}
		if total >= b.totalSize {
			return b.totalSize
		}
	}
}

func (b *directBuffer) chunkTargetSizeLocked(idx int64) int64 {
	start := idx * directChunkSize
	if b.totalSize > 0 && start+directChunkSize > b.totalSize {
		return max(b.totalSize-start, 0)
	}
	return directChunkSize
}

func (b *directBuffer) startPumpLocked(startOffset int64) {
	if b.closed {
		return
	}
	if startOffset < 0 {
		startOffset = 0
	}
	// Align pump start to chunk boundary so chunk slices stay contiguous from offset 0.
	startOffset = (startOffset / directChunkSize) * directChunkSize
	if b.pumpRunning && !b.pumpDone && b.pumpStart <= startOffset && startOffset <= b.pumpOffset+directSeekJumpThreshold {
		return
	}
	if b.pumpCancel != nil {
		b.pumpCancel()
	}
	pctx, pcancel := context.WithCancel(b.media.ctx)
	b.pumpEpoch++
	epoch := b.pumpEpoch
	b.pumpCancel = pcancel
	b.pumpRunning = true
	b.pumpDone = false
	b.pumpErr = nil
	b.pumpStart = startOffset
	b.pumpOffset = startOffset
	b.readCursor = startOffset
	b.media.wg.Add(1)
	go b.runPump(pctx, pcancel, epoch, startOffset)
}

func (b *directBuffer) runPump(ctx context.Context, cancel context.CancelFunc, epoch uint64, startOffset int64) {
	defer b.media.wg.Done()
	defer cancel()

	offset := startOffset
	retries := 0
	buf := make([]byte, 64*1024)

	for {
		if ctx.Err() != nil {
			return
		}
		b.mu.Lock()
		if b.closed || b.pumpEpoch != epoch {
			b.mu.Unlock()
			return
		}
		if b.probed && b.totalSize > 0 && offset >= b.totalSize {
			b.pumpRunning = false
			b.pumpDone = true
			b.cond.Broadcast()
			b.mu.Unlock()
			return
		}
		b.mu.Unlock()

		byteRange := fmt.Sprintf("bytes=%d-", offset)
		started := time.Now()
		resp, err := b.media.transport.OpenResource(ctx, b.resource, http.MethodGet, byteRange)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			retries++
			if retries <= directPumpMaxRetries && b.probed && !b.passthrough {
				if !sleepWithContext(ctx, time.Duration(retries)*150*time.Millisecond) {
					return
				}
				continue
			}
			b.finishPumpError(epoch, err)
			return
		}

		if resp.Request != nil && resp.Request.URL != nil {
			u := resp.Request.URL.String()
			b.media.lastURL.Store(&u)
		}

		totalSize, acceptRanges, ct, ok := inspectDirectResponse(resp, offset, b.mediaType)
		if !ok {
			resp.Body.Close()
			b.mu.Lock()
			if b.pumpEpoch == epoch {
				if !b.probed {
					b.probed = true
					b.passthrough = true
				}
				b.pumpRunning = false
				b.pumpDone = true
				b.cond.Broadcast()
			}
			b.mu.Unlock()
			return
		}

		// If server ignored our non-zero Range and returned 200 OK from byte 0, restart offset at 0.
		if resp.StatusCode == http.StatusOK && offset > 0 {
			offset = 0
		}

		b.mu.Lock()
		if b.closed || b.pumpEpoch != epoch {
			b.mu.Unlock()
			resp.Body.Close()
			return
		}
		if !b.probed {
			b.probed = true
			b.totalSize = totalSize
			b.acceptRanges = acceptRanges || totalSize <= b.activeBudget
			b.contentType = ct
			b.cond.Broadcast()
		} else if b.totalSize <= 0 && totalSize > 0 {
			b.totalSize = totalSize
			b.cond.Broadcast()
		}
		b.mu.Unlock()

		var attemptBytes int64
		reader := &countingReader{r: resp.Body, meter: &b.media.speed}
		var readErr error
		for {
			if ctx.Err() != nil {
				resp.Body.Close()
				return
			}
			if !b.waitForBudget(ctx, epoch, offset) {
				resp.Body.Close()
				return
			}
			n, rerr := reader.Read(buf)
			if n > 0 {
				attemptBytes += int64(n)
				retries = 0
				if !b.commitBytes(epoch, offset, buf[:n]) {
					resp.Body.Close()
					return
				}
				offset += int64(n)
			}
			if rerr != nil {
				if !errors.Is(rerr, io.EOF) {
					readErr = rerr
				}
				break
			}
		}
		resp.Body.Close()
		if attemptBytes > 0 {
			b.media.speed.recordTransfer(attemptBytes, time.Since(started))
		}

		b.mu.Lock()
		if b.closed || b.pumpEpoch != epoch {
			b.mu.Unlock()
			return
		}
		complete := b.totalSize > 0 && offset >= b.totalSize
		canResume := b.acceptRanges && b.totalSize > 0 && offset < b.totalSize
		b.mu.Unlock()

		if complete {
			b.mu.Lock()
			if b.pumpEpoch == epoch {
				b.pumpRunning = false
				b.pumpDone = true
				b.cond.Broadcast()
			}
			b.mu.Unlock()
			return
		}
		if canResume && retries < directPumpMaxRetries && ctx.Err() == nil {
			retries++
			if mediaTrace {
				log.Printf("player-media: direct buffer resume offset=%d retry=%d err=%v", offset, retries, readErr)
			}
			if !sleepWithContext(ctx, time.Duration(retries)*150*time.Millisecond) {
				return
			}
			continue
		}
		if readErr == nil {
			readErr = io.ErrUnexpectedEOF
		}
		b.finishPumpError(epoch, readErr)
		return
	}
}

func (b *directBuffer) finishPumpError(epoch uint64, err error) {
	b.mu.Lock()
	if b.pumpEpoch == epoch {
		if !b.probed {
			b.probed = true
			b.probeErr = err
		}
		b.pumpErr = err
		b.pumpRunning = false
		b.pumpDone = true
		b.cond.Broadcast()
	}
	b.mu.Unlock()
}

func (b *directBuffer) waitForBudget(ctx context.Context, epoch uint64, offset int64) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	chunkIdx := offset / directChunkSize
	for {
		if b.closed || ctx.Err() != nil || b.pumpEpoch != epoch {
			return false
		}
		if _, exists := b.chunks[chunkIdx]; exists || b.used+directChunkSize <= b.budget {
			return true
		}
		if b.evictOneBehindLocked(chunkIdx) {
			continue
		}
		b.cond.Wait()
	}
}

func (b *directBuffer) commitBytes(epoch uint64, offset int64, data []byte) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed || b.pumpEpoch != epoch {
		return false
	}
	pos := offset
	rem := data
	for len(rem) > 0 {
		if b.totalSize > 0 && pos >= b.totalSize {
			break
		}
		idx := pos / directChunkSize
		inChunk := int(pos % directChunkSize)
		target := int(b.chunkTargetSizeLocked(idx))
		avail := target - inChunk
		if avail <= 0 {
			break
		}
		take := min(len(rem), avail)
		existing := b.chunks[idx]
		if existing == nil {
			existing = make([]byte, 0, target)
		}
		if inChunk == len(existing) {
			existing = append(existing, rem[:take]...)
			b.used += int64(take)
		} else if inChunk < len(existing) {
			end := inChunk + take
			if end > len(existing) {
				extra := end - len(existing)
				existing = append(existing, make([]byte, extra)...)
				b.used += int64(extra)
			}
			copy(existing[inChunk:end], rem[:take])
		} else {
			// Gap inside chunk (should not happen with aligned pump); skip cleanly.
			pos += int64(take)
			rem = rem[take:]
			continue
		}
		b.chunks[idx] = existing
		pos += int64(take)
		rem = rem[take:]
	}
	if pos > b.pumpOffset {
		b.pumpOffset = pos
	}
	b.cond.Broadcast()
	return true
}

func (b *directBuffer) evictLocked(protectIdx int64) {
	for b.used > b.budget {
		if !b.evictOneBehindLocked(protectIdx) {
			return
		}
	}
}

func (b *directBuffer) evictOneBehindLocked(protectIdx int64) bool {
	cursorIdx := b.readCursor / directChunkSize
	// Prefer evicting the chunk furthest behind readCursor (never chunk 0 or protectIdx).
	var victim int64 = -1
	var bestDist int64 = -1
	for idx := range b.chunks {
		if b.pinned[idx] || idx == protectIdx {
			continue
		}
		if idx < cursorIdx {
			dist := cursorIdx - idx
			if victim < 0 || dist > bestDist {
				victim, bestDist = idx, dist
			}
		}
	}
	if victim < 0 && protectIdx >= 0 {
		// After a backward seek, evict the chunk furthest ahead of protectIdx so the
		// repositioned pump can always make forward progress under tight budgets.
		for idx := range b.chunks {
			if b.pinned[idx] || idx == protectIdx || idx <= cursorIdx+1 {
				continue
			}
			dist := idx - protectIdx
			if dist < 0 {
				dist = -dist
			}
			if dist > int64(directSeekJumpThreshold/directChunkSize) && (victim < 0 || dist > bestDist) {
				victim, bestDist = idx, dist
			}
		}
	}
	if victim < 0 {
		return false
	}
	b.used -= int64(len(b.chunks[victim]))
	delete(b.chunks, victim)
	return true
}

func (b *directBuffer) serve(w http.ResponseWriter, r *http.Request) {
	if !b.ensureProbed(r.Context()) {
		if r.Context().Err() != nil {
			return
		}
		b.mu.Lock()
		passthrough := b.passthrough
		err := b.probeErr
		b.mu.Unlock()
		if passthrough {
			b.media.serveDirectResource(w, r, b.resource, b.mediaType)
			return
		}
		if err != nil {
			b.media.recordFailure(err)
		}
		http.Error(w, "Media unavailable", http.StatusBadGateway)
		return
	}

	b.mu.Lock()
	if b.passthrough {
		b.mu.Unlock()
		b.media.serveDirectResource(w, r, b.resource, b.mediaType)
		return
	}
	totalSize := b.totalSize
	ct := b.contentType
	acceptRanges := b.acceptRanges
	b.mu.Unlock()

	if ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	if acceptRanges {
		w.Header().Set("Accept-Ranges", "bytes")
	}

	if r.Method == http.MethodHead {
		w.Header().Set("Content-Length", strconv.FormatInt(totalSize, 10))
		w.WriteHeader(http.StatusOK)
		return
	}

	rangeHdr := strings.TrimSpace(r.Header.Get("Range"))
	first, last, partial, ok := parseDirectByteRange(rangeHdr, totalSize)
	if !ok {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", totalSize))
		http.Error(w, "Range Not Satisfiable", http.StatusRequestedRangeNotSatisfiable)
		return
	}

	// Small tail probe (e.g. MP4 moov-at-end or ID3v1 tail) served directly without resetting pump.
	reqLen := last - first + 1
	b.mu.Lock()
	farAhead := first > b.pumpOffset+directSeekJumpThreshold
	hasFirst := b.hasByteLocked(first)
	b.mu.Unlock()
	if partial && acceptRanges && reqLen <= directTailProbeMax && farAhead && !hasFirst {
		b.media.serveDirectResource(w, r, b.resource, ct)
		return
	}

	if partial {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", first, last, totalSize))
		w.Header().Set("Content-Length", strconv.FormatInt(reqLen, 10))
		w.WriteHeader(http.StatusPartialContent)
	} else {
		w.Header().Set("Content-Length", strconv.FormatInt(totalSize, 10))
		w.WriteHeader(http.StatusOK)
	}

	stopWake := context.AfterFunc(r.Context(), func() {
		b.mu.Lock()
		b.cond.Broadcast()
		b.mu.Unlock()
	})
	defer stopWake()

	pos := first
	flusher, _ := w.(http.Flusher)
	for pos <= last {
		if r.Context().Err() != nil {
			return
		}
		b.mu.Lock()
		if pos > b.readCursor {
			b.readCursor = pos
			// Wake pump if it was sleeping waiting for eviction space behind readCursor.
			b.cond.Broadcast()
		}
		idx := pos / directChunkSize
		inChunk := int(pos % directChunkSize)
		chunk := b.chunks[idx]
		if len(chunk) > inChunk {
			avail := min(int64(len(chunk)-inChunk), last-pos+1)
			// Copy slice reference bytes out before releasing lock so concurrent append cannot race.
			out := make([]byte, avail)
			copy(out, chunk[inChunk:inChunk+int(avail)])
			b.mu.Unlock()
			if _, werr := w.Write(out); werr != nil {
				return
			}
			if flusher != nil && pos == first {
				flusher.Flush()
			}
			pos += avail
			continue
		}
		if b.closed {
			b.mu.Unlock()
			return
		}
		// Ensure pump covers pos: restart if pump stopped, or reposition on seek.
		if !b.pumpRunning || b.pumpDone || pos < (b.pumpOffset/directChunkSize)*directChunkSize || pos > b.pumpOffset+directSeekJumpThreshold {
			if b.pumpErr != nil && b.pumpDone && pos >= b.pumpOffset {
				b.mu.Unlock()
				return
			}
			b.startPumpLocked(pos)
		}
		b.cond.Wait()
		b.mu.Unlock()
	}
}

func (b *directBuffer) hasByteLocked(offset int64) bool {
	idx := offset / directChunkSize
	inChunk := int(offset % directChunkSize)
	return len(b.chunks[idx]) > inChunk
}

func (b *directBuffer) ensureProbed(ctx context.Context) bool {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return false
	}
	if !b.probed && !b.pumpRunning {
		b.startPumpLocked(0)
	}
	stopWake := context.AfterFunc(ctx, func() {
		b.mu.Lock()
		b.cond.Broadcast()
		b.mu.Unlock()
	})
	defer stopWake()
	for !b.probed && !b.closed && ctx.Err() == nil {
		b.cond.Wait()
	}
	ok := b.probed && !b.closed && b.probeErr == nil && !b.passthrough
	b.mu.Unlock()
	return ok
}

func inspectDirectResponse(resp *http.Response, reqOffset int64, fallbackCT string) (totalSize int64, acceptRanges bool, contentType string, ok bool) {
	if resp == nil || (resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent) {
		return 0, false, "", false
	}
	ct := fallbackCT
	if ct == "" {
		ct = resp.Header.Get("Content-Type")
	}
	if strings.EqualFold(strings.TrimSpace(resp.Header.Get("Accept-Ranges")), "bytes") {
		acceptRanges = true
	}
	if resp.StatusCode == http.StatusPartialContent {
		acceptRanges = true
		if cr := resp.Header.Get("Content-Range"); cr != "" {
			if slash := strings.LastIndexByte(cr, '/'); slash >= 0 {
				if n, err := strconv.ParseInt(strings.TrimSpace(cr[slash+1:]), 10, 64); err == nil && n > 0 {
					totalSize = n
				}
			}
		}
	}
	if totalSize <= 0 && resp.StatusCode == http.StatusOK && reqOffset == 0 {
		if cl := strings.TrimSpace(resp.Header.Get("Content-Length")); cl != "" {
			if n, err := strconv.ParseInt(cl, 10, 64); err == nil && n > 0 {
				totalSize = n
			}
		}
		if totalSize <= 0 && resp.ContentLength > 0 {
			totalSize = resp.ContentLength
		}
	}
	if totalSize <= 0 {
		return 0, false, ct, false
	}
	return totalSize, acceptRanges, ct, true
}

func parseDirectByteRange(header string, total int64) (first, last int64, partial, ok bool) {
	if header == "" {
		return 0, total - 1, false, total > 0
	}
	if !strings.HasPrefix(header, "bytes=") {
		return 0, 0, true, false
	}
	spec := strings.TrimSpace(strings.TrimPrefix(header, "bytes="))
	if spec == "" || strings.Contains(spec, ",") {
		return 0, 0, true, false
	}
	startStr, endStr, found := strings.Cut(spec, "-")
	if !found {
		return 0, 0, true, false
	}
	startStr, endStr = strings.TrimSpace(startStr), strings.TrimSpace(endStr)
	if startStr == "" {
		suffix, err := strconv.ParseInt(endStr, 10, 64)
		if err != nil || suffix <= 0 || total <= 0 {
			return 0, 0, true, false
		}
		if suffix > total {
			suffix = total
		}
		return total - suffix, total - 1, true, true
	}
	start, err := strconv.ParseInt(startStr, 10, 64)
	if err != nil || start < 0 || start >= total {
		return 0, 0, true, false
	}
	end := total - 1
	if endStr != "" {
		parsedEnd, err := strconv.ParseInt(endStr, 10, 64)
		if err != nil || parsedEnd < start {
			return 0, 0, true, false
		}
		if parsedEnd < end {
			end = parsedEnd
		}
	}
	return start, end, true, true
}

func sleepWithContext(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
