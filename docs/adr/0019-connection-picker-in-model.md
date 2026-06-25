# ADR 0019: The Connection Picker lives inside the Model

## Status

Accepted

## Context

Launching `sqlcery` with no connection argument used to silently exit. We want it
to open a **Connection Picker** instead: a single-step, frecency-ordered list of
named Connections to choose from. The Picker must run when there is **no Session
and no Adapter yet** — which collides with two existing invariants: `app.Run`
rejects a nil Adapter, and the Model has always assumed a live Session for its
whole lifetime.

There were two honest ways to host it:

- **Option A — a separate pre-TUI program.** `main.go` runs a small standalone
  `tea.Program` that returns a chosen Connection, then the existing path runs
  unchanged (open Adapter → `app.Run`). The Model is never touched.
- **Option B — a new Model state.** `app.Run` launches even with no Adapter; the
  Model starts in a new `StateSelectConnection`, and selecting opens the Adapter
  asynchronously and transitions to `StateReady`.

Option A is cleaner in isolation — zero blast radius, the nil-Adapter invariant
stays intact. But mid-session connection switching is on the roadmap, and that is
fundamentally an *in-Model* concern: swapping the live Adapter while panes,
History, and Schema are already on screen. With Option A the Picker would be a
throwaway second app that could never grow into the mid-session switcher; we'd
build the selection UI twice.

## Decision

Host the Connection Picker **inside the Model (Option B)**. One shared selection
state (the *Connection Picker Context*, owned by the single `ModalConnectionPicker`
object) drives one Modal, rendered the same way in both situations it appears:

- **Startup** — the Model's initial state when no connection argument is given.
  Kept as its own `StateSelectConnection` (not collapsed into `StateReady`, which
  would make the app claim readiness with a nil Adapter); the modal renders over
  the empty pane frames.
- **Mid-run** — the same Modal overlaying the live panes, for switching Connections
  without restarting.

The two situations look identical and differ only in behaviour, gated on a
`startup` flag: empty-filter Esc **quits** at startup vs **closes**
non-destructively mid-run; an empty `connections.toml` adds a "create it and
restart" hint at startup; the hint bar reads `esc quit` vs `esc cancel`.

Selecting a Connection from the Picker fires an async open and **keeps the Modal
open** while connecting (a status-bar "Connecting…" indicator); on success the
Modal closes → `StateReady`, on failure the failed Connection is marked inline
(a `✗` row marker, persisting until the next attempt) with the detailed error in
the Status Bar. Only **auto-connect** from a CLI argument — which shows no Picker —
uses the full-screen `StateStartup` "Connecting…" screen. Mid-run switches are
**transactional**: the candidate Adapter is opened *before* the old one is closed,
so a failed switch leaves the working Session untouched; at startup there is no old
Adapter, so the same handler simply opens with nothing to close.

Connection ordering uses **Connection Frecency** — a zoxide-style decayed
open-counter persisted per name under `$XDG_DATA_HOME/sqlcery/`, written at the
"Adapter successfully opened" seam so direct CLI launches feed the ranking too. A
single decayed counter (decay-on-write, with an injected clock for deterministic
tests) was chosen over a stored timestamp list: the canonical algorithm, and the
lossiness is irrelevant at the scale of a handful of Connections.

## Consequences

- `app.Run`'s nil-Adapter guard is relaxed — an adapter-less Model is now the
  *normal* startup path, not an error. A future reader who finds the Model running
  without a Session should read this ADR, not "fix" it.
- History and the Audit Log connection key can no longer be constructed in
  `main.go` before the TUI starts (the connection name is unknown until the user
  picks). Their construction moves into the Model, after a successful open.
- The Model gains injected dependencies it never had: the `open` function and a
  connections loader, since selecting and re-listing now happen in-Model.
- The Picker is single-step *by design* — it is not a Wizard. Creating a new
  Connection (a genuinely multi-step flow) is deferred to a future New Connection
  Wizard, which would reuse the same in-Model hosting established here.
