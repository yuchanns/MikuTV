package tvui

import (
	"time"

	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"
)

const (
	videoToastHold           = 2600 * time.Millisecond
	videoToastSlide          = 350 * time.Millisecond
	videoToastFade           = 250 * time.Millisecond
	videoToastDuration       = videoToastHold + videoToastSlide
	videoCelebrationDuration = 2 * time.Second
)

type videoDetailFeedback struct {
	text        string
	start       time.Time
	celebration bool
	icons       []string
}

func (v *View) renderVideoDetailFeedback(c *ui.Context) {
	f := &v.videoDetail.feedback
	if f.text == "" {
		return
	}
	now := c.Now()
	if f.start.IsZero() {
		f.start = now
	}
	duration := videoToastDuration
	if f.celebration {
		duration = videoCelebrationDuration
	} else if c.Preferences().ReduceMotion {
		duration = videoToastHold
	}
	if remaining := f.start.Add(duration).Sub(now); remaining > 0 {
		c.After(remaining)
	} else {
		*f = videoDetailFeedback{}
		return
	}
	if f.celebration {
		v.videoCelebration(c, *f)
		return
	}
	d := deriveColors(v.Theme)
	width, _ := c.Size()
	ui.Column(c.Key("video-toast")).Absolute().Left(0).Right(0).Bottom(60).AlignItems(ui.Center).
		ZIndex(200).PassThrough().Children(func() {
		toast := ui.Box(c.Key(f.start)).Radius(18).Material(glass.Blur{Radius: 25}).
			Shadow(0, 12, 40, 0, withAlpha(v.Theme.Colors.Overlay.Shadow, 0x44)).PassThrough()
		toast.Children(func() {
			ui.Text(c, f.text).FontSize(22).TextColor(d.toastText).TextAlign(ui.Center).
				MaxWidth(width-220).Padding(20, 32).Radius(18).Border(1, d.toastBorder).
				Background(d.toastFill).Role(ui.RoleStatus).PassThrough()
		})
		if !c.Preferences().ReduceMotion {
			toast.Opacity(toast.Keyframes("fade", []ui.Keyframe{
				{Value: 0}, {At: videoToastFade, Value: 1},
				{At: videoToastHold, Value: 1}, {At: videoToastHold + videoToastFade, Value: 0},
			}))
			toast.Translate(0, toast.Keyframes("position", []ui.Keyframe{
				{Value: 30}, {At: videoToastSlide, Value: 0, Ease: ui.EaseOut},
				{At: videoToastHold, Value: 0}, {At: videoToastDuration, Value: 30, Ease: ui.EaseOut},
			}))
		}
	})
}

func (v *View) videoCelebration(c *ui.Context, f videoDetailFeedback) {
	colors := v.Theme.Colors.Accent
	ink := paint(colors.Soft)
	layer := ui.Box(c.Key(f.start)).Absolute().Left(0).Top(0).Fill().Center().PassThrough().ZIndex(100).
		RadialGradient(ui.RadialGradient{Stops: []ui.GradientStop{
			{Color: withAlpha(colors.Primary, 0x13)}, {Color: ui.Transparent, At: 0.60},
		}})
	if !c.Preferences().ReduceMotion {
		layer.Opacity(layer.Keyframes("fade", []ui.Keyframe{
			{Value: 0}, {At: 450 * time.Millisecond, Value: 1},
			{At: 1260 * time.Millisecond, Value: 1}, {At: 1800 * time.Millisecond, Value: 0},
		}))
		scale := layer.Keyframes("scale", []ui.Keyframe{
			{Value: 0.6}, {At: 450 * time.Millisecond, Value: 1.08},
			{At: 1260 * time.Millisecond, Value: 1}, {At: 1800 * time.Millisecond, Value: 1.12},
		})
		layer.Scale(scale, scale)
	}
	icons := f.icons
	if len(icons) == 0 {
		icons = []string{"check"}
	}
	layer.Children(func() {
		ui.Column(c).Gap(26).AlignItems(ui.Center).PassThrough().Children(func() {
			ui.Row(c).Gap(35).PassThrough().Children(func() {
				for _, icon := range icons {
					ui.Box(c).Size(90, 90).PassThrough().Children(func() {
						ui.Icon(c, v.Assets.Icon(icon)).Absolute().Top(0).Left(0).Fill().Cache().Blur(15).
							TextColor(withAlpha(colors.Primary, 0x80)).PassThrough()
						ui.Icon(c, v.Assets.Icon(icon)).Fill().TextColor(ink).PassThrough()
					})
				}
			})
			ui.Text(c, f.text).FontSize(26).LetterSpacing(4).TextColor(ink).Role(ui.RoleStatus).PassThrough()
		})
	})
}
