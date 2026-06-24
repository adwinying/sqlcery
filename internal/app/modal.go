package app

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/adwinying/sqlcery/internal/db"
	"github.com/adwinying/sqlcery/internal/export"
)

// ModalContext carries read-only context for modal key handling.
// Modals receive this instead of *Model, so their effects are explicit.
type ModalContext struct {
	Interaction      InteractionState
	Session          Session
	Dialect          db.Dialect
	MouseListOffset  int  // 0-based visible list-row offset of a click; -1 if not a list row
	MouseDoubleClick bool // true when the click is a double-click on the same offset
}

// ModalResult is the discriminated union returned by Modal.HandleKey.
type ModalResult interface{ isModalResult() }

// modalResultNone signals no action.
type modalResultNone struct{}

// modalResultPendingStatus updates the pending intent / status line.
// If dismiss is true, the modal is also closed first.
type modalResultPendingStatus struct {
	intent  PendingIntent
	action  string
	status  string
	level   NotificationLevel
	dismiss bool
}

// modalResultReady sets app state to Ready and optionally closes the modal
// or clears the latest result context.
type modalResultReady struct {
	status      string
	level       NotificationLevel
	dismiss     bool
	clearResult bool
}

// modalResultRestoreHistory sets the editor value, syncs SQL, and closes.
type modalResultRestoreHistory struct {
	sql    string
	status string
	level  NotificationLevel
}

// modalResultExecute closes the modal and starts an execution.
type modalResultExecute struct {
	label   string
	status  string
	level   NotificationLevel
	execute func(context.Context, time.Time) tea.Cmd
}

// modalResultForward forwards a tea.Cmd into the model's message loop.
type modalResultForward struct {
	cmd tea.Cmd
}

// modalResultRunHelpRow closes the Keybindings Modal and emits an intent
// message derived from the selected Help Row's key. msgFn may be nil for
// display-only rows, in which case only the modal is closed.
type modalResultRunHelpRow struct {
	msgFn func() tea.Msg
}

// modalResultOpenWizardFor closes the Keybindings Modal and opens the Slash
// Command Wizard pre-seeded to the target-selection step for the named command.
type modalResultOpenWizardFor struct {
	commandName string
	status      string
	level       NotificationLevel
}

// modalResultExportWizard closes the Export Wizard and dispatches the export.
type modalResultExportWizard struct {
	format export.Format
	path   string // empty = copy to clipboard
}

func (modalResultNone) isModalResult()           {}
func (modalResultPendingStatus) isModalResult()  {}
func (modalResultReady) isModalResult()          {}
func (modalResultRestoreHistory) isModalResult() {}
func (modalResultExecute) isModalResult()        {}
func (modalResultForward) isModalResult()        {}
func (modalResultRunHelpRow) isModalResult()     {}
func (modalResultOpenWizardFor) isModalResult()  {}
func (modalResultExportWizard) isModalResult()   {}

// Modal is the interface implemented by every overlay dialog.
// When m.modal is non-nil, Update routes all key events to it — no
// global shortcuts fire while a modal is open.
type Modal interface {
	// HandleKey receives every key press while this modal is active and
	// returns a ModalResult describing the intended side effect. It must
	// not mutate the model directly.
	HandleKey(msg tea.KeyPressMsg, ctx ModalContext) ModalResult
	// HandleMouse handles a left-click within the modal. ctx.MouseListOffset is
	// the 0-based row offset of the click within the modal's visible list region,
	// or -1 if the click was not on a selectable list row. ctx.MouseDoubleClick
	// is true when this click is a double-click on the same offset. Returns
	// modalResultNone when offset is -1 (outside/non-list click is a no-op —
	// never dismisses).
	HandleMouse(msg tea.MouseClickMsg, ctx ModalContext) ModalResult
	// HandleMouseWheel scrolls the modal's list / moves selection by one step
	// in the wheel direction. Mutates the modal in place and returns
	// modalResultNone.
	HandleMouseWheel(msg tea.MouseWheelMsg) ModalResult
	// Render returns the list content string for the suggestions box.
	// innerWidth is the available content width inside the modal border,
	// so modals can pre-apply horizontal scroll offsets to long lines.
	// Title and filter content are provided separately via Title and FilterText.
	Render(interaction InteractionState, innerWidth int) string
	// FilterText returns the current filter value with a trailing cursor "█"
	// always appended (e.g. "sel█", "█" when empty). Return "" to suppress
	// the filter box and render as a single suggestions-only box.
	FilterText() string
	// FilterLabel returns the label embedded in the filter box top border.
	// Return "Filter:" for the standard filter box; modals that repurpose the
	// filter box for other input (e.g. "Path:") return their own label.
	FilterLabel() string
	// Title returns the label embedded in the suggestions box top border.
	Title() string
	// CounterText returns a "N of M" string embedded in the bottom-right of
	// the suggestions box border. Return "" to suppress the counter.
	CounterText(interaction InteractionState) string
	// StatusBarHints returns hints for the status bar, ordered by priority
	// (most important first). The caller drops from the tail to fit the width.
	StatusBarHints(interaction InteractionState) []string
	// Name returns the AppModal tag, kept on InteractionState so sub-models
	// can read which modal is on top of the stack.
	Name() AppModal
}

// currentModal returns the topmost modal, or nil if the stack is empty.
func (m *Model) currentModal() Modal {
	if len(m.modals) == 0 {
		return nil
	}
	return m.modals[len(m.modals)-1]
}

// pushModal pushes a modal onto the stack and updates ActiveModal.
func (m *Model) pushModal(modal Modal) {
	m.modals = append(m.modals, modal)
	m.state.SetActiveModal(modal.Name())
}

// popModal pops the topmost modal and updates ActiveModal to the new top.
func (m *Model) popModal() {
	if len(m.modals) == 0 {
		return
	}
	m.modals = m.modals[:len(m.modals)-1]
	if len(m.modals) == 0 {
		m.state.SetActiveModal(ModalNone)
	} else {
		m.state.SetActiveModal(m.modals[len(m.modals)-1].Name())
	}
}

// closeModal pops the topmost modal (alias kept for call sites that dismiss a single modal).
func (m *Model) closeModal() {
	m.popModal()
}

// applyModalResult dispatches the result of Modal.HandleKey onto the model.
func (m *Model) applyModalResult(result ModalResult) tea.Cmd {
	switch r := result.(type) {
	case modalResultNone:
		return nil
	case modalResultPendingStatus:
		if r.dismiss {
			m.closeModal()
		}
		m.state.SetPendingIntent(r.intent, r.action, r.status, r.level)
		return m.notificationClearCmdIfSet()
	case modalResultReady:
		if r.dismiss {
			m.closeModal()
		}
		m.state.SetReady(r.status, r.level)
		if r.clearResult {
			m.state.SetLatestResultContext(nil)
		}
		return m.notificationClearCmdIfSet()
	case modalResultRestoreHistory:
		m.command.SetEditorValue(r.sql)
		m.syncCurrentSQL()
		m.closeModal()
		m.state.SetPendingIntent(IntentNone, "history", r.status, r.level)
		return m.notificationClearCmdIfSet()
	case modalResultExecute:
		m.closeModal()
		return m.startExecution(r.label, r.status, r.level, r.execute)
	case modalResultForward:
		return r.cmd
	case modalResultRunHelpRow:
		m.closeModal()
		if r.msgFn == nil {
			m.state.SetReady("", NotificationNone)
			return m.notificationClearCmdIfSet()
		}
		return func() tea.Msg { return r.msgFn() }
	case modalResultOpenWizardFor:
		m.closeModal()
		return m.pushWizardForCommand(r.commandName, r.status, r.level)
	case modalResultExportWizard:
		m.closeModal()
		return m.executeExportWizard(r.format, r.path)
	}
	return nil
}
