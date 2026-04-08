// Package app owns SQLcery's live Bubble Tea application.
//
// The interactive event loop, shared state, command editor, history search,
// record viewer, and query execution coordination stay together here because
// they currently operate as one tightly coupled model. UI code should move into
// internal/tui only after reusable presentation-only components emerge.
package app
