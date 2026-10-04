package server

// The M0 skeleton search proves the round trip (host -> daemon ->
// search/batch -> result -> preview -> open) before the real parser and
// engines exist. The whole query box is one case-insensitive literal,
// matched by scanning files. M1 replaces this file.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/cruisinme30/unified-search/daemon/internal/protocol"
	"github.com/cruisinme30/unified-search/daemon/internal/rpc"
)

const (
	binarySniffBytes  = 8 << 10 // a NUL in the first 8 KB means binary
	initialLineBuffer = 64 << 10
	maxLineBytes      = 4 << 20
	batchSize         = 200 // Contract 3: search/batch carries up to 200 items
)

func (s *Server) registerM0() {
	s.conn.Handle(protocol.MethodSearchStart, s.m0Search)
	s.conn.Handle(protocol.MethodPreviewGet, s.m0Preview)
	s.conn.Handle(protocol.MethodOpenResolve, s.m0Resolve)
}

// utf16Len is the length of s in UTF-16 code units, the unit of every protocol offset.
func utf16Len(s string) int { return len(utf16.Encode([]rune(s))) }

// batcher sends results as search/batch notifications of at most batchSize.
type batcher struct {
	conn     *rpc.Conn
	searchID string
	items    []protocol.ResultItem
}

func (b *batcher) add(item protocol.ResultItem) {
	b.items = append(b.items, item)
	if len(b.items) >= batchSize {
		b.flush()
	}
}

func (b *batcher) flush() {
	if len(b.items) == 0 {
		return
	}
	_ = b.conn.Notify(protocol.MethodSearchBatch, protocol.SearchBatchParams{SearchID: b.searchID, Items: b.items})
	b.items = nil
}

func (s *Server) m0Search(ctx context.Context, raw json.RawMessage) (any, error) {
	var params protocol.SearchStartParams
	if err := decode(raw, &params); err != nil {
		return nil, err
	}
	started := time.Now() // wall clock: Options.Now may be frozen
	term := strings.TrimSpace(params.Text)
	if term == "" {
		return nil, rpc.Errorf(protocol.CodeQueryInvalid, "empty query")
	}
	// Matching on the original line keeps offsets right even where
	// lowercasing would change a character's byte length.
	pattern := regexp.MustCompile("(?i)" + regexp.QuoteMeta(term))
	batch := &batcher{conn: s.conn, searchID: params.SearchID}
	total := 0
	for _, root := range s.Roots() {
		err := filepath.WalkDir(root.Path, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return nil // unreadable entries are skipped, not fatal
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if entry.IsDir() {
				if entry.Name() == ".git" || entry.Name() == "node_modules" {
					return filepath.SkipDir
				}
				return nil
			}
			for _, item := range searchFile(root, path, pattern) {
				total++
				batch.add(item)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	batch.flush()
	return protocol.SearchResult{Total: total, Ms: int(time.Since(started).Milliseconds())}, nil
}

// searchFile returns the first match on each matching line of a text file.
func searchFile(root protocol.Root, path string, pattern *regexp.Regexp) []protocol.ResultItem {
	data, err := os.ReadFile(path)
	if err != nil || bytes.IndexByte(data[:min(len(data), binarySniffBytes)], 0) >= 0 {
		return nil
	}
	relative, _ := filepath.Rel(root.Path, path)
	relative = filepath.ToSlash(relative)
	var items []protocol.ResultItem
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, initialLineBuffer), maxLineBytes)
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		line := scanner.Text()
		match := pattern.FindStringIndex(line)
		if match == nil {
			continue
		}
		ref := m0Ref{
			rootID: root.ID, line: lineNumber, path: relative,
			column: utf16Len(line[:match[0]]), length: utf16Len(line[match[0]:match[1]]),
		}
		items = append(items, protocol.ResultItem{
			Kind: "line", Ref: ref.String(), RepoID: root.ID, Path: relative, Line: lineNumber, Text: line,
			Hits: []protocol.Hit{{Start: ref.column, End: ref.column + ref.length}},
		})
	}
	return items
}

// m0Ref locates one match. Its string form is the opaque ref handed to clients.
type m0Ref struct {
	rootID string
	line   int    // 1-based
	column int    // 0-based, in UTF-16 units
	length int    // in UTF-16 units
	path   string // slash-separated, relative to the root
}

const m0RefPrefix = "m0"

// String encodes the ref; the path goes last because it may contain "|".
func (r m0Ref) String() string {
	return strings.Join([]string{m0RefPrefix, r.rootID, strconv.Itoa(r.line), strconv.Itoa(r.column), strconv.Itoa(r.length), r.path}, "|")
}

var errMalformedRef = rpc.Errorf(protocol.CodeRefStale, "malformed ref")

func parseM0Ref(ref string) (m0Ref, error) {
	parts := strings.SplitN(ref, "|", 6)
	if len(parts) != 6 || parts[0] != m0RefPrefix {
		return m0Ref{}, errMalformedRef
	}
	numbers := make([]int, 3)
	for i, text := range parts[2:5] {
		n, err := strconv.Atoi(text)
		if err != nil || n < 0 {
			return m0Ref{}, errMalformedRef
		}
		numbers[i] = n
	}
	return m0Ref{rootID: parts[1], line: numbers[0], column: numbers[1], length: numbers[2], path: parts[5]}, nil
}

// resolveRef finds the file a ref points at, or reports it stale.
func (s *Server) resolveRef(ref string) (m0Ref, string, error) {
	r, err := parseM0Ref(ref)
	if err != nil {
		return m0Ref{}, "", err
	}
	for _, root := range s.Roots() {
		if root.ID == r.rootID {
			return r, filepath.Join(root.Path, filepath.FromSlash(r.path)), nil
		}
	}
	return m0Ref{}, "", rpc.Errorf(protocol.CodeRefStale, "repo no longer open")
}

func (s *Server) m0Preview(_ context.Context, raw json.RawMessage) (any, error) {
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

func (s *Server) m0Resolve(_ context.Context, raw json.RawMessage) (any, error) {
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
