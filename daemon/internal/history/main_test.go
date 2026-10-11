package history

import (
	"os"
	"testing"

	"github.com/cruisinme30/spelunk/daemon/internal/testutil"
)

// TestMain keeps the fixture repos' git commands out of the repo a hook runs in.
// Run by Git as a notebook's textconv filter, the test binary prints the
// notebook's diff text, as the daemon's executable does.
func TestMain(m *testing.M) {
	if ServeNotebookText(os.Args, os.Stdout) {
		os.Exit(0)
	}
	testutil.IsolateGit()
	os.Exit(m.Run())
}
