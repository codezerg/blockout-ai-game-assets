// Package measure scores a generated sheet against its layout: where each
// piece was actually drawn, how far it spills past its box, whether it
// reaches the sheet edge, and whether sketch colours leaked into the art.
// All lengths are in layout units (1K pixels) so sheets of any size compare.
package measure

import (
	"image"
	"image/color"
	"math"

	"github.com/codezerg/blockout-ai-game-assets/internal/extract"
	"github.com/codezerg/blockout-ai-game-assets/internal/layout"
	"github.com/codezerg/blockout-ai-game-assets/internal/segment"
	"github.com/codezerg/blockout-ai-game-assets/internal/sketch"
)

// LeakDistance is how close a pixel must be to a sketch colour to count as
// that colour leaking into the art.
const LeakDistance = 24

type Spill struct {
	Left   int `json:"left"`
	Top    int `json:"top"`
	Right  int `json:"right"`
	Bottom int `json:"bottom"`
}

type Piece struct {
	ID          string        `json:"id"`
	Found       bool          `json:"found"`
	Components  int           `json:"components"`
	Drawn       *extract.Rect `json:"drawn,omitempty"` // bounds of what was drawn
	Spill       Spill         `json:"spill"`           // past each box edge; negative is inside
	MaxSpill    int           `json:"max_spill"`
	IoU         float64       `json:"iou"`
	SizeRatio   float64       `json:"size_ratio"`             // sqrt(drawn area / box area)
	TouchesEdge bool          `json:"touches_edge,omitempty"` // drawn up to the sheet's border, so possibly cut off
	MergedWith  []string      `json:"merged_with,omitempty"`  // drawn as one shape with these pieces
	Intrudes    []string      `json:"intrudes,omitempty"`     // reaches into these pieces' boxes
	Leak        float64       `json:"leak"`                   // share of its pixels within LeakDistance of its sketch colour
}

type Summary struct {
	Pieces int `json:"pieces"`
	Found  int `json:"found"`
	// Inside counts pieces drawn within their box plus Margin: the placement
	// target.
	Inside    int     `json:"inside_margin"`
	TouchEdge int     `json:"touches_edge"`
	Merged    int     `json:"merged"`
	Intrudes  int     `json:"intrudes"` // pieces reaching into another piece's box
	MeanIoU   float64 `json:"mean_iou"`
	MinIoU    float64 `json:"min_iou"`
	MaxSpill  int     `json:"max_spill"`
	MeanRatio float64 `json:"mean_size_ratio"`
	Stray     int     `json:"stray"`      // drawn regions outside every box
	MaxLeak   float64 `json:"max_leak"`   // worst piece leak
	BgNoise   float64 `json:"bg_noise"`   // mean distance of background pixels from the key colour
	BgOffKey  float64 `json:"bg_off_key"` // share of background pixels more than 30 from the key colour
}

type Scores struct {
	Summary Summary `json:"summary"`
	// Iso scores isometric sheets: how close the model came by itself,
	// before extraction corrects scale and pivots.
	Iso    *extract.IsoSummary `json:"iso,omitempty"`
	Pieces []Piece             `json:"pieces"`
	Stray  []extract.Rect      `json:"stray,omitempty"`
}

// Score measures a segmented sheet against l. style is the sketch style the
// sheet was generated with; leaks are only counted when a sketch was shown.
func Score(l *layout.Layout, seg *segment.Result, sheet image.Image, style string) *Scores {
	src := segment.ToRGBA(sheet)
	var swatches []color.RGBA
	if style != layout.SketchNone {
		for _, s := range sketch.Swatches(l, style) {
			swatches = append(swatches, s.Color)
		}
	}

	// Leaked pixels per piece: drawn pixels it owns that match its sketch colour.
	leak := make([]int, len(l.Pieces))
	area := make([]int, len(l.Pieces))
	ncomp := make([]int, len(l.Pieces))
	for _, reg := range seg.Regs {
		if reg.Big && reg.Owner >= 0 {
			ncomp[reg.Owner]++
			area[reg.Owner] += reg.Area
		}
	}
	if swatches != nil {
		for p, id := range seg.Label {
			if id == 0 {
				continue
			}
			reg := seg.Regs[id-1]
			if !reg.Big || reg.Owner < 0 {
				continue
			}
			if segment.Distance(src.Pix[(p/seg.W)*src.Stride+(p%seg.W)*4:], swatches[reg.Owner]) <= LeakDistance {
				leak[reg.Owner]++
			}
		}
	}

	sc := &Scores{}
	for _, reg := range seg.Regs {
		if reg.Big && reg.Owner == segment.Stray {
			sc.Stray = append(sc.Stray, toLayout(reg.Rect, seg.SX, seg.SY))
		}
	}
	s := &sc.Summary
	s.Pieces = len(l.Pieces)
	s.MinIoU = 1
	for i, p := range l.Pieces {
		ps := Piece{ID: p.ID, Components: ncomp[i]}
		r, ok := seg.PieceRect(i, false)
		if !ok {
			s.MinIoU = 0
			sc.Pieces = append(sc.Pieces, ps)
			continue
		}
		ps.Found = true
		d := toLayout(r, seg.SX, seg.SY)
		ps.Drawn = &d
		bx := p.Box
		ps.Spill = Spill{
			Left: bx.X - d.X, Top: bx.Y - d.Y,
			Right: d.X + d.W - (bx.X + bx.W), Bottom: d.Y + d.H - (bx.Y + bx.H),
		}
		ps.MaxSpill = max(ps.Spill.Left, ps.Spill.Top, ps.Spill.Right, ps.Spill.Bottom)
		ps.IoU = round(iou(d, bx), 3)
		ps.SizeRatio = round(math.Sqrt(float64(d.W*d.H)/float64(bx.W*bx.H)), 3)
		ps.TouchesEdge = r.Min.X <= 0 || r.Min.Y <= 0 || r.Max.X >= seg.W || r.Max.Y >= seg.H
		if area[i] > 0 {
			ps.Leak = round(float64(leak[i])/float64(area[i]), 4)
		}
		merged, intrudes := seg.Contact(i)
		for _, j := range merged {
			ps.MergedWith = append(ps.MergedWith, l.Pieces[j].ID)
		}
		for _, j := range intrudes {
			ps.Intrudes = append(ps.Intrudes, l.Pieces[j].ID)
		}

		s.Found++
		if ps.MaxSpill <= Margin {
			s.Inside++
		}
		if ps.TouchesEdge {
			s.TouchEdge++
		}
		if len(ps.Intrudes) > 0 {
			s.Intrudes++
		}
		if len(ps.MergedWith) > 0 {
			s.Merged++
		}
		s.MeanIoU += ps.IoU
		s.MinIoU = math.Min(s.MinIoU, ps.IoU)
		s.MaxSpill = max(s.MaxSpill, ps.MaxSpill)
		s.MeanRatio += ps.SizeRatio
		s.MaxLeak = math.Max(s.MaxLeak, ps.Leak)
		sc.Pieces = append(sc.Pieces, ps)
	}
	if s.Pieces > 0 {
		s.MeanIoU = round(s.MeanIoU/float64(s.Pieces), 3) // missing pieces count as 0
	}
	if s.Found > 0 {
		s.MeanRatio = round(s.MeanRatio/float64(s.Found), 3)
	}
	s.MinIoU = round(s.MinIoU, 3)
	s.Stray = len(sc.Stray)
	s.BgNoise = round(seg.BgNoise, 2)
	s.BgOffKey = round(seg.BgOffKey, 4)
	return sc
}

// Margin is the placement tolerance, in layout units, past a box's edge.
const Margin = 16

func toLayout(r image.Rectangle, sx, sy float64) extract.Rect {
	x0, y0 := int(math.Round(float64(r.Min.X)/sx)), int(math.Round(float64(r.Min.Y)/sy))
	x1, y1 := int(math.Round(float64(r.Max.X)/sx)), int(math.Round(float64(r.Max.Y)/sy))
	return extract.Rect{X: x0, Y: y0, W: x1 - x0, H: y1 - y0}
}

func iou(d extract.Rect, b layout.Box) float64 {
	ix := max(0, min(d.X+d.W, b.X+b.W)-max(d.X, b.X))
	iy := max(0, min(d.Y+d.H, b.Y+b.H)-max(d.Y, b.Y))
	inter := ix * iy
	union := d.W*d.H + b.W*b.H - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

func round(v float64, places int) float64 {
	p := math.Pow(10, float64(places))
	return math.Round(v*p) / p
}
