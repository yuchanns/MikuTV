package tvui

import (
	"fmt"

	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/provider"
)

func (p *PageContext) openSeriesSignIn(action func()) bool {
	p.host.openSignIn()
	if p.State.Page != app.SignInQR {
		return false
	}
	p.host.series.signInAction = action
	return true
}

func (v *View) resumeSeriesAfterSignIn(action func()) {
	if v.Auth == nil || v.Auth.Accounts().CurrentID == "" {
		return
	}
	switch v.State.Page {
	case app.Series, app.SeriesCalendar, app.SeriesIndex:
		v.dispatch(action)
	}
}

func (p *PageContext) setSeriesTrackFeedback(message string) {
	if p.State.Page == app.VideoDetail {
		p.host.videoDetail.feedback = videoDetailFeedback{text: message}
		return
	}
	p.host.series.feedback = videoDetailFeedback{text: message}
}

func (p *PageContext) toggleSeriesTrack(entry provider.SeriesEntry) {
	v := p.host
	if entry.Item.Ref.Kind != provider.Series || entry.Item.Ref.ID == "" {
		return
	}
	currentEntry, _ := p.State.Current()
	label := p.sourceSeriesTrackLabel(entry.Tracked)
	if !currentEntry.Capabilities.Has(provider.TrackSeries) || v.Catalog == nil {
		p.setSeriesTrackFeedback(fmt.Sprintf(p.Assets.Text.Detail.ActionsUnavailable, label))
		return
	}
	if v.Auth == nil || v.Auth.Accounts().CurrentID == "" {
		resume := func() {
			next := entry
			next.Tracked = false
			p.performSeriesTrack(next, true)
		}
		if p.State.Page == app.VideoDetail {
			p.openVideoSignIn(resume)
		} else {
			p.openSeriesSignIn(resume)
		}
		return
	}
	p.performSeriesTrack(entry, !entry.Tracked)
}

func (p *PageContext) performSeriesTrack(entry provider.SeriesEntry, enabled bool) {
	v := p.host
	if v.series.trackPending != nil || v.Catalog == nil {
		return
	}
	req := provider.SeriesTrackRequest{Ref: entry.Item.Ref, Enabled: enabled}
	v.series.trackPending = v.Catalog.TrackSeries(req, func(result provider.SeriesTrackResult, err error) {
		v.series.trackPending = nil
		if err != nil {
			if provider.CodeOf(err) == provider.NeedsSignIn {
				resume := func() { p.performSeriesTrack(entry, enabled) }
				if p.State.Page == app.VideoDetail {
					p.openVideoSignIn(resume)
				} else {
					p.openSeriesSignIn(resume)
				}
				return
			}
			p.setSeriesTrackFeedback(p.videoOperationError(err, p.Assets.Text.Detail.UpdateFailed))
			return
		}
		v.syncSeriesTracked(entry, result.Tracked, true)
		p.setSeriesTrackFeedback(p.sourceSeriesTrackFeedback(result.Tracked))
	})
}

func (v *View) syncSeriesTracked(entry provider.SeriesEntry, tracked bool, refreshActivity bool) {
	ref := entry.Item.Ref
	if ref.ID == "" {
		return
	}
	if v.Series != nil {
		v.Series.ApplyTracked(entry, tracked)
	}
	updateItem := func(item *provider.MediaItem) {
		if item.Ref == ref && item.VideoDetails != nil {
			item.VideoDetails.Engagement.Tracked = tracked
		}
	}
	updateDetail := func(d *videoDetailState) {
		updateItem(&d.seed)
		updateItem(&d.item)
		for i := range d.previous {
			updateItem(&d.previous[i].seed)
			updateItem(&d.previous[i].item)
		}
	}
	updateDetail(&v.videoDetail)
	for i := range v.videoDetailStack {
		updateDetail(&v.videoDetailStack[i])
	}
	updateDetail(&v.player.discussion)
	for i := range v.player.items {
		updateItem(&v.player.items[i].item)
	}
	if refreshActivity && v.Activity != nil {
		v.Activity.Refresh(provider.AccountQuery{Section: provider.AccountTracking})
	}
}
