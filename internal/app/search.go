package app

import (
	"context"
	"slices"
	"strings"

	"github.com/yuchanns/MikuTV/internal/provider"
)

const maxSearchHistory = 10

type SearchHistoryStore interface {
	SearchHistory(entryID, owner string) []string
	SaveSearchHistory(entryID, owner string, queries []string) error
}

type SearchSnapshot struct {
	Phase         CatalogPhase
	Request       provider.SearchRequest
	Page          provider.SearchPage
	PageRevision  uint64
	FeedRevision  uint64
	Error         provider.ErrorCode
	Appending     bool
	AppendStalled bool
	Suggestions   []string
	SuggestQuery  string
	History       []string
	HotKeywords   []string
	Kinds         []provider.SearchKind
}

func (s SearchSnapshot) CanLoadMore() bool {
	return s.Phase == CatalogReady && s.Page.NextCursor != "" && !s.AppendStalled
}

func (s SearchSnapshot) ResultCount() int {
	if s.Request.Kind == provider.SearchCreator {
		return len(s.Page.Creators)
	}
	return len(s.Page.Items)
}

func (s SearchSnapshot) DisplayTotal() int64 {
	if s.Page.Total != nil {
		return *s.Page.Total
	}
	return int64(s.ResultCount())
}

// CatalogSearch is owned by the UI thread. Requests execute in workers and re-enter via post.
type CatalogSearch struct {
	store   SearchHistoryStore
	post    func(func())
	changed func()

	entryID    string
	entryGen   uint64
	owner      string
	canSearch  bool
	canSuggest bool
	searcher   provider.CatalogSearcher
	suggester  provider.CatalogSearchSuggester

	sessionGen     uint64
	searchGen      uint64
	suggestGen     uint64
	pendingSearch  context.CancelFunc
	pendingSuggest context.CancelFunc
	lastRequest    provider.SearchRequest
	snapshot       SearchSnapshot
}

func NewCatalogSearch(store SearchHistoryStore, post func(func()), changed func()) *CatalogSearch {
	s := &CatalogSearch{store: store, post: post, changed: changed}
	s.snapshot.Request = defaultSearchRequest()
	return s
}

func defaultSearchRequest() provider.SearchRequest {
	return provider.SearchRequest{
		Kind:     provider.SearchAll,
		Sort:     provider.SearchSortDefault,
		Duration: provider.SearchDurationAll,
	}
}

func (s *CatalogSearch) Snapshot() SearchSnapshot {
	if s == nil {
		return SearchSnapshot{Request: defaultSearchRequest()}
	}
	return s.snapshot
}

func (s *CatalogSearch) CanSuggest() bool {
	return s != nil && s.canSuggest && s.suggester != nil
}

// Bind attaches the active entry and account session. Switching entry or account
// cancels pending calls, resets page state, and loads that scope's isolated history.
func (s *CatalogSearch) Bind(entry Entry, owner string, searcher provider.CatalogSearcher, suggester provider.CatalogSearchSuggester) bool {
	if s == nil {
		return false
	}
	canSearch := entry.ID != "" && entry.Capabilities.Has(provider.SearchCatalog) && searcher != nil
	canSuggest := canSearch && entry.Capabilities.Has(provider.SearchSuggest) && suggester != nil
	if s.entryID == entry.ID && s.entryGen == entry.Generation && s.owner == owner &&
		s.canSearch == canSearch && s.canSuggest == canSuggest &&
		s.searcher == searcher && s.suggester == suggester {
		return false
	}
	s.sessionGen++
	s.cancelSearch()
	s.cancelSuggest()
	s.entryID, s.entryGen, s.owner = entry.ID, entry.Generation, owner
	s.canSearch, s.canSuggest = canSearch, canSuggest
	s.searcher, s.suggester = searcher, suggester
	var history []string
	if entry.ID != "" && s.store != nil {
		history = slices.Clone(s.store.SearchHistory(entry.ID, owner))
	}
	s.lastRequest = defaultSearchRequest()
	s.snapshot = SearchSnapshot{
		Request:      defaultSearchRequest(),
		PageRevision: s.snapshot.PageRevision + 1,
		FeedRevision: s.snapshot.FeedRevision + 1,
		History:      history,
	}
	s.changed()
	return true
}

func (s *CatalogSearch) EnsureLoaded() {
	if s == nil || !s.canSearch || s.snapshot.Phase != CatalogIdle {
		return
	}
	s.runSearch(s.snapshot.Request, false)
}

func (s *CatalogSearch) Submit(query string) {
	if s == nil || !s.canSearch {
		return
	}
	trimmed := strings.TrimSpace(query)
	if trimmed != "" {
		s.rememberQuery(trimmed)
	}
	req := s.snapshot.Request
	req.Query = trimmed
	req.Cursor = ""
	s.snapshot.Request = req
	s.snapshot.Page = provider.SearchPage{}
	s.snapshot.FeedRevision++
	s.snapshot.PageRevision++
	s.runSearch(req, false)
	if trimmed == "" {
		s.cancelSuggest()
		s.snapshot.Suggestions = nil
		s.snapshot.SuggestQuery = ""
		s.changed()
	} else {
		s.Suggest(trimmed)
	}
}

func (s *CatalogSearch) SelectKind(kind provider.SearchKind) {
	if s == nil || !s.canSearch || !kind.Valid() {
		return
	}
	if kind == "" {
		kind = provider.SearchAll
	}
	if s.snapshot.Request.Kind == kind && s.snapshot.Phase != CatalogIdle && s.snapshot.Phase != CatalogFailed && s.snapshot.Phase != CatalogCanceled {
		return
	}
	req := s.snapshot.Request
	req.Kind = kind
	req.Cursor = ""
	s.snapshot.Request = req
	s.snapshot.Page = provider.SearchPage{}
	s.snapshot.FeedRevision++
	s.snapshot.PageRevision++
	s.runSearch(req, false)
}

func (s *CatalogSearch) ApplyFilters(sort provider.SearchSort, duration provider.SearchDuration) {
	if s == nil || !s.canSearch || !sort.Valid() || !duration.Valid() {
		return
	}
	if sort == "" {
		sort = provider.SearchSortDefault
	}
	if duration == "" {
		duration = provider.SearchDurationAll
	}
	if s.snapshot.Request.Sort == sort && s.snapshot.Request.Duration == duration {
		return
	}
	req := s.snapshot.Request
	req.Sort = sort
	req.Duration = duration
	req.Cursor = ""
	s.snapshot.Request = req
	s.snapshot.Page = provider.SearchPage{}
	s.snapshot.FeedRevision++
	s.snapshot.PageRevision++
	s.runSearch(req, false)
}

func (s *CatalogSearch) LoadMore() {
	if s == nil || !s.canSearch || !s.snapshot.CanLoadMore() {
		return
	}
	req := s.snapshot.Request
	req.Cursor = s.snapshot.Page.NextCursor
	s.runSearch(req, true)
}

func (s *CatalogSearch) Retry() {
	if s == nil || !s.canSearch {
		return
	}
	if s.snapshot.Phase == CatalogFailed || s.snapshot.Phase == CatalogCanceled {
		s.runSearch(s.lastRequest, s.snapshot.Appending)
	}
}

func (s *CatalogSearch) Cancel() {
	if s == nil {
		return
	}
	if s.cancelSearch() {
		s.snapshot.Phase = CatalogCanceled
		s.changed()
	}
}

func (s *CatalogSearch) Suggest(query string) {
	if s == nil {
		return
	}
	trimmed := strings.TrimSpace(query)
	s.cancelSuggest()
	if trimmed == "" || !s.canSuggest || s.suggester == nil {
		if len(s.snapshot.Suggestions) != 0 || s.snapshot.SuggestQuery != "" {
			s.snapshot.Suggestions = nil
			s.snapshot.SuggestQuery = ""
			s.changed()
		}
		return
	}
	s.suggestGen++
	gen := s.suggestGen
	sessionGen := s.sessionGen
	suggester := s.suggester
	ctx, cancel := context.WithCancel(context.Background())
	s.pendingSuggest = cancel
	go func() {
		result, err := suggester.SearchSuggest(ctx, trimmed)
		s.post(func() {
			if ctx.Err() != nil || s.sessionGen != sessionGen || s.suggestGen != gen {
				return
			}
			cancel()
			s.pendingSuggest = nil
			if err != nil {
				return
			}
			s.snapshot.SuggestQuery = trimmed
			s.snapshot.Suggestions = slices.Clone(result.Suggestions)
			s.changed()
		})
	}()
}

func (s *CatalogSearch) MatchesScope(entryID, owner string) bool {
	return s != nil && s.entryID == entryID && s.owner == owner
}

func (s *CatalogSearch) ClearHistory() error {
	if s == nil {
		return nil
	}
	if s.store != nil && s.entryID != "" {
		if err := s.store.SaveSearchHistory(s.entryID, s.owner, nil); err != nil {
			return err
		}
	}
	s.snapshot.History = nil
	s.changed()
	return nil
}

func (s *CatalogSearch) Close() {
	if s == nil {
		return
	}
	s.sessionGen++
	s.cancelSearch()
	s.cancelSuggest()
	s.entryID, s.entryGen, s.owner = "", 0, ""
	s.canSearch, s.canSuggest = false, false
	s.searcher, s.suggester = nil, nil
	s.snapshot = SearchSnapshot{
		Request:      defaultSearchRequest(),
		PageRevision: s.snapshot.PageRevision + 1,
		FeedRevision: s.snapshot.FeedRevision + 1,
	}
}

func (s *CatalogSearch) rememberQuery(query string) {
	next := make([]string, 0, min(len(s.snapshot.History)+1, maxSearchHistory))
	next = append(next, query)
	for _, existing := range s.snapshot.History {
		if existing == query {
			continue
		}
		next = append(next, existing)
		if len(next) >= maxSearchHistory {
			break
		}
	}
	s.snapshot.History = next
	if s.store != nil && s.entryID != "" {
		_ = s.store.SaveSearchHistory(s.entryID, s.owner, next)
	}
}

func (s *CatalogSearch) runSearch(request provider.SearchRequest, appendPage bool) {
	s.cancelSearch()
	s.searchGen++
	gen := s.searchGen
	sessionGen := s.sessionGen
	s.lastRequest = request
	s.snapshot.Phase = CatalogLoading
	s.snapshot.Error = ""
	s.snapshot.Appending = appendPage
	s.snapshot.AppendStalled = false
	previous := s.snapshot.Page
	searcher := s.searcher
	ctx, cancel := context.WithCancel(context.Background())
	s.pendingSearch = cancel
	s.changed()
	go func() {
		page, err := searcher.Search(ctx, request)
		s.post(func() {
			if ctx.Err() != nil || s.sessionGen != sessionGen || s.searchGen != gen {
				return
			}
			cancel()
			s.pendingSearch = nil
			if err != nil {
				s.snapshot.Phase = CatalogFailed
				s.snapshot.Error = provider.CodeOf(err)
			} else {
				s.acceptPage(previous, page, request, appendPage)
			}
			s.changed()
		})
	}()
}

func (s *CatalogSearch) acceptPage(previous, page provider.SearchPage, request provider.SearchRequest, appendPage bool) {
	if page.NextCursor == request.Cursor {
		page.NextCursor = ""
	}
	hotKeywords := s.snapshot.HotKeywords
	if len(page.HotKeywords) != 0 {
		hotKeywords = slices.Clone(page.HotKeywords)
	}
	kinds := s.snapshot.Kinds
	if len(page.Kinds) != 0 {
		kinds = slices.Clone(page.Kinds)
	}
	var added bool
	feedRevision := s.snapshot.FeedRevision
	if appendPage {
		page, added = mergeSearchPage(previous, page)
	} else {
		feedRevision++
		page, _ = mergeSearchPage(provider.SearchPage{}, page)
	}
	stalled := appendPage && !added
	if stalled {
		page.NextCursor = ""
	}
	s.snapshot = SearchSnapshot{
		Phase:         CatalogReady,
		Request:       s.snapshot.Request,
		Page:          page,
		PageRevision:  s.snapshot.PageRevision + 1,
		FeedRevision:  feedRevision,
		AppendStalled: stalled,
		Suggestions:   s.snapshot.Suggestions,
		SuggestQuery:  s.snapshot.SuggestQuery,
		History:       s.snapshot.History,
		HotKeywords:   hotKeywords,
		Kinds:         kinds,
	}
}

func mergeSearchPage(previous, next provider.SearchPage) (provider.SearchPage, bool) {
	merged := provider.SearchPage{
		Items:       slices.Clone(previous.Items),
		Creators:    slices.Clone(previous.Creators),
		Total:       next.Total,
		NextCursor:  next.NextCursor,
		HotKeywords: next.HotKeywords,
		Kinds:       next.Kinds,
	}
	if merged.Total == nil {
		merged.Total = previous.Total
	}
	var added bool
	seenItems := make(map[provider.MediaRef]struct{}, len(merged.Items)+len(next.Items))
	for _, item := range merged.Items {
		seenItems[item.Ref] = struct{}{}
	}
	for _, item := range next.Items {
		if _, dup := seenItems[item.Ref]; dup {
			continue
		}
		seenItems[item.Ref] = struct{}{}
		merged.Items = append(merged.Items, item)
		added = true
	}
	seenCreators := make(map[string]struct{}, len(merged.Creators)+len(next.Creators))
	for _, creator := range merged.Creators {
		seenCreators[creator.Creator.ID] = struct{}{}
	}
	for _, creator := range next.Creators {
		if _, dup := seenCreators[creator.Creator.ID]; dup {
			continue
		}
		seenCreators[creator.Creator.ID] = struct{}{}
		merged.Creators = append(merged.Creators, creator)
		added = true
	}
	return merged, added
}

func (s *CatalogSearch) cancelSearch() bool {
	if s.pendingSearch == nil {
		return false
	}
	s.pendingSearch()
	s.pendingSearch = nil
	return true
}

func (s *CatalogSearch) cancelSuggest() bool {
	if s.pendingSuggest == nil {
		return false
	}
	s.pendingSuggest()
	s.pendingSuggest = nil
	return true
}
