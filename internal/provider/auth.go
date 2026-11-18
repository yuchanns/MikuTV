package provider

import (
	"context"
	"encoding/json"
	"image"
	"time"
)

type AuthPresentation struct {
	AccountSubtitle  string `json:"accountSubtitle,omitempty"`
	GuestTitle       string `json:"guestTitle,omitempty"`
	GuestDescription string `json:"guestDescription,omitempty"`
	GuestArtwork     string `json:"guestArtwork,omitempty"`
	QRTitle          string `json:"qrTitle,omitempty"`
	QRDescription    string `json:"qrDescription,omitempty"`
}

type Account struct {
	ID        string       `json:"id"`
	Name      string       `json:"name"`
	AvatarURL string       `json:"avatarURL,omitempty"`
	Badge     AccountBadge `json:"badge,omitzero"`
	Card      AccountCard  `json:"card,omitzero"`
}

type AccountBadge struct {
	Text string `json:"text,omitempty"`
	Icon string `json:"icon,omitempty"`
}

type AccountCard struct {
	Title       string              `json:"title,omitempty"`
	Description string              `json:"description,omitempty"`
	Icon        string              `json:"icon,omitempty"`
	Benefits    []string            `json:"benefits,omitempty"`
	ActionLabel string              `json:"actionLabel,omitempty"`
	Status      string              `json:"status,omitempty"`
	Highlight   bool                `json:"highlight,omitempty"`
	Details     *AccountCardDetails `json:"details,omitempty"`
}

type AccountCardDetails struct {
	Title         string `json:"title"`
	Kicker        string `json:"kicker"`
	Body          string `json:"body"`
	PhoneAction   string `json:"phoneAction"`
	PhoneBody     string `json:"phoneBody"`
	OfficialLabel string `json:"officialLabel"`
	RefreshAction string `json:"refreshAction"`
	DismissAction string `json:"dismissAction"`
}

type AccountAvatarLoader interface {
	LoadAccountAvatar(context.Context, string) (image.Image, error)
}

// Credential belongs to one account and is kept outside of page state.
type Credential struct {
	Account             Account         `json:"account"`
	Data                json.RawMessage `json:"data"`
	PendingConfirmation bool            `json:"pendingConfirmation,omitempty"`
}

type QRChallenge struct {
	URL       string
	ExpiresAt time.Time
}

type QRStatus string

const (
	QRWaiting  QRStatus = "waiting"
	QRScanned  QRStatus = "scanned"
	QRExpired  QRStatus = "expired"
	QRApproved QRStatus = "approved"
)

type QRProgress struct {
	Status     QRStatus
	Credential Credential
}

type QRAttempt interface {
	Challenge() QRChallenge
	Poll(context.Context) (QRProgress, error)
}

type Authenticator interface {
	BeginSignIn(context.Context) (QRAttempt, error)
	RestoreAccount(context.Context, Credential) (Credential, error)
	SetAccount(*Credential) error
	SignOut(context.Context, Credential) error
}
