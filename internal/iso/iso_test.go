package iso

import (
	"image"
	"image/color"
	"math"
	"testing"

	"github.com/codezerg/blockout-ai-game-assets/internal/layout"
)

// diamondAt is a w×d footprint whose front corner is at (bx, by), tile T.
func diamondAt(bx, by, T, w, d float64) layout.Diamond {
	top := layout.Pt{X: bx - (w-d)*T/2, Y: by - (w+d)*T/4}
	return layout.Diamond{
		Top:    top,
		Right:  layout.Pt{X: top.X + w*T/2, Y: top.Y + w*T/4},
		Bottom: layout.Pt{X: bx, Y: by},
		Left:   layout.Pt{X: top.X - d*T/2, Y: top.Y + d*T/4},
	}
}

func inside(d layout.Diamond, x, y float64) bool {
	pts := []layout.Pt{d.Top, d.Right, d.Bottom, d.Left}
	for i := range pts {
		a, b := pts[i], pts[(i+1)%4]
		if (b.X-a.X)*(y-a.Y)-(b.Y-a.Y)*(x-a.X) < 0 {
			return false
		}
	}
	return true
}

// prism draws a box standing on d, h pixels tall, plus optional eaves: a
// slab wider than the base near the top.
func prism(d layout.Diamond, h float64, eaves bool) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, 400, 400))
	for y := 0; y < 400; y++ {
		for x := 0; x < 400; x++ {
			fx, fy := float64(x)+0.5, float64(y)+0.5
			hit := false
			for t := 0.0; t <= h && !hit; t++ {
				hit = inside(d, fx, fy+t)
			}
			if eaves && fy > d.Top.Y-h-10 && fy < d.Left.Y-h+20 && fx > d.Left.X-15 && fx < d.Right.X+15 {
				hit = true
			}
			if hit {
				img.SetNRGBA(x, y, color.NRGBA{120, 80, 40, 255})
			}
		}
	}
	return img
}

func TestDetectPrism(t *testing.T) {
	for _, eaves := range []bool{false, true} {
		d := diamondAt(200, 360, 80, 2, 2)
		b := Detect(prism(d, 120, eaves), image.Point{}, d)
		if !b.Found {
			t.Fatalf("eaves=%v: not found: %s", eaves, b.Why)
		}
		if math.Abs(b.VX-200) > 2 || math.Abs(b.VY-360) > 2 {
			t.Errorf("eaves=%v: corner %.1f,%.1f, want 200,360", eaves, b.VX, b.VY)
		}
		if math.Abs(b.AngleL-Angle) > 1.5 || math.Abs(b.AngleR-Angle) > 1.5 {
			t.Errorf("eaves=%v: angles %.1f %.1f, want %.1f", eaves, b.AngleL, b.AngleR, Angle)
		}
		if math.Abs(b.Width()-160) > 4 {
			t.Errorf("eaves=%v: base width %.1f, want 160", eaves, b.Width())
		}
	}
}

func TestDetectFlatBottom(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 200, 200))
	for y := 50; y < 150; y++ {
		for x := 40; x < 160; x++ {
			img.SetNRGBA(x, y, color.NRGBA{100, 100, 100, 255})
		}
	}
	if b := Detect(img, image.Point{}, diamondAt(100, 150, 80, 1, 1)); b.Found {
		t.Errorf("a flat-bottomed object should not be read as an isometric base: %+v", b)
	}
}

// cylinder draws a round tower standing on the circle inscribed in d (a
// square footprint), h pixels tall, with a roof cone above.
func cylinder(d layout.Diamond, h float64) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, 400, 400))
	cx, cy := (d.Left.X+d.Right.X)/2, (d.Top.Y+d.Bottom.Y)/2
	a := (d.Right.X - d.Left.X) / 2 / math.Sqrt2
	b := (d.Bottom.Y - d.Top.Y) / 2 / math.Sqrt2
	for y := 0; y < 400; y++ {
		for x := 0; x < 400; x++ {
			fx, fy := float64(x)+0.5, float64(y)+0.5
			u := (fx - cx) / a
			if math.Abs(u) > 1 {
				continue
			}
			bottom := cy + b*math.Sqrt(1-u*u)
			if fy <= bottom && fy >= cy-h-b*math.Sqrt(1-u*u) {
				img.SetNRGBA(x, y, color.NRGBA{150, 140, 120, 255})
			}
		}
	}
	return img
}

func TestDetectRound(t *testing.T) {
	d := diamondAt(200, 360, 80, 2, 2)
	b := Detect(cylinder(d, 150), image.Point{}, d)
	if !b.Found || b.Shape != Round {
		t.Fatalf("want a round base, got %+v", b)
	}
	if math.Abs(b.Width()-160) > 6 {
		t.Errorf("footprint width %.1f, want 160", b.Width())
	}
	if math.Abs(b.VX-200) > 2 || math.Abs(b.VY-360) > 3 {
		t.Errorf("front corner %.1f,%.1f, want 200,360", b.VX, b.VY)
	}
	if math.Abs(b.Angle()-Angle) > 1.5 {
		t.Errorf("angle %.1f, want %.1f", b.Angle(), Angle)
	}
}

// A prism drawn steeper than 2:1 (true isometric, 30°) is still found, and
// its angle measured.
func TestDetectSteep(t *testing.T) {
	d := diamondAt(200, 360, 80, 2, 2)
	k := math.Tan(30*math.Pi/180) / 0.5
	for _, p := range []*layout.Pt{&d.Top, &d.Left, &d.Right} {
		p.Y = 360 - (360-p.Y)*k
	}
	b := Detect(prism(d, 100, true), image.Point{}, d)
	if !b.Found || b.Shape != Corner {
		t.Fatalf("want a corner, got %+v", b)
	}
	if math.Abs(b.Angle()-30) > 1 {
		t.Errorf("angle %.1f, want 30", b.Angle())
	}
}
