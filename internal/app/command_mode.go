package app

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/adwinying/sqlcery/internal/db"
)

const (
	defaultEditorWidth    = 80
	minimumEditorWidth    = 20
	autocompletePanelRows = 5
	// bottomPadding reserves rows below the last editor line for the
	// autocomplete dropdown so the prompt never bounces when the menu opens.
	bottomPadding   = 5
	editorLargeHeight = 9999 // fixed height so the textarea never runs its own viewport
)

type replTranscriptEntry struct {
	Prompt string
	SQL    string
	Output string
}

type commandModeModel struct {
	editor             textarea.Model
	keys               commandModeKeyMap
	highlighter        sqlSyntaxHighlighter
	selectedSuggestion int
	replTranscript     []replTranscriptEntry
	innerWidth         int
	innerHeight        int
	scrollOffset       int // lines scrolled up from the natural bottom; 0 = follow latest
	// autocompleteOpenedByTyping is the primary gate for the autocomplete
	// dropdown: the menu is only allowed to appear when the most recent key
	// event modified the editor buffer (printable characters, backspace,
	// delete). Focus changes, pane switches, cursor movement, history recall,
	// slash-command expansion, record-viewer compose and submit/clear flows
	// all reset it to false. The flag lifts the moment the user types the
	// next character.
	autocompleteOpenedByTyping bool
	// autocompleteSuppressed is set when the user explicitly dismisses the
	// autocomplete dropdown (e.g. via Esc). While the editor value and cursor
	// offset stay identical to the snapshot captured at dismissal time,
	// autocompleteItems returns nil so the menu stays closed. Any edit or
	// cursor movement naturally lifts the suppression because the captured
	// snapshot no longer matches.
	autocompleteSuppressed       bool
	autocompleteSuppressedValue  string
	autocompleteSuppressedCursor int
}

type commandModeKeyMap struct {
	Submit              key.Binding
	Cancel              key.Binding
	Help                key.Binding
	History             key.Binding
	RestoreHistory      key.Binding
	SwitchMode          key.Binding
	LayoutCommandOnly   key.Binding
	AcceptSuggestion    key.Binding
	NextSuggestion      key.Binding
	PrevSuggestion      key.Binding
	ScrollTranscriptUp  key.Binding
	ScrollTranscriptDown key.Binding
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
		highlighter: newSQLSyntaxHighlighter(),
		innerWidth:  defaultEditorWidth,
		innerHeight: 1,
		keys: commandModeKeyMap{
			Submit:               key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "submit")),
			Cancel:               key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
			Help:                 key.NewBinding(key.WithKeys("alt+h"), key.WithHelp("alt+h", "help")),
			History:              key.NewBinding(key.WithKeys("ctrl+r"), key.WithHelp("ctrl+r", "history")),
			RestoreHistory:       key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "restore")),
			SwitchMode:           key.NewBinding(key.WithKeys("ctrl+x"), key.WithHelp("ctrl+x", "focus")),
			LayoutCommandOnly:    key.NewBinding(key.WithKeys("ctrl+3"), key.WithHelp("ctrl+3", "command")),
			AcceptSuggestion:     key.NewBinding(key.WithKeys("tab", "ctrl+y"), key.WithHelp("tab/ctrl+y", "accept")),
			NextSuggestion:       key.NewBinding(key.WithKeys("ctrl+n"), key.WithHelp("ctrl+n", "next suggestion")),
			PrevSuggestion:       key.NewBinding(key.WithKeys("ctrl+p"), key.WithHelp("ctrl+p", "prev suggestion")),
			ScrollTranscriptUp:   key.NewBinding(key.WithKeys("ctrl+u"), key.WithHelp("ctrl+u", "scroll up")),
			ScrollTranscriptDown: key.NewBinding(key.WithKeys("ctrl+d"), key.WithHelp("ctrl+d", "scroll down")),
		},
	}
}

func (m commandModeModel) Init() tea.Cmd {
	return textarea.Blink
}

func (m commandModeModel) Update(msg tea.Msg, interaction InteractionState) (commandModeModel, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyPressMsg); ok {
		suggestions := m.autocompleteItems(interaction)
		switch {
		case key.Matches(keyMsg, m.keys.AcceptSuggestion):
			if len(suggestions) > 0 {
				m.applySuggestion(suggestions[m.selectedSuggestionIndex(len(suggestions))])
				// Accepting a suggestion commits text but should close the
				// menu; it shouldn't linger until the user types again.
				m.autocompleteOpenedByTyping = false
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
		case key.Matches(keyMsg, m.keys.ScrollTranscriptUp):
			step := max(1, m.innerHeight/2)
			maxOff := m.computeNaturalScrollTop(max(1, m.innerHeight))
			m.scrollOffset = min(m.scrollOffset+step, maxOff)
			m.autocompleteOpenedByTyping = false
			return m, nil
		case key.Matches(keyMsg, m.keys.ScrollTranscriptDown):
			step := max(1, m.innerHeight/2)
			m.scrollOffset = max(0, m.scrollOffset-step)
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
			// Any edit or cursor movement snaps back to follow latest output,
			// matching terminal-shell behaviour.
			m.scrollOffset = 0
		}
		if valueChanged {
			// A key event that actually mutated the buffer — printable text,
			// backspace, delete, or a newline in multi-line SQL. This is the
			// only path that opens the autocomplete menu.
			m.autocompleteOpenedByTyping = true
		} else {
			// Any key press that did not change the buffer (arrow keys, Home,
			// End, Ctrl+A/E, modifier-only presses, ignored shortcuts) closes
			// the menu. The user must type again to reopen it.
			m.autocompleteOpenedByTyping = false
		}
	}

	m.clampSuggestionSelection(interaction)
	return m, cmd
}

func (m *commandModeModel) Clear() {
	m.editor.Reset()
	m.editor.Focus()
	m.selectedSuggestion = 0
	m.autocompleteOpenedByTyping = false
	m.clearAutocompleteSuppression()
}

// AutocompleteVisible reports whether the autocomplete dropdown is currently
// showing suggestions to the user for the given query context.
func (m commandModeModel) AutocompleteVisible(interaction InteractionState) bool {
	return len(m.autocompleteItems(interaction)) > 0
}

// DismissAutocomplete suppresses the autocomplete dropdown while the editor
// value and cursor position remain unchanged. Any subsequent edit or cursor
// movement implicitly lifts the suppression.
func (m *commandModeModel) DismissAutocomplete() {
	m.autocompleteSuppressed = true
	m.autocompleteSuppressedValue = m.editor.Value()
	m.autocompleteSuppressedCursor = m.cursorOffset()
	m.selectedSuggestion = 0
	m.autocompleteOpenedByTyping = false
}

func (m *commandModeModel) clearAutocompleteSuppression() {
	m.autocompleteSuppressed = false
	m.autocompleteSuppressedValue = ""
	m.autocompleteSuppressedCursor = 0
}

func (m *commandModeModel) Focus() {
	m.editor.Focus()
	// Refocusing the command pane must not re-open a menu that was already
	// closed when the pane lost focus.
	m.autocompleteOpenedByTyping = false
}

func (m *commandModeModel) Blur() {
	m.editor.Blur()
	m.autocompleteOpenedByTyping = false
}

// SetEditorValue replaces the editor buffer programmatically (history recall,
// slash-command expansion, record-viewer compose) without marking the change
// as a typing event, so the autocomplete menu stays closed until the user
// types the next character.
func (m *commandModeModel) SetEditorValue(value string) {
	m.editor.SetValue(value)
	m.editor.CursorEnd()
	m.selectedSuggestion = 0
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
	return m.renderView(interaction)
}

func (m commandModeModel) FooterHints(interaction InteractionState) string {
	var parts []string
	if len(m.autocompleteItems(interaction)) > 0 {
		parts = append(parts, bindingSummary(m.keys.AcceptSuggestion), bindingSummary(m.keys.NextSuggestion), bindingSummary(m.keys.PrevSuggestion))
	}
	parts = append(parts, "enter submit", bindingSummary(m.keys.Cancel), bindingSummary(m.keys.History))
	if running := formatRunningIndicator(interaction.Running); running != "" {
		parts = append(parts, "esc cancel query")
	}
	parts = append(parts, "ctrl+c quit")
	return strings.Join(parts, " | ")
}

func (m commandModeModel) Footer(connectionName, dialect string, interaction InteractionState) string {
	modeLabel := "Command mode"
	if interaction.ActiveModal == ModalHistorySearch {
		modeLabel = "History search"
	} else if interaction.ActivePane == PaneResultsPane && interaction.Layout == LayoutSplit {
		modeLabel = "Command line hidden focus"
	}
	parts := []string{modeLabel, fmt.Sprintf("layout %s", layoutLabel(interaction.Layout))}

	if label := strings.TrimSpace(connectionName); label != "" {
		parts = append(parts, fmt.Sprintf("connection %s", label))
	}

	if label := strings.TrimSpace(dialect); label != "" {
		parts = append(parts, label)
	}

	if latest := interaction.LatestResult; latest != nil {
		if selectedCount := len(latest.SelectedRows); selectedCount > 0 {
			parts = append(parts, fmt.Sprintf("%d selected", selectedCount))
		}
	}

	parts = append(parts, fmt.Sprintf("line %d col %d", m.editor.Line()+1, m.editor.LineInfo().ColumnOffset+1))
	parts = append(parts, bindingSummary(m.keys.Submit), bindingSummary(m.keys.Cancel), bindingSummary(m.keys.Help), bindingSummary(m.keys.History), bindingSummary(m.keys.SwitchMode), bindingSummary(m.keys.LayoutCommandOnly))
	if interaction.ActivePane == PaneResultsPane {
		parts = append(parts, "ctrl+u scroll up", "ctrl+d scroll down")
	}
	if interaction.ActiveModal == ModalHistorySearch {
		parts = append(parts, bindingSummary(m.keys.RestoreHistory), bindingSummary(m.keys.NextSuggestion), bindingSummary(m.keys.PrevSuggestion))
	}
	if interaction.SlashWizard != nil {
		parts = append(parts, "wizard /commands")
	}
	if running := formatRunningIndicator(interaction.Running); running != "" {
		parts = append(parts, running)
		parts = append(parts, "esc cancel query")
	}
	if len(m.autocompleteItems(interaction)) > 0 {
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
	m.scrollOffset = 0 // auto-scroll to latest
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

// computeNaturalScrollTop returns the scroll-top value that keeps the last
// editor row on screen with bottomPadding blank reserve rows below it. It is
// also the upper bound for scrollOffset: scrolling further would show empty
// space above the first transcript line.
func (m commandModeModel) computeNaturalScrollTop(viewportH int) int {
	transcriptLen := len(m.renderReplTranscriptLines())
	var totalEditorRows int
	if m.editor.Value() == "" {
		totalEditorRows = 1
	} else {
		_, _, totalEditorRows = m.renderedLines()
	}
	contentRows := transcriptLen + totalEditorRows
	topPadding := max(0, viewportH-bottomPadding-contentRows)
	lastEditorRow := topPadding + transcriptLen + totalEditorRows - 1
	return max(0, lastEditorRow+bottomPadding-viewportH+1)
}

func (m commandModeModel) renderView(interaction InteractionState) string {
	viewportH := max(1, m.innerHeight)

	// Build the unified line list: transcript + editor.
	transcriptLines := m.renderReplTranscriptLines()

	ghost := m.ghostText(interaction)

	var editorStrings []string
	var cursorRowInEditor int
	var totalEditorRows int
	if m.editor.Value() == "" && strings.TrimSpace(m.editor.Placeholder) != "" {
		editorStrings = []string{m.renderPlaceholderView()}
		cursorRowInEditor = 0
		totalEditorRows = 1
	} else {
		wrappedLines, editorCursor, editorTotal := m.renderedLines()
		editorStrings = m.renderAllEditorLines(wrappedLines, ghost)
		cursorRowInEditor = editorCursor
		totalEditorRows = editorTotal
	}

	// Assemble the full visual stream as: [top-pad] [transcript] [editor] [reserve].
	// - top-pad: blank rows that push short content down so the prompt hugs
	//   the bottom of the pane (minus the reserve). Zero when content is
	//   tall enough to overflow naturally.
	// - reserve: bottomPadding blank rows the autocomplete dropdown overlays
	//   into. Kept as real entries in allLines so adjustedScrollTop's
	//   internal maxTop clamp does not collapse them away.
	contentRows := len(transcriptLines) + totalEditorRows
	topPadding := max(0, viewportH-bottomPadding-contentRows)

	allLines := make([]string, 0, topPadding+contentRows+bottomPadding)
	for i := 0; i < topPadding; i++ {
		allLines = append(allLines, "")
	}
	allLines = append(allLines, transcriptLines...)
	allLines = append(allLines, editorStrings...)
	for i := 0; i < bottomPadding; i++ {
		allLines = append(allLines, "")
	}

	cursorRow := topPadding + len(transcriptLines) + cursorRowInEditor
	lastEditorRow := topPadding + len(transcriptLines) + totalEditorRows - 1

	// naturalScrollTop: scroll position that keeps the last editor row on
	// screen with the reserve visible below it.
	naturalScrollTop := max(0, lastEditorRow+bottomPadding-viewportH+1)

	// Apply manual scroll offset (lines scrolled up from the natural position).
	rawScroll := max(0, naturalScrollTop-m.scrollOffset)

	var scrollTop int
	if m.scrollOffset == 0 {
		// Not manually scrolled: ensure the cursor is always visible.
		scrollTop = adjustedScrollTop(rawScroll, cursorRow, len(allLines), viewportH)
		scrollTop = min(scrollTop, naturalScrollTop)
	} else {
		// Explicitly scrolled: let the view stay where the user put it.
		// The cursor is allowed to go off-screen (terminal-shell behaviour).
		scrollTop = max(0, min(rawScroll, naturalScrollTop))
	}

	// Build the visible grid: exactly viewportH rows, blank where allLines
	// has no content.
	visible := make([]string, viewportH)
	for i := range visible {
		if idx := scrollTop + i; idx < len(allLines) {
			visible[i] = allLines[idx]
		}
	}

	// Overlay the autocomplete dropdown directly below the cursor line.
	// It replaces whatever rows are there (editor rows or reserve blanks),
	// so the prompt never shifts vertically when the menu opens or closes.
	dropdown := m.renderAutocompleteDropdown(interaction)
	if dropdown == "" {
		return strings.Join(visible, "\n")
	}

	cursorVisualRow := cursorRow - scrollTop
	overlayStart := cursorVisualRow + 1
	for i, dl := range strings.Split(dropdown, "\n") {
		if row := overlayStart + i; row >= 0 && row < len(visible) {
			visible[row] = dl
		}
	}

	return strings.Join(visible, "\n")
}

func (m commandModeModel) renderReplTranscriptLines() []string {
	if len(m.replTranscript) == 0 {
		return nil
	}
	lines := make([]string, 0)
	for _, entry := range m.replTranscript {
		prompt := entry.Prompt
		if prompt == "" {
			prompt = "> "
		}
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
	return lines
}

func (m commandModeModel) ghostText(interaction InteractionState) string {
	suggestions := m.autocompleteItems(interaction)
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

func renderGeneratedStatementWarning(sql string) string {
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
	line := renderedEditorLine{
		logicalLine: 0,
		lineNumber:  1,
		runes:       sqlStyledLine{},
		isCursor:    true,
		cursorCol:   0,
	}
	return m.renderLine(line, "")
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

	return builder.String()
}

// renderAllEditorLines renders every visual editor line into a []string without
// any height-based clipping. The unified viewport in renderView does the slicing.
func (m commandModeModel) renderAllEditorLines(lines []renderedEditorLine, ghostText string) []string {
	result := make([]string, len(lines))
	for i, line := range lines {
		lineGhost := ""
		if line.isCursor {
			lineGhost = ghostText
		}
		result[i] = m.renderLine(line, lineGhost)
	}
	return result
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

func (m commandModeModel) autocompleteItems(interaction InteractionState) []autocompleteItem {
	// Primary gate: the menu is only allowed to appear as a direct result of
	// a typing key event. Cursor movement, focus changes, pane switches,
	// history recall, slash-command expansion, compose flows and submits all
	// leave this flag false, so the menu stays closed until the user types.
	if !m.autocompleteOpenedByTyping {
		return nil
	}
	if m.autocompleteSuppressed &&
		m.editor.Value() == m.autocompleteSuppressedValue &&
		m.cursorOffset() == m.autocompleteSuppressedCursor {
		return nil
	}
	return buildAutocompleteItems(m.editor.Value(), m.cursorOffset(), interaction)
}

func (m *commandModeModel) clampSuggestionSelection(interaction InteractionState) {
	count := len(m.autocompleteItems(interaction))
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

func (m commandModeModel) renderAutocompletePanel(interaction InteractionState) string {
	suggestions := m.autocompleteItems(interaction)
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

func (m commandModeModel) renderAutocompleteDropdown(interaction InteractionState) string {
	suggestions := m.autocompleteItems(interaction)
	if len(suggestions) == 0 {
		return ""
	}

	selected := m.selectedSuggestionIndex(len(suggestions))
	visible := min(len(suggestions), autocompletePanelRows)
	start := 0
	if selected >= visible {
		start = selected - visible + 1
	}

	lines := make([]string, 0, visible)
	for i := start; i < start+visible; i++ {
		if i >= len(suggestions) {
			break
		}
		item := suggestions[i]
		line := fmt.Sprintf("[%s] %s", item.Kind, item.Label)
		if detail := strings.TrimSpace(item.Detail); detail != "" {
			line += " - " + detail
		}
		if i == selected {
			lines = append(lines, appTheme.panelSelected.Render(line))
		} else {
			lines = append(lines, appTheme.panelText.Render(line))
		}
	}

	// Compute popup width from content
	maxWidth := 0
	for _, line := range lines {
		if w := ansi.StringWidth(line); w > maxWidth {
			maxWidth = w
		}
	}
	popupWidth := maxWidth
	editorWidth := m.editor.Width()
	if popupWidth > editorWidth {
		popupWidth = editorWidth
	}
	if popupWidth < 10 {
		popupWidth = 10
	}

	// Determine cursor column offset for positioning
	promptWidth := ansi.StringWidth(m.editor.Prompt)
	cursorCol := promptWidth + m.editor.LineInfo().ColumnOffset
	indent := cursorCol
	if indent+popupWidth > editorWidth+promptWidth {
		indent = max(0, editorWidth+promptWidth-popupWidth)
	}
	indentStr := strings.Repeat(" ", indent)

	var builder strings.Builder
	for i, line := range lines {
		if i > 0 {
			builder.WriteByte('\n')
		}
		padding := popupWidth - ansi.StringWidth(line)
		if padding < 0 {
			padding = 0
			line = ansi.Truncate(line, popupWidth, "")
		}
		builder.WriteString(indentStr + line + strings.Repeat(" ", padding))
	}

	return builder.String()
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

func renderSlashWizard(interaction InteractionState) string {
	wizard := interaction.SlashWizard
	if wizard == nil {
		return ""
	}

	lines := []string{appTheme.panelTitle.Render("Command wizard:")}
	switch wizard.Step {
	case SlashCommandWizardStepTarget:
		selectedCommand, _ := slashWizardCommandByIndex(wizard)
		headerLines := 1 // title already added
		if wizard.DirectInvocation {
			lines = append(lines,
				appTheme.panelText.Render(fmt.Sprintf("Choose a table for %s:", selectedCommand.DisplayName)),
			)
			headerLines++
		} else {
			lines = append(lines,
				appTheme.panelMuted.Render(fmt.Sprintf("Step 1/2 complete: %s", selectedCommand.DisplayName)),
				appTheme.panelText.Render(fmt.Sprintf("Step 2/2: choose a table for %s", selectedCommand.DisplayName)),
			)
			headerLines += 2
		}
		// Filter input row
		lines = append(lines, appTheme.panelText.Render(fmt.Sprintf("filter> %s", defaultWizardFilter(wizard.TargetFilter))))
		headerLines++

		filteredTargets := filterWizardTargets(wizard.Targets, wizard.TargetFilter)
		const footerLines = 1
		listViewport := modalFixedRows - headerLines - footerLines
		if listViewport < 1 {
			listViewport = 1
		}
		selected := clampWizardIndex(wizard.SelectedTarget, len(filteredTargets))
		scrollOffset := max(0, selected-listViewport+1)
		viewEnd := min(len(filteredTargets), scrollOffset+listViewport)

		if len(filteredTargets) == 0 {
			lines = append(lines, appTheme.panelMuted.Render("No matching tables."))
		} else {
			for i := scrollOffset; i < viewEnd; i++ {
				target := filteredTargets[i]
				if i == selected {
					lines = append(lines, appTheme.panelSelected.Render("> "+target.Display))
				} else {
					lines = append(lines, appTheme.panelText.Render("  "+target.Display))
				}
			}
		}
		if wizard.DirectInvocation {
			lines = append(lines, appTheme.panelHint.Render("enter confirm | ctrl+n next | ctrl+p prev | esc close"))
		} else {
			lines = append(lines, appTheme.panelHint.Render("enter confirm | ctrl+n next | ctrl+p prev | esc back"))
		}
	default:
		lines = append(lines, appTheme.panelText.Render("Step 1/2: choose a slash command"))
		const headerLines = 2 // title + step description
		const footerLines = 1
		listViewport := modalFixedRows - headerLines - footerLines
		if listViewport < 1 {
			listViewport = 1
		}
		selected := clampWizardIndex(wizard.SelectedCommand, len(wizard.Commands))
		scrollOffset := max(0, selected-listViewport+1)
		viewEnd := min(len(wizard.Commands), scrollOffset+listViewport)
		for i := scrollOffset; i < viewEnd; i++ {
			command := wizard.Commands[i]
			line := fmt.Sprintf("  %s - %s", command.DisplayName, command.Summary)
			if command.NeedsTarget {
				line += " (choose table next)"
			}
			if i == selected {
				lines = append(lines, appTheme.panelSelected.Render("> "+strings.TrimPrefix(line, "  ")))
			} else {
				lines = append(lines, appTheme.panelText.Render(line))
			}
		}
		lines = append(lines, appTheme.panelHint.Render("enter confirm | ctrl+n next | ctrl+p prev | esc close"))
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
