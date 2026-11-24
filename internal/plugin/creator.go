package plugin

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/yuchanns/MikuTV/internal/provider"
)

const (
	maxCreatorIDLength   = 512
	maxCreatorAttributes = 12
)

func (s *Session) ReadCreator(ctx context.Context, request provider.CreatorRequest) (provider.CreatorPage, error) {
	if strings.TrimSpace(request.CreatorID) == "" || len(request.CreatorID) > maxCreatorIDLength ||
		!request.Tab.Valid() || len(request.Cursor) > maxSearchCursorLength {
		return provider.CreatorPage{}, &provider.Error{Code: provider.Denied}
	}
	auth := s.snapshotAuth()
	input := struct {
		provider.CreatorRequest
		SignedIn bool `json:"signedIn,omitempty"`
	}{request, auth.signedIn()}
	var page provider.CreatorPage
	if err := s.invoke(ctx, auth.scope, auth.client, provider.ReadCreator, "readCreator", input, &page); err != nil {
		return provider.CreatorPage{}, err
	}
	if len(page.NextCursor) > maxSearchCursorLength {
		return provider.CreatorPage{}, &provider.Error{Code: provider.PluginFailed}
	}
	if page.Profile != nil {
		if err := sanitizeCreatorProfile(page.Profile, request.CreatorID); err != nil {
			return provider.CreatorPage{}, err
		}
	}
	if err := s.ownSummaryItems(page.Items); err != nil {
		return provider.CreatorPage{}, err
	}
	for index := range page.Posts {
		post := &page.Posts[index]
		if post.ID == "" || len(post.ID) > maxCreatorIDLength || post.PublishedAt < 0 {
			return provider.CreatorPage{}, &provider.Error{Code: provider.PluginFailed}
		}
		if post.ImageURL != "" && httpsURL(post.ImageURL) == nil {
			post.ImageURL = ""
		}
		if post.Item != nil {
			if err := s.ownSummaryItem(post.Item); err != nil {
				return provider.CreatorPage{}, err
			}
		}
	}
	for index := range page.Collections {
		collection := &page.Collections[index]
		if collection.ID == "" || len(collection.ID) > maxCreatorIDLength ||
			strings.TrimSpace(collection.Title) == "" || collection.Count != nil && *collection.Count < 0 {
			return provider.CreatorPage{}, &provider.Error{Code: provider.PluginFailed}
		}
		if err := s.ownSummaryItems(collection.Items); err != nil {
			return provider.CreatorPage{}, err
		}
	}
	return page, nil
}

func sanitizeCreatorProfile(profile *provider.CreatorProfile, expectedID string) error {
	if profile.Creator.ID != expectedID || strings.TrimSpace(profile.Creator.Name) == "" {
		return &provider.Error{Code: provider.PluginFailed}
	}
	if profile.Followers != nil && *profile.Followers < 0 || profile.Works != nil && *profile.Works < 0 {
		return &provider.Error{Code: provider.PluginFailed}
	}
	if profile.Creator.AvatarURL != "" && httpsURL(profile.Creator.AvatarURL) == nil {
		profile.Creator.AvatarURL = ""
	}
	if profile.BackdropURL != "" && httpsURL(profile.BackdropURL) == nil {
		profile.BackdropURL = ""
	}
	if len(profile.Tabs) != 0 {
		tabs := make([]provider.CreatorTab, 0, len(profile.Tabs))
		seen := make(map[provider.CreatorTab]bool, len(profile.Tabs))
		for _, tab := range profile.Tabs {
			if tab == "" || !tab.Valid() {
				return &provider.Error{Code: provider.PluginFailed}
			}
			if !seen[tab] {
				seen[tab] = true
				tabs = append(tabs, tab)
			}
		}
		profile.Tabs = tabs
	}
	if len(profile.Attributes) != 0 {
		attrs := make([]provider.CreatorAttribute, 0, min(len(profile.Attributes), maxCreatorAttributes))
		for _, attr := range profile.Attributes {
			label := strings.TrimSpace(attr.Label)
			value := strings.TrimSpace(attr.Value)
			if label == "" || value == "" {
				continue
			}
			attrs = append(attrs, provider.CreatorAttribute{Label: label, Value: value})
			if len(attrs) >= maxCreatorAttributes {
				break
			}
		}
		profile.Attributes = attrs
	}
	return nil
}

func (s *Session) FollowCreator(ctx context.Context, request provider.CreatorFollowRequest) (provider.CreatorFollowResult, error) {
	var result provider.CreatorFollowResult
	if strings.TrimSpace(request.CreatorID) == "" || len(request.CreatorID) > maxCreatorIDLength {
		return result, &provider.Error{Code: provider.Denied}
	}
	auth, err := s.requireSignedIn()
	if err != nil {
		return result, err
	}
	input := struct {
		provider.CreatorFollowRequest
		Owner  string          `json:"owner"`
		Secret json.RawMessage `json:"secret"`
	}{request, auth.owner, auth.secret}
	if err := s.invoke(ctx, auth.scope, auth.client, provider.FollowCreator, "followCreator", input, &result); err != nil {
		return provider.CreatorFollowResult{}, err
	}
	return result, nil
}
