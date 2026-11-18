package app

import (
	"context"
	"encoding/json"
	"image"
	"slices"
	"time"

	"github.com/yuchanns/MikuTV/internal/provider"
)

const (
	qrPollInterval = 2 * time.Second
	qrSuccessHold  = 950 * time.Millisecond
)

type CredentialStore interface {
	Load(pluginID, entryID string) ([]byte, error)
	Save(pluginID, entryID string, data []byte) error
}

type AccountCleaner interface {
	ClearAccount(entryID, owner string) error
}

type SignInPhase uint8

const (
	SignInIdle SignInPhase = iota
	SignInLoading
	SignInWaiting
	SignInScanned
	SignInExpired
	SignInSuccess
	SignInFailed
)

type SignInSnapshot struct {
	Phase     SignInPhase
	Challenge provider.QRChallenge
	Error     provider.ErrorCode
}

type AccountsSnapshot struct {
	Accounts   []provider.Account
	CurrentID  string
	SelectedID string
	Ready      bool
	Busy       bool
	Error      provider.ErrorCode
}

type accountRecord struct {
	Selected string                `json:"selected,omitempty"`
	Accounts []provider.Credential `json:"accounts"`
}

type authWork struct {
	context.Context
	cancel context.CancelFunc
}

type AccountAvatar struct{ Image image.Image }

type avatarLoad struct {
	accountID, url string
	cancel         context.CancelFunc
}

// Auth owns the active entry's credentials on the application thread. Network
// calls run in workers; only a result accepted on the application thread can
// activate an account or change its stored record.
type Auth struct {
	store          CredentialStore
	cleaner        AccountCleaner
	post           func(func())
	changed        func()
	accountChanged func()
	signInRequired func()
	onSettled      func()
	entry          Entry
	service        provider.Authenticator
	record         accountRecord
	accounts       AccountsSnapshot
	signIn         SignInSnapshot
	work           *authWork
	confirmWork    *authWork
	avatarLoad     *avatarLoad
	avatar         *AccountAvatar
	selecting      bool
}

func NewAuth(store CredentialStore, post func(func()), changed, accountChanged, signInRequired func()) *Auth {
	return &Auth{
		store: store, post: post, changed: changed,
		accountChanged: accountChanged, signInRequired: signInRequired,
	}
}

func (a *Auth) SetCleaner(cleaner AccountCleaner) { a.cleaner = cleaner }
func (a *Auth) SetOnSettled(fn func())            { a.onSettled = fn }

func (a *Auth) Accounts() AccountsSnapshot {
	snapshot := a.accounts
	snapshot.SelectedID = a.record.Selected
	for _, credential := range a.record.Accounts {
		snapshot.Accounts = append(snapshot.Accounts, credential.Account)
	}
	return snapshot
}

func (a *Auth) SignIn() SignInSnapshot { return a.signIn }

func (a *Auth) Avatar() *AccountAvatar { return a.avatar }

func (a *Auth) Bind(entry Entry, service provider.Authenticator) {
	if entry.ID == a.entry.ID && entry.SourceID == a.entry.SourceID && entry.Generation == a.entry.Generation && service == a.service {
		return
	}
	a.Close()
	a.entry, a.service = entry, service
	a.Load()
}

func (a *Auth) notifySettled() {
	if a.onSettled != nil {
		a.onSettled()
	}
}

func (a *Auth) Load() {
	if a.service == nil || a.accounts.Busy || a.accounts.Ready {
		if a.service == nil {
			a.notifySettled()
		}
		return
	}
	if a.store == nil {
		a.accounts.Error = provider.PluginFailed
		a.changed()
		a.notifySettled()
		return
	}
	work := a.startWork()
	pluginID, entryID := a.entry.SourceID, a.entry.ID
	a.accounts.Busy, a.accounts.Error = true, ""
	a.changed()
	go func() {
		data, err := a.store.Load(pluginID, entryID)
		var record accountRecord
		if err == nil && len(data) != 0 {
			err = json.Unmarshal(data, &record)
		}
		a.deliver(work, func() {
			a.finishWork()
			a.accounts.Busy = false
			if err != nil {
				a.accounts.Error = provider.PluginFailed
				a.changed()
				a.notifySettled()
				return
			}
			a.record, a.accounts.Ready = record, true
			if credential := a.find(record.Selected); credential != nil {
				a.restore(*credential, nil, false)
			} else {
				a.changed()
				a.notifySettled()
			}
		})
	}()
}

func (a *Auth) StartSignIn(onSuccess func()) bool {
	if !a.accounts.Ready || a.accounts.Busy {
		return false
	}
	work := a.startWork()
	service := a.service
	a.signIn = SignInSnapshot{Phase: SignInLoading}
	a.changed()
	go func() {
		attempt, err := service.BeginSignIn(work)
		if err != nil {
			a.deliver(work, func() { a.failSignIn(provider.CodeOf(err)) })
			return
		}
		challenge := attempt.Challenge()
		a.deliver(work, func() {
			a.signIn = SignInSnapshot{Phase: SignInWaiting, Challenge: challenge}
			a.changed()
		})
		ctx, cancel := context.WithDeadline(work, challenge.ExpiresAt)
		defer cancel()
		for {
			select {
			case <-ctx.Done():
				a.deliver(work, a.expireSignIn)
				return
			case <-time.After(qrPollInterval):
			}
			progress, err := attempt.Poll(ctx)
			if err == nil && progress.Status == provider.QRApproved {
				a.deliver(work, func() { a.approve(work, progress.Credential, onSuccess) })
				return
			}
			if ctx.Err() != nil || progress.Status == provider.QRExpired {
				a.deliver(work, a.expireSignIn)
				return
			}
			if err != nil {
				code := provider.CodeOf(err)
				if code == provider.Network || code == provider.Timeout {
					a.deliver(work, a.changed)
					continue
				}
				a.deliver(work, func() { a.failSignIn(code) })
				return
			}
			a.deliver(work, func() {
				a.signIn.Phase = SignInWaiting
				if progress.Status == provider.QRScanned {
					a.signIn.Phase = SignInScanned
				}
				a.changed()
			})
		}
	}()
	return true
}

func (a *Auth) CancelSignIn() {
	if a.signIn.Phase != SignInIdle {
		a.finishWork()
		a.signIn = SignInSnapshot{}
		a.changed()
	}
}

func (a *Auth) expireSignIn() {
	a.finishWork()
	a.signIn = SignInSnapshot{Phase: SignInExpired}
	a.changed()
}

func (a *Auth) failSignIn(code provider.ErrorCode) {
	a.finishWork()
	a.signIn = SignInSnapshot{Phase: SignInFailed, Error: code}
	a.changed()
}

func (a *Auth) approve(work *authWork, credential provider.Credential, onSuccess func()) {
	if err := a.activate(credential); err != nil {
		a.failSignIn(provider.CodeOf(err))
		return
	}
	a.signIn = SignInSnapshot{Phase: SignInSuccess}
	a.changed()
	time.AfterFunc(qrSuccessHold, func() {
		a.deliver(work, func() {
			a.finishWork()
			a.signIn = SignInSnapshot{}
			onSuccess()
			a.changed()
			if credential.PendingConfirmation {
				a.confirm(credential)
			}
		})
	})
}

func (a *Auth) SelectAccount(id string, onSuccess func()) {
	if !a.accounts.Ready || a.accounts.Busy {
		return
	}
	if id == a.accounts.CurrentID && id != "" {
		onSuccess()
		return
	}
	if credential := a.find(id); credential != nil {
		a.restore(*credential, onSuccess, true)
	}
}

func (a *Auth) RefreshCurrent(onSuccess func()) {
	if a.work != nil || a.accounts.Busy || a.accounts.CurrentID == "" {
		return
	}
	if credential := a.find(a.accounts.CurrentID); credential != nil {
		a.restore(*credential, onSuccess, false)
	}
}

func (a *Auth) restore(credential provider.Credential, onSuccess func(), selecting bool) {
	work, service := a.startWork(), a.service
	a.selecting = selecting
	a.accounts.Busy, a.accounts.Error = true, ""
	a.changed()
	go func() {
		updated, err := service.RestoreAccount(work, credential)
		a.deliver(work, func() {
			a.finishWork()
			a.selecting = false
			a.accounts.Busy = false
			if provider.CodeOf(err) == provider.NeedsSignIn {
				forgetErr := a.forgetAccount(credential.Account.ID)
				a.accounts.Error = provider.NeedsSignIn
				if forgetErr != nil {
					a.accounts.Error = provider.CodeOf(forgetErr)
				}
				a.changed()
				a.notifySettled()
				if forgetErr == nil {
					a.signInRequired()
				}
				return
			}
			if err == nil {
				err = a.activate(updated)
			}
			if err != nil {
				a.accounts.Error = provider.CodeOf(err)
			} else if onSuccess != nil {
				onSuccess()
			}
			a.changed()
			if err != nil {
				a.notifySettled()
			}
			if err == nil && updated.PendingConfirmation && !credential.PendingConfirmation {
				a.confirm(updated)
			}
		})
	}()
}

func (a *Auth) CancelAccountSelection() {
	if a.selecting {
		a.finishWork()
		a.selecting, a.accounts.Busy = false, false
		a.changed()
	}
}

// Confirm a rotation independently of foreground account actions so concurrent
// operations cannot silently cancel confirmation, then sync the live session.
func (a *Auth) confirm(credential provider.Credential) {
	a.finishConfirm()
	ctx, cancel := context.WithCancel(context.Background())
	work := &authWork{Context: ctx, cancel: cancel}
	a.confirmWork = work
	service := a.service
	go func() {
		updated, err := service.RestoreAccount(work, credential)
		a.post(func() {
			if a.confirmWork != work {
				return
			}
			a.finishConfirm()
			if err == nil {
				err = a.saveAccount(updated)
			}
			if err == nil && a.accounts.CurrentID == updated.Account.ID && a.service != nil {
				err = a.service.SetAccount(&updated)
			}
			if err != nil {
				a.accounts.Error = provider.CodeOf(err)
			}
			a.changed()
		})
	}()
}

func (a *Auth) SignOut(id string) {
	if !a.accounts.Ready || a.accounts.Busy {
		return
	}
	previous := a.find(id)
	if previous == nil {
		return
	}
	credential := *previous
	if err := a.forgetAccount(id); err != nil {
		a.accounts.Error = provider.CodeOf(err)
		a.changed()
		return
	}
	work, service := a.startWork(), a.service
	a.accounts.Busy, a.accounts.Error = true, ""
	a.changed()
	go func() {
		err := service.SignOut(work, credential)
		a.deliver(work, func() {
			a.finishWork()
			a.accounts.Busy = false
			if code := provider.CodeOf(err); err != nil && code != provider.NeedsSignIn {
				a.accounts.Error = code
			}
			a.changed()
		})
	}()
}

func (a *Auth) forgetAccount(id string) error {
	record := accountRecord{Selected: a.record.Selected, Accounts: slices.Clone(a.record.Accounts)}
	record.Accounts = slices.DeleteFunc(record.Accounts, func(item provider.Credential) bool { return item.Account.ID == id })
	if record.Selected == id {
		record.Selected = ""
	}
	if err := a.save(record); err != nil {
		return err
	}
	if a.cleaner != nil {
		_ = a.cleaner.ClearAccount(a.entry.ID, id)
	}
	if a.accounts.CurrentID == id {
		if err := a.service.SetAccount(nil); err != nil {
			return err
		}
		a.accounts.CurrentID = ""
		a.clearAvatar()
		a.accountChanged()
	}
	return nil
}

func (a *Auth) Close() {
	a.finishWork()
	a.finishConfirm()
	a.clearAvatar()
	a.selecting = false
	a.service, a.entry = nil, Entry{}
	a.record, a.accounts, a.signIn = accountRecord{}, AccountsSnapshot{}, SignInSnapshot{}
}

func (a *Auth) finishConfirm() {
	if a.confirmWork != nil {
		a.confirmWork.cancel()
		a.confirmWork = nil
	}
}

func (a *Auth) find(id string) *provider.Credential {
	index := slices.IndexFunc(a.record.Accounts, func(item provider.Credential) bool { return item.Account.ID == id })
	if index < 0 {
		return nil
	}
	return &a.record.Accounts[index]
}

func (a *Auth) activate(credential provider.Credential) error {
	if err := a.saveAccount(credential); err != nil {
		return err
	}
	if err := a.service.SetAccount(&credential); err != nil {
		return err
	}
	a.accounts.CurrentID = credential.Account.ID
	a.accounts.Error = ""
	a.loadAvatar(credential.Account)
	a.accountChanged()
	return nil
}

func (a *Auth) loadAvatar(account provider.Account) {
	if load := a.avatarLoad; load != nil && load.accountID == account.ID && load.url == account.AvatarURL {
		return
	}
	a.clearAvatar()
	loader, ok := a.service.(provider.AccountAvatarLoader)
	if !ok || account.AvatarURL == "" {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	load := &avatarLoad{accountID: account.ID, url: account.AvatarURL, cancel: cancel}
	a.avatarLoad = load
	go func() {
		avatar, err := loader.LoadAccountAvatar(ctx, load.url)
		a.post(func() {
			if a.avatarLoad != load {
				return
			}
			load.cancel()
			if err != nil || avatar == nil {
				a.avatarLoad = nil
				return
			}
			a.avatar = &AccountAvatar{Image: avatar}
			a.changed()
		})
	}()
}

func (a *Auth) clearAvatar() {
	if a.avatarLoad != nil {
		a.avatarLoad.cancel()
	}
	a.avatarLoad, a.avatar = nil, nil
}

func (a *Auth) saveAccount(credential provider.Credential) error {
	record := accountRecord{Selected: credential.Account.ID, Accounts: slices.Clone(a.record.Accounts)}
	index := slices.IndexFunc(record.Accounts, func(item provider.Credential) bool { return item.Account.ID == credential.Account.ID })
	if index < 0 {
		record.Accounts = append(record.Accounts, credential)
	} else {
		record.Accounts[index] = credential
	}
	return a.save(record)
}

func (a *Auth) save(record accountRecord) error {
	var data []byte
	if len(record.Accounts) != 0 {
		var err error
		data, err = json.Marshal(record)
		if err != nil {
			return &provider.Error{Code: provider.PluginFailed, Cause: err}
		}
	}
	if err := a.store.Save(a.entry.SourceID, a.entry.ID, data); err != nil {
		return &provider.Error{Code: provider.PluginFailed, Cause: err}
	}
	a.record = record
	return nil
}

func (a *Auth) startWork() *authWork {
	a.finishWork()
	ctx, cancel := context.WithCancel(context.Background())
	a.work = &authWork{Context: ctx, cancel: cancel}
	return a.work
}

func (a *Auth) finishWork() {
	if a.work != nil {
		a.work.cancel()
		a.work = nil
	}
}

func (a *Auth) deliver(work *authWork, update func()) {
	a.post(func() {
		if a.work == work {
			update()
		}
	})
}
