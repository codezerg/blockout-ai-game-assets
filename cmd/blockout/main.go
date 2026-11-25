// Command blockout turns a layout of boxes into a generated game-asset sheet,
// one file per piece, and a manifest. It is driven by an agent: JSON on
// stdout, files on disk, no prompts.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"

	_ "image/jpeg"

	_ "golang.org/x/image/webp"

	"github.com/codezerg/blockout-ai-game-assets/internal/extract"
	"github.com/codezerg/blockout-ai-game-assets/internal/layout"
)

const defaultModel = "google/gemini-nano-banana-2.1"

// Exit codes.
const (
	exitOK       = 0
	exitInternal = 1 // a bug or a filesystem failure
	exitUsage    = 2 // bad command line
	exitLayout   = 3 // the layout file needs changing
	exitModel    = 4 // the model call failed; see error.retryable
	exitConfig   = 5 // environment: missing API key
	exitBudget   = 6 // the tag's budget is spent; ask the user before raising it
)

const help = `blockout: layout to game-asset sheet

Commands (all but help print one JSON object on stdout):
  blockout sketch <layout.yaml> [--sketch colors|blocks|outlines|none] [--out file.png]
      Validate the layout and draw its sketch. Free.
  blockout gen <layout.yaml> [--size 1K|2K|4K|0.5K] [--model id] [--sketch style]
               [--tag name --budget usd] [--backend openrouter|fake] [--out dir] [--dry-run]
      Generate into a new run directory <out>/<name>-NNNN. A layout with pieces
      makes a sheet, then extracts, scores and previews it; a layout with only a
      prompt makes a single reference image. --budget refuses to start once runs
      tagged --tag have spent that much. --dry-run and --backend fake are free.
  blockout extract <run-dir> [--matte soft|hard]
      Redo sheet_alpha.png, pieces, manifest.json, scores.json and preview.png
      (and scene.png for isometric sheets) from a stored sheet. Free.
  blockout runs [--out dir] [--tag name] [--name name]
      List runs with cost and scores, grouped by tag, layout, model, size and sketch.
  blockout export <run-dir>... --to <dir> [--js name]
      Copy the pieces of one or more runs into <dir> as <id>.png, with one
      manifest.json for them all (files relative to <dir>). A reference run
      adds its whole image, named after its layout file. --js also writes
      manifest.js setting window.<name>, for pages opened from file://. Free.

Exit codes: 0 ok, 1 internal, 2 usage, 3 layout invalid, 4 model call failed,
5 missing API key, 6 budget reached. The OpenRouter key is read from
OPENROUTER_API_KEY. 0.5K works only on some models.
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout))
}

func run(args []string, out io.Writer) int {
	c := &cli{out: out}
	if len(args) == 0 {
		fmt.Fprint(out, help)
		return exitUsage
	}
	switch args[0] {
	case "sketch":
		return c.sketch(args[1:])
	case "gen":
		return c.gen(args[1:])
	case "extract":
		return c.extract(args[1:])
	case "runs":
		return c.runs(args[1:])
	case "export":
		return c.export(args[1:])
	case "help", "-h", "--help":
		fmt.Fprint(out, help)
		return exitOK
	}
	return c.fail(exitUsage, failure{Code: "usage", Message: fmt.Sprintf("unknown command %q; use sketch, gen, extract, runs or export", args[0])})
}

type cli struct{ out io.Writer }

type failure struct {
	Code      string           `json:"code"`
	Message   string           `json:"message"`
	Problems  []layout.Problem `json:"problems,omitempty"`
	Retryable *bool            `json:"retryable,omitempty"`
	Run       string           `json:"run,omitempty"`
	Dir       string           `json:"dir,omitempty"`
}

func (c *cli) emit(v any) {
	enc := json.NewEncoder(c.out)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

func (c *cli) fail(code int, f failure) int {
	c.emit(struct {
		OK    bool    `json:"ok"`
		Error failure `json:"error"`
	}{false, f})
	return code
}

func (c *cli) internal(err error) int {
	return c.fail(exitInternal, failure{Code: "internal", Message: err.Error()})
}

func (c *cli) layoutFail(err error) int {
	var le *layout.Error
	if errors.As(err, &le) {
		return c.fail(exitLayout, failure{Code: "layout_invalid", Message: "fix these in " + filepath.ToSlash(le.File), Problems: le.Problems})
	}
	return c.internal(err)
}

// parse parses flags that may come before or after positional arguments.
func parse(fs *flag.FlagSet, args []string) ([]string, error) {
	fs.SetOutput(io.Discard)
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		if fs.NArg() == 0 {
			return pos, nil
		}
		pos = append(pos, fs.Arg(0))
		args = fs.Args()[1:]
	}
}

func (c *cli) usage(err error) int {
	return c.fail(exitUsage, failure{Code: "usage", Message: err.Error() + "; run blockout help"})
}

// --- helpers ---

var runDirPattern = regexp.MustCompile(`^(.+)-(\d{4,})$`)

// newRunDir creates <root>/<name>-NNNN with the next free number for name.
func newRunDir(root, name string) (string, string, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", "", err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", "", err
	}
	next := 1
	for _, e := range entries {
		if m := runDirPattern.FindStringSubmatch(e.Name()); m != nil && m[1] == name {
			if n, _ := strconv.Atoi(m[2]); n >= next {
				next = n + 1
			}
		}
	}
	for ; ; next++ {
		id := fmt.Sprintf("%04d", next)
		dir := filepath.Join(root, name+"-"+id)
		err := os.Mkdir(dir, 0o755)
		if err == nil {
			return dir, id, nil
		}
		if !os.IsExist(err) {
			return "", "", err
		}
	}
}

// pieceFiles is the per-piece summary printed on stdout: where each file
// is, its trimmed size, and any flags (missing pieces have no file).
func pieceFiles(dir string, ps []extract.Piece) []map[string]any {
	out := make([]map[string]any, len(ps))
	for i, p := range ps {
		m := map[string]any{"id": p.ID}
		if p.File != "" {
			m["file"] = filepath.ToSlash(filepath.Join(dir, p.File))
			m["w"], m["h"] = p.Rect.W, p.Rect.H
			if p.Size != nil {
				m["w"], m["h"] = p.Size.W, p.Size.H
			}
		}
		if len(p.Flags) > 0 {
			m["flags"] = p.Flags
		}
		out[i] = m
	}
	return out
}

func problemStrings(ps []layout.Problem) []string {
	out := []string{}
	for _, p := range ps {
		s := p.Message
		if p.Line > 0 {
			s = fmt.Sprintf("line %d: %s", p.Line, s)
		}
		out = append(out, s)
	}
	return out
}

func savePNG(path string, img image.Image) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

func copyInto(dir, name string, data []byte) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, name), data, 0o644)
}

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func saveIfAny(path string, data []byte) {
	if len(data) > 0 {
		_ = os.WriteFile(path, data, 0o644)
	}
}

func round(v float64, places int) float64 {
	p := 1.0
	for range places {
		p *= 10
	}
	return float64(int(v*p+0.5)) / p
}
