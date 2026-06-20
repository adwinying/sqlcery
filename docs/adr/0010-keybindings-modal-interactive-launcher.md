# ADR 0010: Keybindings Modal as interactive command launcher

## Status

Accepted

## Context

The Keybindings Modal (`helpModal`) was a static, read-only reference rendered as a fixed string. It had three problems:

1. **Content clipped**: `ModalFixedRows = 16` meant the Slash Commands section — appended last in `renderKeybindingsContent` — was always clipped and never visible to users.
2. **Multi-action lines**: rows like `ctrl+x switch focus; ctrl+z zoom; ctrl+1 focus results` packed multiple actions together, which was only acceptable for a read-only display.
3. **Not actionable**: users had to close the modal and then manually invoke the command they just read about.

The goal was to add vim-style keyword search and command execution directly from the Keybindings Modal.

## Decision

### 1. Flat Help Row list replaces sections

`helpSection` and all section-grouping logic are replaced by a flat `[]helpRow`. Each row carries:
- `display string` — the text shown to the user
- `key string` — the key to synthesize on Enter (keybinding rows), or a slash command name to dispatch (slash command rows)
- `isSlash bool` — discriminates keybinding rows from slash command rows

No section headers are rendered. The flat list is easier to filter and renders more cleanly in a scrollable viewport.

### 2. Context-sensitive rows replace always-show-all

Rows are filtered to the active context when the Keybindings Modal is opened:
- A small set of global rows (quit, toggle keybindings) always appears.
- Command Pane context shows command keybindings and the full Slash Commands list.
- Results Pane context shows results keybindings only.
- Modal contexts (History Search, Slash Command Wizard) show that modal's keybindings only.

`helpModal` already captured the context via `contextModal AppModal`; it now also captures `contextPane Pane` at push time.

### 3. Always-on filter, same model as History Search

Typing immediately filters the visible rows (no `/` mode trigger). `ctrl+n`/`ctrl+p` and up/down arrows navigate among filtered rows. Two-level Esc: Esc clears the filter if non-empty, otherwise closes the modal. This reuses the established History Search pattern rather than introducing a new modal-within-modal search mode.

### 4. Scrollable within the existing 16-row viewport

Content scrolls to keep the selected row visible (same pattern as History Search's entry viewport). `ModalFixedRows` stays at 16 — making modal height dynamic would affect all modals.

### 5. Execution via key synthesis for keybinding rows

Pressing Enter on a keybinding row closes the Keybindings Modal and synthesizes the stored key back through the Update loop. This avoids duplicating the key→action mapping that already exists across all `HandleKey` and Update switch statements — the existing handlers do the work.

### 6. Execution via slash dispatch for slash command rows

Pressing Enter on a Slash Command row with no required target dispatches the command directly. If the command requires a target, the Slash Command Wizard is opened pre-seeded to the target-selection step for that command (same path as `/commands` → select command → enter).

## Consequences

- The Keybindings Modal is now an interactive command launcher, not a static reference.
- The Slash Commands section is visible for the first time (previously always clipped).
- `helpSection` is deleted; `helpRow` is the new primitive.
- Key synthesis (`modalResultSynthesizeKey` or equivalent) is a new `ModalResult` variant.
- `helpModal` gains filter, selection index, and context pane fields.
- One-action-per-line restructuring is required: existing multi-action prose lines are split into individual rows.
