package ui

import (
	"image"
	"image/color"
	"image/draw"
	"testing"

	"github.com/codezerg/blockout-ai-game-assets/internal/layout"
)

func fill(img *image.NRGBA, r image.Rectangle, c color.NRGBA) {
	draw.Draw(img, r, image.NewUniform(c), image.Point{}, draw.Src)
}

// A 300×200 panel: plain face, a dark border, and a 40×40 checkered
// ornament in each corner.
func panel() *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, 300, 200))
	fill(img, img.Bounds(), color.NRGBA{200, 150, 100, 255})
	for _, r := range []image.Rectangle{
		image.Rect(0, 0, 300, 10), image.Rect(0, 190, 300, 200),
		image.Rect(0, 0, 10, 200), image.Rect(290, 0, 300, 200),
	} {
		fill(img, r, color.NRGBA{60, 40, 20, 255})
	}
	for _, o := range []image.Point{{0, 0}, {260, 0}, {0, 160}, {260, 160}} {
		for y := 0; y < 40; y++ {
			for x := 0; x < 40; x++ {
				if (x/5+y/5)%2 == 0 {
					img.SetNRGBA(o.X+x, o.Y+y, color.NRGBA{240, 210, 60, 255})
				}
			}
		}
	}
	return img
}

func TestFindSlice(t *testing.T) {
	s := FindSlice(panel())
	want := Insets{Left: 42, Top: 42, Right: 42, Bottom: 42} // ornaments plus the safety margin
	if s.Insets != want || !s.FlatX || !s.FlatY {
		t.Errorf("got %+v, want insets %+v", s, want)
	}
}

func TestFindSliceOrnamentInMiddle(t *testing.T) {
	img := panel()
	// A crest in the middle of the top edge: stretching would smear it.
	fill(img, image.Rect(140, 0, 160, 30), color.NRGBA{240, 210, 60, 255})
	if s := FindSlice(img); s.FlatX {
		t.Errorf("a crest in the centre should make x not stretchable: %+v", s)
	}
}

// A glare streak on the face near the top-right corner (the E9 well frame)
// widens the right slice inset, since the streak must not stretch, but the
// rim is still 10 px on every side, and that is where content goes.
func TestContentIgnoresOneOffMark(t *testing.T) {
	img := panel()
	if c := Content(img, FindSlice(img).Insets); c != (Insets{10, 10, 10, 10}) {
		t.Errorf("plain panel: content %+v, want 10 on every side", c)
	}
	fill(img, image.Rect(200, 10, 280, 30), color.NRGBA{255, 250, 230, 255})
	s := FindSlice(img)
	if s.Insets.Right < 90 {
		t.Fatalf("the streak should widen the right slice: %+v", s.Insets)
	}
	if c := Content(img, s.Insets); c != (Insets{10, 10, 10, 10}) {
		t.Errorf("content %+v, want 10 on every side (slice %+v)", c, s.Insets)
	}
}

func TestStretchKeepsCorners(t *testing.T) {
	img := panel()
	in := Insets{Left: 42, Top: 42, Right: 42, Bottom: 42}
	out := Stretch(img, in, 600, 400)
	for _, p := range []image.Point{{3, 3}, {20, 7}, {39, 39}} {
		if a, b := img.NRGBAAt(p.X, p.Y), out.NRGBAAt(p.X, p.Y); a != b {
			t.Errorf("top-left corner changed at %v: %v vs %v", p, a, b)
		}
		q := image.Pt(299-p.X, 199-p.Y)
		if a, b := img.NRGBAAt(q.X, q.Y), out.NRGBAAt(q.X+300, q.Y+200); a != b {
			t.Errorf("bottom-right corner changed at %v: %v vs %v", q, a, b)
		}
	}
	if c := out.NRGBAAt(300, 200); c != (color.NRGBA{200, 150, 100, 255}) {
		t.Errorf("centre %v", c)
	}
}

func TestStates(t *testing.T) {
	l, _, err := layout.Parse([]byte(`canvas: { background: "#00FF00" }
pieces:
  - { id: btn, kind: button, box: { x: 0, y: 0, w: 100, h: 40 }, prompt: a }
  - { id: btn_primary, kind: button, box: { x: 0, y: 100, w: 200, h: 60 }, prompt: b }
  - { id: btn_primary_pressed, kind: button, box: { x: 300, y: 100, w: 200, h: 60 }, prompt: c }
  - { id: btn_primary_hover, kind: button, box: { x: 300, y: 200, w: 200, h: 60 }, prompt: d }
  - { id: btn_close, kind: button, box: { x: 300, y: 300, w: 60, h: 60 }, prompt: e }
  - { id: btn_primary_icon, kind: icon, box: { x: 300, y: 400, w: 200, h: 60 }, prompt: f }
`), "layout.yaml", layout.Options{})
	if err != nil {
		t.Fatal(err)
	}
	got := States(l)
	want := map[int]int{2: 1, 3: 1} // pressed and hover of btn_primary; btn_close differs in size; the icon is not a button
	if len(got) != len(want) || got[2] != 1 || got[3] != 1 {
		t.Errorf("got %v, want %v", got, want)
	}
}

// Chains deeper than one level must all map to the root, whatever order
// the map is walked in.
func TestStatesChain(t *testing.T) {
	l, _, err := layout.Parse([]byte(`canvas: { background: "#00FF00" }
pieces:
  - { id: b, kind: button, box: { x: 0, y: 0, w: 100, h: 40 }, prompt: a }
  - { id: b_x, kind: button, box: { x: 0, y: 100, w: 100, h: 40 }, prompt: b }
  - { id: b_x_y, kind: button, box: { x: 0, y: 200, w: 100, h: 40 }, prompt: c }
  - { id: b_x_y_z, kind: button, box: { x: 0, y: 300, w: 100, h: 40 }, prompt: d }
`), "layout.yaml", layout.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for range 50 {
		got := States(l)
		if len(got) != 3 || got[1] != 0 || got[2] != 0 || got[3] != 0 {
			t.Fatalf("got %v, want every state mapped to 0", got)
		}
	}
}

func TestAlign(t *testing.T) {
	base := image.NewNRGBA(image.Rect(0, 0, 100, 40))
	fill(base, base.Bounds(), color.NRGBA{100, 60, 30, 255})
	state := image.NewNRGBA(image.Rect(0, 0, 96, 40)) // drawn 4 px narrower
	fill(state, state.Bounds(), color.NRGBA{80, 50, 20, 255})
	m := Align(base, state)
	if m.DX != 2 || m.DY != 0 || m.DW != -4 || m.ShapeIoU < 0.959 || m.ShapeIoU > 0.961 || m.ColorDif < 13 || m.ColorDif > 14 {
		t.Errorf("%+v", m)
	}
}

func TestFitState(t *testing.T) {
	base := image.NewNRGBA(image.Rect(0, 0, 100, 40))
	near := image.NewNRGBA(image.Rect(0, 0, 96, 40)) // 4% narrower: resized
	far := image.NewNRGBA(image.Rect(0, 0, 90, 40))  // 10% narrower: left alone
	if img, ok := FitState(base, near); !ok || img.Bounds().Size() != base.Bounds().Size() {
		t.Errorf("near: %v %v", ok, img.Bounds())
	}
	if img, ok := FitState(base, far); ok || img != image.Image(far) {
		t.Errorf("far should be left alone")
	}
}
