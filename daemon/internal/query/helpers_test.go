package query

// Test helpers shared by this package's test files.

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cruisinme30/unified-search/daemon/internal/protocol"
)

// fixedNow is the clock for every test, so relative dates are stable.
var fixedNow = time.Date(2026, 10, 3, 10, 0, 0, 0, time.UTC)

// defaultSettings are the extension's default search settings.
var defaultSettings = protocol.Settings{DefaultCount: 500, HistoryDepth: "2y"}

// testResolver knows a small workspace's authors and repos.
var testResolver = fakeResolver{
	authors: []AuthorStat{
		{Name: "Jane Doe", Emails: []string{"jane@payments.example", "jdoe@old.example"}, Commits: 214, Repos: []string{"payments-api", "shared-libs"}, LastAt: "2026-09-30T10:00:00Z"},
		{Name: "Jason Kim", Emails: []string{"jason@checkout.example"}, Commits: 88, Repos: []string{"web-checkout"}, LastAt: "2026-09-26T10:00:00Z"},
		{Name: "Marta Ruiz", Emails: []string{"jamarta@libs.example"}, Commits: 12, Repos: []string{"shared-libs"}, LastAt: "2026-08-03T10:00:00Z"},
	},
	repos: []string{"payments-api", "web-checkout", "shared-libs"},
}

// fakeResolver answers from fixed lists of authors and repos.
type fakeResolver struct {
	authors []AuthorStat
	repos   []string
}

// Authors returns up to limit authors whose name or email contains fragment.
func (r fakeResolver) Authors(fragment string, limit int) []AuthorStat {
	var found []AuthorStat
	for _, a := range r.authors {
		haystack := strings.ToLower(a.Name + " " + strings.Join(a.Emails, " "))
		if strings.Contains(haystack, strings.ToLower(fragment)) && len(found) < limit {
			found = append(found, a)
		}
	}
	return found
}

// RepoNames returns every repo name.
func (r fakeResolver) RepoNames() []string { return r.repos }

// codes lists the codes of diagnostics, in order.
func codes(diagnostics []protocol.Diagnostic) []string {
	list := []string{}
	for _, d := range diagnostics {
		list = append(list, d.Code)
	}
	return list
}

// mustParseCleanly parses text, failing the test if it has any diagnostic.
func mustParseCleanly(t *testing.T, text string) protocol.ParsedQuery {
	t.Helper()
	q := Parse(text, testResolver)
	if len(q.Diagnostics) > 0 {
		t.Fatalf("Parse(%q) diagnostics = %v, want none", text, codes(q.Diagnostics))
	}
	return q
}

// applyFix applies a fix's edits (UTF-16 spans) to text.
func applyFix(text string, fix protocol.Fix) string {
	src := newSource(text)
	var b strings.Builder
	position := 0
	// Edits in a fix never overlap; apply them in span order.
	edits := slices.Clone(fix.Edits)
	slices.SortFunc(edits, func(a, b protocol.TextEdit) int { return a.Span.Start - b.Span.Start })
	for _, edit := range edits {
		b.WriteString(src.slice(position, edit.Span.Start))
		b.WriteString(edit.NewText)
		position = edit.Span.End
	}
	b.WriteString(src.slice(position, src.length()))
	return b.String()
}
