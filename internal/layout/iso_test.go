package layout

import (
	"math"
	"strings"
	"testing"
)

const village = `canvas: { aspect: "1:1", size: 1K, background: "#FF00FF" }
grid: { cols: 3, rows: 2, gutter: 28 }
cell_defaults: { kind: sprite, footprint: [2, 2] }
pieces:
  - { id: house, cell: [0, 0], prompt: a house }
  - { id: well, cell: [1, 0], footprint: [1, 1], prompt: a well }
  - { id: barn, cell: [2, 1], footprint: [3, 2], pivot: center, prompt: a barn }
`

// Cells become boxes, defaults fill in kind and footprint, the pivot
// defaults to bottom-center, and the tile is the largest even size at which
// every footprint fits its box.
func TestGridCellsAndFootprints(t *testing.T) {
	l, _, err := Parse([]byte(village), "l.yaml", Options{})
	if err != nil {
		t.Fatal(err)
	}
	h, w, b := l.Pieces[0], l.Pieces[1], l.Pieces[2]
	if h.Kind != "sprite" || len(h.Footprint) != 2 || h.Pivot != PivotBottomCenter || b.Pivot != PivotCenter {
		t.Errorf("defaults: %+v %+v", h, b)
	}
	if w.Footprint[0] != 1 {
		t.Errorf("a piece's own footprint wins: %v", w.Footprint)
	}
	if h.Box.W <= 0 || w.Box.X <= h.Box.X+h.Box.W || b.Box.Y <= h.Box.Y+h.Box.H {
		t.Errorf("cells to boxes: %+v %+v %+v", h.Box, w.Box, b.Box)
	}
	T := l.Tile()
	if T <= 0 || T%2 != 0 {
		t.Fatalf("tile %d", T)
	}
	// The 3x2 barn is the widest: 2.5 tiles must fit 85% of its box.
	if 2.5*float64(T) > 0.85*float64(b.Box.W) || 2.5*float64(T+2) <= 0.85*float64(b.Box.W) && 1.25*float64(T+2) <= 0.4*float64(b.Box.H) {
		t.Errorf("tile %d is not the largest that fits a %d-wide box", T, b.Box.W)
	}
	// The diamond: 2:1, front corner near the box's bottom, shifted right
	// by (w-d)T/4 for the barn.
	d, ok := l.Footprint(2)
	if !ok {
		t.Fatal("no footprint")
	}
	if got := d.Right.X - d.Left.X; math.Abs(got-2.5*float64(T)) > 0.01 {
		t.Errorf("width %.1f", got)
	}
	if got := d.Bottom.Y - d.Top.Y; math.Abs(got-1.25*float64(T)) > 0.01 {
		t.Errorf("height %.1f", got)
	}
	cx := float64(b.Box.X) + float64(b.Box.W)/2
	if math.Abs(d.Bottom.X-(cx+float64(T)/4)) > 0.01 || d.Bottom.Y >= float64(b.Box.Y+b.Box.H) {
		t.Errorf("front corner %+v for box %+v", d.Bottom, b.Box)
	}
	if !l.IsIso() || !l.Guides() {
		t.Error("footprints make an iso sheet, with guides by default")
	}
}

func TestIsoProblems(t *testing.T) {
	for _, c := range []struct{ from, to, want string }{
		{"cell: [2, 1]", "cell: [3, 1]", "outside the 3x2 grid"},
		{"footprint: [3, 2], pivot: center", "footprint: [3, 2], pivot: top", "unknown pivot"},
		{"footprint: [1, 1]", "footprint: [0, 1]", "whole tiles"},
		{"grid: { cols: 3, rows: 2, gutter: 28 }\n", "iso: { tile: 200 }\n", "no grid"},
	} {
		src := strings.Replace(village, c.from, c.to, 1)
		var msgs []string
		for _, p := range problems(t, src) {
			msgs = append(msgs, p.Message)
		}
		if !strings.Contains(strings.Join(msgs, "|"), c.want) {
			t.Errorf("%s -> %s: want %q, got %v", c.from, c.to, c.want, msgs)
		}
	}
	// A tile too big for a box.
	src := strings.Replace(village, "grid:", "iso: { tile: 200 }\ngrid:", 1)
	ps := problems(t, src)
	if !strings.Contains(ps[0].Message, "does not fit") {
		t.Errorf("got %+v", ps)
	}
}
