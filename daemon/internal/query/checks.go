package query

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/cruisinme30/unified-search/daemon/internal/protocol"
)

// check applies the rules the grammar alone can't express ("Rules the
// parser enforces"), sets the query's globals and mode, and reports what
// breaks them.
func check(src *source, query *protocol.ParsedQuery, problems *diagnostics) {
	if src.length() > maxQueryLength {
		problems.errorf(DiagQueryTooLong, protocol.Span{Start: maxQueryLength, End: src.length()}, nil,
			"Queries can be at most %d characters", maxQueryLength)
	}
	if query.Root == nil {
		return
	}
	collectGlobals(src, query, problems)
	if usesHistory(query.Root) {
		query.Mode = protocol.ModeHistory
	}
	checkOrModes(query.Root, problems)
	if query.Mode == protocol.ModeHistory {
		reportWorkingTreeOperators(src, query.Root, problems)
	}
	if !hasPositiveTerm(query.Root) {
		problems.errorf(DiagNoPositiveTerm, query.Root.Span, nil,
			"Add something to search for: every term is excluded with -")
	}
}

// topLevel returns the nodes that are directly part of the top-level AND.
func topLevel(root *protocol.Node) []*protocol.Node {
	if root.Kind != protocol.NodeKindAnd {
		return []*protocol.Node{root}
	}
	nodes := make([]*protocol.Node, len(root.Children))
	for i := range root.Children {
		nodes[i] = &root.Children[i]
	}
	return nodes
}

// walk visits every node depth-first.
func walk(node *protocol.Node, visit func(*protocol.Node)) {
	visit(node)
	for i := range node.Children {
		walk(&node.Children[i], visit)
	}
	if node.Child != nil {
		walk(node.Child, visit)
	}
}

// collectGlobals fills query.Globals from top-level case:, count: and
// type:, and reports globals that are nested or repeated.
func collectGlobals(src *source, query *protocol.ParsedQuery, problems *diagnostics) {
	atTop := map[*protocol.Node]bool{}
	for _, node := range topLevel(query.Root) {
		atTop[node] = true
	}
	seen := map[protocol.OpName]bool{}
	walkWithParent(query.Root, nil, func(node, parent *protocol.Node) {
		if node.Kind != protocol.NodeKindOp {
			return
		}
		if op, _ := lookupOperator(node.Op); !op.global {
			return
		}
		written := src.slice(node.Span.Start, node.Span.End)
		switch {
		case !atTop[node]:
			// Moving a negated global drops its minus: "-count:5" isn't meaningful.
			removed := node.Span
			if parent != nil && parent.Kind == protocol.NodeKindNot {
				removed = parent.Span
			}
			move := moveToTopLevel(src, written, removeSpan(src, removed))
			problems.errorf(DiagGlobalMisplaced, node.Span, []protocol.Fix{move},
				"%s: applies to the whole query, so it can't be inside ( ), after - or in an OR branch", node.Op)
		case seen[node.Op]:
			problems.errorf(DiagDuplicateGlobal, node.Span, []protocol.Fix{removeFix("Remove "+written, src, node.Span)},
				"%s: can appear only once", node.Op)
		default:
			seen[node.Op] = true
			setGlobal(&query.Globals, node)
		}
	})
}

// moveToTopLevel rewrites the whole query as "<global> <rest>", where rest
// is the query without the global. If rest is an OR, it is wrapped in
// parentheses: AND binds tighter, so "case:yes a OR b" would put case:yes
// back inside an OR branch.
func moveToTopLevel(src *source, global string, removed protocol.Span) protocol.Fix {
	rest := strings.TrimSpace(src.slice(0, removed.Start) + src.slice(removed.End, src.length()))
	if root := Parse(rest, nil).Root; root != nil && root.Kind == protocol.NodeKindOr {
		rest = "(" + rest + ")"
	}
	whole := protocol.Span{Start: 0, End: src.length()}
	return replaceFix(fmt.Sprintf("Move %s to the top level", global), whole, global+" "+rest)
}

// walkWithParent visits every node depth-first along with its parent.
func walkWithParent(node, parent *protocol.Node, visit func(node, parent *protocol.Node)) {
	visit(node, parent)
	for i := range node.Children {
		walkWithParent(&node.Children[i], node, visit)
	}
	if node.Child != nil {
		walkWithParent(node.Child, node, visit)
	}
}

func setGlobal(globals *protocol.Globals, node *protocol.Node) {
	value := node.Value
	switch node.Op {
	case protocol.OpNameCase:
		globals.Case = &value
	case protocol.OpNameType:
		globals.Type = &value
	case protocol.OpNameCount:
		if value == "all" {
			globals.Count = "all"
		} else {
			n, _ := strconv.Atoi(value) // validated by asCount
			globals.Count = n
		}
	}
}

// needsHistory reports whether a subtree only makes sense over commits:
// it has author:, msg: or type:commit.
func needsHistory(node *protocol.Node) bool {
	found := false
	walk(node, func(n *protocol.Node) {
		if n.Kind == protocol.NodeKindOp && (n.Op == protocol.OpNameAuthor || n.Op == protocol.OpNameMsg ||
			n.Op == protocol.OpNameType && n.Value == "commit") {
			found = true
		}
	})
	return found
}

// usesHistory: a query is a history query if it needs history anywhere.
func usesHistory(root *protocol.Node) bool { return needsHistory(root) }

// checkOrModes reports an OR whose branches would need different modes:
// one result list can't mix commits and lines.
func checkOrModes(root *protocol.Node, problems *diagnostics) {
	walk(root, func(node *protocol.Node) {
		if node.Kind != protocol.NodeKindOr {
			return
		}
		history := 0
		for i := range node.Children {
			if needsHistory(&node.Children[i]) {
				history++
			}
		}
		if history > 0 && history < len(node.Children) {
			problems.errorf(DiagMixedModeOr, node.Span, nil,
				"Each side of OR must search the same thing: commits (author:, msg:) or current files")
		}
	})
}

// reportWorkingTreeOperators reports sym: in a history query.
func reportWorkingTreeOperators(src *source, root *protocol.Node, problems *diagnostics) {
	walk(root, func(node *protocol.Node) {
		if node.Kind != protocol.NodeKindOp {
			return
		}
		if op, _ := lookupOperator(node.Op); op.scope == scopeWorkingTreeOnly {
			written := src.slice(node.Span.Start, node.Span.End)
			problems.errorf(DiagOpWrongMode, node.Span, []protocol.Fix{removeFix("Remove "+written, src, node.Span)},
				"%s: searches current files, but this query searches commits (author:, msg: or type:commit)", node.Op)
		}
	})
}

// hasPositiveTerm reports whether anything is searched for outside a NOT.
// Globals (case:, count:, type:) don't count: they only shape results.
func hasPositiveTerm(node *protocol.Node) bool {
	switch node.Kind {
	case protocol.NodeKindNot:
		return false
	case protocol.NodeKindAnd, protocol.NodeKindOr:
		for i := range node.Children {
			if hasPositiveTerm(&node.Children[i]) {
				return true
			}
		}
		return false
	case protocol.NodeKindOp:
		op, _ := lookupOperator(node.Op)
		return !op.global
	default:
		return true
	}
}

// sortedBySpan orders diagnostics by where they start in the query.
func sortedBySpan(list []protocol.Diagnostic) []protocol.Diagnostic {
	if list == nil {
		return []protocol.Diagnostic{}
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].Span.Start < list[j].Span.Start })
	return list
}
