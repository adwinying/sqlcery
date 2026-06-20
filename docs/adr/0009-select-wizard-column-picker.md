# ADR 0009: Column picker step in the /select wizard

## Context

`/select <table>` is a Slash Command that expands a SELECT statement into the Command Pane. Before this change it loaded every column via the adapter and emitted them all explicitly — `SELECT col1, col2, col3 FROM table;`. There was no way to restrict the column list interactively; the user had to edit the generated SQL manually.

The Slash Command Wizard (opened via `/commands`) walks through command → target in two steps. Adding a column-selection step for `/select` fits naturally into this guided flow.

Two paths exist for `/select`:
- **Direct invocation**: `/select users` typed in the Command Pane
- **Wizard invocation**: `/commands` → choose `/select` → choose table → (new) choose columns

## Decision

### 1. Direct invocation uses `SELECT *`

`/select <table>` invoked directly emits `SELECT * FROM table;`. This is a simplification from the previous behaviour (explicit column list). Direct invocation is a speed shortcut — the user already knows what they want; adding a column picker mid-flow would break that contract.

### 2. Wizard invocation adds a column picker step

When `/select` is chosen via the wizard, a third step — `SlashCommandWizardStepColumn` — appears after the target step. The wizard flow becomes: command (1/3) → target (2/3) → columns (3/3).

### 3. Column source priority

Columns are loaded from the `AutocompleteSchemaContext` cache first (available at Session startup). If the cache does not cover the table, the adapter is queried. If neither source has columns, the column step is skipped entirely and execution proceeds with `SELECT * FROM table;`.

### 4. All columns selected by default

The picker starts with every column checked. Column types are shown alongside names when available (e.g. `[x] created_at  timestamptz`).

### 5. All-selected generates `SELECT *`

If the user confirms with every column still checked, the output is `SELECT * FROM table;`. A subset selection emits explicit names: `SELECT col1, col2 FROM table;`. This keeps `*` as the natural "give me everything" expression and avoids verbose boilerplate when nothing was deselected.

### 6. Zero-selection is blocked

Confirming with no columns selected is rejected with a status-bar message: "Select at least one column." The user must select at least one or press `esc` to go back.

### 7. Keybindings in the column step

| Key | Action |
|-----|--------|
| `ctrl+n` / `ctrl+p` | Move cursor down / up |
| `space` | Toggle selected column |
| `a` | Toggle all columns (select all if any are unchecked; deselect all otherwise) |
| `enter` | Confirm selection (blocked if zero selected) |
| `esc` | Go back to the target step |

### 8. Column picker is specific to `/select`

The `NeedsColumns` flag is added to `slashCommandSpec` as a general extension point, but only set for `/select`. Other commands are unaffected.

## Consequences

- Direct `/select users` becomes a true one-keystroke shortcut: `SELECT * FROM table;`, no DB round-trip for columns.
- Wizard users can narrow a wide table's SELECT to relevant columns without post-edit.
- The wizard step counter becomes command-dependent: `/select` shows 3 steps; all other commands show 2.
- If the schema cache is warm (the common case), the column step requires no extra DB round-trip.
