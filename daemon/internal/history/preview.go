package history

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/cruisinme30/unified-search/daemon/internal/protocol"
	"github.com/cruisinme30/unified-search/daemon/internal/query"
	"github.com/cruisinme30/unified-search/daemon/internal/trigram"
)

// ErrStale means the commit behind a ref is gone, after a rebase or a
// garbage collection.
var ErrStale = errors.New("commit no longer exists")

// maxPreviewLines bounds the diff lines one preview carries.
const maxPreviewLines = 20_000

// Preview returns a commit's message and diff, with contextLines unchanged
// lines around each change, read with git show. With the plan that found
// the commit it marks the query's text terms in changed lines and flags the
// files its filters hid; without it (the plan was forgotten) nothing is
// marked or hidden.
func Preview(ctx context.Context, repo *Repo, ref Ref, plan *query.Plan, contextLines int) (protocol.Preview, error) {
	commit, err := readCommitHeader(ctx, repo.Root, ref.SHA)
	if err != nil {
		return protocol.Preview{}, err
	}
	args := slices.Concat([]string{"show", "--format=", "--relative", "-m", "--first-parent"}, diffOptions,
		[]string{"-U" + strconv.Itoa(max(contextLines, 0)), ref.SHA, "--"})
	out, err := git(ctx, repo.Root, args...)
	if err != nil {
		return protocol.Preview{}, ErrStale
	}
	hunks, files := parseShow(out)
	var terms []*query.Content
	if plan != nil {
		terms = plan.Terms
		finder := trigram.NewLineFinder()
		message := newMessageView(&commit, finder)
		for i := range files {
			view := newFileView(changedLinesOf(files[i].Path, hunks), finder, message)
			files[i].HiddenByFilter = !query.Eval(plan.Pred, leafFor(repo, &commit, view))
		}
	}
	for h := range hunks {
		for l := range hunks[h].Lines {
			line := &hunks[h].Lines[l]
			if line.Kind != "ctx" {
				line.Text, line.Hits = trigram.PreviewLine(line.Text, terms)
			} else {
				line.Text, line.Hits = trigram.PreviewLine(line.Text, nil)
			}
		}
	}
	return protocol.Preview{
		Kind: protocol.PreviewKindCommit, SHA: commit.SHA, Subject: commit.Subject, Body: commit.Body,
		Author: commit.AuthorName + " <" + commit.AuthorEmail + ">", At: commit.At.UTC().Format(time.RFC3339),
		Files: files, Hunks: hunks, SubjectHits: messageHits(plan, commit.Subject), BodyHits: messageHits(plan, commit.Body),
	}, nil
}

// OpenTarget returns where opening a commit result goes: the commit, which
// the extension shows as a diff document.
func OpenTarget(ctx context.Context, repo *Repo, ref Ref) (protocol.OpenTarget, error) {
	if _, err := readCommitHeader(ctx, repo.Root, ref.SHA); err != nil {
		return protocol.OpenTarget{}, err
	}
	return protocol.OpenTarget{RepoID: repo.ID, SHA: ref.SHA}, nil
}

// readCommitHeader reads a commit's author, date and message.
func readCommitHeader(ctx context.Context, root, sha string) (Commit, error) {
	out, err := git(ctx, root, "show", "-s", "--use-mailmap", "--format="+logFormat, sha, "--")
	if err != nil {
		return Commit{}, ErrStale
	}
	var commit Commit
	err = parseLog(bytes.NewReader(out), nil, func(c Commit) error {
		commit = c
		return nil
	})
	if err != nil || commit.SHA == "" {
		return Commit{}, ErrStale
	}
	return commit, nil
}

// changedLinesOf collects the added and removed lines of one file's hunks,
// so the plan's leaves can judge the file as Search did.
func changedLinesOf(path string, hunks []protocol.Hunk) *FileChange {
	file := &FileChange{Path: path}
	var text bytes.Buffer
	for _, hunk := range hunks {
		if hunk.Path != path {
			continue
		}
		for _, line := range hunk.Lines {
			if line.Kind == "add" || line.Kind == "del" {
				text.WriteString(line.Text)
				text.WriteByte('\n')
			}
		}
	}
	file.Text = text.Bytes()
	return file
}

// parseShow reads git show's patch into hunks and per-file line counts.
func parseShow(out []byte) ([]protocol.Hunk, []protocol.FileStat) {
	var p showParser
	for _, line := range strings.Split(string(out), "\n") {
		p.line(line)
	}
	return p.hunks, p.files
}

// showParser turns git show's patch into hunks, one line at a time.
type showParser struct {
	hunks  []protocol.Hunk
	files  []protocol.FileStat
	inHunk bool // between "@@" and the next file header
	oldNo  int  // the next old line's number
	newNo  int  // the next new line's number
	kept   int  // diff lines kept, at most maxPreviewLines
}

// line reads one line of the patch.
func (p *showParser) line(line string) {
	switch {
	case strings.HasPrefix(line, "diff --git "):
		p.files = append(p.files, protocol.FileStat{Path: diffGitPath(line)})
		p.inHunk = false
	case len(p.files) == 0:
	case !p.inHunk && strings.HasPrefix(line, "--- "):
		p.setPath(line[4:], "a/")
	case !p.inHunk && strings.HasPrefix(line, "+++ "):
		p.setPath(line[4:], "b/") // the new path wins; a deleted file keeps the old one
	case strings.HasPrefix(line, "@@ "):
		p.inHunk = true
		p.oldNo, p.newNo = hunkStarts(line)
		p.hunks = append(p.hunks, protocol.Hunk{Path: p.files[len(p.files)-1].Path, Header: line, Lines: []protocol.DiffLine{}})
	case p.inHunk && line != "" && line[0] != '\\':
		p.diffLine(line)
	}
}

// setPath takes the file's path from a --- or +++ line.
func (p *showParser) setPath(text, prefix string) {
	if path, ok := patchPath(text, prefix); ok {
		p.files[len(p.files)-1].Path = path
	}
}

// diffLine reads an added, removed or unchanged line of a hunk.
func (p *showParser) diffLine(line string) {
	file := &p.files[len(p.files)-1]
	diffLine := protocol.DiffLine{Text: line[1:]}
	switch line[0] {
	case '+':
		diffLine.Kind, diffLine.NewNo = "add", p.newNo
		p.newNo++
		file.Added++
	case '-':
		diffLine.Kind, diffLine.OldNo = "del", p.oldNo
		p.oldNo++
		file.Removed++
	default:
		diffLine.Kind, diffLine.OldNo, diffLine.NewNo = "ctx", p.oldNo, p.newNo
		p.oldNo++
		p.newNo++
	}
	if p.kept < maxPreviewLines {
		hunk := &p.hunks[len(p.hunks)-1]
		hunk.Lines = append(hunk.Lines, diffLine)
		p.kept++
	}
}

// hunkStarts reads the first old and new line numbers of "@@ -a,b +c,d @@".
func hunkStarts(line string) (oldStart, newStart int) {
	fields := strings.Fields(line)
	if len(fields) < 3 {
		return 0, 0
	}
	return rangeStart(fields[1]), rangeStart(fields[2])
}

// rangeStart reads "-12,3" or "+12" as 12.
func rangeStart(field string) int {
	field = strings.TrimLeft(field, "-+")
	if comma := strings.IndexByte(field, ','); comma >= 0 {
		field = field[:comma]
	}
	n, _ := strconv.Atoi(field)
	return n
}
