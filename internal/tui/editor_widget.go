package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

const (
	editorAutocompletePanelRows = 5
	editorBottomPadding         = 0
)

// AutocompleteSuggestion is a single autocomplete item, pre-computed by
// internal/app and passed into the widget via EditorViewContext.
type AutocompleteSuggestion struct {
	Label      string
	InsertText string
	Kind       string
	Detail     string
}

// editorTranscriptEntry is one entry in the REPL transcript.
type editorTranscriptEntry struct {
	Prompt string
	SQL    string
	Output string
}

// EditorViewContext contains everything the EditorWidget needs to render;
// internal/app constructs it from InteractionState and the textarea.Model.
type EditorViewContext struct {
	// Editor content from textarea.Model
	Value           string
	Lines           []string // SplitEditorLines(Value)
	CursorLine      int      // textarea.Line()
	RowOffset       int      // textarea.LineInfo().RowOffset
	ColOffset       int      // textarea.LineInfo().ColumnOffset
	CharOffset      int      // textarea.LineInfo().CharOffset
	Width           int      // textarea.Width()
	Height          int      // commandModeModel.innerHeight
	Prompt          string
	Placeholder     string
	ShowLineNumbers bool
	MaxHeight       int // for line number digit width
	// Autocomplete — pre-computed by app in Update
	AutocompleteSuggestions []AutocompleteSuggestion
	GhostText               string // pre-computed suffix to show after cursor
	PromptWidth             int    // display width of Prompt, for dropdown indent
	AutocompleteTokenCol    int    // column of the token start, for stable dropdown anchor
}

// EditorWidget owns cosmetic state for the command-mode editor: transcript,
// scroll offset, and suggestion selection. All rendering is stateless given
// the context.
type EditorWidget struct {
	highlighter        sqlSyntaxHighlighter
	transcript         []editorTranscriptEntry
	scrollOffset       int
	selectedSuggestion int
}

// NewEditorWidget creates a new EditorWidget ready to use.
func NewEditorWidget() EditorWidget {
	return EditorWidget{
		highlighter: newSQLSyntaxHighlighter(),
	}
}

// AppendEntry adds a completed REPL entry to the transcript and resets scroll
// to follow the latest output.
func (w *EditorWidget) AppendEntry(prompt, sql, output string) {
	w.transcript = append(w.transcript, editorTranscriptEntry{
		Prompt: prompt,
		SQL:    sql,
		Output: output,
	})
	w.scrollOffset = 0
}

// ScrollOffset returns the current manual scroll offset (lines scrolled up
// from the natural bottom position).
func (w EditorWidget) ScrollOffset() int { return w.scrollOffset }

// SelectedSuggestion returns the current suggestion selection index.
func (w EditorWidget) SelectedSuggestion() int { return w.selectedSuggestion }

// ScrollUp scrolls the transcript view up by step lines.
func (w *EditorWidget) ScrollUp(step, naturalTop int) {
	w.scrollOffset = min(w.scrollOffset+step, naturalTop)
}

// ScrollDown scrolls the transcript view down by step lines.
func (w *EditorWidget) ScrollDown(step int) {
	w.scrollOffset = max(0, w.scrollOffset-step)
}

// SnapToBottom resets the manual scroll so the view follows the latest output.
func (w *EditorWidget) SnapToBottom() {
	w.scrollOffset = 0
}

// SelectNextSuggestion advances the selection by one. Wraps from the last item
// to -1 (the restore slot), and from -1 back to item 0.
func (w *EditorWidget) SelectNextSuggestion(count int) {
	if count <= 0 {
		w.selectedSuggestion = 0
		return
	}
	switch {
	case w.selectedSuggestion == -1:
		w.selectedSuggestion = 0
	case w.selectedSuggestion >= count-1:
		w.selectedSuggestion = -1
	default:
		w.selectedSuggestion++
	}
}

// SelectPrevSuggestion moves the selection back by one. Wraps from item 0 to
// -1 (the restore slot), and from -1 to the last item.
func (w *EditorWidget) SelectPrevSuggestion(count int) {
	if count <= 0 {
		w.selectedSuggestion = 0
		return
	}
	switch {
	case w.selectedSuggestion == -1:
		w.selectedSuggestion = count - 1
	case w.selectedSuggestion <= 0:
		w.selectedSuggestion = -1
	default:
		w.selectedSuggestion--
	}
}

// ClampSuggestionSelection ensures selectedSuggestion is within [0, count).
// The restore slot (-1) is preserved as-is.
func (w *EditorWidget) ClampSuggestionSelection(count int) {
	if count == 0 {
		w.selectedSuggestion = 0
		return
	}
	if w.selectedSuggestion == -1 {
		return
	}
	w.selectedSuggestion = w.selectedSuggestionIndex(count)
}

// ResetSuggestionSelection resets selection to the first suggestion.
func (w *EditorWidget) ResetSuggestionSelection() {
	w.selectedSuggestion = 0
}

// SetSuggestionIndex sets the selection to an explicit index. Accepts -1 for
// the restore slot.
func (w *EditorWidget) SetSuggestionIndex(i int) {
	w.selectedSuggestion = i
}

// SelectedSuggestionIndex returns the clamped selection index for count suggestions.
func (w EditorWidget) SelectedSuggestionIndex(count int) int {
	return w.selectedSuggestionIndex(count)
}

func (w EditorWidget) selectedSuggestionIndex(count int) int {
	if count <= 0 || w.selectedSuggestion < 0 {
		return 0
	}
	if w.selectedSuggestion >= count {
		return count - 1
	}
	return w.selectedSuggestion
}

// ComputeNaturalScrollTop returns the scroll-top that keeps the last editor
// row on screen with the reserve rows visible below it. Pass this value to
// ScrollUp to bound the scroll offset.
func (w EditorWidget) ComputeNaturalScrollTop(ctx EditorViewContext) int {
	viewportH := max(1, ctx.Height)
	transcriptLen := len(w.renderTranscriptLines())
	totalEditorRows := w.countEditorRows(ctx)
	contentRows := transcriptLen + totalEditorRows
	topPadding := max(0, viewportH-editorBottomPadding-contentRows)
	lastEditorRow := topPadding + transcriptLen + totalEditorRows - 1
	return max(0, lastEditorRow+editorBottomPadding-viewportH+1)
}

// View renders the full command pane content for the given context.
func (w EditorWidget) View(ctx EditorViewContext) string {
	viewportH := max(1, ctx.Height)

	transcriptLines := w.renderTranscriptLines()

	var editorStrings []string
	var cursorRowInEditor int
	var totalEditorRows int
	if ctx.Value == "" && strings.TrimSpace(ctx.Placeholder) != "" {
		editorStrings = []string{w.renderPlaceholderLine(ctx)}
		cursorRowInEditor = 0
		totalEditorRows = 1
	} else {
		wrappedLines, editorCursor, editorTotal := w.renderedLines(ctx)
		editorStrings = w.renderAllEditorLines(wrappedLines, ctx)
		cursorRowInEditor = editorCursor
		totalEditorRows = editorTotal
	}

	contentRows := len(transcriptLines) + totalEditorRows
	topPadding := max(0, viewportH-editorBottomPadding-contentRows)

	allLines := make([]string, 0, topPadding+contentRows+editorBottomPadding)
	for i := 0; i < topPadding; i++ {
		allLines = append(allLines, "")
	}
	allLines = append(allLines, transcriptLines...)
	allLines = append(allLines, editorStrings...)
	for i := 0; i < editorBottomPadding; i++ {
		allLines = append(allLines, "")
	}

	cursorRow := topPadding + len(transcriptLines) + cursorRowInEditor
	lastEditorRow := topPadding + len(transcriptLines) + totalEditorRows - 1

	naturalScrollTop := max(0, lastEditorRow+editorBottomPadding-viewportH+1)
	rawScroll := max(0, naturalScrollTop-w.scrollOffset)

	var scrollTop int
	if w.scrollOffset == 0 {
		scrollTop = editorAdjustedScrollTop(rawScroll, cursorRow, len(allLines), viewportH)
		scrollTop = min(scrollTop, naturalScrollTop)
	} else {
		scrollTop = max(0, min(rawScroll, naturalScrollTop))
	}

	visible := make([]string, viewportH)
	for i := range visible {
		if idx := scrollTop + i; idx < len(allLines) {
			visible[i] = allLines[idx]
		}
	}

	dropdown := w.renderAutocompleteDropdown(ctx)
	if dropdown == "" {
		return strings.Join(visible, "\n")
	}

	cursorVisualRow := cursorRow - scrollTop
	dropdownLines := strings.Split(dropdown, "\n")
	overlayStart := cursorVisualRow + 1
	if cursorVisualRow >= len(dropdownLines) {
		overlayStart = cursorVisualRow - len(dropdownLines)
	}
	for i, dl := range dropdownLines {
		if row := overlayStart + i; row >= 0 && row < len(visible) {
			visible[row] = dl
		}
	}

	return strings.Join(visible, "\n")
}

// RenderDropdown renders just the autocomplete dropdown string for the given
// context. Exposed so internal/app can offer a thin wrapper for tests.
func (w EditorWidget) RenderDropdown(ctx EditorViewContext) string {
	return w.renderAutocompleteDropdown(ctx)
}

// RenderAutocompletePanel renders the panel-style suggestion list (used by
// the help overlay / split layout).
func (w EditorWidget) RenderAutocompletePanel(suggestions []AutocompleteSuggestion) string {
	selected := w.selectedSuggestionIndex(len(suggestions))
	visible := min(len(suggestions), editorAutocompletePanelRows)
	start := 0
	if selected >= visible {
		start = selected - visible + 1
	}

	lines := make([]string, 0, editorAutocompletePanelRows+1)
	if len(suggestions) > 0 {
		lines = append(lines, AppTheme.PanelTitle.Render("Suggestions:"))
	} else {
		lines = append(lines, AppTheme.PanelMuted.Render("Suggestions:"))
	}

	for i := start; i < start+editorAutocompletePanelRows; i++ {
		if i < len(suggestions) {
			item := suggestions[i]
			line := fmt.Sprintf("  [%s] %s", item.Kind, item.Label)
			if detail := strings.TrimSpace(item.Detail); detail != "" {
				line += " - " + detail
			}
			if i == selected {
				lines = append(lines, AppTheme.PanelSelected.Render("> "+strings.TrimPrefix(line, "  ")))
			} else {
				lines = append(lines, AppTheme.PanelText.Render(line))
			}
		} else {
			lines = append(lines, "")
		}
	}

	return strings.Join(lines, "\n")
}

func (w EditorWidget) renderAutocompleteDropdown(ctx EditorViewContext) string {
	suggestions := ctx.AutocompleteSuggestions
	if len(suggestions) == 0 {
		return ""
	}

	// rawSelected may be -1 (restore slot) — no row is highlighted in that case.
	rawSelected := w.selectedSuggestion
	visible := min(len(suggestions), editorAutocompletePanelRows)
	start := 0
	if rawSelected >= visible {
		start = rawSelected - visible + 1
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
		if i == rawSelected {
			lines = append(lines, AppTheme.PanelSelected.Render(line))
		} else {
			lines = append(lines, AppTheme.PanelText.Render(line))
		}
	}

	maxWidth := 0
	for _, line := range lines {
		if w := ansi.StringWidth(line); w > maxWidth {
			maxWidth = w
		}
	}
	popupWidth := maxWidth
	editorWidth := ctx.Width
	if popupWidth > editorWidth {
		popupWidth = editorWidth
	}
	if popupWidth < 10 {
		popupWidth = 10
	}

	indent := ctx.PromptWidth + ctx.AutocompleteTokenCol
	if indent+popupWidth > editorWidth+ctx.PromptWidth {
		indent = max(0, editorWidth+ctx.PromptWidth-popupWidth)
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

func (w EditorWidget) renderTranscriptLines() []string {
	if len(w.transcript) == 0 {
		return nil
	}
	lines := make([]string, 0)
	for _, entry := range w.transcript {
		prompt := entry.Prompt
		if prompt == "" {
			prompt = "> "
		}
		sqlLines := strings.Split(strings.TrimRight(entry.SQL, "\n"), "\n")
		for i, sl := range sqlLines {
			if i == 0 {
				lines = append(lines, AppTheme.PromptStyle.Render(prompt)+AppTheme.PanelText.Render(sl))
			} else {
				continuation := strings.Repeat(" ", len([]rune(prompt)))
				lines = append(lines, AppTheme.PanelMuted.Render(continuation+sl))
			}
		}
		if entry.Output != "" {
			for _, ol := range strings.Split(strings.TrimRight(entry.Output, "\n"), "\n") {
				lines = append(lines, AppTheme.PanelMuted.Render(ol))
			}
		}
	}
	return lines
}

func (w EditorWidget) countEditorRows(ctx EditorViewContext) int {
	if ctx.Value == "" {
		return 1
	}
	_, _, total := w.renderedLines(ctx)
	return total
}

func (w EditorWidget) renderedLines(ctx EditorViewContext) ([]editorRenderedLine, int, int) {
	highlighted := w.highlighter.highlightLines(ctx.Lines)
	contentWidth := max(1, ctx.Width)
	currentLine := ctx.CursorLine

	wrappedLines := make([]editorRenderedLine, 0, len(highlighted))
	cursorVisualRow := 0

	for lineIndex, line := range highlighted {
		segments := editorWrapStyledLine(line, contentWidth)
		if len(segments) == 0 {
			segments = []sqlStyledLine{{}}
		}

		for segmentIndex, segment := range segments {
			visualLine := editorRenderedLine{
				logicalLine: lineIndex,
				lineNumber:  0,
				runes:       segment,
				cursorCol:   -1,
			}
			if segmentIndex == 0 {
				visualLine.lineNumber = lineIndex + 1
			}
			if lineIndex == currentLine && segmentIndex == ctx.RowOffset {
				visualLine.isCursor = true
				visualLine.cursorCol = ctx.ColOffset
				cursorVisualRow = len(wrappedLines)
			}
			wrappedLines = append(wrappedLines, visualLine)
		}
	}

	if len(wrappedLines) == 0 {
		wrappedLines = append(wrappedLines, editorRenderedLine{lineNumber: 1, isCursor: true, cursorCol: 0})
	}

	return wrappedLines, cursorVisualRow, len(wrappedLines)
}

func (w EditorWidget) renderAllEditorLines(lines []editorRenderedLine, ctx EditorViewContext) []string {
	result := make([]string, len(lines))
	for i, line := range lines {
		lineGhost := ""
		if line.isCursor {
			lineGhost = ctx.GhostText
		}
		result[i] = w.renderEditorLine(line, lineGhost, ctx)
	}
	return result
}

func (w EditorWidget) renderPlaceholderLine(ctx EditorViewContext) string {
	line := editorRenderedLine{
		logicalLine: 0,
		lineNumber:  1,
		runes:       sqlStyledLine{},
		isCursor:    true,
		cursorCol:   0,
	}
	return w.renderEditorLine(line, "", ctx)
}

func (w EditorWidget) renderEditorLine(line editorRenderedLine, ghostText string, ctx EditorViewContext) string {
	lineStyle := AppTheme.PanelText
	lineNumberStyle := w.highlighter.lineNumberStyle
	content := w.highlighter.renderLineContentWithGhost(line.runes, line.cursorCol, ctx.Width, false, ghostText)

	if line.isCursor {
		lineStyle = w.highlighter.cursorLineStyle
		lineNumberStyle = w.highlighter.cursorLineNumberStyle
	}

	if line.logicalLine == 0 && ctx.Value == "" && strings.TrimSpace(ctx.Placeholder) != "" {
		content = w.highlighter.renderLineContentWithGhost(line.runes, line.cursorCol, ctx.Width, true, "")
	}

	prompt := w.highlighter.promptStyle.Render(ctx.Prompt)
	lineNumber := ""
	if ctx.ShowLineNumbers {
		label := editorFormatLineNumber(" ", ctx.MaxHeight)
		if line.lineNumber > 0 {
			label = editorFormatLineNumber(line.lineNumber, ctx.MaxHeight)
		}
		lineNumber = lineNumberStyle.Render(label)
	}

	return lineStyle.Render(prompt + lineNumber + content)
}

func editorFormatLineNumber(value any, maxHeight int) string {
	digits := len(strconv.Itoa(max(1, maxHeight)))
	return fmt.Sprintf(" %*v ", digits, value)
}

func editorAdjustedScrollTop(current, cursorRow, totalRows, height int) int {
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
