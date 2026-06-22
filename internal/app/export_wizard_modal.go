package app

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/adwinying/sqlcery/internal/export"
	"github.com/adwinying/sqlcery/internal/tui"
)

type exportWizardStep int

const (
	exportWizardStepFormat exportWizardStep = iota
	exportWizardStepPath
)

var exportWizardFormats = []export.Format{
	export.FormatCSV,
	export.FormatTSV,
	export.FormatJSON,
	export.FormatMarkdown,
	export.FormatSQL,
}

// exportWizardModal implements Modal for the two-step export wizard.
// Step 1: choose format (filterable). Step 2: enter a save path (blank = clipboard).
type exportWizardModal struct {
	step           exportWizardStep
	formatFilter   string
	selectedFormat int // index within filteredFormats()
	path           string
	cwd            string // captured at push time from m.session.WorkingDir
}

func (e *exportWizardModal) Name() AppModal { return ModalExportWizard }

func (e *exportWizardModal) FilterText() string {
	if e.step == exportWizardStepPath {
		return e.path + "█"
	}
	return e.formatFilter + "█"
}

func (e *exportWizardModal) FilterLabel() string {
	if e.step == exportWizardStepPath {
		return "Path:"
	}
	return "Filter:"
}

func (e *exportWizardModal) Title() string {
	if e.step == exportWizardStepPath {
		return "Save as " + strings.ToUpper(string(e.chosenFormat()))
	}
	return "Export Format"
}

func (e *exportWizardModal) CounterText(_ InteractionState) string {
	if e.step == exportWizardStepPath {
		return ""
	}
	filtered := e.filteredFormats()
	if len(filtered) == 0 {
		return ""
	}
	selected := wrapSelection(e.selectedFormat, len(filtered))
	return fmt.Sprintf("%d of %d", selected+1, len(filtered))
}

func (e *exportWizardModal) FooterHints(_ InteractionState) []string {
	keys := defaultCommandModeKeys()
	if e.step == exportWizardStepPath {
		return []string{"enter export", "esc back", "backspace delete", bindingSummary(keys.Help)}
	}
	escHint := "esc close"
	if strings.TrimSpace(e.formatFilter) != "" {
		escHint = "esc clear filter"
	}
	return []string{"enter confirm", escHint, "ctrl+p up", "ctrl+n down", bindingSummary(keys.Help)}
}

func (e *exportWizardModal) HandleKey(msg tea.KeyPressMsg, _ ModalContext) ModalResult {
	keys := defaultCommandModeKeys()

	if msg.String() == "ctrl+c" {
		return modalResultReady{status: "", level: NotificationNone, dismiss: true}
	}
	if key.Matches(msg, keys.Help) {
		return modalResultForward{cmd: func() tea.Msg { return toggleHelpIntentMsg{} }}
	}

	if e.step == exportWizardStepFormat {
		return e.handleFormatKey(msg, keys)
	}
	return e.handlePathKey(msg, keys)
}

func (e *exportWizardModal) handleFormatKey(msg tea.KeyPressMsg, keys commandModeKeyMap) ModalResult {
	filtered := e.filteredFormats()
	switch {
	case key.Matches(msg, keys.Cancel):
		if strings.TrimSpace(e.formatFilter) != "" {
			e.formatFilter = ""
			e.selectedFormat = 0
		} else {
			return modalResultReady{status: "", level: NotificationNone, dismiss: true}
		}
	case key.Matches(msg, keys.PrevSuggestion), msg.String() == "up":
		if len(filtered) > 0 {
			e.selectedFormat = wrapSelection(e.selectedFormat-1, len(filtered))
		}
	case key.Matches(msg, keys.NextSuggestion), msg.String() == "down":
		if len(filtered) > 0 {
			e.selectedFormat = wrapSelection(e.selectedFormat+1, len(filtered))
		}
	case msg.String() == "enter":
		if len(filtered) > 0 {
			e.step = exportWizardStepPath
		}
	case msg.String() == "backspace" || msg.String() == "ctrl+h" || msg.String() == "delete":
		e.formatFilter = trimLastRune(e.formatFilter)
		e.selectedFormat = 0
	case len(msg.Text) > 0 && !msg.Mod.Contains(tea.ModAlt):
		e.formatFilter += msg.Text
		e.selectedFormat = 0
	}
	return modalResultNone{}
}

func (e *exportWizardModal) handlePathKey(msg tea.KeyPressMsg, keys commandModeKeyMap) ModalResult {
	switch {
	case key.Matches(msg, keys.Cancel):
		e.step = exportWizardStepFormat
		e.path = ""
	case msg.String() == "backspace" || msg.String() == "ctrl+h" || msg.String() == "delete":
		e.path = trimLastRune(e.path)
	case msg.String() == "enter":
		return modalResultExportWizard{format: e.chosenFormat(), path: e.path}
	case msg.String() == "space":
		e.path += " "
	case len(msg.Text) > 0 && !msg.Mod.Contains(tea.ModAlt):
		e.path += msg.Text
	}
	return modalResultNone{}
}

func (e *exportWizardModal) Render(interaction InteractionState, _ int) string {
	if e.step == exportWizardStepFormat {
		return e.renderFormatStep()
	}
	return e.renderPathStep(interaction)
}

func (e *exportWizardModal) renderFormatStep() string {
	filtered := e.filteredFormats()
	if len(filtered) == 0 {
		return tui.AppTheme.PanelMuted.Render("No matching formats.")
	}

	selected := wrapSelection(e.selectedFormat, len(filtered))
	lines := make([]string, 0, len(filtered))
	for i, f := range filtered {
		content := string(f)
		if i == selected {
			lines = append(lines, tui.AppTheme.PanelSelected.Render(content))
		} else {
			lines = append(lines, tui.AppTheme.PanelText.Render(content))
		}
	}
	return strings.Join(lines, "\n")
}

func (e *exportWizardModal) renderPathStep(interaction InteractionState) string {
	var lines []string

	if latest := interaction.LatestResult; latest != nil && latest.PreservedResult != nil {
		marked := len(interaction.MarkedRows)
		total := len(latest.PreservedResult.Rows)
		if marked > 0 {
			lines = append(lines, tui.AppTheme.PanelMuted.Render(fmt.Sprintf("Exporting %d selected rows", marked)))
		} else {
			lines = append(lines, tui.AppTheme.PanelMuted.Render(fmt.Sprintf("Exporting %d rows", total)))
		}
	}

	if e.cwd != "" {
		lines = append(lines, tui.AppTheme.PanelMuted.Render("cwd: "+e.cwd))
	}

	lines = append(lines, "")
	lines = append(lines, tui.AppTheme.PanelMuted.Render("Leave blank to copy to clipboard"))

	return strings.Join(lines, "\n")
}

func (e *exportWizardModal) filteredFormats() []export.Format {
	trimmed := strings.TrimSpace(e.formatFilter)
	if trimmed == "" {
		return exportWizardFormats
	}
	var result []export.Format
	for _, f := range exportWizardFormats {
		if _, ok := fuzzyMatch(trimmed, strings.ToLower(string(f))); ok {
			result = append(result, f)
		}
	}
	return result
}

func (e *exportWizardModal) chosenFormat() export.Format {
	filtered := e.filteredFormats()
	selected := wrapSelection(e.selectedFormat, len(filtered))
	if selected >= 0 && selected < len(filtered) {
		return filtered[selected]
	}
	return export.FormatCSV
}
