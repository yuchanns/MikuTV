package player

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"io"
	"math"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/yuchanns/MikuTV/internal/provider"
)

const (
	pgsMaxFileBytes  = 256 << 20 // 256 MiB disk cache cap for a single PGS track
	pgsMaxObjWidth   = 4096
	pgsMaxObjHeight  = 2160
	pgsMaxObjPayload = 8 << 20
	pgsNotifyStride  = 32

	pgsSegPDS = 0x14
	pgsSegODS = 0x15
	pgsSegPCS = 0x16
	pgsSegWDS = 0x17
	pgsSegEND = 0x80
)

// PGSFrame holds a decoded bitmap subtitle frame positioned in video-canvas
// coordinates (VideoWidth x VideoHeight).
type PGSFrame struct {
	CueIndex    int
	Image       *image.NRGBA
	X           int
	Y           int
	VideoWidth  int
	VideoHeight int
}

type pgsSpan struct {
	offset int64
	length uint32
}

type pgsCueObj struct {
	x       int
	y       int
	cropped bool
	cropX   int
	cropY   int
	cropW   int
	cropH   int
	spans   []pgsSpan
}

type pgsCue struct {
	start       float64
	end         float64
	videoWidth  int
	videoHeight int
	palette     pgsSpan
	objects     []pgsCueObj
}

// PGSTrack progressively streams a Blu-ray Presentation Graphic Stream (.sup)
// into a temporary file, indexing Display Sets with minimal memory overhead
// and decoding individual RGBA cue bitmaps on demand.
type PGSTrack struct {
	fileMu   sync.RWMutex
	file     *os.File
	filePath string

	cancel context.CancelFunc
	done   chan struct{}

	mu         sync.RWMutex
	cues       []pgsCue
	eof        bool
	streamErr  error
	closed     bool
	cachedIdx  int
	cachedFrm  PGSFrame
	hasCached  bool
}

// IsPGSTrack reports whether a subtitle track delivers Blu-ray PGS bitmap data.
func IsPGSTrack(track provider.PlaybackTrack) bool {
	codec := strings.ToLower(strings.TrimSpace(track.Codec))
	if codec == "pgssub" || codec == "pgs" || codec == "hdmv_pgs_subtitle" || codec == "sup" {
		return true
	}
	ct := strings.ToLower(strings.TrimSpace(track.ContentType))
	if idx := strings.IndexByte(ct, ';'); idx >= 0 {
		ct = strings.TrimSpace(ct[:idx])
	}
	return ct == "application/x-pgs" || ct == "application/pgs"
}

// OpenPGSTrack fetches track.Resource over transport and starts progressively
// indexing its PGS Display Sets. onUpdate is invoked asynchronously when new
// cues become available or streaming completes.
func OpenPGSTrack(ctx context.Context, transport provider.PlaybackTransport, track provider.PlaybackTrack, onUpdate func()) (*PGSTrack, error) {
	if transport == nil || len(track.Resource.URLs) == 0 {
		return nil, &provider.Error{Code: provider.Unsupported}
	}
	streamCtx, cancel := context.WithCancel(ctx)
	var resp *http.Response
	var err error
	if subTransport, ok := transport.(provider.PlaybackSubtitleTransport); ok {
		resp, err = subTransport.OpenSubtitleResource(streamCtx, track.Resource)
	} else {
		resp, err = transport.OpenResource(streamCtx, track.Resource, http.MethodGet, "")
	}
	if err != nil {
		cancel()
		return nil, err
	}
	tmp, err := os.CreateTemp("", "mikutv-pgs-*.sup")
	if err != nil {
		resp.Body.Close()
		cancel()
		return nil, err
	}
	t := &PGSTrack{
		file:      tmp,
		filePath:  tmp.Name(),
		cancel:    cancel,
		done:      make(chan struct{}),
		cachedIdx: -1,
	}
	go t.ingest(streamCtx, resp.Body, onUpdate)
	return t, nil
}

// OpenPGSBytes builds a PGSTrack from an in-memory .sup payload (primarily for testing).
func OpenPGSBytes(data []byte) (*PGSTrack, error) {
	tmp, err := os.CreateTemp("", "mikutv-pgs-*.sup")
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	t := &PGSTrack{
		file:      tmp,
		filePath:  tmp.Name(),
		cancel:    cancel,
		done:      make(chan struct{}),
		cachedIdx: -1,
	}
	t.ingest(ctx, io.NopCloser(bytes.NewReader(data)), nil)
	return t, nil
}

func (t *PGSTrack) Close() {
	if t == nil {
		return
	}
	t.cancel()
	<-t.done
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return
	}
	t.closed = true
	t.cues = nil
	t.hasCached = false
	t.cachedIdx = -1
	t.mu.Unlock()

	t.fileMu.Lock()
	f := t.file
	p := t.filePath
	t.file = nil
	t.filePath = ""
	if f != nil {
		_ = f.Close()
	}
	if p != "" {
		_ = os.Remove(p)
	}
	t.fileMu.Unlock()
}

func (t *PGSTrack) Err() error {
	if t == nil {
		return nil
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.streamErr
}

type pgsPendingPCS struct {
	valid       bool
	pts         float64
	videoWidth  int
	videoHeight int
	paletteID   byte
	objects     []pgsCompObject
}

type pgsCompObject struct {
	objectID uint16
	x        int
	y        int
	cropped  bool
	cropX    int
	cropY    int
	cropW    int
	cropH    int
}

func (t *PGSTrack) ingest(ctx context.Context, body io.ReadCloser, onUpdate func()) {
	defer close(t.done)
	defer body.Close()

	reader := bufio.NewReaderSize(io.LimitReader(body, pgsMaxFileBytes), 64<<10)
	writer := bufio.NewWriterSize(t.file, 64<<10)

	var filePos int64
	var header [13]byte
	payloadBuf := make([]byte, 0, 64<<10)

	epochPalettes := make(map[byte]pgsSpan)
	epochObjects := make(map[uint16][]pgsSpan)
	inProgressObj := make(map[uint16][]pgsSpan)
	var pcs pgsPendingPCS

	cuesSinceNotify := 0
	notifiedFirst := false

	flushWriter := func() error {
		return writer.Flush()
	}

	commitDisplaySet := func() {
		if !pcs.valid {
			return
		}
		pts := pcs.pts
		var cue pgsCue
		hasObjects := len(pcs.objects) > 0
		if hasObjects {
			pal, hasPal := epochPalettes[pcs.paletteID]
			if hasPal {
				objs := make([]pgsCueObj, 0, len(pcs.objects))
				for _, co := range pcs.objects {
					spans := epochObjects[co.objectID]
					if len(spans) == 0 {
						continue
					}
					copied := make([]pgsSpan, len(spans))
					copy(copied, spans)
					objs = append(objs, pgsCueObj{
						x:       co.x,
						y:       co.y,
						cropped: co.cropped,
						cropX:   co.cropX,
						cropY:   co.cropY,
						cropW:   co.cropW,
						cropH:   co.cropH,
						spans:   copied,
					})
				}
				if len(objs) > 0 {
					cue = pgsCue{
						start:       pts,
						videoWidth:  pcs.videoWidth,
						videoHeight: pcs.videoHeight,
						palette:     pal,
						objects:     objs,
					}
				} else {
					hasObjects = false
				}
			} else {
				hasObjects = false
			}
		}
		pcs = pgsPendingPCS{}

		_ = flushWriter()
		t.mu.Lock()
		if n := len(t.cues); n > 0 && t.cues[n-1].end == 0 && pts >= t.cues[n-1].start {
			t.cues[n-1].end = pts
		}
		if hasObjects {
			t.cues = append(t.cues, cue)
			cuesSinceNotify++
		}
		t.mu.Unlock()

		if onUpdate != nil && ((!notifiedFirst && hasObjects) || cuesSinceNotify >= pgsNotifyStride) {
			notifiedFirst = true
			cuesSinceNotify = 0
			onUpdate()
		}
	}

	for {
		if ctx.Err() != nil {
			break
		}
		if _, err := io.ReadFull(reader, header[:]); err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) && ctx.Err() == nil {
				t.mu.Lock()
				t.streamErr = err
				t.mu.Unlock()
			}
			break
		}
		if header[0] != 'P' || header[1] != 'G' {
			t.mu.Lock()
			t.streamErr = errors.New("invalid PGS segment magic")
			t.mu.Unlock()
			break
		}
		ptsTicks := binary.BigEndian.Uint32(header[2:6])
		segType := header[10]
		segLen := int(binary.BigEndian.Uint16(header[11:13]))

		if cap(payloadBuf) < segLen {
			payloadBuf = make([]byte, segLen)
		} else {
			payloadBuf = payloadBuf[:segLen]
		}
		if segLen > 0 {
			if _, err := io.ReadFull(reader, payloadBuf); err != nil {
				if !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) && ctx.Err() == nil {
					t.mu.Lock()
					t.streamErr = err
					t.mu.Unlock()
				}
				break
			}
		}

		// Persist only PDS and ODS payloads needed for on-demand bitmap decode.
		var span pgsSpan
		if (segType == pgsSegPDS || segType == pgsSegODS) && segLen > 0 {
			span = pgsSpan{offset: filePos, length: uint32(segLen)}
			if _, err := writer.Write(payloadBuf); err != nil {
				t.mu.Lock()
				t.streamErr = err
				t.mu.Unlock()
				break
			}
			filePos += int64(segLen)
		}

		switch segType {
		case pgsSegPCS:
			if segLen < 11 {
				continue
			}
			vw := int(binary.BigEndian.Uint16(payloadBuf[0:2]))
			vh := int(binary.BigEndian.Uint16(payloadBuf[2:4]))
			compState := payloadBuf[7]
			palID := payloadBuf[9]
			numObjs := int(payloadBuf[10])
			if compState == 0x80 {
				clear(epochPalettes)
				clear(epochObjects)
				clear(inProgressObj)
			}
			objs := make([]pgsCompObject, 0, numObjs)
			off := 11
			for i := 0; i < numObjs && off+8 <= segLen; i++ {
				oid := binary.BigEndian.Uint16(payloadBuf[off : off+2])
				cropFlag := payloadBuf[off+3]
				ox := int(binary.BigEndian.Uint16(payloadBuf[off+4 : off+6]))
				oy := int(binary.BigEndian.Uint16(payloadBuf[off+6 : off+8]))
				off += 8
				co := pgsCompObject{objectID: oid, x: ox, y: oy}
				if cropFlag&0x80 != 0 {
					if off+8 > segLen {
						break
					}
					co.cropped = true
					co.cropX = int(binary.BigEndian.Uint16(payloadBuf[off : off+2]))
					co.cropY = int(binary.BigEndian.Uint16(payloadBuf[off+2 : off+4]))
					co.cropW = int(binary.BigEndian.Uint16(payloadBuf[off+4 : off+6]))
					co.cropH = int(binary.BigEndian.Uint16(payloadBuf[off+6 : off+8]))
					off += 8
				}
				objs = append(objs, co)
			}
			pcs = pgsPendingPCS{
				valid:       true,
				pts:         float64(ptsTicks) / 90000.0,
				videoWidth:  vw,
				videoHeight: vh,
				paletteID:   palID,
				objects:     objs,
			}

		case pgsSegPDS:
			if segLen >= 2 {
				palID := payloadBuf[0]
				epochPalettes[palID] = span
			}

		case pgsSegODS:
			if segLen < 4 {
				continue
			}
			oid := binary.BigEndian.Uint16(payloadBuf[0:2])
			seqFlag := payloadBuf[3]
			isFirst := seqFlag&0x80 != 0
			isLast := seqFlag&0x40 != 0
			if isFirst {
				inProgressObj[oid] = []pgsSpan{span}
			} else if cur := inProgressObj[oid]; len(cur) > 0 {
				inProgressObj[oid] = append(cur, span)
			} else {
				continue
			}
			if isLast {
				epochObjects[oid] = inProgressObj[oid]
				delete(inProgressObj, oid)
			}

		case pgsSegEND:
			commitDisplaySet()
		}
	}

	_ = flushWriter()
	t.mu.Lock()
	t.eof = true
	t.mu.Unlock()
	if onUpdate != nil && ctx.Err() == nil {
		onUpdate()
	}
}

// FrameAt returns the decoded PGS bitmap subtitle frame active at playback
// position (in seconds). Results are cached per cue index so repeated UI
// ticks perform zero decoding work while the same cue remains on screen.
func (t *PGSTrack) FrameAt(position float64) (PGSFrame, bool) {
	if t == nil || position < 0 {
		return PGSFrame{}, false
	}
	t.mu.RLock()
	if t.closed || t.file == nil || len(t.cues) == 0 {
		t.mu.RUnlock()
		return PGSFrame{}, false
	}
	idx := sort.Search(len(t.cues), func(i int) bool { return t.cues[i].start > position }) - 1
	if idx < 0 {
		t.mu.RUnlock()
		return PGSFrame{}, false
	}
	cue := t.cues[idx]
	eof := t.eof
	if cue.end > 0 && position >= cue.end {
		t.mu.RUnlock()
		return PGSFrame{}, false
	}
	if cue.end == 0 && eof && position > cue.start+15 {
		t.mu.RUnlock()
		return PGSFrame{}, false
	}
	if t.hasCached && t.cachedIdx == idx {
		frm := t.cachedFrm
		t.mu.RUnlock()
		return frm, true
	}
	t.mu.RUnlock()

	t.fileMu.RLock()
	f := t.file
	var frm PGSFrame
	var ok bool
	if f != nil {
		frm, ok = decodePGSCue(f, idx, cue)
	}
	t.fileMu.RUnlock()
	if !ok {
		return PGSFrame{}, false
	}
	t.mu.Lock()
	if !t.closed {
		t.cachedIdx = idx
		t.cachedFrm = frm
		t.hasCached = true
	}
	t.mu.Unlock()
	return frm, true
}

func decodePGSCue(f *os.File, idx int, cue pgsCue) (PGSFrame, bool) {
	if len(cue.objects) == 0 || cue.palette.length < 2 || cue.videoWidth <= 0 || cue.videoHeight <= 0 {
		return PGSFrame{}, false
	}
	palBuf := make([]byte, cue.palette.length)
	if _, err := f.ReadAt(palBuf, cue.palette.offset); err != nil {
		return PGSFrame{}, false
	}
	var palette [256]color.NRGBA
	for off := 2; off+5 <= len(palBuf); off += 5 {
		entryID := palBuf[off]
		palette[entryID] = pgsYCrCbAToNRGBA(palBuf[off+1], palBuf[off+2], palBuf[off+3], palBuf[off+4])
	}

	type decodedObj struct {
		obj    pgsCueObj
		width  int
		height int
		pixels []byte
	}
	decoded := make([]decodedObj, 0, len(cue.objects))
	minX, minY := math.MaxInt, math.MaxInt
	maxX, maxY := 0, 0

	for _, co := range cue.objects {
		w, h, rle, ok := readPGSObject(f, co.spans)
		if !ok || w <= 0 || h <= 0 || w > pgsMaxObjWidth || h > pgsMaxObjHeight {
			continue
		}
		pix := decodePGSRLE(rle, w, h)
		drawW, drawH := w, h
		if co.cropped {
			if co.cropW > 0 && co.cropW < drawW {
				drawW = co.cropW
			}
			if co.cropH > 0 && co.cropH < drawH {
				drawH = co.cropH
			}
		}
		x0 := max(0, co.x)
		y0 := max(0, co.y)
		x1 := min(cue.videoWidth, co.x+drawW)
		y1 := min(cue.videoHeight, co.y+drawH)
		if x1 <= x0 || y1 <= y0 {
			continue
		}
		if x0 < minX {
			minX = x0
		}
		if y0 < minY {
			minY = y0
		}
		if x1 > maxX {
			maxX = x1
		}
		if y1 > maxY {
			maxY = y1
		}
		decoded = append(decoded, decodedObj{obj: co, width: w, height: h, pixels: pix})
	}
	if len(decoded) == 0 || maxX <= minX || maxY <= minY {
		return PGSFrame{}, false
	}

	boxW := maxX - minX
	boxH := maxY - minY
	img := image.NewNRGBA(image.Rect(0, 0, boxW, boxH))
	hasVisible := false
	for _, d := range decoded {
		srcX0, srcY0 := 0, 0
		srcW, srcH := d.width, d.height
		if d.obj.cropped {
			srcX0 = max(0, d.obj.cropX)
			srcY0 = max(0, d.obj.cropY)
			if srcX0 >= d.width || srcY0 >= d.height {
				continue
			}
			srcW = d.width - srcX0
			srcH = d.height - srcY0
			if d.obj.cropW > 0 && d.obj.cropW < srcW {
				srcW = d.obj.cropW
			}
			if d.obj.cropH > 0 && d.obj.cropH < srcH {
				srcH = d.obj.cropH
			}
		}
		for sy := 0; sy < srcH; sy++ {
			dy := d.obj.y + sy - minY
			if dy < 0 || dy >= boxH {
				continue
			}
			srcRow := (srcY0 + sy) * d.width
			dstRow := dy * img.Stride
			for sx := 0; sx < srcW; sx++ {
				dx := d.obj.x + sx - minX
				if dx < 0 || dx >= boxW {
					continue
				}
				c := palette[d.pixels[srcRow+srcX0+sx]]
				if c.A == 0 {
					continue
				}
				hasVisible = true
				off := dstRow + dx*4
				img.Pix[off] = c.R
				img.Pix[off+1] = c.G
				img.Pix[off+2] = c.B
				img.Pix[off+3] = c.A
			}
		}
	}
	if !hasVisible {
		return PGSFrame{}, false
	}
	return PGSFrame{
		CueIndex:    idx,
		Image:       img,
		X:           minX,
		Y:           minY,
		VideoWidth:  cue.videoWidth,
		VideoHeight: cue.videoHeight,
	}, true
}

func readPGSObject(f *os.File, spans []pgsSpan) (width, height int, rle []byte, ok bool) {
	if len(spans) == 0 {
		return 0, 0, nil, false
	}
	var total int
	for _, sp := range spans {
		total += int(sp.length)
		if total > pgsMaxObjPayload {
			return 0, 0, nil, false
		}
	}
	firstBuf := make([]byte, spans[0].length)
	if _, err := f.ReadAt(firstBuf, spans[0].offset); err != nil || len(firstBuf) < 11 {
		return 0, 0, nil, false
	}
	width = int(binary.BigEndian.Uint16(firstBuf[7:9]))
	height = int(binary.BigEndian.Uint16(firstBuf[9:11]))
	if len(spans) == 1 {
		return width, height, firstBuf[11:], true
	}
	rle = make([]byte, 0, total)
	rle = append(rle, firstBuf[11:]...)
	for i := 1; i < len(spans); i++ {
		if spans[i].length <= 4 {
			continue
		}
		buf := make([]byte, spans[i].length)
		if _, err := f.ReadAt(buf, spans[i].offset); err != nil {
			return 0, 0, nil, false
		}
		rle = append(rle, buf[4:]...)
	}
	return width, height, rle, true
}

func decodePGSRLE(rle []byte, width, height int) []byte {
	out := make([]byte, width*height)
	x, y := 0, 0
	for i := 0; i < len(rle) && y < height; {
		b := rle[i]
		i++
		if b != 0 {
			if x < width {
				out[y*width+x] = b
				x++
			}
			continue
		}
		if i >= len(rle) {
			break
		}
		ctrl := rle[i]
		i++
		if ctrl == 0 {
			x = 0
			y++
			continue
		}
		var count int
		var colorIdx byte
		switch ctrl & 0xC0 {
		case 0x00:
			count = int(ctrl & 0x3F)
			colorIdx = 0
		case 0x40:
			if i >= len(rle) {
				return out
			}
			count = (int(ctrl&0x3F) << 8) | int(rle[i])
			i++
			colorIdx = 0
		case 0x80:
			if i >= len(rle) {
				return out
			}
			count = int(ctrl & 0x3F)
			colorIdx = rle[i]
			i++
		case 0xC0:
			if i+1 >= len(rle) {
				return out
			}
			count = (int(ctrl&0x3F) << 8) | int(rle[i])
			colorIdx = rle[i+1]
			i += 2
		}
		rowOff := y * width
		for k := 0; k < count && x < width; k++ {
			out[rowOff+x] = colorIdx
			x++
		}
	}
	return out
}

// pgsYCrCbAToNRGBA converts limited-range BT.709 Y/Cr/Cb + Alpha from a
// Blu-ray PGS Palette Definition Segment into straight-alpha NRGBA.
func pgsYCrCbAToNRGBA(yByte, crByte, cbByte, alpha byte) color.NRGBA {
	y := (float64(yByte) - 16.0) * 1.16438356
	cr := float64(crByte) - 128.0
	cb := float64(cbByte) - 128.0
	r := y + 1.79274107*cr
	g := y - 0.21324861*cb - 0.53290933*cr
	b := y + 2.11240179*cb
	return color.NRGBA{
		R: clampU8(r),
		G: clampU8(g),
		B: clampU8(b),
		A: alpha,
	}
}

func clampU8(v float64) uint8 {
	if v <= 0 {
		return 0
	}
	if v >= 255 {
		return 255
	}
	return uint8(math.Round(v))
}
