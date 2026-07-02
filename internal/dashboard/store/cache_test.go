package store

import (
	"testing"
	"time"
)

func TestLRUCacheHitAndMiss(t *testing.T) {
	c := newLRUCache(4, time.Minute)
	if _, ok := c.get("k", 100); ok {
		t.Fatal("expected miss on empty cache")
	}
	c.put("k", "v", 100)
	got, ok := c.get("k", 100)
	if !ok || got.(string) != "v" {
		t.Fatalf("expected hit v, got %v ok=%v", got, ok)
	}
	m := c.metrics()
	if m.Hits != 1 || m.Misses != 1 || m.Size != 1 || m.Capacity != 4 {
		t.Fatalf("unexpected metrics: %+v", m)
	}
}

func TestLRUCacheMtimeInvalidation(t *testing.T) {
	c := newLRUCache(4, time.Minute)
	c.put("k", "v", 100)
	// Same key, newer mtime -> stale -> miss, and the entry is dropped.
	if _, ok := c.get("k", 200); ok {
		t.Fatal("expected miss on mtime mismatch")
	}
	if m := c.metrics(); m.Size != 0 {
		t.Fatalf("stale entry should be evicted, size=%d", m.Size)
	}
}

func TestLRUCacheTTLExpiry(t *testing.T) {
	c := newLRUCache(4, 30*time.Second)
	now := time.Unix(1000, 0)
	c.now = func() time.Time { return now }
	c.put("k", "v", 1)
	// Within TTL -> hit.
	if _, ok := c.get("k", 1); !ok {
		t.Fatal("expected hit within TTL")
	}
	// Advance past TTL -> miss.
	now = now.Add(31 * time.Second)
	if _, ok := c.get("k", 1); ok {
		t.Fatal("expected miss after TTL expiry")
	}
}

func TestLRUCacheNoTTL(t *testing.T) {
	c := newLRUCache(4, 0) // TTL disabled
	c.now = func() time.Time { return time.Unix(0, 0) }
	c.put("k", "v", 1)
	// Far-future read still hits because TTL is disabled.
	c.now = func() time.Time { return time.Unix(1<<40, 0) }
	if _, ok := c.get("k", 1); !ok {
		t.Fatal("expected hit with TTL disabled")
	}
}

func TestLRUCacheEviction(t *testing.T) {
	c := newLRUCache(2, time.Minute)
	c.put("a", 1, 1)
	c.put("b", 2, 1)
	_, _ = c.get("a", 1) // touch a so b is LRU
	c.put("cc", 3, 1)    // over capacity -> evict LRU (b)
	if _, ok := c.get("b", 1); ok {
		t.Fatal("expected b to be evicted")
	}
	if _, ok := c.get("a", 1); !ok {
		t.Fatal("expected a to survive")
	}
	if m := c.metrics(); m.Evictions != 1 {
		t.Fatalf("expected 1 eviction, got %d", m.Evictions)
	}
}

func TestLRUCacheUpdateExisting(t *testing.T) {
	c := newLRUCache(2, time.Minute)
	c.put("k", "v1", 1)
	c.put("k", "v2", 2) // update in place, new mtime
	got, ok := c.get("k", 2)
	if !ok || got.(string) != "v2" {
		t.Fatalf("expected updated v2, got %v ok=%v", got, ok)
	}
	if m := c.metrics(); m.Size != 1 {
		t.Fatalf("update must not grow size, size=%d", m.Size)
	}
}

func TestLRUCacheEvictKey(t *testing.T) {
	c := newLRUCache(4, time.Minute)
	c.put("a", 1, 1)
	c.put("b", 2, 1)
	c.evict("a")
	c.evict("missing") // no-op branch
	if _, ok := c.get("a", 1); ok {
		t.Fatal("evicted key a should be gone")
	}
	if _, ok := c.get("b", 1); !ok {
		t.Fatal("b should remain")
	}
}

func TestLRUCacheClear(t *testing.T) {
	c := newLRUCache(4, time.Minute)
	c.put("a", 1, 1)
	c.put("b", 2, 1)
	c.clear()
	if m := c.metrics(); m.Size != 0 {
		t.Fatalf("clear should empty cache, size=%d", m.Size)
	}
	if _, ok := c.get("a", 1); ok {
		t.Fatal("a should be gone after clear")
	}
}

func TestNewLRUCacheCapacityClamp(t *testing.T) {
	c := newLRUCache(0, time.Minute)
	if c.capacity != 1 {
		t.Fatalf("capacity below 1 must clamp to 1, got %d", c.capacity)
	}
	c.put("a", 1, 1)
	c.put("b", 2, 1) // over clamped capacity -> a evicted
	if _, ok := c.get("a", 1); ok {
		t.Fatal("expected a evicted at clamped capacity 1")
	}
}
