# Reading blockout results

## Contents

- What `gen` prints
- The run directory
- Flags and what to do
- Scores
- The manifest
- Using it in an engine
- Re-extracting and listing runs

## What `gen` prints

One JSON object on stdout. On success (`"ok": true`):

- `dir`, `run`: the run directory and its number.
- `pieces`: per piece `id`, `file`, `w`, `h` and, if any, `flags`. Act on
  every flag.
- `warnings`: layout warnings plus extraction warnings, in words.
- `scores`: the sheet summary (below).
- `preview`, `sheet`, `sheet_alpha`, `manifest`, `prompt`: paths.
- `cost_usd`, `tag_spent_usd`, `attempts`, `duration_s`.
- `model_text`: anything the model said besides the image (usually empty).

On failure (`"ok": false`): `error.code`, `error.message`, and for layout
errors `error.problems[]` with `line` and `message`; for model failures
`error.retryable`.

A reference layout (no pieces) prints the `sheet` path only: there is
nothing to extract.

## The run directory

`<out>/<name>-NNNN/`, never overwritten:

| File | What |
| --- | --- |
| `layout.yaml` | the layout as it was |
| `sketch.png`, `prompt.txt`, `request.json` | exactly what was sent (images elided with hashes) |
| `response.json`, `run.json` | what came back, status, model, provider, cost |
| `sheet.png` | the image as returned |
| `sheet_alpha.png` | the same with the background removed |
| `pieces/<id>.png` | each piece cut out and trimmed |
| `manifest.json` | the pieces and everything known about them |
| `scores.json` | placement and quality numbers per piece and sheet |
| `preview.png` | the review image; look at it every time |
| `scene.png` | isometric sheets: the sprites stood on a tile grid |
| `refs/` | copies of the style refs used |

## Flags and what to do

| Flag | Meaning | Usually |
| --- | --- | --- |
| `missing` | nothing drawn in or near the box | regenerate; if it repeats, make the prompt plainer or the box bigger |
| `touches_edge` | drawn up to the sheet border; may be cut off | look at the piece; move the box in from the edge |
| `merged:<id>` | drawn as one shape with another piece; both are in the file | widen the gutter and regenerate |
| `intrudes:<id>` | reaches into another box but was cut cleanly | usually fine; check the preview |
| `key_inside` | patches of key colour enclosed in the art | look; often a sketch outline drawn in. Regenerate |
| `key_cast` | at least 3% of the piece is tinted toward the key | the piece took on the background (see-through material, glow). Recolour in code if minor; otherwise ask for opaque materials or switch key colour and regenerate |
| `not_stretchable:x` / `:y` | no plain run across the middle on that axis | don't nine-slice along it; ask for plainer edges, or scale instead |
| `state_mismatch` | a state's silhouette differs from its base's by >10% | regenerate the sheet; states must match to swap cleanly |
| `unmeasured` | isometric: no base found; given the sheet's median scale and a bottom-centre pivot | check its size and pivot in `scene.png` |
| `scale_estimated` | isometric: round base; pivot measured, scale from the median | check its size in `scene.png` |
| `angle_off` | isometric: base lopsided or far from 2:1; code can't fix it | regenerate, or accept |
| `upscaled` | isometric: enlarged by more than 25% to match the tile | may look soft; generate at 2K |

The `scores.json` leak score counts pixels near the sketch box colour. When a
piece is meant to be that colour (a red tile in the red box), a high leak is
expected and not a problem.

## Scores

`scores.summary` (also on stdout as `scores`):

| Field | Meaning |
| --- | --- |
| `found` / `pieces` | pieces drawn at all |
| `inside_margin` | pieces within their box plus a small margin |
| `mean_iou`, `min_iou` | overlap of drawn bounds with boxes (1 = exact) |
| `max_spill` | worst distance drawn past a box edge, layout units |
| `mean_size_ratio` | drawn size ÷ box size (1.25 = 25% too big) |
| `merged`, `intrudes`, `stray`, `touches_edge` | counts |
| `max_leak` | worst sketch-colour leak |
| `bg_noise`, `bg_off_key` | how far the background strays from the key; high values mean a dirtier matte |

Isometric sheets add `iso`: bases measured, drawn angle (mean, sd), drawn
tile ÷ asked (mean, cv), front-corner offset, and `after` (tile and angle
error after correction; within ~1% and ~1° is normal).

Placement scores describe the model, not the output's usability: pieces are
cut from what was drawn, so a low IoU is fine if nothing collided.

## The manifest

`manifest.json` in a run (or the merged one written by `export`):

| Field | Meaning |
| --- | --- |
| `file` | the PNG (relative to the manifest) |
| `size` | the file's pixel size |
| `rect`, `box` | where it was on the sheet, and the layout box |
| `slice` | nine-slice insets in file pixels: what must not stretch |
| `content` | where the plain centre starts: the rim's thickness. Place contents (labels, a game board, text) inside it. It is narrower than `slice` where a one-off mark (a glare) widened the slice |
| `stretch` | share of width/height that stretches; `seam_x/y` > ~8 means shrinking below the drawn size shows a seam |
| `state` | for a state: `of` (base id), `shape_iou`, `resized`, offsets; all states share one canvas size |
| `pivot` | anchor as a share of width and height from the top-left (`{x: 0.5, y: 1}` = bottom centre) |
| `footprint`, `base`, `fix` | isometric: tiles, the measured base, the scale and squash applied |
| `tile` (top level) | isometric: `layout` units and `px` in the piece files |
| `edge_tint`, `key_cast` | key colour left on edges / in the body |
| `flags` | as above |

`export` adds `from` (the run) per piece and `sources` (runs, models,
costs). Each isometric sprite also gets its run's `tile`, since runs at
different sizes have different tiles. Reference runs appear as
`kind: "image"`, named after their layout file.

## Using it in an engine

- **CSS:** lay the border out at `content`, draw the image at `slice`:
  `border-width: content…; border-image: url(p.png) T R B L fill / slice… stretch`
  (slice numbers unitless, widths in px times your scale).
- **Godot:** `NinePatchRect.patch_margin_left/top/right/bottom` = `slice`;
  put children in a `MarginContainer` with margins = `content`.
- **Unity:** sprite border = `slice` in the order (left, bottom, right, top);
  padding for children = `content`.
- **Phaser:** `add.nineslice(x, y, key, frame, w, h, slice.left, slice.right, slice.top, slice.bottom)`.
- **Button states:** swap textures in place; all states share one size and
  alignment.
- **Sprites:** set the origin/offset from `pivot`. Isometric sprites from
  one run all match its `tile.px`, so scale each by (your tile width ÷ its
  `tile.px`); read it per sprite, since a 2K run's tile is twice a 1K
  run's. Phaser:
  `add.image(x, y, p.id).setOrigin(p.pivot.x, p.pivot.y).setScale(TILE_W / p.tile.px)`,
  with (x, y) the front (lowest) corner of the footprint on your grid.
  `footprint: [w, d]`: w tiles run from the back corner toward the lower
  right, d toward the lower left.
- **Sizes:** pieces come out at sheet resolution. Scale down in the engine
  or in a build step; for crisp small icons generate at 2K.

## Re-extracting and listing runs

- `blockout extract <run-dir> [--matte soft|hard]` rewrites everything
  derived from `sheet.png` (pieces, manifest, scores, previews) for free.
  `soft` (default) gives partial alpha and despill at edges.
- `blockout runs --out <dir> [--tag t] [--name n]` lists runs with cost and
  scores, grouped by variant, with `total_cost_usd`.
