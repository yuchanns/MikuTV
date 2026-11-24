package platform

import (
	"testing"

	"github.com/yuchanns/MikuTV/internal/app"
)

func TestPlaybackPreferencesPersistenceCodec(t *testing.T) {
	if got, ok := parsePlaybackPreferences(""); ok || got != app.DefaultPlaybackPreferences() {
		t.Fatalf("empty input = %+v ok=%v, want default false", got, ok)
	}
	if got, ok := parsePlaybackPreferences("{not-json"); ok || got != app.DefaultPlaybackPreferences() {
		t.Fatalf("corrupt input = %+v ok=%v, want default false", got, ok)
	}
	want := app.PlaybackPreferences{
		Quality:  app.PlaybackQuality1080P,
		Speed:    1.25,
		Subtitle: app.PlaybackSubtitleChinese,
		Autoplay: false,
	}
	encoded, err := encodePlaybackPreferences(want)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := parsePlaybackPreferences(encoded)
	if !ok || got != want {
		t.Fatalf("roundtrip = %+v ok=%v, want %+v", got, ok, want)
	}
	// Invalid values normalize cleanly.
	norm, ok := parsePlaybackPreferences(`{"quality":"bogus","speed":1.3,"subtitle":"jp","autoplay":true}`)
	if !ok || norm.Quality != app.PlaybackQualityAuto || norm.Speed != 1.25 || norm.Subtitle != app.PlaybackSubtitleOff || !norm.Autoplay {
		t.Fatalf("normalized = %+v ok=%v", norm, ok)
	}
}
