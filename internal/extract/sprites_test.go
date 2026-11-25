package extract

import (
	"image"
	"image/color"
	"math"
	"slices"
	"testing"

	"github.com/codezerg/blockout-ai-game-assets/internal/iso"
	"github.com/codezerg/blockout-ai-game-assets/internal/layout"
	"github.com/codezerg/blockout-ai-game-assets/internal/matte"
	"github.com/codezerg/blockout-ai-game-assets/internal/segment"
)

// drawPrism draws a box standing on a w×d footprint whose front corner is at
// (bx, by), tile T, h pixels tall, with edges at angle degrees.
func drawPrism(img *image.RGBA, bx, by, T, w, d, h, angle float64, c color.RGBA) {
	k := math.Tan(angle * math.Pi / 180)
	// In ground units u (along w) and v (along d), screen x = (u-v)T/2 and
	// y = (u+v)T/2·k, with the front corner at u=w, v=d.
	for y := int(by - (w+d)*T/2*k - h - 2); y <= int(by)+2; y++ {
		for x := int(bx - d*T/2 - 2); x <= int(bx+w*T/2)+2; x++ {
			fx, fy := float64(x)+0.5-bx, float64(y)+0.5-by
			for t := 0.0; t <= h; t++ {
				a, b := fx/(T/2), (fy+t)/(T/2*k) // a = u-v-(w-d), b = u+v-(w+d)
				u, v := (a+b)/2+w, (b-a)/2+d
				if u >= 0 && u <= w && v >= 0 && v <= d {
					img.SetRGBA(x, y, c)
					break
				}
			}
		}
	}
}

func TestSpritesCorrectScaleAndPivot(t *testing.T) {
	l, _, err := layout.Parse([]byte(`canvas: { background: "#00FF00" }
iso: { tile: 80 }
pieces:
  - { id: big, kind: sprite, box: { x: 40, y: 100, w: 300, h: 320 }, footprint: [2, 2], prompt: x }
  - { id: small, kind: sprite, box: { x: 380, y: 100, w: 260, h: 320 }, footprint: [2, 2], prompt: y }
  - { id: flat, kind: sprite, box: { x: 680, y: 100, w: 300, h: 320 }, footprint: [1, 1], prompt: z }
`), "layout.yaml", layout.Options{})
	if err != nil {
		t.Fatal(err)
	}
	sheet := image.NewRGBA(image.Rect(0, 0, 1024, 1024))
	fill(sheet, sheet.Bounds(), green)
	drawPrism(sheet, 190, 400, 100, 2, 2, 120, 29, red) // drawn too big, a bit steep
	drawPrism(sheet, 510, 400, 60, 2, 2, 90, iso.Angle, blue)
	fill(sheet, image.Rect(720, 200, 940, 400), red) // no isometric base

	seg := segment.Run(l, sheet)
	m := matte.Compute(sheet, seg, matte.Hard)
	dir := t.TempDir()
	ps, imgs, _, err := Run(l, seg, m, dir)
	if err != nil {
		t.Fatal(err)
	}
	tile, sum, _, err := Sprites(l, seg, ps, imgs, dir)
	if err != nil {
		t.Fatal(err)
	}
	if tile.PX != 80 || sum.Measured != 2 {
		t.Fatalf("tile %+v summary %+v", tile, sum)
	}
	for i, want := range []float64{0.8, 80.0 / 60} {
		p := ps[i]
		if p.Base == nil || p.Fix == nil {
			t.Fatalf("%s: %+v", p.ID, p)
		}
		if math.Abs(p.Fix.Scale-want) > 0.03 {
			t.Errorf("%s: scale %.3f, want %.3f", p.ID, p.Fix.Scale, want)
		}
		// The pivot is the front corner, at the bottom middle of a prism.
		if math.Abs(p.Pivot.X-0.5) > 0.02 || math.Abs(p.Pivot.Y-1) > 0.02 {
			t.Errorf("%s: pivot %+v", p.ID, p.Pivot)
		}
		if b := imgs[i].Bounds(); p.Size == nil || p.Size.W != b.Dx() {
			t.Errorf("%s: size %+v, file %v", p.ID, p.Size, b)
		}
	}
	if ps[0].Fix.Squash > 0.95 || ps[1].Fix.Squash != 1 {
		t.Errorf("a 29° base is squashed, a 2:1 one is not: %+v %+v", ps[0].Fix, ps[1].Fix)
	}
	if !slices.Contains(ps[1].Flags, "upscaled") {
		t.Errorf("small was enlarged by a third and should say so: %v", ps[1].Flags)
	}
	if !slices.Contains(ps[2].Flags, "unmeasured") || ps[2].Base != nil || ps[2].Fix == nil {
		t.Errorf("flat: %+v", ps[2])
	}
	if a := sum.After; a == nil || a.ScaleMaxDev > 0.03 || a.AngleMaxDev > 1 {
		t.Errorf("after correction: %+v", sum.After)
	}
}
