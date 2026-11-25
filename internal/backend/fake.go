package backend

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"

	_ "image/jpeg"

	"github.com/codezerg/blockout-ai-game-assets/internal/layout"
)

// Fake stands in for the model at no cost. Given a sketch, it returns a sheet
// at the requested size in which every block has become a shaded shape on the
// key colour, nudged right and down (1% and 0.6% of the canvas) to imitate
// placement drift. Without a sketch it returns a plain gradient.
type Fake struct {
	// Guides says the sketch carries white footprint diamonds, so only the
	// boxes standing on them are drawn, as isometric objects.
	Guides bool
}

func (Fake) Name() string { return "fake" }

func (f Fake) Generate(ctx context.Context, req Request) (*Result, error) {
	_, elided, err := buildBody(req)
	if err != nil {
		return nil, err
	}
	W, H, ok := layout.Dims(req.Aspect, req.Size)
	if !ok {
		return nil, &Error{Code: "bad_request", Message: fmt.Sprintf("unknown aspect %q or size %q", req.Aspect, req.Size)}
	}
	var sk image.Image
	for _, p := range req.Parts {
		if p.Label == "sketch" && p.Image != nil {
			if sk, _, err = image.Decode(bytes.NewReader(p.Image)); err != nil {
				return nil, &Error{Code: "bad_request", Message: "fake: sketch: " + err.Error()}
			}
		}
	}

	out := image.NewRGBA(image.Rect(0, 0, W, H))
	if sk == nil {
		for y := range H {
			for x := range W {
				out.SetRGBA(x, y, color.RGBA{uint8(80 + 100*x/W), uint8(60 + 80*y/H), 90, 255})
			}
		}
	} else {
		const dx, dy = 0.01, 0.006
		sb := sk.Bounds()
		bg := rgba(sk.At(sb.Min.X, sb.Min.Y))
		var prism func(x, y int) bool
		if f.Guides {
			prism = prisms(sk)
		}
		for y := range H {
			sy := sb.Min.Y + int((float64(y)/float64(H)-dy)*float64(sb.Dy()))
			for x := range W {
				sx := sb.Min.X + int((float64(x)/float64(W)-dx)*float64(sb.Dx()))
				c := bg
				if image.Pt(sx, sy).In(sb) {
					if prism != nil && !prism(sx, sy) {
						// With footprint guides, only the boxes standing on them are drawn.
					} else if s := rgba(sk.At(sx, sy)); s != bg {
						if s == white {
							s = color.RGBA{0x9A, 0x8A, 0x70, 255}
						}
						// Shade by height in the image to look less like a flat block.
						k := 0.65 + 0.35*float64(y%97)/97
						c = color.RGBA{uint8(float64(s.R) * k), uint8(float64(s.G) * k), uint8(float64(s.B) * k), 255}
					}
				}
				out.SetRGBA(x, y, c)
			}
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, out); err != nil {
		return nil, err
	}
	zero := 0.0
	return &Result{
		Image:        buf.Bytes(),
		MIME:         "image/png",
		Model:        "fake/" + req.Model,
		ID:           "fake",
		Cost:         &zero,
		Attempts:     1,
		RequestJSON:  elided,
		ResponseJSON: []byte(`{"fake": true}`),
	}, nil
}

var white = color.RGBA{0xFF, 0xFF, 0xFF, 0xFF}

// prismHeight is how tall, in sketch pixels, the fake's isometric boxes are.
const prismHeight = 70

// prisms returns, for a sketch with white footprint guides, a test of
// whether a sketch pixel lies in a box standing on a guide: some guide pixel
// is at most prismHeight below it in its column. Nil if nothing is white.
func prisms(sk image.Image) func(x, y int) bool {
	b := sk.Bounds()
	W, H := b.Dx(), b.Dy()
	// cum[x*(H+1)+y]: guide pixels in column x above row y.
	cum := make([]int32, W*(H+1))
	any := false
	for x := 0; x < W; x++ {
		for y := 0; y < H; y++ {
			v := int32(0)
			if rgba(sk.At(b.Min.X+x, b.Min.Y+y)) == white {
				v, any = 1, true
			}
			cum[x*(H+1)+y+1] = cum[x*(H+1)+y] + v
		}
	}
	if !any {
		return nil
	}
	return func(x, y int) bool {
		x, y = x-b.Min.X, y-b.Min.Y
		if x < 0 || y < 0 || x >= W || y >= H {
			return false
		}
		return cum[x*(H+1)+min(H, y+prismHeight+1)]-cum[x*(H+1)+y] > 0
	}
}

func rgba(c color.Color) color.RGBA {
	r, g, b, a := c.RGBA()
	return color.RGBA{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), uint8(a >> 8)}
}
