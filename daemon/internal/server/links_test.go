package server

import (
	"fmt"
	"testing"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
)

// linkFor asks link/resolve about one ref.
func linkFor(t *testing.T, client *testClient, ref string) protocol.Link {
	t.Helper()
	var links protocol.Links
	if err := client.call(protocol.MethodLinkResolve, protocol.LinkParams{Refs: []string{ref}}, &links); err != nil {
		t.Fatalf("link/resolve %s: %v", ref, err)
	}
	if len(links.Links) != 1 {
		t.Fatalf("link/resolve %s = %+v, want one link", ref, links)
	}
	return links.Links[0]
}

// A pushed repo: its remotes, and links to a code line, an at: result
// and a commit result.
func TestLinkResolveNamesTheCommitPathAndLine(t *testing.T) {
	// @covers rpc:repo/remotes rpc:link/resolve
	root, git := revisionRepo(t)
	git("remote", "add", "origin", "git@github.com:acme/payments-api.git")
	git("update-ref", "refs/remotes/origin/main", "HEAD")
	client := newTestClient(t)
	batches := collectBatches(client)
	client.mustInitialize(t, protocol.Root{ID: "r1", Path: root, Name: "payments-api"})

	var remotes protocol.RepoRemotes
	if err := client.call(protocol.MethodRepoRemotes, protocol.RepoRemotesParams{RepoID: "r1"}, &remotes); err != nil {
		t.Fatal(err)
	}
	want := protocol.Remote{Name: "origin", URL: "git@github.com:acme/payments-api.git"}
	if !remotes.Git || remotes.Preferred != "origin" || len(remotes.Remotes) != 1 || remotes.Remotes[0] != want {
		t.Fatalf("repo/remotes = %+v", remotes)
	}

	head := git("rev-parse", "HEAD")
	_, items := searchAt(t, client, batches, "s1", "cap=30")
	if len(items) != 1 || items[0].Kind != "line" {
		t.Fatalf("cap=30 = %+v", items)
	}
	link := linkFor(t, client, items[0].Ref)
	if link.Problem != "" || link.Commit != head || link.Path != "src/client.py" || link.Line != 1 || *link.Remote != want {
		t.Fatalf("code line: %+v", link)
	}

	_, past := searchAt(t, client, batches, "s2", "at:v1.0.0 parse_retry_after")
	tag := git("rev-parse", "v1.0.0")
	for _, item := range past {
		if link := linkFor(t, client, item.Ref); link.Commit != tag {
			t.Errorf("at: result %s links to %s, want %s", item.Path, link.Commit, tag)
		}
	}

	_, commits := searchAt(t, client, batches, "s3", "msg:\"Cap the delay\"")
	if len(commits) != 1 {
		t.Fatalf("commits = %+v", commits)
	}
	if link := linkFor(t, client, commits[0].Ref); link.Commit != commits[0].SHA || link.Problem != "" {
		t.Fatalf("commit: %+v", link)
	}

	// Many refs at once come back in their order, as an export asks.
	var links protocol.Links
	refs := []string{commits[0].Ref, items[0].Ref, past[0].Ref}
	if err := client.call(protocol.MethodLinkResolve, protocol.LinkParams{Refs: refs}, &links); err != nil {
		t.Fatal(err)
	}
	if len(links.Links) != 3 || links.Links[0].Commit != commits[0].SHA || links.Links[1].Commit != head || links.Links[2].Commit != tag {
		t.Fatalf("three refs: %+v", links)
	}
}

// Without a remote there is no link, and forged refs are refused as stale.
func TestLinkResolveRefusesNoRemoteAndForgedRefs(t *testing.T) {
	// @covers rpc:link/resolve rpc:repo/remotes failure:ref-stale
	root, _ := revisionRepo(t)
	client := newTestClient(t)
	batches := collectBatches(client)
	client.mustInitialize(t, protocol.Root{ID: "r1", Path: root, Name: "payments-api"})
	_, items := searchAt(t, client, batches, "s1", "cap=30")
	if link := linkFor(t, client, items[0].Ref); link.Problem != protocol.LinkProblemNoRemote {
		t.Fatalf("no remote: %+v", link)
	}
	for _, ref := range []string{"tree|1|r1|1|0|1|0|0|../../../etc/passwd", "tree|1|r2|1|0|0|0|0|src/client.py", "hist|1|r1|--output=x", "garbage"} {
		err := client.call(protocol.MethodLinkResolve, protocol.LinkParams{Refs: []string{ref}}, nil)
		wantRPCCode(t, fmt.Sprintf("link/resolve %q", ref), err, protocol.CodeRefStale)
	}
	err := client.call(protocol.MethodRepoRemotes, protocol.RepoRemotesParams{RepoID: "r2"}, nil)
	wantRPCCode(t, "repo/remotes r2", err, protocol.CodeRefStale)
}
