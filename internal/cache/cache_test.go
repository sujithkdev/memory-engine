package cache_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/memory-engine/memory-engine/internal/cache"
)

// ── MemoryCache: basic Get/Set/Delete ─────────────────────────────────────────

func TestMemoryCache_SetAndGet(t *testing.T) {
	c := cache.NewMemoryCache(100, 0)
	c.Set("key1", "value1", 1*time.Minute)

	v, ok := c.Get("key1")
	if !ok {
		t.Fatal("expected cache hit")
	}
	if v != "value1" {
		t.Errorf("expected 'value1', got %v", v)
	}
}

func TestMemoryCache_MissingKey_ReturnsFalse(t *testing.T) {
	c := cache.NewMemoryCache(100, 0)
	_, ok := c.Get("nonexistent")
	if ok {
		t.Error("expected cache miss for nonexistent key")
	}
}

func TestMemoryCache_Expiry_KeyGone(t *testing.T) {
	c := cache.NewMemoryCache(100, 0)
	c.Set("expiring", "val", 50*time.Millisecond)

	// Should hit immediately.
	_, ok := c.Get("expiring")
	if !ok {
		t.Fatal("expected hit before expiry")
	}

	// Wait for expiry.
	time.Sleep(100 * time.Millisecond)

	_, ok = c.Get("expiring")
	if ok {
		t.Error("expected miss after TTL expiry")
	}
}

func TestMemoryCache_Delete(t *testing.T) {
	c := cache.NewMemoryCache(100, 0)
	c.Set("del", "val", 1*time.Minute)
	c.Delete("del")

	_, ok := c.Get("del")
	if ok {
		t.Error("expected miss after Delete")
	}
}

func TestMemoryCache_DeletePrefix(t *testing.T) {
	c := cache.NewMemoryCache(100, 0)
	c.Set("user:1:data", "a", time.Minute)
	c.Set("user:1:list", "b", time.Minute)
	c.Set("user:2:data", "c", time.Minute)
	c.Set("other:key", "d", time.Minute)

	c.DeletePrefix("user:1:")

	if _, ok := c.Get("user:1:data"); ok {
		t.Error("user:1:data should be deleted")
	}
	if _, ok := c.Get("user:1:list"); ok {
		t.Error("user:1:list should be deleted")
	}
	if _, ok := c.Get("user:2:data"); !ok {
		t.Error("user:2:data should still exist")
	}
	if _, ok := c.Get("other:key"); !ok {
		t.Error("other:key should still exist")
	}
}

func TestMemoryCache_Clear(t *testing.T) {
	c := cache.NewMemoryCache(100, 0)
	c.Set("a", 1, time.Minute)
	c.Set("b", 2, time.Minute)
	c.Set("c", 3, time.Minute)
	c.Clear()

	stats := c.Stats()
	if stats.Items != 0 {
		t.Errorf("expected 0 items after Clear, got %d", stats.Items)
	}
}

func TestMemoryCache_Overwrite(t *testing.T) {
	c := cache.NewMemoryCache(100, 0)
	c.Set("key", "v1", time.Minute)
	c.Set("key", "v2", time.Minute)

	v, ok := c.Get("key")
	if !ok {
		t.Fatal("expected hit")
	}
	if v != "v2" {
		t.Errorf("expected overwritten value 'v2', got %v", v)
	}
}

// ── Stats ─────────────────────────────────────────────────────────────────────

func TestMemoryCache_Stats_HitsAndMisses(t *testing.T) {
	c := cache.NewMemoryCache(100, 0)
	c.Set("x", 1, time.Minute)

	c.Get("x")        // hit
	c.Get("x")        // hit
	c.Get("notfound") // miss

	stats := c.Stats()
	if stats.Hits != 2 {
		t.Errorf("expected 2 hits, got %d", stats.Hits)
	}
	if stats.Misses != 1 {
		t.Errorf("expected 1 miss, got %d", stats.Misses)
	}
}

func TestMemoryCache_Stats_HitRate(t *testing.T) {
	c := cache.NewMemoryCache(100, 0)
	c.Set("k", 1, time.Minute)
	c.Get("k")        // hit
	c.Get("k")        // hit
	c.Get("k")        // hit
	c.Get("missing")  // miss

	stats := c.Stats()
	rate := stats.HitRate()
	// 3 hits / 4 total = 0.75
	if rate < 0.74 || rate > 0.76 {
		t.Errorf("expected hit rate ~0.75, got %.4f", rate)
	}
}

func TestMemoryCache_Stats_ZeroTotal_HitRateZero(t *testing.T) {
	c := cache.NewMemoryCache(100, 0)
	stats := c.Stats()
	if stats.HitRate() != 0 {
		t.Errorf("expected 0 hit rate with no requests, got %.4f", stats.HitRate())
	}
}

func TestMemoryCache_Stats_ItemCount(t *testing.T) {
	c := cache.NewMemoryCache(100, 0)
	for i := 0; i < 5; i++ {
		c.Set(fmt.Sprintf("k%d", i), i, time.Minute)
	}
	if c.Stats().Items != 5 {
		t.Errorf("expected 5 items, got %d", c.Stats().Items)
	}
}

// ── MaxItems eviction ─────────────────────────────────────────────────────────

func TestMemoryCache_MaxItems_Evicts(t *testing.T) {
	c := cache.NewMemoryCache(3, 0) // max 3 items
	c.Set("a", 1, time.Minute)
	c.Set("b", 2, time.Minute)
	c.Set("c", 3, time.Minute)
	c.Set("d", 4, time.Minute) // should trigger eviction

	stats := c.Stats()
	if stats.Items > 3 {
		t.Errorf("cache exceeded maxItems=3: got %d items", stats.Items)
	}
	if stats.Evictions < 1 {
		t.Errorf("expected at least 1 eviction, got %d", stats.Evictions)
	}
}

// ── Zero TTL ──────────────────────────────────────────────────────────────────

func TestMemoryCache_ZeroTTL_NotStored(t *testing.T) {
	c := cache.NewMemoryCache(100, 0)
	c.Set("k", "v", 0) // zero TTL should not store

	_, ok := c.Get("k")
	if ok {
		t.Error("zero TTL item should not be retrievable")
	}
}

// ── Cleanup sweep ─────────────────────────────────────────────────────────────

func TestMemoryCache_CleanupSweep_RemovesExpired(t *testing.T) {
	c := cache.NewMemoryCache(100, 50*time.Millisecond)
	defer c.Stop()

	c.Set("short", "v", 30*time.Millisecond)
	c.Set("long", "v", 10*time.Second)

	time.Sleep(100 * time.Millisecond) // wait for sweep

	if c.Stats().Items > 1 {
		t.Errorf("expected expired item to be swept, items=%d", c.Stats().Items)
	}
}

// ── NullCache ─────────────────────────────────────────────────────────────────

func TestNullCache_AlwaysMisses(t *testing.T) {
	c := &cache.NullCache{}
	c.Set("k", "v", time.Minute)
	_, ok := c.Get("k")
	if ok {
		t.Error("NullCache should always miss")
	}
}

func TestNullCache_NoopOperationsDontPanic(t *testing.T) {
	c := &cache.NullCache{}
	c.Delete("k")
	c.DeletePrefix("pre:")
	c.Clear()
	c.Stats()
}

// ── Cache keys ────────────────────────────────────────────────────────────────

func TestCacheKeys_NoCollisions(t *testing.T) {
	keys := []string{
		cache.UserKey("user1"),
		cache.MemoryKey("mem1"),
		cache.MemoryListKey("u1", "NS", "FACT", "ACTIVE", 1, 20),
		cache.NamespaceSuggestKey("u1", "CO"),
		cache.SearchKey("u1", "python", "CODING", "PREFERENCE", 1, 10),
		cache.EmbeddingKey("hello world", "nomic-embed-text"),
		cache.RetrievalKey("u1", "test query", "TRADING", 5),
	}

	seen := make(map[string]bool)
	for _, k := range keys {
		if seen[k] {
			t.Errorf("duplicate cache key: %q", k)
		}
		seen[k] = true
		if k == "" {
			t.Error("empty cache key")
		}
	}
}

func TestCacheKeys_InvalidateUser_ClearsCorrectPrefixes(t *testing.T) {
	c := cache.NewMemoryCache(100, 0)

	userID := "user123"
	c.Set(cache.UserKey(userID), "profile", time.Minute)
	c.Set(cache.MemoryListKey(userID, "NS", "", "ACTIVE", 1, 20), "list", time.Minute)
	c.Set(cache.SearchKey(userID, "q", "", "", 1, 10), "results", time.Minute)
	c.Set(cache.UserKey("other_user"), "other_profile", time.Minute)

	cache.InvalidateUser(c, userID)

	if _, ok := c.Get(cache.UserKey(userID)); ok {
		t.Error("user key should be invalidated")
	}
	if _, ok := c.Get(cache.UserKey("other_user")); !ok {
		t.Error("other user key should remain")
	}
}

func TestCacheKeys_InvalidateMemory_ClearsListCaches(t *testing.T) {
	c := cache.NewMemoryCache(100, 0)

	userID := "u1"
	memID := "m1"
	c.Set(cache.MemoryKey(memID), "memory", time.Minute)
	c.Set(cache.MemoryListKey(userID, "NS", "", "", 1, 20), "list", time.Minute)

	cache.InvalidateMemory(c, memID, userID)

	if _, ok := c.Get(cache.MemoryKey(memID)); ok {
		t.Error("memory key should be invalidated")
	}
	if _, ok := c.Get(cache.MemoryListKey(userID, "NS", "", "", 1, 20)); ok {
		t.Error("memory list cache should be invalidated")
	}
}

// ── Thread safety ─────────────────────────────────────────────────────────────

func TestMemoryCache_ConcurrentAccess_NoRace(t *testing.T) {
	c := cache.NewMemoryCache(1000, 0)

	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func(id int) {
			for j := 0; j < 100; j++ {
				key := fmt.Sprintf("key-%d-%d", id, j)
				c.Set(key, j, time.Minute)
				c.Get(key)
				if j%10 == 0 {
					c.Delete(key)
				}
			}
			done <- struct{}{}
		}(i)
	}

	for i := 0; i < 10; i++ {
		<-done
	}
	// No race conditions — test passes if no panic/race detector error.
}
