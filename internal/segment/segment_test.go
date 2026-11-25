package segment

import (
	"image"
	"image/color"
	"image/draw"
	"testing"

	"github.com/codezerg/blockout-ai-game-assets/internal/layout"
)

func TestContact(t *testing.T) {
	l, _, err := layout.Parse([]byte(`canvas: { background: "#00FF00" }
pieces:
  - { id: a, kind: sprite, box: { x: 100, y: 100, w: 200, h: 200 }, prompt: x }
  - { id: b, kind: sprite, box: { x: 340, y: 100, w: 200, h: 200 }, prompt: y }
  - { id: c, kind: sprite, box: { x: 100, y: 600, w: 200, h: 200 }, prompt: z }
  - { id: d, kind: sprite, box: { x: 340, y: 600, w: 200, h: 200 }, prompt: w }
`), "layout.yaml", layout.Options{})
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 1024, 1024))
	paint := func(r image.Rectangle) {
		draw.Draw(img, r, image.NewUniform(color.RGBA{120, 70, 30, 255}), image.Point{}, draw.Src)
	}
	draw.Draw(img, img.Bounds(), image.NewUniform(color.RGBA{0, 255, 0, 255}), image.Point{}, draw.Src)
	// a overgrows 60 px into b's box; b is drawn separately.
	paint(image.Rect(100, 100, 400, 300))
	paint(image.Rect(420, 100, 540, 300))
	// c and d drawn as one shape across both boxes.
	paint(image.Rect(100, 600, 540, 800))

	seg := Run(l, img)
	if m, in := seg.Contact(0); len(m) != 0 || len(in) != 1 || in[0] != 1 {
		t.Errorf("a: merged %v intrudes %v, want intrudes b", m, in)
	}
	if m, in := seg.Contact(1); len(m) != 0 || len(in) != 0 {
		t.Errorf("b: merged %v intrudes %v", m, in)
	}
	if m, _ := seg.Contact(2); len(m) != 1 || m[0] != 3 {
		t.Errorf("c: merged %v, want d", m)
	}
	if _, ok := seg.PieceRect(3, true); ok {
		t.Errorf("d should own nothing: it was swallowed by c")
	}
}
