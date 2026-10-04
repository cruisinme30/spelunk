// Package query owns the only parser of the query language (specified in
// docs/dev/implementation-plan.md): Parse turns the search box into a
// protocol.ParsedQuery with diagnostics and fix-its, and Complete suggests
// operators and values at the cursor.
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
// All offsets in spans are UTF-16 code units, as JavaScript counts them.
package query
