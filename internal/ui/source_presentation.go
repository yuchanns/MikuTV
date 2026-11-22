package tvui

import (
	"strings"
	"unicode"
	"unicode/utf8"

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

func (p *PageContext) librarySectionLabel(section provider.AccountSection) string {
	sections := p.sourcePresentation().Sections
	switch section {
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
