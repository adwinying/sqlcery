package app

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/adwinying/sqlcery/internal/db"
	"github.com/adwinying/sqlcery/internal/tui"
)

const (
	defaultEditorWidth = 80
	minimumEditorWidth = 20
	// autocompletePanelRows and bottomPadding mirror tui constants; kept for test access.
	autocompletePanelRows = 5
	bottomPadding         = 5
	editorLargeHeight     = 9999
)

type commandModeModel struct {
	editor      textarea.Model
	keys        commandModeKeyMap
	widget      tui.EditorWidget
	innerWidth  int
	innerHeight int
	// autocomplete gate state
	autocompleteOpenedByTyping   bool
	autocompleteSuppressed       bool
	autocompleteSuppressedValue  string
	autocompleteSuppressedCursor int
	// pre-computed in Update; used by buildViewContext and footer methods
	cachedSuggestions []tui.AutocompleteSuggestion
}

type commandModeKeyMap struct {
	Submit               key.Binding
	Cancel               key.Binding
	Help                 key.Binding
	History              key.Binding
	RestoreHistory       key.Binding
	SwitchMode           key.Binding
	LayoutCommandOnly    key.Binding
	AcceptSuggestion     key.Binding
	NextSuggestion       key.Binding
	PrevSuggestion       key.Binding
	ScrollTranscriptUp   key.Binding
	ScrollTranscriptDown key.Binding
}

func defaultCommandModeKeys() commandModeKeyMap {
	return commandModeKeyMap{
		Submit:               key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "submit")),
		Cancel:               key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
		Help:                 key.NewBinding(key.WithKeys("ctrl+e"), key.WithHelp("ctrl+e", "keybindings")),
		History:              key.NewBinding(key.WithKeys("ctrl+r"), key.WithHelp("ctrl+r", "history")),
		RestoreHistory:       key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "restore")),
		SwitchMode:           key.NewBinding(key.WithKeys("ctrl+x"), key.WithHelp("ctrl+x", "focus")),
		LayoutCommandOnly:    key.NewBinding(key.WithKeys("ctrl+3"), key.WithHelp("ctrl+3", "command")),
		AcceptSuggestion:     key.NewBinding(key.WithKeys("tab", "ctrl+y"), key.WithHelp("tab/ctrl+y", "accept")),
		NextSuggestion:       key.NewBinding(key.WithKeys("ctrl+n"), key.WithHelp("ctrl+n", "next suggestion")),
		PrevSuggestion:       key.NewBinding(key.WithKeys("ctrl+p"), key.WithHelp("ctrl+p", "prev suggestion")),
		ScrollTranscriptUp:   key.NewBinding(key.WithKeys("ctrl+u"), key.WithHelp("ctrl+u", "scroll up")),
		ScrollTranscriptDown: key.NewBinding(key.WithKeys("ctrl+d"), key.WithHelp("ctrl+d", "scroll down")),
	}
}

func newCommandModeModel() commandModeModel {
	editor := textarea.New()
	editor.Prompt = "> "
	editor.Placeholder = "Write SQL here"
	editor.ShowLineNumbers = false
	editor.SetWidth(defaultEditorWidth)
	editor.SetHeight(editorLargeHeight)
	editor.Focus()

	return commandModeModel{
		editor:      editor,
		widget:      tui.NewEditorWidget(),
		innerWidth:  defaultEditorWidth,
		innerHeight: 1,
		keys:        defaultCommandModeKeys(),
	}
}

func (m commandModeModel) Init() tea.Cmd {
	return textarea.Blink
}

func (m commandModeModel) Update(msg tea.Msg, interaction InteractionState) (commandModeModel, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyPressMsg); ok {
		suggestions := m.computeSuggestions(interaction)
		switch {
		case key.Matches(keyMsg, m.keys.AcceptSuggestion):
			if len(suggestions) > 0 {
				m.applySuggestion(suggestions[m.widget.SelectedSuggestionIndex(len(suggestions))])
				m.autocompleteOpenedByTyping = false
				return m, textarea.Blink
			}
		case key.Matches(keyMsg, m.keys.NextSuggestion):
			if len(suggestions) > 0 {
				m.widget.SelectNextSuggestion(len(suggestions))
				return m, nil
			}
		case key.Matches(keyMsg, m.keys.PrevSuggestion):
			if len(suggestions) > 0 {
				m.widget.SelectPrevSuggestion(len(suggestions))
				return m, nil
			}
		case key.Matches(keyMsg, m.keys.ScrollTranscriptUp):
			step := max(1, m.innerHeight/2)
			naturalTop := m.widget.ComputeNaturalScrollTop(m.buildViewContext(interaction))
			m.widget.ScrollUp(step, naturalTop)
			m.autocompleteOpenedByTyping = false
			return m, nil
		case key.Matches(keyMsg, m.keys.ScrollTranscriptDown):
			step := max(1, m.innerHeight/2)
			m.widget.ScrollDown(step)
			m.autocompleteOpenedByTyping = false
			return m, nil
		}
	}

	priorValue := m.editor.Value()
	priorCursor := m.cursorOffset()
	_, isKeyPress := msg.(tea.KeyPressMsg)

	var cmd tea.Cmd
	m.editor, cmd = m.editor.Update(msg)

	if isKeyPress {
		valueChanged := m.editor.Value() != priorValue
		cursorMoved := m.cursorOffset() != priorCursor
		if valueChanged || cursorMoved {
			m.widget.SnapToBottom()
		}
		if valueChanged {
			m.autocompleteOpenedByTyping = true
		} else {
			m.autocompleteOpenedByTyping = false
		}
	}

	m.cachedSuggestions = m.computeSuggestions(interaction)
	m.widget.ClampSuggestionSelection(len(m.cachedSuggestions))
	return m, cmd
}

func (m *commandModeModel) Clear() {
	m.editor.Reset()
	m.editor.Focus()
	m.widget.ResetSuggestionSelection()
	m.cachedSuggestions = nil
	m.autocompleteOpenedByTyping = false
	m.clearAutocompleteSuppression()
}

// AutocompleteVisible reports whether the autocomplete dropdown is currently
// showing suggestions. Call after Update to read the cached result.
func (m commandModeModel) AutocompleteVisible(interaction InteractionState) bool {
	if len(m.cachedSuggestions) > 0 {
		return true
	}
	// Fallback: compute inline for callers that haven't gone through Update.
	return len(m.computeSuggestions(interaction)) > 0
}

// DismissAutocomplete suppresses the autocomplete dropdown while the editor
// value and cursor position remain unchanged.
func (m *commandModeModel) DismissAutocomplete() {
	m.autocompleteSuppressed = true
	m.autocompleteSuppressedValue = m.editor.Value()
	m.autocompleteSuppressedCursor = m.cursorOffset()
	m.widget.ResetSuggestionSelection()
	m.cachedSuggestions = nil
	m.autocompleteOpenedByTyping = false
}

func (m *commandModeModel) clearAutocompleteSuppression() {
	m.autocompleteSuppressed = false
	m.autocompleteSuppressedValue = ""
	m.autocompleteSuppressedCursor = 0
}

func (m *commandModeModel) Focus() {
	m.editor.Focus()
	m.autocompleteOpenedByTyping = false
}

func (m *commandModeModel) Blur() {
	m.editor.Blur()
	m.autocompleteOpenedByTyping = false
}

// SetEditorValue replaces the editor buffer programmatically without opening
// the autocomplete menu.
func (m *commandModeModel) SetEditorValue(value string) {
	m.editor.SetValue(value)
	m.editor.CursorEnd()
	m.widget.ResetSuggestionSelection()
	m.cachedSuggestions = nil
	m.autocompleteOpenedByTyping = false
	m.clearAutocompleteSuppression()
}

func (m commandModeModel) KeyMap() commandModeKeyMap {
	return m.keys
}

func (m commandModeModel) Value() string {
	return m.editor.Value()
}

func (m *commandModeModel) SetSize(innerWidth, innerHeight int) {
	m.innerWidth = clampEditorSize(innerWidth, minimumEditorWidth)
	m.innerHeight = innerHeight
	m.editor.SetWidth(m.innerWidth)
	m.editor.SetHeight(editorLargeHeight)
}

func (m commandModeModel) Focused() bool {
	return m.editor.Focused()
}

func (m commandModeModel) View(interaction InteractionState) string {
	return m.widget.View(m.buildViewContext(interaction))
}

func (m commandModeModel) FooterHints(interaction InteractionState) string {
	var parts []string
	if len(m.cachedSuggestions) > 0 || len(m.computeSuggestions(interaction)) > 0 {
		parts = append(parts, bindingSummary(m.keys.AcceptSuggestion), bindingSummary(m.keys.NextSuggestion), bindingSummary(m.keys.PrevSuggestion))
	}
	parts = append(parts, "enter submit", bindingSummary(m.keys.Cancel), bindingSummary(m.keys.History))
	if running := formatRunningIndicator(interaction.Running); running != "" {
		parts = append(parts, "esc cancel query")
	}
	parts = append(parts, "ctrl+c quit", bindingSummary(m.keys.Help))
	return strings.Join(parts, " | ")
}

func (m commandModeModel) Footer(connectionName, dialect string, interaction InteractionState) string {
	modeLabel := "Command mode"
	if interaction.ActiveModal == ModalHistorySearch {
		modeLabel = "History search"
	} else if interaction.ActivePane == PaneResults && interaction.Layout == LayoutSplit {
		modeLabel = "Command line hidden focus"
	}
	parts := []string{modeLabel, fmt.Sprintf("layout %s", layoutLabel(interaction.Layout))}

	if label := strings.TrimSpace(connectionName); label != "" {
		parts = append(parts, fmt.Sprintf("connection %s", label))
	}
	if label := strings.TrimSpace(dialect); label != "" {
		parts = append(parts, label)
	}
	if selectedCount := len(interaction.MarkedRows); selectedCount > 0 {
		parts = append(parts, fmt.Sprintf("%d selected", selectedCount))
	}

	parts = append(parts, fmt.Sprintf("line %d col %d", m.editor.Line()+1, m.editor.LineInfo().ColumnOffset+1))
	parts = append(parts, bindingSummary(m.keys.Submit), bindingSummary(m.keys.Cancel), bindingSummary(m.keys.History), bindingSummary(m.keys.SwitchMode), bindingSummary(m.keys.LayoutCommandOnly), bindingSummary(m.keys.Help))
	if interaction.ActivePane == PaneResults {
		parts = append(parts, "ctrl+u scroll up", "ctrl+d scroll down")
	}
	if interaction.ActiveModal == ModalHistorySearch {
		parts = append(parts, bindingSummary(m.keys.RestoreHistory), bindingSummary(m.keys.NextSuggestion), bindingSummary(m.keys.PrevSuggestion))
	}
	if interaction.ActiveModal == ModalSlashWizard {
		parts = append(parts, "wizard /commands")
	}
	if running := formatRunningIndicator(interaction.Running); running != "" {
		parts = append(parts, running)
		parts = append(parts, "esc cancel query")
	}
	if len(m.cachedSuggestions) > 0 || len(m.computeSuggestions(interaction)) > 0 {
		parts = append(parts, bindingSummary(m.keys.AcceptSuggestion), bindingSummary(m.keys.NextSuggestion), bindingSummary(m.keys.PrevSuggestion))
	}
	parts = append(parts, "ctrl+c quit")

	return tui.AppTheme.Footer.Render(strings.Join(parts, " | "))
}

func (m *commandModeModel) AppendReplEntry(prompt, sql, output string) {
	m.widget.AppendEntry(prompt, sql, output)
}

// buildViewContext maps the textarea state and cached autocomplete suggestions
// into an EditorViewContext for stateless rendering by the EditorWidget.
func (m commandModeModel) buildViewContext(interaction InteractionState) tui.EditorViewContext {
	lineInfo := m.editor.LineInfo()
	_ = interaction // suggestions already cached; interaction used only for legacy callers
	return tui.EditorViewContext{
		Value:           m.editor.Value(),
		Lines:           tui.SplitEditorLines(m.editor.Value()),
		CursorLine:      m.editor.Line(),
		RowOffset:       lineInfo.RowOffset,
		ColOffset:       lineInfo.ColumnOffset,
		CharOffset:      lineInfo.CharOffset,
		Width:           m.editor.Width(),
		Height:          m.innerHeight,
		Prompt:          m.editor.Prompt,
		Placeholder:     m.editor.Placeholder,
		ShowLineNumbers: m.editor.ShowLineNumbers,
		MaxHeight:       m.editor.MaxHeight,
		AutocompleteSuggestions: m.cachedSuggestions,
		GhostText:               m.ghostText(),
		PromptWidth:             ansi.StringWidth(m.editor.Prompt),
	}
}

// computeSuggestions computes the current autocomplete suggestions from the
// editor state and interaction context. The result is cached in Update so that
// View rendering never calls buildAutocompleteItems.
func (m commandModeModel) computeSuggestions(interaction InteractionState) []tui.AutocompleteSuggestion {
	if !m.autocompleteOpenedByTyping {
		return nil
	}
	if m.autocompleteSuppressed &&
		m.editor.Value() == m.autocompleteSuppressedValue &&
		m.cursorOffset() == m.autocompleteSuppressedCursor {
		return nil
	}
	items := buildAutocompleteItems(m.editor.Value(), m.cursorOffset(), interaction)
	result := make([]tui.AutocompleteSuggestion, len(items))
	for i, item := range items {
		result[i] = tui.AutocompleteSuggestion{
			Label:      item.Label,
			InsertText: item.InsertText,
			Kind:       item.Kind,
			Detail:     item.Detail,
		}
	}
	return result
}

// ghostText returns the inline completion suffix to show after the cursor.
// It uses the already-cached suggestions so that no computation happens at
// render time.
func (m commandModeModel) ghostText() string {
	if len(m.cachedSuggestions) == 0 {
		return ""
	}
	selected := m.cachedSuggestions[m.widget.SelectedSuggestionIndex(len(m.cachedSuggestions))]
	ctx := analyzeAutocompleteContext(m.editor.Value(), m.cursorOffset())
	prefix := strings.ToLower(m.editor.Value()[ctx.ReplaceStart:ctx.ReplaceEnd])
	insert := selected.InsertText
	if len(insert) <= len(prefix) {
		return ""
	}
	if !strings.HasPrefix(strings.ToLower(insert), prefix) {
		return ""
	}
	return insert[len(prefix):]
}

func (m *commandModeModel) applySuggestion(item tui.AutocompleteSuggestion) {
	value := []rune(m.editor.Value())
	ctx := analyzeAutocompleteContext(m.editor.Value(), m.cursorOffset())
	start := clampCursorOffset(ctx.ReplaceStart, len(value))
	end := clampCursorOffset(ctx.ReplaceEnd, len(value))
	insert := []rune(item.InsertText)
	updated := string(append(append(append([]rune(nil), value[:start]...), insert...), value[end:]...))
	cursor := start + len(insert)
	m.editor.SetValue(updated)
	m.setCursorOffset(cursor)
	m.widget.ResetSuggestionSelection()
}

func clampCursorOffset(value, size int) int {
	if value < 0 {
		return 0
	}
	if value > size {
		return size
	}
	return value
}

func (m *commandModeModel) setCursorOffset(offset int) {
	lines := splitEditorLines(m.editor.Value())
	row, col := rowColFromCursorOffset(lines, offset)
	for m.editor.Line() > row {
		m.editor.CursorUp()
	}
	for m.editor.Line() < row {
		m.editor.CursorDown()
	}
	m.editor.SetCursorColumn(col)
}

func rowColFromCursorOffset(lines []string, offset int) (int, int) {
	remaining := max(0, offset)
	for row, line := range lines {
		length := len([]rune(line))
		if remaining <= length {
			return row, remaining
		}
		remaining -= length
		if row < len(lines)-1 {
			remaining--
		}
	}
	if len(lines) == 0 {
		return 0, 0
	}
	last := len(lines) - 1
	return last, len([]rune(lines[last]))
}

func (m commandModeModel) cursorOffset() int {
	lines := splitEditorLines(m.editor.Value())
	offset := 0
	for i := 0; i < m.editor.Line() && i < len(lines); i++ {
		offset += len([]rune(lines[i])) + 1
	}
	return offset + m.editor.LineInfo().CharOffset
}

// splitEditorLines splits a SQL editor value into logical lines.
// Thin wrapper over tui.SplitEditorLines kept for package-internal callers.
func splitEditorLines(value string) []string {
	return tui.SplitEditorLines(value)
}

// computeNaturalScrollTop returns the natural scroll top for the current content.
// Kept as a wrapper for test access.
func (m commandModeModel) computeNaturalScrollTop(viewportH int) int {
	ctx := tui.EditorViewContext{
		Value:  m.editor.Value(),
		Lines:  tui.SplitEditorLines(m.editor.Value()),
		Width:  m.editor.Width(),
		Height: viewportH,
	}
	return m.widget.ComputeNaturalScrollTop(ctx)
}

// autocompleteItems returns the current suggestions as a slice.
// Kept as a thin wrapper for test access; callers in production code should
// use cachedSuggestions instead.
func (m commandModeModel) autocompleteItems(interaction InteractionState) []tui.AutocompleteSuggestion {
	return m.computeSuggestions(interaction)
}

// renderAutocompleteDropdown renders the dropdown overlay for tests that call
// it directly. In production, the widget.View() handles this internally.
func (m commandModeModel) renderAutocompleteDropdown(interaction InteractionState) string {
	ctx := m.buildViewContext(interaction)
	ctx.AutocompleteSuggestions = m.computeSuggestions(interaction)
	return m.widget.RenderDropdown(ctx)
}

func bindingSummary(binding key.Binding) string {
	help := binding.Help()
	return strings.TrimSpace(help.Key + " " + help.Desc)
}

func clampEditorSize(value, minimum int) int {
	if value < minimum {
		return minimum
	}
	return value
}

func renderInlineResult(interaction InteractionState) string {
	latest := interaction.LatestResult
	if latest == nil || latest.OriginPane != PaneCommand {
		return ""
	}
	if latest.StatementKind == db.StatementResultKindExec {
		return renderInlineExecResult(latest)
	}
	if latest.InlineResult == nil {
		return ""
	}
	return renderInlineQueryResult(latest)
}

func renderSlashWizardContext(wizard *SlashCommandWizardContext, hScrollOffset *int, innerWidth int) string {
	if wizard == nil {
		return ""
	}

	lines := []string{tui.AppTheme.PanelTitle.Render("Command wizard:")}
	switch wizard.Step {
	case SlashCommandWizardStepColumn:
		selectedCommand, _ := slashWizardCommandByIndex(wizard)
		selectedTarget, _ := slashWizardFilteredTargetByIndex(wizard)
		lines = append(lines,
			tui.AppTheme.PanelMuted.Render(fmt.Sprintf("Step 1/3 complete: %s", selectedCommand.DisplayName)),
			tui.AppTheme.PanelMuted.Render(fmt.Sprintf("Step 2/3 complete: %s", selectedTarget.Display)),
			tui.AppTheme.PanelText.Render(fmt.Sprintf("Step 3/3: choose columns for %s", selectedTarget.Display)),
		)
		const columnHeaderLines = 4
		listViewport := tui.ModalFixedRows - columnHeaderLines
		if listViewport < 1 {
			listViewport = 1
		}
		selected := clampWizardIndex(wizard.SelectedColumnCursor, len(wizard.Columns))
		scrollOffset := max(0, selected-listViewport+1)
		viewEnd := min(len(wizard.Columns), scrollOffset+listViewport)

		if len(wizard.Columns) == 0 {
			lines = append(lines, tui.AppTheme.PanelMuted.Render("No columns available."))
		} else {
			for i := scrollOffset; i < viewEnd; i++ {
				col := wizard.Columns[i]
				check := "[ ]"
				if col.Selected {
					check = "[x]"
				}
				var content string
				if col.Type != "" {
					content = fmt.Sprintf("%s %s  %s", check, col.Name, col.Type)
				} else {
					content = fmt.Sprintf("%s %s", check, col.Name)
				}
				if i == selected {
					*hScrollOffset = tui.ClampHScrollOffset(ansi.StringWidth("> "+content), *hScrollOffset, innerWidth)
					lines = append(lines, tui.AppTheme.PanelSelected.Render(tui.ApplyHScroll("> "+content, *hScrollOffset, innerWidth)))
				} else {
					lines = append(lines, tui.AppTheme.PanelText.Render("  "+content))
				}
			}
		}
	case SlashCommandWizardStepTarget:
		selectedCommand, _ := slashWizardCommandByIndex(wizard)
		totalSteps := 2
		if selectedCommand.NeedsColumns {
			totalSteps = 3
		}
		headerLines := 1
		if wizard.DirectInvocation {
			lines = append(lines,
				tui.AppTheme.PanelText.Render(fmt.Sprintf("Choose a table for %s:", selectedCommand.DisplayName)),
			)
			headerLines++
		} else {
			lines = append(lines,
				tui.AppTheme.PanelMuted.Render(fmt.Sprintf("Step 1/%d complete: %s", totalSteps, selectedCommand.DisplayName)),
				tui.AppTheme.PanelText.Render(fmt.Sprintf("Step 2/%d: choose a table for %s", totalSteps, selectedCommand.DisplayName)),
			)
			headerLines += 2
		}
		lines = append(lines, tui.AppTheme.PanelText.Render(fmt.Sprintf("filter> %s", defaultWizardFilter(wizard.TargetFilter))))
		headerLines++

		filteredTargets := filterWizardTargets(wizard.Targets, wizard.TargetFilter)
		listViewport := tui.ModalFixedRows - headerLines
		if listViewport < 1 {
			listViewport = 1
		}
		selected := clampWizardIndex(wizard.SelectedTarget, len(filteredTargets))
		scrollOffset := max(0, selected-listViewport+1)
		viewEnd := min(len(filteredTargets), scrollOffset+listViewport)

		if len(filteredTargets) == 0 {
			lines = append(lines, tui.AppTheme.PanelMuted.Render("No matching tables."))
		} else {
			for i := scrollOffset; i < viewEnd; i++ {
				target := filteredTargets[i]
				if i == selected {
					content := "> " + target.Display
					*hScrollOffset = tui.ClampHScrollOffset(ansi.StringWidth(content), *hScrollOffset, innerWidth)
					lines = append(lines, tui.AppTheme.PanelSelected.Render(tui.ApplyHScroll(content, *hScrollOffset, innerWidth)))
				} else {
					lines = append(lines, tui.AppTheme.PanelText.Render("  "+target.Display))
				}
			}
		}
	default:
		selectedCommand, _ := slashWizardCommandByIndex(wizard)
		totalSteps := 2
		if selectedCommand.NeedsColumns {
			totalSteps = 3
		}
		lines = append(lines, tui.AppTheme.PanelText.Render(fmt.Sprintf("Step 1/%d: choose a slash command", totalSteps)))
		const headerLines = 2
		listViewport := tui.ModalFixedRows - headerLines
		if listViewport < 1 {
			listViewport = 1
		}
		selected := clampWizardIndex(wizard.SelectedCommand, len(wizard.Commands))
		scrollOffset := max(0, selected-listViewport+1)
		viewEnd := min(len(wizard.Commands), scrollOffset+listViewport)
		for i := scrollOffset; i < viewEnd; i++ {
			command := wizard.Commands[i]
			line := fmt.Sprintf("%s - %s", command.DisplayName, command.Summary)
			if command.NeedsTarget {
				line += " (choose table next)"
			}
			if i == selected {
				*hScrollOffset = tui.ClampHScrollOffset(ansi.StringWidth("> "+line), *hScrollOffset, innerWidth)
				lines = append(lines, tui.AppTheme.PanelSelected.Render(tui.ApplyHScroll("> "+line, *hScrollOffset, innerWidth)))
			} else {
				lines = append(lines, tui.AppTheme.PanelText.Render("  "+line))
			}
		}
	}

	return strings.Join(lines, "\n")
}

func clampWizardIndex(index, size int) int {
	if size <= 0 || index < 0 {
		return 0
	}
	if index >= size {
		return size - 1
	}
	return index
}

func renderInlineExecResult(latest *LatestResultContext) string {
	parts := []string{tui.AppTheme.ResultTitle.Render("Results:")}
	if latest.RowsAffected != nil {
		label := "rows"
		if *latest.RowsAffected == 1 {
			label = "row"
		}
		parts = append(parts, tui.AppTheme.ResultSummary.Render(fmt.Sprintf("%d %s affected", *latest.RowsAffected, label)))
	} else {
		parts = append(parts, tui.AppTheme.ResultSummary.Render("Statement executed successfully"))
	}
	if latest.LastInsertID != nil && *latest.LastInsertID != 0 {
		parts = append(parts, tui.AppTheme.ResultSummary.Render(fmt.Sprintf("last insert id %d", *latest.LastInsertID)))
	}
	return strings.Join(parts, "\n")
}

func renderInlineQueryResult(latest *LatestResultContext) string {
	result := latest.InlineResult

	columns := make([]string, 0, len(result.Columns))
	widths := make([]int, 0, len(result.Columns))
	for _, column := range result.Columns {
		name := strings.TrimSpace(column.Name)
		if name == "" {
			name = fmt.Sprintf("column_%d", len(columns)+1)
		}
		columns = append(columns, name)
		widths = append(widths, runeWidth(name))
	}

	for _, row := range result.Rows {
		for i, value := range row.Values {
			formatted := formatInlineResultValue(value)
			if runeWidth(formatted) > widths[i] {
				widths[i] = runeWidth(formatted)
			}
		}
	}

	headerLine := tui.AppTheme.ResultHeader.Render(renderInlineResultLine(columns, widths))
	lines := []string{tui.AppTheme.ResultTitle.Render("Results:"), headerLine, renderInlineSeparator(widths)}
	for _, row := range result.Rows {
		values := make([]string, len(row.Values))
		for i, value := range row.Values {
			values[i] = formatInlineResultValue(value)
		}
		lines = append(lines, renderInlineResultLine(values, widths))
	}

	if len(result.Rows) == 0 {
		lines = append(lines, tui.AppTheme.ResultsPaneEmpty.Render("(no rows)"))
	}

	rowCount := len(result.Rows)
	if latest.InlineRowsTruncated && latest.PreservedResult != nil {
		lines = append(lines, tui.AppTheme.PanelHint.Render(fmt.Sprintf("Showing first %d of %d rows.", rowCount, len(latest.PreservedResult.Rows))))
		return strings.Join(lines, "\n")
	}

	if rowCount == 1 {
		lines = append(lines, tui.AppTheme.ResultSummary.Render("1 row."))
	} else {
		lines = append(lines, tui.AppTheme.ResultSummary.Render(fmt.Sprintf("%d rows.", rowCount)))
	}

	return strings.Join(lines, "\n")
}

func renderInlineResultLine(values []string, widths []int) string {
	parts := make([]string, 0, len(values))
	for i, value := range values {
		padding := widths[i] - runeWidth(value)
		parts = append(parts, value+strings.Repeat(" ", max(0, padding)))
	}
	return strings.Join(parts, " | ")
}

func renderInlineSeparator(widths []int) string {
	parts := make([]string, 0, len(widths))
	for _, width := range widths {
		parts = append(parts, strings.Repeat("-", max(3, width)))
	}
	return tui.AppTheme.ResultSeparator.Render(strings.Join(parts, "-+-"))
}

func formatInlineResultValue(value db.ResultValue) string {
	switch value.Kind {
	case db.ValueKindNull:
		return "NULL"
	case db.ValueKindBool:
		if typed, ok := value.Value.(bool); ok {
			if typed {
				return "true"
			}
			return "false"
		}
	case db.ValueKindInteger, db.ValueKindFloat, db.ValueKindDecimal, db.ValueKindString:
		return truncateNewlines(fmt.Sprint(value.Value))
	case db.ValueKindBytes:
		if typed, ok := value.Value.([]byte); ok {
			return fmt.Sprintf("0x%x", typed)
		}
	case db.ValueKindTime:
		if typed, ok := value.Value.(time.Time); ok {
			return typed.Format("2006-01-02 15:04:05")
		}
	}
	if value.Value == nil {
		return "NULL"
	}
	return truncateNewlines(fmt.Sprint(value.Value))
}

func runeWidth(value string) int {
	return ansi.StringWidth(value)
}

func filterWizardTargets(targets []SlashCommandWizardTarget, filter string) []SlashCommandWizardTarget {
	trimmed := strings.TrimSpace(filter)
	if trimmed == "" {
		return targets
	}
	lower := strings.ToLower(trimmed)
	filtered := make([]SlashCommandWizardTarget, 0, len(targets))
	for _, t := range targets {
		if strings.Contains(strings.ToLower(t.Display), lower) {
			filtered = append(filtered, t)
		}
	}
	return filtered
}

func defaultWizardFilter(value string) string {
	if strings.TrimSpace(value) == "" {
		return "(empty)"
	}
	return value
}

func renderGeneratedStatementWarning(sql string) string {
	switch leadingSQLKeyword(sql) {
	case "DELETE":
		return tui.AppTheme.WarningNotice.Render("Warning: generated DELETE statement. Review carefully before submitting.")
	case "DROP":
		return tui.AppTheme.WarningNotice.Render("Warning: generated DROP statement. Review carefully before submitting.")
	default:
		return ""
	}
}
