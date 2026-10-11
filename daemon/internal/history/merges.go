package history

import (
	"context"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// landing is the order index.mergedBranches reads commits in: newest
// landed first, each merge on the branch's first-parent line followed by
// the commits it brought in from other branches, newest first, with the
// merge each of those came in by.
type landing struct {
	shas     []string
	mergedIn map[string]string    // a merged-in commit's sha -> its merge's
	mergedAt map[string]time.Time // a merge's sha -> its commit date
}

// landingOrder lists the commits that reached the branch at head after
// from (its whole history for ""), and after since (by first-parent
// commit date; zero for no limit), in landed order. Each first-parent
// commit counts once; a commit merged in counts with the first merge that
// brought it, and never again.
func landingOrder(ctx context.Context, root, from, head string, since time.Time) (landing, error) {
	line, err := firstParentLine(ctx, root, from, head, since)
	if err != nil || len(line) == 0 {
		return landing{}, err
	}
	// Every commit reachable from head but not from what came before the
	// line: from, or the parent of the line's oldest commit.
	exclude := from
	if exclude == "" {
		if parent, parentErr := git(ctx, root, "rev-parse", "--verify", "--quiet", line[len(line)-1]+"^1"); parentErr == nil {
			exclude = strings.TrimSpace(string(parent))
		}
	}
	args := []string{"rev-list", "--parents", "--timestamp", head}
	if exclude != "" {
		args = append(args, "^"+exclude)
	}
	out, err := git(ctx, root, args...)
	if err != nil {
		return landing{}, err
	}
	type node struct {
		parents []string
		at      int64
	}
	graph := map[string]node{}
	for _, row := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Fields(row)
		if len(fields) < 2 {
			continue
		}
		at, _ := strconv.ParseInt(fields[0], 10, 64)
		graph[fields[1]] = node{parents: fields[2:], at: at}
	}
	seen := make(map[string]bool, len(graph))
	merged := map[string][]string{} // a merge -> the commits it brought in
	result := landing{mergedIn: map[string]string{}, mergedAt: map[string]time.Time{}}
	for i := len(line) - 1; i >= 0; i-- { // oldest first
		merge := line[i]
		seen[merge] = true
		parents := graph[merge].parents
		if len(parents) < 2 {
			continue
		}
		result.mergedAt[merge] = time.Unix(graph[merge].at, 0).UTC()
		stack := slices.Clone(parents[1:])
		for len(stack) > 0 {
			sha := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			n, inRange := graph[sha]
			if !inRange || seen[sha] {
				continue
			}
			seen[sha] = true
			merged[merge] = append(merged[merge], sha)
			result.mergedIn[sha] = merge
			stack = append(stack, n.parents...)
		}
	}
	for _, sha := range line { // newest first
		result.shas = append(result.shas, sha)
		brought := merged[sha]
		slices.SortStableFunc(brought, func(a, b string) int { return int(graph[b].at - graph[a].at) })
		result.shas = append(result.shas, brought...)
	}
	return result, nil
}

// firstParentLine lists head's first-parent line after from ("" for all of
// it) and since (zero for no limit), newest first.
func firstParentLine(ctx context.Context, root, from, head string, since time.Time) ([]string, error) {
	args := []string{"rev-list", "--first-parent"}
	if !since.IsZero() {
		args = append(args, "--since="+since.Format(time.RFC3339))
	}
	revisions := head
	if from != "" {
		revisions = from + ".." + head
	}
	out, err := git(ctx, root, append(args, revisions)...)
	if err != nil {
		return nil, err
	}
	return strings.Fields(string(out)), nil
}

// readLanded streams the commits of order, in its order, each merged-in
// one knowing its merge, calling each for every commit.
func readLanded(ctx context.Context, root string, order landing, opts Options, each func(Commit) error) error {
	if len(order.shas) == 0 {
		return nil
	}
	return readCommits(ctx, root, order.shas, opts, func(c Commit) error {
		if merge, ok := order.mergedIn[c.SHA]; ok {
			c.MergedIn, c.MergedAt = merge, order.mergedAt[merge]
		}
		return each(c)
	})
}

// remergeSupport remembers whether git has --remerge-diff (Git 2.36).
var remergeSupport struct {
	once sync.Once
	ok   bool
}

// canReadMergedBranches reports whether the installed Git can show a merge
// as what it changed itself (git log --remerge-diff, Git 2.36 or later).
// Without it, index.mergedBranches reads the first-parent line as usual.
func canReadMergedBranches(ctx context.Context) bool {
	remergeSupport.once.Do(func() {
		out, err := exec.CommandContext(ctx, "git", "version").Output()
		remergeSupport.ok = err == nil && gitAtLeast(string(out), 2, 36)
	})
	return remergeSupport.ok
}

// gitAtLeast reports whether `git version` output names major.minor or later.
func gitAtLeast(version string, major, minor int) bool {
	fields := strings.Fields(version) // git version 2.56.0 (Apple Git-155)
	if len(fields) < 3 {
		return false
	}
	parts := strings.SplitN(fields[2], ".", 3)
	if len(parts) < 2 {
		return false
	}
	gotMajor, err1 := strconv.Atoi(parts[0])
	gotMinor, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return false
	}
	return gotMajor > major || gotMajor == major && gotMinor >= minor
}
