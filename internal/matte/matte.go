// Package matte removes the key colour from a sheet, giving it an alpha
// channel. Background is every near-key region that reaches the sheet's
// border, plus enclosed near-key regions large enough to be holes (a frame's
// empty centre). Small enclosed near-key areas are part of the art and stay.
//
// Two modes: hard makes background fully transparent and everything else
// opaque; soft also works out partial alpha in a thin band along every edge,
// takes the key colour back out of those pixels, and removes key-colour spill.
package matte

import (
	"image"
	"image/color"
	"math"

	"github.com/codezerg/blockout-ai-game-assets/internal/segment"
)

type Mode string

const (
	Hard Mode = "hard"
	Soft Mode = "soft"
)

// Valid reports whether m names a mode.
func Valid(m string) bool { return m == string(Hard) || m == string(Soft) }

// bgFloor is the apparent coverage of near-key pixels that is treated as
// noise rather than a soft edge.
const bgFloor = 0.3

// On a dark or grey key, an enclosed near-key area is a hole, made
// transparent, only if it is both big (HoleSize² layout units) and thick: it
// must hold a square HoleWidth units across. Thin enclosed areas are grooves
// and shadows the model drew in a colour close to the key (on black, any dark
// shadow); they stay opaque. On a saturated key (green, magenta) art is
// rarely that colour, so any enclosed near-key area of KeyHole² units is a
// hole: the gaps between a windmill's slats, under an awning.
const (
	HoleSize  = 24
	HoleWidth = 16
	KeyHole   = 3
)

type Region struct {
	Rect image.Rectangle
	Area int
	// Owner is the piece whose drawn pixels border the region most, or
	// segment.Stray when no piece borders it. A region belongs to that piece
	// only, even where it lies inside another piece's bounds.
	Owner int
}

type Result struct {
	Img  *image.NRGBA
	Band int // edge band width in sheet pixels (soft mode)
	// Holes are enclosed near-key regions made transparent; Kept are enclosed
	// near-key regions kept opaque as part of the art.
	Holes, Kept []Region
	Bg          []bool // background mask, per pixel
	// KeptAt is, per pixel, 1 + the index in Kept of the region holding it,
	// or 0. Nil when nothing was kept.
	KeptAt []int32
}

// Compute builds the matte for a segmented sheet.
func Compute(sheet image.Image, seg *segment.Result, mode Mode) *Result {
	src := segment.ToRGBA(sheet)
	W, H := seg.W, seg.H
	res := &Result{Img: image.NewNRGBA(image.Rect(0, 0, W, H)), Bg: make([]bool, W*H)}

	// Near-key regions, 4-connected (the dual of the 8-connected drawn regions).
	minHole := int(float64(HoleSize*HoleSize) * seg.SX * seg.SY)
	minDepth := int(math.Round(HoleWidth / 2 * seg.SX))
	minKeyHole := max(1, int(float64(KeyHole*KeyHole)*seg.SX*seg.SY))
	saturated := Saturated(seg.Key)
	depth := depthMap(seg.Label, W, H)
	seen := make([]bool, W*H)
	stack := make([]int, 0, 1024)
	var members []int
	touch := map[int]int{} // piece index -> drawn pixels bordering the region
	for start := range seg.Label {
		if seg.Label[start] != 0 || seen[start] {
			continue
		}
		seen[start] = true
		stack = append(stack[:0], start)
		members = members[:0]
		clear(touch)
		r := image.Rectangle{Min: image.Pt(W, H)}
		border := false
		deepest := 0
		for len(stack) > 0 {
			p := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			members = append(members, p)
			deepest = max(deepest, int(depth[p]))
			x, y := p%W, p/W
			r.Min.X, r.Min.Y = min(r.Min.X, x), min(r.Min.Y, y)
			r.Max.X, r.Max.Y = max(r.Max.X, x+1), max(r.Max.Y, y+1)
			if x == 0 || y == 0 || x == W-1 || y == H-1 {
				border = true
			}
			for _, q := range [4]int{p - 1, p + 1, p - W, p + W} {
				if q < 0 || q >= W*H || (q == p-1 && x == 0) || (q == p+1 && x == W-1) {
					continue
				}
				if id := seg.Label[q]; id != 0 {
					if o := seg.Regs[id-1].Owner; o >= 0 {
						touch[o]++
					}
				} else if !seen[q] {
					seen[q] = true
					stack = append(stack, q)
				}
			}
		}
		if border {
			for _, p := range members {
				res.Bg[p] = true
			}
			continue
		}
		reg := Region{Rect: r, Area: len(members), Owner: borderOwner(touch)}
		hole := len(members) >= minHole && deepest >= minDepth || saturated && len(members) >= minKeyHole
		if !hole {
			res.Kept = append(res.Kept, reg)
			if res.KeptAt == nil {
				res.KeptAt = make([]int32, W*H)
			}
			for _, p := range members {
				res.KeptAt[p] = int32(len(res.Kept))
			}
			continue
		}
		res.Holes = append(res.Holes, reg)
		for _, p := range members {
			res.Bg[p] = true
		}
	}

	pix := res.Img.Pix
	for p := range W * H {
		s := src.Pix[(p/W)*src.Stride+(p%W)*4:]
		d := pix[p*4:]
		copy(d[:3], s[:3])
		if res.Bg[p] {
			d[3] = 0
		} else {
			d[3] = 255
		}
	}
	if mode == Soft {
		res.Band = max(1, int(math.Round(2*seg.SX)))
		soften(res, src, seg.Key)
	}
	return res
}

// soften refines the band of pixels within Band of the background boundary.
// For each, the pure foreground colour F is estimated from solid pixels just
// inside the edge; alpha is how far the pixel sits from the key towards F;
// the key's share is then taken back out of its colour.
func soften(res *Result, src *image.RGBA, key color.RGBA) {
	W, H := res.Img.Rect.Dx(), res.Img.Rect.Dy()
	band := res.Band
	// Chessboard distance to the other side of the boundary, up to band+3.
	limit := band + 3
	dist := make([]int8, W*H)
	for i := range dist {
		dist[i] = int8(limit + 1)
	}
	var queue []int
	var buf [8]int
	for p := range W * H {
		x, y := p%W, p/W
		for _, q := range neighbours8(x, y, W, H, &buf) {
			if res.Bg[q] != res.Bg[p] {
				dist[p] = 1
				queue = append(queue, p)
				break
			}
		}
	}
	for i := 0; i < len(queue); i++ {
		p := queue[i]
		if int(dist[p]) >= limit {
			continue
		}
		x, y := p%W, p/W
		for _, q := range neighbours8(x, y, W, H, &buf) {
			if res.Bg[q] == res.Bg[p] && int(dist[q]) > int(dist[p])+1 {
				dist[q] = dist[p] + 1
				queue = append(queue, q)
			}
		}
	}

	K := [3]float64{float64(key.R), float64(key.G), float64(key.B)}
	keyCh := keyChannels(key)
	out := res.Img.Pix
	R := band + 2
	for p := range W * H {
		if int(dist[p]) > band {
			continue
		}
		x, y := p%W, p/W
		// F: mean of the nearest solid foreground pixels, the first layer
		// just inside the band; deeper pixels are often a different colour
		// (a dark outline in front of a bright face).
		var F [3]float64
		n := 0
		for yy := max(0, y-R); yy <= min(H-1, y+R); yy++ {
			for xx := max(0, x-R); xx <= min(W-1, x+R); xx++ {
				q := yy*W + xx
				if res.Bg[q] || int(dist[q]) <= band || int(dist[q]) > band+2 {
					continue
				}
				s := src.Pix[yy*src.Stride+xx*4:]
				F[0] += float64(s[0])
				F[1] += float64(s[1])
				F[2] += float64(s[2])
				n++
			}
		}
		s := src.Pix[y*src.Stride+x*4:]
		P := [3]float64{float64(s[0]), float64(s[1]), float64(s[2])}
		if n == 0 {
			continue // no solid colour nearby: keep the hard decision
		}
		for c := range F {
			F[c] /= float64(n)
		}
		fk := [3]float64{F[0] - K[0], F[1] - K[1], F[2] - K[2]}
		fkn := fk[0]*fk[0] + fk[1]*fk[1] + fk[2]*fk[2]
		if fkn < 1 {
			continue
		}
		a := ((P[0]-K[0])*fk[0] + (P[1]-K[1])*fk[1] + (P[2]-K[2])*fk[2]) / fkn
		a = math.Max(0, math.Min(1, a))
		if res.Bg[p] {
			// Background the model darkened near the art reads as 15-25%
			// coverage; take that floor off so it does not become a halo.
			a = math.Max(0, (a-bgFloor)/(1-bgFloor))
		}
		// Colour: the key taken back out of the pixel where alpha is high
		// enough for that to be stable, blending to F where it is not (dividing
		// by a small alpha amplifies noise into stray hues).
		w := math.Max(0, math.Min(1, (a-0.3)/0.4))
		var C [3]float64
		for c := range C {
			un := F[c]
			if a > 0 {
				un = K[c] + (P[c]-K[c])/a
			}
			C[c] = w*un + (1-w)*F[c]
		}
		despill(&C, keyCh)
		d := out[p*4:]
		for c := range C {
			d[c] = uint8(math.Max(0, math.Min(255, math.Round(C[c]))))
		}
		d[3] = uint8(math.Round(a * 255))
	}
}

// keyChannels marks the channels that make a saturated key its hue: those
// well above the key's grey level (G for green). Grey keys such as black or
// white have none, and are never despilled.
func keyChannels(c color.RGBA) [3]bool {
	k := [3]float64{float64(c.R), float64(c.G), float64(c.B)}
	l := (k[0] + k[1] + k[2]) / 3
	var ch [3]bool
	for i := range k {
		ch[i] = k[i]-l > 50
	}
	return ch
}

// Saturated reports whether key is a coloured key (green, blue, magenta)
// rather than a grey one (black, white). Only a coloured key is a visible
// defect when it shows inside the art; near-black inside art on a black key
// is just shadow.
func Saturated(key color.RGBA) bool {
	ch := keyChannels(key)
	return ch[0] || ch[1] || ch[2]
}

// borderOwner is the piece with the most drawn pixels around a region, or
// segment.Stray when no piece borders it. Ties go to the lower index.
func borderOwner(touch map[int]int) int {
	best, n := segment.Stray, 0
	for o, c := range touch {
		if c > n || c == n && o < best {
			best, n = o, c
		}
	}
	return best
}

// spillOf is the key's share of a colour: how far its weakest key channel
// rises above its strongest other channel. For green that is G - max(R, B);
// for magenta, min(R, B) - G, so a pure red or a pure blue has none.
func spillOf(C [3]float64, keyCh [3]bool) float64 {
	m, k, any := 0.0, math.Inf(1), false
	for i := range C {
		if keyCh[i] {
			k, any = math.Min(k, C[i]), true
		} else {
			m = math.Max(m, C[i])
		}
	}
	if !any {
		return 0
	}
	return math.Max(0, k-m)
}

// despill removes the key's share from every key channel: the classic
// keyer's despill, which for green caps G at max(R, B). It only lowers
// channels, so it never adds a hue, and for a two-channel key such as
// magenta it leaves reds and blues alone.
func despill(C *[3]float64, keyCh [3]bool) {
	s := spillOf(*C, keyCh)
	if s == 0 {
		return
	}
	for i := range C {
		if keyCh[i] {
			C[i] -= s
		}
	}
}

// Spill is how strongly a colour is still tinted by the key, 0-255 (what
// despill would remove); 0 for grey keys.
func Spill(r, g, b uint8, key color.RGBA) float64 {
	return spillOf([3]float64{float64(r), float64(g), float64(b)}, keyChannels(key))
}

// depthMap gives every near-key pixel (label 0) its chessboard distance to
// the nearest drawn pixel, capped at 255; drawn pixels get 0. Two passes.
func depthMap(label []int32, W, H int) []uint8 {
	d := make([]uint8, W*H)
	for p := range d {
		if label[p] == 0 {
			d[p] = 255
		}
	}
	relax := func(p, q int) {
		if v := d[q]; v < 255 && v+1 < d[p] {
			d[p] = v + 1
		}
	}
	for y := 0; y < H; y++ {
		for x := 0; x < W; x++ {
			p := y*W + x
			if d[p] == 0 {
				continue
			}
			if x > 0 {
				relax(p, p-1)
			}
			if y > 0 {
				relax(p, p-W)
				if x > 0 {
					relax(p, p-W-1)
				}
				if x < W-1 {
					relax(p, p-W+1)
				}
			}
		}
	}
	for y := H - 1; y >= 0; y-- {
		for x := W - 1; x >= 0; x-- {
			p := y*W + x
			if d[p] == 0 {
				continue
			}
			if x < W-1 {
				relax(p, p+1)
			}
			if y < H-1 {
				relax(p, p+W)
				if x < W-1 {
					relax(p, p+W+1)
				}
				if x > 0 {
					relax(p, p+W-1)
				}
			}
		}
	}
	return d
}

// neighbours8 returns the in-bounds 8-neighbours of x,y, using buf.
func neighbours8(x, y, W, H int, buf *[8]int) []int {
	n := 0
	for dy := -1; dy <= 1; dy++ {
		for dx := -1; dx <= 1; dx++ {
			if dx == 0 && dy == 0 {
				continue
			}
			nx, ny := x+dx, y+dy
			if nx >= 0 && ny >= 0 && nx < W && ny < H {
				buf[n] = ny*W + nx
				n++
			}
		}
	}
	return buf[:n]
}
