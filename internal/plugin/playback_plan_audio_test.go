package plugin

import (
	"net/http"
	"testing"

	"github.com/yuchanns/MikuTV/internal/provider"
)

func TestOwnPlaybackPlanAudioTracksAndPGSSubtitleMetadata(t *testing.T) {
	sess := &Session{entryID: "jellyfin", source: &Source{}}
	allowAll := func(*http.Request) error { return nil }
	req := provider.PlaybackRequest{
		PlaybackTarget: provider.PlaybackTarget{
			Ref: provider.MediaRef{EntryID: "jellyfin", Kind: provider.Video, ID: "m1"},
		},
		AudioTrack: "2",
	}

	valid := provider.PlaybackPlan{
		ItemID:     "m1",
		Access:     provider.VideoAccessGranted,
		AudioTrack: "2",
		AudioTracks: []provider.PlaybackTrack{
			{ID: "1", Label: "English 7.1", Language: "eng", Default: true, Codec: "truehd"},
			{ID: "2", Label: "Chinese 7.1", Language: "zho", Codec: "truehd"},
			{ID: "2", Label: "Duplicate", Language: "zho"},
		},
		Streams: []provider.PlaybackStream{{
			Format: provider.PlaybackHLS,
			Video: provider.PlaybackTrack{
				Resource:    provider.PlaybackResource{URLs: []string{"https://example.test/master.m3u8"}},
				ContentType: "application/vnd.apple.mpegurl",
			},
		}},
		Subtitles: []provider.PlaybackTrack{
			{
				ID:          "sub-5",
				Label:       "Chinese PGS",
				Language:    "chi",
				Codec:       "PGSSUB",
				ContentType: "application/x-pgs",
				Default:     true,
				Resource:    provider.PlaybackResource{URLs: []string{"https://example.test/Subtitles/5/0/Stream.pgssub"}},
			},
		},
	}
	if !sess.ownPlaybackPlan(&valid, req, allowAll) {
		t.Fatal("expected valid HLS plan with plan-level AudioTracks and PGS subtitle to pass validation")
	}
	if len(valid.AudioTracks) != 2 {
		t.Fatalf("expected deduplicated AudioTracks len 2, got %d", len(valid.AudioTracks))
	}
	if valid.Subtitles[0].Codec != "pgssub" || valid.Subtitles[0].ContentType != "application/x-pgs" {
		t.Fatalf("expected normalized PGS codec/contentType, got codec=%q ct=%q", valid.Subtitles[0].Codec, valid.Subtitles[0].ContentType)
	}

	badActive := valid
	badActive.AudioTrack = "999"
	if sess.ownPlaybackPlan(&badActive, req, allowAll) {
		t.Fatal("expected plan with unknown AudioTrack ID to fail validation")
	}

	badNoTracks := valid
	badNoTracks.AudioTracks = nil
	badNoTracks.AudioTrack = "1"
	if sess.ownPlaybackPlan(&badNoTracks, req, allowAll) {
		t.Fatal("expected plan with AudioTrack but empty AudioTracks to fail validation")
	}
}

func TestHostFeaturesExposed(t *testing.T) {
	rt := newTestHostRuntime(t, t.Context())
	got := evalString(t, rt, `JSON.stringify({
		planAudioTracks: host.features.planAudioTracks,
		pgsSubtitles: host.features.pgsSubtitles,
		playbackStopped: host.features.playbackStopped,
		autoQualityUnderfed: host.features.autoQualityUnderfed
	})`)
	want := `{"planAudioTracks":true,"pgsSubtitles":true,"playbackStopped":true,"autoQualityUnderfed":true}`
	if got != want {
		t.Fatalf("host.features = %s, want %s", got, want)
	}
}
