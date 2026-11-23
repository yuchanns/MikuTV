package platform

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"github.com/yuchanns/MikuTV/internal/app"
)

type PluginCatalogFileCache struct {
	mu   sync.Mutex
	path string
}

func NewPluginCatalogCache() *PluginCatalogFileCache {
	return NewPluginCatalogCacheNamed("plugin-catalog-v1.json")
}

func NewPluginCatalogCacheNamed(filename string) *PluginCatalogFileCache {
	base, err := os.UserCacheDir()
	if err != nil || base == "" {
		base = os.TempDir()
	}
	if filename == "" {
		filename = "plugin-catalog-v1.json"
	}
	return &PluginCatalogFileCache{
		path: filepath.Join(base, "MikuTV", filename),
	}
}

func (c *PluginCatalogFileCache) Load() (app.PluginCatalogCacheData, error) {
	if c == nil || c.path == "" {
		return app.PluginCatalogCacheData{}, nil
	}
	data, err := os.ReadFile(c.path)
	if err != nil {
		if os.IsNotExist(err) {
			return app.PluginCatalogCacheData{}, nil
		}
		return app.PluginCatalogCacheData{}, err
	}
	var out app.PluginCatalogCacheData
	if err := json.Unmarshal(data, &out); err != nil {
		return app.PluginCatalogCacheData{}, err
	}
	return out, nil
}

func (c *PluginCatalogFileCache) Save(data app.PluginCatalogCacheData) error {
	if c == nil || c.path == "" {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return err
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, c.path)
}
