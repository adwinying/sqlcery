# ADR 0023: Visual mode for multi-row marking in the Results Pane

## Status

Accepted

## Context

Marking rows one at a time with `space` becomes tedious when the user needs to
mark a contiguous block of rows — for example, to export rows 5–20 or to
compose bulk INSERT/UPDATE/DELETE statements from a range. Three interaction
models were considered:

1. **Visual range mode** — press `V` to anchor the Row Cursor, extend with
   navigation keys, confirm with `space`. Analogous to vim's visual line mode.

2. **Shift+move accumulation** — `Shift+J`/`Shift+K` moves the cursor and
   marks the row being left, accumulating marks without explicit mode entry.

3. **Mark-all command** — a single keybind marks every row in the Result Set.

## Decision

Implement **visual range mode** (`V` → navigate → `space`/`Esc`).

Key behaviours:

- `V` in the Results Pane enters Visual Mode, setting the Visual Anchor to the
  current Row Cursor position.
- All navigation gestures — `hjkl`/arrows, `gg`, `G`, `ctrl+u`/`ctrl+d`,
  `ctrl+p`/`ctrl+n` — extend the Visual Selection from the Visual Anchor to the
  new Row Cursor position. They do not exit Visual Mode.
- `space` confirms: every row in the Visual Selection is added to Marked Rows
  (rows already marked are unaffected — no toggling), then Visual Mode exits.
- `Esc` cancels without modifying Marked Rows.
- SQL composition keys (`yy`/`cc`/`dd`) are blocked in Visual Mode. The user
  must confirm (`space`) first, then compose from Marked Rows.
- The Visual Selection is page-agnostic: the Visual Anchor and Row Cursor are
  global row indices, so the range spans pages naturally.
- Visual Mode is transient: entering a new Result Set clears it.

## Consequences

- `resultsPaneModeModel` gains two fields: `visualMode bool` and
  `visualAnchor int`. The Visual Selection rendered on each frame is
  `[min(visualAnchor, selectedRow), max(visualAnchor, selectedRow)]`.
- `ResultsPaneViewContext` and `ResultsPaneRenderState` gain a `VisualRange`
  field (nil when not in Visual Mode) so the TUI renderer can highlight the
  range distinctly from the Row Cursor and from Marked Rows.
- The Results Pane status bar hints change while Visual Mode is active to
  surface `space confirm` and `esc cancel`.
- No existing keybinding is displaced: `V` is currently unbound in the Results
  Pane.
- Shift+move (option 2) and mark-all (option 3) are not implemented. If a
  mark-all shortcut is added later it should stack on top of this model, not
  replace it.
