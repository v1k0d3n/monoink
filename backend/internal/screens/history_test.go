package screens

import (
	"image"
	"testing"
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
