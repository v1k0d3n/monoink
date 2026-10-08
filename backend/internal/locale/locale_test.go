package locale

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRegion(t *testing.T) {
	for in, want := range map[string]string{
		"en_US.UTF-8": "US", "de_DE@euro": "DE", "pt_BR": "BR", "en_gb.utf8": "GB",
		"C": "", "POSIX": "", "": "", "en": "", "C.UTF-8": "",
	} {
		if got := Region(in); got != want {
			t.Errorf("Region(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFirstWeekday(t *testing.T) {
	for region, want := range map[string]time.Weekday{
		"US": time.Sunday, "CA": time.Sunday, "JP": time.Sunday, "BR": time.Sunday,
		"GB": time.Monday, "DE": time.Monday, "FR": time.Monday, "CN": time.Monday,
		"EG": time.Saturday, "IR": time.Saturday,
		"MV": time.Friday,
		"":   time.Monday, "ZZ": time.Monday, "us": time.Sunday,
	} {
		if got := FirstWeekday(region); got != want {
			t.Errorf("FirstWeekday(%q) = %v, want %v", region, got, want)
		}
	}
	if CLDRVersion == "" {
		t.Error("CLDR version missing")
	}
}

func put(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, rel)
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.WriteFile(p, []byte(content), 0o644)
}

func TestDetectPriority(t *testing.T) {
	root := t.TempDir()
	env := map[string]string{"LANG": "de_DE.UTF-8"}
	getenv := func(k string) string { return env[k] }

	put(t, root, "etc/locale.conf", "LANG=fr_FR.UTF-8\n")
	if r, src := Detect(root, "/home/user", getenv); r != "DE" || src != FromEnv {
		t.Errorf("env should beat system default: %s %s", r, src)
	}

	put(t, root, "home/user/.config/plasma-localerc", "[Translations]\nLANGUAGE=ja\n[Formats]\nLANG=en_US.UTF-8\nLC_TIME=en_GB.UTF-8\n")
	if r, src := Detect(root, "/home/user", getenv); r != "GB" || src != FromDesktop {
		t.Errorf("desktop LC_TIME should win: %s %s", r, src)
	}

	env = map[string]string{"LANG": "C"}
	os.Remove(filepath.Join(root, "home/user/.config/plasma-localerc"))
	if r, src := Detect(root, "/home/user", getenv); r != "FR" || src != FromSystem {
		t.Errorf("C locale should fall through to the system default: %s %s", r, src)
	}

	if r, src := Detect(t.TempDir(), "/home/user", func(string) string { return "" }); r != "" || src != FromNone {
		t.Errorf("nothing configured: %s %s", r, src)
	}
}
