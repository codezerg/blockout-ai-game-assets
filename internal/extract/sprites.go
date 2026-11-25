package extract

import (
	"fmt"
	"image"
	"image/draw"
	"math"
	"path/filepath"
	"sort"

	"github.com/codezerg/blockout-ai-game-assets/internal/iso"
	"github.com/codezerg/blockout-ai-game-assets/internal/layout"
	"github.com/codezerg/blockout-ai-game-assets/internal/segment"

	xdraw "golang.org/x/image/draw"
)

// Pivot is the engine's anchor point as a share of the file's width and
// height, measured from the top-left.
type Pivot struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// BaseInfo is what was measured of an isometric piece's base, in layout
// units, before any correction.
type BaseInfo struct {
	Shape  string  `json:"shape"`       // corner or round
	AngleL float64 `json:"angle_left"`  // degrees; 26.57 is true 2:1
	AngleR float64 `json:"angle_right"` // degrees
	Tile   float64 `json:"tile"`        // tile width it was drawn at
	DX     float64 `json:"dx"`          // front corner minus where the footprint asked it to be
	DY     float64 `json:"dy"`
	// Left and Right: horizontal length of each lower edge, from the side
	// corner to the front corner.
	Left  float64 `json:"left"`
	Right float64 `json:"right"`
}

// Correction is what was done to an isometric piece: resized by Scale, then
// squashed vertically by Squash to bring its angle to 2:1.
type Correction struct {
	Scale  float64 `json:"scale"`
	Squash float64 `json:"squash"`
}

// TileInfo is the sheet's isometric tile: every corrected sprite is resized
// so its tile matches the layout's.
type TileInfo struct {
	Layout int     `json:"layout"` // layout units
	PX     float64 `json:"px"`     // in the piece files
}

// IsoSummary scores an isometric sheet: how close the model came by itself,
// and what is left after correction.
type IsoSummary struct {
	Pieces   int `json:"pieces"`
	Measured int `json:"measured"` // bases found and trusted, so corrected
	// Before correction, over corner bases (round ones are measured too
	// loosely). AngleMean and AngleSD over each base's mean edge slope,
	// degrees; AngleMaxDev is the worst edge's distance from 26.57.
	AngleMean   float64 `json:"angle_mean"`
	AngleSD     float64 `json:"angle_sd"`
	AngleMaxDev float64 `json:"angle_max_dev"`
	// ScaleMean is drawn tile / asked tile on average; ScaleCV its spread
	// across pieces (sd / mean).
	ScaleMean float64 `json:"scale_mean"`
	ScaleCV   float64 `json:"scale_cv"`
	// OffsetMean and OffsetMax: distance of each front corner from its
	// footprint's, layout units, over every measured base, round ones too.
	OffsetMean float64 `json:"offset_mean"`
	OffsetMax  float64 `json:"offset_max"`
	AngleOff   int     `json:"angle_off"` // bases too lopsided or too far from 2:1 to correct
	// After: the corrected sprites measured again.
	After *IsoAfter `json:"after,omitempty"`
}

// IsoAfter is the error left after correction, measured on the corrected
// sprites: tile against the layout's, and angle against 26.57.
type IsoAfter struct {
	ScaleMaxDev float64 `json:"scale_max_dev"` // corner bases: worst |tile / asked − 1|
	AngleMaxDev float64 `json:"angle_max_dev"` // corner bases: worst squashed slope's distance from 26.57
}

const (
	// maxAsym: when a base's two edges differ by more than this many degrees
	// the object is turned or warped, and resizing cannot fix it.
	maxAsym = 5.0
	// maxSquashDev: a base further than this from 26.57 degrees is not
	// squashed (it would distort heights too much); minSquashDev: closer
	// than this is left as it is.
	maxSquashDev = 8.0
	minSquashDev = 0.5
	// maxUpscale: resizing up by more than this softens the art.
	maxUpscale = 1.25
	// shortEdge: a lower edge shorter than this share of what the footprint
	// allows next to the other edge was cut short.
	shortEdge = 0.6
)

// Sprites measures isometric pieces and corrects them. Each piece whose base
// is found is resized so its tile matches the layout's, squashed vertically
// when its projection is steeper or flatter than 2:1, and gets a pivot on its
// footprint's front corner. Pieces whose base is not found are scaled by the
// sheet's median correction and flagged unmeasured. Pieces with a pivot but no footprint get one from their
// bounds. imgs is updated in place and changed files are rewritten.
func Sprites(l *layout.Layout, seg *segment.Result, pieces []Piece, imgs []image.Image, dir string) (*TileInfo, *IsoSummary, []string, error) {
	var warns []string
	target := float64(l.Tile()) * seg.SX
	sum := &IsoSummary{}
	var angles, scales, offsets []float64
	after := &IsoAfter{}
	// Detect every base first. Round bases take the sheet's camera angle
	// from its corner bases, which measure it more reliably; pieces with no
	// base take the sheet's angle and its median correction, since the model
	// tends to draw a whole sheet too large by a similar amount.
	bases := make([]iso.Base, len(l.Pieces))
	var corners, ks []float64
	for i, p := range l.Pieces {
		if imgs[i] == nil || len(p.Footprint) != 2 {
			continue
		}
		d, _ := l.Footprint(i)
		bases[i] = iso.Detect(imgs[i], image.Pt(pieces[i].Rect.X, pieces[i].Rect.Y), scaleDiamond(d, seg.SX, seg.SY))
		b := bases[i]
		if !b.Found {
			continue
		}
		if b.Shape == iso.Corner {
			ks = append(ks, target/drawnTile(b, p.Footprint))
			if math.Abs(b.AngleL-b.AngleR) <= maxAsym {
				corners = append(corners, b.Angle())
			}
		}
	}
	sheetAngle, sheetK := iso.Angle, 1.0
	if len(corners) > 0 {
		sort.Float64s(corners)
		sheetAngle = corners[len(corners)/2]
	}
	if len(ks) > 0 {
		sort.Float64s(ks)
		sheetK = ks[len(ks)/2]
	}
	sheetSquash := 1.0
	if dev := sheetAngle - iso.Angle; math.Abs(dev) > minSquashDev && math.Abs(dev) <= maxSquashDev {
		sheetSquash = math.Tan(iso.Angle*math.Pi/180) / math.Tan(sheetAngle*math.Pi/180)
	}
	for i, p := range l.Pieces {
		if imgs[i] == nil {
			continue
		}
		pc := &pieces[i]
		if len(p.Footprint) != 2 {
			if p.Pivot != "" {
				pv := Pivot{X: 0.5, Y: 1}
				if p.Pivot == layout.PivotCenter {
					pv.Y = 0.5
				}
				pc.Pivot = &pv
			}
			continue
		}
		sum.Pieces++
		pc.Footprint = p.Footprint
		d, _ := l.Footprint(i)
		want := scaleDiamond(d, seg.SX, seg.SY)
		img := imgs[i]
		sz := img.Bounds().Size()
		ox, oy := float64(pc.Rect.X), float64(pc.Rect.Y)
		b := bases[i]
		if !b.Found {
			pc.Flags = append(pc.Flags, "unmeasured")
			pc.Pivot = &Pivot{X: 0.5, Y: 1}
			pc.Fix = &Correction{Scale: round3(sheetK), Squash: round3(sheetSquash)}
			warns = append(warns, fmt.Sprintf("%s: no isometric base found (%s); scaled by the sheet's median correction (x%.2f), pivot at its bottom centre; check both", pc.ID, b.Why, sheetK))
			if math.Abs(sheetK-1) > 0.01 || math.Abs(sheetSquash-1) > 0.005 {
				img = resize(img, sheetK, sheetK*sheetSquash)
				imgs[i] = img
				s := img.Bounds().Size()
				pc.Size = &Size{W: s.X, H: s.Y}
				if err := writePNG(filepath.Join(dir, pc.File), img); err != nil {
					return nil, nil, nil, err
				}
			}
			continue
		}
		sum.Measured++
		tile := drawnTile(b, p.Footprint)
		pc.Base = &BaseInfo{
			Shape:  b.Shape,
			AngleL: round1(b.AngleL), AngleR: round1(b.AngleR),
			Tile: round1(tile / seg.SX),
			DX:   round1((b.VX - want.Bottom.X) / seg.SX), DY: round1((b.VY - want.Bottom.Y) / seg.SY),
			Left: round1((b.VX - b.XL) / seg.SX), Right: round1((b.XR - b.VX) / seg.SX),
		}
		if b.Shape == iso.Corner {
			angles = append(angles, b.Angle())
			scales = append(scales, tile/target)
		}
		offsets = append(offsets, math.Hypot(pc.Base.DX, pc.Base.DY))
		if b.Shape == iso.Corner {
			sum.AngleMaxDev = math.Max(sum.AngleMaxDev, math.Max(math.Abs(b.AngleL-iso.Angle), math.Abs(b.AngleR-iso.Angle)))
		}

		// Angle: squash vertically when both edges agree; otherwise report.
		squash, lopsided := 1.0, false
		dev := b.Angle() - iso.Angle
		switch asym := math.Abs(b.AngleL - b.AngleR); {
		case b.Shape == iso.Round:
			squash = sheetSquash
		case asym > maxAsym || math.Abs(dev) > maxSquashDev:
			sum.AngleOff++
			lopsided = true
			pc.Flags = append(pc.Flags, "angle_off")
			warns = append(warns, fmt.Sprintf("%s: base edges at %.0f° and %.0f°, not %.1f°: a turned or warped object, or a misread base. Scaled by the sheet's median correction; check it, or regenerate it", pc.ID, b.AngleL, b.AngleR, iso.Angle))
		case math.Abs(dev) > minSquashDev:
			squash = math.Tan(iso.Angle*math.Pi/180) / math.Tan(b.Angle()*math.Pi/180)
		}
		// Scale: the layout's tile. A round base's width is measured too
		// loosely to scale by (±40% in E7), and a lopsided one's cannot be
		// trusted; they take the sheet's median.
		k := target / tile
		if lopsided {
			k = sheetK
		} else if b.Shape == iso.Round {
			k = sheetK
			pc.Flags = append(pc.Flags, "scale_estimated")
			warns = append(warns, fmt.Sprintf("%s: round base: pivot measured, size scaled by the sheet's median correction (x%.2f); check its size", pc.ID, sheetK))
		} else if k > maxUpscale {
			pc.Flags = append(pc.Flags, "upscaled")
			warns = append(warns, fmt.Sprintf("%s: drawn at %.0f%% of the layout's tile size and enlarged to fit; check its sharpness", pc.ID, 100/k))
		}
		pc.Fix = &Correction{Scale: round3(k), Squash: round3(squash)}
		kx, ky := k, k*squash
		if math.Abs(k-1) > 0.01 || math.Abs(squash-1) > 0.005 {
			img = resize(img, kx, ky)
		}
		// The pivot is the footprint's front corner; a round base's lies below
		// the drawing, so the file is padded down to hold it.
		pv := Pivot{X: (b.VX - ox) / float64(sz.X), Y: (b.VY - oy) / float64(sz.Y)}
		if pv.Y > 1 {
			h := img.Bounds().Dy()
			img = padBottom(img, int(math.Ceil(pv.Y*float64(h)))+1-h)
			pv.Y *= float64(h) / float64(img.Bounds().Dy())
		}
		pc.Pivot = &Pivot{X: round3(pv.X), Y: round3(pv.Y)}
		if img != imgs[i] {
			imgs[i] = img
			s := img.Bounds().Size()
			pc.Size = &Size{W: s.X, H: s.Y}
			if err := writePNG(filepath.Join(dir, pc.File), img); err != nil {
				return nil, nil, nil, err
			}
		}

		// Measure the corrected sprite again: what is left.
		var w2 layout.Diamond
		for _, pt := range []struct{ dst, src *layout.Pt }{{&w2.Top, &want.Top}, {&w2.Right, &want.Right}, {&w2.Bottom, &want.Bottom}, {&w2.Left, &want.Left}} {
			*pt.dst = layout.Pt{X: (pt.src.X - ox) * kx, Y: (pt.src.Y - oy) * ky}
		}
		if b.Shape != iso.Corner || lopsided {
			continue
		}
		if b2 := iso.Detect(img, image.Point{}, w2); b2.Found && b2.Shape == iso.Corner {
			after.ScaleMaxDev = math.Max(after.ScaleMaxDev, math.Abs(drawnTile(b2, p.Footprint)/target-1))
			if squash != 1 {
				after.AngleMaxDev = math.Max(after.AngleMaxDev, math.Abs(b2.Angle()-iso.Angle))
			}
		}
	}
	if sum.Pieces == 0 {
		return nil, nil, warns, nil
	}
	if len(offsets) > 0 {
		m, _ := meanSD(offsets)
		sum.OffsetMean = round1(m)
		for _, o := range offsets {
			sum.OffsetMax = math.Max(sum.OffsetMax, round1(o))
		}
	}
	if len(angles) > 0 {
		m, sd := meanSD(angles)
		sum.AngleMean, sum.AngleSD = round1(m), round1(sd)
		m, sd = meanSD(scales)
		sum.ScaleMean, sum.ScaleCV = round3(m), round3(sd/m)
		after.ScaleMaxDev, after.AngleMaxDev = round3(after.ScaleMaxDev), round1(after.AngleMaxDev)
		sum.After = after
	}
	sum.AngleMaxDev = round1(sum.AngleMaxDev)
	return &TileInfo{Layout: l.Tile(), PX: round1(target)}, sum, warns, nil
}

// drawnTile is the tile width a base was drawn at, in sheet pixels: its
// width over the footprint's (w+d)/2. When one lower edge is far shorter
// than the footprint allows, something in front of it (a door, a step, a
// ruin) cut it short, and the longer edge alone gives the tile.
func drawnTile(b iso.Base, foot []int) float64 {
	w, d := float64(foot[0]), float64(foot[1])
	tile := b.Width() / ((w + d) / 2)
	if b.Shape != iso.Corner {
		return tile
	}
	L, R := b.VX-b.XL, b.XR-b.VX
	short, long := math.Min(L, R), math.Max(L, R)
	if short < shortEdge*math.Min(w, d)/math.Max(w, d)*long {
		return 2 * long / math.Max(w, d)
	}
	return tile
}

func scaleDiamond(d layout.Diamond, sx, sy float64) layout.Diamond {
	s := func(p layout.Pt) layout.Pt { return layout.Pt{X: p.X * sx, Y: p.Y * sy} }
	return layout.Diamond{Top: s(d.Top), Right: s(d.Right), Bottom: s(d.Bottom), Left: s(d.Left)}
}

func resize(img image.Image, kx, ky float64) *image.NRGBA {
	b := img.Bounds()
	w, h := max(1, int(math.Round(float64(b.Dx())*kx))), max(1, int(math.Round(float64(b.Dy())*ky)))
	out := image.NewNRGBA(image.Rect(0, 0, w, h))
	xdraw.CatmullRom.Scale(out, out.Bounds(), img, b, xdraw.Src, nil)
	return out
}

// padBottom adds n transparent rows under img.
func padBottom(img image.Image, n int) *image.NRGBA {
	b := img.Bounds()
	out := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()+max(0, n)))
	draw.Draw(out, image.Rect(0, 0, b.Dx(), b.Dy()), img, b.Min, draw.Src)
	return out
}

func meanSD(v []float64) (float64, float64) {
	var s, ss float64
	for _, x := range v {
		s += x
	}
	m := s / float64(len(v))
	for _, x := range v {
		ss += (x - m) * (x - m)
	}
	return m, math.Sqrt(ss / float64(len(v)))
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }
func round3(v float64) float64 { return math.Round(v*1000) / 1000 }
