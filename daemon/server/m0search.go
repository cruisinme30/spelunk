package server

// M0 skeleton search: the whole query is one case-insensitive literal,
// matched by scanning files under each root. It exists to prove the
// round trip (host -> daemon -> search/batch -> result -> preview -> open)
// and is replaced by the planner and engines in M1.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"unifiedsearch/daemon/protocol"
	"unifiedsearch/daemon/rpc"
)

func (s *Server) registerM0() {
	s.conn.Handle(protocol.MethodQueryParse, s.m0Parse)
	s.conn.Handle(protocol.MethodSearchStart, s.m0Search)
	s.conn.Handle(protocol.MethodPreviewGet, s.m0Preview)
	s.conn.Handle(protocol.MethodOpenResolve, s.m0Resolve)
}

func utf16Len(s string) int { return len(utf16.Encode([]rune(s))) }

func (s *Server) m0Search(ctx context.Context, p json.RawMessage) (any, error) {
	var params protocol.SearchStartParams
	if err := decode(p, &params); err != nil {
		return nil, err
	}
	start := s.now()
	needle := strings.ToLower(strings.TrimSpace(params.Text))
	if needle == "" {
		return nil, rpc.Errorf(rpc.CodeQueryInvalid, "empty query")
	}
	var batch []protocol.ResultItem
	total := 0
	flush := func() {
		if len(batch) > 0 {
			_ = s.conn.Notify(protocol.MethodSearchBatch, protocol.SearchBatchParams{SearchID: params.SearchID, Items: batch})
			batch = nil
		}
	}
	for _, root := range s.Roots() {
		err := filepath.WalkDir(root.Path, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if d.IsDir() {
				if d.Name() == ".git" || d.Name() == "node_modules" {
					return filepath.SkipDir
				}
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil || bytes.IndexByte(data[:min(len(data), 8192)], 0) >= 0 {
				return nil
			}
			rel, _ := filepath.Rel(root.Path, path)
			rel = filepath.ToSlash(rel)
			sc := bufio.NewScanner(bytes.NewReader(data))
			sc.Buffer(make([]byte, 64<<10), 4<<20)
			for n := 1; sc.Scan(); n++ {
				line := sc.Text()
				i := strings.Index(strings.ToLower(line), needle)
				if i < 0 {
					continue
				}
				col := utf16Len(line[:i])
				ln := utf16Len(line[i : i+len(needle)])
				total++
				batch = append(batch, protocol.ResultItem{
					Kind: "line", RepoID: root.ID, Path: rel, Line: n, Text: line,
					Ref:  strings.Join([]string{"m0", root.ID, strconv.Itoa(n), strconv.Itoa(col), strconv.Itoa(ln), rel}, "|"),
					Hits: []protocol.Hit{{Start: col, End: col + ln, TermIndex: 0}},
				})
				if len(batch) >= 200 {
					flush()
				}
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	flush()
	return protocol.SearchResult{Total: total, Ms: int(time.Since(start).Milliseconds())}, nil
}

type m0Ref struct {
	root      protocol.Root
	line, col int
	length    int
	path      string
}

func (s *Server) m0ParseRef(ref string) (m0Ref, error) {
	parts := strings.SplitN(ref, "|", 6)
	if len(parts) != 6 || parts[0] != "m0" {
		return m0Ref{}, rpc.Errorf(rpc.CodeRefStale, "unknown ref")
	}
	for _, r := range s.Roots() {
		if r.ID == parts[1] {
			line, _ := strconv.Atoi(parts[2])
			col, _ := strconv.Atoi(parts[3])
			n, _ := strconv.Atoi(parts[4])
			return m0Ref{root: r, line: line, col: col, length: n, path: parts[5]}, nil
		}
	}
	return m0Ref{}, rpc.Errorf(rpc.CodeRefStale, "repo no longer open")
}

func (s *Server) m0Preview(ctx context.Context, p json.RawMessage) (any, error) {
	var params protocol.PreviewParams
	if err := decode(p, &params); err != nil {
		return nil, err
	}
	r, err := s.m0ParseRef(params.Ref)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(r.root.Path, filepath.FromSlash(r.path)))
	if err != nil {
		return nil, rpc.Errorf(rpc.CodeRefStale, "file no longer exists")
	}
	lines := strings.Split(string(data), "\n")
	if r.line > len(lines) {
		return nil, rpc.Errorf(rpc.CodeRefStale, "line no longer exists")
	}
	first := max(1, r.line-params.ContextLines)
	last := min(len(lines), r.line+params.ContextLines)
	return protocol.Preview{
		Kind: "file", Path: r.path, FirstLine: first, Lines: lines[first-1 : last], FocusLine: r.line,
		Hits: []protocol.LineHits{{Line: r.line, Ranges: []protocol.Hit{{Start: r.col, End: r.col + r.length}}}},
	}, nil
}

func (s *Server) m0Resolve(ctx context.Context, p json.RawMessage) (any, error) {
	var params protocol.OpenResolveParams
	if err := decode(p, &params); err != nil {
		return nil, err
	}
	r, err := s.m0ParseRef(params.Ref)
	if err != nil {
		return nil, err
	}
	abs := filepath.Join(r.root.Path, filepath.FromSlash(r.path))
	if _, err := os.Stat(abs); err != nil {
		return nil, rpc.Errorf(rpc.CodeRefStale, "file no longer exists")
	}
	return protocol.OpenTarget{Path: abs, Line: r.line, Column: r.col + 1, Length: r.length}, nil
}

// m0Parse treats the whole box as one literal text term (no operators yet).
func (s *Server) m0Parse(ctx context.Context, p json.RawMessage) (any, error) {
	var params protocol.ParseParams
	if err := decode(p, &params); err != nil {
		return nil, err
	}
	q := protocol.ParsedQuery{Version: 1, Raw: params.Text, Mode: protocol.ModeWorkingTree}
	if v := strings.TrimSpace(params.Text); v != "" {
		start := utf16Len(params.Text[:strings.Index(params.Text, v)])
		q.Root = &protocol.Node{Kind: "text", Value: v, Match: protocol.MatchLiteral,
			Span: protocol.Span{Start: start, End: start + utf16Len(v)}}
	}
	return protocol.ParseResult{Query: q}, nil
}
