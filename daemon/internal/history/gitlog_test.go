package history

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSaveRemovesTempFilesACrashLeftBehind(t *testing.T) {
	r, _ := paymentsHistory(t)
	dir := t.TempDir()
	stale := filepath.Join(dir, ".history-111")
	if err := os.WriteFile(stale, []byte("half a store"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * staleTempAge)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}
	if err := r.ingest().Save(filepath.Join(dir, "repo.history")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a temp file from an hour-old crashed save is still there (%v)", err)
	}
}
