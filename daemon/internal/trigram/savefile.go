package trigram

import (
	"encoding/gob"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// StaleTempAge is how old a temporary file must be before SaveGob treats
// it as left behind by a crash and removes it. Another daemon (one runs per
// VS Code window) may be writing a newer one into the same folder.
const StaleTempAge = time.Hour

// SaveGob gob-encodes value to path atomically: readers see the old file or
// the new one, never half of one, even after a crash. The temporary file is
// named by tempPattern, and SaveGob also removes the ones saves a crash
// interrupted. what names the value in errors. Shards and the history store
// are saved this way.
func SaveGob(path, tempPattern, what string, value any) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	removeStaleTemps(dir, tempPattern, time.Now().Add(-StaleTempAge))
	temp, err := os.CreateTemp(dir, tempPattern)
	if err != nil {
		return err
	}
	// Removing fails harmlessly after a successful rename: the name is gone.
	defer func() { _ = os.Remove(temp.Name()) }()
	if err := gob.NewEncoder(temp).Encode(value); err != nil {
		_ = temp.Close() // the encode error is the one to report
		return fmt.Errorf("encode %s: %w", what, err)
	}
	// Flushed before the rename, so a crash can't leave a renamed but empty file.
	if err := temp.Sync(); err != nil {
		_ = temp.Close() // the sync error is the one to report
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(temp.Name(), path)
}

// LoadGob decodes a file SaveGob wrote into value. what names the value in
// errors.
func LoadGob(path, what string, value any) error {
	f, err := os.Open(path) //nolint:gosec // G304: path is under the index directory the user configured
	if err != nil {
		return err
	}
	defer closeReadOnly(f)
	if err := gob.NewDecoder(f).Decode(value); err != nil {
		return fmt.Errorf("decode %s %s: %w", what, path, err)
	}
	return nil
}

// removeStaleTemps removes the files in dir that match pattern and were
// last written before cutoff: what saves interrupted by a crash left.
// Errors are ignored; a file that can't be removed is tried again next time.
func removeStaleTemps(dir, pattern string, cutoff time.Time) {
	matches, _ := filepath.Glob(filepath.Join(dir, pattern))
	for _, match := range matches {
		if info, err := os.Lstat(match); err == nil && info.Mode().IsRegular() && info.ModTime().Before(cutoff) {
			_ = os.Remove(match)
		}
	}
}
