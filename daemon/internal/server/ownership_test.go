package server

import (
	"reflect"
	"testing"

	"github.com/cruisinme30/spelunk/daemon/internal/codeowners"
	"github.com/cruisinme30/spelunk/daemon/internal/trigram"
)

// A repo's owners are counted once per index and CODEOWNERS file: asking
// again returns the same counts, and a new index is counted anew.
func TestOwnershipIsCountedOncePerIndex(t *testing.T) {
	shard := trigram.NewShard([]trigram.Doc{{Path: "api/a.go"}, {Path: "api/b.go"}, {Path: "web/c.ts"}, {Path: "README.md"}})
	owners := codeowners.Parse("CODEOWNERS", []byte("/api/ @alice @Web\n/web/ @web\n"))
	repo := trigram.Repo{ID: "r1", Shard: shard, Owners: owners}
	var cache ownershipCache

	got := cache.of(&repo)
	want := ownership{owners: []ownerFiles{{name: "@alice", files: 2}, {name: "@Web", files: 3}}, unowned: 1}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ownership = %+v, want %+v", got, want)
	}
	if again := cache.of(&repo); &again.owners[0] != &got.owners[0] {
		t.Error("the same index and CODEOWNERS file were counted again")
	}
	repo.Shard = trigram.NewShard([]trigram.Doc{{Path: "web/d.ts"}})
	if fresh := cache.of(&repo); !reflect.DeepEqual(fresh, ownership{owners: []ownerFiles{{name: "@web", files: 1}}}) {
		t.Errorf("after a new index, ownership = %+v", fresh)
	}
	if none := cache.of(&trigram.Repo{ID: "r2", Shard: shard}); !reflect.DeepEqual(none, ownership{}) {
		t.Errorf("a repo without CODEOWNERS has ownership %+v", none)
	}
}

// The cache lets go of a repo's index once the repo has no CODEOWNERS
// file, or is closed.
func TestOwnershipForgetsReposWithoutOwnersOrClosed(t *testing.T) {
	owners := codeowners.Parse("CODEOWNERS", []byte("* @alice\n"))
	shard := trigram.NewShard([]trigram.Doc{{Path: "a.go"}})
	var cache ownershipCache
	cache.of(&trigram.Repo{ID: "r1", Shard: shard, Owners: owners})
	cache.of(&trigram.Repo{ID: "r2", Shard: shard, Owners: owners})
	cache.of(&trigram.Repo{ID: "r1", Shard: shard}) // its CODEOWNERS file was deleted
	cache.keep(map[string]bool{"r1": true})         // r2 was closed
	if len(cache.byRepo) != 0 {
		t.Errorf("the cache still holds %d repos, want none", len(cache.byRepo))
	}
}
