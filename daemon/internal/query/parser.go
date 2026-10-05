package query

import (
	"fmt"
	"strings"

	"github.com/cruisinme30/unified-search/daemon/internal/protocol"
)

// Parse parses the search box. resolver supplies the "→ Jane Doe" labels
// for author: and repo: values; it may be nil. Parse never fails: every
// problem becomes a diagnostic, and a query with error diagnostics must not
// run.
func Parse(text string, resolver Resolver) protocol.ParsedQuery {
	if resolver == nil {
		resolver = noResolver{}
	}
	src := newSource(text)
	p := &parser{src: src, tokens: lex(src), resolver: resolver}
	checkLength(src, &p.problems)
	root := p.parseQuery()
	query := protocol.ParsedQuery{
		Version: 1,
		Raw:     text,
		Root:    root,
		Mode:    protocol.ModeWorkingTree,
	}
	check(src, &query, &p.problems)
	query.Diagnostics = sortedBySpan(p.problems.list)
	return query
}

// parseTree returns the syntax tree of text without checking it. Fixes use
// it to look at a rewritten query: a full Parse would build that query's
// fixes too, and the fixes for misplaced globals parse the query again, so
// "x -case:yes -case:yes …" took factorial time.
func parseTree(text string) *protocol.Node {
	src := newSource(text)
	p := &parser{src: src, tokens: lex(src), resolver: noResolver{}}
	return p.parseQuery()
}

// parser is a recursive-descent parser over the token list.
type parser struct {
	src      *source
	tokens   []token
	pos      int
	resolver Resolver
	problems diagnostics
	// nextTermIndex numbers text terms in query order, for highlight colors.
	nextTermIndex int
	// depth is how many groups enclose the current position.
	depth int
}

// maxGroupDepth is how deeply groups may nest before the parser skips
// what's inside them. Each "(" is a character, so only a query already too
// long to run nests deeper; without the cap, a pasted run of "(" recursed
// until the stack overflowed and took the daemon down.
const maxGroupDepth = maxQueryLength

func (p *parser) peek() token { return p.tokens[p.pos] }

func (p *parser) at(kind tokenKind) bool { return p.tokens[p.pos].kind == kind }

func (p *parser) advance() token {
	t := p.tokens[p.pos]
	if t.kind != tokenEnd {
		p.pos++
	}
	return t
}

// startsOperand reports whether a token can begin a unary expression.
func startsOperand(kind tokenKind) bool {
	return kind == tokenTerm || kind == tokenOperator || kind == tokenMinus || kind == tokenOpenParen
}

func (*parser) span(t token) protocol.Span { return protocol.Span{Start: t.start, End: t.end} }

// parseQuery parses top-level expressions. A stray ")" is reported and
// skipped, and what follows is joined to what came before with AND.
func (p *parser) parseQuery() *protocol.Node {
	var parts []*protocol.Node
	for !p.at(tokenEnd) {
		if p.at(tokenCloseParen) {
			t := p.advance()
			p.problems.errorf(DiagUnmatchedParen, p.span(t),
				[]protocol.Fix{removeFix("Remove the extra )", p.src, p.span(t))},
				"This ) has no matching (")
			continue
		}
		before := p.pos
		node := p.parseOr()
		switch {
		case node == nil:
		case node.Kind == protocol.NodeKindAnd:
			// Keep one flat top-level AND, where globals belong: "case:yes
			// a ) b" is case:yes AND a AND b, not (case:yes AND a) AND b.
			for i := range node.Children {
				parts = append(parts, &node.Children[i])
			}
		default:
			parts = append(parts, node)
		}
		if p.pos == before { // nothing consumed: a stray keyword was reported; skip it
			p.advance()
		}
	}
	return joinNodes(protocol.NodeKindAnd, parts)
}

func (p *parser) parseOr() *protocol.Node {
	var branches []*protocol.Node
	if first := p.parseAnd(); first != nil {
		branches = append(branches, first)
	}
	for p.at(tokenOr) {
		keyword := p.advance()
		if len(branches) == 0 {
			p.reportMissingOperand(keyword, "before")
		}
		next := p.parseAnd()
		if next == nil {
			p.reportMissingOperand(keyword, "after")
			continue
		}
		branches = append(branches, next)
	}
	return joinNodes(protocol.NodeKindOr, branches)
}

func (p *parser) parseAnd() *protocol.Node {
	var operands []*protocol.Node
	for {
		if p.at(tokenAnd) {
			keyword := p.advance()
			if len(operands) == 0 {
				p.reportMissingOperand(keyword, "before")
			} else if !startsOperand(p.peek().kind) {
				p.reportMissingOperand(keyword, "after")
			}
			continue
		}
		if !startsOperand(p.peek().kind) {
			break
		}
		if node := p.parseUnary(); node != nil {
			operands = append(operands, node)
		}
	}
	return joinNodes(protocol.NodeKindAnd, operands)
}

func (p *parser) reportMissingOperand(keyword token, side string) {
	word := p.src.slice(keyword.start, keyword.end)
	p.problems.errorf(DiagMissingOperand, p.span(keyword),
		[]protocol.Fix{removeFix("Remove "+word, p.src, p.span(keyword))},
		"Nothing %s %s to combine", side, word)
}

func (p *parser) parseUnary() *protocol.Node {
	if !p.at(tokenMinus) {
		return p.parsePrimary()
	}
	minus := p.advance()
	before := p.pos
	operand := p.parsePrimary()
	// An operand that was there but invalid, like -sinse:6m, has already
	// been reported; only a minus followed by nothing is missing one.
	if operand == nil && p.pos == before {
		p.problems.errorf(DiagMissingOperand, p.span(minus),
			[]protocol.Fix{removeFix("Remove -", p.src, p.span(minus))},
			"Nothing after - to exclude")
	}
	if operand == nil {
		return nil
	}
	return &protocol.Node{Kind: protocol.NodeKindNot, Child: operand, Span: protocol.Span{Start: minus.start, End: operand.Span.End}}
}

func (p *parser) parsePrimary() *protocol.Node {
	switch p.peek().kind {
	case tokenOpenParen:
		return p.parseGroup()
	case tokenTerm:
		return p.textNode(p.advance())
	case tokenOperator:
		return p.operatorNode(p.advance())
	default:
		return nil
	}
}

func (p *parser) parseGroup() *protocol.Node {
	if p.depth >= maxGroupDepth {
		p.skipGroup()
		return nil
	}
	open := p.advance()
	p.depth++
	inner := p.parseOr()
	p.depth--
	if !p.at(tokenCloseParen) {
		p.reportUnclosedParen(open, inner)
		return inner
	}
	closing := p.advance()
	if inner == nil {
		group := protocol.Span{Start: open.start, End: closing.end}
		p.problems.errorf(DiagEmptyGroup, group, []protocol.Fix{removeFix("Remove ()", p.src, group)}, "Empty parentheses")
	}
	return inner
}

// skipGroup consumes a group, up to its ")" or the end, without parsing it.
func (p *parser) skipGroup() {
	for open := 0; !p.at(tokenEnd); {
		switch p.advance().kind {
		case tokenOpenParen:
			open++
		case tokenCloseParen:
			open--
		default:
		}
		if open == 0 {
			return
		}
	}
}

// reportUnclosedParen offers to close the group where it most likely ends.
// After an OR, a group usually ends with the operand that follows it, so
// "(timeout OR retry -f:vendor/" closes after retry.
func (p *parser) reportUnclosedParen(open token, inner *protocol.Node) {
	var fixes []protocol.Fix
	if inner != nil {
		closeAfter := inner
		if inner.Kind == protocol.NodeKindOr {
			lastBranch := &inner.Children[len(inner.Children)-1]
			closeAfter = lastBranch
			if lastBranch.Kind == protocol.NodeKindAnd {
				closeAfter = &lastBranch.Children[0]
			}
		}
		closing := ")"
		// An unclosed quote or regex runs to the end of the text, so a ")"
		// inserted there would only join it; close it first.
		if last := p.tokens[len(p.tokens)-2]; last.unclosed != 0 && last.end == closeAfter.Span.End {
			closing = p.closing(last) + ")"
		}
		fixes = append(fixes, insertFix("Close it after "+p.src.excerpt(closeAfter.Span), closeAfter.Span.End, closing))
	}
	p.problems.errorf(DiagUnclosedParen, p.span(open), fixes, "Missing closing parenthesis")
}

func (p *parser) textNode(t token) *protocol.Node {
	p.reportUnclosed(t)
	match := protocol.MatchLiteral
	switch t.form {
	case formQuoted:
		match = protocol.MatchPhrase
	case formRegex:
		match = protocol.MatchRegex
		if problem := regexProblem(t.value); problem != "" && t.unclosed == 0 {
			p.problems.errorf(DiagInvalidRegex, p.span(t), nil, "%s", problem)
		}
	case formBare:
		// a plain word is matched literally
	}
	node := &protocol.Node{Kind: protocol.NodeKindText, Value: t.value, Match: match, TermIndex: p.nextTermIndex, Span: p.span(t)}
	p.nextTermIndex++
	return node
}

// reportUnclosed reports a quote or regex that never closed, with a fix
// that closes it at the end of the token.
func (p *parser) reportUnclosed(t token) {
	switch t.unclosed {
	case '"':
		p.problems.errorf(DiagUnclosedQuote, p.span(t), []protocol.Fix{insertFix(`Close the quote`, t.end, p.closing(t))}, "Missing closing quote")
	case '/':
		p.problems.errorf(DiagUnclosedRegex, p.span(t), []protocol.Fix{insertFix("Close the regex", t.end, p.closing(t))}, "Missing closing / of the regex")
	}
}

// closing is what closes an unclosed quote or regex. After a lone trailing
// backslash, the delimiter alone would be escaped and leave it open, so
// "abc\ closes as "abc\\": the backslash, escaped, then the quote.
func (p *parser) closing(t token) string {
	backslashes := 0
	for i := p.src.runeIndex(t.end) - 1; i > p.src.runeIndex(t.start) && p.src.runes[i] == '\\'; i-- {
		backslashes++
	}
	if backslashes%2 == 1 {
		return `\` + string(t.unclosed)
	}
	return string(t.unclosed)
}

// operatorNode builds an op node, or reports an unknown operator or bad value.
func (p *parser) operatorNode(t token) *protocol.Node {
	op, known := lookupOperator(t.name)
	if !known {
		p.reportUnknownOperator(t)
		return nil
	}
	p.reportUnclosed(t)
	valueSpan := protocol.Span{Start: t.valueStart, End: t.end}
	if t.value == "" && t.unclosed == 0 {
		p.reportBadValue(op, t, valueSpan, op.name+": needs a value")
		return nil
	}
	match, problem := op.interpret(t.value, t.form)
	switch {
	case problem != "" && match == protocol.MatchRegex:
		p.problems.errorf(DiagInvalidRegex, valueSpan, nil, "%s", problem)
		return nil
	case problem != "":
		p.reportBadValue(op, t, valueSpan, problem)
		return nil
	}
	if op.name == protocol.OpNameSince {
		p.warnIfMonthsMeantAsMinutes(t, valueSpan)
	}
	node := &protocol.Node{Kind: protocol.NodeKindOp, Op: op.name, Value: t.value, Match: match, Span: p.span(t)}
	node.Resolved = p.resolve(op.name, t.value)
	return node
}

// warnIfMonthsMeantAsMinutes warns about since:30m, which means 30 months,
// and offers since:30min. It's a warning, so the search still runs.
func (p *parser) warnIfMonthsMeantAsMinutes(t token, valueSpan protocol.Span) {
	minutes := monthsMeantAsMinutes(t.value)
	if minutes == "" {
		return
	}
	p.problems.add(protocol.SeverityWarning, DiagBadValue, p.span(t),
		fmt.Sprintf("since:%s means %s months; for minutes write since:%s", t.value, strings.TrimSuffix(t.value, "m"), minutes),
		replaceFix("Use since:"+minutes, valueSpan, minutes))
}

func (p *parser) reportUnknownOperator(t token) {
	nameSpan := protocol.Span{Start: t.start, End: t.valueStart}
	word := p.src.slice(t.start, t.end)
	var fixes []protocol.Fix
	for _, name := range nearest(strings.ToLower(t.name), operatorNames()) {
		fixes = append(fixes, replaceFix(fmt.Sprintf("Change to %s:", name), nameSpan, name+":"))
	}
	fixes = append(fixes, replaceFix("Search for the text instead", p.span(t), `"`+strings.ReplaceAll(word, `"`, `\"`)+`"`))
	p.problems.errorf(DiagUnknownOperator, nameSpan, fixes, "Unknown operator %s:", t.name)
}

func (p *parser) reportBadValue(op operator, t token, valueSpan protocol.Span, problem string) {
	var fixes []protocol.Fix
	examples := op.examples
	if op.name == protocol.OpNameLang {
		examples = nil
		for _, name := range nearestLanguages(t.value) {
			examples = append(examples, "lang:"+name)
		}
	}
	for _, example := range examples {
		value := strings.TrimPrefix(example, op.name+":")
		fixes = append(fixes, replaceFix("Use "+example, valueSpan, value))
	}
	p.problems.errorf(DiagBadValue, protocol.Span{Start: t.start, End: t.end}, fixes, "%s", problem)
}

// authorsToTellOneFromMany is how many authors resolve asks for: enough to
// know whether a value matches exactly one author.
const authorsToTellOneFromMany = 2

// resolve returns the friendlier label for an author: or repo: value, if
// exactly one author or repo matches. An author whose name equals the value
// (ignoring case) also counts, even if others contain it.
func (p *parser) resolve(op protocol.OpName, value string) *protocol.Resolved {
	switch op {
	case protocol.OpNameAuthor:
		authors := p.resolver.Authors(value, authorsToTellOneFromMany)
		if len(authors) == 1 || len(authors) > 1 && strings.EqualFold(authors[0].Name, value) {
			return &protocol.Resolved{Label: authors[0].Name}
		}
	case protocol.OpNameRepo:
		var matches []string
		for _, name := range repoNames(p.resolver) {
			if strings.Contains(strings.ToLower(name), strings.ToLower(value)) {
				matches = append(matches, name)
			}
		}
		if len(matches) == 1 && matches[0] != value {
			return &protocol.Resolved{Label: matches[0]}
		}
	}
	return nil
}

// joinNodes wraps two or more nodes in an and/or node; one node is returned as is.
func joinNodes(kind string, nodes []*protocol.Node) *protocol.Node {
	switch len(nodes) {
	case 0:
		return nil
	case 1:
		return nodes[0]
	}
	children := make([]protocol.Node, len(nodes))
	for i, n := range nodes {
		children[i] = *n
	}
	return &protocol.Node{Kind: kind, Children: children, Span: protocol.Span{Start: nodes[0].Span.Start, End: nodes[len(nodes)-1].Span.End}}
}
