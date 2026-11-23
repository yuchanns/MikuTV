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

type PluginUpdateOffer struct {
	Item               plugin.CatalogItem
	AddedPermissions   []string
	IncompatibleKind   bool
	IncompatibleReason plugin.CompatibilityCode
	RequiredAppVersion string
	CheckedAt          time.Time
}

func (o *PluginUpdateOffer) HasPermissionExpansion() bool {
	return o != nil && len(o.AddedPermissions) > 0
}

func (o *PluginUpdateOffer) Applicable() bool {
	return o != nil && !o.IncompatibleKind && o.Item.Compatibility.Compatible
}

type InstalledPlugin struct {
	ID              string
	Name            string
	Description     string
	Kind            plugin.Kind
	Version         string
	Repository      string
	ReleaseID       int64
	TagName         string
	AssetID         int64
	SHA256          string
	Bundled         bool
	Enabled         bool
	InstalledAt     int64
	UpdatedAt       int64
	License         string
	SourceCommit    string
	Permissions     []string
	Capabilities    provider.Capabilities
	AvailableUpdate *PluginUpdateOffer
}

type PluginInstallOutcome struct {
	Receipt   plugin.InstalledReceipt
	Err       error
	Canceled  bool
	Retryable bool
}

type PluginUpdateOutcome struct {
	Receipt   plugin.InstalledReceipt
	Err       error
	Canceled  bool
	Retryable bool
}

// PluginUpdateHooks dispatches post-commit lifecycle actions by plugin kind so
// theme updates never pass through content-source playback/session teardown.
type PluginUpdateHooks struct {
	BeforeSourceUpdate func(pluginID string)
	ApplySourceUpdate  func(src *plugin.Source, receipt plugin.InstalledReceipt) error
	ApplyThemeUpdate   func(receipt plugin.InstalledReceipt, data []byte) error
}

// PluginLifecycleHooks dispatches disable, enable, and uninstall callbacks by
// plugin kind so theme removal/disable never clears content-source sessions or
// account data.
type PluginLifecycleHooks struct {
	DisableSource func(pluginID string)
	EnableSource  func(pluginID string) error
	RemoveSource  func(pluginID string, clearData bool)
	DisableTheme  func(pluginID string)
	RemoveTheme   func(pluginID string)
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
	CheckingUpdates  bool
	UpdatesCheckedAt time.Time
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
	fetcher        PluginCatalogFetcher
	cache          PluginCatalogCache
	pkgStore       *plugin.PackageStore
	appVersion     string
	onMounted      func(*plugin.Source, plugin.InstalledReceipt) error
	updateHooks    PluginUpdateHooks
	lifecycleHooks PluginLifecycleHooks
	post           func(func())
	invalidate     func()

	mu                sync.Mutex
	cacheMu           sync.Mutex
	installed         []InstalledPlugin
	status            PluginCatalogStatus
	loadingMore       bool
	checkingUpdates   bool
	updatesCheckedAt  time.Time
	items             []plugin.CatalogItem
	etag              string
	cachedAt          int64
	page              int
	totalCount        int
	hasMore           bool
	rateLimitResetAt  time.Time
	lastErr           string
	gen               uint64
	cancel            context.CancelFunc
	resolveGen        uint64
	resolveCancel     context.CancelFunc
	installGen        uint64
	installCancel     context.CancelFunc
	updateCheckGen    uint64
	updateCheckCancel context.CancelFunc
	updateGen         uint64
	updateCancel      context.CancelFunc
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

func (c *PluginCatalog) SetUpdateHooks(hooks PluginUpdateHooks) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.updateHooks = hooks
}

func (c *PluginCatalog) SetLifecycleHooks(hooks PluginLifecycleHooks) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lifecycleHooks = hooks
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
		CheckingUpdates:  c.checkingUpdates,
		UpdatesCheckedAt: c.updatesCheckedAt,
		HasCache:         len(c.items) > 0,
		CachedAt:         cachedAt,
		RateLimitResetAt: c.rateLimitResetAt,
		Page:             c.page,
		TotalCount:       c.totalCount,
		HasMore:          c.hasMore,
		Installed:        cloneInstalledPlugins(c.installed),
		Items:            append([]plugin.CatalogItem(nil), c.items...),
		Error:            c.lastErr,
	}
}

func cloneInstalledPlugins(src []InstalledPlugin) []InstalledPlugin {
	out := make([]InstalledPlugin, len(src))
	for i, item := range src {
		item.Permissions = append([]string(nil), item.Permissions...)
		if item.AvailableUpdate != nil {
			offer := *item.AvailableUpdate
			offer.AddedPermissions = append([]string(nil), offer.AddedPermissions...)
			item.AvailableUpdate = &offer
		}
		out[i] = item
	}
	return out
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
	updateCheckCancel := c.updateCheckCancel
	updateCancel := c.updateCancel
	c.cancel = nil
	c.resolveCancel = nil
	c.installCancel = nil
	c.updateCheckCancel = nil
	c.updateCancel = nil
	c.gen++
	c.resolveGen++
	c.installGen++
	c.updateCheckGen++
	c.updateGen++
	c.loadingMore = false
	c.checkingUpdates = false
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
	if updateCheckCancel != nil {
		updateCheckCancel()
	}
	if updateCancel != nil {
		updateCancel()
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
				InstalledAt:  receipt.InstalledAt,
				UpdatedAt:    receipt.UpdatedAt,
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

func (c *PluginCatalog) CancelUpdateCheck() {
	c.mu.Lock()
	cancel := c.updateCheckCancel
	c.updateCheckCancel = nil
	c.updateCheckGen++
	c.checkingUpdates = false
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (c *PluginCatalog) CancelUpdate() {
	c.mu.Lock()
	cancel := c.updateCancel
	c.updateCancel = nil
	c.updateGen++
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func evaluatePluginUpdate(inst InstalledPlugin, resolved plugin.CatalogItem, now time.Time) *PluginUpdateOffer {
	if plugin.CompareVersions(inst.Version, resolved.Manifest.Version) <= 0 {
		return nil
	}
	offer := &PluginUpdateOffer{
		Item:               resolved,
		AddedPermissions:   plugin.AddedPermissions(inst.Permissions, resolved.Permissions),
		IncompatibleKind:   resolved.Manifest.Kind != inst.Kind,
		IncompatibleReason: resolved.Compatibility.Code,
		RequiredAppVersion: resolved.Compatibility.RequiredAppVersion,
		CheckedAt:          now,
	}
	return offer
}

// CheckUpdates checks official Releases from each non-bundled plugin's original
// repository and records any newer version offer without changing installed state.
func (c *PluginCatalog) CheckUpdates(onDone func(found int, checked int, err error)) {
	c.checkUpdatesFor("", func(found, checked int, _ *PluginUpdateOffer, err error) {
		if onDone != nil {
			onDone(found, checked, err)
		}
	})
}

// CheckPluginUpdate checks a single non-bundled installed plugin against its
// recorded original repository.
func (c *PluginCatalog) CheckPluginUpdate(pluginID string, onDone func(*PluginUpdateOffer, error)) {
	c.checkUpdatesFor(pluginID, func(_, _ int, offer *PluginUpdateOffer, err error) {
		if onDone != nil {
			onDone(offer, err)
		}
	})
}

func (c *PluginCatalog) checkUpdatesFor(onlyID string, onDone func(found int, checked int, single *PluginUpdateOffer, err error)) {
	c.mu.Lock()
	if c.fetcher == nil {
		c.mu.Unlock()
		if onDone != nil {
			onDone(0, 0, nil, errors.New("plugin catalog unavailable"))
		}
		return
	}
	var targets []InstalledPlugin
	for _, inst := range c.installed {
		if inst.Bundled || strings.TrimSpace(inst.Repository) == "" {
			continue
		}
		if onlyID != "" && !strings.EqualFold(inst.ID, onlyID) {
			continue
		}
		targets = append(targets, inst)
	}
	if len(targets) == 0 {
		c.mu.Unlock()
		if onDone != nil {
			onDone(0, 0, nil, nil)
		}
		return
	}
	if c.updateCheckCancel != nil {
		c.updateCheckCancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.updateCheckCancel = cancel
	c.updateCheckGen++
	gen := c.updateCheckGen
	c.checkingUpdates = true
	c.mu.Unlock()
	c.notify()

	go func() {
		defer cancel()
		type checkResult struct {
			id    string
			offer *PluginUpdateOffer
			err   error
		}
		results := make([]checkResult, len(targets))
		now := time.Now()
		for i, inst := range targets {
			if ctx.Err() != nil {
				return
			}
			resolved, err := c.fetcher.ResolveRepository(ctx, inst.Repository)
			if err != nil {
				results[i] = checkResult{id: inst.ID, err: err}
				continue
			}
			if !strings.EqualFold(resolved.Manifest.ID, inst.ID) {
				results[i] = checkResult{id: inst.ID, err: fmt.Errorf("%w: %s", plugin.ErrSourceConflict, resolved.Manifest.ID)}
				continue
			}
			results[i] = checkResult{
				id:    inst.ID,
				offer: evaluatePluginUpdate(inst, resolved, now),
			}
		}
		if ctx.Err() != nil {
			return
		}
		c.post(func() {
			c.mu.Lock()
			if c.updateCheckGen != gen {
				c.mu.Unlock()
				return
			}
			c.updateCheckCancel = nil
			c.checkingUpdates = false
			c.updatesCheckedAt = now

			var firstErr error
			found := 0
			var singleOffer *PluginUpdateOffer
			for _, res := range results {
				if res.err != nil {
					if firstErr == nil {
						firstErr = res.err
					}
					continue
				}
				for i := range c.installed {
					if strings.EqualFold(c.installed[i].ID, res.id) {
						c.installed[i].AvailableUpdate = res.offer
						if res.offer != nil {
							found++
							if onlyID != "" && strings.EqualFold(res.id, onlyID) {
								copied := *res.offer
								copied.AddedPermissions = append([]string(nil), res.offer.AddedPermissions...)
								singleOffer = &copied
							}
						}
						break
					}
				}
			}
			c.mu.Unlock()
			if onDone != nil {
				onDone(found, len(targets), singleOffer, firstErr)
			}
			c.notify()
		})
	}()
}

// Update downloads and applies the pinned update offer for an installed external
// plugin. On failure or cancellation, the existing package and runtime remain intact.
func (c *PluginCatalog) Update(pluginID string, offer PluginUpdateOffer, onDone func(PluginUpdateOutcome)) {
	c.mu.Lock()
	var existing InstalledPlugin
	found := false
	for _, inst := range c.installed {
		if strings.EqualFold(inst.ID, pluginID) {
			existing = inst
			found = true
			break
		}
	}
	if !found || existing.Bundled {
		c.mu.Unlock()
		if onDone != nil {
			onDone(PluginUpdateOutcome{Err: plugin.ErrIncompatible, Retryable: false})
		}
		return
	}
	if offer.IncompatibleKind || offer.Item.Manifest.Kind != existing.Kind {
		c.mu.Unlock()
		if onDone != nil {
			onDone(PluginUpdateOutcome{Err: plugin.ErrKindChanged, Retryable: false})
		}
		return
	}
	if !offer.Item.Compatibility.Compatible {
		c.mu.Unlock()
		if onDone != nil {
			onDone(PluginUpdateOutcome{Err: plugin.ErrIncompatible, Retryable: false})
		}
		return
	}
	if !strings.EqualFold(strings.TrimSpace(existing.Repository), strings.TrimSpace(offer.Item.Repository)) {
		c.mu.Unlock()
		if onDone != nil {
			onDone(PluginUpdateOutcome{Err: plugin.ErrSourceConflict, Retryable: false})
		}
		return
	}
	if c.fetcher == nil || c.pkgStore == nil {
		c.mu.Unlock()
		if onDone != nil {
			onDone(PluginUpdateOutcome{Err: errors.New("plugin updater unavailable"), Retryable: false})
		}
		return
	}
	if c.updateCancel != nil {
		c.updateCancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.updateCancel = cancel
	c.updateGen++
	gen := c.updateGen
	store := c.pkgStore
	appVersion := c.appVersion
	hooks := c.updateHooks
	existingReceipt := plugin.InstalledReceipt{
		ID:          existing.ID,
		Name:        existing.Name,
		Description: existing.Description,
		Kind:        existing.Kind,
		Version:     existing.Version,
		Repository:  existing.Repository,
		ReleaseID:   existing.ReleaseID,
		TagName:     existing.TagName,
		AssetID:     existing.AssetID,
		SHA256:      existing.SHA256,
		Enabled:     existing.Enabled,
		InstalledAt: existing.InstalledAt,
		UpdatedAt:   existing.UpdatedAt,
		License:     existing.License,
		Permissions: append([]string(nil), existing.Permissions...),
	}
	c.mu.Unlock()

	go func() {
		defer cancel()
		data, _, err := c.fetcher.DownloadAsset(ctx, offer.Item.Release)
		if err != nil {
			canceled := errors.Is(err, context.Canceled) || ctx.Err() != nil
			c.finishUpdate(gen, PluginUpdateOutcome{
				Err:       err,
				Canceled:  canceled,
				Retryable: !errors.Is(err, plugin.ErrDigestMismatch) && !canceled,
			}, onDone)
			return
		}
		src, receipt, err := store.StageAndUpdate(ctx, data, existingReceipt, offer.Item, appVersion)
		if err != nil {
			canceled := errors.Is(err, context.Canceled) || ctx.Err() != nil
			retryable := !errors.Is(err, plugin.ErrKindChanged) &&
				!errors.Is(err, plugin.ErrIncompatible) &&
				!errors.Is(err, plugin.ErrSourceConflict) &&
				!errors.Is(err, plugin.ErrDigestMismatch) &&
				!errors.Is(err, plugin.ErrVersionTagMismatch) &&
				!canceled
			c.finishUpdate(gen, PluginUpdateOutcome{
				Err:       err,
				Canceled:  canceled,
				Retryable: retryable,
			}, onDone)
			return
		}

		c.post(func() {
			c.mu.Lock()
			if c.updateGen != gen {
				c.mu.Unlock()
				return
			}
			c.updateCancel = nil
			c.mu.Unlock()

			if err := dispatchPackageUpdate(hooks, receipt, src, data); err != nil {
				if onDone != nil {
					onDone(PluginUpdateOutcome{Err: err, Retryable: false})
				}
				c.notify()
				return
			}

			c.mu.Lock()
			for i, item := range c.installed {
				if strings.EqualFold(item.ID, receipt.ID) {
					updated := InstalledPlugin{
						ID:              receipt.ID,
						Name:            receipt.Name,
						Description:     receipt.Description,
						Kind:            receipt.Kind,
						Version:         receipt.Version,
						Repository:      receipt.Repository,
						ReleaseID:       receipt.ReleaseID,
						TagName:         receipt.TagName,
						AssetID:         receipt.AssetID,
						SHA256:          receipt.SHA256,
						Bundled:         false,
						Enabled:         receipt.Enabled,
						InstalledAt:     receipt.InstalledAt,
						UpdatedAt:       receipt.UpdatedAt,
						License:         receipt.License,
						Permissions:     append([]string(nil), receipt.Permissions...),
						AvailableUpdate: nil,
					}
					if src != nil {
						updated.Capabilities = src.Capabilities()
					}
					c.installed[i] = updated
					break
				}
			}
			c.mu.Unlock()

			if onDone != nil {
				onDone(PluginUpdateOutcome{Receipt: receipt})
			}
			c.notify()
		})
	}()
}

// dispatchPackageUpdate routes committed package updates strictly by plugin kind
// so theme updates never trigger content-source playback/session teardown.
func dispatchPackageUpdate(hooks PluginUpdateHooks, receipt plugin.InstalledReceipt, src *plugin.Source, data []byte) error {
	switch receipt.Kind {
	case plugin.SourceKind:
		if hooks.BeforeSourceUpdate != nil {
			hooks.BeforeSourceUpdate(receipt.ID)
		}
		if hooks.ApplySourceUpdate != nil {
			return hooks.ApplySourceUpdate(src, receipt)
		}
		return nil
	case plugin.ThemeKind:
		if hooks.ApplyThemeUpdate != nil {
			return hooks.ApplyThemeUpdate(receipt, data)
		}
		return nil
	default:
		return fmt.Errorf("%w: unsupported kind %q", plugin.ErrIncompatible, receipt.Kind)
	}
}

func (c *PluginCatalog) finishUpdate(gen uint64, outcome PluginUpdateOutcome, onDone func(PluginUpdateOutcome)) {
	c.post(func() {
		c.mu.Lock()
		if c.updateGen != gen {
			c.mu.Unlock()
			return
		}
		c.updateCancel = nil
		c.mu.Unlock()
		if onDone != nil {
			onDone(outcome)
		}
		c.notify()
	})
}

// SetEnabled enables or disables an installed plugin (including bundled
// content sources), persists the change, and invokes kind-specific lifecycle hooks.
func (c *PluginCatalog) SetEnabled(pluginID string, enabled bool) error {
	c.mu.Lock()
	idx := -1
	for i, item := range c.installed {
		if strings.EqualFold(item.ID, pluginID) {
			idx = i
			break
		}
	}
	if idx < 0 {
		c.mu.Unlock()
		return fmt.Errorf("plugin %q is not installed", pluginID)
	}
	item := c.installed[idx]
	if item.Enabled == enabled {
		c.mu.Unlock()
		return nil
	}
	store := c.pkgStore
	hooks := c.lifecycleHooks
	c.mu.Unlock()

	if store != nil {
		var err error
		if item.Bundled {
			err = store.SetBundledEnabled(item.ID, enabled)
		} else {
			_, err = store.SetExternalEnabled(item.ID, enabled)
		}
		if err != nil {
			return err
		}
	}

	if err := dispatchPluginToggle(hooks, item.Kind, item.ID, enabled); err != nil {
		// Roll back persisted toggle state if re-enabling failed.
		if store != nil {
			if item.Bundled {
				_ = store.SetBundledEnabled(item.ID, !enabled)
			} else {
				_, _ = store.SetExternalEnabled(item.ID, !enabled)
			}
		}
		return err
	}

	c.mu.Lock()
	for i := range c.installed {
		if strings.EqualFold(c.installed[i].ID, item.ID) {
			c.installed[i].Enabled = enabled
			break
		}
	}
	c.mu.Unlock()
	c.notify()
	return nil
}

// Remove uninstalls an externally installed plugin, deletes its persisted
// package and receipt, and dispatches kind-specific teardown. Bundled plugins
// cannot be uninstalled and must use SetEnabled(id, false) instead.
func (c *PluginCatalog) Remove(pluginID string, clearData bool) error {
	c.mu.Lock()
	idx := -1
	for i, item := range c.installed {
		if strings.EqualFold(item.ID, pluginID) {
			idx = i
			break
		}
	}
	if idx < 0 {
		c.mu.Unlock()
		return fmt.Errorf("plugin %q is not installed", pluginID)
	}
	item := c.installed[idx]
	if item.Bundled {
		c.mu.Unlock()
		return fmt.Errorf("%w: bundled plugin cannot be uninstalled", plugin.ErrIncompatible)
	}
	store := c.pkgStore
	hooks := c.lifecycleHooks
	c.mu.Unlock()

	if store != nil {
		if _, err := store.Remove(item.ID); err != nil {
			return err
		}
	}

	c.mu.Lock()
	next := make([]InstalledPlugin, 0, len(c.installed)-1)
	for _, existing := range c.installed {
		if strings.EqualFold(existing.ID, item.ID) {
			continue
		}
		next = append(next, existing)
	}
	c.installed = next
	c.mu.Unlock()

	dispatchPluginRemove(hooks, item.Kind, item.ID, clearData)
	c.notify()
	return nil
}

func dispatchPluginToggle(hooks PluginLifecycleHooks, kind plugin.Kind, pluginID string, enabled bool) error {
	switch kind {
	case plugin.SourceKind:
		if enabled {
			if hooks.EnableSource != nil {
				return hooks.EnableSource(pluginID)
			}
			return nil
		}
		if hooks.DisableSource != nil {
			hooks.DisableSource(pluginID)
		}
		return nil
	case plugin.ThemeKind:
		if !enabled && hooks.DisableTheme != nil {
			hooks.DisableTheme(pluginID)
		}
		return nil
	default:
		return fmt.Errorf("%w: unsupported kind %q", plugin.ErrIncompatible, kind)
	}
}

func dispatchPluginRemove(hooks PluginLifecycleHooks, kind plugin.Kind, pluginID string, clearData bool) {
	switch kind {
	case plugin.SourceKind:
		if hooks.RemoveSource != nil {
			hooks.RemoveSource(pluginID, clearData)
		}
	case plugin.ThemeKind:
		if hooks.RemoveTheme != nil {
			hooks.RemoveTheme(pluginID)
		}
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
