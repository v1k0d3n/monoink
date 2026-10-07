package render

import (
	"image"
	"image/color"
	"testing"

	"github.com/v1k0d3n/monoink/backend/internal/proto"
)

func TestPackBitOrder(t *testing.T) {
	img := image.NewGray(image.Rect(0, 0, proto.Width, proto.Height))
	img.SetGray(0, 0, color.Gray{Y: 255}) // MSB of byte 0
	img.SetGray(9, 0, color.Gray{Y: 255}) // bit 6 of byte 1
	img.SetGray(proto.Width-1, proto.Height-1, color.Gray{Y: 255})
	out := Pack(img)
	if len(out) != 38880 {
		t.Fatalf("frame is %d bytes", len(out))
	}
	if out[0] != 0x80 || out[1] != 0x40 || out[len(out)-1] != 0x01 {
		t.Fatalf("bit order wrong: % x ... % x", out[:2], out[len(out)-1])
	}
}

func TestDitherIsBinaryAndPreservesExtremes(t *testing.T) {
	src := image.NewGray(image.Rect(0, 0, 64, 64))
	for i := range src.Pix {
		src.Pix[i] = 128
	}
	out := Dither(src)
	whites, blacks := 0, 0
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			v := out.GrayAt(proto.Width/2-32+x, proto.Height/2-32+y).Y
			switch v {
			case 0:
				blacks++
			case 255:
				whites++
			default:
				t.Fatalf("non-binary pixel %d", v)
			}
		}
	}
	if blacks < 1500 || whites < 1500 {
		t.Errorf("50%% grey dithered to %d black / %d white", blacks, whites)
	}
	if out.GrayAt(0, 0).Y != 255 {
		t.Error("margin should be white")
	}
}

func TestFitDownscalesWide(t *testing.T) {
	src := image.NewGray(image.Rect(0, 0, 1296, 480)) // all black, 2:1 wider
	out := Fit(src)
	if out.GrayAt(proto.Width/2, 0).Y != 255 || out.GrayAt(proto.Width/2, proto.Height/2).Y != 0 {
		t.Error("letterboxing incorrect")
	}
}
