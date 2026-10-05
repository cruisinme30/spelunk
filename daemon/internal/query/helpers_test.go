package query

// Test helpers shared by this package's test files.

import (
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
	repos: []RepoStat{
		{Name: "payments-api", Path: "/work/payments-api", Files: 7, State: protocol.IndexStateReady},
		{Name: "web-checkout", Path: "/work/web-checkout", Files: 3, State: protocol.IndexStateReady},
		{Name: "shared-libs", Path: "/work/shared-libs", Files: 4, State: protocol.IndexStateIndexing, Progress: 0.64},
	},
	symbols: []SymbolStat{
		{Name: "RetryPolicy", Kind: protocol.SymbolKindClass, Definitions: 2, Repos: []string{"payments-api", "web-checkout"}},
		{Name: "RetryPolicyConfig", Kind: protocol.SymbolKindClass, Definitions: 1, Repos: []string{"shared-libs"}},
	},
	words: []WordStat{
		{Text: "fix flaky", Commits: 12, LastAt: fixedNow.AddDate(0, 0, -2)},
		{Text: "retry", Commits: 17, LastAt: fixedNow.AddDate(0, 0, -3)},
		{Text: "timeout", Commits: 11, LastAt: fixedNow.AddDate(0, 0, -5)},
	},
	files: []FileStat{
		{Repo: "payments-api", Path: "src/payments/client.py", Lang: "python", ModTime: fixedNow.Add(-20 * time.Minute)},
		{Repo: "payments-api", Path: "src/payments/retry_policy.py", Lang: "python", ModTime: fixedNow.Add(-3 * time.Hour)},
		{Repo: "payments-api", Path: "src/payments/errors.py", Lang: "python", ModTime: fixedNow.AddDate(0, 0, -10)},
		{Repo: "payments-api", Path: "tests/payments/client_test.py", Lang: "python", ModTime: fixedNow.AddDate(0, -2, 0)},
		{Repo: "payments-api", Path: "tests/payments/retry_policy_test.py", Lang: "python", ModTime: fixedNow.AddDate(0, -2, 0)},
		{Repo: "payments-api", Path: "scripts/retry_failed_webhooks.py", Lang: "python", ModTime: fixedNow.AddDate(-2, 0, 0)},
		{Repo: "payments-api", Path: "README.md", Lang: "markdown", ModTime: fixedNow.AddDate(-2, 0, 0)},
		{Repo: "web-checkout", Path: "src/api/checkout.ts", Lang: "typescript", ModTime: fixedNow.AddDate(0, 0, -1)},
		{Repo: "web-checkout", Path: "e2e/checkout_test.py", Lang: "python", ModTime: fixedNow.AddDate(0, 0, -20)},
		{Repo: "web-checkout", Path: "package.json", Lang: "json", ModTime: fixedNow.AddDate(0, -8, 0)},
		{Repo: "shared-libs", Path: "http/retry.py", Lang: "python", ModTime: fixedNow.AddDate(0, 0, -3)},
		{Repo: "shared-libs", Path: "http/config.py", Lang: "python", ModTime: fixedNow.AddDate(0, 0, -3)},
		{Repo: "shared-libs", Path: "http/config.yaml", Lang: "yaml", ModTime: fixedNow.AddDate(0, -1, -5)},
		{Repo: "shared-libs", Path: "docs/retry_policy.md", Lang: "markdown", ModTime: fixedNow.AddDate(0, -7, 0)},
	},
}

// fakeResolver answers from fixed lists of authors, repos and files.
type fakeResolver struct {
	authors []AuthorStat
	repos   []RepoStat
	files   []FileStat
	words   []WordStat
	symbols []SymbolStat
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

// Repos returns every repo.
func (r fakeResolver) Repos() []RepoStat { return r.repos }

// Files returns every file.
func (r fakeResolver) Files() []FileStat { return r.files }

// MessageWords returns the words that start with fragment.
func (r fakeResolver) MessageWords(fragment string, limit int) []WordStat {
	var found []WordStat
	for _, w := range r.words {
		if strings.HasPrefix(w.Text, fragment) && len(found) < limit {
			found = append(found, w)
		}
	}
	return found
}

// Symbols returns the symbols whose name contains fragment.
func (r fakeResolver) Symbols(fragment string, limit int) []SymbolStat {
	var found []SymbolStat
	for _, s := range r.symbols {
		if strings.Contains(strings.ToLower(s.Name), fragment) && len(found) < limit {
			found = append(found, s)
		}
	}
	return found
}

// codes lists the codes of diagnostics, in order.
func codes(diagnostics []protocol.Diagnostic) []string {
	list := []string{}
	for _, d := range diagnostics {
		list = append(list, d.Code)
	}
	return list
}

// count is how many diagnostics have code.
func count(diagnostics []protocol.Diagnostic, code string) int {
	n := 0
	for _, d := range diagnostics {
		if d.Code == code {
			n++
		}
	}
	return n
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
