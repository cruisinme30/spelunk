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

	"github.com/cruisinme30/spelunk/daemon/internal/codeowners"
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

	// life ends when the indexer is closed; work it does outside the
	// workers, such as re-reading saved files, stops with it.
	life    context.Context
	stop    context.CancelFunc
	workers sync.WaitGroup

	// beforeBuild, when tests set it, runs with the root's ID at the start of
	// each working-tree build, which waits for it to return.
	beforeBuild func(id string)
	// onNewCommits hears of the commits each history update brings in.
	onNewCommits func(repo history.Repo, commits []history.Commit)
	// readOnly makes the indexer a reader of saved indexes: it never takes
	// a repo's write lock, so it never builds a working-tree index, reads a
	// whole history, or saves anything.
	readOnly bool
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
	// owners is the repo's CODEOWNERS file, read again with each build and
	// whenever the editor changes it; nil when the repo has none.
	owners *codeowners.File

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

	// lock makes this indexer the repo's only writer. It is nil when
	// another daemon holds it, or the indexer is read-only: the repo then
	// follows that daemon's saved indexes instead of building and saving
	// its own (see follow).
	lock *writeLock
	// savedTree and savedHistory are when the saved indexes this repo last
	// loaded were written, so a follower can load newer saves.
	savedTree, savedHistory time.Time
	// swept is when Sweep last listed the repo's files.
	swept time.Time
}

// queue is a list of root IDs waiting for a worker, and a signal for it.
type queue struct {
	ids  []string
	wake chan struct{}
}

// popQueued takes the first id off q whose repo is still there. Callers
// hold ix.mu.
func (ix *Indexer) popQueued(q *queue) (string, *repo, bool) {
	for len(q.ids) > 0 {
		id := q.ids[0]
		q.ids = q.ids[1:]
		if r, ok := ix.repos[id]; ok {
			return id, r, true
		}
	}
	return "", nil, false
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
		notify: notify, settings: settings, repos: map[string]*repo{}, life: ctx, stop: stop,
		treeQueue:    queue{wake: make(chan struct{}, 1)},
		historyQueue: queue{wake: make(chan struct{}, 1)},
	}
	ix.workers.Add(2)
	go ix.treeWork(ctx)
	go ix.historyWork(ctx)
	return ix
}

// NewReadOnly starts an indexer that only reads saved indexes, as the
// spelunk command's background reader does: every repo follows the
// daemon that writes it, and one that has never been indexed stays empty.
func NewReadOnly(settings protocol.Settings, notify func([]protocol.RepoStatus)) *Indexer {
	ix := New(settings, notify)
	ix.mu.Lock()
	ix.readOnly = true
	ix.mu.Unlock()
	return ix
}

// OnNewCommits sets what hears of the commits each history update brings
// in (see history.NewCommits), with the repo as it now is: for query
// watches. Reading a repo's history for the first time brings in none.
func (ix *Indexer) OnNewCommits(f func(repo history.Repo, commits []history.Commit)) {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	ix.onNewCommits = f
}

// SetRoots starts indexing new roots and drops removed ones. A root's saved
// indexes, if any, serve searches at once while they are refreshed.
func (ix *Indexer) SetRoots(roots []protocol.Root) {
	saved := ix.savedIndexes(roots)
	ix.mu.Lock()
	nestedBefore := map[string][]string{}
	for _, id := range ix.order {
		nestedBefore[id] = ix.nestedRoots(ix.repos[id].root.Path)
	}
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
	// A root opened or closed inside another moves files between them.
	for id, before := range nestedBefore {
		if r, ok := ix.repos[id]; ok && !slices.Equal(before, ix.nestedRoots(r.root.Path)) {
			ix.restart(id)
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
		r.owners, r.savedTree = saved.owners, saved.treeTime
	}
	if saved.history != nil {
		// A history saved under another index.historyDepth or index.exclude
		// serves searches until it is read again under these settings.
		r.history, r.historyState = saved.history, protocol.IndexStateReady
		r.historyFull = saved.history.Key != historyKey(ix.settings)
		r.savedHistory = saved.historyTime
	}
	if !ix.readOnly {
		r.lock, _ = tryLock(lockPath(ix.settings.Location, root)) // nil: another daemon writes it
	}
	if r.lock == nil {
		ix.startFollowing(r, !saved.treeTime.IsZero())
		return
	}
	ix.treeQueue.add(root.ID)
	ix.historyQueue.add(root.ID)
}

// startFollowing sets up a repo another daemon writes, or that a
// read-only indexer reads: it serves the saved indexes, and only waits
// for them when there are none yet. unusable says a saved index exists
// that couldn't be loaded (another version of Spelunk saved it). Callers
// hold mu.
func (ix *Indexer) startFollowing(r *repo, unusable bool) {
	if r.shard == nil {
		r.state, r.visible = protocol.IndexStateQueued, true
		r.message = ix.followerWaiting(unusable)
	}
	switch {
	case r.history == nil && !insideGit(r.root.Path):
		r.historyState = protocol.IndexStateOff
		r.historyMessage = notGitMessage
	case r.history == nil:
		r.historyState = protocol.IndexStateQueued
	default:
		// New commits are read into memory, never saved (see readHistory).
		ix.historyQueue.add(r.root.ID)
	}
}

// followerWaiting says why a follower has no index yet. Callers hold mu.
func (ix *Indexer) followerWaiting(unusable bool) string {
	switch {
	case ix.readOnly && unusable:
		return "Its saved index is from another version of Spelunk. Open this folder in VS Code to index it again."
	case ix.readOnly:
		return "Not indexed yet. Open this folder in VS Code with Spelunk to index it."
	}
	return "Another VS Code window is indexing this folder. Its results appear here when it saves them."
}

// savedIndex is what a previous session saved for a root.
type savedIndex struct {
	shard   *trigram.Shard
	owners  *codeowners.File // read from the repo itself, to go with shard
	history *history.Store
	// treeTime and historyTime are when shard and history were saved.
	treeTime, historyTime time.Time
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
	location, codeownersFiles := ix.settings.Location, ix.settings.CodeownersFiles
	ix.mu.Unlock()
	saved := map[string]savedIndex{}
	for _, root := range fresh {
		saved[root.ID] = loadSaved(location, codeownersFiles, root, true, true)
	}
	return saved
}

// loadSaved reads the saved working-tree index (when wantTree is set) and
// the saved history (when wantHistory is set) of root, with when each was
// saved.
func loadSaved(location string, codeownersFiles []string, root protocol.Root, wantTree, wantHistory bool) savedIndex {
	var index savedIndex
	if wantTree {
		path := shardPath(location, root)
		index.treeTime = modTime(path)
		shard, err := trigram.Load(path)
		switch {
		case err == nil:
			index.shard, index.owners = shard, codeowners.Load(root.Path, codeownersFiles)
		case !errors.Is(err, os.ErrNotExist) && !errors.Is(err, trigram.ErrStaleFormat):
			index.problem = "The saved index couldn't be read, so it is being rebuilt."
		}
	}
	if wantHistory {
		path := historyPath(location, root)
		index.historyTime = modTime(path)
		if store, err := history.Load(path); err == nil {
			index.history = store
		}
	}
	return index
}

// modTime is when the file at path was last written; zero if it is missing.
func modTime(path string) time.Time {
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}

// SetSettings applies new settings, rebuilding every repo's working-tree
// index when a setting that decides what is indexed changed, and reading
// every history again when the history window changed.
func (ix *Indexer) SetSettings(settings protocol.Settings) {
	ix.mu.Lock()
	old := ix.settings
	ix.settings = settings
	treeChanged, historyChanged, ownersChanged := settingsChanges(old, settings)
	if old.Location != settings.Location {
		ix.relock()
	}
	for _, id := range ix.order {
		if treeChanged {
			ix.restart(id)
		}
		if historyChanged {
			ix.restartHistory(id)
		}
	}
	ix.mu.Unlock()
	if ownersChanged {
		ix.reloadOwners(settings.CodeownersFiles)
	}
	if treeChanged || historyChanged {
		ix.publishStatus()
	}
}

// settingsChanges says what new settings change: what the working-tree
// index holds, what the history holds, or only who owns what (a rebuild
// reads CODEOWNERS again, so owners alone change only without one).
func settingsChanges(old, settings protocol.Settings) (tree, commits, owners bool) {
	tree = !slices.Equal(old.Exclude, settings.Exclude) || old.IncludeIgnored != settings.IncludeIgnored ||
		old.MaxFileSizeKB != settings.MaxFileSizeKB || old.Location != settings.Location || old.Symbols != settings.Symbols ||
		old.NotebookOutputs != settings.NotebookOutputs
	commits = old.HistoryDepth != settings.HistoryDepth || old.Location != settings.Location ||
		!slices.Equal(old.Exclude, settings.Exclude)
	owners = !tree && !slices.Equal(old.CodeownersFiles, settings.CodeownersFiles)
	return tree, commits, owners
}

// reloadOwners reads every repo's CODEOWNERS file again from locations.
func (ix *Indexer) reloadOwners(locations []string) {
	ix.mu.Lock()
	roots := map[string]string{}
	for id, r := range ix.repos {
		roots[id] = r.root.Path
	}
	ix.mu.Unlock()
	for id, root := range roots {
		owners := codeowners.Load(root, locations)
		ix.mu.Lock()
		if r, ok := ix.repos[id]; ok && r.root.Path == root {
			r.owners = owners
		}
		ix.mu.Unlock()
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
	if repoID != "" && ix.repos[repoID].lock == nil {
		ix.mu.Unlock()
		return ErrNotWriter
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
			Overlay: r.overlay.shard, Masked: r.overlay.masked, Owners: r.owners,
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
		status := protocol.RepoStatus{
			RepoID: id, Name: r.root.Name, Tree: r.state, History: r.historyState,
			Root: r.root.Path, Reread: len(r.overlay.masked), Writer: r.lock != nil,
		}
		if !r.savedTree.IsZero() {
			status.SavedAt = r.savedTree.UTC().Format(time.RFC3339)
		}
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
	// A build abandoned past the timeout may still save once; the next
	// writer's build replaces it, as after a crash.
	ix.mu.Lock()
	defer ix.mu.Unlock()
	for _, r := range ix.repos {
		r.lock.release()
		r.lock = nil
	}
}

// ErrNotWriter is Rebuild's error for a repo another daemon writes.
var ErrNotWriter = errors.New("another VS Code window keeps this folder's index, so rebuild it there")

// relock moves every repo's write lock to the index location the settings
// now name. Callers hold mu.
func (ix *Indexer) relock() {
	for _, id := range ix.order {
		r := ix.repos[id]
		r.lock.release()
		r.lock = nil
		if !ix.readOnly {
			r.lock, _ = tryLock(lockPath(ix.settings.Location, r.root))
		}
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
		r.lock.release()
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

// SavedRoot is the root a folder belongs to, for the spelunk command: the
// nearest folder, from path up, that has a saved working-tree index under
// location; failing that, the nearest that holds a .git; failing that,
// path itself.
func SavedRoot(location, path string) string {
	gitRoot := ""
	for dir := path; ; dir = filepath.Dir(dir) {
		if !modTime(shardPath(location, protocol.Root{Path: dir})).IsZero() {
			return dir
		}
		if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil && gitRoot == "" {
			gitRoot = dir
		}
		if filepath.Dir(dir) == dir {
			break
		}
	}
	if gitRoot != "" {
		return gitRoot
	}
	return path
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

// FolderState is what folder/explain needs about the folder (slash-separated,
// relative to root id's path): the root's path, the walk options its builds
// use, and whether its index holds a file in the folder. indexed is also
// true before the first build, when nothing can be said yet. ok is false
// for a root that isn't open.
func (ix *Indexer) FolderState(id, folder string) (root string, opts trigram.WalkOptions, indexed, ok bool) {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	r := ix.repos[id]
	if r == nil {
		return "", trigram.WalkOptions{}, false, false
	}
	indexed = r.shard == nil || len(r.shard.PathsUnder(folder)) > 0 ||
		r.overlay.shard != nil && len(r.overlay.shard.PathsUnder(folder)) > 0
	return r.root.Path, walkOptions(ix.settings, ix.nestedRoots(r.root.Path)), indexed, true
}
