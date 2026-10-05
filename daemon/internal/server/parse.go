package server

import (
	"context"
	"encoding/json"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
	"github.com/cruisinme30/spelunk/daemon/internal/query"
)

// parse answers query/parse: the parsed query for chips and diagnostics,
// plus completions at the cursor.
func (s *Server) parse(_ context.Context, raw json.RawMessage) (any, error) {
	var params protocol.ParseParams
	if err := decode(raw, &params); err != nil {
		return nil, err
	}
	resolver := s.resolver()
	parsed := query.Parse(params.Text, resolver)
	// A query without errors is also planned, so warnings only the planner
	// can give (such as a history regex that scans every commit) show too.
	if _, warnings, err := query.NewPlan(parsed, s.Settings(), s.now(), ""); err == nil {
		parsed.Diagnostics = append(parsed.Diagnostics, warnings...)
	}
	return protocol.ParseResult{
		Query:       parsed,
		Completions: query.Complete(params.Text, params.Cursor, resolver, s.now()),
	}, nil
}
