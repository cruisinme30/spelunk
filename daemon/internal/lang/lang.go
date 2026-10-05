package lang

import (
	"bytes"
	"path"
	"sort"
	"strings"
)

// language is one entry in the table.
type language struct {
	Name       string   // canonical, lowercase: the value shown in completions
	Title      string   // how people write it: "TypeScript", "C#"
	Aliases    []string // other accepted lang: values
	Extensions []string // with the dot, lowercase
	Filenames  []string // exact base names (Makefile, Dockerfile)
	Shebangs   []string // interpreter names found after #! (python3, node)
}

var table = []language{
	{Name: "python", Title: "Python", Aliases: []string{"py"}, Extensions: []string{".py", ".pyi", ".pyw"}, Shebangs: []string{"python", "python3", "python2"}},
	{Name: "typescript", Title: "TypeScript", Aliases: []string{"ts", "tsx"}, Extensions: []string{".ts", ".tsx", ".mts", ".cts"}, Shebangs: []string{"ts-node", "deno"}},
	{Name: "javascript", Title: "JavaScript", Aliases: []string{"js", "jsx"}, Extensions: []string{".js", ".jsx", ".mjs", ".cjs"}, Shebangs: []string{"node"}},
	{Name: "go", Title: "Go", Aliases: []string{"golang"}, Extensions: []string{".go"}},
	{Name: "c++", Title: "C++", Aliases: []string{"cpp", "cxx", "cc"}, Extensions: []string{".cc", ".cpp", ".cxx", ".c++", ".hh", ".hpp", ".hxx", ".h++", ".ipp", ".tpp"}},
	{Name: "c", Title: "C", Extensions: []string{".c", ".h"}},
	{Name: "rust", Title: "Rust", Aliases: []string{"rs"}, Extensions: []string{".rs"}},
	{Name: "java", Title: "Java", Extensions: []string{".java"}},
	{Name: "kotlin", Title: "Kotlin", Aliases: []string{"kt"}, Extensions: []string{".kt", ".kts"}},
	{Name: "csharp", Title: "C#", Aliases: []string{"c#", "cs"}, Extensions: []string{".cs"}},
	{Name: "ruby", Title: "Ruby", Aliases: []string{"rb"}, Extensions: []string{".rb"}, Filenames: []string{"Gemfile", "Rakefile"}, Shebangs: []string{"ruby"}},
	{Name: "php", Title: "PHP", Extensions: []string{".php"}, Shebangs: []string{"php"}},
	{Name: "swift", Title: "Swift", Extensions: []string{".swift"}},
	{Name: "scala", Title: "Scala", Extensions: []string{".scala", ".sc"}},
	{Name: "shell", Title: "Shell", Aliases: []string{"sh", "bash", "zsh"}, Extensions: []string{".sh", ".bash", ".zsh"}, Shebangs: []string{"sh", "bash", "zsh"}},
	{Name: "sql", Title: "SQL", Extensions: []string{".sql"}},
	{Name: "html", Title: "HTML", Extensions: []string{".html", ".htm"}},
	{Name: "css", Title: "CSS", Aliases: []string{"scss", "less"}, Extensions: []string{".css", ".scss", ".less"}},
	{Name: "json", Title: "JSON", Extensions: []string{".json", ".jsonc"}},
	{Name: "yaml", Title: "YAML", Aliases: []string{"yml"}, Extensions: []string{".yaml", ".yml"}},
	{Name: "toml", Title: "TOML", Extensions: []string{".toml"}},
	{Name: "markdown", Title: "Markdown", Aliases: []string{"md"}, Extensions: []string{".md", ".markdown"}},
	{Name: "protobuf", Title: "Protocol Buffers", Aliases: []string{"proto"}, Extensions: []string{".proto"}},
	{Name: "make", Title: "Makefile", Aliases: []string{"makefile"}, Extensions: []string{".mk"}, Filenames: []string{"Makefile", "GNUmakefile"}},
	{Name: "docker", Title: "Dockerfile", Aliases: []string{"dockerfile"}, Filenames: []string{"Dockerfile"}},
	{Name: "cmake", Title: "CMake", Extensions: []string{".cmake"}, Filenames: []string{"CMakeLists.txt"}},
	{Name: "lua", Title: "Lua", Extensions: []string{".lua"}, Shebangs: []string{"lua"}},
	{Name: "perl", Title: "Perl", Aliases: []string{"pl"}, Extensions: []string{".pl", ".pm"}, Shebangs: []string{"perl"}},
	{Name: "r", Title: "R", Extensions: []string{".r"}},
	{Name: "dart", Title: "Dart", Extensions: []string{".dart"}},
	{Name: "elixir", Title: "Elixir", Aliases: []string{"ex"}, Extensions: []string{".ex", ".exs"}},
	{Name: "haskell", Title: "Haskell", Aliases: []string{"hs"}, Extensions: []string{".hs"}},
	{Name: "text", Title: "Plain text", Aliases: []string{"txt"}, Extensions: []string{".txt"}},
}

var (
	byExt      = map[string]string{}
	byFilename = map[string]string{}
	byShebang  = map[string]string{}
	byValue    = map[string]string{}
)

func init() {
	for i := range table {
		l := &table[i]
		byValue[l.Name] = l.Name
		for _, a := range l.Aliases {
			byValue[a] = l.Name
		}
		for _, e := range l.Extensions {
			byExt[e] = l.Name
		}
		for _, f := range l.Filenames {
			byFilename[f] = l.Name
		}
		for _, s := range l.Shebangs {
			byShebang[s] = l.Name
		}
	}
}

// Resolve maps a lang: value (case-insensitive name or alias) to its
// canonical name.
func Resolve(value string) (string, bool) {
	n, ok := byValue[strings.ToLower(value)]
	return n, ok
}

// Title returns how people write a language's canonical name ("TypeScript"
// for typescript), or name itself when it isn't one.
func Title(name string) string {
	for i := range table {
		l := &table[i]
		if l.Name == name {
			return l.Title
		}
	}
	return name
}

// Names lists canonical names, sorted.
func Names() []string {
	out := make([]string, 0, len(table))
	for i := range table {
		l := &table[i]
		out = append(out, l.Name)
	}
	sort.Strings(out)
	return out
}

// Values lists every accepted lang: value (names and aliases), sorted.
func Values() []string {
	out := make([]string, 0, len(byValue))
	for v := range byValue {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

// Detect returns the language of the file at filePath (slash-separated)
// from its name, falling back to the shebang on the first line of head, the
// start of its content. "" means unknown.
func Detect(filePath string, head []byte) string {
	base := path.Base(filePath)
	if l, ok := byFilename[base]; ok {
		return l
	}
	if l, ok := byExt[strings.ToLower(path.Ext(base))]; ok {
		return l
	}
	interpreter := shebangInterpreter(head)
	if l, ok := byShebang[interpreter]; ok {
		return l
	}
	// python3.12 → python
	return byShebang[strings.TrimRight(interpreter, "0123456789.")]
}

// shebangInterpreter returns the program a "#!" line runs, looking through
// "/usr/bin/env [-flags] [NAME=value…]"; "" when the file has no shebang.
func shebangInterpreter(head []byte) string {
	line, ok := bytes.CutPrefix(head, []byte("#!"))
	if !ok {
		return ""
	}
	if end := bytes.IndexByte(line, '\n'); end >= 0 {
		line = line[:end]
	}
	fields := strings.Fields(string(line))
	if len(fields) == 0 {
		return ""
	}
	interpreter := path.Base(fields[0])
	if interpreter != "env" {
		return interpreter
	}
	for _, field := range fields[1:] {
		if !strings.HasPrefix(field, "-") && !strings.Contains(field, "=") {
			return field
		}
	}
	return ""
}
