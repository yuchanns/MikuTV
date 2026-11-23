//go:build ios && tvos && arm64

package platform

import (
	"os"
	"strings"

	"github.com/ebitengine/purego/objc"
)

// PreferredLanguages returns the tvOS system preferred BCP-47 languages in order,
// honoring MIKUTV_LOCALE_OVERRIDE when set for automated verification.
func PreferredLanguages() []string {
	if override := strings.TrimSpace(os.Getenv("MIKUTV_LOCALE_OVERRIDE")); override != "" {
		parts := strings.Split(override, ",")
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			if t := strings.TrimSpace(p); t != "" {
				out = append(out, t)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	var out []string
	withAutoreleasePool(func() {
		arr := objc.ID(objc.GetClass("NSLocale")).Send(objc.RegisterName("preferredLanguages"))
		if arr == 0 {
			return
		}
		count := int(arr.Send(objc.RegisterName("count")))
		objAt := objc.RegisterName("objectAtIndex:")
		utf8Sel := objc.RegisterName("UTF8String")
		for i := 0; i < count && i < 8; i++ {
			item := arr.Send(objAt, uintptr(i))
			if item == 0 {
				continue
			}
			if s := strings.TrimSpace(objc.Send[string](item, utf8Sel)); s != "" {
				out = append(out, s)
			}
		}
	})
	return out
}
