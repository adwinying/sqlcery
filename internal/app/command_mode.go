package app

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/adwinying/sqlcery/internal/db"
)

const (
	defaultEditorWidth    = 80
	defaultEditorHeight   = 12
	minimumEditorWidth    = 20
	minimumEditorHeight   = 6
	autocompletePanelRows = 4
)

type replTranscriptEntry struct {
	Prompt string
	SQL    string
	Output string
}

type commandModeModel struct {
	editor             textarea.Model
	keys               commandModeKeyMap
	scrollTop          int
	highlighter        sqlSyntaxHighlighter
	selectedSuggestion int
	replTranscript     []replTranscriptEntry
}

type commandModeKeyMap struct {
	Submit            key.Binding
	Cancel            key.Binding
	Help              key.Binding
	History           key.Binding
	RestoreHistory    key.Binding
	SwitchMode        key.Binding
	LayoutSplit       key.Binding
	LayoutCommandOnly key.Binding
	LayoutViewerOnly  key.Binding
	AcceptSuggestion  key.Binding
	NextSuggestion    key.Binding
	PrevSuggestion    key.Binding
}

func newCommandModeModel() commandModeModel {
	editor := textarea.New()
	editor.Prompt = "sqlcery> "
	editor.Placeholder = "Write SQL here"
	editor.ShowLineNumbers = false
	editor.SetWidth(defaultEditorWidth)
	editor.SetHeight(defaultEditorHeight)
	editor.Focus()

	return commandModeModel{
		editor:             editor,
		highlighter:        newSQLSyntaxHighlighter(),
		selectedSuggestion: 0,
		keys: commandModeKeyMap{
			Submit:            key.NewBinding(key.WithKeys("ctrl+g"), key.WithHelp("ctrl+g", "submit")),
			Cancel:            key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "clear/cancel")),
			Help:              key.NewBinding(key.WithKeys("alt+h"), key.WithHelp("alt+h", "help")),
			History:           key.NewBinding(key.WithKeys("ctrl+r"), key.WithHelp("ctrl+r", "history")),
			RestoreHistory:    key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "restore")),
			SwitchMode:        key.NewBinding(key.WithKeys("ctrl+x"), key.WithHelp("ctrl+x", "focus")),
			LayoutSplit:       key.NewBinding(key.WithKeys("ctrl+1"), key.WithHelp("ctrl+1", "split")),
			LayoutViewerOnly:  key.NewBinding(key.WithKeys("ctrl+2"), key.WithHelp("ctrl+2", "viewer")),
			LayoutCommandOnly: key.NewBinding(key.WithKeys("ctrl+3"), key.WithHelp("ctrl+3", "command")),
			AcceptSuggestion:  key.NewBinding(key.WithKeys("tab", "ctrl+y"), key.WithHelp("tab/ctrl+y", "accept")),
			NextSuggestion:    key.NewBinding(key.WithKeys("alt+n"), key.WithHelp("alt+n", "next suggestion")),
			PrevSuggestion:    key.NewBinding(key.WithKeys("alt+p"), key.WithHelp("alt+p", "prev suggestion")),
		},
	}
}

func (m commandModeModel) Init() tea.Cmd {
	return textarea.Blink
}

func (m commandModeModel) Update(msg tea.Msg, query QueryContext) (commandModeModel, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		suggestions := m.autocompleteItems(query)
		switch {
		case key.Matches(keyMsg, m.keys.AcceptSuggestion):
			if len(suggestions) > 0 {
				m.applySuggestion(suggestions[m.selectedSuggestionIndex(len(suggestions))])
				return m, textarea.Blink
			}
		case key.Matches(keyMsg, m.keys.NextSuggestion):
			if len(suggestions) > 0 {
				m.selectedSuggestion = (m.selectedSuggestionIndex(len(suggestions)) + 1) % len(suggestions)
				return m, nil
			}
		case key.Matches(keyMsg, m.keys.PrevSuggestion):
			if len(suggestions) > 0 {
				m.selectedSuggestion = m.selectedSuggestionIndex(len(suggestions)) - 1
				if m.selectedSuggestion < 0 {
					m.selectedSuggestion = len(suggestions) - 1
				}
				return m, nil
			}
		}
	}

	var cmd tea.Cmd
	m.editor, cmd = m.editor.Update(msg)
	m.syncScroll()
	m.clampSuggestionSelection(query)
	return m, cmd
}

func (m *commandModeModel) Clear() {
	m.editor.Reset()
	m.editor.Focus()
	m.scrollTop = 0
	m.selectedSuggestion = 0
}

func (m *commandModeModel) Focus() {
	m.editor.Focus()
}

func (m *commandModeModel) Blur() {
	m.editor.Blur()
}

func (m commandModeModel) KeyMap() commandModeKeyMap {
	return m.keys
}

func (m commandModeModel) Value() string {
	return m.editor.Value()
}

func (m *commandModeModel) SetSize(innerWidth, innerHeight int) {
	editorWidth := clampEditorSize(innerWidth, minimumEditorWidth)
	editorHeight := clampEditorSize(innerHeight, minimumEditorHeight)

	m.editor.SetWidth(editorWidth)
	m.editor.SetHeight(editorHeight)
	m.syncScroll()
}

func (m commandModeModel) Focused() bool {
	return m.editor.Focused()
}

func (m commandModeModel) View(query QueryContext) string {
	return m.renderView(query)
}

func (m commandModeModel) FooterHints(query QueryContext) string {
	parts := []string{
		fmt.Sprintf("line %d col %d", m.editor.Line()+1, m.editor.LineInfo().ColumnOffset+1),
		"enter submit",
		bindingSummary(m.keys.Cancel),
		bindingSummary(m.keys.History),
	}
	if running := formatRunningIndicator(query.Running); running != "" {
		parts = append(parts, "esc cancel query")
	}
	if len(m.autocompleteItems(query)) > 0 {
		parts = append(parts, bindingSummary(m.keys.AcceptSuggestion), bindingSummary(m.keys.NextSuggestion), bindingSummary(m.keys.PrevSuggestion))
	}
	parts = append(parts, "ctrl+c quit")
	return strings.Join(parts, " | ")
}

func (m commandModeModel) Footer(connectionName, dialect string, query QueryContext) string {
	modeLabel := "Command mode"
	if query.ActiveMode == ModeHistorySearch {
		modeLabel = "History search"
	} else if query.ActiveMode == ModeRecordViewer && query.Layout == LayoutSplit {
		modeLabel = "Command line hidden focus"
	}
	parts := []string{modeLabel, fmt.Sprintf("layout %s", layoutLabel(query.Layout))}

	if label := strings.TrimSpace(connectionName); label != "" {
		parts = append(parts, fmt.Sprintf("connection %s", label))
	}

	if label := strings.TrimSpace(dialect); label != "" {
		parts = append(parts, fmt.Sprintf("dialect %s", label))
	}

	if latest := query.LatestResult; latest != nil {
		if selectedCount := len(latest.SelectedRows); selectedCount > 0 {
			parts = append(parts, fmt.Sprintf("%d selected", selectedCount))
		}
	}

	parts = append(parts, fmt.Sprintf("line %d col %d", m.editor.Line()+1, m.editor.LineInfo().ColumnOffset+1))
	parts = append(parts, bindingSummary(m.keys.Submit), bindingSummary(m.keys.Cancel), bindingSummary(m.keys.Help), bindingSummary(m.keys.History), bindingSummary(m.keys.SwitchMode), bindingSummary(m.keys.LayoutSplit), bindingSummary(m.keys.LayoutCommandOnly), bindingSummary(m.keys.LayoutViewerOnly))
	if query.ActiveMode == ModeRecordViewer {
		parts = append(parts, "ctrl+u prev page", "ctrl+d next page")
	}
	if query.ActiveMode == ModeHistorySearch {
		parts = append(parts, bindingSummary(m.keys.RestoreHistory), bindingSummary(m.keys.NextSuggestion), bindingSummary(m.keys.PrevSuggestion))
	}
	if query.SlashWizard != nil {
		parts = append(parts, "wizard /commands")
	}
	if running := formatRunningIndicator(query.Running); running != "" {
		parts = append(parts, running)
		parts = append(parts, "esc cancel query")
	}
	if len(m.autocompleteItems(query)) > 0 {
		parts = append(parts, bindingSummary(m.keys.AcceptSuggestion), bindingSummary(m.keys.NextSuggestion), bindingSummary(m.keys.PrevSuggestion))
	}
	parts = append(parts, "ctrl+c quit")

	return appTheme.footer.Render(strings.Join(parts, " | "))
}

func (m *commandModeModel) AppendReplEntry(prompt, sql, output string) {
	m.replTranscript = append(m.replTranscript, replTranscriptEntry{
		Prompt: prompt,
		SQL:    sql,
		Output: output,
	})
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

func (m *commandModeModel) syncScroll() {
	cursorRow, totalRows := m.cursorVisualPosition()
	m.scrollTop = adjustedScrollTop(m.scrollTop, cursorRow, totalRows, m.editor.Height())
}

func adjustedScrollTop(current, cursorRow, totalRows, height int) int {
	if height <= 0 {
		return 0
	}

	if cursorRow < current {
		current = cursorRow
	}

	if cursorRow >= current+height {
		current = cursorRow - height + 1
	}

	maxTop := max(0, totalRows-height)
	if current > maxTop {
		current = maxTop
	}

	if current < 0 {
		return 0
	}

	return current
}

func (m commandModeModel) renderView(query QueryContext) string {
	sections := make([]string, 0, 4)

	// REPL transcript — show past commands and results above the editor
	if transcript := m.renderReplTranscript(); transcript != "" {
		sections = append(sections, transcript)
	}

	// History search panel (if active)
	if historySearch := renderHistorySearch(query); historySearch != "" {
		sections = append(sections, historySearch)
	}

	// Slash wizard (if active)
	if wizard := renderSlashWizard(query); wizard != "" {
		sections = append(sections, wizard)
	}

	// Compute ghost text from top autocomplete suggestion
	ghost := m.ghostText(query)

	var editorView string
	if m.editor.Value() == "" && strings.TrimSpace(m.editor.Placeholder) != "" {
		editorView = m.renderPlaceholderView()
	} else {
		wrappedLines, cursorRow, totalRows := m.renderedLines()
		scrollTop := adjustedScrollTop(m.scrollTop, cursorRow, totalRows, m.editor.Height())
		editorView = m.renderVisibleLines(wrappedLines, scrollTop, ghost)
	}
	sections = append(sections, editorView)

	return strings.Join(sections, "\n\n")
}

func (m commandModeModel) renderReplTranscript() string {
	if len(m.replTranscript) == 0 {
		return ""
	}
	lines := make([]string, 0)
	for _, entry := range m.replTranscript {
		prompt := entry.Prompt
		if prompt == "" {
			prompt = "> "
		}
		// Show prompt + first line of SQL
		sqlLines := strings.Split(strings.TrimRight(entry.SQL, "\n"), "\n")
		for i, sl := range sqlLines {
			if i == 0 {
				lines = append(lines, appTheme.promptStyle.Render(prompt)+appTheme.panelText.Render(sl))
			} else {
				continuation := strings.Repeat(" ", len([]rune(prompt)))
				lines = append(lines, appTheme.panelMuted.Render(continuation+sl))
			}
		}
		if entry.Output != "" {
			for _, ol := range strings.Split(strings.TrimRight(entry.Output, "\n"), "\n") {
				lines = append(lines, appTheme.panelMuted.Render(ol))
			}
		}
	}
	return strings.Join(lines, "\n")
}

func (m commandModeModel) ghostText(query QueryContext) string {
	suggestions := m.autocompleteItems(query)
	if len(suggestions) == 0 {
		return ""
	}

	selected := suggestions[m.selectedSuggestionIndex(len(suggestions))]
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

func renderGeneratedCommandWarning(sql string) string {
	switch leadingSQLKeyword(sql) {
	case "DELETE":
		return appTheme.warningNotice.Render("Warning: generated DELETE statement. Review carefully before submitting.")
	case "DROP":
		return appTheme.warningNotice.Render("Warning: generated DROP statement. Review carefully before submitting.")
	default:
		return ""
	}
}

func (m commandModeModel) renderPlaceholderView() string {
	height := max(1, m.editor.Height())
	lines := make([]renderedEditorLine, 0, height)
	placeholder := []rune(m.editor.Placeholder)
	firstLine := sqlStyledLine{}
	for _, r := range placeholder {
		firstLine = append(firstLine, sqlStyledRune{rune: r, kind: sqlTokenPlain})
	}

	lines = append(lines, renderedEditorLine{
		logicalLine: 0,
		lineNumber:  1,
		runes:       firstLine,
		isCursor:    true,
		cursorCol:   0,
	})

	for len(lines) < height {
		lines = append(lines, renderedEditorLine{logicalLine: len(lines), lineNumber: len(lines) + 1})
	}

	return m.renderVisibleLines(lines, 0, "")
}

func (m commandModeModel) renderedLines() ([]renderedEditorLine, int, int) {
	logicalLines := splitEditorLines(m.editor.Value())
	highlighted := m.highlighter.highlightLines(logicalLines)
	contentWidth := max(1, m.editor.Width())
	currentLine := m.editor.Line()
	lineInfo := m.editor.LineInfo()

	wrappedLines := make([]renderedEditorLine, 0, len(highlighted))
	cursorVisualRow := 0

	for lineIndex, line := range highlighted {
		segments := wrapStyledLine(line, contentWidth)
		if len(segments) == 0 {
			segments = []sqlStyledLine{{}}
		}

		for segmentIndex, segment := range segments {
			visualLine := renderedEditorLine{
				logicalLine: lineIndex,
				lineNumber:  0,
				runes:       segment,
				cursorCol:   -1,
			}

			if segmentIndex == 0 {
				visualLine.lineNumber = lineIndex + 1
			}

			if lineIndex == currentLine && segmentIndex == lineInfo.RowOffset {
				visualLine.isCursor = true
				visualLine.cursorCol = lineInfo.ColumnOffset
				cursorVisualRow = len(wrappedLines)
			}

			wrappedLines = append(wrappedLines, visualLine)
		}
	}

	if len(wrappedLines) == 0 {
		wrappedLines = append(wrappedLines, renderedEditorLine{lineNumber: 1, isCursor: true, cursorCol: 0})
	}

	return wrappedLines, cursorVisualRow, len(wrappedLines)
}

func (m commandModeModel) renderVisibleLines(lines []renderedEditorLine, scrollTop int, ghostText string) string {
	height := max(1, m.editor.Height())
	if scrollTop < 0 {
		scrollTop = 0
	}

	end := min(len(lines), scrollTop+height)
	visible := lines[scrollTop:end]

	var builder strings.Builder
	for i, line := range visible {
		if i > 0 {
			builder.WriteByte('\n')
		}
		lineGhost := ""
		if line.isCursor {
			lineGhost = ghostText
		}
		builder.WriteString(m.renderLine(line, lineGhost))
	}

	for len(visible) < height {
		if builder.Len() > 0 {
			builder.WriteByte('\n')
		}
		builder.WriteString(m.renderLine(renderedEditorLine{}, ""))
		visible = append(visible, renderedEditorLine{})
	}

	return builder.String()
}

func (m commandModeModel) renderLine(line renderedEditorLine, ghostText string) string {
	lineStyle := appTheme.panelText
	lineNumberStyle := m.highlighter.lineNumberStyle
	content := m.highlighter.renderLineContentWithGhost(line.runes, line.cursorCol, m.editor.Width(), false, ghostText)

	if line.isCursor {
		lineStyle = m.highlighter.cursorLineStyle
		lineNumberStyle = m.highlighter.cursorLineNumberStyle
	}

	if line.logicalLine == 0 && m.editor.Value() == "" && strings.TrimSpace(m.editor.Placeholder) != "" {
		content = m.highlighter.renderLineContentWithGhost(line.runes, line.cursorCol, m.editor.Width(), true, "")
	}

	prompt := m.highlighter.promptStyle.Render(m.editor.Prompt)
	lineNumber := ""
	if m.editor.ShowLineNumbers {
		label := m.formatLineNumber(" ")
		if line.lineNumber > 0 {
			label = m.formatLineNumber(line.lineNumber)
		}
		lineNumber = lineNumberStyle.Render(label)
	}

	return lineStyle.Render(prompt + lineNumber + content)
}

func (m commandModeModel) formatLineNumber(value any) string {
	digits := len(strconv.Itoa(max(1, m.editor.MaxHeight)))
	return fmt.Sprintf(" %*v ", digits, value)
}

func (m commandModeModel) cursorVisualPosition() (int, int) {
	_, cursorRow, totalRows := m.renderedLines()
	return cursorRow, totalRows
}

func (m commandModeModel) autocompleteItems(query QueryContext) []autocompleteItem {
	return buildAutocompleteItems(m.editor.Value(), m.cursorOffset(), query)
}

func (m *commandModeModel) clampSuggestionSelection(query QueryContext) {
	count := len(m.autocompleteItems(query))
	if count == 0 {
		m.selectedSuggestion = 0
		return
	}

	m.selectedSuggestion = m.selectedSuggestionIndex(count)
}

func (m commandModeModel) selectedSuggestionIndex(count int) int {
	if count <= 0 || m.selectedSuggestion < 0 {
		return 0
	}
	if m.selectedSuggestion >= count {
		return count - 1
	}

	return m.selectedSuggestion
}

func (m *commandModeModel) applySuggestion(item autocompleteItem) {
	value := []rune(m.editor.Value())
	ctx := analyzeAutocompleteContext(m.editor.Value(), m.cursorOffset())
	start := clampCursorOffset(ctx.ReplaceStart, len(value))
	end := clampCursorOffset(ctx.ReplaceEnd, len(value))
	insert := []rune(item.InsertText)
	updated := string(append(append(append([]rune(nil), value[:start]...), insert...), value[end:]...))
	cursor := start + len(insert)
	m.editor.SetValue(updated)
	m.setCursorOffset(cursor)
	m.syncScroll()
	m.selectedSuggestion = 0
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
	m.editor.SetCursor(col)
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

func (m commandModeModel) renderAutocompletePanel(query QueryContext) string {
	suggestions := m.autocompleteItems(query)
	selected := m.selectedSuggestionIndex(len(suggestions))
	visible := min(len(suggestions), autocompletePanelRows)
	start := 0
	if selected >= visible {
		start = selected - visible + 1
	}

	lines := make([]string, 0, autocompletePanelRows+1)
	if len(suggestions) > 0 {
		lines = append(lines, appTheme.panelTitle.Render("Suggestions:"))
	} else {
		lines = append(lines, appTheme.panelMuted.Render("Suggestions:"))
	}

	for i := start; i < start+autocompletePanelRows; i++ {
		if i < len(suggestions) {
			item := suggestions[i]
			line := fmt.Sprintf("  [%s] %s", item.Kind, item.Label)
			if detail := strings.TrimSpace(item.Detail); detail != "" {
				line += " - " + detail
			}
			if i == selected {
				lines = append(lines, appTheme.panelSelected.Render("> "+strings.TrimPrefix(line, "  ")))
			} else {
				lines = append(lines, appTheme.panelText.Render(line))
			}
		} else {
			// Empty row to preserve fixed height
			lines = append(lines, "")
		}
	}

	return strings.Join(lines, "\n")
}

func renderInlineResult(query QueryContext) string {
	latest := query.LatestResult
	if latest == nil || latest.OriginMode != ModeCommand {
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

func renderSlashWizard(query QueryContext) string {
	wizard := query.SlashWizard
	if wizard == nil {
		return ""
	}

	lines := []string{appTheme.panelTitle.Render("Command wizard:")}
	switch wizard.Step {
	case SlashCommandWizardStepTarget:
		selectedCommand, _ := slashWizardCommandByIndex(wizard)
		lines = append(lines,
			appTheme.panelMuted.Render(fmt.Sprintf("Step 1/2 complete: %s", selectedCommand.DisplayName)),
			appTheme.panelText.Render(fmt.Sprintf("Step 2/2: choose a table for %s", selectedCommand.DisplayName)),
		)
		for i, target := range wizard.Targets {
			if i == clampWizardIndex(wizard.SelectedTarget, len(wizard.Targets)) {
				lines = append(lines, appTheme.panelSelected.Render("> "+target.Display))
				continue
			}
			lines = append(lines, appTheme.panelText.Render("  "+target.Display))
		}
		lines = append(lines, appTheme.panelHint.Render("ctrl+g confirm | alt+n next | alt+p prev | esc back"))
	default:
		lines = append(lines, appTheme.panelText.Render("Step 1/2: choose a slash command"))
		for i, command := range wizard.Commands {
			line := fmt.Sprintf("  %s - %s", command.DisplayName, command.Summary)
			if command.NeedsTarget {
				line += " (choose table next)"
			}
			if i == clampWizardIndex(wizard.SelectedCommand, len(wizard.Commands)) {
				lines = append(lines, appTheme.panelSelected.Render("> "+strings.TrimPrefix(line, "  ")))
				continue
			}
			lines = append(lines, appTheme.panelText.Render(line))
		}
		lines = append(lines, appTheme.panelHint.Render("ctrl+g confirm | alt+n next | alt+p prev | esc close"))
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
	parts := []string{appTheme.resultTitle.Render("Results:")}
	if latest.RowsAffected != nil {
		label := "rows"
		if *latest.RowsAffected == 1 {
			label = "row"
		}
		parts = append(parts, appTheme.resultSummary.Render(fmt.Sprintf("%d %s affected", *latest.RowsAffected, label)))
	} else {
		parts = append(parts, appTheme.resultSummary.Render("Statement executed successfully"))
	}
	if latest.LastInsertID != nil && *latest.LastInsertID != 0 {
		parts = append(parts, appTheme.resultSummary.Render(fmt.Sprintf("last insert id %d", *latest.LastInsertID)))
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

	headerLine := appTheme.resultHeader.Render(renderInlineResultLine(columns, widths))
	lines := []string{appTheme.resultTitle.Render("Results:"), headerLine, renderInlineSeparator(widths)}
	for _, row := range result.Rows {
		values := make([]string, len(row.Values))
		for i, value := range row.Values {
			values[i] = formatInlineResultValue(value)
		}
		lines = append(lines, renderInlineResultLine(values, widths))
	}

	if len(result.Rows) == 0 {
		lines = append(lines, appTheme.viewerEmpty.Render("(no rows)"))
	}

	rowCount := len(result.Rows)
	if latest.InlineRowsTruncated && latest.PreservedResult != nil {
		lines = append(lines, appTheme.panelHint.Render(fmt.Sprintf("Showing first %d of %d rows.", rowCount, len(latest.PreservedResult.Rows))))
		return strings.Join(lines, "\n")
	}

	if rowCount == 1 {
		lines = append(lines, appTheme.resultSummary.Render("1 row."))
	} else {
		lines = append(lines, appTheme.resultSummary.Render(fmt.Sprintf("%d rows.", rowCount)))
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
	return appTheme.resultSeparator.Render(strings.Join(parts, "-+-"))
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
		return fmt.Sprint(value.Value)
	case db.ValueKindBytes:
		if typed, ok := value.Value.([]byte); ok {
			return fmt.Sprintf("0x%x", typed)
		}
	case db.ValueKindTime:
		if typed, ok := value.Value.(time.Time); ok {
			return typed.Format(time.RFC3339)
		}
	}

	if value.Value == nil {
		return "NULL"
	}

	return fmt.Sprint(value.Value)
}

func runeWidth(value string) int {
	return ansi.StringWidth(value)
}
