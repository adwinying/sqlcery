# SQLcery Domain Context

SQLcery is a TUI SQL client. Its goal is to minimize the keystrokes needed for database operations — through SQL Assistance (autocomplete, Statement Expansion) and an interactive Results Pane for navigating and acting on query results.

---

## Glossary

### Connection
A named database configuration entry defined in `connections.toml`. Specifies the database type, credentials, and optional lifecycle/SSH settings. A Connection can be referenced by name from the CLI or from `sqlcery.toml`. Distinct from a Session.

### Connection String
A DSN-style string that identifies a database directly (e.g. `postgres://user:pass@host/db`, `sqlite:path/to/file`). Used as an alternative to referencing a named Connection. Can be passed via CLI argument or the `connection` field in `sqlcery.toml`.

### Session
The live runtime connection to a database for the duration of a single SQLcery invocation. Created from a Connection or a Connection String at startup. Holds the active database handle. Distinct from a Connection (config) and an Adapter (implementation). When a Session is lost, SQLcery enters the Reconnect state.

### Reconnect
The application state entered when a Session is lost unexpectedly. Tracks the number of attempts made, the reason for the loss, and the last error encountered. Distinct from the initial Session startup — Reconnect is a recovery path, not normal initialisation.

### Adapter
The internal infrastructure layer that executes SQL against a Session. Wraps the database driver, handles dialect differences, and normalizes results. Not a domain concept exposed to users — it is an implementation detail underneath a Session.

### Running Statement
The live state of a Statement that is currently executing. Tracks the display label, start time, elapsed duration, and spinner animation frame. Stored on the Interaction State while execution is in flight; nil when idle. Occupies the Notification slot in the Status Bar without a Notification Level — it is not a Notification and does not auto-clear.

### Statement
A complete SQL expression submitted to the database. Must end with a semicolon. Covers all SQL verb types (SELECT, INSERT, UPDATE, DELETE, etc.). Distinct from a Slash Command.

### Query
A Statement that returns rows — SELECT, EXPLAIN, SHOW, etc. A subtype of Statement.

### Slash Command
A `/`-prefixed meta-command (e.g. `/tables`, `/select users`) interpreted by SQLcery itself and never sent to the database. May execute immediately (e.g. `/tables`) or expand a SQL template into the Command Pane for the user to review and submit.

### Result Set
The tabular data (columns + rows) returned by a Query. Each cell carries a Value Kind. Displayed in the Results Pane.

### Value Kind
SQLcery's normalised semantic type classification for a single cell value in a Result Set. One of: **null**, **bool**, **integer**, **float**, **decimal**, **string**, **bytes**, **time**, **unknown**. Assigned when the driver's raw value is normalised into a Result Set — distinct from Driver Column Type, which is the raw SQL type name the driver reports for the column.

### Value Literal
The dialect-aware SQL literal rendering of a single Result Set value, keyed off its Value Kind (e.g. `NULL`, `TRUE`, a quote-escaped string, a canonical timestamp). Produced by the Dialect. Most kinds render identically across dialects; the bytes kind is the one that diverges (PostgreSQL `decode('…','hex')` vs `X'…'` elsewhere). Distinct from how a value is *displayed* in the Results Pane.

### Results Pane
The top pane of the TUI. Displays the Result Set from the most recently executed Query. Supports interactive navigation, row marking, SQL composition from rows, and export. Can be maximized (results-only Layout). Some row actions are queued as a Pending Action rather than executed immediately.

### Row Cursor
The single highlighted row in the Results Pane that is currently active for navigation and single-row actions (SQL composition via `yy`/`cc`/`dd`, marking via `space`). Distinct from Marked Rows, which is a multi-row set. The Row Cursor is owned by the Results Pane and does not persist across Result Sets.

### Pending Action
A deferred action stored on the Results Pane model and processed on the next update tick. Used when an action must be applied after the current key-handling pass completes. Current variants: **compose-insert**, **compose-update**, **compose-delete** (trigger Statement Expansion for the selected row), and **goto-top** (scroll the Results Pane to the first row).

### Command Pane
The bottom pane of the TUI. Where the user types and edits SQL Statements or Slash Commands. Contains the editor, autocomplete dropdown, and REPL transcript. Can be maximized (command-only Layout).

### Active Pane
Which pane — Results Pane or Command Pane — currently has keyboard focus. Determines which keybindings apply. Distinct from Layout, which controls which panes are visible.

### Layout
The visual arrangement of the two panes. One of:
- **Split** — both the Results Pane and Command Pane are visible (the default)
- **Command-only** — the Command Pane fills the terminal; the Results Pane is hidden
- **Results-only** — the Results Pane fills the terminal; the Command Pane is hidden

"Maximizing" a pane switches to the corresponding single-pane layout. Distinct from Active Pane, which tracks keyboard focus independently of which panes are visible.

### Status Bar
The single-line strip at the very bottom of the TUI. Combines three elements: an optional Notification on the left, keybind hints in the middle, and the Connection name right-aligned. The hints section changes based on which pane or modal is focused. The Notification section is ephemeral — it auto-clears after 3 seconds of inactivity (timer resets on every new message).

### Notification
A transient message displayed in the left section of the Status Bar. Carries a Notification Level that determines its colour. All notifications auto-clear after 3 seconds; the timer resets whenever a new notification arrives. The running indicator occupies the Notification slot during Statement execution but is not itself a Notification — it has no severity colour and does not auto-clear. Absent when no recent action has produced feedback and no Statement is executing.

### Notification Level
The severity classification of a Notification. One of:
- **None** — no Notification is present (the slot is empty or occupied by the running indicator)
- **Success** — operation completed successfully (green)
- **Info** — informational feedback, no action required (yellow)
- **Error** — operation failed (red)

Distinct from the running indicator, which occupies the Notification slot during Statement execution but carries no Notification Level.

### Modal
An overlay dialog rendered on top of both panes. Does not replace pane focus permanently. Current modals: History Search, Slash Command Wizard, Keybindings, Export Wizard.

### Slash Command Wizard
A guided multi-step Modal for selecting and executing a Slash Command. Opened via `/commands` or by pressing Enter on a Slash Command row in the Keybindings Modal. Distinct from typing a Slash Command directly into the Command Pane.

The wizard has up to three steps, each of which may be skipped:
1. **Choose command** — skipped when opened via direct invocation (the command is pre-selected from the Keybindings Modal row that was pressed)
2. **Choose target table** — skipped when the selected command does not require a table target
3. **Choose columns** — skipped when the selected command does not require column selection

The wizard's in-progress state is called the **Slash Command Wizard Context**: it tracks the current step, the available and selected command/target/column options, and a target filter string for narrowing the table list.

### Keybindings Modal
A Modal that serves as an interactive command launcher and keybindings reference. Displays a flat, context-sensitive list of Help Rows: one row per action, filtered to the active context (Command Pane, Results Pane, or the Modal currently on top of the stack), plus a small set of global rows always present (quit, toggle keybindings). Supports filtering by typing (same model as History Search) and row execution on Enter: keybinding rows synthesize their stored key back through the Update loop; Slash Command rows dispatch the command directly or open the Slash Command Wizard if a target is required.

### Help Row
A single selectable entry in the Keybindings Modal. Carries a display string and the action to execute on Enter — either a key to synthesize (for keybinding rows) or a Slash Command name (for slash command rows). The Keybindings Modal operates on a flat list of Help Rows with no section grouping.

### Export
Writing a Result Set — or a selected subset of its rows — to an output destination. Triggered via the Export Wizard. The `:w [filename]` shortcut is removed in favour of the wizard.

### Export Format
The serialisation format chosen during Export. One of: **CSV**, **TSV**, **JSON**, **Markdown**, **SQL**. Selected in the first step of the Export Wizard; can also be inferred from the file extension of the destination path when not explicitly chosen.

### Export Wizard
The two-step Modal for configuring and triggering an Export:
1. **Choose format** — filterable list of Export Formats
2. **Enter path** — a relative or absolute file path; leaving it blank exports to the clipboard instead

### History
The list of Statements executed against a given Connection or Connection String. Persists across Sessions — a new Session to the same Connection resumes the same History. Used for fuzzy recall via the History Search modal (Ctrl-r). Distinct from the Audit Log, which is a flat append-only record across all Connections.

### Audit Log
The persistent JSON file (`$XDG_DATA_HOME/sqlcery/audit.log`) that records every executed Statement. Each entry contains: connection name, statement text, timestamp, and result summary. Written regardless of whether execution succeeded. Distinct from History.

### Database Type
The database engine a Connection targets. One of: SQLite, PostgreSQL, MySQL. User-facing term used in config (`type = "sqlite"/"postgres"/"mysql"`). Distinct from Dialect and from Driver Column Type.

### Driver Column Type
The raw SQL type name reported by the database driver for a result column (e.g. `"VARCHAR"`, `"TIMESTAMPTZ"`, `"INT"`). This is a driver-level detail surfaced in a Result Set — it is distinct from Database Type (the engine) and from the Schema column type (which comes from schema introspection).

### Dialect
The internal implementation of SQL syntax rules specific to a Database Type. Concretely it owns three primitives: identifier quoting, placeholder generation (`$1` vs `?`), and Value Literal rendering. Maps 1-to-1 with Database Type but is an implementation concept, not a user-facing one. Statement assembly is not the Dialect's job — that belongs to the SQL Composer, which uses a Dialect for these primitives.

### SQL Composer
The internal module that assembles complete INSERT/UPDATE/DELETE statement text from a resolved spec (target table, ordered columns, row values, predicate columns), using a Dialect for identifier quoting and Value Literal rendering. Mechanical only: it renders strings and makes no decisions about which columns are keys, what the source table is, or whether a statement is safe — that policy stays with its callers. Shared by the Statement Expander and the SQL Export Format. Distinct from the Dialect (primitives) and the Statement Expander (policy).

### Schema
The overall structure of the connected database: its tables, columns, and types. SQLcery introspects the Schema at Session startup to power SQL Assistance. When the PostgreSQL-specific namespace qualifier on a table (e.g. `public` in `public.users`) must be named, call it a **Namespace**.

### SQL Assistance
The set of features that help a user write SQL with fewer keystrokes. Current forms: **Autocomplete** (a context-aware dropdown in the Command Pane that suggests SQL keywords, table names, column names, and Slash Commands near the cursor) and **Statement Expansion**. Future forms may include AI-driven suggestions. All SQL Assistance features currently use the lightweight-tokenization SQL Analysis Strategy.

### Autocomplete Scope
The SQL positional context at the cursor, used to decide which Autocomplete suggestions are relevant. Determined by scanning backwards through the current statement's tokens without a full parse. Current scopes, grouped by clause:
- **statement-start** — before any SQL keyword; **unknown** — position could not be determined
- **SELECT clauses**: select-list, table-ref, where-expr, join-condition, having-expr, group-by, order-by, returning
- **UPDATE clauses**: after-update-target, set-list
- **INSERT clauses**: after-insert-target, insert-statement
- **DELETE**: delete-statement
- **DDL**: after-table, create-statement, drop-statement

### SQL Analysis Strategy
The approach used by SQL Assistance features to understand the SQL text in the Command Pane. Two tiers:
- **Lightweight tokenization** — fast token scanning; handles keywords, identifiers, and clause boundaries without building an AST. Currently used by all SQL Assistance features. Implemented by a single tokenizer in `internal/sql` (a leaf package) shared by the editor's syntax highlighter, autocomplete scope/table analysis, and the statement-completion check — so all four read SQL the same way.
- **Full parser** — AST-level understanding; not yet used. Warranted only when a feature needs nested subquery awareness, table alias resolution, dialect grammar validation, or semantic SQL rewriting.

### Statement Expansion
The act of SQLcery generating a SQL Statement and loading it into the Command Pane for the user to review and edit before submitting. Has two triggering paths:
- **Slash Command path** — the Slash Command handler returns a SQL template directly (e.g. `/select users` expands a SELECT)
- **Row action path** — the Statement Expander composes INSERT/UPDATE/DELETE from a selected Result Set row (e.g. `yy`/`cc`/`dd`)

### Statement Expander
The module that performs Statement Expansion via the row action path. Given a selected row in the Results Pane, it resolves the policy for a dialect-aware INSERT, UPDATE, or DELETE — the source table (inferred or declared), which primary key columns form the WHERE predicate (falling back to all visible columns), and the refusal of an unsafe UPDATE with no identifying key — then delegates the actual statement text to the SQL Composer. Bulk variants compose one statement per selected row (UPDATE/DELETE), a `pk IN (…)` form for same-key DELETEs, or a single multi-row statement (INSERT). The Slash Command path does not go through the Statement Expander.

### Widget
A type in `internal/tui` that carries only cosmetic state — scroll offsets, suggestion-selection indices, transcript buffers — and exposes a `View(*ViewContext)` method for rendering. A widget never reads or writes `InteractionState`; all application data arrives through its View Context.

### View Context
A narrow struct defined alongside each widget in `internal/tui` (e.g. `EditorViewContext`, `ResultsPaneViewContext`). `internal/app` constructs one from `InteractionState` immediately before calling the widget's `View` method, then discards it. View contexts flow one way: from `internal/app` into `internal/tui`. They make widget rendering deterministic and isolate widgets from application-state types.
