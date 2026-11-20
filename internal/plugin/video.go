package plugin

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/yuchanns/MikuTV/internal/provider"
)

const (
	maxVideoActions   = 6
	maxCelebrateIcons = 5
)

var (
	actionIDPattern  = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	iconTokenPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9/_-]{0,63}$`)
)

func ownVideoActions(actions []provider.ActionSpec) error {
	if len(actions) > maxVideoActions {
		return &provider.Error{Code: provider.PluginFailed}
	}
	seen := make(map[string]bool, len(actions))
	for _, action := range actions {
		if seen[action.ID] || !actionIDPattern.MatchString(action.ID) ||
			!action.Slot.Valid() || strings.TrimSpace(action.Label) == "" {
			return &provider.Error{Code: provider.PluginFailed}
		}
		seen[action.ID] = true
		if action.Icon != "" && !iconTokenPattern.MatchString(action.Icon) {
			return &provider.Error{Code: provider.PluginFailed}
		}
		if action.ActiveIcon != "" && !iconTokenPattern.MatchString(action.ActiveIcon) {
			return &provider.Error{Code: provider.PluginFailed}
		}
		if action.Confirm != nil {
			if strings.TrimSpace(action.Confirm.Title) == "" {
				return &provider.Error{Code: provider.PluginFailed}
			}
			if action.Confirm.Icon != "" && !iconTokenPattern.MatchString(action.Confirm.Icon) {
				return &provider.Error{Code: provider.PluginFailed}
			}
		}
		if len(action.CelebrateIcons) > maxCelebrateIcons {
			return &provider.Error{Code: provider.PluginFailed}
		}
		for _, icon := range action.CelebrateIcons {
			if !iconTokenPattern.MatchString(icon) {
				return &provider.Error{Code: provider.PluginFailed}
			}
		}
	}
	return nil
}

func (s *Session) EditVideo(ctx context.Context, request provider.VideoEdit) (provider.VideoEditResult, error) {
	var result provider.VideoEditResult
	if !s.ownsVideo(request.Ref) || !request.Interaction.Valid() {
		return result, &provider.Error{Code: provider.Denied}
	}
	if request.Interaction == provider.VideoAction && !actionIDPattern.MatchString(request.ActionID) {
		return result, &provider.Error{Code: provider.Denied}
	}
	capability := provider.EditVideo
	switch request.Interaction {
	case provider.VideoReplyDislike, provider.VideoPostComment:
		capability = provider.WriteVideoComments
	}
	s.requests.RLock()
	client, scope, owner, secret := s.active, s.scope, s.accountID, s.secret
	s.requests.RUnlock()
	if owner == "" {
		return result, &provider.Error{Code: provider.NeedsSignIn}
	}
	input := struct {
		provider.VideoEdit
		Owner  string          `json:"owner"`
		Secret json.RawMessage `json:"secret"`
	}{request, owner, secret}
	if err := s.invoke(ctx, scope, client, capability, "editVideo", input, &result); err != nil {
		return provider.VideoEditResult{}, err
	}
	if len(result.ActionStates) > maxVideoActions {
		return provider.VideoEditResult{}, &provider.Error{Code: provider.PluginFailed}
	}
	for id := range result.ActionStates {
		if !actionIDPattern.MatchString(id) {
			return provider.VideoEditResult{}, &provider.Error{Code: provider.PluginFailed}
		}
	}
	if result.Comment != nil {
		if err := ownVideoComment(result.Comment, false); err != nil {
			return provider.VideoEditResult{}, err
		}
	}
	return result, nil
}

func (s *Session) VideoComments(ctx context.Context, query provider.VideoCommentQuery) (provider.VideoCommentPage, error) {
	var page provider.VideoCommentPage
	if !s.ownsVideo(query.Ref) || len(query.ThreadID) > 512 || query.Page < 0 ||
		(query.ThreadID == "" && query.Order != provider.VideoCommentsHot && query.Order != provider.VideoCommentsLatest) {
		return page, &provider.Error{Code: provider.Denied}
	}
	s.requests.RLock()
	client, scope := s.active, s.scope
	s.requests.RUnlock()
	if err := s.invoke(ctx, scope, client, provider.ReadVideoComments, "videoComments", query, &page); err != nil {
		return provider.VideoCommentPage{}, err
	}
	if err := ownVideoComments(page.Comments, query.ThreadID == ""); err != nil {
		return provider.VideoCommentPage{}, err
	}
	if page.NextPage < 0 || page.NextPage != 0 && (page.NextPage == query.Page ||
		query.ThreadID != "" && page.NextPage <= max(1, query.Page)) {
		return provider.VideoCommentPage{}, &provider.Error{Code: provider.PluginFailed}
	}
	return page, nil
}

func ownVideoComments(comments []provider.VideoComment, previews bool) error {
	for index := range comments {
		if err := ownVideoComment(&comments[index], previews); err != nil {
			return err
		}
	}
	return nil
}

func ownVideoComment(comment *provider.VideoComment, previews bool) error {
	if comment.ID == "" || len(comment.ID) > 512 || strings.TrimSpace(comment.Body) == "" ||
		comment.PublishedAt < 0 || comment.Likes != nil && *comment.Likes < 0 ||
		comment.ReplyCount != nil && *comment.ReplyCount < 0 || !previews && len(comment.Replies) != 0 {
		return &provider.Error{Code: provider.PluginFailed}
	}
	if comment.AvatarURL != "" && httpsURL(comment.AvatarURL) == nil {
		comment.AvatarURL = ""
	}
	if previews {
		return ownVideoComments(comment.Replies, false)
	}
	return nil
}

func (s *Session) ownsVideo(ref provider.MediaRef) bool {
	return ref.EntryID == s.entryID && ref.Kind.Valid() && ref.ID != ""
}

func (s *Session) VideoFavoriteFolders(ctx context.Context, ref provider.MediaRef) ([]provider.VideoFavoriteFolder, error) {
	if !s.ownsVideo(ref) {
		return nil, &provider.Error{Code: provider.Denied}
	}
	s.requests.RLock()
	client, scope, owner := s.active, s.scope, s.accountID
	s.requests.RUnlock()
	if owner == "" {
		return nil, &provider.Error{Code: provider.NeedsSignIn}
	}
	input := struct {
		Ref   provider.MediaRef `json:"ref"`
		Owner string            `json:"owner"`
	}{ref, owner}
	var result struct {
		Folders []provider.VideoFavoriteFolder `json:"folders"`
	}
	if err := s.invoke(ctx, scope, client, provider.EditVideoFavorites, "videoFavoriteFolders", input, &result); err != nil {
		return nil, err
	}
	for _, folder := range result.Folders {
		if folder.ID == "" || strings.TrimSpace(folder.Title) == "" || folder.Count < 0 {
			return nil, &provider.Error{Code: provider.PluginFailed}
		}
	}
	return result.Folders, nil
}

func (s *Session) EditVideoFavoriteFolders(ctx context.Context, request provider.VideoFavoriteEdit) error {
	if !s.ownsVideo(request.Ref) {
		return &provider.Error{Code: provider.Denied}
	}
	if len(request.Add)+len(request.Remove) == 0 {
		return nil
	}
	return s.invokePlaybackWrite(ctx, provider.EditVideoFavorites, "editVideoFavoriteFolders", "", request)
}
