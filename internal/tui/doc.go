// Package tui contains presentation-only terminal UI helpers shared across
// the application layer.
//
// It provides:
//   - Color resolution (ResolveColor) for named, ANSI, and hex colors
//   - The application theme (AppTheme) with lipgloss styles for every UI element
//   - Modal overlay rendering (RenderModal, OverlayCenter, OverlayLine)
//
// The live Bubble Tea application remains in internal/app, which imports this
// package for all styling and overlay primitives.
package tui
