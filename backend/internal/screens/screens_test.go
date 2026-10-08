package screens

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/v1k0d3n/monoink/backend/internal/sysinfo"
)

func sample() *Data { return SampleData() }

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
		{"empty", &Data{Now: time.Date(2026, 2, 1, 9, 0, 0, 0, time.UTC), Battery: -1, FirstWeekday: time.Sunday, Clock24h: true,
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
