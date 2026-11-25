package matte

import (
	"image"
	"image/color"
	"image/draw"
	"testing"

	"github.com/codezerg/blockout-ai-game-assets/internal/layout"
	"github.com/codezerg/blockout-ai-game-assets/internal/segment"
)

func fill(img *image.RGBA, r image.Rectangle, c color.RGBA) {
	draw.Draw(img, r, image.NewUniform(c), image.Point{}, draw.Src)
}

// A red frame on key with a big empty centre, a tiny key-coloured speck
// (2×2) and a thin key-coloured groove (6×120) in its wall, and a soft edge
// column blended half-and-half with the key.
func sheetOn(t *testing.T, bg string, key color.RGBA) (*image.RGBA, *segment.Result) {
	t.Helper()
	l, _, err := layout.Parse([]byte(`canvas: { background: "`+bg+`" }
pieces:
  - { id: f, kind: frame, box: { x: 100, y: 100, w: 300, h: 300 }, prompt: x }
`), "layout.yaml", layout.Options{})
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 1024, 1024))
	fill(img, img.Bounds(), key)
	fill(img, image.Rect(100, 100, 400, 400), color.RGBA{200, 40, 40, 255})
	fill(img, image.Rect(160, 160, 340, 340), key) // hole, 180 px
	fill(img, image.Rect(120, 120, 122, 122), key) // speck in the wall
	fill(img, image.Rect(110, 200, 116, 320), key) // groove: big but thin
	mix := color.RGBA{uint8((200 + int(key.R)) / 2), uint8((40 + int(key.G)) / 2), uint8((40 + int(key.B)) / 2), 255}
	fill(img, image.Rect(400, 100, 401, 400), mix) // 50% red over the key
	return img, segment.Run(l, img)
}

func sheet(t *testing.T) (*image.RGBA, *segment.Result) {
	return sheetOn(t, "#00FF00", color.RGBA{0, 255, 0, 255})
}

// On a saturated key, the groove is a hole too; only the speck, below
// KeyHole², stays.
func TestHolesOnSaturatedKey(t *testing.T) {
	img, seg := sheet(t)
	m := Compute(img, seg, Hard)
	if len(m.Holes) != 2 || len(m.Kept) != 1 {
		t.Fatalf("holes %v kept %v", m.Holes, m.Kept)
	}
	for _, p := range []image.Point{{112, 250}, {250, 250}, {10, 10}} {
		if a := m.Img.NRGBAAt(p.X, p.Y).A; a != 0 {
			t.Errorf("%v should be transparent, alpha %d", p, a)
		}
	}
	if a := m.Img.NRGBAAt(120, 120).A; a != 255 {
		t.Errorf("a 2×2 speck should stay opaque, alpha %d", a)
	}
}

// On black, near-key areas may be shadows: only the big, thick one is a hole.
func TestHolesOnDarkKey(t *testing.T) {
	img, seg := sheetOn(t, "#000000", color.RGBA{0, 0, 0, 255})
	m := Compute(img, seg, Hard)
	if len(m.Holes) != 1 || len(m.Kept) != 2 {
		t.Fatalf("holes %v kept %v", m.Holes, m.Kept)
	}
	if a := m.Img.NRGBAAt(112, 250).A; a != 255 {
		t.Errorf("thin groove should stay opaque, alpha %d", a)
	}
	if a := m.Img.NRGBAAt(250, 250).A; a != 0 {
		t.Errorf("hole alpha %d", a)
	}
	if a := m.Img.NRGBAAt(120, 120).A; a != 255 {
		t.Errorf("speck in the art should stay opaque, alpha %d", a)
	}
}

func TestSoftEdgeUnmixes(t *testing.T) {
	img, seg := sheet(t)
	c := Compute(img, seg, Soft).Img.NRGBAAt(400, 250)
	if c.A < 110 || c.A > 145 {
		t.Errorf("half-blended edge alpha %d, want about 128", c.A)
	}
	if c.G > 70 || c.R < 170 {
		t.Errorf("edge colour %v should be unmixed back to red", c)
	}
	if s := Spill(c.R, c.G, c.B, color.RGBA{0, 255, 0, 255}); s > 5 {
		t.Errorf("spill %v left on the edge", s)
	}
}

func TestDespillMagenta(t *testing.T) {
	ch := keyChannels(color.RGBA{255, 0, 255, 255})
	red := [3]float64{200, 40, 30}
	despill(&red, ch)
	if red != [3]float64{200, 40, 30} {
		t.Errorf("magenta despill changed a red: %v", red)
	}
	pinkish := [3]float64{180, 60, 170} // grey 60 plus magenta spill
	despill(&pinkish, ch)
	if pinkish != [3]float64{70, 60, 60} {
		t.Errorf("magenta spill not removed: %v", pinkish)
	}
	green := [3]float64{60, 200, 50}
	despill(&green, keyChannels(color.RGBA{0, 255, 0, 255}))
	if green != [3]float64{60, 60, 50} {
		t.Errorf("green despill: %v", green)
	}
}
