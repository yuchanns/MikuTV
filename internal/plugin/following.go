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
	s.requests.RLock()
	client, scope, owner := s.active, s.scope, s.accountID
	s.requests.RUnlock()
	if owner == "" {
		return provider.FollowingPage{}, &provider.Error{Code: provider.NeedsSignIn}
	}
	input := struct {
		provider.FollowingRequest
		Owner string `json:"owner"`
	}{request, owner}
	var page provider.FollowingPage
	if err := s.invoke(ctx, scope, client, provider.BrowseFollowing, "readFollowing", input, &page); err != nil {
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
	for index := range page.Updates {
		if page.Updates[index].VideoDetails != nil {
			return provider.FollowingPage{}, &provider.Error{Code: provider.PluginFailed}
		}
		if err := s.ownItem(&page.Updates[index]); err != nil {
			return provider.FollowingPage{}, err
		}
	}
	for index := range page.LiveRooms {
		if page.LiveRooms[index].VideoDetails != nil {
			return provider.FollowingPage{}, &provider.Error{Code: provider.PluginFailed}
		}
		if err := s.ownItem(&page.LiveRooms[index]); err != nil {
			return provider.FollowingPage{}, err
		}
	}
	return page, nil
}
