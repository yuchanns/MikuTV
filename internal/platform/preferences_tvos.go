//go:build ios && tvos && arm64

package platform

import (
	"fmt"

	"github.com/ebitengine/purego/objc"
	"github.com/yuchanns/MikuTV/internal/app"
)

// Preferences uses the app's defaults domain; tvOS does not allow desktop data paths.
type Preferences struct {
	defaults objc.ID
	key      objc.ID
}

func NewPreferences() (*Preferences, error) {
	defaults := objc.ID(objc.GetClass("NSUserDefaults")).Send(objc.RegisterName("standardUserDefaults"))
	if defaults == 0 {
		return nil, fmt.Errorf("app preferences are unavailable")
	}
	key := objc.ID(objc.GetClass("NSString")).Send(objc.RegisterName("stringWithUTF8String:"), "recentEntry")
	key.Send(objc.RegisterName("retain"))
	return &Preferences{defaults: defaults, key: key}, nil
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
	p.defaults.Send(objc.RegisterName("setObject:forKey:"), value, p.key)
	return nil
}
