package query

import (
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

const (
	KindFile   ResultKind = "file"
	KindLine   ResultKind = "line"
	KindSymbol ResultKind = "symbol"
	KindCommit ResultKind = "commit"
)

// Plan is what an engine runs (Contract 4). Engines never see query text.
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
	// Terms are the positive text terms, for highlighting.
	Terms []*Content
}

// Filter is a top-level conjunct that may hide results.
type Filter struct {
	Reason string // a protocol HiddenNote reason: not, since, pathFilter
	Index  int    // which of Pred's top-level kids it is
	Text   string // as typed, e.g. -f:vendor/
	Undo   protocol.Fix
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
		Literal   string
		TermIndex int
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
		return nil, nil, fmt.Errorf("empty query")
	}
	plan := &Plan{Mode: q.Mode, CaseSensitive: settings.CaseSensitive, Limit: settings.DefaultCount}
	if q.Globals.Case != nil {
		plan.CaseSensitive = *q.Globals.Case == "yes"
	}
	switch count := q.Globals.Count.(type) {
	case int:
		plan.Limit = count
	case float64: // after a JSON round trip
		plan.Limit = int(count)
	case string: // "all"
		plan.Limit = MaxResults
	}
	plan.Limit = min(max(plan.Limit, 1), MaxResults)
	if cursor != "" {
		offset, err := strconv.Atoi(cursor)
		if err != nil || offset < 0 {
			return nil, nil, fmt.Errorf("bad cursor %q", cursor)
		}
		plan.Offset = offset
	}
	plan.Kinds = resultKinds(q)

	l := lowering{caseSensitive: plan.CaseSensitive, now: now, plan: plan}
	src := newSource(q.Raw)
	plan.Pred = &And{}
	for _, node := range topLevel(q.Root) {
		pred := l.lower(node)
		if pred == nil {
			continue // a global
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

// resultKinds decides which kinds of result the query returns.
func resultKinds(q protocol.ParsedQuery) map[ResultKind]bool {
	if q.Mode == protocol.ModeHistory {
		return map[ResultKind]bool{KindCommit: true}
	}
	wantsSymbols := false
	walk(q.Root, func(n *protocol.Node) {
		if n.Kind == "op" && n.Op == protocol.OpNameSym {
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
	case "and", "or":
		var kids []Pred
		for i := range node.Children {
			if kid := l.lower(&node.Children[i]); kid != nil {
				kids = append(kids, kid)
			}
		}
		if node.Kind == "or" {
			return &Or{Kids: kids}
		}
		return &And{Kids: kids}
	case "not":
		if kid := l.lower(node.Child); kid != nil {
			return &Not{Kid: kid}
		}
		return nil
	case "text":
		content := &Content{Re: l.regex(node.Value, node.Match), TermIndex: node.TermIndex}
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
	if match != protocol.MatchRegex {
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

// since turns 30d, 2w, 6m or 1y into the start of that window.
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
// how the hidden-results note describes it ("" if it can't).
func filterReason(node *protocol.Node) string {
	switch {
	case node.Kind == "not" && node.Child.Kind == "op" && node.Child.Op == protocol.OpNameF:
		return "pathFilter"
	case node.Kind == "not":
		return "not"
	case node.Kind == "op" && node.Op == protocol.OpNameF:
		return "pathFilter"
	case node.Kind == "op" && node.Op == protocol.OpNameSince:
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
			any := false
			for _, kid := range p.Kids {
				if visit(kid) {
					any = true
				}
			}
			return any
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
