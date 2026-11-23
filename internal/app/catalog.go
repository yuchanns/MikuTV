package app

import (
	"context"
	"slices"

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
	Phase         CatalogPhase
	Page          provider.CollectionPage
	PageRevision  uint64
	FeedRevision  uint64
	Error         provider.ErrorCode
	Appending     bool
	AppendStalled bool
}

func (s CatalogSnapshot) CanLoadMore() bool {
	return s.Phase == CatalogReady && s.Page.NextCursor != "" && !s.AppendStalled
}

// Catalog is owned by the application thread. Only the session call runs
// in a worker, and its result reenters through post before reading state.
type Catalog struct {
	open       func(string) provider.Session
	post       func(func())
	changed    func()
	entryID    string
	entryGen   uint64
	sessionGen uint64
	session    provider.Session
	canBrowse  bool
	canReport  bool
	collection string
	filter     string
	pending    context.CancelFunc
	request    provider.BrowseRequest
	snapshot   CatalogSnapshot

	artwork, backdrops *catalogArtwork
}

func (c *Catalog) CanReportPlayback() bool { return c.canReport }

func NewCatalog(open func(string) provider.Session, post func(func()), changed func()) *Catalog {
	return &Catalog{open: open, post: post, changed: changed}
}

// Snapshot returns a read-only view of the published catalog.
func (c *Catalog) Snapshot() CatalogSnapshot { return c.snapshot }

func (c *Catalog) Authenticator() provider.Authenticator {
	service, _ := c.session.(provider.Authenticator)
	return service
}

func (c *Catalog) AccountReader() provider.AccountReader {
	reader, _ := c.session.(provider.AccountReader)
	return reader
}

func (c *Catalog) PrivateMessageReader() provider.PrivateMessageReader {
	reader, _ := c.session.(provider.PrivateMessageReader)
	return reader
}

func (c *Catalog) PrivateMessageWriter() provider.PrivateMessageWriter {
	writer, _ := c.session.(provider.PrivateMessageWriter)
	return writer
}

func (c *Catalog) Searcher() provider.CatalogSearcher {
	searcher, _ := c.session.(provider.CatalogSearcher)
	return searcher
}

func (c *Catalog) SearchSuggester() provider.CatalogSearchSuggester {
	suggester, _ := c.session.(provider.CatalogSearchSuggester)
	return suggester
}

func (c *Catalog) FollowingReader() provider.FollowingReader {
	reader, _ := c.session.(provider.FollowingReader)
	return reader
}

func (c *Catalog) SeriesReader() provider.SeriesReader {
	reader, _ := c.session.(provider.SeriesReader)
	return reader
}

func (c *Catalog) LiveReader() provider.LiveReader {
	reader, _ := c.session.(provider.LiveReader)
	return reader
}

func (c *Catalog) LiveChatClient() provider.LiveChatClient {
	client, _ := c.session.(provider.LiveChatClient)
	return client
}

func (c *Catalog) SendLiveChat(post provider.LiveChatPost, accept func(provider.LiveChatSendResult, error)) context.CancelFunc {
	return catalogDispatch(c, func(ctx context.Context, writer provider.LiveChatWriter) (provider.LiveChatSendResult, error) {
		return writer.SendLiveChat(ctx, post)
	}, accept)
}

func (c *Catalog) ReadItem(ref provider.MediaRef, accept func(provider.MediaItem, error)) context.CancelFunc {
	if c.session == nil {
		accept(provider.MediaItem{}, &provider.Error{Code: provider.Unsupported})
		return func() {}
	}
	return c.readItem(ref, c.session.Item, accept)
}

func (c *Catalog) ReadVideoDetail(ref provider.MediaRef, accept func(provider.MediaItem, error)) context.CancelFunc {
	session, ok := c.session.(provider.VideoDetailReader)
	if !ok {
		return c.ReadItem(ref, accept)
	}
	return c.readItem(ref, session.VideoDetail, accept)
}

func (c *Catalog) readItem(ref provider.MediaRef, read func(context.Context, provider.MediaRef) (provider.MediaItem, error),
	accept func(provider.MediaItem, error),
) context.CancelFunc {
	return catalogResult(c, func(ctx context.Context) (provider.MediaItem, error) {
		return read(ctx, ref)
	}, accept)
}

func (c *Catalog) EditVideo(request provider.VideoEdit, accept func(provider.VideoEditResult, error)) context.CancelFunc {
	return catalogDispatch(c, func(ctx context.Context, editor provider.VideoEditor) (provider.VideoEditResult, error) {
		return editor.EditVideo(ctx, request)
	}, accept)
}

func (c *Catalog) ReadVideoComments(query provider.VideoCommentQuery, accept func(provider.VideoCommentPage, error)) context.CancelFunc {
	return catalogDispatch(c, func(ctx context.Context, reader provider.VideoCommentReader) (provider.VideoCommentPage, error) {
		return reader.VideoComments(ctx, query)
	}, accept)
}

func (c *Catalog) ReadCreator(request provider.CreatorRequest, accept func(provider.CreatorPage, error)) context.CancelFunc {
	return catalogDispatch(c, func(ctx context.Context, reader provider.CreatorReader) (provider.CreatorPage, error) {
		return reader.ReadCreator(ctx, request)
	}, accept)
}

func (c *Catalog) FollowCreator(request provider.CreatorFollowRequest, accept func(provider.CreatorFollowResult, error)) context.CancelFunc {
	return catalogDispatch(c, func(ctx context.Context, follower provider.CreatorFollower) (provider.CreatorFollowResult, error) {
		return follower.FollowCreator(ctx, request)
	}, accept)
}

func (c *Catalog) TrackSeries(request provider.SeriesTrackRequest, accept func(provider.SeriesTrackResult, error)) context.CancelFunc {
	return catalogDispatch(c, func(ctx context.Context, tracker provider.SeriesTracker) (provider.SeriesTrackResult, error) {
		return tracker.TrackSeries(ctx, request)
	}, accept)
}

type pendingCall struct {
	gen    uint64
	cancel context.CancelFunc
}

func (p *pendingCall) stop() bool {
	if p.cancel == nil {
		return false
	}
	p.cancel()
	p.cancel = nil
	return true
}

func catalogDispatch[S any, T any](c *Catalog, fn func(context.Context, S) (T, error), accept func(T, error)) context.CancelFunc {
	source, ok := c.session.(S)
	return catalogResult(c, func(ctx context.Context) (T, error) {
		if !ok {
			var zero T
			return zero, &provider.Error{Code: provider.Unsupported}
		}
		return fn(ctx, source)
	}, accept)
}

func catalogAction[S any](c *Catalog, fn func(context.Context, S) error, accept func(error)) context.CancelFunc {
	return catalogDispatch(c, func(ctx context.Context, source S) (struct{}, error) {
		return struct{}{}, fn(ctx, source)
	}, func(_ struct{}, err error) {
		accept(err)
	})
}

func catalogResult[T any](c *Catalog, operation func(context.Context) (T, error), accept func(T, error)) context.CancelFunc {
	ctx, cancel := context.WithCancel(context.Background())
	sessionGen := c.sessionGen
	go func() {
		result, err := operation(ctx)
		c.post(func() {
			if ctx.Err() != nil || c.sessionGen != sessionGen {
				return
			}
			cancel()
			accept(result, err)
			c.changed()
		})
	}()
	return cancel
}

// Open binds an entry session without starting an initial browse request,
// allowing account restoration to attach credentials before the first fetch.
func (c *Catalog) Open(entry Entry) bool {
	canBrowse := entry.Capabilities.Has(provider.BrowseCatalog)
	canReport := entry.Capabilities.Has(provider.ReportPlayback)
	if c.entryID == entry.ID && c.entryGen == entry.Generation && c.canBrowse == canBrowse && c.canReport == canReport {
		return false
	}
	c.Close()
	c.entryID = entry.ID
	c.entryGen = entry.Generation
	c.canBrowse = canBrowse
	c.canReport = canReport
	if entry.ID != "" {
		c.session = c.open(entry.ID)
	}
	return true
}

func (c *Catalog) Select(entry Entry) {
	if !c.Open(entry) {
		return
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
	c.sessionGen++
	c.clearPage()
	c.browse(c.query(), false)
}

func (c *Catalog) EnsureLoaded() {
	if c.canBrowse && c.snapshot.Phase == CatalogIdle {
		c.Reload()
	}
}

func (c *Catalog) Show(collection, filter string) {
	if c.collection == collection && c.filter == filter {
		return
	}
	c.collection, c.filter = collection, filter
	c.Reload()
}

func (c *Catalog) query() provider.BrowseRequest {
	return provider.BrowseRequest{Collection: c.collection, Filter: c.filter}
}

func (c *Catalog) Refresh() {
	if c.canBrowse {
		request := c.query()
		request.Refresh, request.Cursor = true, c.snapshot.Page.NextCursor
		c.browse(request, false)
	}
}

func (c *Catalog) LoadMore() {
	if c.snapshot.CanLoadMore() {
		request := c.query()
		request.Cursor = c.snapshot.Page.NextCursor
		c.browse(request, true)
	}
}

func (c *Catalog) Retry() {
	if c.snapshot.Phase == CatalogFailed || c.snapshot.Phase == CatalogCanceled {
		c.browse(c.request, c.snapshot.Appending)
	}
}

func (c *Catalog) browse(request provider.BrowseRequest, appendPage bool) {
	c.cancelRequest()
	c.request = request
	c.snapshot.Phase = CatalogLoading
	c.snapshot.Error = ""
	c.snapshot.Appending = appendPage
	c.snapshot.AppendStalled = false
	previous := c.snapshot.Page
	session := c.session
	ctx, cancel := context.WithCancel(context.Background())
	c.pending = cancel
	c.changed()
	go func() {
		page, err := session.Browse(ctx, request)
		c.post(func() {
			if ctx.Err() != nil {
				return
			}
			cancel()
			c.pending = nil
			if err != nil {
				c.snapshot.Phase, c.snapshot.Error = CatalogFailed, provider.CodeOf(err)
			} else {
				c.acceptPage(previous, page, request, appendPage)
			}
			c.changed()
		})
	}()
}

func (c *Catalog) acceptPage(previous, page provider.CollectionPage, request provider.BrowseRequest, appendPage bool) {
	if page.NextCursor == request.Cursor {
		page.NextCursor = ""
	}
	var added bool
	feedRevision := c.snapshot.FeedRevision
	if appendPage {
		page, added = mergeCatalog(previous, page)
	} else {
		feedRevision++
		c.clearArtwork()
		page, _ = mergeCatalog(provider.CollectionPage{}, page)
	}
	c.snapshot = CatalogSnapshot{
		Phase: CatalogReady, Page: page, PageRevision: c.snapshot.PageRevision + 1,
		FeedRevision: feedRevision, AppendStalled: appendPage && !added,
	}
}

func mergeCatalog(previous, next provider.CollectionPage) (provider.CollectionPage, bool) {
	page := provider.CollectionPage{
		Collections: slices.Clone(previous.Collections), NextCursor: next.NextCursor,
		Filters: next.Filters, Shelves: next.Shelves,
	}
	if page.Filters == nil {
		page.Filters = previous.Filters
	}
	if page.Shelves == nil {
		page.Shelves = previous.Shelves
	}
	indices := make(map[string]int, len(page.Collections))
	for index, group := range page.Collections {
		indices[group.ID] = index
	}
	var added bool
	for _, incoming := range next.Collections {
		index, exists := indices[incoming.ID]
		if !exists {
			index = len(page.Collections)
			indices[incoming.ID] = index
			page.Collections = append(page.Collections, provider.Collection{ID: incoming.ID, Title: incoming.Title})
		}
		group := &page.Collections[index]
		seen := make(map[provider.MediaRef]struct{}, len(group.Items)+len(incoming.Items))
		for _, item := range group.Items {
			seen[item.Ref] = struct{}{}
		}
		for _, item := range incoming.Items {
			if _, duplicate := seen[item.Ref]; duplicate {
				continue
			}
			seen[item.Ref] = struct{}{}
			group.Items = append(group.Items, item)
			added = true
		}
	}
	return page, added
}

func (c *Catalog) Cancel() {
	if c.cancelRequest() {
		c.snapshot.Phase = CatalogCanceled
		c.changed()
	}
}

func (c *Catalog) Close() {
	c.sessionGen++
	c.cancelRequest()
	c.clearPage()
	if c.session != nil {
		c.session.Close()
		c.session = nil
	}
	c.entryID = ""
	c.entryGen = 0
	c.canBrowse = false
	c.canReport = false
	c.collection, c.filter = "", ""
}

func (c *Catalog) clearPage() {
	c.clearArtwork()
	c.snapshot = CatalogSnapshot{PageRevision: c.snapshot.PageRevision + 1, FeedRevision: c.snapshot.FeedRevision + 1}
}

func (c *Catalog) cancelRequest() bool {
	if c.pending == nil {
		return false
	}
	c.pending()
	c.pending = nil
	return true
}
