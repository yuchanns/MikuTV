package plugin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

const MaxLocaleJSONBytes = 256 << 10

var localeTagPattern = regexp.MustCompile(`^[A-Za-z]{2,3}(?:-[A-Za-z0-9]{2,8})*$`)

type localeManifest struct {
	Manifest
	Language   string `json:"language"`
	NativeName string `json:"nativeName,omitempty"`
	Locale     string `json:"locale"`
}

// LocalePackage is a validated declarative language pack loaded without JS/moejs.
type LocalePackage struct {
	manifest    localeManifest
	language    string
	nativeName  string
	rawJSON     []byte
	licenseText string
}

func (p *LocalePackage) Manifest() Manifest  { return p.manifest.Manifest }
func (p *LocalePackage) Language() string    { return p.language }
func (p *LocalePackage) NativeName() string  { return p.nativeName }
func (p *LocalePackage) RawJSON() []byte     { return append([]byte(nil), p.rawJSON...) }
func (p *LocalePackage) LicenseText() string { return p.licenseText }

// NormalizeLocaleTag canonicalizes a BCP-47 language tag (e.g. "zh_hans_cn" -> "zh-Hans-CN",
// "JA-jp" -> "ja-JP", "en" -> "en"). Returns "" if invalid.
func NormalizeLocaleTag(raw string) string {
	cleaned := strings.ReplaceAll(strings.TrimSpace(raw), "_", "-")
	if cleaned == "" {
		return ""
	}
	if idx := strings.IndexAny(cleaned, ".@"); idx >= 0 {
		cleaned = cleaned[:idx]
	}
	if !localeTagPattern.MatchString(cleaned) {
		return ""
	}
	parts := strings.Split(cleaned, "-")
	for i, part := range parts {
		switch {
		case i == 0:
			parts[i] = strings.ToLower(part)
		case len(part) == 4:
			// ISO 15924 script subtag (e.g. Hans, Hant, Latn)
			parts[i] = strings.ToUpper(part[:1]) + strings.ToLower(part[1:])
		case len(part) == 2 && isASCIIAlpha(part):
			// ISO 3166-1 region subtag (e.g. CN, JP, US, TW)
			parts[i] = strings.ToUpper(part)
		default:
			parts[i] = strings.ToLower(part)
		}
	}
	return strings.Join(parts, "-")
}

func isASCIIAlpha(s string) bool {
	for i := range len(s) {
		c := s[i]
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')) {
			return false
		}
	}
	return len(s) > 0
}

// MatchLocaleTag matches a BCP-47 preference list against available tags,
// preferring exact match, then script-compatible match, then primary language match.
func MatchLocaleTag(preferred, available []string) string {
	type normEntry struct {
		orig    string
		norm    string
		lower   string
		primary string
		script  string
	}
	parse := func(raw string) normEntry {
		n := NormalizeLocaleTag(raw)
		if n == "" {
			return normEntry{}
		}
		parts := strings.Split(n, "-")
		e := normEntry{orig: raw, norm: n, lower: strings.ToLower(n), primary: parts[0]}
		if len(parts) >= 2 && len(parts[1]) == 4 {
			e.script = parts[1]
		} else if e.primary == "zh" && len(parts) >= 2 {
			switch parts[1] {
			case "TW", "HK", "MO":
				e.script = "Hant"
			case "CN", "SG", "MY":
				e.script = "Hans"
			}
		}
		return e
	}

	avail := make([]normEntry, 0, len(available))
	for _, a := range available {
		if e := parse(a); e.norm != "" {
			avail = append(avail, e)
		}
	}
	if len(avail) == 0 {
		return ""
	}
	for _, prefRaw := range preferred {
		pref := parse(prefRaw)
		if pref.norm == "" {
			continue
		}
		// 1. Exact canonical match.
		for _, a := range avail {
			if a.lower == pref.lower {
				return a.orig
			}
		}
		// 2. Primary + script match (e.g. zh-CN -> zh-Hans, zh-TW -> zh-Hant).
		if pref.script != "" {
			for _, a := range avail {
				if a.primary == pref.primary && a.script == pref.script {
					return a.orig
				}
			}
		}
		// 3. Primary language match (e.g. ja-JP -> ja, en-GB -> en, zh-Hant -> zh-Hans fallback if no zh-Hant).
		for _, a := range avail {
			if a.primary == pref.primary {
				return a.orig
			}
		}
	}
	return ""
}

func decodeLocaleManifest(data []byte, appVersion string) (localeManifest, error) {
	if _, err := validateManifestHeader(data, appVersion, LocaleKind); err != nil {
		return localeManifest{}, err
	}

	var manifest localeManifest
	if err := decodeJSON(data, &manifest); err != nil {
		return localeManifest{}, fmt.Errorf("locale manifest: %w", err)
	}
	if NormalizeLocaleTag(manifest.Language) == "" {
		return localeManifest{}, fmt.Errorf("invalid locale language tag %q", manifest.Language)
	}
	if !packagePath(manifest.Locale) || !strings.HasSuffix(strings.ToLower(manifest.Locale), ".json") {
		return localeManifest{}, fmt.Errorf("invalid locale file path %q", manifest.Locale)
	}
	if manifest.Artwork.Cover != "" && !packagePath(manifest.Artwork.Cover) {
		return localeManifest{}, fmt.Errorf("invalid cover path %q", manifest.Artwork.Cover)
	}
	return manifest, nil
}

// OpenLocale validates a declarative locale plugin.zip without initializing JS.
func OpenLocale(data []byte, appVersion string) (*LocalePackage, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty locale package")
	}
	files, err := readArchive(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	header, err := files.read("plugin.json")
	if err != nil {
		return nil, fmt.Errorf("plugin.json: %w", err)
	}
	manifest, err := decodeLocaleManifest(header, appVersion)
	if err != nil {
		return nil, err
	}
	rawLocale, err := files.read(manifest.Locale)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", manifest.Locale, err)
	}
	if len(rawLocale) == 0 || len(rawLocale) > MaxLocaleJSONBytes {
		return nil, fmt.Errorf("invalid locale file size (%d bytes)", len(rawLocale))
	}
	if !utf8.Valid(rawLocale) {
		return nil, fmt.Errorf("locale file must be valid UTF-8")
	}
	var parsed map[string]any
	if err := json.Unmarshal(rawLocale, &parsed); err != nil || len(parsed) == 0 {
		return nil, fmt.Errorf("locale file must be a non-empty JSON object")
	}
	lang := NormalizeLocaleTag(manifest.Language)
	native := strings.TrimSpace(manifest.NativeName)
	if native == "" {
		native = strings.TrimSpace(manifest.Name)
	}
	return &LocalePackage{
		manifest:    manifest,
		language:    lang,
		nativeName:  native,
		rawJSON:     append([]byte(nil), rawLocale...),
		licenseText: readPluginLicenseText(files),
	}, nil
}
