package app

import (
	"fmt"
	"sort"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

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

// acSuppressedAt records the editor snapshot at the moment the user dismissed
// autocomplete. Suggestions stay hidden as long as value and cursor are unchanged.
type acSuppressedAt struct {
	value  string
	cursor int
}

// acNavState holds the frozen state for vim-style suggestion cycling (ctrl+n/ctrl+p).
// origValue and origCursor allow ESC to restore the pre-nav text.
type acNavState struct {
	origValue  string
	origCursor int
	frozenList []tui.AutocompleteSuggestion
}

type commandModeModel struct {
	editor      textarea.Model
	keys        commandModeKeyMap
	widget      tui.EditorWidget
	innerWidth  int
	innerHeight int
	// autocomplete state: gate flag + two optional sub-states
	acOpenedByTyping bool
	acSuppressed     *acSuppressedAt // non-nil while dropdown is suppressed
	acNav            *acNavState     // non-nil while cycling through frozen suggestions
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
	AcceptSuggestion     key.Binding
	NextSuggestion       key.Binding
	PrevSuggestion       key.Binding
	ScrollTranscriptUp   key.Binding
	ScrollTranscriptDown key.Binding
	OpenEditor           key.Binding
	SwitchConnection     key.Binding
}

func defaultCommandModeKeys() commandModeKeyMap {
	return commandModeKeyMap{
		Submit:               key.NewBinding(key.WithKeys("enter", "ctrl+o"), key.WithHelp("enter/ctrl+o", "submit")),
		Cancel:               key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
		Help:                 key.NewBinding(key.WithKeys("ctrl+t"), key.WithHelp("ctrl+t", "keybindings")),
		History:              key.NewBinding(key.WithKeys("ctrl+r"), key.WithHelp("ctrl+r", "history")),
		RestoreHistory:       key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "restore")),
		SwitchMode:           key.NewBinding(key.WithKeys("ctrl+x"), key.WithHelp("ctrl+x", "focus")),
		AcceptSuggestion:     key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab/enter", "accept")),
		NextSuggestion:       key.NewBinding(key.WithKeys("ctrl+n"), key.WithHelp("ctrl+n", "next suggestion")),
		PrevSuggestion:       key.NewBinding(key.WithKeys("ctrl+p"), key.WithHelp("ctrl+p", "prev suggestion")),
		ScrollTranscriptUp:   key.NewBinding(key.WithKeys("ctrl+u"), key.WithHelp("ctrl+u", "scroll up")),
		ScrollTranscriptDown: key.NewBinding(key.WithKeys("ctrl+d"), key.WithHelp("ctrl+d", "scroll down")),
		OpenEditor:           key.NewBinding(key.WithKeys("ctrl+e"), key.WithHelp("ctrl+e", "open in $EDITOR")),
		SwitchConnection:     key.NewBinding(key.WithKeys("ctrl+s"), key.WithHelp("ctrl+s", "switch connection")),
	}
}

func newCommandModeModel() commandModeModel {
	editor := textarea.New()
	editor.Prompt = "> "
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
		suggestions := m.computeSuggestions(interaction.AutocompleteSchema, interaction.LatestResult)
		switch {
		case key.Matches(keyMsg, m.keys.AcceptSuggestion), keyMsg.String() == "enter":
			if len(suggestions) > 0 {
				if m.acNav != nil {
					// Text is already in the editor; just close the dropdown.
					m.clearAutocompleteNav()
				} else {
					m.applySuggestion(suggestions[m.widget.SelectedSuggestionIndex(len(suggestions))])
				}
				m.acOpenedByTyping = false
				return m, textarea.Blink
			}
		case key.Matches(keyMsg, m.keys.NextSuggestion):
			if len(suggestions) > 0 {
				if m.acNav == nil {
					m.acNav = &acNavState{
						origValue:  m.editor.Value(),
						origCursor: m.cursorOffset(),
						frozenList: suggestions,
					}
				}
				m.widget.SelectNextSuggestion(len(m.acNav.frozenList))
				sel := m.widget.SelectedSuggestion()
				if sel < 0 {
					m.editor.SetValue(m.acNav.origValue)
					m.setCursorOffset(m.acNav.origCursor)
				} else {
					m.applyNavSuggestion(m.acNav.frozenList[sel])
				}
				return m, nil
			}
			return m.navigateHistoryNext(interaction)
		case key.Matches(keyMsg, m.keys.PrevSuggestion):
			if len(suggestions) > 0 {
				if m.acNav == nil {
					m.acNav = &acNavState{
						origValue:  m.editor.Value(),
						origCursor: m.cursorOffset(),
						frozenList: suggestions,
					}
				}
				m.widget.SelectPrevSuggestion(len(m.acNav.frozenList))
				sel := m.widget.SelectedSuggestion()
				if sel < 0 {
					m.editor.SetValue(m.acNav.origValue)
					m.setCursorOffset(m.acNav.origCursor)
				} else {
					m.applyNavSuggestion(m.acNav.frozenList[sel])
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
			m.ScrollTranscriptUp(max(1, m.innerHeight/2), interaction)
			return m, nil
		case key.Matches(keyMsg, m.keys.ScrollTranscriptDown):
			m.ScrollTranscriptDown(max(1, m.innerHeight/2))
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
			if m.acNav != nil {
				m.clearAutocompleteNav()
			}
			m.acOpenedByTyping = true
			m.historyNavIndex = -1
			m.historyNavDraft = ""
		} else {
			m.acOpenedByTyping = false
		}
	}

	m.cachedSuggestions = m.computeSuggestions(interaction.AutocompleteSchema, interaction.LatestResult)
	m.widget.ClampSuggestionSelection(len(m.cachedSuggestions))
	return m, cmd
}

func (m *commandModeModel) Clear() {
	m.editor.Reset()
	m.editor.Focus()
	m.widget.ResetSuggestionSelection()
	m.cachedSuggestions = nil
	m.acOpenedByTyping = false
	m.acSuppressed = nil
	m.acNav = nil
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
	return len(m.computeSuggestions(interaction.AutocompleteSchema, interaction.LatestResult)) > 0
}

// DismissAutocomplete suppresses the autocomplete dropdown. If vim-style
// navigation is active, the editor is first restored to the original prefix
// typed before navigation began.
func (m *commandModeModel) DismissAutocomplete() {
	if m.acNav != nil {
		m.editor.SetValue(m.acNav.origValue)
		m.setCursorOffset(m.acNav.origCursor)
		m.clearAutocompleteNav()
	}
	m.acSuppressed = &acSuppressedAt{value: m.editor.Value(), cursor: m.cursorOffset()}
	m.widget.ResetSuggestionSelection()
	m.cachedSuggestions = nil
	m.acOpenedByTyping = false
}

func (m *commandModeModel) clearAutocompleteNav() {
	m.acNav = nil
	m.widget.ResetSuggestionSelection()
}

// applyNavSuggestion writes item into the editor, always computing the replace
// range from the saved original value/cursor so cycling never compounds replacements.
func (m *commandModeModel) applyNavSuggestion(item tui.AutocompleteSuggestion) {
	value := []rune(m.acNav.origValue)
	ctx := analyzeAutocompleteContext(m.acNav.origValue, m.acNav.origCursor)
	start := clampCursorOffset(ctx.ReplaceStart, len(value))
	end := clampCursorOffset(ctx.ReplaceEnd, len(value))
	insert := []rune(item.InsertText)
	updated := string(append(append(append([]rune(nil), value[:start]...), insert...), value[end:]...))
	m.editor.SetValue(updated)
	m.setCursorOffset(start + len(insert))
}

func (m *commandModeModel) Focus() {
	m.editor.Focus()
	m.acOpenedByTyping = false
}

func (m *commandModeModel) Blur() {
	m.editor.Blur()
	m.acOpenedByTyping = false
}

// SetEditorValue replaces the editor buffer programmatically without opening
// the autocomplete menu.
func (m *commandModeModel) SetEditorValue(value string) {
	m.editor.SetValue(value)
	m.editor.CursorEnd()
	m.widget.ResetSuggestionSelection()
	m.cachedSuggestions = nil
	m.acOpenedByTyping = false
	m.acSuppressed = nil
	m.acNav = nil
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

func (m commandModeModel) StatusBarHints(interaction InteractionState) []string {
	autocompleteActive := len(m.cachedSuggestions) > 0 || len(m.computeSuggestions(interaction.AutocompleteSchema, interaction.LatestResult)) > 0
	var parts []string
	if autocompleteActive {
		// enter accepts the suggestion here, not submits; ctrl+c closes the dropdown
		parts = append(parts, bindingSummary(m.keys.AcceptSuggestion), "esc cancel")
		parts = append(parts, bindingSummary(m.keys.NextSuggestion), bindingSummary(m.keys.PrevSuggestion))
	} else if interaction.Running != nil {
		parts = append(parts, "esc cancel query", "ctrl+c quit")
	} else {
		parts = append(parts, "enter submit", bindingSummary(m.keys.Cancel), "ctrl+c quit")
	}
	parts = append(parts, bindingSummary(m.keys.History), bindingSummary(m.keys.OpenEditor), bindingSummary(m.keys.Help))
	return parts
}

// ScrollTranscriptUp scrolls the REPL transcript upward by step lines, using
// the natural top computed from the current interaction state. Clears
// acOpenedByTyping so the autocomplete dropdown closes.
func (m *commandModeModel) ScrollTranscriptUp(step int, interaction InteractionState) {
	naturalTop := m.widget.ComputeNaturalScrollTop(m.buildViewContext(interaction))
	m.widget.ScrollUp(step, naturalTop)
	m.acOpenedByTyping = false
}

// ScrollTranscriptDown scrolls the REPL transcript downward by step lines.
// Clears acOpenedByTyping so the autocomplete dropdown closes.
func (m *commandModeModel) ScrollTranscriptDown(step int) {
	m.widget.ScrollDown(step)
	m.acOpenedByTyping = false
}

func (m *commandModeModel) AppendReplEntry(prompt, sql, output string) {
	m.widget.AppendEntry(prompt, sql, output)
}

// buildViewContext maps the textarea state and cached autocomplete suggestions
// into an EditorViewContext for stateless rendering by the EditorWidget.
func (m commandModeModel) buildViewContext(interaction InteractionState) tui.EditorViewContext {
	lineInfo := m.editor.LineInfo()
	return tui.EditorViewContext{
		Value:                   m.editor.Value(),
		Lines:                   tui.SplitEditorLines(m.editor.Value()),
		CursorLine:              m.editor.Line(),
		RowOffset:               lineInfo.RowOffset,
		ColOffset:               lineInfo.ColumnOffset,
		CharOffset:              lineInfo.CharOffset,
		Width:                   m.editor.Width(),
		Height:                  m.innerHeight,
		Prompt:                  m.editor.Prompt,
		ShowLineNumbers:         m.editor.ShowLineNumbers,
		MaxHeight:               m.editor.MaxHeight,
		AutocompleteSuggestions: m.cachedSuggestions,
		GhostText:               m.ghostText(),
		PromptWidth:             ansi.StringWidth(m.editor.Prompt),
		AutocompleteTokenCol:    m.autocompleteTokenCol(),
		ShowCursor:              interaction.ActivePane == PaneCommand && interaction.WindowFocused,
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
	if m.acNav != nil {
		value = m.acNav.origValue
		cursor = m.acNav.origCursor
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
func (m commandModeModel) computeSuggestions(schema *AutocompleteSchemaContext, latestResult *LatestResultContext) []tui.AutocompleteSuggestion {
	if m.acNav != nil {
		return m.acNav.frozenList
	}
	if !m.acOpenedByTyping {
		return nil
	}
	if m.acSuppressed != nil &&
		m.editor.Value() == m.acSuppressed.value &&
		m.cursorOffset() == m.acSuppressed.cursor {
		return nil
	}
	items := buildAutocompleteItems(m.editor.Value(), m.cursorOffset(), schema, latestResult)
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
	return m.computeSuggestions(interaction.AutocompleteSchema, interaction.LatestResult)
}

// renderAutocompleteDropdown renders the dropdown overlay for tests that call
// it directly. In production, the widget.View() handles this internally.
func (m commandModeModel) renderAutocompleteDropdown(interaction InteractionState) string {
	ctx := m.buildViewContext(interaction)
	ctx.AutocompleteSuggestions = m.computeSuggestions(interaction.AutocompleteSchema, interaction.LatestResult)
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

func renderSlashWizardContext(wizard *SlashCommandWizardContext, scrollOffset int, hScrollOffset *int, innerWidth int) string {
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
					*hScrollOffset = tui.ClampHScrollOffset(ansi.StringWidth(content), *hScrollOffset, innerWidth)
					lines = append(lines, tui.AppTheme.PanelSelected.Render(tui.ApplyHScroll(content, *hScrollOffset, innerWidth)))
				} else {
					lines = append(lines, tui.AppTheme.PanelText.Render(content))
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
		viewEnd := min(len(filteredTargets), scrollOffset+listViewport)

		if len(filteredTargets) == 0 {
			lines = append(lines, tui.AppTheme.PanelMuted.Render("No matching tables."))
		} else {
			for i := scrollOffset; i < viewEnd; i++ {
				target := filteredTargets[i]
				if i == selected {
					*hScrollOffset = tui.ClampHScrollOffset(ansi.StringWidth(target.Display), *hScrollOffset, innerWidth)
					lines = append(lines, tui.AppTheme.PanelSelected.Render(tui.ApplyHScroll(target.Display, *hScrollOffset, innerWidth)))
				} else {
					lines = append(lines, tui.AppTheme.PanelText.Render(target.Display))
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
		viewEnd := min(len(wizard.Commands), scrollOffset+listViewport)
		for i := scrollOffset; i < viewEnd; i++ {
			command := wizard.Commands[i]
			line := fmt.Sprintf("%s - %s", command.DisplayName, command.Summary)
			if i == selected {
				*hScrollOffset = tui.ClampHScrollOffset(ansi.StringWidth(line), *hScrollOffset, innerWidth)
				lines = append(lines, tui.AppTheme.PanelSelected.Render(tui.ApplyHScroll(line, *hScrollOffset, innerWidth)))
			} else {
				lines = append(lines, tui.AppTheme.PanelText.Render(line))
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
