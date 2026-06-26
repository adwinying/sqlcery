package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/adwinying/sqlcery/internal/config"
	apphistory "github.com/adwinying/sqlcery/internal/history"
	"github.com/adwinying/sqlcery/internal/tui"
)

// handlePickerInit pushes the startup Connection Picker Modal — the same Modal
// shown mid-run, in startup mode — with the frecency-ordered candidate list.
func (m Model) handlePickerInit() (Model, tea.Cmd) {
	m.pushModal(&connectionPickerModal{
		startup:           true,
		connectionsLoader: m.connectionsLoader,
		candidates:        loadPickerCandidates(m.connectionsLoader, m.frecencyStore),
	})
	return m, nil
}

// handlePickerConnect fires an async open attempt for the auto-connect path
// (a CLI connection argument). It shows the full-screen StateStartup
// "Connecting…" indicator — there is no Picker Modal to keep open here, because
// the user bypassed the Picker by naming a target. Picker-initiated connects go
// through handleMidRunConnect instead (which keeps the Modal open).
func (m Model) handlePickerConnect(msg pickerConnectMsg) (Model, tea.Cmd) {
	m.pendingConnectAbort = false

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
	m.pendingConnectAbort = false

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

// handlePickerConnectFailed handles a failed auto-connect open. A bare
// Connection String arg with no Picker candidates quits with the error (the
// Picker would be useless). Otherwise — including a named-arg failure or a
// double-Esc abort — it drops into the startup Connection Picker, marking the
// failed Connection with ! and surfacing the detail in the Status Bar.
func (m Model) handlePickerConnectFailed(msg pickerConnectFailedMsg) (Model, tea.Cmd) {
	if m.cancelConnect != nil {
		m.cancelConnect()
		m.cancelConnect = nil
	}
	m.pendingConnectAbort = false

	candidates := loadPickerCandidates(m.connectionsLoader, m.frecencyStore)

	// Aborted via double-Esc during auto-connect: drop into the Picker silently.
	if errors.Is(msg.err, context.Canceled) {
		return m.dropIntoStartupPicker(candidates, "", "", NotificationNone)
	}

	errText := FormatTerminalError(msg.err)

	// A bare Connection String arg has nothing to pick — print and quit.
	if m.autoConnectTarget.Connection.Type != "" && len(candidates) == 0 {
		m.state.App.Current = StateError
		m.state.App.Error = errText
		return m, tea.Quit
	}

	// A named-arg failure drops into the Picker with the failure marked.
	return m.dropIntoStartupPicker(candidates, m.autoConnectTarget.Name, "Connection failed: "+errText, NotificationError)
}

// dropIntoStartupPicker pushes the startup Connection Picker Modal after an
// auto-connect failure, optionally marking failedName with ! and setting the
// Status Bar message.
func (m Model) dropIntoStartupPicker(candidates []string, failedName, status string, level NotificationLevel) (Model, tea.Cmd) {
	m.pushModal(&connectionPickerModal{
		startup:              true,
		connectionsLoader:    m.connectionsLoader,
		candidates:           candidates,
		lastFailedConnection: failedName,
	})
	m.state.App.Current = StateSelectConnection
	m.state.App.Error = ""
	m.state.App.Reconnect = nil
	if strings.TrimSpace(status) == "" {
		m.state.Notification = Notification{}
	} else {
		m.state.Notification = Notification{Text: status, Level: level, CreatedAt: time.Now()}
	}
	return m, m.notificationClearCmdIfSet()
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

// pickerRenderRow renders a single connection row: colour swatch (when set) +
// name + dimmed credential-free target summary.
func pickerRenderRow(name string, loader func() (config.Connections, error), maxWidth int) string {
	summary := ""
	color := ""
	if loader != nil {
		if connections, err := loader(); err == nil {
			if conn, ok := connections.Connection[name]; ok {
				summary = pickerConnectionSummary(conn)
				color = strings.TrimSpace(conn.Color)
			}
		}
	}

	// Build the name part, always prefixed with a colour swatch slot.
	// Filled square (■) when a color is set; muted outline square (□) otherwise.
	var swatch string
	if color != "" {
		swatch = lipgloss.NewStyle().Foreground(tui.ResolveColor(color)).Render("■")
	} else {
		swatch = tui.AppTheme.PanelMuted.Render("□")
	}
	namePart := swatch + " " + name
	nameWidth := ansi.StringWidth(name) + 2 // swatch (1) + space (1)

	if summary == "" {
		return namePart
	}

	// Render as "[swatch ]name  summary" with summary dimmed.
	const minPad = 2
	summaryWidth := ansi.StringWidth(summary)
	available := maxWidth - nameWidth - minPad
	if available < summaryWidth {
		// Truncate summary to fit.
		if available <= 0 {
			return namePart
		}
		summary = ansi.Truncate(summary, available, "…")
		summaryWidth = ansi.StringWidth(summary)
	}

	pad := maxWidth - nameWidth - summaryWidth
	if pad < minPad {
		pad = minPad
	}
	return namePart + strings.Repeat(" ", pad) + tui.AppTheme.PanelMuted.Render(summary)
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
