# SQLcery Domain Context

SQLcery is a TUI SQL client. Its goal is to minimize the keystrokes needed for database operations — through SQL Assistance (autocomplete, Statement Expansion) and an interactive Results Pane for navigating and acting on query results.

---

## Glossary

### Connection
A named database configuration entry defined in `connections.toml`. Specifies the database type, credentials, and optional lifecycle/SSH settings. A Connection can be referenced by name from the CLI or from `sqlcery.toml`. Distinct from a Session.

### Connection String
A DSN-style string that identifies a database directly (e.g. `postgres://user:pass@host/db`, `sqlite:path/to/file`). Used as an alternative to referencing a named Connection. Can be passed via CLI argument or the `connection` field in `sqlcery.toml`.

### Session
The live runtime connection to a database for the duration of a single SQLcery invocation. Created from a Connection or a Connection String at startup. Holds the active database handle. Distinct from a Connection (config) and an Adapter (implementation).

### Adapter
The internal infrastructure layer that executes SQL against a Session. Wraps the database driver, handles dialect differences, and normalizes results. Not a domain concept exposed to users — it is an implementation detail underneath a Session.

### Statement
A complete SQL expression submitted to the database. Must end with a semicolon. Covers all SQL verb types (SELECT, INSERT, UPDATE, DELETE, etc.). Distinct from a Slash Command.

### Query
A Statement that returns rows — SELECT, EXPLAIN, SHOW, etc. A subtype of Statement.

### Slash Command
A `/`-prefixed meta-command (e.g. `/tables`, `/select users`) interpreted by SQLcery itself and never sent to the database. May execute immediately (e.g. `/tables`) or expand a SQL template into the Command Pane for the user to review and submit.

### Result Set
The tabular data (columns + rows) returned by a Query. Displayed in the Results Pane.

### Results Pane
The top pane of the TUI. Displays the Result Set from the most recently executed Query. Supports interactive navigation, row selection, SQL composition from rows, and export. Can be maximized.

### Command Pane
The bottom pane of the TUI. Where the user types and edits SQL Statements or Slash Commands. Contains the editor, autocomplete dropdown, and REPL transcript. Can be maximized to fill the terminal.

### Active Pane
Which pane — Results Pane or Command Pane — currently has keyboard focus. Determines which keybindings apply.

### Hints Bar
The persistent single-line strip at the bottom of the TUI (above the Status Line) that shows the key bindings available in the current context. Its content changes based on which pane or modal is focused. Implemented as `statusBarView()` / `tui.AppTheme.Footer`.

### Status Line
The single-line strip at the very bottom of the TUI that shows transient feedback from the last action (e.g. "Executed in 23ms", error text, modal status updates). Replaced on each new action. Distinct from the Hints Bar above it. Implemented as `statusDescriptionView()` / `tui.AppTheme.MetaLine`.

### Modal
An overlay dialog rendered on top of both panes. Does not replace pane focus permanently. Current modals: History Search, Slash Command Wizard, Keybindings, Export Wizard (planned).

### Slash Command Wizard
A guided multi-step Modal for selecting and executing a Slash Command. Opened via `/commands` or by pressing Enter on a Slash Command row in the Keybindings Modal. Steps: choose a command → choose a target table (if required) → choose columns (if required). Distinct from typing a Slash Command directly into the Command Pane.

### Keybindings Modal
A Modal that serves as an interactive command launcher and keybindings reference. Displays a flat, context-sensitive list of Help Rows: one row per action, filtered to the active context (Command Pane, Results Pane, or the Modal currently on top of the stack), plus a small set of global rows always present (quit, toggle keybindings). Supports filtering by typing (same model as History Search) and row execution on Enter: keybinding rows synthesize their stored key back through the Update loop; Slash Command rows dispatch the command directly or open the Slash Command Wizard if a target is required.

### Help Row
A single selectable entry in the Keybindings Modal. Carries a display string and the action to execute on Enter — either a key to synthesize (for keybinding rows) or a Slash Command name (for slash command rows). The Keybindings Modal operates on a flat list of Help Rows with no section grouping.

### Export
Writing a Result Set — or a selected subset of its rows — to an output destination. The user chooses a format (CSV, TSV, JSON, Markdown) and a destination: either a file path (relative or absolute) or the clipboard (when no path is given). Currently triggered via `:w [filename]` in the Results Pane; planned to become an Export Wizard modal.

### History
The list of Statements executed against a given Connection or Connection String. Persists across Sessions — a new Session to the same Connection resumes the same History. Used for fuzzy recall via the History Search modal (Ctrl-r). Distinct from the Audit Log, which is a flat append-only record across all Connections.

### Audit Log
The persistent JSON file (`$XDG_DATA_HOME/sqlcery/audit.log`) that records every executed Statement. Each entry contains: connection name, statement text, timestamp, and result summary. Written regardless of whether execution succeeded. Distinct from History.

### Database Type
The database engine a Connection targets. One of: SQLite, PostgreSQL, MySQL. User-facing term used in config (`type = "sqlite"/"postgres"/"mysql"`). Distinct from Dialect and from Driver Column Type.

### Driver Column Type
The raw SQL type name reported by the database driver for a result column (e.g. `"VARCHAR"`, `"TIMESTAMPTZ"`, `"INT"`). This is a driver-level detail surfaced in a Result Set — it is distinct from Database Type (the engine) and from the Schema column type (which comes from schema introspection).

### Dialect
The internal implementation of SQL syntax rules and behavior specific to a Database Type — identifier quoting, timestamp formatting, SQL generation, etc. Maps 1-to-1 with Database Type but is an implementation concept, not a user-facing one.

### Schema
The overall structure of the connected database: its tables, columns, and types. SQLcery introspects the Schema at Session startup to power SQL Assistance. When the PostgreSQL-specific namespace qualifier on a table (e.g. `public` in `public.users`) must be named, call it a **Namespace**.

### SQL Assistance
The set of features that help a user write SQL with fewer keystrokes. Current form: **Autocomplete** — a context-aware dropdown in the Command Pane that suggests SQL keywords, table names, column names, and Slash Commands near the cursor. Statement Expansion is also a form of SQL Assistance. Future forms may include AI-driven suggestions.

### Statement Expansion
The act of SQLcery generating a SQL Statement and loading it into the Command Pane for the user to review and edit before submitting. Triggered either by a Slash Command (e.g. `/select users` expands a SELECT template) or by a row action in the Results Pane (e.g. `yy`/`cc`/`dd` expands an INSERT/UPDATE/DELETE from the selected row's values).

### Widget
A type in `internal/tui` that carries only cosmetic state — scroll offsets, suggestion-selection indices, transcript buffers — and exposes a `View(*ViewContext)` method for rendering. A widget never reads or writes `InteractionState`; all application data arrives through its View Context.

### View Context
A narrow struct defined alongside each widget in `internal/tui` (e.g. `EditorViewContext`, `ResultsPaneViewContext`). `internal/app` constructs one from `InteractionState` immediately before calling the widget's `View` method, then discards it. View contexts flow one way: from `internal/app` into `internal/tui`. They make widget rendering deterministic and isolate widgets from application-state types.
