package trigram

import (
	"reflect"
	"testing"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
	"github.com/cruisinme30/spelunk/daemon/internal/query"
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

func TestMatchFuzzyFindsFileNamesWithAWordsLettersInOrder(t *testing.T) {
	items, _ := runItems(t, "match:fuzzy rtrypol", bestMatchSettings, "", fuzzyRepo())
	want := []string{"file src/payments/retry_policy.py", "file tests/retry_policy_test.py"}
	if got := files(items); !reflect.DeepEqual(got, want) {
		t.Errorf("match:fuzzy rtrypol:\n got %q\nwant %q", got, want)
	}
	// Each run of matched letters is highlighted: r, try, pol.
	wantHits := []protocol.Range{{Start: 13, End: 14}, {Start: 15, End: 18}, {Start: 19, End: 22}}
	if got := items[0].NameHits; !reflect.DeepEqual(got, wantHits) {
		t.Errorf("hits = %+v, want %+v", got, wantHits)
	}
}

func TestExactNameMatchesRankAboveFuzzyOnesAndCodeLinesStayExact(t *testing.T) {
	items, _ := runItems(t, "match:fuzzy usrsvc", bestMatchSettings, "", fuzzyRepo())
	want := []string{
		"file src/usrsvc.go",      // the name is the word
		"file src/UserService.ts", // only fuzzily
		"line src/usrsvc.go",      // lines match only exactly
		"line docs/notes.md",
	}
	if got := files(items); !reflect.DeepEqual(got, want) {
		t.Errorf("match:fuzzy usrsvc:\n got %q\nwant %q", got, want)
	}
}

func TestUnderMatchFuzzyOnlyABareWordMatchesFuzzilyFromTheStartOfAWord(t *testing.T) {
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
		items, _ := runItems(t, "match:fuzzy "+tc.query, bestMatchSettings, "", fuzzyRepo())
		if got := files(items); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s:\n got %q\nwant %q", tc.query, got, tc.want)
		}
	}
}

func TestWordsMatchFileNamesExactlyByDefaultAndANoteCountsWhatFuzzyAdds(t *testing.T) {
	// @covers op:match setting:search.match screen:fuzzy-file-names
	for _, tc := range []struct {
		query    string
		settings string
		want     []string
		hidden   int
		undo     string
	}{
		// Exact by default: retry finds the names that say retry, not registry.go.
		{"retry", "", []string{
			"file src/payments/retry_policy.py", "file tests/retry_policy_test.py",
			"line src/payments/retry_policy.py", "line tests/retry_policy_test.py",
		}, 1, "match:fuzzy retry"},
		{"rtrypol", "", nil, 2, "match:fuzzy rtrypol"},
		{"usrsvc", "", []string{"file src/usrsvc.go", "line src/usrsvc.go", "line docs/notes.md"}, 1, "match:fuzzy usrsvc"},
		// The setting makes fuzzy the default, and match:exact turns it off again.
		{"rtrypol", "fuzzy", []string{"file src/payments/retry_policy.py", "file tests/retry_policy_test.py"}, 0, ""},
		{"match:exact rtrypol", "fuzzy", nil, 2, "match:fuzzy rtrypol"},
		// Nothing a fuzzy word could add: no note.
		{`"rtrypol"`, "", nil, 0, ""},
		{"type:code rtrypol", "", nil, 0, ""},
	} {
		settings := bestMatchSettings
		settings.Match = tc.settings
		items, stats := runItems(t, tc.query, settings, "", fuzzyRepo())
		if got := files(items); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s (setting %q):\n got %q\nwant %q", tc.query, tc.settings, got, tc.want)
		}
		var note *protocol.HiddenNote
		for i := range stats.Hidden {
			if stats.Hidden[i].Reason == "match" {
				note = &stats.Hidden[i]
			}
		}
		switch {
		case tc.hidden == 0 && note != nil:
			t.Errorf("%s: unexpected note %+v", tc.query, *note)
		case tc.hidden > 0 && note == nil:
			t.Errorf("%s: no note, want %d file names", tc.query, tc.hidden)
		case tc.hidden > 0:
			if note.Count != tc.hidden || note.Unit != "files" {
				t.Errorf("%s: note counts %d %s, want %d files", tc.query, note.Count, note.Unit, tc.hidden)
			}
			if got := query.ApplyFix(tc.query, note.Undo); got != tc.undo {
				t.Errorf("%s: Show them gives %q, want %q", tc.query, got, tc.undo)
			}
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
