package history

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestALongDiffLineIsClippedInThePreview(t *testing.T) {
	r := newTestRepo(t)
	sha := r.commit("Ada <ada@example.com>", "minified", day(1), map[string]string{
		"app.min.js": strings.Repeat("x", 300_000) + "needle" + strings.Repeat("y", 300_000) + "\n",
	})
	repo := &Repo{ID: "r", Name: "r", Root: r.root, Store: r.ingest()}
	preview, err := Preview(context.Background(), repo, Ref{RepoID: "r", SHA: sha}, plan(t, "type:commit needle"), 3)
	if err != nil {
		t.Fatal(err)
	}
	line := preview.Hunks[0].Lines[0]
	shown := []rune(line.Text)
	if len(shown) > 2_002 || len(line.Hits) != 1 || string(shown[line.Hits[0].Start:line.Hits[0].End]) != "needle" {
		t.Errorf("diff line = %d runes with hits %+v, want a clipped window with needle marked", len(shown), line.Hits)
	}
}

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
