package app

import (
	"fmt"
	"sort"
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
	bottomPadding         = 0
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
	// vim-style navigation state: active while the user is cycling suggestions
	// with ctrl+n/ctrl+p; frozen list and original text allow cycling and ESC-restore.
	autocompleteNavActive     bool
	autocompleteNavOrigValue  string
	autocompleteNavOrigCursor int
	autocompleteNavFrozenList []tui.AutocompleteSuggestion
	// history navigation state
	historyNavIndex int    // -1 = at draft; 0 = most recent history entry
	historyNavDraft string // editor content saved when navigation begins
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
	OpenEditor           key.Binding
}

func defaultCommandModeKeys() commandModeKeyMap {
	return commandModeKeyMap{
		Submit:               key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "submit")),
		Cancel:               key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
		Help:                 key.NewBinding(key.WithKeys("ctrl+t"), key.WithHelp("ctrl+t", "keybindings")),
		History:              key.NewBinding(key.WithKeys("ctrl+r"), key.WithHelp("ctrl+r", "history")),
		RestoreHistory:       key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "restore")),
		SwitchMode:           key.NewBinding(key.WithKeys("ctrl+x"), key.WithHelp("ctrl+x", "focus")),
		LayoutCommandOnly:    key.NewBinding(key.WithKeys("ctrl+3"), key.WithHelp("ctrl+3", "command")),
		AcceptSuggestion:     key.NewBinding(key.WithKeys("tab", "ctrl+y"), key.WithHelp("tab/ctrl+y/enter", "accept")),
		NextSuggestion:       key.NewBinding(key.WithKeys("ctrl+n"), key.WithHelp("ctrl+n", "next suggestion")),
		PrevSuggestion:       key.NewBinding(key.WithKeys("ctrl+p"), key.WithHelp("ctrl+p", "prev suggestion")),
		ScrollTranscriptUp:   key.NewBinding(key.WithKeys("ctrl+u"), key.WithHelp("ctrl+u", "scroll up")),
		ScrollTranscriptDown: key.NewBinding(key.WithKeys("ctrl+d"), key.WithHelp("ctrl+d", "scroll down")),
		OpenEditor:           key.NewBinding(key.WithKeys("ctrl+e"), key.WithHelp("ctrl+e", "open in $EDITOR")),
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
		editor:          editor,
		widget:          tui.NewEditorWidget(),
		innerWidth:      defaultEditorWidth,
		innerHeight:     1,
		keys:            defaultCommandModeKeys(),
		historyNavIndex: -1,
	}
}

func (m commandModeModel) Init() tea.Cmd {
	return textarea.Blink
}

func (m commandModeModel) Update(msg tea.Msg, interaction InteractionState) (commandModeModel, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyPressMsg); ok {
		suggestions := m.computeSuggestions(interaction)
		switch {
		case key.Matches(keyMsg, m.keys.AcceptSuggestion), keyMsg.String() == "enter":
			if len(suggestions) > 0 {
				if m.autocompleteNavActive {
					// Text is already in the editor; just close the dropdown.
					m.clearAutocompleteNav()
				} else {
					m.applySuggestion(suggestions[m.widget.SelectedSuggestionIndex(len(suggestions))])
				}
				m.autocompleteOpenedByTyping = false
				return m, textarea.Blink
			}
		case key.Matches(keyMsg, m.keys.NextSuggestion):
			if len(suggestions) > 0 {
				if !m.autocompleteNavActive {
					m.autocompleteNavActive = true
					m.autocompleteNavOrigValue = m.editor.Value()
					m.autocompleteNavOrigCursor = m.cursorOffset()
					m.autocompleteNavFrozenList = suggestions
				}
				m.widget.SelectNextSuggestion(len(m.autocompleteNavFrozenList))
				sel := m.widget.SelectedSuggestion()
				if sel < 0 {
					m.editor.SetValue(m.autocompleteNavOrigValue)
					m.setCursorOffset(m.autocompleteNavOrigCursor)
				} else {
					m.applyNavSuggestion(m.autocompleteNavFrozenList[sel])
				}
				return m, nil
			}
			return m.navigateHistoryNext(interaction)
		case key.Matches(keyMsg, m.keys.PrevSuggestion):
			if len(suggestions) > 0 {
				if !m.autocompleteNavActive {
					m.autocompleteNavActive = true
					m.autocompleteNavOrigValue = m.editor.Value()
					m.autocompleteNavOrigCursor = m.cursorOffset()
					m.autocompleteNavFrozenList = suggestions
				}
				m.widget.SelectPrevSuggestion(len(m.autocompleteNavFrozenList))
				sel := m.widget.SelectedSuggestion()
				if sel < 0 {
					m.editor.SetValue(m.autocompleteNavOrigValue)
					m.setCursorOffset(m.autocompleteNavOrigCursor)
				} else {
					m.applyNavSuggestion(m.autocompleteNavFrozenList[sel])
				}
				return m, nil
			}
			return m.navigateHistoryPrev(interaction)
		case keyMsg.String() == "up":
			if len(suggestions) == 0 && m.editor.Line() == 0 {
				return m.navigateHistoryPrev(interaction)
			}
		case keyMsg.String() == "down":
			lines := splitEditorLines(m.editor.Value())
			if len(suggestions) == 0 && m.editor.Line() == len(lines)-1 {
				return m.navigateHistoryNext(interaction)
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
			if m.autocompleteNavActive {
				m.clearAutocompleteNav()
			}
			m.autocompleteOpenedByTyping = true
			m.historyNavIndex = -1
			m.historyNavDraft = ""
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
	m.clearAutocompleteNav()
	m.historyNavIndex = -1
	m.historyNavDraft = ""
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

// DismissAutocomplete suppresses the autocomplete dropdown. If vim-style
// navigation is active, the editor is first restored to the original prefix
// typed before navigation began.
func (m *commandModeModel) DismissAutocomplete() {
	if m.autocompleteNavActive {
		m.editor.SetValue(m.autocompleteNavOrigValue)
		m.setCursorOffset(m.autocompleteNavOrigCursor)
		m.clearAutocompleteNav()
	}
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

func (m *commandModeModel) clearAutocompleteNav() {
	m.autocompleteNavActive = false
	m.autocompleteNavOrigValue = ""
	m.autocompleteNavOrigCursor = 0
	m.autocompleteNavFrozenList = nil
	m.widget.ResetSuggestionSelection()
}

// applyNavSuggestion writes item into the editor, always computing the replace
// range from the saved original value/cursor so cycling never compounds replacements.
func (m *commandModeModel) applyNavSuggestion(item tui.AutocompleteSuggestion) {
	value := []rune(m.autocompleteNavOrigValue)
	ctx := analyzeAutocompleteContext(m.autocompleteNavOrigValue, m.autocompleteNavOrigCursor)
	start := clampCursorOffset(ctx.ReplaceStart, len(value))
	end := clampCursorOffset(ctx.ReplaceEnd, len(value))
	insert := []rune(item.InsertText)
	updated := string(append(append(append([]rune(nil), value[:start]...), insert...), value[end:]...))
	m.editor.SetValue(updated)
	m.setCursorOffset(start + len(insert))
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
	m.clearAutocompleteNav()
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
	parts = append(parts, "enter submit", bindingSummary(m.keys.Cancel), bindingSummary(m.keys.History), bindingSummary(m.keys.OpenEditor))
	if interaction.Running != nil {
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
	parts = append(parts, bindingSummary(m.keys.Submit), bindingSummary(m.keys.Cancel), bindingSummary(m.keys.History), bindingSummary(m.keys.OpenEditor), bindingSummary(m.keys.SwitchMode), bindingSummary(m.keys.LayoutCommandOnly), bindingSummary(m.keys.Help))
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
		AutocompleteTokenCol:    m.autocompleteTokenCol(),
	}
}

// autocompleteTokenCol returns the visual column of the start of the token
// being completed. This stays constant while cycling through suggestions so
// the dropdown doesn't drift left/right as candidates change.
func (m commandModeModel) autocompleteTokenCol() int {
	if len(m.cachedSuggestions) == 0 {
		return m.editor.LineInfo().ColumnOffset
	}
	value := m.editor.Value()
	cursor := m.cursorOffset()
	if m.autocompleteNavActive {
		value = m.autocompleteNavOrigValue
		cursor = m.autocompleteNavOrigCursor
	}
	ctx := analyzeAutocompleteContext(value, cursor)
	prefixLen := cursor - ctx.ReplaceStart
	col := lineColFromOffset(value, cursor)
	return max(0, col-prefixLen)
}

// lineColFromOffset returns the number of runes between the start of the
// current line and the given rune offset in value.
func lineColFromOffset(value string, offset int) int {
	runes := []rune(value)
	if offset > len(runes) {
		offset = len(runes)
	}
	col := 0
	for i := offset - 1; i >= 0; i-- {
		if runes[i] == '\n' {
			break
		}
		col++
	}
	return col
}

// computeSuggestions computes the current autocomplete suggestions from the
// editor state and interaction context. The result is cached in Update so that
// View rendering never calls buildAutocompleteItems.
func (m commandModeModel) computeSuggestions(interaction InteractionState) []tui.AutocompleteSuggestion {
	if m.autocompleteNavActive {
		return m.autocompleteNavFrozenList
	}
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

	var lines []string
	switch wizard.Step {
	case SlashCommandWizardStepColumn:
		// Single-box (no filter); uses ModalFixedRows.
		selectedCommand, _ := slashWizardCommandByIndex(wizard)
		selectedTarget, _ := slashWizardFilteredTargetByIndex(wizard)
		lines = append(lines,
			tui.AppTheme.PanelMuted.Render(fmt.Sprintf("Step 1/3 complete: %s", selectedCommand.DisplayName)),
			tui.AppTheme.PanelMuted.Render(fmt.Sprintf("Step 2/3 complete: %s", selectedTarget.Display)),
			tui.AppTheme.PanelText.Render(fmt.Sprintf("Step 3/3: choose columns for %s", selectedTarget.Display)),
		)
		const columnHeaderLines = 3
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
		// Two-box (filter visible); uses ModalSplitListRows.
		selectedCommand, _ := slashWizardCommandByIndex(wizard)
		totalSteps := 2
		if selectedCommand.NeedsColumns {
			totalSteps = 3
		}
		var headerLines int
		if wizard.DirectInvocation {
			lines = append(lines,
				tui.AppTheme.PanelText.Render(fmt.Sprintf("Choose a table for %s:", selectedCommand.DisplayName)),
			)
			headerLines = 1
		} else {
			lines = append(lines,
				tui.AppTheme.PanelMuted.Render(fmt.Sprintf("Step 1/%d complete: %s", totalSteps, selectedCommand.DisplayName)),
				tui.AppTheme.PanelText.Render(fmt.Sprintf("Step 2/%d: choose a table for %s", totalSteps, selectedCommand.DisplayName)),
			)
			headerLines = 2
		}

		filteredTargets := filterWizardTargets(wizard.Targets, wizard.TargetFilter)
		listViewport := tui.ModalSplitListRows - headerLines
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
		// Single-box (no filter); uses ModalFixedRows.
		selectedCommand, _ := slashWizardCommandByIndex(wizard)
		totalSteps := 2
		if selectedCommand.NeedsColumns {
			totalSteps = 3
		}
		lines = append(lines, tui.AppTheme.PanelText.Render(fmt.Sprintf("Step 1/%d: choose a slash command", totalSteps)))
		const headerLines = 1
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

func (m commandModeModel) navigateHistoryPrev(interaction InteractionState) (commandModeModel, tea.Cmd) {
	history := deduplicatedHistory(interaction.History)
	if len(history) == 0 {
		return m, nil
	}
	if m.historyNavIndex == -1 {
		m.historyNavDraft = m.editor.Value()
		m.historyNavIndex = 0
		m.SetEditorValue(history[0].Statement)
		return m, nil
	}
	if m.historyNavIndex < len(history)-1 {
		m.historyNavIndex++
		m.SetEditorValue(history[m.historyNavIndex].Statement)
		return m, nil
	}
	return m, func() tea.Msg { return historyNavBoundaryMsg{} }
}

func (m commandModeModel) navigateHistoryNext(interaction InteractionState) (commandModeModel, tea.Cmd) {
	if m.historyNavIndex == -1 {
		return m, nil
	}
	history := deduplicatedHistory(interaction.History)
	if m.historyNavIndex > 0 {
		m.historyNavIndex--
		if m.historyNavIndex < len(history) {
			m.SetEditorValue(history[m.historyNavIndex].Statement)
		}
		return m, nil
	}
	draft := m.historyNavDraft
	m.historyNavIndex = -1
	m.historyNavDraft = ""
	m.SetEditorValue(draft)
	return m, nil
}

func deduplicatedHistory(entries []HistoryEntryContext) []HistoryEntryContext {
	matches := rankHistorySearchEntries(entries, "")
	result := make([]HistoryEntryContext, len(matches))
	for i, m := range matches {
		result[i] = m.Entry
	}
	return result
}

func filterWizardTargets(targets []SlashCommandWizardTarget, filter string) []SlashCommandWizardTarget {
	trimmed := strings.TrimSpace(filter)
	if trimmed == "" {
		return targets
	}
	type scored struct {
		target SlashCommandWizardTarget
		score  int
	}
	matches := make([]scored, 0, len(targets))
	for _, t := range targets {
		if score, ok := fuzzyMatch(trimmed, t.Display); ok {
			matches = append(matches, scored{t, score})
		}
	}
	sort.SliceStable(matches, func(i, j int) bool {
		return matches[i].score > matches[j].score
	})
	filtered := make([]SlashCommandWizardTarget, len(matches))
	for i, m := range matches {
		filtered[i] = m.target
	}
	return filtered
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
