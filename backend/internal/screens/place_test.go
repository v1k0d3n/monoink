package screens

import (
	"image"
	"testing"
)

func TestPlace(t *testing.T) {
	screen := image.Rect(0, 0, W, H) // 648x480
	cases := []struct {
		name string
		src  image.Point
		crop bool
		want image.Rectangle
	}{
		// Fit: whole picture visible, bars on the short side.
		{"fit landscape 3:2", image.Pt(900, 600), false, image.Rect(0, 24, 648, 456)},
		{"fit portrait 2:3", image.Pt(600, 900), false, image.Rect(164, 0, 484, 480)},
		{"fit square", image.Pt(500, 500), false, image.Rect(84, 0, 564, 480)},
		{"fit exact aspect", image.Pt(1296, 960), false, image.Rect(0, 0, 648, 480)},
		// Fill: covers the screen, overflow trimmed equally on both sides.
		{"fill landscape 3:2", image.Pt(900, 600), true, image.Rect(-36, 0, 684, 480)},
		{"fill portrait 2:3", image.Pt(600, 900), true, image.Rect(0, -246, 648, 726)},
		{"fill square", image.Pt(500, 500), true, image.Rect(0, -84, 648, 564)},
		// 4:3 is slightly narrower than the panel's 648:480 (1.35), so a
		// small 4:3 image scales up to 648x486 and loses 3px top and bottom.
		{"fill small 4:3 image upscales", image.Pt(64, 48), true, image.Rect(0, -3, 648, 483)},
	}
	for _, c := range cases {
		got := Place(c.src, screen, c.crop)
		if got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
		if c.crop && !screen.In(got) {
			t.Errorf("%s: fill must cover the whole screen, got %v", c.name, got)
		}
		if !c.crop && !got.In(screen) {
			t.Errorf("%s: fit must stay inside the screen, got %v", c.name, got)
		}
	}
}

func TestPhotoFillCoversScreen(t *testing.T) {
	d := SampleData()
	d.PhotoFill = true
	c := Photo(d)
	// The sample photo is 3:2, so Fit leaves bars at the top and bottom;
	// Fill must put picture pixels in every row, including the edges.
	for _, y := range []int{0, H - 1} {
		if !c.inPicture(W/2, y) {
			t.Errorf("row %d is not covered by the photo in fill mode", y)
		}
	}
	d.PhotoFill = false
	if Photo(d).inPicture(W/2, 0) {
		t.Error("fit mode should letterbox a 3:2 photo")
	}
}
