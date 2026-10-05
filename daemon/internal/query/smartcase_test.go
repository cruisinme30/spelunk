package query

import "testing"

func TestHasCapitalLooksAtWhatIsSearchedFor(t *testing.T) {
	tests := []struct {
		query string
		want  bool
	}{
		{"retry", false},
		{"RetryPolicy", true},
		{`"Retry policy"`, true},
		{"retry -Legacy", true},
		{"sym:Retry", true},
		{"msg:Revert", true},
		{"f:Payments/ retry", false}, // filters don't count
		{"repo:App retry", false},
		{"author:Jane retry", false},
		{`/\W+retry\S/`, false}, // escapes aren't capitals
		{`/\p{Lu}ow/`, false},
		{`/(?i)Retry/`, false}, // nor are letters under (?i)
		{`/[A-Z]ow/`, false},   // a range is a class, not a literal
		{`/retry_Count/`, true},
		{"Éé", true}, // a capital outside ASCII
	}
	for _, tt := range tests {
		if got := Parse(tt.query, testResolver).HasCapital; got != tt.want {
			t.Errorf("Parse(%q).HasCapital = %v, want %v", tt.query, got, tt.want)
		}
	}
}
