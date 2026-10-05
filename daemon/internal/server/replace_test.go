package server

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
	"github.com/cruisinme30/spelunk/daemon/internal/testutil"
)

// byteOrderMark starts some UTF-8 files.
const byteOrderMark = "\xef\xbb\xbf"

// replaceClient indexes files in one repo, "payments-api", and returns a
// client and the repo's folder.
func replaceClient(t *testing.T, files map[string]string) (*testClient, string) {
	t.Helper()
	dir := testutil.WriteTree(t, files)
	client := newTestClient(t)
	client.mustInitialize(t, protocol.Root{ID: "r1", Path: dir, Name: "payments-api"})
	return client, dir
}

// planReplace calls replace/plan.
func planReplace(t *testing.T, client *testClient, text, replacement string) protocol.ReplacePlan {
	t.Helper()
	var plan protocol.ReplacePlan
	params := protocol.ReplacePlanParams{Text: text, Replacement: replacement}
	if err := client.call(protocol.MethodReplacePlan, params, &plan); err != nil {
		t.Fatalf("replace/plan %q: %v", text, err)
	}
	return plan
}

// A replace plan lists every match of the term on the lines the search
// shows, as UTF-16 ranges into the whole line, with the file's absolute
// path, and only in files the rest of the query keeps.
func TestReplacePlanListsEveryMatchInCurrentFiles(t *testing.T) {
	// @covers rpc:replace/plan
	client, dir := replaceClient(t, map[string]string{
		"src/policy.py": "class RetryPolicy:\n    pass\n\ndef make() -> \"RetryPolicy\": return RetryPolicy()\n",
		"src/notes.md":  "“RetryPolicy” is documented here\n",
		"docs/old.py":   "RetryPolicy = None\n",
	})
	plan := planReplace(t, client, "RetryPolicy -f:docs/", "BackoffPolicy")
	if plan.Matches != 4 || plan.Truncated || len(plan.Files) != 2 {
		t.Fatalf("plan = %d matches in %d files, truncated %v; want 4 in 2", plan.Matches, len(plan.Files), plan.Truncated)
	}
	byPath := map[string]protocol.ReplaceFile{}
	for _, file := range plan.Files {
		byPath[file.Path] = file
	}
	policy := byPath["src/policy.py"]
	if policy.File != filepath.Join(dir, "src", "policy.py") || policy.RepoID != "r1" {
		t.Fatalf("policy.py file = %q in %q", policy.File, policy.RepoID)
	}
	wantLines := []protocol.ReplaceLine{
		{Line: 1, Text: "class RetryPolicy:", Edits: []protocol.ReplaceEdit{{Start: 6, End: 17, NewText: "BackoffPolicy"}}},
		{Line: 4, Text: "def make() -> \"RetryPolicy\": return RetryPolicy()", Edits: []protocol.ReplaceEdit{
			{Start: 15, End: 26, NewText: "BackoffPolicy"}, {Start: 36, End: 47, NewText: "BackoffPolicy"},
		}},
	}
	if !reflect.DeepEqual(policy.Lines, wantLines) {
		t.Fatalf("policy.py lines = %+v, want %+v", policy.Lines, wantLines)
	}
	// “ is 3 bytes in UTF-8 but one UTF-16 unit.
	notes := byPath["src/notes.md"].Lines
	if len(notes) != 1 || notes[0].Edits[0].Start != 1 || notes[0].Edits[0].End != 12 {
		t.Fatalf("notes.md lines = %+v, want one edit at UTF-16 [1,12)", notes)
	}
}

// A /regex/ term's replacement fills in $1 and ${name} from each match;
// a literal term's replacement is inserted as written, $ and all.
func TestReplacePlanExpandsGroupsOfRegexTermsOnly(t *testing.T) {
	client, _ := replaceClient(t, map[string]string{
		"config.py": "max_attempts=3, max_delay=9\n",
	})
	plan := planReplace(t, client, `/max_(?P<what>\w+)=(\d)/`, "limit_${what}=$2")
	edits := plan.Files[0].Lines[0].Edits
	if len(edits) != 2 || edits[0].NewText != "limit_attempts=3" || edits[1].NewText != "limit_delay=9" {
		t.Fatalf("regex edits = %+v, want limit_attempts=3 and limit_delay=9", edits)
	}
	plan = planReplace(t, client, "max_attempts", "$1_tries")
	if got := plan.Files[0].Lines[0].Edits[0].NewText; got != "$1_tries" {
		t.Fatalf("literal replacement = %q, want $1_tries as written", got)
	}
}

// The plan follows the search's own matching: word:yes skips parts of
// words, and the offsets are into the line without its \r or a byte order
// mark, as an editor shows it.
func TestReplacePlanMatchesLikeTheSearch(t *testing.T) {
	client, _ := replaceClient(t, map[string]string{
		"a.py": byteOrderMark + "retry = autoretry(retry)\r\nretry_policy\r\n",
	})
	plan := planReplace(t, client, "word:yes retry", "attempt")
	lines := plan.Files[0].Lines
	want := []protocol.ReplaceLine{{Line: 1, Text: "retry = autoretry(retry)", Edits: []protocol.ReplaceEdit{
		{Start: 0, End: 5, NewText: "attempt"}, {Start: 18, End: 23, NewText: "attempt"},
	}}}
	if !reflect.DeepEqual(lines, want) {
		t.Fatalf("word:yes lines = %+v, want %+v", lines, want)
	}
}

// Past the limit, a plan has the count but no edits, so a replace is all
// or nothing.
func TestReplacePlanPastTheLimitHasNoEdits(t *testing.T) {
	client, _ := replaceClient(t, map[string]string{
		"big.txt": strings.Repeat("x ", maxReplaceMatches+1) + "\n",
	})
	plan := planReplace(t, client, "x", "y")
	if !plan.Truncated || len(plan.Files) != 0 || plan.Matches <= maxReplaceMatches {
		t.Fatalf("plan = %d matches, %d files, truncated %v; want over %d, none, true",
			plan.Matches, len(plan.Files), plan.Truncated, maxReplaceMatches)
	}
}

// Only a query with one text term that searches current files for code can
// be replaced.
func TestReplacePlanRefusesQueriesItCannotReplace(t *testing.T) {
	client, _ := replaceClient(t, map[string]string{"a.py": "retry backoff\n"})
	for _, text := range []string{
		"retry OR backoff",   // two terms
		"type:file retry",    // file names, not code
		"author:alice retry", // commits
		"f:a.py",             // no term
	} {
		err := client.call(protocol.MethodReplacePlan, protocol.ReplacePlanParams{Text: text, Replacement: "x"}, nil)
		wantRPCCode(t, "replace/plan "+text, err, protocol.CodeQueryInvalid)
	}
	// A term inside NOT only filters, so it isn't replaced.
	if plan := planReplace(t, client, "retry -backoff2", "x"); plan.Matches != 1 {
		t.Fatalf("retry -backoff2: %d matches, want 1", plan.Matches)
	}
}
