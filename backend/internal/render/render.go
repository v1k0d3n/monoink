// Package render converts images into the faceplate's 1 bpp frame format.
package render

import (
	"image"
	"image/color"
	"image/draw"

	"github.com/v1k0d3n/monoink/backend/internal/proto"
)

// Pack converts a Gray image (exactly proto.Width x proto.Height) into the
// device frame: row-major, MSB first, bit set = white. Pixels >= 128 are white.
func Pack(img *image.Gray) []byte {
	b := img.Bounds()
	out := make([]byte, proto.FrameSize)
	stride := proto.Width / 8
	for y := 0; y < proto.Height; y++ {
		for x := 0; x < proto.Width; x++ {
			if img.GrayAt(b.Min.X+x, b.Min.Y+y).Y >= 128 {
				out[y*stride+x/8] |= 0x80 >> (x % 8)
			}
		}
	}
	return out
}

// Dither reduces src to pure black/white with Floyd–Steinberg error
// diffusion, fitted and centred on a white canvas of the panel size.
func Dither(src image.Image) *image.Gray {
	canvas := Fit(src)
	DitherInPlace(canvas)
	return canvas
}

// DitherInPlace applies Floyd–Steinberg dithering to img.
func DitherInPlace(img *image.Gray) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	errs := make([]float32, (w+2)*(h+1))
	at := func(x, y int) *float32 { return &errs[y*(w+2)+x+1] }
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			v := float32(img.GrayAt(b.Min.X+x, b.Min.Y+y).Y) + *at(x, y)
			var q float32
			if v >= 128 {
				q = 255
			}
			img.SetGray(b.Min.X+x, b.Min.Y+y, color.Gray{Y: uint8(q)})
			e := v - q
			*at(x+1, y) += e * 7 / 16
			*at(x-1, y+1) += e * 3 / 16
			*at(x, y+1) += e * 5 / 16
			*at(x+1, y+1) += e * 1 / 16
		}
	}
}

// Fit scales src (nearest-neighbour, aspect preserved, never upscaled past
// the panel) onto a white panel-sized Gray canvas.
func Fit(src image.Image) *image.Gray {
	canvas := image.NewGray(image.Rect(0, 0, proto.Width, proto.Height))
	draw.Draw(canvas, canvas.Bounds(), image.White, image.Point{}, draw.Src)
	sb := src.Bounds()
	sw, sh := sb.Dx(), sb.Dy()
	if sw == 0 || sh == 0 {
		return canvas
	}
	dw, dh := sw, sh
	if dw > proto.Width || dh > proto.Height {
		if sw*proto.Height > sh*proto.Width {
			dw, dh = proto.Width, sh*proto.Width/sw
		} else {
			dw, dh = sw*proto.Height/sh, proto.Height
		}
	}
	ox, oy := (proto.Width-dw)/2, (proto.Height-dh)/2
	for y := 0; y < dh; y++ {
		for x := 0; x < dw; x++ {
			c := src.At(sb.Min.X+x*sw/dw, sb.Min.Y+y*sh/dh)
			r, g, b, a := c.RGBA()
			// Composite over white.
			lum := (299*r + 587*g + 114*b) / 1000
			lum = (lum*a + 0xffff*(0xffff-a)) / 0xffff
			canvas.SetGray(ox+x, oy+y, color.Gray{Y: uint8(lum >> 8)})
		}
	}
	return canvas
}

// TestPattern returns a checkerboard with a solid border, useful for
// verifying orientation and bit order on real hardware.
func TestPattern(cell int) *image.Gray {
	img := image.NewGray(image.Rect(0, 0, proto.Width, proto.Height))
	for y := 0; y < proto.Height; y++ {
		for x := 0; x < proto.Width; x++ {
			black := (x/cell+y/cell)%2 == 0
			if x < 8 || y < 8 || x >= proto.Width-8 || y >= proto.Height-8 {
				black = true
			}
			// White top-left marker block to reveal orientation.
			if x >= 16 && x < 80 && y >= 16 && y < 80 {
				black = false
			}
			if !black {
				img.SetGray(x, y, color.Gray{Y: 255})
			}
		}
	}
	return img
}
