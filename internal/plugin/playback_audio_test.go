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

func TestPlaybackSubtitlesAcceptsURLlessLyricsTrack(t *testing.T) {
	manifest := `{
		"id": "lyrics-test", "name": "Lyrics Test", "kind": "source",
		"version": "1.0.0", "apiVersion": 1, "minAppVersion": "0.1.0",
		"main": "index.js",
		"capabilities": ["catalog.browse", "playback.resolve", "playback.subtitles"],
		"permissions": {"network": ["cdn.example"]}
	}`
	code := `
		export async function browse() { return { collections: [] }; }
		export async function resolvePlayback() {
			return {
				duration: 240,
				subtitles: [{ id: "lrc-bilingual", label: "双语歌词", language: "ja", default: true }],
				streams: [{
					format: "direct",
					audio: { contentType: "audio/mpeg", resource: { urls: ["https://cdn.example/a.mp3"] } }
				}]
			};
		}
		export async function readSubtitles({ track }) {
			if (track.id !== "lrc-bilingual") return { cues: [] };
			return {
				cues: [
					{ start: 0, end: 4, text: "青い夜の底で\n在蓝调暗夜深处" },
					{ start: 4, end: 9, text: "光だけが回らない\n唯独那束斜光静止不转" }
				]
			};
		}
	`
	src, err := Open(t.Context(), zipFiles(t, map[string][]byte{
		"plugin.json": []byte(manifest),
		"index.js":    []byte(code),
	}), testVersion)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	store, err := NewStorage(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	sess := src.NewSession("lyrics-test", store)
	defer sess.Close()
	target := provider.PlaybackTarget{Ref: provider.MediaRef{EntryID: "lyrics-test", Kind: provider.Video, ID: "s1"}}
	pb, err := sess.ResolvePlayback(t.Context(), provider.PlaybackRequest{PlaybackTarget: target})
	if err != nil {
		t.Fatalf("ResolvePlayback failed: %v", err)
	}
	if len(pb.Plan.Subtitles) != 1 || pb.Plan.Subtitles[0].ID != "lrc-bilingual" {
		t.Fatalf("Subtitles = %+v, want 1 URL-less track", pb.Plan.Subtitles)
	}
	cues, err := sess.PlaybackSubtitles(t.Context(), provider.PlaybackSubtitleQuery{
		PlaybackTarget: target,
		Track:          pb.Plan.Subtitles[0],
	})
	if err != nil {
		t.Fatalf("PlaybackSubtitles failed: %v", err)
	}
	if len(cues) != 2 || cues[0].Text != "青い夜の底で\n在蓝调暗夜深处" {
		t.Fatalf("cues = %+v", cues)
	}
}

func TestSessionPassesSecretAcrossAllReadOperations(t *testing.T) {
	manifest := `{
		"id": "secret-audit",
		"name": "Secret Audit",
		"description": "Verifies session secret forwarding",
		"kind": "source",
		"version": "1.0.0",
		"apiVersion": 1,
		"minAppVersion": "0.1.0",
		"main": "index.js",
		"permissions": {"network": ["api.example"]},
		"authentication": {"methods": ["qrCode"]},
		"capabilities": [
			"account.signIn",
			"catalog.browse",
			"catalog.creator",
			"catalog.series",
			"catalog.live",
			"catalog.videoComments",
			"account.videoFavorites",
			"playback.danmaku",
			"account.danmakuRules"
		]
	}`
	code := `
		function requireToken(input, name) {
			if (!input || !input.secret || input.secret.token !== "tok-123") {
				throw { code: "plugin", message: name + " missing secret" };
			}
		}
		export async function beginSignIn() { return { url: "https://api.example/qr", key: "k1" }; }
		export async function pollSignIn() { return { status: "waiting" }; }
		export async function restoreAccount() { return { account: { id: "u1", name: "User" }, secret: { token: "tok-123" } }; }
		export async function signOut() { return {}; }
		export async function browse() { return { collections: [] }; }
		export async function readCreator(input) {
			requireToken(input, "readCreator");
			return { profile: { creator: { id: input.creatorID, name: "Artist" }, tabs: ["videos"] }, items: [] };
		}
		export async function videoFavoriteFolders(input) {
			requireToken(input, "videoFavoriteFolders");
			return { folders: [{ id: "f1", title: "Fav", count: 1, selected: true }] };
		}
		export async function editVideoFavoriteFolders() { return {}; }
		export async function videoComments(input) {
			requireToken(input, "videoComments");
			return { comments: [] };
		}
		export async function readSeries(input) {
			requireToken(input, "readSeries");
			return { items: [] };
		}
		export async function readLive(input) {
			requireToken(input, "readLive");
			return { rooms: [] };
		}
		export async function readDanmaku(input) {
			requireToken(input, "readDanmaku");
			return { items: [] };
		}
		export async function readDanmakuRules(input) {
			requireToken(input, "readDanmakuRules");
			return { rules: [] };
		}
		export async function addDanmakuRule() { return { rule: { kind: "keyword", pattern: "x" } }; }
		export async function removeDanmakuRule() { return {}; }
	`
	src, err := Open(t.Context(), zipFiles(t, map[string][]byte{
		"plugin.json": []byte(manifest),
		"index.js":    []byte(code),
	}), testVersion)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	store, err := NewStorage(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	sess := src.NewSession("secret-audit", store)
	defer sess.Close()
	cred, err := sess.credential(authResult{
		Status:  provider.QRApproved,
		Account: provider.Account{ID: "u1", Name: "User"},
		Secret:  []byte(`{"token":"tok-123"}`),
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := sess.SetAccount(&cred); err != nil {
		t.Fatal(err)
	}
	ref := provider.MediaRef{EntryID: "secret-audit", Kind: provider.Video, ID: "v1"}
	if _, err := sess.ReadCreator(t.Context(), provider.CreatorRequest{CreatorID: "c1", Tab: provider.CreatorTabVideos, IncludeProfile: true}); err != nil {
		t.Fatalf("ReadCreator: %v", err)
	}
	if _, err := sess.VideoFavoriteFolders(t.Context(), ref); err != nil {
		t.Fatalf("VideoFavoriteFolders: %v", err)
	}
	if _, err := sess.VideoComments(t.Context(), provider.VideoCommentQuery{Ref: ref, Order: provider.VideoCommentsHot}); err != nil {
		t.Fatalf("VideoComments: %v", err)
	}
	if _, err := sess.ReadSeries(t.Context(), provider.SeriesRequest{Mode: provider.SeriesModeHub}); err != nil {
		t.Fatalf("ReadSeries: %v", err)
	}
	if _, err := sess.ReadLive(t.Context(), provider.LiveRequest{Section: provider.LiveSectionHub}); err != nil {
		t.Fatalf("ReadLive: %v", err)
	}
	if _, err := sess.Danmaku(t.Context(), provider.DanmakuQuery{PlaybackTarget: provider.PlaybackTarget{Ref: ref}, Segment: 1}); err != nil {
		t.Fatalf("Danmaku: %v", err)
	}
	if _, err := sess.DanmakuRules(t.Context()); err != nil {
		t.Fatalf("DanmakuRules: %v", err)
	}
}
