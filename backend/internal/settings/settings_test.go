package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenCreatesDefaultsAndPersists(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := st.Get()
	if !got.Enabled || got.RotateMinutes != 10 || len(got.WebUI.Token) != 48 {
		t.Fatalf("unexpected defaults: %+v", got)
	}
	if _, err := st.Update(func(s *Settings) {
		s.Screens = []string{"clock", "bogus", "clock", "weather"}
		s.PinnedScreen = "nope"
		s.RotateMinutes = -1
	}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, "settings.json"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("settings file mode: %v %v", info, err)
	}

	again, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := again.Get()
	if len(s.Screens) != 2 || s.Screens[0] != "clock" || s.Screens[1] != "weather" {
		t.Errorf("screens not normalized: %v", s.Screens)
	}
	if s.PinnedScreen != "" || s.RotateMinutes != 10 {
		t.Errorf("invalid values kept: %+v", s)
	}
	if s.WebUI.Token != got.WebUI.Token {
		t.Error("token regenerated on reload")
	}
}

func TestCorruptFileIsSetAside(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "settings.json"), []byte("{nope"), 0o600)
	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Get().Enabled {
		t.Error("defaults not applied")
	}
	if _, err := os.Stat(filepath.Join(dir, "settings.json.invalid")); err != nil {
		t.Error("corrupt file not preserved")
	}
}

func TestEmptyScreensEncodeAsArray(t *testing.T) {
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	got, _ := st.Update(func(s *Settings) { s.Screens = nil })
	b, _ := json.Marshal(got)
	if !strings.Contains(string(b), `"screens":[]`) {
		t.Fatalf("screens must encode as an empty array: %s", b)
	}
	if b, _ := json.Marshal(st.Get()); !strings.Contains(string(b), `"screens":[]`) {
		t.Fatalf("Get must not return nil screens: %s", b)
	}
}
