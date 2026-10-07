// Package paths resolves where monoinkd keeps settings and sockets.
package paths

import (
	"fmt"
	"os"
	"path/filepath"
)

// SettingsDir: Decky's per-plugin settings dir when run by Decky, otherwise
// $XDG_CONFIG_HOME/monoink.
func SettingsDir() string {
	if d := os.Getenv("DECKY_PLUGIN_SETTINGS_DIR"); d != "" {
		return d
	}
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return filepath.Join(d, "monoink")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "monoink")
}

// DataDir holds caches (e.g. downloaded cover art).
func DataDir() string {
	if d := os.Getenv("DECKY_PLUGIN_RUNTIME_DIR"); d != "" {
		return d
	}
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return filepath.Join(d, "monoink")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "monoink")
}

// RuntimeDir holds the sockets. It is derived from the UID rather than
// $XDG_RUNTIME_DIR because Decky doesn't always pass that variable, and
// providers must find the same path from a normal login session.
func RuntimeDir() string {
	if d := os.Getenv("MONOINK_RUNTIME_DIR"); d != "" {
		return d
	}
	run := fmt.Sprintf("/run/user/%d", os.Getuid())
	if fi, err := os.Stat(run); err == nil && fi.IsDir() {
		return filepath.Join(run, "monoink")
	}
	return filepath.Join(DataDir(), "run")
}

func ControlSocket() string  { return filepath.Join(RuntimeDir(), "control.sock") }
func ProviderSocket() string { return filepath.Join(RuntimeDir(), "providers.sock") }
