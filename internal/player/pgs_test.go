package player

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/yuchanns/MikuTV/internal/provider"
)

func buildPGSSegment(ptsSec float64, segType byte, payload []byte) []byte {
	buf := make([]byte, 13+len(payload))
	buf[0] = 'P'
	buf[1] = 'G'
	pts := uint32(ptsSec * 90000)
	binary.BigEndian.PutUint32(buf[2:6], pts)
	binary.BigEndian.PutUint32(buf[6:10], pts)
	buf[10] = segType
	binary.BigEndian.PutUint16(buf[11:13], uint16(len(payload)))
	copy(buf[13:], payload)
	return buf
}

func buildPCS(w, h uint16, compState, palID byte, objs ...struct {
	id      uint16
	x, y    uint16
	cropped bool
	cx, cy  uint16
	cw, ch  uint16
}) []byte {
	var buf bytes.Buffer
	_ = binary.Write(&buf, binary.BigEndian, w)
	_ = binary.Write(&buf, binary.BigEndian, h)
	buf.WriteByte(0x10) // frame rate
	_ = binary.Write(&buf, binary.BigEndian, uint16(1))
	buf.WriteByte(compState)
	buf.WriteByte(0x00) // palette update flag
	buf.WriteByte(palID)
	buf.WriteByte(byte(len(objs)))
	for _, o := range objs {
		_ = binary.Write(&buf, binary.BigEndian, o.id)
		buf.WriteByte(0) // window ID
		if o.cropped {
			buf.WriteByte(0x80)
		} else {
			buf.WriteByte(0x00)
		}
		_ = binary.Write(&buf, binary.BigEndian, o.x)
		_ = binary.Write(&buf, binary.BigEndian, o.y)
		if o.cropped {
			_ = binary.Write(&buf, binary.BigEndian, o.cx)
			_ = binary.Write(&buf, binary.BigEndian, o.cy)
			_ = binary.Write(&buf, binary.BigEndian, o.cw)
			_ = binary.Write(&buf, binary.BigEndian, o.ch)
		}
	}
	return buf.Bytes()
}

func buildPDS(palID byte, entries ...[5]byte) []byte {
	buf := make([]byte, 2+len(entries)*5)
	buf[0] = palID
	buf[1] = 0
	for i, e := range entries {
		copy(buf[2+i*5:], e[:])
	}
	return buf
}

func buildODSFragments(objID uint16, w, h uint16, rle []byte, splitAt int) [][]byte {
	dataLen := len(rle) + 4
	first := make([]byte, 0, 11+len(rle))
	first = append(first, byte(objID>>8), byte(objID), 0)
	if splitAt <= 0 || splitAt >= len(rle) {
		first = append(first, 0xC0)
		first = append(first, byte(dataLen>>16), byte(dataLen>>8), byte(dataLen))
		first = append(first, byte(w>>8), byte(w), byte(h>>8), byte(h))
		first = append(first, rle...)
		return [][]byte{first}
	}
	first = append(first, 0x80)
	first = append(first, byte(dataLen>>16), byte(dataLen>>8), byte(dataLen))
	first = append(first, byte(w>>8), byte(w), byte(h>>8), byte(h))
	first = append(first, rle[:splitAt]...)

	second := make([]byte, 0, 4+len(rle)-splitAt)
	second = append(second, byte(objID>>8), byte(objID), 0, 0x40)
	second = append(second, rle[splitAt:]...)
	return [][]byte{first, second}
}

func TestPGSTrackDecodesMultiFragmentEpochAndClear(t *testing.T) {
	if !IsPGSTrack(provider.PlaybackTrack{Codec: "pgssub"}) || !IsPGSTrack(provider.PlaybackTrack{ContentType: "application/x-pgs"}) {
		t.Fatal("expected IsPGSTrack to identify PGS tracks")
	}
	if IsPGSTrack(provider.PlaybackTrack{Codec: "vtt"}) {
		t.Fatal("expected non-PGS track to return false")
	}

	// White pixel (Y=235, Cr=128, Cb=128, A=255) at index 1; transparent at index 0.
	// Red pixel (Y=81, Cr=240, Cb=90, A=200) at index 2.
	pds := buildPDS(0,
		[5]byte{0, 16, 128, 128, 0},
		[5]byte{1, 235, 128, 128, 255},
		[5]byte{2, 81, 240, 90, 200},
	)
	// 4x2 bitmap: row0 = [1,1,1,1], row1 = [0,2,2,0]
	rle := []byte{
		0x00, 0x84, 0x01, 0x00, 0x00, // 4 pixels of color 1 + EOL
		0x00, 0x01, 0x02, 0x02, 0x00, 0x01, 0x00, 0x00, // 1x0, 2, 2, 1x0 + EOL
	}
	odsFrags := buildODSFragments(0, 4, 2, rle, 5)

	var sup bytes.Buffer
	// Display Set 1 at t=2.0s (Epoch Start, fragmented ODS)
	sup.Write(buildPGSSegment(2.0, pgsSegPCS, buildPCS(1920, 1080, 0x80, 0, struct {
		id      uint16
		x, y    uint16
		cropped bool
		cx, cy  uint16
		cw, ch  uint16
	}{id: 0, x: 100, y: 900})))
	sup.Write(buildPGSSegment(2.0, pgsSegPDS, pds))
	for _, frag := range odsFrags {
		sup.Write(buildPGSSegment(2.0, pgsSegODS, frag))
	}
	sup.Write(buildPGSSegment(2.0, pgsSegEND, nil))

	// Display Set 2 at t=4.0s (Normal state, reuses epoch object & palette at shifted position + cropped)
	sup.Write(buildPGSSegment(4.0, pgsSegPCS, buildPCS(1920, 1080, 0x00, 0, struct {
		id      uint16
		x, y    uint16
		cropped bool
		cx, cy  uint16
		cw, ch  uint16
	}{id: 0, x: 200, y: 880, cropped: true, cx: 1, cy: 1, cw: 2, ch: 1})))
	sup.Write(buildPGSSegment(4.0, pgsSegEND, nil))

	// Display Set 3 at t=6.0s (Clear screen)
	sup.Write(buildPGSSegment(6.0, pgsSegPCS, buildPCS(1920, 1080, 0x00, 0)))
	sup.Write(buildPGSSegment(6.0, pgsSegEND, nil))

	track, err := OpenPGSBytes(sup.Bytes())
	if err != nil {
		t.Fatalf("OpenPGSBytes: %v", err)
	}
	defer track.Close()

	if _, ok := track.FrameAt(1.5); ok {
		t.Fatal("expected no frame before first cue")
	}

	f1, ok := track.FrameAt(2.5)
	if !ok {
		t.Fatal("expected active frame at t=2.5s")
	}
	if f1.X != 100 || f1.Y != 900 || f1.Image.Bounds().Dx() != 4 || f1.Image.Bounds().Dy() != 2 {
		t.Fatalf("unexpected frame 1 bounds: x=%d y=%d bounds=%v", f1.X, f1.Y, f1.Image.Bounds())
	}
	topLeft := f1.Image.NRGBAAt(0, 0)
	if topLeft.R < 245 || topLeft.G < 245 || topLeft.B < 245 || topLeft.A != 255 {
		t.Fatalf("expected opaque white at (0,0), got %+v", topLeft)
	}
	if botLeft := f1.Image.NRGBAAt(0, 1); botLeft.A != 0 {
		t.Fatalf("expected transparent at (0,1), got %+v", botLeft)
	}
	botMid := f1.Image.NRGBAAt(1, 1)
	if botMid.R < 200 || botMid.A != 200 {
		t.Fatalf("expected reddish semi-transparent pixel at (1,1), got %+v", botMid)
	}

	// Second cue inherits epoch object & palette, cropped to 2x1 red pixels at (200, 880).
	f2, ok := track.FrameAt(4.5)
	if !ok {
		t.Fatal("expected inherited cropped frame at t=4.5s")
	}
	if f2.X != 200 || f2.Y != 880 || f2.Image.Bounds().Dx() != 2 || f2.Image.Bounds().Dy() != 1 {
		t.Fatalf("unexpected frame 2 bounds: x=%d y=%d bounds=%v", f2.X, f2.Y, f2.Image.Bounds())
	}
	if px := f2.Image.NRGBAAt(0, 0); px.R < 200 || px.A != 200 {
		t.Fatalf("expected cropped red pixel, got %+v", px)
	}

	// Cleared at t=6.0s.
	if _, ok := track.FrameAt(6.0); ok {
		t.Fatal("expected screen cleared at t=6.0s")
	}
}
