# SQLcery

SQLcery (pronounced "sequel-cery") is a TUI SQL client that minimizes keystrokes for database operations — through SQL Assistance (autocomplete, statement expansion) and an interactive Results Pane for navigating and acting on query results.

## Elevator Pitch

### Connections as config

Named connections live in `connections.toml`, which you can version-control and share with your team. PostgreSQL and MySQL connections support SSH tunneling through a jump host — resolved against `~/.ssh/config`.

### Act on query results directly.

Select a row in the Results Pane and press `yy`, `cc`, or `dd` to load a ready-to-edit INSERT, UPDATE, or DELETE into the editor — values pre-filled, no retyping.

### SQL without memorizing syntax

Slash commands expand SQL templates into the editor for review (`/select users`, `/insert orders`). `/commands` opens a guided wizard that walks you through picking a command and target table step-by-step.

## Installation

### Prebuilt binaries

Standalone binaries for macOS, Linux, and Windows (`amd64` and `arm64`) are available from the [GitHub Releases](https://github.com/adwinying/sqlcery/releases) page.

If you have [mise](https://github.com/jdx/mise) installed, you can install with the following command:

```sh
mise use --global github:adwinying/sqlcery
```

### `go install`

If you have go installed, you can install the latest release with:

```sh
go install github.com/adwinying/sqlcery/cmd/sqlcery@latest
```

## Features

### Multi-Database Support

- SQLite
- PostgreSQL
- MySQL

### Configuration

SQLcery loads two layered TOML files for app settings and two more for named connections:

- Main config: `<config-home>/sqlcery/sqlcery.toml`
- Local override: `./sqlcery.toml`
- Global connections: `<config-home>/sqlcery/connections.toml`
- Local connection override: `./connections.toml`

`<config-home>` is `$XDG_CONFIG_HOME` when set, `~/.config` on macOS otherwise. Local files override global ones.

Sample files with all options are at `examples/config/sqlcery.toml` and `examples/config/connections.toml`.

`sqlcery.toml` currently supports one setting:

```toml
mouse_disabled = true
```

#### Connections

Named connections live under `[connection.<name>]` and must declare a `type`.

SQLite:

```toml
[connection.local]
type = "sqlite"

[connection.local.sqlite]
database = "tmp/sqlcery.db"
```

PostgreSQL and MySQL (`password` is optional):

```toml
[connection.analytics]
type = "postgres"

[connection.analytics.postgres]
host = "127.0.0.1"
port = 5432
database = "warehouse"
username = "app"
password = "secret"

[connection.reporting]
type = "mysql"

[connection.reporting.mysql]
host = "127.0.0.1"
port = 3306
database = "reporting"
username = "root"
password = "secret"
```

You can also connect directly via a connection string:

- `postgres://...` and `postgresql://...`
- `mysql://...`
- `sqlite:tmp/sqlcery.db`
- `sqlite:///:memory:`

#### SSH Tunnels

PostgreSQL and MySQL connections support SSH tunneling via `ssh_host`:

```toml
[connection.analytics]
type = "postgres"
ssh_host = "bastion"

[connection.analytics.postgres]
host = "db.internal"
port = 5432
database = "warehouse"
username = "app"
```

- `ssh_host` is resolved against `~/.ssh/config`; if no matching alias exists, it is used as the hostname directly.
- Reads `Host`, `HostName`, `User`, `Port`, `IdentityFile`, `UserKnownHostsFile`, and `StrictHostKeyChecking` from SSH config.
- Authenticates via `SSH_AUTH_SOCK` and any resolved `IdentityFile` entries.
- Falls back to `~/.ssh/known_hosts`; if no known-hosts file exists, the tunnel fails unless `StrictHostKeyChecking` is set to `no`, `off`, or `false`.

### Connection Picker

Launching SQLcery without a connection argument opens the Connection Picker — a searchable, frecency-ordered list of named connections from `connections.toml`. Press `ctrl+s` at any time to open it and switch connections mid-session.

- Type to fuzzy-filter; `ctrl+n`/`ctrl+p` or arrow keys navigate.
- Press `Enter` to connect.
- A pinned "Create a new connection" entry at the bottom opens the New Connection Wizard.
- At startup, `Esc` with an empty filter quits. Mid-run, it closes the picker without disrupting the live session.

#### New Connection Wizard

Creates and persists a new named connection to `connections.toml`. Launched from the Connection Picker.

- **Step-by-step mode**: name the connection, choose a database type, then fill in type-specific fields one per screen.
- **DSN mode**: paste a connection string and review the parsed fields as a read-only summary.

Both modes end with a save-location step — choose global (`<configHome>/sqlcery/connections.toml`) or project-local (`<cwd>/connections.toml`), review a summary, and confirm. On completion the wizard returns to the Connection Picker with the new connection pre-selected.

### Command Pane

The Command Pane is the SQL editor at the bottom of the UI. Press `Enter` to submit a complete statement (ending with `;`) or slash command. Press `Ctrl-r` to open history search. Press `Esc` to clear the current input.

Autocomplete is context-aware and prioritizes likely keywords, tables, columns, and slash commands near the cursor. `Ctrl-n`/`Ctrl-p` navigate the dropdown (populate-on-navigate); `Esc` restores the original typed prefix.

Use `Ctrl-e` to open the current buffer in `$EDITOR` for multi-line editing, then return to SQLcery.

#### Slash Commands

- `/commands` — opens the guided slash-command wizard.
- `/tables` — lists tables immediately.
- `/columns <table>` — lists columns for a table immediately.
- `/select <table>` — expands a `SELECT` template into the editor.
- `/insert <table>` — expands an `INSERT` template into the editor.
- `/update <table>` — expands an `UPDATE` template into the editor.
- `/delete <table>` — expands a `DELETE` template into the editor.
- `/create <table>` — expands a `CREATE TABLE` template into the editor.
- `/drop <table>` — expands a `DROP TABLE` template into the editor.

#### History

Press `Ctrl-r` to open history search — type to filter, `Enter` to restore, `Ctrl-r` or `Up` for older matches, `Down` for newer, `Esc` to close. History persists across sessions for the same connection.

You can also step through history inline: `Ctrl-p`/`Ctrl-n` move backward/forward when the autocomplete dropdown is closed. `Up`/`Down` do the same when the cursor is on the first or last line of a multi-line buffer. Starting navigation saves the current content as a draft; `Ctrl-n` past the most recent entry restores it.

### Keybindings Modal

Press `Ctrl-t` to open the Keybindings Modal — an interactive command launcher and reference.

- Rows are context-sensitive: shows keybindings for the currently active pane or modal, plus global rows.
- Type to filter. `Enter` on a keybinding row executes that action. `Enter` on a slash command row dispatches it, or opens the Slash Command Wizard if a table target is required.
- `Esc` clears the filter if non-empty, otherwise closes the modal.

### Results Pane

When a query returns rows, the full result set is kept in the Results Pane. Primary-key columns are highlighted. Press `Ctrl-x` to move focus there, `Ctrl-z` to toggle zoom. Results are paged in chunks of 300 rows.

- Press `yy`/`cc`/`dd` to expand INSERT/UPDATE/DELETE for the active row into the Command Pane.
- Press `Space` to toggle marking for the active row.
- Press `V` to enter Visual Mode: navigate to extend a contiguous selection, then `Space` to add the range to Marked Rows, or `Esc` to cancel.
- Press `u` to clear all Marked Rows.

#### Export Wizard

`Ctrl-e` opens the two-step Export Wizard.

- **Step 1 — Format**: CSV, TSV, JSON, Markdown, or SQL.
- **Step 2 — Path**: a file path, or leave blank to copy to clipboard.

Exports Marked Rows when any are selected, or all rows otherwise.

### Mouse Support

Mouse support is on by default. Single-click moves focus; double-click toggles a row's mark; scroll wheel scrolls without changing focus. To opt out (restores terminal-native text selection), set `mouse_disabled = true` in `sqlcery.toml`.

### Keyboard Shortcuts

#### Global

- `Ctrl-t`: open the Keybindings Modal.
- `Ctrl-c`: quit.
- `Ctrl-s`: open the Connection Picker.

#### Command Pane

- `Enter`: submit.
- `Ctrl-r`: open history search.
- `Ctrl-n` / `Ctrl-p`: navigate autocomplete; or step through history when autocomplete is closed.
- `Ctrl-e`: open in `$EDITOR`.
- `Esc`: clear input / cancel query / back out of wizard.
- `Ctrl-x`: switch to Results Pane.

#### History Search

- `Enter`: restore selected entry.
- `Ctrl-r` or `Up`: older matches.
- `Down`: newer matches.
- `Esc`: close.

#### Results Pane

- Arrow keys or `h`/`j`/`k`/`l`: move active cell.
- `Space`: toggle mark (or confirm Visual Selection).
- `V`: enter Visual Mode.
- `u`: clear all Marked Rows.
- `yy` / `cc` / `dd`: expand INSERT / UPDATE / DELETE.
- `Ctrl-u` / `Ctrl-d`: scroll within page.
- `Ctrl-p` / `Ctrl-n`: previous / next page.
- `Ctrl-e`: open Export Wizard.
- `Ctrl-x`: focus Command Pane.

#### Layouts

- `Ctrl-1`: focus Results Pane.
- `Ctrl-2`: focus Command Pane.
- `Ctrl-z`: toggle zoom.

### Audit Log

All executed statements are written to `$XDG_DATA_HOME/sqlcery/audit.log`. Each entry is a JSON object:

| Field        | Description                                  |
| ---          | ---                                          |
| `connection` | Connection name, or database name if not set |
| `command`    | The SQL statement                            |
| `time`       | Execution timestamp                          |
| `result`     | Result summary                               |

## Go Architecture

SQLcery uses a layered Go design: `cmd/sqlcery` stays thin, application flow lives in `internal/app`, and infrastructure concerns are isolated in focused support packages.

| Package | Role |
| --- | --- |
| `cmd/sqlcery` | CLI entrypoint and dependency wiring |
| `internal/app` | Bubble Tea model, Connection Picker, SQL Assistance, slash commands, Results Pane |
| `internal/config` | Config loading, connection parsing, validation, SSH config helpers |
| `internal/db` | Adapters, dialects, metadata, lifecycle, tunneling, result normalization |
| `internal/history` | History and audit log |
| `internal/export` | CSV, TSV, JSON, Markdown, SQL writers |
| `internal/sql` | Tokenizer and keyword list, shared by highlighter and SQL Assistance |
| `internal/tui` | Presentation-only widgets, isolated from application state |
| `testdata` | Shared test assets and fixtures |

## Usage

```sh
$ sqlcery [connection name]
```
