package topshelf

import "strings"

const keychainGroupSuffix = ".dev.yuchanns.MikuTV.topshelf"

func sharedAccessGroupCandidates(defaultGroup string) []string {
	prefix, _, ok := strings.Cut(strings.TrimSpace(defaultGroup), ".")
	prefix = strings.TrimSpace(prefix)
	if !ok || prefix == "" {
		return nil
	}
	return []string{
		prefix + keychainGroupSuffix,
		prefix + ".*",
	}
}

func selectSharedAccessGroup(defaultGroup string, canAccess func(string) bool) string {
	candidates := sharedAccessGroupCandidates(defaultGroup)
	if len(candidates) == 0 {
		return ""
	}
	if canAccess == nil {
		return candidates[0]
	}
	for _, group := range candidates {
		if canAccess(group) {
			return group
		}
	}
	return ""
}
