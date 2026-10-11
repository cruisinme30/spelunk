package symbols

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// outlined renders Outline's result as "name:line", indented two spaces a
// level.
func outlined(lang, source string) []string {
	var out []string
	for _, e := range Outline(lang, []byte(source)) {
		out = append(out, fmt.Sprintf("%s%s:%d", strings.Repeat("  ", e.Depth), e.Name, e.Line))
	}
	return out
}

func TestOutlineNestsMembersUnderTheirTypes(t *testing.T) {
	tests := []struct {
		name, lang, source string
		want               []string
	}{
		{"go methods under their receiver's type, wherever they are", "go", `package owners

func (f *File) Match(path string) bool { return false }

type File struct {
	Path string
}

func Load() *File { return nil }

func (f File) Owners() []string { return nil }

func (s *Set[K, V]) Add(k K) {}

func (*Rule) String() string { return "" }
`, []string{"File:5", "  Match:3", "  Owners:11", "Load:9", "Add:13", "String:15"}},
		{"python methods until a line as far left as the class", "python", `class Policy:
    def next(self):
        def helper():
            pass

    @property
    def attempts(self):
        pass
def fetch():
    pass
`, []string{"Policy:1", "  next:2", "  helper:3", "  attempts:7", "fetch:9"}},
		{"java nested classes and a second top-level type", "java", `public class Client {
    public void charge() {
    }
    static class Builder {
        Builder retries(int n) {
        }
    }
    public void refund() {
    }
}
interface Waiter {
}
`, []string{"Client:1", "  charge:2", "  Builder:4", "    retries:5", "  refund:8", "Waiter:11"}},
		{"rust methods under the type their impl names", "rust", `pub trait Wait {
    fn wait(&self);
}
pub struct Policy {}
impl fmt::Display for Policy {
    fn fmt(&self) {}
}
impl<T> Policy {
    pub fn new() -> Self {}
}
fn main() {}
`, []string{"Wait:1", "  wait:2", "Policy:4", "  fmt:6", "  new:9", "main:11"}},
		{"ruby classes inside a module", "ruby", `module Payments
  class RetryPolicy
    def next_delay(attempt)
    end
  end
end
def helper; end
`, []string{"Payments:1", "  RetryPolicy:2", "    next_delay:3", "helper:7"}},
		{"a language with no rules", "markdown", "# class Nope\n", nil},
	}
	for _, tt := range tests {
		if got := outlined(tt.lang, tt.source); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%s:\n got %q\nwant %q", tt.name, got, tt.want)
		}
	}
}

func TestOutlineStopsAtMaxOutline(t *testing.T) {
	source := strings.Repeat("func f() {}\n", MaxOutline+5)
	if got := len(Outline("go", []byte(source))); got != MaxOutline {
		t.Errorf("an outline of %d functions has %d entries, want %d", MaxOutline+5, got, MaxOutline)
	}
}
