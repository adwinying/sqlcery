# ADR 0002: Defer extraction of presentation layer into internal/tui

## Status

Superseded by [ADR-0005](0005-internal-tui-widget-layer.md)

## Context

SQLcery's TUI is built with Bubble Tea. Presentation concerns — pane rendering, styling, reusable widgets — will eventually live in `internal/tui`, separate from application logic in `internal/app`.

The alternatives for timing:

- **Split now** — move presentation into `internal/tui` immediately and maintain the boundary from the start.
- **Defer until a clean seam appears** — keep everything in `internal/app` until a widget or styling helper has a stable, reusable interface that justifies a separate package.

The current obstacle to splitting now: the Command Pane editor, History Search modal, Results Pane, and query execution flow all share one Bubble Tea `Update` loop and a single stateful model. Drawing a package boundary across that shared state would create artificial seams and add refactor churn without reducing coupling.

## Decision

Keep the live Bubble Tea UI in `internal/app` for now. Create `internal/tui` as a placeholder to signal intent. Extract presentation concerns into `internal/tui` only when a reusable widget or styling helper has a clear, stable interface — as a planned refactor.

## Consequences

- No premature package boundary across tightly coupled state.
- `internal/tui` exists as an explicit signal that the split is planned, not abandoned.
- Future contributors know where presentation helpers should land when the refactor happens.
- Until the extraction occurs, `internal/app` remains larger than its name suggests — it owns both application logic and all rendering.
