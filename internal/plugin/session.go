package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/MoeForge/moejs"
	"github.com/MoeForge/moejs/eventloop"
	"github.com/MoeForge/moejs/webapi"
	"github.com/yuchanns/MikuTV/internal/provider"
	"golang.org/x/net/publicsuffix"
)

const (
	callTimeout = 12 * time.Second
	heapLimit   = 32 << 20
)

type Session struct {
	source  *Source
	entryID string
	storage *Storage
	client  *http.Client
	cdn     *cdnTracker
	context context.Context
	cancel  context.CancelFunc

	requests   sync.RWMutex
	active     *http.Client
	accountID  string
	secret     json.RawMessage
	userOrigin *url.URL
	scope      context.Context
	stop       context.CancelFunc
}

func (s *Session) activeUserOrigin() *url.URL {
	s.requests.RLock()
	origin := s.userOrigin
	s.requests.RUnlock()
	return origin
}

func (s *Session) allowRequestForSession(secure bool) func(*http.Request) error {
	return s.source.allowRequestWithOrigin(secure, s.activeUserOrigin())
}

func (s *Session) allowedURL(raw string) *url.URL {
	return allowedMediaURL(raw, s.source.AllowsUserServerOrigin(), s.activeUserOrigin())
}

// NewSession gives an entry its own cookies, storage and cancellation scope.
func (s *Source) NewSession(entryID string, storage *Storage) *Session {
	ctx, cancel := context.WithCancel(context.Background())
	scope, stop := context.WithCancel(ctx)
	client := s.httpClient()
	return &Session{
		source: s, entryID: entryID, storage: storage, client: client, cdn: newCDNTracker(),
		context: ctx, cancel: cancel, active: client, scope: scope, stop: stop,
	}
}

func (s *Session) Close() {
	s.cancel()
	if s.client != nil {
		s.client.CloseIdleConnections()
	}
}

func (s *Source) Verify(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	storage, _ := NewStorage(nil, nil)
	_, err := s.execute(ctx, nil, storage, "", nil, false)
	return err
}

func (s *Session) Browse(ctx context.Context, request provider.BrowseRequest) (provider.CollectionPage, error) {
	var page provider.CollectionPage
	s.requests.RLock()
	client, scope, signedIn, secret := s.active, s.scope, s.accountID != "", s.secret
	s.requests.RUnlock()
	input := struct {
		provider.BrowseRequest
		SignedIn bool            `json:"signedIn,omitempty"`
		Secret   json.RawMessage `json:"secret,omitempty"`
	}{request, signedIn, secret}
	if err := s.invoke(ctx, scope, client, provider.BrowseCatalog, "browse", input, &page); err != nil {
		return page, err
	}
	for _, group := range slices.Concat(page.Collections, page.Shelves) {
		if group.ID == "" || strings.TrimSpace(group.Title) == "" {
			return provider.CollectionPage{}, &provider.Error{Code: provider.PluginFailed}
		}
		for index := range group.Items {
			if err := s.ownItem(&group.Items[index]); err != nil {
				return provider.CollectionPage{}, err
			}
		}
	}
	for _, filter := range page.Filters {
		if filter.ID == "" || strings.TrimSpace(filter.Title) == "" {
			return provider.CollectionPage{}, &provider.Error{Code: provider.PluginFailed}
		}
	}
	return page, nil
}

func (s *Session) Item(ctx context.Context, ref provider.MediaRef) (provider.MediaItem, error) {
	return s.item(ctx, ref, false)
}

func (s *Session) VideoDetail(ctx context.Context, ref provider.MediaRef) (provider.MediaItem, error) {
	return s.item(ctx, ref, true)
}

func (s *Session) item(ctx context.Context, ref provider.MediaRef, includeSections bool) (provider.MediaItem, error) {
	var item provider.MediaItem
	if ref.EntryID != s.entryID || !ref.Kind.Valid() || ref.ID == "" {
		return item, &provider.Error{Code: provider.Denied}
	}
	input := struct {
		provider.MediaRef
		IncludeSections bool            `json:"includeSections,omitempty"`
		SignedIn        bool            `json:"signedIn,omitempty"`
		Secret          json.RawMessage `json:"secret,omitempty"`
	}{MediaRef: ref, IncludeSections: includeSections}
	s.requests.RLock()
	client, scope, signedIn, secret := s.active, s.scope, s.accountID != "", s.secret
	s.requests.RUnlock()
	input.SignedIn = signedIn
	input.Secret = secret
	if err := s.invoke(ctx, scope, client, provider.GetCatalogItem, "getItem", input, &item); err != nil {
		return item, err
	}
	if item.Ref.Kind != ref.Kind || item.Ref.ID != ref.ID {
		return provider.MediaItem{}, &provider.Error{Code: provider.PluginFailed}
	}
	if err := s.ownItem(&item); err != nil {
		return provider.MediaItem{}, err
	}
	return item, nil
}

func (s *Session) ownItem(item *provider.MediaItem) error {
	if !item.Ref.Kind.Valid() || item.Ref.ID == "" || strings.TrimSpace(item.Title) == "" {
		return &provider.Error{Code: provider.PluginFailed}
	}
	if item.Creator != nil && (item.Creator.ID == "" || strings.TrimSpace(item.Creator.Name) == "") {
		return &provider.Error{Code: provider.PluginFailed}
	}
	if item.DurationSeconds != nil && *item.DurationSeconds < 0 ||
		item.Views != nil && *item.Views < 0 || item.ProgressSeconds != nil && *item.ProgressSeconds < 0 {
		return &provider.Error{Code: provider.PluginFailed}
	}
	if detail := item.VideoDetails; detail != nil {
		if detail.PublishedAt < 0 || detail.Comments != nil && *detail.Comments < 0 {
			return &provider.Error{Code: provider.PluginFailed}
		}
		switch detail.Access {
		case "", provider.VideoAccessGranted, provider.VideoAccessPreview, provider.VideoAccessRestricted:
		default:
			return &provider.Error{Code: provider.PluginFailed}
		}
		if err := s.ownVideoSections(detail); err != nil {
			return err
		}
	}
	if item.CoverURL != "" && s.allowedURL(item.CoverURL) == nil {
		item.CoverURL = ""
	}
	if item.BackdropURL != "" && s.allowedURL(item.BackdropURL) == nil {
		item.BackdropURL = ""
	}
	item.Ref.EntryID = s.entryID
	return nil
}

func (s *Session) ownVideoSections(detail *provider.VideoDetails) error {
	switch detail.SegmentKind {
	case "", provider.VideoChapters, provider.VideoParts, provider.VideoCollection:
	default:
		return &provider.Error{Code: provider.PluginFailed}
	}
	for _, segment := range detail.Segments {
		if strings.TrimSpace(segment.Title) == "" || segment.StartSeconds < 0 || len(segment.Status) > 128 ||
			segment.DurationSeconds != nil && *segment.DurationSeconds < 0 {
			return &provider.Error{Code: provider.PluginFailed}
		}
		if segment.Ref != nil {
			if !segment.Ref.Kind.Valid() || segment.Ref.ID == "" {
				return &provider.Error{Code: provider.PluginFailed}
			}
			segment.Ref.EntryID = s.entryID
		}
	}
	if err := ownVideoComments(detail.Discussion, true); err != nil {
		return err
	}
	if err := ownVideoActions(detail.Actions); err != nil {
		return err
	}
	for index := range detail.Related {
		item := &detail.Related[index]
		if item.VideoDetails != nil {
			return &provider.Error{Code: provider.PluginFailed}
		}
		if err := s.ownItem(item); err != nil {
			return err
		}
	}
	return nil
}

func (s *Session) ReadAccount(ctx context.Context, request provider.AccountRequest) (provider.AccountPage, error) {
	if len(request.Folder) > 512 {
		return provider.AccountPage{}, &provider.Error{Code: provider.Denied}
	}
	s.requests.RLock()
	client, scope, accountID, secret := s.active, s.scope, s.accountID, s.secret
	s.requests.RUnlock()
	if accountID == "" {
		return provider.AccountPage{}, &provider.Error{Code: provider.NeedsSignIn}
	}
	input := struct {
		provider.AccountRequest
		Owner  string          `json:"owner"`
		Secret json.RawMessage `json:"secret,omitempty"`
	}{request, accountID, secret}
	var page provider.AccountPage
	if err := s.invoke(ctx, scope, client, request.Section.Capability(), "readAccount", input, &page); err != nil {
		return provider.AccountPage{}, err
	}
	if len(page.NextCursor) > 4096 || page.Total != nil && *page.Total < 0 || page.Unread != nil && *page.Unread < 0 {
		return provider.AccountPage{}, &provider.Error{Code: provider.PluginFailed}
	}
	for index := range page.Rows {
		row := &page.Rows[index]
		if row.ID == "" || strings.TrimSpace(row.Title) == "" || row.Count != nil && *row.Count < 0 {
			return provider.AccountPage{}, &provider.Error{Code: provider.PluginFailed}
		}
		if row.DurationSeconds != nil && *row.DurationSeconds < 0 || row.Views != nil && *row.Views < 0 ||
			row.ProgressSeconds != nil && *row.ProgressSeconds < 0 {
			return provider.AccountPage{}, &provider.Error{Code: provider.PluginFailed}
		}
		if row.CreatorAvatarURL != "" && s.allowedURL(row.CreatorAvatarURL) == nil {
			row.CreatorAvatarURL = ""
		}
		if row.Ref != nil {
			if !row.Ref.Kind.Valid() || row.Ref.ID == "" {
				return provider.AccountPage{}, &provider.Error{Code: provider.PluginFailed}
			}
			row.Ref.EntryID = s.entryID
		}
		if row.Item != nil {
			if err := s.ownItem(row.Item); err != nil {
				return provider.AccountPage{}, err
			}
		}
		if row.Notice != nil {
			switch row.Notice.Filter {
			case "", provider.NoticeAll, provider.NoticeReply, provider.NoticeLike, provider.NoticeSystem:
			default:
				row.Notice.Filter = ""
			}
			switch row.Notice.Target {
			case "", provider.NoticeTargetComments:
			default:
				row.Notice.Target = ""
			}
			if len(row.Notice.ActorID) > 128 {
				row.Notice.ActorID = ""
			}
			if len(row.Notice.Cursor) > 512 {
				row.Notice.Cursor = ""
			}
		}
	}
	return page, nil
}

func (s *Session) EditFavoriteFolder(ctx context.Context, edit provider.FavoriteFolderEdit) error {
	if !edit.Valid() {
		return &provider.Error{Code: provider.Denied}
	}
	s.requests.RLock()
	client, scope, owner, secret := s.active, s.scope, s.accountID, s.secret
	s.requests.RUnlock()
	if owner == "" {
		return &provider.Error{Code: provider.NeedsSignIn}
	}
	request := struct {
		provider.FavoriteFolderEdit
		Owner  string          `json:"owner"`
		Secret json.RawMessage `json:"secret"`
	}{edit, owner, secret}
	var result struct{}
	return s.invoke(ctx, scope, client, provider.EditFavoriteFolders, "editFavoriteFolder", request, &result)
}

func (s *Session) RemoveAccountItems(ctx context.Context, section provider.AccountSection, ids []string) error {
	capability := section.RemoveCapability()
	if capability == "" || len(ids) == 0 || len(ids) > 1000 {
		return &provider.Error{Code: provider.Denied}
	}
	for _, id := range ids {
		if id == "" || len(id) > 512 {
			return &provider.Error{Code: provider.Denied}
		}
	}
	s.requests.RLock()
	client, scope, owner, secret := s.active, s.scope, s.accountID, s.secret
	s.requests.RUnlock()
	if owner == "" {
		return &provider.Error{Code: provider.NeedsSignIn}
	}
	input := struct {
		Section provider.AccountSection `json:"section"`
		IDs     []string                `json:"ids"`
		Secret  json.RawMessage         `json:"secret"`
	}{section, ids, secret}
	var result struct{}
	return s.invoke(ctx, scope, client, capability, "removeAccountItems", input, &result)
}

func (s *Session) MarkNotificationsRead(ctx context.Context, request provider.NotificationReadRequest) (provider.NotificationReadResult, error) {
	if !request.Valid() {
		return provider.NotificationReadResult{}, &provider.Error{Code: provider.Denied}
	}
	s.requests.RLock()
	client, scope, owner, secret := s.active, s.scope, s.accountID, s.secret
	s.requests.RUnlock()
	if owner == "" {
		return provider.NotificationReadResult{}, &provider.Error{Code: provider.NeedsSignIn}
	}
	input := struct {
		provider.NotificationReadRequest
		Owner  string          `json:"owner"`
		Secret json.RawMessage `json:"secret"`
	}{request, owner, secret}
	var result provider.NotificationReadResult
	if err := s.invoke(ctx, scope, client, provider.EditNotifications, "markNotificationsRead", input, &result); err != nil {
		return provider.NotificationReadResult{}, err
	}
	if result.Unread != nil && *result.Unread < 0 {
		return provider.NotificationReadResult{}, &provider.Error{Code: provider.PluginFailed}
	}
	return result, nil
}

func (s *Session) invoke(ctx, scope context.Context, client *http.Client, op provider.Capability, export string, request, result any) error {
	return s.invokeWithOrigin(ctx, scope, client, s.activeUserOrigin(), op, export, request, result)
}

func (s *Session) invokeWithOrigin(ctx, scope context.Context, client *http.Client, userOrigin *url.URL, op provider.Capability, export string, request, result any) error {
	if !s.source.manifest.Capabilities.Has(op) {
		return &provider.Error{Code: provider.Unsupported}
	}
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	stop := context.AfterFunc(scope, cancel)
	defer stop()
	if scope.Err() != nil {
		return context.Canceled
	}
	params, err := json.Marshal(request)
	if err != nil {
		return &provider.Error{Code: provider.PluginFailed, Cause: err}
	}
	secure := strings.HasPrefix(string(op), "account.") || op == provider.ReportPlayback || op == provider.WriteLiveChat
	data, err := s.source.executeWithOrigin(ctx, client, s.storage, export, params, secure, userOrigin)
	if scope.Err() != nil {
		return context.Canceled
	}
	if err != nil {
		return err
	}
	if err := decodeJSON(data, result); err != nil {
		return &provider.Error{Code: provider.PluginFailed, Cause: err}
	}
	return nil
}

const invokeBody = `
const fail = (error) => {
  let code = "";
  let message = "";
  try {
    if (error && typeof error.code === "string") code = error.code;
    if (error && typeof error.message === "string") message = error.message;
  } catch {}
  reject(code, message);
};
try {
  Promise.resolve(fn(arg)).then(resolve, fail);
} catch (error) {
  fail(error);
}`

var invokeScript = func() *moejs.Script {
	script, err := moejs.CompileFunction("invoke", "fn, arg, resolve, reject", invokeBody, moejs.FunctionOptions{})
	if err != nil {
		panic(err)
	}
	return script
}()

// Each operation owns its loop. Stopping it cannot interrupt a different call,
// and a completed call cannot leave its timers or promises running.
func (s *Source) execute(ctx context.Context, client *http.Client, store *Storage, export string, params []byte, secure bool) ([]byte, error) {
	return s.executeWithOrigin(ctx, client, store, export, params, secure, nil)
}

func (s *Source) executeWithOrigin(ctx context.Context, client *http.Client, store *Storage, export string, params []byte, secure bool, userOrigin *url.URL) ([]byte, error) {
	rt := moejs.NewRuntime(moejs.Options{MemoryLimit: heapLimit, DisableDynamicCode: true})
	loop, err := eventloop.New(rt, eventloop.Options{})
	if err != nil {
		return nil, &provider.Error{Code: provider.PluginFailed, Cause: err}
	}
	defer loop.Terminate()
	opts := webapi.Options{Context: ctx, Client: client, Allow: s.allowRequestWithOrigin(secure, userOrigin)}
	api, err := webapi.Install(loop, opts)
	if err != nil {
		return nil, &provider.Error{Code: provider.PluginFailed, Cause: err}
	}
	defer api.Close()
	if err := installHost(ctx, rt, store); err != nil {
		return nil, &provider.Error{Code: provider.PluginFailed, Cause: err}
	}
	stop := context.AfterFunc(ctx, func() { loop.Interrupt(ctx.Err()) })
	defer stop()
	var data []byte
	var callErr error
	done := false
	err = loop.Run(func(rt *moejs.Runtime) error {
		if err := rt.Load(s.module); err != nil {
			return err
		}
		for _, capability := range s.manifest.Capabilities {
			names, _ := s.manifest.requiredExports(capability)
			for _, name := range names {
				if value, ok := rt.Export(name); !ok || !value.IsObject() || !value.AsObject().IsCallable() {
					return fmt.Errorf("%w: %s is not callable", ErrIncompatible, name)
				}
			}
		}
		if export == "" {
			loop.StopNoWait()
			return nil
		}
		fn, _ := rt.Export(export)
		arg, err := rt.ParseJSON(params)
		if err != nil {
			return err
		}
		invoke, err := rt.RunScript(invokeScript)
		if err != nil {
			return err
		}
		resolve := rt.Function("resolve", 1, func(_ *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
			data, callErr = rt.AppendJSON(nil, moejs.Arg(args, 0))
			done = true
			loop.StopNoWait()
			return moejs.Undefined(), nil
		})
		reject := rt.Function("reject", 2, func(_ *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
			message := strings.TrimSpace(moejs.Arg(args, 1).String())
			if runes := []rune(message); len(runes) > 120 {
				message = string(runes[:120])
			}
			callErr = &provider.Error{Code: errorCode(moejs.Arg(args, 0).String()), Message: message}
			done = true
			loop.StopNoWait()
			return moejs.Undefined(), nil
		})
		_, err = rt.CallFunction(invoke, moejs.Undefined(), fn, arg, resolve, reject)
		return err
	})
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, &provider.Error{Code: provider.PluginFailed, Cause: err}
	}
	if export == "" {
		return nil, nil
	}
	if callErr != nil {
		var typed *provider.Error
		if errors.As(callErr, &typed) {
			return nil, typed
		}
		return nil, &provider.Error{Code: provider.PluginFailed, Cause: callErr}
	}
	if !done || len(data) == 0 || data[0] != '{' {
		return nil, &provider.Error{Code: provider.PluginFailed}
	}
	return data, nil
}

func errorCode(value string) provider.ErrorCode {
	code := provider.ErrorCode(value)
	switch code {
	case provider.Unsupported, provider.NeedsSignIn, provider.Denied, provider.Expired,
		provider.Network, provider.RateLimited, provider.Canceled, provider.Timeout:
		return code
	default:
		return provider.PluginFailed
	}
}

func (s *Source) httpClient() *http.Client {
	if len(s.manifest.Permissions.Network) == 0 && !s.AllowsUserServerOrigin() {
		return nil
	}
	jar, _ := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	transport := http.DefaultTransport.(*http.Transport).Clone()
	return &http.Client{
		Jar: jar, Timeout: callTimeout, Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("redirect limit exceeded")
			}
			origin := via[0].URL
			sameOrigin := strings.EqualFold(origin.Host, req.URL.Host) && origin.Scheme == req.URL.Scheme
			for _, hop := range via[1:] {
				if !strings.EqualFold(origin.Host, hop.URL.Host) || origin.Scheme != hop.URL.Scheme {
					sameOrigin = false
					break
				}
			}
			if !sameOrigin {
				if req.Method != http.MethodGet && req.Method != http.MethodHead {
					return errors.New("cross-origin redirect requires a read request")
				}
				req.Header = make(http.Header)
			}
			return nil
		},
	}
}
