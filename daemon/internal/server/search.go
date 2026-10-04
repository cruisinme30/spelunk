package server

// search/start: runs a search and streams its results as search/batch
// notifications.
//
// Until the trigram index exists, results come from scanning the files of
// every root for the whole query as one case-insensitive literal.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
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

func (s *Server) registerSearch() {
	s.conn.Handle(protocol.MethodSearchStart, s.search)
	s.conn.Handle(protocol.MethodPreviewGet, s.preview)
	s.conn.Handle(protocol.MethodOpenResolve, s.resolve)
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

func (s *Server) search(ctx context.Context, raw json.RawMessage) (any, error) {
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
		ref := lineRef{
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
