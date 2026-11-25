package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/color"
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/codezerg/blockout-ai-game-assets/internal/backend"
	"github.com/codezerg/blockout-ai-game-assets/internal/extract"
	"github.com/codezerg/blockout-ai-game-assets/internal/layout"
	"github.com/codezerg/blockout-ai-game-assets/internal/matte"
	"github.com/codezerg/blockout-ai-game-assets/internal/measure"
	"github.com/codezerg/blockout-ai-game-assets/internal/preview"
	"github.com/codezerg/blockout-ai-game-assets/internal/prompt"
	"github.com/codezerg/blockout-ai-game-assets/internal/segment"
	"github.com/codezerg/blockout-ai-game-assets/internal/sketch"
)

// runInfo is run.json: what was asked, what came back, what it cost.
type runInfo struct {
	Run          string           `json:"run"`
	Name         string           `json:"name"`
	Tag          string           `json:"tag,omitempty"`
	Status       string           `json:"status"` // ok, failed, dry_run; generated while a paid sheet is being processed
	Kind         string           `json:"kind"`   // sheet, reference
	Layout       string           `json:"layout"`
	Backend      string           `json:"backend"`
	Model        string           `json:"model"`
	ServedModel  string           `json:"served_model,omitempty"`
	Provider     string           `json:"provider,omitempty"`
	GenerationID string           `json:"generation_id,omitempty"`
	Aspect       string           `json:"aspect"`
	Size         string           `json:"size"`
	Sketch       string           `json:"sketch,omitempty"`
	SketchScale  float64          `json:"sketch_scale,omitempty"`
	CostUSD      *float64         `json:"cost_usd"`
	Attempts     int              `json:"attempts,omitempty"`
	DurationS    float64          `json:"duration_s,omitempty"`
	Sheet        *sheetInfo       `json:"sheet,omitempty"`
	ModelText    string           `json:"model_text,omitempty"`
	StyleRefs    []string         `json:"style_refs,omitempty"`
	Started      time.Time        `json:"started"`
	Error        *backend.Error   `json:"error,omitempty"`
	Warnings     []string         `json:"warnings,omitempty"`
	Problems     []layout.Problem `json:"layout_warnings,omitempty"`
}

type sheetInfo struct {
	File string `json:"file"`
	W    int    `json:"w"`
	H    int    `json:"h"`
	MIME string `json:"returned_mime"`
}

// colorsFit rejects a sheet whose key colour is close to every sketch
// colour, and a colors-style sheet with more pieces than nameable colours,
// since two boxes would share a name.
func (c *cli) colorsFit(l *layout.Layout) (int, bool) {
	if l.IsReference() {
		return 0, true
	}
	n := sketch.Distinct(l, l.Sketch)
	if n == 0 {
		return c.fail(exitLayout, failure{Code: "layout_invalid", Message: fmt.Sprintf(
			"no sketch colour stands out from the key %s, so boxes and background can't be told apart; use a saturated key such as #00FF00 or #FF00FF",
			l.Canvas.Background)}), false
	}
	if l.Sketch != layout.SketchColors {
		return 0, true
	}
	if len(l.Pieces) > n {
		return c.fail(exitLayout, failure{Code: "layout_invalid", Message: fmt.Sprintf(
			"sketch style colors names each box by colour and has %d colours that stand out from %s, but the layout has %d pieces; split the sheet or use sketch: blocks",
			n, l.Canvas.Background, len(l.Pieces))}), false
	}
	return 0, true
}

func kindOf(l *layout.Layout) string {
	if l.IsReference() {
		return "reference"
	}
	return "sheet"
}

// --- sketch ---

func (c *cli) sketch(args []string) int {
	fs := flag.NewFlagSet("sketch", flag.ContinueOnError)
	outPath := fs.String("out", "", "where to write the sketch PNG (default out/sketches/<name>.png)")
	style := fs.String("sketch", "", "sketch style: colors, blocks, outlines or none (default: the layout's)")
	pos, err := parse(fs, args)
	if err != nil {
		return c.usage(err)
	}
	if len(pos) != 1 {
		return c.usage(errors.New("sketch takes one layout file"))
	}
	l, warns, err := layout.Load(pos[0], layout.Options{CheckFiles: false})
	if err != nil {
		return c.layoutFail(err)
	}
	if *style != "" {
		if !layout.ValidSketch(*style) {
			return c.usage(fmt.Errorf("--sketch %q: use colors, blocks, outlines or none", *style))
		}
		l.Sketch = *style
	}
	if code, ok := c.colorsFit(l); !ok {
		return code
	}
	W, H := l.Space()
	res := map[string]any{
		"ok": true, "name": l.Name(), "kind": kindOf(l), "sketch_style": l.Sketch,
		"layout_space": extract.Size{W: W, H: H}, "pieces": len(l.Pieces),
		"warnings": problemStrings(warns),
	}
	if l.IsReference() {
		res["sketch"] = nil
		res["note"] = "reference layout (prompt, no pieces): nothing to sketch"
		c.emit(res)
		return exitOK
	}
	path := *outPath
	if path == "" {
		path = filepath.Join("out", "sketches", l.Name()+".png")
	}
	if err := savePNG(path, sketch.Render(l, l.Sketch)); err != nil {
		return c.internal(err)
	}
	res["sketch"] = filepath.ToSlash(path)
	if l.Sketch == layout.SketchNone {
		res["note"] = "style none sends no sketch; this image only shows the boxes"
	}
	c.emit(res)
	return exitOK
}

// --- gen ---

func (c *cli) gen(args []string) int {
	fs := flag.NewFlagSet("gen", flag.ContinueOnError)
	size := fs.String("size", "", "output size: 0.5K, 1K, 2K or 4K (default: the layout's canvas.size)")
	model := fs.String("model", "", "OpenRouter model id (default: the layout's model, else "+defaultModel+")")
	style := fs.String("sketch", "", "sketch style: colors, blocks, outlines or none (default: the layout's)")
	scale := fs.Float64("sketch-scale", 1, "experimental: draw sketch marks and positions at this fraction of each box, about its centre")
	tag := fs.String("tag", "", "label for grouping runs, e.g. an experiment variant")
	budget := fs.Float64("budget", 0, "refuse to run once runs with this --tag have cost this many USD")
	beName := fs.String("backend", "openrouter", "openrouter or fake")
	root := fs.String("out", "out", "directory that holds run directories")
	dry := fs.Bool("dry-run", false, "write the run directory and request without calling the model")
	pos, err := parse(fs, args)
	if err != nil {
		return c.usage(err)
	}
	if len(pos) != 1 {
		return c.usage(errors.New("gen takes one layout file"))
	}
	if *scale < 0.5 || *scale > 1 {
		return c.usage(fmt.Errorf("--sketch-scale %v: use 0.5 to 1", *scale))
	}
	if *budget > 0 && *tag == "" {
		return c.usage(errors.New("--budget needs a --tag to count spending against"))
	}

	l, warns, err := layout.Load(pos[0], layout.Options{CheckFiles: true})
	if err != nil {
		return c.layoutFail(err)
	}
	if *size != "" {
		if _, _, ok := layout.Dims(l.Canvas.Aspect, *size); !ok {
			return c.usage(fmt.Errorf("--size %q: use 0.5K, 1K, 2K or 4K", *size))
		}
		l.Canvas.Size = *size
	}
	if *style != "" {
		if !layout.ValidSketch(*style) {
			return c.usage(fmt.Errorf("--sketch %q: use colors, blocks, outlines or none", *style))
		}
		l.Sketch = *style
	}
	if code, ok := c.colorsFit(l); !ok {
		return code
	}
	m := *model
	if m == "" {
		m = l.Model
	}
	if m == "" {
		m = defaultModel
	}

	var be backend.Backend
	switch *beName {
	case "fake":
		be = backend.Fake{Guides: l.Guides() && l.Sketch != layout.SketchNone}
	case "openrouter":
		key := os.Getenv("OPENROUTER_API_KEY")
		if key == "" && !*dry {
			return c.fail(exitConfig, failure{Code: "no_api_key", Message: "set OPENROUTER_API_KEY, or use --backend fake or --dry-run"})
		}
		be = backend.NewOpenRouter(key)
	default:
		return c.usage(fmt.Errorf("--backend %q: use openrouter or fake", *beName))
	}

	if *budget > 0 && !*dry {
		spent := tagSpend(*root, *tag)
		if spent >= *budget {
			return c.fail(exitBudget, failure{Code: "budget_reached", Message: fmt.Sprintf(
				"runs tagged %q have cost $%.4f of the $%.2f budget; ask the user before raising --budget", *tag, spent, *budget)})
		}
	}

	dir, id, err := newRunDir(*root, l.Name())
	if err != nil {
		return c.internal(err)
	}
	info := runInfo{
		Run: id, Name: l.Name(), Tag: *tag, Kind: kindOf(l), Layout: filepath.ToSlash(l.Path),
		Backend: be.Name(), Model: m, Aspect: l.Canvas.Aspect, Size: l.Canvas.Size,
		Started: time.Now().UTC().Truncate(time.Second), StyleRefs: l.StyleRefs, Problems: warns,
	}
	if !l.IsReference() {
		info.Sketch = l.Sketch
		if *scale != 1 {
			info.SketchScale = *scale
		}
	}
	aim := l // what the sketch and prompt describe
	if *scale != 1 {
		aim = l.Scaled(*scale)
	}
	if err := os.WriteFile(filepath.Join(dir, "layout.yaml"), l.Source, 0o644); err != nil {
		return c.internal(err)
	}

	sketchPath := ""
	if !l.IsReference() && l.Sketch != layout.SketchNone {
		sketchPath = filepath.Join(dir, "sketch.png")
		if err := savePNG(sketchPath, sketch.Render(aim, l.Sketch)); err != nil {
			return c.internal(err)
		}
	}
	parts := prompt.Compile(aim, sketchPath)
	if err := os.WriteFile(filepath.Join(dir, "prompt.txt"), []byte(prompt.Render(parts)), 0o644); err != nil {
		return c.internal(err)
	}
	req := backend.Request{Model: m, Aspect: l.Canvas.Aspect, Size: l.Canvas.Size}
	for _, p := range parts {
		if p.Image == "" {
			req.Parts = append(req.Parts, backend.Part{Text: p.Text})
			continue
		}
		data, err := os.ReadFile(p.Image)
		if err != nil {
			return c.internal(err)
		}
		if p.Label != "sketch" { // keep a copy of every ref the run used
			if err := copyInto(filepath.Join(dir, "refs"), p.Label+filepath.Ext(p.Image), data); err != nil {
				return c.internal(err)
			}
		}
		req.Parts = append(req.Parts, backend.Part{Image: data, MIME: backend.SniffMIME(data), Label: p.Label})
	}

	summary := map[string]any{
		"run": id, "dir": filepath.ToSlash(dir), "kind": info.Kind, "model": m,
		"size": l.Canvas.Size, "aspect": l.Canvas.Aspect,
		"prompt": filepath.ToSlash(filepath.Join(dir, "prompt.txt")),
	}
	if *tag != "" {
		summary["tag"] = *tag
	}
	if info.Sketch != "" {
		summary["sketch_style"] = info.Sketch
	}
	if sketchPath != "" {
		summary["sketch"] = filepath.ToSlash(sketchPath)
	}

	if *dry {
		body, err := backend.BuildRequestJSON(req)
		if err != nil {
			return c.internal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "request.json"), body, 0o644); err != nil {
			return c.internal(err)
		}
		info.Status = "dry_run"
		if err := writeJSON(filepath.Join(dir, "run.json"), info); err != nil {
			return c.internal(err)
		}
		summary["ok"] = true
		summary["dry_run"] = true
		summary["request"] = filepath.ToSlash(filepath.Join(dir, "request.json"))
		summary["warnings"] = problemStrings(warns)
		c.emit(summary)
		return exitOK
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 20*time.Minute)
	defer cancel()

	start := time.Now()
	res, gerr := be.Generate(ctx, req)
	info.DurationS = round(time.Since(start).Seconds(), 1)

	if gerr != nil {
		var berr *backend.Error
		if !errors.As(gerr, &berr) {
			berr = &backend.Error{Code: "internal", Message: gerr.Error()}
		}
		saveIfAny(filepath.Join(dir, "request.json"), berr.RequestJSON)
		saveIfAny(filepath.Join(dir, "response.json"), berr.ResponseJSON)
		info.Status, info.Error, info.Attempts, info.CostUSD = "failed", berr, berr.Attempts, berr.Cost
		_ = writeJSON(filepath.Join(dir, "run.json"), info)
		retry := berr.Retryable
		return c.fail(exitModel, failure{Code: berr.Code, Message: berr.Message, Retryable: &retry, Run: id, Dir: filepath.ToSlash(dir)})
	}

	saveIfAny(filepath.Join(dir, "request.json"), res.RequestJSON)
	saveIfAny(filepath.Join(dir, "response.json"), res.ResponseJSON)
	info.ServedModel, info.Provider, info.GenerationID, info.CostUSD = res.Model, res.Provider, res.ID, res.Cost
	info.Attempts, info.ModelText = res.Attempts, res.Text
	// The call is paid for now: record its cost before anything else can fail,
	// so --budget and runs still count it.
	info.Status = "generated"
	if err := writeJSON(filepath.Join(dir, "run.json"), info); err != nil {
		return c.internal(err)
	}
	failAfterPaid := func(err error) int {
		info.Status, info.Error = "failed", &backend.Error{Code: "internal", Message: err.Error()}
		_ = writeJSON(filepath.Join(dir, "run.json"), info)
		return c.internal(err)
	}

	img, _, err := image.Decode(bytes.NewReader(res.Image))
	if err != nil {
		return failAfterPaid(fmt.Errorf("decoding returned %s image: %w", res.MIME, err))
	}
	sheetPath := filepath.Join(dir, "sheet.png")
	if res.MIME == "image/png" {
		err = os.WriteFile(sheetPath, res.Image, 0o644)
	} else {
		err = savePNG(sheetPath, img) // lossless from the decoded pixels
	}
	if err != nil {
		return failAfterPaid(err)
	}
	b := img.Bounds()
	info.Sheet = &sheetInfo{File: "sheet.png", W: b.Dx(), H: b.Dy(), MIME: res.MIME}
	if ew, eh, ok := layout.Dims(l.Canvas.Aspect, l.Canvas.Size); ok && (ew != b.Dx() || eh != b.Dy()) {
		info.Warnings = append(info.Warnings, fmt.Sprintf("expected %dx%d for %s at %s, got %dx%d", ew, eh, l.Canvas.Aspect, l.Canvas.Size, b.Dx(), b.Dy()))
	}

	summary["sheet"] = filepath.ToSlash(sheetPath)
	summary["sheet_size"] = extract.Size{W: b.Dx(), H: b.Dy()}
	if !l.IsReference() {
		d, err := derive(dir, l, img, info, matte.Soft)
		if err != nil {
			return failAfterPaid(err)
		}
		info.Warnings = append(info.Warnings, d.warnings...)
		d.addTo(summary, dir)
	}

	info.Status = "ok"
	if err := writeJSON(filepath.Join(dir, "run.json"), info); err != nil {
		return c.internal(err)
	}
	summary["ok"] = true
	summary["provider"] = res.Provider
	summary["cost_usd"] = res.Cost
	summary["duration_s"] = info.DurationS
	summary["attempts"] = res.Attempts
	if *tag != "" {
		summary["tag_spent_usd"] = round(tagSpend(*root, *tag), 4)
	}
	if res.Text != "" {
		summary["model_text"] = res.Text
	}
	summary["warnings"] = append(problemStrings(warns), info.Warnings...)
	c.emit(summary)
	return exitOK
}

// --- extract ---

func (c *cli) extract(args []string) int {
	fs := flag.NewFlagSet("extract", flag.ContinueOnError)
	mode := fs.String("matte", string(matte.Soft), "background removal: soft (partial alpha on edges, despill) or hard")
	pos, err := parse(fs, args)
	if err != nil {
		return c.usage(err)
	}
	if len(pos) != 1 {
		return c.usage(errors.New("extract takes one run directory"))
	}
	if !matte.Valid(*mode) {
		return c.usage(fmt.Errorf("--matte %q: use soft or hard", *mode))
	}
	dir := pos[0]
	info, err := readRunInfo(dir)
	if err != nil {
		return c.fail(exitUsage, failure{Code: "not_a_run", Message: "no readable run.json in " + filepath.ToSlash(dir) + "; pass a run directory such as out/<name>-NNNN", Dir: filepath.ToSlash(dir)})
	}
	l, _, err := layout.Load(filepath.Join(dir, "layout.yaml"), layout.Options{CheckFiles: false})
	if err != nil {
		return c.layoutFail(err)
	}
	if l.IsReference() {
		return c.fail(exitUsage, failure{Code: "usage", Message: "this run is a reference image; it has no pieces to extract", Dir: filepath.ToSlash(dir)})
	}
	switch {
	case info.Sketch != "":
		l.Sketch = info.Sketch // the style the sheet was generated with
	case info.Run != "":
		l.Sketch = layout.SketchBlocks // runs from before sketch styles existed
	}
	f, err := os.Open(filepath.Join(dir, "sheet.png"))
	if err != nil {
		return c.fail(exitUsage, failure{Code: "no_sheet", Message: "no sheet.png in " + filepath.ToSlash(dir) + "; was the run a dry run or a failure?"})
	}
	img, _, err := image.Decode(f)
	f.Close()
	if err != nil {
		return c.internal(err)
	}
	d, err := derive(dir, l, img, info, matte.Mode(*mode))
	if err != nil {
		return c.internal(err)
	}
	res := map[string]any{"ok": true, "dir": filepath.ToSlash(dir), "warnings": append([]string{}, d.warnings...)}
	d.addTo(res, dir)
	c.emit(res)
	return exitOK
}

// derived is everything computed from a sheet without calling the model.
type derived struct {
	pieces   []extract.Piece
	scores   *measure.Scores
	warnings []string
}

func (d *derived) addTo(m map[string]any, dir string) {
	m["manifest"] = filepath.ToSlash(filepath.Join(dir, "manifest.json"))
	m["sheet_alpha"] = filepath.ToSlash(filepath.Join(dir, "sheet_alpha.png"))
	m["scores"] = d.scores.Summary
	m["preview"] = filepath.ToSlash(filepath.Join(dir, "preview.png"))
	m["pieces"] = pieceFiles(dir, d.pieces)
}

// derive does everything that needs the sheet but not the model: segment
// it, score placement, remove the background, cut out the pieces, and write
// sheet_alpha.png, pieces/, manifest.json, scores.json and preview.png.
func derive(dir string, l *layout.Layout, sheet image.Image, info runInfo, mode matte.Mode) (*derived, error) {
	if err := os.RemoveAll(filepath.Join(dir, "pieces")); err != nil {
		return nil, err
	}
	seg := segment.Run(l, sheet)
	sc := measure.Score(l, seg, sheet, l.Sketch)

	m := matte.Compute(sheet, seg, mode)
	if err := savePNG(filepath.Join(dir, "sheet_alpha.png"), m.Img); err != nil {
		return nil, err
	}
	pieces, imgs, warns, err := extract.Run(l, seg, m, dir)
	if err != nil {
		return nil, err
	}
	uiWarns, err := extract.UI(l, pieces, imgs, dir)
	if err != nil {
		return nil, err
	}
	warns = append(warns, uiWarns...)
	tile, isoSum, spWarns, err := extract.Sprites(l, seg, pieces, imgs, dir)
	if err != nil {
		return nil, err
	}
	warns = append(warns, spWarns...)
	sc.Iso = isoSum
	if err := writeJSON(filepath.Join(dir, "scores.json"), sc); err != nil {
		return nil, err
	}
	b := sheet.Bounds()
	W, H := l.Space()
	if err := extract.WriteManifest(dir, extract.Manifest{
		Source: "sheet_alpha.png", Matte: string(mode), Run: info.Run, Model: info.Model,
		Sheet: extract.Size{W: b.Dx(), H: b.Dy()}, Layout: extract.Size{W: W, H: H}, Tile: tile, Pieces: pieces,
	}); err != nil {
		return nil, err
	}

	var sk image.Image
	if f, err := os.Open(filepath.Join(dir, "sketch.png")); err == nil {
		sk, _, _ = image.Decode(f)
		f.Close()
	}
	if sk == nil {
		sk = sketch.Render(l, layout.SketchOutlines)
	}
	s := sc.Summary
	title := fmt.Sprintf("%s-%s  %s %s  sketch=%s  bg %s  matte=%s\nfound %d/%d  inside %d  IoU %.2f (min %.2f)  spill %d  merged %d  intrudes %d",
		info.Name, info.Run, shortModel(info.Model), info.Size, l.Sketch, l.Canvas.Background, mode,
		s.Found, s.Pieces, s.Inside, s.MeanIoU, s.MinIoU, s.MaxSpill, s.Merged, s.Intrudes)
	if is := sc.Iso; is != nil {
		title += fmt.Sprintf("\nbases %d/%d  drawn: angle %.1f sd %.1f, tile x%.2f cv %.2f, corner off %.0f (max %.0f)",
			is.Measured, is.Pieces, is.AngleMean, is.AngleSD, is.ScaleMean, is.ScaleCV, is.OffsetMean, is.OffsetMax)
		if a := is.After; a != nil {
			title += fmt.Sprintf("  after: tile within %.0f%%, angle within %.1f", 100*a.ScaleMaxDev, a.AngleMaxDev)
		}
	}
	pivots := make([][2]float64, len(pieces))
	for i, p := range pieces {
		pivots[i] = [2]float64{math.NaN(), math.NaN()}
		if p.Pivot != nil {
			pivots[i] = [2]float64{p.Pivot.X, p.Pivot.Y}
		}
	}
	opt := preview.Options{Marks: isoMarks(l, pieces, seg), Pivots: pivots, SameScale: l.IsIso()}
	if tile != nil {
		var placed []preview.Placed
		for i, p := range pieces {
			if imgs[i] == nil || len(p.Footprint) != 2 || p.Pivot == nil {
				continue
			}
			placed = append(placed, preview.Placed{Img: imgs[i], Pivot: [2]float64{p.Pivot.X, p.Pivot.Y},
				Foot: [2]int{p.Footprint[0], p.Footprint[1]}, Unmeasured: p.Base == nil})
		}
		if scene := preview.Scene(placed, tile.PX, 3); scene != nil {
			opt.Scene = scene
			if err := savePNG(filepath.Join(dir, "scene.png"), scene); err != nil {
				return nil, err
			}
		}
	}
	pv := preview.Render(title, l, sk, sheet, imgs, sc, opt)
	pv = preview.Stack(pv, preview.UIStrip(l, pieces, imgs, pv.Rect.Dx()))
	if err := savePNG(filepath.Join(dir, "preview.png"), pv); err != nil {
		return nil, err
	}
	return &derived{pieces: pieces, scores: sc, warnings: warns}, nil
}

func shortModel(m string) string {
	if i := len("google/"); len(m) > i && m[:i] == "google/" {
		return m[i:]
	}
	return m
}

// isoMarks draws, on the preview's sheet panel, every footprint guide as a
// white outline and every detected base as a V from its side corners to its
// front corner: green when its angle is right, red when flagged.
func isoMarks(l *layout.Layout, pieces []extract.Piece, seg *segment.Result) []preview.Mark {
	var marks []preview.Mark
	white := color.RGBA{0xFF, 0xFF, 0xFF, 255}
	for i := range l.Pieces {
		d, ok := l.Footprint(i)
		if !ok {
			continue
		}
		pts := []layout.Pt{d.Top, d.Right, d.Bottom, d.Left, d.Top}
		for k := 0; k < 4; k++ {
			marks = append(marks, preview.Mark{A: pts[k], B: pts[k+1], C: white})
		}
		b := pieces[i].Base
		if b == nil {
			continue
		}
		c := color.RGBA{0x30, 0xFF, 0x60, 255}
		for _, f := range pieces[i].Flags {
			if f == "angle_off" {
				c = color.RGBA{0xFF, 0x30, 0x30, 255}
			}
		}
		v := layout.Pt{X: d.Bottom.X + b.DX, Y: d.Bottom.Y + b.DY}
		rad := func(deg float64) float64 { return deg * math.Pi / 180 }
		lw, rw := b.Left, b.Right
		marks = append(marks,
			preview.Mark{A: layout.Pt{X: v.X - lw, Y: v.Y - lw*math.Tan(rad(b.AngleL))}, B: v, C: c},
			preview.Mark{A: v, B: layout.Pt{X: v.X + rw, Y: v.Y - rw*math.Tan(rad(b.AngleR))}, C: c})
	}
	return marks
}
