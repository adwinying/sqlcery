package app

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/adwinying/sqlcery/internal/tui"
)

// helpModal implements Modal for the keybindings reference overlay.
// It replaces the old HelpVisible/renderHelpSurface approach with a proper
// centered modal in the stack. contextModal records which modal was on top
// when help was opened so renderKeybindingsContent can show the right sections.
type helpModal struct {
	contextModal AppModal
}

func (h *helpModal) Name() AppModal { return ModalKeybindings }

func (h *helpModal) HandleKey(msg tea.KeyPressMsg, _ ModalContext) ModalResult {
	keys := defaultCommandModeKeys()
	switch {
	case msg.String() == "ctrl+c":
		return modalResultReady{status: "Closed keybindings.", dismiss: true}
	case key.Matches(msg, keys.Help), key.Matches(msg, keys.Cancel):
		return modalResultReady{status: "Closed keybindings.", dismiss: true}
	default:
		return modalResultNone{}
	}
}

func (h *helpModal) Render(interaction InteractionState, _ int) string {
	// Restore the context modal so renderKeybindingsContent shows the right
	// modal-specific sections even though ModalKeybindings is now on top.
	interaction.ActiveModal = h.contextModal
	return renderKeybindingsContent(interaction)
}

func (h *helpModal) FooterHints(_ InteractionState) string {
	keys := defaultCommandModeKeys()
	return strings.Join([]string{bindingSummary(keys.Cancel), bindingSummary(keys.Help)}, " | ")
}

// renderKeybindingsContent produces the content for the keybindings modal.
func renderKeybindingsContent(st InteractionState) string {
	sections := []helpSection{{
		Title: "Help",
		Lines: []string{
			"ctrl+e toggle keybindings",
			"ctrl+c quit",
		},
	}}

	commandLines := []string{
		"enter submit SQL or slash command",
		"ctrl+r open history search",
		"ctrl+y accept suggestion; ctrl+n/ctrl+p move suggestion",
		"ctrl+x switch focus; ctrl+z zoom; ctrl+1 focus results; ctrl+2 focus command; ctrl+3 command layout",
	}
	if st.ActiveModal == ModalHistorySearch {
		commandLines = append(commandLines, "history search: enter restore; ctrl+r older; ctrl+n newer; esc close")
	}
	sections = append(sections, helpSection{Title: "Command mode", Lines: commandLines})

	resultsPaneLines := []string{
		"arrows/hjkl move cell; space toggle selected row",
		"yy/cc/dd load INSERT/UPDATE/DELETE into command mode",
		":w [file] export selected rows or current result rows",
		"ctrl+u/ctrl+d scroll; ctrl+p/ctrl+n page; ctrl+x focus command",
	}
	if st.ActiveModal == ModalSlashWizard {
		resultsPaneLines = append(resultsPaneLines, "slash wizard: enter confirm; ctrl+n/ctrl+p move; esc back or close")
	}
	sections = append(sections, helpSection{Title: "Results Pane", Lines: resultsPaneLines})

	if st.Layout == LayoutSplit {
		var layoutLines []string
		if st.ActivePane == PaneResults {
			layoutLines = []string{"Results Pane [active]", "Command line"}
		} else {
			layoutLines = []string{"Results Pane", "Command line [active]"}
		}
		sections = append(sections, helpSection{Title: "Layout", Lines: layoutLines})
	}

	if st.ActiveModal == ModalHistorySearch {
		sections = append(sections, helpSection{Title: "History search", Lines: []string{
			"type to filter recent commands; enter restore selected entry",
			"ctrl+r or up select older match; ctrl+n or down select newer match",
			"esc close history search",
		}})
	}

	if st.ActiveModal == ModalSlashWizard {
		sections = append(sections, helpSection{Title: "Command wizard", Lines: []string{
			"/commands opens the guided slash command wizard",
			"enter confirm selection; ctrl+n/ctrl+p move selection",
			"esc closes command selection or steps back from table selection",
		}})
	}

	slashLines := []string{
		"/help lists slash commands; /commands opens the guided wizard",
		"/tables and /columns inspect database metadata",
		"/select, /insert, /update, /delete expand SQL templates for review",
		"/create and /drop expand DDL templates for review",
	}
	slashLines = append(slashLines, slashCommandHelpLines()...)
	sections = append(sections, helpSection{Title: "Slash commands", Lines: slashLines})

	parts := make([]string, 0, len(sections))
	for _, section := range sections {
		if len(section.Lines) == 0 {
			continue
		}
		lines := make([]string, 0, len(section.Lines)+1)
		lines = append(lines, tui.AppTheme.PanelTitle.Render(section.Title+":"))
		for _, line := range section.Lines {
			lines = append(lines, tui.AppTheme.PanelText.Render("  "+line))
		}
		parts = append(parts, strings.Join(lines, "\n"))
	}

	return strings.Join(parts, "\n\n")
}
