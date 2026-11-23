package app

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/yuchanns/MikuTV/internal/plugin"
	"github.com/yuchanns/MikuTV/internal/provider"
)

var ErrPluginAlreadyInstalled = errors.New("plugin is already installed")

type PluginCatalogStatus uint8

const (
	PluginCatalogIdle PluginCatalogStatus = iota
	PluginCatalogLoading
	PluginCatalogReady
	PluginCatalogOffline
	PluginCatalogRateLimited
	PluginCatalogFailed
)

type InstalledPlugin struct {
	ID           string
	Name         string
	Description  string
	Kind         plugin.Kind
	Version      string
	Repository   string
	ReleaseID    int64
	TagName      string
	AssetID      int64
	SHA256       string
	Bundled      bool
	Enabled      bool
	License      string
	SourceCommit string
	Permissions  []string
	Capabilities provider.Capabilities
}

type PluginInstallOutcome struct {
	Receipt   plugin.InstalledReceipt
	Err       error
	Canceled  bool
	Retryable bool
}

type PluginCatalogCacheData struct {
	CachedAt   int64                `json:"cachedAt"`
	ETag       string               `json:"etag,omitempty"`
	Page       int                  `json:"page"`
	TotalCount int                  `json:"totalCount"`
	HasMore    bool                 `json:"hasMore"`
	Items      []plugin.CatalogItem `json:"items,omitempty"`
}

type PluginCatalogCache interface {
	Load() (PluginCatalogCacheData, error)
	Save(PluginCatalogCacheData) error
}

type PluginCatalogFetcher interface {
	FetchPage(ctx context.Context, page int, etag string) (plugin.CatalogPage, error)
	SeedInspectCache(items []plugin.CatalogItem)
	ResolveRepository(ctx context.Context, rawRepo string) (plugin.CatalogItem, error)
	DownloadAsset(ctx context.Context, asset plugin.ReleaseAsset) ([]byte, [sha256.Size]byte, error)
}

type PluginCatalogSnapshot struct {
	Status           PluginCatalogStatus
	LoadingMore      bool
	HasCache         bool
	CachedAt         time.Time
	RateLimitResetAt time.Time
	Page             int
	TotalCount       int
	HasMore          bool
	Installed        []InstalledPlugin
	Items            []plugin.CatalogItem
	Error            string
}

type PluginCatalog struct {
	fetcher    PluginCatalogFetcher
	cache      PluginCatalogCache
	pkgStore   *plugin.PackageStore
	appVersion string
	onMounted  func(*plugin.Source, plugin.InstalledReceipt) error
	post       func(func())
	invalidate func()

	mu               sync.Mutex
	cacheMu          sync.Mutex
	installed        []InstalledPlugin
	status           PluginCatalogStatus
	loadingMore      bool
	items            []plugin.CatalogItem
	etag             string
	cachedAt         int64
	page             int
	totalCount       int
	hasMore          bool
	rateLimitResetAt time.Time
	lastErr          string
	gen              uint64
	cancel           context.CancelFunc
	resolveGen       uint64
	resolveCancel    context.CancelFunc
	installGen       uint64
	installCancel    context.CancelFunc
}

func (c *PluginCatalog) SetInstaller(
	store *plugin.PackageStore,
	appVersion string,
	onMounted func(*plugin.Source, plugin.InstalledReceipt) error,
) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pkgStore = store
	c.appVersion = appVersion
	c.onMounted = onMounted
}

func NewPluginCatalog(
	installed []InstalledPlugin,
	fetcher PluginCatalogFetcher,
	cache PluginCatalogCache,
	post func(func()),
	invalidate func(),
) *PluginCatalog {
	c := &PluginCatalog{
		fetcher:    fetcher,
		cache:      cache,
		post:       post,
		invalidate: invalidate,
		installed:  append([]InstalledPlugin(nil), installed...),
		status:     PluginCatalogIdle,
	}
	if cache != nil {
		if saved, err := cache.Load(); err == nil && len(saved.Items) > 0 {
			c.items = deduplicateCatalogItems(nil, saved.Items)
			c.etag = saved.ETag
			c.cachedAt = saved.CachedAt
			c.page = max(1, saved.Page)
			c.totalCount = saved.TotalCount
			c.hasMore = saved.HasMore
			if fetcher != nil {
				fetcher.SeedInspectCache(c.items)
			}
		}
	}
	return c
}

func (c *PluginCatalog) Snapshot() PluginCatalogSnapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	var cachedAt time.Time
	if c.cachedAt > 0 {
		cachedAt = time.Unix(c.cachedAt, 0)
	}
	return PluginCatalogSnapshot{
		Status:           c.status,
		LoadingMore:      c.loadingMore,
		HasCache:         len(c.items) > 0,
		CachedAt:         cachedAt,
		RateLimitResetAt: c.rateLimitResetAt,
		Page:             c.page,
		TotalCount:       c.totalCount,
		HasMore:          c.hasMore,
		Installed:        append([]InstalledPlugin(nil), c.installed...),
		Items:            append([]plugin.CatalogItem(nil), c.items...),
		Error:            c.lastErr,
	}
}

func (c *PluginCatalog) FindInstalled(id string) (InstalledPlugin, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, item := range c.installed {
		if strings.EqualFold(item.ID, id) {
			return item, true
		}
	}
	return InstalledPlugin{}, false
}

func (c *PluginCatalog) FindCatalog(repository string) (plugin.CatalogItem, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, item := range c.items {
		if strings.EqualFold(item.Repository, repository) {
			return item, true
		}
	}
	return plugin.CatalogItem{}, false
}

// UninstalledItems returns discoverable catalog items excluding already
// installed plugins, filtered by kind ("" for all) and search query.
func (s PluginCatalogSnapshot) UninstalledItems(kind plugin.Kind, query string) []plugin.CatalogItem {
	installedIDs := make(map[string]bool, len(s.Installed))
	installedRepos := make(map[string]bool, len(s.Installed))
	for _, inst := range s.Installed {
		if id := strings.ToLower(strings.TrimSpace(inst.ID)); id != "" {
			installedIDs[id] = true
		}
		if repo := strings.ToLower(strings.TrimSpace(inst.Repository)); repo != "" {
			installedRepos[repo] = true
		}
	}
	q := strings.ToLower(strings.TrimSpace(query))
	out := make([]plugin.CatalogItem, 0, len(s.Items))
	for _, item := range s.Items {
		if installedIDs[strings.ToLower(strings.TrimSpace(item.Manifest.ID))] {
			continue
		}
		if repo := strings.ToLower(strings.TrimSpace(item.Repository)); repo != "" && installedRepos[repo] {
			continue
		}
		if kind != "" && item.Manifest.Kind != kind {
			continue
		}
		if q != "" && !catalogMatchesQuery(item, q) {
			continue
		}
		out = append(out, item)
	}
	return out
}

func (s PluginCatalogSnapshot) TotalUninstalled() int {
	return len(s.UninstalledItems("", ""))
}

func catalogMatchesQuery(item plugin.CatalogItem, lowerQuery string) bool {
	fields := [...]string{
		item.Manifest.Name,
		item.Manifest.ID,
		item.Manifest.Description,
		item.Repository,
		item.Owner,
		item.RepoDescription,
	}
	for _, f := range fields {
		if strings.Contains(strings.ToLower(f), lowerQuery) {
			return true
		}
	}
	return false
}

func (c *PluginCatalog) EnsureLoaded() {
	c.mu.Lock()
	shouldLoad := c.status == PluginCatalogIdle
	c.mu.Unlock()
	if shouldLoad {
		c.Refresh()
	}
}

func (c *PluginCatalog) Refresh() {
	c.mu.Lock()
	if c.fetcher == nil {
		if len(c.items) > 0 {
			c.status = PluginCatalogOffline
		} else {
			c.status = PluginCatalogFailed
		}
		c.mu.Unlock()
		c.notify()
		return
	}
	if c.cancel != nil {
		c.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel
	c.gen++
	gen := c.gen
	etag := c.etag
	c.status = PluginCatalogLoading
	c.loadingMore = false
	c.lastErr = ""
	c.mu.Unlock()
	c.notify()

	go func() {
		defer cancel()
		page, err := c.fetcher.FetchPage(ctx, 1, etag)
		c.post(func() {
			c.mu.Lock()
			if c.gen != gen {
				c.mu.Unlock()
				return
			}
			c.cancel = nil
			if err != nil {
				c.applyFetchErrorLocked(err)
				c.mu.Unlock()
				c.notify()
				return
			}
			c.rateLimitResetAt = time.Time{}
			c.lastErr = ""
			if page.NotModified {
				c.status = PluginCatalogReady
				c.cachedAt = time.Now().Unix()
				c.mu.Unlock()
				c.notify()
				return
			}
			c.items = deduplicateCatalogItems(nil, page.Items)
			c.page = 1
			c.totalCount = page.TotalCount
			c.hasMore = page.HasMore
			c.etag = page.ETag
			c.cachedAt = time.Now().Unix()
			c.status = PluginCatalogReady
			cacheData := c.cacheDataLocked()
			c.mu.Unlock()
			c.saveCacheAsync(cacheData)
			c.notify()
		})
	}()
}

func (c *PluginCatalog) LoadMore() {
	c.mu.Lock()
	if c.fetcher == nil || c.loadingMore || c.status == PluginCatalogLoading || !c.hasMore {
		c.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel
	c.loadingMore = true
	gen := c.gen
	nextPage := max(1, c.page) + 1
	c.mu.Unlock()
	c.notify()

	go func() {
		defer cancel()
		page, err := c.fetcher.FetchPage(ctx, nextPage, "")
		c.post(func() {
			c.mu.Lock()
			if c.gen != gen {
				c.mu.Unlock()
				return
			}
			c.cancel = nil
			c.loadingMore = false
			if err != nil {
				c.applyFetchErrorLocked(err)
				c.mu.Unlock()
				c.notify()
				return
			}
			c.items = deduplicateCatalogItems(c.items, page.Items)
			c.page = nextPage
			if page.TotalCount > c.totalCount {
				c.totalCount = page.TotalCount
			}
			c.hasMore = page.HasMore
			c.cachedAt = time.Now().Unix()
			c.status = PluginCatalogReady
			c.lastErr = ""
			cacheData := c.cacheDataLocked()
			c.mu.Unlock()
			c.saveCacheAsync(cacheData)
			c.notify()
		})
	}()
}

func (c *PluginCatalog) Close() {
	c.mu.Lock()
	cancel := c.cancel
	resolveCancel := c.resolveCancel
	installCancel := c.installCancel
	c.cancel = nil
	c.resolveCancel = nil
	c.installCancel = nil
	c.gen++
	c.resolveGen++
	c.installGen++
	c.loadingMore = false
	if c.status == PluginCatalogLoading {
		if len(c.items) > 0 {
			c.status = PluginCatalogReady
		} else {
			c.status = PluginCatalogIdle
		}
	}
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if resolveCancel != nil {
		resolveCancel()
	}
	if installCancel != nil {
		installCancel()
	}
}

func (c *PluginCatalog) CheckInstallable(item plugin.CatalogItem) error {
	if !item.Compatibility.Compatible || item.Manifest.Kind != plugin.SourceKind {
		return plugin.ErrIncompatible
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, existing := range c.installed {
		if strings.EqualFold(existing.ID, item.Manifest.ID) {
			if existing.Bundled || !strings.EqualFold(existing.Repository, item.Repository) {
				return fmt.Errorf("%w: %s", plugin.ErrSourceConflict, item.Manifest.ID)
			}
			return ErrPluginAlreadyInstalled
		}
	}
	return nil
}

func (c *PluginCatalog) UpsertCatalogItem(item plugin.CatalogItem) {
	if strings.TrimSpace(item.Repository) == "" || item.Manifest.ID == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	replaced := false
	for i, existing := range c.items {
		if strings.EqualFold(existing.Repository, item.Repository) {
			c.items[i] = item
			replaced = true
			break
		}
	}
	if !replaced {
		c.items = append([]plugin.CatalogItem{item}, c.items...)
	}
}

func (c *PluginCatalog) CancelResolve() {
	c.mu.Lock()
	cancel := c.resolveCancel
	c.resolveCancel = nil
	c.resolveGen++
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (c *PluginCatalog) ResolveRepository(raw string, onDone func(plugin.CatalogItem, error)) {
	if _, ok := plugin.ParseRepository(raw); !ok {
		if onDone != nil {
			onDone(plugin.CatalogItem{}, plugin.ErrInvalidRepository)
		}
		return
	}
	c.mu.Lock()
	if c.fetcher == nil {
		c.mu.Unlock()
		if onDone != nil {
			onDone(plugin.CatalogItem{}, errors.New("plugin catalog unavailable"))
		}
		return
	}
	if c.resolveCancel != nil {
		c.resolveCancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.resolveCancel = cancel
	c.resolveGen++
	gen := c.resolveGen
	c.mu.Unlock()

	go func() {
		defer cancel()
		item, err := c.fetcher.ResolveRepository(ctx, raw)
		c.post(func() {
			c.mu.Lock()
			if c.resolveGen != gen {
				c.mu.Unlock()
				return
			}
			c.resolveCancel = nil
			c.mu.Unlock()
			if err == nil {
				c.UpsertCatalogItem(item)
			}
			if onDone != nil {
				onDone(item, err)
			}
			c.notify()
		})
	}()
}

func (c *PluginCatalog) CancelInstall() {
	c.mu.Lock()
	cancel := c.installCancel
	c.installCancel = nil
	c.installGen++
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (c *PluginCatalog) Install(item plugin.CatalogItem, onDone func(PluginInstallOutcome)) {
	if err := c.CheckInstallable(item); err != nil {
		if onDone != nil {
			onDone(PluginInstallOutcome{Err: err, Retryable: false})
		}
		return
	}
	c.mu.Lock()
	if c.fetcher == nil || c.pkgStore == nil {
		c.mu.Unlock()
		if onDone != nil {
			onDone(PluginInstallOutcome{Err: errors.New("plugin installer unavailable"), Retryable: false})
		}
		return
	}
	if c.installCancel != nil {
		c.installCancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.installCancel = cancel
	c.installGen++
	gen := c.installGen
	store := c.pkgStore
	appVersion := c.appVersion
	onMounted := c.onMounted
	bound := make(map[string]string, len(c.installed))
	for _, existing := range c.installed {
		normID := strings.ToLower(strings.TrimSpace(existing.ID))
		if normID == "" {
			continue
		}
		if existing.Bundled {
			bound[normID] = ""
		} else {
			bound[normID] = strings.ToLower(strings.TrimSpace(existing.Repository))
		}
	}
	c.mu.Unlock()

	go func() {
		defer cancel()
		data, _, err := c.fetcher.DownloadAsset(ctx, item.Release)
		if err != nil {
			canceled := errors.Is(err, context.Canceled) || ctx.Err() != nil
			c.finishInstall(gen, PluginInstallOutcome{
				Err:       err,
				Canceled:  canceled,
				Retryable: !errors.Is(err, plugin.ErrDigestMismatch) && !canceled,
			}, onDone)
			return
		}
		src, receipt, err := store.StageAndInstall(ctx, data, item, appVersion, bound)
		if err != nil {
			canceled := errors.Is(err, context.Canceled) || ctx.Err() != nil
			retryable := !errors.Is(err, plugin.ErrIncompatible) &&
				!errors.Is(err, plugin.ErrSourceConflict) &&
				!errors.Is(err, plugin.ErrDigestMismatch) &&
				!errors.Is(err, plugin.ErrVersionTagMismatch) &&
				!canceled
			c.finishInstall(gen, PluginInstallOutcome{
				Err:       err,
				Canceled:  canceled,
				Retryable: retryable,
			}, onDone)
			return
		}
		c.post(func() {
			c.mu.Lock()
			if c.installGen != gen {
				c.mu.Unlock()
				return
			}
			c.installCancel = nil
			c.mu.Unlock()

			if onMounted != nil {
				if err := onMounted(src, receipt); err != nil {
					if onDone != nil {
						onDone(PluginInstallOutcome{Err: err, Retryable: false})
					}
					c.notify()
					return
				}
			}

			c.mu.Lock()
			installed := InstalledPlugin{
				ID:           receipt.ID,
				Name:         receipt.Name,
				Description:  receipt.Description,
				Kind:         receipt.Kind,
				Version:      receipt.Version,
				Repository:   receipt.Repository,
				ReleaseID:    receipt.ReleaseID,
				TagName:      receipt.TagName,
				AssetID:      receipt.AssetID,
				SHA256:       receipt.SHA256,
				Bundled:      false,
				Enabled:      receipt.Enabled,
				License:      receipt.License,
				Permissions:  append([]string(nil), receipt.Permissions...),
				Capabilities: src.Capabilities(),
			}
			replaced := false
			for i, existing := range c.installed {
				if strings.EqualFold(existing.ID, installed.ID) {
					c.installed[i] = installed
					replaced = true
					break
				}
			}
			if !replaced {
				c.installed = append(c.installed, installed)
			}
			c.mu.Unlock()

			if onDone != nil {
				onDone(PluginInstallOutcome{Receipt: receipt})
			}
			c.notify()
		})
	}()
}

func (c *PluginCatalog) finishInstall(gen uint64, outcome PluginInstallOutcome, onDone func(PluginInstallOutcome)) {
	c.post(func() {
		c.mu.Lock()
		if c.installGen != gen {
			c.mu.Unlock()
			return
		}
		c.installCancel = nil
		c.mu.Unlock()
		if onDone != nil {
			onDone(outcome)
		}
		c.notify()
	})
}

func (c *PluginCatalog) applyFetchErrorLocked(err error) {
	c.lastErr = err.Error()
	var rlErr *plugin.RateLimitError
	if errors.As(err, &rlErr) {
		c.rateLimitResetAt = rlErr.ResetAt
		c.status = PluginCatalogRateLimited
		return
	}
	if len(c.items) > 0 {
		c.status = PluginCatalogOffline
		return
	}
	c.status = PluginCatalogFailed
}

func (c *PluginCatalog) cacheDataLocked() PluginCatalogCacheData {
	return PluginCatalogCacheData{
		CachedAt:   c.cachedAt,
		ETag:       c.etag,
		Page:       c.page,
		TotalCount: c.totalCount,
		HasMore:    c.hasMore,
		Items:      append([]plugin.CatalogItem(nil), c.items...),
	}
}

func (c *PluginCatalog) saveCacheAsync(data PluginCatalogCacheData) {
	if c.cache == nil {
		return
	}
	go func() {
		c.cacheMu.Lock()
		defer c.cacheMu.Unlock()
		_ = c.cache.Save(data)
	}()
}

func (c *PluginCatalog) notify() {
	if c.invalidate != nil {
		c.invalidate()
	}
}

func deduplicateCatalogItems(existing, incoming []plugin.CatalogItem) []plugin.CatalogItem {
	out := make([]plugin.CatalogItem, 0, len(existing)+len(incoming))
	seenRepo := make(map[string]bool, len(existing)+len(incoming))
	seenRepoID := make(map[int64]bool, len(existing)+len(incoming))
	add := func(item plugin.CatalogItem) {
		repo := strings.ToLower(strings.TrimSpace(item.Repository))
		if repo == "" || item.Manifest.ID == "" {
			return
		}
		if seenRepo[repo] || (item.RepoID > 0 && seenRepoID[item.RepoID]) {
			return
		}
		seenRepo[repo] = true
		if item.RepoID > 0 {
			seenRepoID[item.RepoID] = true
		}
		out = append(out, item)
	}
	for _, item := range existing {
		add(item)
	}
	for _, item := range incoming {
		add(item)
	}
	return out
}
