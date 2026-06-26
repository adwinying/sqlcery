package app

import (
	tea "charm.land/bubbletea/v2"

	"github.com/adwinying/sqlcery/internal/tui"
)

// modalConfirm is a generic yes/no dialog. It is pushed with a prompt string
// and an opaque onYes continuation message. When the user confirms (y/Enter)
// the modal pops itself and forwards onYes into the message loop. When the
// user declines (n/Esc/ctrl+c) the modal pops itself with no other side effect,
// leaving the underlying modal intact with its state unchanged.
//
// It does not reference any wizard-specific types; it is reusable for future
// delete/reconnect/overwrite flows.
type modalConfirm struct {
	prompt string
	onYes  tea.Msg
}

func (m *modalConfirm) Name() AppModal { return ModalConfirm }

func (m *modalConfirm) FilterText() string  { return "" }
func (m *modalConfirm) FilterLabel() string { return "" }

func (m *modalConfirm) Title() string { return "Confirm" }

func (m *modalConfirm) CounterText(_ InteractionState) string { return "" }

func (m *modalConfirm) StatusBarHints(_ InteractionState) []string {
	return []string{"[y] yes", "[n] no", "esc cancel"}
}

func (m *modalConfirm) HandleKey(msg tea.KeyPressMsg, _ ModalContext) ModalResult {
	switch msg.String() {
	case "y", "enter":
		onYes := m.onYes
		return modalResultDismissForward{cmd: func() tea.Msg { return onYes }}
	case "n", "esc", "ctrl+c":
		return modalResultDismiss{}
	}
	return modalResultNone{}
}

func (m *modalConfirm) HandleMouse(_ tea.MouseClickMsg, _ ModalContext) ModalResult {
	return modalResultNone{}
}

func (m *modalConfirm) HandleMouseWheel(_ ModalContext, _ tea.MouseWheelMsg) ModalResult {
	return modalResultNone{}
}

func (m *modalConfirm) Render(_ InteractionState, _ int) string {
	return tui.AppTheme.PanelText.Render(m.prompt)
}
