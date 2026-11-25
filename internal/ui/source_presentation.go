package tvui

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/provider"
)

func (p *PageContext) sourcePresentation() provider.SourcePresentation {
	if p.State == nil {
		return provider.SourcePresentation{}
	}
	entry, ok := p.State.Current()
	if !ok {
		return provider.SourcePresentation{}
	}
	return entry.Presentation
}

func (p *PageContext) sourceCreatorMark() string {
	if mark := strings.TrimSpace(p.sourcePresentation().CreatorMark); mark != "" {
		return mark
	}
	return p.Assets.Text.Detail.CreatorMark
}

func (p *PageContext) sourcePinnedLabel() string {
	text := p.Assets.Text.Detail
	if mark := strings.TrimSpace(p.sourcePresentation().CreatorMark); mark != "" && text.CreatorMark != "" {
		return strings.ReplaceAll(text.Pinned, text.CreatorMark, mark)
	}
	return text.Pinned
}

func (p *PageContext) sourceCreatorGroupTitle() string {
	if title := strings.TrimSpace(p.sourcePresentation().CreatorGroupTitle); title != "" {
		return title
	}
	return p.Assets.Text.Home.Following
}

func (p *PageContext) sourceSearchHint() string {
	if hint := strings.TrimSpace(p.sourcePresentation().Search.Hint); hint != "" {
		return hint
	}
	return p.Assets.Text.Search.Hint
}

func (p *PageContext) sourceSearchCreatorLabel() string {
	if label := strings.TrimSpace(p.sourcePresentation().Search.CreatorLabel); label != "" {
		return label
	}
	return p.Assets.Text.Search.KindCreator
}

func (p *PageContext) sourceSearchSeriesLabel() string {
	if label := strings.TrimSpace(p.sourcePresentation().Search.SeriesLabel); label != "" {
		return label
	}
	return p.Assets.Text.Search.KindSeries
}

func (p *PageContext) sourceSeriesTrackLabel(tracked bool) string {
	pres := p.sourcePresentation().Series
	if tracked {
		if label := strings.TrimSpace(pres.TrackedLabel); label != "" {
			return label
		}
		return p.Assets.Text.Series.Tracked
	}
	if label := strings.TrimSpace(pres.TrackLabel); label != "" {
		return label
	}
	return p.Assets.Text.Series.Track
}

func (p *PageContext) sourceSeriesTrackFeedback(tracked bool) string {
	if tracked {
		return p.sourceSeriesTrackLabel(true)
	}
	removed := p.Assets.Text.Series.TrackRemoved
	if label := strings.TrimSpace(p.sourcePresentation().Series.TrackLabel); label != "" {
		return strings.ReplaceAll(removed, p.Assets.Text.Series.Track, label)
	}
	return removed
}

func (p *PageContext) sourceMembership() string {
	if membership := strings.TrimSpace(p.sourcePresentation().Membership); membership != "" {
		return membership
	}
	return p.Assets.Text.Playback.QualityMember
}

func (p *PageContext) sourceText(raw string) string {
	if raw == "" {
		return raw
	}
	presentation := p.sourcePresentation()
	if membership := strings.TrimSpace(presentation.Membership); membership != "" {
		raw = replaceTerm(raw, p.Assets.Text.Playback.QualityMember, membership)
	}
	if mobileApp := strings.TrimSpace(presentation.MobileApp); mobileApp != "" {
		raw = replaceTerm(raw, p.Assets.Text.Playback.Access.MobileClient, mobileApp)
	}
	return raw
}

func (p *PageContext) canEditWatchLater(entry app.Entry) bool {
	return entry.Capabilities.Has(provider.EditVideo) &&
		entry.Capabilities.Has(provider.AccountWatchLater.Capability())
}

func (p *PageContext) canEditFavorites(entry app.Entry) bool {
	return entry.Capabilities.Has(provider.EditVideo) &&
		entry.Capabilities.Has(provider.EditVideoFavorites)
}

func (p *PageContext) libraryNavLabel() string {
	if label := strings.TrimSpace(p.sourcePresentation().Sections.Library); label != "" {
		return label
	}
	return p.Assets.Text.Nav[app.Library]
}

func (p *PageContext) sourceHomePicks() (string, string) {
	home := p.sourcePresentation().Home
	title, note := p.Assets.Text.Home.Picks, p.Assets.Text.Home.PicksNote
	if v := strings.TrimSpace(home.PicksTitle); v != "" {
		title = v
	}
	if v := strings.TrimSpace(home.PicksNote); v != "" {
		note = v
	}
	return title, note
}

func (p *PageContext) sourceHomeContinueTitle() string {
	if v := strings.TrimSpace(p.sourcePresentation().Home.ContinueTitle); v != "" {
		return v
	}
	return p.Assets.Text.Home.Continue
}

func (p *PageContext) sourceHomeRankingNote() string {
	if v := strings.TrimSpace(p.sourcePresentation().Home.RankingNote); v != "" {
		return v
	}
	return p.Assets.Text.Home.RankingNote
}

func (p *PageContext) sourceSearchPlaceholder() string {
	if v := strings.TrimSpace(p.sourcePresentation().Search.Placeholder); v != "" {
		return v
	}
	return p.Assets.Text.Search.Placeholder
}

func (p *PageContext) sourceSearchVideoLabel() string {
	if v := strings.TrimSpace(p.sourcePresentation().Search.VideoLabel); v != "" {
		return v
	}
	return p.Assets.Text.Search.KindVideo
}

func (p *PageContext) sourceSearchItemCount(count int64) string {
	if tmpl := strings.TrimSpace(p.sourcePresentation().Search.ItemCountLabel); tmpl != "" {
		if strings.Contains(tmpl, "%d") {
			return fmt.Sprintf(tmpl, count)
		}
		return fmt.Sprintf("%d %s", count, tmpl)
	}
	return fmt.Sprintf(p.Assets.Text.Search.VideoCount, count)
}

func (p *PageContext) sourceCreatorTabLabel(tab provider.CreatorTab) string {
	pres := p.sourcePresentation().Creator
	text := p.Assets.Text.Creator
	switch tab {
	case provider.CreatorTabVideos:
		if v := strings.TrimSpace(pres.VideosLabel); v != "" {
			return v
		}
		return text.Videos
	case provider.CreatorTabPosts:
		return text.Posts
	case provider.CreatorTabCollections:
		if v := strings.TrimSpace(pres.CollectionsLabel); v != "" {
			return v
		}
		return text.Collections
	case provider.CreatorTabAbout:
		return text.About
	default:
		return string(tab)
	}
}

func (p *PageContext) sourceCreatorEmptyVideos() string {
	if v := strings.TrimSpace(p.sourcePresentation().Creator.EmptyVideos); v != "" {
		return v
	}
	return p.Assets.Text.Creator.NoVideos
}

func (p *PageContext) sourceCreatorEmptyCollections() string {
	if v := strings.TrimSpace(p.sourcePresentation().Creator.EmptyCollections); v != "" {
		return v
	}
	return p.Assets.Text.Creator.NoCollections
}

func (p *PageContext) sourceDetailItemLabel() string {
	if v := strings.TrimSpace(p.sourcePresentation().Detail.ItemLabel); v != "" {
		return v
	}
	return p.Assets.Text.Detail.CurrentVideo
}

func (p *PageContext) sourceDetailCollectionLabel() string {
	if v := strings.TrimSpace(p.sourcePresentation().Detail.CollectionLabel); v != "" {
		return v
	}
	return p.Assets.Text.Detail.Collection
}

func (p *PageContext) sourceDetailCollectionUnit() string {
	if v := strings.TrimSpace(p.sourcePresentation().Detail.CollectionUnit); v != "" {
		return v
	}
	return p.Assets.Text.Detail.SeriesUnit
}

func (p *PageContext) sourceDetailLoading() string {
	if v := strings.TrimSpace(p.sourcePresentation().Detail.Loading); v != "" {
		return v
	}
	return p.Assets.Text.Detail.Loading
}

func (p *PageContext) sourceDetailLoadFailed() string {
	if v := strings.TrimSpace(p.sourcePresentation().Detail.LoadFailed); v != "" {
		return v
	}
	return p.Assets.Text.Detail.LoadFailed
}

func (p *PageContext) sourceDetailPreviewAction() string {
	if v := strings.TrimSpace(p.sourcePresentation().Detail.PreviewAction); v != "" {
		return v
	}
	return p.Assets.Text.Detail.Preview
}

func (p *PageContext) sourceDetailAccessAction() string {
	if v := strings.TrimSpace(p.sourcePresentation().Detail.AccessAction); v != "" {
		return v
	}
	return p.Assets.Text.Detail.GetAccess
}

func (p *PageContext) librarySectionLabel(section provider.AccountSection) string {
	sections := p.sourcePresentation().Sections
	switch section {
	case provider.AccountHistory:
		if label := strings.TrimSpace(sections.History); label != "" {
			return label
		}
	case provider.AccountFavorites:
		if label := strings.TrimSpace(sections.Favorites); label != "" {
			return label
		}
	case provider.AccountWatchLater:
		if label := strings.TrimSpace(sections.WatchLater); label != "" {
			return label
		}
	case provider.AccountTracking:
		if label := strings.TrimSpace(sections.Tracking); label != "" {
			return label
		}
	case provider.AccountCourses:
		if label := strings.TrimSpace(sections.Courses); label != "" {
			return label
		}
	}
	return p.Assets.Text.Activity.libraryTab(section)
}

func (p *PageContext) libraryFoldersBackLabel() string {
	if label := strings.TrimSpace(p.sourcePresentation().Sections.Favorites); label != "" {
		return label
	}
	return p.Assets.Text.Activity.FoldersBack
}

func (p *PageContext) libraryGuestText() (string, string) {
	sections := p.sourcePresentation().Sections
	title, note := p.Assets.Text.Activity.LibraryGuestTitle, p.Assets.Text.Activity.LibraryGuestNote
	if v := strings.TrimSpace(sections.GuestTitle); v != "" {
		title = v
	}
	if v := strings.TrimSpace(sections.GuestNote); v != "" {
		note = v
	}
	return title, note
}

func (p *PageContext) libraryEmptyText(section provider.AccountSection) (string, string) {
	activity := p.Assets.Text.Activity
	empty := activity.LibraryEmpty[section]
	if section == provider.AccountCourses {
		if label := strings.TrimSpace(p.sourcePresentation().Sections.Courses); label != "" {
			empty.Title = replaceTerm(empty.Title, activity.Courses, label)
		}
	}
	return empty.Title, empty.Note
}

func replaceTerm(text, placeholder, replacement string) string {
	if placeholder == "" || replacement == "" || placeholder == replacement || !strings.Contains(text, placeholder) {
		return text
	}
	firstRep, _ := utf8.DecodeRuneInString(replacement)
	lastRep, _ := utf8.DecodeLastRuneInString(replacement)
	var out strings.Builder
	out.Grow(len(text) + len(replacement))
	remaining := text
	for {
		idx := strings.Index(remaining, placeholder)
		if idx < 0 {
			out.WriteString(remaining)
			return out.String()
		}
		prefix := remaining[:idx]
		out.WriteString(prefix)
		if prev, size := utf8.DecodeLastRuneInString(prefix); size > 0 && needsWordSpace(prev, firstRep) {
			out.WriteByte(' ')
		}
		out.WriteString(replacement)
		remaining = remaining[idx+len(placeholder):]
		if next, size := utf8.DecodeRuneInString(remaining); size > 0 && needsWordSpace(lastRep, next) {
			out.WriteByte(' ')
		}
	}
}

func needsWordSpace(left, right rune) bool {
	return isASCIIAlnum(left) && isWideLetter(right) || isWideLetter(left) && isASCIIAlnum(right)
}

func isASCIIAlnum(r rune) bool {
	return r >= '0' && r <= '9' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z'
}

func isWideLetter(r rune) bool {
	return r > 0x7f && unicode.IsLetter(r)
}
