package trigram

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
	"github.com/cruisinme30/spelunk/daemon/internal/query"
	"github.com/cruisinme30/spelunk/daemon/internal/testutil"
)

// structureFile is the mock's client: two calls of retry, one over three
// lines and one over two, and a comment that looks like one.
var structureFile = strings.Join([]string{
	"class PaymentsClient:", // 1
	"    def charge(self, body):",
	`        """Charge a card, retrying on connection errors."""`,
	`        url = self._base + "/charges"`,
	"        # retry(fn, attempts=N) gives up after N tries", // 5
	"        return retry(",
	"            lambda: self._session.post(url, json=body),",
	"            attempts=self.config.max_attempts)",
	"",
	"    def refund(self, charge_id):", // 10
	"        return retry(self._refund, charge_id,",
	"                     attempts=2)",
	"",
}, "\n")

var structureRepo = repoOf("payments-api", map[string]string{
	"payments/client.py":     structureFile,
	"webhooks/dispatcher.py": "def dispatch(event):\n    return retry(lambda: send(event), attempts=5)\n",
	"notes.md":               "Call retry(fn, attempts=3) to retry.\n",
	"cmd/main.go":            "package main\n\n// retry(f, attempts=1)\nvar s = \"retry(g, attempts=2)\"\nfunc main() { retry(run, attempts=3) }\n",
})

// structureResult renders a code result as its lines, with the template's
// code in [brackets] and each hole's code in {n:braces}, then what each
// hole took.
func structureResult(item protocol.ResultItem) string {
	lines := []string{fmt.Sprintf("%d %s", item.Line, marked(item.Text, item.Hits))}
	for i, later := range item.SpanLines {
		lines = append(lines, fmt.Sprintf("%d %s", item.Line+1+i, marked(later.Text, later.Hits)))
	}
	if item.EndLine != 0 {
		lines = append(lines, fmt.Sprintf("ends on %d", item.EndLine))
	}
	for _, hole := range item.Holes {
		lines = append(lines, fmt.Sprintf(":[%s]#%d=%s", hole.Name, hole.Hole, hole.Text))
	}
	return strings.Join(lines, " | ")
}

// marked marks hits (UTF-16 offsets, ASCII text here) in text: code in
// brackets, a hole's code in braces with its number.
func marked(text string, hits []protocol.Hit) string {
	var b strings.Builder
	at := 0
	for _, hit := range hits {
		start := max(hit.Start, at)
		open, closer := "[", "]"
		if hit.Hole > 0 {
			open, closer = fmt.Sprintf("{%d:", hit.Hole), "}"
		}
		b.WriteString(text[at:start] + open + text[start:max(hit.End, start)] + closer)
		at = max(hit.End, start)
	}
	b.WriteString(text[at:])
	return b.String()
}

func structureResults(t *testing.T, text string, settings protocol.Settings, repos ...Repo) []string {
	t.Helper()
	items, _ := runItems(t, text, settings, "", repos...)
	out := []string{}
	for _, item := range items {
		if item.Kind == query.KindLine {
			out = append(out, item.Path+":"+structureResult(item))
		} else {
			out = append(out, "file "+item.Path)
		}
	}
	return out
}

// A template finds each call however it is laid out, across lines, with
// its code and its holes marked and what each hole took listed; comments
// and strings that look like a call are skipped, by each file's language.
func TestStructFindsCodeByShapeAcrossLines(t *testing.T) {
	// @covers op:struct
	got := structureResults(t, `struct:"retry(:[call], attempts=:[n])"`, defaultSettings, structureRepo)
	want := []string{
		"cmd/main.go:5 func main() { [retry(]{1:run}[, attempts=]{2:3}[)] } | :[call]#1=run | :[n]#2=3",
		"notes.md:1 Call [retry(]{1:fn}[, attempts=]{2:3}[)] to retry. | :[call]#1=fn | :[n]#2=3",
		"payments/client.py:6         return [retry(] | 7             {1:lambda: self._session.post(url, json=body)}[,] | 8             [attempts=]{2:self.config.max_attempts}[)] | ends on 8" +
			" | :[call]#1=lambda: self._session.post(url, json=body) | :[n]#2=self.config.max_attempts",
		"payments/client.py:11         return [retry(]{1:self._refund, charge_id}[,] | 12                      [attempts=]{2:2}[)] | ends on 12" +
			" | :[call]#1=self._refund, charge_id | :[n]#2=2",
		"webhooks/dispatcher.py:2     return [retry(]{1:lambda: send(event)}[, attempts=]{2:5}[)] | :[call]#1=lambda: send(event) | :[n]#2=5",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("results:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// Opening a match on one line selects all of it; one across lines, from
// its start to its end.
func TestOpeningAStructMatchSelectsAllOfIt(t *testing.T) {
	items, _ := runItems(t, `struct:"retry(:[call], attempts=:[n])"`, defaultSettings, "", structureRepo)
	for _, item := range items {
		ref, ok := ParseRef(item.Ref)
		if !ok {
			t.Fatalf("ref %q", item.Ref)
		}
		switch item.Path {
		case "webhooks/dispatcher.py":
			if ref.Column != 11 || ref.Length != len("retry(lambda: send(event), attempts=5)") || ref.Spans() {
				t.Errorf("one-line ref = %+v", ref)
			}
		case "payments/client.py":
			if item.Line == 6 && (ref.Column != 15 || ref.EndLine != 8 || ref.EndColumn != len("            attempts=self.config.max_attempts)")) {
				t.Errorf("spanning ref = %+v", ref)
			}
		}
	}
}

// What a hole took is listed with its space squeezed, and cut when long.
func TestHoleValuesAreSqueezedAndCut(t *testing.T) {
	long := strings.Repeat("argument_name, ", 10)
	repo := repoOf("r", map[string]string{"a.py": "f(\n    a,\n    b)\nf(" + long + "z)\n"})
	got := structureResults(t, `struct:"f(:[args])"`, defaultSettings, repo)
	if len(got) != 2 || !strings.HasSuffix(got[0], ":[args]#1=a, b") {
		t.Fatalf("results = %q", got)
	}
	value := got[1][strings.Index(got[1], ":[args]#1=")+len(":[args]#1="):]
	if len([]rune(value)) != maxHoleRunes || !strings.HasSuffix(value, "…") || !strings.HasPrefix(value, "argument_name, argument_name") {
		t.Errorf("long value = %q (%d runes)", value, len([]rune(value)))
	}
}

// A template that gives up on one file marks the results truncated, and
// still finds its matches in the other files.
func TestStructGivingUpMarksResultsTruncated(t *testing.T) {
	repo := repoOf("r", map[string]string{
		"a.go": "f(" + strings.Repeat("1, ", 400) + "y)\n",
		"b.go": "f(1, 2, 3, 4, 5, 6, 7, x)\n",
	})
	items, stats := runItems(t, `struct:"f(:[a], :[b], :[c], :[d], :[e], :[f], :[g], x)"`, defaultSettings, "", repo)
	if len(items) != 1 || items[0].Path != "b.go" {
		t.Errorf("results = %q, want the match in b.go", summarize(items))
	}
	if !stats.Truncated {
		t.Error("the results aren't marked truncated, though the template gave up on a.go")
	}
}

// Matches on the same line are one result, each hole listed once, as the
// first match took it; :[_] is never listed or marked.
func TestStructMatchesOnALineAreOneResult(t *testing.T) {
	repo := repoOf("r", map[string]string{"a.go": "x := f(1, a) + f(2, b)\n"})
	got := structureResults(t, `struct:"f(:[n], :[_])"`, defaultSettings, repo)
	want := []string{"a.go:1 x := [f(]{1:1}[,] a[)] + [f(]{1:2}[,] b[)] | :[n]#1=1"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("results = %q, want %q", got, want)
	}
}

// struct: works with the rest of the language: NOT, other terms, file
// and language filters, and case.
func TestStructCombinesWithTheRest(t *testing.T) {
	if got := structureResults(t, `lambda -struct:"retry(:[a], attempts=5)"`, defaultSettings, structureRepo); len(got) != 1 || !strings.HasPrefix(got[0], "payments/client.py:7") {
		t.Errorf("NOT: results = %q, want client.py's lambda only", got)
	}
	if got := structureResults(t, `lang:go struct:"retry(:[a], attempts=:[n])"`, defaultSettings, structureRepo); len(got) != 1 || !strings.HasPrefix(got[0], "cmd/main.go:5") {
		t.Errorf("lang:go: results = %q", got)
	}
	if got := structureResults(t, `f:webhooks/ struct:"retry(:[a], attempts=:[n])"`, defaultSettings, structureRepo); len(got) != 1 {
		t.Errorf("f: results = %q", got)
	}
	if got := structureResults(t, `struct:"RETRY(:[a], ATTEMPTS=:[n])"`, defaultSettings, structureRepo); len(got) != 5 {
		t.Errorf("case:no results = %q, want all 5", got)
	}
	if got := structureResults(t, `case:yes struct:"RETRY(:[a], ATTEMPTS=:[n])"`, defaultSettings, structureRepo); len(got) != 0 {
		t.Errorf("case:yes results = %q, want none", got)
	}
	// A template and a word on the same line are one result.
	if got := structureResults(t, `struct:"send(:[e])" lambda`, defaultSettings, structureRepo); len(got) != 1 ||
		got[0] != "webhooks/dispatcher.py:2     return retry([lambda]: [send(]{1:event}[)], attempts=5) | :[e]#1=event" {
		t.Errorf("with a word: results = %q", got)
	}
}

// A template never matches file names, only their text.
func TestStructNeverMatchesFileNames(t *testing.T) {
	repo := repoOf("r", map[string]string{"retry(x).py": "pass\n", "b.py": "retry(x)\n"})
	got := structureResults(t, `struct:"retry(:[a])"`, defaultSettings, repo)
	if want := []string{"b.py:1 [retry(]{1:x}[)] | :[a]#1=x"}; !reflect.DeepEqual(got, want) {
		t.Errorf("results = %q, want %q", got, want)
	}
}

// A file whose lines end in "\r\n" matches like one with "\n".
func TestStructMatchesCRLFFiles(t *testing.T) {
	repo := repoOf("r", map[string]string{"a.py": strings.ReplaceAll(structureFile, "\n", "\r\n")})
	got := structureResults(t, `struct:"retry(:[call], attempts=:[n])"`, defaultSettings, repo)
	if len(got) != 2 || !strings.Contains(got[0], "ends on 8") || !strings.Contains(got[1], ":[n]#2=2") {
		t.Errorf("results = %q", got)
	}
}

// Context lines sit around the whole match.
func TestContextSurroundsAStructMatch(t *testing.T) {
	items, _ := runItems(t, `f:client struct:"retry(:[call], attempts=:[n])"`, withContext(defaultSettings, 1), "", structureRepo)
	if len(items) != 2 {
		t.Fatalf("results = %d", len(items))
	}
	if !reflect.DeepEqual(items[0].ContextBefore, []string{"        # retry(fn, attempts=N) gives up after N tries"}) ||
		!reflect.DeepEqual(items[0].ContextAfter, []string{""}) {
		t.Errorf("context = %q, %q", items[0].ContextBefore, items[0].ContextAfter)
	}
}

// The preview marks the template's code and each hole's code on every line
// of a match, by the file's language: the comment that looks like a call
// stays unmarked.
func TestPreviewMarksAStructMatch(t *testing.T) {
	// @covers rpc:preview/get
	root := testutil.WriteTree(t, map[string]string{"client.py": structureFile})
	repo := &Repo{ID: "r", Name: "r", Root: root}
	plan := mustPlan(t, `struct:"retry(:[call], attempts=:[n])"`, defaultSettings, "")
	ref := Ref{PlanID: 1, RepoID: "r", Path: "client.py", Line: 6, Column: 15, Length: 6, EndLine: 8, EndColumn: 46}
	got, err := Preview(repo, ref, plan, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got.FirstLine != 5 || got.FocusLine != 6 || got.FocusEndLine != 8 {
		t.Errorf("window from %d, focus %d-%d; want 5, 6-8", got.FirstLine, got.FocusLine, got.FocusEndLine)
	}
	var lines []string
	for _, line := range got.Hits {
		lines = append(lines, fmt.Sprintf("%d %s", line.Line, marked(got.Lines[line.Line-got.FirstLine], line.Ranges)))
	}
	want := []string{
		"6         return [retry(]",
		"7             {1:lambda: self._session.post(url, json=body)}[,]",
		"8             [attempts=]{2:self.config.max_attempts}[)]",
	}
	if !reflect.DeepEqual(lines, want) {
		t.Errorf("marked:\n%s\nwant:\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
}

// A template whose holes could split a long line very many ways gives up
// on that file instead of holding the search.
func TestASlowTemplateDoesNotHoldTheSearch(t *testing.T) {
	repo := repoOf("r", map[string]string{"a.go": "f(" + strings.Repeat("1, ", 400) + "y)\nx\n", "b.go": "f(1, 2, 3, 4, 5, 6, 7, x)\n"})
	got := structureResults(t, `struct:"f(:[a], :[b], :[c], :[d], :[e], :[f], :[g], x)"`, defaultSettings, repo)
	if len(got) != 1 || !strings.HasPrefix(got[0], "b.go:1") {
		t.Errorf("results = %q, want b.go's call", got)
	}
}
