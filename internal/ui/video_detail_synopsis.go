package tvui

import (
	"strings"
	"unicode/utf16"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
)

type videoDetailSynopsis struct {
	full, first, second string
	truncated           bool
}

func videoSynopsisWidth(pageWidth float32) float32 {
	return (pageWidth - 2*videoDetailInset - 80) * 2 / 3
}

func (d *videoDetailState) cachedSynopsis(body string, width float32) videoDetailSynopsis {
	if d.synopsisReady && d.synopsisBody == body && d.synopsisWidth == width {
		return d.synopsis
	}
	d.synopsis = prepareVideoSynopsis(body, width)
	d.synopsisBody, d.synopsisWidth, d.synopsisReady = body, width, true
	return d.synopsis
}

func prepareVideoSynopsis(body string, width float32) videoDetailSynopsis {
	synopsis := videoDetailSynopsis{full: body}
	if body == "" {
		return synopsis
	}
	font := ui.Font{Family: videoDetailFont, Size: 24, Weight: 400, Lang: "zh-CN", LineHeight: 1.8}
	lines := ui.ShapeText(body, font, width).Lines()
	if len(lines) <= 2 {
		return synopsis
	}
	text := utf16.Encode([]rune(body))
	lineText := func(line ui.TextLine) string {
		return strings.TrimSpace(string(utf16.Decode(text[line.Start:line.End])))
	}
	synopsis.first = lineText(lines[0])
	synopsis.second = lineText(lines[1]) + "\u2026"
	synopsis.truncated = true
	return synopsis
}

func (p *PageContext) videoSynopsis(c *ui.Context, synopsis videoDetailSynopsis, width float32, expanded bool) {
	const (
		lineHeight    = 43.2
		previewHeight = 88
		linkWidth     = 58
		linkGap       = 12
	)
	if synopsis.full == "" {
		synopsis.full = p.Assets.Text.Detail.Empty
	}
	if !synopsis.truncated {
		p.videoDetailText(c, synopsis.full).MinHeight(previewHeight).Translate(0, 3)
		if expanded {
			p.videoSynopsisLink(c, p.Assets.Text.Detail.Collapse, false).AlignSelf(ui.Start)
		}
		return
	}
	ui.Column(c).FillWidth().Height(previewHeight).Children(func() {
		p.videoDetailText(c, synopsis.first).Height(lineHeight).MaxLines(1).Translate(0, 3)
		ui.Row(c).FillWidth().Height(lineHeight).AlignItems(ui.Center).Gap(linkGap).Children(func() {
			p.videoDetailText(c, synopsis.second).Width(width-linkWidth-linkGap).Height(lineHeight).
				MaxLines(1).Translate(0, 3)
			p.videoSynopsisLink(c, p.Assets.Text.Detail.More, true).Width(linkWidth)
		})
	})
}

func (p *PageContext) videoSynopsisLink(c *ui.Context, label string, expand bool) ui.Element {
	colors := p.Theme.Colors
	background, foreground := ui.Transparent, paint(colors.Text.Primary)
	if p.State.Focus == videoDetailMore {
		background, foreground = paint(colors.Focus.Background), paint(colors.Focus.Foreground)
	}
	button := ui.Button(c.Key(string(videoDetailMore)), "").Label(label).Height(43.2).Padding(0, 0).
		Radius(8).Border(0, ui.Transparent).FontSize(24).FontWeight(550).Center().OnClick(func() {
		p.dispatch(func() { p.host.setVideoSynopsisExpanded(expand) })
	})
	button.Children(func() {
		ui.Text(c, label).FontSize(24).FontWeight(550).Translate(0, 2)
	})
	p.animateFocus(button, background, foreground)
	p.addVideoFocusShadow(button, p.State.Focus == videoDetailMore)
	p.bind(button, videoDetailMore, videoDetailControls)
	if p.active && p.host.reveal && p.State.Focus == videoDetailMore {
		button.ScrollIntoView(ui.ScrollIntoViewOptions{Block: ui.ScrollCenter})
	}
	return button
}

func (v *View) setVideoSynopsisExpanded(expanded bool) {
	v.videoDetail.expanded = expanded
	if !expanded {
		v.moveFocus(videoDetailMore, app.Up, true)
		return
	}
	v.moveFocus(videoDetailOverview.focusID(), app.Down, true)
	if scroll := v.activeScroll; scroll != nil {
		scroll.ScrollTo(scroll.X, 275+v.videoDetailSectionOffset(scroll), focusScroll)
	}
}
