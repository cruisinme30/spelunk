package server

// The opaque ref of a working-tree line result. Clients pass refs back
// unchanged (preview/get, open/resolve); only this file builds or reads them.

import (
	"path/filepath"
	"strconv"
	"strings"

	"github.com/cruisinme30/unified-search/daemon/internal/protocol"
	"github.com/cruisinme30/unified-search/daemon/internal/rpc"
)

// lineRef locates one match. Its string form is the opaque ref handed to clients.
type lineRef struct {
	rootID string
	line   int    // 1-based
	column int    // 0-based, in UTF-16 units
	length int    // in UTF-16 units
	path   string // slash-separated, relative to the root
}

const lineRefPrefix = "line"

// String encodes the ref; the path goes last because it may contain "|".
func (r lineRef) String() string {
	return strings.Join([]string{lineRefPrefix, r.rootID, strconv.Itoa(r.line), strconv.Itoa(r.column), strconv.Itoa(r.length), r.path}, "|")
}

var errMalformedRef = rpc.Errorf(protocol.CodeRefStale, "malformed ref")

func parseLineRef(ref string) (lineRef, error) {
	parts := strings.SplitN(ref, "|", 6)
	if len(parts) != 6 || parts[0] != lineRefPrefix {
		return lineRef{}, errMalformedRef
	}
	numbers := make([]int, 3)
	for i, text := range parts[2:5] {
		n, err := strconv.Atoi(text)
		if err != nil || n < 0 {
			return lineRef{}, errMalformedRef
		}
		numbers[i] = n
	}
	return lineRef{rootID: parts[1], line: numbers[0], column: numbers[1], length: numbers[2], path: parts[5]}, nil
}

// resolveRef finds the file a ref points at, or reports it stale.
func (s *Server) resolveRef(ref string) (lineRef, string, error) {
	r, err := parseLineRef(ref)
	if err != nil {
		return lineRef{}, "", err
	}
	for _, root := range s.Roots() {
		if root.ID == r.rootID {
			return r, filepath.Join(root.Path, filepath.FromSlash(r.path)), nil
		}
	}
	return lineRef{}, "", rpc.Errorf(protocol.CodeRefStale, "repo no longer open")
}
