package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestMCPHelpAndUnknownOptionsDoNotStartServer(t *testing.T) {
	for _, test := range []struct {
		args []string
		code int
	}{
		{[]string{"mcp", "--help"}, exitFound},
		{[]string{"mcp", "-h"}, exitFound},
		{[]string{"mcp", "help"}, exitFound},
		{[]string{"mcp", "unknown"}, exitError},
		{[]string{"mcp", "--inn", "/repo"}, exitError},
		{[]string{"mcp", "--in"}, exitError},
		{[]string{"mcp", "config", "--unknown"}, exitError},
	} {
		var out, errOut bytes.Buffer
		code := Main(t.Context(), Env{Args: test.args, Stdin: unreadableMCPInput{}, Stdout: &out, Stderr: &errOut})
		if code != test.code {
			t.Errorf("%v exit=%d stdout=%q stderr=%q", test.args, code, out.String(), errOut.String())
		}
		if code == exitFound && !strings.Contains(out.String(), "spelunk mcp config") {
			t.Errorf("help omitted setup: %q", out.String())
		}
	}
}

type unreadableMCPInput struct{}

func (unreadableMCPInput) Read([]byte) (int, error) {
	panic("help and setup must not read MCP input")
}

func TestMCPConfigPreservesPathsAndIsRepeatable(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repo with spaces")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	var previous string
	for range 2 {
		var out, errOut bytes.Buffer
		code := Main(t.Context(), Env{Args: []string{"mcp", "config", "--in", "repo with spaces"}, Cwd: filepath.Dir(root), Stdin: unreadableMCPInput{}, Stdout: &out, Stderr: &errOut})
		if code != exitFound || errOut.Len() != 0 {
			t.Fatalf("config exit=%d stderr=%q", code, errOut.String())
		}
		var config struct {
			Servers map[string]struct {
				Command string   `json:"command"`
				Args    []string `json:"args"`
			} `json:"mcpServers"`
		}
		if err := json.Unmarshal(out.Bytes(), &config); err != nil {
			t.Fatal(err)
		}
		entry := config.Servers["spelunk"]
		if !filepath.IsAbs(entry.Command) || !reflect.DeepEqual(entry.Args, []string{"mcp", "--in", root}) {
			t.Fatalf("config = %+v", entry)
		}
		if previous != "" && previous != out.String() {
			t.Error("same setup generated different configuration")
		}
		previous = out.String()
	}
	// JSON encoding must preserve these characters as argv data, never add
	// shell quoting that becomes part of the command or folder name.
	const executable = `/Program Files/Spelunk "dev"/spelunk`
	const folder = "/repo/quoted\"path\\tail"
	encoded, err := json.Marshal(mcpConfig(executable, folder))
	if err != nil {
		t.Fatal(err)
	}
	var roundTrip map[string]map[string]struct {
		Command string   `json:"command"`
		Args    []string `json:"args"`
	}
	if err := json.Unmarshal(encoded, &roundTrip); err != nil {
		t.Fatal(err)
	}
	entry := roundTrip["mcpServers"]["spelunk"]
	if entry.Command != executable || !reflect.DeepEqual(entry.Args, []string{"mcp", "--in", folder}) {
		t.Errorf("JSON changed argv: %+v", entry)
	}
}

func TestMCPSetupAllowsDirectoryWithoutIndexAndRejectsMissingPath(t *testing.T) {
	root := t.TempDir()
	if folder, err := mcpFolder([]string{"--in", root}, root); err != nil || folder != root {
		t.Errorf("folder without index = %q, %v", folder, err)
	}
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{file, filepath.Join(root, "missing"), ""} {
		if _, err := mcpFolder([]string{"--in", path}, root); err == nil {
			t.Errorf("invalid folder %q accepted", path)
		}
	}
	if folder, err := mcpFolder(nil, root); err != nil || folder != "" {
		t.Errorf("default config should follow the client's folder: %q, %v", folder, err)
	}
}
