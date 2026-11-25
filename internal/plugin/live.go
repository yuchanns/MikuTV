package plugin

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/yuchanns/MikuTV/internal/provider"
)

const maxLiveAreas = 32

func (s *Session) ReadLive(ctx context.Context, request provider.LiveRequest) (provider.LivePage, error) {
	if !request.Section.Valid() || len(request.Area) > 128 || len(request.Cursor) > maxSearchCursorLength {
		return provider.LivePage{}, &provider.Error{Code: provider.Denied}
	}
	auth := s.snapshotAuth()
	input := struct {
		provider.LiveRequest
		SignedIn bool            `json:"signedIn,omitempty"`
		Owner    string          `json:"owner,omitempty"`
		Secret   json.RawMessage `json:"secret,omitempty"`
	}{request, auth.signedIn(), auth.owner, auth.secret}
	var page provider.LivePage
	if err := s.invoke(ctx, auth.scope, auth.client, provider.BrowseLive, "readLive", input, &page); err != nil {
		return provider.LivePage{}, err
	}
	if len(page.NextCursor) > maxSearchCursorLength || len(page.FollowingCursor) > maxSearchCursorLength {
		return provider.LivePage{}, &provider.Error{Code: provider.PluginFailed}
	}
	page.Areas = sanitizeLiveAreas(page.Areas)
	if page.Hero != nil {
		if err := s.sanitizeLiveItem(page.Hero); err != nil {
			return provider.LivePage{}, err
		}
	}
	for i := range page.Rooms {
		if err := s.sanitizeLiveItem(&page.Rooms[i]); err != nil {
			return provider.LivePage{}, err
		}
	}
	for i := range page.FollowingRooms {
		if err := s.sanitizeLiveItem(&page.FollowingRooms[i]); err != nil {
			return provider.LivePage{}, err
		}
	}
	return page, nil
}

func (s *Session) sanitizeLiveItem(item *provider.MediaItem) error {
	if item.VideoDetails != nil || item.Ref.Kind != provider.Live {
		return &provider.Error{Code: provider.PluginFailed}
	}
	return s.ownItem(item)
}

func sanitizeLiveAreas(raw []provider.LiveArea) []provider.LiveArea {
	if len(raw) == 0 {
		return nil
	}
	out := make([]provider.LiveArea, 0, min(len(raw), maxLiveAreas))
	seen := make(map[string]bool, min(len(raw), maxLiveAreas))
	for _, area := range raw {
		label := strings.TrimSpace(area.Label)
		if label == "" || len(area.ID) > 128 || seen[area.ID] {
			continue
		}
		seen[area.ID] = true
		out = append(out, provider.LiveArea{ID: area.ID, Label: label})
		if len(out) >= maxLiveAreas {
			break
		}
	}
	return out
}
