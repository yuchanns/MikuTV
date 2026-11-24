package player

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"

	"github.com/yuchanns/MikuTV/internal/provider"
)

type fragment struct {
	provider.PlaybackRange
	duration float64
}

// segmentIndex returns the leaf references as absolute byte ranges. An index
// response can also contain other top-level boxes before or after the sidx.
func segmentIndex(data []byte, start int64) ([]fragment, error) {
	invalid := errors.New("invalid fragmented media index")
	for offset := 0; len(data)-offset >= 8; {
		box := data[offset:]
		size, header := uint64(binary.BigEndian.Uint32(box)), 8
		switch size {
		case 0:
			size = uint64(len(box))
		case 1:
			if len(box) < 16 {
				return nil, invalid
			}
			size, header = binary.BigEndian.Uint64(box[8:]), 16
		}
		if size < uint64(header) || size > uint64(len(box)) {
			return nil, invalid
		}
		end := offset + int(size)
		if string(box[4:8]) != "sidx" {
			offset = end
			continue
		}
		body := box[header:size]
		if len(body) < 24 {
			return nil, invalid
		}
		scale := binary.BigEndian.Uint32(body[8:])
		var first uint64
		var entries []byte
		switch body[0] {
		case 0:
			first, entries = uint64(binary.BigEndian.Uint32(body[16:])), body[22:]
		case 1:
			if len(body) < 32 {
				return nil, invalid
			}
			first, entries = binary.BigEndian.Uint64(body[20:]), body[30:]
		default:
			return nil, invalid
		}
		count := int(binary.BigEndian.Uint16(entries))
		entries = entries[2:]
		if scale == 0 || count == 0 || len(entries) != count*12 ||
			start > math.MaxInt64-int64(end) || first > uint64(math.MaxInt64-start-int64(end)) {
			return nil, invalid
		}
		position := start + int64(end) + int64(first)
		fragments := make([]fragment, 0, count)
		for index := 0; index < count; index++ {
			size := binary.BigEndian.Uint32(entries[index*12:])
			duration := binary.BigEndian.Uint32(entries[index*12+4:])
			if size>>31 != 0 || size == 0 || duration == 0 || int64(size) > math.MaxInt64-position {
				return nil, invalid
			}
			last := position + int64(size) - 1
			fragments = append(fragments, fragment{provider.PlaybackRange{First: position, Last: last}, float64(duration) / float64(scale)})
			position = last + 1
		}
		return fragments, nil
	}
	return nil, invalid
}

// Color information stays in the original init bytes. The HLS manifest also
// signals the transfer function so AVFoundation can pick the display criteria.
func videoRange(init []byte, codec string) string {
	if rangeName := colrVideoRange(init); rangeName != "" {
		return rangeName
	}
	if rangeName := hvccVideoRange(init); rangeName != "" {
		return rangeName
	}
	if bytes.HasPrefix([]byte(codec), []byte("dvh")) ||
		bytes.Contains(init, []byte("dvcC")) || bytes.Contains(init, []byte("dvvC")) {
		return "PQ"
	}
	return "SDR"
}

func transferVideoRange(transfer uint16) string {
	switch transfer {
	case 16:
		return "PQ"
	case 18:
		return "HLG"
	case 0, 2:
		// Unspecified; keep inspecting codec configuration boxes.
		return ""
	default:
		return "SDR"
	}
}

func colrVideoRange(init []byte) string {
	for _, tag := range []struct {
		marker []byte
		minLen int
	}{
		{[]byte("colrnclx"), 15},
		{[]byte("colrnclc"), 14},
	} {
		if i := bytes.Index(init, tag.marker); i >= 0 && len(init)-i >= tag.minLen {
			if rangeName := transferVideoRange(binary.BigEndian.Uint16(init[i+10:])); rangeName != "" {
				return rangeName
			}
		}
	}
	return ""
}

func hvccVideoRange(init []byte) string {
	i := bytes.Index(init, []byte("hvcC"))
	if i < 4 {
		return ""
	}
	boxSize := int(binary.BigEndian.Uint32(init[i-4 : i]))
	if boxSize < 31 || i-4+boxSize > len(init) {
		return ""
	}
	body := init[i+4 : i-4+boxSize]
	if len(body) < 23 || body[0] != 1 {
		return ""
	}
	arrays := int(body[22])
	cursor := 23
	for a := 0; a < arrays && cursor+3 <= len(body); a++ {
		nalType := body[cursor] & 0x3f
		nalCount := int(binary.BigEndian.Uint16(body[cursor+1 : cursor+3]))
		cursor += 3
		for n := 0; n < nalCount && cursor+2 <= len(body); n++ {
			nalLen := int(binary.BigEndian.Uint16(body[cursor : cursor+2]))
			cursor += 2
			if cursor+nalLen > len(body) {
				return ""
			}
			nal := body[cursor : cursor+nalLen]
			cursor += nalLen
			if nalType == 33 && len(nal) > 2 {
				if transfer, ok := hevcSPSTransfer(nal[2:]); ok {
					return transferVideoRange(transfer)
				}
			}
		}
	}
	return ""
}

type bitReader struct {
	data []byte
	pos  int
}

func stripEmulationPrevention(src []byte) []byte {
	out := make([]byte, 0, len(src))
	zeros := 0
	for _, b := range src {
		if zeros == 2 && b == 0x03 {
			zeros = 0
			continue
		}
		if b == 0 {
			zeros++
		} else {
			zeros = 0
		}
		out = append(out, b)
	}
	return out
}

func (r *bitReader) bits(n int) (uint32, bool) {
	if n < 0 || n > 32 || r.pos+n > len(r.data)*8 {
		return 0, false
	}
	var out uint32
	for range n {
		out = (out << 1) | uint32((r.data[r.pos>>3]>>(7-(r.pos&7)))&1)
		r.pos++
	}
	return out, true
}

func (r *bitReader) skip(n int) bool {
	if n < 0 || r.pos+n > len(r.data)*8 {
		return false
	}
	r.pos += n
	return true
}

func (r *bitReader) ue() (uint32, bool) {
	zeros := 0
	for {
		b, ok := r.bits(1)
		if !ok {
			return 0, false
		}
		if b == 1 {
			break
		}
		zeros++
		if zeros > 31 {
			return 0, false
		}
	}
	if zeros == 0 {
		return 0, true
	}
	rest, ok := r.bits(zeros)
	if !ok {
		return 0, false
	}
	return (uint32(1)<<zeros - 1) + rest, true
}

// hevcSPSTransfer parses ITU-T H.265 seq_parameter_set_rbsp up to VUI
// colour_description transfer_characteristics.
func hevcSPSTransfer(spsPayload []byte) (uint16, bool) {
	r := bitReader{data: stripEmulationPrevention(spsPayload)}
	if !r.skip(4) {
		return 0, false
	}
	maxSubLayers, ok := r.bits(3)
	if !ok || !r.skip(1) || !r.skip(96) {
		return 0, false
	}
	var subProf, subLevel [8]bool
	for i := 0; i < int(maxSubLayers); i++ {
		p, ok1 := r.bits(1)
		l, ok2 := r.bits(1)
		if !ok1 || !ok2 {
			return 0, false
		}
		subProf[i], subLevel[i] = p != 0, l != 0
	}
	if maxSubLayers > 0 && !r.skip((8-int(maxSubLayers))*2) {
		return 0, false
	}
	for i := 0; i < int(maxSubLayers); i++ {
		if subProf[i] && !r.skip(88) {
			return 0, false
		}
		if subLevel[i] && !r.skip(8) {
			return 0, false
		}
	}
	if _, ok := r.ue(); !ok {
		return 0, false
	}
	chroma, ok := r.ue()
	if !ok {
		return 0, false
	}
	if chroma == 3 && !r.skip(1) {
		return 0, false
	}
	if _, ok := r.ue(); !ok {
		return 0, false
	}
	if _, ok := r.ue(); !ok {
		return 0, false
	}
	confWindow, ok := r.bits(1)
	if !ok {
		return 0, false
	}
	if confWindow != 0 {
		for range 4 {
			if _, ok := r.ue(); !ok {
				return 0, false
			}
		}
	}
	if _, ok := r.ue(); !ok {
		return 0, false
	}
	if _, ok := r.ue(); !ok {
		return 0, false
	}
	log2MaxPocMinus4, ok := r.ue()
	if !ok || log2MaxPocMinus4 > 12 {
		return 0, false
	}
	subOrdering, ok := r.bits(1)
	if !ok {
		return 0, false
	}
	start := int(maxSubLayers)
	if subOrdering != 0 {
		start = 0
	}
	for i := start; i <= int(maxSubLayers); i++ {
		for range 3 {
			if _, ok := r.ue(); !ok {
				return 0, false
			}
		}
	}
	for range 6 {
		if _, ok := r.ue(); !ok {
			return 0, false
		}
	}
	scalingEnabled, ok := r.bits(1)
	if !ok {
		return 0, false
	}
	if scalingEnabled != 0 {
		scalingPresent, ok := r.bits(1)
		if !ok {
			return 0, false
		}
		if scalingPresent != 0 && !skipHEVCScalingList(&r) {
			return 0, false
		}
	}
	if !r.skip(2) {
		return 0, false
	}
	pcmEnabled, ok := r.bits(1)
	if !ok {
		return 0, false
	}
	if pcmEnabled != 0 {
		if !r.skip(8) {
			return 0, false
		}
		if _, ok := r.ue(); !ok {
			return 0, false
		}
		if _, ok := r.ue(); !ok {
			return 0, false
		}
		if !r.skip(1) {
			return 0, false
		}
	}
	numShortTerm, ok := r.ue()
	if !ok || numShortTerm > 64 {
		return 0, false
	}
	numDeltaPocs := make([]int, numShortTerm)
	for i := 0; i < int(numShortTerm); i++ {
		count, ok := parseHEVCShortTermRPS(&r, i, numDeltaPocs)
		if !ok {
			return 0, false
		}
		numDeltaPocs[i] = count
	}
	longTermPresent, ok := r.bits(1)
	if !ok {
		return 0, false
	}
	if longTermPresent != 0 {
		numLongTerm, ok := r.ue()
		if !ok || numLongTerm > 32 {
			return 0, false
		}
		pocBits := int(log2MaxPocMinus4) + 4
		for i := 0; i < int(numLongTerm); i++ {
			if !r.skip(pocBits + 1) {
				return 0, false
			}
		}
	}
	if !r.skip(2) {
		return 0, false
	}
	vuiPresent, ok := r.bits(1)
	if !ok || vuiPresent == 0 {
		return 0, false
	}
	aspectPresent, ok := r.bits(1)
	if !ok {
		return 0, false
	}
	if aspectPresent != 0 {
		idc, ok := r.bits(8)
		if !ok {
			return 0, false
		}
		if idc == 255 && !r.skip(32) {
			return 0, false
		}
	}
	overscanPresent, ok := r.bits(1)
	if !ok {
		return 0, false
	}
	if overscanPresent != 0 && !r.skip(1) {
		return 0, false
	}
	videoSignalPresent, ok := r.bits(1)
	if !ok || videoSignalPresent == 0 {
		return 0, false
	}
	if !r.skip(4) {
		return 0, false
	}
	colourDescPresent, ok := r.bits(1)
	if !ok || colourDescPresent == 0 {
		return 0, false
	}
	if !r.skip(8) {
		return 0, false
	}
	transfer, ok := r.bits(8)
	if !ok {
		return 0, false
	}
	return uint16(transfer), true
}

func skipHEVCScalingList(r *bitReader) bool {
	for sizeID := 0; sizeID < 4; sizeID++ {
		step := 1
		if sizeID == 3 {
			step = 3
		}
		for matrixID := 0; matrixID < 6; matrixID += step {
			predMode, ok := r.bits(1)
			if !ok {
				return false
			}
			if predMode == 0 {
				if _, ok := r.ue(); !ok {
					return false
				}
				continue
			}
			coefNum := min(64, 1<<(4+(sizeID<<1)))
			if sizeID > 1 {
				if _, ok := r.ue(); !ok {
					return false
				}
			}
			for range coefNum {
				if _, ok := r.ue(); !ok {
					return false
				}
			}
		}
	}
	return true
}

func parseHEVCShortTermRPS(r *bitReader, idx int, prevDelta []int) (int, bool) {
	interPred := uint32(0)
	if idx != 0 {
		var ok bool
		interPred, ok = r.bits(1)
		if !ok {
			return 0, false
		}
	}
	if interPred != 0 {
		if !r.skip(1) {
			return 0, false
		}
		if _, ok := r.ue(); !ok {
			return 0, false
		}
		refCount := prevDelta[idx-1]
		count := 0
		for j := 0; j <= refCount; j++ {
			used, ok := r.bits(1)
			if !ok {
				return 0, false
			}
			if used != 0 {
				count++
			} else {
				useDelta, ok := r.bits(1)
				if !ok {
					return 0, false
				}
				if useDelta != 0 {
					count++
				}
			}
		}
		return count, true
	}
	neg, ok1 := r.ue()
	pos, ok2 := r.ue()
	if !ok1 || !ok2 || neg > 16 || pos > 16 {
		return 0, false
	}
	for range neg {
		if _, ok := r.ue(); !ok || !r.skip(1) {
			return 0, false
		}
	}
	for range pos {
		if _, ok := r.ue(); !ok || !r.skip(1) {
			return 0, false
		}
	}
	return int(neg + pos), true
}
