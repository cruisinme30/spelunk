package symbols

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// found renders Extract's result as "line kind name".
func found(lang, source string) []string {
	var out []string
	for _, s := range Extract(lang, []byte(source)) {
		out = append(out, fmt.Sprintf("%d %s %s", s.Line, s.Kind, s.Name))
	}
	return out
}

func TestExtractFindsTheDefinitionsOfEachLanguage(t *testing.T) {
	tests := []struct {
		lang, source string
		want         []string
	}{
		{"python", `class RetryPolicy:
    """Backoff."""
    def __init__(self):
        pass

async def fetch(url):
    if ready:
        return url
`, []string{"1 class RetryPolicy", "3 method __init__", "6 function fetch"}},
		{"go", `package retry

type Policy struct{}

type Waiter interface {
	Wait()
}

func (p *Policy) Next(attempt int) time.Duration {
	return 0
}

func New() *Policy { return &Policy{} }
`, []string{"3 type Policy", "5 interface Waiter", "9 method Next", "13 function New"}},
		{"typescript", `export interface RetryPolicy {
  maxAttempts: number;
}
export type Delay = number;
export enum Mode { Fast, Slow }
export class Client {
  constructor(private readonly policy: RetryPolicy) {}
  async charge(amount: number): Promise<void> {
    if (amount > 0) {
      return;
    }
  }
}
export function retry(fn: () => void) {}
export const wait = async (ms: number) => {};
`, []string{
			"1 interface RetryPolicy", "4 type Delay", "5 type Mode", "6 class Client", "8 method charge",
			"14 function retry", "15 function wait",
		}},
		{"java", `public class RetryPolicy {
    private final int maxAttempts;
    public RetryPolicy(int maxAttempts) {
    }
    public long nextDelay(int attempt) {
        return attempt;
    }
}
interface Waiter {}
`, []string{"1 class RetryPolicy", "3 method RetryPolicy", "5 method nextDelay", "9 interface Waiter"}},
		{"rust", `pub struct Policy {
    attempts: u32,
}
pub trait Wait {
    fn wait(&self);
}
impl Policy {
    pub fn new() -> Self {
        Policy { attempts: 3 }
    }
}
fn main() {}
`, []string{"1 type Policy", "4 interface Wait", "5 method wait", "8 method new", "12 function main"}},
		{"ruby", `module Payments
  class RetryPolicy
    def next_delay(attempt)
    end
  end
end
def helper; end
`, []string{"1 other Payments", "2 class RetryPolicy", "3 method next_delay", "7 function helper"}},
		{"c++", `class RetryPolicy : public Base {
public:
  int next(int attempt);
};
int RetryPolicy::next(int attempt) {
  if (attempt > 3) {
    return 0;
  }
}
`, []string{"1 class RetryPolicy", "5 function RetryPolicy::next"}},
		{"kotlin", `data class Policy(val attempts: Int)
interface Waiter
fun retry(times: Int) {}
`, []string{"1 class Policy", "2 interface Waiter", "3 function retry"}},
		{"markdown", "# class Nope\n", nil},
	}
	for _, tt := range tests {
		if got := found(tt.lang, tt.source); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%s:\n got %q\nwant %q", tt.lang, got, tt.want)
		}
	}
}

func TestExtractSkipsMinifiedLines(t *testing.T) {
	line := "function minified(){" + strings.Repeat("a", maxLineBytes) + "}"
	if got := found("javascript", line); got != nil {
		t.Errorf("a line over %d bytes = %q, want no symbols", maxLineBytes, got)
	}
}

func TestJavaMethodsDependOnTheLineNotItsIndent(t *testing.T) {
	tests := []struct {
		name, source string
		want         []string
	}{
		{"space-indented method", "    long nextDelay(int attempt) {\n", []string{"1 method nextDelay"}},
		{"tab-indented method", "\tlong nextDelay(int attempt) {\n", []string{"1 method nextDelay"}},
		{"generic return type with a space", "    Map<String, Integer> counts() {\n", []string{"1 method counts"}},
		{"package-private constructor", "    RetryPolicy(int maxAttempts) {\n", []string{"1 method RetryPolicy"}},
		{"tab-indented constructor that throws", "\tRetryPolicy(int maxAttempts) throws IOException {\n", []string{"1 method RetryPolicy"}},
		{"deeply indented call continued on the next line", "        retry(attempt,\n", nil},
	}
	for _, tt := range tests {
		if got := found("java", tt.source); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%s: found(java, %q) = %q, want %q", tt.name, tt.source, got, tt.want)
		}
	}
}
