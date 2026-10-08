package screens

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/v1k0d3n/monoink/backend/internal/steam"
	"github.com/v1k0d3n/monoink/backend/internal/sysinfo"
	"github.com/v1k0d3n/monoink/backend/internal/weather"
)

func sample() *Data {
	now := time.Date(2026, 10, 7, 14, 5, 0, 0, time.UTC)
	hist := make([]sysinfo.Snapshot, 30)
	for i := range hist {
		hist[i] = sysinfo.Snapshot{CPU: float64(20 + i*2), GPU: float64(60 - i)}
	}
	art := image.NewGray(image.Rect(0, 0, 600, 900))
	for y := 0; y < 900; y++ {
		for x := 0; x < 600; x++ {
			art.SetGray(x, y, color.Gray{Y: uint8((x + y) / 6)})
		}
	}
	progress := 0.42
	return &Data{
		Now: now, Battery: 87, YearProgress: true,
		Weather: &weather.Report{Fetched: now.Add(-20 * time.Minute), Temp: 18.4, FeelsLike: 17, Humidity: 60, Wind: 12, Code: 2, IsDay: true,
			Days: []weather.Day{
				{Date: now, Code: 2, Max: 21, Min: 11, Precip: 10},
				{Date: now.AddDate(0, 0, 1), Code: 63, Max: 17, Min: 10, Precip: 80},
				{Date: now.AddDate(0, 0, 2), Code: 95, Max: 16, Min: 9, Precip: 60},
				{Date: now.AddDate(0, 0, 3), Code: 73, Max: 3, Min: -2},
				{Date: now.AddDate(0, 0, 4), Code: 45, Max: 12, Min: 6},
			}},
		Place:   "Example City",
		Sys:     sysinfo.Snapshot{CPU: 37, GPU: 81, Mem: 54, MemUsed: 8.6e9, MemTotal: 16e9, CPUTemp: 61, GPUTemp: 70, Uptime: 5 * time.Hour},
		History: hist,
		Game:    &steam.Game{AppID: 1, Name: "An Exceptionally Long Game Title: Definitive Edition", Running: true, Playtime: 125 * time.Hour},
		GameArt: art,
		Photo:   art,
		Card:    &Card{Provider: "example", Title: "Build pipeline", Lines: []string{"main: passing", "release: 3 jobs queued"}, Progress: &progress, Updated: now},
	}
}

func TestWrap(t *testing.T) {
	lines := Wrap("one two three four five six seven eight", Regular, 20, 120, 2)
	if len(lines) != 2 || !strings.HasSuffix(lines[1], "…") {
		t.Fatalf("got %q", lines)
	}
	if got := Wrap("short", Regular, 20, 500, 3); len(got) != 1 || got[0] != "short" {
		t.Fatalf("got %q", got)
	}
}

func TestRenderAll(t *testing.T) {
	out := os.Getenv("MONOINK_PREVIEW_DIR")
	for _, variant := range []struct {
		name string
		data *Data
	}{
		{"full", sample()},
		{"empty", &Data{Now: time.Date(2026, 2, 1, 9, 0, 0, 0, time.UTC), Battery: -1, WeekStartsSun: true, Clock24h: true,
			Sys: sysinfo.Snapshot{CPU: -1, GPU: -1, Mem: -1, CPUTemp: -1, GPUTemp: -1}}},
	} {
		for id, s := range All {
			c := s.Render(variant.data)
			if c.Bounds().Dx() != W || c.Bounds().Dy() != H {
				t.Fatalf("%s/%s: wrong size", variant.name, id)
			}
			if out != "" {
				f, err := os.Create(filepath.Join(out, variant.name+"-"+id+".png"))
				if err != nil {
					t.Fatal(err)
				}
				png.Encode(f, c.Gray)
				f.Close()
			}
		}
	}
}

func TestInvertKeepsPictures(t *testing.T) {
	c := NewCanvas()
	art := image.NewGray(image.Rect(0, 0, 10, 10)) // all black
	c.Picture(art, image.Rect(100, 100, 110, 110), true)
	c.Invert()
	if c.GrayAt(0, 0).Y != 0 {
		t.Error("white background should become black")
	}
	if c.GrayAt(105, 105).Y != 0 {
		t.Error("picture must not be inverted")
	}
}
