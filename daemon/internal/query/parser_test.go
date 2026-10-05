package query

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/cruisinme30/unified-search/daemon/internal/protocol"
)

// shape renders a parse tree compactly: and(text:a op:f=x not(text:b)).
func shape(node *protocol.Node) string {
	if node == nil {
		return "<empty>"
	}
	switch node.Kind {
	case "and", "or":
		parts := make([]string, len(node.Children))
		for i := range node.Children {
			parts[i] = shape(&node.Children[i])
		}
		return node.Kind + "(" + strings.Join(parts, " ") + ")"
	case "not":
		return "not(" + shape(node.Child) + ")"
	case "text":
		return node.Match + ":" + node.Value
	default:
		return node.Op + "=" + node.Value + "/" + node.Match
	}
}

func TestSyntax(t *testing.T) {
	// @covers syntax:and syntax:or syntax:not syntax:group syntax:phrase syntax:regex
	tests := []struct {
		name, query, want string
	}{
		{"single term", "retry_policy", "literal:retry_policy"},
		{"space is an implicit AND", "timeout retry", "and(literal:timeout literal:retry)"},
		{"explicit AND is the same as a space", "timeout AND retry", "and(literal:timeout literal:retry)"},
		{"AND binds tighter than OR", "a b OR c", "or(and(literal:a literal:b) literal:c)"},
		{"parentheses group", "(a OR b) c", "and(or(literal:a literal:b) literal:c)"},
		{"lowercase or is a search term", "a or b", "and(literal:a literal:or literal:b)"},
		{"minus negates a term", "x -timeout", "and(literal:x not(literal:timeout))"},
		{"minus negates an operator", "x -f:vendor/", "and(literal:x not(f=vendor//regex))"},
		{"minus negates a group", "x -(a OR b)", "and(literal:x not(or(literal:a literal:b)))"},
		{"a lone minus is a term", "a - b", "and(literal:a literal:- literal:b)"},
		{"quotes keep spaces together", `"exact phrase" x`, "and(phrase:exact phrase literal:x)"},
		{"escaped quote inside a phrase", `"say \"hi\""`, `phrase:say "hi"`},
		{"slashes make a regex", "/Retry(Policy|Config)/", "regex:Retry(Policy|Config)"},
		{"a regex may contain spaces", "/fix(ed)? flaky/", "regex:fix(ed)? flaky"},
		{"std::vector is text, not an operator", "std::vector", "literal:std::vector"},
		{"a URL is text, not an operator", "http://example.com/x", "literal:http://example.com/x"},
		{"quoting an operator-shaped word makes it text", `"sinse:6m"`, "phrase:sinse:6m"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shape(mustParseCleanly(t, tt.query).Root); got != tt.want {
				t.Errorf("Parse(%q) = %s, want %s", tt.query, got, tt.want)
			}
		})
	}
}

func TestOperators(t *testing.T) {
	// @covers op:f op:repo op:lang op:type op:sym op:author op:msg op:since op:case op:count
	tests := []struct {
		name, query, want string
	}{
		{"f: is a regex without slashes", `f:.*test\.py$ timeout`, `and(f=.*test\.py$/regex literal:timeout)`},
		{"f: in quotes is literal", `f:"my dir/" x`, `and(f=my dir//phrase literal:x)`},
		{"f: with slashes and spaces", `f:/a b/ x`, `and(f=a b/regex literal:x)`},
		{"f: path starting with a slash stays bare", `f:/src/main x`, `and(f=/src/main/regex literal:x)`},
		{"repo: is a regex", "repo:web x", "and(repo=web/regex literal:x)"},
		{"lang: takes a name", "lang:python x", "and(lang=python/literal literal:x)"},
		{"lang: takes an alias", "lang:py x", "and(lang=py/literal literal:x)"},
		{"type: is global", "type:file retry", "and(type=file/literal literal:retry)"},
		{"sym: alone", "sym:RetryPolicy", "sym=RetryPolicy/literal"},
		{"sym: regex", "sym:/Retry.*/", "sym=Retry.*/regex"},
		{"author: substring", "author:jane timeout", "and(author=jane/literal literal:timeout)"},
		{"author: quoted full name", `author:"Jane Doe" x`, `and(author=Jane Doe/phrase literal:x)`},
		{"msg: phrase", `msg:"fix flaky"`, "msg=fix flaky/phrase"},
		{"msg: regex", "msg:/fix(ed)?/", "msg=fix(ed)?/regex"},
		{"since: days, weeks, months, years", "since:30d since:2w since:6m since:1y x", "and(since=30d/literal since=2w/literal since=6m/literal since=1y/literal literal:x)"},
		{"since: minutes, hours, today and yesterday", "since:45min since:3h since:today since:yesterday x", "and(since=45min/literal since=3h/literal since=today/literal since=yesterday/literal literal:x)"},
		{"case: yes", "case:yes x", "and(case=yes/literal literal:x)"},
		{"count: number", "count:20 x", "and(count=20/literal literal:x)"},
		{"count: all", "count:all x", "and(count=all/literal literal:x)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shape(mustParseCleanly(t, tt.query).Root); got != tt.want {
				t.Errorf("Parse(%q) = %s, want %s", tt.query, got, tt.want)
			}
		})
	}
}

func TestGlobalsAreCollected(t *testing.T) {
	q := mustParseCleanly(t, "case:yes count:all type:code retry")
	got, err := json.Marshal(q.Globals)
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"case":"yes","count":"all","type":"code"}`; string(got) != want {
		t.Errorf("globals = %s, want %s", got, want)
	}
	q = mustParseCleanly(t, "count:20 retry")
	if q.Globals.Count != 20 || q.Globals.Case != nil {
		t.Errorf("globals of count:20 = %+v, want count 20 and no case", q.Globals)
	}
}

func TestModeIsHistoryWhenAuthorMsgOrTypeCommitAppear(t *testing.T) {
	tests := map[string]protocol.Mode{
		"timeout":                     protocol.ModeWorkingTree,
		"since:2w timeout":            protocol.ModeWorkingTree,
		"author:jane timeout":         protocol.ModeHistory,
		`msg:"fix flaky"`:             protocol.ModeHistory,
		"type:commit timeout":         protocol.ModeHistory,
		"x -author:bot":               protocol.ModeHistory,
		"type:file lang:python retry": protocol.ModeWorkingTree,
	}
	for query, want := range tests {
		if got := mustParseCleanly(t, query).Mode; got != want {
			t.Errorf("Parse(%q).Mode = %s, want %s", query, got, want)
		}
	}
}

func TestTextTermsAreNumberedInOrderForHighlightColors(t *testing.T) {
	q := mustParseCleanly(t, "author:jane (timeout OR retry) -f:vendor/ since:6m")
	var terms []string
	walk(q.Root, func(n *protocol.Node) {
		if n.Kind == "text" {
			terms = append(terms, n.Value+"#"+strconv.Itoa(n.TermIndex))
		}
	})
	if got, want := strings.Join(terms, " "), "timeout#0 retry#1"; got != want {
		t.Errorf("term indexes = %s, want %s", got, want)
	}
}

func TestSpansAreUTF16(t *testing.T) {
	// 日 and 本 are one UTF-16 unit each; 😀 is two.
	q := mustParseCleanly(t, "日本 😀 retry")
	retry := q.Root.Children[2]
	if retry.Span != (protocol.Span{Start: 6, End: 11}) {
		t.Errorf("span of retry = %+v, want {6 11}", retry.Span)
	}
}

func TestAuthorAndRepoValuesAreResolved(t *testing.T) {
	q := mustParseCleanly(t, "author:jane repo:web x")
	author, repo := q.Root.Children[0], q.Root.Children[1]
	if author.Resolved == nil || author.Resolved.Label != "Jane Doe" {
		t.Errorf("author:jane resolved = %+v, want Jane Doe", author.Resolved)
	}
	if repo.Resolved == nil || repo.Resolved.Label != "web-checkout" {
		t.Errorf("repo:web resolved = %+v, want web-checkout", repo.Resolved)
	}
	if q := mustParseCleanly(t, "author:ja x"); q.Root.Children[0].Resolved != nil {
		t.Errorf("author:ja is ambiguous; resolved = %+v, want nil", q.Root.Children[0].Resolved)
	}
}

func TestEmptyQueryHasNoRootAndNoDiagnostics(t *testing.T) {
	for _, text := range []string{"", "   "} {
		q := Parse(text, nil)
		if q.Root != nil || len(q.Diagnostics) != 0 {
			t.Errorf("Parse(%q) = root %v, diagnostics %v; want nil and none", text, q.Root, codes(q.Diagnostics))
		}
	}
}

func TestAStrayParenKeepsGlobalsAtTheTopLevel(t *testing.T) {
	q := Parse("case:yes a ) b", testResolver)
	if got := codes(q.Diagnostics); len(got) != 1 || got[0] != DiagUnmatchedParen {
		t.Errorf("Parse(%q) diagnostics = %v, want only %s", q.Raw, got, DiagUnmatchedParen)
	}
	if q.Globals.Case == nil || *q.Globals.Case != "yes" || len(q.Root.Children) != 3 {
		t.Errorf("Parse(%q) = case %v with %d top-level children, want case:yes and 3", q.Raw, q.Globals.Case, len(q.Root.Children))
	}
}

func TestReplacingAValueKeepsItApartFromTheNextWord(t *testing.T) {
	q := Parse("case:/ /y", testResolver)
	if got := ApplyFix(q.Raw, q.Diagnostics[0].Fixes[0]); got != "case:yes y" {
		t.Errorf("first fix of %q gives %q, want %q", q.Raw, got, "case:yes y")
	}
}
