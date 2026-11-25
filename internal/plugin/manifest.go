package plugin

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/MoeForge/moejs/webapi"
	"github.com/yuchanns/MikuTV/internal/provider"
	"golang.org/x/mod/semver"
	"golang.org/x/net/publicsuffix"
)

type Kind string

const (
	SourceKind      Kind = "source"
	ThemeKind       Kind = "theme"
	LocaleKind      Kind = "locale"
	protocolVersion      = 1
)

var ErrIncompatible = errors.New("incompatible plugin")

type Manifest struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Description   string `json:"description,omitempty"`
	Kind          Kind   `json:"kind"`
	Version       string `json:"version"`
	APIVersion    int    `json:"apiVersion"`
	MinAppVersion string `json:"minAppVersion"`
	License       string `json:"license,omitempty"`
	Artwork       struct {
		Cover string `json:"cover,omitempty"`
	} `json:"artwork,omitempty"`
}

type LocalizedSource struct {
	Name         string                    `json:"name,omitempty"`
	Description  string                    `json:"description,omitempty"`
	Auth         provider.AuthPresentation `json:"authentication,omitzero"`
	Presentation provider.SourcePresentation `json:"presentation,omitzero"`
}

type artworkPermission struct {
	Hosts   []string `json:"hosts,omitempty"`
	Referer string   `json:"referer,omitempty"`
}

type sourceManifest struct {
	Manifest
	Main           string `json:"main"`
	Authentication struct {
		Methods      []string                  `json:"methods,omitempty"`
		ServerURL    string                    `json:"serverURL,omitempty"`
		Presentation provider.AuthPresentation `json:"presentation,omitzero"`
	} `json:"authentication,omitempty"`
	Permissions struct {
		Network []string          `json:"network,omitempty"`
		Artwork artworkPermission `json:"artwork,omitzero"`
	} `json:"permissions,omitempty"`
	Presentation provider.SourcePresentation  `json:"presentation,omitzero"`
	Capabilities provider.Capabilities        `json:"capabilities,omitempty"`
	Locales      map[string]LocalizedSource   `json:"locales,omitempty"`
}

var (
	hostLabel               = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
	pluginIDPattern         = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,127}$`)
	cgnatPrefix             = netip.MustParsePrefix("100.64.0.0/10")
	allowLocalNetworkForTest bool
)

func validPluginID(id string) bool {
	return pluginIDPattern.MatchString(id) && packagePath(id) && path.Base(id) == id
}

// InspectManifest decodes and validates the common manifest header for any plugin kind.
func InspectManifest(data []byte) (Manifest, error) {
	var header Manifest
	if err := json.Unmarshal(data, &header); err != nil {
		return Manifest{}, fmt.Errorf("plugin manifest: %w", err)
	}
	if !validPluginID(header.ID) || strings.TrimSpace(header.Name) == "" {
		return Manifest{}, fmt.Errorf("invalid plugin identity")
	}
	if !validVersion(header.Version) || !validVersion(header.MinAppVersion) {
		return Manifest{}, fmt.Errorf("invalid semantic version")
	}
	return header, nil
}

func validateManifestHeader(data []byte, appVersion string, expectedKind Kind) (Manifest, error) {
	header, err := InspectManifest(data)
	if err != nil {
		return Manifest{}, err
	}
	if !validVersion(appVersion) {
		return Manifest{}, fmt.Errorf("invalid semantic version")
	}
	if header.APIVersion != protocolVersion || semver.Compare("v"+appVersion, "v"+header.MinAppVersion) < 0 {
		return Manifest{}, fmt.Errorf("%w: host or protocol version", ErrIncompatible)
	}
	if header.Kind != expectedKind {
		if expectedKind == SourceKind {
			if header.Kind == ThemeKind {
				return Manifest{}, fmt.Errorf("%w: theme loading is unavailable", ErrIncompatible)
			}
			return Manifest{}, fmt.Errorf("%w: unknown kind %q", ErrIncompatible, header.Kind)
		}
		return Manifest{}, fmt.Errorf("%w: expected %s kind, got %q", ErrIncompatible, expectedKind, header.Kind)
	}
	return header, nil
}

func decodeSource(data []byte, appVersion string) (sourceManifest, error) {
	if _, err := validateManifestHeader(data, appVersion, SourceKind); err != nil {
		return sourceManifest{}, err
	}

	var manifest sourceManifest
	if err := decodeJSON(data, &manifest); err != nil {
		return sourceManifest{}, fmt.Errorf("source manifest: %w", err)
	}
	hasCredentials := false
	for i, method := range manifest.Authentication.Methods {
		switch method {
		case string(provider.AuthMethodQRCode):
		case string(provider.AuthMethodCredentials):
			hasCredentials = true
		default:
			return sourceManifest{}, fmt.Errorf("%w: authentication method %q", ErrIncompatible, method)
		}
		if slices.Contains(manifest.Authentication.Methods[:i], method) {
			return sourceManifest{}, fmt.Errorf("duplicate authentication method")
		}
	}
	switch manifest.Authentication.ServerURL {
	case "", "none":
	case "optional", "required":
		if !hasCredentials {
			return sourceManifest{}, fmt.Errorf("%w: authentication serverURL requires credentials method", ErrIncompatible)
		}
	default:
		return sourceManifest{}, fmt.Errorf("%w: authentication serverURL %q", ErrIncompatible, manifest.Authentication.ServerURL)
	}
	if manifest.Capabilities.Has(provider.SignIn) != (len(manifest.Authentication.Methods) != 0) {
		return sourceManifest{}, fmt.Errorf("%w: sign-in capability and authentication method must be declared together", ErrIncompatible)
	}
	if (manifest.Capabilities.Has(provider.EditVideo) || manifest.Capabilities.Has(provider.WriteVideoComments) ||
		manifest.Capabilities.Has(provider.WriteDanmaku) || manifest.Capabilities.Has(provider.DanmakuRules) ||
		manifest.Capabilities.Has(provider.EditVideoFavorites) ||
		manifest.Capabilities.Has(provider.FollowCreator) ||
		manifest.Capabilities.Has(provider.ReportPlayback)) &&
		!manifest.Capabilities.Has(provider.SignIn) {
		return sourceManifest{}, fmt.Errorf("%w: video interaction requires sign-in", ErrIncompatible)
	}
	if manifest.Capabilities.Has(provider.BrowseLibrary) && !manifest.Capabilities.Has(provider.SignIn) {
		return sourceManifest{}, fmt.Errorf("%w: %s requires sign-in", ErrIncompatible, provider.BrowseLibrary)
	}
	if manifest.Capabilities.Has(provider.BrowseFollowing) && !manifest.Capabilities.Has(provider.SignIn) {
		return sourceManifest{}, fmt.Errorf("%w: %s requires sign-in", ErrIncompatible, provider.BrowseFollowing)
	}
	for _, section := range []provider.AccountSection{provider.AccountHistory, provider.AccountWatchLater, provider.AccountFavorites, provider.AccountTracking, provider.AccountCourses, provider.AccountNotifications} {
		if manifest.Capabilities.Has(section.Capability()) && !manifest.Capabilities.Has(provider.SignIn) {
			return sourceManifest{}, fmt.Errorf("%w: %s requires sign-in", ErrIncompatible, section.Capability())
		}
		if write := section.RemoveCapability(); write != "" && manifest.Capabilities.Has(write) && !manifest.Capabilities.Has(section.Capability()) {
			return sourceManifest{}, fmt.Errorf("%w: %s requires %s", ErrIncompatible, write, section.Capability())
		}
	}
	if manifest.Capabilities.Has(provider.EditFavoriteFolders) && !manifest.Capabilities.Has(provider.AccountFavorites.Capability()) {
		return sourceManifest{}, fmt.Errorf("%w: %s requires favorite folders", ErrIncompatible, provider.EditFavoriteFolders)
	}
	if manifest.Capabilities.Has(provider.EditNotifications) && !manifest.Capabilities.Has(provider.AccountNotifications.Capability()) {
		return sourceManifest{}, fmt.Errorf("%w: %s requires %s", ErrIncompatible, provider.EditNotifications, provider.AccountNotifications.Capability())
	}
	if manifest.Capabilities.Has(provider.ReadPrivateMessages) && !manifest.Capabilities.Has(provider.SignIn) {
		return sourceManifest{}, fmt.Errorf("%w: %s requires sign-in", ErrIncompatible, provider.ReadPrivateMessages)
	}
	if manifest.Capabilities.Has(provider.WritePrivateMessages) {
		if !manifest.Capabilities.Has(provider.ReadPrivateMessages) {
			return sourceManifest{}, fmt.Errorf("%w: %s requires %s", ErrIncompatible, provider.WritePrivateMessages, provider.ReadPrivateMessages)
		}
		if !manifest.Capabilities.Has(provider.SignIn) {
			return sourceManifest{}, fmt.Errorf("%w: %s requires sign-in", ErrIncompatible, provider.WritePrivateMessages)
		}
	}
	if manifest.Capabilities.Has(provider.SearchSuggest) && !manifest.Capabilities.Has(provider.SearchCatalog) {
		return sourceManifest{}, fmt.Errorf("%w: %s requires %s", ErrIncompatible, provider.SearchSuggest, provider.SearchCatalog)
	}
	if manifest.Capabilities.Has(provider.WriteLiveChat) {
		if !manifest.Capabilities.Has(provider.LiveChat) {
			return sourceManifest{}, fmt.Errorf("%w: %s requires %s", ErrIncompatible, provider.WriteLiveChat, provider.LiveChat)
		}
		if !manifest.Capabilities.Has(provider.SignIn) {
			return sourceManifest{}, fmt.Errorf("%w: %s requires %s", ErrIncompatible, provider.WriteLiveChat, provider.SignIn)
		}
	}
	for i, host := range manifest.Permissions.Network {
		host = strings.ToLower(host)
		if !validHostPattern(host) {
			return sourceManifest{}, fmt.Errorf("invalid network host %q", host)
		}
		manifest.Permissions.Network[i] = host
	}
	if len(manifest.Permissions.Artwork.Hosts) > 0 || manifest.Permissions.Artwork.Referer != "" {
		if !manifest.Capabilities.Has(provider.SignIn) {
			return sourceManifest{}, fmt.Errorf("%w: authenticated artwork requires sign-in", ErrIncompatible)
		}
		if len(manifest.Permissions.Artwork.Hosts) == 0 || len(manifest.Permissions.Artwork.Hosts) > 16 {
			return sourceManifest{}, fmt.Errorf("invalid artwork hosts count")
		}
		allowNet := webapi.AllowHosts(manifest.Permissions.Network...)
		seenHosts := make(map[string]bool, len(manifest.Permissions.Artwork.Hosts))
		cleanedHosts := make([]string, 0, len(manifest.Permissions.Artwork.Hosts))
		for _, rawHost := range manifest.Permissions.Artwork.Hosts {
			host := strings.ToLower(strings.TrimSpace(rawHost))
			if host == "" || strings.HasPrefix(host, "*.") || !validHostPattern(host) {
				return sourceManifest{}, fmt.Errorf("invalid artwork host %q", rawHost)
			}
			hostAddr := host
			if ip, err := netip.ParseAddr(host); err == nil && ip.Is6() {
				hostAddr = "[" + host + "]"
			}
			if err := allowNet(&http.Request{URL: &url.URL{Scheme: "https", Host: hostAddr, Path: "/"}}); err != nil {
				return sourceManifest{}, fmt.Errorf("artwork host %q must be permitted by network permissions", host)
			}
			if !seenHosts[host] {
				seenHosts[host] = true
				cleanedHosts = append(cleanedHosts, host)
			}
		}
		manifest.Permissions.Artwork.Hosts = cleanedHosts
		if ref := strings.TrimSpace(manifest.Permissions.Artwork.Referer); ref != "" {
			refURL := httpsURL(ref)
			if len(ref) > 512 || refURL == nil {
				return sourceManifest{}, fmt.Errorf("invalid artwork referer %q", ref)
			}
			if err := allowNet(&http.Request{URL: refURL}); err != nil {
				return sourceManifest{}, fmt.Errorf("artwork referer %q must be permitted by network permissions", ref)
			}
			manifest.Permissions.Artwork.Referer = refURL.String()
		}
	}
	if !modulePath(manifest.Main) {
		return sourceManifest{}, fmt.Errorf("invalid source entry path")
	}
	return manifest, nil
}

func validVersion(version string) bool {
	v := "v" + version
	return semver.IsValid(v) && semver.Canonical(v) == strings.TrimSuffix(v, semver.Build(v))
}

func isPublicIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	return ip.IsValid() && ip.Zone() == "" &&
		!ip.IsLoopback() && !ip.IsPrivate() &&
		!ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast() &&
		!ip.IsMulticast() && !ip.IsUnspecified() &&
		!cgnatPrefix.Contains(ip)
}

func validHostPattern(host string) bool {
	if host == "" {
		return false
	}
	wildcard := strings.HasPrefix(host, "*.")
	base := strings.TrimPrefix(host, "*.")
	if !wildcard {
		if ip, err := netip.ParseAddr(host); err == nil {
			return ip.Zone() == "" && (isPublicIP(ip) || allowLocalNetworkForTest)
		}
	} else {
		if _, err := netip.ParseAddr(base); err == nil {
			return false
		}
	}
	if len(base) == 0 || len(base) > 253 {
		return false
	}
	labels := strings.Split(base, ".")
	for _, label := range labels {
		if !hostLabel.MatchString(label) {
			return false
		}
	}
	if !allowLocalNetworkForTest && (base == "localhost" || strings.HasSuffix(base, ".localhost")) {
		return false
	}
	if wildcard {
		if len(labels) < 2 {
			return false
		}
		tld := labels[len(labels)-1]
		hasAlpha := false
		for i := range len(tld) {
			if tld[i] >= 'a' && tld[i] <= 'z' {
				hasAlpha = true
				break
			}
		}
		if !hasAlpha {
			return false
		}
		if etld, err := publicsuffix.EffectiveTLDPlusOne(base); err != nil || etld == "" {
			return false
		}
	}
	return true
}

func decodeJSON(data []byte, value any) error {
	return decodeJSONReader(bytes.NewReader(data), value)
}

func decodeJSONReader(input io.Reader, value any) error {
	decoder := json.NewDecoder(input)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return errors.New("expected exactly one JSON value")
	}
	return nil
}
