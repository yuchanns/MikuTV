package plugin

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/yuchanns/MikuTV/internal/provider"
)

func (s *Session) EditVideo(ctx context.Context, request provider.VideoEdit) (provider.VideoEditResult, error) {
	var result provider.VideoEditResult
	if !s.ownsVideo(request.Ref) || !request.Interaction.Valid() {
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
	if result.Coins != nil && *result.Coins < 0 {
		return provider.VideoEditResult{}, &provider.Error{Code: provider.PluginFailed}
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
		(query.ThreadID == "" && (query.Order != provider.VideoCommentsHot && query.Order != provider.VideoCommentsLatest || query.Page != 0)) {
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
	if page.NextPage != 0 && (query.ThreadID == "" || page.NextPage <= max(1, query.Page)) {
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
	return ref.EntryID == s.entryID && ref.Kind == provider.Video && ref.ID != ""
}
