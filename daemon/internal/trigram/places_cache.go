package trigram

import "sync"

// maxPlacesCacheBytes bounds each immutable shard's retained lexical data,
// including conservative map-entry metadata and slice capacities. Each indexed
// Doc additionally carries one fixed cache pointer (4 or 8 bytes). No file text
// or query results are retained. A full cache leaves further reads uncached.
const maxPlacesCacheBytes = 8 << 20

// placesEntryBytes covers an inText, its map key/value and spare map bucket
// capacity conservatively on supported 32- and 64-bit platforms.
const placesEntryBytes = 192

// placesCache belongs to exactly one shard generation. Built, loaded and overlay
// shards each get their own cache, so changed files never reuse old regions.
// The map is created only if a context-filtered query needs it.
type placesCache struct {
	mu     sync.RWMutex
	values map[*Doc]*inText
	used   int
	limit  int
}

// newPlacesCache includes the shared cache object in its budget.
func newPlacesCache() *placesCache {
	return &placesCache{used: 128, limit: maxPlacesCacheBytes}
}

// attachPlaces prepares runtime-only cache ownership before the shard is
// published. The fixed pointer is part of Doc, not the cache retention budget.
func (s *Shard) attachPlaces(doc *Doc) {
	if s.places == nil {
		s.places = newPlacesCache()
	}
	doc.places = s.places
}

// forDoc returns the immutable lexical data of doc, computing it outside the
// lock. Concurrent first reads may compute twice but publish one value. This
// avoids serializing scans of unrelated files or retaining oversized entries.
func (c *placesCache) forDoc(doc *Doc, read func() *inText) *inText {
	c.mu.RLock()
	cached := c.values[doc]
	c.mu.RUnlock()
	if cached != nil {
		return cached
	}
	parsed := read()
	cost := placesEntryBytes + cap(parsed.starts)*8 + cap(parsed.regions)*24
	c.mu.Lock()
	defer c.mu.Unlock()
	if cached := c.values[doc]; cached != nil {
		return cached
	}
	if cost <= c.limit-c.used {
		if c.values == nil {
			c.values = make(map[*Doc]*inText)
		}
		c.values[doc] = parsed
		c.used += cost
	}
	return parsed
}
