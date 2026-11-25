package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/codezerg/blockout-ai-game-assets/internal/extract"
)

// exportManifest is the manifest.json written by export: the pieces of
// several runs in one directory, with files relative to it. Pieces keep the
// run manifest's fields, plus the run they came from and, for isometric
// sprites, that run's tile.
type exportManifest struct {
	Generator string         `json:"generator"`
	Sources   []exportSource `json:"sources"`
	Pieces    []exportPiece  `json:"pieces"`
}

type exportSource struct {
	Run     string   `json:"run"` // run directory name, e.g. ui-kit-0001
	Kind    string   `json:"kind"`
	Layout  string   `json:"layout"` // the layout's directory and file name, not its full path
	Model   string   `json:"model"`
	CostUSD *float64 `json:"cost_usd"`
}

type exportPiece struct {
	extract.Piece
	From string `json:"from"` // the run directory name
	// Tile is the isometric tile an iso sprite was corrected to: its run's
	// manifest tile, which differs between runs (2K tiles are twice 1K's).
	Tile *extract.TileInfo `json:"tile,omitempty"`
}

var jsIdent = regexp.MustCompile(`^[A-Za-z_$][A-Za-z0-9_$]*$`)

// export copies the pieces of one or more runs into one directory and
// writes a manifest.json for them all, and with --js a manifest.js that
// sets a global, for pages opened from file:// that cannot fetch JSON.
// A reference run contributes its whole image, named after its layout.
func (c *cli) export(args []string) int {
	fs := flag.NewFlagSet("export", flag.ContinueOnError)
	to := fs.String("to", "", "directory to copy pieces and the manifest into (required)")
	js := fs.String("js", "", "also write manifest.js setting window.<name> to the manifest")
	runs, err := parse(fs, args)
	if err != nil {
		return c.usage(err)
	}
	if len(runs) == 0 || *to == "" {
		return c.usage(errors.New("export takes one or more run directories and --to <dir>"))
	}
	if *js != "" && !jsIdent.MatchString(*js) {
		return c.usage(fmt.Errorf("--js %q: use a JavaScript identifier such as GAME_ART", *js))
	}

	// Plan everything before copying anything, so a clash leaves no half
	// export behind.
	type copyJob struct{ src, dst string }
	var jobs []copyJob
	man := exportManifest{Generator: "blockout"}
	seen := map[string]string{} // piece id -> run it came from
	var warns []string
	for _, dir := range runs {
		info, err := readRunInfo(dir)
		if err != nil {
			return c.fail(exitUsage, failure{Code: "not_a_run", Message: "no readable run.json in " + filepath.ToSlash(dir) + "; pass run directories such as out/<name>-NNNN", Dir: filepath.ToSlash(dir)})
		}
		run := filepath.Base(filepath.Clean(dir))
		if info.Status != "ok" {
			return c.fail(exitUsage, failure{Code: "run_not_ok", Message: fmt.Sprintf("run %s has status %q and nothing to export", run, info.Status), Dir: filepath.ToSlash(dir)})
		}
		man.Sources = append(man.Sources, exportSource{Run: run, Kind: info.Kind, Layout: layoutLabel(info.Layout), Model: info.Model, CostUSD: info.CostUSD})

		var pieces []extract.Piece
		var tile *extract.TileInfo
		if info.Kind == "reference" {
			id := imageID(info.Layout, info.Name)
			p := extract.Piece{ID: id, Kind: "image", File: "sheet.png"}
			if info.Sheet != nil {
				p.Size = &extract.Size{W: info.Sheet.W, H: info.Sheet.H}
				p.Rect = extract.Rect{W: info.Sheet.W, H: info.Sheet.H}
			}
			pieces = append(pieces, p)
		} else {
			data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
			if err != nil {
				return c.fail(exitUsage, failure{Code: "no_manifest", Message: "no manifest.json in " + filepath.ToSlash(dir) + "; run blockout extract on it first", Dir: filepath.ToSlash(dir)})
			}
			var m extract.Manifest
			if err := json.Unmarshal(data, &m); err != nil {
				return c.internal(fmt.Errorf("%s: %w", filepath.Join(dir, "manifest.json"), err))
			}
			pieces, tile = m.Pieces, m.Tile
		}

		for _, p := range pieces {
			if p.File == "" {
				warns = append(warns, fmt.Sprintf("%s: missing in %s, not exported", p.ID, run))
				continue
			}
			if prev, ok := seen[p.ID]; ok {
				return c.fail(exitUsage, failure{Code: "id_clash", Message: fmt.Sprintf(
					"piece id %q is in both %s and %s; export them to separate directories or rename one in its layout", p.ID, prev, run)})
			}
			seen[p.ID] = run
			src := filepath.Join(dir, filepath.FromSlash(p.File))
			p.File = p.ID + strings.ToLower(filepath.Ext(src))
			jobs = append(jobs, copyJob{src, filepath.Join(*to, p.File)})
			ep := exportPiece{Piece: p, From: run}
			if p.Footprint != nil {
				ep.Tile = tile
			}
			man.Pieces = append(man.Pieces, ep)
			for _, f := range p.Flags {
				if f != "missing" {
					warns = append(warns, fmt.Sprintf("%s: flagged %s in %s", p.ID, f, run))
				}
			}
		}
	}

	if err := os.MkdirAll(*to, 0o755); err != nil {
		return c.internal(err)
	}
	for _, j := range jobs {
		data, err := os.ReadFile(j.src)
		if err != nil {
			return c.internal(err)
		}
		if err := os.WriteFile(j.dst, data, 0o644); err != nil {
			return c.internal(err)
		}
	}
	manPath := filepath.Join(*to, "manifest.json")
	if err := writeJSON(manPath, man); err != nil {
		return c.internal(err)
	}
	res := map[string]any{"ok": true, "to": filepath.ToSlash(*to), "manifest": filepath.ToSlash(manPath)}
	if *js != "" {
		body, err := json.MarshalIndent(man, "", "  ")
		if err != nil {
			return c.internal(err)
		}
		jsPath := filepath.Join(*to, "manifest.js")
		src := "// Generated by blockout export; do not edit.\nwindow." + *js + " = " + string(body) + ";\n"
		if err := os.WriteFile(jsPath, []byte(src), 0o644); err != nil {
			return c.internal(err)
		}
		res["js"] = filepath.ToSlash(jsPath)
	}

	list := make([]map[string]any, len(man.Pieces))
	for i, p := range man.Pieces {
		list[i] = map[string]any{"id": p.ID, "file": filepath.ToSlash(filepath.Join(*to, p.File)), "from": p.From}
	}
	sort.Strings(warns)
	res["pieces"] = list
	res["warnings"] = append([]string{}, warns...)
	c.emit(res)
	return exitOK
}

// imageID names a reference run's image after its layout file: the stem,
// or the directory for a file named layout.yaml. When the path doesn't name
// that directory (a bare "layout.yaml"), the run's name does.
func imageID(layoutPath, runName string) string {
	p := filepath.FromSlash(layoutPath)
	stem := strings.TrimSuffix(filepath.Base(p), filepath.Ext(p))
	if stem != "layout" {
		return stem
	}
	if dir := filepath.Base(filepath.Dir(p)); !isRootOrDot(dir) {
		return dir
	}
	return runName
}

// layoutLabel is how an exported manifest names a run's layout: its
// directory and file name. The full path would publish the local file system
// in a game's assets.
func layoutLabel(layoutPath string) string {
	p := filepath.FromSlash(layoutPath)
	if dir := filepath.Base(filepath.Dir(p)); !isRootOrDot(dir) {
		return dir + "/" + filepath.Base(p)
	}
	return filepath.Base(p)
}

func isRootOrDot(dir string) bool {
	return dir == "." || dir == string(filepath.Separator) || dir == "/"
}
