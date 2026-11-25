package layout

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const good = `canvas: { aspect: "1:1", size: 2K, background: "#00FF00" }
style: test
pieces:
  - id: a
    kind: button
    box: { x: 60, y: 60, w: 380, h: 120 }
    prompt: one
  - id: b
    kind: icon
    box: { x: 500, y: 60, w: 160, h: 160 }
    prompt: two
`

func TestParseGood(t *testing.T) {
	l, warns, err := Parse([]byte(good), "cases/x/layout.yaml", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(warns) != 0 {
		t.Errorf("unexpected warnings: %v", warns)
	}
	if w, h := l.Space(); w != 1024 || h != 1024 {
		t.Errorf("space %dx%d", w, h)
	}
	if l.Name() != "x" {
		t.Errorf("name %q", l.Name())
	}
	if w, h, _ := Dims("16:9", "2K"); w != 2752 || h != 1536 {
		t.Errorf("dims %dx%d", w, h)
	}
}

func problems(t *testing.T, src string) []Problem {
	t.Helper()
	_, _, err := Parse([]byte(src), "l.yaml", Options{})
	le, ok := err.(*Error)
	if !ok {
		t.Fatalf("want *Error, got %v", err)
	}
	return le.Problems
}

func TestOverlapHasLine(t *testing.T) {
	src := strings.Replace(good, "x: 500, y: 60", "x: 400, y: 60", 1)
	ps := problems(t, src)
	if len(ps) != 1 || !strings.Contains(ps[0].Message, "overlaps") || ps[0].Line != 10 {
		t.Errorf("got %+v", ps)
	}
}

func TestUnknownFieldHasLine(t *testing.T) {
	src := strings.Replace(good, "    prompt: two", "    promt: two", 1)
	ps := problems(t, src)
	if len(ps) != 1 || ps[0].Line != 11 {
		t.Errorf("got %+v", ps)
	}
}

func TestOutOfCanvasAndBadID(t *testing.T) {
	src := strings.Replace(good, "id: b", "id: B b", 1)
	src = strings.Replace(src, "x: 500, y: 60, w: 160", "x: 900, y: 60, w: 160", 1)
	var msgs []string
	for _, p := range problems(t, src) {
		msgs = append(msgs, p.Message)
	}
	all := strings.Join(msgs, "|")
	if !strings.Contains(all, "file name") || !strings.Contains(all, "leaves the 1024x1024 canvas") {
		t.Errorf("got %v", msgs)
	}
}

func TestReferenceLayout(t *testing.T) {
	l, _, err := Parse([]byte("canvas: { aspect: \"16:9\" }\nprompt: a town\n"), "r.yaml", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !l.IsReference() || l.Canvas.Size != "1K" {
		t.Errorf("%+v", l)
	}
	ps := problems(t, "canvas: { aspect: \"16:9\" }\n")
	if !strings.Contains(ps[0].Message, "nothing to generate") {
		t.Errorf("got %+v", ps)
	}
}

func TestGridWithoutRoomForCells(t *testing.T) {
	ps := problems(t, `canvas: { background: "#00FF00" }
grid: { cols: 20, rows: 1, gutter: 64 }
pieces:
  - { id: a, kind: icon, cell: [0, 0], prompt: a }
`)
	if len(ps) != 1 || !strings.Contains(ps[0].Message, "leaves cells of -") || ps[0].Line != 2 {
		t.Errorf("got %+v", ps)
	}
}

// A bare "layout.yaml" (gen run inside the case directory) is named after
// that directory, not ".".
func TestNameOfRelativeLayout(t *testing.T) {
	l, _, err := Parse([]byte(good), "layout.yaml", Options{})
	if err != nil {
		t.Fatal(err)
	}
	wd, _ := os.Getwd()
	if got, want := l.Name(), filepath.Base(wd); got != want {
		t.Errorf("name %q, want %q", got, want)
	}
}

func TestNarrowGutterWarns(t *testing.T) {
	src := strings.Replace(good, "x: 500, y: 60", "x: 450, y: 60", 1)
	_, warns, err := Parse([]byte(src), "l.yaml", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(warns) != 1 || !strings.Contains(warns[0].Message, "only 10 units") {
		t.Errorf("got %+v", warns)
	}
}
