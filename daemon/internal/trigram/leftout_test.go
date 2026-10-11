package trigram

import (
	"reflect"
	"testing"
)

func TestAWordLeftOutLeavesOutTheFilesWhoseTextHasIt(t *testing.T) {
	repo := repoOf("r", map[string]string{
		"daemon/main.go":      "var inputGrace = 2\n",
		"daemon/main_test.go": "func TestGrace() { _ = inputGrace }\n",
		"daemon/doc.go":       "// Package main serves.\n",
	})
	for query, want := range map[string][]string{
		"NOT inputGrace file:daemon/":          {"file daemon/doc.go"},
		"-inputGrace file:daemon/":             {"file daemon/doc.go"},
		"-content:inputGrace file:daemon/":     {"file daemon/doc.go"},
		"-(inputGrace OR serves) file:daemon/": {},
		"main -inputGrace type:file":           {},
		"main type:file":                       {"file daemon/main.go", "file daemon/main_test.go"},
	} {
		got, _ := run(t, query, repo)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s = %q, want %q", query, got, want)
		}
	}
}

func TestAFileLeftOutForItsTextIsCountedForTheNot(t *testing.T) {
	repo := repoOf("r", map[string]string{"daemon/main.go": "var inputGrace = 2\n", "daemon/doc.go": "// main\n"})
	_, stats := run(t, "-inputGrace file:daemon/ type:file", repo)
	if len(stats.Hidden) != 1 || stats.Hidden[0].Reason != "not" || stats.Hidden[0].Count != 1 {
		t.Errorf("hidden = %+v, want 1 file hidden by -inputGrace", stats.Hidden)
	}
}
