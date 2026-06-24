# ADR 0018: Mouse support

## Status

Accepted

## Context

SQLcery is keyboard-first, but several interactions feel awkward without mouse support: navigating to a distant row in the Results Pane, scrolling a long Result Set, and clicking into a pane that isn't currently focused. Adding mouse support means deciding precisely which interactions are mouse-driven and how they compose with the existing keyboard model.

The primary concerns were:
1. Scope — which interactions to cover.
2. Focus semantics — does hovering or scrolling transfer Active Pane focus?
3. Modal interaction — click-to-select vs. click-to-confirm, and whether clicking outside dismisses.
4. Opt-out — mouse capture breaks terminal-native text selection; power users need an escape hatch.

## Decision

### 1. Mouse mode: cell motion only

In `charm.land/bubbletea/v2`, mouse capture is requested by setting the `MouseMode` field on the `tea.View` returned by `Model.View()` — there is no `WithMouseCellMotion()` program option. `View()` returns `MouseMode: tea.MouseModeCellMotion` when mouse support is enabled. Cell motion reports clicks and scroll events with coordinates but does not track hover movement. All motion mode (`tea.MouseModeAllMotion`, which adds hover reporting) is not used — none of the interactions below require hover state.

### 2. Results Pane: click and scroll

- **Single click** on a Results Pane row: if the Command Pane is the Active Pane, switch focus to the Results Pane and move the cursor to the clicked row in one gesture. If the Results Pane is already active, move the cursor only.
- **Double click** on a Results Pane row: move cursor to the row and toggle its mark (add/remove from MarkedRows).
- **Scroll wheel** (vertical and horizontal): scrolls the Results Pane regardless of which pane is currently the Active Pane. Does not switch focus.

### 3. Command Pane: click only

- **Single click** anywhere in the Command Pane: if the Results Pane is the Active Pane, switch focus to the Command Pane. No cursor repositioning within the editor — the bubbles `textarea` has no mouse handling, and click-to-position would require mapping screen coordinates to buffer positions for low payoff in a SQL editor.
- **Scroll wheel**: scrolls the REPL transcript regardless of Active Pane. Does not switch focus.

### 4. Scroll does not transfer focus

Scrolling a non-focused pane is a passive "look around" gesture. It does not change the Active Pane. Clicking is the explicit focus-switch action.

### 5. Modals: click-to-select, double-click-to-confirm

- **Single click** on a modal list row: moves the row selection cursor.
- **Double click** on a modal list row: confirms the selection (equivalent to Enter).
- **Scroll wheel**: scrolls the modal list regardless of Active Pane.
- **Click outside a modal**: no-op. Multi-step modals (Slash Command Wizard, Export Wizard) would be frustrating to dismiss by accidental outside click.

### 6. Opt-out via config

Mouse support is always on by default. Users who need terminal-native text selection (which mouse capture disables) can opt out with `mouse_disabled = true` in `sqlcery.toml`. A `MouseDisabled bool` field is added to `config.Config`. The zero value (`false`) means mouse is enabled, which is the correct default without requiring a pointer. The flag is threaded into the `Model`, which consults it in `View()` to return `MouseModeCellMotion` or `MouseModeNone`.

## Consequences

- `Model.View()` returns `MouseMode: tea.MouseModeCellMotion` when enabled and `tea.MouseModeNone` when disabled; the flag is threaded from the CLI entrypoint through `Session.MouseDisabled` into the `Model`, which consults it in `View()`.
- `config.Config` gains `MouseDisabled bool` (toml: `mouse_disabled`), threaded from the CLI entrypoint into the `Model`.
- The model's `Update` must route `tea.MouseClickMsg` and `tea.MouseWheelMsg` events. Routing click events to the correct pane or modal requires knowing the rendered Y-coordinate boundaries of each region.
- Results Pane and Command Pane widgets must expose their rendered height so the event router can determine which region a click or scroll landed in.
- Double-click detection requires tracking the timestamp and position of the previous click; a click within a short window on the same row counts as a double-click.
- The Command Pane editor does not gain click-to-position-cursor behaviour.
