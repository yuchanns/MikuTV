package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/draw"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/yuchanns/MikuTV/internal/provider"
	"golang.org/x/net/publicsuffix"
)

const (
	beginSignIn           = "beginSignIn"
	pollSignIn            = "pollSignIn"
	signInWithCredentials = "signInWithCredentials"
	restoreAccount        = "restoreAccount"
	signOut               = "signOut"
	maxAvatarBytes        = 512 << 10
	maxAvatarSide         = 1024
	maxArtworkRedirects   = 5
	artworkUserAgent      = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/134.0.0.0 Safari/537.36"
)

var artworkTrace = os.Getenv("MIKUTV_NAV_TRACE") == "1"

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
	Secret       json.RawMessage `json:"secret"`
	Cookies      []storedCookie  `json:"cookies,omitempty"`
	ServerOrigin string          `json:"serverOrigin,omitempty"`
}

// NormalizeServerOrigin validates a user-entered self-hosted server address
// and returns its exact origin (scheme://host[:port]) plus cleaned base URL.
func NormalizeServerOrigin(raw string) (*url.URL, string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || len(trimmed) > 512 {
		return nil, "", errors.New("server address required")
	}
	candidate := trimmed
	if !strings.Contains(candidate, "://") {
		if strings.Contains(candidate, ":") && !strings.HasSuffix(candidate, ":443") {
			candidate = "http://" + candidate
		} else {
			candidate = "https://" + candidate
		}
	}
	u, err := url.Parse(candidate)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" ||
		u.User != nil || u.Opaque != "" || u.RawQuery != "" || u.Fragment != "" {
		return nil, "", errors.New("invalid server URL")
	}
	origin := &url.URL{Scheme: strings.ToLower(u.Scheme), Host: u.Host}
	baseURL := strings.TrimRight(origin.String()+u.Path, "/")
	return origin, baseURL, nil
}

type qrAttempt struct {
	session   *Session
	client    *http.Client
	challenge provider.QRChallenge
	key       string
}

func (s *Session) BeginSignIn(ctx context.Context) (provider.QRAttempt, error) {
	if !s.source.HasAuthMethod(provider.AuthMethodQRCode) {
		return nil, &provider.Error{Code: provider.Unsupported}
	}
	client, err := s.credentialClient(nil, true, nil)
	if err != nil {
		return nil, err
	}
	var result qrResult
	if err := s.invokeWithOrigin(ctx, s.context, client, nil, provider.SignIn, beginSignIn, struct{}{}, &result); err != nil {
		return nil, err
	}
	expires := time.UnixMilli(result.ExpiresAt)
	if httpsURL(result.URL) == nil || result.Key == "" || !expires.After(time.Now()) {
		return nil, &provider.Error{Code: provider.PluginFailed}
	}
	return &qrAttempt{session: s, client: client, challenge: provider.QRChallenge{URL: result.URL, ExpiresAt: expires}, key: result.Key}, nil
}

func (s *Session) SignInWithCredentials(ctx context.Context, req provider.CredentialsRequest) (provider.Credential, error) {
	if !s.source.HasAuthMethod(provider.AuthMethodCredentials) {
		return provider.Credential{}, &provider.Error{Code: provider.Unsupported}
	}
	var userOrigin *url.URL
	serverURL := ""
	if s.source.AllowsUserServerOrigin() {
		rawServer := strings.TrimSpace(req.ServerURL)
		if rawServer == "" {
			if s.source.ServerURLMode() == "required" {
				return provider.Credential{}, &provider.Error{Code: provider.Denied}
			}
		} else {
			var err error
			userOrigin, serverURL, err = NormalizeServerOrigin(rawServer)
			if err != nil {
				return provider.Credential{}, &provider.Error{Code: provider.Denied, Cause: err}
			}
		}
	}
	client, err := s.credentialClient(nil, userOrigin == nil, userOrigin)
	if err != nil {
		return provider.Credential{}, err
	}
	params := provider.CredentialsRequest{
		ServerURL: serverURL,
		Username:  strings.TrimSpace(req.Username),
		Password:  req.Password,
	}
	var result authResult
	if err := s.invokeWithOrigin(ctx, s.context, client, userOrigin, provider.SignIn, signInWithCredentials, params, &result); err != nil {
		return provider.Credential{}, err
	}
	if result.Status != "" && result.Status != provider.QRApproved {
		return provider.Credential{}, &provider.Error{Code: provider.Denied}
	}
	return s.credential(result, nil, userOrigin)
}

func (a *qrAttempt) Challenge() provider.QRChallenge { return a.challenge }

func (a *qrAttempt) Poll(ctx context.Context) (provider.QRProgress, error) {
	var result authResult
	params := struct {
		Key string `json:"key"`
	}{a.key}
	s := a.session
	if err := s.invokeWithOrigin(ctx, s.context, a.client, nil, provider.SignIn, pollSignIn, params, &result); err != nil {
		return provider.QRProgress{}, err
	}
	switch result.Status {
	case provider.QRWaiting, provider.QRScanned, provider.QRExpired:
		return provider.QRProgress{Status: result.Status}, nil
	case provider.QRApproved:
		credential, err := s.credential(result, nil, nil)
		return provider.QRProgress{Status: provider.QRApproved, Credential: credential}, err
	default:
		return provider.QRProgress{}, &provider.Error{Code: provider.PluginFailed}
	}
}

func (s *Session) RestoreAccount(ctx context.Context, previous provider.Credential) (provider.Credential, error) {
	stored, userOrigin, client, err := s.useCredential(previous)
	if err != nil {
		return provider.Credential{}, err
	}
	var result authResult
	if err := s.invokeWithOrigin(ctx, s.context, client, userOrigin, provider.SignIn, restoreAccount, stored.Secret, &result); err != nil {
		return provider.Credential{}, err
	}
	if result.Account.ID != previous.Account.ID {
		return provider.Credential{}, &provider.Error{Code: provider.Denied}
	}
	return s.credential(result, stored.Cookies, userOrigin)
}

func (s *Session) SignOut(ctx context.Context, previous provider.Credential) error {
	stored, userOrigin, client, err := s.useCredential(previous)
	if err != nil {
		return err
	}
	var result struct{}
	return s.invokeWithOrigin(ctx, s.context, client, userOrigin, provider.SignIn, signOut, stored.Secret, &result)
}

func (s *Session) SetAccount(credential *provider.Credential) error {
	var client *http.Client
	var secret json.RawMessage
	var userOrigin *url.URL
	var err error
	if credential == nil {
		client, err = s.credentialClient(nil, false, nil)
	} else {
		var stored storedCredential
		stored, userOrigin, client, err = s.useCredential(*credential)
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
	nextAccountID := ""
	if credential != nil {
		nextAccountID = credential.Account.ID
	}
	sameAccount := nextAccountID != "" && nextAccountID == s.accountID
	if !sameAccount {
		s.stop()
		s.scope, s.stop = context.WithCancel(s.context)
	}
	s.active, s.secret, s.userOrigin = client, secret, userOrigin
	s.accountID = nextAccountID
	return nil
}

func (s *Session) credential(result authResult, cookies []storedCookie, userOrigin *url.URL) (provider.Credential, error) {
	if result.Account.ID == "" || strings.TrimSpace(result.Account.Name) == "" || len(result.Secret) == 0 {
		return provider.Credential{}, &provider.Error{Code: provider.PluginFailed}
	}
	if result.Cookies != nil {
		cookies = nil
		for _, item := range result.Cookies {
			origin := s.cookieOrigin(item.URL, userOrigin)
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
	stored := storedCredential{Secret: result.Secret, Cookies: cookies}
	if userOrigin != nil && s.source.AllowsUserServerOrigin() {
		stored.ServerOrigin = userOrigin.String()
	}
	data, err := json.Marshal(stored)
	if err != nil {
		return provider.Credential{}, &provider.Error{Code: provider.PluginFailed}
	}
	return provider.Credential{Account: result.Account, Data: data, PendingConfirmation: result.PendingConfirmation}, nil
}

func (s *Session) useCredential(credential provider.Credential) (storedCredential, *url.URL, *http.Client, error) {
	var stored storedCredential
	if err := decodeJSON(credential.Data, &stored); err != nil || len(stored.Secret) == 0 {
		return storedCredential{}, nil, nil, &provider.Error{Code: provider.PluginFailed}
	}
	var userOrigin *url.URL
	if stored.ServerOrigin != "" && s.source.AllowsUserServerOrigin() {
		origin, _, err := NormalizeServerOrigin(stored.ServerOrigin)
		if err != nil {
			return storedCredential{}, nil, nil, &provider.Error{Code: provider.Denied, Cause: err}
		}
		userOrigin = origin
	}
	client, err := s.credentialClient(stored.Cookies, userOrigin == nil, userOrigin)
	return stored, userOrigin, client, err
}

func (s *Session) credentialClient(cookies []storedCookie, secure bool, userOrigin *url.URL) (*http.Client, error) {
	if s.client == nil {
		if len(cookies) != 0 {
			return nil, &provider.Error{Code: provider.Denied}
		}
		return nil, nil
	}
	jar, _ := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	for _, cookie := range cookies {
		origin := s.cookieOrigin(cookie.URL, userOrigin)
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

func (s *Session) artworkAuthHost(u *url.URL) bool {
	if s == nil || s.source == nil || u == nil || u.Scheme != "https" {
		return false
	}
	hosts := s.source.manifest.Permissions.Artwork.Hosts
	if len(hosts) == 0 {
		return false
	}
	return slices.Contains(hosts, strings.ToLower(u.Hostname()))
}

func (s *Session) loadArtwork(ctx context.Context, location string, maxBytes int64, maxSide int) (image.Image, error) {
	target := s.allowedURL(location)
	if target == nil || s.client == nil {
		return nil, &provider.Error{Code: provider.Denied}
	}
	allow := s.allowRequestForSession(true)
	checkReq, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, err
	}
	if err := allow(checkReq); err != nil {
		return nil, &provider.Error{Code: provider.Denied, Cause: err}
	}

	noRedirect := func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	publicClient := *s.client
	publicClient.Jar = nil
	publicClient.CheckRedirect = noRedirect

	// Public artwork never receives the account's cookies or follows redirects.
	// Only hosts explicitly declared in permissions.artwork.hosts for a signed-in
	// source may receive the active session's HTTPS cookie jar, and cross-origin
	// redirects immediately drop cookies while still enforcing network permissions.
	useAuthFlow := s.artworkAuthHost(target)
	var authClient *http.Client
	referer := ""
	if useAuthFlow {
		auth := s.snapshotAuth()
		if auth.signedIn() && auth.client != nil && auth.client.Jar != nil {
			cloned := *auth.client
			cloned.CheckRedirect = noRedirect
			authClient = &cloned
		}
		referer = s.source.manifest.Permissions.Artwork.Referer
	}

	cur := target
	sameOrigin := true
	var response *http.Response
	maxHops := 0
	if useAuthFlow {
		maxHops = maxArtworkRedirects
	}
	for hop := 0; hop <= maxHops; hop++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, cur.String(), nil)
		if err != nil {
			return nil, err
		}
		if err := allow(req); err != nil {
			return nil, &provider.Error{Code: provider.Denied, Cause: err}
		}
		client := &publicClient
		if useAuthFlow {
			req.Header.Set("Accept", "image/*")
			req.Header.Set("User-Agent", artworkUserAgent)
			if referer != "" {
				req.Header.Set("Referer", referer)
			}
			if sameOrigin && authClient != nil && s.artworkAuthHost(cur) {
				client = authClient
			}
		}
		resp, err := client.Do(req)
		if err != nil {
			if artworkTrace && !errors.Is(err, context.Canceled) {
				log.Printf("artwork: host=%s hop=%d failed", cur.Hostname(), hop)
			}
			return nil, err
		}
		if useAuthFlow && (resp.StatusCode == http.StatusMovedPermanently ||
			resp.StatusCode == http.StatusFound ||
			resp.StatusCode == http.StatusSeeOther ||
			resp.StatusCode == http.StatusTemporaryRedirect ||
			resp.StatusCode == http.StatusPermanentRedirect) {
			loc := resp.Header.Get("Location")
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
			resp.Body.Close()
			if loc == "" || hop == maxHops {
				if artworkTrace {
					log.Printf("artwork: host=%s hop=%d redirect rejected status=%d", cur.Hostname(), hop, resp.StatusCode)
				}
				return nil, errors.New("artwork redirect limit exceeded")
			}
			resolved, err := cur.Parse(loc)
			if err != nil {
				return nil, errors.New("invalid artwork redirect")
			}
			next := s.allowedURL(resolved.String())
			if next == nil {
				if artworkTrace {
					log.Printf("artwork: host=%s redirect denied nextHost=%s", cur.Hostname(), resolved.Hostname())
				}
				return nil, &provider.Error{Code: provider.Denied}
			}
			if !sameHTTPOrigin(next, target) {
				sameOrigin = false
			}
			cur = next
			continue
		}
		response = resp
		break
	}
	if response == nil {
		return nil, errors.New("artwork request failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		if artworkTrace {
			log.Printf("artwork: host=%s status=%d", cur.Hostname(), response.StatusCode)
		}
		return nil, errors.New("artwork request failed")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		if artworkTrace {
			log.Printf("artwork: host=%s oversized bytes>%d", cur.Hostname(), maxBytes)
		}
		return nil, errors.New("artwork exceeds size limit")
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width <= 0 || config.Height <= 0 || config.Width > maxSide || config.Height > maxSide {
		if artworkTrace {
			log.Printf("artwork: host=%s invalid format=%q dims=%dx%d maxSide=%d err=%v",
				cur.Hostname(), format, config.Width, config.Height, maxSide, err)
		}
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

func (s *Session) cookieOrigin(value string, userOrigin *url.URL) *url.URL {
	u := allowedMediaURL(value, s.source.AllowsUserServerOrigin(), userOrigin)
	if u == nil || s.source.allowRequestWithOrigin(true, userOrigin)(&http.Request{URL: u}) != nil {
		return nil
	}
	u.RawQuery, u.Fragment, u.RawFragment, u.ForceQuery = "", "", "", false
	return u
}

func allowedMediaURL(value string, allowUserOrigin bool, userOrigin *url.URL) *url.URL {
	if u := httpsURL(value); u != nil {
		return u
	}
	if !allowUserOrigin || userOrigin == nil {
		return nil
	}
	u, err := url.Parse(value)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.Opaque != "" {
		return nil
	}
	if !sameHTTPOrigin(u, userOrigin) {
		return nil
	}
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
