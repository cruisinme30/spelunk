package server

// preview/get and open/resolve: what a selected result looks like, and
// where opening it goes.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/cruisinme30/unified-search/daemon/internal/protocol"
	"github.com/cruisinme30/unified-search/daemon/internal/rpc"
)

func (s *Server) preview(_ context.Context, raw json.RawMessage) (any, error) {
	var params protocol.PreviewParams
	if err := decode(raw, &params); err != nil {
		return nil, err
	}
	ref, path, err := s.resolveRef(params.Ref)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, rpc.Errorf(protocol.CodeRefStale, "file no longer exists")
	}
	lines := strings.Split(string(data), "\n")
	for i := range lines {
		lines[i] = strings.TrimSuffix(lines[i], "\r") // same lines the search saw
	}
	if ref.line < 1 || ref.line > len(lines) {
		return nil, rpc.Errorf(protocol.CodeRefStale, "line no longer exists")
	}
	first := max(1, ref.line-params.ContextLines)
	last := min(len(lines), ref.line+params.ContextLines)
	return protocol.Preview{
		Kind: "file", Path: ref.path, FirstLine: first, Lines: lines[first-1 : last], FocusLine: ref.line,
		Hits: []protocol.LineHits{{Line: ref.line, Ranges: []protocol.Hit{{Start: ref.column, End: ref.column + ref.length}}}},
	}, nil
}

func (s *Server) resolve(_ context.Context, raw json.RawMessage) (any, error) {
	var params protocol.OpenResolveParams
	if err := decode(raw, &params); err != nil {
		return nil, err
	}
	ref, path, err := s.resolveRef(params.Ref)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		return nil, rpc.Errorf(protocol.CodeRefStale, "file no longer exists")
	} else if err != nil {
		return nil, fmt.Errorf("stat %s: %w", ref.path, err)
	}
	// OpenTarget.Column is 1-based, like VS Code's UI; ref.column is 0-based.
	return protocol.OpenTarget{Path: path, Line: ref.line, Column: ref.column + 1, Length: ref.length}, nil
}
