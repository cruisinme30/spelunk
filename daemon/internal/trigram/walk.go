package trigram

import (
	"bytes"
	"context"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// binarySniffBytes: a NUL byte in the first 8 KB marks a file as binary.
const binarySniffBytes = 8 << 10

// WalkOptions are the index.* settings that decide which files are indexed.
type WalkOptions struct {
	Exclude        Excluder
	IncludeIgnored bool
	MaxFileBytes   int64
}

// File is one file chosen for indexing.
type File struct {
	Path string // slash-separated, relative to the repo root
}

// ListFiles returns the files under root to index, sorted by path. In a Git
// repo, Git decides what .gitignore excludes (unless IncludeIgnored), and
// the files of nested repos and submodules are listed by their own Git;
// elsewhere every file is listed. .git itself is never listed. Binary and
// oversized files are left out, and so is anything that isn't a regular
// file: symlinks, FIFOs, sockets and devices.
func ListFiles(ctx context.Context, root string, opts WalkOptions) ([]File, error) {
	paths, err := candidatePaths(ctx, root, opts.IncludeIgnored)
	if err != nil {
		return nil, err
	}
	var files []File
	// paths grows as nested repos are found, so it is walked by index.
	for i := 0; i < len(paths); i++ {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		path := strings.TrimSuffix(paths[i], "/") // Git lists a nested repo as "dir/"
		if opts.Exclude.Excludes(path) {
			continue
		}
		full := filepath.Join(root, filepath.FromSlash(path))
		info, err := os.Lstat(full)
		switch {
		case err != nil:
			continue
		case info.IsDir():
			// Git lists a submodule or a nested repo as one entry.
			paths = append(paths, nestedRepoPaths(ctx, root, path)...)
			continue
		case !info.Mode().IsRegular():
			continue
		}
		if opts.MaxFileBytes > 0 && info.Size() > opts.MaxFileBytes {
			continue
		}
		if isBinaryFile(full) {
			continue
		}
		files = append(files, File{Path: path})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

// nestedRepoPaths lists the files of the Git repo at dir (relative to
// root), a submodule or a repo cloned inside another, as paths relative to
// root. It lists nothing for a folder without its own .git, such as a
// submodule that isn't checked out.
func nestedRepoPaths(ctx context.Context, root, dir string) []string {
	full := filepath.Join(root, filepath.FromSlash(dir))
	if !hasGitDir(full) {
		return nil
	}
	nested, err := gitListFiles(ctx, full)
	if err != nil {
		return nil
	}
	for i, path := range nested {
		nested[i] = dir + "/" + path
	}
	return nested
}

// hasGitDir reports whether dir has a .git of its own: a folder in a
// repo, a file in a submodule or worktree.
func hasGitDir(dir string) bool {
	_, err := os.Lstat(filepath.Join(dir, ".git"))
	return err == nil
}

// SelectFiles returns which of paths (slash-separated, relative to root)
// ListFiles would index now: files that exist and are not excluded,
// ignored by Git, binary or too big. It is how files saved since the last
// build are re-read without listing the whole repo.
func SelectFiles(ctx context.Context, root string, paths []string, opts WalkOptions) []File {
	local := make([]string, 0, len(paths))
	for _, path := range paths {
		if filepath.IsLocal(filepath.FromSlash(path)) && !opts.Exclude.Excludes(path) {
			local = append(local, path)
		}
	}
	ignored := map[string]bool{}
	if !opts.IncludeIgnored {
		ignored = gitIgnored(ctx, root, local)
	}
	var files []File
	for _, path := range local {
		if ignored[path] {
			continue
		}
		full := filepath.Join(root, filepath.FromSlash(path))
		info, err := os.Lstat(full)
		if err != nil || !info.Mode().IsRegular() || opts.MaxFileBytes > 0 && info.Size() > opts.MaxFileBytes || isBinaryFile(full) {
			continue
		}
		files = append(files, File{Path: path})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files
}

// gitIgnored returns which of paths .gitignore excludes; none outside Git.
// Each path is asked of the innermost repo that holds it: Git refuses a
// path inside a submodule, and would then answer for no path at all.
func gitIgnored(ctx context.Context, root string, paths []string) map[string]bool {
	ignored := map[string]bool{}
	repos := map[string]string{} // by folder, the nested repo holding it ("" for root's)
	byRepo := map[string][]string{}
	for _, path := range paths {
		repo := innermostRepo(root, parentDir(path), repos)
		byRepo[repo] = append(byRepo[repo], path)
	}
	for repo, inRepo := range byRepo {
		prefix := ""
		if repo != "" {
			prefix = repo + "/"
		}
		relative := make([]string, len(inRepo))
		for i, path := range inRepo {
			relative[i] = strings.TrimPrefix(path, prefix)
		}
		dir := filepath.Join(root, filepath.FromSlash(repo))
		//nolint:gosec // G204: dir is a folder inside the root being indexed; the paths go to git on stdin
		cmd := exec.CommandContext(ctx, "git", "-C", dir, "check-ignore", "-z", "--stdin")
		cmd.Stdin = strings.NewReader(strings.Join(relative, "\x00") + "\x00")
		out, _ := cmd.Output() // exit status 1 means "none ignored"; outside Git there is no output
		for _, path := range strings.Split(strings.TrimRight(string(out), "\x00"), "\x00") {
			if path != "" {
				ignored[prefix+path] = true
			}
		}
	}
	return ignored
}

// parentDir is the folder of a slash-separated relative path, "." at the top.
func parentDir(path string) string {
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		return path[:i]
	}
	return "."
}

// innermostRepo returns the nested repo (relative to root) that holds dir,
// or "" when that is root's own. known caches the answer for each folder.
func innermostRepo(root, dir string, known map[string]string) string {
	if dir == "." {
		return ""
	}
	if repo, ok := known[dir]; ok {
		return repo
	}
	repo := dir
	if !hasGitDir(filepath.Join(root, filepath.FromSlash(dir))) {
		repo = innermostRepo(root, parentDir(dir), known)
	}
	known[dir] = repo
	return repo
}

// candidatePaths lists the files ListFiles then filters: what Git says the
// repo holds (tracked files and untracked ones .gitignore doesn't exclude),
// or every file when includeIgnored is set or root isn't a Git work tree.
func candidatePaths(ctx context.Context, root string, includeIgnored bool) ([]string, error) {
	if !includeIgnored {
		if paths, err := gitListFiles(ctx, root); err == nil {
			return paths, nil
		}
		// Not a Git work tree, or no git installed: list every file instead.
	}
	return walkAllFiles(ctx, root)
}

// gitListFiles lists tracked and untracked-but-not-ignored files.
func gitListFiles(ctx context.Context, root string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", root, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var paths []string
	for _, path := range strings.Split(strings.TrimRight(string(out), "\x00"), "\x00") {
		if path != "" && !seen[path] { // a file with a merge conflict is listed once per stage
			seen[path] = true
			paths = append(paths, path)
		}
	}
	return paths, nil
}

// walkAllFiles lists every regular file under root except inside .git.
func walkAllFiles(ctx context.Context, root string) ([]string, error) {
	// WalkDir doesn't follow a symlink, even at the top: a root reached
	// through one (/tmp on macOS, say) is walked where it points.
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	var paths []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // an unreadable entry is skipped, not fatal
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if entry.IsDir() {
			if entry.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err == nil {
			paths = append(paths, filepath.ToSlash(relative))
		}
		return nil
	})
	return paths, err
}

// isBinaryFile reports whether the file at path looks binary; unreadable
// files, and anything that is no longer a regular file, count as binary,
// so they are skipped.
func isBinaryFile(path string) bool {
	f, _, err := openRegular(path)
	if err != nil {
		return true
	}
	defer closeReadOnly(f)
	head := make([]byte, binarySniffBytes)
	n, _ := io.ReadFull(f, head)
	return isBinary(head[:n])
}

// isBinary reports whether a file's first bytes contain a NUL, unless they
// start with a UTF-16 byte order mark: UTF-16 text is full of NULs, and is
// converted to UTF-8 when it is read (see decodeText).
func isBinary(head []byte) bool { return !hasUTF16BOM(head) && bytes.IndexByte(head, 0) >= 0 }

// closeReadOnly closes a file that was only read: a close error can't lose data.
func closeReadOnly(f *os.File) { _ = f.Close() }
