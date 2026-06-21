# Fuzzy matching for all modal filters

All modal filter inputs (History Search, Slash Command Wizard target picker, Keybindings Modal) use the same character-by-character fuzzy subsequence algorithm, with results ranked by score when a filter is active.

## Considered options

**Substring match (previous approach for Wizard and Keybindings):** Simple and predictable, but requires the user to type the exact substring. Typing `jobpost` would not surface `job_post` or `job_with_posts`.

**Fuzzy subsequence match (chosen):** Matches any query whose characters appear in order within the candidate, skipping non-matching characters (including `_`). `jobpost` matches `job_post` (skips `_`) and `job_with_posts` (skips `_with_`). Results are re-ranked by score: consecutive character runs score higher than scattered matches, and earlier matches score higher than later ones.

## Implementation

The core algorithm lives in a single shared `fuzzyMatch(query, candidate string) (score int, ok bool)` function (previously `fuzzyHistoryMatch`, renamed). All three filter paths call it:

- `rankHistorySearchEntries` — History Search (pre-existing)
- `filterWizardTargets` — Slash Command Wizard target step
- `helpModal.filteredRows` — Keybindings Modal

When no filter is active, original list order is preserved. When a filter is active, matched results are sorted descending by score.

### Scoring

Each matched character contributes `1 + streak² + wordBonus`, where streak counts consecutive matched characters and wordBonus (+15) fires when the matched character follows a word separator (space, `_`, `+`, `-`, `/`, `.`) or starts the string. A separate `max(0, 32-firstMatch)` bonus rewards matches that appear early in the candidate.

The word-boundary bonus ensures that `ne` ranks `next` above `enter`: `n` at the start of "next" (after a space) gets +15, overcoming the small firstMatch advantage that "enter" would otherwise have due to `n` appearing at position 1.
