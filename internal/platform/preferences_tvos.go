//go:build ios && tvos && arm64

package platform

import (
	"crypto/sha256"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"github.com/ebitengine/purego/objc"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/provider"
)

const (
	// tvOS terminates the app at the documented 1 MB defaults database limit.
	userDefaultsProcessLimit = 1_000_000
	pluginStorageCapacity    = 8 << 10
)

// Preferences uses the app's defaults domain; tvOS does not allow desktop data paths.
type Preferences struct {
	defaults          objc.ID
	key               objc.ID
	positionsKey      objc.ID
	searchHistoryKey  objc.ID
	danmakuDisplayKey objc.ID
	danmakuRulesKey   objc.ID
	domain            objc.ID
	writes            sync.Mutex
}

func NewPreferences() (*Preferences, error) {
	defaults := objc.ID(objc.GetClass("NSUserDefaults")).Send(objc.RegisterName("standardUserDefaults"))
	if defaults == 0 {
		return nil, fmt.Errorf("app preferences are unavailable")
	}
	strCls := objc.ID(objc.GetClass("NSString"))
	strSel := objc.RegisterName("stringWithUTF8String:")
	key := strCls.Send(strSel, "recentEntry")
	positionsKey := strCls.Send(strSel, playbackPositionsDefaultsKey)
	searchHistoryKey := strCls.Send(strSel, searchHistoryDefaultsKey)
	danmakuDisplayKey := strCls.Send(strSel, danmakuDisplayDefaultsKey)
	danmakuRulesKey := strCls.Send(strSel, danmakuRulesDefaultsKey)
	bundle := objc.ID(objc.GetClass("NSBundle")).Send(objc.RegisterName("mainBundle"))
	domain := bundle.Send(objc.RegisterName("bundleIdentifier"))
	if domain == 0 {
		return nil, fmt.Errorf("app defaults domain is unavailable")
	}
	retain := objc.RegisterName("retain")
	key.Send(retain)
	positionsKey.Send(retain)
	searchHistoryKey.Send(retain)
	danmakuDisplayKey.Send(retain)
	danmakuRulesKey.Send(retain)
	domain.Send(retain)
	return &Preferences{
		defaults: defaults, key: key, positionsKey: positionsKey, searchHistoryKey: searchHistoryKey,
		danmakuDisplayKey: danmakuDisplayKey, danmakuRulesKey: danmakuRulesKey, domain: domain,
	}, nil
}

func withAutoreleasePool(fn func()) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	pool := objc.ID(objc.GetClass("NSAutoreleasePool")).Send(objc.RegisterName("new"))
	defer pool.Send(objc.RegisterName("drain"))
	fn()
}

func nsStringObj(value string) objc.ID {
	return objc.ID(objc.GetClass("NSString")).Send(objc.RegisterName("stringWithUTF8String:"), value)
}

func (p *Preferences) Load() app.Preferences {
	value := p.defaults.Send(objc.RegisterName("stringForKey:"), p.key)
	if value == 0 {
		return app.Preferences{}
	}
	return app.Preferences{RecentEntry: objc.Send[string](value, objc.RegisterName("UTF8String"))}
}

func (p *Preferences) Save(preferences app.Preferences) error {
	value := nsStringObj(preferences.RecentEntry)
	return p.saveString(p.key, value, "")
}

func historySyncKey(entry, owner string) objc.ID {
	digest := sha256.Sum256([]byte(entry + "\x00" + owner))
	return nsStringObj(fmt.Sprintf("historySync.%x", digest))
}

func (p *Preferences) HistorySync(entry, owner string) bool {
	value := p.defaults.Send(objc.RegisterName("stringForKey:"), historySyncKey(entry, owner))
	return value == 0 || objc.Send[string](value, objc.RegisterName("UTF8String")) != "disabled"
}

func (p *Preferences) SaveHistorySync(entry, owner string, enabled bool) error {
	state := "enabled"
	if !enabled {
		state = "disabled"
	}
	return p.saveString(historySyncKey(entry, owner), nsStringObj(state), "")
}

func (p *Preferences) loadPlaybackPositionsLocked() playbackPositionStore {
	value := p.defaults.Send(objc.RegisterName("stringForKey:"), p.positionsKey)
	if value == 0 {
		return playbackPositionStore{}
	}
	return parsePlaybackPositionStore(objc.Send[string](value, objc.RegisterName("UTF8String")))
}

func (p *Preferences) PlaybackPosition(owner string, target provider.PlaybackTarget) (float64, bool) {
	var seconds float64
	var ok bool
	withAutoreleasePool(func() {
		p.writes.Lock()
		defer p.writes.Unlock()
		store := p.loadPlaybackPositionsLocked()
		if seconds, ok = store.get(owner, target); ok {
			return
		}
		legacyKey := nsStringObj(legacyPlaybackPositionKeyName(owner, target))
		value := p.defaults.Send(objc.RegisterName("stringForKey:"), legacyKey)
		if value == 0 {
			return
		}
		parsed, err := strconv.ParseFloat(objc.Send[string](value, objc.RegisterName("UTF8String")), 64)
		if err == nil && parsed >= 0 {
			seconds, ok = parsed, true
		}
	})
	return seconds, ok
}

func (p *Preferences) SavePlaybackPosition(owner string, target provider.PlaybackTarget, seconds float64) error {
	var saveErr error
	withAutoreleasePool(func() {
		p.writes.Lock()
		defer p.writes.Unlock()
		store := p.loadPlaybackPositionsLocked()
		store.put(owner, target, seconds)
		encoded, err := store.encode()
		if err != nil {
			saveErr = err
			return
		}
		legacyKey := nsStringObj(legacyPlaybackPositionKeyName(owner, target))
		saveErr = p.saveStringLocked(p.positionsKey, nsStringObj(encoded), "", legacyKey)
	})
	return saveErr
}

func (p *Preferences) loadSearchHistoryLocked() searchHistoryStore {
	value := p.defaults.Send(objc.RegisterName("stringForKey:"), p.searchHistoryKey)
	if value == 0 {
		return searchHistoryStore{}
	}
	return parseSearchHistoryStore(objc.Send[string](value, objc.RegisterName("UTF8String")))
}

func (p *Preferences) SearchHistory(entryID, owner string) []string {
	var queries []string
	withAutoreleasePool(func() {
		p.writes.Lock()
		defer p.writes.Unlock()
		store := p.loadSearchHistoryLocked()
		queries = store.get(entryID, owner)
	})
	return queries
}

func (p *Preferences) SaveSearchHistory(entryID, owner string, queries []string) error {
	var saveErr error
	withAutoreleasePool(func() {
		p.writes.Lock()
		defer p.writes.Unlock()
		store := p.loadSearchHistoryLocked()
		store.put(entryID, owner, queries)
		encoded, err := store.encode()
		if err != nil {
			saveErr = err
			return
		}
		saveErr = p.saveStringLocked(p.searchHistoryKey, nsStringObj(encoded), "", 0)
	})
	return saveErr
}

func (p *Preferences) DanmakuDisplay() (app.DanmakuDisplay, bool) {
	var display app.DanmakuDisplay
	var ok bool
	withAutoreleasePool(func() {
		p.writes.Lock()
		defer p.writes.Unlock()
		value := p.defaults.Send(objc.RegisterName("stringForKey:"), p.danmakuDisplayKey)
		if value == 0 {
			display = app.DefaultDanmakuDisplay()
			return
		}
		display, ok = parseDanmakuDisplay(objc.Send[string](value, objc.RegisterName("UTF8String")))
	})
	return display, ok
}

func (p *Preferences) SaveDanmakuDisplay(display app.DanmakuDisplay) error {
	var saveErr error
	withAutoreleasePool(func() {
		p.writes.Lock()
		defer p.writes.Unlock()
		encoded, err := encodeDanmakuDisplay(display)
		if err != nil {
			saveErr = err
			return
		}
		saveErr = p.saveStringLocked(p.danmakuDisplayKey, nsStringObj(encoded), "", 0)
	})
	return saveErr
}

func (p *Preferences) loadDanmakuRulesLocked() danmakuRulesStore {
	value := p.defaults.Send(objc.RegisterName("stringForKey:"), p.danmakuRulesKey)
	if value == 0 {
		return danmakuRulesStore{}
	}
	return parseDanmakuRulesStore(objc.Send[string](value, objc.RegisterName("UTF8String")))
}

func (p *Preferences) DanmakuRules(entryID, owner string) []provider.DanmakuRule {
	var rules []provider.DanmakuRule
	withAutoreleasePool(func() {
		p.writes.Lock()
		defer p.writes.Unlock()
		store := p.loadDanmakuRulesLocked()
		rules = store.get(entryID, owner)
	})
	return rules
}

func (p *Preferences) SaveDanmakuRules(entryID, owner string, rules []provider.DanmakuRule) error {
	var saveErr error
	withAutoreleasePool(func() {
		p.writes.Lock()
		defer p.writes.Unlock()
		store := p.loadDanmakuRulesLocked()
		store.put(entryID, owner, rules)
		encoded, err := store.encode()
		if err != nil {
			saveErr = err
			return
		}
		saveErr = p.saveStringLocked(p.danmakuRulesKey, nsStringObj(encoded), "", 0)
	})
	return saveErr
}

func (p *Preferences) ClearAccount(entryID, owner string) error {
	var clearErr error
	withAutoreleasePool(func() {
		p.writes.Lock()
		defer p.writes.Unlock()
		p.defaults.Send(objc.RegisterName("removeObjectForKey:"), historySyncKey(entryID, owner))
		store := p.loadPlaybackPositionsLocked()
		if store.clearAccount(entryID, owner) {
			encoded, err := store.encode()
			if err != nil {
				clearErr = err
				return
			}
			if err := p.saveStringLocked(p.positionsKey, nsStringObj(encoded), "", 0); err != nil {
				clearErr = err
				return
			}
		}
		history := p.loadSearchHistoryLocked()
		if history.clearAccount(entryID, owner) {
			encoded, err := history.encode()
			if err != nil {
				clearErr = err
				return
			}
			if err := p.saveStringLocked(p.searchHistoryKey, nsStringObj(encoded), "", 0); err != nil {
				clearErr = err
				return
			}
		}
		rules := p.loadDanmakuRulesLocked()
		if rules.clearAccount(entryID, owner) {
			encoded, err := rules.encode()
			if err != nil {
				clearErr = err
				return
			}
			clearErr = p.saveStringLocked(p.danmakuRulesKey, nsStringObj(encoded), "", 0)
		}
	})
	return clearErr
}

func (p *Preferences) ClearEntry(entryID string) error {
	var clearErr error
	withAutoreleasePool(func() {
		p.writes.Lock()
		defer p.writes.Unlock()
		store := p.loadPlaybackPositionsLocked()
		if store.clearEntry(entryID) {
			encoded, err := store.encode()
			if err != nil {
				clearErr = err
				return
			}
			if err := p.saveStringLocked(p.positionsKey, nsStringObj(encoded), "", 0); err != nil {
				clearErr = err
				return
			}
		}
		history := p.loadSearchHistoryLocked()
		if history.clearEntry(entryID) {
			encoded, err := history.encode()
			if err != nil {
				clearErr = err
				return
			}
			if err := p.saveStringLocked(p.searchHistoryKey, nsStringObj(encoded), "", 0); err != nil {
				clearErr = err
				return
			}
		}
		rules := p.loadDanmakuRulesLocked()
		if rules.clearEntry(entryID) {
			encoded, err := rules.encode()
			if err != nil {
				clearErr = err
				return
			}
			clearErr = p.saveStringLocked(p.danmakuRulesKey, nsStringObj(encoded), "", 0)
		}
	})
	return clearErr
}

// LoadPluginStorage uses the UIKit thread and its autorelease pool at startup.
func (p *Preferences) LoadPluginStorage(pluginID, entryID string) []byte {
	key := nsStringObj(pluginStorageKey(pluginID, entryID))
	value := p.defaults.Send(objc.RegisterName("stringForKey:"), key)
	if value == 0 {
		return nil
	}
	return []byte(objc.Send[string](value, objc.RegisterName("UTF8String")))
}

func (p *Preferences) SavePluginStorage(pluginID, entryID string, data []byte) error {
	var saveErr error
	withAutoreleasePool(func() {
		key := nsStringObj(pluginStorageKey(pluginID, entryID))
		value := nsStringObj(string(data))
		saveErr = p.saveString(key, value, pluginID)
	})
	return saveErr
}

func pluginStorageKey(pluginID, entryID string) string {
	scope := "pluginStorage." + pluginID
	if pluginID == entryID {
		return scope
	}
	return scope + "/" + entryID
}

func (p *Preferences) saveString(key, value objc.ID, pluginID string) error {
	p.writes.Lock()
	defer p.writes.Unlock()
	return p.saveStringLocked(key, value, pluginID, 0)
}

func (p *Preferences) saveStringLocked(key, value objc.ID, pluginID string, removeKey objc.ID) error {
	domain := p.defaults.Send(objc.RegisterName("persistentDomainForName:"), p.domain)
	candidate := objc.ID(objc.GetClass("NSMutableDictionary")).Send(objc.RegisterName("dictionary"))
	if domain != 0 {
		candidate.Send(objc.RegisterName("addEntriesFromDictionary:"), domain)
	}
	set := objc.RegisterName("setObject:forKey:")
	candidate.Send(set, value, key)
	if removeKey != 0 {
		candidate.Send(objc.RegisterName("removeObjectForKey:"), removeKey)
	}
	if pluginID != "" {
		size, err := defaultsSize(pluginDefaults(candidate, pluginID))
		if err != nil {
			return err
		}
		if size > pluginStorageCapacity {
			return fmt.Errorf("plugin storage capacity exceeded")
		}
	}
	size, err := defaultsSize(candidate)
	if err != nil {
		return err
	}
	if size >= userDefaultsProcessLimit {
		return fmt.Errorf("write would reach the tvOS defaults database limit")
	}
	p.defaults.Send(set, value, key)
	if removeKey != 0 {
		p.defaults.Send(objc.RegisterName("removeObjectForKey:"), removeKey)
	}
	return nil
}

func pluginDefaults(domain objc.ID, pluginID string) objc.ID {
	scope := pluginStorageKey(pluginID, pluginID)
	prefix := scope + "/"
	plugin := objc.ID(objc.GetClass("NSMutableDictionary")).Send(objc.RegisterName("dictionary"))
	keys := domain.Send(objc.RegisterName("allKeys"))
	for index := range uintptr(keys.Send(objc.RegisterName("count"))) {
		key := keys.Send(objc.RegisterName("objectAtIndex:"), index)
		name := objc.Send[string](key, objc.RegisterName("UTF8String"))
		if name == scope || strings.HasPrefix(name, prefix) {
			value := domain.Send(objc.RegisterName("objectForKey:"), key)
			plugin.Send(objc.RegisterName("setObject:forKey:"), value, key)
		}
	}
	return plugin
}

func defaultsSize(domain objc.ID) (uintptr, error) {
	const binaryPropertyList = 200
	data := objc.ID(objc.GetClass("NSPropertyListSerialization")).Send(
		objc.RegisterName("dataWithPropertyList:format:options:error:"), domain,
		uintptr(binaryPropertyList), uintptr(0), uintptr(0),
	)
	if data == 0 {
		return 0, fmt.Errorf("encode defaults data")
	}
	return uintptr(data.Send(objc.RegisterName("length"))), nil
}
