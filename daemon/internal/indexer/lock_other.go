//go:build !unix && !windows

package indexer

import "os"

// openLocked opens path. Platforms without a file lock get none: each
// daemon writes, as before the lock existed.
func openLocked(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600) //nolint:gosec // G304: path is under the index directory the user configured
}
