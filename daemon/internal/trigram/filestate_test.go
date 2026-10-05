package trigram

import (
	"reflect"
	"testing"
)

func TestIsFiltersFilesByState(t *testing.T) {
	// @covers op:is screen:test-files
	repo := WithOpenFiles([]Repo{webRepo}, []string{"/repos/web/src/client.ts", "/elsewhere/src/retry.ts"})[0]
	repo.History = fakeHistory{dirty: map[string]bool{"src/retry.ts": true}}
	tests := []struct {
		query string
		want  []string
	}{
		{"is:open RetryPolicy", []string{"src/client.ts:1 import { RetryPolicy } from './retry';", "src/client.ts:3 new RetryPolicy();"}},
		{"is:changed RetryPolicy", []string{"src/retry.ts:1 export class RetryPolicy {"}},
		{"is:test timeout", []string{"src/retry_test.py:1 def test_timeout():", "src/retry_test.py:2     assert retry(timeout=1)"}},
		{"retry -is:test -f:vendor/", []string{"file src/retry.ts", "README.md:1 Retries use a RetryPolicy.", "src/client.ts:1 import { RetryPolicy } from './retry';", "src/client.ts:3 new RetryPolicy();", "src/retry.ts:1 export class RetryPolicy {"}},
		{"i:test f:retry", []string{"file src/retry_test.py"}},
	}
	for _, tt := range tests {
		if got, _ := run(t, tt.query, repo); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%s:\n got %q\nwant %q", tt.query, got, tt.want)
		}
	}
}

func TestIsChangedMatchesNothingOutsideGit(t *testing.T) {
	if got, _ := run(t, "is:changed RetryPolicy", webRepo); len(got) != 0 {
		t.Errorf("is:changed without a history = %q, want nothing", got)
	}
}

func TestOpenInKeepsOnlyTheRootsFiles(t *testing.T) {
	open := []string{"/repos/web/src/a.ts", "/repos/web", "/repos/website/b.ts", "/repos/other/c.ts", "/repos/web/../web2/d.ts"}
	want := map[string]bool{"src/a.ts": true}
	if got := OpenIn("/repos/web", open); !reflect.DeepEqual(got, want) {
		t.Errorf("OpenIn = %v, want %v", got, want)
	}
	if got := OpenIn("/repos/web", nil); got != nil {
		t.Errorf("OpenIn(nil) = %v, want nil", got)
	}
}
