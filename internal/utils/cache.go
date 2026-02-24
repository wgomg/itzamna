package utils

import (
	"maps"
	"slices"
	"sync"
	"time"
)

type TagsCache struct {
	mu          sync.RWMutex
	items       map[string]CacheItem
	reads       int
	updates     int
	startupTime time.Time
}

type CacheItem struct {
	id         int
	value      string
	hits       int
	lastAccess time.Time
}

func NewTagsCache() *TagsCache {
	return &TagsCache{
		items:       make(map[string]CacheItem),
		startupTime: time.Now(),
	}
}

func NewCacheItem(id int, value string) CacheItem {
	return CacheItem{id: id, value: value, hits: 0, lastAccess: time.Now()}
}

func (c *CacheItem) GetId() int {
	return c.id
}

func (c *TagsCache) GetCachedTags() map[string]CacheItem {
	c.mu.RLock()
	defer c.mu.RUnlock()
	c.reads++
	return c.items
}

func (c *TagsCache) AddNewTags(items []CacheItem) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(items) > 0 {
		c.updates++
	}

	for _, item := range items {
		c.items[item.value] = item
	}
}

func (c *TagsCache) GetCachedTagsValues() []string {
	return slices.Collect(maps.Keys(c.items))
}

func (c *TagsCache) Size() int {
	return len(c.items)
}

func (c *TagsCache) Stats() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return map[string]interface{}{
		"size":                 len(c.items),
		"total_reads":          c.reads,
		"total_updates":        c.updates,
		"uptime_seconds":       time.Since(c.startupTime).Seconds(),
		"avg_reads_per_second": float64(c.reads) / time.Since(c.startupTime).Seconds(),
	}
}
