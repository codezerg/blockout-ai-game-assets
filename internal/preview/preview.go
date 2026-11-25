// Package preview draws one image for reviewing a run at a glance:
// sketch | sheet with layout boxes and drawn bounds | the cut-out pieces on a
// checkerboard, with a strip of UI checks below (UIStrip). Isometric sheets
// also get a scene of the sprites stood on a tile grid (Scene).
package preview

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"strings"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"

	"github.com/codezerg/blockout-ai-game-assets/internal/layout"
	"github.com/codezerg/blockout-ai-game-assets/internal/measure"
)

const (
	panel = 512 // each panel fits in panel×panel
	gap   = 12
	line  = 28 // title line height at scale 2
)

var (
	paper   = color.RGBA{0x24, 0x24, 0x28, 255}
	white   = color.RGBA{0xFF, 0xFF, 0xFF, 255}
	black   = color.RGBA{0x00, 0x00, 0x00, 255}
	good    = color.RGBA{0x00, 0xC8, 0xFF, 255} // drawn bounds inside box + margin
	bad     = color.RGBA{0xFF, 0x30, 0x30, 255} // spills past the margin
	missing = color.RGBA{0xFF, 0xB0, 0x00, 255}
)

// Options add to the preview: marks drawn on the sheet panel, and pivots
// (fractions of each piece file) shown as dots on the pieces panel. With
// SameScale every piece is drawn at one scale, so relative sizes show. A
// Scene (see Scene) replaces the pieces panel.
type Options struct {
	Marks     []Mark
	Pivots    [][2]float64 // per piece; NaN when it has none
	SameScale bool
	Scene     image.Image
}

// Render builds the preview. title may hold several lines. sk is the sketch
// (or a guide for style none), pieces are the extracted piece images (nil
// when missing), sc may be nil.
func Render(title string, l *layout.Layout, sk, sheet image.Image, pieces []image.Image, sc *measure.Scores, opt Options) *image.RGBA {
	marks := opt.Marks
	sb := sheet.Bounds()
	pw, ph := fit(sb.Dx(), sb.Dy(), panel)
	lines := strings.Split(title, "\n")
	header := 12 + line*len(lines)
	W := gap + 3*(pw+gap)
	H := header + ph + gap
	out := image.NewRGBA(image.Rect(0, 0, W, H))
	fill(out, out.Bounds(), paper)
	for i, t := range lines {
		text(out, gap, 8+i*line, t, white, 2)
	}

	// 1: sketch.
	p1 := image.Rect(gap, header, gap+pw, header+ph)
	xdraw.CatmullRom.Scale(out, p1, sk, sk.Bounds(), xdraw.Src, nil)

	// 2: sheet with overlays.
	p2 := p1.Add(image.Pt(pw+gap, 0))
	xdraw.CatmullRom.Scale(out, p2, sheet, sb, xdraw.Src, nil)
	lw, lh := l.Space()
	fromLayout := func(x, y, w, h int) image.Rectangle {
		fx, fy := float64(pw)/float64(lw), float64(ph)/float64(lh)
		return image.Rect(int(float64(x)*fx), int(float64(y)*fy), int(float64(x+w)*fx), int(float64(y+h)*fy)).Add(p2.Min)
	}
	for i, p := range l.Pieces {
		r := fromLayout(p.Box.X, p.Box.Y, p.Box.W, p.Box.H)
		stroke(out, r, black, 2)
		stroke(out, r.Inset(1), white, 1)
		if sc == nil || i >= len(sc.Pieces) {
			continue
		}
		ps := sc.Pieces[i]
		if !ps.Found {
			stroke(out, r.Inset(3), missing, 2)
			continue
		}
		c := good
		if ps.MaxSpill > measure.Margin {
			c = bad
		}
		d := ps.Drawn
		stroke(out, fromLayout(d.X, d.Y, d.W, d.H), c, 2)
	}
	for _, m := range marks {
		fx, fy := float64(pw)/float64(lw), float64(ph)/float64(lh)
		drawLine(out, float64(p2.Min.X)+m.A.X*fx, float64(p2.Min.Y)+m.A.Y*fy, float64(p2.Min.X)+m.B.X*fx, float64(p2.Min.Y)+m.B.Y*fy, m.C)
	}
	if sc != nil {
		for _, d := range sc.Stray {
			stroke(out, fromLayout(d.X, d.Y, d.W, d.H), missing, 2)
		}
	}

	// 3: the extracted pieces over a checkerboard, so transparency shows, or
	// the scene they make.
	p3 := p2.Add(image.Pt(pw+gap, 0))
	if sc := opt.Scene; sc != nil {
		b := sc.Bounds()
		w, h := fitBox(b.Dx(), b.Dy(), p3.Dx(), p3.Dy())
		fill(out, p3, color.RGBA{0x2E, 0x3A, 0x2C, 255})
		dst := image.Rect(0, 0, w, h).Add(p3.Min).Add(image.Pt((p3.Dx()-w)/2, (p3.Dy()-h)/2))
		xdraw.CatmullRom.Scale(out, dst, sc, b, xdraw.Src, nil)
		return out
	}
	checker(out, p3)
	n := len(pieces)
	cols := 1
	for cols*cols < n {
		cols++
	}
	rows := (n + cols - 1) / cols
	cw, ch := pw/cols, ph/max(rows, 1)
	same := math.Inf(1) // one scale for every piece: the one that fits the largest
	for _, pc := range pieces {
		if pc != nil {
			b := pc.Bounds()
			same = math.Min(same, math.Min(float64(cw-12)/float64(b.Dx()), float64(ch-12)/float64(b.Dy())))
		}
	}
	for i, pc := range pieces {
		cell := image.Rect(0, 0, cw, ch).Add(p3.Min).Add(image.Pt((i%cols)*cw, (i/cols)*ch)).Inset(6)
		text(out, cell.Min.X-4, cell.Min.Y-4, fmt.Sprint(i+1), white, 1)
		if pc == nil {
			stroke(out, cell, missing, 2)
			continue
		}
		b := pc.Bounds()
		w, h := fitBox(b.Dx(), b.Dy(), cell.Dx(), cell.Dy())
		if opt.SameScale {
			w, h = max(1, int(float64(b.Dx())*same)), max(1, int(float64(b.Dy())*same))
		}
		// Bottom-aligned, so sprites stand on a common line.
		dst := image.Rect(0, 0, w, h).Add(cell.Min).Add(image.Pt((cell.Dx()-w)/2, cell.Dy()-h))
		xdraw.CatmullRom.Scale(out, dst, pc, b, xdraw.Over, nil)
		if i < len(opt.Pivots) && !math.IsNaN(opt.Pivots[i][0]) {
			px := dst.Min.X + int(opt.Pivots[i][0]*float64(w))
			py := dst.Min.Y + int(opt.Pivots[i][1]*float64(h))
			fill(out, image.Rect(px-3, py-3, px+4, py+4), color.RGBA{0xFF, 0x20, 0x20, 255})
			fill(out, image.Rect(px-1, py-1, px+2, py+2), white)
		}
	}
	return out
}

// checker fills r with a grey checkerboard.
func checker(img *image.RGBA, r image.Rectangle) {
	const sq = 8
	a, b := color.RGBA{0x9A, 0x9A, 0x9A, 255}, color.RGBA{0x6A, 0x6A, 0x6A, 255}
	for y := r.Min.Y; y < r.Max.Y; y += sq {
		for x := r.Min.X; x < r.Max.X; x += sq {
			c := a
			if ((x-r.Min.X)/sq+(y-r.Min.Y)/sq)%2 == 1 {
				c = b
			}
			fill(img, image.Rect(x, y, x+sq, y+sq).Intersect(r), c)
		}
	}
}

// fit scales w×h to fit in a size×size square.
func fit(w, h, size int) (int, int) { return fitBox(w, h, size, size) }

func fitBox(w, h, bw, bh int) (int, int) {
	if w*bh > h*bw {
		return bw, max(1, h*bw/w)
	}
	return max(1, w*bh/h), bh
}

func fill(img *image.RGBA, r image.Rectangle, c color.RGBA) {
	xdraw.Draw(img, r, image.NewUniform(c), image.Point{}, xdraw.Src)
}

func stroke(img *image.RGBA, r image.Rectangle, c color.RGBA, t int) {
	for _, s := range []image.Rectangle{
		{r.Min, image.Pt(r.Max.X, r.Min.Y+t)},
		{image.Pt(r.Min.X, r.Max.Y-t), r.Max},
		{r.Min, image.Pt(r.Min.X+t, r.Max.Y)},
		{image.Pt(r.Max.X-t, r.Min.Y), r.Max},
	} {
		fill(img, s.Intersect(img.Bounds()), c)
	}
}

func text(dst *image.RGBA, x, y int, s string, c color.RGBA, scale int) {
	face := basicfont.Face7x13
	w := font.MeasureString(face, s).Ceil() + 2
	small := image.NewRGBA(image.Rect(0, 0, w, 13))
	d := font.Drawer{Dst: small, Src: image.NewUniform(c), Face: face, Dot: fixed.P(1, 11)}
	d.DrawString(s)
	r := image.Rect(x, y, x+w*scale, y+13*scale)
	xdraw.NearestNeighbor.Scale(dst, r, small, small.Bounds(), xdraw.Over, nil)
}

// Mark is a line on the sheet panel, in layout units.
type Mark struct {
	A, B layout.Pt
	C    color.RGBA
}

// drawLine draws a 2-pixel line.
func drawLine(img *image.RGBA, x0, y0, x1, y1 float64, c color.RGBA) {
	n := int(math.Max(math.Abs(x1-x0), math.Abs(y1-y0))) + 1
	for i := 0; i <= n; i++ {
		t := float64(i) / float64(n)
		x, y := int(x0+(x1-x0)*t), int(y0+(y1-y0)*t)
		fill(img, image.Rect(x, y, x+2, y+2).Intersect(img.Bounds()), c)
	}
}
