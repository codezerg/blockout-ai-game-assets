package preview

import (
	"fmt"
	"image"
	"image/color"

	"github.com/codezerg/blockout-ai-game-assets/internal/extract"
	"github.com/codezerg/blockout-ai-game-assets/internal/layout"
	"github.com/codezerg/blockout-ai-game-assets/internal/ui"

	xdraw "golang.org/x/image/draw"
)

// UIStrip shows the UI checks, for a person or an agent to judge by eye:
// every nine-slice panel with its slice insets (cyan lines) and content box
// (yellow) and stretched three ways, and every button state group aligned, with a silhouette overlay (white where
// both are opaque, red only the base, cyan only the state). It returns nil
// when the layout has none of these. width is the strip's width.
func UIStrip(l *layout.Layout, pieces []extract.Piece, imgs []image.Image, width int) *image.RGBA {
	const rowH, pad, label = 200, 12, 18
	type item struct {
		img     image.Image
		name    string
		in      *ui.Insets // draw slice guides
		content *ui.Insets // draw the content box
	}
	var rows [][]item

	for i, p := range pieces {
		if p.Slice == nil || imgs[i] == nil || l.Pieces[i].Kind != "nine_slice" {
			continue
		}
		img, in := imgs[i], *p.Slice
		b := img.Bounds()
		minW, minH := in.Left+in.Right+2, in.Top+in.Bottom+2
		rows = append(rows, []item{
			{img, fmt.Sprintf("%s %dx%d slice %d %d %d %d", p.ID, b.Dx(), b.Dy(), in.Left, in.Top, in.Right, in.Bottom), &in, p.Content},
			{ui.Stretch(img, in, b.Dx()*2, b.Dy()), "x2 wide", nil, nil},
			{ui.Stretch(img, in, b.Dx(), b.Dy()*3/2), "x1.5 tall", nil, nil},
			{ui.Stretch(img, in, max(minW, b.Dx()/2), max(minH, b.Dy()/2)), "half", nil, nil},
		})
	}

	done := map[string]bool{}
	for i, p := range pieces {
		if p.State == nil || imgs[i] == nil || done[p.State.Of] {
			continue
		}
		done[p.State.Of] = true
		base := -1
		var members []int
		for j, q := range pieces {
			if q.ID == p.State.Of {
				base = j
			}
			if q.State != nil && q.State.Of == p.State.Of && imgs[j] != nil {
				members = append(members, j)
			}
		}
		if base < 0 || imgs[base] == nil {
			continue
		}
		row := []item{{imgs[base], pieces[base].ID, pieces[base].Slice, pieces[base].Content}}
		for _, j := range members {
			s := pieces[j].State
			row = append(row,
				item{imgs[j], fmt.Sprintf("%s shape %.1f%% diff %.0f", pieces[j].ID, s.ShapeIoU*100, s.ColorDif), pieces[j].Slice, pieces[j].Content},
				item{overlay(imgs[base], imgs[j]), "overlay", nil, nil})
		}
		if in := pieces[base].Slice; in != nil {
			b := imgs[base].Bounds()
			row = append(row, item{ui.Stretch(imgs[base], *in, b.Dx()*2, b.Dy()), "x2 wide", nil, nil})
		}
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		return nil
	}

	// Scale each row to fit the width at no more than rowH tall.
	hs := make([]int, len(rows))
	H := pad
	for r, row := range rows {
		natural := 0
		for _, it := range row {
			b := it.img.Bounds()
			natural += b.Dx() * rowH / b.Dy()
		}
		avail := width - pad*(len(row)+1)
		hs[r] = rowH
		if natural > avail {
			hs[r] = rowH * avail / natural
		}
		H += label + hs[r] + pad
	}
	out := image.NewRGBA(image.Rect(0, 0, width, H))
	fill(out, out.Bounds(), paper)
	y := pad
	for r, row := range rows {
		h := hs[r]
		x := pad
		for _, it := range row {
			b := it.img.Bounds()
			w := max(1, b.Dx()*h/b.Dy())
			cell := image.Rect(x, y+label, x+w, y+label+h)
			checker(out, cell)
			xdraw.CatmullRom.Scale(out, cell, it.img, b, xdraw.Over, nil)
			if it.in != nil {
				f := float64(h) / float64(b.Dy())
				in := *it.in
				guide := color.RGBA{0x00, 0xE0, 0xFF, 255}
				for _, gx := range []int{in.Left, b.Dx() - in.Right} {
					X := cell.Min.X + int(float64(gx)*f)
					fill(out, image.Rect(X, cell.Min.Y, X+1, cell.Max.Y), guide)
				}
				for _, gy := range []int{in.Top, b.Dy() - in.Bottom} {
					Y := cell.Min.Y + int(float64(gy)*f)
					fill(out, image.Rect(cell.Min.X, Y, cell.Max.X, Y+1), guide)
				}
			}
			if c := it.content; c != nil {
				f := float64(h) / float64(b.Dy())
				r := image.Rect(
					cell.Min.X+int(float64(c.Left)*f), cell.Min.Y+int(float64(c.Top)*f),
					cell.Min.X+int(float64(b.Dx()-c.Right)*f), cell.Min.Y+int(float64(b.Dy()-c.Bottom)*f))
				yellow := color.RGBA{0xFF, 0xD8, 0x30, 255}
				for _, e := range []image.Rectangle{
					{r.Min, image.Pt(r.Max.X, r.Min.Y+2)}, {image.Pt(r.Min.X, r.Max.Y-2), r.Max},
					{r.Min, image.Pt(r.Min.X+2, r.Max.Y)}, {image.Pt(r.Max.X-2, r.Min.Y), r.Max},
				} {
					fill(out, e, yellow)
				}
			}
			name := it.name
			if n := (w + pad - 4) / 7; len(name) > n { // basicfont is 7 px a character
				name = name[:max(0, n)]
			}
			text(out, x, y, name, white, 1)
			x += w + pad
		}
		y += label + h + pad
	}
	return out
}

// overlay compares two same-size silhouettes: white where both are opaque,
// red where only a is, cyan where only b is.
func overlay(a, b image.Image) image.Image {
	ab, bb := a.Bounds(), b.Bounds()
	w, h := max(ab.Dx(), bb.Dx()), max(ab.Dy(), bb.Dy())
	out := image.NewNRGBA(image.Rect(0, 0, w, h))
	op := func(img image.Image, x, y int) bool {
		r := img.Bounds()
		if x >= r.Dx() || y >= r.Dy() {
			return false
		}
		_, _, _, al := img.At(r.Min.X+x, r.Min.Y+y).RGBA()
		return al >= 0x8000
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			pa, pb := op(a, x, y), op(b, x, y)
			switch {
			case pa && pb:
				out.SetNRGBA(x, y, color.NRGBA{0xE8, 0xE8, 0xE8, 255})
			case pa:
				out.SetNRGBA(x, y, color.NRGBA{0xFF, 0x30, 0x30, 255})
			case pb:
				out.SetNRGBA(x, y, color.NRGBA{0x00, 0xD0, 0xFF, 255})
			}
		}
	}
	return out
}

// Stack puts b under a; b may be nil.
func Stack(a, b *image.RGBA) *image.RGBA {
	if b == nil {
		return a
	}
	out := image.NewRGBA(image.Rect(0, 0, max(a.Rect.Dx(), b.Rect.Dx()), a.Rect.Dy()+b.Rect.Dy()))
	fill(out, out.Bounds(), paper)
	xdraw.Draw(out, a.Rect.Sub(a.Rect.Min), a, a.Rect.Min, xdraw.Src)
	xdraw.Draw(out, b.Rect.Sub(b.Rect.Min).Add(image.Pt(0, a.Rect.Dy())), b, b.Rect.Min, xdraw.Src)
	return out
}
