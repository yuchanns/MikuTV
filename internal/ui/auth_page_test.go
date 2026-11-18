package tvui

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/provider"
	"github.com/yuchanns/MikuTV/internal/theme"
)

func TestExpiredQRCodeKeepsConfirmAndBackReachable(t *testing.T) {
	assets, err := LoadAssets()
	if err != nil {
		t.Fatal(err)
	}
	pages, err := NewPages(assets)
	if err != nil {
		t.Fatal(err)
	}
	entries := testEntries()
	entries[0].Capabilities = provider.Capabilities{provider.SignIn}
	entries[0].Auth = provider.AuthPresentation{GuestTitle: "Welcome to the source", QRTitle: "Authorize this source"}
	state, err := app.NewState(entries, entries[0].ID, pages.Routes)
	if err != nil {
		t.Fatal(err)
	}
	queued := make(chan func(), 12)
	post := func(f func()) { queued <- f }
	source := &uiAuthSource{}
	catalog := app.NewCatalog(func(string) provider.Session { return source }, post, func() {})
	store := uiTestKeychain{data: []byte(`{"accounts":[
		{"account":{"id":"backup","name":"Backup viewer"},"data":{}},
		{"account":{"id":"previous","name":"Saved viewer"},"data":{}}
	]}`)}
	auth := app.NewAuth(store, post, func() {}, catalog.Reload, func() {})
	defer auth.Close()
	view := NewView(Options{Auth: auth, Catalog: catalog, Assets: assets, Pages: pages, State: state, Theme: theme.Default()})
	tester := ui.NewTester(view.Render, 1920, 1080)
	await := func(done func() bool) {
		t.Helper()
		alarm := time.NewTimer(4 * time.Second)
		defer alarm.Stop()
		for !done() {
			select {
			case result := <-queued:
				result()
				tester.Frame()
			case <-alarm.C:
				t.Fatal("sign-in view did not reach the expected phase")
			}
		}
	}
	view.SyncEntry()
	await(func() bool { return auth.Accounts().Ready })
	tester.Key(0, ui.KeyLeft)
	for range 3 {
		tester.Key(0, ui.KeyDown)
	}
	tester.Key(0, ui.KeyEnter)
	if state.Page != app.Account || auth.SignIn().Phase != app.SignInIdle {
		t.Fatal("the sidebar account control did not open the account page")
	}
	if _, ok := tester.Find(assets.Text.Auth.ScanLogin); !ok {
		t.Fatal("guest account page did not show its sign-in action")
	}
	if _, ok := tester.Find(entries[0].Auth.GuestTitle); !ok {
		t.Fatal("guest account page did not display the entry's title")
	}
	tester.Key(0, ui.KeyEnter)
	layers := state.Layers()
	if state.Page != app.SignInQR || len(layers) != 2 || layers[0].Page != app.Account {
		t.Fatal("sign-in did not open over the account page")
	}
	if _, ok := tester.Find(entries[0].Auth.QRTitle); !ok {
		t.Fatal("sign-in did not use the entry's challenge title")
	}
	await(func() bool { return auth.SignIn().Phase == app.SignInWaiting })
	codeRect, codeOK := tester.Find(assets.Text.Auth.QR)
	await(func() bool { return auth.SignIn().Phase == app.SignInExpired })
	refreshRect, refreshOK := tester.Find(assets.Text.Auth.Refresh)
	if !codeOK || !refreshOK || codeRect != refreshRect {
		t.Fatalf("the expired refresh control must cover the QR code: code %v (%v), refresh %v (%v)",
			codeRect, codeOK, refreshRect, refreshOK)
	}
	tester.Key(0, ui.KeyEnter)
	await(func() bool { return auth.SignIn().Phase == app.SignInWaiting })
	if source.issued.Load() != 2 {
		t.Fatal("Confirm did not issue a replacement QR code")
	}
	await(func() bool { return auth.SignIn().Phase == app.SignInScanned })
	if _, ok := tester.Find(assets.Text.Auth.PhoneConfirm); !ok {
		t.Fatal("the scanned challenge did not ask for confirmation on the phone")
	}
	if _, ok := tester.Find(assets.Text.Auth.Secure); !ok {
		t.Fatal("the scanned challenge did not retain the secure sign-in caption")
	}
	tester.Key(0, ui.KeyEscape)
	if state.Page != app.Account || state.Focus != app.AccountAction || auth.SignIn().Phase != app.SignInIdle {
		t.Fatal("Back did not restore the account page sign-in action")
	}
	tester.Key(0, ui.KeyRight)
	tester.Key(0, ui.KeyEnter)
	if state.Page != app.AccountPicker {
		t.Fatal("guest could not open the saved accounts")
	}
	first, firstOK := tester.Find("Backup viewer")
	second, secondOK := tester.Find("Saved viewer")
	add, addOK := tester.Find(assets.Text.Auth.Add)
	if !firstOK || !secondOK || !addOK || first.Y+first.H > second.Y || second.Y+second.H > add.Y {
		t.Fatal("both saved account rows must fit above the add button")
	}
	tester.Key(0, ui.KeyUp)
	if state.Focus == app.AccountFocus("backup") {
		tester.Key(0, ui.KeyDown)
	}
	if state.Focus != app.AccountFocus("previous") {
		t.Fatalf("the saved account was unreachable from the add button: %s", state.Focus)
	}
	tester.Key(0, ui.KeyEnter)
	await(func() bool { return auth.Accounts().CurrentID == "previous" })
	if state.Page != app.Account || state.Focus != app.AccountAction {
		t.Fatal("selecting a saved account did not focus the signed-in action")
	}
}

type uiTestKeychain struct{ data []byte }

func (s uiTestKeychain) Load(string, string) ([]byte, error) { return s.data, nil }
func (uiTestKeychain) Save(string, string, []byte) error     { return nil }

type uiAuthSource struct{ issued atomic.Int32 }

func (s *uiAuthSource) BeginSignIn(context.Context) (provider.QRAttempt, error) {
	lifetime := 10 * time.Second
	if s.issued.Add(1) == 1 {
		lifetime = 700 * time.Millisecond
	}
	return uiAuthChallenge{URL: "https://source.invalid/scan", ExpiresAt: time.Now().Add(lifetime)}, nil
}

func (*uiAuthSource) RestoreAccount(_ context.Context, credential provider.Credential) (provider.Credential, error) {
	return credential, nil
}

func (*uiAuthSource) SetAccount(*provider.Credential) error              { return nil }
func (*uiAuthSource) SignOut(context.Context, provider.Credential) error { return nil }
func (*uiAuthSource) Browse(context.Context, provider.BrowseRequest) (provider.CollectionPage, error) {
	return provider.CollectionPage{}, nil
}
func (*uiAuthSource) Item(context.Context, provider.MediaRef) (provider.MediaItem, error) {
	return provider.MediaItem{}, nil
}
func (*uiAuthSource) Close() {}

type uiAuthChallenge provider.QRChallenge

func (a uiAuthChallenge) Challenge() provider.QRChallenge { return provider.QRChallenge(a) }
func (uiAuthChallenge) Poll(context.Context) (provider.QRProgress, error) {
	return provider.QRProgress{Status: provider.QRScanned}, nil
}
