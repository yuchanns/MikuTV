package app

import (
	"context"
	"errors"
	"fmt"
	"hash/crc32"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/yuchanns/MikuTV/internal/provider"
)

type DanmakuArea string

const (
	DanmakuAreaUpper DanmakuArea = "upper"
	DanmakuAreaFull  DanmakuArea = "full"
	DanmakuAreaTop   DanmakuArea = "top"
)

type DanmakuFontSize string

const (
	DanmakuFontNormal DanmakuFontSize = "normal"
	DanmakuFontLarge  DanmakuFontSize = "large"
)

type DanmakuDisplay struct {
	Show     bool            `json:"show"`
	Density  int             `json:"density"`
	Area     DanmakuArea     `json:"area"`
	Opacity  int             `json:"opacity"`
	FontSize DanmakuFontSize `json:"fontSize"`
}

func DefaultDanmakuDisplay() DanmakuDisplay {
	return DanmakuDisplay{
		Show:     true,
		Density:  60,
		Area:     DanmakuAreaUpper,
		Opacity:  75,
		FontSize: DanmakuFontNormal,
	}
}

func (d DanmakuDisplay) Normalize() DanmakuDisplay {
	if d.Density < 10 {
		d.Density = 10
	} else if d.Density > 100 {
		d.Density = 100
	}
	if d.Opacity < 10 {
		d.Opacity = 10
	} else if d.Opacity > 100 {
		d.Opacity = 100
	}
	switch d.Area {
	case DanmakuAreaUpper, DanmakuAreaFull, DanmakuAreaTop:
	default:
		d.Area = DanmakuAreaUpper
	}
	switch d.FontSize {
	case DanmakuFontNormal, DanmakuFontLarge:
	default:
		d.FontSize = DanmakuFontNormal
	}
	return d
}

type DanmakuSettingsStore interface {
	DanmakuDisplay() (DanmakuDisplay, bool)
	SaveDanmakuDisplay(DanmakuDisplay) error
}

type DanmakuRulesStore interface {
	DanmakuRules(entryID, owner string) []provider.DanmakuRule
	SaveDanmakuRules(entryID, owner string, rules []provider.DanmakuRule) error
}

type DanmakuRulesSyncState uint8

const (
	DanmakuRulesIdle DanmakuRulesSyncState = iota
	DanmakuRulesSyncing
	DanmakuRulesSynced
	DanmakuRulesFailed
)

var (
	ErrDanmakuRuleEmpty       = errors.New("danmaku rule pattern is required")
	ErrDanmakuRuleInvalidUser = errors.New("danmaku user rule must contain digits only")
	ErrDanmakuRuleDuplicate   = errors.New("danmaku rule already exists")
)

type DanmakuRulesSnapshot struct {
	Display        DanmakuDisplay
	Rules          []provider.DanmakuRule
	EntryID        string
	Owner          string
	CloudAvailable bool
	SignedIn       bool
	SyncState      DanmakuRulesSyncState
	SyncError      provider.ErrorCode
	Busy           bool
	Revision       uint64
}

type DanmakuRules struct {
	mu             sync.RWMutex
	settingsStore  DanmakuSettingsStore
	rulesStore     DanmakuRulesStore
	post           func(func())
	notify         func()
	catalog        *Catalog
	display        DanmakuDisplay
	entryID        string
	owner          string
	cloudAvailable bool
	rules          []provider.DanmakuRule
	syncState      DanmakuRulesSyncState
	syncError      provider.ErrorCode
	syncCancel     context.CancelFunc
	syncSeq        uint64
	mutationCancel context.CancelFunc
	mutationSeq    uint64
	mutationActive bool
	generation     uint64
	nextLocalID    uint64
	revision       uint64
}

func NewDanmakuRules(store any, post func(func()), notify func()) *DanmakuRules {
	if post == nil {
		post = func(fn func()) { fn() }
	}
	if notify == nil {
		notify = func() {}
	}
	settingsStore, _ := store.(DanmakuSettingsStore)
	rulesStore, _ := store.(DanmakuRulesStore)
	display := DefaultDanmakuDisplay()
	if settingsStore != nil {
		if loaded, ok := settingsStore.DanmakuDisplay(); ok {
			display = loaded.Normalize()
		}
	}
	return &DanmakuRules{
		settingsStore: settingsStore,
		rulesStore:    rulesStore,
		post:          post,
		notify:        notify,
		display:       display,
		revision:      1,
	}
}

func (r *DanmakuRules) Close() {
	r.mu.Lock()
	r.generation++
	if r.syncCancel != nil {
		r.syncCancel()
		r.syncCancel = nil
	}
	if r.mutationCancel != nil {
		r.mutationCancel()
		r.mutationCancel = nil
	}
	r.mutationActive = false
	r.mu.Unlock()
}

func (r *DanmakuRules) Snapshot() DanmakuRulesSnapshot {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return DanmakuRulesSnapshot{
		Display:        r.display,
		Rules:          slices.Clone(r.rules),
		EntryID:        r.entryID,
		Owner:          r.owner,
		CloudAvailable: r.cloudAvailable,
		SignedIn:       r.owner != "",
		SyncState:      r.syncState,
		SyncError:      r.syncError,
		Busy:           r.mutationActive,
		Revision:       r.revision,
	}
}

func (r *DanmakuRules) Display() DanmakuDisplay {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.display
}

func (r *DanmakuRules) UpdateDisplay(update func(*DanmakuDisplay)) error {
	r.mu.Lock()
	next := r.display
	update(&next)
	next = next.Normalize()
	if next == r.display {
		r.mu.Unlock()
		return nil
	}
	r.display = next
	r.revision++
	store := r.settingsStore
	r.mu.Unlock()
	var err error
	if store != nil {
		err = store.SaveDanmakuDisplay(next)
	}
	r.notify()
	return err
}

func (r *DanmakuRules) SetScope(entryID string, caps provider.Capabilities, owner string, catalog *Catalog) {
	cloud := caps.Has(provider.DanmakuRules)
	r.mu.Lock()
	if r.entryID == entryID && r.owner == owner && r.cloudAvailable == cloud && r.catalog == catalog {
		r.mu.Unlock()
		return
	}
	r.generation++
	if r.syncCancel != nil {
		r.syncCancel()
		r.syncCancel = nil
	}
	if r.mutationCancel != nil {
		r.mutationCancel()
		r.mutationCancel = nil
	}
	r.mutationActive = false
	r.entryID = entryID
	r.owner = owner
	r.cloudAvailable = cloud
	r.catalog = catalog
	r.syncState = DanmakuRulesIdle
	r.syncError = ""
	var loaded []provider.DanmakuRule
	if r.rulesStore != nil && entryID != "" {
		loaded = normalizeStoredRules(r.rulesStore.DanmakuRules(entryID, owner))
	}
	r.rules = loaded
	r.revision++
	r.mu.Unlock()
	r.notify()
}

func (r *DanmakuRules) EnsureSynced() {
	r.mu.RLock()
	shouldSync := r.cloudAvailable && r.owner != "" && r.catalog != nil &&
		r.syncState == DanmakuRulesIdle && r.syncCancel == nil
	r.mu.RUnlock()
	if shouldSync {
		r.Sync()
	}
}

func (r *DanmakuRules) Sync() {
	r.mu.Lock()
	if !r.cloudAvailable || r.owner == "" || r.catalog == nil {
		r.mu.Unlock()
		return
	}
	if r.syncCancel != nil {
		r.syncCancel()
		r.syncCancel = nil
	}
	gen := r.generation
	r.syncSeq++
	seq := r.syncSeq
	entryID, owner := r.entryID, r.owner
	catalog := r.catalog
	r.syncState = DanmakuRulesSyncing
	r.syncError = ""
	r.revision++
	r.mu.Unlock()

	cancel := catalog.ReadDanmakuRules(func(remote []provider.DanmakuRule, err error) {
		r.mu.Lock()
		if r.generation != gen || r.syncSeq != seq || r.entryID != entryID || r.owner != owner {
			r.mu.Unlock()
			return
		}
		r.syncCancel = nil
		if err != nil {
			r.syncState = DanmakuRulesFailed
			r.syncError = provider.CodeOf(err)
			r.revision++
			r.mu.Unlock()
			r.notify()
			return
		}
		merged := mergeCloudRules(r.rules, remote)
		r.rules = merged
		r.syncState = DanmakuRulesSynced
		r.syncError = ""
		r.revision++
		store := r.rulesStore
		toSave := slices.Clone(merged)
		r.mu.Unlock()
		if store != nil && entryID != "" {
			_ = store.SaveDanmakuRules(entryID, owner, toSave)
		}
		r.notify()
	})
	r.mu.Lock()
	if r.generation == gen && r.syncSeq == seq && r.entryID == entryID && r.owner == owner && r.syncState == DanmakuRulesSyncing {
		r.syncCancel = cancel
	}
	r.mu.Unlock()
	r.notify()
}

func (r *DanmakuRules) ValidateDraft(kind provider.DanmakuRuleKind, raw string) (string, error) {
	pattern := strings.TrimSpace(raw)
	if pattern == "" {
		return "", ErrDanmakuRuleEmpty
	}
	if kind == provider.DanmakuRuleUser && !provider.ValidDanmakuUserID(pattern) {
		return "", ErrDanmakuRuleInvalidUser
	}
	if !provider.ValidDanmakuRulePattern(kind, pattern) {
		return "", ErrDanmakuRuleEmpty
	}
	r.mu.RLock()
	dup := hasMatchingRule(r.rules, kind, pattern)
	r.mu.RUnlock()
	if dup {
		return "", ErrDanmakuRuleDuplicate
	}
	return pattern, nil
}

func (r *DanmakuRules) Add(kind provider.DanmakuRuleKind, raw string, done func(error)) {
	pattern, err := r.ValidateDraft(kind, raw)
	if err != nil {
		if done != nil {
			done(err)
		}
		return
	}
	r.mu.Lock()
	if r.mutationActive {
		r.mu.Unlock()
		return
	}
	gen := r.generation
	entryID, owner := r.entryID, r.owner
	useCloud := r.cloudAvailable && owner != "" && r.catalog != nil
	catalog := r.catalog
	if !useCloud {
		r.nextLocalID++
		rule := provider.DanmakuRule{
			ID:      fmt.Sprintf("local:%d", r.nextLocalID),
			Kind:    kind,
			Pattern: pattern,
			Enabled: true,
		}
		r.rules = append(r.rules, rule)
		r.revision++
		store := r.rulesStore
		toSave := slices.Clone(r.rules)
		r.mu.Unlock()
		var saveErr error
		if store != nil && entryID != "" {
			saveErr = store.SaveDanmakuRules(entryID, owner, toSave)
		}
		r.notify()
		if done != nil {
			done(saveErr)
		}
		return
	}
	r.mutationActive = true
	r.mutationSeq++
	seq := r.mutationSeq
	r.revision++
	r.mu.Unlock()

	cancel := catalog.AddDanmakuRule(provider.DanmakuRuleDraft{
		Owner: owner, Kind: kind, Pattern: pattern,
	}, func(created provider.DanmakuRule, err error) {
		r.mu.Lock()
		if r.generation != gen || r.mutationSeq != seq || r.entryID != entryID || r.owner != owner {
			r.mu.Unlock()
			return
		}
		r.mutationActive = false
		r.mutationCancel = nil
		if err != nil {
			r.revision++
			r.mu.Unlock()
			r.notify()
			if done != nil {
				done(err)
			}
			return
		}
		created.Kind = kind
		created.Pattern = pattern
		created.Enabled = true
		created.Synced = true
		if created.ID == "" {
			if created.RemoteID != "" {
				created.ID = "remote:" + created.RemoteID
			} else {
				r.nextLocalID++
				created.ID = fmt.Sprintf("local:%d", r.nextLocalID)
			}
		}
		if !hasMatchingRule(r.rules, kind, pattern) {
			r.rules = append(r.rules, created)
		}
		r.syncState = DanmakuRulesSynced
		r.syncError = ""
		r.revision++
		store := r.rulesStore
		toSave := slices.Clone(r.rules)
		r.mu.Unlock()
		if store != nil && entryID != "" {
			_ = store.SaveDanmakuRules(entryID, owner, toSave)
		}
		r.notify()
		if done != nil {
			done(nil)
		}
	})
	r.mu.Lock()
	if r.generation == gen && r.mutationSeq == seq && r.entryID == entryID && r.owner == owner && r.mutationActive {
		r.mutationCancel = cancel
	}
	r.mu.Unlock()
	r.notify()
}

func (r *DanmakuRules) Toggle(id string) error {
	r.mu.Lock()
	idx := slices.IndexFunc(r.rules, func(rule provider.DanmakuRule) bool { return rule.ID == id })
	if idx < 0 {
		r.mu.Unlock()
		return nil
	}
	r.rules[idx].Enabled = !r.rules[idx].Enabled
	r.revision++
	store := r.rulesStore
	entryID, owner := r.entryID, r.owner
	toSave := slices.Clone(r.rules)
	r.mu.Unlock()
	var err error
	if store != nil && entryID != "" {
		err = store.SaveDanmakuRules(entryID, owner, toSave)
	}
	r.notify()
	return err
}

func (r *DanmakuRules) Remove(id string, done func(error)) {
	r.mu.Lock()
	if r.mutationActive {
		r.mu.Unlock()
		return
	}
	idx := slices.IndexFunc(r.rules, func(rule provider.DanmakuRule) bool { return rule.ID == id })
	if idx < 0 {
		r.mu.Unlock()
		if done != nil {
			done(nil)
		}
		return
	}
	target := r.rules[idx]
	gen := r.generation
	entryID, owner := r.entryID, r.owner
	useCloud := target.Synced && target.RemoteID != "" && r.cloudAvailable && owner != "" && r.catalog != nil
	catalog := r.catalog
	if !useCloud {
		r.rules = slices.Delete(r.rules, idx, idx+1)
		r.revision++
		store := r.rulesStore
		toSave := slices.Clone(r.rules)
		r.mu.Unlock()
		var saveErr error
		if store != nil && entryID != "" {
			saveErr = store.SaveDanmakuRules(entryID, owner, toSave)
		}
		r.notify()
		if done != nil {
			done(saveErr)
		}
		return
	}
	r.mutationActive = true
	r.mutationSeq++
	seq := r.mutationSeq
	r.revision++
	r.mu.Unlock()

	cancel := catalog.RemoveDanmakuRule(provider.DanmakuRuleRemoval{
		Owner: owner, RemoteID: target.RemoteID,
	}, func(err error) {
		r.mu.Lock()
		if r.generation != gen || r.mutationSeq != seq || r.entryID != entryID || r.owner != owner {
			r.mu.Unlock()
			return
		}
		r.mutationActive = false
		r.mutationCancel = nil
		if err != nil {
			r.revision++
			r.mu.Unlock()
			r.notify()
			if done != nil {
				done(err)
			}
			return
		}
		r.rules = slices.DeleteFunc(r.rules, func(rule provider.DanmakuRule) bool {
			return rule.ID == id || (target.RemoteID != "" && rule.RemoteID == target.RemoteID)
		})
		r.revision++
		store := r.rulesStore
		toSave := slices.Clone(r.rules)
		r.mu.Unlock()
		if store != nil && entryID != "" {
			_ = store.SaveDanmakuRules(entryID, owner, toSave)
		}
		r.notify()
		if done != nil {
			done(nil)
		}
	})
	r.mu.Lock()
	if r.generation == gen && r.mutationSeq == seq && r.entryID == entryID && r.owner == owner && r.mutationActive {
		r.mutationCancel = cancel
	}
	r.mu.Unlock()
	r.notify()
}

func (r *DanmakuRules) Blocks(text, senderID, senderHash string) bool {
	if r == nil {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return DanmakuBlockedByRules(r.rules, text, senderID, senderHash)
}

func DanmakuBlockedByRules(rules []provider.DanmakuRule, text, senderID, senderHash string) bool {
	if len(rules) == 0 {
		return false
	}
	lowerText := strings.ToLower(text)
	normHash := normalizeDanmakuHash(senderHash)
	trimmedSenderID := strings.TrimSpace(senderID)
	for _, rule := range rules {
		if !rule.Enabled || rule.Pattern == "" {
			continue
		}
		switch rule.Kind {
		case provider.DanmakuRuleKeyword:
			if strings.Contains(lowerText, strings.ToLower(rule.Pattern)) {
				return true
			}
		case provider.DanmakuRuleUser:
			if trimmedSenderID != "" && trimmedSenderID == rule.Pattern {
				return true
			}
			if normHash != "" {
				if normalizeDanmakuHash(rule.Pattern) == normHash {
					return true
				}
				if provider.ValidDanmakuUserID(rule.Pattern) && DanmakuUserHash(rule.Pattern) == normHash {
					return true
				}
			}
		}
	}
	return false
}

func DanmakuUserHash(uid string) string {
	return strconv.FormatUint(uint64(crc32.ChecksumIEEE([]byte(uid))), 16)
}

func normalizeDanmakuHash(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	if s == "" {
		return ""
	}
	trimmed := strings.TrimLeft(s, "0")
	if trimmed == "" {
		return "0"
	}
	return trimmed
}

func rulesMatch(aKind provider.DanmakuRuleKind, aPattern string, bKind provider.DanmakuRuleKind, bPattern string) bool {
	if aKind != bKind {
		return false
	}
	if aKind == provider.DanmakuRuleKeyword {
		return strings.EqualFold(strings.TrimSpace(aPattern), strings.TrimSpace(bPattern))
	}
	aNorm := strings.TrimSpace(aPattern)
	bNorm := strings.TrimSpace(bPattern)
	if strings.EqualFold(aNorm, bNorm) {
		return true
	}
	if provider.ValidDanmakuUserID(aNorm) && normalizeDanmakuHash(bNorm) == DanmakuUserHash(aNorm) {
		return true
	}
	if provider.ValidDanmakuUserID(bNorm) && normalizeDanmakuHash(aNorm) == DanmakuUserHash(bNorm) {
		return true
	}
	return false
}

func hasMatchingRule(rules []provider.DanmakuRule, kind provider.DanmakuRuleKind, pattern string) bool {
	for _, rule := range rules {
		if rulesMatch(rule.Kind, rule.Pattern, kind, pattern) {
			return true
		}
	}
	return false
}

func normalizeStoredRules(rules []provider.DanmakuRule) []provider.DanmakuRule {
	if len(rules) == 0 {
		return nil
	}
	out := make([]provider.DanmakuRule, 0, len(rules))
	for i, rule := range rules {
		if !provider.ValidDanmakuRuleKind(rule.Kind) || strings.TrimSpace(rule.Pattern) == "" {
			continue
		}
		rule.Pattern = strings.TrimSpace(rule.Pattern)
		if rule.ID == "" {
			if rule.RemoteID != "" {
				rule.ID = "remote:" + rule.RemoteID
			} else {
				rule.ID = fmt.Sprintf("saved:%d", i+1)
			}
		}
		if hasMatchingRule(out, rule.Kind, rule.Pattern) {
			continue
		}
		out = append(out, rule)
	}
	return out
}

func mergeCloudRules(existing, remote []provider.DanmakuRule) []provider.DanmakuRule {
	merged := make([]provider.DanmakuRule, 0, len(existing)+len(remote))
	matchedRemote := make([]bool, len(remote))
	for _, local := range existing {
		foundIdx := -1
		for j, cloud := range remote {
			if matchedRemote[j] {
				continue
			}
			if (local.RemoteID != "" && local.RemoteID == cloud.RemoteID) ||
				rulesMatch(local.Kind, local.Pattern, cloud.Kind, cloud.Pattern) {
				foundIdx = j
				break
			}
		}
		if foundIdx >= 0 {
			matchedRemote[foundIdx] = true
			cloud := remote[foundIdx]
			pattern := local.Pattern
			if local.Kind == provider.DanmakuRuleUser && !provider.ValidDanmakuUserID(pattern) && cloud.Pattern != "" {
				pattern = cloud.Pattern
			}
			id := local.ID
			if id == "" && cloud.RemoteID != "" {
				id = "remote:" + cloud.RemoteID
			}
			merged = append(merged, provider.DanmakuRule{
				ID:       id,
				Kind:     cloud.Kind,
				Pattern:  pattern,
				Enabled:  local.Enabled,
				Synced:   true,
				RemoteID: cloud.RemoteID,
			})
			continue
		}
		if !local.Synced {
			merged = append(merged, local)
		}
	}
	for j, cloud := range remote {
		if matchedRemote[j] {
			continue
		}
		if !provider.ValidDanmakuRuleKind(cloud.Kind) || strings.TrimSpace(cloud.Pattern) == "" {
			continue
		}
		id := cloud.ID
		if id == "" {
			if cloud.RemoteID != "" {
				id = "remote:" + cloud.RemoteID
			} else {
				id = fmt.Sprintf("cloud:%d", j+1)
			}
		}
		merged = append(merged, provider.DanmakuRule{
			ID:       id,
			Kind:     cloud.Kind,
			Pattern:  strings.TrimSpace(cloud.Pattern),
			Enabled:  true,
			Synced:   true,
			RemoteID: cloud.RemoteID,
		})
	}
	return merged
}
