package screens

import (
	"image"
	"testing"
	"time"
)

func countBlack(c *Canvas, r image.Rectangle) int {
	n := 0
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			if c.GrayAt(x, y).Y == 0 {
				n++
			}
		}
	}
	return n
}

func TestPlotAreaShadesOnlyBelowTheCurve(t *testing.T) {
	c := NewCanvas()
	r := image.Rect(0, 0, 290, 100)
	vals := make([]float64, 30)
	for i := range vals {
		vals[i] = 50 // flat at half height: the curve sits at y=50
	}
	plotArea(c, r, vals)
	if n := countBlack(c, image.Rect(0, 0, 290, 49)); n != 0 {
		t.Errorf("%d dots above the curve", n)
	}
	if n := countBlack(c, image.Rect(0, 51, 290, 100)); n == 0 {
		t.Error("no shading below the curve")
	}
}

func TestPlotAreaSkipsUnknownSamples(t *testing.T) {
	c := NewCanvas()
	r := image.Rect(0, 0, 290, 100)
	vals := []float64{50, -1, 50}
	plotArea(c, r, vals)
	if n := countBlack(c, r); n != 0 {
		t.Errorf("unknown sample should break the area, got %d dots", n)
	}
}

func TestHistoryPointClamps(t *testing.T) {
	r := image.Rect(0, 0, 290, 100)
	if _, y := historyPoint(r, 29, 30, 150); y != 0 {
		t.Errorf("values above 100%% must clamp to the top, got y=%v", y)
	}
	if x, y := historyPoint(r, 29, 30, 0); x != 290 || y != 100 {
		t.Errorf("newest zero sample should sit bottom-right, got %v,%v", x, y)
	}
}

func TestIdleLinesStayClearOfTheFrame(t *testing.T) {
	d := SampleData()
	for i := range d.History {
		d.History[i].CPU, d.History[i].GPU, d.History[i].Mem = 0, 0, 0
	}
	c := Performance(d)
	// The graph frame's bottom edge is 2px thick at H-44; a 0% line must
	// leave a clear white gap above it.
	frameBottom := H - 44
	gap := image.Rect(W/2, frameBottom-8, W/2+1, frameBottom-2)
	if n := countBlack(c, gap); n != 0 {
		t.Errorf("0%% line touches the frame (%d black pixels in the gap)", n)
	}
	line := image.Rect(W/2, frameBottom-16, W/2+1, frameBottom-8)
	if countBlack(c, line) == 0 {
		t.Error("0% line not drawn above the frame")
	}
}

func TestSessionClock(t *testing.T) {
	for d, want := range map[time.Duration]string{
		0:                            "0:00",
		59 * time.Second:             "0:00",
		83 * time.Minute:             "1:23",
		10*time.Hour + 5*time.Minute: "10:05",
	} {
		if got := sessionClock(d); got != want {
			t.Errorf("sessionClock(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestGameLayouts(t *testing.T) {
	d := SampleData() // running, 83 minutes into the session
	cover := Game(d)
	d.GameTimer = true
	timer := Game(d)
	if countBlack(cover, cover.Bounds()) == countBlack(timer, timer.Bounds()) {
		t.Fatal("timer layout should differ from the cover layout")
	}
	if len(timer.pictures) != 0 {
		t.Error("timer layout shouldn't draw cover art")
	}
	// With no game running there's no session: the timer layout falls back
	// to the cover layout (last played).
	d.Game.Running = false
	if got := Game(d); len(got.pictures) == 0 {
		t.Error("timer layout without a running game should fall back to cover")
	}
}

func TestDashboardGameColumnStaysLeftOfDivider(t *testing.T) {
	d := SampleData()
	c := Dashboard(d)
	// The divider is a 2px line at W/2-1..W/2+1; nothing from the game
	// column may touch the white gutter just left of it.
	gutter := image.Rect(W/2-8, 214, W/2-2, H-20)
	if n := countBlack(c, gutter); n != 0 {
		t.Fatalf("game column overflows into the divider gutter (%d px)", n)
	}
}
