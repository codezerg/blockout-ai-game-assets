# blockout

Agent-first CLI: describe sprites as boxes in YAML, get AI-generated transparent PNGs and a manifest.

You place boxes on a canvas and say what goes in each one. blockout draws a sketch of the boxes and sends it to Nano Banana (through OpenRouter) in **one** call, together with your style references and one line per piece. It then cuts every piece out of the returned sheet as a transparent PNG and writes a `manifest.json` that a game engine can load. Because the whole set comes from one generation, every piece shares one style.

It is built for UI kits (buttons with pressed states, nine-slice panels, frames, icons), tile and block sets, isometric sprites and backdrops.

Its primary user is a coding agent such as Claude Code. Every command prints one JSON object on stdout, writes its files to disk and never prompts. Exit codes have fixed meanings.

## Install

Needs Go 1.27+.

```sh
go install github.com/codezerg/blockout-ai-game-assets/cmd/blockout@latest
```

Or from a checkout: `go build -o blockout ./cmd/blockout`.

Paid generation reads your OpenRouter key from `OPENROUTER_API_KEY`. Everything else works offline and costs nothing.

## Quick start (free)

From a checkout of this repo:

```sh
blockout sketch examples/ui-kit/layout.yaml                 # validate and draw the sketch
blockout gen    examples/ui-kit/layout.yaml --backend fake  # full pipeline, fake model
blockout gen    examples/ui-kit/layout.yaml --dry-run       # write the exact request, no call
```

The fake backend turns the sketch into shaded shapes, so you can check extraction, the manifest and the previews without paying. When you're ready for a real sheet:

```sh
blockout gen examples/ui-kit/layout.yaml --tag first --budget 0.50
```

A 1K sheet costs about $0.04 and takes 10–20 s on the default model (`google/gemini-nano-banana-2.1`). 2K and 4K cost more. `--budget` refuses to start a run once runs under that `--tag` have spent the budget. Failed generations are not billed, and transient OpenRouter errors are retried.

## A layout

This is most of [examples/ui-kit/layout.yaml](examples/ui-kit/layout.yaml):

```yaml
# Coordinates are pixels of the 1K canvas (1024x1024 at 1:1);
# they scale to whatever size is generated.
canvas: { aspect: "1:1", size: 1K, background: "#00FF00" }  # key colour, removed on extraction
style: fantasy RPG UI, carved wood and gold trim, soft painted shading, no text

pieces:
  - id: btn_primary
    kind: button
    box: { x: 60, y: 60, w: 380, h: 120 }
    prompt: wide primary button, gold trim
  - id: btn_primary_pressed            # <id>_pressed is a state of <id>
    kind: button
    box: { x: 500, y: 60, w: 380, h: 120 }
    prompt: the same button in its pressed state, darker and inset
  - id: panel
    kind: nine_slice
    box: { x: 380, y: 260, w: 560, h: 420 }
    prompt: parchment dialog panel, ornate corners, plain edges that can stretch
  - id: icon_settings
    kind: icon
    box: { x: 60, y: 760, w: 160, h: 160 }
    prompt: gear icon
```

Add `style_refs: [path/to/image.png]` to copy the look of existing art. References set the style only, never the layout. Unknown fields are errors, and every problem is reported with its line number. A layout with a `prompt` and no `pieces` makes a single image, such as a style reference or a backdrop. Sprite sheets can use `grid` and `cell` instead of explicit boxes, and isometric sprites add `footprint`, `pivot` and `iso`. The full format is in [skill/blockout/references/layout.md](skill/blockout/references/layout.md).

## Commands

```
blockout sketch <layout.yaml>                     validate and draw the sketch (free)
blockout gen <layout.yaml> [--size 1K|2K|4K]      generate into out/<name>-NNNN
          [--tag t --budget usd] [--dry-run] [--backend fake]
blockout extract <run-dir> [--matte soft|hard]    re-derive everything from a stored sheet (free)
blockout runs [--tag t] [--name n]                list runs with cost and scores
blockout export <run-dir>... --to <dir> [--js NAME]
                                                  copy pieces of several runs + one manifest (free)
```

`blockout help` gives the full usage. Exit codes: 0 ok, 1 internal, 2 usage, 3 layout invalid, 4 model call failed (`error.retryable` says whether to retry), 5 missing API key, 6 budget reached.

## What a run writes

Each `gen` creates a run directory holding:

- `sheet.png`: the image the model returned, and `sheet_alpha.png`, the same with the key colour matted out.
- `pieces/<id>.png`: each piece, cut from the pixels it owns and trimmed. A piece file never contains part of a neighbour.
- `manifest.json`: per piece, its file, rect and size, plus:
  - nine-slice `slice` insets (what must not stretch) and `content` insets (where an engine puts contents) for `nine_slice` and `button` pieces;
  - button states aligned on one shared canvas;
  - for isometric sprites, the scale correction and a pivot on the footprint's front corner.
- `scores.json`: per-piece placement scores and flags such as `missing`, `touches_edge`, `merged:<id>` and `key_cast`.
- `preview.png`: the sketch, the sheet with boxes and drawn bounds, and the pieces on a checkerboard. Isometric sheets also get `scene.png`.
- `layout.yaml`, `sketch.png`, `prompt.txt`, `request.json`, `response.json` and `run.json` (model, cost, attempts), so every run can be reproduced and re-extracted.

`blockout export` gathers the pieces of several runs into one asset folder with one manifest. `--js NAME` also writes `manifest.js` for pages opened from `file://`.

## Using it from Claude Code

[skill/blockout/](skill/blockout/) is a Claude Code skill that teaches an agent the whole workflow: how to plan a set, check it for free, agree a budget before paying, review the flags and get the pieces into an engine. To install it, copy the folder to `~/.claude/skills/blockout` (or to `.claude/skills/blockout` in a project).

## In this repo

- `cmd/blockout`, `internal/`: the CLI and one package per pipeline stage (layout, sketch, prompt, backend, segment, measure, matte, extract, ui, iso, preview).
- [EXPERIMENTS.md](EXPERIMENTS.md): the lab notebook. It explains why each default exists, with scores and decisions. The generated sheets it cites are not in the repo.
- `examples/`: a layout to start from.

```sh
go test ./...
```

## License

[MIT](LICENSE)
