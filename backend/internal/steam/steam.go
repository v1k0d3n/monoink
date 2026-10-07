// Package steam reads local Steam client files to describe the current or
// most recent game. It never contacts the network.
package steam

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Game describes one Steam app as seen locally.
type Game struct {
	AppID      int
	Name       string
	Running    bool
	Playtime   time.Duration // total
	LastPlayed time.Time
	ArtPath    string // local portrait cover, if cached
}

// Client locates Steam's files under a home directory.
type Client struct {
	Root string // e.g. ~/.local/share/Steam
	Proc string // procfs mount, "/proc" outside tests
}

// New finds the Steam root for home, or returns nil if Steam isn't present.
func New(home string) *Client {
	for _, p := range []string{".local/share/Steam", ".steam/steam"} {
		root := filepath.Join(home, p)
		if fi, err := os.Stat(filepath.Join(root, "steamapps")); err == nil && fi.IsDir() {
			return &Client{Root: root, Proc: "/proc"}
		}
	}
	return nil
}

var appIDArg = regexp.MustCompile(`(?:^|\x00)AppId=(\d+)(?:\x00|$)`)

// RunningAppID returns the app launched through Steam's reaper, if any.
func (c *Client) RunningAppID() (int, bool) {
	entries, err := os.ReadDir(c.Proc)
	if err != nil {
		return 0, false
	}
	for _, e := range entries {
		if e.Name()[0] < '0' || e.Name()[0] > '9' {
			continue
		}
		cmd, err := os.ReadFile(filepath.Join(c.Proc, e.Name(), "cmdline"))
		if err != nil || !bytes.Contains(cmd, []byte("SteamLaunch")) {
			continue
		}
		if m := appIDArg.FindSubmatch(cmd); m != nil {
			if id, err := strconv.Atoi(string(m[1])); err == nil && id > 0 {
				return id, true
			}
		}
	}
	return 0, false
}

// libraries returns every steamapps directory.
func (c *Client) libraries() []string {
	dirs := []string{filepath.Join(c.Root, "steamapps")}
	f, err := os.Open(filepath.Join(c.Root, "steamapps", "libraryfolders.vdf"))
	if err != nil {
		return dirs
	}
	defer f.Close()
	root, err := ParseVDF(f)
	if err != nil {
		return dirs
	}
	for _, v := range root.Child("libraryfolders") {
		if lib, ok := v.(Node); ok {
			if p := lib.Str("path"); p != "" {
				d := filepath.Join(p, "steamapps")
				if d != dirs[0] {
					dirs = append(dirs, d)
				}
			}
		}
	}
	return dirs
}

// Name returns the installed app's name from its manifest.
func (c *Client) Name(appID int) string {
	for _, lib := range c.libraries() {
		f, err := os.Open(filepath.Join(lib, "appmanifest_"+strconv.Itoa(appID)+".acf"))
		if err != nil {
			continue
		}
		n, err := ParseVDF(f)
		f.Close()
		if err == nil {
			if name := n.Str("AppState", "name"); name != "" {
				return name
			}
		}
	}
	return ""
}

// installed returns the app IDs that have a manifest in any library.
func (c *Client) installed() map[int]bool {
	ids := map[int]bool{}
	for _, lib := range c.libraries() {
		matches, _ := filepath.Glob(filepath.Join(lib, "appmanifest_*.acf"))
		for _, m := range matches {
			base := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(m), "appmanifest_"), ".acf")
			if id, err := strconv.Atoi(base); err == nil {
				ids[id] = true
			}
		}
	}
	return ids
}

// localConfig loads the most recently written per-user localconfig.vdf.
func (c *Client) localConfig() Node {
	matches, _ := filepath.Glob(filepath.Join(c.Root, "userdata", "*", "config", "localconfig.vdf"))
	sort.Slice(matches, func(i, j int) bool { return mtime(matches[i]).After(mtime(matches[j])) })
	for _, m := range matches {
		f, err := os.Open(m)
		if err != nil {
			continue
		}
		n, err := ParseVDF(f)
		f.Close()
		if err == nil {
			return n.Child("UserLocalConfigStore", "Software", "Valve", "Steam", "apps")
		}
	}
	return nil
}

func mtime(p string) time.Time {
	fi, err := os.Stat(p)
	if err != nil {
		return time.Time{}
	}
	return fi.ModTime()
}

// ArtPath finds a cached portrait cover, falling back to the wide header.
// Steam has used several layouts: flat "<id>_library_600x900.jpg", a
// per-app folder, and (current) per-app folders with hashed subfolders
// where the portrait cover is named library_capsule.jpg.
func (c *Client) ArtPath(appID int) string {
	id := strconv.Itoa(appID)
	cache := filepath.Join(c.Root, "appcache", "librarycache")
	dir := filepath.Join(cache, id)
	for _, name := range []string{"library_600x900.jpg", "library_capsule.jpg", "header.jpg", "library_header.jpg"} {
		candidates := []string{filepath.Join(dir, name), filepath.Join(cache, id+"_"+name)}
		nested, _ := filepath.Glob(filepath.Join(dir, "*", name))
		for _, p := range append(candidates, nested...) {
			if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() {
				return p
			}
		}
	}
	return ""
}

// Current returns the running game or, failing that, the most recently
// played one. ok is false if nothing is known.
func (c *Client) Current() (Game, bool) {
	apps := c.localConfig()
	running, isRunning := c.RunningAppID()
	id := running
	if !isRunning {
		installed := c.installed()
		var newest int64
		for k, v := range apps {
			n, ok := v.(Node)
			if !ok {
				continue
			}
			lp, _ := strconv.ParseInt(n.Str("LastPlayed"), 10, 64)
			appID, err := strconv.Atoi(k)
			if err == nil && lp > newest && installed[appID] {
				newest, id = lp, appID
			}
		}
	}
	if id == 0 {
		return Game{}, false
	}
	g := Game{AppID: id, Running: isRunning, Name: c.Name(id), ArtPath: c.ArtPath(id)}
	if n := apps.Child(strconv.Itoa(id)); n != nil {
		mins, _ := strconv.Atoi(n.Str("Playtime"))
		g.Playtime = time.Duration(mins) * time.Minute
		if lp, _ := strconv.ParseInt(n.Str("LastPlayed"), 10, 64); lp > 0 {
			g.LastPlayed = time.Unix(lp, 0)
		}
	}
	if g.Name == "" {
		g.Name = "App " + strconv.Itoa(id)
	}
	return g, true
}
