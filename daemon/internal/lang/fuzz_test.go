package lang

import (
	"slices"
	"testing"
)

// FuzzDetect checks that Detect never panics and answers "" or a canonical name.
func FuzzDetect(f *testing.F) {
	seeds := []struct{ path, head string }{
		{"src/a.py", ""},
		{"Makefile", ""},
		{"bin/run", "#!/usr/bin/env python3\nprint()"},
		{"bin/run", "#!/usr/bin/env -S LANG=C node --flag\r\n"},
		{"", "#!"},
		{".", "#!/usr/bin/env -"},
		{"a/b/", "#!\xff\xfe\x00"},
		{"x.PY", "#! /bin/sh"},
		{"😀.rs", "#!/usr/bin/python3.12.1"},
	}
	for _, seed := range seeds {
		f.Add(seed.path, []byte(seed.head))
	}
	names := Names()
	f.Fuzz(func(t *testing.T, filePath string, head []byte) {
		if got := Detect(filePath, head); got != "" && !slices.Contains(names, got) {
			t.Fatalf("Detect(%q, %q) = %q, not a canonical name", filePath, head, got)
		}
	})
}

func TestEveryNameAndAliasResolvesToItsLanguage(t *testing.T) {
	for _, value := range Values() {
		name, ok := Resolve(value)
		if !ok || !slices.Contains(Names(), name) {
			t.Errorf("Resolve(%q) = %q, %v; want a canonical name", value, name, ok)
		}
	}
	for _, name := range Names() {
		if got, ok := Resolve(name); !ok || got != name {
			t.Errorf("Resolve(%q) = %q, %v; want itself", name, got, ok)
		}
		if got, ok := Resolve(Title(name)); ok && got != name {
			t.Errorf("Resolve(Title(%q)) = %q, want %q", name, got, name)
		}
	}
}
