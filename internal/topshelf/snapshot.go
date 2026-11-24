package topshelf

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"net/url"
	"strings"
)

var cgnatPrefix = netip.MustParsePrefix("100.64.0.0/10")

const (
	URLScheme          = "mikutv"
	SnapshotVersion    = 2
	MaxSections        = 3
	MaxItemsPerSection = 8
	MaxItems           = 12

	maxTitleRunes = 120
	maxMetaRunes  = 64
	maxURLBytes   = 1024
)

type ActionKind string

const (
	ActionPlay   ActionKind = "play"
	ActionDetail ActionKind = "detail"
)

type Link struct {
	Action  ActionKind
	EntryID string
	Kind    string
	ID      string
}

type Item struct {
	EntryID          string `json:"entryId"`
	EntryName        string `json:"entryName,omitempty"`
	Kind             string `json:"kind"`
	ID               string `json:"id"`
	Title            string `json:"title"`
	Genre            string `json:"genre,omitempty"`
	Badge            string `json:"badge,omitempty"`
	CreatorName      string `json:"creatorName,omitempty"`
	ImageURL         string `json:"imageUrl"`
	DurationSeconds  int    `json:"durationSeconds,omitempty"`
	ProgressSeconds  int    `json:"progressSeconds,omitempty"`
	ContinueWatching bool   `json:"continueWatching,omitempty"`
}

func (it Item) PlaybackProgress() float64 {
	if !it.ContinueWatching || it.ProgressSeconds <= 0 || it.DurationSeconds <= 0 {
		return 0
	}
	ratio := float64(it.ProgressSeconds) / float64(it.DurationSeconds)
	if ratio < 0.02 {
		return 0.02
	}
	if ratio > 0.98 {
		return 0.98
	}
	return ratio
}

type Section struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Items []Item `json:"items,omitempty"`
}

type Snapshot struct {
	Version   int       `json:"version"`
	EntryID   string    `json:"entryId,omitempty"`
	EntryName string    `json:"entryName,omitempty"`
	UpdatedAt int64     `json:"updatedAt,omitempty"`
	Sections  []Section `json:"sections,omitempty"`
	Items     []Item    `json:"items,omitempty"`
}

type Store interface {
	Load() (Snapshot, error)
	Save(Snapshot) error
	NotifyChanged()
}

func ValidMediaKind(kind string) bool {
	switch kind {
	case "video", "movie", "series", "episode", "live", "course":
		return true
	default:
		return false
	}
}

func BuildURL(link Link) string {
	action := link.Action
	if action != ActionPlay && action != ActionDetail {
		return ""
	}
	entryID := strings.TrimSpace(link.EntryID)
	kind := strings.TrimSpace(link.Kind)
	id := strings.TrimSpace(link.ID)
	if entryID == "" || !ValidMediaKind(kind) || id == "" {
		return ""
	}
	query := url.Values{}
	query.Set("entry", entryID)
	query.Set("kind", kind)
	query.Set("id", id)
	u := url.URL{
		Scheme:   URLScheme,
		Host:     string(action),
		RawQuery: query.Encode(),
	}
	return u.String()
}

func ParseURL(raw string) (Link, bool) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || !strings.EqualFold(parsed.Scheme, URLScheme) {
		return Link{}, false
	}
	action := ActionKind(strings.ToLower(strings.TrimSpace(parsed.Host)))
	if action == "" {
		action = ActionKind(strings.ToLower(strings.Trim(strings.TrimSpace(parsed.Path), "/")))
	}
	if action != ActionPlay && action != ActionDetail {
		return Link{}, false
	}
	q := parsed.Query()
	entryID := strings.TrimSpace(q.Get("entry"))
	kind := strings.TrimSpace(q.Get("kind"))
	id := strings.TrimSpace(q.Get("id"))
	if entryID == "" || !ValidMediaKind(kind) || id == "" {
		return Link{}, false
	}
	return Link{Action: action, EntryID: entryID, Kind: kind, ID: id}, true
}

func SanitizeSnapshot(s Snapshot) Snapshot {
	out := Snapshot{
		Version:   SnapshotVersion,
		EntryID:   strings.TrimSpace(s.EntryID),
		EntryName: clampRunes(s.EntryName, maxMetaRunes),
		UpdatedAt: s.UpdatedAt,
	}
	sections := s.Sections
	if len(sections) == 0 && len(s.Items) > 0 {
		sections = []Section{{
			ID:    "default",
			Title: out.EntryName,
			Items: s.Items,
		}}
	}
	seen := make(map[string]bool)
	total := 0
	for _, sec := range sections {
		if len(out.Sections) >= MaxSections || total >= MaxItems {
			break
		}
		secID := clampRunes(sec.ID, maxMetaRunes)
		secTitle := clampRunes(sec.Title, maxMetaRunes)
		if secID == "" {
			secID = fmt.Sprintf("section-%d", len(out.Sections)+1)
		}
		var items []Item
		for _, item := range sec.Items {
			if len(items) >= MaxItemsPerSection || total >= MaxItems {
				break
			}
			clean, ok := sanitizeItem(item, out.EntryID, out.EntryName)
			if !ok {
				continue
			}
			key := clean.EntryID + "\x00" + clean.Kind + "\x00" + clean.ID
			if seen[key] {
				continue
			}
			seen[key] = true
			items = append(items, clean)
			total++
		}
		if len(items) == 0 {
			continue
		}
		out.Sections = append(out.Sections, Section{
			ID:    secID,
			Title: secTitle,
			Items: items,
		})
	}
	return out
}

func sanitizeItem(item Item, fallbackEntryID, fallbackEntryName string) (Item, bool) {
	item.EntryID = strings.TrimSpace(item.EntryID)
	if item.EntryID == "" {
		item.EntryID = fallbackEntryID
	}
	item.EntryName = clampRunes(item.EntryName, maxMetaRunes)
	if item.EntryName == "" {
		item.EntryName = fallbackEntryName
	}
	item.Kind = strings.TrimSpace(item.Kind)
	item.ID = strings.TrimSpace(item.ID)
	item.Title = clampRunes(item.Title, maxTitleRunes)
	item.ImageURL = sanitizeImageURL(item.ImageURL)
	if item.EntryID == "" || !ValidMediaKind(item.Kind) || item.ID == "" || item.Title == "" || item.ImageURL == "" {
		return Item{}, false
	}
	item.Genre = clampRunes(item.Genre, maxMetaRunes)
	item.Badge = clampRunes(item.Badge, maxMetaRunes)
	item.CreatorName = clampRunes(item.CreatorName, maxMetaRunes)
	if item.DurationSeconds < 0 {
		item.DurationSeconds = 0
	}
	if item.ProgressSeconds < 0 {
		item.ProgressSeconds = 0
	}
	return item, true
}

func sanitizeImageURL(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || len(trimmed) > maxURLBytes {
		return ""
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Hostname() == "" || parsed.User != nil || parsed.Opaque != "" {
		return ""
	}
	switch {
	case strings.EqualFold(parsed.Scheme, "https"):
		return parsed.String()
	case strings.EqualFold(parsed.Scheme, "http") && isLocalNetworkHost(parsed.Hostname()):
		return parsed.String()
	default:
		return ""
	}
}

func isLocalNetworkHost(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if host == "" || host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return false
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		ip = ip.Unmap()
		if !ip.IsValid() || ip.Zone() != "" || ip.IsLoopback() || ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalMulticast() {
			return false
		}
		return ip.IsPrivate() || ip.IsLinkLocalUnicast() || cgnatPrefix.Contains(ip)
	}
	if !strings.Contains(host, ".") {
		return isSingleLabelLANHost(host)
	}
	for _, suffix := range []string{".local", ".lan", ".home", ".home.arpa", ".internal", ".localdomain"} {
		if strings.HasSuffix(host, suffix) && len(host) > len(suffix) {
			return isValidHostnameLabels(host)
		}
	}
	return false
}

func isValidHostnameLabels(host string) bool {
	if len(host) == 0 || len(host) > 253 {
		return false
	}
	for label := range strings.SplitSeq(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for i := range len(label) {
			c := label[i]
			if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-') {
				return false
			}
		}
	}
	return true
}

func isSingleLabelLANHost(label string) bool {
	if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
		return false
	}
	hasAlpha := false
	allHex := strings.HasPrefix(label, "0x") && len(label) > 2
	for i := range len(label) {
		c := label[i]
		switch {
		case c >= 'a' && c <= 'z':
			hasAlpha = true
			if i >= 2 && c > 'f' {
				allHex = false
			}
		case c >= '0' && c <= '9':
		case c == '-':
			allHex = false
		default:
			return false
		}
	}
	// Reject purely numeric or 0x-hex integer IP forms such as 2130706433 or 0x7f000001.
	return hasAlpha && !allHex
}

func clampRunes(s string, limit int) string {
	s = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", " "), "\n", " "))
	if limit <= 0 || len(s) <= limit {
		return s
	}
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return strings.TrimSpace(string(runes[:limit]))
}

func Encode(s Snapshot) ([]byte, error) {
	return json.Marshal(SanitizeSnapshot(s))
}

func Decode(data []byte) (Snapshot, error) {
	if len(data) == 0 {
		return Snapshot{Version: SnapshotVersion}, nil
	}
	var raw Snapshot
	if err := json.Unmarshal(data, &raw); err != nil {
		return Snapshot{}, fmt.Errorf("decode topshelf snapshot: %w", err)
	}
	return SanitizeSnapshot(raw), nil
}

func (s Snapshot) ItemCount() int {
	n := 0
	for _, sec := range s.Sections {
		n += len(sec.Items)
	}
	if n == 0 {
		return len(s.Items)
	}
	return n
}

func (s Snapshot) Find(entryID, kind, id string) (Item, bool) {
	for _, sec := range s.Sections {
		for _, item := range sec.Items {
			if item.EntryID == entryID && item.Kind == kind && item.ID == id {
				return item, true
			}
		}
	}
	for _, item := range s.Items {
		if item.EntryID == entryID && item.Kind == kind && item.ID == id {
			return item, true
		}
	}
	return Item{}, false
}
