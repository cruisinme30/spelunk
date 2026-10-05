package indexer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/cruisinme30/spelunk/daemon/internal/history"
	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
	"github.com/cruisinme30/spelunk/daemon/internal/trigram"
)

// Indexer builds and publishes the working-tree index and the history index
// of every root, and keeps them fresh. Its methods are safe for concurrent
// use.
type Indexer struct {
	notify func([]protocol.RepoStatus)

	mu       sync.Mutex
	settings protocol.Settings
	order    []string         // root IDs, in workspace order
	repos    map[string]*repo // by root ID
	// The working-tree builds and the history reads each have a queue and
	// a worker, so a long history read never delays a tree build.
	treeQueue    queue
	historyQueue queue

	stop    context.CancelFunc
	workers sync.WaitGroup

	// beforeBuild, when tests set it, runs with the root's ID at the start of
	// each working-tree build, which waits for it to return.
	beforeBuild func(id string)
}

// repo is one root's index state.
type repo struct {
	root protocol.Root

	// The working-tree index.
	shard *trigram.Shard // published; nil until the first build or load
	state protocol.IndexState
	// progress and message are reported in RepoStatus.
	progress float64
	message  string
	// generation changes whenever what a build would produce changes
	// (settings, a rebuild request), so an outdated build is discarded.
	generation  int
	cancelBuild context.CancelFunc
	// visible makes a build show as "indexing"; a background refresh of a
	// repo that already has a shard stays "ready".
	visible bool
	// The files re-read since the shard was built (see FilesChanged).
	overlay overlay

	// The history index.
	history           *history.Store // published; nil until read or loaded
	historyState      protocol.IndexState
	historyProgress   float64
	historyMessage    string
	historyGeneration int
	cancelHistory     context.CancelFunc
	// historyFull makes the next history job read everything again instead
	// of only what changed.
	historyFull bool
}

// queue is a list of root IDs waiting for a worker, and a signal for it.
type queue struct {
	ids  []string
	wake chan struct{}
}

// add queues id once and wakes the worker. Callers hold Indexer.mu.
func (q *queue) add(id string) {
	if !slices.Contains(q.ids, id) {
		q.ids = append(q.ids, id)
	}
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

// remove takes id out of the queue. Callers hold Indexer.mu.
func (q *queue) remove(id string) {
	q.ids = slices.DeleteFunc(q.ids, func(queued string) bool { return queued == id })
}

// New starts an indexer. notify receives every status change; it is called
// without locks held and must not block for long.
func New(settings protocol.Settings, notify func([]protocol.RepoStatus)) *Indexer {
	ctx, stop := context.WithCancel(context.Background())
	ix := &Indexer{
		notify: notify, settings: settings, repos: map[string]*repo{}, stop: stop,
		treeQueue:    queue{wake: make(chan struct{}, 1)},
		historyQueue: queue{wake: make(chan struct{}, 1)},
	}
	ix.workers.Add(2)
	go ix.treeWork(ctx)
	go ix.historyWork(ctx)
	return ix
}

// SetRoots starts indexing new roots and drops removed ones. A root's saved
// indexes, if any, serve searches at once while they are refreshed.
func (ix *Indexer) SetRoots(roots []protocol.Root) {
	saved := ix.savedIndexes(roots)
	ix.mu.Lock()
	keep := map[string]bool{}
	ix.order = ix.order[:0]
	for _, root := range roots {
		keep[root.ID] = true
		ix.order = append(ix.order, root.ID)
		if r, ok := ix.repos[root.ID]; ok && r.root.Path == root.Path {
			r.root.Name = root.Name
			continue
		}
		ix.drop(root.ID)
		ix.add(root, saved[root.ID])
	}
	for id := range ix.repos {
		if !keep[id] {
			ix.drop(id)
		}
	}
	ix.mu.Unlock()
	ix.publishStatus()
}

// add starts tracking a root and queues its builds. Callers hold mu.
func (ix *Indexer) add(root protocol.Root, saved savedIndex) {
	r := &repo{
		root: root, state: protocol.IndexStateQueued, visible: true,
		historyState: protocol.IndexStateQueued, historyFull: true, message: saved.problem,
	}
	ix.repos[root.ID] = r
	if saved.shard != nil {
		r.shard, r.state, r.visible = saved.shard, protocol.IndexStateReady, false // refresh quietly
	}
	if saved.history != nil {
		r.history, r.historyState, r.historyFull = saved.history, protocol.IndexStateReady, false
	}
	ix.treeQueue.add(root.ID)
	ix.historyQueue.add(root.ID)
}

// savedIndex is what a previous session saved for a root.
type savedIndex struct {
	shard   *trigram.Shard
	history *history.Store
	// problem says why a saved index couldn't be used, when it exists but
	// is unreadable: it is rebuilt.
	problem string
}

// savedIndexes loads the saved indexes of every root the indexer doesn't
// have yet. Decoding happens without the lock: a large repo's index takes a
// while to read, and searches must not wait for it.
func (ix *Indexer) savedIndexes(roots []protocol.Root) map[string]savedIndex {
	ix.mu.Lock()
	var fresh []protocol.Root
	for _, root := range roots {
		if r, ok := ix.repos[root.ID]; !ok || r.root.Path != root.Path {
			fresh = append(fresh, root)
		}
	}
	location := ix.settings.Location
	ix.mu.Unlock()
	saved := map[string]savedIndex{}
	for _, root := range fresh {
		var index savedIndex
		shard, err := trigram.Load(shardPath(location, root))
		switch {
		case err == nil:
			index.shard = shard
		case !errors.Is(err, os.ErrNotExist) && !errors.Is(err, trigram.ErrStaleFormat):
			index.problem = "The saved index couldn't be read, so it is being rebuilt."
		}
		if store, err := history.Load(historyPath(location, root)); err == nil {
			index.history = store
		}
		saved[root.ID] = index
	}
	return saved
}

// SetSettings applies new settings, rebuilding every repo's working-tree
// index when a setting that decides what is indexed changed, and reading
// every history again when the history window changed.
func (ix *Indexer) SetSettings(settings protocol.Settings) {
	ix.mu.Lock()
	old := ix.settings
	ix.settings = settings
	treeChanged := !slices.Equal(old.Exclude, settings.Exclude) || old.IncludeIgnored != settings.IncludeIgnored ||
		old.MaxFileSizeKB != settings.MaxFileSizeKB || old.Location != settings.Location || old.Symbols != settings.Symbols
	historyChanged := old.HistoryDepth != settings.HistoryDepth || old.Location != settings.Location ||
		!slices.Equal(old.Exclude, settings.Exclude)
	for _, id := range ix.order {
		if treeChanged {
			ix.restart(id)
		}
		if historyChanged {
			ix.restartHistory(id)
		}
	}
	ix.mu.Unlock()
	if treeChanged || historyChanged {
		ix.publishStatus()
	}
}

// Rebuild rebuilds both indexes of one repo, or of every repo when repoID
// is "". The old indexes keep serving searches until the new ones are ready.
func (ix *Indexer) Rebuild(repoID string) error {
	ix.mu.Lock()
	if repoID != "" && ix.repos[repoID] == nil {
		ix.mu.Unlock()
		return fmt.Errorf("no open repo %q", repoID)
	}
	for _, id := range ix.order {
		if repoID == "" || id == repoID {
			ix.restart(id)
			ix.restartHistory(id)
		}
	}
	ix.mu.Unlock()
	ix.publishStatus()
	return nil
}

// Repos returns the published working-tree indexes, in workspace order.
// Repos without an index yet are left out: searches return what is ready.
func (ix *Indexer) Repos() []trigram.Repo {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	var repos []trigram.Repo
	for _, id := range ix.order {
		r := ix.repos[id]
		if r.shard == nil {
			continue
		}
		repo := trigram.Repo{
			ID: id, Name: r.root.Name, Root: r.root.Path, Shard: r.shard,
			Overlay: r.overlay.shard, Masked: r.overlay.masked,
		}
		if r.history != nil {
			repo.History = fileHistory{r.history}
		}
		repos = append(repos, repo)
	}
	return repos
}

// HistoryRepos returns the published history indexes, in workspace order,
// including ones still being read (they hold the newest commits so far).
func (ix *Indexer) HistoryRepos() []history.Repo {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	var repos []history.Repo
	for _, id := range ix.order {
		if r := ix.repos[id]; r.history != nil {
			repos = append(repos, history.Repo{ID: id, Name: r.root.Name, Root: r.root.Path, Store: r.history})
		}
	}
	return repos
}

// Status returns every repo's state, in workspace order. Progress is the
// working tree's while it is indexing, otherwise the history's.
func (ix *Indexer) Status() []protocol.RepoStatus {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	statuses := make([]protocol.RepoStatus, 0, len(ix.order))
	for _, id := range ix.order {
		r := ix.repos[id]
		status := protocol.RepoStatus{RepoID: id, Name: r.root.Name, Tree: r.state, History: r.historyState}
		switch {
		case r.state == protocol.IndexStateIndexing:
			status.Progress = r.progress
		case r.historyState == protocol.IndexStateIndexing:
			status.Progress = r.historyProgress
		}
		status.Message = r.message
		if status.Message == "" {
			status.Message = r.historyMessage
		}
		statuses = append(statuses, status)
	}
	return statuses
}

// Close stops the workers, abandoning any build, and waits up to timeout.
func (ix *Indexer) Close(timeout time.Duration) {
	ix.stop()
	done := make(chan struct{})
	go func() {
		ix.workers.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(timeout):
	}
}

// drop forgets a repo and abandons its work. Callers hold mu.
func (ix *Indexer) drop(id string) {
	if r, ok := ix.repos[id]; ok {
		if r.cancelBuild != nil {
			r.cancelBuild()
		}
		if r.cancelHistory != nil {
			r.cancelHistory()
		}
		delete(ix.repos, id)
	}
	ix.treeQueue.remove(id)
	ix.historyQueue.remove(id)
}

// publishStatus sends every repo's status to notify. Callers must not
// hold mu.
func (ix *Indexer) publishStatus() {
	if ix.notify != nil {
		ix.notify(ix.Status())
	}
}

// indexFile names a root's index file: one per root path under
// index.location, in a folder per kind of index.
func indexFile(location, folder, extension string, root protocol.Root) string {
	sum := sha256.Sum256([]byte(root.Path))
	return filepath.Join(expandHome(location), folder, hex.EncodeToString(sum[:8])+extension)
}

// shardPath is where a root's working-tree index is saved.
func shardPath(location string, root protocol.Root) string {
	return indexFile(location, "tree", ".shard", root)
}

// historyPath is where a root's history index is saved.
func historyPath(location string, root protocol.Root) string {
	return indexFile(location, "history", ".history", root)
}

// expandHome expands a leading "~" or "~/"; the host normally already has.
func expandHome(path string) string {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, strings.TrimPrefix(path, "~"))
}

// fileHistory lets the working-tree engine ask a history store about files.
type fileHistory struct{ store *history.Store }

// LastCommit returns the newest commit that changed path.
func (h fileHistory) LastCommit(path string) (protocol.LastCommit, time.Time, bool) {
	touch, ok := h.store.LastCommit(path)
	if !ok {
		return protocol.LastCommit{}, time.Time{}, false
	}
	return protocol.LastCommit{SHA: touch.SHA, Author: touch.Author, At: touch.At.UTC().Format(time.RFC3339)}, touch.At, true
}

// Dirty reports whether path has uncommitted changes.
func (h fileHistory) Dirty(path string) bool { return h.store.Dirty(path) }
