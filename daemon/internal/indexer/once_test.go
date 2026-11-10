package indexer

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
	"github.com/cruisinme30/spelunk/daemon/internal/testutil"
)

func TestCanceledOneShotKeepsLockUntilWorkerDrains(t *testing.T) {
	root := protocol.Root{ID: "r", Name: "test", Path: testutil.WriteTree(t, map[string]string{"a.txt": "alpha"})}
	settings := testSettings(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ix := newIndexer(ctx, settings, nil, true)
	started, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var unblock sync.Once
	ix.beforeBuild = func(string) {
		close(started)
		<-release
		close(finished)
	}
	t.Cleanup(func() {
		unblock.Do(func() { close(release) })
		ix.closeWriter()
	})
	ix.SetRoots([]protocol.Root{root})
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("tree worker did not start")
	}
	cancel()
	closed := make(chan struct{})
	go func() { ix.closeWriter(); close(closed) }()
	select {
	case <-closed:
		t.Fatal("one-shot close returned while the worker was blocked")
	case <-time.After(25 * time.Millisecond):
		// Give closeWriter time to reach its drain while the worker is held.
	}
	lock, err := tryLock(lockPath(settings.Location, root))
	if lock != nil {
		lock.release()
	}
	if !errors.Is(err, errLocked) {
		t.Fatalf("writer lock released before worker drained: %v", err)
	}
	unblock.Do(func() { close(release) })
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("one-shot close did not finish after the worker drained")
	}
	select {
	case <-finished:
	default:
		t.Fatal("worker still active after close")
	}
	lock, err = tryLock(lockPath(settings.Location, root))
	if err != nil || lock == nil || lock.file == nil {
		t.Fatalf("writer lock not released after drain: %v", err)
	}
	defer lock.release()
	if _, err := os.Stat(shardPath(settings.Location, root)); !os.IsNotExist(err) {
		t.Fatalf("canceled worker published a shard: %v", err)
	}
}

func TestOneShotDoesNotFollowOrTakeOver(t *testing.T) {
	root := protocol.Root{ID: "r", Name: "test", Path: testutil.WriteTree(t, map[string]string{"a.txt": "alpha"})}
	settings := testSettings(t)
	writer := New(settings, nil)
	writer.SetRoots([]protocol.Root{root})
	defer writer.Close(time.Second)
	ix := newIndexer(t.Context(), settings, nil, true)
	defer ix.closeWriter()
	ix.SetRoots([]protocol.Root{root})
	if _, err := ix.savedWorkDone(root.ID); !errors.Is(err, ErrNotWriter) {
		t.Fatalf("one-shot should reject an active writer: %v", err)
	}
	writer.Close(time.Second)
	if jobs := ix.followers(); len(jobs) != 0 || ix.Status()[0].Writer {
		t.Fatalf("one-shot took over another writer's index: jobs %v, status %+v", jobs, ix.Status())
	}
}
