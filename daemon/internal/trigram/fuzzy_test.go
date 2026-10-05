package trigram

import (
	"reflect"
	"testing"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
)

// fuzzyRepo has names a fuzzy word can find, and lines it must not.
func fuzzyRepo() Repo {
	return repoOf("app", map[string]string{
		"src/UserService.ts":           "export class UserService {}\n",
		"src/users/user_settings.py":   "SETTINGS = {}\n",
		"src/payments/retry_policy.py": "class RetryPolicy: pass\n",
		"src/registry.go":              "package src\n",
		"src/usrsvc.go":                "package src // usrsvc\n",
		"docs/notes.md":                "usrsvc is short for UserService\n",
		"tests/retry_policy_test.py":   "def test_retry(): pass\n",
	})
}

func TestABareWordFindsFileNamesWithItsLettersInOrder(t *testing.T) {
	items, _ := runItems(t, "rtrypol", bestMatchSettings, "", fuzzyRepo())
	want := []string{"file src/payments/retry_policy.py", "file tests/retry_policy_test.py"}
	if got := files(items); !reflect.DeepEqual(got, want) {
		t.Errorf("rtrypol:\n got %q\nwant %q", got, want)
	}
	// Each run of matched letters is highlighted: r, try, pol.
	wantHits := []protocol.Range{{Start: 13, End: 14}, {Start: 15, End: 18}, {Start: 19, End: 22}}
	if got := items[0].NameHits; !reflect.DeepEqual(got, wantHits) {
		t.Errorf("hits = %+v, want %+v", got, wantHits)
	}
}

func TestExactNameMatchesRankAboveFuzzyOnesAndCodeLinesStayExact(t *testing.T) {
	items, _ := runItems(t, "usrsvc", bestMatchSettings, "", fuzzyRepo())
	want := []string{
		"file src/usrsvc.go",      // the name is the word
		"file src/UserService.ts", // only fuzzily
		"line src/usrsvc.go",      // lines match only exactly
		"line docs/notes.md",
	}
	if got := files(items); !reflect.DeepEqual(got, want) {
		t.Errorf("usrsvc:\n got %q\nwant %q", got, want)
	}
}

func TestOnlyABareWordMatchesFuzzilyFromTheStartOfAWord(t *testing.T) {
	for _, tc := range []struct {
		query string
		want  []string
	}{
		{"ervce", nil}, // no e of UserService starts a word
		{"retry", []string{ // registry has r, e, t, r, y in order, as in Quick Open, but ranks after the exact name
			"file src/payments/retry_policy.py", "file src/registry.go", "file tests/retry_policy_test.py",
			"line src/payments/retry_policy.py", "line tests/retry_policy_test.py",
		}},
		{"py -rtrypol", []string{ // a NOT stays exact, so it removes nothing here
			"file src/payments/retry_policy.py", "file src/users/user_settings.py", "file tests/retry_policy_test.py",
		}},
		{`"rtrypol"`, nil},        // a phrase stays exact
		{"/rtrypol/", nil},        // so does a regex
		{"word:yes rtrypol", nil}, // and a whole word
		{"case:yes Usrsvc", nil},  // case:yes compares case
		{"pay/rtry", []string{"file src/payments/retry_policy.py"}}, // a / matches the whole path
		{"type:code rtrypol", nil},                                  // code lines never match fuzzily
	} {
		items, _ := runItems(t, tc.query, bestMatchSettings, "", fuzzyRepo())
		if got := files(items); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s:\n got %q\nwant %q", tc.query, got, tc.want)
		}
	}
}

func TestBestAlignmentPrefersWordStartsAndRuns(t *testing.T) {
	fold := func(r rune) rune { return r }
	for _, tc := range []struct {
		want, name string
		matched    []int
	}{
		{"usrsvc", "usrsvc.go", []int{0, 1, 2, 3, 4, 5}},
		{"rtrypol", "retry_policy.py", []int{0, 2, 3, 4, 6, 7, 8}},
		{"ab", "xa_ab", []int{3, 4}}, // a run at a word start, not the first a
	} {
		if got, _ := bestAlignment([]rune(tc.want), []rune(tc.name), fold); !reflect.DeepEqual(got, tc.matched) {
			t.Errorf("bestAlignment(%q, %q) = %v, want %v", tc.want, tc.name, got, tc.matched)
		}
	}
}
