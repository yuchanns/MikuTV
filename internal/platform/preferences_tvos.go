//go:build ios && tvos && arm64

package platform

import (
	"crypto/sha256"
	"fmt"
	"runtime"
	"strings"
	"sync"

	"github.com/ebitengine/purego/objc"
	"github.com/yuchanns/MikuTV/internal/app"
)

const (
	// tvOS terminates the app at the documented 1 MB defaults database limit.
	userDefaultsProcessLimit = 1_000_000
	pluginStorageCapacity    = 8 << 10
)

// Preferences uses the app's defaults domain; tvOS does not allow desktop data paths.
type Preferences struct {
	defaults objc.ID
	key      objc.ID
	domain   objc.ID
	writes   sync.Mutex
}

func NewPreferences() (*Preferences, error) {
	defaults := objc.ID(objc.GetClass("NSUserDefaults")).Send(objc.RegisterName("standardUserDefaults"))
	if defaults == 0 {
		return nil, fmt.Errorf("app preferences are unavailable")
	}
	key := objc.ID(objc.GetClass("NSString")).Send(objc.RegisterName("stringWithUTF8String:"), "recentEntry")
	bundle := objc.ID(objc.GetClass("NSBundle")).Send(objc.RegisterName("mainBundle"))
	domain := bundle.Send(objc.RegisterName("bundleIdentifier"))
	if domain == 0 {
		return nil, fmt.Errorf("app defaults domain is unavailable")
	}
	key.Send(objc.RegisterName("retain"))
	domain.Send(objc.RegisterName("retain"))
	return &Preferences{defaults: defaults, key: key, domain: domain}, nil
}

func (p *Preferences) Load() (app.Preferences, error) {
	value := p.defaults.Send(objc.RegisterName("stringForKey:"), p.key)
	if value == 0 {
		return app.Preferences{}, nil
	}
	return app.Preferences{RecentEntry: objc.Send[string](value, objc.RegisterName("UTF8String"))}, nil
}

func (p *Preferences) Save(preferences app.Preferences) error {
	value := objc.ID(objc.GetClass("NSString")).Send(objc.RegisterName("stringWithUTF8String:"), preferences.RecentEntry)
	if value == 0 {
		return fmt.Errorf("encode recent entry")
	}
	return p.saveString(p.key, value, "")
}

func historySyncKey(entry, owner string) objc.ID {
	digest := sha256.Sum256([]byte(entry + "\x00" + owner))
	return objc.ID(objc.GetClass("NSString")).Send(objc.RegisterName("stringWithUTF8String:"), fmt.Sprintf("historySync.%x", digest))
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
	value := objc.ID(objc.GetClass("NSString")).Send(objc.RegisterName("stringWithUTF8String:"), state)
	key := historySyncKey(entry, owner)
	if key == 0 || value == 0 {
		return fmt.Errorf("encode history sync preference")
	}
	return p.saveString(key, value, "")
}

// LoadPluginStorage uses the UIKit thread and its autorelease pool at startup.
func (p *Preferences) LoadPluginStorage(pluginID, entryID string) []byte {
	key := objc.ID(objc.GetClass("NSString")).Send(objc.RegisterName("stringWithUTF8String:"), pluginStorageKey(pluginID, entryID))
	value := p.defaults.Send(objc.RegisterName("stringForKey:"), key)
	if value == 0 {
		return nil
	}
	return []byte(objc.Send[string](value, objc.RegisterName("UTF8String")))
}

func (p *Preferences) SavePluginStorage(pluginID, entryID string, data []byte) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	pool := objc.ID(objc.GetClass("NSAutoreleasePool")).Send(objc.RegisterName("new"))
	defer pool.Send(objc.RegisterName("drain"))
	stringClass := objc.ID(objc.GetClass("NSString"))
	key := stringClass.Send(objc.RegisterName("stringWithUTF8String:"), pluginStorageKey(pluginID, entryID))
	value := stringClass.Send(objc.RegisterName("stringWithUTF8String:"), string(data))
	if key == 0 || value == 0 {
		return fmt.Errorf("encode plugin storage")
	}
	return p.saveString(key, value, pluginID)
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
	domain := p.defaults.Send(objc.RegisterName("persistentDomainForName:"), p.domain)
	candidate := objc.ID(objc.GetClass("NSMutableDictionary")).Send(objc.RegisterName("dictionary"))
	if domain != 0 {
		candidate.Send(objc.RegisterName("addEntriesFromDictionary:"), domain)
	}
	set := objc.RegisterName("setObject:forKey:")
	candidate.Send(set, value, key)
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
