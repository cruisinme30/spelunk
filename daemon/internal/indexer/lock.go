package indexer

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
)

// errLocked means another process holds a repo's write lock.
var errLocked = errors.New("another process writes this index")

// writeLock is the per-repo lock that makes one daemon the only writer of
// a repo's saved indexes. The operating system releases it when the
// process ends, even after a crash, so it never goes stale.
type writeLock struct{ file *os.File }

// lockPath is where a root's write lock lives, beside its saved indexes.
func lockPath(location string, root protocol.Root) string {
	return indexFile(location, "locks", ".lock", root)
}

// tryLock takes the lock at path without waiting: errLocked when another
// process (or another indexer in this one) holds it. When the lock file
// can't be made at all (a full disk, an unwritable index location) there
// is nothing to share, so this daemon writes without one, as it did before
// locks existed, and its saves report the problem.
func tryLock(path string) (*writeLock, error) {
	err := os.MkdirAll(filepath.Dir(path), 0o750)
	var file *os.File
	if err == nil {
		file, err = openLocked(path)
	}
	if errors.Is(err, errLocked) {
		return nil, err
	}
	return &writeLock{file: file}, nil
}

// release gives the lock up. A nil lock is fine.
func (l *writeLock) release() {
	if l != nil && l.file != nil {
		_ = l.file.Close() // closing the file releases the lock
	}
}
