package query

import (
	"fmt"
	"os"
	"path"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/cruisinme30/spelunk/daemon/internal/lang"
	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
)

// valueCandidate is a value an operator could take, with how to show it.
type valueCandidate struct {
	value   string
	detail  string // what it means: "Last 2 hours"
	context string // facts that help choose it: "Since 08:04"
	note    string // a short status at the end of the row: "5 files changed"
	section string // a heading above it, when it starts a group
	group   string // a protocol.Completion group
}

// valueCandidates lists the values of op that match fragment (lowercase),
// best first: authors, repos, languages and paths found in the workspace,
// time windows for since: and until:, file states for is:, and the fixed
// values of type:, kind:, case:, word:, count: and order:.
func valueCandidates(op operator, fragment string, resolver Resolver, now time.Time) []valueCandidate {
	switch op.name {
	case protocol.OpNameAuthor:
		return authorCandidates(fragment, resolver, now)
	case protocol.OpNameRepo:
		return repoCandidates(fragment, resolver.Repos())
	case protocol.OpNameLang:
		return languageCandidates(fragment, resolver.Files())
	case protocol.OpNameF:
		return pathCandidates(fragment, resolver.Files())
	case protocol.OpNameSince:
		return sinceCandidates(fragment, resolver.Files(), now)
	case protocol.OpNameUntil:
		return untilCandidates(fragment, resolver.Files(), now)
	case protocol.OpNameMsg:
		return messageCandidates(resolver.MessageWords(fragment, maxValueCompletions+1), now)
	case protocol.OpNameSym, protocol.OpNameRef:
		return symbolCandidates(resolver.Symbols(fragment, maxValueCompletions+1))
	case protocol.OpNameIs:
		return stateCandidates(fragment, resolver)
	case protocol.OpNameType, protocol.OpNameKind, protocol.OpNameCase, protocol.OpNameWord, protocol.OpNameCount, protocol.OpNameOrder:
		return fixedCandidates(fixedValues[op.name], fragment)
	default:
		return nil
	}
}

// ------------------------------------------------------------ sym: and ref:

// symbolCandidates offers definition names: "class · 2 definitions ·
// payments-api, web-checkout".
func symbolCandidates(found []SymbolStat) []valueCandidate {
	candidates := make([]valueCandidate, len(found))
	for i, symbol := range found {
		candidates[i] = valueCandidate{
			value: symbol.Name, detail: symbol.Kind, context: plural(symbol.Definitions, "definition"),
			note: strings.Join(symbol.Repos, ", "), group: "value",
		}
	}
	return candidates
}

// ------------------------------------------------------------ msg:

// messageCandidates offers the phrases, then the words, of recent commit
// subjects. A phrase is inserted quoted, so it matches as written.
func messageCandidates(words []WordStat, now time.Time) []valueCandidate {
	var phrases, singles []valueCandidate
	for _, word := range words {
		candidate := valueCandidate{
			value: word.Text, detail: word.Text,
			context: "In " + plural(word.Commits, "commit message"), note: "last " + timeAgo(now.Sub(word.LastAt)),
			group: "value",
		}
		if strings.Contains(word.Text, " ") {
			phrases = append(phrases, candidate)
		} else {
			singles = append(singles, candidate)
		}
	}
	if len(phrases) > 0 {
		phrases[0].section = "Phrases"
	}
	if len(singles) > 0 && len(phrases) > 0 {
		singles[0].section = "Words"
	}
	return append(phrases, singles...)
}

// ------------------------------------------------------------ author:

func authorCandidates(fragment string, resolver Resolver, now time.Time) []valueCandidate {
	// One more than fits: valueCompletions skips a value already typed in
	// full, and the list should still be full after that.
	authors := resolver.Authors(fragment, maxValueCompletions+1)
	candidates := make([]valueCandidate, len(authors))
	for i, author := range authors {
		candidates[i] = valueCandidate{value: author.Name, detail: authorDetail(author, now), group: "author"}
	}
	return candidates
}

// authorDetail reads like "214 commits · payments-api, shared-libs · last 3 days ago".
func authorDetail(author AuthorStat, now time.Time) string {
	parts := []string{plural(author.Commits, "commit")}
	if len(author.Repos) > 0 {
		parts = append(parts, strings.Join(author.Repos, ", "))
	}
	if last, err := time.Parse(time.RFC3339, author.LastAt); err == nil {
		parts = append(parts, "last "+timeAgo(now.Sub(last)))
	}
	return strings.Join(parts, " · ")
}

// ------------------------------------------------------------ repo:

func repoCandidates(fragment string, repos []RepoStat) []valueCandidate {
	var candidates []valueCandidate
	for _, repo := range repos {
		if !strings.Contains(strings.ToLower(repo.Name), fragment) {
			continue
		}
		candidates = append(candidates, valueCandidate{
			value:   repo.Name,
			detail:  homeRelative(repo.Path),
			context: plural(repo.Files, "file"),
			note:    indexNote(repo),
			group:   "repo",
		})
	}
	return candidates
}

// indexNote says where a repo's index stands: "Index ready", "Indexing 64%".
func indexNote(repo RepoStat) string {
	switch repo.State {
	case protocol.IndexStateReady:
		return "Index ready"
	case protocol.IndexStateIndexing:
		return fmt.Sprintf("Indexing %.0f%%", repo.Progress*100)
	case protocol.IndexStateQueued:
		return "Waiting to index"
	case protocol.IndexStateError:
		return "Index failed"
	default:
		return ""
	}
}

// homeRelative writes a path under the home folder as ~/…, as people read it.
func homeRelative(dir string) string {
	home, err := os.UserHomeDir()
	if err == nil && home != "" && strings.HasPrefix(dir, home+string(os.PathSeparator)) {
		return "~" + dir[len(home):]
	}
	return dir
}

// ------------------------------------------------------------ lang:

// languageStat counts one language's files in the workspace.
type languageStat struct {
	name       string
	files      int
	extensions []string // as found in the workspace, in first-seen order
	repos      []string
}

func languageCandidates(fragment string, files []FileStat) []valueCandidate {
	stats := languageStats(files)
	var candidates []valueCandidate
	seen := map[string]bool{}
	for _, stat := range stats {
		seen[stat.name] = true
		if !languageMatches(stat.name, fragment) {
			continue
		}
		context := plural(stat.files, "file")
		if len(stat.extensions) > 0 {
			context = strings.Join(stat.extensions, " ") + " · " + context
		}
		candidates = append(candidates, valueCandidate{
			value: stat.name, detail: lang.Title(stat.name), context: context, note: summarizeRepos(stat.repos), group: "lang",
		})
	}
	if len(candidates) > 0 {
		candidates[0].section = "In this workspace"
	}
	// Typing a name offers every language, not just the ones already here.
	if fragment == "" && len(candidates) > 0 {
		return candidates
	}
	others := 0
	for _, name := range lang.Names() {
		if seen[name] || !languageMatches(name, fragment) {
			continue
		}
		candidate := valueCandidate{value: name, detail: lang.Title(name), context: "No files in this workspace", group: "lang"}
		if others == 0 && len(candidates) > 0 {
			candidate.section = "Other languages"
		}
		candidates = append(candidates, candidate)
		others++
	}
	return candidates
}

// languageMatches reports whether fragment starts the language's name, its
// title or one of its aliases (py for python).
func languageMatches(name, fragment string) bool {
	if strings.HasPrefix(name, fragment) || strings.HasPrefix(strings.ToLower(lang.Title(name)), fragment) {
		return true
	}
	resolved, ok := lang.Resolve(fragment)
	return ok && resolved == name
}

// languageStats counts files per language, most files first.
func languageStats(files []FileStat) []languageStat {
	byName := map[string]*languageStat{}
	var order []*languageStat
	for _, file := range files {
		if file.Lang == "" {
			continue
		}
		stat, ok := byName[file.Lang]
		if !ok {
			stat = &languageStat{name: file.Lang}
			byName[file.Lang] = stat
			order = append(order, stat)
		}
		stat.files++
		if ext := strings.ToLower(path.Ext(file.Path)); ext != "" && !slices.Contains(stat.extensions, ext) {
			stat.extensions = append(stat.extensions, ext)
		}
		if !slices.Contains(stat.repos, file.Repo) {
			stat.repos = append(stat.repos, file.Repo)
		}
	}
	stats := make([]languageStat, len(order))
	for i, stat := range order {
		stats[i] = *stat
	}
	slices.SortStableFunc(stats, func(a, b languageStat) int { return b.files - a.files })
	return stats
}

// ------------------------------------------------------------ f:

// The most file types and folders f: offers.
const (
	maxFileTypes = 5
	maxFolders   = 5
)

// pathStat counts the files with one extension, or under one folder.
type pathStat struct {
	key   string // ".py" or "src/payments"
	lang  string // the language of a file type's files, if they share one
	files int
	repos []string
}

func pathCandidates(fragment string, files []FileStat) []valueCandidate {
	candidates := fileTypeCandidates(fragment, files)
	return append(candidates, folderCandidates(fragment, files)...)
}

// fileTypeCandidates offers *.py-style globs for the extensions in the
// workspace that fragment ("py", ".py" or "*.py") starts.
func fileTypeCandidates(fragment string, files []FileStat) []valueCandidate {
	if strings.Contains(fragment, "/") {
		return nil
	}
	typed := strings.TrimLeft(fragment, "*.")
	stats := countPaths(files, func(file FileStat) []string {
		if ext := strings.ToLower(path.Ext(file.Path)); ext != "" {
			return []string{ext}
		}
		return nil
	})
	var candidates []valueCandidate
	for _, stat := range stats {
		if len(candidates) == maxFileTypes {
			break
		}
		if !strings.HasPrefix(stat.key[1:], typed) {
			continue
		}
		detail := stat.key + " files"
		if stat.lang != "" {
			detail = lang.Title(stat.lang) + " files"
		}
		candidates = append(candidates, valueCandidate{
			value: "*" + stat.key, detail: detail,
			context: plural(stat.files, "file") + " · " + summarizeRepos(stat.repos), group: "value",
		})
	}
	if len(candidates) > 0 {
		candidates[0].section = "File types"
	}
	return candidates
}

// folderCandidates offers folders: the top-level ones for an empty value,
// the ones under a typed path ("src/"), or the ones whose name a typed word
// starts ("pay" offers src/payments/).
func folderCandidates(fragment string, files []FileStat) []valueCandidate {
	stats := countPaths(files, func(file FileStat) []string {
		var folders []string
		for dir := path.Dir(file.Path); dir != "."; dir = path.Dir(dir) {
			folders = append(folders, dir)
		}
		return folders
	})
	var candidates []valueCandidate
	for _, stat := range stats {
		if len(candidates) == maxFolders {
			break
		}
		if !folderMatches(strings.ToLower(stat.key), fragment) {
			continue
		}
		candidates = append(candidates, valueCandidate{
			value: stat.key + "/", detail: "Folder in " + summarizeRepos(stat.repos),
			context: plural(stat.files, "file"), group: "value",
		})
	}
	if len(candidates) > 0 {
		candidates[0].section = "Folders"
	}
	return candidates
}

func folderMatches(folder, fragment string) bool {
	switch {
	case fragment == "":
		return !strings.Contains(folder, "/")
	case strings.Contains(fragment, "/"):
		return strings.HasPrefix(folder+"/", fragment) && folder+"/" != fragment
	default:
		return strings.HasPrefix(path.Base(folder), fragment)
	}
}

// countPaths groups files by the keys keysOf gives each one, most files first.
func countPaths(files []FileStat, keysOf func(FileStat) []string) []pathStat {
	byKey := map[string]*pathStat{}
	var order []*pathStat
	for _, file := range files {
		for _, key := range keysOf(file) {
			stat, ok := byKey[key]
			if !ok {
				stat = &pathStat{key: key, lang: file.Lang}
				byKey[key] = stat
				order = append(order, stat)
			}
			stat.files++
			if stat.lang != file.Lang {
				stat.lang = ""
			}
			if !slices.Contains(stat.repos, file.Repo) {
				stat.repos = append(stat.repos, file.Repo)
			}
		}
	}
	stats := make([]pathStat, len(order))
	for i, stat := range order {
		stats[i] = *stat
	}
	slices.SortStableFunc(stats, func(a, b pathStat) int { return b.files - a.files })
	return stats
}

// summarizeRepos names up to two repos: "payments-api, shared-libs", "3 repos".
func summarizeRepos(repos []string) string {
	if len(repos) <= 2 {
		return strings.Join(repos, ", ")
	}
	return fmt.Sprintf("%d repos", len(repos))
}

// ------------------------------------------------------------ since: and until:

// windowPresets are the time windows since: and until: offer, in groups.
var windowPresets = []struct{ value, section string }{
	{windowToday, "Calendar days"},
	{windowYesterday, ""},
	{"30min", "The last few hours"},
	{"2h", ""},
	{"2w", "Longer windows"},
	{"30d", ""},
	{"6m", ""},
	{"1y", ""},
}

// windowUnits are durationPattern's units, shortest first.
var windowUnits = []string{"min", "h", "d", "w", "m", "y"}

// partialDuration is a since: or until: value being typed: a number, perhaps with
// the start of a unit ("3", "45mi").
var partialDuration = regexp.MustCompile(`^([1-9]\d{0,4})([a-z]*)$`)

// windowPicks offers the since: or until: values that fragment starts.
// Dates aren't offered; a date being typed (2026-0) matches none.
func windowPicks(fragment string) []windowPick {
	if parts := partialDuration.FindStringSubmatch(fragment); parts != nil {
		return windowUnitPicks(parts[1], parts[2])
	}
	return windowPresetPicks(fragment)
}

func sinceCandidates(fragment string, files []FileStat, now time.Time) []valueCandidate {
	picks := windowPicks(fragment)
	candidates := make([]valueCandidate, len(picks))
	for i, p := range picks {
		start := since(now, p.value)
		candidates[i] = valueCandidate{
			value:   p.value,
			detail:  describeWindow(p.value),
			context: describeStart(start, now),
			note:    changedFiles(files, start),
			section: p.section,
			group:   "value",
		}
	}
	return candidates
}

// windowPick is a since: or until: value to offer, with the heading it starts, if any.
type windowPick struct{ value, section string }

// windowUnitPicks offers a typed number with each unit its typed letters
// start: 3 → 3min, 3h … 3y; 45mi → 45min.
func windowUnitPicks(number, unitStart string) []windowPick {
	var picks []windowPick
	for _, unit := range windowUnits {
		if strings.HasPrefix(unit, unitStart) {
			picks = append(picks, windowPick{value: number + unit})
		}
	}
	return picks
}

// windowPresetPicks offers the windows fragment starts, each group's
// heading on the first of its windows that is offered.
func windowPresetPicks(fragment string) []windowPick {
	var picks []windowPick
	section, shown := "", ""
	for _, window := range windowPresets {
		if window.section != "" {
			section = window.section
		}
		if !strings.HasPrefix(window.value, fragment) {
			continue
		}
		heading := ""
		if section != shown {
			heading, shown = section, section
		}
		picks = append(picks, windowPick{window.value, heading})
	}
	return picks
}

// windowUnitNames names durationPattern's units, singular and plural.
var windowUnitNames = map[string][2]string{
	"min": {"minute", "minutes"},
	"h":   {"hour", "hours"},
	"d":   {"day", "days"},
	"w":   {"week", "weeks"},
	"m":   {"month", "months"},
	"y":   {"year", "years"},
}

// describeWindow says what a since: value covers: "Last 2 hours", "Today".
func describeWindow(value string) string {
	switch strings.ToLower(value) {
	case windowToday:
		return "Today"
	case windowYesterday:
		return "Yesterday and today"
	}
	parts := durationPattern.FindStringSubmatch(value)
	names := windowUnitNames[parts[2]]
	if parts[1] == "1" {
		return "Last " + names[0]
	}
	return "Last " + parts[1] + " " + names[1]
}

// describeStart says when a window starts, as precisely as it matters:
// "Since 08:04" today, "Since Fri, Oct 2, 00:00" for a recent day,
// "Since Sat, Sep 19" further back, with the year when it differs.
func describeStart(start, now time.Time) string {
	start = start.In(now.Location())
	sameDay := start.Year() == now.Year() && start.YearDay() == now.YearDay()
	switch {
	case sameDay && start.Hour() == 0 && start.Minute() == 0:
		return "Since midnight"
	case sameDay:
		return "Since " + start.Format("15:04")
	case now.Sub(start) < 3*24*time.Hour:
		return "Since " + start.Format("Mon, Jan 2, 15:04")
	case start.Year() == now.Year():
		return "Since " + start.Format("Mon, Jan 2")
	default:
		return "Since " + start.Format("Mon, Jan 2, 2006")
	}
}

// changedFiles counts the indexed files modified since start: "5 files changed".
func changedFiles(files []FileStat, start time.Time) string {
	changed := 0
	for _, file := range files {
		if !file.ModTime.Before(start) {
			changed++
		}
	}
	if changed == 0 {
		return "No files changed"
	}
	return plural(changed, "file") + " changed"
}

// ------------------------------------------------------------ until:

// untilSections retitle since:'s groups for until:, whose windows reach
// back from a time instead of up to now.
var untilSections = map[string]string{
	"The last few hours": "Hours ago",
	"Longer windows":     "Longer ago",
}

// untilCandidates offers since:'s windows for until:, each saying when it
// ends and how many files haven't changed since: "More than 2 weeks ago |
// Before Sat, Sep 19 | 8 files not changed since".
func untilCandidates(fragment string, files []FileStat, now time.Time) []valueCandidate {
	picks := windowPicks(fragment)
	candidates := make([]valueCandidate, len(picks))
	for i, p := range picks {
		end := until(now, p.value)
		section := p.section
		if retitled, ok := untilSections[section]; ok {
			section = retitled
		}
		candidates[i] = valueCandidate{
			value:   p.value,
			detail:  describeUntil(p.value),
			context: describeEnd(end, now),
			note:    unchangedFiles(files, end),
			section: section,
			group:   "value",
		}
	}
	return candidates
}

// describeUntil says what an until: value keeps: "Through today", "More
// than 2 hours ago", "More than a year ago".
func describeUntil(value string) string {
	switch strings.ToLower(value) {
	case windowToday:
		return "Through today"
	case windowYesterday:
		return "Through yesterday"
	}
	parts := durationPattern.FindStringSubmatch(value)
	names := windowUnitNames[parts[2]]
	switch {
	case parts[1] != "1":
		return "More than " + parts[1] + " " + names[1] + " ago"
	case parts[2] == "h":
		return "More than an hour ago"
	default:
		return "More than a " + names[0] + " ago"
	}
}

// describeEnd says when an until: window ends, like describeStart:
// "Before tomorrow", "Before today", "Before 08:00", "Before Sat, Sep 19".
func describeEnd(end, now time.Time) string {
	end = end.In(now.Location())
	today := midnight(now)
	switch {
	case end.Equal(today.AddDate(0, 0, 1)):
		return "Before tomorrow"
	case end.Equal(today):
		return "Before today"
	default:
		return "Before" + strings.TrimPrefix(describeStart(end, now), "Since")
	}
}

// unchangedFiles counts the indexed files last modified before end: "8
// files not changed since".
func unchangedFiles(files []FileStat, end time.Time) string {
	unchanged := 0
	for _, file := range files {
		if file.ModTime.Before(end) {
			unchanged++
		}
	}
	if unchanged == 0 {
		return "Every file changed since"
	}
	return plural(unchanged, "file") + " not changed since"
}

// ------------------------------------------------------------ is:

// maxStateNames is how many file names a file state's suggestion lists.
const maxStateNames = 3

// stateCandidates offers the file states of is:, each with how many files
// are in it now and, for open and changed, which ones: "client.py, retry.py".
func stateCandidates(fragment string, resolver Resolver) []valueCandidate {
	var open, changed, tests []string
	for _, file := range resolver.Files() {
		if file.Open {
			open = append(open, file.Path)
		}
		if file.Changed {
			changed = append(changed, file.Path)
		}
		if lang.IsTest(file.Path) {
			tests = append(tests, file.Path)
		}
	}
	changedNote := plural(len(changed), "file")
	if !slices.ContainsFunc(resolver.Repos(), func(repo RepoStat) bool { return repo.Git }) {
		changedNote = "No Git repo"
	}
	all := []valueCandidate{
		{value: StateOpen, detail: "Files open in the editor", context: fileNames(open, "None open"), note: plural(len(open), "file")},
		{value: StateChanged, detail: "Files with uncommitted changes", context: fileNames(changed, "Edited, added or staged since the last commit"), note: changedNote},
		{value: StateTest, detail: "Test files", context: "test_*.py, *_test.go, *.spec.ts, tests/ folders", note: plural(len(tests), "file")},
	}
	var candidates []valueCandidate
	for _, candidate := range all {
		if strings.HasPrefix(candidate.value, fragment) {
			candidate.group = "value"
			candidates = append(candidates, candidate)
		}
	}
	return candidates
}

// fileNames lists the base names of up to maxStateNames paths, sorted, and
// how many more there are: "client.py, retry.py +2 more"; none when empty.
func fileNames(paths []string, none string) string {
	if len(paths) == 0 {
		return none
	}
	names := make([]string, len(paths))
	for i, p := range paths {
		names[i] = path.Base(p)
	}
	slices.Sort(names)
	shown := strings.Join(names[:min(len(names), maxStateNames)], ", ")
	if extra := len(names) - maxStateNames; extra > 0 {
		shown += fmt.Sprintf(" +%d more", extra)
	}
	return shown
}

// ------------------------------------------------------------ type:, case:, word:, count:, order:

// fixedValue is one value of an operator whose values never change.
type fixedValue struct{ value, detail, context string }

// fixedValues describes the values of type:, kind:, case:, word:, count: and order:.
var fixedValues = map[protocol.OpName][]fixedValue{
	protocol.OpNameType: {
		{"file", "File names only", "Paths that match, no code lines"},
		{"code", "Code lines only", "Matching lines, without the file-name section"},
		{"commit", "Commits only", "Searches history: messages and diffs"},
		{TypeAdded, "Lines commits added", "Who introduced it: only the + lines of diffs"},
		{TypeRemoved, "Lines commits removed", "When it went away: only the - lines of diffs"},
	},
	protocol.OpNameKind: {
		{protocol.SymbolKindFunction, "Functions", "Top-level functions, and functions assigned to a name"},
		{protocol.SymbolKindMethod, "Methods", "Functions inside a class, struct or interface"},
		{protocol.SymbolKindClass, "Classes", "Classes, and structs, enums and objects in languages that call them classes"},
		{protocol.SymbolKindInterface, "Interfaces", "Interfaces, traits and protocols"},
		{protocol.SymbolKindType, "Types", "Type aliases, structs, enums and unions"},
		{protocol.SymbolKindOther, "Other definitions", "Ruby modules"},
	},
	protocol.OpNameCase: {
		{"yes", "Match case exactly", "RetryPolicy, not retrypolicy"},
		{"no", "Ignore case", "RetryPolicy, retrypolicy and RETRYPOLICY"},
		{"smart", "Match case only when the query has a capital", "RetryPolicy matches case; retrypolicy ignores it"},
	},
	protocol.OpNameWord: {
		{"yes", "Whole words only", "retry, not retryCount or autoretry"},
		{"no", "Parts of words too", "retry, retryCount and autoretry"},
	},
	protocol.OpNameCount: {
		{"50", "50 results per page", "The quickest first page"},
		{"200", "200 results per page", "A longer first page"},
		{"all", "Every result", "No Load more; can be slow on a large workspace"},
	},
	protocol.OpNameOrder: {
		{"best", "Best match first", "Definitions and file-name matches first; tests, vendored and generated files last"},
		{"path", "Path order", "Repo by repo, then by path, then by line"},
	},
}

// fixedValueNames lists the values of a fixed-value operator, in order;
// the operator table checks values against it.
func fixedValueNames(name protocol.OpName) []string {
	names := make([]string, len(fixedValues[name]))
	for i, v := range fixedValues[name] {
		names[i] = v.value
	}
	return names
}

func fixedCandidates(values []fixedValue, fragment string) []valueCandidate {
	var candidates []valueCandidate
	for _, v := range values {
		if strings.HasPrefix(v.value, fragment) {
			candidates = append(candidates, valueCandidate{value: v.value, detail: v.detail, context: v.context, group: "value"})
		}
	}
	return candidates
}
