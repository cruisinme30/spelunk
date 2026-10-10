package engine

import (
	"reflect"
	"regexp"
	"testing"
	"time"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
	"github.com/cruisinme30/spelunk/daemon/internal/query"
)

func TestFacetsListBiggestBucketsFirstWithTheirFilters(t *testing.T) {
	f := NewFacets(FacetRepo, FacetLang, FacetFolder, FacetAuthor, FacetMonth)
	for _, path := range []string{"src/a.py", "src/b.py", "README.md", "http/c.py"} {
		f.CountRepo("payments-api")
		f.CountFolder(path)
	}
	f.CountRepo("shared.libs")
	f.CountLang("python")
	f.CountLang("python")
	f.CountLang("markdown")
	f.CountLang("") // unknown languages aren't counted
	f.CountAuthor("Jane Doe")
	sept := time.Date(2026, 9, 30, 23, 0, 0, 0, time.UTC)
	f.CountMonth(sept, time.UTC)
	f.CountMonth(sept, time.FixedZone("UTC+2", 2*60*60)) // October there

	want := []protocol.Facet{
		{Field: FacetRepo, Buckets: []protocol.FacetBucket{
			{Label: "payments-api", Count: 4, Filter: "repo:^payments-api$"},
			{Label: "shared.libs", Count: 1, Filter: `repo:^shared\.libs$`},
		}},
		{Field: FacetLang, Buckets: []protocol.FacetBucket{
			{Label: "Python", Count: 2, Filter: "lang:python"},
			{Label: "Markdown", Count: 1, Filter: "lang:markdown"},
		}},
		{Field: FacetFolder, Buckets: []protocol.FacetBucket{
			{Label: "src/", Count: 2, Filter: "f:^src/"},
			{Label: "http/", Count: 1, Filter: "f:^http/"},
		}},
		{Field: FacetAuthor, Buckets: []protocol.FacetBucket{
			{Label: "Jane Doe", Count: 1, Filter: `author:"Jane Doe"`},
		}},
		{Field: FacetMonth, Buckets: []protocol.FacetBucket{
			{Label: "Oct 2026", Count: 1, Filter: "since:2026-10 until:2026-10"},
			{Label: "Sep 2026", Count: 1, Filter: "since:2026-09 until:2026-09"},
		}},
	}
	if got := f.List(); !reflect.DeepEqual(got, want) {
		t.Errorf("List() =\n %+v\nwant\n %+v", got, want)
	}
}

func TestFacetsKeepTheBiggestBucketsAndCountTheRest(t *testing.T) {
	f := NewFacets(FacetFolder, FacetRepo)
	for i := range maxFacetBuckets + 3 {
		for range i + 1 {
			f.CountFolder(string(rune('a'+i)) + "/x")
		}
	}
	got := f.List()
	if len(got) != 1 {
		t.Fatalf("List() = %+v, want only the folder facet: repo counted nothing", got)
	}
	if len(got[0].Buckets) != maxFacetBuckets || got[0].More != 3 {
		t.Errorf("folder facet has %d buckets and %d more, want %d and 3", len(got[0].Buckets), got[0].More, maxFacetBuckets)
	}
	if first := got[0].Buckets[0]; first.Label != "k/" || first.Count != maxFacetBuckets+3 {
		t.Errorf("first bucket = %+v, want the biggest, k/", first)
	}
}

// A repo or folder bucket's filter parses back as one operator matching
// that name, even with a space, quote or parenthesis in it, and a repo's
// matches no other repo whose name holds it.
func TestRepoAndFolderFiltersParseBackWhole(t *testing.T) {
	for _, name := range []string{"payments-api", "My Project", "legacy (old)", `say "hi"`, "a/b c"} {
		f := NewFacets(FacetRepo, FacetFolder)
		f.CountRepo(name)
		f.CountFolder(name + "/x.go")
		for _, facet := range f.List() {
			filter := facet.Buckets[0].Filter
			plan, problems, err := query.NewPlan(query.Parse(filter, nil), protocol.Settings{}, time.Now(), "")
			if err != nil || len(problems) > 0 {
				t.Fatalf("%s: %v %v", filter, err, problems)
			}
			and := plan.Pred
			if len(and.Kids) != 1 {
				t.Fatalf("%s plans %#v, want one operator", filter, plan.Pred)
			}
			var re *regexp.Regexp
			want := name
			switch pred := and.Kids[0].(type) {
			case *query.Repo:
				re = pred.Re
				if re.MatchString("payments-" + name) {
					t.Errorf("%s matches payments-%s too", filter, name)
				}
			case *query.Path:
				re, want = pred.Re, name+"/x.go"
			default:
				t.Fatalf("%s plans %T", filter, pred)
			}
			if !re.MatchString(want) {
				t.Errorf("%s doesn't match %q", filter, want)
			}
		}
	}
}
