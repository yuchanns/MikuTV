package plugin

import (
	"context"
	"strings"

	"github.com/yuchanns/MikuTV/internal/provider"
)

func (s *Session) ReadFollowing(ctx context.Context, request provider.FollowingRequest) (provider.FollowingPage, error) {
	if !request.Section.Valid() || len(request.Cursor) > maxSearchCursorLength {
		return provider.FollowingPage{}, &provider.Error{Code: provider.Denied}
	}
	auth, err := s.requireSignedIn()
	if err != nil {
		return provider.FollowingPage{}, err
	}
	input := struct {
		provider.FollowingRequest
		Owner string `json:"owner"`
	}{request, auth.owner}
	var page provider.FollowingPage
	if err := s.invoke(ctx, auth.scope, auth.client, provider.BrowseFollowing, "readFollowing", input, &page); err != nil {
		return provider.FollowingPage{}, err
	}
	if len(page.CreatorsCursor) > maxSearchCursorLength ||
		len(page.UpdatesCursor) > maxSearchCursorLength ||
		len(page.LiveCursor) > maxSearchCursorLength {
		return provider.FollowingPage{}, &provider.Error{Code: provider.PluginFailed}
	}
	for index := range page.Creators {
		entry := &page.Creators[index]
		if strings.TrimSpace(entry.Creator.ID) == "" || len(entry.Creator.ID) > maxCreatorIDLength ||
			strings.TrimSpace(entry.Creator.Name) == "" {
			return provider.FollowingPage{}, &provider.Error{Code: provider.PluginFailed}
		}
		entry.Creator.Following = true
		if entry.Creator.AvatarURL != "" && httpsURL(entry.Creator.AvatarURL) == nil {
			entry.Creator.AvatarURL = ""
		}
	}
	if err := s.ownSummaryItems(page.Updates); err != nil {
		return provider.FollowingPage{}, err
	}
	if err := s.ownSummaryItems(page.LiveRooms); err != nil {
		return provider.FollowingPage{}, err
	}
	return page, nil
}
