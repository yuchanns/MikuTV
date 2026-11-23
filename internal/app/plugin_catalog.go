package app

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/yuchanns/MikuTV/internal/plugin"
	"github.com/yuchanns/MikuTV/internal/provider"
)

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
	Bundled      bool
	Enabled      bool
	License      string
	SourceCommit string
	Permissions  []string
	Capabilities provider.Capabilities
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
	c.cancel = nil
	c.gen++
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
