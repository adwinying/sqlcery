# ADR 0005: Establish internal/tui as the widget layer

## Status

Accepted

## Context

ADR-0002 deferred extraction of presentation concerns into `internal/tui` until a clean seam appeared. That seam emerged: `ResultsPane` and `EditorWidget` were extracted as reusable widgets with stable interfaces (resolved in the issues that close ADR-0002).

Four decisions made during the extraction need to be recorded so future contributors know where things belong and why.

## Decision

### 1. Widget-layer boundary

`internal/tui` owns:

- Color and theme primitives — stateless, shared across the whole UI.
- Stateless rendering functions — pure functions of their inputs (e.g. `PrepareResultsPanePage`, `RenderPreparedResultsPanePage`).
- Widgets — types that carry only **cosmetic state**: scroll offsets, suggestion-selection indices, transcript buffers. No application state.

`internal/app` owns:

- `InteractionState` and all types that compose it — these are application state types.
- Orchestration, event handling, business logic, query execution.
- Mapping from `InteractionState` to the view contexts that widgets consume.

### 2. View context contract

Each widget in `internal/tui` defines a narrow `*ViewContext` struct (e.g. `EditorViewContext`, `ResultsPaneViewContext`). `internal/app` constructs one from `InteractionState` immediately before calling the widget's `View` method. The widget is then a deterministic function of its cosmetic state plus that context.

This keeps widgets testable in isolation — tests construct a `*ViewContext` directly without any application model — and keeps the package boundary clean: `internal/tui` never reaches into `InteractionState`.

### 3. Autocomplete computed outside the widget

`buildAutocompleteItems`, which resolves schema context, cursor position, and slash command candidates into a `[]AutocompleteSuggestion`, lives in `internal/app`. The computed list is passed to `EditorWidget` via `EditorViewContext.AutocompleteSuggestions`. The widget only knows how to display and navigate a pre-computed list; it does not compute it.

This prevents the widget from depending on schema types, slash command registries, or any other application-level concept. `AutocompleteSuggestion` is defined in `internal/tui` because it is a display type (label, kind, detail, insert text) — not a schema type.

### 4. internal/tui imports internal/db

`internal/tui` imports `internal/db` for `db.ResultSet`, `db.ResultRow`, `db.ResultColumn`, and `db.ResultValue`. Formatting tabular data — rendering NULL, truncating newlines, resolving empty column names, formatting timestamps — is a rendering concern. This import does not give `internal/tui` ownership of any persistence or query logic.

The permitted dependency direction is:

```
internal/app  →  internal/tui  →  internal/db
```

`internal/tui` must not import `internal/app` or `internal/db/adapter`.

### 5. InteractionState stays in internal/app

`InteractionState` tracks pane focus, modal state, running statement context, autocomplete schema, marked rows, layout, and history. It is an application state type. Moving it into `internal/tui` would give the presentation package ownership of application semantics and invert the dependency direction.

`internal/app` owns `InteractionState` and derives view contexts from it before each render. View contexts are one-way: they carry data out of `internal/app` into `internal/tui`, never back.

## Consequences

- Widget rendering is deterministic: given the same cosmetic state and view context, `View()` always produces the same string. Straightforward to unit-test.
- Adding a new widget (e.g. an Export Wizard modal) requires defining a `*ViewContext` in `internal/tui` and a mapper in `internal/app`. `InteractionState` may gain new fields to carry the new widget's app-level state, but the widget itself has no access to `InteractionState`.
- The `internal/tui` → `internal/db` import is deliberate and narrow. Reviewers should reject attempts to import `internal/app` from `internal/tui`.
- All schema-resolution and autocomplete logic stays in `internal/app`, keeping the widget layer ignorant of database concepts beyond `db.ResultSet` display.
