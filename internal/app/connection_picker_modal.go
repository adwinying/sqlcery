package app

import (
	"context"
	"errors"
	"sort"
	"strings"

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
	activeConnection  string                             // m.session.ConnectionName at push time
	connectionsLoader func() (config.Connections, error) // captured for row rendering
	filter            string
	selected          int
	candidates        []string
	vpStart           int // lazy viewport start for the 16-row visible window
}

func (c *connectionPickerModal) Name() AppModal { return ModalConnectionPicker }

func (c *connectionPickerModal) FilterText() string  { return c.filter + "█" }
func (c *connectionPickerModal) FilterLabel() string { return "Filter:" }
func (c *connectionPickerModal) Title() string       { return "Connection Picker" }

func (c *connectionPickerModal) CounterText(_ InteractionState) string {
	filtered := pickerFilteredCandidates(c.candidates, c.filter)
	if len(filtered) == 0 {
		return ""
	}
	sel := wrapSelection(c.selected, len(filtered))
	return strings.Join([]string{intStr(sel + 1), "of", intStr(len(filtered))}, " ")
}

func (c *connectionPickerModal) StatusBarHints(_ InteractionState) []string {
	escHint := "esc cancel"
	if strings.TrimSpace(c.filter) != "" {
		escHint = "esc clear filter"
	}
	return []string{"enter connect", escHint, "ctrl+p up", "ctrl+n down"}
}

func (c *connectionPickerModal) HandleKey(msg tea.KeyPressMsg, ctx ModalContext) ModalResult {
	keys := defaultCommandModeKeys()

	switch {
	case msg.String() == "ctrl+c":
		return modalResultReady{status: "", level: NotificationNone, dismiss: true}
	case key.Matches(msg, keys.Help):
		return modalResultForward{cmd: func() tea.Msg { return toggleHelpIntentMsg{} }}
	case key.Matches(msg, keys.Cancel):
		// Mid-run Esc: clear filter first if set; otherwise close the Modal non-destructively.
		if strings.TrimSpace(c.filter) != "" {
			c.filter = ""
			c.selected = 0
			c.vpStart = 0
			return modalResultNone{}
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

// confirmSelection resolves the currently highlighted Connection and emits the
// appropriate ModalResult. Selecting the active Connection is a NO-OP close.
func (c *connectionPickerModal) confirmSelection(ctx ModalContext) ModalResult {
	_ = ctx
	filtered := pickerFilteredCandidates(c.candidates, c.filter)
	if len(filtered) == 0 {
		return modalResultNone{}
	}
	idx := wrapSelection(c.selected, len(filtered))
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
	if len(filtered) == 0 {
		return
	}
	c.selected = wrapSelection(c.selected+delta, len(filtered))
	const maxRows = 16
	c.vpStart = lazyScroll(c.selected, c.vpStart, maxRows)
}

func (c *connectionPickerModal) Render(_ InteractionState, innerWidth int) string {
	filtered := pickerFilteredCandidates(c.candidates, c.filter)

	if len(c.candidates) == 0 {
		return tui.AppTheme.PanelMuted.Render("No connections defined.")
	}
	if len(filtered) == 0 {
		return tui.AppTheme.PanelMuted.Render("No fuzzy matches.")
	}

	const maxRows = 16
	selected := wrapSelection(c.selected, len(filtered))
	vpStart := lazyScroll(selected, c.vpStart, maxRows)
	vpEnd := vpStart + maxRows
	if vpEnd > len(filtered) {
		vpEnd = len(filtered)
	}

	availWidth := innerWidth
	if availWidth < 20 {
		availWidth = 20
	}

	lines := make([]string, 0, vpEnd-vpStart)
	for i := vpStart; i < vpEnd; i++ {
		name := filtered[i]
		row := c.renderRow(name, availWidth)
		if i == selected {
			// Pad so the highlight fills the full width.
			stripped := ansi.Strip(row)
			if pad := availWidth - ansi.StringWidth(stripped); pad > 0 {
				row += strings.Repeat(" ", pad)
			}
			lines = append(lines, tui.AppTheme.PanelSelected.Render(row))
		} else {
			lines = append(lines, tui.AppTheme.PanelText.Render(row))
		}
	}

	return strings.Join(lines, "\n")
}

// renderRow renders a single row. Active connection is prefixed with ●; the
// pickerRenderRow helper is reused for the name+summary layout.
func (c *connectionPickerModal) renderRow(name string, availWidth int) string {
	prefix := "  "
	prefixWidth := 2
	if name == c.activeConnection {
		prefix = "● "
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
	if len(filtered) == 0 {
		return modalResultNone{}
	}
	idx := c.vpStart + ctx.MouseListOffset
	if idx < 0 || idx >= len(filtered) {
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
	if len(filtered) == 0 {
		return modalResultNone{}
	}
	switch msg.Button {
	case tea.MouseWheelUp:
		c.selected = max(0, c.selected-1)
	case tea.MouseWheelDown:
		c.selected = min(c.selected+1, len(filtered)-1)
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

// midRunConnectFailedMsg signals a failed mid-run open; the old Session is untouched.
type midRunConnectFailedMsg struct {
	err error
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

	m.picker.PendingAbort = false
	m.picker.ConnectError = ""

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
			return midRunConnectFailedMsg{err: err}
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
	m.picker.PendingAbort = false

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
// The old Adapter is NOT closed here.
func (m Model) handleMidRunConnectFailed(msg midRunConnectFailedMsg) (Model, tea.Cmd) {
	if m.cancelConnect != nil {
		m.cancelConnect()
		m.cancelConnect = nil
	}
	m.picker.PendingAbort = false

	// If the user aborted via double-Esc, return silently to the picker.
	if errors.Is(msg.err, context.Canceled) {
		m.state.SetReady("Connect cancelled.", NotificationInfo)
		return m, m.notificationClearCmdIfSet()
	}

	errText := FormatTerminalError(msg.err)
	m.state.SetReady("Connection failed: "+errText, NotificationError)
	return m, m.notificationClearCmdIfSet()
}

// handleMidRunConnectingKeyPress handles keys while a mid-run connect is in
// flight (StateReady with cancelConnect set). Mirrors the startup connecting
// path: double-Esc cancels the connect, ctrl+c quits, any other key is
// swallowed (the modal stays visible but non-interactive).
func (m Model) handleMidRunConnectingKeyPress(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		if m.picker.PendingAbort {
			// Second Esc: cancel the in-flight connect.
			if m.cancelConnect != nil {
				m.cancelConnect()
			}
			m.picker.PendingAbort = false
			return m, nil
		}
		// First Esc: arm the pending abort and show the hint.
		m.picker.PendingAbort = true
		m.state.SetPendingIntent(IntentNone, "connect", "Press esc again to cancel connecting.", NotificationInfo)
		return m, nil
	default:
		// Any other key disarms the pending abort.
		if m.picker.PendingAbort {
			m.picker.PendingAbort = false
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
