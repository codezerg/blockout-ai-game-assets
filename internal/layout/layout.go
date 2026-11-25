// Package layout reads and validates blockout layout files.
//
// Coordinates in a layout are pixels of a 1K image at the canvas aspect
// (1024x1024 for 1:1, 1376x768 for 16:9, ...). Extraction scales them to the
// size the model actually returned, so one layout serves 1K, 2K and 4K.
package layout

import (
	"errors"
	"fmt"
	"image/color"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"
	"github.com/goccy/go-yaml/token"
)

type Layout struct {
	Canvas    Canvas   `yaml:"canvas" json:"canvas"`
	Model     string   `yaml:"model,omitempty" json:"model,omitempty"`
	Style     string   `yaml:"style,omitempty" json:"style,omitempty"`
	StyleRefs []string `yaml:"style_refs,omitempty" json:"style_refs,omitempty"`
	// Sketch is how the layout is shown to the model: colors (the default),
	// blocks, outlines or none. See the Sketch* constants and EXPERIMENTS.md.
	Sketch string `yaml:"sketch,omitempty" json:"sketch,omitempty"`
	// Prompt describes the whole image. A layout with a prompt and no pieces
	// generates a single reference image (an art-direction target) instead of
	// a sheet of pieces.
	Prompt string `yaml:"prompt,omitempty" json:"prompt,omitempty"`
	// Grid, when set, gives every piece with a cell its box.
	Grid *Grid `yaml:"grid,omitempty" json:"grid,omitempty"`
	// CellDefaults fill in kind, footprint and pivot for pieces that omit them.
	CellDefaults *CellDefaults `yaml:"cell_defaults,omitempty" json:"cell_defaults,omitempty"`
	// Iso sets the isometric ground tile for pieces with a footprint.
	Iso    *Iso    `yaml:"iso,omitempty" json:"iso,omitempty"`
	Pieces []Piece `yaml:"pieces,omitempty" json:"pieces,omitempty"`

	Path   string    `yaml:"-" json:"-"` // file it was read from
	Source []byte    `yaml:"-" json:"-"` // raw file contents
	file   *ast.File // parsed tree, for line numbers
}

type Canvas struct {
	Aspect     string `yaml:"aspect" json:"aspect"`
	Size       string `yaml:"size" json:"size"`
	Background string `yaml:"background,omitempty" json:"background,omitempty"`
}

type Piece struct {
	ID   string `yaml:"id" json:"id"`
	Kind string `yaml:"kind" json:"kind"`
	Box  Box    `yaml:"box,omitempty" json:"box"`
	// Cell is [column, row] in the grid, counted from 0; it sets Box.
	Cell []int `yaml:"cell,omitempty" json:"cell,omitempty"`
	// Footprint is [w, d]: the object's size on the isometric ground in
	// tiles, w along the axis running to the lower right, d to the lower left.
	Footprint []int `yaml:"footprint,omitempty" json:"footprint,omitempty"`
	// Pivot is where the engine anchors the sprite: bottom-center (the
	// footprint's front corner, or the bottom middle of the art) or center.
	Pivot  string `yaml:"pivot,omitempty" json:"pivot,omitempty"`
	Prompt string `yaml:"prompt" json:"prompt"`
}

type Grid struct {
	Cols   int `yaml:"cols" json:"cols"`
	Rows   int `yaml:"rows" json:"rows"`
	Gutter int `yaml:"gutter" json:"gutter"`
	// Margin around the grid; defaults to the gutter.
	Margin *int `yaml:"margin,omitempty" json:"margin,omitempty"`
}

// cell returns the size of one cell and the margin for a W×H layout space.
// The size is 0 or less when the gutters and margins don't leave room.
func (g *Grid) cell(W, H int) (cw, ch, margin int) {
	margin = g.Gutter
	if g.Margin != nil {
		margin = *g.Margin
	}
	cw = (W - 2*margin - (g.Cols-1)*g.Gutter) / g.Cols
	ch = (H - 2*margin - (g.Rows-1)*g.Gutter) / g.Rows
	return cw, ch, margin
}

type CellDefaults struct {
	Kind      string `yaml:"kind,omitempty" json:"kind,omitempty"`
	Footprint []int  `yaml:"footprint,omitempty" json:"footprint,omitempty"`
	Pivot     string `yaml:"pivot,omitempty" json:"pivot,omitempty"`
}

// Iso describes the isometric ground: a 2:1 projection in which one tile is
// a diamond Tile layout units wide and Tile/2 tall.
type Iso struct {
	// Tile defaults to the largest even size at which every footprint fits
	// its box.
	Tile int `yaml:"tile,omitempty" json:"tile,omitempty"`
	// Guides draws each footprint as a white diamond in the sketch; default
	// true. With false the footprint is given in words only.
	Guides *bool `yaml:"guides,omitempty" json:"guides,omitempty"`
}

// Pivots.
const (
	PivotBottomCenter = "bottom-center"
	PivotCenter       = "center"
)

func validPivot(p string) bool { return p == PivotBottomCenter || p == PivotCenter }

// Pt is a point in layout units.
type Pt struct{ X, Y float64 }

// Diamond is a footprint on the isometric ground, in layout units.
type Diamond struct{ Top, Right, Bottom, Left Pt }

// IsIso reports whether any piece has a footprint.
func (l *Layout) IsIso() bool {
	for _, p := range l.Pieces {
		if len(p.Footprint) == 2 {
			return true
		}
	}
	return false
}

// Tile is the isometric tile width in layout units; 0 without footprints.
func (l *Layout) Tile() int {
	if l.Iso == nil {
		return 0
	}
	return l.Iso.Tile
}

// Guides reports whether footprints are drawn in the sketch.
func (l *Layout) Guides() bool {
	return l.IsIso() && (l.Iso == nil || l.Iso.Guides == nil || *l.Iso.Guides)
}

// Footprint returns piece i's footprint diamond: centred across its box,
// with its front corner a little above the box's bottom edge.
func (l *Layout) Footprint(i int) (Diamond, bool) {
	p := l.Pieces[i]
	T := float64(l.Tile())
	if len(p.Footprint) != 2 || T <= 0 {
		return Diamond{}, false
	}
	w, d := float64(p.Footprint[0]), float64(p.Footprint[1])
	b := p.Box
	cx := float64(b.X) + float64(b.W)/2
	bottom := Pt{cx + (w-d)*T/4, float64(b.Y+b.H) - footPad(b)}
	top := Pt{bottom.X - (w-d)*T/2, bottom.Y - (w+d)*T/4}
	return Diamond{
		Top:    top,
		Right:  Pt{top.X + w*T/2, top.Y + w*T/4},
		Bottom: bottom,
		Left:   Pt{top.X - d*T/2, top.Y + d*T/4},
	}, true
}

// footPad is the gap between a footprint's front corner and its box's
// bottom edge.
func footPad(b Box) float64 { return math.Max(6, 0.04*float64(b.H)) }

// resolve applies cell defaults, turns cells into boxes and picks the tile
// size. It runs before validation, which reports what it could not use.
func (l *Layout) resolve() {
	if d := l.CellDefaults; d != nil {
		for i := range l.Pieces {
			p := &l.Pieces[i]
			if p.Kind == "" {
				p.Kind = d.Kind
			}
			if p.Footprint == nil && d.Footprint != nil {
				p.Footprint = append([]int(nil), d.Footprint...)
			}
			if p.Pivot == "" {
				p.Pivot = d.Pivot
			}
		}
	}
	if g := l.Grid; g != nil && g.Cols > 0 && g.Rows > 0 {
		cw, ch, m := g.cell(l.Space())
		for i := range l.Pieces {
			p := &l.Pieces[i]
			if len(p.Cell) == 2 && p.Box == (Box{}) {
				p.Box = Box{X: m + p.Cell[0]*(cw+g.Gutter), Y: m + p.Cell[1]*(ch+g.Gutter), W: cw, H: ch}
			}
		}
	}
	for i := range l.Pieces {
		if len(l.Pieces[i].Footprint) == 2 && l.Pieces[i].Pivot == "" {
			l.Pieces[i].Pivot = PivotBottomCenter
		}
	}
	if !l.IsIso() {
		return
	}
	if l.Iso == nil {
		l.Iso = &Iso{}
	}
	if l.Iso.Tile == 0 {
		// The largest even tile at which every footprint spans at most 85% of
		// its box's width and 40% of its height.
		best := math.Inf(1)
		for _, p := range l.Pieces {
			if len(p.Footprint) != 2 || p.Footprint[0] <= 0 || p.Footprint[1] <= 0 || p.Box.W <= 0 {
				continue
			}
			n := float64(p.Footprint[0] + p.Footprint[1])
			best = math.Min(best, math.Min(0.85*float64(p.Box.W)*2/n, 0.40*float64(p.Box.H)*4/n))
		}
		if !math.IsInf(best, 1) && best >= 2 {
			l.Iso.Tile = int(best) / 2 * 2
		}
	}
}

type Box struct {
	X int `yaml:"x" json:"x"`
	Y int `yaml:"y" json:"y"`
	W int `yaml:"w" json:"w"`
	H int `yaml:"h" json:"h"`
}

// Problem is one thing wrong with a layout, located as precisely as possible.
type Problem struct {
	Path    string `json:"path,omitempty"`
	Line    int    `json:"line,omitempty"`
	Message string `json:"message"`
}

// Error is returned when a layout cannot be used.
type Error struct {
	File     string
	Problems []Problem
}

func (e *Error) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %d problem(s)", e.File, len(e.Problems))
	for _, p := range e.Problems {
		b.WriteString("\n  ")
		if p.Line > 0 {
			fmt.Fprintf(&b, "line %d: ", p.Line)
		}
		if p.Path != "" {
			fmt.Fprintf(&b, "%s: ", p.Path)
		}
		b.WriteString(p.Message)
	}
	return b.String()
}

const (
	DefaultAspect = "1:1"
	DefaultSize   = "1K"
)

// space1K is the layout coordinate space per aspect: the 1K output size
// Google documents for its Gemini 3 image models. It is a reference frame,
// not a promise; extraction always scales by the size actually returned.
var space1K = map[string][2]int{
	"1:1":  {1024, 1024},
	"2:3":  {848, 1264},
	"3:2":  {1264, 848},
	"3:4":  {896, 1200},
	"4:3":  {1200, 896},
	"4:5":  {928, 1152},
	"5:4":  {1152, 928},
	"9:16": {768, 1376},
	"16:9": {1376, 768},
	"21:9": {1584, 672},
}

var sizeScale = map[string]float64{"0.5K": 0.5, "1K": 1, "2K": 2, "4K": 4}

// Dims returns the expected pixel size of an image at an aspect and size.
func Dims(aspect, size string) (int, int, bool) {
	d, ok := space1K[aspect]
	s, ok2 := sizeScale[size]
	if !ok || !ok2 {
		return 0, 0, false
	}
	return int(float64(d[0]) * s), int(float64(d[1]) * s), true
}

func aspects() string {
	keys := make([]string, 0, len(space1K))
	for k := range space1K {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return ratio(keys[i]) < ratio(keys[j]) })
	return strings.Join(keys, ", ")
}

func ratio(a string) float64 {
	w, h, _ := strings.Cut(a, ":")
	x, _ := strconv.ParseFloat(w, 64)
	y, _ := strconv.ParseFloat(h, 64)
	return x / y
}

// Space is the layout coordinate space in which boxes are given.
func (l *Layout) Space() (int, int) {
	d := space1K[l.Canvas.Aspect]
	return d[0], d[1]
}

// Scaled returns a copy whose boxes are scaled by f about their centres. The
// sketch and prompt can aim at it while scoring keeps the real boxes, to
// offset a model that draws pieces larger than asked.
func (l *Layout) Scaled(f float64) *Layout {
	c := *l
	c.Pieces = make([]Piece, len(l.Pieces))
	for i, p := range l.Pieces {
		b := p.Box
		w, h := int(float64(b.W)*f+0.5), int(float64(b.H)*f+0.5)
		p.Box = Box{X: b.X + (b.W-w)/2, Y: b.Y + (b.H-h)/2, W: w, H: h}
		c.Pieces[i] = p
	}
	return &c
}

// Sketch styles.
const (
	SketchColors   = "colors"   // filled boxes named by colour: "in the red box, draw ..."
	SketchBlocks   = "blocks"   // filled boxes with numbers, positions in words
	SketchOutlines = "outlines" // numbered outlines, positions in words
	SketchNone     = "none"     // no image, positions in words only
)

// ValidSketch reports whether s names a sketch style.
func ValidSketch(s string) bool {
	return s == SketchColors || s == SketchBlocks || s == SketchOutlines || s == SketchNone
}

// IsReference reports whether the layout makes a single reference image.
func (l *Layout) IsReference() bool { return len(l.Pieces) == 0 }

// Name derives a run name from the file path: the case directory for
// layout.yaml, otherwise directory and file stem.
func (l *Layout) Name() string {
	path := l.Path
	if abs, err := filepath.Abs(path); err == nil {
		path = abs // so a relative "layout.yaml" still names its directory
	}
	dir := filepath.Base(filepath.Dir(path))
	stem := strings.TrimSuffix(filepath.Base(l.Path), filepath.Ext(l.Path))
	if stem == "layout" {
		return dir
	}
	if dir == "." || dir == string(filepath.Separator) {
		return stem
	}
	return dir + "-" + stem
}

// Dir is the directory the layout's relative paths resolve against.
func (l *Layout) Dir() string { return filepath.Dir(l.Path) }

// RefPath resolves a style_refs entry.
func (l *Layout) RefPath(ref string) string {
	if filepath.IsAbs(ref) {
		return ref
	}
	return filepath.Join(l.Dir(), filepath.FromSlash(ref))
}

// Background parses the canvas background colour.
func (l *Layout) Background() (color.RGBA, bool) {
	return ParseHex(l.Canvas.Background)
}

func ParseHex(s string) (color.RGBA, bool) {
	if len(s) != 7 || s[0] != '#' {
		return color.RGBA{}, false
	}
	v, err := strconv.ParseUint(s[1:], 16, 32)
	if err != nil {
		return color.RGBA{}, false
	}
	return color.RGBA{uint8(v >> 16), uint8(v >> 8), uint8(v), 255}, true
}

// Options control validation.
type Options struct {
	// CheckFiles verifies that style_refs exist. Off when re-reading the copy
	// of a layout stored in a run directory.
	CheckFiles bool
}

// Load reads, parses and validates a layout file. Warnings are returned even
// when the layout is valid.
func Load(path string, opt Options) (*Layout, []Problem, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, &Error{File: path, Problems: []Problem{{Message: err.Error()}}}
	}
	return Parse(src, path, opt)
}

// Parse is Load for bytes already in memory.
func Parse(src []byte, path string, opt Options) (*Layout, []Problem, error) {
	l := &Layout{Path: path, Source: src}
	if err := yaml.UnmarshalWithOptions(src, l, yaml.Strict()); err != nil {
		return nil, nil, &Error{File: path, Problems: []Problem{yamlProblem(err)}}
	}
	if f, err := parser.ParseBytes(src, 0); err == nil {
		l.file = f
	}
	if l.Canvas.Aspect == "" {
		l.Canvas.Aspect = DefaultAspect
	}
	if l.Canvas.Size == "" {
		l.Canvas.Size = DefaultSize
	}
	if l.Sketch == "" {
		l.Sketch = SketchColors
	}
	l.resolve()
	errs, warns := l.validate(opt)
	if len(errs) > 0 {
		return nil, warns, &Error{File: path, Problems: errs}
	}
	return l, warns, nil
}

func yamlProblem(err error) Problem {
	var ye interface {
		GetToken() *token.Token
		GetMessage() string
	}
	if errors.As(err, &ye) {
		p := Problem{Message: ye.GetMessage()}
		if t := ye.GetToken(); t != nil {
			p.Line = t.Position.Line
		}
		return p
	}
	return Problem{Message: err.Error()}
}

var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

// KnownKinds get kind-specific prompt hints. Other kinds are allowed.
var KnownKinds = map[string]bool{"button": true, "frame": true, "nine_slice": true, "icon": true, "sprite": true}

// minGutter is the gap, in layout units, below which crops risk catching a
// neighbour.
const minGutter = 16

func (l *Layout) validate(opt Options) (errs, warns []Problem) {
	add := func(list *[]Problem, path, format string, a ...any) {
		*list = append(*list, Problem{Path: path, Line: l.line(path), Message: fmt.Sprintf(format, a...)})
	}

	if _, ok := space1K[l.Canvas.Aspect]; !ok {
		add(&errs, "$.canvas.aspect", "unknown aspect %q; use one of %s", l.Canvas.Aspect, aspects())
	}
	if _, ok := sizeScale[l.Canvas.Size]; !ok {
		add(&errs, "$.canvas.size", "unknown size %q; use 0.5K, 1K, 2K or 4K", l.Canvas.Size)
	}
	if !ValidSketch(l.Sketch) {
		add(&errs, "$.sketch", "unknown sketch style %q; use colors, blocks, outlines or none", l.Sketch)
	}

	for i, ref := range l.StyleRefs {
		path := fmt.Sprintf("$.style_refs[%d]", i)
		switch strings.ToLower(filepath.Ext(ref)) {
		case ".png", ".jpg", ".jpeg", ".webp":
		default:
			add(&errs, path, "%q is not a .png, .jpg or .webp image", ref)
			continue
		}
		if opt.CheckFiles {
			if _, err := os.Stat(l.RefPath(ref)); err != nil {
				add(&errs, path, "style ref %q not found (looked at %s); paths are relative to the layout file", ref, l.RefPath(ref))
			}
		}
	}
	if len(l.StyleRefs) > 14 {
		add(&errs, "$.style_refs", "%d style refs; models accept at most 14", len(l.StyleRefs))
	}

	if l.IsReference() {
		if strings.TrimSpace(l.Prompt) == "" {
			add(&errs, "$", "nothing to generate: add pieces (a sheet) or a prompt (a single reference image)")
		}
		return errs, warns
	}

	_, ok := l.Background()
	if !ok {
		add(&errs, "$.canvas.background", "a sheet needs a flat key colour as #RRGGBB, e.g. #00FF00; got %q", l.Canvas.Background)
	}

	W, H := l.Space()
	if g := l.Grid; g != nil && (g.Cols < 1 || g.Rows < 1 || g.Gutter < 0 || (g.Margin != nil && *g.Margin < 0)) {
		add(&errs, "$.grid", "grid needs cols and rows of at least 1 and no negative gutter or margin")
	} else if g != nil {
		if cw, ch, _ := g.cell(W, H); cw < 1 || ch < 1 {
			add(&errs, "$.grid", "a %dx%d grid with gutter %d leaves cells of %dx%d in the %dx%d canvas; use fewer cells or a smaller gutter or margin",
				g.Cols, g.Rows, g.Gutter, cw, ch, W, H)
		}
	}
	if d := l.CellDefaults; d != nil && d.Pivot != "" && !validPivot(d.Pivot) {
		add(&errs, "$.cell_defaults.pivot", "unknown pivot %q; use bottom-center or center", d.Pivot)
	}
	if l.Iso != nil && l.Iso.Tile < 0 {
		add(&errs, "$.iso.tile", "tile must be positive")
	}
	seen := map[string]int{}
	for i, p := range l.Pieces {
		base := fmt.Sprintf("$.pieces[%d]", i)
		if p.Cell != nil {
			g := l.Grid
			switch {
			case g == nil:
				add(&errs, base+".cell", "piece %q has a cell but the layout has no grid", p.ID)
			case len(p.Cell) != 2:
				add(&errs, base+".cell", "cell is [column, row]; got %v", p.Cell)
			case p.Cell[0] < 0 || p.Cell[0] >= g.Cols || p.Cell[1] < 0 || p.Cell[1] >= g.Rows:
				add(&errs, base+".cell", "cell %v is outside the %dx%d grid (columns and rows count from 0)", p.Cell, g.Cols, g.Rows)
			}
		}
		if p.Footprint != nil {
			if len(p.Footprint) != 2 || p.Footprint[0] < 1 || p.Footprint[1] < 1 {
				add(&errs, base+".footprint", "footprint is [w, d] in whole tiles of at least 1; got %v", p.Footprint)
			} else if d, ok := l.Footprint(i); ok && (d.Left.X < float64(p.Box.X) || d.Right.X > float64(p.Box.X+p.Box.W) || d.Top.Y < float64(p.Box.Y)) {
				add(&errs, base+".footprint", "a %dx%d footprint at tile %d does not fit %q's box; use a smaller iso.tile or a bigger box", p.Footprint[0], p.Footprint[1], l.Tile(), p.ID)
			}
		}
		if p.Pivot != "" && !validPivot(p.Pivot) {
			add(&errs, base+".pivot", "unknown pivot %q; use bottom-center or center", p.Pivot)
		}
		switch {
		case p.ID == "":
			add(&errs, base, "piece %d has no id", i+1)
		case !idPattern.MatchString(p.ID):
			add(&errs, base+".id", "id %q must be lowercase letters, digits, _ or -, since it becomes a file name", p.ID)
		default:
			if j, dup := seen[p.ID]; dup {
				add(&errs, base+".id", "id %q is already used by piece %d", p.ID, j+1)
			}
			seen[p.ID] = i
		}
		if p.Kind == "" {
			add(&errs, base, "piece %q has no kind (e.g. button, frame, nine_slice, icon, sprite)", p.ID)
		} else if !KnownKinds[p.Kind] {
			add(&warns, base+".kind", "kind %q has no special handling yet; known kinds: button, frame, nine_slice, icon, sprite", p.Kind)
		}
		if strings.TrimSpace(p.Prompt) == "" {
			add(&errs, base, "piece %q has no prompt", p.ID)
		}
		b := p.Box
		if b.W <= 0 || b.H <= 0 {
			if p.Cell == nil {
				add(&errs, base+".box", "piece %q needs a box with positive w and h, or a cell in a grid; got w=%d h=%d", p.ID, b.W, b.H)
			}
			continue
		}
		if ok && W > 0 && (b.X < 0 || b.Y < 0 || b.X+b.W > W || b.Y+b.H > H) {
			add(&errs, base+".box", "box x=%d..%d y=%d..%d leaves the %dx%d canvas (layout units are 1K pixels at aspect %s)",
				b.X, b.X+b.W, b.Y, b.Y+b.H, W, H, l.Canvas.Aspect)
		}
	}

	for i := range l.Pieces {
		for j := i + 1; j < len(l.Pieces); j++ {
			a, b := l.Pieces[i], l.Pieces[j]
			if a.Box.W <= 0 || b.Box.W <= 0 || a.Box.H <= 0 || b.Box.H <= 0 {
				continue
			}
			path := fmt.Sprintf("$.pieces[%d].box", j)
			ox, oy := overlap(a.Box, b.Box)
			switch {
			case ox > 0 && oy > 0:
				add(&errs, path, "box of %q overlaps %q by %dx%d; move or shrink one of them", b.ID, a.ID, ox, oy)
			case gap(a.Box, b.Box) < minGutter:
				add(&warns, path, "%q is only %d units from %q; gutters under %d may let crops catch a neighbour", b.ID, gap(a.Box, b.Box), a.ID, minGutter)
			}
		}
	}
	return errs, warns
}

// overlap returns the overlap of two boxes on each axis; negative is a gap.
func overlap(a, b Box) (int, int) {
	ox := min(a.X+a.W, b.X+b.W) - max(a.X, b.X)
	oy := min(a.Y+a.H, b.Y+b.H) - max(a.Y, b.Y)
	return ox, oy
}

// gap is the distance between two non-overlapping boxes along the axis that
// separates them, or a large number if they do not face each other.
func gap(a, b Box) int {
	ox, oy := overlap(a, b)
	switch {
	case oy > 0:
		return -ox
	case ox > 0:
		return -oy
	default:
		return 1 << 30 // diagonal neighbours do not face each other
	}
}

// line finds the source line for a YAML path, falling back to its parents.
func (l *Layout) line(path string) int {
	if l.file == nil {
		return 0
	}
	for path != "" {
		if p, err := yaml.PathString(path); err == nil {
			if n, err := p.FilterFile(l.file); err == nil && n != nil {
				if t := n.GetToken(); t != nil {
					return t.Position.Line
				}
			}
		}
		i := strings.LastIndexAny(path, ".[")
		if i <= 0 {
			break
		}
		path = path[:i]
	}
	return 0
}
