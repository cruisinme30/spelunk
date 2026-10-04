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

// goldenQueries cover every operator, the boolean forms and a query with
// two errors. Each golden file pins the exact ParsedQuery (spans, term
// indexes, globals, mode, diagnostics and fixes) the webview receives.
// Refresh them with go test -update, then review the diff.
var goldenQueries = []struct{ name, query string }{
	{"plain_text", "retry_policy"},
	{"path_and_text", `f:.*test\.py$ timeout`},
	{"author_path_text", `author:jane f:.*test\.py$ timeout`},
	{"boolean_history", "author:jane (timeout OR retry) -f:vendor/ since:6m"},
	{"case_regex_lang", "case:yes /Retry(Policy|Config)/ lang:python"},
	{"symbols", "sym:RetryPolicy"},
	{"since_files", "since:2w timeout"},
	{"msg_repo_count", `msg:"fix flaky" repo:web count:20`},
	{"type_file", "type:file lang:python retry"},
	{"two_errors", "author:jane (timeout OR retry -f:vendor/ sinse:6m"},
	{"case_symbol", "case:yes sym:retrypolicy"},
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
				if mkdirErr := os.MkdirAll(filepath.Dir(path), 0o750); mkdirErr != nil {
					t.Fatal(mkdirErr)
				}
				if writeErr := os.WriteFile(path, got, 0o600); writeErr != nil {
					t.Fatal(writeErr)
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
