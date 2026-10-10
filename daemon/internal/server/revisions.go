package server

// at: searches: each open repo's files at the revision the query names,
// read from Git on demand into a trigram shard that is kept for the next
// search (see revisionCache) but never saved.

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/cruisinme30/spelunk/daemon/internal/history"
	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
	"github.com/cruisinme30/spelunk/daemon/internal/query"
	"github.com/cruisinme30/spelunk/daemon/internal/trigram"
)

const (
	// keptRevisions is how many revision shards stay in memory: typing a
	// query searches the same revision again and again, and someone may
	// go back and forth between two.
	keptRevisions = 2
	// revisionReadTimeout bounds reading one revision's files. The read
	// isn't tied to the search that started it, which the next keystroke
	// cancels, so the next search finds it done.
	revisionReadTimeout = 2 * time.Minute
)

// revisionRepos resolves the plan's revision in each open repo and returns
// the repos at it, to search, and what each repo read, for the panel. A
// repo without that revision, or outside Git, is left out, saying why.
func (s *Server) revisionRepos(ctx context.Context, plan *query.Plan) ([]trigram.Repo, []protocol.RevisionInfo, error) {
	settings := s.Settings()
	var repos []trigram.Repo
	var infos []protocol.RevisionInfo
	for _, current := range s.index.Repos() {
		if plan.ExcludesRepo(current.Name) {
			continue
		}
		info := protocol.RevisionInfo{RepoID: current.ID}
		revision, err := resolveRevision(ctx, current.Root, plan)
		if err != nil {
			if ctx.Err() != nil {
				return nil, nil, context.Cause(ctx)
			}
			info.Problem = revisionProblem(err, plan, current.Name)
			infos = append(infos, info)
			continue
		}
		shard, err := s.revisions.shard(ctx, current.Root, revision, settings)
		if err != nil {
			if ctx.Err() != nil {
				return nil, nil, context.Cause(ctx)
			}
			info.Problem = fmt.Sprintf("Couldn't read %s at %s: %v", current.Name, short(revision.SHA), err)
			infos = append(infos, info)
			continue
		}
		info.SHA, info.Date = revision.SHA, revision.At.UTC().Format(time.RFC3339)
		infos = append(infos, info)
		repos = append(repos, trigram.Repo{
			// No Owners: owner: reads today's CODEOWNERS file, which can't
			// speak for the revision's files.
			ID: current.ID, Name: current.Name, Root: current.Root, Shard: shard,
			At: &trigram.AtRevision{SHA: revision.SHA, Current: current.CurrentFiles()},
		})
	}
	return repos, infos, nil
}

// resolveRevision finds the commit the plan's at: names in the repo at root.
func resolveRevision(ctx context.Context, root string, plan *query.Plan) (history.Revision, error) {
	if !plan.AtTime.IsZero() {
		return history.RevisionBefore(ctx, root, plan.AtTime)
	}
	return history.ResolveRevision(ctx, root, plan.At)
}

// revisionProblem says why a repo has no revision for the plan's at:.
func revisionProblem(err error, plan *query.Plan, repo string) string {
	switch {
	case errors.Is(err, history.ErrNotGit):
		return repo + " isn't a Git repository, so it has no past revisions"
	case errors.Is(err, exec.ErrNotFound):
		return "Git isn't installed, so " + repo + " can't be searched at a revision"
	case !errors.Is(err, history.ErrNoRevision):
		return fmt.Sprintf("Couldn't find %s in %s: %v", plan.At, repo, err)
	case !plan.AtTime.IsZero():
		return fmt.Sprintf("%s has no commit from before %s", repo, plan.AtTime.Format("Mon, Jan 2, 2006 15:04"))
	default:
		return fmt.Sprintf("%s isn't a tag, branch or commit in %s", plan.At, repo)
	}
}

// short is a sha as people read it: its first 7 digits.
func short(sha string) string { return sha[:min(len(sha), 7)] }

// revisionKey names a revision shard: the repo, the commit, and the
// settings that decide which files it holds and what it keeps of them.
type revisionKey struct {
	root, sha string
	symbols   bool
	exclude   string // the index.exclude patterns, joined
	maxBytes  int64
}

// revisionCache keeps the last keptRevisions revision shards. Each is read
// once: a search that asks for one being read waits for that read.
type revisionCache struct {
	mu      sync.Mutex
	entries []*revisionEntry // most recently used first
}

type revisionEntry struct {
	key   revisionKey
	ready chan struct{} // closed once shard or err is set
	shard *trigram.Shard
	err   error
}

// shard returns the shard of revision's files in the repo at root, as the
// settings choose them: read now, or kept from an earlier search. It
// waits for a read another search started; ctx only stops the wait.
func (c *revisionCache) shard(ctx context.Context, root string, revision history.Revision, settings protocol.Settings) (*trigram.Shard, error) {
	key := revisionKey{
		root: root, sha: revision.SHA, symbols: settings.Symbols,
		exclude: strings.Join(settings.Exclude, "\x00"), maxBytes: int64(settings.MaxFileSizeKB) * 1024,
	}
	// The read outlives ctx, which the next keystroke's search cancels.
	detached := context.WithoutCancel(ctx)
	entry := c.entry(key, func(entry *revisionEntry) {
		readCtx, cancel := context.WithTimeout(detached, revisionReadTimeout)
		defer cancel()
		entry.shard, entry.err = readRevision(readCtx, root, revision, settings)
	})
	select {
	case <-entry.ready:
		return entry.shard, entry.err
	case <-ctx.Done():
		return nil, context.Cause(ctx)
	}
}

// entry returns the cache's entry for key, moved to the front, starting
// read in a new goroutine for a key it doesn't have. A read that failed
// is forgotten, so the next search tries again.
func (c *revisionCache) entry(key revisionKey, read func(*revisionEntry)) *revisionEntry {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, entry := range c.entries {
		if entry.key == key {
			copy(c.entries[1:i+1], c.entries[:i])
			c.entries[0] = entry
			return entry
		}
	}
	entry := &revisionEntry{key: key, ready: make(chan struct{})}
	c.entries = append([]*revisionEntry{entry}, c.entries...)
	if len(c.entries) > keptRevisions {
		c.entries = c.entries[:keptRevisions]
	}
	go func() {
		read(entry)
		if entry.err != nil {
			c.forget(entry)
		}
		close(entry.ready)
	}()
	return entry
}

// forget drops entry from the cache.
func (c *revisionCache) forget(entry *revisionEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, e := range c.entries {
		if e == entry {
			c.entries = append(c.entries[:i], c.entries[i+1:]...)
			return
		}
	}
}

// readRevision reads the files of revision under root that the index
// would hold, as the settings choose them (index.exclude and
// index.maxFileSizeKB), and indexes them. Binary files are left out, as
// the index leaves them out.
func readRevision(ctx context.Context, root string, revision history.Revision, settings protocol.Settings) (*trigram.Shard, error) {
	files, err := history.ListTree(ctx, root, revision.SHA)
	if err != nil {
		return nil, err
	}
	exclude := trigram.NewExcluder(settings.Exclude)
	maxBytes := int64(settings.MaxFileSizeKB) * 1024
	kept := files[:0]
	for _, file := range files {
		if !exclude.Excludes(file.Path) && (maxBytes <= 0 || file.Size <= maxBytes) {
			kept = append(kept, file)
		}
	}
	opts := trigram.BuildOptions{Symbols: settings.Symbols}
	var docs []trigram.Doc
	err = history.ReadBlobs(ctx, root, kept, func(file history.TreeFile, content []byte) error {
		if doc, ok := trigram.DocFromBlob(file.Path, content, revision.At, opts); ok {
			docs = append(docs, doc)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return trigram.NewShard(docs), nil
}

// readAtRevision reads and decodes the file a ref at a revision names, as
// the revision's shard holds it. A file the revision doesn't have, or
// that is binary, is stale.
func readAtRevision(ctx context.Context, root string, ref trigram.Ref) ([]byte, error) {
	raw, err := history.ReadFileAt(ctx, root, ref.Revision, ref.Path)
	if err != nil {
		return nil, err
	}
	doc, ok := trigram.DocFromBlob(ref.Path, raw, time.Time{}, trigram.BuildOptions{})
	if !ok {
		return nil, history.ErrStale
	}
	return doc.Content, nil
}
