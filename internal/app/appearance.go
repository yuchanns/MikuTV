package app

import (
	"fmt"
	"strings"
	"sync"

	"github.com/yuchanns/MikuTV/internal/plugin"
	"github.com/yuchanns/MikuTV/internal/theme"
)

type ThemeOption struct {
	ID          string
	Name        string
	Description string
	Version     string
	Bundled     bool
	Active      bool
	Swatches    [6]theme.Color
	Theme       theme.Theme
}

type AppearanceSnapshot struct {
	ActiveID     string
	ActiveTheme  theme.Theme
	HoverPreview bool
	ReduceMotion bool
	Options      []ThemeOption
}

// AppearanceSettings owns the global theme selection and motion preferences.
// Theme selection is independent of content entries, accounts, and playback (§7.2).
type AppearanceSettings struct {
	store      *plugin.PackageStore
	onTheme    func(theme.Theme)
	onSyncCat  func(activeThemeID string)
	invalidate func()

	mu           sync.Mutex
	activeID     string // "" or "mikutv-dark" means built-in default
	activeTheme  theme.Theme
	hoverPreview bool
	reduceMotion bool
	order        []string
	themes       map[string]ThemeOption
}

func NewAppearanceSettings(
	store *plugin.PackageStore,
	initialThemes []*plugin.ThemePackage,
	onTheme func(theme.Theme),
	onSyncCat func(string),
	invalidate func(),
) *AppearanceSettings {
	a := &AppearanceSettings{
		store:        store,
		onTheme:      onTheme,
		onSyncCat:    onSyncCat,
		invalidate:   invalidate,
		activeTheme:  theme.Default(),
		hoverPreview: true,
		themes:       make(map[string]ThemeOption, len(initialThemes)),
	}
	for _, pkg := range initialThemes {
		if pkg == nil {
			continue
		}
		a.registerLocked(pkg)
	}
	if store != nil {
		if st, err := store.LoadAppearanceState(); err == nil {
			a.hoverPreview = st.HoverPreview
			a.reduceMotion = st.ReduceMotion
			if st.ActiveTheme != "" && !strings.EqualFold(st.ActiveTheme, theme.BaseMikuTVDark) {
				if opt, ok := a.themes[strings.ToLower(st.ActiveTheme)]; ok {
					if err := theme.ValidateContrast(opt.Theme); err == nil {
						a.activeID = opt.ID
						a.activeTheme = opt.Theme
					}
				}
			}
		}
	}
	return a
}

func (a *AppearanceSettings) registerLocked(pkg *plugin.ThemePackage) {
	info := pkg.Manifest()
	th := pkg.Theme()
	key := strings.ToLower(info.ID)
	if _, exists := a.themes[key]; !exists {
		a.order = append(a.order, key)
	}
	a.themes[key] = ThemeOption{
		ID:          info.ID,
		Name:        info.Name,
		Description: info.Description,
		Version:     info.Version,
		Bundled:     false,
		Swatches:    th.Swatches(),
		Theme:       th,
	}
}

func (a *AppearanceSettings) Snapshot() AppearanceSnapshot {
	a.mu.Lock()
	defer a.mu.Unlock()
	def := theme.Default()
	opts := make([]ThemeOption, 0, 1+len(a.order))
	opts = append(opts, ThemeOption{
		ID:       def.ID,
		Name:     def.Name,
		Bundled:  true,
		Active:   a.activeID == "" || strings.EqualFold(a.activeID, def.ID),
		Swatches: def.Swatches(),
		Theme:    def,
	})
	for _, key := range a.order {
		opt := a.themes[key]
		opt.Active = a.activeID != "" && strings.EqualFold(a.activeID, opt.ID)
		opts = append(opts, opt)
	}
	activeID := a.activeID
	if activeID == "" {
		activeID = def.ID
	}
	return AppearanceSnapshot{
		ActiveID:     activeID,
		ActiveTheme:  a.activeTheme,
		HoverPreview: a.hoverPreview,
		ReduceMotion: a.reduceMotion,
		Options:      opts,
	}
}

func (a *AppearanceSettings) ActiveTheme() theme.Theme {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.activeTheme
}

func (a *AppearanceSettings) ActiveThemeID() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.activeID == "" {
		return theme.BaseMikuTVDark
	}
	return a.activeID
}

func (a *AppearanceSettings) HoverPreview() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.hoverPreview
}

func (a *AppearanceSettings) ReduceMotion() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.reduceMotion
}

func (a *AppearanceSettings) SetHoverPreview(enabled bool) error {
	a.mu.Lock()
	if a.hoverPreview == enabled {
		a.mu.Unlock()
		return nil
	}
	reduce := a.reduceMotion
	store := a.store
	a.mu.Unlock()

	if store != nil {
		if err := store.SaveAppearanceMotion(enabled, reduce); err != nil {
			return err
		}
	}
	a.mu.Lock()
	a.hoverPreview = enabled
	a.mu.Unlock()
	a.notify()
	return nil
}

func (a *AppearanceSettings) SetReduceMotion(enabled bool) error {
	a.mu.Lock()
	if a.reduceMotion == enabled {
		a.mu.Unlock()
		return nil
	}
	hover := a.hoverPreview
	store := a.store
	a.mu.Unlock()

	if store != nil {
		if err := store.SaveAppearanceMotion(hover, enabled); err != nil {
			return err
		}
	}
	a.mu.Lock()
	a.reduceMotion = enabled
	a.mu.Unlock()
	a.notify()
	return nil
}

// RegisterTheme registers a newly installed theme without applying it (§7.2).
func (a *AppearanceSettings) RegisterTheme(pkg *plugin.ThemePackage) {
	if pkg == nil {
		return
	}
	a.mu.Lock()
	a.registerLocked(pkg)
	a.mu.Unlock()
	a.notify()
}

// UpdateTheme replaces a registered theme and, if it is the active theme,
// atomically swaps the live theme without touching any content source (§7.2).
func (a *AppearanceSettings) UpdateTheme(pkg *plugin.ThemePackage) error {
	if pkg == nil {
		return theme.ErrInvalidTheme
	}
	resolved := pkg.Theme()
	if err := theme.ValidateContrast(resolved); err != nil {
		return err
	}
	a.mu.Lock()
	a.registerLocked(pkg)
	isActive := a.activeID != "" && strings.EqualFold(a.activeID, pkg.Manifest().ID)
	if isActive {
		a.activeTheme = resolved
	}
	onTheme := a.onTheme
	a.mu.Unlock()

	if isActive && onTheme != nil {
		onTheme(resolved)
	}
	a.notify()
	return nil
}

// SelectTheme atomically applies either built-in "mikutv-dark" (or "") or a
// registered theme plugin ID and persists the global choice.
func (a *AppearanceSettings) SelectTheme(id string) (theme.Theme, error) {
	trimmed := strings.TrimSpace(id)
	a.mu.Lock()
	store := a.store
	onTheme := a.onTheme
	onSyncCat := a.onSyncCat

	if trimmed == "" || strings.EqualFold(trimmed, theme.BaseMikuTVDark) {
		next := theme.Default()
		a.mu.Unlock()
		if store != nil {
			if err := store.SaveActiveTheme(""); err != nil {
				return theme.Theme{}, err
			}
		}
		a.mu.Lock()
		a.activeID = ""
		a.activeTheme = next
		a.mu.Unlock()
		if onTheme != nil {
			onTheme(next)
		}
		if onSyncCat != nil {
			onSyncCat("")
		}
		a.notify()
		return next, nil
	}

	opt, ok := a.themes[strings.ToLower(trimmed)]
	a.mu.Unlock()
	if !ok {
		return theme.Theme{}, fmt.Errorf("theme %q is unavailable", trimmed)
	}
	if err := theme.ValidateContrast(opt.Theme); err != nil {
		return theme.Theme{}, err
	}
	if store != nil {
		if err := store.SaveActiveTheme(opt.ID); err != nil {
			return theme.Theme{}, err
		}
	}
	a.mu.Lock()
	a.activeID = opt.ID
	a.activeTheme = opt.Theme
	a.mu.Unlock()
	if onTheme != nil {
		onTheme(opt.Theme)
	}
	if onSyncCat != nil {
		onSyncCat(opt.ID)
	}
	a.notify()
	return opt.Theme, nil
}

// DisableTheme restores the built-in baseline when the active theme is disabled.
func (a *AppearanceSettings) DisableTheme(id string) {
	a.mu.Lock()
	isActive := a.activeID != "" && strings.EqualFold(a.activeID, id)
	a.mu.Unlock()
	if isActive {
		_, _ = a.SelectTheme(theme.BaseMikuTVDark)
	}
}

// RemoveTheme deletes a theme from the picker and restores the built-in baseline
// if it was currently active — never touching content sources or playback (§7.2).
func (a *AppearanceSettings) RemoveTheme(id string) {
	key := strings.ToLower(strings.TrimSpace(id))
	a.mu.Lock()
	wasActive := a.activeID != "" && strings.EqualFold(a.activeID, id)
	delete(a.themes, key)
	nextOrder := make([]string, 0, len(a.order))
	for _, k := range a.order {
		if k != key {
			nextOrder = append(nextOrder, k)
		}
	}
	a.order = nextOrder
	a.mu.Unlock()

	if wasActive {
		_, _ = a.SelectTheme(theme.BaseMikuTVDark)
	} else {
		a.notify()
	}
}

func (a *AppearanceSettings) notify() {
	if a.invalidate != nil {
		a.invalidate()
	}
}
