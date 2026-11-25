# blockout layout format

A layout is YAML. Unknown fields are errors (typos are caught), and every
problem is reported with its line number. `blockout sketch <file>` validates
for free.

## Contents

- Top-level fields
- Pieces
- Canvas sizes
- Grids and cells
- Isometric sprites
- Example: UI kit
- Example: tile set
- Example: isometric sheet
- Example: style reference / backdrop

## Top-level fields

| Field | Meaning |
| --- | --- |
| `canvas.aspect` | `1:1` (default), `2:3`, `3:2`, `3:4`, `4:3`, `4:5`, `5:4`, `9:16`, `16:9`, `21:9` |
| `canvas.size` | `1K` (default), `2K`, `4K`; `0.5K` only on some models. `gen --size` overrides it |
| `canvas.background` | the key colour as `#RRGGBB`, required for sheets. `#00FF00` default choice, `#FF00FF` when the art is green, `#000000` only for art with light outer rims |
| `style` | one line of art direction, repeated in the prompt |
| `style_refs` | up to 14 images (`.png`, `.jpg`, `.webp`), paths relative to the layout file. Used for look only, never layout |
| `prompt` | with `pieces`: a line about the whole sheet. Without `pieces`: the layout makes one whole image (a style reference or backdrop) |
| `model` | OpenRouter model id; default `google/gemini-nano-banana-2.1`. `gen --model` overrides it |
| `sketch` | `colors` (default: boxes filled with named colours), `blocks` (filled and numbered), `outlines`, `none` (positions in words only, worst placement) |
| `grid`, `cell_defaults`, `iso` | sprite sheets, below |
| `pieces` | the boxes |

## Pieces

| Field | Meaning |
| --- | --- |
| `id` | lowercase letters, digits, `_`, `-`; unique; becomes `<id>.png` |
| `kind` | `button`, `nine_slice`, `frame`, `icon`, `sprite`, or your own (warned, no special handling) |
| `box` | `{ x, y, w, h }` in layout units (below); or use `cell` |
| `cell` | `[col, row]` in the grid, from 0 |
| `prompt` | what to draw; one short phrase, no text on it |
| `footprint` | isometric: `[w, d]` in tiles |
| `pivot` | `bottom-center` (default with a footprint: the footprint's front corner) or `center` |

What each kind adds:
- `button`: prompt says plain face, no label; gets `slice` and `content`;
  `<id>_<state>` buttons with the same box size become states of `<id>`.
- `nine_slice`: prompt asks for decorated corners and plain, uniform edges
  and centre; gets `slice`, `content`, `stretch`; flagged
  `not_stretchable:x|y` if no plain run crosses the middle.
- `frame`: prompt asks for an empty centre showing the background, which is
  cut out as a hole.
- `icon`: one centred icon.
- `sprite`: one complete object; with a footprint, the isometric path.

## Canvas sizes

Coordinates are pixels of the 1K image at the canvas aspect, whatever size
is generated:

| Aspect | Layout space |
| --- | --- |
| 1:1 | 1024 × 1024 |
| 4:3 / 3:4 | 1200 × 896 / 896 × 1200 |
| 3:2 / 2:3 | 1264 × 848 / 848 × 1264 |
| 5:4 / 4:5 | 1152 × 928 / 928 × 1152 |
| 16:9 / 9:16 | 1376 × 768 / 768 × 1376 |
| 21:9 | 1584 × 672 |

Boxes must stay inside the space and not overlap. Keep 48–64 units of gap
between boxes and from the edge; pieces that aren't box-shaped are drawn
about 25% larger than their box.

## Grids and cells

```yaml
grid: { cols: 3, rows: 3, gutter: 64 }   # margin defaults to the gutter
cell_defaults: { kind: sprite, footprint: [2, 2] }
```

Every piece with a `cell` gets the box of that cell. A 3×3 grid with gutter
64 on a 1:1 canvas gives 256-unit square cells. `cell_defaults` fills `kind`,
`footprint` and `pivot` for pieces that leave them out.

## Isometric sprites

- `footprint: [w, d]`: tiles along the axis to the lower right (w) and lower
  left (d). The sketch draws it as a white diamond at the bottom of the box
  (`iso: { guides: false }` to send it in words only).
- `iso.tile`: tile width in layout units; by default the largest even size
  at which every footprint fits its box.
- After generation blockout finds each object's base, scales the sprite so
  its tile matches, squashes the angle to 2:1, and sets the pivot on the
  front corner. The manifest's `tile.px` is the tile width in the piece files.
- Use whole grid cells: smaller "fitted" boxes did not change the drawn
  scale, they only made objects spill and collide.

## Example: UI kit

```yaml
canvas: { aspect: "1:1", size: 1K, background: "#00FF00" }
style_refs: [refs/style_ref.png]
style: fantasy RPG UI, carved wood and gold trim, soft painted shading. Opaque materials; no outer glow or shadow around any piece

pieces:
  - id: btn_primary
    kind: button
    box: { x: 64, y: 64, w: 380, h: 120 }
    prompt: wide primary button, gold trim
  - id: btn_primary_pressed
    kind: button
    box: { x: 512, y: 64, w: 380, h: 120 }      # same size: a state of btn_primary
    prompt: the same button in its pressed state, darker and inset
  - id: panel
    kind: nine_slice
    box: { x: 64, y: 260, w: 560, h: 420 }
    prompt: parchment dialog panel, ornate corners, plain edges and centre that can stretch, no chips or cracks
  - id: avatar_frame
    kind: frame
    box: { x: 700, y: 260, w: 260, h: 260 }
    prompt: round portrait frame with an empty centre
  - id: icon_settings
    kind: icon
    box: { x: 64, y: 760, w: 160, h: 160 }
    prompt: gear icon
```

## Example: tile set

Pieces ordered so each tile sits in a sketch box of its own colour (colors
sketch on green assigns red, blue, yellow, orange, purple, cyan, pink, brown
in that order):

```yaml
canvas: { aspect: "1:1", size: 1K, background: "#00FF00" }
style_refs: [refs/style_ref.png]
style: glossy candy tiles, soft top-left light, crisp bevels. Every tile is opaque; no outer glow or shadow
grid: { cols: 3, rows: 3, gutter: 64 }
cell_defaults: { kind: tile }

pieces:
  - { id: tile_red,    cell: [0, 0], prompt: one square red candy tile seen straight on, filling the box edge to edge }
  - { id: tile_blue,   cell: [1, 0], prompt: the same tile in blue }
  - { id: tile_yellow, cell: [2, 0], prompt: the same tile in yellow }
  - { id: tile_orange, cell: [0, 1], prompt: the same tile in orange }
  - { id: tile_purple, cell: [1, 1], prompt: the same tile in purple }
```

## Example: isometric sheet

```yaml
canvas: { aspect: "1:1", size: 1K, background: "#FF00FF" }   # magenta: the art has grass and green roofs
style: isometric 2:1 projection, cosy village builder, clean painted style, light from the top left
style_refs: [refs/style_ref.png]
grid: { cols: 3, rows: 3, gutter: 28 }
cell_defaults: { kind: sprite, footprint: [2, 2] }

pieces:
  - { id: house_small, cell: [0, 0], prompt: a small timber cottage with a red tiled roof }
  - { id: house_large, cell: [1, 0], footprint: [3, 3], prompt: a two-storey stone house with a slate roof }
  - { id: well,        cell: [2, 0], footprint: [1, 1], prompt: a round stone well with a small wooden roof }
```

## Example: style reference / backdrop

No pieces, so no key colour or sketch: one whole image.

```yaml
canvas: { aspect: "16:9", size: 1K }
style: neon glass arcade puzzle art, deep midnight blue, cool rim light from the top left
prompt: >
  A mock in-game screen of the game, seen straight on: the play area, two
  side panels and a round button, in the final look. No labels.
```

For a backdrop, describe an empty scene ("no objects, no panels, low
contrast so pieces drawn on top stay readable") and add `style_refs` to
match the game.
