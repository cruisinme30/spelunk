package history

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Commit is one commit as the history index keeps it: who wrote it and
// when, its message, and the lines each file added and removed. Context
// lines and line numbers are not kept; a preview reads them with git show.
type Commit struct {
	SHA         string
	AuthorName  string // after .mailmap
	AuthorEmail string
	At          time.Time // the author date
	Subject     string
	Body        string
	Files       []FileChange
}

// message is the commit's whole message: its subject, a newline, its body.
func (c *Commit) message() string { return c.Subject + "\n" + c.Body }

// FileChange is what one commit changed in one file.
type FileChange struct {
	Path    string // slash-separated, relative to the repo root
	Added   int
	Removed int
	// Text is the added and removed lines in diff order, each ending in a
	// newline, and is never modified once read. It holds at most
	// maxFileTextBytes, so a huge generated diff stays countable but is only
	// partly searchable, and it is empty for a file the Options skip.
	Text []byte
	// Signs holds the diff sign of each line of Text, in order: '+' for an
	// added line, '-' for a removed one, so type:added and type:removed can
	// tell them apart.
	Signs []byte
}

// The signs of Signs.
const (
	signAdded   = '+'
	signRemoved = '-'
)

// Limits on what one commit keeps, so a vendored import or a generated
// file doesn't fill memory.
const (
	maxFileTextBytes = 1 << 20
	maxLineBytes     = 2_000
)

// Options decide what a history index keeps.
type Options struct {
	// Since is where the history window starts; zero means all history.
	Since time.Time
	// Skip, if not nil, reports whether to leave out a path's changed lines,
	// as index.exclude leaves files out of the working-tree index. Commits
	// still list the file and count its lines, so f: finds them.
	Skip func(path string) bool
}

// ErrNotGit means the folder is not inside a Git working tree.
var ErrNotGit = errors.New("not a Git repository")

// logFormat starts each commit with a record separator (0x1e) and ends each
// field with a NUL: sha, author name, author email, author date (ISO 8601,
// and as a Unix time for dates ISO 8601 can't write), subject, body. Names
// and messages can hold any other control character, but Git can't store
// a NUL in them. No line of a patch starts with 0x1e.
const logFormat = "%x1e%H%x00%aN%x00%aE%x00%aI%x00%at%x00%s%x00%b%x00"

// fieldEnd ends each field of logFormat.
const fieldEnd = 0x00

// headerFields is how many fields logFormat writes.
const headerFields = 7

// gitOptions come before every git command: plain path names; no optional
// locks, so the daemon never makes the user's own git commands wait or fail
// on a lock; and output in the shape the parsers read, whatever the user's
// Git configuration says (signatures shown, another log encoding, root
// commits without a diff).
var gitOptions = []string{
	"--no-optional-locks", "-c", "core.quotepath=off", "-c", "log.showSignature=false",
	"-c", "i18n.logOutputEncoding=UTF-8", "-c", "log.showRoot=true",
}

// diffOptions fix how patches look, whatever the user's diff.* settings
// say: "a/" and "b/" before paths (diff.noPrefix, diff.mnemonicPrefix and
// diff.srcPrefix change them), no color, no external diff tool, renames
// as a deletion and an addition, and a submodule as one line.
var diffOptions = []string{
	"--src-prefix=a/", "--dst-prefix=b/", "--no-color", "--no-ext-diff", "--no-renames", "--submodule=short",
}

// git runs git in root and returns its standard output.
func git(ctx context.Context, root string, args ...string) ([]byte, error) {
	//nolint:gosec // G204: the arguments are this package's own, and shas come from git or are checked hexadecimal
	cmd := exec.CommandContext(ctx, "git", slices.Concat(gitOptions, []string{"-C", root}, args)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// Head returns the commit HEAD points at, "" for a repo without commits,
// or ErrNotGit. Without git installed it returns exec.ErrNotFound, wrapped:
// the folder may well be a repo.
func Head(ctx context.Context, root string) (string, error) {
	inside, err := git(ctx, root, "rev-parse", "--is-inside-work-tree")
	switch {
	case ctx.Err() != nil:
		return "", ctx.Err()
	case errors.Is(err, exec.ErrNotFound):
		return "", err
	case err != nil, strings.TrimSpace(string(inside)) != "true": // "false" inside .git itself
		return "", ErrNotGit
	}
	out, err := git(ctx, root, "rev-parse", "--verify", "--quiet", "HEAD")
	if err != nil {
		return "", nil //nolint:nilerr // no HEAD yet: a repo without commits has an empty history
	}
	return strings.TrimSpace(string(out)), nil
}

// isAncestor reports whether commit old is on HEAD's history, so the
// commits after it are all that changed.
func isAncestor(ctx context.Context, root, old string) bool {
	_, err := git(ctx, root, "merge-base", "--is-ancestor", old, "HEAD")
	return err == nil
}

// countCommits counts the first-parent commits readLog would read, for
// progress. It returns 0 when it can't tell.
func countCommits(ctx context.Context, root, revisions string, opts Options) int {
	args := []string{"rev-list", "--count", "--first-parent"}
	if !opts.Since.IsZero() {
		args = append(args, "--since="+opts.Since.Format(time.RFC3339))
	}
	out, err := git(ctx, root, append(args, revisions, "--", ".")...)
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	return n
}

// readLog streams the first-parent commits of revisions ("HEAD", or
// "old..HEAD") newest first, limited to root's folder and to the window
// opts sets, calling each for every commit read.
func readLog(ctx context.Context, root, revisions string, opts Options, each func(Commit) error) error {
	args := slices.Concat(gitOptions, []string{
		"-C", root, "log", "--first-parent", "-m", "--relative", "-p", "-U0", "--use-mailmap", "--format=" + logFormat,
	}, diffOptions)
	if !opts.Since.IsZero() {
		args = append(args, "--since="+opts.Since.Format(time.RFC3339))
	}
	//nolint:gosec // G204: revisions are shas git itself reported, not user input
	cmd := exec.CommandContext(ctx, "git", append(args, revisions, "--", ".")...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	parseErr := parseLog(out, opts.Skip, each)
	if parseErr != nil {
		_ = cmd.Process.Kill() // stop reading; the parse error is the one to report
	}
	waitErr := cmd.Wait()
	switch {
	case parseErr != nil:
		return parseErr
	case ctx.Err() != nil:
		return ctx.Err()
	case waitErr != nil:
		return fmt.Errorf("git log: %w: %s", waitErr, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// parseLog reads git log output in logFormat with -p -U0 patches. skip,
// if not nil, says which files' changed lines to leave out.
func parseLog(r io.Reader, skip func(string) bool, each func(Commit) error) error {
	p := logParser{lines: bufio.NewReaderSize(r, 64*1024), skip: skip}
	for {
		line, err := p.readLine()
		if len(line) > 0 || err == nil {
			if feedErr := p.feed(line, each); feedErr != nil {
				return feedErr
			}
		}
		if errors.Is(err, io.EOF) {
			return p.finish(each)
		}
		if err != nil {
			return err
		}
	}
}

// logParser turns git log output into commits, one line at a time.
type logParser struct {
	lines *bufio.Reader
	skip  func(string) bool

	commit *Commit       // the commit being read, nil before the first
	header *bytes.Buffer // a commit's header while it spans lines
	file   *FileChange   // the file whose patch is being read
	text   bytes.Buffer  // the file's changed lines so far
	signs  []byte        // the sign of each line of text
	inHunk bool          // between "@@" and the next file header
	// skipFile is set at the file's first hunk, once its path is known.
	skipFile bool
}

// readLine returns the next line without its newline, cut at
// maxLineBytes; the rest of a longer line is skipped. In a commit's header
// the field separators of the rest are kept, so a long subject or body
// line is cut short but doesn't run into the next field (or commit).
func (p *logParser) readLine() ([]byte, error) {
	var line []byte
	inHeader := p.header != nil
	for {
		chunk, err := p.lines.ReadSlice('\n')
		if len(line) == 0 && len(chunk) > 0 && chunk[0] == 0x1e {
			inHeader = true
		}
		kept := chunk[:min(len(chunk), max(maxLineBytes-len(line), 0))]
		line = append(line, kept...)
		if inHeader {
			for range bytes.Count(chunk[len(kept):], []byte{fieldEnd}) {
				line = append(line, fieldEnd)
			}
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		return bytes.TrimRight(line, "\r\n"), err
	}
}

// feed takes one line of output.
func (p *logParser) feed(line []byte, each func(Commit) error) error {
	if p.header != nil {
		p.header.WriteByte('\n')
		p.header.Write(line)
		return p.finishHeader()
	}
	if len(line) > 0 && line[0] == 0x1e {
		if err := p.finish(each); err != nil {
			return err
		}
		p.header = bytes.NewBuffer(append([]byte(nil), line[1:]...))
		return p.finishHeader()
	}
	if p.commit != nil {
		p.patchLine(string(line))
	}
	return nil
}

// finishHeader starts the commit once its header has every field.
func (p *logParser) finishHeader() error {
	text := p.header.String()
	if strings.Count(text, string(rune(fieldEnd))) < headerFields {
		return nil // the body continues on the next line
	}
	p.header = nil
	fields := strings.SplitN(text, string(rune(fieldEnd)), headerFields+1)
	p.commit = &Commit{
		SHA: fields[0], AuthorName: fields[1], AuthorEmail: fields[2], At: commitDate(fields[3], fields[4]),
		Subject: fields[5], Body: strings.TrimSpace(fields[6]),
	}
	return nil
}

// commitDate reads a commit's author date from its ISO 8601 form, or from
// its Unix time when that is out of range: Git writes whatever a commit
// holds, such as the time zone +9999 or the year 10000. A date Git can't
// read either is the zero time; it never stops the history being read.
func commitDate(iso, unix string) time.Time {
	if at, err := time.Parse(time.RFC3339, iso); err == nil {
		return at
	}
	if seconds, err := strconv.ParseInt(unix, 10, 64); err == nil {
		return time.Unix(seconds, 0).UTC()
	}
	return time.Time{}
}

// patchLine reads one line of a commit's patch.
func (p *logParser) patchLine(line string) {
	switch {
	case strings.HasPrefix(line, "diff --git "):
		p.startFile(diffGitPath(line))
	case p.file == nil:
		// The blank line between header and patch.
	case !p.inHunk && p.fileHeader(line):
		// The path is read.
	case strings.HasPrefix(line, "@@ "):
		p.startHunk()
	case p.inHunk && strings.HasPrefix(line, "+"):
		p.file.Added++
		p.keep(line[1:], signAdded)
	case p.inHunk && strings.HasPrefix(line, "-"):
		p.file.Removed++
		p.keep(line[1:], signRemoved)
	}
}

// fileHeader reads a "--- a/path" or "+++ b/path" line, reporting whether
// line was one.
func (p *logParser) fileHeader(line string) bool {
	switch {
	case strings.HasPrefix(line, "--- "):
		if path, ok := patchPath(line[4:], "a/"); ok {
			p.file.Path = path
		}
	case strings.HasPrefix(line, "+++ "):
		if path, ok := patchPath(line[4:], "b/"); ok {
			p.file.Path = path // the new path wins; a deleted file keeps the old one
		}
	default:
		return false
	}
	return true
}

// startHunk begins a hunk. At the file's first hunk its path is final, so
// that is when the Options decide whether to skip its lines.
func (p *logParser) startHunk() {
	if !p.inHunk && p.text.Len() == 0 {
		p.skipFile = p.skip != nil && p.skip(p.file.Path)
	}
	p.inHunk = true
}

// keep stores a changed line and its sign unless the file is skipped or
// already has its fill.
func (p *logParser) keep(line string, sign byte) {
	if !p.skipFile && p.text.Len()+len(line) < maxFileTextBytes {
		p.text.WriteString(line)
		p.text.WriteByte('\n')
		p.signs = append(p.signs, sign)
	}
}

// startFile begins the next file of the commit.
func (p *logParser) startFile(path string) {
	p.endFile()
	p.commit.Files = append(p.commit.Files, FileChange{Path: path})
	p.file = &p.commit.Files[len(p.commit.Files)-1]
	p.inHunk, p.skipFile = false, false
}

// endFile stores the changed lines of the file being read and their signs,
// in slices of their own size.
func (p *logParser) endFile() {
	if p.file != nil && p.text.Len() > 0 {
		p.file.Text = bytes.Clone(p.text.Bytes())
		p.file.Signs = bytes.Clone(p.signs)
	}
	p.text.Reset()
	p.signs = p.signs[:0]
}

// finish hands over the commit read so far.
func (p *logParser) finish(each func(Commit) error) error {
	if p.commit == nil {
		return nil
	}
	p.endFile()
	commit := *p.commit
	p.commit, p.file, p.inHunk = nil, nil, false
	return each(commit)
}

// diffGitPath takes the path from "diff --git a/x b/x", or from
// "diff --git "a/x\ty" "b/x\ty"" when Git quoted an unusual name; the ---
// and +++ lines that follow correct it when the name holds " b/". A binary
// or mode-only change has no such lines, so this is its path.
func diffGitPath(line string) string {
	rest := strings.TrimPrefix(line, "diff --git ")
	if strings.HasSuffix(rest, `"`) {
		if i := strings.LastIndex(rest, ` "b/`); i >= 0 {
			return strings.TrimPrefix(unquote(rest[i+1:]), "b/")
		}
	}
	if i := strings.LastIndex(rest, " b/"); i >= 0 {
		return unquote(rest[i+3:])
	}
	return rest
}

// patchPath reads the path of a ---/+++ line: "a/src/x.go" with prefix
// "a/", possibly C-quoted. ok is false for /dev/null.
func patchPath(text, prefix string) (string, bool) {
	text = unquote(strings.TrimRight(text, "\t"))
	if text == "/dev/null" || !strings.HasPrefix(text, prefix) {
		return "", false
	}
	return text[len(prefix):], true
}

// unquote undoes Git's C-style quoting of unusual path names.
func unquote(text string) string {
	if len(text) >= 2 && text[0] == '"' && text[len(text)-1] == '"' {
		if unquoted, err := strconv.Unquote(text); err == nil {
			return unquoted
		}
	}
	return text
}
