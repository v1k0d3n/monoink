package screens

import (
	"image"
	"image/color"
	"math"
	"strings"
	"sync"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/gomedium"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"

	"github.com/v1k0d3n/monoink/backend/internal/proto"
	"github.com/v1k0d3n/monoink/backend/internal/render"
)

var (
	black = color.Gray{Y: 0}
	white = color.Gray{Y: 255}
)

// Weight selects one of the embedded Go fonts (BSD licensed).
type Weight int

const (
	Regular Weight = iota
	Medium
	Bold
)

var (
	fontOnce  sync.Once
	fontFiles map[Weight]*opentype.Font
	faceMu    sync.Mutex
	faces     = map[[2]int]font.Face{}
)

func face(w Weight, size int) font.Face {
	fontOnce.Do(func() {
		fontFiles = map[Weight]*opentype.Font{}
		for wt, ttf := range map[Weight][]byte{Regular: goregular.TTF, Medium: gomedium.TTF, Bold: gobold.TTF} {
			f, err := opentype.Parse(ttf)
			if err != nil {
				panic(err) // embedded fonts are known-good
			}
			fontFiles[wt] = f
		}
	})
	faceMu.Lock()
	defer faceMu.Unlock()
	key := [2]int{int(w), size}
	if f, ok := faces[key]; ok {
		return f
	}
	f, err := opentype.NewFace(fontFiles[w], &opentype.FaceOptions{Size: float64(size), DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		panic(err)
	}
	faces[key] = f
	return f
}

// Canvas is a panel-sized grayscale drawing surface.
type Canvas struct {
	*image.Gray
	pictures []image.Rectangle // where photos/art were placed (kept as-is by Invert)
}

func NewCanvas() *Canvas {
	c := &Canvas{Gray: image.NewGray(image.Rect(0, 0, proto.Width, proto.Height))}
	c.Fill(c.Bounds(), white)
	return c
}

func (c *Canvas) Fill(r image.Rectangle, col color.Gray) {
	r = r.Intersect(c.Bounds())
	for y := r.Min.Y; y < r.Max.Y; y++ {
		row := c.Pix[y*c.Stride+r.Min.X : y*c.Stride+r.Max.X]
		for i := range row {
			row[i] = col.Y
		}
	}
}

// Box draws a rectangle outline of thickness t.
func (c *Canvas) Box(r image.Rectangle, t int, col color.Gray) {
	c.Fill(image.Rect(r.Min.X, r.Min.Y, r.Max.X, r.Min.Y+t), col)
	c.Fill(image.Rect(r.Min.X, r.Max.Y-t, r.Max.X, r.Max.Y), col)
	c.Fill(image.Rect(r.Min.X, r.Min.Y, r.Min.X+t, r.Max.Y), col)
	c.Fill(image.Rect(r.Max.X-t, r.Min.Y, r.Max.X, r.Max.Y), col)
}

// Disc fills a circle.
func (c *Canvas) Disc(cx, cy, r float64, col color.Gray) {
	for y := int(cy - r); y <= int(cy+r); y++ {
		for x := int(cx - r); x <= int(cx+r); x++ {
			dx, dy := float64(x)+0.5-cx, float64(y)+0.5-cy
			if dx*dx+dy*dy <= r*r && image.Pt(x, y).In(c.Bounds()) {
				c.SetGray(x, y, col)
			}
		}
	}
}

// Line draws a line of width w with round caps.
func (c *Canvas) Line(x0, y0, x1, y1, w float64, col color.Gray) {
	steps := int(math.Max(math.Abs(x1-x0), math.Abs(y1-y0))) + 1
	for i := 0; i <= steps; i++ {
		t := float64(i) / float64(steps)
		c.Disc(x0+(x1-x0)*t, y0+(y1-y0)*t, w/2, col)
	}
}

// Polygon fills a simple polygon (even-odd rule).
func (c *Canvas) Polygon(pts [][2]float64, col color.Gray) {
	minY, maxY := pts[0][1], pts[0][1]
	for _, p := range pts {
		minY, maxY = math.Min(minY, p[1]), math.Max(maxY, p[1])
	}
	for y := int(minY); y <= int(maxY); y++ {
		fy := float64(y) + 0.5
		var xs []float64
		for i := range pts {
			a, b := pts[i], pts[(i+1)%len(pts)]
			if (a[1] <= fy) != (b[1] <= fy) {
				xs = append(xs, a[0]+(fy-a[1])/(b[1]-a[1])*(b[0]-a[0]))
			}
		}
		for i := 1; i < len(xs); i++ {
			for j := i; j > 0 && xs[j] < xs[j-1]; j-- {
				xs[j], xs[j-1] = xs[j-1], xs[j]
			}
		}
		for i := 0; i+1 < len(xs); i += 2 {
			c.Fill(image.Rect(int(xs[i]+0.5), y, int(xs[i+1]+0.5), y+1), col)
		}
	}
}

type Align int

const (
	Left Align = iota
	Center
	Right
)

// Measure returns the advance width of s.
func Measure(s string, w Weight, size int) int {
	return font.MeasureString(face(w, size), s).Ceil()
}

// Text draws s with its top at y (cap-height aligned) and returns the width.
func (c *Canvas) Text(s string, x, y int, w Weight, size int, a Align, col color.Gray) int {
	f := face(w, size)
	width := font.MeasureString(f, s).Ceil()
	switch a {
	case Center:
		x -= width / 2
	case Right:
		x -= width
	}
	d := &font.Drawer{Dst: c.Gray, Src: image.NewUniform(col), Face: f,
		Dot: fixed.P(x, y+capHeight(w, size))}
	d.DrawString(s)
	return width
}

func capHeight(w Weight, size int) int {
	b, _, ok := face(w, size).GlyphBounds('H')
	if !ok {
		return size * 7 / 10
	}
	return (-b.Min.Y).Ceil()
}

// Fit truncates s with an ellipsis so it fits in maxW pixels.
func Fit(s string, w Weight, size, maxW int) string {
	if Measure(s, w, size) <= maxW {
		return s
	}
	r := []rune(s)
	for len(r) > 0 && Measure(string(r)+"…", w, size) > maxW {
		r = r[:len(r)-1]
	}
	return strings.TrimSpace(string(r)) + "…"
}

// Wrap breaks s into at most maxLines lines no wider than maxW. Overflow
// is folded into the last line and ellipsized.
func Wrap(s string, w Weight, size, maxW, maxLines int) []string {
	words := strings.Fields(s)
	var lines []string
	for len(words) > 0 {
		if len(lines) == maxLines-1 {
			return append(lines, Fit(strings.Join(words, " "), w, size, maxW))
		}
		n := 1
		for n < len(words) && Measure(strings.Join(words[:n+1], " "), w, size) <= maxW {
			n++
		}
		lines = append(lines, Fit(strings.Join(words[:n], " "), w, size, maxW))
		words = words[n:]
	}
	return lines
}

// Picture scales img to cover (crop=true) or fit inside r, converts to
// gray and dithers it in place so photos look right on a 1-bit panel.
func (c *Canvas) Picture(img image.Image, r image.Rectangle, crop bool) {
	sb := img.Bounds()
	if sb.Empty() || r.Empty() {
		return
	}
	dst := Place(sb.Size(), r, crop)
	dw, dh := dst.Dx(), dst.Dy()

	tmp := image.NewGray(image.Rect(0, 0, dw, dh))
	xdraw.Draw(tmp, tmp.Bounds(), image.White, image.Point{}, xdraw.Src)
	xdraw.CatmullRom.Scale(tmp, tmp.Bounds(), img, sb, xdraw.Over, nil)
	render.DitherInPlace(tmp)

	clip := dst.Intersect(r)
	xdraw.Draw(c.Gray, clip, tmp, clip.Min.Sub(dst.Min), xdraw.Src)
	c.pictures = append(c.pictures, clip)
}

// Place returns where a picture of size src lands in r, centered and with
// its aspect ratio kept. With crop false it fits entirely inside r
// (letterboxed); with crop true it covers all of r and the overflow is
// trimmed equally from both sides.
func Place(src image.Point, r image.Rectangle, crop bool) image.Rectangle {
	sx := float64(r.Dx()) / float64(src.X)
	sy := float64(r.Dy()) / float64(src.Y)
	scale := math.Min(sx, sy)
	if crop {
		scale = math.Max(sx, sy)
	}
	dw, dh := int(float64(src.X)*scale+0.5), int(float64(src.Y)*scale+0.5)
	min := image.Pt(r.Min.X+(r.Dx()-dw)/2, r.Min.Y+(r.Dy()-dh)/2)
	return image.Rectangle{Min: min, Max: min.Add(image.Pt(dw, dh))}
}

// Invert flips everything except photos and cover art ("dark mode"):
// inverted pictures would look like photographic negatives.
func (c *Canvas) Invert() {
	b := c.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		row := c.Pix[y*c.Stride : y*c.Stride+b.Dx()]
		for x := range row {
			if !c.inPicture(x, y) {
				row[x] = 255 - row[x]
			}
		}
	}
}

func (c *Canvas) inPicture(x, y int) bool {
	p := image.Pt(x, y)
	for _, r := range c.pictures {
		if p.In(r) {
			return true
		}
	}
	return false
}
