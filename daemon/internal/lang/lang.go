// Package lang maps file names and shebangs to languages, and lang:
// values (names or aliases) to canonical language names.
package lang

import (
	"path"
	"sort"
	"strings"
)

// Language is one entry in the table.
type Language struct {
	Name       string   // canonical, lowercase: the value shown in completions
	Aliases    []string // other accepted lang: values
	Extensions []string // with the dot, lowercase
	Filenames  []string // exact base names (Makefile, Dockerfile)
	Shebangs   []string // interpreter names found after #! (python3, node)
}

var table = []Language{
	{Name: "python", Aliases: []string{"py"}, Extensions: []string{".py", ".pyi", ".pyw"}, Shebangs: []string{"python", "python3", "python2"}},
	{Name: "typescript", Aliases: []string{"ts", "tsx"}, Extensions: []string{".ts", ".tsx", ".mts", ".cts"}, Shebangs: []string{"ts-node", "deno"}},
	{Name: "javascript", Aliases: []string{"js", "jsx"}, Extensions: []string{".js", ".jsx", ".mjs", ".cjs"}, Shebangs: []string{"node"}},
	{Name: "go", Aliases: []string{"golang"}, Extensions: []string{".go"}},
	{Name: "c++", Aliases: []string{"cpp", "cxx", "cc"}, Extensions: []string{".cc", ".cpp", ".cxx", ".c++", ".hh", ".hpp", ".hxx", ".h++", ".ipp", ".tpp"}},
	{Name: "c", Extensions: []string{".c", ".h"}},
	{Name: "rust", Aliases: []string{"rs"}, Extensions: []string{".rs"}},
	{Name: "java", Extensions: []string{".java"}},
	{Name: "kotlin", Aliases: []string{"kt"}, Extensions: []string{".kt", ".kts"}},
	{Name: "csharp", Aliases: []string{"c#", "cs"}, Extensions: []string{".cs"}},
	{Name: "ruby", Aliases: []string{"rb"}, Extensions: []string{".rb"}, Filenames: []string{"Gemfile", "Rakefile"}, Shebangs: []string{"ruby"}},
	{Name: "php", Extensions: []string{".php"}, Shebangs: []string{"php"}},
	{Name: "swift", Extensions: []string{".swift"}},
	{Name: "scala", Extensions: []string{".scala", ".sc"}},
	{Name: "shell", Aliases: []string{"sh", "bash", "zsh"}, Extensions: []string{".sh", ".bash", ".zsh"}, Shebangs: []string{"sh", "bash", "zsh"}},
	{Name: "sql", Extensions: []string{".sql"}},
	{Name: "html", Extensions: []string{".html", ".htm"}},
	{Name: "css", Aliases: []string{"scss", "less"}, Extensions: []string{".css", ".scss", ".less"}},
	{Name: "json", Extensions: []string{".json", ".jsonc"}},
	{Name: "yaml", Aliases: []string{"yml"}, Extensions: []string{".yaml", ".yml"}},
	{Name: "toml", Extensions: []string{".toml"}},
	{Name: "markdown", Aliases: []string{"md"}, Extensions: []string{".md", ".markdown"}},
	{Name: "protobuf", Aliases: []string{"proto"}, Extensions: []string{".proto"}},
	{Name: "make", Aliases: []string{"makefile"}, Extensions: []string{".mk"}, Filenames: []string{"Makefile", "GNUmakefile"}},
	{Name: "docker", Aliases: []string{"dockerfile"}, Filenames: []string{"Dockerfile"}},
	{Name: "cmake", Extensions: []string{".cmake"}, Filenames: []string{"CMakeLists.txt"}},
	{Name: "lua", Extensions: []string{".lua"}, Shebangs: []string{"lua"}},
	{Name: "perl", Aliases: []string{"pl"}, Extensions: []string{".pl", ".pm"}, Shebangs: []string{"perl"}},
	{Name: "r", Extensions: []string{".r"}},
	{Name: "dart", Extensions: []string{".dart"}},
	{Name: "elixir", Aliases: []string{"ex"}, Extensions: []string{".ex", ".exs"}},
	{Name: "haskell", Aliases: []string{"hs"}, Extensions: []string{".hs"}},
	{Name: "text", Aliases: []string{"txt"}, Extensions: []string{".txt"}},
}

var (
	byExt      = map[string]string{}
	byFilename = map[string]string{}
	byShebang  = map[string]string{}
	byValue    = map[string]string{}
)

func init() {
	for _, l := range table {
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

// Names lists canonical names, sorted.
func Names() []string {
	out := make([]string, 0, len(table))
	for _, l := range table {
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

// Detect returns the language of a file from its name, falling back to
// the shebang on its first line. "" means unknown.
func Detect(p string, head []byte) string {
	base := path.Base(p)
	if l, ok := byFilename[base]; ok {
		return l
	}
	if l, ok := byExt[strings.ToLower(path.Ext(base))]; ok {
		return l
	}
	if len(head) > 2 && head[0] == '#' && head[1] == '!' {
		line := string(head[2:])
		if i := strings.IndexByte(line, '\n'); i >= 0 {
			line = line[:i]
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			return ""
		}
		interp := path.Base(fields[0])
		if interp == "env" {
			for _, f := range fields[1:] {
				if !strings.HasPrefix(f, "-") {
					interp = f
					break
				}
			}
		}
		if l, ok := byShebang[interp]; ok {
			return l
		}
		if l, ok := byShebang[strings.TrimRight(interp, "0123456789.")]; ok {
			return l
		}
	}
	return ""
}
