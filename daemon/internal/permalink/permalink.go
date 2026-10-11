package permalink

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/cruisinme30/spelunk/daemon/internal/history"
	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
)

// gitOptions come before every git command: no optional locks, so a link
// never waits on the user's own git, and plain path names.
var gitOptions = []string{"--no-optional-locks", "-c", "core.quotepath=off"}

// errGitFailed is a git command that ran and failed (its exit status, not
// a missing git), which the callers read as "no such thing".
var errGitFailed = errors.New("git failed")

// git runs git in root and returns its standard output, trimmed.
func git(ctx context.Context, root string, args ...string) (string, error) {
	//nolint:gosec // G204: the arguments are this package's own; shas and remote names come from git
	cmd := exec.CommandContext(ctx, "git", slices.Concat(gitOptions, []string{"-C", root}, args)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return "", fmt.Errorf("%w: git %s: %s", errGitFailed, args[0], strings.TrimSpace(stderr.String()))
		}
		return "", fmt.Errorf("git %s: %w", args[0], err)
	}
	return strings.TrimSpace(string(out)), nil
}

// lines splits git's output into its non-empty lines.
func lines(out string) []string {
	return slices.DeleteFunc(strings.Split(out, "\n"), func(line string) bool { return line == "" })
}

// Remotes lists the remotes of the repo root is in, and picks the one
// links go to: the remote the current branch tracks, else origin, else the
// only one. A folder outside Git has none, and Git false.
func Remotes(ctx context.Context, root string) (protocol.RepoRemotes, error) {
	inside, err := git(ctx, root, "rev-parse", "--is-inside-work-tree")
	if err != nil || inside != "true" {
		if errors.Is(err, errGitFailed) || err == nil {
			return protocol.RepoRemotes{Git: false, Remotes: []protocol.Remote{}}, nil
		}
		return protocol.RepoRemotes{}, err
	}
	names, err := git(ctx, root, "remote")
	if err != nil {
		return protocol.RepoRemotes{}, err
	}
	result := protocol.RepoRemotes{Git: true, Remotes: []protocol.Remote{}}
	for _, name := range lines(names) {
		url, urlErr := git(ctx, root, "remote", "get-url", name)
		if urlErr != nil {
			continue // a remote with no URL can't be linked to
		}
		result.Remotes = append(result.Remotes, protocol.Remote{Name: name, URL: url})
	}
	result.Preferred = preferred(ctx, root, result.Remotes)
	return result, nil
}

// preferred is the remote links go to, or "" when no rule picks one.
func preferred(ctx context.Context, root string, remotes []protocol.Remote) string {
	has := func(name string) bool {
		return slices.ContainsFunc(remotes, func(remote protocol.Remote) bool { return remote.Name == name })
	}
	if branch, err := git(ctx, root, "symbolic-ref", "--quiet", "--short", "HEAD"); err == nil && branch != "" {
		if tracked, configErr := git(ctx, root, "config", "--get", "branch."+branch+".remote"); configErr == nil && has(tracked) {
			return tracked
		}
	}
	switch {
	case has("origin"):
		return "origin"
	case len(remotes) == 1:
		return remotes[0].Name
	}
	return ""
}

// File is a result in a file: a file name (Line 0), a code line or a
// definition, now or at a revision.
type File struct {
	Root string // the workspace folder
	Path string // slash-separated, relative to Root
	// Line and EndLine are 1-based; Line is 0 for a file-name result, and
	// EndLine is 0 unless the match spans lines.
	Line, EndLine int
	// Revision is the commit an at: result's file is from; "" for the file now.
	Revision string
}

// Linker links results, reading each repo's remote and base commit, and
// each file's diff against that commit, once however many results share
// them. It is for one request: it never sees a later change.
type Linker struct {
	ctx context.Context // a Linker lives for one request, whose context it is
	// picked is the remote the user picked; "" for each repo's preferred one.
	picked string
	repos  map[string]*repoLinks
	files  map[[2]string]*fileLinks
	pushed map[[2]string]bool
}

// repoLinks is what every link into one repo shares.
type repoLinks struct {
	remote  protocol.Remote
	problem *protocol.Link // set when the repo can't have links
	prefix  string         // the folder's path in its repository, ending in "/"
	base    string         // the commit links to the files now go to; "" when the remote has none
	err     error
}

// fileLinks is what every link into one file now shares.
type fileLinks struct {
	path  string // its path in base, from the folder: the old path of a file renamed since
	hunks []hunk // base's file against the file on disk
	gone  bool   // base doesn't have the file
	err   error
}

// NewLinker starts linking for one request, to the remote picked, or to
// each repo's preferred remote when picked is "".
func NewLinker(ctx context.Context, picked string) *Linker {
	return &Linker{
		ctx: ctx, picked: picked,
		repos: map[string]*repoLinks{}, files: map[[2]string]*fileLinks{}, pushed: map[[2]string]bool{},
	}
}

// repo reads (once) what links into root share.
func (l *Linker) repo(root string) *repoLinks {
	if repo, ok := l.repos[root]; ok {
		return repo
	}
	repo := &repoLinks{}
	l.repos[root] = repo
	repo.remote, repo.problem, repo.err = chosen(l.ctx, root, l.picked)
	if repo.err != nil || repo.problem != nil {
		return repo
	}
	prefix, err := git(l.ctx, root, "rev-parse", "--show-prefix")
	if err != nil {
		repo.err = err
		return repo
	}
	repo.prefix = prefix
	repo.base = pushedBase(l.ctx, root, repo.remote.Name)
	return repo
}

// onRemote reports (once per commit) whether the remote has sha.
func (l *Linker) onRemote(root, remote, sha string) bool {
	key := [2]string{root, sha}
	if pushed, ok := l.pushed[key]; ok {
		return pushed
	}
	pushed := onRemote(l.ctx, root, remote, sha)
	l.pushed[key] = pushed
	return pushed
}

// Commit links to a commit result: the commit itself, when the remote has it.
func (l *Linker) Commit(root, sha string) (protocol.Link, error) {
	repo := l.repo(root)
	if repo.err != nil || repo.problem != nil {
		return deref(repo.problem), repo.err
	}
	remote := repo.remote
	if !l.onRemote(root, remote.Name, sha) {
		return protocol.Link{Remote: &remote, Problem: protocol.LinkProblemNotPushed}, nil
	}
	return protocol.Link{Remote: &remote, Commit: sha}, nil
}

// File links to a result in a file.
func (l *Linker) File(file File) (protocol.Link, error) {
	repo := l.repo(file.Root)
	if repo.err != nil || repo.problem != nil {
		return deref(repo.problem), repo.err
	}
	remote := repo.remote
	link := protocol.Link{Remote: &remote, Line: file.Line, EndLine: file.EndLine}
	// An at: result's file is the revision's, so its lines need no mapping.
	if file.Revision != "" {
		if !l.onRemote(file.Root, remote.Name, file.Revision) {
			return protocol.Link{Remote: &remote, Problem: protocol.LinkProblemNotPushed}, nil
		}
		link.Commit, link.Path = file.Revision, repo.prefix+file.Path
		return link, nil
	}
	if repo.base == "" {
		return protocol.Link{Remote: &remote, Problem: protocol.LinkProblemNotPushed}, nil
	}
	now := l.fileSince(file, repo.base)
	switch {
	case now.err != nil:
		return protocol.Link{}, now.err
	case now.gone:
		return protocol.Link{Remote: &remote, Problem: protocol.LinkProblemNotOnRemote}, nil
	}
	link.Commit, link.Path = repo.base, repo.prefix+now.path
	if file.Line == 0 {
		return link, nil
	}
	line, lineOK := mapLine(now.hunks, file.Line)
	end, endOK := line, lineOK
	if file.EndLine > 0 {
		end, endOK = mapLine(now.hunks, file.EndLine)
	}
	if !lineOK || !endOK {
		return protocol.Link{Remote: &remote, Problem: protocol.LinkProblemLineChanged}, nil
	}
	link.Line = line
	if file.EndLine > 0 {
		link.EndLine = end
	}
	return link, nil
}

// fileSince reads (once) the file's path in base and its diff from there to disk.
func (l *Linker) fileSince(file File, base string) *fileLinks {
	key := [2]string{file.Root, file.Path}
	if found, ok := l.files[key]; ok {
		return found
	}
	found := &fileLinks{}
	l.files[key] = found
	path, then, err := fileAt(l.ctx, file.Root, base, file.Path)
	switch {
	case err != nil:
		found.err = err
		return found
	case then == nil:
		found.gone = true
		return found
	}
	found.path = path
	now, err := os.ReadFile(filepath.Join(file.Root, filepath.FromSlash(file.Path)))
	if err != nil {
		found.err = err
		return found
	}
	found.hunks, found.err = diffHunks(l.ctx, file.Root, then, now)
	return found
}

// deref returns the problem link, or an empty one.
func deref(link *protocol.Link) protocol.Link {
	if link == nil {
		return protocol.Link{}
	}
	return *link
}

// chosen picks the link's remote, or says why there is none.
func chosen(ctx context.Context, root, picked string) (protocol.Remote, *protocol.Link, error) {
	remotes, err := Remotes(ctx, root)
	switch {
	case err != nil:
		return protocol.Remote{}, nil, err
	case !remotes.Git:
		return protocol.Remote{}, &protocol.Link{Problem: protocol.LinkProblemNotGit}, nil
	case len(remotes.Remotes) == 0:
		return protocol.Remote{}, &protocol.Link{Problem: protocol.LinkProblemNoRemote}, nil
	}
	name := remotes.Preferred
	if picked != "" {
		name = picked
	}
	for _, remote := range remotes.Remotes {
		if remote.Name == name {
			return remote, nil, nil
		}
	}
	return protocol.Remote{}, &protocol.Link{Problem: protocol.LinkProblemPickRemote, Remotes: remotes.Remotes}, nil
}

// onRemote reports whether one of the remote's branches has commit sha,
// as far as the last fetch knows.
func onRemote(ctx context.Context, root, remote, sha string) bool {
	out, err := git(ctx, root, "for-each-ref", "--count=1", "--contains", sha, "--format=%(refname)", "refs/remotes/"+remote+"/")
	return err == nil && out != ""
}

// pushedBase is the commit a link to the files now goes to: HEAD when the
// remote has it, else the newest commit of the remote's branch that HEAD
// builds on (the upstream's, or else the remote's default branch). It is
// "" when the remote has none of HEAD's history.
func pushedBase(ctx context.Context, root, remote string) string {
	head, err := git(ctx, root, "rev-parse", "--verify", "--quiet", "HEAD")
	if err != nil || head == "" {
		return ""
	}
	if onRemote(ctx, root, remote, head) {
		return head
	}
	var branches []string
	if upstream, upErr := git(ctx, root, "rev-parse", "--symbolic-full-name", "@{upstream}"); upErr == nil &&
		strings.HasPrefix(upstream, "refs/remotes/"+remote+"/") {
		branches = append(branches, upstream)
	}
	branches = append(branches, "refs/remotes/"+remote+"/HEAD")
	for _, branch := range branches {
		if base, baseErr := git(ctx, root, "merge-base", head, branch); baseErr == nil && base != "" {
			return base
		}
	}
	return ""
}

// fileAt reads path (relative to root) as it was in commit sha, following a
// rename since: it returns the path the commit has it at and its content,
// or a nil content when the commit doesn't have the file.
func fileAt(ctx context.Context, root, sha, path string) (string, []byte, error) {
	content, err := history.ReadFileAt(ctx, root, sha, path)
	if err == nil {
		return path, content, nil
	}
	if !errors.Is(err, history.ErrStale) {
		return "", nil, err
	}
	old := renamedFrom(ctx, root, sha, path)
	if old == "" {
		return path, nil, nil
	}
	content, err = history.ReadFileAt(ctx, root, sha, old)
	if errors.Is(err, history.ErrStale) {
		return path, nil, nil
	}
	return old, content, err
}

// renamedFrom is the path that path had in commit sha, when git finds it was
// renamed from there since (committed or not); "" otherwise.
func renamedFrom(ctx context.Context, root, sha, path string) string {
	// With no paths given, so git can pair the deleted path with the added one.
	out, err := git(ctx, root, "diff", "--find-renames", "--name-status", "-z", "--relative", "--no-ext-diff", sha)
	if err != nil {
		return ""
	}
	fields := strings.Split(out, "\x00")
	for i := 0; i+2 < len(fields); i++ {
		if strings.HasPrefix(fields[i], "R") && fields[i+2] == path {
			return fields[i+1]
		}
	}
	return ""
}
