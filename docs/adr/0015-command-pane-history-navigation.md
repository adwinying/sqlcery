# ADR 0015: Command Pane History Navigation

## Status

Accepted

## Context

History is currently accessible only via the History Search modal (`ctrl+r`). There is no way to step through previous commands inline, the way any terminal lets you press ↑/↓ or `ctrl+p`/`ctrl+n`.

## Decision

### 1. Two key groups; different gates

**`ctrl+p` / `ctrl+n`** — navigate history unconditionally whenever the autocomplete dropdown is not visible. No line-boundary gate: these bindings have no competing meaning in the editor.

**↑ / ↓** — navigate history only when the autocomplete dropdown is not visible **and** the cursor is on the first (for ↑) or last (for ↓) line of the buffer. On any other line they fall through to the textarea for normal cursor movement. This mirrors how terminal emulators with multi-line input (fish, zsh with `zle`) behave.

When the autocomplete dropdown is visible, `ctrl+p`/`ctrl+n` continue to navigate the dropdown (existing behaviour); ↑/↓ continue to pass through to the textarea.

### 2. Deduplicated history list, newest-first

Navigation steps through the same deduplicated list produced by `rankHistorySearchEntries` with an empty filter — the same source used by the History Search modal. Entries with identical display text are collapsed to their most recent occurrence. The most recent unique command is at index 0.

### 3. Draft preservation

When the user begins navigating (first ↑ or `ctrl+p`), the current editor content is saved as a draft. Pressing ↓ / `ctrl+n` past the most recent history entry restores the draft. Any edit to the buffer while mid-navigation discards the nav cursor and treats the current content as the new draft; further ↑/↓ resumes from index 0 of the deduplicated list.

### 4. Edge clamping with notification

At the oldest entry, further ↑/`ctrl+p` clamps and emits a `NotificationInfo` ("Beginning of history"). At the draft (already at the newest), ↓/`ctrl+n` clamps silently — the user is at their normal editing state and needs no feedback.

### 5. State lives in `commandModeModel`

Two fields are added to `commandModeModel`:
- `historyNavIndex int` — the current position in the deduplicated list; `-1` means "at draft"
- `historyNavDraft string` — the saved editor content captured when navigation began

Both fields reset on `Clear()`, on submit, and whenever a buffer edit is detected mid-navigation.

## Considered Options

- **Arrow keys always navigate history**: rejected — it breaks cursor movement within multi-line SQL buffers.
- **Arrow keys only when buffer is single-line**: simpler gate, but inconsistent with how terminals actually work on multi-line input; line-boundary is the right model.
- **`ctrl+p`/`ctrl+n` use the same line-boundary gate as arrows**: rejected — these keys have no competing cursor-movement meaning, so adding a gate makes them silently do nothing mid-buffer with no obvious reason.
- **Raw (non-deduplicated) history**: rejected — identical to what the History Search modal uses; avoids a second source of truth for "the history list" and matches user expectations from shells.
- **No draft preservation**: rejected — a user who accidentally navigates away from in-progress SQL has no recovery path.
