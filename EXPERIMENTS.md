# Experiments

The lab notebook. One entry per question: the question, the variants, the run
ids, the scores, the decision. A default changes only when an entry backs it.
Run ids name run directories (`<name>-NNNN`), each holding the run's
layout, sketch, prompt, request, response, sheet and cost.

The runs, contact sheets, test cases (`testdata/`), the E9 game
(`games/glassfall/`) and the skill's test tasks (`skill/blockout/evals/`) are
kept locally and are not part of this repository. The entries record what they
showed.

## E1. Does the smallest loop work end to end?

Phase 1, 2026-11-25. Case `testdata/cases/ui-kit-wood`. Model
`google/gemini-nano-banana-2.1`, 1K. Budget $0.50; spent $0.136 (three images
plus about 30 one-cent-or-less text probes).

**Setup.** The tool validates the layout, draws the sketch (filled colour block
plus a small number per piece, on the key colour), compiles one interleaved
prompt (intro, sketch, each style ref introduced by name, then one line per
piece with its position in words), makes one Chat Completions call, saves the
sheet, crops each box plus a 16-unit margin, and writes `manifest.json`.

The style reference is generated rather than supplied: a layout with a
`prompt` and no `pieces` makes one art-direction image (`style_ref.yaml`), which
is copied to the case's `refs/` and used by every sheet of that game.

| Run | What | Result | Cost |
| --- | --- | --- | --- |
| ui-kit-wood-style_ref-0001, -0002 | style ref | 400 "Request path cannot be relayed" | $0 |
| ui-kit-wood-style_ref-0003 | style ref | ok, 1376×768, 9.7 s | $0.0367 |
| ui-kit-wood-0001, -0003 | UI kit sheet | 400 relay error (-0003 on all 5 attempts) | $0 |
| ui-kit-wood-0002 | UI kit sheet | ok, 1024×1024, 12 s | $0.0430 |
| ui-kit-wood-0004 | UI kit sheet, same layout | ok, 1024×1024, 19.5 s | $0.0478 |

**Findings.**

1. *The API path works.* `modalities: ["image","text"]` and `image_config`
   are honoured, the image arrives in `choices[0].message.images[0]` as a PNG
   data URL, and `usage.cost` is present. Returned sizes match the layout's
   1K table exactly (1376×768 at 16:9, 1024×1024 at 1:1). About $0.04 and
   10 to 20 s per 1K image with one or two inputs.
2. *OpenRouter fails a large share of requests with a 400 that is really
   transient.* "Request path cannot be relayed" came back on 25 to 75% of
   requests, for every Gemini image model tried (Nano Banana 2.1, 3 Pro,
   3.1 Flash), on both providers (AI Studio and Vertex), for bodies from
   27 KB to 3.4 MB, and for text-only output too. The identical request
   then succeeds, and failures are not billed: key usage rose by the sum of
   the successful calls only. I first read it as a broken Vertex endpoint,
   because pinning AI Studio happened to succeed; repeated probes disproved
   that, and the pin was removed.

   *Cause, added 2026-11-25 (from the user):* the error is a DNS issue at
   the edge. That is why it was the same across models, providers and body
   sizes, and why E2 saw none. Nothing in the request can fix it; retrying
   is the right response.
3. *A generated style ref anchors the set well.* Both sheets reuse the ref's
   wood, gold trim, parchment and rivets closely. The pieces look lifted from
   the ref rather than designed to match it, which is good for consistency but
   needs checking when a ref contains the same element types as the sheet
   (the "style from references" experiment).
4. *No sketch leakage* in either sheet: no block colours, outlines or numbers;
   the background is flat key green, including the frame's centre; no text.
5. *Placement drifts, in one direction.* Drawn bounds are connected regions of
   non-key pixels, compared with the layout box (throwaway script, to become a
   phase-2 score). Spill is how far the drawing passes each box edge, in 1K px.

   | Piece | Run 0002 spill L/T/R/B | IoU | Run 0004 spill L/T/R/B | IoU |
   | --- | --- | --- | --- | --- |
   | btn_primary | 2 / 0 / 20 / 10 | 0.87 | 10 / 0 / 34 / 26 | 0.74 |
   | btn_primary_pressed | 0 / 0 / 10 / 10 | 0.90 | 0 / 0 / 34 / 26 | 0.75 |
   | avatar_frame | 42 / 0 / 30 / 70 | 0.62 | 40 / −46 / 28 / 118 | 0.46 |
   | panel | 10 / 6 / 70 / 330 | 0.49 | 4 / 2 / 60 / 6 | 0.88 |
   | icon_settings | 2 / 0 / 0 / 2 | 0.98 | 0 / 0 / 2 / 2 | 0.98 |

   Pieces start at their box's top-left corner and overgrow right and down,
   coming out 5 to 27% larger. In run 0002 the 4:3 panel was drawn tall,
   330 px past its box. The small square icon is placed almost exactly.
6. *Box crops are not enough, but the sheets are easy to extract.* The 16-unit
   margin cut the panel and the frame in both runs. Yet every piece was a
   single connected region on clean key colour, touching nothing, so
   extracting connected regions and assigning each to the box it overlaps most
   would have cut every piece whole in both runs.

**Decisions.**

- Chat Completions with default provider routing. The 400 relay error is
  retried like a 502, up to 8 attempts with short backoff; other 400s fail
  at once.
- Layout coordinates are pixels of the 1K image at the canvas aspect;
  extraction scales by the size actually returned.
- Style refs come from Nano Banana: a reference layout (prompt, no pieces),
  then copy the chosen run's `sheet.png` into the case's `refs/`.

**For phase 2.**

- Turn finding 5's measurement into a tool score (spill per side, IoU, pieces
  found per box), add a key-colour leak check and `preview.png`.
- Sketch-style experiment as planned (filled blocks, outlines only, text only),
  3 runs each. Overgrowth suggests also trying "draw slightly smaller than the
  block" and stating each block's size in pixels.
- Try connected-region extraction on the stored sheets (free).
- Refs are sent as stored (the ref PNG is 2.5 MB). Size did not cause the 400s,
  but 14 refs would approach the 413 limit; downscale or re-encode when needed.

## Tools added in phase 2

`gen` and `extract` now write `scores.json` (per piece: drawn bounds, spill
past each box edge, IoU, size ratio, whether the crop cuts it, sketch-colour
leak; per sheet: background noise and off-key share) and `preview.png`
(sketch | sheet with boxes in white and drawn bounds in cyan or red | crops).
`blockout runs` groups runs by tag, layout, model, size and sketch style, and
`gen --tag t --budget usd` refuses to start once runs tagged `t` have cost
`usd`. Scores reproduce E1's hand measurements exactly.

## E2. Which sketch style keeps pieces in their boxes?

Phase 2, 2026-11-25. Case `ui-kit-wood`, 1K, style ref as in E1. Tag `e2`,
budget $1.00, spent $0.755. Variants: sketch `blocks` (filled colour block
plus number), `outlines` (numbered outline), `none` (no image, positions in
words only), on Nano Banana 2.1 and 2 Lite (`google/gemini-3.1-flash-lite-image`),
3 runs each, one of each variant per round. Runs ui-kit-wood-0005 to -0022.
All 18 succeeded on the first attempt; no relay errors this time.

| Model | Sketch | Inside box+margin | Cut by crop | Mean IoU | Min IoU | Max spill | Size ratio | Max leak | Bg noise |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Nano Banana 2.1 | blocks | 13% | 87% | 0.63 | 0.36 | 296 | 1.26 | 0 | 11 |
| Nano Banana 2.1 | outlines | 7% | 93% | 0.70 | 0.27 | 298 | 1.25 | 0 | 11 |
| Nano Banana 2.1 | none | 0% | 100% | 0.48 | 0.28 | 316 | 1.36 | 0 | 11 |
| 2 Lite | blocks | 0% | 100% | 0.60 | 0.40 | 120 | 1.22 | 0.64 | 26–29 |
| 2 Lite | outlines | 47% | 53% | 0.83 | 0.46 | 112 | 1.07 | 0.01 | 27 |
| 2 Lite | none | 0% | 100% | 0.51 | 0.42 | 284 | 1.31 | 0 | 34–47 |

Contact sheet: `e2_contact.png` (rows: Lite blocks, none, outlines; 2.1
blocks, none, outlines).

**Findings.**

1. *Nano Banana 2.1 draws everything about 25% too large, whatever the
   sketch.* Pieces start near their box's top-left corner and grow right and
   down. No variant got more than 13% of pieces inside box plus margin, so
   gutters cannot absorb this: it is a bias, not noise.
2. *It also ignores box shape for the panel.* The 4:3 panel came out portrait
   in 7 of 14 sheets across all styles (E1, E2, E3).
3. *Positions in words alone are worst* for both models, and 2 Lite then
   wrote the coordinates into the image ("6–43%, y 6–18%").
4. *2 Lite follows outlines best but leaks.* One blocks run kept the block
   colours behind the pieces (leak 0.64); it drew the nine-slice panel as a
   literal 3×3 grid in most runs; its background is far from the key colour
   (11–100% of background pixels more than 30 off). Not a usable default,
   and not a stand-in for 2.1 either, since it behaves differently.
5. *Nano Banana 2.1 never leaked*: no block colours, outlines, numbers or
   text in any sheet, and its background stays within about 11 of the key.
6. *Pieces never merged and were never missed.* All pieces were found in
   all 23 sheets of E1 to E3, each as its own region with clear background
   around it. Drift moves and grows pieces but does not make them collide on
   this layout.

## E3. Does addressing boxes by colour ("in the red box, draw ...") help?

Phase 2, 2026-11-25, from the user's experience that Nano Banana does not
follow numbers and should be told what to draw in each filled colour box.
New sketch style `colors`: each box filled with a vivid, plainly named
colour, no outlines or numbers; the prompt says "In the red box, draw ..."
per piece, with no numbers or coordinates. Nano Banana 2.1, 3 runs, tag `e3`,
spent $0.125. Runs ui-kit-wood-0023 to -0025; contact sheet `e3_contact.png`.

| Sketch | Inside | Cut | Mean IoU | Min IoU | Max spill | Size ratio | Leak | Bg noise |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| colors | 13% | 87% | 0.64 | 0.40 | 306 | 1.24 | 0 | 11 |
| blocks (E2) | 13% | 87% | 0.63 | 0.36 | 296 | 1.26 | 0 | 11 |

**Findings.** Placement is the same as numbered blocks; the panel came out
portrait in all 3 runs. Output is clean: no box colour left behind, no text.
The prompt is shorter and cannot leak numbers or coordinates.

**Decision.** `colors` becomes the default sketch style: equal placement on
the default model, a simpler prompt, and nothing to leak. It names at most
11 boxes against a green key (12 against magenta); larger sheets must use
`sketch: blocks` or be split, and `gen` says so.

## Where placement stands after phase 2

The starting hypothesis (sketch plus words keeps pieces in their boxes;
gutters absorb drift) is not settled by any prompt or sketch variant tried,
and $0.88 of the $1.00 phase budget is spent. What the data does support:

- Extraction should find each drawn piece as a connected region and assign
  it to the box it overlaps most, rather than crop the box. On E1 to E3 that
  would have cut every piece whole (finding 6). This is phase 3 work and
  free to test on the 23 stored sheets.
- Size and shape are then decided after generation, in code, which suits the
  "code does what code can" principle for UI kits. It does not suit sprite
  sheets, where relative scale between cells matters; that needs its own
  experiment in phase 5.
- Untested ideas for placement itself: draw sketch boxes smaller to offset
  the 25% overgrowth (`gen --sketch-scale 0.8`, implemented, no paid runs
  yet); layouts that leave no large empty areas for pieces to grow into.

## E4. Does a black background change placement?

Phase 2, 2026-11-25, at the user's request. `layout_black.yaml` is
`layout.yaml` with background `#000000`; sketch `colors`, Nano Banana 2.1.
Two runs (the phase budget allowed no more), tag `e4`, spent $0.088. Runs
ui-kit-wood-layout_black-0001 and -0002; contact sheet `e4_contact.png`.
On dark keys the scorer counts a pixel as drawn beyond distance 40 instead of
90, since dark outlines come close to black.

| Run | Inside | Mean IoU | Min IoU | Max spill | Size ratio | Panel | Bg noise | Off key |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 0001 | 1/5 | 0.83 | 0.67 | 75 | 1.08 | wide, as asked | 1.5 | 1.4% |
| 0002 | 0/5 | 0.51 | 0.32 | 298 | 1.39 | portrait | 2.1 | 2.4% |
| green, E3 (3 runs) | 1/15 | 0.64 | 0.40 | 306 | 1.24 | portrait 3/3 | 11 | 2–4% |

**Findings.** Run 0001 is the best Nano Banana 2.1 placement so far, run 0002
fails the usual way, so two runs cannot say whether black helps placement.
The background itself is much flatter on black (mean distance from the key
1.5–2 against 11 on green). Keying black is the catch: the wood UI has dark
outlines, which research says cannot be separated from a black key (below).
Black stays an option for the black/white difference matte in phase 3, which
can now be tested for free on these two sheets.

## Research notes (2026-11-25)

- No source reports box-exact placement from Nano Banana. A developer
  generating game sprites with Nano Banana Pro found it "does not space
  sprites in a perfectly uniform grid, even when you ask for one" and did not
  keep requested sizes; they cut sprites by finding connected regions, then
  trimmed and resized. This matches E2 finding 6 and the phase-3 plan.
- The same developer found black (and magenta with brightness thresholds)
  failed for keying sprites with dark outlines. What worked: pure `#00FF00`,
  a 2–3 px white outline around every sprite as a buffer, HSV keying, banning
  gradients, noise, shadows and texture, "no green anywhere in the sprites",
  and repeating the background rules in every prompt.
- Google's prompting guide says sketches can "define exactly where the text
  and object should sit" and uses wording such as "strictly adhere to the
  button placement and grid structure"; grid images can force tile layouts.
  Its other tips post shows the opposite use ("don't exactly follow the
  sketch, get inspired from it"). Nothing on referring to colour-coded
  regions; for reference images, "clearly define the role of each".
- Google's API docs: no transparent backgrounds; they suggest white.

Untested ideas from this: a white outline around each asset; "strictly
adhere to the layout" wording; more black-background runs.

Sources: [roboticape: sprites with Nano Banana Pro](https://roboticape.com/2026/03/07/generating-game-sprites-with-gemini-image-generation-nano-banana-pro-lessons-learned/),
[Google AI: Nano Banana Pro prompting guide](https://dev.to/googleai/nano-banana-pro-prompting-guide-strategies-1h9n),
[Google Cloud: ultimate prompting guide](https://cloud.google.com/blog/products/ai-machine-learning/ultimate-prompting-guide-for-nano-banana),
[Google: 7 tips for Nano Banana Pro](https://blog.google/products/gemini/prompting-tips-nano-banana-pro/).

Phase 2 total: $0.968 of the $1.00 budget.

## Tools added in phase 3

- `internal/segment`: one segmentation shared by scoring and extraction.
  Pixels far from the key are drawn; drawn pixels form 8-connected regions;
  each region big enough to count goes to the box holding most of its
  pixels, and each small one to the piece whose bounds it sits in.
- `internal/matte`: background removal into `sheet_alpha.png`. Near-key
  areas reaching the sheet's border are background. Enclosed near-key areas
  are holes (made transparent) only if big and thick, so that they hold a
  16-unit square; anything thinner is a groove or shadow and stays opaque.
  `--matte soft` (default) also works out partial alpha in a 2-unit band
  along every edge, unmixes the key from those pixels and despills them;
  `--matte hard` is the binary baseline.
- `internal/extract` now cuts each piece from the pixels it owns, not a
  box, and trims it to what is visible, with transparency. Its file can never
  hold a neighbour, even when their bounds overlap. Manifest flags per piece:
  `missing`, `touches_edge` (may be cut off), `merged:<id>` (drawn as one
  shape with another piece), `intrudes:<id>` (reaches into another box
  but is a separate shape, so it is cut cleanly), `key_inside` (patches of a
  coloured key enclosed in the art); plus `holes` and `edge_tint`, the green
  left on edge pixels on a 0–255 scale.
- The preview's third panel shows the cut-out pieces over a checkerboard;
  stdout lists each piece's size and flags.

## E5. Cut-out pieces and background removal

Phase 3, 2026-11-25, free: re-extraction of every stored sheet, 25 sheets
(23 green, 2 black) and 125 pieces, about 0.5 s a sheet. Both mattes are
kept in `e5/{hard,soft}/<run>/`; contact sheet `e5_contact.png`
(pieces over magenta); edge zooms `e5_zoom_button.png` and
`e5_zoom_frame.png` (hard left, soft right, on dark, light and magenta).

| | Green, hard | Green, soft | Black, soft |
| --- | --- | --- | --- |
| Pieces cut out | 115/115 | 115/115 | 10/10 |
| Edge tint, mean / median / p90 / max | 77.7 / 79.5 / 100.5 / 114.6 | 3.0 / 0 / 10.2 / 46.2 | 0 (no hue to spill) |
| Frame centre transparent | 23/23 | 23/23 | 2/2 |
| Shadows punched out | 0 | 0 | 0 (4/10 before the thickness rule) |

Flags across all 125 pieces: 18 `key_inside`, all on 2 Lite; 1
`key_inside` on 2.1 (0002 panel); 1 `intrudes` (0024 frame into the
panel's box); 1 `touches_edge` (0013 icon, drawn against the sheet's
edge); 0 `merged`.

**Findings.**

1. Cutting pieces out by connected region works on every stored sheet: all
   125 pieces came out whole, and none merged. Overgrowth stops mattering for
   extraction; it only matters for size, which is now measured.
2. A hard key leaves a bright green rim on every green piece (edge tint
   about 80). The soft matte removes it (median 0). The first two soft
   versions failed in ways the numbers did not show and the zooms did. One
   despilled along the key's hue, which turns green into magenta. One unmixed
   pixels with little coverage, which amplifies noise into a faint violet
   halo. The fix:
   - classic despill: green capped at max(red, blue), which never adds a
     hue;
   - edge colour estimated from the nearest solid pixels only;
   - a 30% coverage floor on background-side pixels, because Nano Banana
     darkens the green slightly next to the art.
3. Black needed one rule and then worked on both sheets. Without it, the
   pressed button's inner groove and the panel's shadow strip were keyed
   out as holes on both black sheets: the model draws those shadows as dark
   as the background. Requiring holes to be thick fixes this and changes
   nothing on green. Black's background is 5–7 times flatter (E4) and needs
   no despill.
4. Black is still riskier for art with dark outer outlines (sprites). An
   outline within distance 40 of black that touches the background is keyed
   away as background, and no rule can recover it. The wood UI has gold rims
   outside, so these two sheets do not test that. Default stays `#00FF00`
   with the soft matte; black is fine for UI kits like this one.
5. 2 Lite with outline sketches draws the sketch's outlines into the art and
   traps green between them and the piece. Extraction keeps what was drawn
   and flags it (`key_inside` on 18 pieces). The colour-box sketch does not
   do this.
6. Not tested: the black/white difference matte. It needs the same sheet
   drawn twice with only the background changed, which is a paid edit call
   and Nano Banana does not redraw identically. Since a single green or
   black key now gives clean edges, it is low priority.

## Tools added in phase 4

- `internal/ui`:
  - **Nine-slice insets.** For every boundary between neighbouring columns
    (rows), the mean difference is taken within each of 16 bands across the
    piece, and the worst band counts, so a small crest on one edge is not
    averaged away. A boundary is flat at no more than 2.5× the piece's
    median (floor 1.5). The stretch region is the flat run through the
    centre, less 2 px each side, and must cover at least 10% of the piece.
  - `Stretch` draws a piece nine-sliced to any size.
  - **Button states.** A button whose id is another button's id plus
    `_<state>`, with a box of the same size, is a state of it (for example
    `btn_primary_pressed`). No new layout field. Each state is aligned to
    its base by best silhouette overlap. If it was drawn within 5% of the
    base's size it is first resized to that exact size. Then every member of
    the group is padded onto one shared canvas.
- Manifest, per piece:
  - `size`: the file's dimensions.
  - `slice`: insets in file pixels, for `nine_slice` and `button` pieces,
    since engines stretch buttons to fit labels. A state group shares the
    widest insets.
  - `stretch {x, y, seam_x, seam_y}`.
  - `state {of, shape_iou, dw, dh, color_diff, dx, dy, resized}`.
  - Flags `not_stretchable:x|y` and `state_mismatch` (shape overlap as
    drawn below 90%).
- The preview gains a UI strip:
  - every panel with its insets drawn on it, stretched 2× wide, 1.5× tall
    and to half size;
  - every button state group aligned, with a silhouette overlay (white:
    both opaque; red: only the base; cyan: only the state) and the base
    stretched 2× wide.

## E6. Nine-slice panels and button states

Phase 4, 2026-11-25, free: all 25 stored sheets. Images:
- `e6_stretch.png`: every 2.1 panel stretched 2× wide and 1.5× tall,
  plus button and pressed stretched 2× wide.
- `e6_ui_checks.png`: the UI strips of 0024, 0018 and black-0001.

| | Nano Banana 2.1 (16 sheets) | 2 Lite (9 sheets) |
| --- | --- | --- |
| Panel stretches through its centre | 16/16 | 6/9 |
| Share of panel width that stretches (min / median / max) | 0.50 / 0.61 / 0.67 | 0.00 / 0.30 / 0.63 |
| Share of panel height that stretches | 0.30 / 0.57 / 0.61 | 0.21 / 0.28 / 0.54 |
| Share of button width that stretches | 0.39 / 0.48 / 0.57 | 0.01 / 0.36 / 0.48 |
| Panel insets, px (panels about 600 px wide) | 76–169, one top of 354 | 48–394 |
| Pressed vs normal shape overlap as drawn (min / median) | 0.953 / 0.992 | 0.897 / 0.989 |
| Pressed size as drawn, worst | 16 px wider or narrower, height within 2 px | 44 px |
| Pairs exact after resizing (≤5%) / already exact / too far | 12 / 4 / 0 | 5 / 2 / 2 |
| Shrink seam on panels, 0–255 (x, y) | 8.4–13.9, 4.5–11 | – |

**Findings.**

1. Insets found from flat runs work on every 2.1 panel. Stretched 2× wide
   and 1.5× tall, no corner ornament is squashed and no seam shows. The
   doc's settled criterion ("no seams or squashed ornaments when
   stretched") is met for growing panels. The insets, about 80–170 px on a
   600 px panel, are wider than the doc's guess of 64. Nano Banana puts
   flourishes inside the parchment's corners as well as on the frame.
2. Strict beats tolerant. I tried letting thin, weak bumps through (at most
   2 px wide and 1.5× the threshold), to win back range lost to faint
   parchment creases. It changed half the pieces, and it let the pressed
   button's chip and the ends of the corner swirls into the stretch region.
   The strict rule stays. Its cost is an occasional tall fixed part (0025's
   top inset of 354 px), which only limits how small the panel can go.
3. Shrinking is not clean. Once a panel is smaller than drawn, its fixed
   parts meet, and the parchment's tone drifts slowly across its length, so
   they do not match. That is the shrink seam: visible at about 8 on smooth
   parchment (0018, black-0001), and most 2.1 panels measure 8–14. Use
   panels at their drawn size or larger; for a smaller panel, scale the
   texture first, then nine-slice it.
4. Nano Banana adds one-off marks that limit stretching. The pressed button
   has a small chip in its inner border at about two-thirds of its width,
   on two sheets. A state group shares insets so both states stretch alike,
   so the chip costs the normal button stretch range too (0.55 down to 0.44
   in 0024). Untested prompt idea: edges with no chips, cracks or one-off
   marks.
5. Button states drawn side by side in one sheet match closely on 2.1. The
   median shape overlap is 99%, the worst 95%, and heights are within 2 px.
   Widths are off by up to 16 px (3.7%), which would make the button's
   edges jump when pressed, so code now resizes those to the exact size.
   The colour difference (10–35) is the pressed look, a darker inset face,
   which is what was asked. Whether each pressed state reads as pressed is a
   judgement for the user (see the UI strips). 2 Lite drew two pairs 28 and
   44 px apart; one is flagged `state_mismatch`.
6. 2 Lite's panels drawn as literal 3×3 grids are caught: 3 are not
   stretchable, and the others stretch only 20–31%, between their grid
   lines.

Not tested (paid): states in one sheet against separate calls; hover and
disabled states; the "no one-off marks" prompt.

## Tools added in phase 5

- Layout:
  - `grid {cols, rows, gutter, margin}`, `cell: [col, row]` instead of a
    box, and `cell_defaults {kind, footprint, pivot}`.
  - `footprint: [w, d]` in tiles, `pivot: bottom-center | center` (default
    bottom-center for footprints), `iso {tile, guides}`.
  - The default tile is the largest even size at which every footprint
    spans at most 85% of its box's width and 40% of its height.
  - Validation covers the grid, cell range, footprint shape and fit, and
    pivot names.
- Sketch: each footprint is drawn as a white diamond at the bottom of its
  box (the guide), unless `iso.guides: false`. White is no longer a box
  colour on iso sheets.
- Prompt: an ISOMETRIC CAMERA paragraph covers:
  - 2:1 projection, one tile size across the sheet, and objects not growing
    to fill their boxes;
  - with guides: stand on the white diamond, which must not show;
  - without guides: footprint in tiles, plus the tile as a share of the box.
  - Each piece line also says "standing on a w×d-tile footprint".
- `internal/iso` finds an object's base from its lower silhouette.
  - Corner base: fits the two lower edges near the front corner (Theil–Sen),
    walks out until the silhouette jumps up (a wall or eave: the side
    corner), then refits each edge over its whole length.
  - Round base: fits the largest ground ellipse the silhouette follows
    (aspect 0.2–0.7); the footprint is the diamond around it, √2 as wide.
  - Gates, so a crate or a curve is not taken for a base:
    - edges 15–40°;
    - each edge at least 20% of the base;
    - the base at least 60% (corner) or the ellipse 45% (round) of the
      sprite's lower body. The lower body is the columns reaching into its
      bottom third, so sails and eaves don't count.
- `extract.Sprites` corrects each isometric piece:
  - **Scale** to the layout's tile. When one lower edge is cut short (below
    0.6 of what the footprint allows next to the other), the longer edge
    alone gives the tile.
  - **Squash** vertically by tan 26.57° / tan θ when both edges agree within
    5° and θ is within 8° of 2:1.
  - **Pivot** on the footprint's front corner; the file is padded if the
    corner lies below the drawing.
  - **Fallbacks:**
    - round and lopsided bases take the sheet's median scale;
    - round bases also take the sheet's median corner angle for the squash;
    - pieces with no base take both, plus a bottom-centre pivot.
  - **Check:** every corrected corner piece is measured again.
- Manifest:
  - `tile {layout, px}`;
  - per piece: `footprint`, `pivot {x, y}`,
    `base {shape, angle_left, angle_right, tile, dx, dy, left, right}`
    (as drawn, layout units), and `fix {scale, squash}`;
  - flags `unmeasured`, `scale_estimated` (round base), `angle_off` and
    `upscaled` (more than 1.25×).
- `scores.json` gains `iso`:
  - before correction: corner-base angle mean, sd and worst edge; drawn tile
    over asked (mean and cv); front-corner offset from the guide;
  - `after {scale_max_dev, angle_max_dev}`, from measuring again.
- Preview:
  - the sheet panel shows each guide diamond (white) and each detected base
    (green; red when `angle_off`);
  - on iso sheets the pieces panel is replaced by `scene.png`: every sprite
    stood on a 2:1 tile grid at its pivot, back to front, as an engine would
    draw it. Red footprints are unmeasured pieces.
- Matte: on a saturated key (green, magenta), any enclosed key-coloured area
  of 3×3 units or more is now a hole (the gaps between a windmill's slats,
  under an awning). Dark keys keep the big-and-thick rule from E5.
- The fake backend stands an isometric prism on each guide diamond, so the
  whole iso path runs for free.
- Tests:
  - base detection: prism with eaves, a 30° prism, a cylinder, and a flat
    box that must be rejected;
  - end-to-end correction: two prisms drawn at tile 100 and 60 both come out
    at 80, with the pivot on the front corner;
  - grid, cell and footprint layouts;
  - matte holes on green and black keys.

## E7. Footprint guides, and correcting sprites in code

Phase 5, 2026-11-25, $0.33: an iso style reference ($0.04), then the
iso-village case (9 buildings, 1K, magenta key, style ref) at Nano
Banana 2.1:
- 3 sheets with white guide diamonds (`layout.yaml`);
- 3 sheets without them (`layout_noguides.yaml`).

The user's direction was to accept fuzzy output and build around it:
detect, then adjust. So the question changed from "do guides make the
model accurate?" to "how much can code correct, and what is left?".

Images:
- `e7_before_after.png` and `e7_before_after_all.png`.
  - Before: the raw cut-outs, stood on a 2:1 grid at a bottom-centre pivot,
    unscaled.
  - After: blockout's `scene.png`.
- `e7_bases.png` and `e7_bases_zoom.png`: detected bases over the
  sheets.

| Per sheet, 3 sheets each | Guides | No guides |
| --- | --- | --- |
| Corner bases found (of 27) / round / none | 14 / 2 / 11 | 13 / 5 / 9 |
| Corner-base angle, sheet mean (sd within sheet) | 28.2–28.4° (0.5–0.8) | 27.9–28.2° (0.2–0.9) |
| Worst edge from 26.57° | 3.2° | 3.7° |
| Drawn tile ÷ asked tile, mean | ×1.13–1.34 | ×1.24–1.32 |
| Spread of drawn tile across one sheet (cv) | 0.17–0.20 | 0.18–0.20 |
| Front corner from where it was asked, mean (max), units | 15 (41) | 18–20 (38) |
| Applied: scale / squash | ×0.59–1.15 / 0.87–0.97 | ×0.60–1.05 / 0.87–0.97 |
| **After correction**, corner bases measured again: tile / angle | within 1.2% / within 0.4° | within 1.2% / within 0.8° |

Each object across the 6 sheets (corner / round / no base):

| Object | Corner | Round | No base |
| --- | --- | --- | --- |
| house_large, tower, statue | 6 | 0 | 0 |
| house_small, barn | 4 | 0 | 2 |
| house_small_damaged | 1 | 0 | 5 |
| windmill | 0 | 3 | 3 |
| well | 0 | 4 | 2 |
| market_stall | 0 | 0 | 6 |

**Findings.**

1. **The model does not follow footprint scale, with or without guides.**
   - It draws every object 1.1–1.3× the asked tile on average, with a 17–20%
     spread within a sheet.
   - The 1×1 statue comes out at 1.2–1.7× its tile, while the 3×3 house is
     close to 1×. So the big house is barely bigger than the 2×2 cottage.
   - The "true size, don't fill the box" wording did not change this.
2. **Its camera is steady but steeper than 2:1, and box shapes disagree with
   round ones.**
   - Box-like bases come out at 28.1–28.4° on every sheet, within ±0.9° on
     each sheet, never lopsided. That is a consistent, correctable error.
   - Round bases are drawn much flatter (ellipses at 13–22°). Its isometric
     is not one projection, and no single squash fixes both.
3. **Guides help position a little and scale not at all.**
   - Front corners land 15 units from the guide on average, against 18–20
     without one. That is the only measurable effect.
   - Since pivots now come from the detected base, even that matters little.
     Guides stay on by default because they are free and nothing white leaked
     in 3 sheets.
4. **Detect-and-adjust works for box-shaped bases.**
   - About half the pieces have one (27 of 54).
   - After scale and squash, every one measures again within 1.2% of the
     tile and 0.8° of 2:1.
   - By eye, the corrected sets read as one village at one scale. Before
     correction they are a pile of mismatched sizes.
   - Most corrections shrink (the model draws large), which keeps the art
     sharp. The largest enlargement was 1.15×.
5. **What the gates reject:**
   - stalls on legs (never measured);
   - ruins whose base is broken (1 of 6);
   - round bases, whose width a fit gets only to about ±40%. They keep their
     measured pivot but take the sheet's median scale.
   - These, about a third of an iso sheet, are the pieces to check by eye.
     They carry `unmeasured` or `scale_estimated` and a warning.
6. **The first correction pass made things worse until the gates were
   added.**
   - It read the windmill's round base as a 27-unit tile and enlarged the
     sprite 3.2×; a crate under the stall gave 5×.
   - The rule: correct only from a measurement that passed its checks, and
     re-measure afterwards.
7. **Matte findings.**
   - Magenta between sail slats and under awnings was kept as art by E5's
     thickness rule. On a saturated key it is now cut.
   - Left: a pink tint on semi-transparent chimney smoke, and a few specks
     in the burnt roofs.

## E8. Does a box sized to the footprint fix scale at generation?

Phase 5, $0.15: 3 sheets of `layout_fitted.yaml`.
- Same prompts, guides and tile 80 as E7.
- Each box is about 1.3× its footprint's width, as tall as the object
  should be, at the bottom centre of its old cell.

Images: `e8_fitted_preview.png` and `e8_before_after.png`.

| | E7 guides (cells) | E8 fitted boxes |
| --- | --- | --- |
| Mean box IoU | 0.63–0.67 | 0.47–0.69 |
| Worst spill past a box, units | 20–28 | 102–150 |
| Front corner from guide, mean (max) | 15 (41) | 21–68 (150) |
| Drawn tile ÷ asked, mean (cv) | ×1.13–1.34 (0.17–0.20) | ×1.21–1.22 (0.15–0.28) |
| house_small drawn tile, units | 118, 123 | 118, 118, 124 |

**Finding.** The box is not a scale lever for these sprites. Nano Banana
drew each building at the same size as in E7, whatever its box:
- the cottage came out at a tile of 118–124 in a 208-wide box, as in a
  304-wide cell;
- small boxes only made objects spill over, run into each other and drift
  from their guides.

So keep whole grid cells, and correct scale in code. No fitted-box or
object-height feature.

Not tested: more than 3 sheets per arm; other subjects (top-down items,
characters); a target angle other than 2:1. If the game's tiles are true
isometric (30°) or the existing tile angle is known, that changes the
squash target.

Phase 5 spend: $0.48 of $1.00 (tag `p5`).

## E9. Dogfooding: all the art for a small game

2026-11-25, at the user's request, to learn how good or bad the workflow is
when an agent uses it for real. The game is Glassfall, an original
falling-block puzzle in `games/glassfall/` (HTML, CSS and JS; open
`index.html`). Neon-glass style, $0.50 cap, tag `e9`, spent $0.16. The CLI
was used as shipped; where it fell short, the gap was worked around in the
game and logged here, not fixed in the tool.

| Run | What | Cost | Time | Result |
| --- | --- | --- | --- | --- |
| glassfall-style_ref-0001 | style ref, 16:9 | $0.037 | 9.5 s | used as is |
| glassfall-blocks-0001 | 7 block tiles + ghost tile, 3×3 grid | $0.043 | 19.9 s | blocks used as is; ghost recoloured in code |
| glassfall-ui-0001 | well frame, side panel, button + pressed, 3 icon buttons | $0.043 | 19.3 s | used as is |
| glassfall-background-0001 | full-screen backdrop, 16:9 | $0.037 | 15.8 s | used as is |

Every call succeeded on the first attempt (no relay errors). Four paid
calls, no rerolls, produced every image the game uses.

| Sheet | Found | Inside | Mean IoU (min) | Max spill | Size ratio | Max leak |
| --- | --- | --- | --- | --- | --- | --- |
| blocks | 8/8 | 8/8 | 1.00 (1.00) | 0 | 1.00 | 0.48 (false positive, finding 5) |
| ui | 7/7 | 4/7 | 0.95 (0.87) | 40 | 1.03 | 0 |

**What worked.**

1. *Simple, box-shaped assets land on their boxes.* Square tiles, panels and
   buttons came out at IoU 0.87–1.00 and size ratio 1.00–1.03, against
   0.63 and 1.26 for the wood UI kit in E2. The tall well frame came out tall.
   One run each, so this is a lead, not a result: the overgrowth in E2 may
   depend on the subject more than the sketch.
2. *The loop is agent-friendly end to end.* Every step's JSON gave the next
   step what it needed: files, sizes, scores, the preview path and
   `tag_spent_usd`. `sketch` caught layout problems for free before any
   spend. No exit code other than 0 came up.
3. *The style ref anchored the set.* Blocks and UI read as one game, and the
   blocks look lifted from the ref, as in E1 finding 3.
4. *Extraction needed no cleanup on the blocks.* The soft matte left no
   green rim, and every block came out 256×256.
5. *Button states work in an engine.* The pressed button was resized to the
   same 440×126 and shares insets, so swapping it on press doesn't move
   anything.

**Where it fell short**, most costly first.

1. *The key colour tints glass, and nothing reports it.* The ghost tile (a
   thin "pale-blue glass rim") came out key-green throughout its body. The
   UI glass came out teal-green, where the style ref's glass is grey-navy.
   The manifest reported `edge_tint: 0` for the ghost, because edge tint
   only looks at edge pixels, and `key_inside` only covers enclosed patches.
   Worked around by desaturating the ghost in code. Candidates:
   - a per-piece score for a body cast toward the key's hue;
   - for translucent or neon styles on a dark game, a black key (bright rims,
     so E5's dark-outline risk doesn't apply). Untested.
2. *Nine-slice insets conflate "what may stretch" with "where content
   goes".* A glare streak on one edge made the right insets much wider than
   the left: well frame 111 against 41, side panel 81 against 34, button 97
   against 80. Used directly as CSS `border-width`, the board sat about
   35 px off-centre. Worked around in the game: content is inset by the
   thinner side, and the wider slice is drawn under it. Candidate: a
   `content` inset (rim thickness) in the manifest next to `slice`.
3. *No export.* Manifest paths are relative to the run directory, and a game
   opened from `file://` cannot fetch `manifest.json`. That took a
   60-line `tools/import-art.mjs`: copy pieces, then write `art.js` with sizes
   and slices. Candidate: `blockout export <run>... --to <dir>`, ahead of
   engine-specific atlases.
4. *Promoting a ref is manual.* `cp <run-dir>/sheet.png refs/style_ref.png`,
   as in E1. Low cost, but it's the one step that is not a command.
5. *The colors sketch fights coloured assets.* Box colours come in a fixed
   order (red, blue, yellow, orange, purple, cyan, pink, brown). I ordered
   the blocks so each was drawn in a box of its own colour; a blue block in
   a red box is untested. The leak score then flagged the intended colours
   as leaks (0.25–0.48 on 5 of 7 blocks). Candidate: let a piece choose its
   box colour, or skip the leak check when the prompt names it.
6. *No single-image asset.* The backdrop was made with a reference layout,
   whose prompt calls the image "the art-direction reference for a game".
   It worked, but the framing is wrong for an asset.
7. *No `tile` kind,* though tiles are one of the tool's stated purposes. The
   unknown-kind warning repeats once per piece (7 identical lines in both
   `sketch` and `gen` output), and its line numbers point at each piece
   while the kind came from `cell_defaults`.
8. Minor:
   - Run names come from paths. `games/glassfall/art/blocks.yaml` would
     have been `art-blocks`, so the layouts sit in the game's root.
   - The preview's pieces panel scales each piece to its slot, so it hides
     relative size: the 140 px icons look bigger than the 900 px frame.
   - No piece-level regenerate (phase 6), so the ghost was fixed in code.

**Decision.** No defaults change; one game is a single run per sheet. The
next tool work, by what it cost here, is items 1–3: a key-cast score, content
insets, export.

## Tools added after E9

All three fixes for E9's costliest gaps were built and checked for free on
stored sheets: 221 pieces from E1 to E9, re-extracted.

- **`key_cast`.** Manifest field: the share of a piece's opaque pixels
  tinted toward a coloured key by more than 16 (on despill's 0–255 scale).
  Flag and warning at 0.03 or more. The threshold comes from the data:
  - The E9 ghost scores 0.884, far above everything else.
  - The next tier is all real casts:
    - 2 Lite drawing its outline sketch into the art: 0.16–0.19, and
      0.03–0.05 on its `key_inside` pieces;
    - magenta smoke and ash on the burnt cottage: 0.039–0.116 on 7 of 9
      sheets, the "pink tint on smoke" E7 noted by eye;
    - the green rim of the glass pressed button: 0.067.
  - 20 pieces are flagged. Every one looked at by eye was a visible tint.
  - Grey keys score 0, since they add no hue.
- **`content`.** Manifest insets next to `slice`: where a sliced piece's
  plain centre begins, the rim's thickness. That is where an engine puts a
  label or a board. The method: along every row (column) through the
  stretch region, follow the centre outward until a run of 3 pixels stops
  matching it; take the median over rows (columns); cap at the slice.
  - The first version scanned inward from the edge and stopped at a dark
    groove in the glass rim that matched the navy centre, giving 4 px for a
    30 px rim. As in E5, the numbers looked plausible and only the pixel
    dump showed the error.
  - Results, slice → content:
    - well frame 41/41/111/41 → 30/30/31/30;
    - side panel 34/34/81/35 → 22/22/23/23;
    - glass button 80/21/97/21 → 25/21/26/21.
  - On wood buttons, content stops where the carved swirls start (about
    80–95 px from each end), which is right for a label. Textured faces fall
    back to the slice.
  - The preview's UI strip now draws the content box in yellow.
- **`blockout export <run>... --to <dir> [--js name]`.** Copies every piece
  as `<id>.png` with one `manifest.json` (files relative to `<dir>`, plus
  `from` and the sources with cost).
  - A reference run adds its whole image, named after its layout file.
  - `--js` also writes `manifest.js` for pages opened from `file://`.
  - It refuses an id clash before writing anything, and refuses failed or
    dry runs.
  - Pieces' flags are repeated as warnings.

Glassfall now uses all three:
- `export` replaced the 60-line import script;
- the frame and panels are laid out from `content`, so the min-side
  workaround is gone;
- the ghost is desaturated only when it carries `key_cast`.

The smoke test passes. Still open from E9: promoting a ref, coloured assets
in the colors sketch, single-image assets, a `tile` kind.

## E10. A Claude Code skill, and what its test runs found (2026-11-26)

**Question.** Does a skill (`skill/blockout/`) make an agent use blockout
better than `blockout help` alone, and what do agents trip on?

**Setup.** Three tasks, each run once with the skill and once without, all
free (dry runs, fake backend, a stored sheet). The tasks, checks and graded
runs are kept locally.
1. Cozy farming UI kit, "don't spend money yet": layout, sketch, dry run, plan.
2. Isometric city builder: fake run, export, how to anchor and scale in Phaser.
3. Review of `glassfall-blocks-0001`: can the pieces go in as they are?

**Results.** With the skill 22/22 checks, without 20/22; with the skill 55 s
faster and 2.6k fewer tokens per run on average.
- Only task 1 separated them. Without the skill, the agent planned no style
  reference, gave no cost ("I can't see the model's price") and picked a $1
  budget itself. Without the skill, task 3 also suggested a paid `gen` with
  no `--tag` or `--budget`.
- Tasks 2 and 3 passed either way: `help`, the flags and the warning text
  already led a capable agent to the right answer, including `key_cast` on
  the ghost and the leak score being expected.
- With the skill, the colour-matching tip (E9 friction 5, untested) changed
  the art: the iso run gave the apartment block teal walls to suit its cyan
  box. The tip is now worded as a free precaution that never changes a
  piece.

**Tool gaps that both configurations hit.**
- *`export` dropped the iso tile.* An engine scales iso sprites by
  (its tile ÷ `tile.px`), but only the run manifest had `tile`. Both agents
  dug it out of the run directory. Fix: in the exported manifest every iso
  sprite carries its own run's `tile` (runs at 2K have twice the tile, so
  one top-level value would be wrong for mixed sets).
- *A faint sliver padded `block_yellow` to 256×258.* Row 0 held 72 pixels of
  JPEG speckle near the key (alpha ≤ 7, seven of them at 4 or more), row 1
  was empty, and the art began at row 2, so the file sat 2 px off the other
  blocks. Fix: the trim drops an edge strip that an empty row or column cuts
  off from the art and that never reaches alpha 32.
  - Raising the visible threshold from 4 to 8 also removed this sliver, but
    only because the speckle peaked at 7. The gap rule doesn't depend on how
    strong the noise is, and never touches soft edges joined to the art.
  - On the 221 stored pieces: 6 change, each by 2 px (`block_yellow`, two
    wood panels, two avatar frames, a gear icon); none grows; no flag
    changes; nine-slice insets on the two panels move 1–4 px. Pieces lose at
    most 0.002% of their alpha.
  - A first measurement reported 42 pieces, including 42 px "strips" on
    wood buttons and 5 px under iso sprites. It had started from the whole
    file, so it counted the transparent canvas that aligns button states
    and the iso sprite canvas as strips. Measured from the trimmed bounds,
    those are untouched.

**Decision.** Keep the skill, and make tests that the tool's own output does
not already answer. Both fixes are in, with tests.

## Fixes from the pre-release code review (2026-11-26)

A read-through of the whole code base before the first public push found
these bugs, all now fixed. Every fix has a test except the two about cost
after a paid call, which would need a faked failing OpenRouter response. The
extraction and fake-backend tests were checked to fail on the old code. No
defaults change.

- **A neighbour's enclosed key pixels could land in a piece's file.** An
  enclosed near-key area was kept in any piece whose drawn bounds held it,
  so an L-shaped piece reaching under a neighbour took specks from inside
  that neighbour. The matte now records which piece's drawn pixels border
  each enclosed area, and only that piece gets it (and counts it for
  `holes` and `key_inside`).
  - On the 221 stored pieces (E1–E9, 36 sheets, re-extracted with the old
    and the new code): 217 are byte-identical. 4 lose 2–7 edge pixels
    each, near-key specks the piece didn't enclose, one of them fully opaque
    key green. In one manifest a piece's `holes` drops from 4 to 2 for the
    same reason, and its `edge_tint` from 26.9 to 26.8. No flag, rect, inset
    or score changes.
- **Button state chains** (`b`, `b_x`, `b_x_y`) collapsed in one pass over
  a map, so deeper chains came out differently from run to run. Every state
  now maps to the root of its chain.
- **Iso offsets** (`offset_mean`, `offset_max`) were 0 on sheets whose bases
  were all round. They now cover every measured base, as documented.
- **A grey key** that every sketch colour is close to crashed `sketch` and
  `gen --dry-run` with a divide by zero. It is now a layout error.
- **The fake backend** took any white pixel for a footprint guide, so on
  non-iso sheets the white labels of `blocks` and a white `colors` box made
  pieces go missing. It is now told whether the layout has guides.
- **Run names.** `gen layout.yaml` from inside the case directory named the
  run `.-0001`. Names now come from the absolute path, so a layout gets the
  same run name whatever the working directory.
- **Grid cells of zero or negative size** passed validation and went to the
  model as an empty sketch. They are now a layout error.
- **Iso prompts for `blocks`, `outlines` and `none`** never explained the
  footprints, though the first two draw the diamonds. They now carry the
  same camera paragraph and footprint sizes as `colors` (`none` in words,
  since it sends no sketch). Untested on the model, since these styles lost
  to `colors` in E2/E3.
- **Cost after a paid call.** If decoding the sheet or deriving from it
  failed, `run.json` was never written, so the cost vanished from `runs` and
  `--budget`. `run.json` is now written (status `generated`) as soon as the
  call returns, and marked `failed` if a later step fails. An OpenRouter
  answer that is billed but has no usable image keeps its cost too.
- **Exported manifests** carried each layout's full local path into a
  game's assets. They now name it by directory and file (`art/ui.yaml`).
- `extract` on a directory that isn't a run is a usage error (`not_a_run`)
  rather than a layout error. Help text, stale comments and dead code
  (an unused per-pixel distance buffer of 4 bytes per pixel, among others)
  were cleaned up.
