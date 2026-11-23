package app

import (
	"fmt"
	"strings"
	"sync"

	"github.com/yuchanns/MikuTV/internal/plugin"
	"github.com/yuchanns/MikuTV/internal/theme"
)

type AppearanceMode string

const (
	AppearanceModeSystem AppearanceMode = "system"
	AppearanceModeLight  AppearanceMode = "light"
	AppearanceModeDark   AppearanceMode = "dark"
)

type ThemeSlot uint8

const (
	ThemeSlotLight ThemeSlot = iota
	ThemeSlotDark
)

type ThemeOption struct {
	ID          string
	Name        string
	Description string
	Version     string
	Dark        bool
	Bundled     bool
	Active      bool
	Swatches    [6]theme.Color
	Theme       theme.Theme
}

type AppearanceSnapshot struct {
	ActiveID     string
	ActiveTheme  theme.Theme
	ActiveSlot   ThemeSlot
	Mode         AppearanceMode
	SystemDark   bool
	LightID      string
	LightName    string
	DarkID       string
	DarkName     string
	HoverPreview bool
	ReduceMotion bool
	Options      []ThemeOption
}

// AppearanceSettings owns the global 3-way appearance mode (system / light / dark),
// independent daytime and nighttime theme slots, and motion preferences.
// Theme selection is independent of content entries, accounts, and playback (§7.2).
type AppearanceSettings struct {
	store      *plugin.PackageStore
	onTheme    func(theme.Theme)
	onSyncCat  func(activeThemeID string)
	invalidate func()

	mu           sync.Mutex
	mode         AppearanceMode
	systemDark   bool
	lightID      string // "" or "mikutv-light" -> built-in light; or plugin ID
	darkID       string // "" or "mikutv-dark" -> built-in dark; or plugin ID
	activeID     string // resolved currently effective theme ID
	activeTheme  theme.Theme
	hoverPreview bool
	reduceMotion bool
	order        []string
	themes       map[string]ThemeOption
}

func normalizeAppearanceMode(raw string, legacyFollow bool) AppearanceMode {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case string(AppearanceModeSystem):
		return AppearanceModeSystem
	case string(AppearanceModeLight):
		return AppearanceModeLight
	case string(AppearanceModeDark):
		return AppearanceModeDark
	default:
		if legacyFollow {
			return AppearanceModeSystem
		}
		return AppearanceModeDark
	}
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
		mode:         AppearanceModeDark,
		systemDark:   true,
		activeID:     theme.BaseMikuTVDark,
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
			a.mode = normalizeAppearanceMode(st.AppearanceMode, st.FollowSystem)
			a.lightID = a.sanitizeStoredIDLocked(st.LightTheme, theme.BaseMikuTVLight)
			a.darkID = a.sanitizeStoredIDLocked(st.DarkTheme, theme.BaseMikuTVDark)
			// Migrate legacy Build 296 single activeTheme cleanly when no explicit mode was stored.
			if st.AppearanceMode == "" && !st.FollowSystem && st.ActiveTheme != "" {
				if strings.EqualFold(st.ActiveTheme, theme.BaseMikuTVLight) {
					a.mode = AppearanceModeLight
					a.lightID = ""
				} else if opt, ok := a.themes[strings.ToLower(st.ActiveTheme)]; ok {
					if err := theme.ValidateContrast(opt.Theme); err == nil {
						if opt.Theme.Dark {
							a.mode = AppearanceModeDark
							if a.darkID == "" {
								a.darkID = opt.ID
							}
						} else {
							a.mode = AppearanceModeLight
							if a.lightID == "" {
								a.lightID = opt.ID
							}
						}
					}
				}
			}
		}
	}
	a.activeID, a.activeTheme = a.effectiveLocked()
	return a
}

func (a *AppearanceSettings) sanitizeStoredIDLocked(id, defaultID string) string {
	trimmed := strings.TrimSpace(id)
	if trimmed == "" || strings.EqualFold(trimmed, defaultID) {
		return ""
	}
	if strings.EqualFold(trimmed, theme.BaseMikuTVDark) {
		return theme.BaseMikuTVDark
	}
	if strings.EqualFold(trimmed, theme.BaseMikuTVLight) {
		return theme.BaseMikuTVLight
	}
	if opt, ok := a.themes[strings.ToLower(trimmed)]; ok {
		if err := theme.ValidateContrast(opt.Theme); err == nil {
			return opt.ID
		}
	}
	return ""
}

func (a *AppearanceSettings) resolveLocked(id string, fallback theme.Theme) (string, theme.Theme) {
	trimmed := strings.TrimSpace(id)
	if trimmed == "" {
		return fallback.ID, fallback
	}
	if strings.EqualFold(trimmed, theme.BaseMikuTVDark) {
		def := theme.Default()
		return def.ID, def
	}
	if strings.EqualFold(trimmed, theme.BaseMikuTVLight) {
		def := theme.DefaultLight()
		return def.ID, def
	}
	if opt, ok := a.themes[strings.ToLower(trimmed)]; ok {
		if err := theme.ValidateContrast(opt.Theme); err == nil {
			return opt.ID, opt.Theme
		}
	}
	return fallback.ID, fallback
}

func (a *AppearanceSettings) activeSlotLocked() ThemeSlot {
	switch a.mode {
	case AppearanceModeLight:
		return ThemeSlotLight
	case AppearanceModeDark:
		return ThemeSlotDark
	default:
		if a.systemDark {
			return ThemeSlotDark
		}
		return ThemeSlotLight
	}
}

func (a *AppearanceSettings) effectiveLocked() (string, theme.Theme) {
	if a.activeSlotLocked() == ThemeSlotLight {
		return a.resolveLocked(a.lightID, theme.DefaultLight())
	}
	return a.resolveLocked(a.darkID, theme.Default())
}

func externalThemeID(resolvedID string) string {
	if theme.IsBuiltin(resolvedID) {
		return ""
	}
	return resolvedID
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
		Dark:        th.Dark,
		Bundled:     false,
		Swatches:    th.Swatches(),
		Theme:       th,
	}
}

func (a *AppearanceSettings) Snapshot() AppearanceSnapshot {
	a.mu.Lock()
	defer a.mu.Unlock()
	darkDef := theme.Default()
	lightDef := theme.DefaultLight()
	opts := make([]ThemeOption, 0, 2+len(a.order))
	opts = append(opts, ThemeOption{
		ID:       darkDef.ID,
		Name:     darkDef.Name,
		Dark:     true,
		Bundled:  true,
		Active:   strings.EqualFold(a.activeID, darkDef.ID),
		Swatches: darkDef.Swatches(),
		Theme:    darkDef,
	}, ThemeOption{
		ID:       lightDef.ID,
		Name:     lightDef.Name,
		Dark:     false,
		Bundled:  true,
		Active:   strings.EqualFold(a.activeID, lightDef.ID),
		Swatches: lightDef.Swatches(),
		Theme:    lightDef,
	})
	for _, key := range a.order {
		opt := a.themes[key]
		opt.Active = strings.EqualFold(a.activeID, opt.ID)
		opts = append(opts, opt)
	}
	lightID, lightTheme := a.resolveLocked(a.lightID, lightDef)
	darkID, darkTheme := a.resolveLocked(a.darkID, darkDef)
	return AppearanceSnapshot{
		ActiveID:     a.activeID,
		ActiveTheme:  a.activeTheme,
		ActiveSlot:   a.activeSlotLocked(),
		Mode:         a.mode,
		SystemDark:   a.systemDark,
		LightID:      lightID,
		LightName:    lightTheme.Name,
		DarkID:       darkID,
		DarkName:     darkTheme.Name,
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

func (a *AppearanceSettings) Mode() AppearanceMode {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.mode
}

func (a *AppearanceSettings) ActiveSlot() ThemeSlot {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.activeSlotLocked()
}

func (a *AppearanceSettings) SystemDark() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.systemDark
}

func (a *AppearanceSettings) LightThemeID() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	id, _ := a.resolveLocked(a.lightID, theme.DefaultLight())
	return id
}

func (a *AppearanceSettings) DarkThemeID() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	id, _ := a.resolveLocked(a.darkID, theme.Default())
	return id
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

// SetSystemDark updates the observed OS light/dark appearance and, when Mode is
// AppearanceModeSystem, atomically swaps the live theme between daytime and nighttime slots.
func (a *AppearanceSettings) SetSystemDark(dark bool) {
	a.mu.Lock()
	if a.systemDark == dark {
		a.mu.Unlock()
		return
	}
	a.systemDark = dark
	prevID, prevTheme := a.activeID, a.activeTheme
	nextID, nextTheme := a.effectiveLocked()
	changed := !strings.EqualFold(prevID, nextID) || prevTheme != nextTheme
	a.activeID, a.activeTheme = nextID, nextTheme
	onTheme := a.onTheme
	onSyncCat := a.onSyncCat
	a.mu.Unlock()

	if changed {
		if onTheme != nil {
			onTheme(nextTheme)
		}
		if onSyncCat != nil {
			onSyncCat(externalThemeID(nextID))
		}
	}
	a.notify()
}

// SetMode switches the appearance mode among system, light, and dark, persists
// the choice, and atomically applies the resolved slot's theme.
func (a *AppearanceSettings) SetMode(mode AppearanceMode) error {
	mode = normalizeAppearanceMode(string(mode), false)
	a.mu.Lock()
	if a.mode == mode {
		a.mu.Unlock()
		return nil
	}
	lightID, darkID := a.lightID, a.darkID
	store := a.store
	a.mu.Unlock()

	if store != nil {
		if err := store.SaveAppearanceConfig(string(mode), lightID, darkID); err != nil {
			return err
		}
	}

	a.mu.Lock()
	a.mode = mode
	prevID, prevTheme := a.activeID, a.activeTheme
	nextID, nextTheme := a.effectiveLocked()
	changed := !strings.EqualFold(prevID, nextID) || prevTheme != nextTheme
	a.activeID, a.activeTheme = nextID, nextTheme
	onTheme := a.onTheme
	onSyncCat := a.onSyncCat
	a.mu.Unlock()

	if changed {
		if onTheme != nil {
			onTheme(nextTheme)
		}
		if onSyncCat != nil {
			onSyncCat(externalThemeID(nextID))
		}
	}
	a.notify()
	return nil
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

// UpdateTheme replaces a registered theme and, if it is the currently effective
// theme, atomically swaps the live theme without touching any content source (§7.2).
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
	isActive := strings.EqualFold(a.activeID, pkg.Manifest().ID)
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

// SelectTheme assigns id to the currently active slot (Light or Dark),
// applies it atomically, and persists the configuration.
func (a *AppearanceSettings) SelectTheme(id string) (theme.Theme, error) {
	return a.SelectThemeForSlot(a.ActiveSlot(), id)
}

// SelectThemeForSlot validates id, assigns it to slot (Light or Dark),
// persists the appearance configuration, and atomically applies the new theme
// if slot determines the currently active appearance.
func (a *AppearanceSettings) SelectThemeForSlot(slot ThemeSlot, id string) (theme.Theme, error) {
	trimmed := strings.TrimSpace(id)

	a.mu.Lock()
	var chosenID string
	var chosenTheme theme.Theme
	switch {
	case trimmed == "" && slot == ThemeSlotLight:
		chosenTheme = theme.DefaultLight()
		chosenID = chosenTheme.ID
	case trimmed == "" || strings.EqualFold(trimmed, theme.BaseMikuTVDark):
		chosenTheme = theme.Default()
		chosenID = chosenTheme.ID
	case strings.EqualFold(trimmed, theme.BaseMikuTVLight):
		chosenTheme = theme.DefaultLight()
		chosenID = chosenTheme.ID
	default:
		opt, ok := a.themes[strings.ToLower(trimmed)]
		if !ok {
			a.mu.Unlock()
			return theme.Theme{}, fmt.Errorf("theme %q is unavailable", trimmed)
		}
		chosenID = opt.ID
		chosenTheme = opt.Theme
	}
	mode, lightID, darkID := a.mode, a.lightID, a.darkID
	store := a.store
	a.mu.Unlock()

	if err := theme.ValidateContrast(chosenTheme); err != nil {
		return theme.Theme{}, err
	}

	storedVal := func(val, def string) string {
		if strings.EqualFold(val, def) {
			return ""
		}
		return val
	}
	if slot == ThemeSlotLight {
		lightID = storedVal(chosenID, theme.BaseMikuTVLight)
	} else {
		darkID = storedVal(chosenID, theme.BaseMikuTVDark)
	}

	if store != nil {
		if err := store.SaveAppearanceConfig(string(mode), lightID, darkID); err != nil {
			return theme.Theme{}, err
		}
	}

	a.mu.Lock()
	a.lightID = lightID
	a.darkID = darkID
	prevID, prevTheme := a.activeID, a.activeTheme
	nextID, nextTheme := a.effectiveLocked()
	changed := !strings.EqualFold(prevID, nextID) || prevTheme != nextTheme
	a.activeID, a.activeTheme = nextID, nextTheme
	onTheme := a.onTheme
	onSyncCat := a.onSyncCat
	a.mu.Unlock()

	if changed {
		if onTheme != nil {
			onTheme(nextTheme)
		}
		if onSyncCat != nil {
			onSyncCat(externalThemeID(nextID))
		}
	}
	a.notify()
	return chosenTheme, nil
}

// DisableTheme clears any slot referencing id and restores the matching
// built-in baseline (MikuTV Light for daytime, MikuTV Dark for nighttime).
func (a *AppearanceSettings) DisableTheme(id string) {
	a.clearThemeReferences(id, false)
}

// RemoveTheme deletes a theme from the picker, clears any slot referencing it,
// and restores the matching built-in baseline if it was active — never touching
// content sources or playback (§7.2).
func (a *AppearanceSettings) RemoveTheme(id string) {
	a.clearThemeReferences(id, true)
}

func (a *AppearanceSettings) clearThemeReferences(id string, removeOption bool) {
	key := strings.ToLower(strings.TrimSpace(id))
	if key == "" {
		return
	}
	a.mu.Lock()
	if removeOption {
		delete(a.themes, key)
		nextOrder := make([]string, 0, len(a.order))
		for _, k := range a.order {
			if k != key {
				nextOrder = append(nextOrder, k)
			}
		}
		a.order = nextOrder
	}
	slotsChanged := false
	if a.lightID != "" && strings.EqualFold(a.lightID, id) {
		a.lightID = ""
		slotsChanged = true
	}
	if a.darkID != "" && strings.EqualFold(a.darkID, id) {
		a.darkID = ""
		slotsChanged = true
	}
	mode, lightID, darkID := a.mode, a.lightID, a.darkID
	prevID, prevTheme := a.activeID, a.activeTheme
	nextID, nextTheme := a.effectiveLocked()
	activeChanged := !strings.EqualFold(prevID, nextID) || prevTheme != nextTheme
	a.activeID, a.activeTheme = nextID, nextTheme
	store := a.store
	onTheme := a.onTheme
	onSyncCat := a.onSyncCat
	a.mu.Unlock()

	if slotsChanged && store != nil {
		_ = store.SaveAppearanceConfig(string(mode), lightID, darkID)
	}
	if activeChanged {
		if onTheme != nil {
			onTheme(nextTheme)
		}
		if onSyncCat != nil {
			onSyncCat(externalThemeID(nextID))
		}
	}
	a.notify()
}

func (a *AppearanceSettings) notify() {
	if a.invalidate != nil {
		a.invalidate()
	}
}
