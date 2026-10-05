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
	Size int64
}

// ListFiles returns the files under root to index, sorted by path. In a Git
// repo, Git decides what .gitignore excludes (unless IncludeIgnored);
// elsewhere every file is listed. .git itself is never listed. Binary and
// oversized files are left out.
func ListFiles(ctx context.Context, root string, opts WalkOptions) ([]File, error) {
	paths, err := candidatePaths(ctx, root, opts.IncludeIgnored)
	if err != nil {
		return nil, err
	}
	var files []File
	for _, path := range paths {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if opts.Exclude.Excludes(path) {
			continue
		}
		full := filepath.Join(root, filepath.FromSlash(path))
		info, err := os.Lstat(full)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		if opts.MaxFileBytes > 0 && info.Size() > opts.MaxFileBytes {
			continue
		}
		if isBinaryFile(full) {
			continue
		}
		files = append(files, File{Path: path, Size: info.Size()})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

// SelectFiles returns which of paths (slash-separated, relative to root)
// ListFiles would index now: files that exist and are not excluded,
// ignored by Git, binary or too big. It is how files saved since the last
// build are re-read without listing the whole repo.
func SelectFiles(ctx context.Context, root string, paths []string, opts WalkOptions) []File {
	ignored := map[string]bool{}
	if !opts.IncludeIgnored {
		ignored = gitIgnored(ctx, root, paths)
	}
	var files []File
	for _, path := range paths {
		if ignored[path] || opts.Exclude.Excludes(path) || !filepath.IsLocal(filepath.FromSlash(path)) {
			continue
		}
		full := filepath.Join(root, filepath.FromSlash(path))
		info, err := os.Lstat(full)
		if err != nil || !info.Mode().IsRegular() || opts.MaxFileBytes > 0 && info.Size() > opts.MaxFileBytes || isBinaryFile(full) {
			continue
		}
		files = append(files, File{Path: path, Size: info.Size()})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files
}

// gitIgnored returns which of paths .gitignore excludes; none outside Git.
func gitIgnored(ctx context.Context, root string, paths []string) map[string]bool {
	ignored := map[string]bool{}
	cmd := exec.CommandContext(ctx, "git", "-C", root, "check-ignore", "-z", "--stdin")
	cmd.Stdin = strings.NewReader(strings.Join(paths, "\x00") + "\x00")
	out, _ := cmd.Output() // exit status 1 means "none ignored"; outside Git there is no output
	for _, path := range strings.Split(strings.TrimRight(string(out), "\x00"), "\x00") {
		if path != "" {
			ignored[path] = true
		}
	}
	return ignored
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

// isBinary reports whether a file's first bytes contain a NUL.
func isBinary(head []byte) bool { return bytes.IndexByte(head, 0) >= 0 }

// closeReadOnly closes a file that was only read: a close error can't lose data.
func closeReadOnly(f *os.File) { _ = f.Close() }
