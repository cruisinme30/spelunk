//go:build windows

package indexer

import (
	"errors"
	"os"
	"syscall"
)

// errSharingViolation is ERROR_SHARING_VIOLATION: the file is open
// elsewhere with no sharing.
const errSharingViolation syscall.Errno = 32

// openLocked opens path with no sharing: while it is open, every other
// open of it fails, which is the lock.
func openLocked(path string) (*os.File, error) {
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	handle, err := syscall.CreateFile(name, syscall.GENERIC_READ|syscall.GENERIC_WRITE, 0, nil,
		syscall.OPEN_ALWAYS, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		if errors.Is(err, errSharingViolation) {
			return nil, errLocked
		}
		return nil, err
	}
	return os.NewFile(uintptr(handle), path), nil
}
