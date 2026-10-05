package lang

import (
	"path"
	"slices"
	"strings"
)

// testDirs are folders whose files are all tests or test fixtures.
var testDirs = []string{"test", "tests", "__tests__", "spec", "specs", "e2e", "testdata"}

// testSuffixes end a test file's name before its extension, in the languages
// that name tests that way: client_test.go, client.test.ts, client.spec.js,
// client_spec.rb, ClientTest.java, ClientTests.swift.
var testSuffixes = map[string][]string{
	"go":         {"_test"},
	"javascript": {".test", ".spec"},
	"typescript": {".test", ".spec"},
	"python":     {"_test"},
	"ruby":       {"_spec", "_test"},
	"java":       {"Test", "Tests"},
	"kotlin":     {"Test", "Tests"},
	"csharp":     {"Test", "Tests"},
	"scala":      {"Test", "Spec", "Suite"},
	"swift":      {"Test", "Tests"},
	"php":        {"Test"},
	"dart":       {"_test"},
	"elixir":     {"_test"},
	"rust":       {"_test", "_tests"},
	"c++":        {"_test", "_unittest"},
	"c":          {"_test"},
}

// testPrefixes start a test file's name: test_client.py.
var testPrefixes = map[string][]string{
	"python": {"test_"},
	"shell":  {"test_"},
}

// testNames are whole file names that hold tests or their setup.
var testNames = []string{"conftest.py"}

// IsTest reports whether the file at filePath (slash-separated, relative to
// its repo) holds tests: it sits in a test folder (tests/, __tests__/,
// spec/, e2e/, testdata/), or its name follows its language's convention
// for test files (client_test.go, test_client.py, client.spec.ts,
// ClientTest.java).
func IsTest(filePath string) bool {
	if slices.ContainsFunc(strings.Split(path.Dir(filePath), "/"), isTestDir) {
		return true
	}
	base := path.Base(filePath)
	if slices.Contains(testNames, base) {
		return true
	}
	language := Detect(filePath, nil)
	stem := strings.TrimSuffix(base, path.Ext(base))
	for _, suffix := range testSuffixes[language] {
		if strings.HasSuffix(stem, suffix) && stem != suffix {
			return true
		}
	}
	for _, prefix := range testPrefixes[language] {
		if strings.HasPrefix(stem, prefix) {
			return true
		}
	}
	return false
}

// isTestDir reports whether a folder name is one of testDirs, in any case.
func isTestDir(name string) bool {
	return slices.Contains(testDirs, strings.ToLower(name))
}
