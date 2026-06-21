# Vim-style populate-on-navigate autocomplete

Autocomplete navigation (`ctrl+n`/`ctrl+p`) immediately writes the highlighted suggestion into the editor buffer, rather than previewing via ghost text and requiring a separate confirm step (Tab/Enter). This matches vim's insert-mode completion model and reduces the required keystrokes to accept a suggestion.

**Considered options rejected:**

- *Confirm-to-commit (previous behaviour):* `ctrl+n`/`ctrl+p` only moved the selection highlight; Tab or Enter was required to write the text. Retained as a flag-guarded alternative but rejected — maintaining two autocomplete models adds ongoing complexity with no clear second user.

**Consequences:**

- The suggestion list is frozen when navigation begins; it does not recompute from the new editor content mid-cycle. A new cycle starts only after the dropdown closes.
- Navigation includes a restore slot (position -1): wrapping past the first/last item reverts the editor to the original typed prefix, matching vim's "none selected" state.
- Esc during navigation restores the original prefix. Typing a character accepts the current suggestion in place and opens a fresh autocomplete cycle.
- `EditorWidget.selectedSuggestion` supports -1 to represent the restore slot (no item highlighted in the dropdown).
