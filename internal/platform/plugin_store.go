package platform

import (
	"os"
	"path/filepath"

	"github.com/yuchanns/MikuTV/internal/plugin"
)

func NewPluginPackageStore() *plugin.PackageStore {
	return NewPluginPackageStoreNamed("plugins")
}

func NewPluginPackageStoreNamed(subdir string) *plugin.PackageStore {
	if subdir == "" {
		subdir = "plugins"
	}
	candidates := make([]string, 0, 3)
	if dir, err := os.UserConfigDir(); err == nil && dir != "" {
		candidates = append(candidates, filepath.Join(dir, "MikuTV", subdir))
	}
	if dir, err := os.UserCacheDir(); err == nil && dir != "" {
		candidates = append(candidates, filepath.Join(dir, "MikuTV", subdir))
	}
	candidates = append(candidates, filepath.Join(os.TempDir(), "MikuTV", subdir))
	for _, dir := range candidates {
		if writableDir(dir) {
			return plugin.NewPackageStore(dir)
		}
	}
	return plugin.NewPackageStore(candidates[len(candidates)-1])
}

func writableDir(dir string) bool {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false
	}
	f, err := os.CreateTemp(dir, ".probe-*")
	if err != nil {
		return false
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	return true
}
