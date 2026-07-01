package store

import (
	"container/list"
	"sync"
	"time"
)

// CacheMetrics is the exposed read-cache instrumentation (task requirement:
// "exposed metrics"). Handlers / the health surface can surface it for operator
// triage; it is a snapshot, safe to copy.
type CacheMetrics struct {
	Hits      int64 `json:"hits"`
	Misses    int64 `json:"misses"`
	Evictions int64 `json:"evictions"`
	Size      int   `json:"size"`
	Capacity  int   `json:"capacity"`
}

// cacheEntry is one LRU slot. mtime is the newest contributing-file mtime the
// value was built from (UnixNano); a get whose current mtime differs treats the
// entry as stale so the store re-reads disk (mtime-change invalidation).
type cacheEntry struct {
	key      string
	value    any
	mtime    int64
	storedAt time.Time
}

// lruCache is a small mutex-protected LRU with a TTL and mtime-change
// invalidation. It backs the store's read-through path: the expensive unit is a
// single iter-log root's parsed snapshot, keyed by root, so one entry serves
// every session/iteration query for that root and all invalidate together on
// the root's newest mtime.
type lruCache struct {
	mu        sync.Mutex
	capacity  int
	ttl       time.Duration
	ll        *list.List
	items     map[string]*list.Element
	now       func() time.Time
	hits      int64
	misses    int64
	evictions int64
}

// newLRUCache builds an LRU with the given capacity and TTL. A capacity below 1
// is clamped to 1; a non-positive TTL disables TTL expiry (mtime invalidation
// still applies).
func newLRUCache(capacity int, ttl time.Duration) *lruCache {
	if capacity < 1 {
		capacity = 1
	}
	return &lruCache{
		capacity: capacity,
		ttl:      ttl,
		ll:       list.New(),
		items:    make(map[string]*list.Element),
		now:      time.Now,
	}
}

// get returns the cached value for key when it is present, not TTL-expired, and
// built from the same mtime. A stale entry (mtime mismatch or expired) is
// dropped and reported as a miss so the caller re-reads.
func (c *lruCache) get(key string, mtime int64) (any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.items[key]
	if !ok {
		c.misses++
		return nil, false
	}
	ent := el.Value.(*cacheEntry)
	if ent.mtime != mtime || (c.ttl > 0 && c.now().Sub(ent.storedAt) > c.ttl) {
		c.removeElement(el)
		c.misses++
		return nil, false
	}
	c.ll.MoveToFront(el)
	c.hits++
	return ent.value, true
}

// put inserts or refreshes key with value and its source mtime, evicting the
// least-recently-used entry when capacity is exceeded.
func (c *lruCache) put(key string, value any, mtime int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[key]; ok {
		ent := el.Value.(*cacheEntry)
		ent.value = value
		ent.mtime = mtime
		ent.storedAt = c.now()
		c.ll.MoveToFront(el)
		return
	}
	ent := &cacheEntry{key: key, value: value, mtime: mtime, storedAt: c.now()}
	c.items[key] = c.ll.PushFront(ent)
	if c.ll.Len() > c.capacity {
		if back := c.ll.Back(); back != nil {
			c.removeElement(back)
			c.evictions++
		}
	}
}

// evict drops one key if present (the broker's per-root push-eviction hook).
func (c *lruCache) evict(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[key]; ok {
		c.removeElement(el)
	}
}

// clear drops every entry (the broker's whole-cache push-eviction hook).
func (c *lruCache) clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ll.Init()
	c.items = make(map[string]*list.Element)
}

// metrics returns a snapshot of the cache counters and current occupancy.
func (c *lruCache) metrics() CacheMetrics {
	c.mu.Lock()
	defer c.mu.Unlock()
	return CacheMetrics{
		Hits:      c.hits,
		Misses:    c.misses,
		Evictions: c.evictions,
		Size:      c.ll.Len(),
		Capacity:  c.capacity,
	}
}

// removeElement unlinks el and forgets its key. Caller holds c.mu.
func (c *lruCache) removeElement(el *list.Element) {
	c.ll.Remove(el)
	delete(c.items, el.Value.(*cacheEntry).key)
}
