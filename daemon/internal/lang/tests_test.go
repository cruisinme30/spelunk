package lang

import "testing"

func TestIsTest(t *testing.T) {
	cases := map[string]bool{
		"pkg/client_test.go":             true,
		"pkg/client.go":                  false,
		"src/test_client.py":             true,
		"src/client_test.py":             true,
		"src/conftest.py":                true,
		"src/contest.py":                 false,
		"web/client.test.ts":             true,
		"web/client.spec.jsx":            true,
		"web/spec.ts":                    false,
		"lib/client_spec.rb":             true,
		"src/main/java/ClientTest.java":  true,
		"src/main/java/Latest.java":      false,
		"Sources/ClientTests.swift":      true,
		"tests/payments/helpers.py":      true,
		"web/__tests__/App.tsx":          true,
		"e2e/checkout.py":                true,
		"daemon/testdata/workspace/a.go": true,
		"Tests/Fixtures/a.json":          true,
		"docs/testing.md":                false,
		"latest/notes.md":                false,
		"test_notes.md":                  false, // test_ marks tests only in Python and shell
	}
	for path, want := range cases {
		if got := IsTest(path); got != want {
			t.Errorf("IsTest(%q) = %v, want %v", path, got, want)
		}
	}
}
