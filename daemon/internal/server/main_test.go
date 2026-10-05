package server

import (
	"os"
	"testing"

	"github.com/cruisinme30/spelunk/daemon/internal/testutil"
)

// TestMain keeps the fixture repos' git commands out of the repo a hook runs in.
func TestMain(m *testing.M) {
	testutil.IsolateGit()
	os.Exit(m.Run())
}
