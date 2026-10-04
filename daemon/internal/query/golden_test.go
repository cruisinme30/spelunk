package query

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the golden files in testdata/parse")

// The query of every mock that shows one, parsed in full. Goldens pin the
// exact ParsedQuery (spans, term indexes, globals, mode, diagnostics and
// fixes) that the webview receives. Refresh with go test -update, then
// review the diff.
var goldenQueries = []struct{ name, query string }{
	{"mock01_plain_text", "retry_policy"},
	{"mock02_path_and_text", `f:.*test\.py$ timeout`},
	{"mock03_author_path_text", `author:jane f:.*test\.py$ timeout`},
	{"mock07_boolean_history", "author:jane (timeout OR retry) -f:vendor/ since:6m"},
	{"mock08_case_regex_lang", "case:yes /Retry(Policy|Config)/ lang:python"},
	{"mock09_symbols", "sym:RetryPolicy"},
	{"mock10_since_files", "since:2w timeout"},
	{"mock11_msg_repo_count", `msg:"fix flaky" repo:web count:20`},
	{"mock12_type_file", "type:file lang:python retry"},
	{"mock13_two_errors", "author:jane (timeout OR retry -f:vendor/ sinse:6m"},
	{"mock14_case_symbol", "case:yes sym:retrypolicy"},
}

func TestParseGolden(t *testing.T) {
	for _, g := range goldenQueries {
		t.Run(g.name, func(t *testing.T) {
			parsed := Parse(g.query, testResolver)
			got, err := json.MarshalIndent(parsed, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			got = append(got, '\n')
			path := filepath.Join("testdata", "parse", g.name+".json")
			if *update {
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, got, 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read golden (run go test -update): %v", err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("Parse(%q) differs from %s; run go test -update and review the diff.\ngot:\n%s", g.query, path, got)
			}
		})
	}
}
