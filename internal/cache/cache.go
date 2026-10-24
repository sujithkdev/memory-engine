// Package cache provides a generic caching layer with TTL support.
//
// Design: the Cache interface decouples callers from the backend.
// Phase 10 ships with an in-memory implementation (no Redis required).
// Redis can be plugged in later by implementing the same interface.
//
// Cached items:
//   - User profile lookups       (TTL: 5m)
//   - Memory list queries        (TTL: 2m — short because memories change)
//   - Embedding vectors          (TTL: 1h — stable until model changes)
//   - Namespace suggestions      (TTL: 10m)
package cache

import (
	"time"
)

// Cache is the generic caching interface.
type Cache interface {
	// Get returns the cached value and true if found and not expired.
	Get(key string) (interface{}, bool)
	// Set stores a value with the given TTL.
	Set(key string, value interface{}, ttl time.Duration)
	// Delete removes a key.
	Delete(key string)
	// DeletePrefix removes all keys with the given prefix.
	DeletePrefix(prefix string)
	// Clear removes all keys.
	Clear()
	// Stats returns cache statistics.
	Stats() Stats
}

// Stats holds cache hit/miss statistics.
type Stats struct {
	Hits      int64
	Misses    int64
	Items     int
	Evictions int64
}

// HitRate returns the cache hit rate [0,1].
func (s Stats) HitRate() float64 {
	total := s.Hits + s.Misses
	if total == 0 {
		return 0
	}
	return float64(s.Hits) / float64(total)
}
