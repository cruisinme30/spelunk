package glob

import "testing"

func TestBody(t *testing.T) {
	tests := []struct {
		pattern string
		escapes bool
		want    string
	}{
		{"src/**/*.go", false, `src/(?:.*/)?[^/]*\.go`},
		{"a**b", false, `a.*b`},
		{"file?.txt", false, `file[^/]\.txt`},
		{`\*.go`, true, `\*\.go`},
		{`\*.go`, false, `\\[^/]*\.go`},
		{`trailing\`, true, `trailing\\`},
	}
	for _, tt := range tests {
		if got := Body(tt.pattern, tt.escapes); got != tt.want {
			t.Errorf("Body(%q, %v) = %q, want %q", tt.pattern, tt.escapes, got, tt.want)
		}
	}
}
