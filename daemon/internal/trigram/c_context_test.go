package trigram

import (
	"context"
	"strings"
	"testing"
)

func TestCContextReplacementPreservesExecutableCode(t *testing.T) {
	// @covers op:in rpc:replace/plan
	for _, tt := range []struct {
		name, path, source, query, want string
		matches                         int
	}{
		{
			"spliced block closer", "x.c", "/* dangerCall *\\\n/ dangerCall(); /* dangerCall */\n", "dangerCall in:comments",
			"/* safe *\\\n/ dangerCall(); /* safe */\n", 2,
		},
		{
			"continued line comment", "x.c", "// dangerCall \\\ndangerCall\ndangerCall();\n", "dangerCall in:comments",
			"// safe \\\nsafe\ndangerCall();\n", 2,
		},
		{
			"string escape after splice", "x.c", "const char *s = \"dangerCall\\\\\n\"dangerCall\"; dangerCall();", "dangerCall in:strings",
			"const char *s = \"safe\\\\\n\"safe\"; dangerCall();", 2,
		},
		{
			"split raw prefix", "x.cpp", "auto s = u\\\n8R\"tag(dangerCall /* dangerCall */)tag\"; dangerCall();", "dangerCall in:strings",
			"auto s = u\\\n8R\"tag(safe /* safe */)tag\"; dangerCall();", 2,
		},
		{
			"numeric separators", "x.cpp", "int n = 1'000'000; const char *s = \"000\";", "000 in:strings",
			"int n = 1'000'000; const char *s = \"safe\";", 1,
		},
		{
			"split identifier", "x.cpp", "#define abR\nconst char *s = ab\\\nR\"(dangerCall\"; dangerCall(); const char *t = \"dangerCall)\";", "dangerCall in:strings",
			"#define abR\nconst char *s = ab\\\nR\"(safe\"; dangerCall(); const char *t = \"safe)\";", 2,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			repo := repoOf("r", map[string]string{tt.path: tt.source})
			plan := mustPlan(t, tt.query, defaultSettings, "")
			out, err := Replacements(context.Background(), plan, []Repo{repo}, plan.Terms[0], "safe", 100)
			if err != nil || out.Matches != tt.matches || len(out.Files) != 1 {
				t.Fatalf("Replacements(%q) = %+v, %v, want %d literal edits", tt.source, out, err, tt.matches)
			}
			lines := strings.Split(tt.source, "\n")
			for _, line := range out.Files[0].Lines {
				got := line.Text
				for i := len(line.Edits) - 1; i >= 0; i-- {
					edit := line.Edits[i]
					got = got[:edit.Start] + edit.NewText + got[edit.End:]
				}
				lines[line.Line-1] = got
			}
			if got := strings.Join(lines, "\n"); got != tt.want {
				t.Errorf("planned replacement = %q, want %q", got, tt.want)
			}
		})
	}
}
