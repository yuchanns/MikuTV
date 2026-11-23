package tvui

import (
	"context"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/provider"
	"github.com/yuchanns/MikuTV/internal/theme"
)

type uiCredentialsSource struct {
	gotReq provider.CredentialsRequest
	fail   provider.ErrorCode
}

func (*uiCredentialsSource) BeginSignIn(context.Context) (provider.QRAttempt, error) {
	return nil, &provider.Error{Code: provider.Unsupported}
}

func (s *uiCredentialsSource) SignInWithCredentials(_ context.Context, req provider.CredentialsRequest) (provider.Credential, error) {
	s.gotReq = req
	if s.fail != "" {
		return provider.Credential{}, &provider.Error{Code: s.fail}
	}
	return provider.Credential{
		Account: provider.Account{ID: "u-media", Name: req.Username},
		Data:    []byte(`{"secret":{"token":"ok"}}`),
	}, nil
}

func (*uiCredentialsSource) RestoreAccount(_ context.Context, credential provider.Credential) (provider.Credential, error) {
	return credential, nil
}
func (*uiCredentialsSource) SetAccount(*provider.Credential) error              { return nil }
func (*uiCredentialsSource) SignOut(context.Context, provider.Credential) error { return nil }
func (s *uiCredentialsSource) Browse(context.Context, provider.BrowseRequest) (provider.CollectionPage, error) {
	return provider.CollectionPage{}, &provider.Error{Code: provider.NeedsSignIn}
}
func (*uiCredentialsSource) Item(context.Context, provider.MediaRef) (provider.MediaItem, error) {
	return provider.MediaItem{}, nil
}
func (*uiCredentialsSource) Close() {}

func TestCredentialsSignInFlowMasksAndClearsPassword(t *testing.T) {
	assets, err := LoadAssets()
	if err != nil {
		t.Fatal(err)
	}
	pages, err := NewPages(assets)
	if err != nil {
		t.Fatal(err)
	}
	entries := []app.Entry{{
		ID:            "selfhosted",
		SourceID:      "selfhosted",
		Name:          "Home Media",
		Capabilities:  provider.Capabilities{provider.SignIn, provider.BrowseCatalog},
		AuthMethods:   []provider.AuthMethod{provider.AuthMethodCredentials},
		ServerURLMode: "required",
		Auth: provider.AuthPresentation{
			GuestTitle:       "Connect to Home Media",
			GuestDescription: "Sign in to browse your library.",
			CredentialsTitle: "Sign In to Home Media",
		},
	}}
	state, err := app.NewState(entries, "selfhosted", pages.Routes)
	if err != nil {
		t.Fatal(err)
	}
	queued := make(chan func(), 16)
	post := func(f func()) { queued <- f }
	source := &uiCredentialsSource{}
	catalog := app.NewCatalog(func(string) provider.Session { return source }, post, func() {})
	auth := app.NewAuth(uiTestKeychain{}, post, func() {}, catalog.Reload, func() {})
	defer auth.Close()

	view := newTestView(t, Options{
		Auth: auth, Catalog: catalog, Assets: assets, Pages: pages, State: state, Theme: theme.Default(),
	})
	tester := ui.NewTester(view.Render, 1920, 1080)
	await := func(done func() bool) {
		t.Helper()
		alarm := time.NewTimer(4 * time.Second)
		defer alarm.Stop()
		for !done() {
			select {
			case fn := <-queued:
				fn()
				tester.Frame()
			case <-alarm.C:
				t.Fatal("credentials flow did not settle in time")
			}
		}
	}

	view.SyncEntry()
	await(func() bool { return auth.Accounts().Ready && catalog.Snapshot().Phase == app.CatalogFailed })

	// Guest Home page should display the entry's guest hero + CredentialsLogin button (not ScanLogin).
	if _, ok := tester.Find(assets.Text.Auth.CredentialsLogin); !ok {
		t.Fatalf("guest Home page did not show CredentialsLogin %q", assets.Text.Auth.CredentialsLogin)
	}
	if _, ok := tester.Find(assets.Text.Auth.ScanLogin); ok {
		t.Fatal("credentials-only source must not show QR ScanLogin button")
	}

	// Open Account page and click Sign In -> opens SignInCredentials modal focused on Server URL.
	state.Navigate(app.Route{Page: app.Account})
	tester.Frame()
	if _, ok := tester.Find(assets.Text.Auth.CredentialsLogin); !ok {
		t.Fatal("guest Account page did not show CredentialsLogin")
	}
	tester.Key(0, ui.KeyEnter)
	if state.Page != app.SignInCredentials {
		t.Fatalf("page = %q, want %q", state.Page, app.SignInCredentials)
	}
	if state.Focus != credServerField {
		t.Fatalf("initial focus = %q, want %q", state.Focus, credServerField)
	}
	if _, ok := tester.Find("Sign In to Home Media"); !ok {
		t.Fatal("credentials modal did not show entry CredentialsTitle")
	}

	// Submit with empty server URL -> local validation error.
	view.submitCredentials()
	tester.Frame()
	if _, ok := tester.Find(assets.Text.Auth.ServerURLInvalid); !ok {
		t.Fatal("expected ServerURLInvalid error on empty server URL")
	}

	// Fill valid fields including password; verify plaintext password is NEVER exposed in UI labels!
	secretPass := "SuperSecretPass99"
	view.credDraft.serverURL = "192.168.1.50:8096"
	view.credDraft.username = "alice"
	view.credDraft.password = secretPass
	tester.Frame()
	if _, leaked := tester.Find(secretPass); leaked {
		t.Fatal("plaintext password leaked into rendered UI or accessibility label!")
	}

	// Submit valid credentials -> signs in, dismisses modal, clears draft password.
	view.submitCredentials()
	await(func() bool { return auth.Accounts().CurrentID == "u-media" && state.Page == app.Account })

	if source.gotReq.ServerURL != "http://192.168.1.50:8096" {
		t.Fatalf("normalized ServerURL = %q, want http://192.168.1.50:8096", source.gotReq.ServerURL)
	}
	if source.gotReq.Username != "alice" || source.gotReq.Password != secretPass {
		t.Fatalf("unexpected credentials request: username=%q", source.gotReq.Username)
	}
	if view.credDraft.password != "" || view.credDraft.serverURL != "" {
		t.Fatal("expected credentials draft to be completely cleared after leaving sign-in modal")
	}
	if state.Focus != app.AccountAction {
		t.Fatalf("focus after sign-in = %q, want %q", state.Focus, app.AccountAction)
	}
}
