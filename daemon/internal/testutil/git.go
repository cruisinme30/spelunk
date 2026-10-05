package testutil

import (
	"os"
	"strings"
)

// IsolateGit clears every GIT_* variable from the environment, so the git
// commands tests run (to build fixture repos, or through the code under
// test) act on the folder they name and nothing else. A git hook runs with
// GIT_DIR set, absolute in a linked worktree, and git honours it over -C:
// without this, a test run from the pre-push hook would init, configure and
// commit to the repo being pushed. Call it from TestMain.
func IsolateGit() {
	for _, entry := range os.Environ() {
		if name, _, _ := strings.Cut(entry, "="); strings.HasPrefix(name, "GIT_") {
			_ = os.Unsetenv(name)
		}
	}
}
