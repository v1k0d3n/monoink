package screens

import (
	"image"
	"image/color"
	"math"
	"time"

	"github.com/v1k0d3n/monoink/backend/internal/steam"
	"github.com/v1k0d3n/monoink/backend/internal/sysinfo"
	"github.com/v1k0d3n/monoink/backend/internal/weather"
)

// SampleData returns fixed, fictional data for tests and the documentation
// previews (`make previews`). It must stay deterministic: committed preview
// images are compared byte-for-byte in CI.
func SampleData() *Data {
	now := time.Date(2026, 10, 7, 14, 5, 0, 0, time.UTC)
	hist := make([]sysinfo.Snapshot, 30)
	for i := range hist {
		hist[i] = sysinfo.Snapshot{CPU: float64(20 + i*2), GPU: float64(60 - i), Mem: 48 + 6*math.Sin(float64(i)/5)}
	}
	art := sampleArt(600, 900)
	progress := 0.42
	return &Data{
		Now: now, Battery: 87, YearProgress: true, FirstWeekday: time.Monday,
		Weather: &weather.Report{Fetched: now.Add(-20 * time.Minute), Temp: 18.4, FeelsLike: 17, Humidity: 60, Wind: 12, Code: 2, IsDay: true,
			Days: []weather.Day{
				{Date: now, Code: 2, Max: 21, Min: 11, Precip: 10},
				{Date: now.AddDate(0, 0, 1), Code: 63, Max: 17, Min: 10, Precip: 80},
				{Date: now.AddDate(0, 0, 2), Code: 95, Max: 16, Min: 9, Precip: 60},
				{Date: now.AddDate(0, 0, 3), Code: 73, Max: 3, Min: -2},
				{Date: now.AddDate(0, 0, 4), Code: 45, Max: 12, Min: 6},
			}},
		Place:       "Example City",
		Sys:         sysinfo.Snapshot{CPU: 37, GPU: 81, Mem: 54, MemUsed: 8.6e9, MemTotal: 16e9, CPUTemp: 61, GPUTemp: 70, Uptime: 5 * time.Hour},
		History:     hist,
		Game:        &steam.Game{AppID: 1, Name: "An Exceptionally Long Game Title: Definitive Edition", Running: true, Playtime: 125 * time.Hour, Started: now.Add(-83 * time.Minute)},
		GameArt:     art,
		Controllers: 1,
		Photo:       sampleArt(900, 600),
		Card:        &Card{Provider: "example", Title: "Build pipeline", Lines: []string{"main: passing", "release: 3 jobs queued"}, Progress: &progress, Updated: now},
	}
}

// sampleArt draws a simple synthetic landscape (sky gradient, sun,
// mountain ridges) so previews show how pictures dither, without using
// anyone's real artwork.
func sampleArt(w, h int) *image.Gray {
	img := image.NewGray(image.Rect(0, 0, w, h))
	fw, fh := float64(w), float64(h)
	sunX, sunY, sunR := fw*0.68, fh*0.32, math.Min(fw, fh)*0.12
	for y := 0; y < h; y++ {
		fy := float64(y)
		for x := 0; x < w; x++ {
			fx := float64(x)
			v := 235 - 90*fy/fh // sky darkens toward the horizon
			if math.Hypot(fx-sunX, fy-sunY) < sunR {
				v = 250
			}
			far := fh*0.55 + fh*0.08*math.Sin(fx/fw*7) + fh*0.03*math.Sin(fx/fw*23)
			near := fh*0.70 + fh*0.10*math.Sin(fx/fw*4+1) + fh*0.02*math.Sin(fx/fw*31)
			switch {
			case fy > near:
				v = 40 + 30*(fy-near)/fh
			case fy > far:
				v = 110
			}
			img.SetGray(x, y, color.Gray{Y: uint8(v)})
		}
	}
	return img
}
