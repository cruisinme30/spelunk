package lang

import "testing"

// @covers op:lang
func TestDetectAndResolve(t *testing.T) {
	cases := []struct{ path, head, want string }{
		{"src/a.py", "", "python"},
		{"web/App.TSX", "", "typescript"},
		{"Makefile", "", "make"},
		{"bin/run", "#!/usr/bin/env python3\nprint()", "python"},
		{"bin/tool", "#!/bin/bash -e\n", "shell"},
		{"bin/x", "#!/usr/bin/env -S node --flag\n", "javascript"},
		{"notes", "hello", ""},
		{"lib/x.hpp", "", "c++"},
	}
	for _, c := range cases {
		if got := Detect(c.path, []byte(c.head)); got != c.want {
			t.Errorf("Detect(%q) = %q, want %q", c.path, got, c.want)
		}
	}
	for v, want := range map[string]string{"py": "python", "Python": "python", "ts": "typescript", "cpp": "c++", "golang": "go"} {
		if got, ok := Resolve(v); !ok || got != want {
			t.Errorf("Resolve(%q) = %q, %v", v, got, ok)
		}
	}
	if _, ok := Resolve("cobol"); ok {
		t.Error("cobol should be unknown")
	}
}
