package permalink

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
)

// hunk is one change between a commit's file and the file now, as git's
// "@@ -oldStart,oldCount +newStart,newCount @@" says it: newCount lines
// from newStart replace oldCount lines from oldStart. A count of 0 starts
// after its line instead.
type hunk struct {
	oldStart, oldCount, newStart, newCount int
}

var hunkHeader = regexp.MustCompile(`(?m)^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

// diffHunks diffs then (a commit's file) against now with git's own diff,
// without context, so each hunk is exactly the lines that changed.
func diffHunks(ctx context.Context, root string, then, now []byte) ([]hunk, error) {
	if bytes.Equal(then, now) {
		return nil, nil
	}
	dir, err := os.MkdirTemp("", "spelunk-link-")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	thenFile, nowFile := filepath.Join(dir, "then"), filepath.Join(dir, "now")
	if writeErr := errors.Join(os.WriteFile(thenFile, then, 0o600), os.WriteFile(nowFile, now, 0o600)); writeErr != nil {
		return nil, writeErr
	}
	args := slices.Concat(gitOptions, []string{
		"-C", root, "diff", "--no-index", "--no-color", "--no-ext-diff", "--no-renames", "-U0", "--", thenFile, nowFile,
	})
	//nolint:gosec // G204: the arguments are this package's own and two files it just wrote
	out, err := exec.CommandContext(ctx, "git", args...).Output()
	// --no-index exits 1 when the files differ, which they do here.
	var exit *exec.ExitError
	if err != nil && (!errors.As(err, &exit) || exit.ExitCode() != 1) {
		return nil, fmt.Errorf("git diff: %w", err)
	}
	return parseHunks(out), nil
}

// parseHunks reads the hunk headers of a unified diff.
func parseHunks(diff []byte) []hunk {
	count := func(text []byte) int {
		if len(text) == 0 {
			return 1 // "@@ -5 +5 @@" is one line
		}
		n, _ := strconv.Atoi(string(text))
		return n
	}
	var hunks []hunk
	for _, match := range hunkHeader.FindAllSubmatch(diff, -1) {
		oldStart, _ := strconv.Atoi(string(match[1]))
		newStart, _ := strconv.Atoi(string(match[3]))
		hunks = append(hunks, hunk{oldStart, count(match[2]), newStart, count(match[4])})
	}
	return hunks
}

// mapLine maps line of the file now to the same line in the commit's file:
// shifted by the lines added and removed above it, or false when the line
// itself was added or changed since.
func mapLine(hunks []hunk, line int) (int, bool) {
	shift := 0
	for _, h := range hunks {
		if h.newCount > 0 && line >= h.newStart && line < h.newStart+h.newCount {
			return 0, false
		}
		// A hunk that only removes lines sits after line newStart.
		above := h.newStart+h.newCount <= line
		if h.newCount == 0 {
			above = h.newStart < line
		}
		if above {
			shift += h.oldCount - h.newCount
		}
	}
	return line + shift, true
}
