package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/cookiejar"
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
	context context.Context
	cancel  context.CancelFunc

	requests  sync.RWMutex
	active    *http.Client
	accountID string
	secret    json.RawMessage
	scope     context.Context
	stop      context.CancelFunc
}

// NewSession gives an entry its own cookies, storage and cancellation scope.
func (s *Source) NewSession(entryID string, storage *Storage) *Session {
	ctx, cancel := context.WithCancel(context.Background())
	scope, stop := context.WithCancel(ctx)
	client := s.httpClient()
	return &Session{
		source: s, entryID: entryID, storage: storage, client: client,
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
	client, scope, signedIn := s.active, s.scope, s.accountID != ""
	s.requests.RUnlock()
	input := struct {
		provider.BrowseRequest
		SignedIn bool `json:"signedIn,omitempty"`
	}{request, signedIn}
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
		IncludeSections bool `json:"includeSections,omitempty"`
		SignedIn        bool `json:"signedIn,omitempty"`
	}{MediaRef: ref, IncludeSections: includeSections}
	s.requests.RLock()
	client, scope, signedIn := s.active, s.scope, s.accountID != ""
	s.requests.RUnlock()
	input.SignedIn = signedIn
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
	if item.CoverURL != "" && httpsURL(item.CoverURL) == nil {
		item.CoverURL = ""
	}
	if item.BackdropURL != "" && httpsURL(item.BackdropURL) == nil {
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
		if strings.TrimSpace(segment.Title) == "" || segment.StartSeconds < 0 ||
			segment.DurationSeconds != nil && *segment.DurationSeconds < 0 {
			return &provider.Error{Code: provider.PluginFailed}
		}
		if segment.Ref != nil {
			if segment.Ref.Kind != provider.Video || segment.Ref.ID == "" {
				return &provider.Error{Code: provider.PluginFailed}
			}
			segment.Ref.EntryID = s.entryID
		}
	}
	if err := ownVideoComments(detail.Discussion, true); err != nil {
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
	client, scope, accountID := s.active, s.scope, s.accountID
	s.requests.RUnlock()
	if accountID == "" {
		return provider.AccountPage{}, &provider.Error{Code: provider.NeedsSignIn}
	}
	input := struct {
		provider.AccountRequest
		Owner string `json:"owner"`
	}{request, accountID}
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
			row.ProgressSeconds != nil && *row.ProgressSeconds < -1 {
			return provider.AccountPage{}, &provider.Error{Code: provider.PluginFailed}
		}
		if row.CreatorAvatarURL != "" && httpsURL(row.CreatorAvatarURL) == nil {
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
	if capability == "" || len(ids) == 0 || len(ids) > 20 {
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

func (s *Session) invoke(ctx, scope context.Context, client *http.Client, op provider.Capability, export string, request, result any) error {
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
	data, err := s.source.execute(ctx, client, s.storage, export, params, strings.HasPrefix(string(op), "account."))
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
  try {
    if (error && typeof error.code === "string") code = error.code;
  } catch {}
  reject(code);
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
	rt := moejs.NewRuntime(moejs.Options{MemoryLimit: heapLimit, DisableDynamicCode: true})
	loop, err := eventloop.New(rt, eventloop.Options{})
	if err != nil {
		return nil, &provider.Error{Code: provider.PluginFailed, Cause: err}
	}
	defer loop.Terminate()
	opts := webapi.Options{Context: ctx, Client: client, Allow: s.allowRequest(secure)}
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
			for _, name := range operations[capability] {
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
		reject := rt.Function("reject", 1, func(_ *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
			callErr = &provider.Error{Code: errorCode(moejs.Arg(args, 0).String())}
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
	if len(s.manifest.Permissions.Network) == 0 {
		return nil
	}
	jar, _ := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	transport := http.DefaultTransport.(*http.Transport).Clone()
	return &http.Client{
		Jar: jar, Timeout: callTimeout, Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			origin := via[0].URL
			from := via[len(via)-1].URL
			sameOrigin := strings.EqualFold(origin.Host, req.URL.Host) && origin.Scheme == req.URL.Scheme
			samePrev := strings.EqualFold(from.Host, req.URL.Host) && from.Scheme == req.URL.Scheme
			if !sameOrigin || !samePrev {
				if req.Method != http.MethodGet && req.Method != http.MethodHead {
					return errors.New("cross-origin redirect requires a read request")
				}
				req.Header = make(http.Header)
			}
			return nil
		},
	}
}
