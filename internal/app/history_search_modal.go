package app

import (
	"fmt"
	"sort"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/adwinying/sqlcery/internal/tui"
)

const historySearchPreviewRows = tui.ModalSplitListRows

// historySearchModal implements Modal for the reverse-history search overlay.
// It owns the filter text and selection index; History entries are read from
// InteractionState (set by syncHistorySnapshot on every history append).
type historySearchModal struct {
	filter        string
	selectedIndex int
	viewportStart int
}

func (h *historySearchModal) Name() AppModal { return ModalHistorySearch }

func (h *historySearchModal) FilterText() string  { return h.filter + "█" }
func (h *historySearchModal) FilterLabel() string { return "Filter:" }

func (h *historySearchModal) Title() string { return "History" }

func (h *historySearchModal) CounterText(interaction InteractionState) string {
	matches := filterHistorySearchEntries(interaction.History, h.filter)
	if len(matches) == 0 {
		return ""
	}
	selected := wrapHistorySearchIndex(h.selectedIndex, len(matches))
	return fmt.Sprintf("%d of %d", selected+1, len(matches))
}

func (h *historySearchModal) StatusBarHints(interaction InteractionState) []string {
	keys := defaultCommandModeKeys()
	matches := filterHistorySearchEntries(interaction.History, h.filter)
	escHint := "esc close"
	if strings.TrimSpace(h.filter) != "" {
		escHint = "esc clear filter"
	}
	switch {
	case len(interaction.History) == 0:
		return []string{escHint, bindingSummary(keys.Help)}
	case len(matches) == 0:
		return []string{escHint, bindingSummary(keys.Help)}
	default:
		return []string{"enter restore", escHint, "ctrl+p up", "ctrl+n down", bindingSummary(keys.Help)}
	}
}

func (h *historySearchModal) HandleKey(msg tea.KeyPressMsg, ctx ModalContext) ModalResult {
	keys := defaultCommandModeKeys()

	switch {
	case msg.String() == "ctrl+c":
		return modalResultPendingStatus{intent: IntentNone, action: "history", status: "", level: NotificationNone, dismiss: true}
	case key.Matches(msg, keys.Help):
		return modalResultForward{cmd: func() tea.Msg { return toggleHelpIntentMsg{} }}
	case key.Matches(msg, keys.RestoreHistory):
		return h.restore(ctx)
	case key.Matches(msg, keys.Cancel):
		if strings.TrimSpace(h.filter) != "" {
			h.filter = ""
			h.selectedIndex = 0
			h.viewportStart = 0
			return modalResultPendingStatus{intent: IntentHistory, action: "history", status: "", level: NotificationNone}
		}
		return modalResultPendingStatus{intent: IntentNone, action: "history", status: "", level: NotificationNone, dismiss: true}
	case key.Matches(msg, keys.PrevSuggestion), msg.String() == "up":
		return h.cycle(ctx, -1)
	case key.Matches(msg, keys.NextSuggestion), msg.String() == "down":
		return h.cycle(ctx, 1)
	case msg.String() == "backspace" || msg.String() == "ctrl+h" || msg.String() == "delete":
		return h.updateFilter(ctx, trimLastRune(h.filter))
	case msg.String() == "space":
		return h.updateFilter(ctx, h.filter+" ")
	case len(msg.Text) > 0 && !msg.Mod.Contains(tea.ModAlt):
		return h.updateFilter(ctx, h.filter+msg.Text)
	// Layout keys pass through so the user can rearrange panes while searching.
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

func (h *historySearchModal) Render(interaction InteractionState, innerWidth int) string {
	matches := filterHistorySearchEntries(interaction.History, h.filter)

	if len(interaction.History) == 0 {
		return tui.AppTheme.PanelMuted.Render("No history yet.")
	}

	if len(matches) == 0 {
		return tui.AppTheme.PanelMuted.Render("No fuzzy matches.")
	}

	blocks, vpStart, selected := h.historyViewport(interaction, innerWidth)

	vpEnd := vpStart + historySearchPreviewRows
	displayLines := make([]string, 0, historySearchPreviewRows)
	for i, block := range blocks {
		if block.rowStart >= vpEnd {
			break
		}
		for j, text := range block.lines {
			rowIdx := block.rowStart + j
			if rowIdx < vpStart {
				continue
			}
			if rowIdx >= vpEnd {
				break
			}
			isSelected := i == selected
			var rendered string
			if isSelected {
				if pad := innerWidth - ansi.StringWidth(text); pad > 0 {
					text = text + strings.Repeat(" ", pad)
				}
				rendered = tui.AppTheme.PanelSelected.Render(text)
			} else {
				rendered = tui.AppTheme.PanelText.Render(text)
			}
			displayLines = append(displayLines, rendered)
		}
	}

	return strings.Join(displayLines, "\n")
}

func (h *historySearchModal) restore(ctx ModalContext) ModalResult {
	matches := filterHistorySearchEntries(ctx.Interaction.History, h.filter)
	if len(matches) == 0 {
		return modalResultNone{}
	}
	idx := wrapHistorySearchIndex(h.selectedIndex, len(matches))
	return modalResultRestoreHistory{
		sql:    matches[idx].Statement,
		status: "",
		level:  NotificationNone,
	}
}

// historyViewport computes the render parameters for the history list.
// It returns blocks, the stored lazy-scroll viewport top, and the resolved
// selection index. Call updateHistoryViewport after any selection change so
// h.viewportStart is up to date before Render or HandleMouse reads it.
func (h *historySearchModal) historyViewport(interaction InteractionState, innerWidth int) (blocks []historyEntryBlock, vpStart int, selected int) {
	matches := filterHistorySearchEntries(interaction.History, h.filter)
	if len(matches) == 0 {
		return nil, 0, 0
	}
	contentW := max(1, innerWidth-2)
	selected = wrapHistorySearchIndex(h.selectedIndex, len(matches))
	blocks = computeHistoryBlocks(matches, contentW)
	vpStart = h.viewportStart
	return blocks, vpStart, selected
}

// computeHistoryBlocks builds the per-entry display line slices and their
// cumulative row offsets for the given filtered match list.
func computeHistoryBlocks(matches []HistoryEntryContext, contentW int) []historyEntryBlock {
	blocks := make([]historyEntryBlock, len(matches))
	cumRow := 0
	for i, m := range matches {
		wrapped := wrapTextAt(historySearchDisplaySQL(m.Statement), contentW)
		if len(wrapped) > historySearchPreviewRows {
			wrapped = wrapped[:historySearchPreviewRows]
			last := wrapped[len(wrapped)-1]
			wrapped[len(wrapped)-1] = ansi.Truncate(last, contentW-1, "") + "…"
		}
		blocks[i] = historyEntryBlock{lines: wrapped, rowStart: cumRow}
		cumRow += len(wrapped)
	}
	return blocks
}

// updateHistoryViewport applies lazy scroll after a selection change:
// scrolls only when the selected entry exits the current viewport window.
func (h *historySearchModal) updateHistoryViewport(interaction InteractionState) {
	matches := filterHistorySearchEntries(interaction.History, h.filter)
	if len(matches) == 0 {
		h.viewportStart = 0
		return
	}
	selected := wrapHistorySearchIndex(h.selectedIndex, len(matches))
	// Use the same inner-width approximation as HandleMouse.
	contentW := max(1, tui.ModalMaxWidth-4)
	blocks := computeHistoryBlocks(matches, contentW)
	if selected >= len(blocks) {
		h.viewportStart = 0
		return
	}
	selBlock := blocks[selected]
	selEnd := selBlock.rowStart + len(selBlock.lines)
	if selBlock.rowStart < h.viewportStart {
		h.viewportStart = selBlock.rowStart
	} else if selEnd > h.viewportStart+historySearchPreviewRows {
		h.viewportStart = selEnd - historySearchPreviewRows
	}
}

// historyEntryBlock holds the rendered lines for one history entry and its
// cumulative row offset within the full (pre-viewport) display.
type historyEntryBlock struct {
	lines    []string
	rowStart int
}

// HandleMouse implements Modal.HandleMouse for historySearchModal.
func (h *historySearchModal) HandleMouse(msg tea.MouseClickMsg, ctx ModalContext) ModalResult {
	if ctx.MouseListOffset < 0 {
		return modalResultNone{}
	}

	matches := filterHistorySearchEntries(ctx.Interaction.History, h.filter)
	if len(matches) == 0 {
		return modalResultNone{}
	}

	// innerWidth is unknown from ModalContext; use a representative value.
	// The exact value only affects text wrapping (wrapTextAt), and since the
	// modal is rendered at tui.ModalMaxWidth-2 columns, we use that.
	innerWidth := tui.ModalMaxWidth - 2
	blocks, vpStart, _ := h.historyViewport(ctx.Interaction, innerWidth)

	// Map the visible offset to the entry whose rendered block contains that row.
	absRow := vpStart + ctx.MouseListOffset
	clickedEntry := -1
	for i, block := range blocks {
		blockEnd := block.rowStart + len(block.lines)
		if absRow >= block.rowStart && absRow < blockEnd {
			clickedEntry = i
			break
		}
	}
	if clickedEntry < 0 || clickedEntry >= len(matches) {
		return modalResultNone{}
	}

	h.selectedIndex = clickedEntry
	if ctx.MouseDoubleClick {
		return modalResultRestoreHistory{
			sql:    matches[clickedEntry].Statement,
			status: "",
			level:  NotificationNone,
		}
	}
	return modalResultNone{}
}

// HandleMouseWheel implements Modal.HandleMouseWheel for historySearchModal.
// Clamps at the list boundaries — does not wrap.
func (h *historySearchModal) HandleMouseWheel(ctx ModalContext, msg tea.MouseWheelMsg) ModalResult {
	matches := filterHistorySearchEntries(ctx.Interaction.History, h.filter)
	if len(matches) == 0 {
		return modalResultNone{}
	}
	switch msg.Button {
	case tea.MouseWheelUp:
		h.selectedIndex = max(0, h.selectedIndex-1)
	case tea.MouseWheelDown:
		h.selectedIndex = min(h.selectedIndex+1, len(matches)-1)
	}
	h.updateHistoryViewport(ctx.Interaction)
	return modalResultNone{}
}

func (h *historySearchModal) cycle(ctx ModalContext, delta int) ModalResult {
	matches := filterHistorySearchEntries(ctx.Interaction.History, h.filter)
	if len(matches) == 0 {
		return modalResultNone{}
	}
	h.selectedIndex = wrapHistorySearchIndex(h.selectedIndex+delta, len(matches))
	h.updateHistoryViewport(ctx.Interaction)
	return modalResultNone{}
}

func (h *historySearchModal) updateFilter(ctx ModalContext, filter string) ModalResult {
	h.filter = filter
	h.selectedIndex = 0
	h.viewportStart = 0
	return modalResultNone{}
}

// --- pure helpers ---

func filterHistorySearchEntries(entries []HistoryEntryContext, filter string) []HistoryEntryContext {
	matches := rankHistorySearchEntries(entries, filter)
	filtered := make([]HistoryEntryContext, 0, len(matches))
	for _, match := range matches {
		filtered = append(filtered, match.Entry)
	}
	return filtered
}

type historySearchMatch struct {
	Entry HistoryEntryContext
	Score int
}

func rankHistorySearchEntries(entries []HistoryEntryContext, filter string) []historySearchMatch {
	trimmed := strings.TrimSpace(filter)
	matches := make([]historySearchMatch, 0, len(entries))
	seen := make(map[string]struct{}, len(entries))
	for i := len(entries) - 1; i >= 0; i-- {
		entry := entries[i]
		key := historySearchDisplaySQL(entry.Statement)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}

		if trimmed == "" {
			matches = append(matches, historySearchMatch{Entry: entry})
			continue
		}

		score, ok := fuzzyMatch(trimmed, entry.Statement)
		if !ok {
			continue
		}

		matches = append(matches, historySearchMatch{Entry: entry, Score: score})
	}

	if trimmed == "" {
		return matches
	}

	sort.SliceStable(matches, func(i, j int) bool {
		return matches[i].Score > matches[j].Score
	})
	return matches
}

func fuzzyMatch(query, candidate string) (int, bool) {
	needle := []rune(strings.ToLower(strings.TrimSpace(query)))
	haystack := []rune(strings.ToLower(candidate))
	if len(needle) == 0 {
		return 0, true
	}

	needleIndex := 0
	score := 0
	streak := 0
	firstMatch := -1
	lastMatch := -2

	for i, r := range haystack {
		if needleIndex >= len(needle) || r != needle[needleIndex] {
			streak = 0
			continue
		}

		if firstMatch < 0 {
			firstMatch = i
		}
		if lastMatch == i-1 {
			streak++
		} else {
			streak = 1
		}

		wordBonus := 0
		if i == 0 || isFuzzyWordSep(haystack[i-1]) {
			wordBonus = 15
		}
		score += 1 + streak*streak + wordBonus
		lastMatch = i
		needleIndex++
	}

	if needleIndex != len(needle) {
		return 0, false
	}

	if firstMatch >= 0 {
		score += max(0, 32-firstMatch)
	}
	return score, true
}

func isFuzzyWordSep(r rune) bool {
	return r == ' ' || r == '_' || r == '+' || r == '-' || r == '/' || r == '.'
}

func wrapHistorySearchIndex(index, count int) int {
	if count <= 0 {
		return 0
	}

	index %= count
	if index < 0 {
		index += count
	}
	return index
}

func trimLastRune(value string) string {
	runes := []rune(value)
	if len(runes) == 0 {
		return ""
	}
	return string(runes[:len(runes)-1])
}

func historySearchDisplaySQL(sql string) string {
	return strings.Join(strings.Fields(sql), " ")
}

// wrapTextAt breaks s into lines of at most width display columns at rune boundaries.
func wrapTextAt(s string, width int) []string {
	if width <= 0 || s == "" {
		return []string{s}
	}
	var lines []string
	for {
		w := ansi.StringWidth(s)
		if w <= width {
			lines = append(lines, s)
			return lines
		}
		lines = append(lines, ansi.Cut(s, 0, width))
		s = ansi.Cut(s, width, w)
	}
}
