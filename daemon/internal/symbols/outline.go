package symbols

import (
	"regexp"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
)

// Entry is one definition in a file's outline, and how deep it sits inside
// the classes, types and interfaces around it: 0 at the top.
type Entry struct {
	Symbol
	Depth int
}

// MaxOutline is the most definitions an outline lists; a file with more is
// generated or minified, not one to read by its outline.
const MaxOutline = 1_000

// implScope finds a Rust impl block, which defines nothing itself but holds
// the methods of the type it names: "impl Policy", "impl fmt::Display for
// Policy", "impl<T> Wait for Policy<T>".
var implScope = regexp.MustCompile(
	`^\s*(?:unsafe\s+)?impl(?:\s*<[^>]*>)?\s+(?:[\w:]+(?:<[^>]*>)?\s+for\s+)?(?:\w+::)*(?P<name>[A-Za-z_]\w*)`)

// Outline returns the definitions in content, a file in language lang, as
// a tree read top to bottom: each class, interface or module followed by
// the definitions inside it, one Depth deeper. Inside means indented under
// it until a line as far left as it ends it, as Python's indentation and
// the usual brace styles both show; a Go method goes under the type its
// receiver names, and a Rust method under the type its impl block names,
// when the file defines that type. Everything else keeps its line order.
func Outline(lang string, content []byte) []Entry {
	rules := byLanguage[lang]
	if len(rules) == 0 {
		return nil
	}
	var r outlineReader
	eachLine(content, func(number int, line []byte) { r.read(rules, lang == "rust", number, line) })
	return r.flatten()
}

// outlineReader reads a file's definitions a line at a time, and which
// definition each one is inside.
type outlineReader struct {
	found    []Symbol
	inIndex  []int    // each definition's parent, or -1
	inName   []string // or the parent's name, to find once every definition is known
	open     []scope  // the definitions the current line may be inside, innermost last
	squeezed []byte
}

// scope is a definition, or a Rust impl block, that the lines indented
// under it are inside.
type scope struct {
	indent int
	index  int    // the definition that opened it, or -1 for an impl block
	name   string // the type an impl block names
}

// read takes line number of the file; implBlocks says whether impl blocks hold
// methods (Rust).
func (r *outlineReader) read(rules []rule, implBlocks bool, number int, line []byte) {
	indent := indentOf(line)
	if indent == len(line) {
		return // a blank line ends nothing
	}
	for len(r.open) > 0 && r.open[len(r.open)-1].indent >= indent {
		r.open = r.open[:len(r.open)-1]
	}
	r.squeezed = squeezeSpace(r.squeezed[:0], line)
	symbol, receiver, ok := match(rules, r.squeezed)
	if !ok {
		if m := implScope.FindSubmatch(r.squeezed); implBlocks && m != nil {
			r.open = append(r.open, scope{indent: indent, index: -1, name: string(m[1])})
		}
		return
	}
	symbol.Line = number
	parentIndex, parentName := -1, receiver
	if receiver == "" && len(r.open) > 0 {
		top := r.open[len(r.open)-1]
		parentIndex, parentName = top.index, top.name
	}
	r.found = append(r.found, symbol)
	r.inIndex = append(r.inIndex, parentIndex)
	r.inName = append(r.inName, parentName)
	if holdsMembers(symbol.Kind) {
		r.open = append(r.open, scope{indent: indent, index: len(r.found) - 1})
	}
}

// flatten lists the definitions read as a tree, top to bottom, at most
// MaxOutline of them.
func (r *outlineReader) flatten() []Entry {
	children := make(map[int][]int, len(r.found))
	for i := range r.found {
		children[r.parentOf(i)] = append(children[r.parentOf(i)], i)
	}
	entries := make([]Entry, 0, min(len(r.found), MaxOutline))
	seen := make([]bool, len(r.found))
	var walk func(parent, depth int)
	walk = func(parent, depth int) {
		for _, i := range children[parent] {
			if seen[i] || len(entries) == MaxOutline {
				continue
			}
			seen[i] = true
			entries = append(entries, Entry{Symbol: r.found[i], Depth: depth})
			walk(i, depth+1)
		}
	}
	walk(-1, 0)
	// Types that name each other as parents reach no top-level definition;
	// list them at the top rather than lose them.
	for i := range r.found {
		if !seen[i] && len(entries) < MaxOutline {
			seen[i] = true
			entries = append(entries, Entry{Symbol: r.found[i]})
			walk(i, 1)
		}
	}
	return entries
}

// parentOf returns the index of the definition that found[i] is inside,
// or -1.
func (r *outlineReader) parentOf(i int) int {
	if r.inIndex[i] >= 0 || r.inName[i] == "" {
		return r.inIndex[i]
	}
	if parent := typeNamed(r.found, r.inName[i]); parent != i {
		return parent
	}
	return -1
}

// holdsMembers says whether definitions indented under one of kind are its
// members.
func holdsMembers(kind protocol.SymbolKind) bool {
	return kind == class || kind == iface || kind == otherKind
}

// typeNamed returns the index of the class, interface, type or module
// called name in found, or -1.
func typeNamed(found []Symbol, name string) int {
	for i, symbol := range found {
		if symbol.Name == name && symbol.Kind != function && symbol.Kind != method {
			return i
		}
	}
	return -1
}

// indentOf counts the whitespace bytes line starts with.
func indentOf(line []byte) int {
	for i, c := range line {
		if c != ' ' && c != '\t' && c != '\r' && c != '\f' {
			return i
		}
	}
	return len(line)
}
