# ADR 0008: Line wrap for History Search modal

Supersedes ADR-0007 **for the History Search modal only**. The Slash Wizard modal retains the horizontal-scroll model from ADR-0007.

## Context

ADR-0007 introduced `alt+←`/`alt+→` horizontal scroll for long SQL lines in History Search. In practice this costs keystrokes — the user must press a key combination to read each entry — which conflicts with the goal of minimal interaction. The distinguishing content of a SQL statement is visible in full only after scrolling, which forces extra effort on every history lookup.

## Decision

### 1. All entries wrap

Every entry in the preview list wraps at `innerWidth - 2` display columns (leaving room for the 2-character `> `/`  ` prefix). `historySearchDisplaySQL` is unchanged — whitespace is still collapsed before wrapping, maximising SQL per line.

### 2. Entry-based viewport

The modal has a fixed `historySearchPreviewRows` (13) display rows for entries. Entries have variable height (≥1 wrapped lines each). The viewport scrolls by entry, not by display row: it advances just enough to keep the selected entry visible at the bottom edge of the preview area, matching the existing single-row behaviour.

### 3. Per-entry clip at preview height

A single entry whose wrapped line count exceeds `historySearchPreviewRows` is clipped at that limit; the last visible line has its final display column replaced with `…` to signal continuation. This is an edge case for very long statements.

### 4. `ctrl+r` removed inside the modal

`ctrl+r` opened History Search from the Command Pane. Inside the modal it previously cycled to older entries, creating two meanings for the same key. It is removed. Navigation inside the modal is `↑`/`↓` (or `ctrl+p`/`ctrl+n`) only.

### 5. Horizontal scroll removed

`hScrollOffset`, `alt+←`/`alt+→` key handling, and edge markers (`<`/`>`) are all removed from `historySearchModal`. The `ClampHScrollOffset`/`ApplyHScroll` helpers in `internal/tui` are retained (still used by the Slash Wizard modal).

### 6. `ctrl+n`/`ctrl+p` direction corrected

The previous implementation mapped both `ctrl+r` and `ctrl+n` to `cycle(+1)` (older), contradicting the hint that showed "ctrl+n newer". With `ctrl+r` removed: `ctrl+p`/`↑` cycle toward newer (lower index), `ctrl+n`/`↓` cycle toward older (higher index), matching bash history-search conventions.

## Consequences

- No keystrokes required to read any history entry — full SQL is visible on selection.
- The number of simultaneously visible entries is lower when statements are long, but the selected entry is always fully readable without interaction.
- `historySearchModal` no longer implements any of the horizontal-scroll protocol from ADR-0007. The Slash Wizard modal is unaffected.
