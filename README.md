# SQLcery

SQLcery (pronouced "sequel-cery") is a SQL client for the command line. It should be able to predict your next SQL command, and help you write it as if it was magic.

SQLcery takes a hybrid approach by providing:
- an intuitive interface for executing SQL commands
- a powerful record viewer for easy inspection of query results.

## Features

### Mutli-Database Support

SQLcery supports multiple databases. It can connect to a database using a connection string, or by specifying the connection name in the config file.

It supports the following database types:

- SQLite
- PostgreSQL
- MySQL
- more coming soon

### Command Line Mode

In command line mode, SQLcery reads SQL commands and executes them. It has built-in auto-completion and syntax highlighting.
The auto-completion is context-aware, so it will prioritize keywords and table names that are likely to be used in the current context.

#### SQL Assistance Strategy

SQLcery currently standardizes on `lightweight-tokenization` for command-line assistance, including autocomplete and upcoming slash-command SQL composition.

- Use lightweight token scanning to identify the current statement, clause, qualifier, and referenced tables near the cursor.
- Keep command composition schema-driven and explicit instead of parsing and rewriting arbitrary SQL entered by the user.
- Revisit a full SQL parser only when assistance must understand nested queries or CTEs, resolve aliases and derived tables, validate dialect grammar, or preserve semantics while rewriting existing SQL.

#### Slash Commands

`/commands` allows you to compose SQL commands using a wizard-style interface.

- `/tables` to list tables in the current database
- `/columns` to list columns in a table
- `/select` to select columns from a table
- `/insert` to insert rows into a table
- `/update` to update rows in a table
- `/delete` to delete rows from a table
- `/create` to create a table
- `/drop` to drop a table

#### History

Hitting `Ctrl-r` will bring up a list of recently executed commands that are searchable fzf style.

### Record Viewer Mode

When SELECT-ing data in Command Line Mode, SQLcery only displays the first 5 rows of the result.
To view all the results, hit `Ctrl-x` to enter Record Viewer Mode.

Record Viewer Mode displays the results in a table, with the first column being the primary key.
Primary keys are highlighted in bold and the column is frozen.

#### Commands

- Arrow keys/hjkl to navigate the table
- `Ctrl-d`/`Ctrl-u` to scroll down/up
- `cc` to edit the selected row. This will compose an UPDATE statement in Command Line Mode.
- `dd` to delete the selected row. This will compose a DELETE statement in Command Line Mode.
- `yy` to duplicate the selected row. This will compose a INSERT statement in Command Line Mode.
- `:w [filename]` to export the selected rows to a file in the current directory. Depending on the file extension, this will export to a CSV, TSV, JSON, or Markdown file.
- `space` to toggle select a row. This is useful when you want to perform an action on multiple rows.
- `Ctrl-x` to toggle between Record Viewer Mode and Command Line Mode.

### Auditing

All SQL commands executed in SQLcery are logged to `$XDG_DATA_HOME/sqlcery/history.log`. Each command is logged as a JSON object, with the following fields:

| Field | Description |
| --- | --- |
| `connection` | The connection name if specified, or database name if not set |
| `command` | The SQL command that was executed |
| `time` | The time the command was executed |
| `result` | The result of the command, if any |

### Configuration

Configuration is stored in `$XDG_CONFIG_HOME/sqlcery/sqlcery.toml`.
Configuration can be extended and/or overriden using a `sqlcery.toml` file in the current working directory.

Connections are separately defined in `$XDG_CONFIG_HOME/sqlcery/connections.toml`.
Connections can be extended and/or overriden using a `connections.toml` file in the current working directory.

Connection can be defined as follows:

```toml
[connection.mydb]
type = "postgres"
host = "localhost"
port = 5432
database = "mydb"
username = "root"
password = "password"

# if you need to connect via SSH. Will respect ~/.ssh/config
ssh_host = "mydb.example.com"
```

## Usage

```sh
$ sqlcery [connection name]
```
