package highlight

import (
	"container/list"
	"sync"
)

// lineCacheCap bounds the highlight LRU. Content-keyed entries are small
// (one styled line each); a few thousand covers several screens of every
// open file.
const lineCacheCap = 4096

type lineKey struct {
	lexer string // chroma config name, so identical lines share across files
	line  string
}

type lineEntry struct {
	key lineKey
	out string
}

// lineCache is a mutex-guarded LRU of highlighted lines keyed by
// (lexer, content). Content-keyed means edits invalidate naturally.
type lineCache struct {
	mu      sync.Mutex
	cap     int
	order   *list.List // front = most recently used
	entries map[lineKey]*list.Element
}

func newLineCache(capacity int) *lineCache {
	if capacity < 1 {
		capacity = 1
	}
	return &lineCache{
		cap:     capacity,
		order:   list.New(),
		entries: make(map[lineKey]*list.Element, capacity),
	}
}

func (c *lineCache) get(key lineKey) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.entries[key]
	if !ok {
		return "", false
	}
	c.order.MoveToFront(el)
	return el.Value.(*lineEntry).out, true
}

func (c *lineCache) put(key lineKey, out string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.entries[key]; ok {
		c.order.MoveToFront(el)
		el.Value.(*lineEntry).out = out
		return
	}
	c.entries[key] = c.order.PushFront(&lineEntry{key: key, out: out})
	for c.order.Len() > c.cap {
		oldest := c.order.Back()
		c.order.Remove(oldest)
		delete(c.entries, oldest.Value.(*lineEntry).key)
	}
}

func (c *lineCache) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.order.Len()
}
