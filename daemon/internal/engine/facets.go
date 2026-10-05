package engine

import (
	"cmp"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/cruisinme30/spelunk/daemon/internal/lang"
	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
)

// The fields results are counted by, in the order the panel shows them.
const (
	FacetRepo   = "repo"
	FacetLang   = "lang"
	FacetFolder = "folder"
	FacetAuthor = "author"
	FacetMonth  = "month"
)

// maxFacetBuckets is how many buckets a facet lists; the rest are counted
// in its More.
const maxFacetBuckets = 8

// Facets counts a search's results by field, for the buttons above the
// results that narrow them. Each bucket carries the filter that keeps only
// its results, so the panel never has to build one.
type Facets struct {
	fields  []string
	buckets map[string]map[string]*protocol.FacetBucket // field → filter → bucket
}

// NewFacets counts results by fields, listed in that order.
func NewFacets(fields ...string) *Facets {
	f := &Facets{fields: fields, buckets: map[string]map[string]*protocol.FacetBucket{}}
	for _, field := range fields {
		f.buckets[field] = map[string]*protocol.FacetBucket{}
	}
	return f
}

// Count counts one result in field's bucket for label, which filter keeps.
func (f *Facets) Count(field, label, filter string) {
	buckets := f.buckets[field]
	if buckets == nil {
		return
	}
	bucket := buckets[filter]
	if bucket == nil {
		bucket = &protocol.FacetBucket{Label: label, Filter: filter}
		buckets[filter] = bucket
	}
	bucket.Count++
}

// List returns each field that has a bucket, biggest bucket first (ties by
// label) or, for months, newest first, with at most maxFacetBuckets buckets.
func (f *Facets) List() []protocol.Facet {
	var facets []protocol.Facet
	for _, field := range f.fields {
		buckets := make([]protocol.FacetBucket, 0, len(f.buckets[field]))
		for _, bucket := range f.buckets[field] {
			buckets = append(buckets, *bucket)
		}
		if len(buckets) == 0 {
			continue
		}
		slices.SortFunc(buckets, func(a, b protocol.FacetBucket) int {
			if field == FacetMonth {
				return cmp.Compare(b.Filter, a.Filter) // since:2026-10 … before since:2026-09 …
			}
			return cmp.Or(cmp.Compare(b.Count, a.Count), cmp.Compare(a.Label, b.Label))
		})
		facet := protocol.Facet{Field: field, Buckets: buckets}
		if len(buckets) > maxFacetBuckets {
			facet.Buckets, facet.More = buckets[:maxFacetBuckets], len(buckets)-maxFacetBuckets
		}
		facets = append(facets, facet)
	}
	return facets
}

// CountRepo counts a result in its repo's bucket: repo:payments-api, the
// filter the repo menu writes.
func (f *Facets) CountRepo(name string) {
	f.Count(FacetRepo, name, "repo:"+regexp.QuoteMeta(name))
}

// CountLang counts a result in its language's bucket: lang:python. Files
// of no known language aren't counted.
func (f *Facets) CountLang(name string) {
	if name != "" {
		f.Count(FacetLang, lang.Title(name), "lang:"+name)
	}
}

// CountFolder counts a result in the bucket of the folder at the top of
// its repo-relative path: f:^src/. Files at the repo root aren't counted.
func (f *Facets) CountFolder(path string) {
	if folder, _, ok := strings.Cut(path, "/"); ok {
		f.Count(FacetFolder, folder+"/", "f:^"+regexp.QuoteMeta(folder)+"/")
	}
}

// CountAuthor counts a commit in its author's bucket: author:"Jane Doe",
// a phrase, so it matches the name exactly.
func (f *Facets) CountAuthor(name string) {
	if name != "" {
		f.Count(FacetAuthor, name, "author:"+strconv.Quote(name))
	}
}

// CountMonth counts a commit in the bucket of the calendar month it was
// made in, in loc: since:2026-09 until:2026-09 keeps September 2026.
func (f *Facets) CountMonth(at time.Time, loc *time.Location) {
	month := at.In(loc).Format("2006-01")
	f.Count(FacetMonth, at.In(loc).Format("Jan 2006"), "since:"+month+" until:"+month)
}
