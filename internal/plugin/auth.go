package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/draw"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"

	"github.com/yuchanns/MikuTV/internal/provider"
	"golang.org/x/net/publicsuffix"
)

const (
	beginSignIn    = "beginSignIn"
	pollSignIn     = "pollSignIn"
	restoreAccount = "restoreAccount"
	signOut        = "signOut"
	maxAvatarBytes = 512 << 10
	maxAvatarSide  = 1024
)

type qrResult struct {
	URL       string `json:"url"`
	Key       string `json:"key"`
	ExpiresAt int64  `json:"expiresAt"`
}

type authResult struct {
	Status              provider.QRStatus `json:"status"`
	Account             provider.Account  `json:"account"`
	Secret              json.RawMessage   `json:"secret"`
	PendingConfirmation bool              `json:"pendingConfirmation"`
	Cookies             []struct {
		URL     string   `json:"url"`
		Headers []string `json:"headers"`
	} `json:"cookies"`
}

type storedCookie struct {
	URL    string       `json:"url"`
	Cookie *http.Cookie `json:"cookie"`
}

type storedCredential struct {
	Secret  json.RawMessage `json:"secret"`
	Cookies []storedCookie  `json:"cookies,omitempty"`
}

type qrAttempt struct {
	session   *Session
	client    *http.Client
	challenge provider.QRChallenge
	key       string
}

func (s *Session) BeginSignIn(ctx context.Context) (provider.QRAttempt, error) {
	client, err := s.credentialClient(nil, true)
	if err != nil {
		return nil, err
	}
	var result qrResult
	if err := s.invoke(ctx, s.context, client, provider.SignIn, beginSignIn, struct{}{}, &result); err != nil {
		return nil, err
	}
	expires := time.UnixMilli(result.ExpiresAt)
	if httpsURL(result.URL) == nil || result.Key == "" || !expires.After(time.Now()) {
		return nil, &provider.Error{Code: provider.PluginFailed}
	}
	return &qrAttempt{session: s, client: client, challenge: provider.QRChallenge{URL: result.URL, ExpiresAt: expires}, key: result.Key}, nil
}

func (a *qrAttempt) Challenge() provider.QRChallenge { return a.challenge }

func (a *qrAttempt) Poll(ctx context.Context) (provider.QRProgress, error) {
	var result authResult
	params := struct {
		Key string `json:"key"`
	}{a.key}
	s := a.session
	if err := s.invoke(ctx, s.context, a.client, provider.SignIn, pollSignIn, params, &result); err != nil {
		return provider.QRProgress{}, err
	}
	switch result.Status {
	case provider.QRWaiting, provider.QRScanned, provider.QRExpired:
		return provider.QRProgress{Status: result.Status}, nil
	case provider.QRApproved:
		credential, err := s.credential(result, nil)
		return provider.QRProgress{Status: provider.QRApproved, Credential: credential}, err
	default:
		return provider.QRProgress{}, &provider.Error{Code: provider.PluginFailed}
	}
}

func (s *Session) RestoreAccount(ctx context.Context, previous provider.Credential) (provider.Credential, error) {
	stored, client, err := s.useCredential(previous)
	if err != nil {
		return provider.Credential{}, err
	}
	var result authResult
	if err := s.invoke(ctx, s.context, client, provider.SignIn, restoreAccount, stored.Secret, &result); err != nil {
		return provider.Credential{}, err
	}
	if result.Account.ID != previous.Account.ID {
		return provider.Credential{}, &provider.Error{Code: provider.Denied}
	}
	return s.credential(result, stored.Cookies)
}

func (s *Session) SignOut(ctx context.Context, previous provider.Credential) error {
	stored, client, err := s.useCredential(previous)
	if err != nil {
		return err
	}
	var result struct{}
	return s.invoke(ctx, s.context, client, provider.SignIn, signOut, stored.Secret, &result)
}

func (s *Session) SetAccount(credential *provider.Credential) error {
	var client *http.Client
	var secret json.RawMessage
	var err error
	if credential == nil {
		client, err = s.credentialClient(nil, false)
	} else {
		var stored storedCredential
		stored, client, err = s.useCredential(*credential)
		secret = stored.Secret
	}
	if err != nil {
		return err
	}
	s.requests.Lock()
	defer s.requests.Unlock()
	if err := s.context.Err(); err != nil {
		return err
	}
	s.stop()
	s.active, s.secret = client, secret
	s.accountID = ""
	if credential != nil {
		s.accountID = credential.Account.ID
	}
	s.scope, s.stop = context.WithCancel(s.context)
	return nil
}

func (s *Session) credential(result authResult, cookies []storedCookie) (provider.Credential, error) {
	if result.Account.ID == "" || strings.TrimSpace(result.Account.Name) == "" || len(result.Secret) == 0 {
		return provider.Credential{}, &provider.Error{Code: provider.PluginFailed}
	}
	if result.Cookies != nil {
		cookies = nil
		for _, item := range result.Cookies {
			origin := s.cookieOrigin(item.URL)
			if origin == nil {
				return provider.Credential{}, &provider.Error{Code: provider.Denied}
			}
			for _, header := range item.Headers {
				cookie, err := http.ParseSetCookie(header)
				if err != nil || cookie.Valid() != nil {
					return provider.Credential{}, &provider.Error{Code: provider.PluginFailed}
				}
				cookie.Raw, cookie.RawExpires, cookie.Unparsed = "", "", nil
				if cookie.MaxAge > 0 {
					now := time.Now().Unix()
					latest := time.Date(9999, time.December, 31, 23, 59, 59, 0, time.UTC).Unix()
					cookie.Expires = time.Unix(now+min(int64(cookie.MaxAge), latest-now), 0).UTC()
					cookie.MaxAge = 0
				}
				cookies = append(cookies, storedCookie{URL: origin.String(), Cookie: cookie})
			}
		}
	}
	data, err := json.Marshal(storedCredential{Secret: result.Secret, Cookies: cookies})
	if err != nil {
		return provider.Credential{}, &provider.Error{Code: provider.PluginFailed}
	}
	return provider.Credential{Account: result.Account, Data: data, PendingConfirmation: result.PendingConfirmation}, nil
}

func (s *Session) useCredential(credential provider.Credential) (storedCredential, *http.Client, error) {
	var stored storedCredential
	if err := decodeJSON(credential.Data, &stored); err != nil || len(stored.Secret) == 0 {
		return storedCredential{}, nil, &provider.Error{Code: provider.PluginFailed}
	}
	client, err := s.credentialClient(stored.Cookies, true)
	return stored, client, err
}

func (s *Session) credentialClient(cookies []storedCookie, secure bool) (*http.Client, error) {
	if s.client == nil {
		if len(cookies) != 0 {
			return nil, &provider.Error{Code: provider.Denied}
		}
		return nil, nil
	}
	jar, _ := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	for _, cookie := range cookies {
		origin := s.cookieOrigin(cookie.URL)
		if origin == nil || cookie.Cookie == nil || cookie.Cookie.Valid() != nil {
			return nil, &provider.Error{Code: provider.Denied}
		}
		jar.SetCookies(origin, []*http.Cookie{cookie.Cookie})
	}
	client := *s.client
	client.Jar = jar
	if secure {
		client.Jar = httpsCookies{jar}
	}
	return &client, nil
}

func httpsURL(value string) *url.URL {
	u, err := url.Parse(value)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Opaque != "" {
		return nil
	}
	return u
}

func (s *Session) LoadAccountAvatar(ctx context.Context, location string) (image.Image, error) {
	return s.loadArtwork(ctx, location, maxAvatarBytes, maxAvatarSide)
}

func (s *Session) LoadCatalogArtwork(ctx context.Context, location string) (image.Image, error) {
	return s.loadArtwork(ctx, location, 1<<20, 1024)
}

func (s *Session) LoadCatalogBackdrop(ctx context.Context, location string) (image.Image, error) {
	return s.loadArtwork(ctx, location, 2<<20, 2048)
}

func (s *Session) loadArtwork(ctx context.Context, location string, maxBytes int64, maxSide int) (image.Image, error) {
	target := httpsURL(location)
	if target == nil || s.client == nil {
		return nil, &provider.Error{Code: provider.Denied}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, err
	}
	if err := s.source.allowRequest(true)(request); err != nil {
		return nil, &provider.Error{Code: provider.Denied, Cause: err}
	}

	// Public artwork never receives the account's cookies.
	client := *s.client
	client.Jar = nil
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, errors.New("artwork request failed")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, errors.New("artwork exceeds size limit")
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width <= 0 || config.Height <= 0 || config.Width > maxSide || config.Height > maxSide {
		return nil, errors.New("invalid artwork dimensions or format")
	}
	artwork, _, err := image.Decode(bytes.NewReader(data))
	if err != nil || artwork == nil {
		return nil, err
	}
	return toTightRGBA(artwork), nil
}

// toTightRGBA converts decoded images (such as JPEG YCbCr) on the worker goroutine
// so the UI thread uploads tight RGBA pixels without blocking or doubling memory.
func toTightRGBA(src image.Image) *image.RGBA {
	bounds := src.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if rgba, ok := src.(*image.RGBA); ok && bounds.Min.X == 0 && bounds.Min.Y == 0 && rgba.Stride == w*4 && len(rgba.Pix) >= w*h*4 {
		return rgba
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(dst, dst.Bounds(), src, bounds.Min, draw.Src)
	return dst
}

func (s *Session) cookieOrigin(value string) *url.URL {
	u := httpsURL(value)
	if u == nil || s.source.allowRequest(true)(&http.Request{URL: u}) != nil {
		return nil
	}
	u.RawQuery, u.Fragment, u.RawFragment, u.ForceQuery = "", "", "", false
	return u
}

type httpsCookies struct{ http.CookieJar }

func (j httpsCookies) Cookies(u *url.URL) []*http.Cookie {
	if u.Scheme != "https" {
		return nil
	}
	return j.CookieJar.Cookies(u)
}

func (j httpsCookies) SetCookies(u *url.URL, cookies []*http.Cookie) {
	if u.Scheme == "https" {
		j.CookieJar.SetCookies(u, cookies)
	}
}
