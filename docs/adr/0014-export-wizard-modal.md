# ADR 0014: Export Wizard Modal

## Status

Accepted

## Context

Export was previously triggered via `:w [filename]` in the Results Pane — a vim-style prompt that inferred format from the file extension. This had two problems:

1. **No discoverability** — new users have no way to find the `:w` command without consulting the Keybindings Modal.
2. **No clipboard path** — leaving the filename blank was not supported; clipboard export required a separate mechanism.

The decision is to replace `:w` with a guided two-step Export Wizard modal.

## Decision

### 1. `:w [filename]` is removed

The `resultsPanePendingActionExport` path and all associated buffer logic (`exportBuffer`, `handleResultsPaneExportKey`, `updateResultsPaneExportPrompt`) are deleted. The Export Wizard is the only export path.

### 2. Trigger: `ctrl+e` from the Results Pane

`ctrl+e` opens the Export Wizard when the Results Pane is active. This key was previously bound globally to the Keybindings Modal. The Keybindings Modal trigger moves to `ctrl+t` (free, no conflicts).

### 3. Two-step flow

**Step 1 — Format:** Single-box modal listing CSV, TSV, JSON, Markdown. `ctrl+n`/`ctrl+p` or arrow keys navigate; Enter confirms. No filter box (only 4 options).

**Step 2 — Save Path:** Two-box layout. The filter box (top) is repurposed as a free-text path input; its label reads `Path:` rather than `Filter:`. The suggestions box (bottom) shows the format chosen in Step 1, the row scope (all rows or N marked rows), the current working directory, and a hint that leaving the field blank copies to clipboard. Esc goes back to Step 1.

### 4. Format wins over extension

The format chosen in Step 1 is authoritative. If the user types `output.json` but chose CSV, the file is written as CSV with a `.json` extension. `ExportOptions` gains a `Format export.Format` field; `export.Export()` skips `DetectFormat` when `Format` is set.

### 5. Blank path → clipboard

When the user presses Enter with an empty path field, the serialized bytes are written to the system clipboard via `golang.design/x/clipboard` (or equivalent). The file-write path is skipped entirely.

### 6. Scope inheritance

The wizard exports the same scope as `:w` did: marked rows if any are selected, otherwise all rows. No explicit step for scope selection.

### 7. `FilterLabel() string` added to Modal interface

The filter box label was hardcoded as `"Filter:"` in `model.go`. A new `FilterLabel() string` method on `Modal` lets the Export Wizard return `"Path:"` for Step 2. All existing modals return `"Filter:"` as the default.

## Consequences

- The `:w` shortcut is removed with no migration path — it is not a public API.
- `ctrl+e` changes meaning globally: it opens Export Wizard in Results Pane context and does nothing elsewhere (keybindings move to `ctrl+t`).
- `ExportOptions.Format` is a new field; callers that leave it zero use the existing `DetectFormat` fallback.
- The `Modal` interface gains one method (`FilterLabel`); all existing modals need a one-line implementation.
