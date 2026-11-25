package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/codezerg/blockout-ai-game-assets/internal/extract"
)

func call(t *testing.T, args ...string) (map[string]any, int) {
	t.Helper()
	var out bytes.Buffer
	code := run(args, &out)
	var m map[string]any
	if err := json.Unmarshal(out.Bytes(), &m); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, out.String())
	}
	return m, code
}

func TestGenFakeEndToEnd(t *testing.T) {
	dir := t.TempDir()
	caseDir := filepath.Join(dir, "case")
	os.MkdirAll(filepath.Join(caseDir, "refs"), 0o755)
	os.WriteFile(filepath.Join(caseDir, "refs", "s.png"), []byte("\x89PNG\r\n\x1a\nfake"), 0o644)
	os.WriteFile(filepath.Join(caseDir, "layout.yaml"), []byte(`canvas: { aspect: "1:1", size: 2K, background: "#00FF00" }
style_refs: [refs/s.png]
pieces:
  - { id: a, kind: button, box: { x: 60, y: 60, w: 380, h: 120 }, prompt: button }
  - { id: b, kind: icon, box: { x: 60, y: 300, w: 160, h: 160 }, prompt: gear }
`), 0o644)
	root := filepath.Join(dir, "out")

	m, code := call(t, "gen", filepath.Join(caseDir, "layout.yaml"), "--backend", "fake", "--out", root)
	if code != 0 || m["ok"] != true {
		t.Fatalf("code %d: %v", code, m)
	}
	run := filepath.Join(root, "case-0001")
	for _, f := range []string{"layout.yaml", "sketch.png", "prompt.txt", "request.json", "response.json",
		"sheet.png", "manifest.json", "run.json", "pieces/a.png", "pieces/b.png", "refs/style_ref_1.png"} {
		if _, err := os.Stat(filepath.Join(run, f)); err != nil {
			t.Errorf("missing %s", f)
		}
	}
	if s := m["sheet_size"].(map[string]any); s["w"] != 2048.0 {
		t.Errorf("sheet size %v", s)
	}

	m, code = call(t, "extract", run)
	if code != 0 || len(m["pieces"].([]any)) != 2 {
		t.Errorf("extract code %d: %v", code, m)
	}
	m, _ = call(t, "gen", filepath.Join(caseDir, "layout.yaml"), "--backend", "fake", "--out", root, "--dry-run")
	if m["run"] != "0002" {
		t.Errorf("second run id %v", m["run"])
	}
}

func TestBudgetAndRuns(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "out")
	lay := filepath.Join(dir, "case", "layout.yaml")
	os.MkdirAll(filepath.Dir(lay), 0o755)
	os.WriteFile(lay, []byte(`canvas: { background: "#FF00FF" }
sketch: outlines
pieces:
  - { id: a, kind: icon, box: { x: 60, y: 60, w: 300, h: 300 }, prompt: gear }
`), 0o644)

	// Two fake runs under a tag, then pretend one of them cost money.
	for range 2 {
		if m, code := call(t, "gen", lay, "--backend", "fake", "--out", root, "--tag", "e"); code != 0 {
			t.Fatalf("gen: %v", m)
		}
	}
	runJSON := filepath.Join(root, "case-0001", "run.json")
	var info map[string]any
	data, _ := os.ReadFile(runJSON)
	json.Unmarshal(data, &info)
	if info["sketch"] != "outlines" || info["tag"] != "e" {
		t.Errorf("run.json: %v", info)
	}
	info["cost_usd"] = 0.30
	data, _ = json.Marshal(info)
	os.WriteFile(runJSON, data, 0o644)

	m, code := call(t, "gen", lay, "--backend", "fake", "--out", root, "--tag", "e", "--budget", "0.25")
	if code != exitBudget || m["error"].(map[string]any)["code"] != "budget_reached" {
		t.Errorf("want budget refusal, got %d %v", code, m)
	}
	if _, code := call(t, "gen", lay, "--backend", "fake", "--out", root, "--tag", "other", "--budget", "0.25"); code != 0 {
		t.Errorf("other tag should run, got %d", code)
	}

	m, code = call(t, "runs", "--out", root, "--tag", "e")
	if code != 0 || len(m["runs"].([]any)) != 2 || m["total_cost_usd"] != 0.3 {
		t.Fatalf("runs: %v", m)
	}
	g := m["groups"].([]any)[0].(map[string]any)
	if g["runs"] != 2.0 || g["scored"] != 2.0 || g["sketch"] != "outlines" || g["missing"] != 0.0 {
		t.Errorf("group: %v", g)
	}
	for _, f := range []string{"scores.json", "preview.png"} {
		if _, err := os.Stat(filepath.Join(root, "case-0001", f)); err != nil {
			t.Errorf("missing %s", f)
		}
	}
}

func TestExport(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "out")
	game := filepath.Join(dir, "game")
	os.MkdirAll(game, 0o755)
	os.WriteFile(filepath.Join(game, "ui.yaml"), []byte(`canvas: { background: "#00FF00" }
pieces:
  - { id: panel, kind: nine_slice, box: { x: 60, y: 60, w: 400, h: 300 }, prompt: panel }
  - { id: gear, kind: icon, box: { x: 600, y: 60, w: 160, h: 160 }, prompt: gear }
`), 0o644)
	os.WriteFile(filepath.Join(game, "backdrop.yaml"), []byte("canvas: { aspect: \"16:9\" }\nprompt: a calm sky\n"), 0o644)
	for _, f := range []string{"ui.yaml", "backdrop.yaml"} {
		if m, code := call(t, "gen", filepath.Join(game, f), "--backend", "fake", "--out", root); code != 0 {
			t.Fatalf("gen %s: %v", f, m)
		}
	}
	ui, sky := filepath.Join(root, "game-ui-0001"), filepath.Join(root, "game-backdrop-0001")
	assets := filepath.Join(dir, "assets")

	m, code := call(t, "export", ui, sky, "--to", assets, "--js", "ART")
	if code != 0 || len(m["pieces"].([]any)) != 3 {
		t.Fatalf("export code %d: %v", code, m)
	}
	for _, f := range []string{"panel.png", "gear.png", "backdrop.png", "manifest.json", "manifest.js"} {
		if _, err := os.Stat(filepath.Join(assets, f)); err != nil {
			t.Errorf("missing %s", f)
		}
	}
	var man exportManifest
	data, _ := os.ReadFile(filepath.Join(assets, "manifest.json"))
	if err := json.Unmarshal(data, &man); err != nil {
		t.Fatal(err)
	}
	if len(man.Sources) != 2 || len(man.Pieces) != 3 {
		t.Fatalf("manifest: %+v", man)
	}
	// The manifest ships with a game, so it names layouts without the local path.
	if got := man.Sources[0].Layout; got != "game/ui.yaml" {
		t.Errorf("source layout %q, want game/ui.yaml", got)
	}
	panel, sky0 := man.Pieces[0], man.Pieces[2]
	if panel.File != "panel.png" || panel.From != "game-ui-0001" || panel.Slice == nil || panel.Content == nil {
		t.Errorf("panel entry: %+v", panel)
	}
	if sky0.ID != "backdrop" || sky0.Kind != "image" || sky0.File != "backdrop.png" || sky0.Size == nil || sky0.Size.W != 1376 {
		t.Errorf("reference entry: %+v", sky0)
	}
	js, _ := os.ReadFile(filepath.Join(assets, "manifest.js"))
	if !bytes.Contains(js, []byte("window.ART = {")) {
		t.Errorf("manifest.js: %.80s", js)
	}
	if panel.Tile != nil {
		t.Errorf("a UI piece got a tile: %+v", panel.Tile)
	}

	// Isometric sprites carry their run's tile, which an engine scales by.
	os.WriteFile(filepath.Join(game, "town.yaml"), []byte(`canvas: { background: "#FF00FF" }
grid: { cols: 2, rows: 1, gutter: 64 }
cell_defaults: { kind: sprite, footprint: [2, 2] }
pieces:
  - { id: house, cell: [0, 0], prompt: a house }
  - { id: well, cell: [1, 0], footprint: [1, 1], prompt: a well }
`), 0o644)
	if m, code := call(t, "gen", filepath.Join(game, "town.yaml"), "--backend", "fake", "--out", root); code != 0 {
		t.Fatalf("gen town: %v", m)
	}
	town := filepath.Join(root, "game-town-0001")
	var runMan extract.Manifest
	data, _ = os.ReadFile(filepath.Join(town, "manifest.json"))
	json.Unmarshal(data, &runMan)
	if m, code := call(t, "export", town, "--to", filepath.Join(dir, "iso")); code != 0 {
		t.Fatalf("export town: %v", m)
	}
	var iso exportManifest
	data, _ = os.ReadFile(filepath.Join(dir, "iso", "manifest.json"))
	json.Unmarshal(data, &iso)
	if runMan.Tile == nil || len(iso.Pieces) != 2 {
		t.Fatalf("run tile %+v, exported %+v", runMan.Tile, iso.Pieces)
	}
	for _, p := range iso.Pieces {
		if p.Tile == nil || *p.Tile != *runMan.Tile {
			t.Errorf("%s: tile %+v, want the run's %+v", p.ID, p.Tile, runMan.Tile)
		}
	}

	// The same piece id from two runs is refused before anything is written.
	call(t, "gen", filepath.Join(game, "ui.yaml"), "--backend", "fake", "--out", root)
	m, code = call(t, "export", ui, filepath.Join(root, "game-ui-0002"), "--to", filepath.Join(dir, "clash"))
	if code != exitUsage || m["error"].(map[string]any)["code"] != "id_clash" {
		t.Errorf("want id_clash, got %d %v", code, m)
	}
	if _, err := os.Stat(filepath.Join(dir, "clash")); !os.IsNotExist(err) {
		t.Errorf("a refused export wrote files")
	}

	// A dry run has nothing to export.
	call(t, "gen", filepath.Join(game, "ui.yaml"), "--backend", "fake", "--out", root, "--dry-run")
	if m, code := call(t, "export", filepath.Join(root, "game-ui-0003"), "--to", assets); code != exitUsage || m["error"].(map[string]any)["code"] != "run_not_ok" {
		t.Errorf("want run_not_ok, got %d %v", code, m)
	}
}

func TestLayoutErrorExitCode(t *testing.T) {
	p := filepath.Join(t.TempDir(), "bad.yaml")
	os.WriteFile(p, []byte("canvas: { aspect: \"7:5\" }\nprompt: x\n"), 0o644)
	m, code := call(t, "gen", p, "--dry-run")
	if code != exitLayout || m["error"].(map[string]any)["code"] != "layout_invalid" {
		t.Errorf("code %d: %v", code, m)
	}
}

// A key close to every sketch colour is refused as a layout error, not a
// crash, for every style.
func TestKeyWithNoSketchColours(t *testing.T) {
	p := filepath.Join(t.TempDir(), "grey.yaml")
	os.WriteFile(p, []byte(`canvas: { background: "#8C8C8C" }
sketch: blocks
pieces:
  - { id: a, kind: icon, box: { x: 60, y: 60, w: 300, h: 300 }, prompt: gear }
`), 0o644)
	for _, args := range [][]string{{"sketch", p}, {"gen", p, "--dry-run"}} {
		m, code := call(t, args...)
		if code != exitLayout || m["error"].(map[string]any)["code"] != "layout_invalid" {
			t.Errorf("%s: code %d: %v", args[0], code, m)
		}
	}
}

// The fake backend draws every box whatever the sketch style; white labels
// and white boxes are not footprint guides.
func TestFakeDrawsEveryStyle(t *testing.T) {
	dir := t.TempDir()
	lay := filepath.Join(dir, "case", "layout.yaml")
	os.MkdirAll(filepath.Dir(lay), 0o755)
	os.WriteFile(lay, []byte(`canvas: { background: "#00FF00" }
pieces:
  - { id: a, kind: icon, box: { x: 60, y: 60, w: 200, h: 200 }, prompt: a }
  - { id: b, kind: icon, box: { x: 400, y: 60, w: 200, h: 200 }, prompt: b }
  - { id: c, kind: icon, box: { x: 60, y: 400, w: 200, h: 200 }, prompt: c }
`), 0o644)
	for _, style := range []string{"colors", "blocks", "outlines"} {
		m, code := call(t, "gen", lay, "--backend", "fake", "--sketch", style, "--out", filepath.Join(dir, "out"))
		if code != 0 {
			t.Fatalf("%s: %v", style, m)
		}
		for _, p := range m["pieces"].([]any) {
			if fl, ok := p.(map[string]any)["flags"]; ok {
				t.Errorf("%s: %v flagged %v", style, p.(map[string]any)["id"], fl)
			}
		}
	}
}

// extract on a directory that is not a run is a usage error.
func TestExtractNotARun(t *testing.T) {
	m, code := call(t, "extract", t.TempDir())
	if code != exitUsage || m["error"].(map[string]any)["code"] != "not_a_run" {
		t.Errorf("code %d: %v", code, m)
	}
}
