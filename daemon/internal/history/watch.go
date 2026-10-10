package history

import (
	"context"
	"strings"
)

// NewCommits returns the commits next holds that old did not, newest first:
// what an Update brought in, for query watches. When HEAD moved forward
// they are the commits read on top of old. When history was rewritten (a
// rebase, a reset, a branch switch) they are the commits old never held
// that were also committed after everything it held, so switching to an
// older branch does not count commits that were already there as new. It
// goes by when each commit was made here, not authored: a pull that
// rebased your commits on today's upstream brings in upstream commits
// authored before yours. A nil old store (the first read) brings no new
// commits.
func NewCommits(old, next *Store) []Commit {
	if old == nil || next == nil || old.Head == next.Head {
		return nil
	}
	if next.from == old.Head {
		return next.newest(next.fresh)
	}
	known := map[string]bool{}
	var newest Commit
	for _, seg := range old.segments {
		for i := range seg.commits {
			known[seg.commits[i].SHA] = true
			if seg.commits[i].Committed.After(newest.Committed) {
				newest = seg.commits[i]
			}
		}
	}
	var fresh []Commit
	for _, seg := range next.segments {
		for i := range seg.commits {
			if c := seg.commits[i]; !known[c.SHA] && c.Committed.After(newest.Committed) {
				fresh = append(fresh, c)
			}
		}
	}
	return fresh
}

// newest returns the store's n newest commits, newest first.
func (s *Store) newest(n int) []Commit {
	commits := make([]Commit, 0, n)
	for _, seg := range s.segments {
		if len(commits) == n {
			break
		}
		commits = append(commits, seg.commits[:min(n-len(commits), len(seg.commits))]...)
	}
	return commits
}

// StoreOf returns a store holding only commits, newest first, so a history
// plan can be run over them alone.
func StoreOf(head string, commits []Commit) *Store {
	b := &builder{store: &Store{Head: head, touches: map[string]Touch{}}}
	for i := range commits {
		b.add(commits[i])
	}
	b.flush()
	return b.snapshot().WithDirty(map[string]bool{})
}

// UserEmail returns the email Git commits under in root (user.email), or ""
// when none is set.
func UserEmail(ctx context.Context, root string) string {
	out, err := git(ctx, root, "config", "--get", "user.email")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
