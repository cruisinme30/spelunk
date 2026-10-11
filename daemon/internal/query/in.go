package query

import (
	"fmt"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
)

// The values of in:, the parts of a file a match may be in.
const (
	InComments = "comments" // comments and Python docstrings
	InStrings  = "strings"  // string and character literals
	InCode     = "code"     // outside both
)

// Places says which parts of a file in: lets a match be in, one bit each;
// zero means anywhere.
type Places uint8

// The places in: tells apart.
const (
	PlaceComment Places = 1 << iota // comments and Python docstrings
	PlaceString                     // string and character literals
	PlaceCode                       // outside both
	everyPlace   = PlaceComment | PlaceString | PlaceCode
)

// Keeps reports whether in: keeps a match wholly in place (one of the
// Place bits), or, with place zero, one that runs across places, which
// only a negated in: keeps: -in:comments keeps a match that starts in
// code and ends in a comment, since it isn't inside one.
func (in Places) Keeps(place Places) bool {
	switch {
	case in == 0:
		return true
	case place == 0:
		return in != PlaceComment && in != PlaceString && in != PlaceCode
	default:
		return in&place != 0
	}
}

// inNode returns the query's top-level in: or -in: node (the -in:'s
// minus), its operator node and whether it is negated; nil when there is
// none.
func inNode(root *protocol.Node) (whole, op *protocol.Node, negated bool) {
	for _, node := range topLevel(root) {
		if isIn(node) {
			if node.Kind == protocol.NodeKindNot {
				return node, node.Child, true
			}
			return node, node, false
		}
	}
	return nil, nil, false
}

// isIn reports whether a top-level node is in: or -in:.
func isIn(node *protocol.Node) bool {
	if node.Kind == protocol.NodeKindNot && node.Child != nil {
		node = node.Child
	}
	return node.Kind == protocol.NodeKindOp && node.Op == protocol.OpNameIn
}

// addIn reads a top-level in: or -in: into In and InFilter. A file's name
// is in no comment or string, so in: searches file text only; and
// definitions are code, so in: drops them when it leaves code out.
func (p *Plan) addIn(root *protocol.Node, src *source) {
	whole, op, negated := inNode(root)
	if whole == nil {
		return
	}
	places := map[string]Places{InComments: PlaceComment, InStrings: PlaceString, InCode: PlaceCode}[op.Value]
	if negated {
		places = everyPlace &^ places
	}
	p.In = places
	text := src.slice(whole.Span.Start, whole.Span.End)
	p.InFilter = &Filter{Reason: "in", Index: -1, Text: text, Undo: removeFix("Show them", src, whole.Span)}
	delete(p.Kinds, KindFile)
	if !places.Keeps(PlaceCode) {
		delete(p.Kinds, KindSymbol)
	}
}

// Anywhere copies the plan without its in: and its other filters, to
// count what in: hid.
func (p *Plan) Anywhere() *Plan {
	copied := *p
	copied.Filters, copied.TypeFilter, copied.CaseFilter, copied.WordFilter, copied.MatchFilter = nil, nil, nil, nil, nil
	copied.In, copied.InFilter = 0, nil
	return &copied
}

// inWarnings warns when in: leaves out code but the query asks for what is
// only ever code, a struct: template or sym: and kind: definitions (not
// the lines inside them, which kind:function content:TODO in:comments
// finds), or asks for file names only (type:file), which are in no file's
// text. They are warnings, so the search still runs, and finds nothing.
func inWarnings(root *protocol.Node, src *source, plan *Plan) []protocol.Diagnostic {
	in := plan.In
	whole, _, _ := inNode(root)
	if whole == nil {
		return nil
	}
	written := src.slice(whole.Span.Start, whole.Span.End)
	var problems diagnostics
	walk(root, func(node *protocol.Node) {
		var finds string
		switch {
		case in.Keeps(PlaceCode):
		case node.Match == protocol.MatchStructural:
			finds = "struct: matches code"
		case node.Kind == protocol.NodeKindOp && (node.Op == protocol.OpNameSym || node.Op == protocol.OpNameKind) && !plan.InDefinitions:
			finds = node.Op + ": finds definitions, which are code"
		}
		if node.Kind == protocol.NodeKindOp && node.Op == protocol.OpNameType && node.Value == "file" {
			finds = "type:file finds file names, which are in no file's text"
		}
		if finds != "" {
			problems.add(protocol.SeverityWarning, DiagBadValue, node.Span, fmt.Sprintf("%s, so %s hides all of it", finds, written),
				removeFix("Remove "+written, src, whole.Span))
		}
	})
	return problems.list
}
