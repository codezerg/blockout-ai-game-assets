// Package ui adds what UI pieces need after extraction: nine-slice insets
// for panels and buttons, and button states padded onto one shared canvas
// so an engine can swap them without the art jumping.
package ui

import (
	"image"
	"image/color"
	"sort"
	"strings"

	"github.com/codezerg/blockout-ai-game-assets/internal/layout"

	xdraw "golang.org/x/image/draw"
)

// Insets are nine-slice margins in piece pixels: the fixed corners and
// edges; everything between them stretches.
type Insets struct {
	Left   int `json:"left"`
	Top    int `json:"top"`
	Right  int `json:"right"`
	Bottom int `json:"bottom"`
}

// Sliced reports whether pieces of this kind get nine-slice insets.
func Sliced(kind string) bool { return kind == "nine_slice" || kind == "button" }

const (
	// flatFactor: a column boundary is flat when its difference is at most
	// this many times the piece's median difference.
	flatFactor = 2.5
	// flatFloor is the smallest threshold, for very smooth art.
	flatFloor = 1.5
	// bands: differences are averaged within this many bands across the
	// other axis and the worst band counts, so a small ornament in one edge
	// is not averaged away by the plain rest.
	bands = 16
	// safety pixels kept between the flat run's ends and the insets.
	safety = 2
	// minStretch: the stretch region must be at least 1/minStretch of the
	// piece along an axis to count.
	minStretch = 10
)

// Slice is the result of looking for a nine-slice along both axes.
type Slice struct {
	Insets Insets
	// FlatX, FlatY: the centre column (row) lies in a flat run, so the
	// piece stretches along that axis without smearing anything.
	FlatX, FlatY bool
	// SeamX, SeamY: mean difference (0-255) between the first and last
	// column (row) of the stretch region. Growing never shows a seam, but
	// shrinking below the drawn size or tiling puts those two side by side;
	// a large value means scale the texture instead.
	SeamX, SeamY float64
}

// FindSlice finds nine-slice insets for img: along each axis, the run of
// mutually similar columns (rows) through the centre stretches, and the
// rest is fixed.
func FindSlice(img image.Image) Slice {
	p := premul(img)
	b := img.Bounds()
	W, H := b.Dx(), b.Dy()
	var s Slice
	var l, r, t, bt int
	l, r, s.FlatX = flatRun(profile(p, W, H, true))
	t, bt, s.FlatY = flatRun(profile(p, W, H, false))
	s.Insets = Insets{Left: l, Right: W - r, Top: t, Bottom: H - bt}
	s.SeamX = lineDiff(p, W, H, l, r-1, true)
	s.SeamY = lineDiff(p, W, H, t, bt-1, false)
	return s
}

// FindSliceAt measures the seams of img at given insets (a state group
// shares the widest insets, which may not be the piece's own).
func FindSliceAt(img image.Image, in Insets) Slice {
	p := premul(img)
	b := img.Bounds()
	W, H := b.Dx(), b.Dy()
	return Slice{
		Insets: in,
		SeamX:  lineDiff(p, W, H, in.Left, W-in.Right-1, true),
		SeamY:  lineDiff(p, W, H, in.Top, H-in.Bottom-1, false),
	}
}

const (
	// contentTol: a pixel matches the plain centre when its mean channel
	// difference from it (premultiplied, 0-255) is at most this.
	contentTol = 12
	// contentRun: the rim ends where this many pixels in a row match.
	contentRun = 3
)

// Content finds where a sliced piece's plain centre begins on each side:
// the rim's thickness, which is where an engine should put content (a label,
// a board, text). It is often thinner than the slice inset, which must also
// cover any one-off mark along an edge, such as a glare streak, so that it
// does not stretch; using the slice to place content would push it off
// centre.
//
// Along each row through the stretch region (rows that cross only the side
// rims, not the corners) the plain centre is followed outward from the
// middle until a run of pixels stops matching it; that is the rim's inner
// edge. Walking outward, not in from the edge, matters: a rim can hold a
// dark groove as dark as the centre (the E9 well frame). The inset is the
// median over rows, so a mark crossing a few rows does not move it. Columns
// give the top and bottom the same way. Each inset is at most its slice
// inset, which is also the answer for a textured face that never matches
// its middle for long.
func Content(img image.Image, in Insets) Insets {
	p := premul(img)
	b := img.Bounds()
	W, H := b.Dx(), b.Dy()
	if W < 3 || H < 3 {
		return in
	}
	left, right := rimDepths(p, W, H, in.Top, H-in.Bottom, true)
	top, bottom := rimDepths(p, W, H, in.Left, W-in.Right, false)
	return Insets{
		Left:   min(left, in.Left),
		Right:  min(right, in.Right),
		Top:    min(top, in.Top),
		Bottom: min(bottom, in.Bottom),
	}
}

// rimDepths measures, over lines [from, to) across the other axis (rows
// when alongX), the rim depth at both ends of each line, and returns the
// medians. A line that matches its middle all the way out has no rim.
func rimDepths(p []float32, W, H, from, to int, alongX bool) (int, int) {
	n, m := W, H // n: length of a line, m: number of lines
	if !alongX {
		n, m = H, W
	}
	if from < 0 || to > m || to-from < 1 {
		from, to = m/3, max(m/3+1, 2*m/3) // no stretch region: middle third
	}
	at := func(i, j int) int { // i along the line, j: which line
		if alongX {
			return (j*W + i) * 4
		}
		return (i*W + j) * 4
	}
	diff := func(a, b int) float32 {
		var s float32
		for c := 0; c < 4; c++ {
			v := p[a+c] - p[b+c]
			if v < 0 {
				v = -v
			}
			s += v
		}
		return s / 4
	}
	c := n / 2
	var lo, hi []int
	for j := from; j < to; j++ {
		ref := at(c, j)
		// rimStart walks from the middle toward one end (step -1 or +1) and
		// returns the index of the first pixel of the first run of
		// contentRun pixels that differ from the middle, or -1.
		rimStart := func(step int) int {
			run := 0
			for i := c; i >= 0 && i < n; i += step {
				if diff(at(i, j), ref) <= contentTol {
					run = 0
					continue
				}
				if run++; run == contentRun {
					return i - step*(contentRun-1)
				}
			}
			return -1
		}
		l, h := 0, 0
		if f := rimStart(-1); f >= 0 {
			l = f + 1
		}
		if f := rimStart(1); f >= 0 {
			h = n - f
		}
		lo = append(lo, l)
		hi = append(hi, h)
	}
	return median(lo), median(hi)
}

func median(v []int) int {
	if len(v) == 0 {
		return 0
	}
	s := append([]int(nil), v...)
	sort.Ints(s)
	return s[len(s)/2]
}

// premul returns premultiplied RGBA as floats, row-major.
func premul(img image.Image) []float32 {
	b := img.Bounds()
	W, H := b.Dx(), b.Dy()
	out := make([]float32, W*H*4)
	for y := 0; y < H; y++ {
		for x := 0; x < W; x++ {
			c := color.NRGBAModel.Convert(img.At(b.Min.X+x, b.Min.Y+y)).(color.NRGBA)
			a := float32(c.A) / 255
			o := (y*W + x) * 4
			out[o] = float32(c.R) * a
			out[o+1] = float32(c.G) * a
			out[o+2] = float32(c.B) * a
			out[o+3] = float32(c.A)
		}
	}
	return out
}

// profile gives, for every boundary between neighbouring columns (alongX)
// or rows, how different the two are: the mean absolute channel difference
// within each band across the other axis, worst band.
func profile(p []float32, W, H int, alongX bool) []float64 {
	n, m := W, H // n: positions along the axis, m: across it
	if !alongX {
		n, m = H, W
	}
	if n < 2 {
		return nil
	}
	nb := min(bands, m)
	d := make([]float64, n-1)
	at := func(i, j int) int { // i along, j across
		if alongX {
			return (j*W + i) * 4
		}
		return (i*W + j) * 4
	}
	for i := 0; i < n-1; i++ {
		worst := 0.0
		for k := 0; k < nb; k++ {
			j0, j1 := k*m/nb, (k+1)*m/nb
			var sum float64
			for j := j0; j < j1; j++ {
				a, b := at(i, j), at(i+1, j)
				for c := 0; c < 4; c++ {
					v := p[a+c] - p[b+c]
					if v < 0 {
						v = -v
					}
					sum += float64(v)
				}
			}
			if j1 > j0 {
				worst = max(worst, sum/float64(4*(j1-j0)))
			}
		}
		d[i] = worst
	}
	return d
}

// lineDiff is the mean absolute channel difference between columns (or
// rows) i and j of premultiplied pixels p.
func lineDiff(p []float32, W, H, i, j int, cols bool) float64 {
	n := H
	if !cols {
		n = W
	}
	var sum float64
	for k := 0; k < n; k++ {
		a, b := (k*W+i)*4, (k*W+j)*4
		if !cols {
			a, b = (i*W+k)*4, (j*W+k)*4
		}
		for c := 0; c < 4; c++ {
			v := float64(p[a+c] - p[b+c])
			if v < 0 {
				v = -v
			}
			sum += v
		}
	}
	return sum / float64(4*n)
}

// flatRun returns the stretchable span [lo, hi) of positions: the flat run
// of boundaries through the centre, shrunk by the safety margin. ok is
// false when the centre is not flat; then the longest flat run is used.
func flatRun(d []float64) (lo, hi int, ok bool) {
	n := len(d) + 1
	if len(d) == 0 {
		return 0, n, false
	}
	sorted := append([]float64(nil), d...)
	sort.Float64s(sorted)
	thr := max(flatFloor, flatFactor*sorted[len(sorted)/2])
	flat := func(i int) bool { return d[i] <= thr }

	c := len(d) / 2
	a, b := c, c // boundary run [a, b]
	if flat(c) {
		ok = true
		for a > 0 && flat(a-1) {
			a--
		}
		for b < len(d)-1 && flat(b+1) {
			b++
		}
	} else {
		best := -1
		for i := 0; i < len(d); {
			if !flat(i) {
				i++
				continue
			}
			j := i
			for j < len(d)-1 && flat(j+1) {
				j++
			}
			if j-i > best {
				best, a, b = j-i, i, j
			}
			i = j + 1
		}
		if best < 0 {
			return n / 2, n/2 + 1, false
		}
	}
	// Boundaries a..b join positions a..b+1, which are all alike.
	lo, hi = a+safety, b+2-safety
	if hi <= lo {
		mid := (a + b + 2) / 2
		lo, hi = mid, mid+1
	}
	// A sliver is no stretch region: whatever is around it gets smeared
	// once the piece grows.
	if (hi-lo)*minStretch < n {
		ok = false
	}
	return lo, hi, ok
}

// Stretch draws img nine-sliced to w×h: corners fixed, edges stretched
// along their length, centre stretched both ways. w and h must be at least
// the insets' sums.
func Stretch(img image.Image, in Insets, w, h int) *image.NRGBA {
	b := img.Bounds()
	xs := [4]int{0, in.Left, b.Dx() - in.Right, b.Dx()}
	ys := [4]int{0, in.Top, b.Dy() - in.Bottom, b.Dy()}
	xd := [4]int{0, in.Left, w - in.Right, w}
	yd := [4]int{0, in.Top, h - in.Bottom, h}
	out := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			sr := image.Rect(xs[i], ys[j], xs[i+1], ys[j+1]).Add(b.Min)
			dr := image.Rect(xd[i], yd[j], xd[i+1], yd[j+1])
			if sr.Empty() || dr.Empty() {
				continue
			}
			xdraw.BiLinear.Scale(out, dr, img, sr, xdraw.Src, nil)
		}
	}
	return out
}

// States maps every button state piece to its base piece: a button whose id
// is another button's id plus "_<state>" and whose box is the same size.
// The longest matching base wins.
func States(l *layout.Layout) map[int]int {
	out := map[int]int{}
	for i, p := range l.Pieces {
		if p.Kind != "button" {
			continue
		}
		best := -1
		for j, q := range l.Pieces {
			if j == i || q.Kind != "button" || q.Box.W != p.Box.W || q.Box.H != p.Box.H {
				continue
			}
			if strings.HasPrefix(p.ID, q.ID+"_") && (best < 0 || len(q.ID) > len(l.Pieces[best].ID)) {
				best = j
			}
		}
		if best >= 0 {
			out[i] = best
		}
	}
	// A base must not itself be a state: map every state to the root of its
	// chain. Ids get shorter along a chain, so the walk ends.
	roots := make(map[int]int, len(out))
	for i, base := range out {
		for {
			next, ok := out[base]
			if !ok {
				break
			}
			base = next
		}
		roots[i] = base
	}
	return roots
}

// Match describes how a state lines up with its base.
type Match struct {
	Of       string  `json:"of"`
	ShapeIoU float64 `json:"shape_iou"`  // opaque-silhouette overlap once aligned
	DW       int     `json:"dw"`         // width minus the base's
	DH       int     `json:"dh"`         // height minus the base's
	ColorDif float64 `json:"color_diff"` // mean RGB difference where both are opaque, 0-255
	DX       int     `json:"dx"`         // where its art sits relative to the base's, after any resize
	DY       int     `json:"dy"`
	// Resized: the state was drawn within MaxResize of the base's size and
	// has been scaled to exactly that size, so swapping states does not
	// move the button's edges. DW and DH still give the size as drawn.
	Resized bool `json:"resized,omitempty"`
}

// MaxResize is the largest size difference, as a share of the base's
// width or height, that is fixed by resizing a state rather than flagged.
const MaxResize = 0.05

// FitState scales state to base's size when the two differ by at most
// MaxResize along each axis; otherwise it returns state unchanged.
func FitState(base, state image.Image) (image.Image, bool) {
	bb, sb := base.Bounds(), state.Bounds()
	if bb.Size() == sb.Size() {
		return state, false
	}
	if float64(abs(bb.Dx()-sb.Dx())) > MaxResize*float64(bb.Dx()) || float64(abs(bb.Dy()-sb.Dy())) > MaxResize*float64(bb.Dy()) {
		return state, false
	}
	out := image.NewNRGBA(image.Rect(0, 0, bb.Dx(), bb.Dy()))
	xdraw.CatmullRom.Scale(out, out.Bounds(), state, sb, xdraw.Src, nil)
	return out, true
}

// Align finds the offset of state relative to base (top-left to top-left)
// that best overlaps their opaque silhouettes, searching around centre
// alignment.
func Align(base, state image.Image) Match {
	ba, sa := mask(base), mask(state)
	bb, sb := base.Bounds(), state.Bounds()
	cx := (bb.Dx() - sb.Dx()) / 2
	cy := (bb.Dy() - sb.Dy()) / 2
	R := 8 + max(abs(bb.Dx()-sb.Dx()), abs(bb.Dy()-sb.Dy()))/2
	m := Match{DW: sb.Dx() - bb.Dx(), DH: sb.Dy() - bb.Dy(), ShapeIoU: -1}
	nb, ns := count(ba), count(sa)
	for dy := cy - R; dy <= cy+R; dy++ {
		for dx := cx - R; dx <= cx+R; dx++ {
			inter := overlap(ba, bb.Dx(), bb.Dy(), sa, sb.Dx(), sb.Dy(), dx, dy)
			u := nb + ns - inter
			if u == 0 {
				continue
			}
			iou := float64(inter) / float64(u)
			// Ties go to the offset nearest centre alignment.
			if iou > m.ShapeIoU+1e-9 || (iou > m.ShapeIoU-1e-9 && abs(dx-cx)+abs(dy-cy) < abs(m.DX-cx)+abs(m.DY-cy)) {
				m.ShapeIoU, m.DX, m.DY = iou, dx, dy
			}
		}
	}
	m.ColorDif = colorDiff(base, state, m.DX, m.DY)
	return m
}

func mask(img image.Image) []bool {
	b := img.Bounds()
	out := make([]bool, b.Dx()*b.Dy())
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			_, _, _, a := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			out[y*b.Dx()+x] = a >= 0x8000
		}
	}
	return out
}

func count(m []bool) int {
	n := 0
	for _, v := range m {
		if v {
			n++
		}
	}
	return n
}

// overlap counts pixels opaque in both when s is placed at dx,dy on b.
func overlap(b []bool, bw, bh int, s []bool, sw, sh, dx, dy int) int {
	n := 0
	for y := max(0, -dy); y < min(sh, bh-dy); y++ {
		srow := s[y*sw:]
		brow := b[(y+dy)*bw:]
		for x := max(0, -dx); x < min(sw, bw-dx); x++ {
			if srow[x] && brow[x+dx] {
				n++
			}
		}
	}
	return n
}

func colorDiff(base, state image.Image, dx, dy int) float64 {
	bb, sb := base.Bounds(), state.Bounds()
	var sum float64
	n := 0
	for y := 0; y < sb.Dy(); y++ {
		for x := 0; x < sb.Dx(); x++ {
			bx, by := x+dx, y+dy
			if bx < 0 || by < 0 || bx >= bb.Dx() || by >= bb.Dy() {
				continue
			}
			c1 := color.NRGBAModel.Convert(base.At(bb.Min.X+bx, bb.Min.Y+by)).(color.NRGBA)
			c2 := color.NRGBAModel.Convert(state.At(sb.Min.X+x, sb.Min.Y+y)).(color.NRGBA)
			if c1.A < 128 || c2.A < 128 {
				continue
			}
			sum += float64(abs(int(c1.R)-int(c2.R))+abs(int(c1.G)-int(c2.G))+abs(int(c1.B)-int(c2.B))) / 3
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return sum / float64(n)
}

// Pad draws img at off on a transparent w×h canvas.
func Pad(img image.Image, off image.Point, w, h int) *image.NRGBA {
	out := image.NewNRGBA(image.Rect(0, 0, w, h))
	xdraw.Draw(out, img.Bounds().Sub(img.Bounds().Min).Add(off), img, img.Bounds().Min, xdraw.Src)
	return out
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
