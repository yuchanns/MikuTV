package plugin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/yuchanns/MikuTV/internal/theme"
	"golang.org/x/mod/semver"
)

type themeManifest struct {
	Manifest
	Theme string `json:"theme"`
}

// ThemePackage is a validated declarative theme plugin loaded without JS/moejs.
type ThemePackage struct {
	manifest    themeManifest
	resolved    theme.Theme
	cover       []byte
	licenseText string
}

func (p *ThemePackage) Manifest() Manifest   { return p.manifest.Manifest }
func (p *ThemePackage) Theme() theme.Theme   { return p.resolved }
func (p *ThemePackage) Cover() []byte        { return append([]byte(nil), p.cover...) }
func (p *ThemePackage) LicenseText() string  { return p.licenseText }

func decodeThemeManifest(data []byte, appVersion string) (themeManifest, error) {
	var header Manifest
	if err := json.Unmarshal(data, &header); err != nil {
		return themeManifest{}, fmt.Errorf("plugin manifest: %w", err)
	}
	if !validPluginID(header.ID) || strings.TrimSpace(header.Name) == "" {
		return themeManifest{}, fmt.Errorf("invalid plugin identity")
	}
	if !validVersion(header.Version) || !validVersion(header.MinAppVersion) || !validVersion(appVersion) {
		return themeManifest{}, fmt.Errorf("invalid semantic version")
	}
	if header.APIVersion != protocolVersion || semver.Compare("v"+appVersion, "v"+header.MinAppVersion) < 0 {
		return themeManifest{}, fmt.Errorf("%w: host or protocol version", ErrIncompatible)
	}
	if header.Kind != ThemeKind {
		return themeManifest{}, fmt.Errorf("%w: expected theme kind, got %q", ErrIncompatible, header.Kind)
	}

	// Strict decode rejects content-source fields (main, authentication, permissions, capabilities, presentation).
	var manifest themeManifest
	if err := decodeJSON(data, &manifest); err != nil {
		return themeManifest{}, fmt.Errorf("theme manifest: %w", err)
	}
	if !packagePath(manifest.Theme) || !strings.HasSuffix(strings.ToLower(manifest.Theme), ".json") {
		return themeManifest{}, fmt.Errorf("invalid theme file path %q", manifest.Theme)
	}
	if manifest.Artwork.Cover != "" && !packagePath(manifest.Artwork.Cover) {
		return themeManifest{}, fmt.Errorf("invalid cover path %q", manifest.Artwork.Cover)
	}
	return manifest, nil
}

// OpenTheme validates a declarative theme plugin.zip without initializing JS.
func OpenTheme(data []byte, appVersion string) (*ThemePackage, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty theme package")
	}
	files, err := readArchive(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	header, err := files.read("plugin.json")
	if err != nil {
		return nil, fmt.Errorf("plugin.json: %w", err)
	}
	manifest, err := decodeThemeManifest(header, appVersion)
	if err != nil {
		return nil, err
	}
	themeData, err := files.read(manifest.Theme)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", manifest.Theme, err)
	}
	resolved, err := theme.Parse(manifest.ID, manifest.Name, themeData)
	if err != nil {
		return nil, err
	}
	cover, err := artworkFile(files, manifest.Artwork.Cover)
	if err != nil {
		return nil, fmt.Errorf("cover %s: %w", manifest.Artwork.Cover, err)
	}
	return &ThemePackage{
		manifest:    manifest,
		resolved:    resolved,
		cover:       cover,
		licenseText: readPluginLicenseText(files),
	}, nil
}
