package main

import (
	"testing"
	"time"
)

func TestCache_SetGet(t *testing.T) {
	cache := NewCache(10, 5*time.Second, 1000000)

	entry := &CacheEntry{
		Content:    "test content",
		StatusCode: 200,
		Metadata: Metadata{
			Title:       "Test Page",
			Description: "A test page",
		},
		XPIAFlags: []string{},
		FetchedAt: time.Now(),
	}

	url := "https://example.com/test"
	cache.Set(url, entry)

	retrieved, found := cache.Get(url)
	if !found {
		t.Fatal("expected entry to be found in cache")
	}

	if retrieved.Content != entry.Content {
		t.Errorf("expected content %q, got %q", entry.Content, retrieved.Content)
	}

	if retrieved.StatusCode != entry.StatusCode {
		t.Errorf("expected status code %d, got %d", entry.StatusCode, retrieved.StatusCode)
	}
}

func TestCache_TTLExpiry(t *testing.T) {
	cache := NewCache(10, 50*time.Millisecond, 1000000)

	entry := &CacheEntry{
		Content:    "test content",
		StatusCode: 200,
		Metadata:   Metadata{},
		XPIAFlags:  []string{},
		FetchedAt:  time.Now(),
	}

	url := "https://example.com/test"
	cache.Set(url, entry)

	// Entry should be retrievable immediately
	_, found := cache.Get(url)
	if !found {
		t.Fatal("expected entry to be found immediately after set")
	}

	// Wait for expiration
	time.Sleep(60 * time.Millisecond)

	// Entry should be expired and evicted
	_, found = cache.Get(url)
	if found {
		t.Fatal("expected entry to be expired after TTL")
	}
}

func TestCache_MaxEntries(t *testing.T) {
	cache := NewCache(3, 5*time.Second, 1000000)

	// Add entries up to max capacity
	for i := 1; i <= 3; i++ {
		entry := &CacheEntry{
			Content:    "content " + string(rune(i)),
			StatusCode: 200,
			Metadata:   Metadata{},
			XPIAFlags:  []string{},
			FetchedAt:  time.Now(),
		}
		url := "https://example.com/test" + string(rune(48+i))
		cache.Set(url, entry)
	}

	entries, _ := cache.Stats()
	if entries != 3 {
		t.Errorf("expected 3 entries, got %d", entries)
	}

	// Add one more to trigger eviction of oldest (first entry)
	entry := &CacheEntry{
		Content:    "content 4",
		StatusCode: 200,
		Metadata:   Metadata{},
		XPIAFlags:  []string{},
		FetchedAt:  time.Now(),
	}
	url := "https://example.com/test4"
	cache.Set(url, entry)

	entries, _ = cache.Stats()
	if entries != 3 {
		t.Errorf("expected 3 entries after eviction, got %d", entries)
	}

	// First entry should be evicted
	_, found := cache.Get("https://example.com/test1")
	if found {
		t.Fatal("expected first entry to be evicted")
	}

	// New entry should be present
	_, found = cache.Get(url)
	if !found {
		t.Fatal("expected new entry to be in cache")
	}
}

func TestCache_MaxEntrySize(t *testing.T) {
	cache := NewCache(10, 5*time.Second, 50) // max 50 bytes per entry

	// Entry larger than max should not be cached
	longContent := "this is a very long piece of content that definitely exceeds the maximum allowed entry size for the cache"
	entry := &CacheEntry{
		Content:    longContent,
		StatusCode: 200,
		Metadata:   Metadata{},
		XPIAFlags:  []string{},
		FetchedAt:  time.Now(),
	}

	url := "https://example.com/large"
	cache.Set(url, entry)

	// Entry should not be in cache
	_, found := cache.Get(url)
	if found {
		t.Fatal("expected oversized entry to not be cached")
	}

	entries, _ := cache.Stats()
	if entries != 0 {
		t.Errorf("expected 0 entries, got %d", entries)
	}
}

func TestCache_NormalizeURL(t *testing.T) {
	cache := NewCache(10, 5*time.Second, 1000000)

	entry := &CacheEntry{
		Content:    "test content",
		StatusCode: 200,
		Metadata:   Metadata{},
		XPIAFlags:  []string{},
		FetchedAt:  time.Now(),
	}

	// Set with one URL variant
	url1 := "https://example.com/page?param1=value1&param2=value2"
	cache.Set(url1, entry)

	// Get with different param order (should normalize and hit cache)
	url2 := "https://example.com/page?param2=value2&param1=value1"
	retrieved, found := cache.Get(url2)
	if !found {
		t.Fatal("expected cache hit for URL with reordered params")
	}

	if retrieved.Content != entry.Content {
		t.Errorf("expected content %q, got %q", entry.Content, retrieved.Content)
	}

	// Get with fragment (should be stripped and normalize to cache hit)
	url3 := "https://example.com/page?param1=value1&param2=value2#section"
	retrieved, found = cache.Get(url3)
	if !found {
		t.Fatal("expected cache hit for URL with fragment stripped")
	}

	// Get with tracking params (should be stripped)
	url4 := "https://example.com/page?param1=value1&param2=value2&utm_source=google&utm_medium=cpc"
	retrieved, found = cache.Get(url4)
	if !found {
		t.Fatal("expected cache hit for URL with tracking params stripped")
	}
}

func TestCache_Stats(t *testing.T) {
	cache := NewCache(10, 5*time.Second, 1000000)

	entry := &CacheEntry{
		Content:    "test content",
		StatusCode: 200,
		Metadata:   Metadata{},
		XPIAFlags:  []string{},
		FetchedAt:  time.Now(),
	}

	url := "https://example.com/test"
	cache.Set(url, entry)

	entries, hitRate := cache.Stats()
	if entries != 1 {
		t.Errorf("expected 1 entry, got %d", entries)
	}

	// Hit rate should be 0 (no gets yet)
	if hitRate != 0 {
		t.Errorf("expected hit rate 0, got %f", hitRate)
	}

	// Hit the cache
	cache.Get(url)
	_, hitRate = cache.Stats()
	if hitRate != 1.0 {
		t.Errorf("expected hit rate 1.0 after 1 hit, got %f", hitRate)
	}

	// Miss the cache
	cache.Get("https://example.com/nonexistent")
	_, hitRate = cache.Stats()
	expectedRate := 0.5 // 1 hit, 1 miss
	if hitRate != expectedRate {
		t.Errorf("expected hit rate %f, got %f", expectedRate, hitRate)
	}
}
