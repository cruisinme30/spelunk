package symbols

import "github.com/cruisinme30/unified-search/daemon/internal/protocol"

// Short names for the kinds, to keep the rules readable.
const (
	class     = protocol.SymbolKindClass
	iface     = protocol.SymbolKindInterface
	function  = protocol.SymbolKindFunction
	method    = protocol.SymbolKindMethod
	typeKind  = protocol.SymbolKindType
	otherKind = protocol.SymbolKindOther
)

// Pieces the rules share.
const (
	ident   = `(?P<name>[A-Za-z_]\w*)`
	jsIdent = `(?P<name>[A-Za-z_$][\w$]*)`
	// modifiers are the words Java-like languages put before a definition.
	modifiers = `(?:(?:public|private|protected|internal|static|final|abstract|sealed|open|override|virtual|` +
		`partial|readonly|async|synchronized|native|data|inline|suspend|unsafe|extern|export|default|const)\s+)*`
)

// cFunction is a C or C++ function definition at the left margin: a return
// type, the name and the opening parenthesis, without a ";" after it (that
// would be a declaration).
const cFunction = `^[A-Za-z_][\w\s\*&:<>,]*?[\s\*&]` + `(?P<name>[A-Za-z_][\w:~]*)\s*\([^;]*$`

// byLanguage holds each language's rules, tried in order on every line.
var byLanguage = map[string][]rule{
	"python": {
		define(class, `^\s*class\s+`+ident),
		defineFunction(`^(?P<indent>\s*)(?:async\s+)?def\s+` + ident),
	},
	"go": {
		define(method, `^func\s+\([^)]*\)\s*`+ident),
		define(function, `^func\s+`+ident),
		define(iface, `^(?:type\s+|\s+)`+ident+`(?:\[[^\]]*\])?\s+interface\b`),
		define(typeKind, `^type\s+`+ident),
	},
	"typescript": jsRules,
	"javascript": jsRules,
	"java":       javaLikeRules,
	"csharp":     javaLikeRules,
	"kotlin": {
		define(iface, `^\s*`+modifiers+`(?:fun\s+)?interface\s+`+ident),
		define(class, `^\s*`+modifiers+`(?:enum\s+|annotation\s+)?(?:class|object)\s+`+ident),
		defineFunction(`^(?P<indent>\s*)` + modifiers + `fun\s+(?:<[^>]*>\s*)?(?:[\w.]+\.)?` + ident),
		define(typeKind, `^\s*typealias\s+`+ident),
	},
	"scala": {
		define(iface, `^\s*`+modifiers+`trait\s+`+ident),
		define(class, `^\s*`+modifiers+`(?:case\s+)?(?:class|object)\s+`+ident),
		defineFunction(`^(?P<indent>\s*)` + modifiers + `def\s+` + ident),
		define(typeKind, `^\s*type\s+`+ident),
	},
	"swift": {
		define(iface, `^\s*`+modifiers+`protocol\s+`+ident),
		define(class, `^\s*`+modifiers+`(?:final\s+)?(?:class|struct|enum|actor|extension)\s+`+ident),
		defineFunction(`^(?P<indent>\s*)` + modifiers + `func\s+` + ident),
		define(typeKind, `^\s*`+modifiers+`typealias\s+`+ident),
	},
	"rust": {
		define(iface, `^\s*(?:pub(?:\([^)]*\))?\s+)?(?:unsafe\s+)?trait\s+`+ident),
		define(typeKind, `^\s*(?:pub(?:\([^)]*\))?\s+)?(?:struct|enum|union|type)\s+`+ident),
		defineFunction(`^(?P<indent>\s*)(?:pub(?:\([^)]*\))?\s+)?(?:const\s+)?(?:async\s+)?(?:unsafe\s+)?` +
			`(?:extern\s+"[^"]*"\s+)?fn\s+` + ident),
	},
	"ruby": {
		define(class, `^\s*class\s+(?:[A-Z]\w*::)*(?P<name>[A-Z]\w*)`),
		define(otherKind, `^\s*module\s+(?:[A-Z]\w*::)*(?P<name>[A-Z]\w*)`),
		defineFunction(`^(?P<indent>\s*)def\s+(?:self\.)?(?P<name>[A-Za-z_]\w*[?!=]?)`),
	},
	"php": {
		define(iface, `^\s*interface\s+`+ident),
		define(class, `^\s*(?:(?:abstract|final|readonly)\s+)*(?:class|trait|enum)\s+`+ident),
		defineFunction(`^(?P<indent>\s*)(?:(?:public|private|protected|static|abstract|final)\s+)*function\s+&?` + ident),
	},
	"c": {
		define(typeKind, `^\s*(?:typedef\s+)?(?:struct|enum|union)\s+`+ident+`\s*\{`),
		define(typeKind, `^\s*}\s*`+ident+`\s*;`),
		define(function, cFunction),
	},
	"c++": {
		define(class, `^\s*(?:template\s*<[^>]*>\s*)?(?:class|struct)\s+(?:\w+\s+)?`+ident+`\s*(?:final\s*)?(?::[^;]*)?\{?\s*$`),
		define(typeKind, `^\s*(?:enum(?:\s+class)?|union)\s+`+ident+`\b[^;]*$`),
		define(typeKind, `^\s*using\s+`+ident+`\s*=`),
		define(function, cFunction),
	},
}

// jsRules serve JavaScript and TypeScript.
var jsRules = []rule{
	define(class, `^\s*(?:export\s+)?(?:default\s+)?(?:declare\s+)?(?:abstract\s+)?class\s+`+jsIdent),
	define(iface, `^\s*(?:export\s+)?(?:declare\s+)?interface\s+`+jsIdent),
	define(typeKind, `^\s*(?:export\s+)?(?:declare\s+)?type\s+`+jsIdent+`\s*(?:<[^=]*>)?\s*=`),
	define(typeKind, `^\s*(?:export\s+)?(?:declare\s+)?(?:const\s+)?enum\s+`+jsIdent),
	define(function, `^\s*(?:export\s+)?(?:default\s+)?(?:async\s+)?function\s*\*?\s*`+jsIdent),
	define(function, `^\s*(?:export\s+)?(?:const|let|var)\s+`+jsIdent+
		`\s*(?::[^=]+)?=\s*(?:async\s+)?(?:function\b|(?:\([^)]*\)|[A-Za-z_$][\w$]*)\s*(?::[^=]+)?=>)`),
	define(method, `^\s+(?:(?:public|private|protected|static|async|readonly|override|abstract|get|set)\s+)*\*?`+
		jsIdent+`\s*(?:<[^>]*>)?\([^)]*\)\s*(?::[^{]+)?\{\s*$`),
}

// javaLikeRules serve Java and C#.
var javaLikeRules = []rule{
	define(iface, `^\s*`+modifiers+`(?:@)?interface\s+`+ident),
	define(class, `^\s*`+modifiers+`(?:class|enum|record|struct)\s+`+ident),
	define(method, `^\s+`+modifiers+`(?:<[^>]*>\s*)?[\w.<>\[\],? ]+\s+`+ident+`\s*\([^;]*$`),
}
