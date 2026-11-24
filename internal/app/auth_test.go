package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/yuchanns/MikuTV/internal/provider"
)

type authMemoryStore struct {
	mu      sync.Mutex
	records map[[2]string][]byte
}

func (s *authMemoryStore) Load(source, entry string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return bytes.Clone(s.records[[2]string{source, entry}]), nil
}

func (s *authMemoryStore) Save(source, entry string, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records[[2]string{source, entry}] = bytes.Clone(data)
	return nil
}

type testAuthService struct {
	begin   func(context.Context) (provider.QRAttempt, error)
	restore func(context.Context, provider.Credential) (provider.Credential, error)
	active  string
}

func (s *testAuthService) BeginSignIn(ctx context.Context) (provider.QRAttempt, error) {
	return s.begin(ctx)
}

func (s *testAuthService) RestoreAccount(ctx context.Context, credential provider.Credential) (provider.Credential, error) {
	return s.restore(ctx, credential)
}

func (s *testAuthService) SetAccount(credential *provider.Credential) error {
	s.active = ""
	if credential != nil {
		s.active = credential.Account.ID
	}
	return nil
}

func (s *testAuthService) SignOut(context.Context, provider.Credential) error { return nil }

type testAttempt struct {
	challenge provider.QRChallenge
	poll      func(context.Context) (provider.QRProgress, error)
}

func (a testAttempt) Challenge() provider.QRChallenge { return a.challenge }
func (a testAttempt) Poll(ctx context.Context) (provider.QRProgress, error) {
	return a.poll(ctx)
}

func TestAuthDiscardsCanceledScanAndIsolatesEntries(t *testing.T) {
	store := &authMemoryStore{records: make(map[[2]string][]byte)}
	entryA, entryB := Entry{ID: "entry-a", SourceID: "source"}, Entry{ID: "entry-b", SourceID: "source"}
	for _, entry := range []Entry{entryA, entryB} {
		credential := testCredential(entry.ID)
		data, err := json.Marshal(accountRecord{Selected: entry.ID, Accounts: []provider.Credential{credential}})
		if err != nil {
			t.Fatal(err)
		}
		if err := store.Save(entry.SourceID, entry.ID, data); err != nil {
			t.Fatal(err)
		}
	}
	posted := make(chan func(), 20)
	auth := NewAuth(store, func(update func()) { posted <- update }, func() {}, func() {}, func() {})
	defer auth.Close()
	await := func(ready func() bool) {
		t.Helper()
		timer := time.NewTimer(6 * time.Second)
		defer timer.Stop()
		for !ready() {
			select {
			case update := <-posted:
				update()
			case <-timer.C:
				t.Fatal("authentication state did not settle")
			}
		}
	}
	oldPoll, release := make(chan struct{}), make(chan struct{})
	releaseOnce := sync.OnceFunc(func() { close(release) })
	defer releaseOnce()
	var attempts int
	serviceA := &testAuthService{
		restore: func(_ context.Context, credential provider.Credential) (provider.Credential, error) {
			return credential, nil
		},
		begin: func(context.Context) (provider.QRAttempt, error) {
			attempts++
			attempt := testAttempt{challenge: provider.QRChallenge{
				URL: "https://provider.invalid/new", ExpiresAt: time.Now().Add(15 * time.Second),
			}}
			if attempts == 1 {
				attempt.challenge.URL = "https://provider.invalid/old"
				attempt.poll = func(context.Context) (provider.QRProgress, error) {
					close(oldPoll)
					<-release
					return provider.QRProgress{Status: provider.QRApproved, Credential: testCredential("late-result")}, nil
				}
			} else {
				attempt.poll = func(ctx context.Context) (provider.QRProgress, error) {
					<-ctx.Done()
					return provider.QRProgress{}, ctx.Err()
				}
			}
			return attempt, nil
		},
	}
	auth.Bind(entryA, serviceA)
	await(func() bool { return auth.Accounts().CurrentID == entryA.ID })
	if !auth.StartSignIn(func() {}) {
		t.Fatal("could not begin sign-in")
	}
	await(func() bool { return auth.SignIn().Phase == SignInWaiting })
	select {
	case <-oldPoll:
	case <-time.After(6 * time.Second):
		t.Fatal("old poll did not start")
	}
	auth.CancelSignIn()
	if !auth.StartSignIn(func() {}) {
		t.Fatal("could not issue a replacement challenge")
	}
	await(func() bool { return auth.SignIn().Challenge.URL == "https://provider.invalid/new" })
	releaseOnce()
	select {
	case late := <-posted:
		late()
	case <-time.After(6 * time.Second):
		t.Fatal("late result did not reach the delivery boundary")
	}
	if auth.Accounts().CurrentID != entryA.ID || auth.SignIn().Phase != SignInWaiting {
		t.Fatal("a canceled attempt changed the next challenge or active account")
	}

	confirmation := make(chan error, 1)
	serviceB := &testAuthService{restore: func(_ context.Context, credential provider.Credential) (provider.Credential, error) {
		if !credential.PendingConfirmation {
			credential.Data = json.RawMessage(`{"rotated":true}`)
			credential.PendingConfirmation = true
			return credential, nil
		}
		stored, err := store.Load(entryB.SourceID, entryB.ID)
		if err == nil && !bytes.Contains(stored, []byte(`"pendingConfirmation":true`)) {
			err = errors.New("confirmation preceded persistence")
		}
		confirmation <- err
		credential.PendingConfirmation = false
		return credential, nil
	}}
	auth.Bind(entryB, serviceB)
	await(func() bool { return auth.Accounts().CurrentID == entryB.ID })
	select {
	case err := <-confirmation:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("credential rotation was not confirmed")
	}
	await(func() bool { return auth.work == nil && auth.confirmWork == nil })
	if accounts := auth.Accounts().Accounts; len(accounts) != 1 || accounts[0].ID != entryB.ID {
		t.Fatal("loaded accounts from a different entry")
	}
	auth.SignOut(entryB.ID)
	await(func() bool { return !auth.Accounts().Busy })
	dataA, _ := store.Load(entryA.SourceID, entryA.ID)
	dataB, _ := store.Load(entryB.SourceID, entryB.ID)
	if !bytes.Contains(dataA, []byte(`"selected":"entry-a"`)) || len(dataB) != 0 || serviceB.active != "" {
		t.Fatal("sign-out did not clear only the selected entry's credentials")
	}
}

func testCredential(id string) provider.Credential {
	return provider.Credential{Account: provider.Account{ID: id, Name: id}, Data: json.RawMessage(`{}`)}
}

func TestAuthRestoreOnlyForgetsCredentialsWhenSignInIsRequired(t *testing.T) {
	for _, code := range []provider.ErrorCode{provider.NeedsSignIn, provider.Network, provider.Timeout, provider.RateLimited} {
		t.Run(string(code), func(t *testing.T) {
			entry := Entry{ID: "entry", SourceID: "source"}
			store := &authMemoryStore{records: make(map[[2]string][]byte)}
			saved := accountRecord{
				Selected: "expired",
				Accounts: []provider.Credential{testCredential("expired"), testCredential("other")},
			}
			original, err := json.Marshal(saved)
			if err != nil {
				t.Fatal(err)
			}
			if err := store.Save(entry.SourceID, entry.ID, original); err != nil {
				t.Fatal(err)
			}
			callbacks := make(chan func(), 2)
			prompts := 0
			auth := NewAuth(store, func(update func()) { callbacks <- update }, func() {}, func() {}, func() { prompts++ })
			defer auth.Close()
			auth.Bind(entry, &testAuthService{
				restore: func(context.Context, provider.Credential) (provider.Credential, error) {
					return provider.Credential{}, &provider.Error{Code: code}
				},
			})
			waitAccountRead(t, callbacks)()
			waitAccountRead(t, callbacks)()
			snapshot := auth.Accounts()
			if !snapshot.Ready || snapshot.Busy || snapshot.Error != code {
				t.Fatalf("unexpected account state: %+v", snapshot)
			}
			stored, err := store.Load(entry.SourceID, entry.ID)
			if err != nil {
				t.Fatal(err)
			}
			if code == provider.NeedsSignIn {
				var record accountRecord
				if err := json.Unmarshal(stored, &record); err != nil {
					t.Fatal(err)
				}
				if prompts != 1 || record.Selected != "" || len(record.Accounts) != 1 || record.Accounts[0].Account.ID != "other" {
					t.Fatal("sign-in recovery did not remove exactly the expired credential and prompt once")
				}
			} else if prompts != 0 || !bytes.Equal(stored, original) {
				t.Fatal("a transient failure removed saved account data or opened sign-in")
			}
		})
	}
}

func TestAuthAcceptsApprovalDeliveredAfterExpiry(t *testing.T) {
	store := &authMemoryStore{records: make(map[[2]string][]byte)}
	posted := make(chan func(), 2)
	auth := NewAuth(store, func(update func()) { posted <- update }, func() {}, func() {}, func() {})
	auth.entry = Entry{ID: "entry", SourceID: "source"}
	auth.service = &testAuthService{}
	auth.signIn = SignInSnapshot{Phase: SignInWaiting, Challenge: provider.QRChallenge{
		ExpiresAt: time.Now().Add(-time.Second),
	}}
	work := auth.startWork()
	defer auth.Close()
	auth.deliver(work, func() { auth.approve(work, testCredential("viewer"), func() {}) })
	(<-posted)()
	data, _ := store.Load("source", "entry")
	if auth.SignIn().Phase != SignInSuccess || auth.Accounts().CurrentID != "viewer" || len(data) == 0 {
		t.Fatal("a confirmed approval was discarded while waiting to be delivered")
	}
}
