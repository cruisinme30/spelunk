package trigram

import (
	"bytes"
	"context"
	"fmt"
	"math/rand"
	"os/exec"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
	"github.com/cruisinme30/spelunk/daemon/internal/query"
	"github.com/cruisinme30/spelunk/daemon/internal/testutil"
)

// TestMatchesRipgrep compares the lines this engine finds with ripgrep's
// on a generated corpus: the trigram prefilter must never drop a match.
func TestMatchesRipgrep(t *testing.T) {
	rg, err := exec.LookPath("rg")
	if err != nil {
		t.Skip("ripgrep not installed")
	}
	words := []string{"retry", "Retry", "RETRY", "policy", "timeout", "Timeout", "café", "CAFÉ", "naïve", "ab", "xyz", "foo_bar", "fooBar", "kelvin", kelvinSign + "elvin", longS + "top", "stop", "😀", "{", "}", "\t"}
	random := rand.New(rand.NewSource(1))
	files := map[string]string{}
	for f := range 60 {
		var b strings.Builder
		for range 1 + random.Intn(20) {
			for range random.Intn(8) {
				b.WriteString(words[random.Intn(len(words))])
				b.WriteString([]string{" ", "", "-", "."}[random.Intn(4)])
			}
			b.WriteString([]string{"\n", "\r\n"}[random.Intn(2)])
		}
		files[fmt.Sprintf("dir%d/file%02d.txt", f%4, f)] = b.String()
	}
	root := testutil.WriteTree(t, files)
	repo := Repo{ID: "r", Name: "r", Root: root, Shard: shardOf(files)}

	queries := []struct {
		text    string
		rgFlags []string
		pattern string
	}{
		{"retry", []string{"-i", "-F"}, "retry"},
		{"case:yes Retry", []string{"-s", "-F"}, "Retry"},
		{"café", []string{"-i", "-F"}, "café"},
		{"timeout.", []string{"-i", "-F"}, "timeout."},
		{"foo_bar", []string{"-i", "-F"}, "foo_bar"},
		{`/foo(_b|B)ar/`, []string{"-i"}, `foo(_b|B)ar`},
		{`/re?try\s+pol/`, []string{"-i"}, `re?try\s+pol`},
		{`case:yes /^RETRY/`, []string{"-s"}, `^RETRY`},
		{"naïve", []string{"-i", "-F"}, "naïve"},
		{"kelvin", []string{"-i", "-F"}, "kelvin"},
		{"stop", []string{"-i", "-F"}, "stop"},
	}
	settings := protocol.Settings{DefaultCount: query.MaxResults}
	for _, q := range queries {
		t.Run(q.text, func(t *testing.T) {
			plan := mustPlan(t, "type:code "+q.text, settings, "")
			var got []string
			_, err := Search(context.Background(), plan, []Repo{repo}, 1, func(item protocol.ResultItem) {
				got = append(got, item.Path+":"+strconv.Itoa(item.Line))
			})
			if err != nil {
				t.Fatal(err)
			}
			// Each output line is path:line:text; the parsing below needs all three.
			args := append([]string{"--no-config", "--no-heading", "-n", "--crlf", "--with-filename"}, q.rgFlags...)
			cmd := exec.CommandContext(context.Background(), rg, append(args, "-e", q.pattern, ".")...)
			cmd.Dir = root
			out, _ := cmd.Output() // exit status 1 means no matches
			want := []string{}
			for _, line := range strings.Split(string(bytes.TrimSpace(out)), "\n") {
				if line == "" {
					continue
				}
				parts := strings.SplitN(line, ":", 3)
				want = append(want, strings.TrimPrefix(parts[0], "./")+":"+parts[1])
			}
			sort.Strings(got)
			sort.Strings(want)
			if got == nil {
				got = []string{}
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("engine and ripgrep disagree on %q:\nengine  %d lines %q\nripgrep %d lines %q", q.text, len(got), got, len(want), want)
			}
		})
	}
}

// TestMatchesAcrossLinesLikeRipgrep compares the lines that regexes with
// \n cover with ripgrep's multiline mode (-U) on a generated corpus whose
// lines end in "\n" (ripgrep's \n doesn't match "\r\n"; this engine reads
// "\r\n" as "\n"). Each result covers its first line through endLine.
func TestMatchesAcrossLinesLikeRipgrep(t *testing.T) {
	rg, err := exec.LookPath("rg")
	if err != nil {
		t.Skip("ripgrep not installed")
	}
	words := []string{"retry", "Retry", "policy", "timeout", "café", "CAFÉ", "ab", "xyz", "{", "}", "😀"}
	random := rand.New(rand.NewSource(2))
	files := map[string]string{}
	for f := range 60 {
		var b strings.Builder
		for range 1 + random.Intn(30) {
			for range random.Intn(5) {
				b.WriteString(words[random.Intn(len(words))])
				b.WriteString([]string{" ", "", "-"}[random.Intn(3)])
			}
			b.WriteString("\n")
		}
		files[fmt.Sprintf("dir%d/file%02d.txt", f%4, f)] = b.String()
	}
	root := testutil.WriteTree(t, files)
	repo := Repo{ID: "r", Name: "r", Root: root, Shard: shardOf(files)}

	queries := []struct {
		text    string
		rgFlags []string
		pattern string
	}{
		{`/retry\npolicy/`, []string{"-i"}, `retry\npolicy`},
		{`case:yes /Retry[ -]*\n[^\n]*timeout/`, []string{"-s"}, `Retry[ -]*\n[^\n]*timeout`},
		{`/xyz\n.*\n.*ab/`, []string{"-i"}, `xyz\n.*\n.*ab`},
		{`/}\n\{/`, []string{"-i"}, `}\n\{`},
		{`/café\s*\n\s*café/`, []string{"-i"}, `café\s*\n\s*café`},
		{`/^ab.*\n^xyz/`, []string{"-i"}, `^ab.*\n^xyz`},
		{`/timeout[ -]*\n(.*\n)?.*😀/`, []string{"-i"}, `timeout[ -]*\n(.*\n)?.*😀`},
	}
	settings := protocol.Settings{DefaultCount: query.MaxResults}
	for _, q := range queries {
		t.Run(q.text, func(t *testing.T) {
			plan := mustPlan(t, "type:code "+q.text, settings, "")
			got := []string{}
			_, err := Search(context.Background(), plan, []Repo{repo}, 1, func(item protocol.ResultItem) {
				last := item.Line
				if item.EndLine > 0 {
					last = item.EndLine
				}
				for line := item.Line; line <= last; line++ {
					got = append(got, item.Path+":"+strconv.Itoa(line))
				}
			})
			if err != nil {
				t.Fatal(err)
			}
			args := append([]string{"--no-config", "--no-heading", "-n", "-U", "--with-filename"}, q.rgFlags...)
			cmd := exec.CommandContext(context.Background(), rg, append(args, "-e", q.pattern, ".")...)
			cmd.Dir = root
			out, _ := cmd.Output() // exit status 1 means no matches
			want := []string{}
			for _, line := range strings.Split(string(bytes.TrimSpace(out)), "\n") {
				if line == "" {
					continue
				}
				parts := strings.SplitN(line, ":", 3)
				want = append(want, strings.TrimPrefix(parts[0], "./")+":"+parts[1])
			}
			sort.Strings(got)
			sort.Strings(want)
			if len(want) == 0 {
				t.Fatalf("ripgrep found nothing for %q; pick a pattern the corpus has", q.pattern)
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("engine and ripgrep disagree on %q:\nengine  %d lines %q\nripgrep %d lines %q", q.text, len(got), got, len(want), want)
			}
		})
	}
}
