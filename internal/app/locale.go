package app

import (
	"strings"
	"sync"

	"github.com/yuchanns/MikuTV/internal/plugin"
)

const (
	LocaleModeSystem  = "system"
	LocaleBuiltinZH   = "zh-Hans"
	LocaleBuiltinEN   = "en"
)

type LocaleOption struct {
	ID          string // "zh-Hans", "en", or external locale plugin ID
	Name        string // Native display name, e.g. "简体中文", "English", "日本語"
	Description string
	Language    string // Normalized BCP-47 tag, e.g. "zh-Hans", "en", "ja"
	Version     string
	Bundled     bool
	Active      bool
	RawJSON     []byte
}

type LocaleSnapshot struct {
	Selection   string // "system", "zh-Hans", "en", or plugin ID
	ActiveID    string // Effective resolved option ID ("zh-Hans", "en", or plugin ID)
	ActiveTag   string // Effective BCP-47 tag ("zh-Hans", "en", "ja", ...)
	ActiveName  string // Native display name of effective option
	SystemTags  []string
	Options     []LocaleOption
}

// LocaleSettings owns the app language selection ("system" / "zh-Hans" / "en" /
// installed kind:"locale" plugin ID), automatic matching against tvOS
// NSLocale.preferredLanguages, and live hot-swapping across UI & source plugins.
type LocaleSettings struct {
	store      *plugin.PackageStore
	onLocale   func(tag string, overrideJSON []byte)
	onSyncCat  func(activeLocaleID string)
	invalidate func()

	mu         sync.Mutex
	selection  string
	systemTags []string
	activeID   string
	activeTag  string
	order      []string
	locales    map[string]LocaleOption
}

func NewLocaleSettings(
	store *plugin.PackageStore,
	initialLocales []*plugin.LocalePackage,
	systemTags []string,
	onLocale func(tag string, overrideJSON []byte),
	onSyncCat func(activeLocaleID string),
	invalidate func(),
) *LocaleSettings {
	l := &LocaleSettings{
		store:      store,
		onLocale:   onLocale,
		onSyncCat:  onSyncCat,
		invalidate: invalidate,
		selection:  LocaleModeSystem,
		systemTags: append([]string(nil), systemTags...),
		locales:    make(map[string]LocaleOption, len(initialLocales)),
	}
	for _, pkg := range initialLocales {
		if pkg != nil {
			l.registerLocked(pkg)
		}
	}
	if store != nil {
		if stored, err := store.LoadLanguagePreference(); err == nil && stored != "" {
			l.selection = l.sanitizeSelectionLocked(stored)
		}
	}
	l.activeID, l.activeTag = l.resolveLocked()
	return l
}

func (l *LocaleSettings) sanitizeSelectionLocked(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || strings.EqualFold(trimmed, LocaleModeSystem) {
		return LocaleModeSystem
	}
	if strings.EqualFold(trimmed, LocaleBuiltinZH) {
		return LocaleBuiltinZH
	}
	if strings.EqualFold(trimmed, LocaleBuiltinEN) {
		return LocaleBuiltinEN
	}
	if opt, ok := l.locales[strings.ToLower(trimmed)]; ok {
		return opt.ID
	}
	return LocaleModeSystem
}

func (l *LocaleSettings) registerLocked(pkg *plugin.LocalePackage) {
	m := pkg.Manifest()
	key := strings.ToLower(m.ID)
	if _, exists := l.locales[key]; !exists {
		l.order = append(l.order, key)
	}
	l.locales[key] = LocaleOption{
		ID:          m.ID,
		Name:        pkg.NativeName(),
		Description: m.Description,
		Language:    pkg.Language(),
		Version:     m.Version,
		Bundled:     false,
		RawJSON:     pkg.RawJSON(),
	}
}

func (l *LocaleSettings) resolveLocked() (id string, tag string) {
	switch l.selection {
	case LocaleBuiltinZH:
		return LocaleBuiltinZH, LocaleBuiltinZH
	case LocaleBuiltinEN:
		return LocaleBuiltinEN, LocaleBuiltinEN
	case LocaleModeSystem, "":
		// Build candidate list: installed community locales first, then built-in zh-Hans & en.
		// Matching checks systemTags in priority order so an installed "ja" pack auto-activates
		// on a Japanese tvOS system.
		tagToID := make(map[string]string, len(l.order)+2)
		avail := make([]string, 0, len(l.order)+2)
		for _, key := range l.order {
			opt := l.locales[key]
			if opt.Language != "" {
				if _, exists := tagToID[strings.ToLower(opt.Language)]; !exists {
					tagToID[strings.ToLower(opt.Language)] = opt.ID
					avail = append(avail, opt.Language)
				}
			}
		}
		avail = append(avail, LocaleBuiltinZH, LocaleBuiltinEN)
		tagToID[strings.ToLower(LocaleBuiltinZH)] = LocaleBuiltinZH
		tagToID[strings.ToLower(LocaleBuiltinEN)] = LocaleBuiltinEN

		if matched := plugin.MatchLocaleTag(l.systemTags, avail); matched != "" {
			if targetID, ok := tagToID[strings.ToLower(matched)]; ok {
				if targetID == LocaleBuiltinZH || targetID == LocaleBuiltinEN {
					return targetID, targetID
				}
				if opt, exists := l.locales[strings.ToLower(targetID)]; exists {
					return opt.ID, opt.Language
				}
			}
		}
		// Default fallback when system language is unavailable or unmatched.
		return LocaleBuiltinZH, LocaleBuiltinZH
	default:
		if opt, ok := l.locales[strings.ToLower(l.selection)]; ok {
			return opt.ID, opt.Language
		}
		return LocaleBuiltinZH, LocaleBuiltinZH
	}
}

func (l *LocaleSettings) activePayloadLocked() (string, []byte) {
	if l.activeID == LocaleBuiltinZH || l.activeID == LocaleBuiltinEN {
		return l.activeTag, nil
	}
	if opt, ok := l.locales[strings.ToLower(l.activeID)]; ok {
		return opt.Language, append([]byte(nil), opt.RawJSON...)
	}
	return LocaleBuiltinZH, nil
}

func (l *LocaleSettings) ActiveTag() string {
	if l == nil {
		return LocaleBuiltinZH
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.activeTag == "" {
		return LocaleBuiltinZH
	}
	return l.activeTag
}

func (l *LocaleSettings) ActiveID() string {
	if l == nil {
		return LocaleBuiltinZH
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.activeID
}

func (l *LocaleSettings) ActivePayload() (string, []byte) {
	if l == nil {
		return LocaleBuiltinZH, nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.activePayloadLocked()
}

func (l *LocaleSettings) Snapshot() LocaleSnapshot {
	if l == nil {
		return LocaleSnapshot{
			Selection:  LocaleModeSystem,
			ActiveID:   LocaleBuiltinZH,
			ActiveTag:  LocaleBuiltinZH,
			ActiveName: "简体中文",
		}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	options := make([]LocaleOption, 0, len(l.order)+2)
	options = append(options,
		LocaleOption{
			ID:       LocaleBuiltinZH,
			Name:     "简体中文",
			Language: LocaleBuiltinZH,
			Bundled:  true,
			Active:   l.activeID == LocaleBuiltinZH,
		},
		LocaleOption{
			ID:       LocaleBuiltinEN,
			Name:     "English",
			Language: LocaleBuiltinEN,
			Bundled:  true,
			Active:   l.activeID == LocaleBuiltinEN,
		},
	)
	activeName := "简体中文"
	if l.activeID == LocaleBuiltinEN {
		activeName = "English"
	}
	for _, key := range l.order {
		opt := l.locales[key]
		opt.RawJSON = nil
		opt.Active = strings.EqualFold(l.activeID, opt.ID)
		if opt.Active {
			activeName = opt.Name
		}
		options = append(options, opt)
	}
	return LocaleSnapshot{
		Selection:  l.selection,
		ActiveID:   l.activeID,
		ActiveTag:  l.activeTag,
		ActiveName: activeName,
		SystemTags: append([]string(nil), l.systemTags...),
		Options:    options,
	}
}

// Select sets the language mode ("system", "zh-Hans", "en", or installed locale plugin ID),
// persists it, and hot-applies the resolved language immediately.
func (l *LocaleSettings) Select(id string) error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	target := l.sanitizeSelectionLocked(id)
	prevID := l.activeID
	prevTag := l.activeTag
	l.selection = target
	nextID, nextTag := l.resolveLocked()
	l.activeID = nextID
	l.activeTag = nextTag
	tag, payload := l.activePayloadLocked()
	store := l.store
	onLocale := l.onLocale
	onSyncCat := l.onSyncCat
	l.mu.Unlock()

	if store != nil {
		if err := store.SaveLanguagePreference(target); err != nil {
			return err
		}
	}
	if nextID != prevID || nextTag != prevTag {
		if onLocale != nil {
			onLocale(tag, payload)
		}
		if onSyncCat != nil {
			onSyncCat(nextID)
		}
	}
	l.notify()
	return nil
}

// RegisterLocale registers a newly installed locale plugin. If the app is in
// Follow System mode and the new plugin matches the system language, it
// activates automatically on the spot.
func (l *LocaleSettings) RegisterLocale(pkg *plugin.LocalePackage) {
	if l == nil || pkg == nil {
		return
	}
	l.mu.Lock()
	l.registerLocked(pkg)
	prevID, prevTag := l.activeID, l.activeTag
	nextID, nextTag := l.resolveLocked()
	l.activeID, l.activeTag = nextID, nextTag
	tag, payload := l.activePayloadLocked()
	onLocale := l.onLocale
	onSyncCat := l.onSyncCat
	l.mu.Unlock()

	if nextID != prevID || nextTag != prevTag {
		if onLocale != nil {
			onLocale(tag, payload)
		}
		if onSyncCat != nil {
			onSyncCat(nextID)
		}
	}
	l.notify()
}

// UpdateLocale replaces an installed locale plugin and live-reloads UI copy
// if it is currently active.
func (l *LocaleSettings) UpdateLocale(pkg *plugin.LocalePackage) error {
	if l == nil || pkg == nil {
		return plugin.ErrIncompatible
	}
	l.mu.Lock()
	l.registerLocked(pkg)
	isActive := strings.EqualFold(l.activeID, pkg.Manifest().ID)
	tag, payload := l.activePayloadLocked()
	onLocale := l.onLocale
	l.mu.Unlock()

	if isActive && onLocale != nil {
		onLocale(tag, payload)
	}
	l.notify()
	return nil
}

// DisableLocale falls back cleanly when the active locale plugin is disabled.
func (l *LocaleSettings) DisableLocale(pluginID string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	changed := false
	persisted := LocaleModeSystem
	if strings.EqualFold(l.selection, pluginID) || strings.EqualFold(l.activeID, pluginID) {
		l.selection = LocaleModeSystem
		nextID, nextTag := l.resolveLocked()
		if strings.EqualFold(nextID, pluginID) {
			nextID, nextTag = LocaleBuiltinEN, LocaleBuiltinEN
			l.selection = LocaleBuiltinEN
		}
		persisted = l.selection
		l.activeID, l.activeTag = nextID, nextTag
		changed = true
	}
	tag, payload := l.activePayloadLocked()
	store := l.store
	onLocale := l.onLocale
	onSyncCat := l.onSyncCat
	activeID := l.activeID
	l.mu.Unlock()

	if changed {
		if store != nil {
			_ = store.SaveLanguagePreference(persisted)
		}
		if onLocale != nil {
			onLocale(tag, payload)
		}
		if onSyncCat != nil {
			onSyncCat(activeID)
		}
	}
	l.notify()
}

// RemoveLocale removes an uninstalled locale plugin and falls back cleanly if it was active.
func (l *LocaleSettings) RemoveLocale(pluginID string) {
	if l == nil {
		return
	}
	key := strings.ToLower(strings.TrimSpace(pluginID))
	l.mu.Lock()
	delete(l.locales, key)
	nextOrder := make([]string, 0, len(l.order))
	for _, k := range l.order {
		if k != key {
			nextOrder = append(nextOrder, k)
		}
	}
	l.order = nextOrder
	prevID, prevTag := l.activeID, l.activeTag
	if strings.EqualFold(l.selection, pluginID) {
		l.selection = LocaleModeSystem
	}
	nextID, nextTag := l.resolveLocked()
	l.activeID, l.activeTag = nextID, nextTag
	tag, payload := l.activePayloadLocked()
	store := l.store
	selection := l.selection
	onLocale := l.onLocale
	onSyncCat := l.onSyncCat
	l.mu.Unlock()

	if store != nil && selection == LocaleModeSystem && strings.EqualFold(prevID, pluginID) {
		_ = store.SaveLanguagePreference(LocaleModeSystem)
	}
	if nextID != prevID || nextTag != prevTag {
		if onLocale != nil {
			onLocale(tag, payload)
		}
		if onSyncCat != nil {
			onSyncCat(nextID)
		}
	}
	l.notify()
}

// ReapplyStoredLocale re-binds persisted language choice after startup cache recovery.
func (l *LocaleSettings) ReapplyStoredLocale() {
	if l == nil || l.store == nil {
		return
	}
	stored, err := l.store.LoadLanguagePreference()
	if err != nil {
		return
	}
	l.mu.Lock()
	prevID, prevTag := l.activeID, l.activeTag
	l.selection = l.sanitizeSelectionLocked(stored)
	nextID, nextTag := l.resolveLocked()
	l.activeID, l.activeTag = nextID, nextTag
	tag, payload := l.activePayloadLocked()
	onLocale := l.onLocale
	onSyncCat := l.onSyncCat
	l.mu.Unlock()

	if nextID != prevID || nextTag != prevTag {
		if onLocale != nil {
			onLocale(tag, payload)
		}
		if onSyncCat != nil {
			onSyncCat(nextID)
		}
	}
	l.notify()
}

func (l *LocaleSettings) notify() {
	if l.invalidate != nil {
		l.invalidate()
	}
}
