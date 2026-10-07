package steam

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseVDF(t *testing.T) {
	n, err := ParseVDF(strings.NewReader(`
"Root"
{
	// comment
	"Name"		"Quoted \"value\""
	"Child"	{ "Key" "1" }
}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := n.Str("root", "name"); got != `Quoted "value"` {
		t.Errorf("name = %q", got)
	}
	if got := n.Str("Root", "child", "KEY"); got != "1" {
		t.Errorf("nested = %q", got)
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCurrentGame(t *testing.T) {
	root := t.TempDir()
	proc := t.TempDir()
	c := &Client{Root: root, Proc: proc}
	write(t, filepath.Join(root, "steamapps", "appmanifest_100.acf"), `"AppState" { "name" "Old Game" }`)
	write(t, filepath.Join(root, "steamapps", "appmanifest_200.acf"), `"AppState" { "name" "New Game" }`)
	write(t, filepath.Join(root, "userdata", "1", "config", "localconfig.vdf"), `
"UserLocalConfigStore" { "Software" { "Valve" { "Steam" { "apps" {
	"100" { "LastPlayed" "1000" "Playtime" "60" }
	"200" { "LastPlayed" "2000" "Playtime" "90" }
	"300" { "LastPlayed" "9999" }
} } } } }`)
	write(t, filepath.Join(root, "appcache", "librarycache", "200", "library_600x900.jpg"), "x")

	g, ok := c.Current()
	if !ok || g.AppID != 200 || g.Running || g.Name != "New Game" || g.Playtime != 90*time.Minute {
		t.Fatalf("recent game wrong: %+v", g) // 300 is skipped: not installed
	}
	if g.ArtPath == "" {
		t.Error("art not found")
	}

	write(t, filepath.Join(root, "appcache", "librarycache", "100", "abc123", "library_header.jpg"), "x")
	write(t, filepath.Join(root, "appcache", "librarycache", "100", "def456", "library_capsule.jpg"), "x")
	if got := c.ArtPath(100); filepath.Base(got) != "library_capsule.jpg" {
		t.Errorf("hashed layout: got %q, want the portrait capsule", got)
	}

	write(t, filepath.Join(proc, "4242", "cmdline"), "reaper\x00SteamLaunch\x00AppId=100\x00--\x00game.exe\x00")
	g, ok = c.Current()
	if !ok || g.AppID != 100 || !g.Running || g.Name != "Old Game" {
		t.Fatalf("running game wrong: %+v", g)
	}
}
