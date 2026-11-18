package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"strings"
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
}

// NewSession gives an entry its own cookies, storage and cancellation scope.
func (s *Source) NewSession(entryID string, storage *Storage) *Session {
	ctx, cancel := context.WithCancel(context.Background())
	return &Session{
		source: s, entryID: entryID, storage: storage, client: s.httpClient(),
		context: ctx, cancel: cancel,
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
	_, err := s.execute(ctx, nil, nil, "", nil)
	return err
}

func (s *Session) Browse(ctx context.Context, request provider.BrowseRequest) (provider.CollectionPage, error) {
	var page provider.CollectionPage
	if err := s.call(ctx, provider.BrowseCatalog, request, &page); err != nil {
		return page, err
	}
	for _, group := range page.Collections {
		if group.ID == "" || strings.TrimSpace(group.Title) == "" {
			return provider.CollectionPage{}, &provider.Error{Code: provider.PluginFailed}
		}
		for index := range group.Items {
			if err := s.ownItem(&group.Items[index]); err != nil {
				return provider.CollectionPage{}, err
			}
		}
	}
	return page, nil
}

func (s *Session) Item(ctx context.Context, ref provider.MediaRef) (provider.MediaItem, error) {
	var item provider.MediaItem
	if ref.EntryID != s.entryID || !ref.Kind.Valid() || ref.ID == "" {
		return item, &provider.Error{Code: provider.Denied}
	}
	if err := s.call(ctx, provider.GetCatalogItem, ref, &item); err != nil {
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
	item.Ref.EntryID = s.entryID
	return nil
}

func (s *Session) call(ctx context.Context, op provider.Capability, request, result any) error {
	if !s.source.manifest.Capabilities.Has(op) {
		return &provider.Error{Code: provider.Unsupported}
	}
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	stop := context.AfterFunc(s.context, cancel)
	defer stop()
	if s.context.Err() != nil {
		return context.Canceled
	}
	params, err := json.Marshal(request)
	if err != nil {
		return &provider.Error{Code: provider.PluginFailed, Cause: err}
	}
	data, err := s.source.execute(ctx, s.client, s.storage, operations[op], params)
	if s.context.Err() != nil {
		return context.Canceled
	}
	if err != nil {
		return err
	}
	if err := decodeJSON(data, result); err != nil {
		return &provider.Error{Code: provider.PluginFailed, Cause: err}
	}
	return ctx.Err()
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
func (s *Source) execute(ctx context.Context, client *http.Client, store *Storage, export string, params []byte) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	rt := moejs.NewRuntime(moejs.Options{MemoryLimit: heapLimit, DisableDynamicCode: true})
	loop, err := eventloop.New(rt, eventloop.Options{})
	if err != nil {
		return nil, &provider.Error{Code: provider.PluginFailed, Cause: err}
	}
	defer loop.Terminate()
	opts := webapi.Options{Context: ctx, Client: client, Allow: s.allowRequest()}
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
			name := operations[capability]
			if value, ok := rt.Export(name); !ok || !value.IsObject() || !value.AsObject().IsCallable() {
				return fmt.Errorf("%w: %s is not callable", ErrIncompatible, name)
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
