package server

import (
	"strings"
	"sync"

	"github.com/cruisinme30/spelunk/daemon/internal/codeowners"
	"github.com/cruisinme30/spelunk/daemon/internal/trigram"
)

// ownership is what a repo's CODEOWNERS file says of its indexed files.
type ownership struct {
	// owners count each owner's files, in the order owners first appear,
	// one owner however the file spells its case.
	owners []ownerFiles
	// unowned counts the files no rule gives an owner.
	unowned int
}

// ownerFiles is one owner and how many files it has.
type ownerFiles struct {
	name  string
	files int
}

// ownershipCache keeps each repo's ownership for the index and CODEOWNERS
// file it was counted from. Counting matches every file against the
// rules, and parsing and completions ask for it on every keystroke.
type ownershipCache struct {
	mu     sync.Mutex
	byRepo map[string]countedOwnership
}

type countedOwnership struct {
	shard  *trigram.Shard
	owners *codeowners.File
	ownership
}

// of returns repo's ownership, counting it again only when its index or
// its CODEOWNERS file changed. A repo without CODEOWNERS has none.
func (c *ownershipCache) of(repo *trigram.Repo) ownership {
	if repo.Owners == nil {
		return ownership{}
	}
	c.mu.Lock()
	kept, ok := c.byRepo[repo.ID]
	c.mu.Unlock()
	if ok && kept.shard == repo.Shard && kept.owners == repo.Owners {
		return kept.ownership
	}
	counted := countOwnership(repo.Shard, repo.Owners)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.byRepo == nil {
		c.byRepo = map[string]countedOwnership{}
	}
	c.byRepo[repo.ID] = countedOwnership{shard: repo.Shard, owners: repo.Owners, ownership: counted}
	return counted
}

// countOwnership counts the owners of shard's files by owners' rules.
func countOwnership(shard *trigram.Shard, owners *codeowners.File) ownership {
	var counted ownership
	index := map[string]int{} // lowercased name → index in counted.owners
	for i := range shard.Docs {
		names := owners.Owners(shard.Docs[i].Path)
		if len(names) == 0 {
			counted.unowned++
		}
		for _, name := range names {
			key := strings.ToLower(name)
			at, ok := index[key]
			if !ok {
				at = len(counted.owners)
				index[key] = at
				counted.owners = append(counted.owners, ownerFiles{name: name})
			}
			counted.owners[at].files++
		}
	}
	return counted
}
