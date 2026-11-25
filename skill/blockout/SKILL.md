---
name: blockout
description: Make consistent game art with the blockout CLI. You lay out boxes in a YAML file, blockout generates the whole sheet in one Nano Banana call so every piece shares one style, then cuts each box out as a transparent PNG with a manifest an engine can load (nine-slice and content insets, button states, isometric pivots). Use this skill whenever the user wants game assets — UI kits, buttons and their pressed states, panels, frames, icons, block or tile sets, sprite sheets, isometric buildings, a game backdrop or a style reference — or mentions blockout, a layout.yaml, nine-slice art, sprite pivots or getting generated art into a game engine, even if they never name blockout. Not for photos, marketing images or editing an existing picture.
compatibility: Needs the blockout binary (Go; `go install github.com/codezerg/blockout-ai-game-assets/cmd/blockout@latest`, or `go build -o blockout ./cmd/blockout` in a checkout) and OPENROUTER_API_KEY for paid generation. Everything except `gen` without --dry-run/--backend fake runs offline and free.
---

# blockout

blockout turns a layout file into game art. You place boxes on a canvas and
say what goes in each; blockout draws a sketch of the boxes, sends sketch +
style references + one line per piece to Nano Banana in **one** call, then
cuts every drawn piece out of the returned sheet as a transparent PNG and
writes `manifest.json`. One call per sheet is the point: pieces generated
together share a style, which separate calls do not.

The layout file is the source of truth. Prompts, crops and the manifest all
derive from it, so change the layout and regenerate rather than editing
outputs by hand.

## Before you start

- Run `blockout help`. If the command is missing, ask the user where blockout
  lives, or install it (needs Go):
  `go install github.com/codezerg/blockout-ai-game-assets/cmd/blockout@latest`.
- Generation reads `OPENROUTER_API_KEY` from the environment. Never ask the
  user to paste a key into chat; ask them to set it.
- **Money.** A 1K sheet costs about $0.04 and takes 10–20 s on the default
  model (`google/gemini-nano-banana-2.1`); 2K and 4K cost more. Failed
  generations are not billed and transient failures are retried for you.
  Agree a budget with the user before the first paid call, then give every
  paid `gen` a `--tag` and `--budget`: blockout refuses to start once runs
  under that tag have spent the budget (exit 6). When that happens, stop and
  ask the user; don't raise the budget yourself.
- **Free commands:** `sketch`, `gen --dry-run`, `gen --backend fake`,
  `extract`, `runs`, `export`. Use them to check everything before paying.

## The loop

1. **Plan the set with the user.** Which pieces, which engine, what size
   they end up on screen, what art style. Group pieces that must match
   (a button and its pressed state, a block set, one village) onto one
   sheet. When the deciding factor is how something looks, show the user
   candidates and let them choose.
2. **Make a style reference first.** A layout with a `prompt` and no
   `pieces` generates one image: a mock in-game screen showing the look
   (materials, palette, light), ~$0.04 at 16:9 1K. Look at it, then copy the
   chosen run's `sheet.png` into the project (e.g. `art/refs/style_ref.png`)
   and list it under `style_refs` in every sheet's layout. This is what makes
   later sheets belong to the same game; the pieces will look lifted from it.
   `gen`, even with `--dry-run`, refuses a layout whose ref doesn't exist
   yet (`sketch` doesn't check refs). To dry-run a sheet before its ref
   exists, comment that line out for the dry run, then put it back.
3. **Write one layout per sheet** (format: `references/layout.md`). Keep
   layouts in the game project, e.g. `art/<sheet>.yaml`; the run name comes
   from the file path, so name files distinctly (`art/ui.yaml` → runs named
   `art-ui-NNNN`).
4. **Validate for free:** `blockout sketch art/ui.yaml`. Fix every problem
   it reports (they carry line numbers), then open the sketch PNG it wrote
   and check the boxes are where you meant.
5. **Generate:** `blockout gen art/ui.yaml --out art/out --tag ui --budget 0.50`.
   Runs go to `<out>/<name>-NNNN/`; keep `art/out/` out of version control
   but don't delete it, since every run can be re-extracted for free.
6. **Review** (details: `references/results.md`):
   - read the JSON on stdout: every piece's file, size and `flags`, plus
     `warnings`, `scores` and `cost_usd`;
   - **open `preview.png`**: sketch | sheet with boxes (white) and drawn
     bounds | the cut-out pieces on a checkerboard, then a UI strip with
     each panel's slice lines (cyan) and content box (yellow) stretched three
     ways and each button state group overlaid. Isometric sheets also get
     `scene.png`, every sprite stood on a tile grid;
   - act on every flag and warning.
7. **Decide what to change**, cheapest first:
   - fixable in code (size, tint, alignment): do it in the game, and say
     what you did;
   - the layout or prompt is wrong (a piece misread, text drawn, wrong
     orientation): edit the layout and regenerate the sheet;
   - one piece is bad but the sheet is good: blockout can't regenerate a
     single piece yet. Regenerate the sheet, or fix the piece in code;
   - extraction choices (e.g. `--matte hard`): `blockout extract <run-dir>`
     redoes pieces, manifest, scores and preview from the stored sheet.
8. **Export into the game:**
   `blockout export art/out/art-ui-0003 art/out/art-blocks-0002 --to <game>/assets [--js GAME_ART]`.
   It copies each piece as `<id>.png` with one `manifest.json` (files
   relative to the folder) and refuses duplicate ids. `--js` also writes
   `manifest.js` setting `window.GAME_ART`, for HTML games opened from
   `file://`, which cannot fetch JSON.
9. **Use the manifest, not guesses:** `size`, `slice` (what must not
   stretch), `content` (where contents go), `state`, `pivot`, `footprint`,
   `tile` (isometric: scale every sprite by your tile width ÷ `tile.px`),
   `flags`. Engine mapping is in `references/results.md`.

## Layout essentials

Full format and complete examples: `references/layout.md`.

- Coordinates are **pixels of the 1K image at the canvas aspect**: 1:1 is
  1024×1024, 16:9 is 1376×768, 9:16 is 768×1376, 4:3 is 1200×896. The same
  layout works at 1K, 2K and 4K.
- Leave 48–64 units between boxes and around the edge. Boxes may not
  overlap; gaps under 16 draw a warning.
- `id`: lowercase letters, digits, `_`, `-`; it becomes the file name.
- `kind` changes the prompt and post-processing: `button` (plain face, no
  label, gets nine-slice insets), `nine_slice` (stretchable panel), `frame`
  (empty centre, made transparent), `icon`, `sprite`. Other kinds are
  allowed but get no special handling.
- **Button states**: a button whose id is another button's id plus
  `_<state>` (e.g. `btn_play_pressed`) with a box of the **same size** is a
  state of it. blockout aligns it to the base, resizes it if within 5%, and
  pads all states onto one canvas so swapping them doesn't move the art.
- Sprite sheets: `grid: {cols, rows, gutter}` with `cell: [col, row]` per
  piece; isometric sprites add `footprint: [w, d]` in tiles.

## What the model does, and how to write for it

These come from measured experiments; they save paid reruns.

- **Box-shaped assets land on their boxes; free-form ones grow.** Tiles,
  panels and buttons came back within 0–3% of their box. Frames, ornate
  icons and objects come back ~25% larger, growing right and down, and a
  wide panel was sometimes drawn tall. Extraction finds each piece wherever
  it was drawn, so this costs nothing unless pieces collide: give free-form
  pieces generous gutters, and set final sizes in code, not with box size.
- **No text in the art.** Labels belong to the engine. Kind hints already
  say so; don't ask for words on buttons.
- **Never put the key colour in the art.** The background is a flat key
  colour that extraction removes. On the default `#00FF00`, nothing should
  be green; for green art use `#FF00FF`. Black (`#000000`) keys more cleanly
  but eats dark outlines, so use it only for art with light rims.
- **Ask for opaque materials.** Glass, smoke, glow, thin rims and anything
  see-through take on the key colour (a "pale-blue glass rim" came back
  green). Write "opaque", "no outer glow, halo or shadow"; add glows in code.
  Pieces that picked up the key are flagged `key_cast`.
- **The default sketch names boxes by colour** ("in the red box, draw …").
  Box colours are assigned in piece order (on a green key: red, blue,
  yellow, orange, purple, cyan, pink, brown, magenta, white, black, grey),
  so a sheet holds at most about 11 pieces; split bigger sets. Whether a
  piece drawn in a box of another colour picks up that colour is untested.
  If ordering the pieces lets a red piece sit in the red box for free, do
  it, but never change what a piece looks like to fit a box colour.
- **Nine-slice panels:** ask for "plain, uniform edges and centre" and no
  one-off chips or cracks; insets are detected, not prompted.
- **Matching states and variants:** put them side by side on one sheet and
  say "the same button in its pressed state, darker and inset".
- **Isometric:** the model draws a steady but steeper angle (~28° instead
  of 26.57°) and ignores footprint scale (a 1×1 statue comes out as big as a
  2×2 house). blockout measures each base and corrects scale, angle and
  pivot in code. Shrinking boxes to force scale doesn't work; use whole grid
  cells. Round bases, legs and ruins can't be measured; they are flagged and
  need a look.
- Stay on the default model. `google/gemini-3.1-flash-lite-image` is cheaper
  but leaks sketch marks into the art and draws panels as literal 3×3 grids.

## Exit codes

| Code | Meaning | Do |
| --- | --- | --- |
| 0 | ok | read stdout |
| 2 | bad command line | fix the flags (`blockout help`) |
| 3 | layout invalid | fix each `problems` entry (line numbers given) |
| 4 | model call failed | if `error.retryable`, run again; otherwise read the message |
| 5 | no API key | ask the user to set OPENROUTER_API_KEY, or use `--dry-run` / `--backend fake` |
| 6 | tag budget spent | stop and ask the user |
| 1 | internal error | report it with the message |

## Not built yet

- Regenerating one piece of a sheet: regenerate the sheet or fix in code.
- Promoting a run to a style reference: copy its `sheet.png` by hand.
- A plain single-image asset (a backdrop): use a reference layout (prompt,
  no pieces) and take its `sheet.png`; `export` includes it, named after the
  layout file.
- Text, pixel-art grids, atlas packing: do them in code or the engine.
