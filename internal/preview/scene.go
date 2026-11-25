package preview

import (
	"image"
	"image/color"
	"math"
	"sort"

	"github.com/codezerg/blockout-ai-game-assets/internal/layout"
	"github.com/codezerg/blockout-ai-game-assets/internal/sketch"

	xdraw "golang.org/x/image/draw"
)

// Placed is one sprite for Scene: its image, its pivot as a share of the
// image, and its footprint in tiles. Unmeasured sprites' footprints are
// drawn red.
type Placed struct {
	Img        image.Image
	Pivot      [2]float64
	Foot       [2]int
	Unmeasured bool
}

// Scene stands every sprite on an isometric ground with tiles tile pixels
// wide, its pivot on its footprint's front corner, back to front, the way an
// engine would draw them. Sprites go in plots cols wide, in order. It shows
// whether corrected sprites share one scale and one camera.
func Scene(items []Placed, tile float64, cols int) *image.RGBA {
	if len(items) == 0 || tile <= 0 {
		return nil
	}
	P := 0 // plot size in tiles: the largest footprint plus a gap
	for _, it := range items {
		P = max(P, it.Foot[0], it.Foot[1])
	}
	P++
	rows := (len(items) + cols - 1) / cols
	scr := func(gx, gy float64) layout.Pt {
		return layout.Pt{X: (gx - gy) * tile / 2, Y: (gx + gy) * tile / 4}
	}
	type spot struct {
		i      int
		ox, oy float64 // footprint origin in tiles
		rect   image.Rectangle
		depth  float64
	}
	var spots []spot
	bounds := image.Rectangle{}
	grow := func(r image.Rectangle) {
		if bounds.Empty() {
			bounds = r
		} else {
			bounds = bounds.Union(r)
		}
	}
	// The ground.
	n := float64(P)
	for _, c := range []layout.Pt{scr(0, 0), scr(float64(cols)*n, 0), scr(0, float64(rows)*n), scr(float64(cols)*n, float64(rows)*n)} {
		grow(image.Rect(int(c.X), int(c.Y), int(c.X)+1, int(c.Y)+1))
	}
	for i, it := range items {
		w, d := float64(it.Foot[0]), float64(it.Foot[1])
		ox := float64(i%cols)*n + math.Floor((n-w)/2)
		oy := float64(i/cols)*n + math.Floor((n-d)/2)
		corner := scr(ox+w, oy+d)
		b := it.Img.Bounds()
		x0 := int(math.Round(corner.X - it.Pivot[0]*float64(b.Dx())))
		y0 := int(math.Round(corner.Y - it.Pivot[1]*float64(b.Dy())))
		r := image.Rect(x0, y0, x0+b.Dx(), y0+b.Dy())
		spots = append(spots, spot{i: i, ox: ox, oy: oy, rect: r, depth: ox + w + oy + d})
		grow(r)
	}
	bounds = bounds.Inset(-8)
	out := image.NewRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	fill(out, out.Bounds(), color.RGBA{0x2E, 0x3A, 0x2C, 255})
	at := func(p layout.Pt) layout.Pt {
		return layout.Pt{X: p.X - float64(bounds.Min.X), Y: p.Y - float64(bounds.Min.Y)}
	}
	diamond := func(gx, gy, w, d float64) layout.Diamond {
		return layout.Diamond{Top: at(scr(gx, gy)), Right: at(scr(gx+w, gy)), Bottom: at(scr(gx+w, gy+d)), Left: at(scr(gx, gy+d))}
	}
	// Footprints, then the tile grid over them.
	for _, s := range spots {
		it := items[s.i]
		c := color.RGBA{0x56, 0x6E, 0x48, 255}
		if it.Unmeasured {
			c = color.RGBA{0x8A, 0x3A, 0x36, 255}
		}
		sketch.FillDiamond(out, diamond(s.ox, s.oy, float64(it.Foot[0]), float64(it.Foot[1])), c)
	}
	grid := color.RGBA{0x4A, 0x5A, 0x46, 255}
	for k := 0.0; k <= float64(cols)*n; k++ {
		a, b := at(scr(k, 0)), at(scr(k, float64(rows)*n))
		thinLine(out, a, b, grid)
	}
	for k := 0.0; k <= float64(rows)*n; k++ {
		a, b := at(scr(0, k)), at(scr(float64(cols)*n, k))
		thinLine(out, a, b, grid)
	}
	// Sprites, back to front.
	sort.SliceStable(spots, func(a, b int) bool { return spots[a].depth < spots[b].depth })
	for _, s := range spots {
		img := items[s.i].Img
		dst := s.rect.Sub(bounds.Min)
		xdraw.Copy(out, dst.Min, img, img.Bounds(), xdraw.Over, nil)
	}
	return out
}

// thinLine draws a 1-pixel line.
func thinLine(img *image.RGBA, a, b layout.Pt, c color.RGBA) {
	n := int(math.Max(math.Abs(b.X-a.X), math.Abs(b.Y-a.Y))) + 1
	for i := 0; i <= n; i++ {
		t := float64(i) / float64(n)
		x, y := int(a.X+(b.X-a.X)*t), int(a.Y+(b.Y-a.Y)*t)
		if image.Pt(x, y).In(img.Bounds()) {
			img.SetRGBA(x, y, c)
		}
	}
}
