package query

import (
	"errors"
	"fmt"
	"regexp"
	"regexp/syntax"
	"strconv"
	"strings"
	"time"

	"github.com/cruisinme30/unified-search/daemon/internal/lang"
	"github.com/cruisinme30/unified-search/daemon/internal/protocol"
)

// MaxResults is the hard cap on one search's results, even with count:all.
const MaxResults = 50_000

// ResultKind is a kind of search result.
type ResultKind = string

// The kinds of result, as ResultItem.Kind names them.
const (
	KindFile   ResultKind = protocol.ResultItemKindFile
	KindLine   ResultKind = protocol.ResultItemKindLine
	KindSymbol ResultKind = protocol.ResultItemKindSymbol
	KindCommit ResultKind = protocol.ResultItemKindCommit
)

// Plan is what a search engine runs. Engines never see query text.
type Plan struct {
	Mode  protocol.Mode
	Kinds map[ResultKind]bool
	// Pred is always an *And whose Kids are the top-level conjuncts
	// (case:, count: and type: shape the plan and have no predicate).
	Pred          *And
	CaseSensitive bool
	// Limit is how many results one page returns (count:, or the default).
	Limit int
	// Offset is where this page starts (from the Load more cursor).
	Offset int
	// Filters are the top-level conjuncts that can hide results; engines
	// count what each one hid ("3 commits hidden by -f:vendor/").
	Filters []Filter
	// KindFilter is set when type: narrows the result kinds, so engines can
	// count what it hid ("17 code matches hidden by type:file").
	KindFilter *Filter
	// CaseFilter is set when the query says case:yes, so engines can count
	// the matches that differ only in case ("1 match hidden by case:yes").
	CaseFilter *Filter
	// Terms are the text terms in query order, for highlighting.
	Terms []*Content
}

// Filter is part of a query that may hide results. Engines count what each
// filter alone hid, and the panel shows the count with an undo.
type Filter struct {
	// Reason is the HiddenNote reason that names the kind of filter:
	// "pathFilter" (-f:), "not" (any other negation), "since" (since:), and,
	// for Plan.KindFilter and Plan.CaseFilter, "type" and "case".
	Reason string
	// Index is the position of the filter's conjunct in Plan.Pred.Kids, so
	// an engine can tell which filter a result failed. It is -1 for
	// KindFilter and CaseFilter, which have no conjunct.
	Index int
	// Text is the filter as typed, e.g. -f:vendor/.
	Text string
	// Undo is the fix that removes the filter from the query.
	Undo protocol.Fix
}

// Pred is a node of the lowered predicate tree.
type Pred interface {
	String() string
	isPred()
}

type (
	// And is true when every kid is.
	And struct{ Kids []Pred }
	// Or is true when any kid is.
	Or struct{ Kids []Pred }
	// Not is true when its kid isn't.
	Not struct{ Kid Pred }
	// Content matches file text, or a commit's added and removed lines.
	Content struct {
		Re *regexp.Regexp
		// Literal is the text to search for when the term is not a regex;
		// engines use it (or Re) to narrow candidates with trigrams.
		Literal string
		// IgnoreCase is true when Re ignores case.
		IgnoreCase bool
		TermIndex  int
	}
	// Path matches the repo-relative path (f:).
	Path struct{ Re *regexp.Regexp }
	// Repo matches the repo's display name (repo:).
	Repo struct{ Re *regexp.Regexp }
	// Lang matches the file's language (lang:).
	Lang struct{ Name string }
	// Symbol matches a symbol definition's name (sym:).
	Symbol struct{ Re *regexp.Regexp }
	// Author matches a commit author's name or email, after .mailmap (author:).
	Author struct {
		Fragment string // lowercase; a substring of name or email
		Exact    bool   // a "quoted full name" must equal the name
	}
	// Message matches a commit's subject and body (msg:).
	Message struct{ Re *regexp.Regexp }
	// Since matches commits after a time, or files last changed after it.
	Since struct{ After time.Time }
)

func (*And) isPred()     {}
func (*Or) isPred()      {}
func (*Not) isPred()     {}
func (*Content) isPred() {}
func (*Path) isPred()    {}
func (*Repo) isPred()    {}
func (*Lang) isPred()    {}
func (*Symbol) isPred()  {}
func (*Author) isPred()  {}
func (*Message) isPred() {}
func (*Since) isPred()   {}

func joinPreds(kind string, kids []Pred) string {
	parts := make([]string, len(kids))
	for i, k := range kids {
		parts[i] = k.String()
	}
	return kind + "(" + strings.Join(parts, " ") + ")"
}

func (p *And) String() string     { return joinPreds("and", p.Kids) }
func (p *Or) String() string      { return joinPreds("or", p.Kids) }
func (p *Not) String() string     { return "not(" + p.Kid.String() + ")" }
func (p *Content) String() string { return fmt.Sprintf("content#%d:/%s/", p.TermIndex, p.Re) }
func (p *Path) String() string    { return "path:/" + p.Re.String() + "/" }
func (p *Repo) String() string    { return "repo:/" + p.Re.String() + "/" }
func (p *Lang) String() string    { return "lang:" + p.Name }
func (p *Symbol) String() string  { return "sym:/" + p.Re.String() + "/" }
func (p *Message) String() string { return "msg:/" + p.Re.String() + "/" }
func (p *Since) String() string   { return "since:" + p.After.UTC().Format(time.RFC3339) }
func (p *Author) String() string {
	if p.Exact {
		return fmt.Sprintf("author:=%q", p.Fragment)
	}
	return fmt.Sprintf("author:~%q", p.Fragment)
}

// NewPlan lowers a parsed query without error diagnostics into a Plan.
// cursor is the Load more cursor ("" for the first page). The returned
// diagnostics are warnings, such as a history regex that must scan every
// commit.
func NewPlan(q protocol.ParsedQuery, settings protocol.Settings, now time.Time, cursor string) (*Plan, []protocol.Diagnostic, error) {
	for _, d := range q.Diagnostics {
		if d.Severity == protocol.SeverityError {
			return nil, nil, fmt.Errorf("query has errors: %s", d.Message)
		}
	}
	if q.Root == nil {
		return nil, nil, errors.New("empty query")
	}
	offset, err := pageOffset(cursor)
	if err != nil {
		return nil, nil, err
	}
	plan := &Plan{
		Mode:          q.Mode,
		Kinds:         resultKinds(q),
		CaseSensitive: settings.CaseSensitive,
		Limit:         pageSize(q.Globals.Count, settings.DefaultCount),
		Offset:        offset,
		Pred:          &And{},
	}
	if q.Globals.Case != nil {
		plan.CaseSensitive = *q.Globals.Case == "yes"
	}

	l := lowering{caseSensitive: plan.CaseSensitive, now: now, plan: plan}
	src := newSource(q.Raw)
	for _, node := range topLevel(q.Root) {
		pred := l.lower(node)
		if pred == nil { // a global: case:, count: or type:
			plan.addGlobalFilter(node, src)
			continue
		}
		if reason := filterReason(node); reason != "" {
			text := src.slice(node.Span.Start, node.Span.End)
			plan.Filters = append(plan.Filters, Filter{
				Reason: reason, Index: len(plan.Pred.Kids), Text: text,
				Undo: removeFix("Remove "+text, src, node.Span),
			})
		}
		plan.Pred.Kids = append(plan.Pred.Kids, pred)
	}
	return plan, historyScanWarnings(plan), nil
}

// pageSize is how many results one page holds: count: if given (a number,
// or "all"), otherwise the defaultCount setting, kept within 1..MaxResults.
func pageSize(count any, defaultCount int) int {
	size := defaultCount
	switch count := count.(type) {
	case int:
		size = count
	case float64: // after a JSON round trip
		size = int(count)
	case string: // "all"
		size = MaxResults
	}
	return min(max(size, 1), MaxResults)
}

// pageOffset reads a Load more cursor: the index of the page's first result.
func pageOffset(cursor string) (int, error) {
	if cursor == "" {
		return 0, nil
	}
	offset, err := strconv.Atoi(cursor)
	if err != nil || offset < 0 {
		return 0, fmt.Errorf("bad cursor %q", cursor)
	}
	return offset, nil
}

// addGlobalFilter records a type: or case:yes global as a filter whose
// hidden results the engine counts (the type:file and case:yes notes).
func (p *Plan) addGlobalFilter(node *protocol.Node, src *source) {
	if node.Kind != protocol.NodeKindOp {
		return
	}
	text := src.slice(node.Span.Start, node.Span.End)
	switch {
	case node.Op == protocol.OpNameType:
		p.KindFilter = &Filter{Reason: "type", Index: -1, Text: text, Undo: removeFix("Remove "+text, src, node.Span)}
	case node.Op == protocol.OpNameCase && p.CaseSensitive:
		p.CaseFilter = &Filter{Reason: "case", Index: -1, Text: text, Undo: removeFix("Ignore case", src, node.Span)}
	}
}

// resultKinds decides which kinds of result the query returns.
func resultKinds(q protocol.ParsedQuery) map[ResultKind]bool {
	if q.Mode == protocol.ModeHistory {
		return map[ResultKind]bool{KindCommit: true}
	}
	wantsSymbols := false
	walk(q.Root, func(n *protocol.Node) {
		if n.Kind == protocol.NodeKindOp && n.Op == protocol.OpNameSym {
			wantsSymbols = true
		}
	})
	kinds := map[ResultKind]bool{}
	switch {
	case q.Globals.Type != nil && *q.Globals.Type == "file":
		kinds[KindFile] = true
	case wantsSymbols:
		kinds[KindSymbol] = true
	case q.Globals.Type != nil && *q.Globals.Type == "code":
		kinds[KindLine] = true
	default:
		kinds[KindFile] = true
		kinds[KindLine] = true
	}
	return kinds
}

// lowering turns AST nodes into predicates.
type lowering struct {
	caseSensitive bool
	now           time.Time
	plan          *Plan
}

func (l *lowering) lower(node *protocol.Node) Pred {
	switch node.Kind {
	case protocol.NodeKindAnd, protocol.NodeKindOr:
		var kids []Pred
		for i := range node.Children {
			if kid := l.lower(&node.Children[i]); kid != nil {
				kids = append(kids, kid)
			}
		}
		if node.Kind == protocol.NodeKindOr {
			return &Or{Kids: kids}
		}
		return &And{Kids: kids}
	case protocol.NodeKindNot:
		if kid := l.lower(node.Child); kid != nil {
			return &Not{Kid: kid}
		}
		return nil
	case protocol.NodeKindText:
		content := &Content{Re: l.regex(node.Value, node.Match), IgnoreCase: !l.caseSensitive, TermIndex: node.TermIndex}
		if node.Match != protocol.MatchRegex {
			content.Literal = node.Value
		}
		l.plan.Terms = append(l.plan.Terms, content)
		return content
	default:
		return l.lowerOperator(node)
	}
}

func (l *lowering) lowerOperator(node *protocol.Node) Pred {
	switch node.Op {
	case protocol.OpNameF:
		return &Path{Re: l.regex(node.Value, node.Match)}
	case protocol.OpNameRepo:
		return &Repo{Re: l.regex(node.Value, node.Match)}
	case protocol.OpNameLang:
		name, _ := lang.Resolve(node.Value)
		return &Lang{Name: name}
	case protocol.OpNameSym:
		pattern := node.Value
		if node.Match != protocol.MatchRegex {
			pattern = "^" + regexp.QuoteMeta(node.Value) + "$" // a literal names the whole symbol
		}
		return &Symbol{Re: l.compile(pattern)}
	case protocol.OpNameAuthor:
		return &Author{Fragment: strings.ToLower(node.Value), Exact: node.Match == protocol.MatchPhrase}
	case protocol.OpNameMsg:
		return &Message{Re: l.regex(node.Value, node.Match)}
	case protocol.OpNameSince:
		return &Since{After: since(l.now, node.Value)}
	default: // case:, count:, type: shape the plan, not the predicate
		return nil
	}
}

// regex builds the regex for a value: literals and phrases are quoted.
func (l *lowering) regex(value string, match protocol.Match) *regexp.Regexp {
	switch match {
	case protocol.MatchRegex:
	case protocol.MatchGlob:
		value = globPattern(value)
	case protocol.MatchLiteral, protocol.MatchPhrase:
		value = regexp.QuoteMeta(value)
	}
	return l.compile(value)
}

func (l *lowering) compile(pattern string) *regexp.Regexp {
	if !l.caseSensitive {
		pattern = "(?i)" + pattern
	}
	return regexp.MustCompile(pattern) // the parser already validated it
}

// since returns when the window named by a since: value starts, counting
// back from now: <n>d is n days, <n>w n weeks, <n>m n calendar months and
// <n>y n calendar years, so since:6m on October 3 starts on April 3. value
// must match durationPattern, which the parser already checked.
func since(now time.Time, value string) time.Time {
	parts := durationPattern.FindStringSubmatch(value)
	n, _ := strconv.Atoi(parts[1])
	switch parts[2] {
	case "d":
		return now.AddDate(0, 0, -n)
	case "w":
		return now.AddDate(0, 0, -7*n)
	case "m":
		return now.AddDate(0, -n, 0)
	default:
		return now.AddDate(-n, 0, 0)
	}
}

// filterReason says whether a top-level conjunct can hide results, and
// how the hidden-results note describes it ("" if it can't). A positive f:
// is the search's scope rather than a filter: the panel explains the scope
// instead of counting everything outside it.
func filterReason(node *protocol.Node) string {
	switch {
	case node.Kind == protocol.NodeKindNot && node.Child.Kind == protocol.NodeKindOp && node.Child.Op == protocol.OpNameF:
		return "pathFilter"
	case node.Kind == protocol.NodeKindNot:
		return "not"
	case node.Kind == protocol.NodeKindOp && node.Op == protocol.OpNameSince:
		return "since"
	default:
		return ""
	}
}

// Eval evaluates a predicate, asking leaf for the truth of each leaf.
func Eval(p Pred, leaf func(Pred) bool) bool {
	switch p := p.(type) {
	case *And:
		for _, kid := range p.Kids {
			if !Eval(kid, leaf) {
				return false
			}
		}
		return true
	case *Or:
		for _, kid := range p.Kids {
			if Eval(kid, leaf) {
				return true
			}
		}
		return false
	case *Not:
		return !Eval(p.Kid, leaf)
	default:
		return leaf(p)
	}
}

// Contributing returns the content terms that make p true: every term of a
// true AND, the true branches of an OR, nothing under NOT. Engines show
// matches only for these, so results(A OR B) = results(A) ∪ results(B).
func Contributing(p Pred, leaf func(Pred) bool) []*Content {
	var terms []*Content
	var visit func(Pred) bool
	visit = func(p Pred) bool {
		switch p := p.(type) {
		case *And:
			if !Eval(p, leaf) {
				return false
			}
			for _, kid := range p.Kids {
				visit(kid)
			}
			return true
		case *Or:
			matched := false
			for _, kid := range p.Kids {
				if visit(kid) {
					matched = true
				}
			}
			return matched
		case *Not:
			return Eval(p, leaf)
		case *Content:
			if leaf(p) {
				terms = append(terms, p)
				return true
			}
			return false
		default:
			return leaf(p)
		}
	}
	visit(p)
	return terms
}

// historyScanWarnings warns when a history regex has no literal of three
// or more characters and nothing else narrows the commits: FTS5 trigrams
// can't help, so the search scans every row (capped at 2 seconds).
func historyScanWarnings(plan *Plan) []protocol.Diagnostic {
	if plan.Mode != protocol.ModeHistory || narrowsHistory(plan.Pred) {
		return nil
	}
	var problems diagnostics
	for _, term := range plan.Terms {
		if term.Literal == "" && len(RequiredLiteral(term.Re)) < 3 {
			problems.add(protocol.SeverityWarning, DiagHistoryFullScan, protocol.Span{}, fmt.Sprintf(
				"/%s/ has no 3-character literal, so every commit is scanned. Add author:, since: or f: to narrow it.", term.Re))
		}
	}
	return problems.list
}

// narrowsHistory reports whether a top-level author:, since: or f: limits the commits to scan.
func narrowsHistory(p *And) bool {
	for _, kid := range p.Kids {
		switch kid.(type) {
		case *Author, *Since, *Path:
			return true
		}
	}
	return false
}

// RequiredLiteral returns the longest literal every match of re must
// contain ("" if none is certain). Engines use it to narrow candidates.
func RequiredLiteral(re *regexp.Regexp) string {
	parsed, err := syntax.Parse(re.String(), syntax.Perl)
	if err != nil {
		return ""
	}
	return longestLiteral(parsed.Simplify())
}

func longestLiteral(re *syntax.Regexp) string {
	switch re.Op {
	case syntax.OpLiteral:
		if re.Flags&syntax.FoldCase != 0 {
			return strings.ToLower(string(re.Rune))
		}
		return string(re.Rune)
	case syntax.OpCapture:
		return longestLiteral(re.Sub[0])
	case syntax.OpPlus:
		return longestLiteral(re.Sub[0])
	case syntax.OpConcat:
		best := ""
		for _, sub := range re.Sub {
			if lit := longestLiteral(sub); len(lit) > len(best) {
				best = lit
			}
		}
		return best
	default: // alternation, repetition that may be empty, classes: nothing certain
		return ""
	}
}

// IgnoringCase returns a copy of the plan whose regexes ignore case, to
// count what case:yes hid. The copy has no filters of its own.
func (p *Plan) IgnoringCase() *Plan {
	folded := *p
	folded.CaseSensitive = false
	folded.Filters, folded.KindFilter, folded.CaseFilter = nil, nil, nil
	terms := map[*Content]*Content{}
	folded.Pred = foldAnd(p.Pred, terms)
	folded.Terms = make([]*Content, len(p.Terms))
	for i, term := range p.Terms {
		folded.Terms[i] = terms[term]
	}
	return &folded
}

// foldPred copies p with every regex made case-insensitive, recording
// which copy replaced which content term.
func foldPred(p Pred, terms map[*Content]*Content) Pred {
	fold := func(re *regexp.Regexp) *regexp.Regexp { return regexp.MustCompile("(?i)" + re.String()) }
	switch p := p.(type) {
	case *And:
		return foldAnd(p, terms)
	case *Or:
		return &Or{Kids: foldKids(p.Kids, terms)}
	case *Not:
		return &Not{Kid: foldPred(p.Kid, terms)}
	case *Content:
		folded := &Content{Re: fold(p.Re), Literal: p.Literal, IgnoreCase: true, TermIndex: p.TermIndex}
		terms[p] = folded
		return folded
	case *Path:
		return &Path{Re: fold(p.Re)}
	case *Repo:
		return &Repo{Re: fold(p.Re)}
	case *Symbol:
		return &Symbol{Re: fold(p.Re)}
	case *Message:
		return &Message{Re: fold(p.Re)}
	default: // no regex: languages, authors, dates
		return p
	}
}

func foldAnd(p *And, terms map[*Content]*Content) *And {
	return &And{Kids: foldKids(p.Kids, terms)}
}

func foldKids(kids []Pred, terms map[*Content]*Content) []Pred {
	folded := make([]Pred, len(kids))
	for i, kid := range kids {
		folded[i] = foldPred(kid, terms)
	}
	return folded
}
