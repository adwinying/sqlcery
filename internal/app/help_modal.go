package app

import (
	"fmt"
	"sort"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/lipgloss"

	"github.com/adwinying/sqlcery/internal/tui"
)

const helpContentRows = tui.ModalSplitListRows

// helpRow is a single selectable entry in the Keybindings Modal.
// actionKey is "" for display-only rows, "ctrl+r" etc. for key-action rows,
// and "/tables" etc. for slash command rows. needsWizard is set when the slash
// command requires a target table and should open the Slash Command Wizard.
type helpRow struct {
	keyText     string
	desc        string
	actionKey   string
	needsWizard bool
}

// helpModal implements Modal for the interactive Keybindings Modal.
// It presents a flat, context-sensitive list of Help Rows with always-on
// filtering (same model as History Search) and row execution on Enter.
type helpModal struct {
	contextModal  AppModal
	contextPane   Pane
	filter        string
	selectedIndex int
}

func (h *helpModal) Name() AppModal { return ModalKeybindings }

func (h *helpModal) FilterText() string  { return h.filter + "█" }
func (h *helpModal) FilterLabel() string { return "Filter:" }

func (h *helpModal) Title() string { return "Keybindings" }

func (h *helpModal) CounterText(_ InteractionState) string {
	rows := h.filteredRows()
	if len(rows) == 0 {
		return ""
	}
	selected := wrapSelection(h.selectedIndex, len(rows))
	return fmt.Sprintf("%d of %d", selected+1, len(rows))
}

func (h *helpModal) HandleKey(msg tea.KeyPressMsg, ctx ModalContext) ModalResult {
	keys := defaultCommandModeKeys()
	rows := h.filteredRows()

	switch {
	case msg.String() == "ctrl+c":
		return modalResultReady{status: "", level: NotificationNone, dismiss: true}

	case key.Matches(msg, keys.Help):
		return modalResultReady{status: "", level: NotificationNone, dismiss: true}

	case key.Matches(msg, keys.Cancel):
		if strings.TrimSpace(h.filter) != "" {
			h.filter = ""
			h.selectedIndex = 0
			return modalResultReady{status: "", level: NotificationNone}
		}
		return modalResultReady{status: "", level: NotificationNone, dismiss: true}

	case key.Matches(msg, keys.PrevSuggestion), msg.String() == "up":
		return h.cycle(rows, -1)

	case key.Matches(msg, keys.NextSuggestion), msg.String() == "down":
		return h.cycle(rows, 1)

	case msg.String() == "enter":
		return h.execute(ctx, rows)

	case msg.String() == "backspace" || msg.String() == "ctrl+h" || msg.String() == "delete":
		return h.updateFilter(trimLastRune(h.filter))

	case msg.String() == "space":
		return h.updateFilter(h.filter + " ")

	case len(msg.Text) > 0 && !msg.Mod.Contains(tea.ModAlt):
		return h.updateFilter(h.filter + msg.Text)

	// Layout/zoom keys pass through so the user can rearrange panes while the modal is open.
	case key.Matches(msg, keys.LayoutCommandOnly), msg.String() == "ctrl+3", msg.String() == "alt+3":
		return modalResultForward{cmd: func() tea.Msg { return switchLayoutIntentMsg{Layout: LayoutCommandOnly} }}
	case msg.String() == "ctrl+q":
		return modalResultForward{cmd: func() tea.Msg { return focusPaneIntentMsg{Pane: PaneResults} }}
	case msg.String() == "ctrl+w":
		return modalResultForward{cmd: func() tea.Msg { return focusPaneIntentMsg{Pane: PaneCommand} }}
	case msg.String() == "ctrl+z":
		return modalResultForward{cmd: func() tea.Msg { return toggleZoomIntentMsg{} }}

	default:
		return modalResultNone{}
	}
}

func (h *helpModal) Render(_ InteractionState, _ int) string {
	rows := h.filteredRows()

	if len(rows) == 0 {
		return tui.AppTheme.PanelMuted.Render("No matching actions.")
	}

	selected := wrapSelection(h.selectedIndex, len(rows))
	vpStart := max(0, selected+1-helpContentRows)
	vpEnd := min(len(rows), vpStart+helpContentRows)

	colWidth := 0
	for _, r := range rows {
		if w := lipgloss.Width(r.keyText); w > colWidth {
			colWidth = w
		}
	}

	var lines []string
	for i := vpStart; i < vpEnd; i++ {
		content := helpRowLine(rows[i].keyText, rows[i].desc, colWidth)
		if i == selected {
			lines = append(lines, tui.AppTheme.PanelSelected.Render(content))
		} else {
			lines = append(lines, tui.AppTheme.PanelText.Render(content))
		}
	}

	return strings.Join(lines, "\n")
}

// helpRowLine right-aligns keyText within colWidth and appends two spaces and desc.
func helpRowLine(keyText, desc string, colWidth int) string {
	pad := colWidth - lipgloss.Width(keyText)
	if pad < 0 {
		pad = 0
	}
	return strings.Repeat(" ", pad) + keyText + "  " + desc
}

func (h *helpModal) StatusBarHints(_ InteractionState) []string {
	keys := defaultCommandModeKeys()
	rows := h.filteredRows()
	escHint := "esc close"
	if strings.TrimSpace(h.filter) != "" {
		escHint = "esc clear filter"
	}
	var parts []string
	if len(rows) > 0 {
		idx := wrapSelection(h.selectedIndex, len(rows))
		if rows[idx].actionKey != "" {
			parts = append(parts, "enter execute")
		}
		parts = append(parts, escHint, "ctrl+p up", "ctrl+n down")
	} else {
		parts = append(parts, escHint)
	}
	parts = append(parts, bindingSummary(keys.Help))
	return parts
}

// filteredRows returns all Help Rows for the current context, filtered by the
// current filter string using fuzzy matching, ranked by score.
func (h *helpModal) filteredRows() []helpRow {
	all := buildHelpRows(h.contextPane, h.contextModal)
	trimmed := strings.TrimSpace(h.filter)
	if trimmed == "" {
		return all
	}
	type scored struct {
		row   helpRow
		score int
	}
	matches := make([]scored, 0, len(all))
	for _, row := range all {
		if score, ok := fuzzyMatch(trimmed, row.keyText+" "+row.desc); ok {
			matches = append(matches, scored{row, score})
		}
	}
	sort.SliceStable(matches, func(i, j int) bool {
		return matches[i].score > matches[j].score
	})
	filtered := make([]helpRow, len(matches))
	for i, m := range matches {
		filtered[i] = m.row
	}
	return filtered
}

func (h *helpModal) cycle(rows []helpRow, delta int) ModalResult {
	if len(rows) == 0 {
		return modalResultNone{}
	}
	h.selectedIndex = wrapSelection(h.selectedIndex+delta, len(rows))
	return modalResultNone{}
}

func (h *helpModal) updateFilter(filter string) ModalResult {
	h.filter = filter
	h.selectedIndex = 0
	rows := h.filteredRows()
	trimmed := strings.TrimSpace(filter)
	if trimmed != "" && len(rows) == 0 {
		return modalResultReady{status: fmt.Sprintf("No actions match %q.", filter), level: NotificationInfo}
	}
	return modalResultNone{}
}

func (h *helpModal) execute(ctx ModalContext, rows []helpRow) ModalResult {
	if len(rows) == 0 {
		return modalResultReady{status: "", level: NotificationNone, dismiss: true}
	}
	idx := wrapSelection(h.selectedIndex, len(rows))
	row := rows[idx]

	if row.actionKey == "" {
		return modalResultReady{status: "", level: NotificationNone, dismiss: true}
	}

	if strings.HasPrefix(row.actionKey, "/") {
		name := strings.TrimPrefix(row.actionKey, "/")
		if row.needsWizard {
			return modalResultOpenWizardFor{
				commandName: name,
				status:      fmt.Sprintf("Choose a table for %s and press enter.", row.actionKey),
				level:       NotificationInfo,
			}
		}
		parsed := slashCommand{RawInput: row.actionKey, DisplayName: row.actionKey, Name: name}
		return modalResultExecute{
			label:  row.actionKey,
			status: fmt.Sprintf("Dispatching %s.", row.actionKey),
			level:  NotificationInfo,
			execute: executeSlashCommandCmd(slashCommandContext{
				Session: ctx.Session,
				Dialect: ctx.Dialect,
				Query:   ctx.Interaction,
			}, parsed),
		}
	}

	return modalResultRunHelpRow{msgFn: keyToMsgFn(row.actionKey)}
}

// buildHelpRows returns the flat list of Help Rows for the given context.
// Global rows (always present) are followed by context-specific rows.
func buildHelpRows(pane Pane, modal AppModal) []helpRow {
	global := []helpRow{
		{keyText: "ctrl+t", desc: "toggle keybindings"},
		{keyText: "ctrl+c", desc: "quit"},
	}

	var contextRows []helpRow

	switch modal {
	case ModalHistorySearch:
		contextRows = []helpRow{
			{keyText: "enter", desc: "restore selected entry"},
			{keyText: "ctrl+p or up", desc: "select newer match"},
			{keyText: "ctrl+n or down", desc: "select older match"},
			{keyText: "esc", desc: "close history search"},
		}
	case ModalSlashWizard:
		contextRows = []helpRow{
			{keyText: "enter", desc: "confirm selection"},
			{keyText: "ctrl+n", desc: "next item"},
			{keyText: "ctrl+p", desc: "previous item"},
			{keyText: "space", desc: "toggle column (column step)"},
			{keyText: "a", desc: "toggle all columns (column step)"},
			{keyText: "alt+← →", desc: "scroll long lines"},
			{keyText: "esc", desc: "back or close"},
		}
	default:
		switch pane {
		case PaneResults:
			contextRows = []helpRow{
				{keyText: "arrows or hjkl", desc: "move cell"},
				{keyText: "space", desc: "toggle selected row"},
				{keyText: "yy", desc: "load INSERT into command pane", actionKey: "yy"},
				{keyText: "cc", desc: "load UPDATE into command pane", actionKey: "cc"},
				{keyText: "dd", desc: "load DELETE into command pane", actionKey: "dd"},
				{keyText: "ctrl+e", desc: "export selected or current rows"},
				{keyText: "ctrl+u", desc: "scroll up"},
				{keyText: "ctrl+d", desc: "scroll down"},
				{keyText: "ctrl+p", desc: "previous page"},
				{keyText: "ctrl+n", desc: "next page"},
				{keyText: "ctrl+x", desc: "focus command pane", actionKey: "ctrl+x"},
				{keyText: "ctrl+z", desc: "zoom / unzoom", actionKey: "ctrl+z"},
				{keyText: "ctrl+1", desc: "focus results pane", actionKey: "ctrl+1"},
				{keyText: "ctrl+2", desc: "focus command pane", actionKey: "ctrl+2"},
				{keyText: "ctrl+3", desc: "command-only layout", actionKey: "ctrl+3"},
			}
		default: // PaneCommand
			contextRows = []helpRow{
				{keyText: "enter", desc: "submit SQL or slash command", actionKey: "enter"},
				{keyText: "ctrl+r", desc: "open history search", actionKey: "ctrl+r"},
				{keyText: "ctrl+e", desc: "open command in $EDITOR", actionKey: "ctrl+e"},
				{keyText: "ctrl+u", desc: "scroll up"},
				{keyText: "ctrl+d", desc: "scroll down"},
				{keyText: "ctrl+n", desc: "next autocomplete suggestion"},
				{keyText: "ctrl+p", desc: "previous autocomplete suggestion"},
				{keyText: "ctrl+x", desc: "switch focus", actionKey: "ctrl+x"},
				{keyText: "ctrl+z", desc: "zoom / unzoom", actionKey: "ctrl+z"},
				{keyText: "ctrl+1", desc: "focus results pane", actionKey: "ctrl+1"},
				{keyText: "ctrl+2", desc: "focus command pane", actionKey: "ctrl+2"},
				{keyText: "ctrl+3", desc: "command-only layout", actionKey: "ctrl+3"},
			}
			for _, spec := range slashCommandSpecs() {
				s := spec
				contextRows = append(contextRows, helpRow{
					keyText:     "/" + s.Name,
					desc:        s.Summary,
					actionKey:   "/" + s.Name,
					needsWizard: s.NeedsTarget,
				})
			}
		}
	}

	return append(global, contextRows...)
}

// keyToMsgFn maps a key action string to the intent message it should emit
// after the Keybindings Modal closes.
func keyToMsgFn(actionKey string) func() tea.Msg {
	switch actionKey {
	case "enter":
		return func() tea.Msg { return submitIntentMsg{} }
	case "ctrl+r":
		return func() tea.Msg { return historyIntentMsg{} }
	case "ctrl+e":
		return func() tea.Msg { return openEditorIntentMsg{} }
	case "ctrl+x":
		return func() tea.Msg { return switchPaneIntentMsg{} }
	case "ctrl+z":
		return func() tea.Msg { return toggleZoomIntentMsg{} }
	case "ctrl+1":
		return func() tea.Msg { return focusPaneIntentMsg{Pane: PaneResults} }
	case "ctrl+2":
		return func() tea.Msg { return focusPaneIntentMsg{Pane: PaneCommand} }
	case "ctrl+3":
		return func() tea.Msg { return switchLayoutIntentMsg{Layout: LayoutCommandOnly} }
	case "yy":
		return func() tea.Msg { return composeResultsPaneIntentMsg{action: "insert"} }
	case "cc":
		return func() tea.Msg { return composeResultsPaneIntentMsg{action: "update"} }
	case "dd":
		return func() tea.Msg { return composeResultsPaneIntentMsg{action: "delete"} }
	default:
		return nil
	}
}

