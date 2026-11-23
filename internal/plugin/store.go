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

	"github.com/yuchanns/MikuTV/internal/theme"
)

type InstalledReceipt struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Kind        Kind           `json:"kind"`
	Version     string         `json:"version"`
	Repository  string         `json:"repository"`
	ReleaseID   int64          `json:"releaseId"`
	TagName     string         `json:"tagName"`
	AssetID     int64          `json:"assetId"`
	SHA256      string         `json:"sha256"`
	Enabled     bool           `json:"enabled"`
	InstalledAt int64          `json:"installedAt"`
	UpdatedAt   int64          `json:"updatedAt,omitempty"`
	License     string         `json:"license,omitempty"`
	Permissions []string       `json:"permissions,omitempty"`
	Swatches    [6]theme.Color `json:"swatches,omitempty"`
}

// InstalledPackage holds a verified external plugin receipt paired with its
// loaded kind-specific runtime representation (either Source or Theme).
type InstalledPackage struct {
	Receipt InstalledReceipt
	Source  *Source
	Theme   *ThemePackage
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

func (s *PackageStore) statePath() string {
	return filepath.Join(s.root, "state.json")
}

// AddedPermissions returns normalized network domains present in incoming that
// were not previously approved in existing.
func AddedPermissions(existing, incoming []string) []string {
	known := make(map[string]bool, len(existing))
	for _, host := range existing {
		if norm := strings.ToLower(strings.TrimSpace(host)); norm != "" {
			known[norm] = true
		}
	}
	seen := make(map[string]bool, len(incoming))
	var added []string
	for _, host := range incoming {
		norm := strings.ToLower(strings.TrimSpace(host))
		if norm == "" || known[norm] || seen[norm] {
			continue
		}
		seen[norm] = true
		added = append(added, norm)
	}
	return added
}

type AppearanceState struct {
	ActiveTheme  string
	HoverPreview bool
	ReduceMotion bool
}

type packageStoreState struct {
	DisabledBundled []string `json:"disabledBundled,omitempty"`
	ActiveTheme     string   `json:"activeTheme,omitempty"`
	HoverPreview    *bool    `json:"hoverPreview,omitempty"`
	ReduceMotion    bool     `json:"reduceMotion,omitempty"`
}

func (s *PackageStore) LoadAppearanceState() (AppearanceState, error) {
	def := AppearanceState{HoverPreview: true}
	if s == nil || s.root == "" {
		return def, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.loadStateLocked()
	if err != nil {
		return def, err
	}
	hover := true
	if st.HoverPreview != nil {
		hover = *st.HoverPreview
	}
	return AppearanceState{
		ActiveTheme:  strings.TrimSpace(st.ActiveTheme),
		HoverPreview: hover,
		ReduceMotion: st.ReduceMotion,
	}, nil
}

func (s *PackageStore) SaveActiveTheme(id string) error {
	if s == nil || s.root == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.loadStateLocked()
	if err != nil {
		return err
	}
	st.ActiveTheme = strings.TrimSpace(id)
	return s.saveStateLocked(st)
}

func (s *PackageStore) SaveAppearanceMotion(hoverPreview, reduceMotion bool) error {
	if s == nil || s.root == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.loadStateLocked()
	if err != nil {
		return err
	}
	hp := hoverPreview
	st.HoverPreview = &hp
	st.ReduceMotion = reduceMotion
	return s.saveStateLocked(st)
}

func (s *PackageStore) loadStateLocked() (packageStoreState, error) {
	raw, err := os.ReadFile(s.statePath())
	if err != nil {
		if os.IsNotExist(err) {
			return packageStoreState{}, nil
		}
		return packageStoreState{}, err
	}
	var st packageStoreState
	if err := json.Unmarshal(raw, &st); err != nil {
		return packageStoreState{}, fmt.Errorf("decode plugin store state: %w", err)
	}
	return st, nil
}

func (s *PackageStore) saveStateLocked(st packageStoreState) error {
	if err := os.MkdirAll(s.root, 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.statePath() + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.statePath())
}

func (s *PackageStore) LoadDisabledBundled() ([]string, error) {
	if s == nil || s.root == "" {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.loadStateLocked()
	if err != nil {
		return nil, err
	}
	return append([]string(nil), st.DisabledBundled...), nil
}

func (s *PackageStore) SetBundledEnabled(id string, enabled bool) error {
	if s == nil || s.root == "" {
		return nil
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("plugin id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.loadStateLocked()
	if err != nil {
		return err
	}
	next := make([]string, 0, len(st.DisabledBundled)+1)
	found := false
	for _, existing := range st.DisabledBundled {
		if strings.EqualFold(existing, id) {
			found = true
			if !enabled {
				next = append(next, existing)
			}
			continue
		}
		next = append(next, existing)
	}
	if !enabled && !found {
		next = append(next, id)
	}
	st.DisabledBundled = next
	return s.saveStateLocked(st)
}

func (s *PackageStore) SetExternalEnabled(id string, enabled bool) (InstalledReceipt, error) {
	if s == nil || s.root == "" {
		return InstalledReceipt{}, fmt.Errorf("plugin package store is unavailable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	receipts, err := s.loadReceiptsLocked()
	if err != nil {
		return InstalledReceipt{}, err
	}
	idx := -1
	for i, r := range receipts {
		if strings.EqualFold(r.ID, id) {
			idx = i
			break
		}
	}
	if idx < 0 {
		return InstalledReceipt{}, os.ErrNotExist
	}
	updated := append([]InstalledReceipt(nil), receipts...)
	updated[idx].Enabled = enabled
	if err := s.saveReceiptsLocked(updated); err != nil {
		return InstalledReceipt{}, err
	}
	return updated[idx], nil
}

// Remove deletes an externally installed plugin's receipt atomically first,
// then removes its stored package directory from disk.
func (s *PackageStore) Remove(id string) (InstalledReceipt, error) {
	if s == nil || s.root == "" {
		return InstalledReceipt{}, fmt.Errorf("plugin package store is unavailable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	receipts, err := s.loadReceiptsLocked()
	if err != nil {
		return InstalledReceipt{}, err
	}
	idx := -1
	for i, r := range receipts {
		if strings.EqualFold(r.ID, id) {
			idx = i
			break
		}
	}
	if idx < 0 {
		return InstalledReceipt{}, os.ErrNotExist
	}
	removed := receipts[idx]
	updated := make([]InstalledReceipt, 0, len(receipts)-1)
	updated = append(updated, receipts[:idx]...)
	updated = append(updated, receipts[idx+1:]...)
	if err := s.saveReceiptsLocked(updated); err != nil {
		return InstalledReceipt{}, err
	}
	_ = os.RemoveAll(filepath.Dir(s.packageZipPath(removed.ID)))
	return removed, nil
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
		switch receipt.Kind {
		case SourceKind:
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
		case ThemeKind:
			pkg, err := OpenTheme(data, appVersion)
			if err != nil {
				failures = append(failures, fmt.Errorf("installed theme %s: %w", receipt.ID, err))
				continue
			}
			manifest := pkg.Manifest()
			if manifest.ID != receipt.ID || manifest.Version != receipt.Version || manifest.Kind != receipt.Kind {
				failures = append(failures, fmt.Errorf("installed theme %s: manifest does not match receipt", receipt.ID))
				continue
			}
			receipt.Swatches = pkg.Theme().Swatches()
			loaded = append(loaded, InstalledPackage{Receipt: receipt, Theme: pkg})
		default:
			failures = append(failures, fmt.Errorf("installed plugin %s: %w: unsupported kind %q", receipt.ID, ErrIncompatible, receipt.Kind))
		}
	}
	return loaded, failures
}

// StageAndInstall writes the downloaded archive to a temporary staging file,
// validates digest, version/tag alignment, manifest, archive safety, kind-specific
// payload (JS exports without execution for source; theme.json schema & contrast
// without JS for theme), and same-ID source ownership before atomically
// committing it to disk. On any error or cancellation, all staged files are cleaned up.
func (s *PackageStore) StageAndInstall(
	ctx context.Context,
	data []byte,
	item CatalogItem,
	appVersion string,
	boundSources map[string]string, // lowercase pluginID -> bound repo ("" for bundled)
) (InstalledPackage, error) {
	if s == nil || s.root == "" {
		return InstalledPackage{}, fmt.Errorf("plugin package store is unavailable")
	}
	if err := ctx.Err(); err != nil {
		return InstalledPackage{}, err
	}
	if err := os.MkdirAll(s.stagingDir(), 0o755); err != nil {
		return InstalledPackage{}, err
	}
	stagedFile, err := os.CreateTemp(s.stagingDir(), "install-*.zip")
	if err != nil {
		return InstalledPackage{}, err
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
		return InstalledPackage{}, err
	}
	if err := stagedFile.Close(); err != nil {
		return InstalledPackage{}, err
	}

	if err := ctx.Err(); err != nil {
		return InstalledPackage{}, err
	}
	digest := sha256.Sum256(data)
	digestHex := fmt.Sprintf("%x", digest)
	if expected := strings.ToLower(strings.TrimSpace(item.Release.SHA256)); expected != "" && digestHex != expected {
		return InstalledPackage{}, fmt.Errorf("%w: got %s want %s", ErrDigestMismatch, digestHex, expected)
	}

	inspected, err := InspectPackage(data, appVersion)
	if err != nil {
		return InstalledPackage{}, err
	}
	if !inspected.Compatibility.Compatible {
		return InstalledPackage{}, fmt.Errorf("%w: %s", ErrIncompatible, inspected.Compatibility.Code)
	}

	var src *Source
	var themePkg *ThemePackage
	var manifest Manifest
	var perms []string
	var swatches [6]theme.Color
	enabledByDefault := true

	switch inspected.Manifest.Kind {
	case SourceKind:
		opened, err := Open(ctx, data, appVersion)
		if err != nil {
			return InstalledPackage{}, err
		}
		src = opened
		manifest = opened.Manifest()
		perms = append([]string(nil), opened.manifest.Permissions.Network...)
	case ThemeKind:
		opened, err := OpenTheme(data, appVersion)
		if err != nil {
			return InstalledPackage{}, err
		}
		themePkg = opened
		manifest = opened.Manifest()
		swatches = opened.Theme().Swatches()
		// Themes are installed without auto-applying (§7.2).
		enabledByDefault = false
	default:
		return InstalledPackage{}, fmt.Errorf("%w: unsupported kind %q", ErrIncompatible, inspected.Manifest.Kind)
	}

	if item.Manifest.ID != "" && !strings.EqualFold(manifest.ID, item.Manifest.ID) {
		return InstalledPackage{}, fmt.Errorf("package id %q does not match previewed %q", manifest.ID, item.Manifest.ID)
	}
	if item.Manifest.Kind != "" && manifest.Kind != item.Manifest.Kind {
		return InstalledPackage{}, fmt.Errorf("package kind %q does not match previewed %q", manifest.Kind, item.Manifest.Kind)
	}
	if item.Manifest.Version != "" && manifest.Version != item.Manifest.Version {
		return InstalledPackage{}, fmt.Errorf("package version %q does not match previewed %q", manifest.Version, item.Manifest.Version)
	}
	if !ReleaseVersionMatches(item.Release.TagName, manifest.Version) {
		return InstalledPackage{}, fmt.Errorf("%w: release %q vs manifest %q", ErrVersionTagMismatch, item.Release.TagName, manifest.Version)
	}
	if err := ctx.Err(); err != nil {
		return InstalledPackage{}, err
	}

	normID := strings.ToLower(manifest.ID)
	normRepo := strings.ToLower(strings.TrimSpace(item.Repository))

	s.mu.Lock()
	defer s.mu.Unlock()

	if boundRepo, exists := boundSources[normID]; exists {
		if boundRepo == "" || !strings.EqualFold(boundRepo, normRepo) {
			return InstalledPackage{}, fmt.Errorf("%w: %s", ErrSourceConflict, manifest.ID)
		}
	}
	receipts, err := s.loadReceiptsLocked()
	if err != nil {
		return InstalledPackage{}, err
	}
	for _, existing := range receipts {
		if strings.EqualFold(existing.ID, manifest.ID) {
			if !strings.EqualFold(existing.Repository, item.Repository) {
				return InstalledPackage{}, fmt.Errorf("%w: %s", ErrSourceConflict, manifest.ID)
			}
		}
	}

	destPath := s.packageZipPath(manifest.ID)
	destDir := filepath.Dir(destPath)
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return InstalledPackage{}, err
	}
	if err := os.Rename(stagedPath, destPath); err != nil {
		return InstalledPackage{}, err
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
		Enabled:     enabledByDefault,
		InstalledAt: time.Now().Unix(),
		License:     manifest.License,
		Permissions: perms,
		Swatches:    swatches,
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
		return InstalledPackage{}, err
	}
	return InstalledPackage{Receipt: receipt, Source: src, Theme: themePkg}, nil
}

// StageAndUpdate validates a downloaded update archive in staging without
// executing JS, enforcing pinned SHA-256, same repository, same plugin ID,
// unchanged kind, tag/version alignment, and host compatibility before
// atomically replacing the stored package. On any failure or cancellation,
// the original package and receipt remain completely intact.
func (s *PackageStore) StageAndUpdate(
	ctx context.Context,
	data []byte,
	existing InstalledReceipt,
	target CatalogItem,
	appVersion string,
) (InstalledPackage, error) {
	if s == nil || s.root == "" {
		return InstalledPackage{}, fmt.Errorf("plugin package store is unavailable")
	}
	if err := ctx.Err(); err != nil {
		return InstalledPackage{}, err
	}
	if strings.TrimSpace(existing.ID) == "" || strings.TrimSpace(existing.Repository) == "" {
		return InstalledPackage{}, fmt.Errorf("invalid installed receipt for update")
	}
	if !strings.EqualFold(strings.TrimSpace(existing.Repository), strings.TrimSpace(target.Repository)) {
		return InstalledPackage{}, fmt.Errorf("%w: update repository %q does not match %q", ErrSourceConflict, target.Repository, existing.Repository)
	}
	if err := os.MkdirAll(s.stagingDir(), 0o755); err != nil {
		return InstalledPackage{}, err
	}
	stagedFile, err := os.CreateTemp(s.stagingDir(), "update-*.zip")
	if err != nil {
		return InstalledPackage{}, err
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
		return InstalledPackage{}, err
	}
	if err := stagedFile.Close(); err != nil {
		return InstalledPackage{}, err
	}
	if err := ctx.Err(); err != nil {
		return InstalledPackage{}, err
	}

	digest := sha256.Sum256(data)
	digestHex := fmt.Sprintf("%x", digest)
	if expected := strings.ToLower(strings.TrimSpace(target.Release.SHA256)); expected != "" && digestHex != expected {
		return InstalledPackage{}, fmt.Errorf("%w: got %s want %s", ErrDigestMismatch, digestHex, expected)
	}

	// Inspect header first to authoritatively block kind changes before kind-specific validation.
	inspected, err := InspectPackage(data, appVersion)
	if err != nil {
		return InstalledPackage{}, err
	}
	if !strings.EqualFold(inspected.Manifest.ID, existing.ID) {
		return InstalledPackage{}, fmt.Errorf("%w: package id %q does not match installed %q", ErrSourceConflict, inspected.Manifest.ID, existing.ID)
	}
	if inspected.Manifest.Kind != existing.Kind {
		return InstalledPackage{}, fmt.Errorf("%w: %s -> %s", ErrKindChanged, existing.Kind, inspected.Manifest.Kind)
	}
	if target.Manifest.Version != "" && inspected.Manifest.Version != target.Manifest.Version {
		return InstalledPackage{}, fmt.Errorf("package version %q does not match pinned %q", inspected.Manifest.Version, target.Manifest.Version)
	}
	if !ReleaseVersionMatches(target.Release.TagName, inspected.Manifest.Version) {
		return InstalledPackage{}, fmt.Errorf("%w: release %q vs manifest %q", ErrVersionTagMismatch, target.Release.TagName, inspected.Manifest.Version)
	}
	if !inspected.Compatibility.Compatible {
		return InstalledPackage{}, fmt.Errorf("%w: %s", ErrIncompatible, inspected.Compatibility.Code)
	}

	var src *Source
	var themePkg *ThemePackage
	var perms []string
	var swatches [6]theme.Color
	switch existing.Kind {
	case SourceKind:
		opened, err := Open(ctx, data, appVersion)
		if err != nil {
			return InstalledPackage{}, err
		}
		src = opened
		perms = append([]string(nil), opened.manifest.Permissions.Network...)
	case ThemeKind:
		opened, err := OpenTheme(data, appVersion)
		if err != nil {
			return InstalledPackage{}, err
		}
		themePkg = opened
		swatches = opened.Theme().Swatches()
	default:
		return InstalledPackage{}, fmt.Errorf("%w: unsupported kind %q", ErrIncompatible, existing.Kind)
	}
	if err := ctx.Err(); err != nil {
		return InstalledPackage{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	receipts, err := s.loadReceiptsLocked()
	if err != nil {
		return InstalledPackage{}, err
	}
	idx := -1
	for i, r := range receipts {
		if strings.EqualFold(r.ID, existing.ID) {
			if !strings.EqualFold(r.Repository, existing.Repository) {
				return InstalledPackage{}, fmt.Errorf("%w: %s", ErrSourceConflict, existing.ID)
			}
			if r.Kind != existing.Kind {
				return InstalledPackage{}, fmt.Errorf("%w: %s -> %s", ErrKindChanged, r.Kind, existing.Kind)
			}
			idx = i
			break
		}
	}
	if idx < 0 {
		return InstalledPackage{}, fmt.Errorf("installed plugin %s not found in receipts", existing.ID)
	}

	destPath := s.packageZipPath(existing.ID)
	destDir := filepath.Dir(destPath)
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return InstalledPackage{}, err
	}
	backupPath := destPath + ".bak"
	hadBackup := false
	if _, err := os.Stat(destPath); err == nil {
		_ = os.Remove(backupPath)
		if err := os.Rename(destPath, backupPath); err != nil {
			return InstalledPackage{}, err
		}
		hadBackup = true
	}
	if err := os.Rename(stagedPath, destPath); err != nil {
		if hadBackup {
			_ = os.Rename(backupPath, destPath)
		}
		return InstalledPackage{}, err
	}
	cleanupStaged = false

	prev := receipts[idx]
	nextReceipt := InstalledReceipt{
		ID:          inspected.Manifest.ID,
		Name:        inspected.Manifest.Name,
		Description: inspected.Manifest.Description,
		Kind:        inspected.Manifest.Kind,
		Version:     inspected.Manifest.Version,
		Repository:  prev.Repository,
		ReleaseID:   target.Release.ReleaseID,
		TagName:     target.Release.TagName,
		AssetID:     target.Release.AssetID,
		SHA256:      digestHex,
		Enabled:     prev.Enabled,
		InstalledAt: prev.InstalledAt,
		UpdatedAt:   time.Now().Unix(),
		License:     inspected.Manifest.License,
		Permissions: perms,
		Swatches:    swatches,
	}
	updated := append([]InstalledReceipt(nil), receipts...)
	updated[idx] = nextReceipt
	if err := s.saveReceiptsLocked(updated); err != nil {
		_ = os.Remove(destPath)
		if hadBackup {
			_ = os.Rename(backupPath, destPath)
		}
		return InstalledPackage{}, err
	}
	if hadBackup {
		_ = os.Remove(backupPath)
	}
	return InstalledPackage{Receipt: nextReceipt, Source: src, Theme: themePkg}, nil
}
