package trigram

import (
	"bytes"
	"context"
	"fmt"
	"math/rand"
	"os/exec"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/cruisinme30/unified-search/daemon/internal/protocol"
	"github.com/cruisinme30/unified-search/daemon/internal/query"
)

// TestMatchesRipgrep compares the lines this engine finds with ripgrep's
// on a generated corpus: the trigram prefilter must never drop a match.
func TestMatchesRipgrep(t *testing.T) {
	rg, err := exec.LookPath("rg")
	if err != nil {
		t.Skip("ripgrep not installed")
	}
	words := []string{"retry", "Retry", "RETRY", "policy", "timeout", "Timeout", "café", "CAFÉ", "naïve", "ab", "xyz", "foo_bar", "fooBar", "😀", "{", "}", "\t"}
	random := rand.New(rand.NewSource(1))
	files := map[string]string{}
	for f := 0; f < 60; f++ {
		var b strings.Builder
		for line := 0; line < 1+random.Intn(20); line++ {
			for w := 0; w < random.Intn(8); w++ {
				b.WriteString(words[random.Intn(len(words))])
				b.WriteString([]string{" ", "", "-", "."}[random.Intn(4)])
			}
			b.WriteString([]string{"\n", "\r\n"}[random.Intn(2)])
		}
		files[fmt.Sprintf("dir%d/file%02d.txt", f%4, f)] = b.String()
	}
	root := writeTree(t, files)
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
	}
	settings := protocol.Settings{DefaultCount: query.MaxResults}
	for _, q := range queries {
		t.Run(q.text, func(t *testing.T) {
			plan := mustPlan(t, "type:code "+q.text, settings, "")
			var got []string
			_, err := Search(context.Background(), plan, []Repo{repo}, 1, func(item protocol.ResultItem) {
				got = append(got, fmt.Sprintf("%s:%d", item.Path, item.Line))
			})
			if err != nil {
				t.Fatal(err)
			}
			args := append([]string{"--no-config", "--no-heading", "-n", "--crlf", "--no-filename", "--with-filename"}, q.rgFlags...)
			cmd := exec.Command(rg, append(args, "-e", q.pattern, ".")...)
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
