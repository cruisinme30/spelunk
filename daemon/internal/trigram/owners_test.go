package trigram

// Tests for owner:, which keeps the files a repo's CODEOWNERS file gives an owner.

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/cruisinme30/spelunk/daemon/internal/codeowners"
	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
	"github.com/cruisinme30/spelunk/daemon/internal/testutil"
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

func TestResultsCarryTheirFilesOwners(t *testing.T) {
	// @covers op:owner screen:owner-filter
	items, _ := runItems(t, "retry f:http", defaultSettings, "", ownedRepo(), unownedRepo)
	var got []string
	for _, item := range items {
		got = append(got, item.Kind+" "+item.Path+" "+strings.Join(item.Owners, ","))
	}
	want := []string{
		"file src/payments/http/retry.py @acme/payments-core,@jane",
		"line src/payments/http/retry.py @acme/payments-core,@jane",
		"line src/payments/http/retry.py @acme/payments-core,@jane",
	}
	if !slices.Equal(got, want) {
		t.Errorf("items = %q, want %q", got, want)
	}
	// A repo without CODEOWNERS, and a file no line gives an owner, carry none.
	for _, query := range []string{"content:retry repo:web-checkout", "content:old_retry"} {
		if items, _ := runItems(t, query, defaultSettings, "", ownedRepo(), unownedRepo); len(items) != 1 || items[0].Owners != nil {
			t.Errorf("%s: items = %+v, want one without owners", query, items)
		}
	}
}

func TestTheOwnerFacetCountsEachOwnersMatches(t *testing.T) {
	// @covers op:owner screen:owner-filter
	_, stats := run(t, "content:retry", ownedRepo(), unownedRepo)
	var owner *protocol.Facet
	for i := range stats.Facets {
		if stats.Facets[i].Field == "owner" {
			owner = &stats.Facets[i]
		}
	}
	if owner == nil {
		t.Fatalf("facets = %+v, want an owner facet", stats.Facets)
	}
	var got []string
	for _, bucket := range owner.Buckets {
		got = append(got, fmt.Sprintf("%s %d %s", bucket.Label, bucket.Count, bucket.Filter))
	}
	// web-checkout has no CODEOWNERS, so its match counts in no owner bucket.
	want := []string{
		"@acme/payments-core 5 owner:@acme/payments-core",
		"@jane 2 owner:@jane",
		"@acme/platform 1 owner:@acme/platform",
		"No owner 1 owner:none",
		"writers@acme.example 1 owner:writers@acme.example",
	}
	if !slices.Equal(got, want) {
		t.Errorf("owner buckets = %q, want %q", got, want)
	}
	var fields []string
	for _, facet := range stats.Facets {
		fields = append(fields, facet.Field)
	}
	if want := []string{"repo", "lang", "owner", "folder"}; !slices.Equal(fields, want) {
		t.Errorf("facet fields = %q, want %q", fields, want)
	}
	if _, stats := run(t, "content:retry", unownedRepo); slices.ContainsFunc(stats.Facets, func(f protocol.Facet) bool { return f.Field == "owner" }) {
		t.Errorf("facets without any CODEOWNERS = %+v, want no owner facet", stats.Facets)
	}
}

func TestThePreviewSaysWhoOwnsTheFileAndWhichLineSaysSo(t *testing.T) {
	// @covers op:owner screen:owner-filter rpc:preview/get
	root := testutil.WriteTree(t, map[string]string{
		"src/payments/http/retry.py":   "def retry(fn):\n",
		"src/payments/legacy/retry.py": "def old_retry(): pass\n",
	})
	repo := &Repo{ID: "r", Name: "payments-api", Root: root, Owners: codeowners.Parse(".github/CODEOWNERS", []byte(ownersFile))}
	plan := mustPlan(t, "retry owner:jane", defaultSettings, "")
	got, err := Preview(repo, Ref{PlanID: 1, RepoID: "r", Path: "src/payments/http/retry.py", Line: 1, Column: 4, Length: 5}, plan, 2)
	if err != nil {
		t.Fatal(err)
	}
	want := &protocol.Ownership{Owners: []string{"@acme/payments-core", "@jane"}, File: ".github/CODEOWNERS", Line: 4}
	if !reflect.DeepEqual(got.Ownership, want) {
		t.Errorf("ownership = %+v, want %+v", got.Ownership, want)
	}
	// owner:jane is true of this file, so retry is still marked.
	if len(got.Hits) != 1 || got.Hits[0].Line != 1 {
		t.Errorf("hits = %+v, want retry marked on line 1", got.Hits)
	}
	got, err = Preview(repo, Ref{PlanID: 1, RepoID: "r", Path: "src/payments/legacy/retry.py"}, nil, 2)
	if err != nil {
		t.Fatal(err)
	}
	if want := (&protocol.Ownership{File: ".github/CODEOWNERS", Line: 6}); !reflect.DeepEqual(got.Ownership, want) {
		t.Errorf("ownership of an unowned file = %+v, want line 6, no owners", got.Ownership)
	}
	repo.Owners = nil
	if got, _ = Preview(repo, Ref{PlanID: 1, RepoID: "r", Path: "src/payments/legacy/retry.py"}, nil, 2); got.Ownership != nil {
		t.Errorf("ownership without CODEOWNERS = %+v, want none", got.Ownership)
	}
}
