// Package locale works out the user's region from local settings (never
// from the network) and the region's conventional first day of the week.
package locale

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Region extracts the territory from a POSIX locale name: "en_US.UTF-8"
// gives "US", "de_DE@euro" gives "DE". "C", "POSIX" and names without a
// territory give "".
func Region(name string) string {
	name, _, _ = strings.Cut(name, ".")
	name, _, _ = strings.Cut(name, "@")
	_, region, ok := strings.Cut(name, "_")
	if !ok || len(region) != 2 {
		return ""
	}
	return strings.ToUpper(region)
}

// FirstWeekday returns the first day of the week customary in region,
// per Unicode CLDR. Unknown or empty regions start on Monday (ISO 8601).
func FirstWeekday(region string) time.Weekday {
	if d, ok := firstDay[strings.ToUpper(region)]; ok {
		return d
	}
	return time.Monday
}

// Source says where a detected region came from, for display.
type Source string

const (
	FromDesktop Source = "desktop region setting"
	FromEnv     Source = "environment"
	FromSystem  Source = "system default"
	FromNone    Source = "none"
)

// Detect finds the user's region for date formatting, in order of how
// specifically it reflects the user's choice:
//  1. KDE's Region & Language setting (~/.config/plasma-localerc, [Formats])
//  2. the LC_ALL, LC_TIME or LANG environment variables
//  3. the system default (/etc/locale.conf)
//
// root is the filesystem root ("/" outside tests); getenv reads the
// environment.
func Detect(root, home string, getenv func(string) string) (string, Source) {
	if r := fromKeyFile(filepath.Join(root, home, ".config", "plasma-localerc"), "Formats"); r != "" {
		return r, FromDesktop
	}
	for _, k := range []string{"LC_ALL", "LC_TIME", "LANG"} {
		if r := Region(getenv(k)); r != "" {
			return r, FromEnv
		}
	}
	if r := fromKeyFile(filepath.Join(root, "etc", "locale.conf"), ""); r != "" {
		return r, FromSystem
	}
	return "", FromNone
}

// fromKeyFile reads LC_TIME (preferred) or LANG from a KEY=value file,
// optionally only within an INI [section].
func fromKeyFile(path, section string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	vals := map[string]string{}
	inSection := section == ""
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "[") {
			inSection = line == "["+section+"]"
			continue
		}
		if !inSection {
			continue
		}
		if k, v, ok := strings.Cut(line, "="); ok {
			vals[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), `"`)
		}
	}
	for _, k := range []string{"LC_TIME", "LANG"} {
		if r := Region(vals[k]); r != "" {
			return r
		}
	}
	return ""
}
