package cache

import (
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// entry is one cached item.
type entry struct {
	value     interface{}
	expiresAt time.Time
}

// isExpired returns true if the entry has passed its TTL.
func (e *entry) isExpired() bool {
	return time.Now().After(e.expiresAt)
}

// MemoryCache is a thread-safe in-memory cache with TTL expiry.
// Eviction is lazy (on access) + periodic cleanup via a background goroutine.
type MemoryCache struct {
	mu        sync.RWMutex
	items     map[string]*entry
	maxItems  int
	hits      int64
	misses    int64
	evictions int64
	stopClean chan struct{}
}

// NewMemoryCache creates a new MemoryCache.
// maxItems limits the number of entries (0 = unlimited).
// cleanupInterval controls how often expired items are swept (0 = no sweep).
func NewMemoryCache(maxItems int, cleanupInterval time.Duration) *MemoryCache {
	c := &MemoryCache{
		items:     make(map[string]*entry),
		maxItems:  maxItems,
		stopClean: make(chan struct{}),
	}

	if cleanupInterval > 0 {
		go c.cleanupLoop(cleanupInterval)
	}

	return c
}

// Get returns a cached value if present and not expired.
func (c *MemoryCache) Get(key string) (interface{}, bool) {
	c.mu.RLock()
	e, ok := c.items[key]
	c.mu.RUnlock()

	if !ok {
		atomic.AddInt64(&c.misses, 1)
		return nil, false
	}
	if e.isExpired() {
		c.mu.Lock()
		delete(c.items, key)
		c.mu.Unlock()
		atomic.AddInt64(&c.misses, 1)
		atomic.AddInt64(&c.evictions, 1)
		return nil, false
	}

	atomic.AddInt64(&c.hits, 1)
	return e.value, true
}

// Set stores a value with a TTL.
func (c *MemoryCache) Set(key string, value interface{}, ttl time.Duration) {
	if ttl <= 0 {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Evict oldest if at capacity.
	if c.maxItems > 0 && len(c.items) >= c.maxItems {
		c.evictOne()
	}

	c.items[key] = &entry{
		value:     value,
		expiresAt: time.Now().Add(ttl),
	}
}

// Delete removes a single key.
func (c *MemoryCache) Delete(key string) {
	c.mu.Lock()
	delete(c.items, key)
	c.mu.Unlock()
}

// DeletePrefix removes all keys starting with the given prefix.
func (c *MemoryCache) DeletePrefix(prefix string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k := range c.items {
		if strings.HasPrefix(k, prefix) {
			delete(c.items, k)
		}
	}
}

// Clear removes all keys.
func (c *MemoryCache) Clear() {
	c.mu.Lock()
	c.items = make(map[string]*entry)
	c.mu.Unlock()
}

// Stats returns current cache statistics.
func (c *MemoryCache) Stats() Stats {
	c.mu.RLock()
	items := len(c.items)
	c.mu.RUnlock()
	return Stats{
		Hits:      atomic.LoadInt64(&c.hits),
		Misses:    atomic.LoadInt64(&c.misses),
		Items:     items,
		Evictions: atomic.LoadInt64(&c.evictions),
	}
}

// Stop halts the background cleanup goroutine.
func (c *MemoryCache) Stop() {
	close(c.stopClean)
}

// evictOne removes the first expired entry found, or the first entry if none expired.
// Must be called with c.mu held for write.
func (c *MemoryCache) evictOne() {
	now := time.Now()
	for k, e := range c.items {
		if now.After(e.expiresAt) {
			delete(c.items, k)
			atomic.AddInt64(&c.evictions, 1)
			return
		}
	}
	// No expired entries — remove an arbitrary one.
	for k := range c.items {
		delete(c.items, k)
		atomic.AddInt64(&c.evictions, 1)
		return
	}
}

// cleanupLoop periodically removes all expired entries.
func (c *MemoryCache) cleanupLoop(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-c.stopClean:
			return
		case <-ticker.C:
			c.sweep()
		}
	}
}

// sweep removes all expired entries.
func (c *MemoryCache) sweep() {
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, e := range c.items {
		if now.After(e.expiresAt) {
			delete(c.items, k)
			atomic.AddInt64(&c.evictions, 1)
		}
	}
}

// NullCache is a no-op cache (always misses). Used when caching is disabled.
type NullCache struct{}

func (n *NullCache) Get(_ string) (interface{}, bool) { return nil, false }
func (n *NullCache) Set(_ string, _ interface{}, _ time.Duration) {}
func (n *NullCache) Delete(_ string)                  {}
func (n *NullCache) DeletePrefix(_ string)            {}
func (n *NullCache) Clear()                           {}
func (n *NullCache) Stats() Stats                     { return Stats{} }
