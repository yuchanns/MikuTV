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
		Methods []string `json:"methods,omitempty"`
	} `json:"authentication,omitempty"`
	Permissions struct {
		Network []string `json:"network,omitempty"`
	} `json:"permissions,omitempty"`
	Capabilities provider.Capabilities `json:"capabilities,omitempty"`
}

var (
	hostLabel                = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
	pluginIDPattern          = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,127}$`)
	cgnatPrefix              = netip.MustParsePrefix("100.64.0.0/10")
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

func validHostPattern(host string) bool {
	wildcard := strings.HasPrefix(host, "*.")
	base := strings.TrimPrefix(host, "*.")
	if base == "" || len(base) > 253 {
		return false
	}
	if ip, err := netip.ParseAddr(base); err == nil {
		if wildcard || ip.Zone() != "" {
			return false
		}
		if allowLocalNetworkForTest {
			return true
		}
		return isPublicAddr(ip)
	}
	labels := strings.Split(base, ".")
	for _, label := range labels {
		if !hostLabel.MatchString(label) {
			return false
		}
	}
	if base == "localhost" || strings.HasSuffix(base, ".localhost") {
		return allowLocalNetworkForTest
	}
	if wildcard {
		// Reject wildcards spanning an entire public suffix (e.g. "*.com", "*.co.uk") or an IP tail.
		if _, err := publicsuffix.EffectiveTLDPlusOne(base); err != nil {
			return false
		}
		allDigits := true
		for _, label := range labels {
			for i := 0; i < len(label); i++ {
				if label[i] < '0' || label[i] > '9' {
					allDigits = false
					break
				}
			}
		}
		if allDigits {
			return false
		}
	}
	return true
}

func isPublicAddr(addr netip.Addr) bool {
	addr = addr.Unmap()
	if !addr.IsValid() || addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast() ||
		addr.IsLinkLocalMulticast() || addr.IsMulticast() || addr.IsUnspecified() ||
		addr.IsInterfaceLocalMulticast() || cgnatPrefix.Contains(addr) {
		return false
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
