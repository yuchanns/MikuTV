package plugin

import (
	"context"
	"strings"

	"github.com/yuchanns/MikuTV/internal/provider"
)

const (
	maxSearchQueryLength  = 512
	maxSearchCursorLength = 4096
	maxSearchHotKeywords  = 20
	maxSearchSuggestions  = 12
)

func (s *Session) Search(ctx context.Context, request provider.SearchRequest) (provider.SearchPage, error) {
	if !request.Kind.Valid() || !request.Sort.Valid() || !request.Duration.Valid() ||
		len(request.Query) > maxSearchQueryLength || len(request.Cursor) > maxSearchCursorLength {
		return provider.SearchPage{}, &provider.Error{Code: provider.Denied}
	}
	s.requests.RLock()
	client, scope, signedIn := s.active, s.scope, s.accountID != ""
	s.requests.RUnlock()
	input := struct {
		provider.SearchRequest
		SignedIn bool `json:"signedIn,omitempty"`
	}{request, signedIn}
	var page provider.SearchPage
	if err := s.invoke(ctx, scope, client, provider.SearchCatalog, "search", input, &page); err != nil {
		return provider.SearchPage{}, err
	}
	if len(page.NextCursor) > maxSearchCursorLength || page.Total != nil && *page.Total < 0 {
		return provider.SearchPage{}, &provider.Error{Code: provider.PluginFailed}
	}
	for index := range page.Items {
		if page.Items[index].VideoDetails != nil {
			return provider.SearchPage{}, &provider.Error{Code: provider.PluginFailed}
		}
		if err := s.ownItem(&page.Items[index]); err != nil {
			return provider.SearchPage{}, err
		}
	}
	for index := range page.Creators {
		creator := &page.Creators[index]
		if creator.Creator.ID == "" || strings.TrimSpace(creator.Creator.Name) == "" {
			return provider.SearchPage{}, &provider.Error{Code: provider.PluginFailed}
		}
		if creator.Followers != nil && *creator.Followers < 0 || creator.Videos != nil && *creator.Videos < 0 {
			return provider.SearchPage{}, &provider.Error{Code: provider.PluginFailed}
		}
		if creator.Creator.AvatarURL != "" && httpsURL(creator.Creator.AvatarURL) == nil {
			creator.Creator.AvatarURL = ""
		}
	}
	page.HotKeywords = sanitizeKeywords(page.HotKeywords, maxSearchHotKeywords)
	if len(page.Kinds) != 0 {
		kinds := make([]provider.SearchKind, 0, len(page.Kinds))
		seen := make(map[provider.SearchKind]bool, len(page.Kinds))
		for _, kind := range page.Kinds {
			if kind == "" || !kind.Valid() {
				return provider.SearchPage{}, &provider.Error{Code: provider.PluginFailed}
			}
			if !seen[kind] {
				seen[kind] = true
				kinds = append(kinds, kind)
			}
		}
		page.Kinds = kinds
	}
	return page, nil
}

func (s *Session) SearchSuggest(ctx context.Context, query string) (provider.SearchSuggestResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return provider.SearchSuggestResult{}, nil
	}
	if len(query) > maxSearchQueryLength {
		return provider.SearchSuggestResult{}, &provider.Error{Code: provider.Denied}
	}
	s.requests.RLock()
	client, scope := s.active, s.scope
	s.requests.RUnlock()
	input := struct {
		Query string `json:"query"`
	}{Query: query}
	var result provider.SearchSuggestResult
	if err := s.invoke(ctx, scope, client, provider.SearchSuggest, "searchSuggest", input, &result); err != nil {
		return provider.SearchSuggestResult{}, err
	}
	result.Suggestions = sanitizeKeywords(result.Suggestions, maxSearchSuggestions)
	return result, nil
}

func sanitizeKeywords(values []string, limit int) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, min(len(values), limit))
	seen := make(map[string]bool, min(len(values), limit))
	for _, raw := range values {
		term := strings.TrimSpace(raw)
		if term == "" || len(term) > maxSearchQueryLength {
			continue
		}
		key := strings.ToLower(term)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, term)
		if len(out) >= limit {
			break
		}
	}
	return out
}
