// Command previews renders every screen from fixed sample data into PNG
// files for the documentation (docs/images/screens). Output is
// deterministic so CI can check the committed images are up to date.
//
//	go run ./cmd/previews -out ../docs/images/screens
package main

import (
	"flag"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"sort"

	"github.com/v1k0d3n/monoink/backend/internal/screens"
)

func main() {
	out := flag.String("out", "../docs/images/screens", "output directory")
	flag.Parse()
	if err := os.MkdirAll(*out, 0o755); err != nil {
		fail(err)
	}
	ids := make([]string, 0, len(screens.All))
	for id := range screens.All {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		light := screens.All[id].Render(screens.SampleData())
		write(filepath.Join(*out, id+".png"), oneBit(light.Gray))
		dark := screens.All[id].Render(screens.SampleData())
		dark.Invert()
		write(filepath.Join(*out, id+"-dark.png"), oneBit(dark.Gray))
	}
	// Variants of a screen with a non-default setting.
	fill := screens.SampleData()
	fill.PhotoFill = true
	write(filepath.Join(*out, "photo-fill.png"), oneBit(screens.Photo(fill).Gray))

	fmt.Printf("rendered %d screens (light and dark) and variants to %s\n", len(ids), *out)
}

// oneBit thresholds to exactly what the panel shows, which also keeps the
// PNGs small.
func oneBit(img *image.Gray) *image.Gray {
	for i, v := range img.Pix {
		if v >= 128 {
			img.Pix[i] = 255
		} else {
			img.Pix[i] = 0
		}
	}
	return img
}

func write(path string, img image.Image) {
	f, err := os.Create(path)
	if err != nil {
		fail(err)
	}
	enc := png.Encoder{CompressionLevel: png.BestCompression}
	if err := enc.Encode(f, img); err != nil {
		fail(err)
	}
	if err := f.Close(); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "previews:", err)
	os.Exit(1)
}
