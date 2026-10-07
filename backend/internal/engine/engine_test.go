package engine

import (
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/v1k0d3n/monoink/backend/internal/settings"
)

func newTest(t *testing.T) *Engine {
	st, err := settings.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return New(&Engine{Store: st})
}

func TestDue(t *testing.T) {
	base := time.Date(2026, 1, 1, 10, 0, 30, 0, time.UTC)
	if !due(time.Minute, time.Time{}, base) {
		t.Error("first render must be due")
	}
	if due(time.Minute, base, base.Add(20*time.Second)) {
		t.Error("same minute should not be due")
	}
	if !due(time.Minute, base, base.Add(31*time.Second)) {
		t.Error("minute boundary should be due")
	}
	if due(0, base, base.Add(time.Hour)) {
		t.Error("zero cadence is event-driven only")
	}
}

func TestRotationAndOverride(t *testing.T) {
	e := newTest(t)
	e.Store.Update(func(s *settings.Settings) { s.Screens = []string{"clock", "weather"}; s.RotateMinutes = 1 })
	s := e.Store.Get()
	t0 := time.Unix(600, 0) // slot 10 → index 0
	if got := e.activeScreen(t0, s); got != "clock" {
		t.Errorf("slot 10: %s", got)
	}
	if got := e.activeScreen(t0.Add(time.Minute), s); got != "weather" {
		t.Errorf("slot 11: %s", got)
	}
	e.override, e.overrideTo = "game", t0.Add(30*time.Second)
	if got := e.activeScreen(t0, s); got != "game" {
		t.Errorf("override ignored: %s", got)
	}
	if got := e.activeScreen(t0.Add(31*time.Second), s); got != "clock" {
		t.Errorf("override not expired: %s", got)
	}
	s.PinnedScreen = "calendar"
	if got := e.activeScreen(t0, s); got != "calendar" {
		t.Errorf("pin ignored: %s", got)
	}
}

func TestProviderApprovalFlow(t *testing.T) {
	e := newTest(t)
	in := CardInput{ID: "ci-status", Name: "CI", Title: "Builds", Lines: []string{"ok"}, TTL: 60}
	if err := e.PushCard(in); !errors.Is(err, ErrPending) {
		t.Fatalf("unapproved push: %v", err)
	}
	if e.freshCardLocked(time.Now()) != nil {
		t.Fatal("pending card must not be shown")
	}
	ps := e.Providers()
	if len(ps) != 1 || !ps[0].Pending {
		t.Fatalf("pending provider not listed: %+v", ps)
	}
	if err := e.SetProvider("ci-status", true); err != nil {
		t.Fatal(err)
	}
	if c := e.freshCardLocked(time.Now()); c == nil || c.Title != "Builds" {
		t.Fatal("approved card not shown")
	}
	if err := e.PushCard(in); err != nil {
		t.Fatalf("approved push: %v", err)
	}
	e.SetProvider("ci-status", false)
	if e.freshCardLocked(time.Now()) != nil || len(e.Store.Get().Providers) != 0 {
		t.Fatal("revoke did not remove provider")
	}
	if err := e.PushCard(CardInput{ID: "../etc"}); err == nil || errors.Is(err, ErrPending) {
		t.Fatal("bad id accepted")
	}
}

func TestPhotosAndLoad(t *testing.T) {
	dir := t.TempDir()
	img := image.NewGray(image.Rect(0, 0, 4, 4))
	img.SetGray(0, 0, color.Gray{Y: 255})
	for _, n := range []string{"b.png", "a.PNG"} {
		f, _ := os.Create(filepath.Join(dir, n))
		png.Encode(f, img)
		f.Close()
	}
	os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x"), 0o644)
	list := Photos(dir)
	if len(list) != 2 || filepath.Base(list[0]) != "a.PNG" {
		t.Fatalf("photos: %v", list)
	}
	if _, err := loadImage(list[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := loadImage(filepath.Join(dir, "notes.txt")); err == nil {
		t.Fatal("non-image decoded")
	}
}
