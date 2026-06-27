package app

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/adwinying/sqlcery/internal/config"
	"github.com/adwinying/sqlcery/internal/db"
	apphistory "github.com/adwinying/sqlcery/internal/history"
	"github.com/adwinying/sqlcery/internal/tui"
)

// connectionPickerModal implements Modal for the mid-run Connection Picker
// overlay. It reuses pickerFilteredCandidates and pickerRenderRow from
// connection_picker.go (the startup picker), adding mid-run semantics:
//
//   - Esc closes the Modal without disturbing the live Session (non-destructive).
//   - Selecting the already-active Connection is a NO-OP that simply closes the Modal.
//   - Selecting a different Connection performs a transactional swap:
//     open the new Adapter while the old one stays live, close the old one only
//     on success.
//
// The active connection name is captured at push time from m.session.ConnectionName;
// the connections loader is captured for row rendering.
type connectionPickerModal struct {
	startup              bool                               // true when opened at startup (no Session yet)
	activeConnection     string                             // m.session.ConnectionName at push time
	connectionsLoader    func() (config.Connections, error) // captured for row rendering
	filter               string
	selected             int
	candidates           []string
	vpStart              int    // lazy viewport start for the 16-row visible window
	lastFailedConnection string // name marked with ! after a failed connect; cleared on next attempt
}

func (c *connectionPickerModal) Name() AppModal { return ModalConnectionPicker }

func (c *connectionPickerModal) FilterText() string  { return c.filter + "█" }
func (c *connectionPickerModal) FilterLabel() string { return "Filter:" }
func (c *connectionPickerModal) Title() string       { return "Connection Picker" }

func (c *connectionPickerModal) CounterText(_ InteractionState) string {
	filtered := pickerFilteredCandidates(c.candidates, c.filter)
	total := len(filtered) + 1 // +1 for the pinned "Create a new connection" row
	sel := wrapSelection(c.selected, total)
	return strings.Join([]string{intStr(sel + 1), "of", intStr(total)}, " ")
}

func (c *connectionPickerModal) StatusBarHints(_ InteractionState) []string {
	var escHint string
	switch {
	case strings.TrimSpace(c.filter) != "":
		escHint = "esc clear filter"
	case c.startup:
		escHint = "esc quit"
	default:
		escHint = "esc cancel"
	}
	return []string{"enter connect", escHint, "ctrl+p up", "ctrl+n down"}
}

func (c *connectionPickerModal) HandleKey(msg tea.KeyPressMsg, ctx ModalContext) ModalResult {
	keys := defaultCommandModeKeys()

	switch {
	case msg.String() == "ctrl+c":
		// At startup ctrl+c quits the app (nothing to return to); mid-run it
		// closes the Modal back to the live Session.
		if c.startup {
			return modalResultForward{cmd: tea.Quit}
		}
		return modalResultReady{status: "", level: NotificationNone, dismiss: true}
	case key.Matches(msg, keys.Help):
		return modalResultForward{cmd: func() tea.Msg { return toggleHelpIntentMsg{} }}
	case key.Matches(msg, keys.Cancel):
		// Esc clears a non-empty filter first, in both modes.
		if strings.TrimSpace(c.filter) != "" {
			c.filter = ""
			c.selected = 0
			c.vpStart = 0
			return modalResultNone{}
		}
		// Empty-filter Esc forks on entry path: at startup there is nothing to
		// return to, so quit; mid-run closes the Modal non-destructively.
		if c.startup {
			return modalResultForward{cmd: tea.Quit}
		}
		return modalResultReady{status: "", level: NotificationNone, dismiss: true}
	case key.Matches(msg, keys.PrevSuggestion), msg.String() == "up":
		c.move(-1)
		return modalResultNone{}
	case key.Matches(msg, keys.NextSuggestion), msg.String() == "down":
		c.move(1)
		return modalResultNone{}
	case msg.String() == "backspace" || msg.String() == "ctrl+h" || msg.String() == "delete":
		c.filter = trimLastRune(c.filter)
		c.selected = 0
		c.vpStart = 0
		return modalResultNone{}
	case msg.String() == "space":
		c.filter += " "
		c.selected = 0
		c.vpStart = 0
		return modalResultNone{}
	case msg.String() == "enter":
		return c.confirmSelection(ctx)
	default:
		if len(msg.Text) > 0 && !msg.Mod.Contains(tea.ModAlt) && !msg.Mod.Contains(tea.ModCtrl) {
			c.filter += msg.Text
			c.selected = 0
			c.vpStart = 0
		}
		return modalResultNone{}
	}
}

// confirmSelection resolves the currently highlighted row and emits the
// appropriate ModalResult.
//
//   - The last row is the pinned "Create a new connection" row → push the wizard.
//   - Selecting the already-active Connection is a NO-OP close.
//   - Any other row emits a midRunConnectMsg.
func (c *connectionPickerModal) confirmSelection(_ ModalContext) ModalResult {
	filtered := pickerFilteredCandidates(c.candidates, c.filter)
	total := len(filtered) + 1 // +1 for the create row
	idx := wrapSelection(c.selected, total)

	// Create row: push the New Connection Wizard.
	if idx == len(filtered) {
		return modalResultForward{cmd: func() tea.Msg {
			return openNewConnectionWizardMsg{}
		}}
	}

	name := filtered[idx]

	// NO-OP: selecting the already-active Connection just closes the Modal.
	if name == c.activeConnection {
		return modalResultReady{status: "", level: NotificationNone, dismiss: true}
	}

	// Emit a mid-run connect command through modalResultForward.
	return modalResultForward{cmd: func() tea.Msg {
		return midRunConnectMsg{name: name}
	}}
}

func (c *connectionPickerModal) move(delta int) {
	filtered := pickerFilteredCandidates(c.candidates, c.filter)
	total := len(filtered) + 1 // +1 for the pinned create row
	c.selected = wrapSelection(c.selected+delta, total)
	const maxRows = 16
	c.vpStart = lazyScroll(c.selected, c.vpStart, maxRows)
}

func (c *connectionPickerModal) Render(_ InteractionState, innerWidth int) string {
	filtered := pickerFilteredCandidates(c.candidates, c.filter)
	total := len(filtered) + 1 // +1 for the pinned "Create a new connection" row

	const maxRows = 16
	selected := wrapSelection(c.selected, total)
	vpStart := lazyScroll(selected, c.vpStart, maxRows)
	vpEnd := min(vpStart+maxRows, total)

	availWidth := innerWidth
	if availWidth < 20 {
		availWidth = 20
	}

	createIdx := len(filtered) // index of the create row in the effective list

	lines := make([]string, 0, vpEnd-vpStart)
	for i := vpStart; i < vpEnd; i++ {
		var row string
		if i == createIdx {
			row = c.renderCreateRow(availWidth)
		} else {
			row = c.renderRow(filtered[i], availWidth)
		}
		if i == selected {
			// Strip embedded ANSI so it doesn't cancel the selection background
			// mid-row, then pad to full width.
			plain := ansi.Strip(row)
			if pad := availWidth - ansi.StringWidth(plain); pad > 0 {
				plain += strings.Repeat(" ", pad)
			}
			lines = append(lines, tui.AppTheme.PanelSelected.Render(plain))
		} else {
			lines = append(lines, tui.AppTheme.PanelText.Render(row))
		}
	}

	return strings.Join(lines, "\n")
}

// renderCreateRow renders the pinned "Create a new connection" row.
func (c *connectionPickerModal) renderCreateRow(_ int) string {
	return "  + Create a new connection"
}

// renderRow renders a single row. The 2-char prefix slot shows ! for the
// last-failed connection (error-coloured), ● for the active connection, or
// blanks otherwise; the pickerRenderRow helper is reused for the name+summary
// layout. A failed connection is never the active one, so the markers never
// collide.
func (c *connectionPickerModal) renderRow(name string, availWidth int) string {
	const prefixWidth = 2
	var prefix string
	switch {
	case name == c.lastFailedConnection:
		prefix = tui.AppTheme.WarningNotice.Render("!") + " "
	case name == c.activeConnection:
		prefix = "● "
	default:
		prefix = "  "
	}

	// Delegate to the shared row renderer, then prepend the prefix.
	rowContent := pickerRenderRow(name, c.connectionsLoader, availWidth-prefixWidth)
	return prefix + rowContent
}

// HandleMouse implements Modal.HandleMouse for connectionPickerModal.
func (c *connectionPickerModal) HandleMouse(_ tea.MouseClickMsg, ctx ModalContext) ModalResult {
	if ctx.MouseListOffset < 0 {
		return modalResultNone{}
	}
	filtered := pickerFilteredCandidates(c.candidates, c.filter)
	total := len(filtered) + 1 // +1 for the create row
	idx := c.vpStart + ctx.MouseListOffset
	if idx < 0 || idx >= total {
		return modalResultNone{}
	}
	c.selected = idx
	if ctx.MouseDoubleClick {
		return c.confirmSelection(ctx)
	}
	return modalResultNone{}
}

// HandleMouseWheel implements Modal.HandleMouseWheel for connectionPickerModal.
func (c *connectionPickerModal) HandleMouseWheel(_ ModalContext, msg tea.MouseWheelMsg) ModalResult {
	filtered := pickerFilteredCandidates(c.candidates, c.filter)
	total := len(filtered) + 1 // +1 for the create row
	switch msg.Button {
	case tea.MouseWheelUp:
		c.selected = max(0, c.selected-1)
	case tea.MouseWheelDown:
		c.selected = min(c.selected+1, total-1)
	}
	const maxRows = 16
	c.vpStart = lazyScroll(c.selected, c.vpStart, maxRows)
	return modalResultNone{}
}

// intStr converts an int to a decimal string without importing strconv.
func intStr(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf []byte
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	if neg {
		buf = append([]byte{'-'}, buf...)
	}
	return string(buf)
}

// ---- Mid-run connect message types ----

// midRunConnectMsg triggers an async open attempt for a mid-run connection switch.
type midRunConnectMsg struct {
	name string
}

// midRunConnectSuccessMsg signals that the new Adapter is open and ready to swap.
type midRunConnectSuccessMsg struct {
	adapter    *db.SQLAdapter // new adapter (to swap in)
	oldAdapter *db.SQLAdapter // old adapter (to close after swap, ONLY on success)
	resolved   config.ResolvedConnection
}

// midRunConnectFailedMsg signals a failed open; the old Session (if any) is
// untouched. name is the Connection that failed, marked with ! in the Picker.
type midRunConnectFailedMsg struct {
	err  error
	name string
}

// ---- Mid-run connect handlers ----

// handleMidRunConnect starts the transactional open. The live Adapter stays open.
// The old adapter reference is captured into the success message so it can be
// closed there — and ONLY there.
func (m Model) handleMidRunConnect(msg midRunConnectMsg) (Model, tea.Cmd) {
	// Look up the connection config.
	var conn config.Connection
	if m.connectionsLoader != nil {
		if connections, err := m.connectionsLoader(); err == nil {
			conn = connections.Connection[msg.name]
		}
	}
	resolved := config.ResolvedConnection{
		Name:       msg.name,
		Raw:        msg.name,
		Connection: conn,
	}

	m.pendingConnectAbort = false

	// Clear any prior ! marker on the Picker — a fresh attempt is starting.
	if pm, ok := m.currentModal().(*connectionPickerModal); ok {
		pm.lastFailedConnection = ""
	}

	oldAdapter := m.session.Adapter
	openFn := m.open

	ctx, cancel := context.WithCancel(context.Background())
	m.cancelConnect = cancel

	// Show a notification that we are connecting; stay in StateReady so panes
	// remain visible and the existing session is still usable.
	m.state.SetPendingIntent(IntentNone, "connect", "Connecting to "+msg.name+"...", NotificationInfo)

	return m, func() tea.Msg {
		adapter, err := openFn(ctx, resolved.Connection)
		if err != nil {
			return midRunConnectFailedMsg{err: err, name: resolved.Name}
		}
		return midRunConnectSuccessMsg{
			adapter:    adapter,
			oldAdapter: oldAdapter,
			resolved:   resolved,
		}
	}
}

// handleMidRunConnectSuccess swaps in the new Session, rebuilds state, records
// frecency, then closes the OLD Adapter. The old one is closed ONLY HERE, never
// in the failure path.
func (m Model) handleMidRunConnectSuccess(msg midRunConnectSuccessMsg) (Model, tea.Cmd) {
	if m.cancelConnect != nil {
		m.cancelConnect()
		m.cancelConnect = nil
	}
	m.pendingConnectAbort = false

	// Swap the session.
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

	// Rebuild history for the new connection.
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

	// Reset per-session UI: results, marked rows, schema.
	m.state.SetLatestResultContext(nil)
	m.state.SetMarkedRows(nil)
	m.schema = nil
	m.syncAutocompleteSchemaSnapshot()

	// Close the connection picker modal (if still open) and transition to Ready.
	if m.currentModal() != nil && m.currentModal().Name() == ModalConnectionPicker {
		m.closeModal()
	}
	m.state.SetReady("Connected to "+msg.resolved.Name+".", NotificationSuccess)

	// Close the OLD adapter — only on success.
	oldAdapter := msg.oldAdapter
	newAdapter := msg.adapter
	loader := m.loader
	closeFn := m.closeAdapter

	return m, tea.Batch(
		m.command.Init(),
		func() tea.Msg {
			schema, err := loader(context.Background(), newAdapter)
			if err != nil {
				return pickerSchemaReadyMsg{schema: nil}
			}
			return pickerSchemaReadyMsg{schema: schema}
		},
		func() tea.Msg {
			if oldAdapter != nil && closeFn != nil {
				_ = closeFn(oldAdapter)
			}
			return nil
		},
		m.notificationClearCmdIfSet(),
	)
}

// handleMidRunConnectFailed leaves the existing Session completely untouched.
// The old Adapter is NOT closed here. The Picker Modal stays open; the failed
// Connection is marked with ! and the detailed error goes to the Status Bar.
// At startup there is no Session to fall back to, so the app state returns to
// StateSelectConnection rather than StateReady.
func (m Model) handleMidRunConnectFailed(msg midRunConnectFailedMsg) (Model, tea.Cmd) {
	if m.cancelConnect != nil {
		m.cancelConnect()
		m.cancelConnect = nil
	}
	m.pendingConnectAbort = false

	// Mark the failed Connection inline and learn whether this is the startup
	// Picker (no Session behind it) from the live modal.
	startup := false
	if pm, ok := m.currentModal().(*connectionPickerModal); ok {
		pm.lastFailedConnection = msg.name
		startup = pm.startup
	}

	// If the user aborted via double-Esc, return silently to the Picker.
	if errors.Is(msg.err, context.Canceled) {
		return m, m.returnToPickerOrReady(startup, "Connect cancelled.", NotificationInfo)
	}

	errText := FormatTerminalError(msg.err)
	return m, m.returnToPickerOrReady(startup, "Connection failed: "+errText, NotificationError)
}

// returnToPickerOrReady sets the post-connect-failure status. Mid-run lands in
// StateReady (the old Session is still live); startup stays in
// StateSelectConnection (there is no Session) with the status in the Status Bar.
func (m *Model) returnToPickerOrReady(startup bool, status string, level NotificationLevel) tea.Cmd {
	if startup {
		m.state.App.Current = StateSelectConnection
		m.state.App.Error = ""
		m.state.App.Reconnect = nil
		m.state.Notification = Notification{Text: status, Level: level, CreatedAt: time.Now()}
		return m.notificationClearCmdIfSet()
	}
	m.state.SetReady(status, level)
	return m.notificationClearCmdIfSet()
}

// handleMidRunConnectingKeyPress handles keys while a mid-run connect is in
// flight (StateReady with cancelConnect set). Mirrors the startup connecting
// path: double-Esc cancels the connect, ctrl+c quits, any other key is
// swallowed (the modal stays visible but non-interactive).
func (m Model) handleMidRunConnectingKeyPress(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		// Cancel the in-flight open before quitting so a late-completing adapter
		// is not leaked (never swapped in, never closed).
		if m.cancelConnect != nil {
			m.cancelConnect()
			m.cancelConnect = nil
		}
		m.pendingConnectAbort = false
		return m, tea.Quit
	case "esc":
		if m.pendingConnectAbort {
			// Second Esc: cancel the in-flight connect.
			if m.cancelConnect != nil {
				m.cancelConnect()
			}
			m.pendingConnectAbort = false
			return m, nil
		}
		// First Esc: arm the pending abort and show the hint.
		m.pendingConnectAbort = true
		m.state.SetPendingIntent(IntentNone, "connect", "Press esc again to cancel connecting.", NotificationInfo)
		return m, nil
	default:
		// Any other key disarms the pending abort.
		if m.pendingConnectAbort {
			m.pendingConnectAbort = false
			m.state.SetPendingIntent(IntentNone, "connect", "Connecting...", NotificationInfo)
		}
		return m, nil
	}
}

// openConnectionPickerModal pushes the mid-run Connection Picker Modal.
// It loads the current candidate list (or reuses m.picker.Candidates if
// already loaded by the startup picker path).
func (m *Model) openConnectionPickerModal() (Model, tea.Cmd) {
	candidates := loadPickerCandidates(m.connectionsLoader, m.frecencyStore)

	modal := &connectionPickerModal{
		activeConnection:  m.session.ConnectionName,
		connectionsLoader: m.connectionsLoader,
		candidates:        candidates,
	}
	m.pushModal(modal)
	m.state.SetReady("Select a connection and press enter.", NotificationInfo)
	return *m, m.notificationClearCmdIfSet()
}

// loadPickerCandidates synchronously loads and frecency-orders the connection
// candidate list. Used by the mid-run modal path (synchronous, small list).
func loadPickerCandidates(connectionsLoader func() (config.Connections, error), frecencyStore FrecencyStore) []string {
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
	return names
}
