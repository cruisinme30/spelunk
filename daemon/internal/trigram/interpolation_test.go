package trigram

import (
	"context"
	"strings"
	"testing"
)

func TestStringReplacementPreservesExecutableInterpolation(t *testing.T) {
	// @covers op:in rpc:replace/plan
	for _, tt := range []struct{ path, source, want string }{
		{"x.js", "const message = `dangerCall ${dangerCall()} dangerCall`;", "const message = `safe ${dangerCall()} safe`;"},
		{"x.py", `message = f"dangerCall {dangerCall()} dangerCall"`, `message = f"safe {dangerCall()} safe"`},
	} {
		t.Run(tt.path, func(t *testing.T) {
			repo := repoOf("r", map[string]string{tt.path: tt.source})
			plan := mustPlan(t, "dangerCall in:strings", defaultSettings, "")
			out, err := Replacements(context.Background(), plan, []Repo{repo}, plan.Terms[0], "safe", 100)
			if err != nil {
				t.Fatal(err)
			}
			if out.Matches != 2 || len(out.Files) != 1 || len(out.Files[0].Lines) != 1 {
				t.Fatalf("Replacements(%q) = %+v, want two literal edits", tt.source, out)
			}
			line := out.Files[0].Lines[0]
			got := line.Text
			for i := len(line.Edits) - 1; i >= 0; i-- {
				edit := line.Edits[i]
				got = got[:edit.Start] + edit.NewText + got[edit.End:]
			}
			if got != tt.want {
				t.Errorf("planned replacement = %q, want %q", got, tt.want)
			}
			items, _ := run(t, "dangerCall in:code", repo)
			if len(items) != 1 {
				t.Errorf("code query = %q, want expression result", items)
			}
		})
	}
}

func TestNestedRustCommentDoesNotLeakIntoCode(t *testing.T) {
	// @covers op:in
	text := "/* outer /* inner */ dangerCall(); */\ndangerCall();\n"
	repo := repoOf("r", map[string]string{"x.rs": text})
	items, _ := run(t, "dangerCall in:code", repo)
	if len(items) != 1 || !strings.HasPrefix(items[0], "x.rs:2 ") {
		t.Errorf("code query = %q, want only line 2", items)
	}
	comments, _ := run(t, "dangerCall in:comments", repo)
	if len(comments) != 1 || !strings.HasPrefix(comments[0], "x.rs:1 ") {
		t.Errorf("comment query = %q, want only line 1", comments)
	}
}
