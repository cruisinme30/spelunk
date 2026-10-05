package query

import (
	"reflect"
	"regexp"
	"testing"
)

func TestWholeWordKeepsOnlyMatchesAtWordEdges(t *testing.T) {
	// @covers op:word
	tests := []struct {
		pattern, text string
		want          [][]int
	}{
		{`retry`, "retry retryCount autoretry retry_x (retry)", [][]int{{0, 5}, {36, 41}}},
		{`retry`, "retryCount", nil},
		// An edge where the term itself starts or ends with a non-word
		// character always qualifies, as in VS Code.
		{`bar\.`, "bar.baz rebar.", [][]int{{0, 4}}},
		{`->`, "a->b", [][]int{{1, 3}}},
		// Letters and digits outside ASCII are word characters too.
		{`café`, "cafés café", [][]int{{7, 12}}},
		{`naïve`, "naïveté", nil},
		// A whole word that starts inside a rejected match is still found.
		{`a-a`, "ba-a-a ", [][]int{{3, 6}}},
		// A regex with assertions is matched over the whole text, so ^ still means the start.
		{`^retry`, "retry", [][]int{{0, 5}}},
		{`\bretry`, "retryCount retry", [][]int{{11, 16}}},
	}
	for _, tt := range tests {
		re := regexp.MustCompile(tt.pattern)
		term := &Content{Re: re, WholeWord: true, resumable: canResume(re)}
		if got := term.FindAllIndex([]byte(tt.text), -1); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("/%s/ whole words in %q = %v, want %v", tt.pattern, tt.text, got, tt.want)
		}
		if got := term.FindAllStringIndex(tt.text, -1); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("/%s/ whole words in string %q = %v, want %v", tt.pattern, tt.text, got, tt.want)
		}
		if got, want := term.MatchString(tt.text), tt.want != nil; got != want {
			t.Errorf("/%s/ MatchString(%q) = %v, want %v", tt.pattern, tt.text, got, want)
		}
	}
}

func TestWholeWordStopsAtTheLimit(t *testing.T) {
	term := &Content{Re: regexp.MustCompile(`a`), WholeWord: true, resumable: true}
	if got := term.FindAllIndex([]byte("ab a a a"), 2); !reflect.DeepEqual(got, [][]int{{3, 4}, {5, 6}}) {
		t.Errorf("first 2 whole-word a = %v, want [[3 4] [5 6]]", got)
	}
	term.resumable = false
	if got := term.FindAllIndex([]byte("ab a a a"), 2); !reflect.DeepEqual(got, [][]int{{3, 4}, {5, 6}}) {
		t.Errorf("first 2 whole-word a, without resuming = %v, want [[3 4] [5 6]]", got)
	}
}

func TestPartsOfWordsMatchWithoutWholeWord(t *testing.T) {
	term := &Content{Re: regexp.MustCompile(`retry`)}
	if got := term.FindAllStringIndex("autoretry", -1); !reflect.DeepEqual(got, [][]int{{4, 9}}) {
		t.Errorf("retry in autoretry = %v, want [[4 9]]", got)
	}
}
