package trigram

// Finding definitions by name for the editor: VS Code's Go to Symbol in
// Workspace types a few letters and wants the best definitions back while
// the user is still typing, so this scans only the names the index already
// keeps, never file text. Linux's 943,000 definitions take about 20 ms.

import (
	"context"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/cruisinme30/spelunk/daemon/internal/excludes"
	"github.com/cruisinme30/spelunk/daemon/internal/lang"
	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
	"github.com/cruisinme30/spelunk/daemon/internal/symbols"
)

// SymbolQuery is what FindSymbols looks for.
type SymbolQuery struct {
	// Text is what was typed. Unless Exact, its letters must appear in a
	// name in order, ignoring case; spaces don't count.
	Text string
	// Exact matches the whole name, case and all.
	Exact bool
	// Limit is the most definitions returned, best first.
	Limit int
	// Kinds say which files are generated and vendored; IncludeKinds keeps
	// their definitions, marked, instead of leaving them out.
	Kinds        *lang.FileKinds
	IncludeKinds bool
	// Excludes are VS Code's excludes, whose files are left out.
	Excludes *excludes.Set
	// Language, when set, keeps only the definitions in files of that
	// language, if any of those match.
	Language string
}

// FoundSymbol is one definition FindSymbols found.
type FoundSymbol struct {
	Repo   *Repo
	Doc    *Doc
	Symbol *symbols.Symbol
	// Mark is "generated" or "vendored" for a definition in such a file.
	Mark string
	// rank orders the results: lower is better (see matchRank).
	rank int
}

// Column returns the 1-based UTF-16 column where the definition's name
// starts on its line, as VS Code counts, or 1 when the line doesn't hold it.
func (f FoundSymbol) Column() int {
	text, _ := lineOf(f.Doc.Content, f.Symbol.Line)
	at := strings.Index(text, f.Symbol.Name)
	if at < 0 {
		return 1
	}
	return utf16Len(text[:at]) + 1
}

// checkEvery is how many files FindSymbols reads between looks at ctx.
const checkEvery = 4096

// FindSymbols returns the current definitions in repos that match q, best
// first: the exact name, then names that start with the text, then names
// that hold it, then names that hold its letters in order; within each,
// generated and vendored files last, shorter names first, then by repo,
// path and line. It stops early, with what it
// has, once ctx is done.
func FindSymbols(ctx context.Context, repos []Repo, q SymbolQuery) []FoundSymbol {
	needle := strings.Join(strings.Fields(q.Text), "")
	if needle == "" || q.Limit <= 0 {
		return nil
	}
	f := finder{q: q, needle: needle, folded: foldName(needle)}
	scanned := 0
	for r := range repos {
		repo := &repos[r]
		for _, shard := range repo.shards() {
			for d := range shard.Docs {
				scanned++
				if scanned%checkEvery == 0 && ctx.Err() != nil {
					return f.finish()
				}
				if doc := &shard.Docs[d]; len(doc.Symbols) > 0 && repo.current(shard, doc) {
					f.scan(repo, doc)
				}
			}
		}
	}
	return f.finish()
}

// finder collects what FindSymbols finds: in found, or, when the query
// names a language, the definitions in that language in sameLanguage.
type finder struct {
	q                   SymbolQuery
	needle, folded      string
	found, sameLanguage []FoundSymbol
}

// scan adds the definitions in doc that match.
func (f *finder) scan(repo *Repo, doc *Doc) {
	mark, keep := f.q.fileMark(repo, doc)
	if !keep {
		return
	}
	for s := range doc.Symbols {
		symbol := &doc.Symbols[s]
		rank, ok := matchRank(symbol.Name, f.needle, f.folded, f.q.Exact)
		if !ok {
			continue
		}
		hit := FoundSymbol{Repo: repo, Doc: doc, Symbol: symbol, Mark: mark, rank: rank}
		if f.q.Language != "" && doc.Lang == f.q.Language {
			f.sameLanguage = keepBest(append(f.sameLanguage, hit), f.q.Limit)
		} else {
			f.found = keepBest(append(f.found, hit), f.q.Limit)
		}
	}
}

// finish puts the definitions in the query's language first, alone when
// there are any, and returns the best of them, best first.
func (f *finder) finish() []FoundSymbol {
	found := f.found
	if len(f.sameLanguage) > 0 {
		found = f.sameLanguage
	}
	sortFound(found)
	if len(found) > f.q.Limit {
		found = found[:f.q.Limit]
	}
	return found
}

// fileMark says whether a file's definitions are found, and how they are
// marked: VS Code's excludes hide a file, and generated and vendored files
// are left out unless the query includes them.
func (q SymbolQuery) fileMark(repo *Repo, doc *Doc) (mark string, keep bool) {
	if q.Excludes.Hiding(repo.ID, repo.Root, doc.Path) != "" {
		return "", false
	}
	if q.Kinds == nil {
		return "", true
	}
	switch {
	case q.Kinds.IsVendored(doc.Path):
		mark = protocol.RankReasonVendored
	case q.Kinds.IsGenerated(doc.Path, doc.Content):
		mark = protocol.RankReasonGenerated
	}
	return mark, mark == "" || q.IncludeKinds
}

// keepBest trims found to its limit best once it holds four times as many,
// so a one-letter query over a huge repo keeps memory bounded.
func keepBest(found []FoundSymbol, limit int) []FoundSymbol {
	if len(found) < 4*limit {
		return found
	}
	sortFound(found)
	return found[:limit]
}

func sortFound(found []FoundSymbol) {
	slices.SortFunc(found, func(a, b FoundSymbol) int {
		if a.rank != b.rank {
			return a.rank - b.rank
		}
		if (a.Mark == "") != (b.Mark == "") {
			if a.Mark == "" {
				return -1
			}
			return 1
		}
		if n := len(a.Symbol.Name) - len(b.Symbol.Name); n != 0 {
			return n
		}
		if c := strings.Compare(a.Repo.Name, b.Repo.Name); c != 0 {
			return c
		}
		if c := strings.Compare(a.Doc.Path, b.Doc.Path); c != 0 {
			return c
		}
		return a.Symbol.Line - b.Symbol.Line
	})
}

// The ranks matchRank gives, best first.
const (
	rankExact = iota
	rankPrefix
	rankContains
	rankLetters
)

// matchRank says whether name matches the needle (folded is foldName of it)
// and how well. Exact matching wants the whole name, case and all.
func matchRank(name, needle, folded string, exact bool) (int, bool) {
	if exact {
		return rankExact, name == needle
	}
	if !lettersInOrder(name, folded) {
		return 0, false
	}
	lower := foldName(name)
	switch {
	case lower == folded:
		return rankExact, true
	case strings.HasPrefix(lower, folded):
		return rankPrefix, true
	case strings.Contains(lower, folded):
		return rankContains, true
	default:
		return rankLetters, true
	}
}

// lettersInOrder reports whether the characters of folded appear in name in
// order, ignoring case. It allocates nothing for ASCII names, which is
// nearly all of them, since it runs on every definition in the index.
func lettersInOrder(name, folded string) bool {
	i := 0
	for j := 0; j < len(name) && i < len(folded); {
		c, size := rune(name[j]), 1
		if c >= utf8.RuneSelf {
			c, size = utf8.DecodeRuneInString(name[j:])
		}
		j += size
		want, wantSize := rune(folded[i]), 1
		if want >= utf8.RuneSelf {
			want, wantSize = utf8.DecodeRuneInString(folded[i:])
		}
		if foldRune(c) == want {
			i += wantSize
		}
	}
	return i == len(folded)
}

// foldName lowercases a name for matching that ignores case.
func foldName(s string) string {
	return strings.Map(foldRune, s)
}

func foldRune(r rune) rune {
	if r < utf8.RuneSelf {
		if 'A' <= r && r <= 'Z' {
			return r + ('a' - 'A')
		}
		return r
	}
	return unicode.ToLower(r)
}
