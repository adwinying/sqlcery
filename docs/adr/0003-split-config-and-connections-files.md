# ADR 0003: Split app config and connection credentials into separate files

## Status

Accepted

## Context

SQLcery needs two categories of configuration:

- **App settings** — e.g. the default connection to open. Non-sensitive. A project team might want to commit these to a repo so that running `sqlcery` from the project directory opens the right database by default.
- **Connection credentials** — host, port, username, password, SSH keys. Sensitive. Should never be committed to a repo.

The alternative is a single merged config file (`sqlcery.toml`) containing both. This is simpler on the surface, but it forces a choice: either commit the file (leaking credentials) or never commit it (losing the ability to share app settings through version control).

## Decision

Split configuration into two files:

- `sqlcery.toml` — app settings only (e.g. `connection = "analytics"`). No credentials. Safe to commit.
- `connections.toml` — named connection definitions including credentials. Should be git-ignored in project directories.

Both files support a global-then-local layering: a file in `<config-home>/sqlcery/` is loaded first, then a file in the current working directory overrides it. This applies independently to each file.

## Consequences

- A project can commit `./sqlcery.toml` to set a default connection without exposing credentials.
- `./connections.toml` should be added to `.gitignore` in any project that uses it.
- The two-file structure looks arbitrary without this context — this ADR is the explanation.
- Users managing only personal machines (never committing config) gain no benefit from the split, but are not harmed by it.
