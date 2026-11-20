package app

import (
	"context"

	"github.com/yuchanns/MikuTV/internal/provider"
)

func (c *Catalog) ReadPlaybackDanmaku(query provider.DanmakuQuery, accept func([]provider.Danmaku, error)) context.CancelFunc {
	source, _ := c.session.(provider.PlaybackDanmaku)
	return catalogResult(c, func(ctx context.Context) ([]provider.Danmaku, error) {
		if source == nil {
			return nil, &provider.Error{Code: provider.Unsupported}
		}
		return source.Danmaku(ctx, query)
	}, accept)
}

func (c *Catalog) PostPlaybackDanmaku(draft provider.DanmakuPost, accept func(error)) context.CancelFunc {
	source, _ := c.session.(provider.PlaybackDanmaku)
	return catalogResult(c, func(ctx context.Context) (struct{}, error) {
		if source == nil {
			return struct{}{}, &provider.Error{Code: provider.Unsupported}
		}
		return struct{}{}, source.PostDanmaku(ctx, draft)
	}, func(_ struct{}, err error) { accept(err) })
}

func (c *Catalog) ReadPlaybackSubtitles(query provider.PlaybackSubtitleQuery, accept func([]provider.SubtitleCue, error)) context.CancelFunc {
	source, _ := c.session.(provider.PlaybackSubtitleReader)
	return catalogResult(c, func(ctx context.Context) ([]provider.SubtitleCue, error) {
		if source == nil {
			return nil, &provider.Error{Code: provider.Unsupported}
		}
		return source.PlaybackSubtitles(ctx, query)
	}, accept)
}

func (c *Catalog) ReadVideoFavoriteFolders(ref provider.MediaRef, accept func([]provider.VideoFavoriteFolder, error)) context.CancelFunc {
	source, _ := c.session.(provider.VideoFavoriteEditor)
	return catalogResult(c, func(ctx context.Context) ([]provider.VideoFavoriteFolder, error) {
		if source == nil {
			return nil, &provider.Error{Code: provider.Unsupported}
		}
		return source.VideoFavoriteFolders(ctx, ref)
	}, accept)
}

func (c *Catalog) EditVideoFavoriteFolders(edit provider.VideoFavoriteEdit, accept func(error)) context.CancelFunc {
	source, _ := c.session.(provider.VideoFavoriteEditor)
	return catalogResult(c, func(ctx context.Context) (struct{}, error) {
		if source == nil {
			return struct{}{}, &provider.Error{Code: provider.Unsupported}
		}
		return struct{}{}, source.EditVideoFavoriteFolders(ctx, edit)
	}, func(_ struct{}, err error) { accept(err) })
}
