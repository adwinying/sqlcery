# ADR 0020: Append raw TOML fragments when writing connections.toml

## Status

Accepted

## Context

The New Connection Wizard needs to persist a new named Connection to
`connections.toml`. Until now the config layer (`internal/config`) has been
read-only — `LoadConnections` decodes the file, but nothing in the application
writes to it. The wizard is the first in-app write path.

The file is hand-edited by users: the sample `connections.toml` in the repo
root shows comments, careful ordering, and per-type sections. A TOML encoder
that round-trips a decoded struct back to text would mangle this — comments are
lost, tables are re-sorted, and formatting normalised. Users who maintain
`connections.toml` by hand would see their file rewritten out from under them
the first time they use the wizard.

There were two honest approaches:

- **Option A — read-modify-write with a TOML encoder.** Decode the target file
  into a `Connections` struct, insert or replace the new entry, re-encode, and
  write the whole file back. Correct and complete (can update *and* create),
  but requires a round-tripping encoder and destroys human-authored content
  (comments, ordering, blank-line grouping) on every write.
- **Option B — append a raw TOML fragment.** Read the file (or create it if
  absent), append a single well-formed `[connection.<name>]\n…` table block at
  the end with proper string escaping, and write it back. The existing file
  content is preserved byte-for-byte; only the new table is appended.

## Decision

Use **Option B — append a raw TOML fragment** — for the New Connection Wizard's
write path. The wizard:

1. Resolves the target path via the existing `DiscoverConnectionPaths` (global
   or project-local, per the user's save-location choice).
2. `MkdirAll`s the parent directory if it does not exist (a first-time user may
   have no `~/.config/sqlcery/` yet).
3. Reads the existing file content (empty if absent); ensures it ends with a
   newline, and inserts a blank line before the new table when the file already
   holds records (so entries stay visually separated).
4. Appends a `[connection.<name>]\n` table block with the new Connection's
   fields, using a minimal TOML-string-escape helper for string values (host,
   database, username, password, ssh_host). Integer fields (`port`) render
   bare. Optional/empty fields are omitted from the block so the written entry
   stays clean.
5. Writes the full content back.

### Table-key quoting

The Connection name is itself a TOML table key (`[connection.<name>]`), and the
key obeys stricter rules than string values. A bare key may only contain
`[A-Za-z0-9_-]`. Names the wizard routinely produces violate that: the DSN
mode's derived default is `<database>@<host>` (the `@` is not bare-safe), and a
user may legitimately type a name with spaces or dots. Writing
`[connection.orders@h]` produces TOML that will not decode.

The writer therefore branches on a bare-key predicate:

- If the name matches `^[A-Za-z0-9_-]+$`, emit a bare key: `[connection.orders]`.
- Otherwise, emit a quoted, escaped key reusing the same string-escape helper
  applied to values: `[connection."orders@h"]`, `[connection."my db"]`,
  `[connection."a.b"]` (the dot inside quotes is *not* treated as a key
  separator).

The escape helper thus applies to both string values **and** the table key.
Simple names stay readable as bare keys; anything else is quoted so the output
is always valid TOML that decodes back to the same name.

The wizard's name-collision check (against the loaded `Connections`) runs
*before* the write, guaranteeing no duplicate `[connection.<name>]` table is
appended.

The wizard is create-only in its first iteration. Editing an existing
Connection — which *does* require read-modify-write to replace a table block
in place — is deferred to a future flow and is not covered by this decision.

## Consequences

- The file's human-authored content (comments, ordering, grouping) is
  preserved across wizard writes. A future reader who finds the wizard
  appending rather than re-encoding should read this ADR, not "fix" it by
  switching to a full encoder.
- The written entry always lands at the end of the file. The existing loader
  reads the file regardless of table order, so this is cosmetically suboptimal
  but functionally correct.
- A minimal TOML-string-escape helper is introduced in `internal/config`. It
  must correctly handle quotes, backslashes, and control characters in
  passwords and other string fields — getting this wrong would produce
  unparseable TOML. It is applied both to string values and (via the bare-key
  predicate) to the table key, so a name containing `@`, spaces, or dots is
  written as a quoted key rather than an invalid bare one. It is unit-tested
  against the existing decoder to guarantee round-trip safety, with cases
  covering both the bare-key and quoted-key paths.
- The create-only scope means duplicate-name prevention is purely a
  pre-write check, not a structural guarantee of the write itself. A future
  edit flow that rewrites a table block will need read-modify-write and is not
  served by the append primitive; that flow will need its own decision.
- `connectionsLoader` (currently a startup snapshot closure) must be able to
  surface the newly-appended entry after the wizard pops. It is **not** made a
  live per-call disk read: `connectionsLoader` is invoked once per visible row
  on every picker render frame (for the summary and colour swatch), so a live
  read would re-stat, re-decode, and re-validate the file N times per frame.
  Instead the loader keeps returning an in-memory cached `Connections` value,
  and a separate `reloadConnections func() error` trigger re-reads disk once and
  refreshes the cache. The write-success handler calls it before rebuilding the
  picker candidate list. This keeps render IO-free while guaranteeing freshness
  exactly when it is needed. The reload trigger is threaded through
  `RunOptions` → `Model` alongside the existing loader, a small wiring change in
  `cmd/sqlcery/main.go`.
