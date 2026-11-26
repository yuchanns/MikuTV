package tvui

import (
	"fmt"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/provider"
)

type playbackSettingKind string

const (
	playbackSettingQuality   playbackSettingKind = "quality"
	playbackSettingSpeed     playbackSettingKind = "speed"
	playbackSettingSubtitles playbackSettingKind = "subtitles"
)

const (
	playbackPrefQuality   app.FocusID = "settings/playback/quality"
	playbackPrefSpeed     app.FocusID = "settings/playback/speed"
	playbackPrefSubtitles       app.FocusID = "settings/playback/subtitles"
	playbackPrefAutoplay        app.FocusID = "settings/playback/autoplay"
	playbackPrefAudioScreensaver app.FocusID = "settings/playback/audio-screensaver"
)

func playbackPickerOptionID(index int) app.FocusID {
	return app.FocusID(fmt.Sprintf("settings/playback/choice/%d", index))
}

type playbackSettingsUI struct {
	pickerKind playbackSettingKind
}

func (v *View) playbackPreferences() app.PlaybackPreferences {
	if v.PlaybackSettings == nil {
		return app.DefaultPlaybackPreferences()
	}
	return v.PlaybackSettings.Preferences()
}

type playbackChoice struct {
	label    string
	selected bool
	apply    func(*app.PlaybackPreferences)
}

func (v *View) playbackChoices(kind playbackSettingKind) (string, []playbackChoice) {
	l := v.Assets.Text.PlaybackPrefs
	cur := v.playbackPreferences()
	switch kind {
	case playbackSettingQuality:
		return l.Quality, []playbackChoice{
			{label: l.QualityAuto, selected: cur.Quality == app.PlaybackQualityAuto, apply: func(p *app.PlaybackPreferences) { p.Quality = app.PlaybackQualityAuto }},
			{label: "4K HDR", selected: cur.Quality == app.PlaybackQuality4KHDR, apply: func(p *app.PlaybackPreferences) { p.Quality = app.PlaybackQuality4KHDR }},
			{label: "4K", selected: cur.Quality == app.PlaybackQuality4K, apply: func(p *app.PlaybackPreferences) { p.Quality = app.PlaybackQuality4K }},
			{label: "1080P", selected: cur.Quality == app.PlaybackQuality1080P, apply: func(p *app.PlaybackPreferences) { p.Quality = app.PlaybackQuality1080P }},
			{label: "720P", selected: cur.Quality == app.PlaybackQuality720P, apply: func(p *app.PlaybackPreferences) { p.Quality = app.PlaybackQuality720P }},
		}
	case playbackSettingSpeed:
		return l.Speed, []playbackChoice{
			{label: "0.75x", selected: cur.Speed == 0.75, apply: func(p *app.PlaybackPreferences) { p.Speed = 0.75 }},
			{label: "1.0x", selected: cur.Speed == 1.0, apply: func(p *app.PlaybackPreferences) { p.Speed = 1.0 }},
			{label: "1.25x", selected: cur.Speed == 1.25, apply: func(p *app.PlaybackPreferences) { p.Speed = 1.25 }},
			{label: "1.5x", selected: cur.Speed == 1.5, apply: func(p *app.PlaybackPreferences) { p.Speed = 1.5 }},
			{label: "2.0x", selected: cur.Speed == 2.0, apply: func(p *app.PlaybackPreferences) { p.Speed = 2.0 }},
		}
	case playbackSettingSubtitles:
		return l.Subtitles, []playbackChoice{
			{label: l.QualityAuto, selected: cur.Subtitle == app.PlaybackSubtitleAuto, apply: func(p *app.PlaybackPreferences) { p.Subtitle = app.PlaybackSubtitleAuto }},
			{label: l.SubtitlesChinese, selected: cur.Subtitle == app.PlaybackSubtitleChinese, apply: func(p *app.PlaybackPreferences) { p.Subtitle = app.PlaybackSubtitleChinese }},
			{label: l.SubtitlesEnglish, selected: cur.Subtitle == app.PlaybackSubtitleEnglish, apply: func(p *app.PlaybackPreferences) { p.Subtitle = app.PlaybackSubtitleEnglish }},
			{label: l.SubtitlesOff, selected: cur.Subtitle == app.PlaybackSubtitleOff, apply: func(p *app.PlaybackPreferences) { p.Subtitle = app.PlaybackSubtitleOff }},
		}
	default:
		return "", nil
	}
}

func (v *View) playbackPickerInitialFocus() app.FocusID {
	_, choices := v.playbackChoices(v.playbackUI.pickerKind)
	for i, ch := range choices {
		if ch.selected {
			return playbackPickerOptionID(i)
		}
	}
	return playbackPickerOptionID(0)
}

func (p *PageContext) playbackQualityPrefLabel(pref app.PlaybackQualityPref) string {
	switch pref {
	case app.PlaybackQuality4KHDR:
		return "4K HDR"
	case app.PlaybackQuality4K:
		return "4K"
	case app.PlaybackQuality1080P:
		return "1080P"
	case app.PlaybackQuality720P:
		return "720P"
	default:
		return p.Assets.Text.PlaybackPrefs.QualityAuto
	}
}

func (p *PageContext) playbackSubtitlePrefLabel(pref app.PlaybackSubtitlePref) string {
	l := p.Assets.Text.PlaybackPrefs
	switch pref {
	case app.PlaybackSubtitleOff:
		return l.SubtitlesOff
	case app.PlaybackSubtitleChinese:
		return l.SubtitlesChinese
	case app.PlaybackSubtitleEnglish:
		return l.SubtitlesEnglish
	default:
		return l.QualityAuto
	}
}

func (v *View) showPlaybackSubtitlesSetting() bool {
	if v.State == nil {
		return true
	}
	entry, ok := v.State.Current()
	if !ok || entry.ID == "" {
		return true
	}
	return entry.Capabilities.Has(provider.ReadSubtitles)
}

type playbackSettingsPage struct{}

func (playbackSettingsPage) Render(c *ui.Context, p *PageContext) {
	v := p.host
	l := p.Assets.Text.PlaybackPrefs
	prefs := v.playbackPreferences()
	showSubtitles := v.showPlaybackSubtitlesSetting()
	if p.active && !showSubtitles && p.State.Focus == playbackPrefSubtitles {
		p.State.Focus, v.reveal = playbackPrefQuality, true
	}
	p.region("content", nil)

	p.settingsSectionHeader(c, l.Title, l.Intro)

	p.settingsValueRow(c, playbackPrefQuality, "play", l.Quality, l.QualityNote, p.playbackQualityPrefLabel(prefs.Quality), func() {
		v.playbackUI.pickerKind = playbackSettingQuality
		p.State.Push(app.Route{Page: app.PlaybackSettingPicker, Section: "playback"})
		p.State.Focus, v.reveal = v.playbackPickerInitialFocus(), true
	})
	p.settingsValueRow(c, playbackPrefSpeed, "play", l.Speed, "", playbackRateLabel(prefs.Speed), func() {
		v.playbackUI.pickerKind = playbackSettingSpeed
		p.State.Push(app.Route{Page: app.PlaybackSettingPicker, Section: "playback"})
		p.State.Focus, v.reveal = v.playbackPickerInitialFocus(), true
	})
	if showSubtitles {
		p.settingsValueRow(c, playbackPrefSubtitles, "captions", l.Subtitles, "", p.playbackSubtitlePrefLabel(prefs.Subtitle), func() {
			v.playbackUI.pickerKind = playbackSettingSubtitles
			p.State.Push(app.Route{Page: app.PlaybackSettingPicker, Section: "playback"})
			p.State.Focus, v.reveal = v.playbackPickerInitialFocus(), true
		})
	}
	p.settingsToggleRow(c, playbackPrefAutoplay, "skip-forward", l.Autoplay, l.AutoplayNote, prefs.Autoplay, func() {
		if v.PlaybackSettings != nil {
			if err := v.PlaybackSettings.Update(func(cfg *app.PlaybackPreferences) { cfg.Autoplay = !cfg.Autoplay }); err != nil {
				v.showSettingsFeedback(p.Assets.Text.SaveFailed)
			}
		}
	})
	p.settingsToggleRow(c, playbackPrefAudioScreensaver, "moon", l.AudioScreensaver, l.AudioScreensaverNote, prefs.AllowAudioScreensaver, func() {
		if v.PlaybackSettings != nil {
			if err := v.PlaybackSettings.Update(func(cfg *app.PlaybackPreferences) { cfg.AllowAudioScreensaver = !cfg.AllowAudioScreensaver }); err != nil {
				v.showSettingsFeedback(p.Assets.Text.SaveFailed)
			}
		}
	})
}

type playbackSettingPickerPage struct{}

func (playbackSettingPickerPage) Render(c *ui.Context, p *PageContext) {
	v := p.host
	title, rawChoices := v.playbackChoices(v.playbackUI.pickerKind)
	choices := make([]settingsPickerChoice, len(rawChoices))
	for i, choice := range rawChoices {
		apply := choice.apply
		choices[i] = settingsPickerChoice{
			id:       playbackPickerOptionID(i),
			label:    choice.label,
			selected: choice.selected,
			onSelect: func() {
				if v.PlaybackSettings != nil {
					if err := v.PlaybackSettings.Update(apply); err != nil {
						v.showSettingsFeedback(p.Assets.Text.SaveFailed)
					}
				}
				p.State.Back()
			},
		}
	}
	p.renderSettingsPickerDrawer(c, "PLAYBACK \u00b7 PREFERENCES", title, choices)
}
