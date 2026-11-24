package app

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/yuchanns/MikuTV/internal/player"
	"github.com/yuchanns/MikuTV/internal/provider"
)

func TestRoutesInheritParentCapabilitiesAndUpdateEntriesReconcilesRemoval(t *testing.T) {
	home := Route{Page: Home}
	picker := Route{Page: Entries}
	lib := Route{Page: Library}
	hist := Route{Page: Library, Section: "history"}
	routes, err := NewRoutes(home, picker, []RouteSpec{
		{Route: picker},
		{Route: home, RequiresEntry: true},
		{Route: lib, RequiresEntry: true, Capability: provider.BrowseLibrary},
		{Route: hist, Parent: lib, Capability: provider.AccountHistory.Capability()},
	})
	if err != nil {
		t.Fatal(err)
	}
	spec, ok := routes.Resolve(hist)
	if !ok || !spec.RequiresEntry || len(spec.Capabilities) != 2 {
		t.Fatalf("child route did not inherit parent capability: %+v", spec)
	}

	entries := []Entry{
		{ID: "e1", SourceID: "s1", Capabilities: provider.Capabilities{provider.BrowseCatalog, provider.AccountHistory.Capability()}},
		{ID: "e2", SourceID: "s2", Capabilities: provider.Capabilities{provider.BrowseCatalog, provider.BrowseLibrary, provider.AccountHistory.Capability()}},
	}
	state, err := NewState(entries, "e1", routes)
	if err != nil {
		t.Fatal(err)
	}
	// e1 lacks parent BrowseLibrary -> must NOT allow hist even though e1 has AccountHistory!
	if state.Allows(spec) {
		t.Fatal("route allowed without parent capability")
	}
	if !state.SelectEntry("e2") || !state.Allows(spec) {
		t.Fatal("route denied despite having both parent and child capabilities")
	}
	state.Navigate(hist)
	// Removing current entry e2 must return cleanly to Entries picker.
	state.UpdateEntries([]Entry{entries[0]})
	if state.CurrentEntry != "" || state.Route != picker {
		t.Fatalf("removing active entry did not return to picker: entry=%q route=%+v", state.CurrentEntry, state.Route)
	}
	// Updating e1 capabilities bumps Generation for same-ID hot rebinding.
	updated := entries[0]
	updated.Capabilities = append(updated.Capabilities, provider.SignIn)
	state.UpdateEntries([]Entry{updated})
	if got := state.Entries()[0]; got.Generation == 0 {
		t.Fatalf("updating entry did not increment Generation: %+v", got)
	}
}

func TestAuthConfirmSurvivesConcurrentSignInAndSyncsSession(t *testing.T) {
	store := &authMemoryStore{records: make(map[[2]string][]byte)}
	entry := Entry{ID: "entry", SourceID: "source"}
	cred := testCredential("u1")
	raw, _ := json.Marshal(accountRecord{Selected: "u1", Accounts: []provider.Credential{cred}})
	_ = store.Save(entry.SourceID, entry.ID, raw)

	confirmStarted := make(chan struct{})
	releaseConfirm := make(chan struct{})
	var lastActivated provider.Credential
	service := &testAuthService{
		restore: func(ctx context.Context, c provider.Credential) (provider.Credential, error) {
			if !c.PendingConfirmation {
				c.Data = json.RawMessage(`{"step":"rotated"}`)
				c.PendingConfirmation = true
				return c, nil
			}
			close(confirmStarted)
			select {
			case <-releaseConfirm:
			case <-ctx.Done():
				return provider.Credential{}, ctx.Err()
			}
			c.Data = json.RawMessage(`{"step":"confirmed"}`)
			c.PendingConfirmation = false
			return c, nil
		},
		begin: func(context.Context) (provider.QRAttempt, error) {
			return testAttempt{
				challenge: provider.QRChallenge{URL: "https://example.com/qr", ExpiresAt: time.Now().Add(time.Minute)},
				poll: func(ctx context.Context) (provider.QRProgress, error) {
					<-ctx.Done()
					return provider.QRProgress{}, ctx.Err()
				},
			}, nil
		},
	}
	// Wrap SetAccount to observe confirmed payload.
	wrapped := &capturingAuthService{testAuthService: service, onSet: func(c *provider.Credential) {
		if c != nil {
			lastActivated = *c
		}
	}}

	posted := make(chan func(), 20)
	auth := NewAuth(store, func(fn func()) { posted <- fn }, func() {}, func() {}, func() {})
	defer auth.Close()

	drain := func(cond func() bool) {
		t.Helper()
		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()
		for !cond() {
			select {
			case fn := <-posted:
				fn()
			case <-timer.C:
				t.Fatal("timed out waiting for auth condition")
			}
		}
	}

	auth.Bind(entry, wrapped)
	drain(func() bool { return auth.Accounts().CurrentID == "u1" })
	<-confirmStarted

	// Starting a concurrent QR sign-in must NOT cancel confirm().
	if !auth.StartSignIn(func() {}) {
		t.Fatal("StartSignIn failed while confirming")
	}
	close(releaseConfirm)
	drain(func() bool { return auth.confirmWork == nil })
	if string(lastActivated.Data) != `{"step":"confirmed"}` || lastActivated.PendingConfirmation {
		t.Fatalf("confirmed credentials were not synced to live session: %+v", lastActivated)
	}
}

type capturingAuthService struct {
	*testAuthService
	onSet func(*provider.Credential)
}

func (c *capturingAuthService) SetAccount(cred *provider.Credential) error {
	if c.onSet != nil {
		c.onSet(cred)
	}
	return c.testAuthService.SetAccount(cred)
}

type reportingSession struct {
	playbackTestSession
	reported int
}

func (r *reportingSession) ReportPlayback(context.Context, provider.PlaybackProgress) error {
	r.reported++
	return nil
}

func TestPlaybackChecksReportPlaybackCapability(t *testing.T) {
	session := &reportingSession{}
	catalog := &Catalog{session: session, entryID: "e", canReport: false}
	events := make(chan func(), 4)
	controller := NewPlayback(catalog, player.Engine{}, new(playbackTestStore), func(fn func()) { events <- fn }, func() {})
	controller.loaded = true
	controller.owner = "u1"
	controller.syncSource = true
	controller.state.Target.Ref = provider.MediaRef{EntryID: "e", Kind: provider.Video, ID: "v1"}
	controller.state.Position = 30
	controller.save()
	if session.reported != 0 {
		t.Fatalf("ReportPlayback called without capability")
	}
	catalog.canReport = true
	controller.state.Position = 60
	controller.save()
	select {
	case fn := <-events:
		fn()
	case <-time.After(3 * time.Second):
		t.Fatal("ReportPlayback did not complete with capability enabled")
	}
	if session.reported != 1 {
		t.Fatalf("ReportPlayback calls = %d, want 1", session.reported)
	}
}

type hdrFallbackSession struct {
	playbackTestSession
	requests []provider.PlaybackRequest
}

func (s *hdrFallbackSession) ResolvePlayback(_ context.Context, req provider.PlaybackRequest) (provider.Playback, error) {
	s.requests = append(s.requests, req)
	return provider.Playback{
		Plan: provider.PlaybackPlan{
			PartID: "p1", Quality: "120", Duration: 60, Access: provider.VideoAccessGranted,
			Streams: []provider.PlaybackStream{{
				Format: provider.PlaybackDirect,
				Video:  provider.PlaybackTrack{Resource: provider.PlaybackResource{URLs: []string{"https://cdn.example/v.mp4"}}},
			}},
		},
		Transport: playbackTestTransport{},
	}, nil
}

func TestPlaybackFallsBackFromHDRWhenDisplayIncompatible(t *testing.T) {
	session := &hdrFallbackSession{}
	catalog := &Catalog{session: session, entryID: "e"}
	events := make(chan func(), 8)
	var status player.Status
	engine := player.Engine{
		Capabilities: func() player.Capabilities { return player.Capabilities{HDR: true, HEVC: true} },
		Open:         func(string, float64) bool { return true },
		Snapshot:     func() player.Status { return status },
		SetRate:      func(float64) {},
		Stop:         func() {},
	}
	controller := NewPlayback(catalog, engine, new(playbackTestStore), func(fn func()) { events <- fn }, func() {})
	defer controller.Close()
	controller.Start(provider.PlaybackTarget{Ref: provider.MediaRef{EntryID: "e", Kind: provider.Video, ID: "v1"}}, "u1", false, nil)
	(<-events)()
	if len(session.requests) != 1 || !session.requests[0].HDR {
		t.Fatalf("initial resolve should request HDR=true: %+v", session.requests)
	}
	// Simulate native AVPlayer rejecting PQ stream with AVErrorNoCompatibleAlternatesForExternalDisplay (-11868).
	controller.state.VideoRange = "PQ"
	status = player.Status{Failed: true, DisplayIncompatible: true, ErrorCode: -11868}
	controller.Tick(time.Now())
	(<-events)()
	if len(session.requests) != 2 || session.requests[1].HDR {
		t.Fatalf("expected automatic SDR fallback (HDR=false), got %+v", session.requests)
	}
	if controller.State().Failure != "" {
		t.Fatalf("expected clean fallback without failure state, got %q", controller.State().Failure)
	}
}
