# ADR 0001: Semicolon as statement completion signal

## Status

Accepted

## Context

SQLcery is a multi-line terminal REPL. When the user presses `Enter`, the editor must decide whether to submit the current input as a Statement or insert a newline for continued editing.

The main alternatives considered:

- **Ctrl-Enter** — an explicit two-key submit chord. Common in GUI clients (TablePlus, DBeaver). Awkward on terminals where Ctrl-Enter is not universally reliable across terminal emulators.
- **Parse-based detection** — infer completeness by parsing the SQL AST. No delimiter required. High complexity: requires a full SQL parser per Dialect, with grammar quirks across SQLite, PostgreSQL, and MySQL.
- **Single Enter submits** — every Enter submits. Simple, but breaks multi-line editing entirely.

## Decision

A Statement is complete when it ends with a semicolon (`;`). `Enter` submits only when the trailing semicolon is present; otherwise it inserts a newline.

## Consequences

- Multi-line SQL editing works naturally — the user presses `Enter` freely until they append `;` to submit.
- The implementation is simple: a string suffix check, with no per-dialect parser dependency.
- Semicolon is the SQL standard statement terminator, so the behavior is immediately familiar to any SQL user.
- Users must always type a trailing semicolon; there is no shortcut to submit without one. This is consistent with `psql`, `mysql`, and other CLI clients.
