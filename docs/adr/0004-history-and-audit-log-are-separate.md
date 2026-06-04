# ADR 0004: History and Audit Log are separate persistence mechanisms

## Status

Accepted

## Context

SQLcery needs to persist executed Statements for two distinct purposes:

- **Interactive recall** — the user presses `Ctrl-r` and fuzzy-searches recent Statements to restore one into the Command Pane. This is connection-scoped: when working against a given database, you want to recall what you ran against *that* database, not every Statement ever executed across all connections.
- **Audit trail** — a complete, ordered record of every Statement executed, across all Connections, for compliance, debugging, or external tooling. This is global: connection boundary is irrelevant.

These two use cases have incompatible access patterns. A single unified log optimized for audit (flat, append-only, cross-connection) is the wrong structure for interactive recall — the user would need to filter by connection on every `Ctrl-r` invocation. A per-connection history optimized for recall is the wrong structure for audit — there is no single file to tail, grep, or ingest.

## Decision

Maintain two separate persistence mechanisms:

- **History** — per-Connection (or per-Connection String). Persists across Sessions to the same Connection. Used exclusively for interactive recall via `Ctrl-r`.
- **Audit Log** — a single flat append-only JSON file at `$XDG_DATA_HOME/sqlcery/history.log`. Records every executed Statement across all Connections with connection name, statement text, timestamp, and result summary.

## Consequences

- `Ctrl-r` search is fast and relevant — no cross-connection noise.
- The Audit Log is a simple file that external tools (grep, jq, log shippers) can consume without understanding SQLcery internals.
- Two storage paths to maintain. They are not in sync by design — History can be cleared without affecting the Audit Log.
- The Audit Log and History are permanently separate; neither is intended to absorb the other.
