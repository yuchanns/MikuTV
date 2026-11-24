package app

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime/debug"
	"slices"
	"strings"
)

const (
	DefaultAppLicense    = "Apache License 2.0"
	DefaultAppPlatform   = "Apple TV \u00b7 tvOS"
	DefaultAppProjectURL = "https://github.com/yuchanns/MikuTV"
)

type AboutPlugin struct {
	ID           string
	Name         string
	Kind         string
	Version      string
	Bundled      bool
	SourceCommit string
	License      string
	LicenseText  string
}

type AboutDependency struct {
	Module      string `json:"module"`
	Version     string `json:"version"`
	License     string `json:"license,omitempty"`
	Indirect    bool   `json:"indirect,omitempty"`
	LicenseText string `json:"licenseText,omitempty"`
}

type AboutInfo struct {
	Version      string
	Build        string
	Commit       string
	License      string
	Platform     string
	ProjectURL   string
	Plugins      []AboutPlugin
	Dependencies []AboutDependency
}

func (a AboutInfo) Normalized(fallbackVersion string) AboutInfo {
	out := a
	if strings.TrimSpace(out.Version) == "" {
		out.Version = strings.TrimSpace(fallbackVersion)
	}
	if out.Version == "" {
		out.Version = "0.1.0"
	}
	out.Build = strings.TrimSpace(out.Build)
	out.Commit = strings.TrimSpace(out.Commit)
	if strings.TrimSpace(out.License) == "" {
		out.License = DefaultAppLicense
	}
	if strings.TrimSpace(out.Platform) == "" {
		out.Platform = DefaultAppPlatform
	}
	if strings.TrimSpace(out.ProjectURL) == "" {
		out.ProjectURL = DefaultAppProjectURL
	}
	return out
}

func (a AboutInfo) DisplayVersion() string {
	norm := a.Normalized("")
	if norm.Build != "" {
		return norm.Version + " (" + norm.Build + ")"
	}
	return norm.Version
}

func LoadBundledDependencies(path string) ([]AboutDependency, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return DecodeBundledDependencies(data)
}

func DecodeBundledDependencies(data []byte) ([]AboutDependency, error) {
	var raw []AboutDependency
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("decode bundled licenses: %w", err)
	}
	seen := make(map[string]bool, len(raw))
	out := make([]AboutDependency, 0, len(raw))
	for _, dep := range raw {
		dep.Module = strings.TrimSpace(dep.Module)
		dep.Version = strings.TrimSpace(dep.Version)
		dep.License = strings.TrimSpace(dep.License)
		dep.LicenseText = strings.TrimSpace(strings.ReplaceAll(dep.LicenseText, "\r\n", "\n"))
		if dep.Module == "" {
			return nil, fmt.Errorf("bundled license entry missing module path")
		}
		if seen[dep.Module] {
			continue
		}
		seen[dep.Module] = true
		out = append(out, dep)
	}
	sortAboutDependencies(out)
	return out, nil
}

func RuntimeDependencies() []AboutDependency {
	info, ok := debug.ReadBuildInfo()
	if !ok || info == nil {
		return nil
	}
	out := make([]AboutDependency, 0, len(info.Deps))
	for _, dep := range info.Deps {
		if dep == nil {
			continue
		}
		resolved := dep
		if dep.Replace != nil {
			resolved = dep.Replace
		}
		modPath := strings.TrimSpace(resolved.Path)
		if modPath == "" {
			continue
		}
		out = append(out, AboutDependency{
			Module:  modPath,
			Version: strings.TrimSpace(resolved.Version),
		})
	}
	sortAboutDependencies(out)
	return out
}

func sortAboutDependencies(deps []AboutDependency) {
	slices.SortStableFunc(deps, func(a, b AboutDependency) int {
		if cmp := strings.Compare(strings.ToLower(a.Module), strings.ToLower(b.Module)); cmp != 0 {
			return cmp
		}
		return strings.Compare(a.Module, b.Module)
	})
}
