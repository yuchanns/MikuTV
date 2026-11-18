package plugin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"

	"github.com/MoeForge/moejs"
	"github.com/MoeForge/moejs/webapi"
)

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

func installHost(ctx context.Context, rt *moejs.Runtime, store *Storage) error {
	if store == nil {
		store, _ = NewStorage(nil, nil)
	}
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
	return rt.SetGlobal("host", map[string]any{
		"storage": map[string]any{"get": get, "set": set, "delete": remove},
		"crypto":  map[string]any{"sha256": digest},
	})
}

func (s *Source) allowRequest() func(*http.Request) error {
	allow := webapi.AllowHosts(s.manifest.Permissions.Network...)
	return func(req *http.Request) error {
		if (req.URL.Scheme != "http" && req.URL.Scheme != "https") || req.URL.User != nil ||
			(req.Host != "" && !strings.EqualFold(req.Host, req.URL.Host)) {
			return errors.New("source requests require an authorized HTTP origin")
		}
		return allow(req)
	}
}
