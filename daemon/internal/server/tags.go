package server

import (
	"context"
	"sync"
	"time"

	"github.com/cruisinme30/spelunk/daemon/internal/history"
)

// Tag suggestions for at: run git for-each-ref, so each repo's list is
// kept for a while: completions ask on every keystroke.
const (
	tagsTTL     = 10 * time.Second
	tagsTimeout = 2 * time.Second
	// maxTags is how many of each repo's newest tags are offered.
	maxTags = 50
)

// tagCache keeps each repo's newest tags for tagsTTL.
type tagCache struct {
	now func() time.Time

	mu    sync.Mutex
	repos map[string]cachedTags // by root path
}

type cachedTags struct {
	tags []history.Tag
	read time.Time
}

// list returns the newest tags of the repo at root, read again once
// tagsTTL has passed. A folder outside Git has none.
func (c *tagCache) list(root string) []history.Tag {
	c.mu.Lock()
	defer c.mu.Unlock()
	if cached, ok := c.repos[root]; ok && c.now().Sub(cached.read) < tagsTTL {
		return cached.tags
	}
	ctx, cancel := context.WithTimeout(context.Background(), tagsTimeout)
	defer cancel()
	tags, err := history.Tags(ctx, root, maxTags)
	if err != nil {
		tags = nil
	}
	if c.repos == nil {
		c.repos = map[string]cachedTags{}
	}
	c.repos[root] = cachedTags{tags: tags, read: c.now()}
	return tags
}
