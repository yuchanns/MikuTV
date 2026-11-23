package plugin

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type InstalledReceipt struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Kind        Kind     `json:"kind"`
	Version     string   `json:"version"`
	Repository  string   `json:"repository"`
	ReleaseID   int64    `json:"releaseId"`
	TagName     string   `json:"tagName"`
	AssetID     int64    `json:"assetId"`
	SHA256      string   `json:"sha256"`
	Enabled     bool     `json:"enabled"`
	InstalledAt int64    `json:"installedAt"`
	License     string   `json:"license,omitempty"`
	Permissions []string `json:"permissions,omitempty"`
}

type InstalledPackage struct {
	Receipt InstalledReceipt
	Source  *Source
}

type PackageStore struct {
	mu   sync.Mutex
	root string
}

func NewPackageStore(root string) *PackageStore {
	return &PackageStore{root: root}
}

func (s *PackageStore) Root() string {
	if s == nil {
		return ""
	}
	return s.root
}

func (s *PackageStore) receiptsPath() string {
	return filepath.Join(s.root, "receipts.json")
}

func (s *PackageStore) stagingDir() string {
	return filepath.Join(s.root, ".staging")
}

func (s *PackageStore) packageZipPath(id string) string {
	return filepath.Join(s.root, "packages", id, "plugin.zip")
}

func (s *PackageStore) LoadReceipts() ([]InstalledReceipt, error) {
	if s == nil || s.root == "" {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadReceiptsLocked()
}

func (s *PackageStore) loadReceiptsLocked() ([]InstalledReceipt, error) {
	raw, err := os.ReadFile(s.receiptsPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var receipts []InstalledReceipt
	if err := json.Unmarshal(raw, &receipts); err != nil {
		return nil, fmt.Errorf("decode installed plugin receipts: %w", err)
	}
	return receipts, nil
}

func (s *PackageStore) saveReceiptsLocked(receipts []InstalledReceipt) error {
	if err := os.MkdirAll(s.root, 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(receipts, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.receiptsPath() + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.receiptsPath())
}

// LoadAll opens every persisted external plugin package, verifying each stored
// archive's SHA-256 against its receipt before compiling it.
func (s *PackageStore) LoadAll(ctx context.Context, appVersion string) ([]InstalledPackage, []error) {
	if s == nil || s.root == "" {
		return nil, nil
	}
	s.mu.Lock()
	receipts, err := s.loadReceiptsLocked()
	s.mu.Unlock()
	if err != nil {
		return nil, []error{err}
	}
	var loaded []InstalledPackage
	var failures []error
	seen := make(map[string]bool, len(receipts))
	for _, receipt := range receipts {
		idKey := strings.ToLower(strings.TrimSpace(receipt.ID))
		if !validPluginID(receipt.ID) || seen[idKey] {
			failures = append(failures, fmt.Errorf("invalid or duplicate installed receipt %q", receipt.ID))
			continue
		}
		seen[idKey] = true
		if receipt.Kind != SourceKind {
			failures = append(failures, fmt.Errorf("installed plugin %s: %w: unsupported kind %q", receipt.ID, ErrIncompatible, receipt.Kind))
			continue
		}
		data, err := os.ReadFile(s.packageZipPath(receipt.ID))
		if err != nil {
			failures = append(failures, fmt.Errorf("installed plugin %s: %w", receipt.ID, err))
			continue
		}
		digest := sha256.Sum256(data)
		if strings.ToLower(receipt.SHA256) != fmt.Sprintf("%x", digest) {
			failures = append(failures, fmt.Errorf("installed plugin %s: %w", receipt.ID, ErrDigestMismatch))
			continue
		}
		src, err := Open(ctx, data, appVersion)
		if err != nil {
			failures = append(failures, fmt.Errorf("installed plugin %s: %w", receipt.ID, err))
			continue
		}
		manifest := src.Manifest()
		if manifest.ID != receipt.ID || manifest.Version != receipt.Version || manifest.Kind != receipt.Kind {
			failures = append(failures, fmt.Errorf("installed plugin %s: manifest does not match receipt", receipt.ID))
			continue
		}
		loaded = append(loaded, InstalledPackage{Receipt: receipt, Source: src})
	}
	return loaded, failures
}

// StageAndInstall writes the downloaded archive to a temporary staging file,
// validates digest, version/tag alignment, manifest, archive safety, JS exports,
// and same-ID source ownership before atomically committing it to disk.
// On any error or cancellation, all staged files are cleaned up.
func (s *PackageStore) StageAndInstall(
	ctx context.Context,
	data []byte,
	item CatalogItem,
	appVersion string,
	boundSources map[string]string, // lowercase pluginID -> bound repo ("" for bundled)
) (*Source, InstalledReceipt, error) {
	if s == nil || s.root == "" {
		return nil, InstalledReceipt{}, fmt.Errorf("plugin package store is unavailable")
	}
	if err := ctx.Err(); err != nil {
		return nil, InstalledReceipt{}, err
	}
	if err := os.MkdirAll(s.stagingDir(), 0o755); err != nil {
		return nil, InstalledReceipt{}, err
	}
	stagedFile, err := os.CreateTemp(s.stagingDir(), "install-*.zip")
	if err != nil {
		return nil, InstalledReceipt{}, err
	}
	stagedPath := stagedFile.Name()
	cleanupStaged := true
	defer func() {
		if cleanupStaged {
			_ = os.Remove(stagedPath)
		}
	}()
	if _, err := stagedFile.Write(data); err != nil {
		_ = stagedFile.Close()
		return nil, InstalledReceipt{}, err
	}
	if err := stagedFile.Close(); err != nil {
		return nil, InstalledReceipt{}, err
	}

	if err := ctx.Err(); err != nil {
		return nil, InstalledReceipt{}, err
	}
	digest := sha256.Sum256(data)
	digestHex := fmt.Sprintf("%x", digest)
	if expected := strings.ToLower(strings.TrimSpace(item.Release.SHA256)); expected != "" && digestHex != expected {
		return nil, InstalledReceipt{}, fmt.Errorf("%w: got %s want %s", ErrDigestMismatch, digestHex, expected)
	}

	// Validate package completely without executing plugin JS.
	src, err := Open(ctx, data, appVersion)
	if err != nil {
		return nil, InstalledReceipt{}, err
	}
	manifest := src.Manifest()
	if item.Manifest.ID != "" && !strings.EqualFold(manifest.ID, item.Manifest.ID) {
		return nil, InstalledReceipt{}, fmt.Errorf("package id %q does not match previewed %q", manifest.ID, item.Manifest.ID)
	}
	if item.Manifest.Kind != "" && manifest.Kind != item.Manifest.Kind {
		return nil, InstalledReceipt{}, fmt.Errorf("package kind %q does not match previewed %q", manifest.Kind, item.Manifest.Kind)
	}
	if item.Manifest.Version != "" && manifest.Version != item.Manifest.Version {
		return nil, InstalledReceipt{}, fmt.Errorf("package version %q does not match previewed %q", manifest.Version, item.Manifest.Version)
	}
	if !ReleaseVersionMatches(item.Release.TagName, manifest.Version) {
		return nil, InstalledReceipt{}, fmt.Errorf("%w: release %q vs manifest %q", ErrVersionTagMismatch, item.Release.TagName, manifest.Version)
	}
	if err := ctx.Err(); err != nil {
		return nil, InstalledReceipt{}, err
	}

	normID := strings.ToLower(manifest.ID)
	normRepo := strings.ToLower(strings.TrimSpace(item.Repository))

	s.mu.Lock()
	defer s.mu.Unlock()

	if boundRepo, exists := boundSources[normID]; exists {
		if boundRepo == "" || !strings.EqualFold(boundRepo, normRepo) {
			return nil, InstalledReceipt{}, fmt.Errorf("%w: %s", ErrSourceConflict, manifest.ID)
		}
	}
	receipts, err := s.loadReceiptsLocked()
	if err != nil {
		return nil, InstalledReceipt{}, err
	}
	for _, existing := range receipts {
		if strings.EqualFold(existing.ID, manifest.ID) {
			if !strings.EqualFold(existing.Repository, item.Repository) {
				return nil, InstalledReceipt{}, fmt.Errorf("%w: %s", ErrSourceConflict, manifest.ID)
			}
		}
	}

	destPath := s.packageZipPath(manifest.ID)
	destDir := filepath.Dir(destPath)
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return nil, InstalledReceipt{}, err
	}
	if err := os.Rename(stagedPath, destPath); err != nil {
		return nil, InstalledReceipt{}, err
	}
	cleanupStaged = false

	receipt := InstalledReceipt{
		ID:          manifest.ID,
		Name:        manifest.Name,
		Description: manifest.Description,
		Kind:        manifest.Kind,
		Version:     manifest.Version,
		Repository:  item.Repository,
		ReleaseID:   item.Release.ReleaseID,
		TagName:     item.Release.TagName,
		AssetID:     item.Release.AssetID,
		SHA256:      digestHex,
		Enabled:     true,
		InstalledAt: time.Now().Unix(),
		License:     manifest.License,
		Permissions: append([]string(nil), src.manifest.Permissions.Network...),
	}
	updated := make([]InstalledReceipt, 0, len(receipts)+1)
	replaced := false
	for _, existing := range receipts {
		if strings.EqualFold(existing.ID, receipt.ID) {
			updated = append(updated, receipt)
			replaced = true
		} else {
			updated = append(updated, existing)
		}
	}
	if !replaced {
		updated = append(updated, receipt)
	}
	if err := s.saveReceiptsLocked(updated); err != nil {
		_ = os.RemoveAll(destDir)
		return nil, InstalledReceipt{}, err
	}
	return src, receipt, nil
}
