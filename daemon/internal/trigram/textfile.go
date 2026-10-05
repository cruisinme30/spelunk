package trigram

import (
	"errors"
	"io"
	"os"
)

// errNotRegular means a path names something other than a regular file: a
// directory, a symlink, a FIFO, a socket or a device.
var errNotRegular = errors.New("not a regular file")

// errTooBig means a file grew past the size limit after it was listed.
var errTooBig = errors.New("file is over the size limit")

// openRegular opens a regular file for reading. It never blocks on a FIFO
// and never follows a symlink, even when one replaced the file after it was
// listed: either fails with errNotRegular.
func openRegular(path string) (*os.File, os.FileInfo, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|openFlags, 0) //nolint:gosec // G304: paths come from listing the indexed repo
	if err != nil {
		if isSymlinkRefusal(path) {
			return nil, nil, errNotRegular
		}
		return nil, nil, err
	}
	info, err := f.Stat()
	if err != nil {
		closeReadOnly(f)
		return nil, nil, err
	}
	if !info.Mode().IsRegular() {
		closeReadOnly(f)
		return nil, nil, errNotRegular
	}
	return f, info, nil
}

// isSymlinkRefusal reports whether opening path failed because it is a
// symlink, which openFlags refuses to follow.
func isSymlinkRefusal(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode()&os.ModeSymlink != 0
}

// readText reads a regular file's text. maxBytes, when positive, bounds what is read: a file
// that is bigger now than when it was listed fails with errTooBig rather
// than filling memory, and binary content (it may have changed since it
// was listed) fails with errBinary.
func readText(path string, maxBytes int64) ([]byte, os.FileInfo, error) {
	f, info, err := openRegular(path)
	if err != nil {
		return nil, nil, err
	}
	defer closeReadOnly(f)
	var r io.Reader = f
	if maxBytes > 0 {
		r = io.LimitReader(f, maxBytes+1)
	}
	raw, err := io.ReadAll(r)
	if err != nil {
		return nil, nil, err
	}
	if maxBytes > 0 && int64(len(raw)) > maxBytes {
		return nil, nil, errTooBig
	}
	if isBinary(raw[:min(len(raw), binarySniffBytes)]) {
		return nil, nil, errBinary
	}
	return raw, info, nil
}

// errBinary means a file's content is binary (see isBinary).
var errBinary = errors.New("binary file")
