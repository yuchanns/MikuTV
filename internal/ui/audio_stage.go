package tvui

import (
	"image"
	"image/color"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/provider"
	"github.com/yuchanns/MikuTV/internal/theme"
)

const (
	audioAmbientCacheLimit = 16
	audioOLEDDriftPeriod   = 180.0
	audioOLEDDriftMaxX     = 14.0
	audioOLEDDriftMaxY     = 10.0
	audioPauseDimOpacity   = float32(0.50)
)

type audioAmbientKey struct {
	art    *app.CatalogArtwork
	dark   bool
	canvas theme.Color
}

type audioAmbientEntry struct {
	bitmap   *ui.Bitmap
	lastUsed uint64
}

var (
	audioAmbientMu    sync.Mutex
	audioAmbientCache = make(map[audioAmbientKey]audioAmbientEntry)
	audioAmbientClock uint64
)

func cachedAudioAmbientBackdrop(art *app.CatalogArtwork, dark bool, canvas theme.Color) *ui.Bitmap {
	if art == nil || art.Image == nil {
		return nil
	}
	key := audioAmbientKey{art: art, dark: dark, canvas: canvas}
	audioAmbientMu.Lock()
	audioAmbientClock++
	if hit, ok := audioAmbientCache[key]; ok {
		hit.lastUsed = audioAmbientClock
		audioAmbientCache[key] = hit
		audioAmbientMu.Unlock()
		return hit.bitmap
	}
	audioAmbientMu.Unlock()

	bmp := buildAudioAmbientBackdrop(art.Image, dark, canvas)

	audioAmbientMu.Lock()
	audioAmbientClock++
	audioAmbientCache[key] = audioAmbientEntry{bitmap: bmp, lastUsed: audioAmbientClock}
	if len(audioAmbientCache) > audioAmbientCacheLimit {
		var oldestKey audioAmbientKey
		var oldestUse uint64
		first := true
		for k, entry := range audioAmbientCache {
			if first || entry.lastUsed < oldestUse {
				oldestKey, oldestUse, first = k, entry.lastUsed, false
			}
		}
		delete(audioAmbientCache, oldestKey)
	}
	audioAmbientMu.Unlock()
	return bmp
}

// buildAudioAmbientBackdrop generates a silky blurred ambient wash from album art,
// matched to either MikuTV Dark or MikuTV Light canvases with zero per-frame GPU cost.
func buildAudioAmbientBackdrop(src image.Image, dark bool, canvas theme.Color) *ui.Bitmap {
	const sw, sh = 96, 54
	b := src.Bounds()
	if b.Dx() <= 0 || b.Dy() <= 0 {
		return nil
	}
	buf := make([][3]float64, sw*sh)
	for y := 0; y < sh; y++ {
		y0 := b.Min.Y + y*b.Dy()/sh
		y1 := b.Min.Y + (y+1)*b.Dy()/sh
		if y1 <= y0 {
			y1 = y0 + 1
		}
		for x := 0; x < sw; x++ {
			x0 := b.Min.X + x*b.Dx()/sw
			x1 := b.Min.X + (x+1)*b.Dx()/sw
			if x1 <= x0 {
				x1 = x0 + 1
			}
			var sr, sg, sb, cnt float64
			for py := y0; py < y1; py++ {
				for px := x0; px < x1; px++ {
					r, g, bl, _ := src.At(px, py).RGBA()
					sr += float64(r) / 65535.0
					sg += float64(g) / 65535.0
					sb += float64(bl) / 65535.0
					cnt++
				}
			}
			buf[y*sw+x] = [3]float64{sr / cnt, sg / cnt, sb / cnt}
		}
	}
	tmp := make([][3]float64, sw*sh)
	const rad = 8
	for pass := 0; pass < 4; pass++ {
		for y := 0; y < sh; y++ {
			for x := 0; x < sw; x++ {
				var r, g, bl, n float64
				for dx := -rad; dx <= rad; dx++ {
					p := buf[y*sw+min(max(x+dx, 0), sw-1)]
					r += p[0]
					g += p[1]
					bl += p[2]
					n++
				}
				tmp[y*sw+x] = [3]float64{r / n, g / n, bl / n}
			}
		}
		for y := 0; y < sh; y++ {
			for x := 0; x < sw; x++ {
				var r, g, bl, n float64
				for dy := -rad; dy <= rad; dy++ {
					p := tmp[min(max(y+dy, 0), sh-1)*sw+x]
					r += p[0]
					g += p[1]
					bl += p[2]
					n++
				}
				buf[y*sw+x] = [3]float64{r / n, g / n, bl / n}
			}
		}
	}

	cn := canvas.NRGBA()
	cr, cg, cb := float64(cn.R)/255.0, float64(cn.G)/255.0, float64(cn.B)/255.0
	sat := 1.48
	maxWeight := 0.46
	if !dark {
		sat = 1.55
		maxWeight = 0.24
	}

	const outW, outH = 480, 270
	out := image.NewNRGBA(image.Rect(0, 0, outW, outH))
	for y := 0; y < outH; y++ {
		fy := float64(y) / float64(outH-1) * float64(sh-1)
		y0 := int(math.Floor(fy))
		y1 := min(y0+1, sh-1)
		ty := fy - float64(y0)
		ny := float64(y)/float64(outH) - 0.48
		for x := 0; x < outW; x++ {
			fx := float64(x) / float64(outW-1) * float64(sw-1)
			x0 := int(math.Floor(fx))
			x1 := min(x0+1, sw-1)
			tx := fx - float64(x0)

			p00, p10 := buf[y0*sw+x0], buf[y0*sw+x1]
			p01, p11 := buf[y1*sw+x0], buf[y1*sw+x1]
			var col [3]float64
			for i := 0; i < 3; i++ {
				top := p00[i]*(1-tx) + p10[i]*tx
				bot := p01[i]*(1-tx) + p11[i]*tx
				col[i] = top*(1-ty) + bot*ty
			}
			luma := 0.2126*col[0] + 0.7152*col[1] + 0.0722*col[2]
			for i := 0; i < 3; i++ {
				col[i] = min(max(luma+(col[i]-luma)*sat, 0), 1)
			}
			nx := float64(x)/float64(outW) - 0.28
			dist := math.Sqrt(nx*nx*1.25 + ny*ny*1.55)
			vignette := min(max(1.0-dist*0.72, 0.18), 1.0)
			w := maxWeight * vignette
			out.SetNRGBA(x, y, color.NRGBA{
				R: uint8(min(max(math.Round((cr*(1-w)+col[0]*w)*255), 0), 255)),
				G: uint8(min(max(math.Round((cg*(1-w)+col[1]*w)*255), 0), 255)),
				B: uint8(min(max(math.Round((cb*(1-w)+col[2]*w)*255), 0), 255)),
				A: 255,
			})
		}
	}
	return ui.NewBitmap(out)
}

func activeAudioLyricIndex(cues []provider.SubtitleCue, position float64) int {
	if len(cues) == 0 {
		return -1
	}
	// First cue with Start > position; active line is index-1.
	idx := sort.Search(len(cues), func(i int) bool {
		return cues[i].Start > position
	}) - 1
	if idx < 0 {
		return 0
	}
	return idx
}

func splitAudioLyricText(raw string) (primary, secondary string) {
	trimmed := strings.TrimSpace(raw)
	if before, after, ok := strings.Cut(trimmed, "\n"); ok {
		return strings.TrimSpace(before), strings.TrimSpace(after)
	}
	return trimmed, ""
}

func audioStageSubtitle(item playbackItem) string {
	var parts []string
	if item.item.Creator != nil && strings.TrimSpace(item.item.Creator.Name) != "" {
		parts = append(parts, strings.TrimSpace(item.item.Creator.Name))
	}
	if cat := strings.TrimSpace(item.item.Category); cat != "" {
		parts = append(parts, cat)
	}
	return strings.Join(parts, " · ")
}

func (p *PageContext) renderAudioStage(c *ui.Context, state app.PlaybackState, item playbackItem, now time.Time) {
	v, s := p.host, &p.host.player
	colors := p.Theme.Colors
	var cover *ui.Bitmap
	var art *app.CatalogArtwork
	if item.item.BackdropURL != "" {
		cover = p.catalogBitmap(catalogImageKey{location: item.item.BackdropURL, backdrop: true})
		art = p.catalogArtwork(catalogImageKey{location: item.item.BackdropURL, backdrop: true})
	}
	if cover == nil {
		cover = p.catalogCover(item.item.CoverURL)
	}
	if art == nil {
		art = p.catalogArtwork(catalogImageKey{location: item.item.CoverURL})
	}
	ambient := cachedAudioAmbientBackdrop(art, p.Theme.Dark, colors.Surface.Canvas)

	var driftX, driftY float32
	if state.Playing && !v.reduceMotionEnabled(c) {
		sec := float64(now.UnixNano()) / 1e9
		phase := (sec / audioOLEDDriftPeriod) * 2 * math.Pi
		driftX = float32(math.Sin(phase) * audioOLEDDriftMaxX)
		driftY = float32(math.Sin(phase*1.5+0.7) * audioOLEDDriftMaxY)
	}

	// Keep the outermost stage container 100% opaque Surface.Canvas so OLED pause
	// dimming on the inner canvas never leaks underlying transparent window pixels.
	ui.Box(c.Key("audio-stage")).Absolute().Top(0).Left(0).Fill().ZIndex(0).
		Background(paint(colors.Surface.Canvas)).Clip().IgnoreInput(true).Children(func() {
		inner := ui.Box(c.Key("audio-stage-canvas")).Absolute().Top(0).Left(0).Fill()
		if state.AudioDimmed {
			inner.Opacity(audioPauseDimOpacity)
		}
		inner.Children(func() {
			if ambient != nil {
				ui.Image(c.Key("audio-ambient"), ambient).Absolute().Top(0).Left(0).Fill().Fit(ui.Cover)
			}
			cues := s.content.cues
			title := item.item.Title
			sub := audioStageSubtitle(item)
			compact := p.Width < 1500

			if len(cues) == 0 && s.content.subtitleQuery == nil {
				p.renderAudioStageArtworkOnly(c, cover, title, sub, compact, driftX, driftY)
				return
			}
			p.renderAudioStageLyricsSplit(c, cover, title, sub, cues, state.Position, compact, driftX, driftY)
		})
	})
}

func (p *PageContext) renderAudioCoverSquare(c *ui.Context, key string, cover *ui.Bitmap, size, radius float32) {
	colors := p.Theme.Colors
	shadow := withAlpha(colors.Overlay.Shadow, 0x99)
	border := withAlpha(colors.Text.Primary, 0x1e)
	if !p.Theme.Dark {
		shadow = withAlpha(colors.Overlay.Shadow, 0x38)
		border = withAlpha(colors.Text.Primary, 0x14)
	}
	box := ui.Box(c.Key(key)).Size(size, size).Radius(radius).Clip().
		Background(paint(colors.Surface.Panel)).Border(1, border).Shadow(0, 28, 68, 0, shadow)
	box.Children(func() {
		if cover != nil {
			ui.Image(c, cover).Fill().Fit(ui.Cover)
		} else {
			ui.Box(c).Fill().Center().Children(func() {
				ui.Icon(c, p.Assets.Icon("stage-audio")).Size(size*0.24, size*0.24).TextColor(paint(colors.Text.Muted))
			})
		}
	})
}

func (p *PageContext) renderAudioStageArtworkOnly(c *ui.Context, cover *ui.Bitmap, title, sub string, compact bool, driftX, driftY float32) {
	colors := p.Theme.Colors
	coverSize := float32(490)
	if compact {
		coverSize = 420
	}
	ui.Column(c.Key("audio-stage-centered")).Absolute().Top(driftY).Left(driftX).Right(-driftX).Bottom(-driftY).
		Center().Padding(0, 80, 0, 80).Children(func() {
		p.renderAudioCoverSquare(c, "audio-cover-hero", cover, coverSize, 22)
		ui.Column(c).Margin(36, 0, 0, 0).Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Text(c, title).FontSize(40).FontWeight(650).MaxLines(1).TextColor(paint(colors.Text.Primary))
			if sub != "" {
				ui.Text(c, sub).FontSize(22).MaxLines(1).TextColor(paint(colors.Text.Secondary))
			}
		})
	})
}

func (p *PageContext) renderAudioStageLyricsSplit(c *ui.Context, cover *ui.Bitmap, title, sub string, cues []provider.SubtitleCue, position float64, compact bool, driftX, driftY float32) {
	colors := p.Theme.Colors
	coverSize := float32(490)
	padLeft, padRight, gap := float32(140), float32(124), float32(108)
	if compact {
		coverSize = min(coverSize, 360)
		padLeft, padRight, gap = 72, 64, 56
	}

	activeIdx := activeAudioLyricIndex(cues, position)
	const windowHalf = 2
	startIdx := max(0, activeIdx-windowHalf)
	endIdx := min(len(cues), activeIdx+windowHalf+1)

	ui.Row(c.Key("audio-stage-split")).Absolute().Top(driftY).Left(driftX).Right(-driftX).Bottom(-driftY).
		Padding(0, padRight, 0, padLeft).Gap(gap).AlignItems(ui.Center).Children(func() {
		ui.Column(c.Key("audio-stage-left")).Width(coverSize).Shrink(0).Children(func() {
			p.renderAudioCoverSquare(c, "audio-cover-square", cover, coverSize, 22)
			ui.Column(c).Margin(30, 0, 0, 0).Gap(10).Children(func() {
				ui.Text(c, title).FontSize(34).FontWeight(650).MaxLines(1).TextColor(paint(colors.Text.Primary))
				if sub != "" {
					ui.Text(c, sub).FontSize(21).MaxLines(1).TextColor(paint(colors.Text.Secondary))
				}
			})
		})
		ui.Column(c.Key("audio-stage-lyrics")).Grow(1).MinWidth(0).Gap(36).Children(func() {
			for i := startIdx; i < endIdx; i++ {
				mainLine, transLine := splitAudioLyricText(cues[i].Text)
				dist := i - activeIdx
				if dist < 0 {
					dist = -dist
				}
				mainSize, subSize := float32(34), float32(21)
				if compact {
					mainSize, subSize = 28, 19
				}
				mainWeight := 550
				var mainCol, subCol ui.Color
				switch dist {
				case 0:
					mainSize, subSize, mainWeight = 46, 25, 700
					if compact {
						mainSize, subSize = 36, 22
					}
					mainCol = paint(colors.Text.Primary)
					subCol = withAlpha(colors.Text.Primary, 0xc8)
				case 1:
					mainCol = withAlpha(colors.Text.Primary, 0x5c)
					subCol = withAlpha(colors.Text.Primary, 0x3d)
				default:
					mainCol = withAlpha(colors.Text.Primary, 0x33)
					subCol = withAlpha(colors.Text.Primary, 0x22)
				}
				ui.Column(c.Key(i)).Gap(8).Children(func() {
					ui.Text(c, mainLine).FontSize(mainSize).FontWeight(mainWeight).MaxLines(2).TextColor(mainCol)
					if transLine != "" {
						ui.Text(c, transLine).FontSize(subSize).MaxLines(2).TextColor(subCol)
					}
				})
			}
		})
	})
}
