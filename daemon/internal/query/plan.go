package query

import (
	"errors"
	"fmt"
	"regexp"
	"regexp/syntax"
	"strconv"
	"strings"
	"time"

	"github.com/cruisinme30/spelunk/daemon/internal/lang"
	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
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
	// (case:, word:, count:, type: and order: shape the plan and have no predicate).
	Pred          *And
	CaseSensitive bool
	// WholeWord means text terms match only whole words: word:, or the
	// wholeWord setting.
	WholeWord bool
	// DiffSide limits a history search's text terms to the lines commits
	// added (TypeAdded, from type:added) or removed (TypeRemoved), leaving
	// messages out; "" searches both sides of diffs and the messages.
	DiffSide string
	// Order is how current files are sorted: order:, or the order setting.
	Order protocol.ResultOrder
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
	// CaseFilter is set when the query says case:yes, or smart case (from
	// case:smart or the setting) matches case, so engines can count the
	// matches that differ only in case ("1 match hidden by case:yes").
	CaseFilter *Filter
	// WordFilter is set when the query says word:yes, so engines can count
	// the matches that are only parts of words ("4 matches hidden by word:yes").
	WordFilter *Filter
	// SymbolFilter is set when the query has sym:. Its undo searches the
	// symbol names as text, and the server counts what that finds ("Search
	// RetryPolicy as text · 23").
	SymbolFilter *Filter
	// Terms are the text terms in query order, for highlighting.
	Terms []*Content
}

// Filter is part of a query that may hide results. Engines count what each
// filter alone hid, and the panel shows the count with an undo.
type Filter struct {
	// Reason is the HiddenNote reason that names the kind of filter:
	// "pathFilter" (-f:), "not" (any other negation), "since" (since:),
	// "kind" (kind:), and, for Plan.KindFilter, Plan.CaseFilter,
	// Plan.WordFilter and Plan.SymbolFilter, "type", "case", "word" and
	// "symbol".
	Reason string
	// Index is the position of the filter's conjunct in Plan.Pred.Kids, so
	// an engine can tell which filter a result failed. It is -1 for
	// KindFilter, CaseFilter, WordFilter and SymbolFilter, which have no conjunct.
	Index int
	// Text is the filter as typed, e.g. -f:vendor/.
	Text string
	// Undo is the fix that removes the filter from the query.
	Undo protocol.Fix
}

// Note is the hidden-results note for count results, counted in unit, that
// only f hid.
func (f *Filter) Note(count int, unit string) protocol.HiddenNote {
	return protocol.HiddenNote{Reason: f.Reason, Filter: f.Text, Count: count, Unit: unit, Undo: f.Undo}
}

// ExcludesRepo reports whether a top-level repo: rules out the whole repo
// named name.
func (p *Plan) ExcludesRepo(name string) bool {
	for _, kid := range p.Pred.Kids {
		if r, ok := kid.(*Repo); ok && !r.Re.MatchString(name) {
			return true
		}
	}
	return false
}

// OnPage reports whether the nth result (counting from 0) falls on the
// requested page.
func (p *Plan) OnPage(n int) bool {
	return n >= p.Offset && n < p.Offset+p.Limit
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
		// WholeWord keeps only the matches of Re that are whole words
		// (word:yes). Match with the term's Find methods, not Re's, so it
		// applies.
		WholeWord bool
		// Fuzzy lets the term also match a file name whose characters it
		// has in order, as Quick Open does: usrsvc finds UserService.ts.
		// Only a bare word outside a NOT, without word:yes, is fuzzy, and
		// only file names match it so; code lines stay exact.
		Fuzzy bool
		// ContentOnly is set for content:, which matches file text (or a
		// commit's changed lines) and never file names or commit messages.
		ContentOnly bool
		TermIndex   int
		// Reference marks a ref: term: a whole-word use of a name, never on
		// a line that defines that name. The working-tree engine finds the
		// file's definitions and drops those lines (see IsDefinitionOf).
		Reference bool
		// resumable means Re can be matched from inside a text (canResume),
		// so a match that isn't a whole word can be retried a rune later.
		resumable bool
	}
	// Path matches the repo-relative path (f:).
	Path struct{ Re *regexp.Regexp }
	// Repo matches the repo's display name (repo:).
	Repo struct{ Re *regexp.Regexp }
	// Lang matches the file's language (lang:).
	Lang struct{ Name string }
	// Symbol matches a symbol definition's name (sym:).
	Symbol struct{ Re *regexp.Regexp }
	// Kind matches what a symbol definition is (kind:): one of SymbolKinds.
	Kind struct{ Name protocol.SymbolKind }
	// Author matches a commit author's name or email, after .mailmap (author:).
	Author struct {
		Fragment string // lowercase; a substring of name or email
		Exact    bool   // a "quoted full name" must equal the name
	}
	// Message matches a commit's subject and body (msg:).
	Message struct{ Re *regexp.Regexp }
	// Is matches files in a state (is:): open in the editor, with uncommitted
	// changes, or holding tests. State is one of StateOpen, StateChanged, StateTest.
	Is struct{ State string }
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
func (*Kind) isPred()    {}
func (*Author) isPred()  {}
func (*Message) isPred() {}
func (*Since) isPred()   {}
func (*Is) isPred()      {}

func joinPreds(kind string, kids []Pred) string {
	parts := make([]string, len(kids))
	for i, k := range kids {
		parts[i] = k.String()
	}
	return kind + "(" + strings.Join(parts, " ") + ")"
}

func (p *And) String() string { return joinPreds("and", p.Kids) }
func (p *Or) String() string  { return joinPreds("or", p.Kids) }
func (p *Not) String() string { return "not(" + p.Kid.String() + ")" }
func (p *Content) String() string {
	if p.Reference {
		return fmt.Sprintf("ref#%d:/%s/", p.TermIndex, p.Re)
	}
	only := ""
	if p.ContentOnly {
		only = "only"
	}
	if p.WholeWord {
		return fmt.Sprintf("content%s#%d:word/%s/", only, p.TermIndex, p.Re)
	}
	return fmt.Sprintf("content%s#%d:/%s/", only, p.TermIndex, p.Re)
}
func (p *Path) String() string    { return "path:/" + p.Re.String() + "/" }
func (p *Repo) String() string    { return "repo:/" + p.Re.String() + "/" }
func (p *Lang) String() string    { return "lang:" + p.Name }
func (p *Symbol) String() string  { return "sym:/" + p.Re.String() + "/" }
func (p *Kind) String() string    { return "kind:" + p.Name }
func (p *Message) String() string { return "msg:/" + p.Re.String() + "/" }
func (p *Is) String() string      { return "is:" + p.State }
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
		CaseSensitive: matchesCase(q, settings.CaseSensitive),
		WholeWord:     settings.WholeWord,
		Limit:         pageSize(q.Globals.Count, settings.DefaultCount),
		Offset:        offset,
		Pred:          &And{},
	}
	if t := q.Globals.Type; t != nil && (*t == TypeAdded || *t == TypeRemoved) {
		plan.DiffSide = *t
	}
	if q.Globals.Word != "" {
		plan.WholeWord = q.Globals.Word == "yes"
	}
	plan.Order = resultOrder(q.Globals.Order, settings.Order)

	l := lowering{caseSensitive: plan.CaseSensitive, wholeWord: plan.WholeWord, now: now, plan: plan, nextRefIndex: textTermCount(q.Root)}
	src := newSource(q.Raw)
	for _, node := range topLevel(q.Root) {
		pred := l.lower(node)
		if pred == nil { // a global: case:, word:, count:, type: or order:
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
	if plan.CaseSensitive && q.Globals.Case == nil && settings.CaseSensitive == protocol.CaseSettingSmart {
		// Smart case from the setting can surprise, so it is undone like case:smart.
		plan.CaseFilter = &Filter{Reason: "case", Index: -1, Text: "case:smart", Undo: insertFix("Ignore case", 0, "case:no ")}
	}
	plan.SymbolFilter = symbolFilter(q.Root, src)
	return plan, append(historyScanWarnings(plan), emptyWindowWarnings(q.Root, src, now)...), nil
}

// emptyWindowWarnings warns when a top-level until: ends before a
// top-level since: starts, so no change can be inside both. It's a
// warning, so the search still runs (and finds nothing).
func emptyWindowWarnings(root *protocol.Node, src *source, now time.Time) []protocol.Diagnostic {
	var startsAt, endsAt []*protocol.Node
	for _, node := range topLevel(root) {
		switch {
		case node.Kind != protocol.NodeKindOp:
		case node.Op == protocol.OpNameSince:
			startsAt = append(startsAt, node)
		case node.Op == protocol.OpNameUntil:
			endsAt = append(endsAt, node)
		}
	}
	var problems diagnostics
	for _, u := range endsAt {
		end := until(now, u.Value)
		for _, s := range startsAt {
			if end.After(since(now, s.Value)) {
				continue
			}
			problems.add(protocol.SeverityWarning, DiagBadValue, u.Span, fmt.Sprintf(
				"%s ends before %s starts, so nothing can match both",
				src.slice(u.Span.Start, u.Span.End), src.slice(s.Span.Start, s.Span.End)))
			break
		}
	}
	return problems.list
}

// textTermCount counts the query's text terms. ref: terms are numbered
// after them, so each has a highlight color of its own.
func textTermCount(root *protocol.Node) int {
	count := 0
	walk(root, func(n *protocol.Node) {
		if n.Kind == protocol.NodeKindText {
			count++
		}
	})
	return count
}

// symbolFilter turns every sym: of the query into its value as text, or
// returns nil when there is none.
func symbolFilter(root *protocol.Node, src *source) *Filter {
	var edits []protocol.TextEdit
	var texts, values []string
	walk(root, func(n *protocol.Node) {
		if n.Kind != protocol.NodeKindOp || n.Op != protocol.OpNameSym {
			return
		}
		value := src.slice(n.Span.Start, n.Span.End)[len("sym:"):] // as typed: quoted or /regex/
		edits = append(edits, protocol.TextEdit{Span: n.Span, NewText: value})
		texts, values = append(texts, src.slice(n.Span.Start, n.Span.End)), append(values, value)
	})
	if len(edits) == 0 {
		return nil
	}
	return &Filter{
		Reason: "symbol", Index: -1, Text: strings.Join(texts, " "),
		Undo: protocol.Fix{Title: "Search " + strings.Join(values, " ") + " as text", Edits: edits},
	}
}

// matchesCase resolves case: (yes, no or smart), or the caseSensitive
// setting when the query doesn't say: smart matches case only when the
// query has a capital letter.
func matchesCase(q protocol.ParsedQuery, setting protocol.CaseSetting) bool {
	mode := setting
	if q.Globals.Case != nil {
		switch *q.Globals.Case {
		case "yes":
			mode = protocol.CaseSettingOn
		case "no":
			mode = protocol.CaseSettingOff
		case "smart":
			mode = protocol.CaseSettingSmart
		}
	}
	switch mode {
	case protocol.CaseSettingOn:
		return true
	case protocol.CaseSettingSmart:
		return q.HasCapital
	default:
		return false
	}
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

// resultOrder is order: if given, otherwise the order setting, and best
// match first when neither says.
func resultOrder(order, setting protocol.ResultOrder) protocol.ResultOrder {
	switch {
	case order != "":
		return order
	case setting == protocol.ResultOrderPath:
		return setting
	default:
		return protocol.ResultOrderBest
	}
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

// addGlobalFilter records a type:, case:yes, case:smart or word:yes global
// as a filter whose hidden results the engine counts (the type:file,
// type:added, case:yes and word:yes notes).
func (p *Plan) addGlobalFilter(node *protocol.Node, src *source) {
	if node.Kind != protocol.NodeKindOp {
		return
	}
	text := src.slice(node.Span.Start, node.Span.End)
	switch {
	case node.Op == protocol.OpNameType && p.DiffSide != "":
		// Removing it could leave nothing that searches commits.
		undo := replaceFix("Search every changed line", node.Span, "type:commit")
		p.KindFilter = &Filter{Reason: "type", Index: -1, Text: text, Undo: undo}
	case node.Op == protocol.OpNameType:
		p.KindFilter = &Filter{Reason: "type", Index: -1, Text: text, Undo: removeFix("Remove "+text, src, node.Span)}
	case node.Op == protocol.OpNameCase && p.CaseSensitive:
		undo := removeFix("Ignore case", src, node.Span)
		if node.Value == "smart" { // removing it may leave the setting's smart case on
			undo = replaceFix("Ignore case", node.Span, "case:no")
		}
		p.CaseFilter = &Filter{Reason: "case", Index: -1, Text: text, Undo: undo}
	case node.Op == protocol.OpNameWord && p.WholeWord:
		p.WordFilter = &Filter{Reason: "word", Index: -1, Text: text, Undo: removeFix("Match parts of words", src, node.Span)}
	}
}

// resultKinds decides which kinds of result the query returns.
func resultKinds(q protocol.ParsedQuery) map[ResultKind]bool {
	if q.Mode == protocol.ModeHistory {
		return map[ResultKind]bool{KindCommit: true}
	}
	wantsSymbols, wantsReferences := false, false
	walk(q.Root, func(n *protocol.Node) {
		if n.Kind == protocol.NodeKindOp && (n.Op == protocol.OpNameSym || n.Op == protocol.OpNameKind) {
			wantsSymbols = true
		}
		if n.Kind == protocol.NodeKindOp && n.Op == protocol.OpNameRef {
			wantsReferences = true
		}
	})
	kinds := map[ResultKind]bool{}
	switch {
	case q.Globals.Type != nil && *q.Globals.Type == "file":
		kinds[KindFile] = true
	case wantsSymbols:
		kinds[KindSymbol] = true
	case q.Globals.Type != nil && *q.Globals.Type == "code", wantsReferences:
		// A use of a name is a line of code; a file named after it is not.
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
	wholeWord     bool
	negated       bool // lowering a NOT's kid
	now           time.Time
	plan          *Plan
	// nextRefIndex is the term index the next ref: term gets.
	nextRefIndex int
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
		negated := l.negated
		l.negated = true
		kid := l.lower(node.Child)
		l.negated = negated
		if kid != nil {
			return &Not{Kid: kid}
		}
		return nil
	case protocol.NodeKindText:
		content := l.term(node, l.wholeWord, node.TermIndex)
		content.ContentOnly = node.ContentOnly
		content.Fuzzy = node.Match == protocol.MatchLiteral && !l.wholeWord && !l.negated && !node.ContentOnly
		return content
	default:
		if node.Op == protocol.OpNameRef {
			content := l.term(node, true, l.nextRefIndex)
			content.Reference = true
			l.nextRefIndex++
			return content
		}
		return l.lowerOperator(node)
	}
}

// term lowers a text term, or the name of a ref:, into a content term.
func (l *lowering) term(node *protocol.Node, wholeWord bool, termIndex int) *Content {
	content := &Content{Re: l.regex(node.Value, node.Match), IgnoreCase: !l.caseSensitive, WholeWord: wholeWord, TermIndex: termIndex}
	content.resumable = content.WholeWord && canResume(content.Re)
	if node.Match != protocol.MatchRegex {
		content.Literal = node.Value
	}
	l.plan.Terms = append(l.plan.Terms, content)
	return content
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
			pattern = regexp.QuoteMeta(node.Value) // like text, a literal matches anywhere in the name
		}
		return &Symbol{Re: l.compile(pattern)}
	case protocol.OpNameKind:
		return &Kind{Name: node.Value}
	case protocol.OpNameAuthor:
		return &Author{Fragment: strings.ToLower(node.Value), Exact: node.Match == protocol.MatchPhrase}
	case protocol.OpNameMsg:
		return &Message{Re: l.regex(node.Value, node.Match)}
	case protocol.OpNameIs:
		return &Is{State: node.Value}
	case protocol.OpNameSince:
		return &Since{After: since(l.now, node.Value)}
	case protocol.OpNameUntil:
		// Changes up to the end are those not since it, so engines need no until:.
		return &Not{Kid: &Since{After: until(l.now, node.Value)}}
	default: // case:, word:, count:, type:, order: shape the plan, not the predicate
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
// back from now: <n>min and <n>h are minutes and hours, <n>d days, <n>w
// weeks, <n>m calendar months and <n>y calendar years, so since:6m on
// October 3 starts on April 3. today starts at midnight in now's time zone,
// and yesterday at the midnight before. A date starts at its midnight, and
// a month (2026-09) at the midnight its first day starts, in now's time
// zone. value must be one the parser accepted (see asWindow).
func since(now time.Time, value string) time.Time {
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	switch strings.ToLower(value) {
	case sinceToday:
		return midnight
	case sinceYesterday:
		return midnight.AddDate(0, 0, -1)
	}
	if datePattern.MatchString(value) {
		year, month, day, _ := dateParts(value)
		return time.Date(year, time.Month(month), day, 0, 0, 0, 0, now.Location())
	}
	parts := durationPattern.FindStringSubmatch(value)
	n, _ := strconv.Atoi(parts[1])
	switch parts[2] {
	case "min":
		return now.Add(-time.Duration(n) * time.Minute)
	case "h":
		return now.Add(-time.Duration(n) * time.Hour)
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

// until returns when the period named by an until: value ends: until:
// keeps the changes before it. A day or a month ends at the midnight after
// it, so until:2026-09 and until:2026-09-30 end when October 1 starts;
// until:today ends at the coming midnight and until:yesterday at the last
// one. A length of time ends where since: would start (until:2w keeps what
// is older than two weeks). value must be one the parser accepted.
func until(now time.Time, value string) time.Time {
	start := since(now, value)
	switch {
	case strings.EqualFold(value, sinceToday), strings.EqualFold(value, sinceYesterday):
		return start.AddDate(0, 0, 1)
	case datePattern.MatchString(value):
		if _, _, _, hasDay := dateParts(value); hasDay {
			return start.AddDate(0, 0, 1)
		}
		return start.AddDate(0, 1, 0)
	default:
		return start
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
	case node.Kind == protocol.NodeKindOp && (node.Op == protocol.OpNameSince || node.Op == protocol.OpNameUntil):
		return "since" // until: is a time window too, so its note reads like since:'s
	case node.Kind == protocol.NodeKindOp && node.Op == protocol.OpNameKind:
		return "kind"
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

// VisitPositive calls visit for every leaf of p of type T that isn't
// negated.
func VisitPositive[T Pred](p Pred, visit func(T)) {
	switch p := p.(type) {
	case *And:
		for _, kid := range p.Kids {
			VisitPositive(kid, visit)
		}
	case *Or:
		for _, kid := range p.Kids {
			VisitPositive(kid, visit)
		}
	case T:
		visit(p)
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
// or more characters and nothing else narrows the commits: the trigram
// index can't help, so the search scans every commit (within engine.Budget).
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

// narrowsHistory reports whether a top-level author:, since:, until: or f:
// limits the commits to scan.
func narrowsHistory(p *And) bool {
	for _, kid := range p.Kids {
		switch kid := kid.(type) {
		case *Author, *Since, *Path:
			return true
		case *Not: // until: is lowered to not(since:)
			if _, ok := kid.Kid.(*Since); ok {
				return true
			}
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
	fold := func(re *regexp.Regexp) *regexp.Regexp { return regexp.MustCompile("(?i)" + re.String()) }
	folded := p.relaxed(func(c *Content) { c.Re, c.IgnoreCase = fold(c.Re), true }, fold)
	folded.CaseSensitive = false
	return folded
}

// MatchingPartialWords returns a copy of the plan whose text terms match
// parts of words too, to count what word:yes hid. The copy has no filters
// of its own.
func (p *Plan) MatchingPartialWords() *Plan {
	partial := p.relaxed(func(c *Content) { c.WholeWord = c.Reference }, nil) // ref: is always whole words
	partial.WholeWord = false
	return partial
}

// OnEitherSide returns a copy of the plan whose text terms match both
// sides of diffs and the messages, to count what type:added or
// type:removed hid. The copy has no filters of its own.
func (p *Plan) OnEitherSide() *Plan {
	either := p.relaxed(func(*Content) {}, nil)
	either.DiffSide = ""
	return either
}

// relaxed copies the plan without its filters, changing each content term
// with term and every other regex with re (left alone when re is nil).
func (p *Plan) relaxed(term func(*Content), re func(*regexp.Regexp) *regexp.Regexp) *Plan {
	copied := *p
	copied.Filters, copied.KindFilter, copied.CaseFilter, copied.WordFilter = nil, nil, nil, nil
	r := relaxer{term: term, re: re, terms: map[*Content]*Content{}}
	if r.re == nil {
		r.re = func(re *regexp.Regexp) *regexp.Regexp { return re }
	}
	copied.Pred = r.and(p.Pred)
	copied.Terms = make([]*Content, len(p.Terms))
	for i, term := range p.Terms {
		copied.Terms[i] = r.terms[term]
	}
	return &copied
}

// relaxer copies a predicate tree for Plan.relaxed, recording which copy
// replaced which content term.
type relaxer struct {
	term  func(*Content)
	re    func(*regexp.Regexp) *regexp.Regexp
	terms map[*Content]*Content
}

func (r relaxer) pred(p Pred) Pred {
	switch p := p.(type) {
	case *And:
		return r.and(p)
	case *Or:
		return &Or{Kids: r.kids(p.Kids)}
	case *Not:
		return &Not{Kid: r.pred(p.Kid)}
	case *Content:
		copied := *p
		r.term(&copied)
		r.terms[p] = &copied
		return &copied
	case *Path:
		return &Path{Re: r.re(p.Re)}
	case *Repo:
		return &Repo{Re: r.re(p.Re)}
	case *Symbol:
		return &Symbol{Re: r.re(p.Re)}
	case *Message:
		return &Message{Re: r.re(p.Re)}
	default: // no regex: languages, authors, dates
		return p
	}
}

func (r relaxer) and(p *And) *And {
	return &And{Kids: r.kids(p.Kids)}
}

func (r relaxer) kids(kids []Pred) []Pred {
	copied := make([]Pred, len(kids))
	for i, kid := range kids {
		copied[i] = r.pred(kid)
	}
	return copied
}
