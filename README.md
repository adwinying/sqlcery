# SQLcery

SQLcery (pronouced "sequel-cery") is a SQL client for the command line. It should be able to predict your next SQL command, and help you write it as if it was magic.

SQLcery takes a hybrid approach by providing:
- an intuitive interface for executing SQL commands
- a powerful record viewer for easy inspection of query results.

## Installation

### `go install`

Install the latest CLI directly from the module:

```sh
go install github.com/adwinying/sqlcery/cmd/sqlcery@latest
```

Use a Go toolchain compatible with the `go 1.24.2` requirement in `go.mod`. The installed binary lands in `$(go env GOBIN)` when set, or `$(go env GOPATH)/bin` otherwise, so make sure that directory is on your `PATH`.

### Prebuilt binaries

This repository is configured to build standalone release binaries with GoReleaser for:

- macOS (`darwin`)
- Linux (`linux`)
- Windows (`windows`)
- `amd64` and `arm64`

When a tagged release is published, download the matching binary from the GitHub Releases page and place it on your `PATH`:

- <https://github.com/adwinying/sqlcery/releases>

If you need the same artifacts before an official release exists, build them locally with:

```sh
mise run release-snapshot
```

That command writes snapshot binaries to `dist/`.

Maintainers can rehearse the tagged-release workflow with the checklist in `RELEASING.md`.

### Package managers

`sqlcery` is not currently published from this repository to a package manager such as Homebrew, Scoop, or apt. For now, install it with `go install` or use a binary from GitHub Releases. If you package it internally, prefer pinning a tagged release instead of tracking `main`.

## Features

### Mutli-Database Support

SQLcery supports multiple databases. It can connect to a database using a connection string, or by specifying the connection name in the config file.

It supports the following database types:

- SQLite
- PostgreSQL
- MySQL
- more coming soon

### Command Mode

Command mode is the SQL editor at the bottom of the UI. It is where you write SQL, run slash commands, inspect autocomplete suggestions, and restore earlier queries from history.

- Press `Ctrl-g` to submit the current input.
- SQL only runs when the statement is complete, which currently means ending it with a semicolon (`;`).
- Slash commands such as `/tables` or `/select users` run through the same submit flow, but do not need a trailing semicolon.
- Autocomplete is context-aware and prioritizes likely keywords, tables, columns, and slash commands near the cursor.
- `Ctrl-r` opens reverse history search so you can filter and restore recent commands.
- `Esc` clears the current input when nothing else needs cancelling.

#### SQL Assistance Strategy

SQLcery currently standardizes on `lightweight-tokenization` for command-line assistance, including autocomplete and upcoming slash-command SQL composition.

- Use lightweight token scanning to identify the current statement, clause, qualifier, and referenced tables near the cursor.
- Keep command composition schema-driven and explicit instead of parsing and rewriting arbitrary SQL entered by the user.
- Revisit a full SQL parser only when assistance must understand nested queries or CTEs, resolve aliases and derived tables, validate dialect grammar, or preserve semantics while rewriting existing SQL.

#### Working In Command Mode

- Write multi-line SQL directly in the editor; line numbers are shown in the left gutter.
- The TUI uses an adaptive terminal palette so prompts, panels, result headers, warnings, and selected items stay readable in both light and dark terminals.
- Run `SELECT` statements from here to preview results inline. Command mode shows the first 5 rows and preserves the full result for the record viewer.
- Use `Ctrl-y` to accept the highlighted autocomplete suggestion.
- Use `Alt-n` and `Alt-p` to move through autocomplete suggestions.
- Use `Ctrl-x` to switch focus between command mode and the record viewer.
- Use `Ctrl-1`, `Ctrl-2`, and `Ctrl-3` to switch between split layout, command-only layout, and viewer-only layout.

#### Slash Commands

Slash commands are submitted from command mode with `Ctrl-g`.

- `/help` lists available slash commands.
- `/commands` opens the guided slash-command wizard.
- `/tables` lists tables in the current database immediately.
- `/columns <table>` lists columns for a table immediately.
- `/select <table>` expands a `SELECT` template into the editor for review.
- `/insert <table>` expands an `INSERT` template into the editor for review.
- `/update <table>` expands an `UPDATE` template into the editor for review.
- `/delete <table>` expands a `DELETE` template into the editor for review.
- `/create <table>` expands a `CREATE TABLE` template into the editor for review.
- `/drop <table>` expands a `DROP TABLE` template into the editor for review.

Commands that inspect metadata, such as `/tables` and `/columns`, execute immediately and return results. Commands that compose SQL load the generated statement into command mode so you can edit it before running it.

#### Slash Command Wizard

`/commands` opens a wizard in the viewer area.

- Step 1 lets you choose the slash command.
- Step 2 appears for commands that need a table target.
- Use `Ctrl-g` to confirm the current wizard choice.
- Use `Alt-n` and `Alt-p` to move through wizard choices.
- Use `Esc` to go back from table selection, or close the wizard from the first step.

#### History

Press `Ctrl-r` to open reverse history search. Type to filter recent commands, press `Enter` to restore the selected entry, press `Ctrl-r` or `Up` for older matches, press `Alt-p` or `Down` for newer matches, and press `Esc` to close the search.

### Record Viewer Mode

When a query returns rows, SQLcery keeps the full result set and lets you inspect it in record viewer mode.

- Command mode previews only the first 5 rows of a `SELECT` result.
- Press `Ctrl-x` to move focus to the record viewer in split layout.
- Press `Ctrl-3` to open the viewer-only layout.
- Record viewer pages results in chunks of 300 rows.
- Primary-key columns are highlighted with the shared adaptive accent palette.

#### Working In Record Viewer Mode

- Use the arrow keys or `h`/`j`/`k`/`l` to move the active cell.
- Press `Space` to toggle selection for the active row.
- Press `Ctrl-u` and `Ctrl-d` to move between record-viewer pages.
- Press `yy` to load an `INSERT` statement for the active row into command mode.
- Press `cc` to load an `UPDATE` statement for the active row into command mode.
- Press `dd` to load a `DELETE` statement for the active row into command mode.
- Type `:w [filename]` and press `Enter` to export selected rows, or the current result rows when nothing is selected. Export format is inferred from the file extension and supports CSV, TSV, JSON, and Markdown.
- Press `Esc` while entering `:w` to cancel the export prompt.
- Press `Ctrl-x` to return focus to command mode.

### Keyboard Shortcuts

#### Global

- `Alt-h`: toggle the help overlay for keybindings and slash commands.
- `Ctrl-c`: quit SQLcery.

#### Command Mode

- `Ctrl-g`: submit SQL or a slash command.
- `Ctrl-r`: open reverse history search.
- `Ctrl-y`: accept the highlighted autocomplete suggestion.
- `Alt-n` / `Alt-p`: move to the next or previous autocomplete suggestion.
- `Esc`: clear the input, cancel a running query, or back out of wizard flow depending on context.
- `Ctrl-x`: switch focus between command mode and record viewer.

#### History Search

- Type to filter recent commands.
- `Enter`: restore the selected history item into command mode.
- `Ctrl-r` or `Up`: move to older matches.
- `Alt-p` or `Down`: move to newer matches.
- `Esc`: close history search.

#### Record Viewer

- Arrow keys or `h`/`j`/`k`/`l`: move the active cell.
- `Space`: toggle selection for the active row.
- `yy` / `cc` / `dd`: compose `INSERT`, `UPDATE`, or `DELETE` SQL for the active row.
- `Ctrl-u` / `Ctrl-d`: move to the previous or next result page.
- `:w [filename]`: export selected rows or current result rows.
- `Ctrl-x`: focus command mode.

#### Layouts

- `Ctrl-1`: split layout.
- `Ctrl-2`: command-only layout.
- `Ctrl-3`: viewer-only layout.

### Auditing

All SQL commands executed in SQLcery are logged to `$XDG_DATA_HOME/sqlcery/history.log`. Each command is logged as a JSON object, with the following fields:

| Field | Description |
| --- | --- |
| `connection` | The connection name if specified, or database name if not set |
| `command` | The SQL command that was executed |
| `time` | The time the command was executed |
| `result` | The result of the command, if any |

### Configuration

SQLcery loads two layered TOML files for app settings and two more for named connections:

- Main config: `<config-home>/sqlcery/sqlcery.toml`
- Local override: `./sqlcery.toml`
- Global connections: `<config-home>/sqlcery/connections.toml`
- Local connection override: `./connections.toml`

`<config-home>` resolves as follows:

- `$XDG_CONFIG_HOME` when it is set to an absolute path
- `~/.config` on macOS when `XDG_CONFIG_HOME` is unset
- the platform user config directory on other platforms when `XDG_CONFIG_HOME` is unset

Files are loaded in global-then-local order. Later files override earlier values. For named connections, a local `connections.toml` can add brand new connections and override individual fields on an existing connection of the same name.

Kitchen-sink sample files live at `examples/config/sqlcery.toml` and `examples/config/connections.toml`.

`sqlcery.toml` currently supports a single default connection target:

```toml
connection = "analytics"
```

That value can be either a named connection from `connections.toml` or a direct connection string. A CLI argument always wins over `sqlcery.toml`. See `examples/config/sqlcery.toml` for a commented sample.

#### Connection Definitions

Named connections live under `[connection.<name>]` and must declare `type = "sqlite"`, `"postgres"`, or `"mysql"`.

SQLite requires a database path:

```toml
[connection.local]
type = "sqlite"

[connection.local.sqlite]
database = "tmp/sqlcery.db"
```

PostgreSQL and MySQL require `host`, `port`, `database`, and `username`. `password` is optional.

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

SQLcery also still accepts the older flat fields on `[connection.<name>]`:

- SQLite: `database`
- PostgreSQL/MySQL: `host`, `port`, `database`, `username`, `password`

The nested `[connection.<name>.sqlite]`, `[connection.<name>.postgres]`, and `[connection.<name>.mysql]` tables are preferred because they match the current typed config model. See `examples/config/connections.toml` for a fuller sample that includes SQLite, PostgreSQL, MySQL, lifecycle settings, SSH tunneling, and legacy flat fields.

You can also skip `connections.toml` entirely and connect directly from the CLI or `sqlcery.toml` with a connection string. The current parser supports:

- `postgres://...` and `postgresql://...`
- `mysql://...`
- `sqlite:tmp/sqlcery.db`
- `sqlite:///:memory:`

#### SSH Tunnels

SSH tunneling is supported for PostgreSQL and MySQL connections through `ssh_host`:

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

- `ssh_host` selects the SSH jump host; the database `host` and `port` stay the remote database address dialed through that tunnel
- `ssh_host` is not supported for SQLite connections
- SQLcery resolves `ssh_host` against `~/.ssh/config`; if no matching alias exists, it uses the value as the SSH hostname directly
- The current SSH config parser reads `Host`, `HostName`, `User`, `Port`, `IdentityFile`, `UserKnownHostsFile`, and `StrictHostKeyChecking`
- Authentication comes from `SSH_AUTH_SOCK` when an SSH agent is available and from any resolved `IdentityFile` entries
- Host key verification uses `UserKnownHostsFile` values from SSH config, or falls back to `~/.ssh/known_hosts`; if no known-hosts file exists, the tunnel fails unless `StrictHostKeyChecking` is set to `no`, `off`, or `false`

## Go Architecture

SQLcery uses a small layered Go design: `cmd/sqlcery` stays thin, the application flow lives in `internal/app`, and infrastructure concerns are pushed into focused support packages.

The current startup and execution flow is:

1. `cmd/sqlcery` resolves the working directory and CLI target, loads the chosen connection, opens a `db.SQLAdapter`, creates persistent history, and starts the Bubble Tea program.
2. `internal/config` loads layered TOML config from XDG paths plus the current working directory, validates it, and resolves named connections or direct connection strings.
3. `internal/db` handles dialect-aware database access, including connection opening, lifecycle settings, optional SSH tunneling, metadata lookup, and normalized query results.
4. `internal/app` owns the interactive model and shared state for command mode, history search, autocomplete, slash commands, query execution, and record viewer behavior.
5. `internal/history` persists session history and audit entries, while `internal/export` writes result sets to CSV, TSV, JSON, and Markdown.

### Package Layout

- `cmd/sqlcery`: CLI entrypoint and dependency wiring.
- `internal/app`: the live Bubble Tea application, including shared app state, SQL assistance, slash-command flows, and record viewer logic.
- `internal/config`: config loading, connection parsing, validation, and SSH config helpers.
- `internal/db`: adapters, dialects, metadata introspection, lifecycle management, tunneling, and result normalization.
- `internal/history`: in-memory session history plus persistent JSON log writing and rotation.
- `internal/export`: file export path validation and format-specific writers.
- `internal/tui`: reserved for future presentation-only UI helpers; it stays as a placeholder until reusable widgets or styling helpers can be extracted cleanly from `internal/app`.
- `testdata`: shared test assets and fixtures.

This keeps the terminal UI dependent on narrow package interfaces instead of letting configuration, storage, and SQL dialect details spread across the app model.

For now, the project intentionally keeps the live Bubble Tea UI in `internal/app` instead of moving it into `internal/tui`. The current command editor, history search, record viewer, and query execution flow all share one stateful update loop, so splitting packages now would add churn without reducing coupling.

## Usage

```sh
$ sqlcery [connection name]
```
