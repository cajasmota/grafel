package dashboard

import (
	"strings"
	"sync"
)

type repositoryTopologyCacheEntry struct {
	sourceVersion string
	index         repositoryTopologyIndex
}

type repositoryTopologyCache struct {
	mu      sync.RWMutex
	entries map[string]repositoryTopologyCacheEntry
}

func newRepositoryTopologyCache() *repositoryTopologyCache {
	return &repositoryTopologyCache{entries: map[string]repositoryTopologyCacheEntry{}}
}

func (c *repositoryTopologyCache) GetOrBuild(group, ref string, grp *DashGroup) repositoryTopologyIndex {
	if c == nil {
		return buildRepositoryTopologyIndex(grp)
	}
	key := group
	if ref != "" {
		key += "@" + ref
	}
	c.mu.RLock()
	entry, ok := c.entries[key]
	c.mu.RUnlock()
	if ok && entry.sourceVersion == grp.sourceVersion {
		return entry.index
	}
	index := buildRepositoryTopologyIndex(grp)
	c.mu.Lock()
	c.entries[key] = repositoryTopologyCacheEntry{sourceVersion: grp.sourceVersion, index: index}
	c.mu.Unlock()
	return index
}

func (c *repositoryTopologyCache) InvalidateGroup(group string) {
	if c == nil {
		return
	}
	prefix := group + "@"
	c.mu.Lock()
	delete(c.entries, group)
	for key := range c.entries {
		if strings.HasPrefix(key, prefix) {
			delete(c.entries, key)
		}
	}
	c.mu.Unlock()
}

func (c *repositoryTopologyCache) InvalidateAll() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.entries = map[string]repositoryTopologyCacheEntry{}
	c.mu.Unlock()
}
