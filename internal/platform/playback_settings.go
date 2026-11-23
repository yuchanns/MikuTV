package platform

import (
	"encoding/json"

	"github.com/yuchanns/MikuTV/internal/app"
)

const playbackPrefsDefaultsKey = "playbackPrefs.v1"

func parsePlaybackPreferences(raw string) (app.PlaybackPreferences, bool) {
	if raw == "" {
		return app.DefaultPlaybackPreferences(), false
	}
	prefs := app.DefaultPlaybackPreferences()
	if err := json.Unmarshal([]byte(raw), &prefs); err != nil {
		return app.DefaultPlaybackPreferences(), false
	}
	return prefs.Normalize(), true
}

func encodePlaybackPreferences(prefs app.PlaybackPreferences) (string, error) {
	data, err := json.Marshal(prefs.Normalize())
	if err != nil {
		return "", err
	}
	return string(data), nil
}
