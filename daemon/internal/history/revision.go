package history

// Reading the tree of one past commit, for at: (see docs/dev/implementation-plan.md).
// Nothing here is indexed: the server reads a revision's files on demand and
// builds a short-lived trigram shard of them.

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

// ErrNoRevision means a name at: was given names no commit in the repo.
var ErrNoRevision = errors.New("no such revision")

// Revision is the commit at: searches: the commit's sha and its committer
// date, which is when its tree became the repo's state.
type Revision struct {
	SHA string
	At  time.Time
}

// revisionFormat is a commit's sha and committer date, as a Unix time.
const revisionFormat = "--format=%H%x00%ct"

// ResolveRevision finds the commit that name (a tag, a branch, a sha,
// HEAD~3 …) points at in the repo at root. A name that names nothing, or
// names something other than a commit or a tag of one, is ErrNoRevision;
// a folder outside Git is ErrNotGit.
func ResolveRevision(ctx context.Context, root, name string) (Revision, error) {
	// A name starting with "-" would be read as an option; the query
	// language never lets one through, but a revision is never one either.
	if name == "" || strings.HasPrefix(name, "-") {
		return Revision{}, ErrNoRevision
	}
	if _, err := Head(ctx, root); err != nil {
		return Revision{}, err
	}
	out, err := git(ctx, root, "rev-parse", "--verify", "--quiet", "--end-of-options", name+"^{commit}")
	if err != nil {
		if ctx.Err() != nil {
			return Revision{}, ctx.Err()
		}
		return Revision{}, ErrNoRevision
	}
	return commitRevision(ctx, root, strings.TrimSpace(string(out)))
}

// RevisionBefore finds the newest commit on HEAD's first-parent line that
// was committed at or before a time: the repo as it was then. A repo with
// no commit that old is ErrNoRevision.
func RevisionBefore(ctx context.Context, root string, before time.Time) (Revision, error) {
	head, err := Head(ctx, root)
	if err != nil {
		return Revision{}, err
	}
	if head == "" {
		return Revision{}, ErrNoRevision
	}
	out, err := git(ctx, root, "log", "-1", "--first-parent", "--before="+before.Format(time.RFC3339), revisionFormat, head)
	if err != nil {
		return Revision{}, err
	}
	if len(bytes.TrimSpace(out)) == 0 {
		return Revision{}, ErrNoRevision
	}
	return parseRevision(out)
}

// commitRevision reads the committer date of the commit sha.
func commitRevision(ctx context.Context, root, sha string) (Revision, error) {
	out, err := git(ctx, root, "log", "-1", "--no-walk", revisionFormat, sha)
	if err != nil {
		return Revision{}, err
	}
	return parseRevision(out)
}

// parseRevision reads one line of revisionFormat.
func parseRevision(out []byte) (Revision, error) {
	sha, seconds, ok := strings.Cut(strings.TrimSpace(string(out)), "\x00")
	unix, err := strconv.ParseInt(seconds, 10, 64)
	if !ok || err != nil || !isHex(sha) {
		return Revision{}, fmt.Errorf("unexpected git log output %q", out)
	}
	return Revision{SHA: sha, At: time.Unix(unix, 0)}, nil
}

// TreeFile is a regular file in a commit's tree.
type TreeFile struct {
	Path string // slash-separated, relative to root
	OID  string // the blob's object id
	Size int64
}

// Modes of tree entries that are regular files; symlinks (120000) and
// submodules (160000) are left out, as the working-tree index leaves them out.
const (
	modeFile       = "100644"
	modeExecutable = "100755"
)

// ListTree lists the regular files of commit sha under root, sorted by
// path, as paths relative to root: a root inside a bigger repo sees only
// its own folder, as the working-tree index does.
func ListTree(ctx context.Context, root, sha string) ([]TreeFile, error) {
	if !isHex(sha) {
		return nil, ErrNoRevision
	}
	out, err := git(ctx, root, "ls-tree", "-r", "-z", "--long", sha, "--", ".")
	if err != nil {
		return nil, err
	}
	var files []TreeFile
	for _, entry := range bytes.Split(out, []byte{0}) {
		// <mode> SP <type> SP <object> SP+ <size> TAB <path>
		info, path, ok := bytes.Cut(entry, []byte{'\t'})
		fields := strings.Fields(string(info))
		if !ok || len(fields) != 4 || (fields[0] != modeFile && fields[0] != modeExecutable) {
			continue
		}
		size, err := strconv.ParseInt(fields[3], 10, 64)
		if err != nil {
			continue
		}
		files = append(files, TreeFile{Path: string(path), OID: fields[2], Size: size})
	}
	slices.SortFunc(files, func(a, b TreeFile) int { return strings.Compare(a.Path, b.Path) })
	return files, nil
}

// ReadBlobs reads the content of each file with one git cat-file, calling
// each in order. It stops at the first error each returns.
func ReadBlobs(ctx context.Context, root string, files []TreeFile, each func(TreeFile, []byte) error) error {
	if len(files) == 0 {
		return nil
	}
	//nolint:gosec // G204: the arguments are fixed; object ids go on stdin
	cmd := exec.CommandContext(ctx, "git", slices.Concat(gitOptions, []string{"-C", root, "cat-file", "--batch"})...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	// Ids are written while answers are read, so neither pipe fills up.
	go func() {
		w := bufio.NewWriter(stdin)
		for _, file := range files {
			if _, err := w.WriteString(file.OID + "\n"); err != nil {
				break
			}
		}
		_ = w.Flush()
		_ = stdin.Close()
	}()
	readErr := readBatch(bufio.NewReaderSize(stdout, 64<<10), files, each)
	if readErr != nil {
		_ = cmd.Process.Kill() // stop git; the read error is the one to report
	}
	waitErr := cmd.Wait()
	switch {
	case ctx.Err() != nil:
		// Killing git for ctx cuts its output short: say why, not how.
		return ctx.Err()
	case readErr != nil:
		return readErr
	case waitErr != nil:
		return fmt.Errorf("git cat-file: %w: %s", waitErr, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// readBatch reads git cat-file --batch's answers, one per file in order:
// "<oid> blob <size>\n<content>\n", or "<oid> missing\n".
func readBatch(r *bufio.Reader, files []TreeFile, each func(TreeFile, []byte) error) error {
	for _, file := range files {
		header, err := r.ReadString('\n')
		if err != nil {
			return fmt.Errorf("git cat-file: %w", err)
		}
		fields := strings.Fields(header)
		if len(fields) != 3 {
			continue // missing: nothing follows the header
		}
		size, err := strconv.Atoi(fields[2])
		if err != nil || size < 0 {
			return fmt.Errorf("git cat-file: bad header %q", header)
		}
		if fields[1] != "blob" {
			// Not a file's content; skip it and its trailing newline.
			if _, err := r.Discard(size + 1); err != nil {
				return fmt.Errorf("git cat-file: %w", err)
			}
			continue
		}
		content := make([]byte, size+1) // the content and its trailing newline
		if _, err := io.ReadFull(r, content); err != nil {
			return fmt.Errorf("git cat-file: %w", err)
		}
		if err := each(file, content[:size]); err != nil {
			return err
		}
	}
	return nil
}

// ReadFileAt reads the file at path (relative to root) as it was in commit
// sha. A file the commit doesn't have, or that isn't a regular file in it,
// is ErrStale.
func ReadFileAt(ctx context.Context, root, sha, path string) ([]byte, error) {
	if !isHex(sha) {
		return nil, ErrStale
	}
	// Listing the one path checks it is a regular file, and finds its blob.
	out, err := git(ctx, root, "ls-tree", "-z", "--long", sha, "--", path)
	if err != nil {
		return nil, ErrStale
	}
	var found *TreeFile
	for _, entry := range bytes.Split(out, []byte{0}) {
		info, name, ok := bytes.Cut(entry, []byte{'\t'})
		fields := strings.Fields(string(info))
		if ok && string(name) == path && len(fields) == 4 && (fields[0] == modeFile || fields[0] == modeExecutable) {
			found = &TreeFile{Path: path, OID: fields[2]}
		}
	}
	if found == nil {
		return nil, ErrStale
	}
	var content []byte
	err = ReadBlobs(ctx, root, []TreeFile{*found}, func(_ TreeFile, blob []byte) error {
		content = blob
		return nil
	})
	if err != nil {
		return nil, err
	}
	if content == nil {
		return nil, ErrStale
	}
	return content, nil
}

// Tag is a tag at: can name, with when it was made.
type Tag struct {
	Name string
	At   time.Time // the tag's date, or its commit's for a lightweight tag
}

// Tags lists up to limit of the repo's tags, newest first. A folder
// outside Git has none.
func Tags(ctx context.Context, root string, limit int) ([]Tag, error) {
	out, err := git(ctx, root, "for-each-ref", "--sort=-creatordate", "--count="+strconv.Itoa(limit),
		"--format=%(refname:short)%00%(creatordate:unix)", "refs/tags")
	if err != nil {
		return nil, err
	}
	var tags []Tag
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		name, seconds, ok := strings.Cut(line, "\x00")
		unix, err := strconv.ParseInt(seconds, 10, 64)
		if !ok || err != nil || name == "" {
			continue
		}
		tags = append(tags, Tag{Name: name, At: time.Unix(unix, 0)})
	}
	return tags, nil
}

// RenamedSince maps the path at sha of each file renamed between sha and
// HEAD to its path at HEAD, as Git finds renames; nil when Git can't say.
func RenamedSince(ctx context.Context, root, sha string) map[string]string {
	out, err := git(ctx, root, "diff", "--find-renames", "--name-status", "-z", "--relative", "--no-ext-diff", sha, "HEAD", "--")
	if err != nil {
		return nil
	}
	renamed := map[string]string{}
	fields := strings.Split(string(out), "\x00")
	for i := 0; i < len(fields)-1; {
		status := fields[i]
		switch {
		case strings.HasPrefix(status, "R") && i+2 < len(fields):
			renamed[fields[i+1]] = fields[i+2]
			i += 3
		case strings.HasPrefix(status, "C") && i+2 < len(fields):
			i += 3
		default:
			i += 2
		}
	}
	return renamed
}
