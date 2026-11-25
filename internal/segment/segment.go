// Package segment finds what was drawn on a sheet and which piece owns it.
// A pixel is drawn when it is far enough from the key colour; drawn pixels
// form 8-connected regions; each region belongs to the layout box it overlaps
// most. This is the shared ground truth for scoring and extraction.
package segment

import (
	"image"
	"image/color"
	"image/draw"
	"math"

	"github.com/codezerg/blockout-ai-game-assets/internal/layout"
)

const (
	// FgDistance is the RGB distance from the key colour beyond which a
	// pixel counts as drawn.
	FgDistance = 90
	// DarkFgDistance replaces FgDistance for dark keys such as black, where
	// dark outlines and shadows in the art come close to the key.
	DarkFgDistance = 40
	// SpeckPad is how far (layout units) outside a piece's drawn bounds a
	// small region may sit and still belong to it.
	SpeckPad = 4
)

// Owner values for regions that belong to no piece.
const (
	Stray = -1 // a sizeable region outside every box
	Speck = -2 // a tiny region near no piece; ignored
)

type Region struct {
	Rect    image.Rectangle // sheet pixels
	Area    int
	Owner   int   // piece index, Stray or Speck
	Big     bool  // at least MinArea; only big regions count for placement
	Touched []int // boxes its pixels cover by more than 5%, its owner's included
	InBox   []int // its pixels inside each box (big regions only)
}

type Result struct {
	W, H   int
	Key    color.RGBA
	FgDist float64
	Label  []int32 // region index + 1 per pixel; 0 is near the key colour
	Regs   []Region
	Boxes  []image.Rectangle // layout boxes in sheet pixels
	SX, SY float64           // sheet pixels per layout unit
	// Background statistics over pixels near the key colour.
	BgNoise  float64 // mean distance from the key
	BgOffKey float64 // share more than 30 from the key
}

// Run segments sheet against l.
func Run(l *layout.Layout, sheet image.Image) *Result {
	src := ToRGBA(sheet)
	W, H := src.Bounds().Dx(), src.Bounds().Dy()
	key, _ := l.Background()
	r := &Result{W: W, H: H, Key: key, FgDist: FgDistance}
	if max(key.R, key.G, key.B) < 64 {
		r.FgDist = DarkFgDistance
	}

	fg := make([]bool, W*H)
	var bgSum float64
	var bgN, bgOff int
	for y := 0; y < H; y++ {
		row := src.Pix[y*src.Stride:]
		for x := 0; x < W; x++ {
			d := Distance(row[x*4:], key)
			if d > r.FgDist {
				fg[y*W+x] = true
				continue
			}
			bgSum += d
			bgN++
			if d > 30 {
				bgOff++
			}
		}
	}
	if bgN > 0 {
		r.BgNoise = bgSum / float64(bgN)
		r.BgOffKey = float64(bgOff) / float64(bgN)
	}

	lw, lh := l.Space()
	r.SX, r.SY = float64(W)/float64(lw), float64(H)/float64(lh)
	r.Boxes = make([]image.Rectangle, len(l.Pieces))
	for i, p := range l.Pieces {
		r.Boxes[i] = image.Rect(
			int(float64(p.Box.X)*r.SX), int(float64(p.Box.Y)*r.SY),
			int(float64(p.Box.X+p.Box.W)*r.SX), int(float64(p.Box.Y+p.Box.H)*r.SY))
	}

	r.label(fg)
	r.assign()
	return r
}

// MinArea is the smallest region that counts on its own: 0.01% of the sheet.
func (r *Result) MinArea() int { return max(20, r.W*r.H/10000) }

func (r *Result) label(fg []bool) {
	W, H := r.W, r.H
	r.Label = make([]int32, W*H)
	stack := make([]int, 0, 1024)
	for start := range fg {
		if !fg[start] || r.Label[start] != 0 {
			continue
		}
		// Built by hand: image.Rect would swap these into the full image.
		reg := Region{Rect: image.Rectangle{Min: image.Pt(W, H)}}
		id := int32(len(r.Regs) + 1)
		r.Label[start] = id
		stack = append(stack[:0], start)
		for len(stack) > 0 {
			p := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			x, y := p%W, p/W
			reg.Area++
			reg.Rect.Min.X, reg.Rect.Min.Y = min(reg.Rect.Min.X, x), min(reg.Rect.Min.Y, y)
			reg.Rect.Max.X, reg.Rect.Max.Y = max(reg.Rect.Max.X, x+1), max(reg.Rect.Max.Y, y+1)
			for dy := -1; dy <= 1; dy++ {
				for dx := -1; dx <= 1; dx++ {
					nx, ny := x+dx, y+dy
					if nx < 0 || ny < 0 || nx >= W || ny >= H {
						continue
					}
					q := ny*W + nx
					if fg[q] && r.Label[q] == 0 {
						r.Label[q] = id
						stack = append(stack, q)
					}
				}
			}
		}
		r.Regs = append(r.Regs, reg)
	}
}

// assign gives every big region to the box holding most of its pixels (or,
// if none holds any, the box its bounds overlap most), then every small
// region to the piece whose drawn bounds it sits in.
func (r *Result) assign() {
	minArea := r.MinArea()
	for i := range r.Regs {
		reg := &r.Regs[i]
		if reg.Area < minArea {
			reg.Owner = Speck
			continue
		}
		reg.Big = true
		id := int32(i + 1)
		reg.InBox = make([]int, len(r.Boxes))
		best, bestPix, fallback, bestOv := Stray, 0, Stray, 0
		for j, bx := range r.Boxes {
			in := reg.Rect.Intersect(bx)
			if in.Empty() {
				continue
			}
			if ov := area(in); ov > bestOv {
				fallback, bestOv = j, ov
			}
			pix := 0
			for y := in.Min.Y; y < in.Max.Y; y++ {
				row := r.Label[y*r.W:]
				for x := in.Min.X; x < in.Max.X; x++ {
					if row[x] == id {
						pix++
					}
				}
			}
			reg.InBox[j] = pix
			if pix > 0 && pix*20 > area(bx) { // its pixels cover more than 5% of the box
				reg.Touched = append(reg.Touched, j)
			}
			if pix > bestPix {
				best, bestPix = j, pix
			}
		}
		if best == Stray {
			best = fallback
		}
		reg.Owner = best
	}
	padX, padY := int(SpeckPad*r.SX+0.5), int(SpeckPad*r.SY+0.5)
	for i := range r.Regs {
		reg := &r.Regs[i]
		if reg.Big {
			continue
		}
		for j := range r.Boxes {
			pr, ok := r.PieceRect(j, false)
			if !ok {
				continue
			}
			pr = image.Rect(pr.Min.X-padX, pr.Min.Y-padY, pr.Max.X+padX, pr.Max.Y+padY)
			if reg.Rect.In(pr) {
				reg.Owner = j
				break
			}
		}
	}
}

// Contact lists, for piece i, the other pieces its drawn regions reach into.
// merged: one of its regions covers another box by more than 5% and has
// more pixels there than that piece's own regions, so the two were drawn as
// one shape. intrudes: it covers the box by more than 5% but the other piece
// was drawn there too, so they are separate shapes and only placement suffers.
func (r *Result) Contact(i int) (merged, intrudes []int) {
	own := make([]int, len(r.Boxes)) // pixels of each box owned by its piece
	for _, reg := range r.Regs {
		if reg.Big && reg.Owner >= 0 {
			own[reg.Owner] += reg.InBox[reg.Owner]
		}
	}
	seen := map[int]bool{}
	for _, reg := range r.Regs {
		if !reg.Big || reg.Owner != i {
			continue
		}
		for _, j := range reg.Touched {
			if j == i || seen[j] {
				continue
			}
			seen[j] = true
			if reg.InBox[j] > own[j] {
				merged = append(merged, j)
			} else {
				intrudes = append(intrudes, j)
			}
		}
	}
	return merged, intrudes
}

// PieceRect is the union of the regions piece i owns, big ones only unless
// specks is set. ok is false when nothing was drawn for it.
func (r *Result) PieceRect(i int, specks bool) (image.Rectangle, bool) {
	var u image.Rectangle
	found := false
	for _, reg := range r.Regs {
		if reg.Owner != i || (!reg.Big && !specks) {
			continue
		}
		if !found {
			u, found = reg.Rect, true
		} else {
			u = u.Union(reg.Rect)
		}
	}
	return u, found
}

// ToRGBA returns img as *image.RGBA with its origin at 0,0.
func ToRGBA(img image.Image) *image.RGBA {
	b := img.Bounds()
	if rgba, ok := img.(*image.RGBA); ok && b.Min == (image.Point{}) {
		return rgba
	}
	rgba := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(rgba, rgba.Bounds(), img, b.Min, draw.Src)
	return rgba
}

// Distance is the RGB distance between pixel bytes p and c.
func Distance(p []uint8, c color.RGBA) float64 {
	dr := float64(p[0]) - float64(c.R)
	dg := float64(p[1]) - float64(c.G)
	db := float64(p[2]) - float64(c.B)
	return math.Sqrt(dr*dr + dg*dg + db*db)
}

func area(r image.Rectangle) int {
	if r.Empty() {
		return 0
	}
	return r.Dx() * r.Dy()
}
