package app

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/adwinying/sqlcery/internal/tui"
)

// modalConnecting is the overlay shown while the auto-connect open is in
// flight. It displays the target connection name and a single Cancel button.
// Pressing Cancel, Esc, or Enter cancels the in-flight open and quits.
type modalConnecting struct {
	displayName string
}

func (c *modalConnecting) Name() AppModal { return ModalConnecting }

func (c *modalConnecting) FilterText() string  { return "" }
func (c *modalConnecting) FilterLabel() string { return "" }
func (c *modalConnecting) Title() string       { return "Connecting" }
func (c *modalConnecting) CounterText(_ InteractionState) string { return "" }
func (c *modalConnecting) DialogRows() int     { return tui.ModalDialogRows }

func (c *modalConnecting) StatusBarHints(_ InteractionState) []string {
	return []string{"esc cancel", "ctrl+c quit"}
}

func (c *modalConnecting) HandleKey(msg tea.KeyPressMsg, _ ModalContext) ModalResult {
	switch msg.String() {
	case "esc", "enter", "ctrl+c":
		return modalResultForward{cmd: func() tea.Msg { return pickerConnectAbortMsg{} }}
	}
	return modalResultNone{}
}

func (c *modalConnecting) HandleMouse(_ tea.MouseClickMsg, _ ModalContext) ModalResult {
	return modalResultNone{}
}

func (c *modalConnecting) HandleMouseWheel(_ ModalContext, _ tea.MouseWheelMsg) ModalResult {
	return modalResultNone{}
}

func (c *modalConnecting) Render(_ InteractionState, innerWidth int) string {
	const cancelLabel = "[ Cancel ]"

	body := "Connecting to " + c.displayName + "…"
	bodyPad := (innerWidth - len(body)) / 2
	if bodyPad < 0 {
		bodyPad = 0
	}

	cancelPad := (innerWidth - len(cancelLabel)) / 2
	if cancelPad < 0 {
		cancelPad = 0
	}

	lines := []string{
		"",
		tui.AppTheme.PanelText.Render(strings.Repeat(" ", bodyPad) + body),
		"",
		strings.Repeat(" ", cancelPad) + tui.AppTheme.PanelSelected.Render(cancelLabel),
		"",
	}
	return strings.Join(lines, "\n")
}
