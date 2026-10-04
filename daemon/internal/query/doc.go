// Package query is the only parser of the query language, and the planner
// that turns a parsed query into what a search engine runs.
//
// Parse turns the text of the search box into a protocol.ParsedQuery: the
// syntax tree, the globals (case:, count:, type:), the mode (current files
// or commit history) and diagnostics, each with fix-its that edit the text.
// Complete suggests operators and operator values at the cursor. NewPlan
// lowers a query without errors into a Plan: a predicate tree of And, Or,
// Not and leaves such as Content, Path and Lang, plus the result kinds,
// paging and the Filter values whose hidden results engines count. Engines
// evaluate a Plan with Eval and Contributing; they never see query text.
//
// The grammar, informally:
//
//	query    = [ orExpr ]
//	orExpr   = andExpr { "OR" andExpr }
//	andExpr  = unary { [ "AND" ] unary }     a space is an implicit AND
//	unary    = [ "-" ] primary
//	primary  = "(" orExpr ")" | operator | term
//	operator = name ":" value
//	term     = "quoted phrase" | /regex/ | bare
//
// AND and OR are keywords only in uppercase, and AND binds tighter than OR.
//
// Invariants:
//   - Parse never fails: every problem becomes a diagnostic, and NewPlan
//     refuses a query with error diagnostics, so it never runs.
//   - Every offset in a span or fix is in UTF-16 code units, as JavaScript
//     counts them.
//   - Parse and Complete are pure apart from the Resolver, which only looks
//     up author and repo names for labels and completions.
package query
