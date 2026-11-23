package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/MoeForge/moejs"
	"github.com/MoeForge/moejs/eventloop"
	"github.com/MoeForge/moejs/webapi"
	"github.com/yuchanns/MikuTV/internal/provider"
)

const (
	liveChatEventBuffer    = 128
	liveChatMaxAuthorRunes = 64
	liveChatMaxBodyRunes   = 200
	liveChatMaxReasonRunes = 80
)

const connectLiveChatBody = `
const fail = error => {
  let code = "plugin", message = "";
  try {
    if (error && typeof error.code === "string") code = error.code;
    if (error && typeof error.message === "string") message = error.message;
  } catch {}
  finish(code, message);
};
try {
  Promise.resolve(fn(arg, emit)).then(() => finish("", ""), fail);
} catch (error) {
  fail(error);
}`

var connectLiveChatScript = func() *moejs.Script {
	script, err := moejs.CompileFunction("connectLiveChat", "fn, arg, emit, finish", connectLiveChatBody, moejs.FunctionOptions{})
	if err != nil {
		panic(err)
	}
	return script
}()

type liveChatSubscription struct {
	events chan provider.LiveChatEvent
	cancel context.CancelFunc
	done   chan struct{}
	once   sync.Once
}

func (s *liveChatSubscription) Events() <-chan provider.LiveChatEvent {
	return s.events
}

func (s *liveChatSubscription) Close() {
	s.once.Do(func() {
		s.cancel()
	})
}

func (s *Session) OpenLiveChat(ctx context.Context, request provider.LiveChatRequest) (provider.LiveChatSubscription, error) {
	if !s.ownsVideo(request.Ref) || request.Ref.Kind != provider.Live || len(request.ItemID) > 512 {
		return nil, &provider.Error{Code: provider.Denied}
	}
	if !s.source.manifest.Capabilities.Has(provider.LiveChat) {
		return nil, &provider.Error{Code: provider.Unsupported}
	}
	s.requests.RLock()
	client, scope, owner := s.active, s.scope, s.accountID
	s.requests.RUnlock()
	if client == nil {
		return nil, &provider.Error{Code: provider.PluginFailed}
	}
	if err := scope.Err(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	subCtx, subCancel := context.WithCancel(context.Background())
	stopCtx := context.AfterFunc(ctx, subCancel)
	stopScope := context.AfterFunc(scope, subCancel)

	sub := &liveChatSubscription{
		events: make(chan provider.LiveChatEvent, liveChatEventBuffer),
		cancel: subCancel,
		done:   make(chan struct{}),
	}
	input := struct {
		provider.LiveChatRequest
		SignedIn bool   `json:"signedIn,omitempty"`
		Owner    string `json:"owner,omitempty"`
	}{request, owner != "", owner}

	params, err := json.Marshal(input)
	if err != nil {
		stopCtx()
		stopScope()
		subCancel()
		return nil, &provider.Error{Code: provider.PluginFailed, Cause: err}
	}

	go func() {
		defer close(sub.done)
		defer close(sub.events)
		defer subCancel()
		defer stopScope()
		defer stopCtx()

		emit := func(event provider.LiveChatEvent) {
			if subCtx.Err() != nil {
				return
			}
			sanitized, ok := sanitizeLiveChatEvent(event)
			if !ok {
				return
			}
			if sanitized.Type == provider.LiveChatEventMessage {
				select {
				case sub.events <- sanitized:
				default:
					// Drop oldest buffered message under burst to protect UI latency.
					select {
					case <-sub.events:
					default:
					}
					select {
					case sub.events <- sanitized:
					default:
					}
				}
				return
			}
			select {
			case sub.events <- sanitized:
			case <-subCtx.Done():
			}
		}

		err := s.source.runLiveChat(subCtx, client, s.storage, params, emit, !allowLocalNetworkForTest)
		if err != nil && subCtx.Err() == nil {
			emit(provider.LiveChatEvent{
				Type:   provider.LiveChatEventStatus,
				Status: provider.LiveChatDisconnected,
				Error:  provider.CodeOf(err),
			})
		}
	}()

	return sub, nil
}

func (s *Source) runLiveChat(ctx context.Context, client *http.Client, store *Storage, params []byte, emit func(provider.LiveChatEvent), secure bool) error {
	rt := moejs.NewRuntime(moejs.Options{MemoryLimit: heapLimit, DisableDynamicCode: true})
	loop, err := eventloop.New(rt, eventloop.Options{})
	if err != nil {
		return &provider.Error{Code: provider.PluginFailed, Cause: err}
	}
	defer loop.Terminate()

	allow := s.allowRequest(secure)
	opts := webapi.Options{Context: ctx, Client: client, Allow: allow}
	api, err := webapi.Install(loop, opts)
	if err != nil {
		return &provider.Error{Code: provider.PluginFailed, Cause: err}
	}
	defer api.Close()

	bridge := newWSBridge(ctx, loop, client, allow, secure)
	defer bridge.closeAll()

	if err := installHost(ctx, rt, store, map[string]any{"ws": bridge.hostObject(rt)}); err != nil {
		return &provider.Error{Code: provider.PluginFailed, Cause: err}
	}

	stop := context.AfterFunc(ctx, func() {
		bridge.closeAll()
		loop.Interrupt(ctx.Err())
	})
	defer stop()

	var callErr error
	err = loop.Run(func(rt *moejs.Runtime) error {
		if err := rt.Load(s.module); err != nil {
			return err
		}
		fn, ok := rt.Export("connectLiveChat")
		if !ok || !fn.IsObject() || !fn.AsObject().IsCallable() {
			return errors.New("connectLiveChat is not callable")
		}
		arg, err := rt.ParseJSON(params)
		if err != nil {
			return err
		}
		runner, err := rt.RunScript(connectLiveChatScript)
		if err != nil {
			return err
		}
		emitFn := rt.Function("emit", 1, func(_ *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
			if ctx.Err() != nil {
				return moejs.Undefined(), ctx.Err()
			}
			raw, err := rt.AppendJSON(nil, moejs.Arg(args, 0))
			if err != nil {
				return moejs.Undefined(), nil
			}
			var event provider.LiveChatEvent
			if err := json.Unmarshal(raw, &event); err == nil {
				emit(event)
			}
			return moejs.Undefined(), nil
		})
		finishFn := rt.Function("finish", 2, func(_ *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
			code := strings.TrimSpace(moejs.Arg(args, 0).String())
			msg := strings.TrimSpace(moejs.Arg(args, 1).String())
			if code != "" {
				callErr = &provider.Error{Code: errorCode(code), Message: truncateRunes(msg, 120)}
			}
			bridge.closeAll()
			loop.StopNoWait()
			return moejs.Undefined(), nil
		})
		_, err = rt.CallFunction(runner, moejs.Undefined(), fn, arg, emitFn, finishFn)
		return err
	})
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return &provider.Error{Code: provider.PluginFailed, Cause: err}
	}
	return callErr
}

func (s *Session) SendLiveChat(ctx context.Context, post provider.LiveChatPost) (provider.LiveChatSendResult, error) {
	var result provider.LiveChatSendResult
	post.Text = strings.TrimSpace(post.Text)
	if !s.ownsVideo(post.Ref) || post.Ref.Kind != provider.Live || len(post.ItemID) > 512 ||
		post.Text == "" || utf8.RuneCountInString(post.Text) > provider.LiveChatLimit {
		return result, &provider.Error{Code: provider.Denied}
	}
	s.requests.RLock()
	client, scope, owner, secret := s.active, s.scope, s.accountID, s.secret
	s.requests.RUnlock()
	if owner == "" {
		return result, &provider.Error{Code: provider.NeedsSignIn}
	}
	if post.Owner != "" && post.Owner != owner {
		return result, context.Canceled
	}
	input := struct {
		Data   provider.LiveChatPost `json:"data"`
		Secret json.RawMessage       `json:"secret"`
	}{post, secret}
	if err := s.invoke(ctx, scope, client, provider.WriteLiveChat, "sendLiveChat", input, &result); err != nil {
		return provider.LiveChatSendResult{}, err
	}
	result.MuteReason = truncateRunes(cleanChatText(result.MuteReason), liveChatMaxReasonRunes)
	return result, nil
}

func sanitizeLiveChatEvent(event provider.LiveChatEvent) (provider.LiveChatEvent, bool) {
	switch event.Type {
	case provider.LiveChatEventStatus:
		switch event.Status {
		case provider.LiveChatConnecting, provider.LiveChatConnected, provider.LiveChatReconnecting, provider.LiveChatDisconnected:
		default:
			return provider.LiveChatEvent{}, false
		}
		if event.Error != "" {
			event.Error = errorCode(string(event.Error))
		}
		event.Message = nil
		event.Viewers = nil
		return event, true
	case provider.LiveChatEventMessage:
		if event.Message == nil {
			return provider.LiveChatEvent{}, false
		}
		msg := *event.Message
		msg.ID = truncateRunes(strings.TrimSpace(msg.ID), 64)
		msg.Author = truncateRunes(cleanChatText(msg.Author), liveChatMaxAuthorRunes)
		msg.AuthorID = truncateRunes(strings.TrimSpace(msg.AuthorID), 64)
		msg.Body = truncateRunes(cleanChatText(msg.Body), liveChatMaxBodyRunes)
		if msg.Body == "" {
			return provider.LiveChatEvent{}, false
		}
		if msg.Timestamp < 0 {
			msg.Timestamp = 0
		}
		event.Message = &msg
		event.Status, event.Error, event.Viewers, event.Muted, event.MuteReason = "", "", nil, nil, ""
		return event, true
	case provider.LiveChatEventViewers:
		if event.Viewers == nil || *event.Viewers < 0 {
			return provider.LiveChatEvent{}, false
		}
		v := *event.Viewers
		return provider.LiveChatEvent{Type: provider.LiveChatEventViewers, Viewers: &v}, true
	case provider.LiveChatEventMute:
		if event.Muted == nil {
			return provider.LiveChatEvent{}, false
		}
		m := *event.Muted
		return provider.LiveChatEvent{
			Type:       provider.LiveChatEventMute,
			Muted:      &m,
			MuteReason: truncateRunes(cleanChatText(event.MuteReason), liveChatMaxReasonRunes),
		}, true
	case provider.LiveChatEventEnded:
		return provider.LiveChatEvent{Type: provider.LiveChatEventEnded}, true
	default:
		return provider.LiveChatEvent{}, false
	}
}

func cleanChatText(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' {
			return ' '
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
	return strings.TrimSpace(s)
}

func truncateRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max])
}
