# ADR 0007: Horizontal scroll for long modal lines

Long content lines inside modals (SQL entries in History Search, table names in the Slash Wizard) are silently truncated at `innerWidth` — the distinguishing part of a SQL statement is often at the tail, making entries indistinguishable when the head is shared.

## Decision

### 1. `Render` gains an `innerWidth` parameter

The `Modal` interface changes from `Render(InteractionState) string` to `Render(InteractionState, int) string`. Modals receive the actual inner width at render time so they can pre-apply per-row scroll offsets rather than relying on `RenderModal` to clip blindly.

### 2. Per-selected-row horizontal scroll

Modals that render a list track `hScrollOffset int` on their own struct. Only the **selected row** has the offset applied — via `ansi.Cut(line, hScrollOffset, hScrollOffset+innerWidth)` — before the content string reaches `RenderModal`. Non-selected overflowing rows fall through to `RenderModal`'s existing clip path, which is changed from silent truncation (`""`) to ellipsis (`"…"`).

`hScrollOffset` resets to 0 whenever the selection or filter changes.

### 3. `alt+←` / `alt+→` at 8-column steps

Bare `←`/`→` are reserved: both filter modals (History Search, Slash Wizard target step) accept arbitrary printable input, and bare arrows are the natural keys for future cursor movement within a filter. `alt+←`/`alt+→` carry the conventional "pan a viewport" semantic in TUI apps and do not overlap with either current or future cursor navigation. Step size is 8 columns — coarse enough to reach the tail of a long SQL line in a few presses.

### 4. Edge markers on the selected row

When `hScrollOffset > 0`, a `<` marker occupies the leftmost column of the selected row. When content extends beyond the right edge, a `>` marker occupies the rightmost column. Each marker costs one display column, taken from `innerWidth`.

## Consequences

- All four current `Modal` implementors (`historySearchModal`, `slashWizardModal`, `helpModal`, and future Export Wizard) must update their `Render` signature. `helpModal` ignores `innerWidth` — its content is static and designed to fit.
- `FooterHints` for list modals should include `alt+← alt+→ scroll` when the selected row overflows.
- `RenderModal`'s silent truncation is replaced by `"…"` as a fallback for all non-pre-scrolled overflowing lines.
