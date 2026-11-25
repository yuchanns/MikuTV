package plugin

import (
	"container/list"
	"errors"
	"sync"
	"time"
)

const (
	// CacheCapacity bounds the total in-memory UTF-8 bytes held by one entry session's host.cache.
	CacheCapacity = 4 << 20
	// CacheMaxEntryBytes bounds any single key+value entry in host.cache.
	CacheMaxEntryBytes = 256 << 10
	// CacheMaxEntries caps total live keys in one SessionCache.
	CacheMaxEntries = 4096
)

type cacheEntry struct {
	key       string
	value     string
	size      int
	expiresAt time.Time
}

// SessionCache is a thread-safe, bounded, TTL-aware in-memory LRU cache owned
// by a single entry Session and cleared on entry close or account switch.
type SessionCache struct {
	mu       sync.Mutex
	capacity int
	maxEntry int
	maxItems int
	bytes    int
	ll       *list.List
	items    map[string]*list.Element
	now      func() time.Time
}

func NewSessionCache() *SessionCache {
	return newSessionCache(CacheCapacity, CacheMaxEntryBytes, CacheMaxEntries, time.Now)
}

func newSessionCache(capacity, maxEntry, maxItems int, now func() time.Time) *SessionCache {
	if now == nil {
		now = time.Now
	}
	return &SessionCache{
		capacity: capacity,
		maxEntry: maxEntry,
		maxItems: maxItems,
		ll:       list.New(),
		items:    make(map[string]*list.Element),
		now:      now,
	}
}

func (c *SessionCache) Get(key string) (string, bool) {
	if c == nil || key == "" {
		return "", false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	elem, ok := c.items[key]
	if !ok {
		return "", false
	}
	entry := elem.Value.(*cacheEntry)
	if !entry.expiresAt.IsZero() && !c.now().Before(entry.expiresAt) {
		c.removeElementLocked(elem)
		return "", false
	}
	c.ll.MoveToFront(elem)
	return entry.value, true
}

func (c *SessionCache) Set(key, value string, ttl time.Duration) error {
	if c == nil {
		return nil
	}
	if key == "" {
		return errors.New("cache key required")
	}
	size := len(key) + len(value)
	if size > c.maxEntry || size > c.capacity {
		return errors.New("cache entry exceeds limit")
	}
	var expiresAt time.Time
	if ttl > 0 {
		expiresAt = c.now().Add(ttl)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if elem, ok := c.items[key]; ok {
		entry := elem.Value.(*cacheEntry)
		c.bytes -= entry.size
		entry.value = value
		entry.size = size
		entry.expiresAt = expiresAt
		c.bytes += size
		c.ll.MoveToFront(elem)
	} else {
		entry := &cacheEntry{key: key, value: value, size: size, expiresAt: expiresAt}
		elem := c.ll.PushFront(entry)
		c.items[key] = elem
		c.bytes += size
	}
	c.evictLocked()
	return nil
}

func (c *SessionCache) Delete(key string) {
	if c == nil || key == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if elem, ok := c.items[key]; ok {
		c.removeElementLocked(elem)
	}
}

func (c *SessionCache) Clear() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ll.Init()
	clear(c.items)
	c.bytes = 0
}

func (c *SessionCache) Len() int {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ll.Len()
}

func (c *SessionCache) Bytes() int {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.bytes
}

func (c *SessionCache) evictLocked() {
	now := c.now()
	for c.ll.Len() > 0 && (c.bytes > c.capacity || c.ll.Len() > c.maxItems) {
		back := c.ll.Back()
		if back == nil {
			break
		}
		c.removeElementLocked(back)
	}
	// Opportunistically drop expired tail entries when checking back.
	for back := c.ll.Back(); back != nil; {
		entry := back.Value.(*cacheEntry)
		if entry.expiresAt.IsZero() || now.Before(entry.expiresAt) {
			break
		}
		prev := back.Prev()
		c.removeElementLocked(back)
		back = prev
	}
}

func (c *SessionCache) removeElementLocked(elem *list.Element) {
	entry := elem.Value.(*cacheEntry)
	c.ll.Remove(elem)
	delete(c.items, entry.key)
	c.bytes -= entry.size
}
