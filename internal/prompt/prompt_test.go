package prompt

import (
	"strings"
	"testing"

	"github.com/codezerg/blockout-ai-game-assets/internal/layout"
)

func TestWhere(t *testing.T) {
	got := Where(layout.Box{X: 60, Y: 60, W: 380, H: 120}, 1024, 1024)
	want := "top left (x 6–43%, y 6–18%, wide, 3.2:1)"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
	if got := Where(layout.Box{X: 412, Y: 412, W: 200, H: 200}, 1024, 1024); !strings.HasPrefix(got, "centre (") {
		t.Errorf("got %q", got)
	}
}

func TestCompileSheet(t *testing.T) {
	l, _, err := layout.Parse([]byte(`canvas: { background: "#00FF00" }
style: wood
style_refs: [a.png]
sketch: blocks
pieces:
  - { id: b, kind: button, box: { x: 10, y: 10, w: 300, h: 100 }, prompt: big button }
`), "x/layout.yaml", layout.Options{})
	if err != nil {
		t.Fatal(err)
	}
	parts := Compile(l, "sketch.png")
	if len(parts) != 5 || parts[1].Label != "sketch" || parts[3].Label != "style_ref_1" {
		t.Fatalf("parts: %+v", parts)
	}
	if !strings.HasPrefix(parts[2].Text, "STYLE REFERENCE 1") {
		t.Errorf("ref intro: %q", parts[2].Text)
	}
	last := parts[4].Text
	for _, want := range []string{"1. blue block, top left", "button — big button. One button shape", "#00FF00"} {
		if !strings.Contains(last, want) {
			t.Errorf("missing %q in:\n%s", want, last)
		}
	}
}

// Every sketch style explains footprints on an isometric sheet; only styles
// that send a sketch point at its white diamonds.
func TestCompileIsoEveryStyle(t *testing.T) {
	for _, tc := range []struct {
		style    string
		diamonds bool
	}{{"colors", true}, {"blocks", true}, {"outlines", true}, {"none", false}} {
		l, _, err := layout.Parse([]byte(`canvas: { background: "#FF00FF" }
sketch: `+tc.style+`
grid: { cols: 2, rows: 1, gutter: 64 }
cell_defaults: { kind: sprite, footprint: [2, 2] }
pieces:
  - { id: a, cell: [0, 0], prompt: a house }
  - { id: b, cell: [1, 0], footprint: [1, 1], prompt: a well }
`), "x/layout.yaml", layout.Options{})
		if err != nil {
			t.Fatal(err)
		}
		parts := Compile(l, "sketch.png")
		last := parts[len(parts)-1].Text
		for _, want := range []string{"ISOMETRIC CAMERA", "standing on a 1×1-tile footprint", "keeps its true size"} {
			if !strings.Contains(last, want) {
				t.Errorf("%s: missing %q in:\n%s", tc.style, want, last)
			}
		}
		if got := strings.Contains(last, "white diamond"); got != tc.diamonds {
			t.Errorf("%s: mentions white diamonds = %v, want %v", tc.style, got, tc.diamonds)
		}
	}
}

func TestCompileColors(t *testing.T) {
	l, _, err := layout.Parse([]byte(`canvas: { background: "#00FF00" }
pieces:
  - { id: b, kind: button, box: { x: 10, y: 10, w: 300, h: 100 }, prompt: a big button }
  - { id: g, kind: icon, box: { x: 10, y: 200, w: 100, h: 100 }, prompt: a gear }
`), "x/layout.yaml", layout.Options{})
	if err != nil {
		t.Fatal(err)
	}
	parts := Compile(l, "sketch.png")
	last := parts[len(parts)-1].Text
	for _, want := range []string{"In the red box, draw a big button. One button shape", "In the blue box, draw a gear. One centred icon."} {
		if !strings.Contains(last, want) {
			t.Errorf("missing %q in:\n%s", want, last)
		}
	}
	for _, banned := range []string{"%", "1.", "top left"} {
		if strings.Contains(last, banned) {
			t.Errorf("colors prompt should not contain %q:\n%s", banned, last)
		}
	}
}
