package app

import (
	"context"

	"github.com/yuchanns/MikuTV/internal/provider"
)

type CatalogPhase uint8

const (
	CatalogIdle CatalogPhase = iota
	CatalogLoading
	CatalogReady
	CatalogFailed
	CatalogCanceled
)

type CatalogSnapshot struct {
	Phase CatalogPhase
	Page  provider.CollectionPage
	Error provider.ErrorCode
}

// Catalog is owned by the application thread. Only the session call runs
// in a worker, and its result reenters through post before reading state.
type Catalog struct {
	open       func(string) provider.Session
	post       func(func())
	changed    func()
	entryID    string
	session    provider.Session
	canBrowse  bool
	generation uint64
	pending    context.CancelFunc
	snapshot   CatalogSnapshot
}

func NewCatalog(open func(string) provider.Session, post func(func()), changed func()) *Catalog {
	return &Catalog{open: open, post: post, changed: changed}
}

func (c *Catalog) Snapshot() CatalogSnapshot { return c.snapshot }

func (c *Catalog) Select(entry Entry) {
	canBrowse := entry.Capabilities.Has(provider.BrowseCatalog)
	if c.entryID == entry.ID && c.canBrowse == canBrowse {
		return
	}
	c.Close()
	c.entryID = entry.ID
	c.canBrowse = canBrowse
	if entry.ID != "" {
		c.session = c.open(entry.ID)
	}
	if c.canBrowse {
		c.Reload()
	} else {
		c.changed()
	}
}

func (c *Catalog) Reload() {
	if !c.canBrowse {
		return
	}
	c.invalidateRequest()
	session := c.session
	if session == nil {
		c.snapshot = CatalogSnapshot{Phase: CatalogFailed, Error: provider.PluginFailed}
		c.changed()
		return
	}
	generation := c.generation
	ctx, cancel := context.WithCancel(context.Background())
	c.pending = cancel
	c.snapshot = CatalogSnapshot{Phase: CatalogLoading}
	c.changed()
	go func() {
		page, err := session.Browse(ctx, provider.BrowseRequest{})
		c.post(func() {
			if generation != c.generation || ctx.Err() != nil {
				return
			}
			cancel()
			c.pending = nil
			if err == nil {
				c.snapshot = CatalogSnapshot{Phase: CatalogReady, Page: page}
			} else {
				c.snapshot = CatalogSnapshot{Phase: CatalogFailed, Error: provider.CodeOf(err)}
			}
			c.changed()
		})
	}()
}

func (c *Catalog) Cancel() {
	if c.pending != nil {
		c.invalidateRequest()
		c.snapshot = CatalogSnapshot{Phase: CatalogCanceled}
		c.changed()
	}
}

func (c *Catalog) Close() {
	c.invalidateRequest()
	if c.session != nil {
		c.session.Close()
		c.session = nil
	}
	c.entryID = ""
	c.canBrowse = false
	c.snapshot = CatalogSnapshot{}
}

func (c *Catalog) invalidateRequest() {
	c.generation++
	if c.pending != nil {
		c.pending()
		c.pending = nil
	}
}
