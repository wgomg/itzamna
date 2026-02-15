package utils

import (
	"maps"
	"slices"
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
	id         int
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

func NewCacheItem(id int, value string) CacheItem {
	return CacheItem{id: id, value: value, hits: 0, lastAccess: time.Now()}
}

func (c *CacheItem) GetId() int {
	return c.id
}

func (c *TagsCache) GetCachedTags() map[string]CacheItem {
	return c.items
}

func (c *TagsCache) AddNewTags(items []CacheItem) {
	c.mu.Lock()
	defer c.mu.Unlock()

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

func (c *TagsCache) HitRate() float64 {
	if c.hits+c.misses > 0 {
		return float64(c.hits) / float64(c.hits+c.misses)
	} else {
		return 0.0
	}
}
