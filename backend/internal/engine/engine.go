// Package engine decides what the faceplate shows and when. It gathers
// data, renders the active screen, and hands changed frames to the
// connection manager.
package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "golang.org/x/image/webp"

	"github.com/v1k0d3n/monoink/backend/internal/conn"
	"github.com/v1k0d3n/monoink/backend/internal/render"
	"github.com/v1k0d3n/monoink/backend/internal/screens"
	"github.com/v1k0d3n/monoink/backend/internal/settings"
	"github.com/v1k0d3n/monoink/backend/internal/steam"
	"github.com/v1k0d3n/monoink/backend/internal/sysinfo"
	"github.com/v1k0d3n/monoink/backend/internal/weather"
)

// Engine coordinates data sources, rendering and sending.
type Engine struct {
	Store   *settings.Store
	Conn    *conn.Manager
	Sys     *sysinfo.Sampler
	Steam   *steam.Client // nil when Steam isn't installed
	Weather *weather.Client
	DataDir string // for cached cover art
	Log     *slog.Logger

	mu         sync.Mutex
	report     *weather.Report
	wxMsg      string
	wxKey      string
	wxAt       time.Time
	game       *steam.Game
	gameAt     time.Time
	art        map[int]image.Image
	cards      map[string]*screens.Card
	pending    map[string]*screens.Card
	photoIdx   int
	photoAt    time.Time
	screensKey string // pinned screen + rotation, to detect changes
	override   string // screen shown until overrideTo
	overrideTo time.Time
	current    string
	renderAt   time.Time
	lastFrame  []byte
	force      bool
	sending    bool

	kick chan struct{}
}

func (e *Engine) init() {
	e.art = map[int]image.Image{}
	e.cards = map[string]*screens.Card{}
	e.pending = map[string]*screens.Card{}
	e.kick = make(chan struct{}, 1)
	if e.Log == nil {
		e.Log = slog.Default()
	}
}

// New prepares e (with its exported fields set) for Run.
func New(e *Engine) *Engine {
	e.init()
	return e
}

// rememberDevice pins the first display we connect to, so a neighbour's
// faceplate is never picked up later. Scanning in the UI changes it.
func (e *Engine) rememberDevice(addr string) {
	if e.Store.Get().DeviceAddress != "" || addr == "" {
		return
	}
	if _, err := e.Store.Update(func(s *settings.Settings) { s.DeviceAddress = addr }); err == nil {
		e.Log.Info("remembering display", "address", addr)
		e.ApplySettings()
	}
}

// ApplySettings pushes settings to the manager and re-evaluates the
// display. Changing which screens are shown cancels a "Next" override so
// the user's choice takes effect immediately.
func (e *Engine) ApplySettings() {
	s := e.Store.Get()
	if e.Conn != nil {
		e.Conn.Configure(conn.Config{Enabled: s.Enabled, Address: s.DeviceAddress, KeepConnected: s.KeepConnected})
	}
	key := s.PinnedScreen + "|" + strings.Join(s.Screens, ",")
	e.mu.Lock()
	if key != e.screensKey {
		e.screensKey, e.override = key, ""
	}
	e.force = true
	e.mu.Unlock()
	e.Kick()
}

// Kick re-evaluates the display soon.
func (e *Engine) Kick() {
	select {
	case e.kick <- struct{}{}:
	default:
	}
}

// Refresh re-reads data sources and re-sends the current screen even if
// the picture hasn't changed. Fresh weather arrives asynchronously and
// triggers another redraw only if it changes the picture.
func (e *Engine) Refresh() {
	e.mu.Lock()
	e.force = true
	e.lastFrame = nil
	e.gameAt = time.Time{}
	clear(e.art)
	e.mu.Unlock()
	e.Sys.Sample()
	go e.RefreshWeather(context.Background())
	e.Kick()
}

// Show displays a screen for one rotation period, then rotation resumes.
// It has no effect while a screen is pinned.
func (e *Engine) Show(id string) error {
	if _, ok := screens.All[id]; !ok {
		return fmt.Errorf("unknown screen %q", id)
	}
	s := e.Store.Get()
	e.mu.Lock()
	e.override = id
	e.overrideTo = time.Now().Add(time.Duration(s.RotateMinutes) * time.Minute)
	e.mu.Unlock()
	e.Kick()
	return nil
}

// Next advances the rotation to the screen after the one on display.
func (e *Engine) Next() (string, error) {
	s := e.Store.Get()
	if s.PinnedScreen != "" {
		return "", errors.New("a screen is pinned; choose Rotate to cycle screens")
	}
	if len(s.Screens) == 0 {
		return "", errors.New("no screens are enabled for rotation")
	}
	cur := e.Current()
	next := s.Screens[0]
	for i, id := range s.Screens {
		if id == cur {
			next = s.Screens[(i+1)%len(s.Screens)]
		}
	}
	return next, e.Show(next)
}

// Current returns the screen on the panel (or about to be).
func (e *Engine) Current() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.current
}

// Run blocks until ctx is cancelled and the connection manager has
// released the display.
func (e *Engine) Run(ctx context.Context) {
	e.Sys.Sample() // prime CPU counters before the first render
	e.Conn.OnConnected = e.rememberDevice
	e.ApplySettings()
	connDone := make(chan struct{})
	go func() { e.Conn.Run(ctx); close(connDone) }()
	defer func() { <-connDone }()
	go e.sampleLoop(ctx)
	go e.weatherLoop(ctx)

	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		e.step(ctx)
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		case <-e.kick:
		}
	}
}

func (e *Engine) sampleLoop(ctx context.Context) {
	// CPU load is a delta between two samples; take a quick first pair so
	// the first frame after startup has a value instead of "—".
	e.Sys.Sample()
	time.Sleep(500 * time.Millisecond)
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	for {
		e.Sys.Sample()
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// ---- screen selection ------------------------------------------------------

// activeScreen picks the screen for now. Precedence: a pinned screen
// (the user's explicit choice) > "Next screen" override > the game while
// one is running > a fresh provider card > the rotation.
func (e *Engine) activeScreen(now time.Time, s settings.Settings, playing bool) string {
	if s.PinnedScreen != "" {
		return s.PinnedScreen
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.override != "" && now.Before(e.overrideTo) {
		return e.override
	}
	e.override = ""
	if playing && s.GameWhilePlaying {
		return "game"
	}
	if s.CardsPreempt {
		if c := e.freshCardLocked(now); c != nil && now.Sub(c.Updated) < 2*time.Minute {
			return "card"
		}
	}
	return rotation(now, s)
}

func rotation(now time.Time, s settings.Settings) string {
	if len(s.Screens) == 0 {
		return "clock"
	}
	slot := now.Unix() / int64(s.RotateMinutes*60)
	return s.Screens[int(slot%int64(len(s.Screens)))]
}

// due reports whether the screen's content has moved on since last render.
func due(cadence time.Duration, last, now time.Time) bool {
	if last.IsZero() {
		return true
	}
	if cadence <= 0 {
		return false
	}
	return now.Truncate(cadence).After(last.Truncate(cadence))
}

func (e *Engine) step(ctx context.Context) {
	s := e.Store.Get()
	if !s.Enabled {
		return
	}
	now := time.Now()
	playing := false
	if s.GameWhilePlaying && s.PinnedScreen == "" {
		if g := e.currentGame(now); g != nil && g.Running {
			playing = true
		}
	}
	id := e.activeScreen(now, s, playing)
	scr := screens.All[id]

	e.mu.Lock()
	if e.sending {
		e.mu.Unlock()
		return
	}
	photoDue := id == "photo" && now.Sub(e.photoAt) >= time.Duration(s.PhotoMinutes)*time.Minute
	need := e.force || id != e.current || due(scr.Cadence, e.renderAt, now) || photoDue
	e.mu.Unlock()
	if !need {
		return
	}

	// Don't hammer a display that is off or out of range: wait for the
	// manager's retry time before attempting another send.
	st := e.Conn.Status()
	if st.State == conn.Off || (st.State == conn.Backoff && now.Before(st.RetryAt)) ||
		st.State == conn.Searching || st.State == conn.Connecting {
		return
	}

	if photoDue {
		e.mu.Lock()
		e.photoIdx++
		e.photoAt = now
		e.mu.Unlock()
	}
	data := e.snapshot(ctx, now, s, id)
	frame := render.Pack(scr.Render(data).Gray)

	e.mu.Lock()
	e.current, e.renderAt, e.force = id, now, false
	if bytes.Equal(frame, e.lastFrame) {
		e.mu.Unlock()
		return
	}
	e.sending = true
	e.mu.Unlock()

	go func() {
		sctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		start := time.Now()
		err := e.Conn.Send(sctx, frame)
		e.mu.Lock()
		e.sending = false
		if err == nil {
			e.lastFrame = frame
		} else {
			e.renderAt = time.Time{} // retry once the manager allows it
		}
		e.mu.Unlock()
		if err != nil && !errors.Is(err, conn.ErrBusy) {
			e.Log.Warn("frame not delivered", "screen", id, "err", err)
		} else if err == nil {
			e.Log.Info("frame delivered", "screen", id, "took", time.Since(start).Round(time.Millisecond))
		}
	}()
}

// Preview renders a screen without sending it.
func (e *Engine) Preview(ctx context.Context, id string) (*image.Gray, error) {
	scr, ok := screens.All[id]
	if !ok {
		return nil, fmt.Errorf("unknown screen %q", id)
	}
	data := e.snapshot(ctx, time.Now(), e.Store.Get(), id)
	img := scr.Render(data).Gray
	// Show exactly what the panel receives (1-bit).
	for i, v := range img.Pix {
		if v >= 128 {
			img.Pix[i] = 255
		} else {
			img.Pix[i] = 0
		}
	}
	return img, nil
}

// ---- data ------------------------------------------------------------------

func (e *Engine) snapshot(ctx context.Context, now time.Time, s settings.Settings, id string) *screens.Data {
	d := &screens.Data{Now: now, Clock24h: s.Clock24h, WeekStartsSun: s.WeekStartsSun, Battery: -1, Place: s.Weather.Place}
	if st := e.Conn.Status(); st.Info != nil {
		d.Battery = st.Info.Battery
	}
	d.Sys, d.History = e.Sys.Last()

	e.mu.Lock()
	d.Weather, d.WeatherMsg = e.report, e.wxMsg
	d.Card = e.freshCardLocked(now)
	e.mu.Unlock()
	if !s.Weather.Configured() {
		d.Weather, d.WeatherMsg = nil, "Choose a location in the plugin settings."
	}

	if id == "game" || id == "dashboard" {
		if g := e.currentGame(now); g != nil {
			d.Game = g
			d.GameArt = e.gameArt(ctx, g, s.AllowSteamCDN)
		}
	}
	if id == "photo" {
		d.Photo, d.PhotoName = e.photo(s)
	}
	return d
}

func (e *Engine) currentGame(now time.Time) *steam.Game {
	if e.Steam == nil {
		return nil
	}
	e.mu.Lock()
	if now.Sub(e.gameAt) < 30*time.Second {
		g := e.game
		e.mu.Unlock()
		return g
	}
	e.mu.Unlock()
	var g *steam.Game
	if cur, ok := e.Steam.Current(); ok {
		g = &cur
	}
	e.mu.Lock()
	e.game, e.gameAt = g, now
	e.mu.Unlock()
	return g
}

func (e *Engine) gameArt(ctx context.Context, g *steam.Game, allowCDN bool) image.Image {
	e.mu.Lock()
	if img, ok := e.art[g.AppID]; ok {
		e.mu.Unlock()
		return img
	}
	e.mu.Unlock()

	path := g.ArtPath
	cached := filepath.Join(e.DataDir, "art", strconv.Itoa(g.AppID)+".jpg")
	if path == "" {
		if _, err := os.Stat(cached); err == nil {
			path = cached
		} else if allowCDN {
			if err := e.fetchArt(ctx, g.AppID, cached); err == nil {
				path = cached
			} else {
				e.Log.Info("cover art download failed", "app", g.AppID, "err", err)
			}
		}
	}
	img, _ := loadImage(path)
	e.mu.Lock()
	if len(e.art) > 32 {
		clear(e.art)
	}
	e.art[g.AppID] = img // cache misses too, until restart or cache reset
	e.mu.Unlock()
	return img
}

// fetchArt downloads public cover art from Steam's CDN (opt-in only).
func (e *Engine) fetchArt(ctx context.Context, appID int, dst string) error {
	url := fmt.Sprintf("https://cdn.akamai.steamstatic.com/steam/apps/%d/library_600x900.jpg", appID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(io.LimitReader(resp.Body, 8<<20)); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	return os.WriteFile(dst, buf.Bytes(), 0o600)
}

var photoExt = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true}

// Photos lists usable images in the configured folder (not recursive).
func Photos(dir string) []string {
	if dir == "" {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, ent := range entries {
		if ent.Type().IsRegular() && photoExt[strings.ToLower(filepath.Ext(ent.Name()))] {
			out = append(out, filepath.Join(dir, ent.Name()))
		}
	}
	sort.Strings(out)
	return out
}

func (e *Engine) photo(s settings.Settings) (image.Image, string) {
	list := Photos(s.PhotoDir)
	if len(list) == 0 {
		return nil, ""
	}
	e.mu.Lock()
	start := e.photoIdx
	e.mu.Unlock()
	for i := range list {
		p := list[(start+i)%len(list)]
		if img, err := loadImage(p); err == nil {
			return img, filepath.Base(p)
		}
	}
	return nil, ""
}

// loadImage decodes an image, refusing absurd sizes.
func loadImage(path string) (image.Image, error) {
	if path == "" {
		return nil, errors.New("no path")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return nil, err
	}
	if cfg.Width*cfg.Height > 40_000_000 {
		return nil, fmt.Errorf("%s is too large (%dx%d)", filepath.Base(path), cfg.Width, cfg.Height)
	}
	if _, err := f.Seek(0, 0); err != nil {
		return nil, err
	}
	img, _, err := image.Decode(f)
	return img, err
}

// ---- weather ---------------------------------------------------------------

func (e *Engine) weatherLoop(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		e.refreshWeather(ctx, false)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// RefreshWeather fetches now (e.g. after the location changed).
func (e *Engine) RefreshWeather(ctx context.Context) { e.refreshWeather(ctx, true) }

func (e *Engine) refreshWeather(ctx context.Context, force bool) {
	s := e.Store.Get()
	w := s.Weather
	if !w.Configured() || !s.Enabled || e.Weather == nil {
		e.mu.Lock()
		e.report, e.wxKey = nil, ""
		e.mu.Unlock()
		return
	}
	key := fmt.Sprintf("%.4f,%.4f,%v", w.Latitude, w.Longitude, w.Imperial)
	e.mu.Lock()
	fresh := key == e.wxKey && time.Since(e.wxAt) < 30*time.Minute
	e.mu.Unlock()
	if fresh && !force {
		return
	}
	rctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	r, err := e.Weather.Forecast(rctx, w.Latitude, w.Longitude, w.Imperial)
	e.mu.Lock()
	e.wxAt, e.wxKey = time.Now(), key
	if err != nil {
		e.wxMsg = "Weather unavailable: check the network connection."
		e.Log.Info("weather fetch failed", "err", err)
		// Keep a stale report for up to 6 hours rather than blanking it.
		if e.report != nil && time.Since(e.report.Fetched) > 6*time.Hour {
			e.report = nil
		}
		e.wxAt = time.Now().Add(-25 * time.Minute) // retry in ~5 minutes
	} else {
		e.report, e.wxMsg = r, ""
	}
	e.mu.Unlock()
	e.Kick()
}

// ---- provider cards --------------------------------------------------------

var providerID = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,47}$`)

// ErrPending means the provider is not yet approved; the card is held.
var ErrPending = errors.New("provider is awaiting approval in the plugin settings")

// CardInput is what a provider submits.
type CardInput struct {
	ID       string   `json:"id"`
	Name     string   `json:"name,omitempty"`
	Title    string   `json:"title"`
	Lines    []string `json:"lines"`
	Progress *float64 `json:"progress,omitempty"`
	TTL      int      `json:"ttl_seconds,omitempty"`
}

func clip(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) > n {
		r = r[:n]
	}
	return string(r)
}

// PushCard validates and stores a card from a provider.
func (e *Engine) PushCard(in CardInput) error {
	if !providerID.MatchString(in.ID) {
		return errors.New("id must be 1-48 chars of a-z, 0-9, '.', '_' or '-'")
	}
	ttl := time.Duration(in.TTL) * time.Second
	if ttl <= 0 {
		ttl = time.Hour
	}
	ttl = min(ttl, 24*time.Hour)
	now := time.Now()
	card := &screens.Card{Provider: clip(in.Name, 32), Title: clip(in.Title, 80), Updated: now, Expires: now.Add(ttl)}
	if card.Provider == "" {
		card.Provider = in.ID
	}
	for i, l := range in.Lines {
		if i == 8 {
			break
		}
		card.Lines = append(card.Lines, clip(l, 100))
	}
	if in.Progress != nil {
		p := max(0, min(1, *in.Progress))
		card.Progress = &p
	}

	approved := e.Store.Get().Providers[in.ID].Approved
	e.mu.Lock()
	if approved {
		e.cards[in.ID] = card
	} else {
		e.pending[in.ID] = card
	}
	e.mu.Unlock()
	if !approved {
		return ErrPending
	}
	e.Kick()
	return nil
}

func (e *Engine) freshCardLocked(now time.Time) *screens.Card {
	var best *screens.Card
	for id, c := range e.cards {
		if now.After(c.Expires) {
			delete(e.cards, id)
			continue
		}
		if best == nil || c.Updated.After(best.Updated) {
			best = c
		}
	}
	return best
}

// ProviderInfo describes a provider for the UI.
type ProviderInfo struct {
	ID       string        `json:"id"`
	Name     string        `json:"name"`
	Approved bool          `json:"approved"`
	Pending  bool          `json:"pending"`
	Card     *screens.Card `json:"card,omitempty"`
}

// Providers lists approved and pending providers.
func (e *Engine) Providers() []ProviderInfo {
	s := e.Store.Get()
	e.mu.Lock()
	defer e.mu.Unlock()
	byID := map[string]*ProviderInfo{}
	for id, p := range s.Providers {
		byID[id] = &ProviderInfo{ID: id, Name: p.Name, Approved: p.Approved, Card: e.cards[id]}
	}
	for id, c := range e.pending {
		if _, ok := byID[id]; !ok {
			byID[id] = &ProviderInfo{ID: id, Name: c.Provider, Pending: true, Card: c}
		}
	}
	out := make([]ProviderInfo, 0, len(byID))
	for _, p := range byID {
		out = append(out, *p)
	}
	slices.SortFunc(out, func(a, b ProviderInfo) int { return strings.Compare(a.ID, b.ID) })
	return out
}

// SetProvider approves or revokes a provider.
func (e *Engine) SetProvider(id string, approve bool) error {
	if !providerID.MatchString(id) {
		return errors.New("invalid provider id")
	}
	e.mu.Lock()
	pending := e.pending[id]
	e.mu.Unlock()
	_, err := e.Store.Update(func(s *settings.Settings) {
		if approve {
			name := s.Providers[id].Name
			if pending != nil {
				name = pending.Provider
			}
			s.Providers[id] = settings.Provider{Approved: true, Name: name}
		} else {
			delete(s.Providers, id)
		}
	})
	if err != nil {
		return err
	}
	e.mu.Lock()
	delete(e.pending, id)
	if approve && pending != nil {
		e.cards[id] = pending
	}
	if !approve {
		delete(e.cards, id)
	}
	e.mu.Unlock()
	e.Kick()
	return nil
}
