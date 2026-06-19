package app

import (
	"fmt"
	"sort"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

const historySearchPreviewRows = modalFixedRows - 4 // 4 = title + query + match-count + hint

// historySearchModal implements Modal for the reverse-history search overlay.
// It owns the filter text and selection index; History entries are read from
// InteractionState (set by syncHistorySnapshot on every history append).
type historySearchModal struct {
	filter        string
	selectedIndex int
}

func (h *historySearchModal) Name() AppModal { return ModalHistorySearch }

func (h *historySearchModal) HandleKey(msg tea.KeyPressMsg, m *Model) tea.Cmd {
	keys := m.command.KeyMap()

	switch {
	case msg.String() == "ctrl+c":
		h.close(m, "Exited history search.")
		return nil
	case key.Matches(msg, keys.Help):
		return func() tea.Msg { return toggleHelpIntentMsg{} }
	case key.Matches(msg, keys.RestoreHistory):
		h.restore(m)
		return nil
	case key.Matches(msg, keys.Cancel):
		h.close(m, "Exited history search.")
		return nil
	case key.Matches(msg, keys.History), key.Matches(msg, keys.NextSuggestion), msg.String() == "up":
		h.cycle(m, 1)
		return nil
	case key.Matches(msg, keys.PrevSuggestion), msg.String() == "down":
		h.cycle(m, -1)
		return nil
	case msg.String() == "backspace" || msg.String() == "ctrl+h" || msg.String() == "delete":
		h.updateFilter(m, trimLastRune(h.filter))
		return nil
	case msg.String() == "space":
		h.updateFilter(m, h.filter+" ")
		return nil
	case len(msg.Text) > 0 && !msg.Mod.Contains(tea.ModAlt):
		h.updateFilter(m, h.filter+msg.Text)
		return nil
	// Layout keys pass through so the user can rearrange panes while searching.
	case key.Matches(msg, m.command.KeyMap().LayoutCommandOnly), msg.String() == "ctrl+3", msg.String() == "alt+3":
		return func() tea.Msg { return switchLayoutIntentMsg{Layout: LayoutCommandOnly} }
	case msg.String() == "ctrl+q":
		return func() tea.Msg { return focusPaneIntentMsg{Pane: PaneResults} }
	case msg.String() == "ctrl+w":
		return func() tea.Msg { return focusPaneIntentMsg{Pane: PaneCommand} }
	case msg.String() == "ctrl+z":
		return func() tea.Msg { return toggleZoomIntentMsg{} }
	default:
		return nil
	}
}

func (h *historySearchModal) Render(interaction InteractionState) string {
	matches := filterHistorySearchEntries(interaction.History, h.filter)
	lines := []string{
		appTheme.panelTitle.Render("Reverse search:"),
		appTheme.panelText.Render(fmt.Sprintf("query> %s", defaultHistorySearchQuery(h.filter))),
	}

	if len(interaction.History) == 0 {
		lines = append(lines, appTheme.panelMuted.Render("No history yet."), appTheme.panelHint.Render("esc close"))
		return strings.Join(lines, "\n")
	}

	if len(matches) == 0 {
		lines = append(lines, appTheme.panelMuted.Render("No fuzzy matches."), appTheme.panelHint.Render("ctrl+r keep searching | esc close"))
		return strings.Join(lines, "\n")
	}

	selected := wrapHistorySearchIndex(h.selectedIndex, len(matches))
	lines = append(lines, appTheme.panelMuted.Render(fmt.Sprintf("%d match(es); newest first.", len(matches))))

	scrollOffset := max(0, selected-historySearchPreviewRows+1)
	viewEnd := min(len(matches), scrollOffset+historySearchPreviewRows)
	for i := scrollOffset; i < viewEnd; i++ {
		display := historySearchDisplaySQL(matches[i].Statement)
		var line string
		if i == selected {
			line = appTheme.panelSelected.Render("> " + display)
		} else {
			line = appTheme.panelText.Render("  " + display)
		}
		lines = append(lines, line)
	}
	lines = append(lines, appTheme.panelHint.Render("enter restore | ctrl+r older | ctrl+n newer | esc close"))

	return strings.Join(lines, "\n")
}

func (h *historySearchModal) close(m *Model, status string) {
	m.modal = nil
	m.state.SetActiveModal(ModalNone)
	m.state.SetPendingIntent(IntentNone, "history", status)
}

func (h *historySearchModal) restore(m *Model) {
	matches := filterHistorySearchEntries(m.state.Interaction.History, h.filter)
	if len(matches) == 0 {
		m.state.SetPendingIntent(IntentHistory, "history", h.status(m.state.Interaction))
		return
	}
	idx := wrapHistorySearchIndex(h.selectedIndex, len(matches))
	m.command.SetEditorValue(matches[idx].Statement)
	m.syncCurrentSQL()
	h.close(m, "Restored selected history entry into the editor.")
}

func (h *historySearchModal) cycle(m *Model, delta int) {
	matches := filterHistorySearchEntries(m.state.Interaction.History, h.filter)
	if len(matches) == 0 {
		m.state.SetPendingIntent(IntentHistory, "history", h.status(m.state.Interaction))
		return
	}
	h.selectedIndex = wrapHistorySearchIndex(h.selectedIndex+delta, len(matches))
	m.state.SetPendingIntent(IntentHistory, "history", h.status(m.state.Interaction))
}

func (h *historySearchModal) updateFilter(m *Model, filter string) {
	h.filter = filter
	h.selectedIndex = 0
	m.state.SetPendingIntent(IntentHistory, "history", h.status(m.state.Interaction))
}

func (h *historySearchModal) status(interaction InteractionState) string {
	matches := filterHistorySearchEntries(interaction.History, h.filter)
	if len(interaction.History) == 0 {
		return "History search opened; history is empty."
	}
	if len(matches) == 0 {
		return fmt.Sprintf("History search for %q returned no matches.", h.filter)
	}
	idx := wrapHistorySearchIndex(h.selectedIndex, len(matches))
	return fmt.Sprintf("History search matched %d entries; selected %q.", len(matches), historySearchDisplaySQL(matches[idx].Statement))
}

// --- pure helpers (unchanged) ---

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

		score, ok := fuzzyHistoryMatch(trimmed, entry.Statement)
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

func fuzzyHistoryMatch(query, candidate string) (int, bool) {
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

		score += 1 + streak*streak
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

func defaultHistorySearchQuery(value string) string {
	if strings.TrimSpace(value) == "" {
		return "(empty)"
	}
	return value
}

func historySearchDisplaySQL(sql string) string {
	return strings.Join(strings.Fields(sql), " ")
}
