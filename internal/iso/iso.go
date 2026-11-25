// Package iso measures isometric sprites from what was drawn, so code can
// correct what the model got roughly right. It finds each object's base on
// the ground: a corner (the front corner and two lower edges of a box-like
// footprint) or a round base (a 2:1 ellipse). From the base come the pivot,
// the tile size the object was drawn at and its projection angle. When no
// base is found it says why; the caller decides what to do with the sprite.
package iso

import (
	"image"
	"math"
	"sort"

	"github.com/codezerg/blockout-ai-game-assets/internal/layout"
)

// Angle is the slope of ground edges in a 2:1 isometric projection, in
// degrees: atan(1/2).
var Angle = math.Atan(0.5) * 180 / math.Pi

// Base shapes.
const (
	Corner = "corner"
	Round  = "round"
)

// Base is a detected base, in sheet pixels.
type Base struct {
	Found bool
	Shape string
	// VX, VY: the footprint's front corner. For a round base it lies below
	// the drawn object, where the corner of the square around it would be.
	VX, VY float64
	// AngleL, AngleR: slopes of the left and right lower edges, degrees.
	// A round base has one slope, from its ellipse's height to width.
	AngleL, AngleR float64
	// XL, XR: the footprint's side corners.
	XL, XR float64
	// Fit: how far the silhouette strays from the fitted base (80th
	// percentile for a corner, 90th for a round base; pixels).
	Fit float64
	// Why is set when nothing usable was found.
	Why string
}

// Width is the footprint's width in sheet pixels.
func (b Base) Width() float64 { return b.XR - b.XL }

// Angle is the base's mean slope in degrees.
func (b Base) Angle() float64 { return (b.AngleL + b.AngleR) / 2 }

// Gates: what a fit must show before it is trusted.
const (
	minAngle, maxAngle = 15.0, 40.0
	minArm             = 0.2 // each lower edge, as a share of the base width
	// A base must span most of the sprite's lower body (the columns that
	// reach into its bottom third), so a crate or a bucket beside the object
	// is not taken for its base, while sails and eaves higher up do not count.
	lowerBody      = 0.35
	minCornerShare = 0.6  // base width / lower body width
	minRoundShare  = 0.45 // ellipse width / lower body width
)

// Detect finds the base of the object in img, whose top-left sits at origin
// on the sheet. want is the footprint it was asked to stand on, in sheet
// pixels; it only sets where along the edges to look first.
func Detect(img image.Image, origin image.Point, want layout.Diamond) Base {
	yb, top := silhouette(img)
	ymax := -1.0
	for _, y := range yb {
		ymax = math.Max(ymax, y)
	}
	if ymax < 0 {
		return Base{Why: "empty"}
	}
	vx := frontColumn(yb, ymax)
	hw := math.Max(8, (want.Right.X-want.Left.X)/2)

	c, cwhy := fitCorner(yb, vx, ymax, hw)
	r, rwhy := fitRound(yb, vx, ymax)
	var b Base
	switch {
	case c.Found && r.Found:
		// Both fit: keep the one the silhouette follows more closely for its size.
		if c.Fit/c.Width() <= r.Fit/r.Width() {
			b = c
		} else {
			b = r
		}
	case c.Found:
		b = c
	case r.Found:
		b = r
	default:
		return Base{Why: "no corner (" + cwhy + ") and no round base (" + rwhy + ")"}
	}
	// The lower body's width: the columns whose lowest pixel is in the
	// sprite's bottom third.
	floor := ymax - lowerBody*(ymax-top)
	first, last := -1, -1
	for x, y := range yb {
		if y >= floor {
			if first < 0 {
				first = x
			}
			last = x
		}
	}
	share := b.Width() / float64(last-first+1)
	if b.Shape == Round {
		share /= math.Sqrt2 // the ellipse, not the footprint around it
		if share < minRoundShare {
			return Base{Why: "round base too small for the sprite"}
		}
	} else if share < minCornerShare {
		return Base{Why: "corner base too small for the sprite"}
	}
	ox, oy := float64(origin.X), float64(origin.Y)
	b.VX += ox
	b.VY += oy
	b.XL += ox
	b.XR += ox
	return b
}

// silhouette is the lowest opaque row in every column, or -1, and the
// sprite's top row.
func silhouette(img image.Image) ([]float64, float64) {
	b := img.Bounds()
	yb := make([]float64, b.Dx())
	top := float64(b.Dy())
	for x := range yb {
		for y := 0; y < b.Dy(); y++ {
			if _, _, _, a := img.At(b.Min.X+x, b.Min.Y+y).RGBA(); a >= 0x8000 {
				top = math.Min(top, float64(y))
				break
			}
		}
		yb[x] = -1
		for y := b.Dy() - 1; y >= 0; y-- {
			if _, _, _, a := img.At(b.Min.X+x, b.Min.Y+y).RGBA(); a >= 0x8000 {
				yb[x] = float64(y)
				break
			}
		}
	}
	return yb, top
}

// frontColumn is the middle of the run of lowest columns nearest the
// sprite's centre.
func frontColumn(yb []float64, ymax float64) int {
	best, bestD := 0, math.Inf(1)
	for x := 0; x < len(yb); {
		if yb[x] < ymax-1 {
			x++
			continue
		}
		start, end := x, x
		for x < len(yb) && x-end <= 3 {
			if yb[x] >= ymax-1 {
				end = x
			}
			x++
		}
		mid := (start + end) / 2
		if d := math.Abs(float64(mid) - float64(len(yb))/2); d < bestD {
			best, bestD = mid, d
		}
	}
	return best
}

// fitCorner fits two straight lower edges meeting at the front corner.
func fitCorner(yb []float64, vx int, ymax, hw float64) (Base, string) {
	W := len(yb)
	fit := func(lo, hi float64) (slope, icept float64, n int) {
		var xs, ys []float64
		for x := int(math.Ceil(lo)); x <= int(math.Floor(hi)); x++ {
			if x >= 0 && x < W && yb[x] >= 0 {
				xs, ys = append(xs, float64(x)), append(ys, yb[x])
			}
		}
		if len(xs) < 6 {
			return 0, 0, len(xs)
		}
		return theilSen(xs, ys)
	}
	// Walk out along an edge until the silhouette jumps up (a wall or eave:
	// the side corner) or drops away; slow drift is allowed.
	rise, drop := math.Max(4, 0.06*hw), math.Max(6, 0.12*hw)
	walk := func(step int, slope, icept float64) int {
		last, miss := vx, 0
		for x := vx; x >= 0 && x < W; x += step {
			dev := yb[x] - (slope*float64(x) + icept)
			if yb[x] >= 0 && dev > -rise && dev < drop {
				last, miss = x, 0
				continue
			}
			if miss++; miss > 3 {
				break
			}
		}
		return last
	}
	fx := float64(vx)
	sl, il, nl := fit(fx-0.6*hw, fx-0.1*hw)
	sr, ir, nr := fit(fx+0.1*hw, fx+0.6*hw)
	if nl < 6 || nr < 6 {
		return Base{}, "too narrow to fit its lower edges"
	}
	slopeOK := func(s float64) bool {
		a := math.Atan(math.Abs(s)) * 180 / math.Pi
		return a >= minAngle && a <= maxAngle
	}
	if sl <= 0 || sr >= 0 || !slopeOK(sl) || !slopeOK(sr) {
		return Base{}, "lower edges do not slope like an isometric base"
	}
	xl, xr := walk(-1, sl, il), walk(1, sr, ir)
	// Refit each edge over its whole length, then walk again.
	if L := fx - float64(xl); L >= 10 {
		if s, i, n := fit(fx-0.9*L, fx-0.1*L); n >= 6 && s > 0 {
			sl, il = s, i
		}
	}
	if R := float64(xr) - fx; R >= 10 {
		if s, i, n := fit(fx+0.1*R, fx+0.9*R); n >= 6 && s < 0 {
			sr, ir = s, i
		}
	}
	if !slopeOK(sl) || !slopeOK(sr) {
		return Base{}, "lower edges do not slope like an isometric base"
	}
	xl, xr = walk(-1, sl, il), walk(1, sr, ir)
	L, R := fx-float64(xl), float64(xr)-fx
	if L < minArm*(L+R) || R < minArm*(L+R) {
		return Base{}, "one lower edge is too short"
	}
	var devs []float64
	for x := xl; x <= xr; x++ {
		if yb[x] < 0 {
			continue
		}
		line := sl*float64(x) + il
		if x > vx {
			line = sr*float64(x) + ir
		}
		devs = append(devs, math.Abs(yb[x]-line))
	}
	return Base{
		Found:  true,
		Shape:  Corner,
		VX:     fx + 0.5,
		VY:     math.Max(sl*fx+il, sr*fx+ir) + 1,
		AngleL: math.Atan(sl) * 180 / math.Pi,
		AngleR: math.Atan(-sr) * 180 / math.Pi,
		XL:     float64(xl),
		XR:     float64(xr) + 1,
		Fit:    percentile(devs, 0.8),
	}, ""
}

// fitRound fits the lower half of a ground ellipse under the front column:
// the largest one the silhouette follows. The footprint is the diamond
// around it, √2 times as wide.
func fitRound(yb []float64, vx int, ymax float64) (Base, string) {
	W := len(yb)
	devs := make([]float64, 0, W)
	for a := (W + 1) / 2; a >= 8; a-- {
		lo, hi := vx-int(0.98*float64(a)), vx+int(0.98*float64(a))
		if lo < 0 || hi >= W {
			continue
		}
		tol := math.Max(1.5, 0.025*float64(a))
		// At the largest width that fits, keep the aspect that fits best.
		bestP, bestAsp := math.Inf(1), 0.0
		for asp := 0.2; asp <= 0.70001; asp += 0.01 {
			b := float64(a) * asp
			cy := ymax + 0.5 - b // the ellipse's lowest point is the lowest row's centre
			devs = devs[:0]
			for x := lo; x <= hi; x++ {
				if yb[x] < 0 {
					break // the base is solid: every column under it is drawn
				}
				u := (float64(x) - float64(vx)) / float64(a)
				devs = append(devs, math.Abs(yb[x]+0.5-(cy+b*math.Sqrt(1-u*u))))
			}
			if len(devs) < hi-lo+1 {
				break
			}
			if p := percentile(devs, 0.9); p < bestP {
				bestP, bestAsp = p, asp
			}
		}
		if bestP > tol {
			continue
		}
		b := float64(a) * bestAsp
		cy := ymax + 0.5 - b
		c := float64(vx) + 0.5
		half := float64(a) * math.Sqrt2
		ang := math.Atan(bestAsp) * 180 / math.Pi
		return Base{
			Found: true, Shape: Round,
			VX: c, VY: cy + b*math.Sqrt2,
			AngleL: ang, AngleR: ang,
			XL: c - half, XR: c + half,
			Fit: bestP,
		}, ""
	}
	return Base{}, "the bottom does not follow an ellipse"
}

// theilSen fits y = slope*x + icept robustly: the median of pairwise
// slopes, then the median intercept.
func theilSen(xs, ys []float64) (slope, icept float64, n int) {
	n = len(xs)
	step := max(1, n/60) // subsample long runs
	var slopes []float64
	for i := 0; i < n; i += step {
		for j := i + step; j < n; j += step {
			if xs[j] != xs[i] {
				slopes = append(slopes, (ys[j]-ys[i])/(xs[j]-xs[i]))
			}
		}
	}
	slope = percentile(slopes, 0.5)
	ic := make([]float64, n)
	for i := range xs {
		ic[i] = ys[i] - slope*xs[i]
	}
	return slope, percentile(ic, 0.5), n
}

func percentile(v []float64, q float64) float64 {
	if len(v) == 0 {
		return 0
	}
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	return s[min(len(s)-1, int(q*float64(len(s))))]
}
