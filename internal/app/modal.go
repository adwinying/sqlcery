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

func (modalResultNone) isModalResult()           {}
func (modalResultPendingStatus) isModalResult()  {}
func (modalResultReady) isModalResult()          {}
func (modalResultRestoreHistory) isModalResult() {}
func (modalResultExecute) isModalResult()        {}
func (modalResultForward) isModalResult()        {}

// Modal is the interface implemented by every overlay dialog.
// When m.modal is non-nil, Update routes all key events to it — no
// global shortcuts fire while a modal is open.
type Modal interface {
	// HandleKey receives every key press while this modal is active and
	// returns a ModalResult describing the intended side effect. It must
	// not mutate the model directly.
	HandleKey(msg tea.KeyPressMsg, ctx ModalContext) ModalResult
	// Render returns the modal content string for overlayCenter.
	Render(interaction InteractionState) string
	// Name returns the AppModal tag, kept on InteractionState so sub-models
	// (footer, help surface) can read which modal is open.
	Name() AppModal
}

// closeModal closes whatever modal is currently open.
func (m *Model) closeModal() {
	if m.modal == nil {
		return
	}
	m.modal = nil
	m.state.SetActiveModal(ModalNone)
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
	}
	return nil
}
