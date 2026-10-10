package server

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cruisinme30/spelunk/daemon/internal/history"
	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
	"github.com/cruisinme30/spelunk/daemon/internal/testutil"
	"github.com/cruisinme30/spelunk/daemon/internal/trigram"
)

// backoffV1 is src/backoff.py as it was at v1.0.0; a later commit deleted it.
const backoffV1 = "import time\n\ndef parse_retry_after(header):\n    return int(header)\n"

// revisionRepo is payments-api with backoff.py added and tagged v1.0.0 in
// March, client.py changed in May, and backoff.py deleted in September.
func revisionRepo(t *testing.T) (root string, git func(args ...string) string) {
	t.Helper()
	root = gitRepo(t, gitCommit{
		author: "Jane Doe", subject: "Add backoff", at: time.Date(2026, 3, 14, 9, 0, 0, 0, time.UTC),
		files: map[string]string{
			"src/backoff.py": backoffV1,
			"src/client.py":  "delay = backoff.parse_retry_after(headers)\n",
			"README.md":      "payments\n",
		},
	})
	git = func(args ...string) string {
		t.Helper()
		cmd := exec.CommandContext(context.Background(), "git", append([]string{"-C", root}, args...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	commitAt := func(at time.Time, subject string) {
		t.Helper()
		date := at.Format(time.RFC3339)
		cmd := exec.CommandContext(context.Background(), "git", "-C", root, "commit", "--quiet", "-am", subject)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_DATE="+date, "GIT_COMMITTER_DATE="+date)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git commit: %v\n%s", err, out)
		}
	}
	git("tag", "v1.0.0")
	testutil.WriteFiles(t, root, map[string]string{"src/client.py": "delay = backoff.parse_retry_after(headers, cap=30)\n"})
	commitAt(time.Date(2026, 5, 2, 9, 0, 0, 0, time.UTC), "Cap the delay")
	git("rm", "--quiet", "src/backoff.py")
	commitAt(time.Date(2026, 9, 20, 9, 0, 0, 0, time.UTC), "Drop backoff")
	return root, git
}

// searchAt runs a search and returns its result and items.
func searchAt(t *testing.T, client *testClient, batches *batchLog, id, text string) (protocol.SearchResult, []protocol.ResultItem) {
	t.Helper()
	var result protocol.SearchResult
	if err := client.call(protocol.MethodSearchStart, protocol.SearchStartParams{SearchID: id, Text: text}, &result); err != nil {
		t.Fatalf("search %s: %v", text, err)
	}
	return result, batches.of(id)
}

// at: searches each repo's files at the revision: a file deleted since is
// found, results say how their file differs now, and the result names the
// commit each repo read.
func TestAtSearchesTheFilesOfARevision(t *testing.T) {
	// @covers op:at
	root, git := revisionRepo(t)
	client := newTestClient(t)
	batches := collectBatches(client)
	client.mustInitialize(t, protocol.Root{ID: "r1", Path: root, Name: "payments-api"})

	_, now := searchAt(t, client, batches, "now", "parse_retry_after")
	if len(now) != 1 || now[0].Path != "src/client.py" || now[0].SinceRevision != "" {
		t.Fatalf("current files = %+v, want only client.py", now)
	}
	result, items := searchAt(t, client, batches, "s1", "at:v1.0.0 parse_retry_after")
	var got []string
	for _, item := range items {
		got = append(got, item.Kind+" "+item.Path+" "+item.SinceRevision)
	}
	want := []string{"line src/backoff.py deleted", "line src/client.py changed"}
	if !slices.Equal(got, want) {
		t.Errorf("at:v1.0.0 parse_retry_after = %q, want %q", got, want)
	}
	sha := git("rev-parse", "v1.0.0")
	wantRevisions := []protocol.RevisionInfo{{RepoID: "r1", SHA: sha, Date: "2026-03-14T09:00:00Z"}}
	if !slices.Equal(result.Revisions, wantRevisions) {
		t.Errorf("revisions = %+v, want %+v", result.Revisions, wantRevisions)
	}
	// A search without at: says nothing about revisions.
	if result, _ := searchAt(t, client, batches, "s2", "parse_retry_after"); result.Revisions != nil {
		t.Errorf("a search without at: has revisions %+v", result.Revisions)
	}
}

func TestAtATimeSearchesTheNewestCommitMadeByThen(t *testing.T) {
	// @covers op:at
	root, git := revisionRepo(t)
	client := newTestClient(t)
	client.server.now = func() time.Time { return time.Date(2026, 10, 3, 10, 0, 0, 0, time.UTC) }
	batches := collectBatches(client)
	client.mustInitialize(t, protocol.Root{ID: "r1", Path: root, Name: "payments-api"})
	for _, tc := range []struct{ at, commit string }{
		{"6m", "v1.0.0"},         // April 3: before May's commit
		{"2026-05", "HEAD~1"},    // the end of May: after it
		{"2026-05-01", "v1.0.0"}, // the end of May 1: the day before
		{"yesterday", "HEAD"},    // everything
		{"2026-03-14", "v1.0.0"}, // the day of the first commit
		{"HEAD~1", "HEAD~1"},     // a revision, not a time
		{"main", "HEAD"},         // a branch, whatever its name
		{git("rev-parse", "--short", "HEAD~1"), "HEAD~1"},
	} {
		if tc.at == "main" {
			tc.at = git("branch", "--show-current")
		}
		result, _ := searchAt(t, client, batches, "s-"+tc.at, "at:"+tc.at+" parse_retry_after")
		if want := git("rev-parse", tc.commit); len(result.Revisions) != 1 || result.Revisions[0].SHA != want {
			t.Errorf("at:%s read %+v, want %s (%s)", tc.at, result.Revisions, tc.commit, want)
		}
	}
	result, items := searchAt(t, client, batches, "old", "at:2026-03-13 parse_retry_after")
	if len(items) != 0 || len(result.Revisions) != 1 || result.Revisions[0].SHA != "" ||
		result.Revisions[0].Problem != "payments-api has no commit from before Sat, Mar 14, 2026 00:00" {
		t.Errorf("at:2026-03-13 = %d results, revisions %+v, want none and why", len(items), result.Revisions)
	}
}

// A repo without the revision, or outside Git, is left out and says why;
// the others are searched.
func TestAtSaysWhichReposHaveNoSuchRevision(t *testing.T) {
	// @covers op:at failure:no-git
	tagged, git := revisionRepo(t)
	untagged := gitRepo(t, gitCommit{author: "Jason Kim", subject: "Start", at: time.Date(2026, 4, 1, 9, 0, 0, 0, time.UTC),
		files: map[string]string{"api.ts": "parse_retry_after()\n"}})
	plain := t.TempDir()
	testutil.WriteFiles(t, plain, map[string]string{"notes.txt": "parse_retry_after\n"})
	client := newTestClient(t)
	batches := collectBatches(client)
	client.mustInitialize(t,
		protocol.Root{ID: "r1", Path: tagged, Name: "payments-api"},
		protocol.Root{ID: "r2", Path: untagged, Name: "web-checkout"},
		protocol.Root{ID: "r3", Path: plain, Name: "notes"},
	)
	result, items := searchAt(t, client, batches, "s1", "at:v1.0.0 parse_retry_after")
	want := []protocol.RevisionInfo{
		{RepoID: "r1", SHA: git("rev-parse", "v1.0.0"), Date: "2026-03-14T09:00:00Z"},
		{RepoID: "r2", Problem: "v1.0.0 isn't a tag, branch or commit in web-checkout"},
		{RepoID: "r3", Problem: "notes isn't a Git repository, so it has no past revisions"},
	}
	if !slices.Equal(result.Revisions, want) {
		t.Errorf("revisions = %+v\nwant %+v", result.Revisions, want)
	}
	for _, item := range items {
		if item.RepoID != "r1" {
			t.Errorf("result %s in %s, want only payments-api's", item.Path, item.RepoID)
		}
	}
	// repo: leaves the other repos out altogether.
	result, _ = searchAt(t, client, batches, "s2", "at:v1.0.0 parse_retry_after repo:payments")
	if len(result.Revisions) != 1 || result.Revisions[0].RepoID != "r1" {
		t.Errorf("revisions with repo: = %+v, want payments-api's alone", result.Revisions)
	}
}

// A result at a revision previews and opens the file as it was, deleted
// since or not, and revision/text reads its whole text for the editor.
func TestAtResultsPreviewAndOpenAsTheyWere(t *testing.T) {
	// @covers op:at rpc:revision/text rpc:preview/get rpc:open/resolve failure:ref-stale
	root, git := revisionRepo(t)
	client := newTestClient(t)
	batches := collectBatches(client)
	client.mustInitialize(t, protocol.Root{ID: "r1", Path: root, Name: "payments-api"})
	_, items := searchAt(t, client, batches, "s1", "at:v1.0.0 parse_retry_after")
	deleted := items[0]

	var preview protocol.Preview
	if err := client.call(protocol.MethodPreviewGet, protocol.PreviewParams{Ref: deleted.Ref, ContextLines: 3}, &preview); err != nil {
		t.Fatal(err)
	}
	if preview.FocusLine != 3 || !slices.Contains(preview.Lines, "def parse_retry_after(header):") || len(preview.Hits) != 1 {
		t.Errorf("preview = %+v, want backoff.py as it was, focused on line 3", preview)
	}
	var target protocol.OpenTarget
	if err := client.call(protocol.MethodOpenResolve, protocol.OpenResolveParams{Ref: deleted.Ref}, &target); err != nil {
		t.Fatal(err)
	}
	want := protocol.OpenTarget{
		Path: filepath.Join(root, "src", "backoff.py"), Line: 3, Column: 5, Length: 17,
		Revision: git("rev-parse", "v1.0.0"), RevisionLabel: "v1.0.0",
	}
	if target != want {
		t.Errorf("open/resolve = %+v\nwant %+v", target, want)
	}
	var text protocol.RevisionText
	if err := client.call(protocol.MethodRevisionText, protocol.RevisionTextParams{Ref: deleted.Ref}, &text); err != nil {
		t.Fatal(err)
	}
	if text.Text != backoffV1 {
		t.Errorf("revision/text = %q, want %q", text.Text, backoffV1)
	}

	// A ref to a file the revision doesn't have is stale, and so is
	// revision/text for a current file's ref.
	missing := strings.Replace(deleted.Ref, "src/backoff.py", "src/missing.py", 1)
	for method, params := range map[string]any{
		protocol.MethodPreviewGet:            protocol.PreviewParams{Ref: missing, ContextLines: 3},
		protocol.MethodOpenResolve:           protocol.OpenResolveParams{Ref: missing},
		protocol.MethodRevisionText:          protocol.RevisionTextParams{Ref: missing},
		protocol.MethodRevisionText + " now": protocol.RevisionTextParams{Ref: strings.Replace(deleted.Ref, "@"+want.Revision, "", 1)},
	} {
		err := client.call(strings.TrimSuffix(method, " now"), params, nil)
		wantRPCCode(t, method+" of a missing file", err, protocol.CodeRefStale)
	}
}

// Closing a folder drops the revisions read from it, and their memory.
func TestAtForgetsTheRevisionsOfAClosedFolder(t *testing.T) {
	root, _ := revisionRepo(t)
	client := newTestClient(t)
	batches := collectBatches(client)
	client.mustInitialize(t, protocol.Root{ID: "r1", Path: root, Name: "payments-api"})
	searchAt(t, client, batches, "s1", "at:v1.0.0 parse")
	other := protocol.Root{ID: "r2", Path: t.TempDir(), Name: "web-checkout"}
	client.server.applyRoots([]protocol.Root{{ID: "r1", Path: root, Name: "payments-api"}, other})
	if n := len(client.server.revisions.entries); n != 1 {
		t.Errorf("with the folder still open the cache holds %d revisions, want 1", n)
	}
	client.server.applyRoots([]protocol.Root{other})
	if n := len(client.server.revisions.entries); n != 0 {
		t.Errorf("after closing the folder the cache holds %d revisions, want none", n)
	}
}

// A result's preview, open and text come from the files its search read,
// without asking Git again.
func TestAtResultsReuseTheRevisionTheSearchRead(t *testing.T) {
	root, _ := revisionRepo(t)
	client := newTestClient(t)
	batches := collectBatches(client)
	client.mustInitialize(t, protocol.Root{ID: "r1", Path: root, Name: "payments-api"})
	_, items := searchAt(t, client, batches, "s1", "at:v1.0.0 parse_retry_after")
	// Git still holds the old text, so only the kept shard can answer this.
	const kept = "kept by the search\n"
	docs := client.server.revisions.entries[0].shard.Docs
	for i := range docs {
		if docs[i].Path == "src/backoff.py" {
			docs[i].Content = []byte(kept)
		}
	}
	var text protocol.RevisionText
	if err := client.call(protocol.MethodRevisionText, protocol.RevisionTextParams{Ref: items[0].Ref}, &text); err != nil {
		t.Fatal(err)
	}
	if text.Text != kept {
		t.Errorf("revision/text = %q, want the kept shard's %q", text.Text, kept)
	}
}

// Once a search has read a revision, the next search of it reads nothing
// again; another revision is read anew.
func TestAtKeepsTheRevisionsItRead(t *testing.T) {
	root, _ := revisionRepo(t)
	client := newTestClient(t)
	batches := collectBatches(client)
	client.mustInitialize(t, protocol.Root{ID: "r1", Path: root, Name: "payments-api"})
	searchAt(t, client, batches, "s1", "at:v1.0.0 parse")
	first := client.server.revisions.entries[0].shard
	searchAt(t, client, batches, "s2", "at:v1.0.0 retry")
	if len(client.server.revisions.entries) != 1 || client.server.revisions.entries[0].shard != first {
		t.Errorf("the second search of v1.0.0 read it again")
	}
	searchAt(t, client, batches, "s3", "at:HEAD~1 retry")
	searchAt(t, client, batches, "s4", "at:HEAD retry")
	entries := client.server.revisions.entries
	if len(entries) != keptRevisions || entries[0].shard == first || entries[1].shard == first {
		t.Errorf("after two more revisions the cache holds %d, want the %d newest without v1.0.0", len(entries), keptRevisions)
	}
}

// Each repo keeps its own revisions: with more repos than keptRevisions,
// searching one revision again reads none of them anew.
func TestAtKeepsEachReposRevisions(t *testing.T) {
	client := newTestClient(t)
	batches := collectBatches(client)
	var roots []protocol.Root
	for _, id := range []string{"r1", "r2", "r3"} {
		root, _ := revisionRepo(t)
		roots = append(roots, protocol.Root{ID: id, Path: root, Name: "payments-" + id})
	}
	client.mustInitialize(t, roots...)
	searchAt(t, client, batches, "s1", "at:v1.0.0 parse")
	first := slices.Clone(client.server.revisions.entries)
	searchAt(t, client, batches, "s2", "at:v1.0.0 retry")
	if again := client.server.revisions.entries; len(again) != len(roots) || !sameShards(first, again) {
		t.Errorf("the second search of v1.0.0 read %d repos' revisions again", len(roots))
	}
}

// sameShards reports whether two lists of entries hold the same shards, in any order.
func sameShards(a, b []*revisionEntry) bool {
	held := map[*trigram.Shard]bool{}
	for _, entry := range a {
		held[entry.shard] = true
	}
	return len(a) == len(b) && !slices.ContainsFunc(b, func(entry *revisionEntry) bool { return !held[entry.shard] })
}

// A revision that falls out of the cache while it is being read stops
// being read.
func TestAtStopsReadingADroppedRevision(t *testing.T) {
	var cache revisionCache
	key := func(sha string) revisionKey { return revisionKey{root: "/r", sha: sha} }
	stopped := make(chan string, 1)
	cache.entry(context.Background(), key("a"), func(ctx context.Context, _ *revisionEntry) {
		<-ctx.Done()
		stopped <- "a"
	})
	done := func(context.Context, *revisionEntry) {}
	cache.entry(context.Background(), key("b"), done)
	cache.entry(context.Background(), key("c"), done)
	select {
	case sha := <-stopped:
		if sha != "a" {
			t.Errorf("stopped %s, want a", sha)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the read of a, dropped for b and c, kept running")
	}
}

// The index's settings choose a revision's files as they choose the
// current ones: index.exclude and index.maxFileSizeKB leave files out.
func TestAtReadsTheFilesTheIndexWould(t *testing.T) {
	root, _ := revisionRepo(t)
	client := newTestClient(t)
	batches := collectBatches(client)
	client.mustInitialize(t, protocol.Root{ID: "r1", Path: root, Name: "payments-api"})
	settings := client.server.Settings()
	settings.Exclude = append(settings.Exclude, "src/backoff.py")
	client.server.applySettings(settings)
	if _, items := searchAt(t, client, batches, "s1", "at:v1.0.0 parse_retry_after"); len(items) != 1 || items[0].Path != "src/client.py" {
		t.Errorf("with backoff.py excluded = %+v, want client.py alone", items)
	}
}

func TestReplaceRefusesAQueryAtARevision(t *testing.T) {
	// @covers rpc:replace/plan op:at
	root, _ := revisionRepo(t)
	client := newTestClient(t)
	client.mustInitialize(t, protocol.Root{ID: "r1", Path: root, Name: "payments-api"})
	err := client.call(protocol.MethodReplacePlan, protocol.ReplacePlanParams{Text: "at:v1.0.0 parse_retry_after", Replacement: "x"}, nil)
	wantRPCCode(t, "replace at a revision", err, protocol.CodeQueryInvalid)
	if err == nil || !strings.Contains(err.Error(), "replace changes current files, not the files at:v1.0.0") {
		t.Errorf("replace at a revision = %v", err)
	}
}

// at: suggests the open repos' tags, newest first, each with the repos
// that have it.
func TestAtSuggestsTheOpenReposTags(t *testing.T) {
	// @covers op:at rpc:query/parse
	root, git := revisionRepo(t)
	git("tag", "v0.9.0", "HEAD~2")
	cmd := exec.CommandContext(context.Background(), "git", "-C", root, "tag", "-a", "-m", "release", "v1.1.0", "HEAD")
	cmd.Env = append(os.Environ(), "GIT_COMMITTER_DATE=2026-09-21T09:00:00Z")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git tag: %v\n%s", err, out)
	}
	client := newTestClient(t)
	client.mustInitialize(t, protocol.Root{ID: "r1", Path: root, Name: "payments-api"})
	var result protocol.ParseResult
	if err := client.call(protocol.MethodQueryParse, protocol.ParseParams{Text: "retry at:v", Cursor: 10}, &result); err != nil {
		t.Fatal(err)
	}
	var labels []string
	for _, c := range result.Completions {
		labels = append(labels, c.Label+" "+c.Note)
	}
	// v1.0.0 and v0.9.0 are lightweight tags of the same March commit.
	if len(labels) < 3 || labels[0] != "v1.1.0 payments-api" || !slices.Contains(labels[1:3], "v1.0.0 payments-api") ||
		!slices.Contains(labels[1:3], "v0.9.0 payments-api") {
		t.Errorf("at:v suggests %q, want v1.1.0 first, then v1.0.0 and v0.9.0", labels)
	}
}

// Today's CODEOWNERS file can't speak for a past revision's files: results
// at a revision and their previews name no owners.
func TestAtResultsNameNoOwners(t *testing.T) {
	// @covers op:at
	root, _ := revisionRepo(t)
	testutil.WriteFiles(t, root, map[string]string{".github/CODEOWNERS": "* @acme/payments-core\n"})
	client := newTestClient(t)
	batches := collectBatches(client)
	client.mustInitialize(t, protocol.Root{ID: "r1", Path: root, Name: "payments-api"})
	_, now := searchAt(t, client, batches, "now", "parse_retry_after")
	if len(now) != 1 || len(now[0].Owners) != 1 {
		t.Fatalf("current results = %+v, want client.py owned by the team", now)
	}
	_, items := searchAt(t, client, batches, "s1", "at:v1.0.0 parse_retry_after")
	for _, item := range items {
		if item.Owners != nil {
			t.Errorf("%s at v1.0.0 names owners %v", item.Path, item.Owners)
		}
		var preview protocol.Preview
		if err := client.call(protocol.MethodPreviewGet, protocol.PreviewParams{Ref: item.Ref, ContextLines: 1}, &preview); err != nil {
			t.Fatal(err)
		}
		if preview.Ownership != nil {
			t.Errorf("the preview of %s at v1.0.0 names owners %+v", item.Path, preview.Ownership)
		}
	}
}

// The repos of a workspace are read at a revision in parallel: each read
// here waits until every repo's read has started, which reads one after
// another never reach.
func TestAtReadsTheReposRevisionsInParallel(t *testing.T) {
	client := newTestClient(t)
	batches := collectBatches(client)
	var roots []protocol.Root
	for _, id := range []string{"r1", "r2", "r3"} {
		root, _ := revisionRepo(t)
		roots = append(roots, protocol.Root{ID: id, Path: root, Name: "payments-" + id})
	}
	client.mustInitialize(t, roots...)
	var started sync.WaitGroup
	started.Add(len(roots))
	client.server.revisions.read = func(ctx context.Context, root string, revision history.Revision, settings protocol.Settings) (*trigram.Shard, error) {
		started.Done()
		all := make(chan struct{})
		go func() { started.Wait(); close(all) }()
		select {
		case <-all:
		case <-time.After(5 * time.Second):
			return nil, errors.New("the other repos' reads never started")
		}
		return readRevision(ctx, root, revision, settings)
	}
	result, items := searchAt(t, client, batches, "s1", "at:v1.0.0 parse_retry_after")
	for _, info := range result.Revisions {
		if info.Problem != "" {
			t.Errorf("%s: %s", info.RepoID, info.Problem)
		}
	}
	if len(items) != 2*len(roots) {
		t.Errorf("found %d results, want backoff.py and client.py in each of %d repos", len(items), len(roots))
	}
	if ids := revisionRepoIDs(result.Revisions); !slices.Equal(ids, []string{"r1", "r2", "r3"}) {
		t.Errorf("revisions are listed for %v, want the repos in order", ids)
	}
}

// revisionRepoIDs lists the repos a search's revisions are for.
func revisionRepoIDs(infos []protocol.RevisionInfo) []string {
	var ids []string
	for _, info := range infos {
		ids = append(ids, info.RepoID)
	}
	return ids
}
