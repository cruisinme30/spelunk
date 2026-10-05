package query

import (
	"strings"
	"testing"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
)

// shown renders completions one per line, as the panel lays them out:
// "[section] label | detail | context | note".
func shown(completions []protocol.Completion) string {
	var lines []string
	for _, c := range completions {
		line := c.Label + " | " + c.Detail + " | " + c.Context + " | " + c.Note
		if c.Section != "" {
			line = "[" + c.Section + "] " + line
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func TestSinceSuggestsWindowsWithTheirStartAndChangedFiles(t *testing.T) {
	// @covers screen:since-values
	text := "timeout since:"
	want := strings.Join([]string{
		"[Calendar days] today | Today | Since midnight | 2 files changed",
		"yesterday | Yesterday and today | Since Fri, Oct 2, 00:00 | 3 files changed",
		"[The last few hours] 30min | Last 30 minutes | Since 09:30 | 1 file changed",
		"2h | Last 2 hours | Since 08:00 | 1 file changed",
		"[Longer windows] 2w | Last 2 weeks | Since Sat, Sep 19 | 6 files changed",
		"30d | Last 30 days | Since Thu, Sep 3 | 7 files changed",
		"6m | Last 6 months | Since Fri, Apr 3 | 10 files changed",
		"1y | Last year | Since Fri, Oct 3, 2025 | 12 files changed",
	}, "\n")
	if got := shown(Complete(text, len(text), testResolver, fixedNow)); got != want {
		t.Errorf("Complete(%q) =\n%s\nwant\n%s", text, got, want)
	}
}

func TestSinceOffersEveryUnitForATypedNumber(t *testing.T) {
	tests := map[string]string{
		"since:3":    "3min 3h 3d 3w 3m 3y",
		"since:45mi": "45min",
		"since:2":    "2min 2h 2d 2w 2m 2y",
		"since:y":    "yesterday",
	}
	for text, want := range tests {
		if got := strings.Join(labels(text, len(text)), " "); got != want {
			t.Errorf("Complete(%q) = %q, want %q", text, got, want)
		}
	}
	completions := Complete("since:1", len("since:1"), testResolver, fixedNow)
	if got := completions[len(completions)-1].Detail; got != "Last year" {
		t.Errorf("since:1y reads %q, want %q", got, "Last year")
	}
	if got := completions[0].Note; got != "No files changed" {
		t.Errorf("since:1min note = %q, want %q", got, "No files changed")
	}
}

func TestPathSuggestsFileTypesThenFolders(t *testing.T) {
	// @covers screen:path-values
	text := "retry f:"
	want := strings.Join([]string{
		"[File types] *.py | Python files | 9 files · 3 repos | ",
		"*.md | Markdown files | 2 files · payments-api, shared-libs | ",
		"*.ts | TypeScript files | 1 file · web-checkout | ",
		"*.json | JSON files | 1 file · web-checkout | ",
		"*.yaml | YAML files | 1 file · shared-libs | ",
		"[Folders] src/ | Folder in payments-api, web-checkout | 4 files | ",
		"http/ | Folder in shared-libs | 3 files | ",
		"tests/ | Folder in payments-api | 2 files | ",
		"scripts/ | Folder in payments-api | 1 file | ",
		"e2e/ | Folder in web-checkout | 1 file | ",
	}, "\n")
	if got := shown(Complete(text, len(text), testResolver, fixedNow)); got != want {
		t.Errorf("Complete(%q) =\n%s\nwant\n%s", text, got, want)
	}
	if got := ApplyFix(text, Complete(text, len(text), testResolver, fixedNow)[0].Insert); got != "retry f:*.py " {
		t.Errorf("accepting *.py gives %q, want %q", got, "retry f:*.py ")
	}
}

func TestPathSuggestionsNarrowAsYouType(t *testing.T) {
	tests := map[string]string{
		"f:py":   "*.py",
		"f:*.y":  "*.yaml",
		"f:src/": "src/payments/ src/api/",
		"f:pay":  "src/payments/ tests/payments/",
	}
	for text, want := range tests {
		if got := strings.Join(labels(text, len(text)), " "); got != want {
			t.Errorf("Complete(%q) = %q, want %q", text, got, want)
		}
	}
}

func TestRepoSuggestionsShowPathSizeAndIndexState(t *testing.T) {
	// @covers screen:other-values
	want := strings.Join([]string{
		"payments-api | /work/payments-api | 7 files | Index ready",
		"web-checkout | /work/web-checkout | 3 files | Index ready",
		"shared-libs | /work/shared-libs | 4 files | Indexing 64%",
	}, "\n")
	if got := shown(Complete("repo:", len("repo:"), testResolver, fixedNow)); got != want {
		t.Errorf("Complete(repo:) =\n%s\nwant\n%s", got, want)
	}
}

func TestLanguageSuggestionsPutTheWorkspacesOwnFirst(t *testing.T) {
	want := strings.Join([]string{
		"[In this workspace] python | Python | .py · 9 files | 3 repos",
		"markdown | Markdown | .md · 2 files | payments-api, shared-libs",
		"typescript | TypeScript | .ts · 1 file | web-checkout",
		"json | JSON | .json · 1 file | web-checkout",
		"yaml | YAML | .yaml · 1 file | shared-libs",
	}, "\n")
	if got := shown(Complete("lang:", len("lang:"), testResolver, fixedNow)); got != want {
		t.Errorf("Complete(lang:) =\n%s\nwant\n%s", got, want)
	}
	if got := strings.Join(labels("lang:r", len("lang:r")), " "); got != "ruby rust" {
		t.Errorf("Complete(lang:r) = %q, want languages not in the workspace too", got)
	}
	if got := strings.Join(labels("lang:j", len("lang:j")), " "); got != "json java javascript" {
		t.Errorf("Complete(lang:j) = %q, want json first (in the workspace), then java and javascript", got)
	}
}

func TestFileStateSuggestionsCountTheirFiles(t *testing.T) {
	// @covers op:is
	want := strings.Join([]string{
		"open | Files open in the editor | client.py, retry_policy.py | 2 files",
		"changed | Files with uncommitted changes | client.py | 1 file",
		"test | Test files | test_*.py, *_test.go, *.spec.ts, tests/ folders | 3 files",
	}, "\n")
	if got := shown(Complete("is:", len("is:"), testResolver, fixedNow)); got != want {
		t.Errorf("Complete(is:) =\n%s\nwant\n%s", got, want)
	}
	outsideGit := fakeResolver{repos: []RepoStat{{Name: "notes"}}}
	if got, want := shown(Complete("is:c", len("is:c"), outsideGit, fixedNow)), "changed | Files with uncommitted changes | Edited, added or staged since the last commit | No Git repo"; got != want {
		t.Errorf("Complete(is:c) outside Git = %q, want %q", got, want)
	}
}

func TestFixedValuesExplainThemselves(t *testing.T) {
	completions := Complete("type:", len("type:"), testResolver, fixedNow)
	if got, want := shown(completions[:1]), "file | File names only | Paths that match, no code lines | "; got != want {
		t.Errorf("type:file = %q, want %q", got, want)
	}
	completions = Complete("order:", len("order:"), testResolver, fixedNow)
	if got, want := shown(completions[:1]), "best | Best match first | Definitions and file-name matches first; tests, vendored and generated files last | "; got != want {
		t.Errorf("order:best = %q, want %q", got, want)
	}
}

func TestMessageSuggestionsOfferPhrasesThenWords(t *testing.T) {
	want := strings.Join([]string{
		"[Phrases] fix flaky | fix flaky | In 12 commit messages | last 2 days ago",
		"[Words] retry | retry | In 17 commit messages | last 3 days ago",
		"timeout | timeout | In 11 commit messages | last 5 days ago",
	}, "\n")
	if got := shown(Complete("msg:", len("msg:"), testResolver, fixedNow)); got != want {
		t.Errorf("Complete(msg:) =\n%s\nwant\n%s", got, want)
	}
	completions := Complete("msg:", len("msg:"), testResolver, fixedNow)
	if got := ApplyFix("msg:", completions[0].Insert); got != `msg:"fix flaky" ` {
		t.Errorf("accepting a phrase gives %q, want it quoted", got)
	}
}

func TestSymbolSuggestionsNameTheDefinitions(t *testing.T) {
	want := "RetryPolicy | class | 2 definitions | payments-api, web-checkout\n" +
		"RetryPolicyConfig | class | 1 definition | shared-libs"
	if got := shown(Complete("sym:retry", len("sym:retry"), testResolver, fixedNow)); got != want {
		t.Errorf("Complete(sym:retry) =\n%s\nwant\n%s", got, want)
	}
}
