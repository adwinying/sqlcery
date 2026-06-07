// Package app owns SQLcery's live Bubble Tea application.
//
// The interactive event loop, shared state, Command Pane, history search,
// Results Pane, and query execution coordination stay together here because
// they currently operate as one tightly coupled model. UI code should move into
// internal/tui only after reusable presentation-only components emerge.
package app
