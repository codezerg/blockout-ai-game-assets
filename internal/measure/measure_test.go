package measure

import (
	"image"
	"image/color"
	"image/draw"
	"testing"

	"github.com/codezerg/blockout-ai-game-assets/internal/layout"
	"github.com/codezerg/blockout-ai-game-assets/internal/segment"
)

func TestScore(t *testing.T) {
	l, _, err := layout.Parse([]byte(`canvas: { background: "#00FF00" }
pieces:
  - { id: a, kind: icon, box: { x: 100, y: 100, w: 200, h: 200 }, prompt: x }
  - { id: b, kind: icon, box: { x: 500, y: 100, w: 200, h: 200 }, prompt: y }
  - { id: c, kind: icon, box: { x: 100, y: 600, w: 200, h: 200 }, prompt: z }
`), "layout.yaml", layout.Options{})
	if err != nil {
		t.Fatal(err)
	}
	// A 2K sheet: a drawn exactly in its box, b spilling 40 units right,
	// c missing, plus a stray blob far from every box.
	sheet := image.NewRGBA(image.Rect(0, 0, 2048, 2048))
	draw.Draw(sheet, sheet.Bounds(), image.NewUniform(color.RGBA{0, 255, 0, 255}), image.Point{}, draw.Src)
	paint := func(x0, y0, x1, y1 int) {
		draw.Draw(sheet, image.Rect(x0*2, y0*2, x1*2, y1*2), image.NewUniform(color.RGBA{120, 70, 30, 255}), image.Point{}, draw.Src)
	}
	paint(100, 100, 300, 300)
	paint(500, 100, 740, 300)
	paint(800, 800, 850, 850)

	sc := Score(l, segment.Run(l, sheet), sheet, layout.SketchBlocks)
	a, b, c := sc.Pieces[0], sc.Pieces[1], sc.Pieces[2]
	if !a.Found || a.IoU != 1 || a.MaxSpill != 0 || a.TouchesEdge {
		t.Errorf("a: %+v", a)
	}
	if !b.Found || b.Spill.Right != 40 || b.MaxSpill != 40 {
		t.Errorf("b: %+v", b)
	}
	if c.Found {
		t.Errorf("c: %+v", c)
	}
	s := sc.Summary
	if s.Found != 2 || s.Inside != 1 || s.Stray != 1 || s.MinIoU != 0 || s.BgNoise != 0 {
		t.Errorf("summary: %+v", s)
	}
}

func TestLeak(t *testing.T) {
	l, _, err := layout.Parse([]byte(`canvas: { background: "#00FF00" }
pieces:
  - { id: a, kind: icon, box: { x: 100, y: 100, w: 200, h: 200 }, prompt: x }
`), "layout.yaml", layout.Options{})
	if err != nil {
		t.Fatal(err)
	}
	sheet := image.NewRGBA(image.Rect(0, 0, 1024, 1024))
	draw.Draw(sheet, sheet.Bounds(), image.NewUniform(color.RGBA{0, 255, 0, 255}), image.Point{}, draw.Src)
	// Half the piece is the sketch's blue block colour.
	draw.Draw(sheet, image.Rect(100, 100, 300, 200), image.NewUniform(color.RGBA{0x4E, 0x79, 0xA7, 255}), image.Point{}, draw.Src)
	draw.Draw(sheet, image.Rect(100, 200, 300, 300), image.NewUniform(color.RGBA{120, 70, 30, 255}), image.Point{}, draw.Src)
	if got := Score(l, segment.Run(l, sheet), sheet, layout.SketchBlocks).Pieces[0].Leak; got != 0.5 {
		t.Errorf("leak %v", got)
	}
	if got := Score(l, segment.Run(l, sheet), sheet, layout.SketchNone).Pieces[0].Leak; got != 0 {
		t.Errorf("leak without a sketch %v", got)
	}
}
