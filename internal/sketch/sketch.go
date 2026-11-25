// Package sketch draws a layout as the annotation image sent to the model:
// the canvas in its key colour with one mark per piece, in colours that
// stand out from the key. Style colors (the default) fills each box with a
// plain-named colour; blocks fills and numbers it; outlines draws a numbered
// border; none sends no image (previews draw it as outlines). Isometric
// layouts add white footprint diamonds unless guides are off. Colours and
// numbers only, never words.
package sketch

import (
	"image"
	"image/color"
	"math"
	"strconv"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"

	"github.com/codezerg/blockout-ai-game-assets/internal/layout"
)

// Swatch is a named block colour, so the prompt can say "the blue block".
type Swatch struct {
	Name  string
	Color color.RGBA
}

var palette = []Swatch{
	{"blue", color.RGBA{0x4E, 0x79, 0xA7, 255}},
	{"orange", color.RGBA{0xF2, 0x8E, 0x2B, 255}},
	{"red", color.RGBA{0xE1, 0x57, 0x59, 255}},
	{"teal", color.RGBA{0x76, 0xB7, 0xB2, 255}},
	{"yellow", color.RGBA{0xED, 0xC9, 0x48, 255}},
	{"purple", color.RGBA{0xB0, 0x7A, 0xA1, 255}},
	{"brown", color.RGBA{0x9C, 0x75, 0x5F, 255}},
	{"grey", color.RGBA{0x8C, 0x8C, 0x8C, 255}},
	{"green", color.RGBA{0x59, 0xA1, 0x4F, 255}},
	{"pink", color.RGBA{0xFF, 0x9D, 0xA7, 255}},
}

// vivid is the colors-style palette: saturated colours with plain names, so
// the prompt can say "in the red box" and mean only one box.
var vivid = []Swatch{
	{"red", color.RGBA{0xE8, 0x25, 0x1F, 255}},
	{"blue", color.RGBA{0x1F, 0x5F, 0xE0, 255}},
	{"yellow", color.RGBA{0xF5, 0xD0, 0x00, 255}},
	{"orange", color.RGBA{0xF5, 0x7C, 0x00, 255}},
	{"purple", color.RGBA{0x7B, 0x2F, 0xBE, 255}},
	{"cyan", color.RGBA{0x00, 0xB8, 0xD4, 255}},
	{"pink", color.RGBA{0xF0, 0x62, 0x92, 255}},
	{"brown", color.RGBA{0x79, 0x55, 0x48, 255}},
	{"magenta", color.RGBA{0xE9, 0x1E, 0xAD, 255}},
	{"white", color.RGBA{0xFF, 0xFF, 0xFF, 255}},
	{"black", color.RGBA{0x11, 0x11, 0x11, 255}},
	{"grey", color.RGBA{0x8A, 0x8A, 0x8A, 255}},
	{"green", color.RGBA{0x2E, 0x9E, 0x3E, 255}},
}

// usable returns the style's palette without colours close to the key
// colour, so no box can be mistaken for background.
func usable(l *layout.Layout, style string) []Swatch {
	bg, _ := l.Background()
	var out []Swatch
	for _, s := range paletteFor(style) {
		// White is kept for footprint guides on isometric sheets, with or
		// without guides, so both variants use the same box colours.
		if dist(s.Color, bg) > 200 && !(l.IsIso() && s.Color == Guide) {
			out = append(out, s)
		}
	}
	return out
}

func paletteFor(style string) []Swatch {
	if style == layout.SketchColors {
		return vivid
	}
	return palette
}

// Guide is the colour of footprint diamonds.
var Guide = color.RGBA{0xFF, 0xFF, 0xFF, 255}

// Distinct is how many pieces a style can tell apart by colour alone.
func Distinct(l *layout.Layout, style string) int { return len(usable(l, style)) }

// Swatches returns one swatch per piece. Colours repeat past the palette
// size, which numbered styles tolerate; the colors style must not exceed
// Distinct. When no colour stands out from the key (Distinct is 0, which gen
// and sketch refuse), it falls back to the whole palette.
func Swatches(l *layout.Layout, style string) []Swatch {
	u := usable(l, style)
	if len(u) == 0 {
		u = paletteFor(style)
	}
	out := make([]Swatch, len(l.Pieces))
	for i := range out {
		out[i] = u[i%len(u)]
	}
	return out
}

func dist(a, b color.RGBA) float64 {
	dr, dg, db := float64(a.R)-float64(b.R), float64(a.G)-float64(b.G), float64(a.B)-float64(b.B)
	return math.Sqrt(dr*dr + dg*dg + db*db)
}

// Render draws the sketch in a style at layout resolution (1K at the canvas
// aspect). colors fills each box with a plain-named colour and nothing else;
// blocks fills each box and numbers it; outlines draws only a numbered
// border. Style none has no sketch, so it draws outlines, which is what
// previews show.
func Render(l *layout.Layout, style string) *image.RGBA {
	w, h := l.Space()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	bg, _ := l.Background()
	xdraw.Draw(img, img.Bounds(), image.NewUniform(bg), image.Point{}, xdraw.Src)

	sw := Swatches(l, style)
	for i, p := range l.Pieces {
		r := image.Rect(p.Box.X, p.Box.Y, p.Box.X+p.Box.W, p.Box.Y+p.Box.H)
		c := sw[i].Color
		switch style {
		case layout.SketchColors:
			xdraw.Draw(img, r, image.NewUniform(c), image.Point{}, xdraw.Src)
			continue
		case layout.SketchBlocks:
			xdraw.Draw(img, r, image.NewUniform(c), image.Point{}, xdraw.Src)
			outline(img, r, shade(c, 0.6), 2)
			label(img, r, strconv.Itoa(i+1), ink(c))
			continue
		}
		outline(img, r, c, 4)
		label(img, r, strconv.Itoa(i+1), c)
	}
	if l.Guides() && style != layout.SketchNone {
		for i := range l.Pieces {
			if d, ok := l.Footprint(i); ok {
				FillDiamond(img, d, Guide)
			}
		}
	}
	return img
}

// FillDiamond fills a footprint diamond given in image pixels.
func FillDiamond(img *image.RGBA, d layout.Diamond, c color.RGBA) {
	b := img.Bounds()
	y0 := max(b.Min.Y, int(math.Floor(d.Top.Y)))
	y1 := min(b.Max.Y, int(math.Ceil(d.Bottom.Y)))
	for y := y0; y < y1; y++ {
		fy := float64(y) + 0.5
		lo, hi := math.Inf(1), math.Inf(-1)
		edges := [4][2]layout.Pt{{d.Top, d.Right}, {d.Right, d.Bottom}, {d.Bottom, d.Left}, {d.Left, d.Top}}
		for _, e := range edges {
			a, z := e[0], e[1]
			if (fy < a.Y) == (fy < z.Y) || a.Y == z.Y {
				continue
			}
			x := a.X + (fy-a.Y)*(z.X-a.X)/(z.Y-a.Y)
			lo, hi = math.Min(lo, x), math.Max(hi, x)
		}
		for x := max(b.Min.X, int(math.Round(lo))); x < min(b.Max.X, int(math.Round(hi))); x++ {
			img.SetRGBA(x, y, c)
		}
	}
}

func outline(img *image.RGBA, r image.Rectangle, c color.RGBA, t int) {
	u := image.NewUniform(c)
	for _, s := range []image.Rectangle{
		{r.Min, image.Pt(r.Max.X, r.Min.Y+t)},
		{image.Pt(r.Min.X, r.Max.Y-t), r.Max},
		{r.Min, image.Pt(r.Min.X+t, r.Max.Y)},
		{image.Pt(r.Max.X-t, r.Min.Y), r.Max},
	} {
		xdraw.Draw(img, s.Intersect(r), u, image.Point{}, xdraw.Src)
	}
}

// label draws a small number in the top-left corner of r.
func label(dst *image.RGBA, r image.Rectangle, s string, c color.Color) {
	face := basicfont.Face7x13
	tw := font.MeasureString(face, s).Ceil() + 2
	small := image.NewRGBA(image.Rect(0, 0, tw, 13))
	d := font.Drawer{Dst: small, Src: image.NewUniform(c), Face: face, Dot: fixed.P(1, 11)}
	d.DrawString(s)

	scale := min(r.Dx(), r.Dy()) / 60
	scale = max(1, min(scale, 2))
	pad := 4 + scale*2
	dr := image.Rect(r.Min.X+pad, r.Min.Y+pad, r.Min.X+pad+tw*scale, r.Min.Y+pad+13*scale)
	xdraw.NearestNeighbor.Scale(dst, dr.Intersect(r), small, small.Bounds(), xdraw.Over, nil)
}

func shade(c color.RGBA, f float64) color.RGBA {
	return color.RGBA{uint8(float64(c.R) * f), uint8(float64(c.G) * f), uint8(float64(c.B) * f), 255}
}

// ink picks black or white text for a fill colour.
func ink(c color.RGBA) color.RGBA {
	lum := 0.299*float64(c.R) + 0.587*float64(c.G) + 0.114*float64(c.B)
	if lum > 150 {
		return color.RGBA{0x20, 0x20, 0x20, 255}
	}
	return color.RGBA{0xFF, 0xFF, 0xFF, 255}
}
