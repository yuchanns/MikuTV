package plugin

import (
	"bytes"
	"compress/zlib"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/MoeForge/moejs"
	"github.com/MoeForge/moejs/webapi"
)

const maxInflateBytes = 4 << 20

const StorageCapacity = 8 << 10

type Storage struct {
	mu      sync.Mutex
	values  map[string]string
	persist func([]byte) error
}

func NewStorage(data []byte, persist func([]byte) error) (*Storage, error) {
	if len(data) > StorageCapacity {
		return nil, errors.New("storage quota exceeded")
	}
	values := make(map[string]string)
	if len(data) > 0 {
		if err := decodeJSON(data, &values); err != nil {
			return nil, err
		}
	}
	return &Storage{values: values, persist: persist}, nil
}

func (s *Storage) get(key string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.values[key]
	return value, ok
}

func (s *Storage) set(key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	old, existed := s.values[key]
	s.values[key] = value
	data, err := json.Marshal(s.values)
	if err == nil && len(data) > StorageCapacity {
		err = errors.New("storage quota exceeded")
	}
	if err == nil && s.persist != nil {
		err = s.persist(data)
	}
	if err != nil {
		if existed {
			s.values[key] = old
		} else {
			delete(s.values, key)
		}
	}
	return err
}

func (s *Storage) delete(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	old, existed := s.values[key]
	if !existed {
		return nil
	}
	delete(s.values, key)
	if s.persist == nil {
		return nil
	}
	data, err := json.Marshal(s.values)
	if err == nil {
		err = s.persist(data)
	}
	if err != nil {
		s.values[key] = old
	}
	return err
}

var (
	hostLocaleMu sync.RWMutex
	hostLocaleFn func() string
)

// SetHostLocaleProvider registers a callback returning the host's currently
// active BCP-47 language tag (e.g. "zh-Hans", "en", "ja") for host.locale.current().
func SetHostLocaleProvider(fn func() string) {
	hostLocaleMu.Lock()
	hostLocaleFn = fn
	hostLocaleMu.Unlock()
}

func currentHostLocale() string {
	hostLocaleMu.RLock()
	fn := hostLocaleFn
	hostLocaleMu.RUnlock()
	if fn != nil {
		if tag := NormalizeLocaleTag(fn()); tag != "" {
			return tag
		}
	}
	return "zh-Hans"
}

func installHost(ctx context.Context, rt *moejs.Runtime, store *Storage, extra ...map[string]any) error {
	var extras map[string]any
	if len(extra) > 0 {
		extras = extra[0]
	}
	return installHostWith(ctx, rt, store, extras)
}

func installHostWith(ctx context.Context, rt *moejs.Runtime, store *Storage, extra map[string]any) error {
	get := rt.Function("get", 1, func(_ *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
		if err := ctx.Err(); err != nil {
			return moejs.Undefined(), err
		}
		if value, ok := store.get(moejs.Arg(args, 0).String()); ok {
			return rt.FromGo(value)
		}
		return moejs.Undefined(), nil
	})
	set := rt.Function("set", 2, func(_ *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
		if err := ctx.Err(); err != nil {
			return moejs.Undefined(), err
		}
		return moejs.Undefined(), store.set(moejs.Arg(args, 0).String(), moejs.Arg(args, 1).String())
	})
	remove := rt.Function("delete", 1, func(_ *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
		if err := ctx.Err(); err != nil {
			return moejs.Undefined(), err
		}
		return moejs.Undefined(), store.delete(moejs.Arg(args, 0).String())
	})
	digest := rt.Function("sha256", 1, func(_ *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
		input := moejs.Arg(args, 0).String()
		sum := sha256.Sum256([]byte(input))
		return rt.FromGo(hex.EncodeToString(sum[:]))
	})
	encrypt := rt.Function("encryptRSAOAEP", 2, func(_ *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
		der, err := base64.StdEncoding.DecodeString(moejs.Arg(args, 0).String())
		if err != nil {
			return moejs.Undefined(), err
		}
		public, err := x509.ParsePKIXPublicKey(der)
		if err != nil {
			return moejs.Undefined(), err
		}
		key, ok := public.(*rsa.PublicKey)
		if !ok {
			return moejs.Undefined(), errors.New("RSA public key required")
		}
		ciphertext, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, key, []byte(moejs.Arg(args, 1).String()), nil)
		if err != nil {
			return moejs.Undefined(), err
		}
		return rt.FromGo(hex.EncodeToString(ciphertext))
	})
	inflateZlib := rt.Function("inflateZlib", 1, func(_ *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
		if err := ctx.Err(); err != nil {
			return moejs.Undefined(), err
		}
		raw, err := rt.ToGo(moejs.Arg(args, 0))
		if err != nil {
			return moejs.Undefined(), err
		}
		data, ok := raw.([]byte)
		if !ok || len(data) == 0 {
			return moejs.Undefined(), errors.New("byte buffer required")
		}
		reader, err := zlib.NewReader(bytes.NewReader(data))
		if err != nil {
			return moejs.Undefined(), err
		}
		defer reader.Close()
		out, err := io.ReadAll(io.LimitReader(reader, maxInflateBytes+1))
		if err != nil {
			return moejs.Undefined(), err
		}
		if len(out) > maxInflateBytes {
			return moejs.Undefined(), errors.New("decompressed payload exceeds limit")
		}
		return rt.FromGo(out)
	})
	currentLocale := rt.Function("current", 0, func(_ *moejs.Realm, _ moejs.Value, _ []moejs.Value) (moejs.Value, error) {
		if err := ctx.Err(); err != nil {
			return moejs.Undefined(), err
		}
		return rt.FromGo(currentHostLocale())
	})
	hostObj := map[string]any{
		"storage":  map[string]any{"get": get, "set": set, "delete": remove},
		"crypto":   map[string]any{"sha256": digest, "encryptRSAOAEP": encrypt},
		"compress": map[string]any{"inflateZlib": inflateZlib},
		"locale":   map[string]any{"current": currentLocale},
	}
	for k, v := range extra {
		hostObj[k] = v
	}
	return rt.SetGlobal("host", hostObj)
}

func sameHTTPOrigin(a, b *url.URL) bool {
	if a == nil || b == nil {
		return false
	}
	return strings.EqualFold(a.Scheme, b.Scheme) && strings.EqualFold(a.Host, b.Host)
}

func (s *Source) allowRequest(secure bool) func(*http.Request) error {
	return s.allowRequestWithOrigin(secure, nil)
}

func (s *Source) allowRequestWithOrigin(secure bool, userOrigin *url.URL) func(*http.Request) error {
	allow := webapi.AllowHosts(s.manifest.Permissions.Network...)
	allowUser := userOrigin != nil && s.AllowsUserServerOrigin()
	return func(req *http.Request) error {
		if req == nil || req.URL == nil || (req.URL.Scheme != "http" && req.URL.Scheme != "https") || req.URL.User != nil ||
			(req.Host != "" && !strings.EqualFold(req.Host, req.URL.Host)) {
			return errors.New("source requests require an authorized HTTP origin")
		}
		if allowUser && sameHTTPOrigin(req.URL, userOrigin) {
			return nil
		}
		if secure && req.URL.Scheme != "https" {
			return errors.New("source requests require an authorized HTTPS origin")
		}
		return allow(req)
	}
}
