package app

import (
	"context"

	"github.com/yuchanns/MikuTV/internal/provider"
)

func (c *Catalog) ReadPlaybackDanmaku(query provider.DanmakuQuery, accept func([]provider.Danmaku, error)) context.CancelFunc {
	return catalogDispatch(c, func(ctx context.Context, source provider.PlaybackDanmaku) ([]provider.Danmaku, error) {
		return source.Danmaku(ctx, query)
	}, accept)
}

func (c *Catalog) PostPlaybackDanmaku(draft provider.DanmakuPost, accept func(error)) context.CancelFunc {
	return catalogAction(c, func(ctx context.Context, source provider.PlaybackDanmaku) error {
		return source.PostDanmaku(ctx, draft)
	}, accept)
}

func (c *Catalog) ReadDanmakuRules(accept func([]provider.DanmakuRule, error)) context.CancelFunc {
	return catalogDispatch(c, func(ctx context.Context, source provider.DanmakuRulesProvider) ([]provider.DanmakuRule, error) {
		return source.DanmakuRules(ctx)
	}, accept)
}

func (c *Catalog) AddDanmakuRule(draft provider.DanmakuRuleDraft, accept func(provider.DanmakuRule, error)) context.CancelFunc {
	return catalogDispatch(c, func(ctx context.Context, source provider.DanmakuRulesProvider) (provider.DanmakuRule, error) {
		return source.AddDanmakuRule(ctx, draft)
	}, accept)
}

func (c *Catalog) RemoveDanmakuRule(removal provider.DanmakuRuleRemoval, accept func(error)) context.CancelFunc {
	return catalogAction(c, func(ctx context.Context, source provider.DanmakuRulesProvider) error {
		return source.RemoveDanmakuRule(ctx, removal)
	}, accept)
}

func (c *Catalog) ReadPlaybackSubtitles(query provider.PlaybackSubtitleQuery, accept func([]provider.SubtitleCue, error)) context.CancelFunc {
	return catalogDispatch(c, func(ctx context.Context, source provider.PlaybackSubtitleReader) ([]provider.SubtitleCue, error) {
		return source.PlaybackSubtitles(ctx, query)
	}, accept)
}

func (c *Catalog) ReadVideoFavoriteFolders(ref provider.MediaRef, accept func([]provider.VideoFavoriteFolder, error)) context.CancelFunc {
	return catalogDispatch(c, func(ctx context.Context, source provider.VideoFavoriteEditor) ([]provider.VideoFavoriteFolder, error) {
		return source.VideoFavoriteFolders(ctx, ref)
	}, accept)
}

func (c *Catalog) EditVideoFavoriteFolders(edit provider.VideoFavoriteEdit, accept func(error)) context.CancelFunc {
	return catalogAction(c, func(ctx context.Context, source provider.VideoFavoriteEditor) error {
		return source.EditVideoFavoriteFolders(ctx, edit)
	}, accept)
}
