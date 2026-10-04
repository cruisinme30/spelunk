package indexer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/cruisinme30/unified-search/daemon/internal/protocol"
	"github.com/cruisinme30/unified-search/daemon/internal/trigram"
)

// Indexer builds and publishes the working-tree index of every root.
// Its methods are safe for concurrent use.
type Indexer struct {
	notify func([]protocol.RepoStatus)

	mu       sync.Mutex
	settings protocol.Settings
	order    []string         // root IDs, in workspace order
	repos    map[string]*repo // by root ID
	queue    []string         // root IDs waiting for a build
	wake     chan struct{}    // signals the worker that the queue changed

	stop    context.CancelFunc
	stopped chan struct{}
}

// repo is one root's index state.
type repo struct {
	root  protocol.Root
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
}

// New starts an indexer. notify receives every status change; it is called
// without locks held and must not block for long.
func New(settings protocol.Settings, notify func([]protocol.RepoStatus)) *Indexer {
	ctx, stop := context.WithCancel(context.Background())
	ix := &Indexer{
		notify: notify, settings: settings, repos: map[string]*repo{},
		wake: make(chan struct{}, 1), stop: stop, stopped: make(chan struct{}),
	}
	go ix.work(ctx)
	return ix
}

// SetRoots starts indexing new roots and drops removed ones.
func (ix *Indexer) SetRoots(roots []protocol.Root) {
	saved := ix.savedShards(roots)
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
		r := &repo{root: root, state: protocol.IndexStateQueued, visible: true}
		ix.repos[root.ID] = r
		if shard := saved[root.ID]; shard != nil {
			r.shard, r.state, r.visible = shard, protocol.IndexStateReady, false // refresh quietly
		}
		ix.enqueue(root.ID)
	}
	for id := range ix.repos {
		if !keep[id] {
			ix.drop(id)
		}
	}
	ix.mu.Unlock()
	ix.publishStatus()
}

// savedShards loads the saved shard of every root the indexer doesn't have
// yet. Decoding happens without the lock: a large repo's shard takes a
// while to read, and searches must not wait for it.
func (ix *Indexer) savedShards(roots []protocol.Root) map[string]*trigram.Shard {
	ix.mu.Lock()
	paths := map[string]string{}
	for _, root := range roots {
		if r, ok := ix.repos[root.ID]; !ok || r.root.Path != root.Path {
			paths[root.ID] = ix.shardPath(root)
		}
	}
	ix.mu.Unlock()
	shards := map[string]*trigram.Shard{}
	for id, path := range paths {
		if shard, err := trigram.Load(path); err == nil {
			shards[id] = shard
		}
	}
	return shards
}

// SetSettings applies new settings, rebuilding every repo when a setting
// that decides what is indexed changed.
func (ix *Indexer) SetSettings(settings protocol.Settings) {
	ix.mu.Lock()
	old := ix.settings
	ix.settings = settings
	changed := !slices.Equal(old.Exclude, settings.Exclude) || old.IncludeIgnored != settings.IncludeIgnored ||
		old.MaxFileSizeKB != settings.MaxFileSizeKB || old.Location != settings.Location
	if changed {
		for _, id := range ix.order {
			ix.restart(id)
		}
	}
	ix.mu.Unlock()
	if changed {
		ix.publishStatus()
	}
}

// Rebuild rebuilds one repo, or every repo when repoID is "". The old shard
// keeps serving searches until the new one is ready.
func (ix *Indexer) Rebuild(repoID string) error {
	ix.mu.Lock()
	if repoID != "" && ix.repos[repoID] == nil {
		ix.mu.Unlock()
		return fmt.Errorf("no open repo %q", repoID)
	}
	for _, id := range ix.order {
		if repoID == "" || id == repoID {
			ix.restart(id)
		}
	}
	ix.mu.Unlock()
	ix.publishStatus()
	return nil
}

// Repos returns the published shards, in workspace order. Repos without a
// shard yet are left out: searches return what is ready.
func (ix *Indexer) Repos() []trigram.Repo {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	var repos []trigram.Repo
	for _, id := range ix.order {
		if r := ix.repos[id]; r.shard != nil {
			repos = append(repos, trigram.Repo{ID: id, Name: r.root.Name, Root: r.root.Path, Shard: r.shard})
		}
	}
	return repos
}

// Status returns every repo's state, in workspace order.
func (ix *Indexer) Status() []protocol.RepoStatus {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	statuses := make([]protocol.RepoStatus, 0, len(ix.order))
	for _, id := range ix.order {
		r := ix.repos[id]
		status := protocol.RepoStatus{
			RepoID: id, Name: r.root.Name, Tree: r.state,
			History: protocol.IndexStateOff, Message: r.message,
		}
		if r.state == protocol.IndexStateIndexing {
			status.Progress = r.progress
		}
		statuses = append(statuses, status)
	}
	return statuses
}

// Close stops the worker, abandoning any build, and waits up to timeout.
func (ix *Indexer) Close(timeout time.Duration) {
	ix.stop()
	select {
	case <-ix.stopped:
	case <-time.After(timeout):
	}
}

// enqueue adds a repo to the build queue once. Callers hold mu.
func (ix *Indexer) enqueue(id string) {
	if !slices.Contains(ix.queue, id) {
		ix.queue = append(ix.queue, id)
	}
	select {
	case ix.wake <- struct{}{}:
	default:
	}
}

// restart abandons a repo's build in progress and queues a new one, shown
// as queued and then indexing. Callers hold mu.
func (ix *Indexer) restart(id string) {
	r := ix.repos[id]
	r.generation++
	if r.cancelBuild != nil {
		r.cancelBuild()
	}
	r.visible = true
	r.state = protocol.IndexStateQueued
	ix.enqueue(id)
}

// drop forgets a repo and abandons its build. Callers hold mu.
func (ix *Indexer) drop(id string) {
	if r, ok := ix.repos[id]; ok {
		if r.cancelBuild != nil {
			r.cancelBuild()
		}
		delete(ix.repos, id)
	}
	ix.queue = slices.DeleteFunc(ix.queue, func(q string) bool { return q == id })
}

// publishStatus sends every repo's status to notify. Callers must not
// hold mu.
func (ix *Indexer) publishStatus() {
	if ix.notify != nil {
		ix.notify(ix.Status())
	}
}

// work builds queued repos, one at a time, until ctx ends.
func (ix *Indexer) work(ctx context.Context) {
	defer close(ix.stopped)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ix.wake:
		}
		for ctx.Err() == nil {
			job, ok := ix.next(ctx)
			if !ok {
				break
			}
			ix.build(job)
		}
	}
}

// buildJob is a snapshot of what one build needs.
type buildJob struct {
	ctx        context.Context
	done       context.CancelFunc
	id         string
	root       protocol.Root
	generation int
	settings   protocol.Settings
	shardPath  string
}

// next takes the first queued repo and marks it as building.
func (ix *Indexer) next(ctx context.Context) (buildJob, bool) {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	for len(ix.queue) > 0 {
		id := ix.queue[0]
		ix.queue = ix.queue[1:]
		r, ok := ix.repos[id]
		if !ok {
			continue
		}
		buildCtx, cancel := context.WithCancel(ctx)
		r.cancelBuild = cancel
		if r.visible {
			r.state, r.progress, r.message = protocol.IndexStateIndexing, 0, ""
		}
		return buildJob{
			ctx: buildCtx, done: cancel, id: id, root: r.root, generation: r.generation,
			settings: ix.settings, shardPath: ix.shardPath(r.root),
		}, true
	}
	return buildJob{}, false
}

// build lists, indexes and saves one repo, then publishes the shard.
func (ix *Indexer) build(job buildJob) {
	defer job.done()
	ix.publishStatus()
	shard, err := buildShard(job, func(progress float64) {
		visible := false
		ix.update(job, func(r *repo) { r.progress, visible = progress, r.visible })
		if visible {
			ix.publishStatus()
		}
	})
	if job.ctx.Err() != nil {
		return // superseded, dropped or shutting down
	}
	var saveErr error
	if err == nil {
		saveErr = shard.Save(job.shardPath)
	}
	ix.update(job, func(r *repo) {
		r.cancelBuild = nil
		if err != nil {
			r.state, r.message = protocol.IndexStateError, err.Error()
			return
		}
		// A shard that couldn't be saved still serves this session.
		r.shard, r.state, r.message, r.visible = shard, protocol.IndexStateReady, "", false
		if saveErr != nil {
			r.message = "Index not saved: " + saveErr.Error()
		}
	})
	ix.publishStatus()
}

// update applies change to the job's repo if the job is still current: a
// newer build, or a dropped root, means its result no longer matters.
func (ix *Indexer) update(job buildJob, change func(*repo)) {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	r, ok := ix.repos[job.id]
	if !ok || r.generation != job.generation || r.root.Path != job.root.Path {
		return
	}
	change(r)
}

// buildShard lists the root's files and indexes them.
func buildShard(job buildJob, progress func(float64)) (*trigram.Shard, error) {
	info, err := os.Stat(job.root.Path)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("folder not found: %s", job.root.Path)
	}
	files, err := trigram.ListFiles(job.ctx, job.root.Path, trigram.WalkOptions{
		Exclude:        trigram.NewExcluder(job.settings.Exclude),
		IncludeIgnored: job.settings.IncludeIgnored,
		MaxFileBytes:   int64(job.settings.MaxFileSizeKB) * 1024,
	})
	if err != nil {
		return nil, fmt.Errorf("list files: %w", err)
	}
	return trigram.Build(job.ctx, job.root.Path, files, progress)
}

// shardPath is where a root's shard is saved: one file per root path under
// index.location. Callers hold mu.
func (ix *Indexer) shardPath(root protocol.Root) string {
	sum := sha256.Sum256([]byte(root.Path))
	return filepath.Join(expandHome(ix.settings.Location), "tree", hex.EncodeToString(sum[:8])+".shard")
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
