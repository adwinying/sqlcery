# ADR 0006: Modal stack, Hints Bar ownership, and Keybindings modal

## Status

Accepted

## Context

Each modal (History Search, Slash Command Wizard) embedded a `PanelHint` line at the bottom of its own `Render()` output — a per-modal cheatsheet rendered inside the modal box. The TUI also had a separate full-width help overlay (toggled by `alt+h`) that prepended above the pane content via a `HelpVisible` boolean on `InteractionState`. Neither approach composed well:

- Hints inside modal boxes were invisible to the Hints Bar; the Hints Bar always showed command-mode hints even when a modal was focused.
- The help overlay rendered outside the modal path (`tui.RenderModal` / `tui.OverlayCenter`) and was not displaying correctly.
- `m.modal` was a single slot — no mechanism to return to a previous modal after opening a secondary one.

## Decision

### 1. Modal stack replaces single modal slot

`m.modal Modal` is replaced by `m.modals []Modal`. Three helpers operate the stack:

- `pushModal(Modal)` — appends and updates `InteractionState.ActiveModal` to the new top.
- `popModal()` — removes the top and updates `ActiveModal` to the new top (or `ModalNone`).
- `currentModal() Modal` — returns the top, or nil if empty.

`closeModal()` calls `popModal()`. Pressing `esc` or dismissing a modal always pops one level; there is no full-stack clear. `InteractionState.ActiveModal` reflects the name of the topmost modal only — existing code that checks `interaction.ActiveModal == ModalXxx` continues to work unchanged.

### 2. `FooterHints` added to the `Modal` interface

```go
type Modal interface {
    HandleKey(tea.KeyPressMsg, ModalContext) ModalResult
    Render(InteractionState) string
    FooterHints(InteractionState) string
    Name() AppModal
}
```

`FooterHints` returns a pipe-separated hint string for the Hints Bar, state-conditional on the modal's own receiver state and `InteractionState`. `statusBarView` calls `m.currentModal().FooterHints(interaction)` when a modal is active, falling back to `m.command.FooterHints(interaction)` otherwise. Modal boxes no longer contain `PanelHint` lines — all hint surface moves to the Hints Bar.

### 3. Keybindings modal replaces the help overlay

A new `helpModal` implements `Modal`. Its `Render` method contains the content previously rendered by `renderHelpSurface`. `HelpVisible` is removed from `InteractionState`; the `toggleHelpIntentMsg` path is replaced by a direct `pushModal` / `popModal` call. `ctrl+?` is the trigger: if `helpModal` is already on top of the stack, pop it; otherwise push it.

### 4. Every Hints Bar context ends with `ctrl+? keybindings`

`alt+h help` is retired. All contexts — command mode, Results Pane mode, and every modal's `FooterHints` — append `ctrl+? keybindings` as the final hint.

## Consequences

- The Hints Bar is now the single surface for key hints in all contexts, including modals.
- Adding a new modal requires implementing `FooterHints` alongside `HandleKey`, `Render`, and `Name` — four methods, no ambient cheatsheet logic elsewhere to update.
- The modal stack makes the Keybindings modal a natural first-class modal: `ctrl+?` while History Search is open pushes Keybindings on top; closing it returns to History Search.
- `HelpVisible` is removed from `InteractionState`; callers that checked it must be updated.
- `historySearchPreviewRows` gains one row (the hint line it previously reserved is now gone from the modal box).
