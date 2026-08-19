package generated

import (
	"regexp"
	"sync"
)

// regexCache memoises compiled glob patterns.
//
// The pattern set is fixed at build time (pathRules is a package var, not
// config — see the package doc for why that distinction matters here), so this
// fills once on first use and is read-only afterwards. It is still guarded:
// FromPath is called from the extraction workers, which run concurrently.
type regexCache struct {
	mu sync.RWMutex
	m  map[string]*regexp.Regexp
}

func newRegexCache() *regexCache {
	return &regexCache{m: make(map[string]*regexp.Regexp, len(pathRules))}
}

func (c *regexCache) get(k string) (*regexp.Regexp, bool) {
	c.mu.RLock()
	re, ok := c.m[k]
	c.mu.RUnlock()
	return re, ok
}

func (c *regexCache) put(k string, re *regexp.Regexp) {
	c.mu.Lock()
	c.m[k] = re
	c.mu.Unlock()
}
