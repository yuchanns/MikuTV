package plugin

import (
	"testing"

	"github.com/yuchanns/MikuTV/internal/provider"
)

func TestOwnPlaybackPlanAcceptsPureAudioDirectStream(t *testing.T) {
	var manifest sourceManifest
	manifest.ID = "audio-test"
	manifest.Permissions.Network = []string{"cdn.example"}
	src := &Source{manifest: manifest}
	sess := &Session{source: src}
	allow := src.allowRequest(true)
	req := provider.PlaybackRequest{
		PlaybackTarget: provider.PlaybackTarget{
			Ref: provider.MediaRef{EntryID: "audio-test", Kind: provider.Video, ID: "track-1"},
		},
	}

	plan := provider.PlaybackPlan{
		Duration: 215,
		Quality:  "lossless",
		Qualities: []provider.PlaybackQuality{
			{ID: "standard", Label: "320k"},
			{ID: "lossless", Label: "FLAC"},
		},
		Streams: []provider.PlaybackStream{{
			Format: provider.PlaybackDirect,
			Audio: &provider.PlaybackTrack{
				ContentType: "audio/flac",
				Resource: provider.PlaybackResource{
					URLs: []string{"https://cdn.example/audio/track-1.flac"},
				},
			},
		}},
	}
	if !sess.ownPlaybackPlan(&plan, req, allow) {
		t.Fatal("expected pure audio PlaybackDirect plan to pass validation")
	}

	// Reject stream containing both Video and Audio on PlaybackDirect
	mixed := plan
	mixed.Streams = []provider.PlaybackStream{{
		Format: provider.PlaybackDirect,
		Video: provider.PlaybackTrack{
			Resource: provider.PlaybackResource{URLs: []string{"https://cdn.example/video.mp4"}},
		},
		Audio: &provider.PlaybackTrack{
			Resource: provider.PlaybackResource{URLs: []string{"https://cdn.example/audio.flac"}},
		},
	}}
	if sess.ownPlaybackPlan(&mixed, req, allow) {
		t.Fatal("expected mixed video+audio PlaybackDirect stream to be rejected")
	}

	// Reject unauthorized audio host
	unauth := plan
	unauth.Streams = []provider.PlaybackStream{{
		Format: provider.PlaybackDirect,
		Audio: &provider.PlaybackTrack{
			Resource: provider.PlaybackResource{URLs: []string{"https://evil.example/track.mp3"}},
		},
	}}
	if sess.ownPlaybackPlan(&unauth, req, allow) {
		t.Fatal("expected unauthorized audio host to be rejected")
	}
}

func TestOwnPlaybackPlanValidatesAvailableKinds(t *testing.T) {
	var manifest sourceManifest
	manifest.ID = "audio-test"
	manifest.Permissions.Network = []string{"cdn.example"}
	src := &Source{manifest: manifest}
	sess := &Session{source: src}
	allow := src.allowRequest(true)
	req := provider.PlaybackRequest{
		PlaybackTarget: provider.PlaybackTarget{
			Ref: provider.MediaRef{EntryID: "audio-test", Kind: provider.Video, ID: "track-1"},
		},
		MediaKind: provider.PlaybackMediaVideo,
	}
	if !validPlaybackMediaKind(req.MediaKind) || !validPlaybackMediaKind("") || validPlaybackMediaKind("invalid") {
		t.Fatal("unexpected validPlaybackMediaKind behavior")
	}
	plan := provider.PlaybackPlan{
		Duration:       215,
		AvailableKinds: []string{"audio", "video", "audio"},
		Streams: []provider.PlaybackStream{{
			Format: provider.PlaybackDirect,
			Video: provider.PlaybackTrack{
				ContentType: "video/mp4",
				Resource:    provider.PlaybackResource{URLs: []string{"https://cdn.example/mv.mp4"}},
			},
		}},
	}
	if !sess.ownPlaybackPlan(&plan, req, allow) {
		t.Fatal("expected valid AvailableKinds to pass")
	}
	if len(plan.AvailableKinds) != 2 {
		t.Fatalf("expected deduplicated AvailableKinds, got %v", plan.AvailableKinds)
	}
	bad := plan
	bad.AvailableKinds = []string{"audio", "bogus"}
	if sess.ownPlaybackPlan(&bad, req, allow) {
		t.Fatal("expected invalid AvailableKinds entry to fail validation")
	}
}
