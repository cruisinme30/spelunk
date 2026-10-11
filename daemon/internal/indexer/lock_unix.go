//go:build unix

package indexer

import (
	"errors"
	"os"
	"syscall"
)

// openLocked opens path and takes an exclusive flock on it. flock belongs
// to the open file, so a second open in the same process is refused too.
func openLocked(path string) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600) //nolint:gosec // G304: path is under the index directory the user configured
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, errLocked
		}
		return nil, err
	}
	return file, nil
}
