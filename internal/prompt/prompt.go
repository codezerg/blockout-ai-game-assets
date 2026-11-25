// Package prompt compiles a layout into the interleaved text and image parts
// of one generation request. Everything here derives from the layout file.
package prompt

import (
	"fmt"
	"math"
	"path/filepath"
	"strings"

	"github.com/codezerg/blockout-ai-game-assets/internal/layout"
	"github.com/codezerg/blockout-ai-game-assets/internal/sketch"
)

// Part is one piece of message content: text, or an image file.
type Part struct {
	Text  string `json:"text,omitempty"`
	Image string `json:"image,omitempty"` // path to an image file
	Label string `json:"label,omitempty"` // "sketch", "style_ref_1", ...
}

// Compile builds the parts for a layout. sketchPath is where the sketch has
// been written; it is ignored for reference layouts.
func Compile(l *layout.Layout, sketchPath string) []Part {
	if l.IsReference() {
		return compileReference(l)
	}
	return compileSheet(l, sketchPath)
}

// words holds what a sketch style calls its marks, so the prompt describes
// exactly what the model is shown.
type words struct {
	intro  string // the sketch paragraph, or the positions paragraph for none
	unit   string // "block", "outline", "area"
	colour bool   // piece lines name the mark's colour
}

var styleWords = map[string]words{
	layout.SketchBlocks: {
		intro:  "LAYOUT SKETCH. This is an annotation, not artwork. Each coloured block marks where one asset goes and its number identifies the asset. Never draw the blocks, their outlines or the numbers.",
		unit:   "block",
		colour: true,
	},
	layout.SketchOutlines: {
		intro:  "LAYOUT SKETCH. This is an annotation, not artwork. Each coloured rectangle outline marks the area where one asset goes and its number identifies the asset. Never draw the outlines or the numbers.",
		unit:   "outline",
		colour: true,
	},
	layout.SketchNone: {
		intro: "Positions are given as percentages of the canvas width (x) and height (y), measured from its top-left corner.",
		unit:  "area",
	},
}

// compileColors addresses each box only by its colour: "In the red box,
// draw ...". No numbers or coordinates, which Nano Banana does not follow.
func compileColors(l *layout.Layout, sketchPath string) []Part {
	bg := strings.ToUpper(l.Canvas.Background)
	parts := []Part{
		{Text: "Create one game asset sheet: separate, game-ready assets on a flat " + bg + " background, laid out exactly like the colour-box sketch below.\n\nLAYOUT SKETCH: each solid colour box shows where one asset goes and how big it is. The sketch is only a guide; no box colour may appear in the result."},
		{Image: sketchPath, Label: "sketch"},
	}
	parts = append(parts, refParts(l, "copy its look only (materials, palette, lighting, shading, line quality, level of detail); ignore its layout, composition, subjects and any text")...)

	var b strings.Builder
	if l.Style != "" {
		fmt.Fprintf(&b, "Art style: %s\n", l.Style)
	}
	if l.Prompt != "" {
		fmt.Fprintf(&b, "Sheet: %s\n", l.Prompt)
	}
	if l.IsIso() {
		b.WriteString("\n" + isoParagraph(l, l.Guides()) + "\n")
	}
	b.WriteString("\n")
	sw := sketch.Swatches(l, layout.SketchColors)
	for i, p := range l.Pieces {
		fmt.Fprintf(&b, "In the %s box, draw %s", sw[i].Name, strings.TrimRight(p.Prompt, ". "))
		if len(p.Footprint) == 2 {
			fmt.Fprintf(&b, ", standing on a %d×%d-tile footprint", p.Footprint[0], p.Footprint[1])
		}
		if h := kindHint(p.Kind); h != "" {
			fmt.Fprintf(&b, ". %s", h)
		}
		b.WriteString(".\n")
	}
	fill := "- Each asset fills its own box and stays inside it; no part crosses the box's edge."
	if l.IsIso() {
		fill = "- Each object keeps its true size on the tile grid and stays inside its own box; no part crosses the box's edge."
	}
	fmt.Fprintf(&b, `
Rules:
%[2]s
- Replace every colour box with its asset, so no box colour is left anywhere.
- Assets never touch or overlap.
- Everything that is not an asset is one flat, solid %[1]s: no gradient, texture, noise, vignette, ground, cast shadow or frame. Holes and empty centres inside assets are %[1]s too.
- No text, letters, numbers or symbols anywhere unless an asset asks for them.
- All assets belong to one set: same style, light direction, palette and level of detail.`, bg, fill)
	return append(parts, Part{Text: b.String()})
}

// isoParagraph states the camera and scale rule for sheets with footprints:
// when the sketch shows guides it points at the white diamonds, otherwise it
// gives the same scale in words.
func isoParagraph(l *layout.Layout, guides bool) string {
	var b strings.Builder
	b.WriteString("ISOMETRIC CAMERA: every object is seen from the same isometric game camera, in 2:1 projection. Ground tiles are diamonds twice as wide as they are tall, and vertical edges stay vertical. One tile is the same size in every box, so a 1×1 object is small and a 3×3 object is large; objects do not grow to fill their boxes.")
	if guides {
		b.WriteString(" Each box has a white diamond near its bottom: the object's footprint on the ground. The object stands on its white diamond, its base covering the diamond exactly, corner to corner, and rises straight up from it. The diamonds are guides only: no white diamond, ground tile or base plate may show in the result.")
		return b.String()
	}
	// The same information in words: tile size as a share of a box's width.
	share := 0.0
	for _, p := range l.Pieces {
		if len(p.Footprint) == 2 && p.Box.W > 0 {
			share = float64(l.Tile()) / float64(p.Box.W)
			break
		}
	}
	fmt.Fprintf(&b, " Each object stands at the bottom centre of its box on a footprint of the given number of tiles; one tile is %s of a box's width. No ground tile or base plate may show in the result.", fraction(share))
	return b.String()
}

// fraction says a share in words: "about a quarter", "about 30%".
func fraction(f float64) string {
	for _, c := range []struct {
		v    float64
		name string
	}{{0.5, "half"}, {1.0 / 3, "a third"}, {0.25, "a quarter"}, {0.2, "a fifth"}} {
		if math.Abs(f-c.v) < 0.02 {
			return "about " + c.name
		}
	}
	return fmt.Sprintf("about %.0f%%", f*100)
}

func compileSheet(l *layout.Layout, sketchPath string) []Part {
	if l.Sketch == layout.SketchColors {
		return compileColors(l, sketchPath)
	}
	bg := strings.ToUpper(l.Canvas.Background)
	w := styleWords[l.Sketch]
	var parts []Part

	if l.Sketch == layout.SketchNone {
		parts = append(parts, Part{Text: "Create one game asset sheet: separate, game-ready assets laid out on a flat background, each placed in the area given below.\n\n" + w.intro})
	} else {
		parts = append(parts, Part{Text: "Create one game asset sheet: separate, game-ready assets laid out on a flat background, placed as the layout sketch shows.\n\n" + w.intro})
		parts = append(parts, Part{Image: sketchPath, Label: "sketch"})
	}
	parts = append(parts, refParts(l, "copy its look only (materials, palette, lighting, shading, line quality, level of detail); ignore its layout, composition, subjects and any text")...)

	var b strings.Builder
	if l.Style != "" {
		fmt.Fprintf(&b, "Art style: %s\n", l.Style)
	}
	if l.Prompt != "" {
		fmt.Fprintf(&b, "Sheet: %s\n", l.Prompt)
	}
	if l.IsIso() {
		// Style none sends no sketch, so there are no diamonds to point at.
		b.WriteString("\n" + isoParagraph(l, l.Guides() && l.Sketch != layout.SketchNone) + "\n")
	}
	if l.Sketch == layout.SketchNone {
		b.WriteString("\nAssets, each in its own area:\n")
	} else {
		fmt.Fprintf(&b, "\nAssets, one per numbered %s:\n", w.unit)
	}
	W, H := l.Space()
	sw := sketch.Swatches(l, l.Sketch)
	for i, p := range l.Pieces {
		mark := ""
		if w.colour {
			mark = sw[i].Name + " " + w.unit + ", "
		}
		fmt.Fprintf(&b, "%d. %s%s: %s — %s", i+1, mark, Where(p.Box, W, H), p.Kind, strings.TrimRight(p.Prompt, ". "))
		if len(p.Footprint) == 2 {
			fmt.Fprintf(&b, ", standing on a %d×%d-tile footprint", p.Footprint[0], p.Footprint[1])
		}
		if h := kindHint(p.Kind); h != "" {
			fmt.Fprintf(&b, ". %s", h)
		}
		b.WriteString(".\n")
	}
	area := w.unit
	if l.Sketch == layout.SketchOutlines {
		area = "outlined area"
	}
	fill := fmt.Sprintf("- Draw each asset inside its own %[1]s, centred, filling most of the %[1]s, with a small margin so no part crosses the %[1]s's edge.", area)
	if l.IsIso() {
		fill = fmt.Sprintf("- Each object keeps its true size on the tile grid and stays inside its own %[1]s; no part crosses the %[1]s's edge.", area)
	}
	fmt.Fprintf(&b, `
Rules:
%[3]s
- Every asset stands alone. Assets never touch or overlap, and nothing is drawn in the gaps between %[1]ss.
- Everything that is not an asset is one flat, solid %[2]s: no gradient, texture, noise, vignette, ground, cast shadow or frame. Holes and empty centres inside assets are %[2]s too.
- No text, letters, numbers or symbols anywhere unless an asset asks for them.
- All assets belong to one set: same style, light direction, palette and level of detail.`, gapUnit(l.Sketch), bg, fill)
	parts = append(parts, Part{Text: b.String()})
	return parts
}

func gapUnit(style string) string {
	if style == layout.SketchBlocks {
		return "block"
	}
	return "area"
}

func compileReference(l *layout.Layout) []Part {
	var parts []Part
	parts = append(parts, Part{Text: "Create one image that will be the art-direction reference for a game. Every later asset for this game will be generated to match it, so the style must be clear, consistent and easy to read."})
	parts = append(parts, refParts(l, "match its look so this image belongs to the same game")...)
	var b strings.Builder
	if l.Style != "" {
		fmt.Fprintf(&b, "Art style: %s\n", l.Style)
	}
	fmt.Fprintf(&b, "Image: %s\n", strings.TrimSpace(l.Prompt))
	b.WriteString("No text, letters, numbers, logos or watermarks anywhere.")
	parts = append(parts, Part{Text: b.String()})
	return parts
}

func refParts(l *layout.Layout, instruction string) []Part {
	var parts []Part
	for i, ref := range l.StyleRefs {
		label := fmt.Sprintf("style_ref_%d", i+1)
		parts = append(parts, Part{Text: fmt.Sprintf("STYLE REFERENCE %d: %s.", i+1, instruction)})
		parts = append(parts, Part{Image: l.RefPath(ref), Label: label})
	}
	return parts
}

func kindHint(kind string) string {
	switch kind {
	case "button":
		return "One button shape with a plain face and no label, since the game adds the label"
	case "frame":
		return "The centre is empty and shows the flat background colour"
	case "nine_slice":
		return "Built to stretch: decorated corners, plain uniform straight edges, and a plain uniform centre"
	case "icon":
		return "One centred icon"
	case "sprite":
		return "One complete object, nothing cut off"
	}
	return ""
}

// Where describes a box in words: coarse position, extent in percent and shape.
func Where(b layout.Box, W, H int) string {
	cx := (float64(b.X) + float64(b.W)/2) / float64(W)
	cy := (float64(b.Y) + float64(b.H)/2) / float64(H)
	row := third(cy, "top", "middle", "bottom")
	col := third(cx, "left", "centre", "right")
	pos := row + " " + col
	if pos == "middle centre" {
		pos = "centre"
	}
	pct := func(v, total int) int { return int(100*float64(v)/float64(total) + 0.5) }
	r := float64(b.W) / float64(b.H)
	var shape string
	switch {
	case r >= 1.3:
		shape = fmt.Sprintf("wide, %.1f:1", r)
	case r <= 1/1.3:
		shape = fmt.Sprintf("tall, 1:%.1f", 1/r)
	default:
		shape = "about square"
	}
	return fmt.Sprintf("%s (x %d–%d%%, y %d–%d%%, %s)", pos,
		pct(b.X, W), pct(b.X+b.W, W), pct(b.Y, H), pct(b.Y+b.H, H), shape)
}

func third(v float64, a, b, c string) string {
	switch {
	case v < 1.0/3:
		return a
	case v < 2.0/3:
		return b
	}
	return c
}

// Render returns the parts as readable text, with images as placeholders,
// for prompt.txt in the run directory.
func Render(parts []Part) string {
	var b strings.Builder
	for _, p := range parts {
		if p.Image != "" {
			fmt.Fprintf(&b, "[image %s: %s]\n\n", p.Label, filepath.Base(p.Image))
			continue
		}
		b.WriteString(p.Text)
		b.WriteString("\n\n")
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}
