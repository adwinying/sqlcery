# ADR 0021: Connecting Modal replaces the StateStartup full-screen

## Status

Accepted

## Context

The auto-connect path — launched with a CLI connection argument — currently
shows a full-screen `StateStartup` view ("[ startup ] / Preparing command
mode… / Connecting to X…") while the async open is in flight. It is the only
place in the application where a blocking operation replaces the entire UI
rather than composing on top of it. Every other blocking flow (mid-run
connection switch, reconnect-in-progress, statement execution) works inside
the normal pane layout.

Two options were considered:

- **Keep `StateStartup`** — improve the text or hints, but leave the
  full-screen takeover in place.
- **Replace with a Connecting Modal** — push a compact overlay modal over the
  (empty) panes, matching the visual language every other blocking dialog uses.

The full-screen approach is the odd one out: it bypasses the modal stack,
requires a dedicated render branch in the `View` switch, and creates a
divergent interaction model (Esc behaviour, hint layout, and key handling all
differ from every other modal). The `StateSelectConnection` state already
renders the Connection Picker Modal over empty panes at startup — the
Connecting Modal is the same idea applied to the connecting phase.

## Decision

Replace the `StateStartup` full-screen view with a **Connecting Modal** pushed
onto the modal stack while the auto-connect open is in flight. The application
stays in `StateSelectConnection` throughout (there is still no Session), and
the panes behind the modal are empty as they already are during the startup
Connection Picker.

The modal:
- Uses the standard two-box Modal frame with compact `DialogRows` height (same
  as `modalConfirm`).
- Displays the target connection name centred in the body, with a spinner and
  a single `[ Cancel ]` button.
- On **Cancel / Esc / Enter**: cancels the in-flight open and quits the
  application. The user launched with an explicit argument; aborting means they
  want out, not the Connection Picker.
- On **ctrl+c**: quits immediately (unchanged from current behaviour).
- On **connect failure** (non-user-aborted): dismisses the modal and drops
  into the startup Connection Picker with the failure marked and the error in
  the Status Bar — same destination as today.

`StateStartup` is retired. The initial `SharedAppState` no longer needs it as
a default; a future reader who encounters `StateSelectConnection` at startup
should read ADR 0019, not "fix" it.

The mid-run connecting path is **not changed**: the Connection Picker Modal
stays open and "Connecting to X…" appears in the Status Bar as a
Notification, as before.

## Consequences

- `StateStartup` and `SetStartup` are removed. The `StateStartup` branch in
  the `View` switch is deleted. `NewSharedAppState` no longer defaults to it.
- `handlePickerConnect` no longer calls `m.state.SetStartup`. Instead it stays
  in `StateSelectConnection` and pushes a `ModalConnecting`.
- `handleMidRunConnectingKeyPress` (the auto-connect key handler while
  connecting) is simplified: the double-Esc arm-then-cancel sequence is
  replaced by a single Esc that cancels and quits. The `pendingConnectAbort`
  flag and its associated Status Bar hint are removed for the auto-connect
  path.
- `handlePickerConnectFailed`'s `context.Canceled` branch no longer tries to
  drop into the Picker — it quits instead, consistent with Cancel behaviour.
- The Connecting Modal is a normal `Modal` implementation; it gains a
  `ModalConnecting` tag in `AppModal`. All existing modal infrastructure
  (push/pop stack, hint bar, key routing) composes for free.
