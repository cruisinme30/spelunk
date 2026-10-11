package cli

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
	"github.com/cruisinme30/spelunk/daemon/internal/server"
)

// Config is what the extension records in ~/.spelunk/cli.json for the
// command: the settings VS Code builds indexes with, so the reader finds
// them at the same location and re-reads changed files the same way.
type Config struct {
	Settings protocol.Settings `json:"settings"`
}

// ConfigPath is where the extension records Config.
func ConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".spelunk", "cli.json")
}

// LoadConfig reads the Config at path. A missing or unreadable file means
// the defaults: the index at ~/.spelunk/index.
func LoadConfig(path string) Config {
	config := Config{Settings: server.DefaultSettings()}
	data, err := os.ReadFile(path) //nolint:gosec // G304: the command's own config file
	if err != nil {
		return config
	}
	var saved Config
	if json.Unmarshal(data, &saved) == nil && saved.Settings.Location != "" {
		config = saved
	}
	return config
}
