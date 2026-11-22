package plugin

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/yuchanns/MikuTV/internal/provider"
	"golang.org/x/mod/semver"
	"golang.org/x/net/publicsuffix"
)

type Kind string

const (
	SourceKind      Kind = "source"
	ThemeKind       Kind = "theme"
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
	Artwork       struct {
		Cover string `json:"cover,omitempty"`
	} `json:"artwork,omitempty"`
}

type sourceManifest struct {
	Manifest
	Main           string `json:"main"`
	Authentication struct {
		Methods      []string                  `json:"methods,omitempty"`
		Presentation provider.AuthPresentation `json:"presentation,omitzero"`
	} `json:"authentication,omitempty"`
	Permissions struct {
		Network []string `json:"network,omitempty"`
	} `json:"permissions,omitempty"`
	Presentation provider.SourcePresentation `json:"presentation,omitzero"`
	Capabilities provider.Capabilities       `json:"capabilities,omitempty"`
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

func decodeSource(data []byte, appVersion string) (sourceManifest, error) {
	var header Manifest
	if err := json.Unmarshal(data, &header); err != nil {
		return sourceManifest{}, fmt.Errorf("plugin manifest: %w", err)
	}
	if !validPluginID(header.ID) || strings.TrimSpace(header.Name) == "" {
		return sourceManifest{}, fmt.Errorf("invalid plugin identity")
	}
	if !validVersion(header.Version) || !validVersion(header.MinAppVersion) || !validVersion(appVersion) {
		return sourceManifest{}, fmt.Errorf("invalid semantic version")
	}
	if header.APIVersion != protocolVersion || semver.Compare("v"+appVersion, "v"+header.MinAppVersion) < 0 {
		return sourceManifest{}, fmt.Errorf("%w: host or protocol version", ErrIncompatible)
	}
	switch header.Kind {
	case SourceKind:
	case ThemeKind:
		return sourceManifest{}, fmt.Errorf("%w: theme loading is unavailable", ErrIncompatible)
	default:
		return sourceManifest{}, fmt.Errorf("%w: unknown kind %q", ErrIncompatible, header.Kind)
	}

	var manifest sourceManifest
	if err := decodeJSON(data, &manifest); err != nil {
		return sourceManifest{}, fmt.Errorf("source manifest: %w", err)
	}
	for i, method := range manifest.Authentication.Methods {
		if method != "qrCode" {
			return sourceManifest{}, fmt.Errorf("%w: authentication method %q", ErrIncompatible, method)
		}
		if slices.Contains(manifest.Authentication.Methods[:i], method) {
			return sourceManifest{}, fmt.Errorf("duplicate authentication method")
		}
	}
	if manifest.Capabilities.Has(provider.SignIn) != (len(manifest.Authentication.Methods) != 0) {
		return sourceManifest{}, fmt.Errorf("%w: sign-in capability and authentication method must be declared together", ErrIncompatible)
	}
	if (manifest.Capabilities.Has(provider.EditVideo) || manifest.Capabilities.Has(provider.WriteVideoComments) ||
		manifest.Capabilities.Has(provider.WriteDanmaku) || manifest.Capabilities.Has(provider.EditVideoFavorites) ||
		manifest.Capabilities.Has(provider.ReportPlayback)) &&
		!manifest.Capabilities.Has(provider.SignIn) {
		return sourceManifest{}, fmt.Errorf("%w: video interaction requires sign-in", ErrIncompatible)
	}
	if manifest.Capabilities.Has(provider.BrowseLibrary) && !manifest.Capabilities.Has(provider.SignIn) {
		return sourceManifest{}, fmt.Errorf("%w: %s requires sign-in", ErrIncompatible, provider.BrowseLibrary)
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
	if manifest.Capabilities.Has(provider.SearchSuggest) && !manifest.Capabilities.Has(provider.SearchCatalog) {
		return sourceManifest{}, fmt.Errorf("%w: %s requires %s", ErrIncompatible, provider.SearchSuggest, provider.SearchCatalog)
	}
	for i, host := range manifest.Permissions.Network {
		host = strings.ToLower(host)
		if !validHostPattern(host) {
			return sourceManifest{}, fmt.Errorf("invalid network host %q", host)
		}
		manifest.Permissions.Network[i] = host
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
