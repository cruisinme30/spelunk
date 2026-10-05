package server

// replace/plan: the edits that replacing a query's matches in current files
// would make, for the panel's preview and the host's WorkspaceEdit.

import (
	"context"
	"encoding/json"
	"time"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
	"github.com/cruisinme30/spelunk/daemon/internal/query"
	"github.com/cruisinme30/spelunk/daemon/internal/rpc"
	"github.com/cruisinme30/spelunk/daemon/internal/trigram"
)

const (
	// maxReplaceMatches is the most matches one replace changes. A query
	// with more gets only the count, so the panel can ask for a narrower one.
	maxReplaceMatches = 10_000
	// replaceBudget is how long planning a replace may take. Unlike a
	// search, which returns what it found in its budget, a replace that runs
	// out of time fails: part of a replace must never pass for all of it.
	replaceBudget = 10 * time.Second
)

// replacePlan answers replace/plan. The query must search current files
// for code and have exactly one text term outside a NOT, the one replaced.
func (s *Server) replacePlan(ctx context.Context, raw json.RawMessage) (any, error) {
	var params protocol.ReplacePlanParams
	if err := decode(raw, &params); err != nil {
		return nil, err
	}
	parsed := query.Parse(params.Text, s.resolver(params.OpenFiles))
	plan, _, err := query.NewPlan(parsed, s.Settings(), s.now(), "")
	if err != nil {
		return nil, rpc.Errorf(protocol.CodeQueryInvalid, "%v", err)
	}
	term, err := replacedTerm(plan)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeoutCause(ctx, replaceBudget,
		rpc.Errorf(protocol.CodeOverloaded, "finding every match took too long; narrow the search to replace"))
	defer cancel()
	repos := trigram.WithOpenFiles(s.index.Repos(), params.OpenFiles)
	return trigram.Replacements(ctx, plan, repos, term, params.Replacement, maxReplaceMatches)
}

// replacedTerm is the one text term a replace changes, or why plan can't
// be replaced.
func replacedTerm(plan *query.Plan) (*query.Content, error) {
	if plan.Mode != protocol.ModeWorkingTree {
		return nil, rpc.Errorf(protocol.CodeQueryInvalid, "replace changes current files, not commits")
	}
	if !plan.Kinds[query.KindLine] {
		return nil, rpc.Errorf(protocol.CodeQueryInvalid, "replace changes code matches, and this query finds none")
	}
	var terms []*query.Content
	query.VisitPositive(plan.Pred, func(term *query.Content) { terms = append(terms, term) })
	if len(terms) != 1 {
		return nil, rpc.Errorf(protocol.CodeQueryInvalid, "replace needs exactly one search term")
	}
	return terms[0], nil
}
