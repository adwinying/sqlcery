package app

import tea "charm.land/bubbletea/v2"

// Modal is the interface implemented by every overlay dialog.
// When m.modal is non-nil, Update routes all key events to it — no
// global shortcuts fire while a modal is open.
type Modal interface {
	// HandleKey receives every key press while this modal is active.
	// It may mutate m freely and return a Cmd.
	HandleKey(msg tea.KeyPressMsg, m *Model) tea.Cmd
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
