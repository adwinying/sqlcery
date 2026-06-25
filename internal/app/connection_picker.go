package app

import (
	"context"
	"fmt"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/adwinying/sqlcery/internal/config"
	apphistory "github.com/adwinying/sqlcery/internal/history"
	"github.com/adwinying/sqlcery/internal/tui"
)

// pickerLoadConnectionsCmd loads and frecency-orders Connection names.
func (m Model) pickerLoadConnectionsCmd() tea.Cmd {
	connectionsLoader := m.connectionsLoader
	frecencyStore := m.frecencyStore
	return func() tea.Msg {
		var names []string
		if connectionsLoader != nil {
			connections, err := connectionsLoader()
			if err == nil {
				for name := range connections.Connection {
					names = append(names, name)
				}
			}
		}
		sort.Strings(names) // stable base order before frecency
		if frecencyStore != nil {
			names = frecencyStore.Order(names)
		}
		return pickerConnectionsLoadedMsg{names: names}
	}
}

func (m Model) handlePickerConnectionsLoaded(msg pickerConnectionsLoadedMsg) (Model, tea.Cmd) {
	m.picker.Candidates = msg.names
	m.picker.Selected = 0
	m.picker.Filter = ""
	m.picker.ConnectError = ""
	return m, nil
}

// handlePickerConnect fires an async open attempt.
func (m Model) handlePickerConnect(msg pickerConnectMsg) (Model, tea.Cmd) {
	m.picker.PendingAbort = false
	m.picker.ConnectError = ""

	// Transition to StateStartup to show a connecting indicator.
	m.state.SetStartup("Connecting to " + pickerConnectionDisplayName(msg.resolved) + "...")

	openFn := m.open
	resolved := msg.resolved

	ctx, cancel := context.WithCancel(context.Background())
	m.cancelConnect = cancel

	return m, func() tea.Msg {
		adapter, err := openFn(ctx, resolved.Connection)
		if err != nil {
			return pickerConnectFailedMsg{err: err}
		}
		return pickerConnectSuccessMsg{adapter: adapter, resolved: resolved}
	}
}

// handlePickerConnectSuccess wires the session, history, schema, and frecency.
func (m Model) handlePickerConnectSuccess(msg pickerConnectSuccessMsg) (Model, tea.Cmd) {
	if m.cancelConnect != nil {
		m.cancelConnect()
		m.cancelConnect = nil
	}
	m.picker.PendingAbort = false

	// Build the session.
	m.session = Session{
		ConnectionName:  msg.resolved.Name,
		DatabaseType:    msg.resolved.Connection.Type,
		ConnectionColor: msg.resolved.Connection.Color,
		WorkingDir:      m.session.WorkingDir,
		Adapter:         msg.adapter,
		MouseDisabled:   m.session.MouseDisabled,
	}

	// Record frecency for named connections only.
	if msg.resolved.Name != "" && m.frecencyStore != nil {
		_ = m.frecencyStore.RecordOpen(msg.resolved.Name)
	}

	// Build history.
	var history *apphistory.History
	if m.newHistory != nil {
		h, err := m.newHistory(msg.resolved.Name)
		if err == nil {
			history = h
		} else {
			history = apphistory.NewHistory()
		}
	} else {
		history = apphistory.NewHistory()
	}
	m.history = history
	m.syncHistorySnapshot()

	// Transition to Ready.
	m.state.SetReady("", NotificationNone)

	// Kick off command mode init + schema introspection.
	adapter := msg.adapter
	loader := m.loader

	return m, tea.Batch(
		m.command.Init(),
		func() tea.Msg {
			schema, err := loader(context.Background(), adapter)
			if err != nil {
				return pickerSchemaReadyMsg{schema: nil}
			}
			return pickerSchemaReadyMsg{schema: schema}
		},
	)
}

// handlePickerConnectFailed returns to the Picker with the error shown inline.
func (m Model) handlePickerConnectFailed(msg pickerConnectFailedMsg) (Model, tea.Cmd) {
	if m.cancelConnect != nil {
		m.cancelConnect()
		m.cancelConnect = nil
	}
	m.picker.PendingAbort = false

	errText := FormatTerminalError(msg.err)
	if strings.Contains(msg.err.Error(), "context canceled") {
		// Aborted by user — return silently without error.
		errText = ""
	}
	m.picker.ConnectError = errText
	m.state.App.Current = StateSelectConnection
	return m, nil
}

// handlePickerKeyPress handles keys while in StateSelectConnection.
func (m Model) handlePickerKeyPress(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		// At startup with no adapter, Esc quits (nothing to return to).
		return m, tea.Quit
	case "enter":
		return m.pickerSelect()
	case "up", "ctrl+p":
		return m.pickerMove(-1)
	case "down", "ctrl+n":
		return m.pickerMove(1)
	case "backspace", "ctrl+h", "delete":
		m.picker.Filter = trimLastRune(m.picker.Filter)
		m.picker.Selected = 0
		return m, nil
	case "space":
		m.picker.Filter += " "
		m.picker.Selected = 0
		return m, nil
	default:
		if len(msg.Text) > 0 && !msg.Mod.Contains(tea.ModAlt) && !msg.Mod.Contains(tea.ModCtrl) {
			m.picker.Filter += msg.Text
			m.picker.Selected = 0
			return m, nil
		}
	}
	return m, nil
}

// handleConnectingKeyPress handles keys while a connect is in flight (StateStartup from Picker).
func (m Model) handleConnectingKeyPress(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		if m.picker.PendingAbort {
			// Second Esc: cancel the in-flight connect and return to Picker.
			if m.cancelConnect != nil {
				m.cancelConnect()
				// cancelConnect will cause pickerConnectFailedMsg with context.Canceled
				// which returns to the Picker.
			}
			m.picker.PendingAbort = false
			return m, nil
		}
		// First Esc: arm the pending abort.
		m.picker.PendingAbort = true
		return m, nil
	default:
		// Any other key disarms the pending abort.
		m.picker.PendingAbort = false
		return m, nil
	}
}

// pickerSelect selects the currently highlighted connection and triggers a connect.
func (m Model) pickerSelect() (tea.Model, tea.Cmd) {
	filtered := pickerFilteredCandidates(m.picker.Candidates, m.picker.Filter)
	if len(filtered) == 0 {
		return m, nil
	}
	idx := wrapSelection(m.picker.Selected, len(filtered))
	name := filtered[idx]

	var conn config.Connection
	if m.connectionsLoader != nil {
		connections, err := m.connectionsLoader()
		if err == nil {
			conn = connections.Connection[name]
		}
	}

	resolved := config.ResolvedConnection{
		Name:       name,
		Raw:        name,
		Connection: conn,
	}
	return m, func() tea.Msg { return pickerConnectMsg{resolved: resolved} }
}

func (m Model) pickerMove(delta int) (tea.Model, tea.Cmd) {
	filtered := pickerFilteredCandidates(m.picker.Candidates, m.picker.Filter)
	if len(filtered) == 0 {
		return m, nil
	}
	m.picker.Selected = wrapSelection(m.picker.Selected+delta, len(filtered))
	return m, nil
}

// pickerView renders the full-screen Connection Picker.
func (m Model) pickerView(contentHeight int) string {
	filtered := pickerFilteredCandidates(m.picker.Candidates, m.picker.Filter)

	lines := []string{
		tui.AppTheme.PanelTitle.Render("[ connection picker ]"),
	}

	// Filter box.
	filterText := m.picker.Filter + "█"
	lines = append(lines, tui.AppTheme.PanelText.Render("Filter: "+filterText))
	lines = append(lines, "")

	switch {
	case len(m.picker.Candidates) == 0:
		lines = append(lines, tui.AppTheme.PanelMuted.Render("No connections defined."))
		lines = append(lines, "")
		lines = append(lines, tui.AppTheme.PanelMuted.Render("Define connections in connections.toml and restart."))
	case len(filtered) == 0:
		lines = append(lines, tui.AppTheme.PanelMuted.Render("No fuzzy matches."))
	default:
		// Viewport: show up to 16 rows.
		const maxRows = 16
		selected := wrapSelection(m.picker.Selected, len(filtered))
		vpStart := lazyScroll(selected, 0, maxRows)
		vpEnd := vpStart + maxRows
		if vpEnd > len(filtered) {
			vpEnd = len(filtered)
		}

		availWidth := m.width - 4
		if availWidth < 20 {
			availWidth = 20
		}

		for i := vpStart; i < vpEnd; i++ {
			name := filtered[i]
			row := pickerRenderRow(name, m.connectionsLoader, availWidth)
			if i == selected {
				if pad := availWidth - ansi.StringWidth(row); pad > 0 {
					row += strings.Repeat(" ", pad)
				}
				lines = append(lines, tui.AppTheme.PanelSelected.Render(row))
			} else {
				lines = append(lines, tui.AppTheme.PanelText.Render(row))
			}
		}
	}

	// Error display.
	if m.picker.ConnectError != "" {
		lines = append(lines, "")
		lines = append(lines, tui.AppTheme.NotificationError.Render("Error: "+m.picker.ConnectError))
	}

	_ = contentHeight
	return strings.Join(lines, "\n")
}

// pickerFilteredCandidates applies the fuzzy filter and returns matching names.
func pickerFilteredCandidates(candidates []string, filter string) []string {
	trimmed := strings.TrimSpace(filter)
	if trimmed == "" {
		return candidates
	}
	result := make([]string, 0, len(candidates))
	for _, name := range candidates {
		if _, ok := fuzzyMatch(trimmed, name); ok {
			result = append(result, name)
		}
	}
	return result
}

// pickerRenderRow renders a single connection row: name + dimmed credential-free target summary.
func pickerRenderRow(name string, loader func() (config.Connections, error), maxWidth int) string {
	summary := ""
	if loader != nil {
		if connections, err := loader(); err == nil {
			if conn, ok := connections.Connection[name]; ok {
				summary = pickerConnectionSummary(conn)
			}
		}
	}

	if summary == "" {
		return name
	}

	// Render as "name  summary" with summary dimmed.
	const minPad = 2
	nameWidth := ansi.StringWidth(name)
	summaryWidth := ansi.StringWidth(summary)
	available := maxWidth - nameWidth - minPad
	if available < summaryWidth {
		// Truncate summary to fit.
		if available <= 0 {
			return name
		}
		summary = ansi.Truncate(summary, available, "…")
		summaryWidth = ansi.StringWidth(summary)
	}

	pad := maxWidth - nameWidth - summaryWidth
	if pad < minPad {
		pad = minPad
	}
	return name + strings.Repeat(" ", pad) + tui.AppTheme.PanelMuted.Render(summary)
}

// pickerConnectionSummary builds a credential-free one-line summary of a connection.
// Format: "<type> <host>:<port>/<db>" or "sqlite <path>"
// Username and password are NEVER included.
func pickerConnectionSummary(conn config.Connection) string {
	switch conn.Type {
	case "sqlite":
		db := conn.Database
		if db == "" {
			return "sqlite"
		}
		return fmt.Sprintf("sqlite %s", db)
	case "postgres", "mysql":
		host := conn.Host
		if host == "" {
			return conn.Type
		}
		if conn.Port > 0 {
			host = fmt.Sprintf("%s:%d", host, conn.Port)
		}
		if conn.Database != "" {
			return fmt.Sprintf("%s %s/%s", conn.Type, host, conn.Database)
		}
		return fmt.Sprintf("%s %s", conn.Type, host)
	default:
		return conn.Type
	}
}

// pickerConnectionDisplayName returns a short display name for a resolved connection.
func pickerConnectionDisplayName(resolved config.ResolvedConnection) string {
	if resolved.Name != "" {
		return resolved.Name
	}
	if resolved.Raw != "" {
		// Strip credentials from connection strings for display.
		return resolved.Connection.Type
	}
	return "database"
}
