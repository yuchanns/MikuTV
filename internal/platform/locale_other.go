//go:build !(ios && tvos && arm64)

package platform

import (
	"os"
	"strings"
)

// PreferredLanguages returns preferred BCP-47 languages from environment variables
// for non-tvOS test and desktop builds.
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
	for _, key := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if val := strings.TrimSpace(os.Getenv(key)); val != "" && !strings.EqualFold(val, "C") && !strings.EqualFold(val, "POSIX") {
			return []string{val}
		}
	}
	return nil
}
