package utils

import (
	"sync"
	"time"
)

type TagsCache struct {
	mu     sync.RWMutex
	items  map[string]CacheItem
	hits   int
	misses int
}

type CacheItem struct {
	value      string
	hits       int
	lastAccess time.Time
}

func NewTagsCache() *TagsCache {
	return &TagsCache{
		items:  make(map[string]CacheItem),
		hits:   0,
		misses: 0,
	}
}

func (c *TagsCache) GetMissingAndAdd(keys []string) []string {
	c.mu.Lock()
	defer c.mu.Unlock()

	missing := []string{}
	for _, key := range keys {
		if _, exists := c.items[key]; !exists {
			missing = append(missing, key)
			c.misses += 1
			c.items[key] = CacheItem{
				value:      key,
				lastAccess: time.Now(),
				hits:       0,
			}
		} else {
			c.hits += 1
			item := c.items[key]
			item.hits += 1
			item.lastAccess = time.Now()
			c.items[key] = item
		}
	}
	return missing
}

func (c *TagsCache) Size() int {
	return len(c.items)
}

func (c *TagsCache) HitRate() float64 {
	if c.hits+c.misses > 0 {
		return float64(c.hits) / float64(c.hits+c.misses)
	} else {
		return 0.0
	}
}
