# ADR 0011: Status Bar combines Hints Bar and Status Line

## Status

Accepted

## Context

The TUI previously occupied two lines at the bottom of the screen:

1. **Hints Bar** — persistent strip showing the running indicator, connection name, and keybind hints for the active context.
2. **Status Line** — transient strip showing feedback from the last action ("Executed in 23ms", error text, modal navigation guidance).

Two problems motivated a change:

- The Status Line was permanent: a stale message from five minutes ago would still be on screen unless a new action replaced it. This made the bottom of the TUI visually noisy.
- Two lines cost one row of content area that the panes could use instead.

## Decision

### 1. Merge into a single Status Bar

The two lines are replaced by one. Layout:

```
[notification |] hints <spacer> connection name
```

- **Notification** (optional, left): ephemeral feedback or the running indicator.
- **Hints** (middle): keybind strings from `FooterHints` for the active context, unchanged.
- **Connection name** (right-aligned): padded right with spaces so it sits flush at the terminal edge.

The `|` separator between notification and hints is only rendered when a notification is present. `statusBarHeight` drops from `2` to `1`; the content area gains one row.

### 2. Notifications carry an explicit severity level

A `NotificationLevel` type is introduced with three values: `Success`, `Info`, `Error`. Each `SetReady` / `SetPendingIntent` call site explicitly tags a level. The Status Bar renders the notification text in the corresponding colour — green, yellow, or red — using the existing `InfoNotice`, `WarningNotice`, and `ErrorNotice` theme styles (remapped to match the new semantics).

The running indicator occupies the notification slot during execution but carries no level and is rendered in the default footer colour. Running indicator and timed notification are mutually exclusive: when `interaction.Running != nil` the slot shows the spinner; otherwise it shows the most recent timed notification (if any).

### 3. Auto-clear with reset-on-update

Every new notification (re)starts a 3-second timer. When the timer fires, the notification is cleared. This applies uniformly to all severity levels, including errors. Modal navigation guidance (e.g. "Choose a table and press enter.") flows through the same mechanism: each navigation keypress emits a new notification, which resets the timer, keeping guidance visible during active interaction and clearing it naturally after a pause.

### 4. CONTEXT.md term changes

- "Hints Bar" and "Status Line" are retired.
- "Status Bar" replaces both.
- "Notification" is introduced as the ephemeral left-section concept.

## Consequences

- `statusBarHeight` becomes `1`. All code that hardcodes `2` must be updated.
- `statusDescriptionView()` and `tui.AppTheme.MetaLine` are removed.
- `m.state.Status string` is replaced by a `Notification` struct carrying `{ Text string; Level NotificationLevel; ExpiresAt time.Time }`.
- All `SetReady` / `SetPendingIntent` call sites gain a `NotificationLevel` parameter.
- `statusBarView()` renders the three-section layout: notification (styled by level), hints, right-aligned connection name.
- Adding a new action that produces feedback requires choosing a level at the call site — no ambient default.
