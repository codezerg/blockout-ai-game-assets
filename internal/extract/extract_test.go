package extract

import (
	"image"
	"image/color"
	"image/draw"
	"testing"

	"github.com/codezerg/blockout-ai-game-assets/internal/layout"
	"github.com/codezerg/blockout-ai-game-assets/internal/matte"
	"github.com/codezerg/blockout-ai-game-assets/internal/segment"
)

var (
	green = color.RGBA{0, 255, 0, 255}
	red   = color.RGBA{200, 30, 30, 255}
	blue  = color.RGBA{30, 30, 200, 255}
)

func fill(img *image.RGBA, r image.Rectangle, c color.RGBA) {
	draw.Draw(img, r, image.NewUniform(c), image.Point{}, draw.Src)
}

// Faint strips cut off from the art by an empty row or column are matte
// noise and must not pad the file; soft edges joined to the art stay.
func TestTrimDropsFaintStrips(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 200, 120))
	set := func(r image.Rectangle, a uint8) {
		draw.Draw(img, r, image.NewUniform(color.NRGBA{200, 180, 40, a}), image.Point{}, draw.Src)
	}
	set(image.Rect(60, 20, 180, 100), 255) // the art
	set(image.Rect(58, 20, 60, 100), 12)   // a soft edge joined to it: kept
	set(image.Rect(70, 0, 150, 1), 7)      // a sliver above, one empty row between (block_yellow)
	set(image.Rect(0, 30, 40, 90), 20)     // a wide faint strip on the left
	set(image.Rect(198, 118, 200, 120), 9) // a faint corner speck, empty once the others go
	if got, want := trim(img), image.Rect(58, 20, 180, 100); got != want {
		t.Errorf("trim = %v, want %v", got, want)
	}

	// A strip bright enough to be art is kept, gap or not.
	set(image.Rect(0, 30, 40, 90), 40)
	if got, want := trim(img), image.Rect(0, 20, 180, 100); got != want {
		t.Errorf("bright strip: trim = %v, want %v", got, want)
	}

	// A piece that is faint all over is trimmed only to its visible pixels.
	faint := image.NewNRGBA(image.Rect(0, 0, 50, 50))
	draw.Draw(faint, image.Rect(5, 5, 10, 10), image.NewUniform(color.NRGBA{0, 0, 0, 20}), image.Point{}, draw.Src)
	draw.Draw(faint, image.Rect(30, 30, 40, 40), image.NewUniform(color.NRGBA{0, 0, 0, 20}), image.Point{}, draw.Src)
	if got, want := trim(faint), image.Rect(5, 5, 40, 40); got != want {
		t.Errorf("all faint: trim = %v, want %v", got, want)
	}
}

// Two pieces whose bounding boxes overlap: a red L-shape reaching under a
// blue square. Each file must hold only its own piece.
func TestNoNeighbourLeak(t *testing.T) {
	l, _, err := layout.Parse([]byte(`canvas: { background: "#00FF00" }
pieces:
  - { id: a, kind: sprite, box: { x: 100, y: 100, w: 300, h: 300 }, prompt: x }
  - { id: b, kind: sprite, box: { x: 500, y: 100, w: 200, h: 200 }, prompt: y }
`), "layout.yaml", layout.Options{})
	if err != nil {
		t.Fatal(err)
	}
	sheet := image.NewRGBA(image.Rect(0, 0, 1024, 1024))
	fill(sheet, sheet.Bounds(), green)
	fill(sheet, image.Rect(100, 100, 400, 400), red)
	fill(sheet, image.Rect(400, 330, 700, 400), red) // a's foot runs under b
	fill(sheet, image.Rect(500, 100, 700, 300), blue)
	fill(sheet, image.Rect(600, 200, 602, 202), green) // a speck of key enclosed by b, inside a's bounds

	seg := segment.Run(l, sheet)
	m := matte.Compute(sheet, seg, matte.Hard)
	ps, imgs, warns, err := Run(l, seg, m, t.TempDir())
	if err != nil || len(warns) != 0 {
		t.Fatal(err, warns)
	}
	if ps[0].Rect != (Rect{X: 100, Y: 100, W: 600, H: 300}) || ps[1].Rect != (Rect{X: 500, Y: 100, W: 200, H: 200}) {
		t.Errorf("rects %+v %+v", ps[0].Rect, ps[1].Rect)
	}
	// at reads piece i at sheet coordinates.
	at := func(i, x, y int) color.NRGBA {
		img := imgs[i].(*image.NRGBA)
		o := img.Bounds().Min
		return img.NRGBAAt(o.X+x-ps[i].Rect.X, o.Y+y-ps[i].Rect.Y)
	}
	// Inside a's bounds but on b's pixels: must be transparent in a.
	if c := at(0, 550, 150); c.A != 0 {
		t.Errorf("b leaked into a: %v", c)
	}
	if c := at(0, 550, 350); c.A != 255 || c.R != 200 {
		t.Errorf("a's foot missing: %v", c)
	}
	if c := at(1, 550, 150); c.A != 255 || c.B != 200 {
		t.Errorf("b wrong: %v", c)
	}
	// The enclosed speck is kept as part of b, the piece around it, and
	// nowhere in a.
	if len(m.Kept) != 1 || m.Kept[0].Owner != 1 {
		t.Fatalf("kept %+v", m.Kept)
	}
	if c := at(0, 600, 200); c.A != 0 {
		t.Errorf("b's enclosed speck leaked into a: %v", c)
	}
	if c := at(1, 600, 200); c.A != 255 {
		t.Errorf("b lost its enclosed speck: %v", c)
	}
}

// A navy panel whose glass rim took on the green key (the E9 ghost tile),
// next to a clean one. Only the first is cast; edge tint misses both.
func TestKeyCast(t *testing.T) {
	l, _, _ := layout.Parse([]byte(`canvas: { background: "#00FF00" }
pieces:
  - { id: cast, kind: icon, box: { x: 100, y: 100, w: 200, h: 200 }, prompt: x }
  - { id: clean, kind: icon, box: { x: 500, y: 100, w: 200, h: 200 }, prompt: y }
`), "layout.yaml", layout.Options{})
	navy := color.RGBA{20, 30, 60, 255}
	glass := color.RGBA{60, 170, 90, 255} // green leaks through: G - max(R, B) = 80
	sheet := image.NewRGBA(image.Rect(0, 0, 1024, 1024))
	fill(sheet, sheet.Bounds(), green)
	fill(sheet, image.Rect(100, 100, 300, 300), glass)
	fill(sheet, image.Rect(120, 120, 280, 280), navy)
	fill(sheet, image.Rect(500, 100, 700, 300), navy)

	seg := segment.Run(l, sheet)
	ps, _, warns, err := Run(l, seg, matte.Compute(sheet, seg, matte.Soft), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// The rim is (200² - 160²) / 200² = 36% of the piece, less its outer
	// pixels, which the soft matte despills.
	if c := ps[0].KeyCast; c < 0.28 || c > 0.37 || len(ps[0].Flags) != 1 || ps[0].Flags[0] != "key_cast" || len(warns) != 1 {
		t.Errorf("cast piece: key_cast %v, flags %v, warnings %v", c, ps[0].Flags, warns)
	}
	if ps[1].KeyCast != 0 || len(ps[1].Flags) != 0 {
		t.Errorf("clean piece: key_cast %v, flags %v", ps[1].KeyCast, ps[1].Flags)
	}

	// A grey key adds no hue, so nothing is cast on black.
	img := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	draw.Draw(img, img.Bounds(), image.NewUniform(glass), image.Point{}, draw.Src)
	if c := keyCast(img, color.RGBA{0, 0, 0, 255}); c != 0 {
		t.Errorf("black key: %v", c)
	}
}

func TestMissingPiece(t *testing.T) {
	l, _, _ := layout.Parse([]byte(`canvas: { background: "#00FF00" }
pieces:
  - { id: a, kind: icon, box: { x: 100, y: 100, w: 100, h: 100 }, prompt: x }
`), "layout.yaml", layout.Options{})
	sheet := image.NewRGBA(image.Rect(0, 0, 1024, 1024))
	fill(sheet, sheet.Bounds(), green)
	seg := segment.Run(l, sheet)
	ps, _, warns, err := Run(l, seg, matte.Compute(sheet, seg, matte.Soft), t.TempDir())
	if err != nil || len(ps[0].Flags) != 1 || ps[0].Flags[0] != "missing" || ps[0].File != "" || len(warns) != 1 {
		t.Errorf("%v %+v %v", err, ps[0], warns)
	}
}
