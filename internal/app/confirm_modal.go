package app

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/adwinying/sqlcery/internal/tui"
)

// modalConfirm is a generic yes/no dialog. It is pushed with a prompt string
// and an opaque onYes continuation message. When the user confirms (y, or Enter
// while Yes is focused) the modal pops itself and forwards onYes into the
// message loop. When the user declines (n, Esc, ctrl+c, or Enter while No is
// focused) the modal pops itself with no other side effect, leaving the
// underlying modal intact with its state unchanged.
//
// Tab / left / right move focus between the Yes and No buttons. Default focus
// is No, so a careless Enter always declines.
//
// It does not reference any wizard-specific types; it is reusable for future
// delete/reconnect/overwrite flows.
type modalConfirm struct {
	prompt      string
	onYes       tea.Msg
	yesSelected bool // false = No focused (default)
}

func (m *modalConfirm) Name() AppModal { return ModalConfirm }

func (m *modalConfirm) FilterText() string  { return "" }
func (m *modalConfirm) FilterLabel() string { return "" }

func (m *modalConfirm) Title() string { return "Confirm" }

func (m *modalConfirm) CounterText(_ InteractionState) string { return "" }

func (m *modalConfirm) DialogRows() int { return tui.ModalDialogRows }

func (m *modalConfirm) StatusBarHints(_ InteractionState) []string {
	return []string{"[y] yes", "[n] no", "tab/←→ switch", "esc cancel"}
}

func (m *modalConfirm) HandleKey(msg tea.KeyPressMsg, _ ModalContext) ModalResult {
	switch msg.String() {
	case "y":
		onYes := m.onYes
		return modalResultDismissForward{cmd: func() tea.Msg { return onYes }}
	case "n", "esc", "ctrl+c":
		return modalResultDismiss{}
	case "enter":
		if m.yesSelected {
			onYes := m.onYes
			return modalResultDismissForward{cmd: func() tea.Msg { return onYes }}
		}
		return modalResultDismiss{}
	case "tab", "left", "right":
		m.yesSelected = !m.yesSelected
		return modalResultNone{}
	}
	return modalResultNone{}
}

func (m *modalConfirm) HandleMouse(_ tea.MouseClickMsg, _ ModalContext) ModalResult {
	return modalResultNone{}
}

func (m *modalConfirm) HandleMouseWheel(_ ModalContext, _ tea.MouseWheelMsg) ModalResult {
	return modalResultNone{}
}

func (m *modalConfirm) Render(_ InteractionState, innerWidth int) string {
	const (
		yesLabel = "[ Yes ]"
		noLabel  = "[ No ]"
		gap      = 4
	)
	btnsWidth := len(yesLabel) + gap + len(noLabel)
	leftPad := (innerWidth - btnsWidth) / 2
	if leftPad < 0 {
		leftPad = 0
	}

	var yesStr, noStr string
	if m.yesSelected {
		yesStr = tui.AppTheme.PanelSelected.Render(yesLabel)
		noStr = tui.AppTheme.PanelText.Render(noLabel)
	} else {
		yesStr = tui.AppTheme.PanelText.Render(yesLabel)
		noStr = tui.AppTheme.PanelSelected.Render(noLabel)
	}
	btnsRow := strings.Repeat(" ", leftPad) + yesStr + strings.Repeat(" ", gap) + noStr

	promptPad := (innerWidth - len(m.prompt)) / 2
	if promptPad < 0 {
		promptPad = 0
	}
	promptLine := tui.AppTheme.PanelText.Render(strings.Repeat(" ", promptPad) + m.prompt)

	lines := []string{
		"",
		promptLine,
		"",
		btnsRow,
		"",
	}
	return strings.Join(lines, "\n")
}
