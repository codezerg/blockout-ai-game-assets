// Package extract cuts each piece out of a matted sheet and writes the
// manifest. A piece is exactly the drawn regions segmentation gave it, plus
// its soft edge and any small key-coloured areas enclosed in it, so a
// neighbour can never leak into its file. Each file is trimmed to its
// visible pixels.
package extract

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"sort"

	"github.com/codezerg/blockout-ai-game-assets/internal/layout"
	"github.com/codezerg/blockout-ai-game-assets/internal/matte"
	"github.com/codezerg/blockout-ai-game-assets/internal/segment"
	"github.com/codezerg/blockout-ai-game-assets/internal/ui"
)

type Rect struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
}

type Piece struct {
	ID    string     `json:"id"`
	Kind  string     `json:"kind"`
	File  string     `json:"file,omitempty"`
	Rect  Rect       `json:"rect"`           // trimmed bounds in sheet pixels
	Box   layout.Box `json:"box"`            // as given in the layout
	Size  *Size      `json:"size,omitempty"` // of the file; larger than rect when padded to match its states
	Holes int        `json:"holes,omitempty"`
	// Slice is the nine-slice insets in file pixels (nine_slice and button
	// pieces): what must not stretch. Content is where the plain centre
	// begins, the rim's thickness: where to put a label or a board. It is at
	// most Slice, and thinner where a one-off mark widened the slice.
	// Stretch is the share of width and height between the slice insets.
	Slice   *ui.Insets `json:"slice,omitempty"`
	Content *ui.Insets `json:"content,omitempty"`
	Stretch *Stretch   `json:"stretch,omitempty"`
	// State is set on a button state: which piece it is a state of and how
	// well it matches it.
	State *ui.Match `json:"state,omitempty"`
	// Sprites: the footprint in tiles, the anchor point, the measured base
	// of an isometric piece, and the correction applied so its tile and
	// angle match the layout's.
	Footprint []int       `json:"footprint,omitempty"`
	Pivot     *Pivot      `json:"pivot,omitempty"`
	Base      *BaseInfo   `json:"base,omitempty"`
	Fix       *Correction `json:"fix,omitempty"`
	EdgeTint  float64     `json:"edge_tint"` // mean key hue left on edge pixels, 0-255
	// KeyCast is the share of the piece's opaque pixels tinted toward a
	// coloured key: a thin or see-through material (glass, smoke, a glow)
	// that the model drew in the background's colour. Edge tint misses this,
	// since it looks at the outline only.
	KeyCast float64 `json:"key_cast"`
	// Flags: missing; touches_edge (may be cut off); merged:<id> (drawn as
	// one shape with another piece); intrudes:<id> (reaches into another
	// piece's box, cut cleanly anyway); key_inside (patches of a coloured
	// key, such as green, enclosed in the art and kept opaque); key_cast
	// (at least KeyCastFlag of the piece is tinted toward the key);
	// not_stretchable:x|y (no plain run through the centre along that axis,
	// so stretching smears something); state_mismatch (a state's shape
	// differs from its base's). Isometric pieces: unmeasured (no base
	// found: scaled by the sheet's median correction, pivot at the bottom
	// centre); scale_estimated (round base: pivot measured, scale from the
	// sheet's median); angle_off (base lopsided or far from 2:1, which code
	// cannot fix); upscaled (enlarged more than 25% to match the tile).
	Flags []string `json:"flags,omitempty"`
}

// Stretch: the share of width and height between the insets, and the
// shrink seam along each axis (see ui.Slice).
type Stretch struct {
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
	SeamX float64 `json:"seam_x"`
	SeamY float64 `json:"seam_y"`
}

type Size struct {
	W int `json:"w"`
	H int `json:"h"`
}

type Manifest struct {
	Source string `json:"source"`
	Matte  string `json:"matte"`
	Run    string `json:"run"`
	Model  string `json:"model,omitempty"`
	Sheet  Size   `json:"sheet"`
	Layout Size   `json:"layout_space"`
	// Tile is set for isometric sheets: the tile every sprite now matches.
	Tile   *TileInfo `json:"tile,omitempty"`
	Pieces []Piece   `json:"pieces"`
}

// minAlpha is the alpha below which a pixel does not count as visible when
// trimming.
const minAlpha = 4

// faintMax: an edge strip that never reaches this alpha and is cut off from
// the rest by an empty row or column is matte noise (JPEG speckle near the
// key), not art. Left in, it padded 6 of 221 stored pieces by 2 px (E10).
const faintMax = 32

// trim returns the bounds of img's visible pixels, without faint strips cut
// off from the art at its edges.
func trim(img *image.NRGBA) image.Rectangle {
	W, H := img.Rect.Dx(), img.Rect.Dy()
	a := func(x, y int) uint8 { return img.Pix[y*img.Stride+x*4+3] }
	t := image.Rectangle{Min: image.Pt(W, H)}
	var peak uint8
	for y := 0; y < H; y++ {
		for x := 0; x < W; x++ {
			if v := a(x, y); v >= minAlpha {
				t.Min.X, t.Min.Y = min(t.Min.X, x), min(t.Min.Y, y)
				t.Max.X, t.Max.Y = max(t.Max.X, x+1), max(t.Max.Y, y+1)
				peak = max(peak, v)
			}
		}
	}
	if t.Empty() || peak < faintMax {
		return t // nothing that is clearly art to keep instead
	}
	// line is the highest alpha on row (or column) i inside t.
	line := func(rows bool, i int) uint8 {
		var m uint8
		if rows {
			for x := t.Min.X; x < t.Max.X; x++ {
				m = max(m, a(x, i))
			}
		} else {
			for y := t.Min.Y; y < t.Max.Y; y++ {
				m = max(m, a(i, y))
			}
		}
		return m
	}
	// cut walks in from one edge (from, step) to the far edge (to); if a gap
	// comes before anything reaches faintMax, it returns the first line
	// after the gap.
	cut := func(rows bool, from, to, step int) (int, bool) {
		for i := from; i != to; i += step {
			switch m := line(rows, i); {
			case m >= faintMax:
				return 0, false
			case m < minAlpha:
				for ; i != to && line(rows, i) < minAlpha; i += step {
				}
				return i, i != to
			}
		}
		return 0, false
	}
	// The brightest pixel is never in a faint strip, so this ends with it
	// still inside.
	for changed := true; changed; {
		changed = false
		if i, ok := cut(true, t.Min.Y, t.Max.Y, 1); ok {
			t.Min.Y, changed = i, true
		}
		if i, ok := cut(true, t.Max.Y-1, t.Min.Y-1, -1); ok {
			t.Max.Y, changed = i+1, true
		}
		if i, ok := cut(false, t.Min.X, t.Max.X, 1); ok {
			t.Min.X, changed = i, true
		}
		if i, ok := cut(false, t.Max.X-1, t.Min.X-1, -1); ok {
			t.Max.X, changed = i+1, true
		}
	}
	return t
}

// Run writes dir/pieces/<id>.png for every piece found and returns the
// pieces, their images (nil when missing, for previews) and warnings.
func Run(l *layout.Layout, seg *segment.Result, m *matte.Result, dir string) ([]Piece, []image.Image, []string, error) {
	if err := os.MkdirAll(filepath.Join(dir, "pieces"), 0o755); err != nil {
		return nil, nil, nil, err
	}
	W, H := seg.W, seg.H
	sheet := m.Img
	band := max(m.Band, 1)

	var pieces []Piece
	var imgs []image.Image
	var warns []string
	for i, lp := range l.Pieces {
		pc := Piece{ID: lp.ID, Kind: lp.Kind, Box: lp.Box}
		core, ok := seg.PieceRect(i, true)
		if !ok {
			pc.Flags = append(pc.Flags, "missing")
			warns = append(warns, fmt.Sprintf("%s: nothing was drawn in or near its box", lp.ID))
			pieces, imgs = append(pieces, pc), append(imgs, nil)
			continue
		}
		r := image.Rect(core.Min.X-band-1, core.Min.Y-band-1, core.Max.X+band+1, core.Max.Y+band+1).Intersect(image.Rect(0, 0, W, H))

		// Copy the pixels that belong to this piece.
		out := image.NewNRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
		for y := r.Min.Y; y < r.Max.Y; y++ {
			for x := r.Min.X; x < r.Max.X; x++ {
				if !belongs(seg, m, i, y*W+x, x, y, band) {
					continue
				}
				s := sheet.Pix[y*sheet.Stride+x*4:]
				copy(out.Pix[(y-r.Min.Y)*out.Stride+(x-r.Min.X)*4:], s[:4])
			}
		}

		t := trim(out)
		if t.Empty() {
			pc.Flags = append(pc.Flags, "missing")
			warns = append(warns, fmt.Sprintf("%s: drawn but fully transparent after keying", lp.ID))
			pieces, imgs = append(pieces, pc), append(imgs, nil)
			continue
		}
		trimmed := out.SubImage(t).(*image.NRGBA)
		abs := t.Add(r.Min)
		pc.Rect = Rect{X: abs.Min.X, Y: abs.Min.Y, W: abs.Dx(), H: abs.Dy()}
		pc.EdgeTint = edgeTint(trimmed, seg)
		pc.KeyCast = keyCast(trimmed, seg.Key)
		if pc.KeyCast >= KeyCastFlag {
			pc.Flags = append(pc.Flags, "key_cast")
			k := seg.Key
			warns = append(warns, fmt.Sprintf("%s: %.0f%% of it is tinted toward the key colour #%02X%02X%02X, so a thin or see-through part took on the background; ask for opaque materials, or use a key colour the art does not contain",
				lp.ID, pc.KeyCast*100, k.R, k.G, k.B))
		}

		for _, h := range m.Holes {
			if h.Owner == i {
				pc.Holes++
			}
		}
		kept := 0
		for _, k := range m.Kept {
			if k.Owner == i {
				kept += k.Area
			}
		}
		// More than about 6x6 layout units of key colour inside the art.
		if matte.Saturated(seg.Key) && float64(kept) > 36*seg.SX*seg.SY {
			pc.Flags = append(pc.Flags, "key_inside")
		}
		if core.Min.X <= 0 || core.Min.Y <= 0 || core.Max.X >= W || core.Max.Y >= H {
			pc.Flags = append(pc.Flags, "touches_edge")
			warns = append(warns, fmt.Sprintf("%s: drawn up to the sheet's edge and may be cut off", lp.ID))
		}
		merged, intrudes := seg.Contact(i)
		for _, j := range merged {
			pc.Flags = append(pc.Flags, "merged:"+l.Pieces[j].ID)
			warns = append(warns, fmt.Sprintf("%s: drawn as one shape with %s; both are in this file", lp.ID, l.Pieces[j].ID))
		}
		for _, j := range intrudes {
			pc.Flags = append(pc.Flags, "intrudes:"+l.Pieces[j].ID)
		}

		pc.File = filepath.ToSlash(filepath.Join("pieces", lp.ID+".png"))
		if err := writePNG(filepath.Join(dir, pc.File), trimmed); err != nil {
			return nil, nil, nil, err
		}
		pc.Size = &Size{W: trimmed.Rect.Dx(), H: trimmed.Rect.Dy()}
		pieces, imgs = append(pieces, pc), append(imgs, trimmed)
	}
	return pieces, imgs, warns, nil
}

// MinStateIoU is the silhouette overlap below which a state is flagged as
// not matching its base.
const MinStateIoU = 0.9

// UI adds what UI kinds need, rewriting piece files where it pads them:
// button states are aligned to their base and every member of the group is
// padded onto one shared canvas; sliced kinds get nine-slice insets, shared
// across a state group so all states stretch alike. imgs is updated in
// place.
func UI(l *layout.Layout, pieces []Piece, imgs []image.Image, dir string) ([]string, error) {
	var warns []string
	groups := map[int][]int{} // base -> states
	for st, base := range ui.States(l) {
		if imgs[st] != nil && imgs[base] != nil {
			groups[base] = append(groups[base], st)
		}
	}
	for base, states := range groups {
		off := map[int]image.Point{base: {}}
		u := imgs[base].Bounds().Sub(imgs[base].Bounds().Min)
		for _, st := range states {
			m := ui.Align(imgs[base], imgs[st]) // as drawn: scores the model
			if fitted, ok := ui.FitState(imgs[base], imgs[st]); ok {
				imgs[st] = fitted
				a := ui.Align(imgs[base], fitted)
				m.DX, m.DY, m.Resized = a.DX, a.DY, true
			}
			m.Of = l.Pieces[base].ID
			m.ShapeIoU = math.Round(m.ShapeIoU*1000) / 1000
			m.ColorDif = math.Round(m.ColorDif*10) / 10
			pieces[st].State = &m
			if m.ShapeIoU < MinStateIoU {
				pieces[st].Flags = append(pieces[st].Flags, "state_mismatch")
				warns = append(warns, fmt.Sprintf("%s: its shape matches %s only %.0f%%", l.Pieces[st].ID, m.Of, m.ShapeIoU*100))
			}
			o := image.Pt(m.DX, m.DY)
			off[st] = o
			u = u.Union(imgs[st].Bounds().Sub(imgs[st].Bounds().Min).Add(o))
		}
		for i, o := range off {
			padded := ui.Pad(imgs[i], o.Sub(u.Min), u.Dx(), u.Dy())
			imgs[i] = padded
			pieces[i].Size = &Size{W: u.Dx(), H: u.Dy()}
			if err := writePNG(filepath.Join(dir, pieces[i].File), padded); err != nil {
				return nil, err
			}
		}
	}

	// Nine-slice insets; a state group shares the widest insets.
	slices := map[int]ui.Slice{}
	for i, p := range l.Pieces {
		if ui.Sliced(p.Kind) && imgs[i] != nil {
			slices[i] = ui.FindSlice(imgs[i])
		}
	}
	for base, states := range groups {
		in := slices[base].Insets
		for _, st := range states {
			s := slices[st].Insets
			in = ui.Insets{Left: max(in.Left, s.Left), Top: max(in.Top, s.Top), Right: max(in.Right, s.Right), Bottom: max(in.Bottom, s.Bottom)}
		}
		for _, i := range append([]int{base}, states...) {
			s := slices[i]
			s.Insets = in
			slices[i] = s
		}
	}
	for i, s := range slices {
		in := s.Insets
		pieces[i].Slice = &in
		content := ui.Content(imgs[i], in)
		pieces[i].Content = &content
		W, H := imgs[i].Bounds().Dx(), imgs[i].Bounds().Dy()
		pieces[i].Stretch = &Stretch{
			X: math.Round(float64(W-in.Left-in.Right)/float64(W)*100) / 100,
			Y: math.Round(float64(H-in.Top-in.Bottom)/float64(H)*100) / 100,
		}
		// The seam is measured at the group's insets.
		sl := ui.FindSliceAt(imgs[i], in)
		pieces[i].Stretch.SeamX = math.Round(sl.SeamX*10) / 10
		pieces[i].Stretch.SeamY = math.Round(sl.SeamY*10) / 10
		for axis, flat := range map[string]bool{"x": s.FlatX, "y": s.FlatY} {
			if !flat {
				pieces[i].Flags = append(pieces[i].Flags, "not_stretchable:"+axis)
			}
		}
		if !s.FlatX || !s.FlatY {
			sort.Strings(pieces[i].Flags)
			if l.Pieces[i].Kind == "nine_slice" {
				warns = append(warns, fmt.Sprintf("%s: no plain middle to stretch through; stretching will smear its centre", l.Pieces[i].ID))
			}
		}
	}
	sort.Strings(warns)
	return warns, nil
}

// belongs decides whether pixel p (at x,y) is part of piece i: drawn pixels
// by ownership; enclosed key-coloured pixels kept as art if the piece
// encloses them (not merely a neighbour inside its bounds); soft-edge
// background pixels if a pixel of the piece is within the edge band.
func belongs(seg *segment.Result, m *matte.Result, i, p, x, y int, band int) bool {
	if seg.Label[p] != 0 {
		return seg.Regs[seg.Label[p]-1].Owner == i
	}
	if !m.Bg[p] {
		return m.KeptAt != nil && m.KeptAt[p] > 0 && m.Kept[m.KeptAt[p]-1].Owner == i
	}
	if m.Img.Pix[p*4+3] == 0 {
		return false
	}
	for yy := max(0, y-band); yy <= min(seg.H-1, y+band); yy++ {
		for xx := max(0, x-band); xx <= min(seg.W-1, x+band); xx++ {
			if id := seg.Label[yy*seg.W+xx]; id != 0 && seg.Regs[id-1].Owner == i {
				return true
			}
		}
	}
	return false
}

// edgeTint is the mean key hue of opaque pixels next to transparent ones.
func edgeTint(img *image.NRGBA, seg *segment.Result) float64 {
	b := img.Rect
	at := func(x, y int) uint8 {
		if !image.Pt(x, y).In(b) {
			return 0
		}
		return img.Pix[img.PixOffset(x, y)+3]
	}
	var sum float64
	n := 0
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			o := img.PixOffset(x, y)
			if img.Pix[o+3] < 128 {
				continue
			}
			if at(x-1, y) >= 128 && at(x+1, y) >= 128 && at(x, y-1) >= 128 && at(x, y+1) >= 128 {
				continue
			}
			sum += matte.Spill(img.Pix[o], img.Pix[o+1], img.Pix[o+2], seg.Key)
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return math.Round(sum/float64(n)*10) / 10
}

// castMin is the key tint (0-255, what despill would remove) above which a
// pixel counts as cast toward the key.
const castMin = 16

// KeyCastFlag is the key cast at which a piece is flagged key_cast. On the
// 221 stored pieces (E1-E9) every piece at or above it showed a visible
// tint: green glass, magenta smoke, a green sketch outline drawn in.
const KeyCastFlag = 0.03

// keyCast is the share of opaque pixels whose colour leans toward a coloured
// key by more than castMin; 0 for grey keys, which add no hue.
func keyCast(img *image.NRGBA, key color.RGBA) float64 {
	if !matte.Saturated(key) {
		return 0
	}
	b := img.Rect
	n, cast := 0, 0
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			o := img.PixOffset(x, y)
			if img.Pix[o+3] < 128 {
				continue
			}
			n++
			if matte.Spill(img.Pix[o], img.Pix[o+1], img.Pix[o+2], key) > castMin {
				cast++
			}
		}
	}
	if n == 0 {
		return 0
	}
	return math.Round(float64(cast)/float64(n)*1000) / 1000
}

func writePNG(path string, img image.Image) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// WriteManifest writes manifest.json into dir.
func WriteManifest(dir string, m Manifest) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "manifest.json"), append(data, '\n'), 0o644)
}
