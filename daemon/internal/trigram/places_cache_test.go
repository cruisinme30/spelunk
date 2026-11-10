package trigram

import (
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/cruisinme30/spelunk/daemon/internal/query"
)

func TestPlacesCacheReusesImmutableRegions(t *testing.T) {
	// @covers op:in
	shard := NewShard([]Doc{{Path: "a.js", Lang: "javascript", Content: []byte("const text = `value ${call()}`;\r\n// call\r\n")}})
	doc := &shard.Docs[0]
	first := newFileMatcher(doc, termCache{}).inRegions()
	used := shard.places.used
	second := newFileMatcher(doc, termCache{}).inRegions()
	if !reflect.DeepEqual(first, second) || &first.regions[0] != &second.regions[0] {
		t.Fatal("repeated context reads did not share immutable regions")
	}
	if shard.places.used != used {
		t.Fatalf("second read charged %d bytes, want %d", shard.places.used, used)
	}
	if len(second.starts) != 2 || second.starts[1] != 32 {
		t.Errorf("LF line starts = %v, want [0 32]", second.starts)
	}
}

func TestPlacesCacheConcurrentReadsPublishOneValue(t *testing.T) {
	// @covers op:in
	shard := NewShard([]Doc{{Path: "a.go", Lang: "go", Content: []byte("// comment\nvar text = \"string\"\n")}})
	doc := &shard.Docs[0]
	var workers sync.WaitGroup
	values := make(chan *inText, 20)
	for range 20 {
		workers.Go(func() {
			matcher := newFileMatcher(doc, termCache{})
			values <- matcher.inRegions()
		})
	}
	workers.Wait()
	close(values)
	cached := shard.places.values[doc]
	for got := range values {
		if &got.regions[0] != &cached.regions[0] {
			t.Fatal("concurrent readers did not return the published regions")
		}
	}
	if len(shard.places.values) != 1 || shard.places.used > shard.places.limit {
		t.Fatalf("cache has %d entries using %d of %d bytes", len(shard.places.values), shard.places.used, shard.places.limit)
	}
}

func TestPlacesCacheBudgetFallsBackWithoutRetaining(t *testing.T) {
	// @covers op:in
	shard := NewShard([]Doc{{Path: "a.go", Lang: "go", Content: []byte("// comment\n")}})
	shard.places.limit = shard.places.used
	doc := &shard.Docs[0]
	first := newFileMatcher(doc, termCache{}).inRegions()
	second := newFileMatcher(doc, termCache{}).inRegions()
	if !reflect.DeepEqual(first, second) || len(shard.places.values) != 0 {
		t.Fatal("full cache changed results or retained an entry")
	}
	if &first.regions[0] == &second.regions[0] {
		t.Fatal("full cache unexpectedly reused an uncached region slice")
	}
}

func TestPlacesCacheDoesNotFollowChangedOverlayOrDisk(t *testing.T) {
	// @covers op:in
	old := NewShard([]Doc{{Path: "a.go", Lang: "go", Content: []byte("// needle\n")}})
	repo := Repo{ID: "r", Name: "r", Root: "/r", Shard: old}
	got, _ := run(t, "type:code needle in:comments", repo)
	if len(got) != 1 {
		t.Fatalf("original comments = %v, want one result", got)
	}
	changed := old.Docs[0]
	changed.Content = []byte("var needle = 1\n")
	repo.Overlay = NewShard([]Doc{changed})
	repo.Masked = map[string]bool{"a.go": true}
	if repo.Overlay.places == old.places {
		t.Fatal("overlay reused the old lexical cache")
	}
	if got, _ = run(t, "type:code needle in:code", repo); len(got) != 1 {
		t.Fatalf("changed code = %v, want one result", got)
	}
	if got, _ = run(t, "type:code needle in:comments", repo); len(got) != 0 {
		t.Fatalf("changed comments = %v, want no results", got)
	}
	path := filepath.Join(t.TempDir(), "shard")
	if err := old.Save(path); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.places == old.places || len(loaded.places.values) != 0 {
		t.Fatal("loaded shard reused or persisted runtime lexical data")
	}
	matcher := newFileMatcher(&loaded.Docs[0], termCache{})
	matcher.in = query.PlaceComment
	if !matcher.keeps(3, 9) {
		t.Fatal("freshly loaded shard lost comment classification")
	}
}
