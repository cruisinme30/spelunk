package trigram

// Why a folder's files aren't indexed, and which Git repo inside a root
// holds a folder: what Search in Folder's notes say.

import (
	"context"
	"io/fs"
	"path/filepath"
)

// FolderSkip is why ListFiles lists no file inside a folder.
type FolderSkip string

// The reasons WhySkipped gives.
const (
	FolderListed   FolderSkip = ""         // its files can be listed: none is left out on purpose
	FolderIgnored  FolderSkip = "ignored"  // Git ignores them (.gitignore)
	FolderExcluded FolderSkip = "excluded" // the index.exclude patterns or nested roots leave them out
	FolderLink     FolderSkip = "link"     // the folder is reached through a symlink
	FolderEmpty    FolderSkip = "empty"    // it holds no file
)

// maxFolderProbe bounds how many of a folder's files WhySkipped looks at:
// enough to tell why they're left out, without walking a huge folder.
const maxFolderProbe = 200

// WhySkipped says why ListFiles leaves out the files in folder
// (slash-separated, relative to root), for a folder the index holds no
// file of. It looks at the folder's first files on disk: none means it is
// empty; all excluded, or ignored by Git, names that reason. FolderListed
// means some file there would be indexed, as when the index hasn't caught
// up with a new folder yet.
func WhySkipped(ctx context.Context, root, folder string, opts WalkOptions) FolderSkip {
	if throughLink(root, folder, map[string]bool{}) {
		return FolderLink
	}
	files := firstFiles(root, folder, maxFolderProbe)
	if len(files) == 0 {
		return FolderEmpty
	}
	var kept []string
	for _, path := range files {
		if !opts.leavesOut(path) {
			kept = append(kept, path)
		}
	}
	if len(kept) == 0 {
		return FolderExcluded
	}
	if !opts.IncludeIgnored && len(gitIgnored(ctx, root, kept)) == len(kept) {
		return FolderIgnored
	}
	return FolderListed
}

// GitRepoOf returns the Git repo inside root (a submodule, or a repo cloned
// in it) that holds folder, relative to root, or "" when that is root's own.
func GitRepoOf(root, folder string) string {
	if folder == "" {
		return ""
	}
	return innermostRepo(root, folder, map[string]string{})
}

// firstFiles lists up to limit regular files under folder, relative to
// root, never going into .git or through a symlink.
func firstFiles(root, folder string, limit int) []string {
	var paths []string
	_ = filepath.WalkDir(filepath.Join(root, filepath.FromSlash(folder)), func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // an unreadable entry is skipped, as ListFiles skips it
		}
		switch {
		case entry.IsDir() && entry.Name() == ".git":
			return filepath.SkipDir
		case entry.Type().IsRegular():
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return nil //nolint:nilerr // can't happen: WalkDir stays under root
			}
			paths = append(paths, filepath.ToSlash(relative))
			if len(paths) >= limit {
				return filepath.SkipAll
			}
		}
		return nil
	})
	return paths
}
