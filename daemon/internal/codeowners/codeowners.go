package codeowners

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/cruisinme30/spelunk/daemon/internal/glob"
)

// DefaultLocations are where GitHub looks for a repo's CODEOWNERS file, in
// the order it looks: the first one that exists is the only one that
// counts. The index.codeownersFiles setting can name others.
var DefaultLocations = []string{".github/CODEOWNERS", "CODEOWNERS", "docs/CODEOWNERS"}

// LocationsOr returns locations, or DefaultLocations when it is nil.
func LocationsOr(locations []string) []string {
	if locations == nil {
		return DefaultLocations
	}
	return locations
}

// maxFileBytes is the largest CODEOWNERS file GitHub reads, 3 MB; a bigger
// one counts as having no owners.
const maxFileBytes = 3 << 20

// File is a parsed CODEOWNERS file.
type File struct {
	// Path is where the file is, relative to the repo root: one of the
	// locations Load looked in.
	Path string
	// Rules are its valid lines, in file order.
	Rules []Rule
	// Skipped are the line numbers (1-based) of lines GitHub would skip as
	// invalid: a ! or [ ] pattern, or an owner that is neither a @user, an
	// @org/team nor an email.
	Skipped []int
}

// Rule is one line of a CODEOWNERS file: a pattern and the owners of the
// paths it matches. A rule with no owners leaves its paths unowned, even
// where an earlier rule gave them owners.
type Rule struct {
	Line    int // 1-based
	Pattern string
	Owners  []string
	re      *regexp.Regexp
}

// Load reads the CODEOWNERS file of the repo at root from the first of
// locations (slash-separated, relative to root; nil for DefaultLocations)
// that holds a file. It returns nil when there is none, or when that file
// can't be read or is too big for GitHub. A location outside root is
// skipped, and so is one reached through a symlink, which could point
// anywhere (/dev/zero, say), or that isn't a regular file.
func Load(root string, locations []string) *File {
	for _, location := range LocationsOr(locations) {
		if !filepath.IsLocal(filepath.FromSlash(location)) {
			continue
		}
		info, err := lstatEach(root, location)
		switch {
		case errors.Is(err, fs.ErrNotExist), err == nil && !info.Mode().IsRegular():
			continue
		case err != nil || info.Size() > maxFileBytes:
			return nil
		}
		content, err := readAtMost(filepath.Join(root, filepath.FromSlash(location)), maxFileBytes)
		if err != nil {
			return nil
		}
		return Parse(location, content)
	}
	return nil
}

// lstatEach stats each element of the slash-separated location under
// root without following links, and returns the last one's info. A
// symlink anywhere on the way counts as no file there.
func lstatEach(root, location string) (fs.FileInfo, error) {
	var info fs.FileInfo
	path := root
	for _, element := range strings.Split(location, "/") {
		path = filepath.Join(path, element)
		var err error
		if info, err = os.Lstat(path); err != nil {
			return nil, err
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			return nil, fs.ErrNotExist
		}
	}
	return info, nil
}

// readAtMost reads the regular file at path, failing if it is not one
// (it may have changed since it was checked) or holds more than limit bytes.
func readAtMost(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path) //nolint:gosec // G304: a local path inside a workspace folder the user opened
	if err != nil {
		return nil, err
	}
	defer f.Close() //nolint:errcheck // read-only
	if info, err := f.Stat(); err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("not a regular file")
	}
	content, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err == nil && int64(len(content)) > limit {
		err = errors.New("too big")
	}
	return content, err
}

// Parse reads a CODEOWNERS file's content; path is where it is (see File.Path).
func Parse(path string, content []byte) *File {
	file := &File{Path: path}
	scanner := bufio.NewScanner(bytes.NewReader(content))
	scanner.Buffer(nil, maxFileBytes)
	for number := 1; scanner.Scan(); number++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if rule, ok := parseRule(line); ok {
			rule.Line = number
			file.Rules = append(file.Rules, rule)
		} else {
			file.Skipped = append(file.Skipped, number)
		}
	}
	return file
}

// parseRule reads one line: a pattern, then its owners, then perhaps a
// # comment. ok is false for a line GitHub would skip.
func parseRule(line string) (rule Rule, ok bool) {
	fields := splitFields(line)
	pattern := fields[0]
	var owners []string
	for _, field := range fields[1:] {
		if strings.HasPrefix(field, "#") {
			break
		}
		if !ownerPattern.MatchString(field) {
			return Rule{}, false
		}
		owners = append(owners, field)
	}
	re, ok := compilePattern(pattern)
	if !ok {
		return Rule{}, false
	}
	return Rule{Pattern: pattern, Owners: owners, re: re}, true
}

// ownerPattern is a valid owner: a @user, an @org/team, or an email.
var ownerPattern = regexp.MustCompile(`^(?:@[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?(?:/[A-Za-z0-9._-]+)?|[^@\s]+@[^@\s]+\.[^@\s]+)$`)

// splitFields splits a line at runs of whitespace, except a space written
// \ , which stays in its field as a space: a path with a space in it.
func splitFields(line string) []string {
	var fields []string
	var field strings.Builder
	flush := func() {
		if field.Len() > 0 {
			fields = append(fields, field.String())
			field.Reset()
		}
	}
	for i := 0; i < len(line); i++ {
		switch c := line[i]; {
		case c == '\\' && i+1 < len(line) && line[i+1] == ' ':
			field.WriteByte(' ')
			i++
		case c == ' ' || c == '\t':
			flush()
		default:
			field.WriteByte(c)
		}
	}
	flush()
	return fields
}

// compilePattern turns a pattern into a regex over repo-relative paths, as
// .gitignore patterns match, with GitHub's exceptions:
//   - A pattern with a slash at its start or in its middle matches from the
//     repo root; one without matches at any depth.
//   - A pattern that ends in a slash matches a folder's files at any depth;
//     one that matches a folder or a file name matches that file, or every
//     file in that folder, except that a last segment of only * matches the
//     folder's own files and not those of folders inside it (docs/*).
//   - * and ? stay within a folder, and ** spans folders.
//
// ok is false for what GitHub doesn't support: negation (!) and character
// ranges ([ ]).
func compilePattern(pattern string) (*regexp.Regexp, bool) {
	if strings.HasPrefix(pattern, "!") || strings.ContainsAny(pattern, "[]") {
		return nil, false
	}
	folder := strings.HasSuffix(pattern, "/")
	trimmed := strings.TrimSuffix(pattern, "/")
	anchored := strings.Contains(trimmed, "/")
	trimmed = strings.TrimPrefix(trimmed, "/")
	if trimmed == "" {
		return nil, false
	}
	start := "^(?:.*/)?"
	if anchored {
		start = "^"
	}
	end := "(?:/.*)?$"
	switch {
	case folder:
		end = "/.*$"
	case trimmed == "*" || strings.HasSuffix(trimmed, "/*"):
		end = "$"
	}
	re, err := regexp.Compile(start + glob.Body(trimmed, false) + end)
	return re, err == nil
}

// Match returns the rule that decides path's owners: the last one whose
// pattern matches it. ok is false when no rule matches.
func (f *File) Match(path string) (rule Rule, ok bool) {
	for i := len(f.Rules) - 1; i >= 0; i-- {
		if f.Rules[i].re.MatchString(path) {
			return f.Rules[i], true
		}
	}
	return Rule{}, false
}

// Owners returns path's owners, or nil when it has none.
func (f *File) Owners(path string) []string {
	rule, _ := f.Match(path)
	return rule.Owners
}

// SameOwner reports whether an owner: value names owner. Case doesn't
// matter, and neither does the @: @jane, jane and JANE all name @jane. A
// team can be named in full (@acme/payments) or by its name alone
// (payments); an email must be given in full.
func SameOwner(owner, value string) bool {
	owner, value = strings.ToLower(owner), strings.TrimPrefix(strings.ToLower(value), "@")
	handle := strings.TrimPrefix(owner, "@")
	if handle == value {
		return true
	}
	if !strings.HasPrefix(owner, "@") {
		return false // an email
	}
	_, team, isTeam := strings.Cut(handle, "/")
	return isTeam && team == value
}
