package trigram

// Tests for owner:, which keeps the files a repo's CODEOWNERS file gives an owner.

import (
	"reflect"
	"testing"

	"github.com/cruisinme30/spelunk/daemon/internal/codeowners"
)

// ownersFile is payments-api's CODEOWNERS file, as on board 42.
const ownersFile = `# Default owners
*                     @acme/platform
/src/payments/        @acme/payments-core
/src/payments/http/   @acme/payments-core @jane
/docs/                writers@acme.example
/src/payments/legacy/
`

// ownedRepo is payments-api, with its CODEOWNERS file read.
func ownedRepo() Repo {
	repo := repoOf("payments-api", map[string]string{
		".github/CODEOWNERS":           ownersFile,
		"src/payments/retry_policy.py": "class RetryPolicy:\n    retry = 3\n",
		"src/payments/client.py":       "from payments.http.retry import retry\n",
		"src/payments/http/retry.py":   "def retry(fn):\n    log.warning('retry')\n",
		"src/payments/legacy/retry.py": "def old_retry(): pass\n",
		"src/search/retry.py":          "def search_retry(): pass\n",
		"docs/retry.md":                "# How retry works\n",
	})
	repo.Owners = codeowners.Parse(".github/CODEOWNERS", []byte(ownersFile))
	return repo
}

// unownedRepo is web-checkout, which has no CODEOWNERS file.
var unownedRepo = repoOf("web-checkout", map[string]string{
	"src/retry.ts": "export const retry = 1;\n",
})

func TestOwnerKeepsTheFilesCodeOwnersGivesThatOwner(t *testing.T) {
	// @covers op:owner screen:owner-filter
	tests := []struct {
		query string
		want  []string
	}{
		{"content:retry owner:@payments-core", []string{
			"src/payments/client.py:1 from payments.http.retry import retry",
			"src/payments/http/retry.py:1 def retry(fn):",
			"src/payments/http/retry.py:2     log.warning('retry')",
			"src/payments/retry_policy.py:1 class RetryPolicy:",
			"src/payments/retry_policy.py:2     retry = 3",
		}},
		// The team in full, its name alone, any case: the same owner.
		{"content:retry owner:acme/PAYMENTS-core f:client", []string{
			"src/payments/client.py:1 from payments.http.retry import retry",
		}},
		{"content:retry owner:jane", []string{
			"src/payments/http/retry.py:1 def retry(fn):",
			"src/payments/http/retry.py:2     log.warning('retry')",
		}},
		{"content:retry owner:writers@acme.example", []string{"docs/retry.md:1 # How retry works"}},
		// The default owner covers what no later line takes.
		{"content:retry owner:@acme/platform", []string{"src/search/retry.py:1 def search_retry(): pass"}},
		// A line with no owners leaves its files unowned.
		{"type:file owner:none", []string{"file src/payments/legacy/retry.py"}},
		{"type:file owner:NONE f:legacy", []string{"file src/payments/legacy/retry.py"}},
		// Owners are whole handles, never parts of them.
		{"content:retry owner:jan", []string{}},
		{"content:retry owner:payments", []string{}},
	}
	for _, tt := range tests {
		if got, _ := run(t, tt.query, ownedRepo(), unownedRepo); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%s:\n got %q\nwant %q", tt.query, got, tt.want)
		}
	}
}

func TestOwnerNeverMatchesInARepoWithoutCodeOwners(t *testing.T) {
	// @covers op:owner
	for query, want := range map[string][]string{
		"content:retry owner:none":    {},
		"content:retry owner:jane":    {},
		"content:retry -owner:jane":   {"src/retry.ts:1 export const retry = 1;"},
		"content:retry -owner:none":   {"src/retry.ts:1 export const retry = 1;"},
		"retry (owner:jane OR f:src)": {"file src/retry.ts", "src/retry.ts:1 export const retry = 1;"},
	} {
		if got, _ := run(t, query, unownedRepo); !reflect.DeepEqual(got, want) {
			t.Errorf("%s:\n got %q\nwant %q", query, got, want)
		}
	}
}

func TestOwnerSaysWhatItLeftOut(t *testing.T) {
	// @covers op:owner screen:owner-filter
	tests := []struct {
		query, filter, reason string
		count                 int
	}{
		// 5 matches are the team's; the other 4 are hidden, web-checkout's among them.
		{"content:retry owner:@payments-core", "owner:@payments-core", "owner", 4},
		{"content:retry -owner:@acme/platform", "-owner:@acme/platform", "not", 1},
	}
	for _, tt := range tests {
		_, stats := run(t, tt.query, ownedRepo(), unownedRepo)
		if len(stats.Hidden) != 1 {
			t.Errorf("%s: hidden = %+v, want one note", tt.query, stats.Hidden)
			continue
		}
		note := stats.Hidden[0]
		if note.Filter != tt.filter || note.Reason != tt.reason || note.Count != tt.count || note.Undo.Title != "Remove "+tt.filter {
			t.Errorf("%s: note = %+v, want %d hidden by %s (%s), undone by removing it", tt.query, note, tt.count, tt.filter, tt.reason)
		}
	}
}
