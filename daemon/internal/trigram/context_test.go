package trigram

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
)

func TestLineStartsEndTheLastLineAtAFinalLineEnd(t *testing.T) {
	tests := []struct {
		content string
		want    []int
	}{
		{"", []int{0}},
		{"a", []int{0}},
		{"a\n", []int{0}},
		{"a\nb", []int{0, 2}},
		{"a\nb\n", []int{0, 2}},
		{"\n", []int{0}},
		{"\n\n", []int{0, 1}},
		{"ab\r\ncd\r\n", []int{0, 4}},
	}
	for _, tt := range tests {
		if got := lineStarts([]byte(tt.content)); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("lineStarts(%q) = %v, want %v", tt.content, got, tt.want)
		}
	}
}

func TestContextAroundTakesUpToNLinesEachSide(t *testing.T) {
	content := []byte("one\ntwo\nthree\nfour\nfive\nsix\n")
	starts := lineStarts(content)
	tests := []struct {
		number, n     int
		before, after []string
	}{
		{3, 2, []string{"one", "two"}, []string{"four", "five"}},
		{3, 1, []string{"two"}, []string{"four"}},
		{1, 2, nil, []string{"two", "three"}},                      // nothing above the first line
		{2, 3, []string{"one"}, []string{"three", "four", "five"}}, // fewer above line 2
		{6, 2, []string{"four", "five"}, nil},                      // nothing past the last line, not even the final line end
		{5, 3, []string{"two", "three", "four"}, []string{"six"}},
		{3, 0, nil, nil},
		{0, 2, nil, nil}, // out of range
		{7, 2, nil, nil},
	}
	for _, tt := range tests {
		before, after := contextAround(content, starts, tt.number, tt.number, tt.n)
		if !reflect.DeepEqual(before, tt.before) || !reflect.DeepEqual(after, tt.after) {
			t.Errorf("contextAround(line %d, n %d) = %q, %q; want %q, %q", tt.number, tt.n, before, after, tt.before, tt.after)
		}
	}
}

func TestContextLinesDropLineEndsAndKeepBlankLines(t *testing.T) {
	content := []byte("a\r\n\r\nmatch\r\n\r\nz")
	before, after := contextAround(content, lineStarts(content), 3, 3, 2)
	if want := []string{"a", ""}; !reflect.DeepEqual(before, want) {
		t.Errorf("before = %q, want %q", before, want)
	}
	if want := []string{"", "z"}; !reflect.DeepEqual(after, want) {
		t.Errorf("after = %q, want %q (the last line has no line end)", after, want)
	}
}

func TestLongContextLinesAreClippedFromTheirStart(t *testing.T) {
	long := strings.Repeat("é", 1000)
	content := []byte(long + "\nmatch\n")
	before, _ := contextAround(content, lineStarts(content), 2, 2, 1)
	if len(before) != 1 {
		t.Fatalf("before = %d lines, want 1", len(before))
	}
	if got := before[0]; !strings.HasPrefix(got, "éé") || !strings.HasSuffix(got, "…") || utf8.RuneCountInString(got) != maxResultLineRunes+1 {
		t.Errorf("clipped context = %.10q…, %d runes; want the line's first %d runes and an ellipsis", got, utf8.RuneCountInString(got), maxResultLineRunes)
	}
}

// contextRepo has two matches of retry three lines apart, one on the
// first line and one on the last.
var contextRepo = repoOf("r", map[string]string{"client.py": strings.Join([]string{
	"retry = None", // 1
	"",
	"def charge():",
	"    return retry(post)", // 4
	"",
	"def refund():",
	"    return retry(delete)", // 7
	"",
	"# end: retry", // 9
}, "\n") + "\n"})

func withContext(settings protocol.Settings, n int) protocol.Settings {
	settings.ContextLines = n
	return settings
}

func TestCodeResultsCarryContextOnlyWhenTheSettingAsks(t *testing.T) {
	// @covers setting:results.contextLines
	items, _ := runItems(t, "type:code retry", defaultSettings, "", contextRepo)
	for _, item := range items {
		if item.ContextBefore != nil || item.ContextAfter != nil {
			t.Errorf("line %d carries context %q, %q without the setting", item.Line, item.ContextBefore, item.ContextAfter)
		}
	}

	items, _ = runItems(t, "type:code retry", withContext(defaultSettings, 2), "", contextRepo)
	type context struct{ before, after []string }
	want := map[int]context{
		1: {nil, []string{"", "def charge():"}},
		4: {[]string{"", "def charge():"}, []string{"", "def refund():"}},
		// Results whose context overlaps each carry all of theirs; the panel merges them.
		7: {[]string{"", "def refund():"}, []string{"", "# end: retry"}},
		9: {[]string{"    return retry(delete)", ""}, nil},
	}
	if len(items) != len(want) {
		t.Fatalf("got %d results, want %d: %q", len(items), len(want), summarize(items))
	}
	for _, item := range items {
		w := want[item.Line]
		if !reflect.DeepEqual(item.ContextBefore, w.before) || !reflect.DeepEqual(item.ContextAfter, w.after) {
			t.Errorf("line %d context = %q, %q; want %q, %q", item.Line, item.ContextBefore, item.ContextAfter, w.before, w.after)
		}
	}
}

func TestContextIsLeftOutOfTheJSONWhereThereIsNone(t *testing.T) {
	items, _ := runItems(t, "type:code retry", withContext(defaultSettings, 1), "", contextRepo)
	first, err := json.Marshal(items[0])
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(first, &fields); err != nil {
		t.Fatal(err)
	}
	if _, ok := fields["contextBefore"]; ok {
		t.Errorf("line 1's JSON has contextBefore: %s", first)
	}
	if got, ok := fields["contextAfter"].([]any); !ok || len(got) != 1 || got[0] != "" {
		t.Errorf("line 1's contextAfter = %v, want [\"\"] (a blank line is still context)", fields["contextAfter"])
	}
}

func TestEveryPageCarriesContext(t *testing.T) {
	settings := withContext(defaultSettings, 1)
	first, stats := runItems(t, "type:code count:2 retry", settings, "", contextRepo)
	second, _ := runItems(t, "type:code count:2 retry", settings, "2", contextRepo)
	if len(first) != 2 || len(second) != 2 || stats.Total != 4 {
		t.Fatalf("pages of %d and %d results of %d, want 2 and 2 of 4", len(first), len(second), stats.Total)
	}
	for _, item := range append(first, second...) {
		if item.ContextBefore == nil && item.ContextAfter == nil {
			t.Errorf("line %d has no context", item.Line)
		}
	}
	if second[0].Line != 7 || !reflect.DeepEqual(second[0].ContextBefore, []string{"def refund():"}) {
		t.Errorf("second page starts at line %d with context %q, want line 7 after def refund():", second[0].Line, second[0].ContextBefore)
	}
}

func TestBestMatchOrderKeepsEachResultsContext(t *testing.T) {
	other := repoOf("o", map[string]string{
		"retry.py":            "import time\ndef retry(call):\n    return call()\n",
		"tests/test_retry.py": "from retry import retry\n",
	})
	items, _ := runItems(t, "type:code retry", withContext(bestMatchSettings, 1), "", other)
	if len(items) == 0 {
		t.Fatal("no results")
	}
	for _, item := range items {
		content := string(other.Shard.Docs[docIndex(t, other.Shard, item.Path)].Content)
		lines := strings.Split(strings.TrimSuffix(content, "\n"), "\n")
		var before, after []string
		if item.Line > 1 {
			before = []string{lines[item.Line-2]}
		}
		if item.Line < len(lines) {
			after = []string{lines[item.Line]}
		}
		if !reflect.DeepEqual(item.ContextBefore, before) || !reflect.DeepEqual(item.ContextAfter, after) {
			t.Errorf("%s:%d context = %q, %q; want %q, %q", item.Path, item.Line, item.ContextBefore, item.ContextAfter, before, after)
		}
	}
}

func TestContextComesFromTheCurrentVersionOfAChangedFile(t *testing.T) {
	repo := repoOf("r", map[string]string{"a.go": "old above\nretry()\nold below\n"})
	repo.Overlay = shardOf(map[string]string{"a.go": "new above\nretry()\nnew below\n"})
	repo.Masked = map[string]bool{"a.go": true}
	items, _ := runItems(t, "type:code retry", withContext(defaultSettings, 1), "", repo)
	if len(items) != 1 {
		t.Fatalf("got %q, want the overlay's one line", summarize(items))
	}
	if !reflect.DeepEqual(items[0].ContextBefore, []string{"new above"}) || !reflect.DeepEqual(items[0].ContextAfter, []string{"new below"}) {
		t.Errorf("context = %q, %q; want the saved version's lines", items[0].ContextBefore, items[0].ContextAfter)
	}
}

func TestFileNamesAndDefinitionsCarryNoContext(t *testing.T) {
	repo := repoOf("r", map[string]string{"retry.go": "package r\n\nfunc Retry() {}\n"})
	items, _ := runItems(t, "retry", withContext(defaultSettings, 3), "", repo)
	symbols, _ := runItems(t, "sym:Retry", withContext(defaultSettings, 3), "", repo)
	for _, item := range append(items, symbols...) {
		if item.Kind != protocol.ResultItemKindLine && (item.ContextBefore != nil || item.ContextAfter != nil) {
			t.Errorf("%s result %s carries context", item.Kind, item.Path)
		}
	}
}

// docIndex is the index of path's doc in shard.
func docIndex(t *testing.T, shard *Shard, path string) int {
	t.Helper()
	for i := range shard.Docs {
		if shard.Docs[i].Path == path {
			return i
		}
	}
	t.Fatalf("no doc %s", path)
	return -1
}
