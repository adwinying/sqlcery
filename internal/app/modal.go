package app

import (
	"context"
	"time"

	"github.com/adwinying/sqlcery/internal/db"
	tea "charm.land/bubbletea/v2"
)

// ModalContext carries read-only context for modal key handling.
// Modals receive this instead of *Model, so their effects are explicit.
type ModalContext struct {
	Interaction InteractionState
	Session     Session
	Dialect     db.Dialect
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
	dismiss bool
}

// modalResultReady sets app state to Ready and optionally closes the modal
// or clears the latest result context.
type modalResultReady struct {
	status      string
	dismiss     bool
	clearResult bool
}

// modalResultRestoreHistory sets the editor value, syncs SQL, and closes.
type modalResultRestoreHistory struct {
	sql    string
	status string
}

// modalResultExecute closes the modal and starts an execution.
type modalResultExecute struct {
	label   string
	status  string
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
}

func (modalResultNone) isModalResult()           {}
func (modalResultPendingStatus) isModalResult()  {}
func (modalResultReady) isModalResult()          {}
func (modalResultRestoreHistory) isModalResult() {}
func (modalResultExecute) isModalResult()        {}
func (modalResultForward) isModalResult()        {}
func (modalResultRunHelpRow) isModalResult()     {}
func (modalResultOpenWizardFor) isModalResult()  {}

// Modal is the interface implemented by every overlay dialog.
// When m.modal is non-nil, Update routes all key events to it — no
// global shortcuts fire while a modal is open.
type Modal interface {
	// HandleKey receives every key press while this modal is active and
	// returns a ModalResult describing the intended side effect. It must
	// not mutate the model directly.
	HandleKey(msg tea.KeyPressMsg, ctx ModalContext) ModalResult
	// Render returns the modal content string for overlayCenter.
	// innerWidth is the available content width inside the modal border,
	// so modals can pre-apply horizontal scroll offsets to long lines.
	Render(interaction InteractionState, innerWidth int) string
	// FooterHints returns a pipe-separated hint string for the Hints Bar,
	// state-conditional on the modal's own state and interaction.
	FooterHints(interaction InteractionState) string
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
		m.state.SetPendingIntent(r.intent, r.action, r.status)
		return nil
	case modalResultReady:
		if r.dismiss {
			m.closeModal()
		}
		m.state.SetReady(r.status)
		if r.clearResult {
			m.state.SetLatestResultContext(nil)
		}
		return nil
	case modalResultRestoreHistory:
		m.command.SetEditorValue(r.sql)
		m.syncCurrentSQL()
		m.closeModal()
		m.state.SetPendingIntent(IntentNone, "history", r.status)
		return nil
	case modalResultExecute:
		m.closeModal()
		return m.startExecution(r.label, r.status, r.execute)
	case modalResultForward:
		return r.cmd
	case modalResultRunHelpRow:
		m.closeModal()
		if r.msgFn == nil {
			m.state.SetReady("Closed keybindings.")
			return nil
		}
		return func() tea.Msg { return r.msgFn() }
	case modalResultOpenWizardFor:
		m.closeModal()
		return m.pushWizardForCommand(r.commandName, r.status)
	}
	return nil
}
