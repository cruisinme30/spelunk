package server

import (
	"slices"
	"testing"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
	"github.com/cruisinme30/spelunk/daemon/internal/testutil"
)

// paths runs text and returns the repo/path:line of each result.
func paths(t *testing.T, client *testClient, text string) ([]string, protocol.SearchResult) {
	t.Helper()
	items, result := search(t, client, text)
	return resultLines(items), result
}

// updateSettings sends settings/update with change applied to the settings
// the client initialized with.
func updateSettings(t *testing.T, client *testClient, change func(*protocol.Settings)) {
	t.Helper()
	settings := client.server.Settings()
	change(&settings)
	if err := client.conn.Notify(protocol.MethodSettingsUpdate, protocol.SettingsUpdateParams{Settings: settings}); err != nil {
		t.Fatal(err)
	}
}

func TestChangedSettingsTakeEffectWithoutARestart(t *testing.T) {
	// @covers rpc:settings/update screen:settings setting:caseSensitive setting:defaultCount
	root := testutil.WriteTree(t, map[string]string{
		"src/a.py": "retry = 1\nRetry = 2\n",
		"gen/b.py": "retry = 3\n",
	})
	client := newTestClient(t)
	client.mustInitialize(t, protocol.Root{ID: "r", Path: root, Name: "app"})

	if got, _ := paths(t, client, "type:code retry"); len(got) != 3 {
		t.Fatalf("retry = %q, want 3 lines in both files", got)
	}

	updateSettings(t, client, func(s *protocol.Settings) { s.CaseSensitive = protocol.CaseSettingOn })
	waitUntil(t, "case-sensitive matching", func() bool {
		got, _ := paths(t, client, "type:code Retry")
		return slices.Equal(got, []string{"r/src/a.py:2"})
	})

	updateSettings(t, client, func(s *protocol.Settings) { s.DefaultCount = 1 })
	waitUntil(t, "pages of one result", func() bool {
		got, result := paths(t, client, "type:code retry")
		return len(got) == 1 && result.Total == 2 && result.NextCursor != ""
	})

	updateSettings(t, client, func(s *protocol.Settings) { s.Exclude = append(s.Exclude, "gen/**") })
	waitUntil(t, "gen/ to leave the index", func() bool {
		got, _ := paths(t, client, "type:code retry")
		return slices.Equal(got, []string{"r/src/a.py:1"})
	})

	updateSettings(t, client, func(s *protocol.Settings) { s.CaseSensitive = protocol.CaseSettingSmart })
	waitUntil(t, "smart case to ignore case in a lowercase query", func() bool {
		got, result := paths(t, client, "type:code retry")
		return len(got) == 1 && result.Total == 2
	})
	if got, _ := paths(t, client, "type:code Retry"); !slices.Equal(got, []string{"r/src/a.py:2"}) {
		t.Errorf("Retry with smart case = %q, want only the capitalized line", got)
	}
}

func TestTheContextLinesSettingAddsLinesAroundCodeResults(t *testing.T) {
	// @covers rpc:settings/update setting:results.contextLines
	root := testutil.WriteTree(t, map[string]string{"a.py": "import time\n\ndef go():\n    retry()\n    return\n"})
	client := newTestClient(t)
	client.mustInitialize(t, protocol.Root{ID: "r", Path: root, Name: "app"})

	if items, _ := search(t, client, "type:code retry"); len(items) != 1 || items[0].ContextBefore != nil || items[0].ContextAfter != nil {
		t.Fatalf("without the setting: %+v, want one line without context", items)
	}
	updateSettings(t, client, func(s *protocol.Settings) { s.ContextLines = 2 })
	waitUntil(t, "two lines of context", func() bool {
		items, _ := search(t, client, "type:code retry")
		return len(items) == 1 &&
			slices.Equal(items[0].ContextBefore, []string{"", "def go():"}) &&
			slices.Equal(items[0].ContextAfter, []string{"    return"})
	})
	updateSettings(t, client, func(s *protocol.Settings) { s.ContextLines = 99 })
	waitUntil(t, "context capped at three lines", func() bool {
		items, _ := search(t, client, "type:code retry")
		return len(items) == 1 && len(items[0].ContextBefore) == 3
	})
}

func TestSetRootsAddsAndDropsRepos(t *testing.T) {
	// @covers rpc:workspace/setRoots
	first := testutil.WriteTree(t, map[string]string{"one.txt": "needle\n"})
	second := testutil.WriteTree(t, map[string]string{"two.txt": "needle\n"})
	client := newTestClient(t)
	client.mustInitialize(t, protocol.Root{ID: "a", Path: first, Name: "first"})

	setRoots := func(roots ...protocol.Root) {
		t.Helper()
		if err := client.conn.Notify(protocol.MethodWorkspaceSetRoots, protocol.SetRootsParams{Roots: roots}); err != nil {
			t.Fatal(err)
		}
	}
	setRoots(protocol.Root{ID: "a", Path: first, Name: "first"}, protocol.Root{ID: "b", Path: second, Name: "second"})
	waitUntil(t, "the added folder to be searchable", func() bool {
		got, _ := paths(t, client, "type:code needle")
		return slices.Equal(got, []string{"a/one.txt:1", "b/two.txt:1"})
	})
	setRoots(protocol.Root{ID: "b", Path: second, Name: "second"})
	waitUntil(t, "the removed folder to leave the results", func() bool {
		got, _ := paths(t, client, "type:code needle")
		return slices.Equal(got, []string{"b/two.txt:1"})
	})
}
