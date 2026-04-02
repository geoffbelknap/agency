package main

import (
	"crypto/sha256"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

// CacheEntry represents a cached web fetch response
type CacheEntry struct {
	Content   string     `json:"content"`
	StatusCode int        `json:"status_code"`
	Metadata  Metadata   `json:"metadata"`
	XPIAFlags []string   `json:"xpia_flags,omitempty"`
	FetchedAt time.Time  `json:"fetched_at"`
}

// cacheItem wraps an entry with expiration and cache key
type cacheItem struct {
	entry     *CacheEntry
	expiresAt time.Time
	key       string
}

// Cache is an in-memory LRU cache with TTL support
type Cache struct {
	mu            sync.RWMutex
	items         map[string]*cacheItem // key -> cacheItem
	order         []string              // LRU order, most recent at end
	maxEntries    int
	ttl           time.Duration
	maxEntryBytes int64
	hits          int64
	misses        int64
}

// NewCache creates a new LRU cache
func NewCache(maxEntries int, ttl time.Duration, maxEntryBytes int64) *Cache {
	return &Cache{
		items:         make(map[string]*cacheItem),
		order:         make([]string, 0, maxEntries),
		maxEntries:    maxEntries,
		ttl:           ttl,
		maxEntryBytes: maxEntryBytes,
	}
}

// Get retrieves an entry from cache if it exists and hasn't expired.
// It moves the entry to the end of the LRU order (most recent).
// Expired entries are evicted during this call.
func (c *Cache) Get(rawURL string) (*CacheEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := c.cacheKey(rawURL)
	item, exists := c.items[key]
	if !exists {
		c.misses++
		return nil, false
	}

	// Check if entry has expired
	if time.Now().After(item.expiresAt) {
		c.evictItem(key)
		c.misses++
		return nil, false
	}

	// Move to end of LRU order (most recently used)
	c.moveToEnd(key)
	c.hits++
	return item.entry, true
}

// Set stores an entry in the cache. If the entry exceeds maxEntryBytes, it is not cached.
// If the cache is at capacity, the oldest (least recently used) entry is evicted.
func (c *Cache) Set(rawURL string, entry *CacheEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Skip if entry is too large
	entryBytes := int64(len(entry.Content))
	if entryBytes > c.maxEntryBytes {
		return
	}

	key := c.cacheKey(rawURL)

	// Remove existing entry if present
	if _, exists := c.items[key]; exists {
		c.evictItem(key)
	}

	// If at capacity, evict oldest entry
	if len(c.items) >= c.maxEntries {
		if len(c.order) > 0 {
			oldest := c.order[0]
			c.evictItem(oldest)
		}
	}

	// Add new entry
	item := &cacheItem{
		entry:     entry,
		expiresAt: time.Now().Add(c.ttl),
		key:       key,
	}
	c.items[key] = item
	c.order = append(c.order, key)
}

// Stats returns the current number of entries and hit rate
func (c *Cache) Stats() (entries int, hitRate float64) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entries = len(c.items)
	total := c.hits + c.misses
	if total == 0 {
		hitRate = 0
	} else {
		hitRate = float64(c.hits) / float64(total)
	}
	return entries, hitRate
}

// normalizeURL normalizes a URL for consistent cache key generation
func (c *Cache) normalizeURL(rawURL string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}

	// Lowercase scheme and host
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)

	// Strip fragment
	parsed.Fragment = ""

	// Sort query parameters and strip tracking params
	params := parsed.Query()
	trackingParams := []string{
		"utm_source", "utm_medium", "utm_campaign", "utm_term", "utm_content", "utm_id",
		"fbclid", "gclid", "gclsrc", "msclkid", "twclid",
	}
	for _, param := range trackingParams {
		params.Del(param)
	}

	// Sort params by key for consistent ordering
	keys := make([]string, 0, len(params))
	for key := range params {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	// Rebuild query string with sorted params
	var queryBuilder strings.Builder
	first := true
	for _, key := range keys {
		if !first {
			queryBuilder.WriteString("&")
		}
		queryBuilder.WriteString(url.QueryEscape(key))
		queryBuilder.WriteString("=")
		values := params[key]
		for i, val := range values {
			if i > 0 {
				queryBuilder.WriteString("&")
				queryBuilder.WriteString(url.QueryEscape(key))
				queryBuilder.WriteString("=")
			}
			queryBuilder.WriteString(url.QueryEscape(val))
		}
		first = false
	}

	parsed.RawQuery = queryBuilder.String()
	return parsed.String(), nil
}

// cacheKey generates a SHA-256 cache key from a normalized URL
func (c *Cache) cacheKey(rawURL string) string {
	normalized, err := c.normalizeURL(rawURL)
	if err != nil {
		// Fallback to SHA-256 of raw URL if normalization fails
		normalized = rawURL
	}
	hash := sha256.Sum256([]byte(normalized))
	return fmt.Sprintf("%x", hash)
}

// evictItem removes an item from the cache (must be called with lock held)
func (c *Cache) evictItem(key string) {
	delete(c.items, key)
	// Remove from order slice
	for i, k := range c.order {
		if k == key {
			c.order = append(c.order[:i], c.order[i+1:]...)
			break
		}
	}
}

// moveToEnd moves an entry to the end of the LRU order (must be called with lock held)
func (c *Cache) moveToEnd(key string) {
	for i, k := range c.order {
		if k == key {
			c.order = append(append(c.order[:i], c.order[i+1:]...), key)
			break
		}
	}
}
