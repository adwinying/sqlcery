# Two-box modal layout with title-in-border

Modals that have a filterable list are rendered as two stacked bordered boxes — a small Filter box (1 content row) on top and a Suggestions box (13 content rows) below — keeping the same 18 total outer rows as the previous single-box layout. Modals without a filter (e.g. the Slash Command Wizard's Command and Column steps) collapse to the original single box (16 content rows). The title of the Suggestions box is embedded in its top border rather than appearing as a styled content line.

## Considered options

**Single box with title as first content line (previous approach):** Simpler rendering but the title, filter prompt, and list items all compete for the same fixed row budget; the `query>` and `filter>` prompt lines consume a row and look cluttered.

**Two-box layout (chosen):** The filter input gets its own clearly labelled box, the list gets a titled border, and the row budgets are independent. The cost is a new `RenderTitledBox` primitive and two new methods on the `Modal` interface.

## Interface contract

`Modal` gains `FilterText() string` and `Title() string`. `FilterText` returns `""` to signal no filter box, or the current filter value with `█` always appended as a cursor (e.g. `"sel█"`, `"█"` when empty). `Render()` is unchanged in signature but now returns list content only — no title line, no filter prompt line.
