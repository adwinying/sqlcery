package app

import (
	"fmt"
	"sort"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

const historySearchPreviewRows = popupBoxFixedRows - 4 // 4 = title + query + match-count + hint

type historySearchMatch struct {
	Entry HistoryEntryContext
	Score int
}

func (m *Model) openHistorySearch() {
	if !layoutShowsCommand(m.state.Interaction.Layout) {
		m.state.SetLayout(LayoutSplit)
	}
	m.state.SetActiveMode(ModeHistorySearch)
	m.state.SetHistorySearchContext(&HistorySearchContext{})
	m.syncHistorySearchSelection()
	m.state.SetPendingIntent(IntentHistory, "history", historySearchStatus(m.state.Interaction))
}

func (m *Model) closeHistorySearch() {
	if m.state.Interaction.ActiveMode != ModeHistorySearch && m.state.Interaction.HistorySearch == nil && m.state.Interaction.SelectedHistoryEntry == nil {
		return
	}
	m.state.SetActiveMode(ModeCommand)
	m.state.SetHistorySearchContext(nil)
	m.state.SetSelectedHistoryEntry(nil)
	m.state.SetPendingIntent(IntentNone, "history", "Exited history search.")
}

func (m *Model) restoreSelectedHistoryEntry() {
	selected := m.state.Interaction.SelectedHistoryEntry
	if selected == nil {
		m.state.SetPendingIntent(IntentHistory, "history", historySearchStatus(m.state.Interaction))
		return
	}

	m.command.SetEditorValue(selected.SQL)
	m.syncCurrentSQL()
	m.closeHistorySearch()
	m.state.SetPendingIntent(IntentNone, "history", "Restored selected history entry into the editor.")
}

func (m *Model) cycleHistorySearch(delta int) {
	search := m.state.Interaction.HistorySearch
	if search == nil {
		m.openHistorySearch()
		return
	}

	matches := filterHistorySearchEntries(m.state.Interaction.SessionHistory, search.Query)
	if len(matches) == 0 {
		m.syncHistorySearchSelection()
		m.state.SetPendingIntent(IntentHistory, "history", historySearchStatus(m.state.Interaction))
		return
	}

	search.SelectedIndex = wrapHistorySearchIndex(search.SelectedIndex+delta, len(matches))
	m.state.SetHistorySearchContext(search)
	m.syncHistorySearchSelection()
	m.state.SetPendingIntent(IntentHistory, "history", historySearchStatus(m.state.Interaction))
}

func (m *Model) updateHistorySearchQuery(query string) {
	search := m.state.Interaction.HistorySearch
	if search == nil {
		search = &HistorySearchContext{}
	}

	search.Query = query
	search.SelectedIndex = 0
	m.state.SetHistorySearchContext(search)
	m.syncHistorySearchSelection()
	m.state.SetPendingIntent(IntentHistory, "history", historySearchStatus(m.state.Interaction))
}

func (m *Model) syncHistorySearchSelection() {
	search := m.state.Interaction.HistorySearch
	if search == nil {
		m.state.SetSelectedHistoryEntry(nil)
		return
	}

	matches := filterHistorySearchEntries(m.state.Interaction.SessionHistory, search.Query)
	if len(matches) == 0 {
		search.SelectedIndex = 0
		m.state.SetHistorySearchContext(search)
		m.state.SetSelectedHistoryEntry(nil)
		return
	}

	search.SelectedIndex = wrapHistorySearchIndex(search.SelectedIndex, len(matches))
	m.state.SetHistorySearchContext(search)
	m.state.SetSelectedHistoryEntry(&matches[search.SelectedIndex])
}

func (m *Model) handleHistorySearchKey(msg tea.KeyPressMsg) tea.Cmd {
	keys := m.command.KeyMap()

	switch {
	case msg.String() == "ctrl+c":
		m.closeHistorySearch()
		return nil
	case key.Matches(msg, keys.Help):
		return func() tea.Msg { return toggleHelpIntentMsg{} }
	case key.Matches(msg, keys.RestoreHistory):
		m.restoreSelectedHistoryEntry()
		return nil
	case key.Matches(msg, keys.Cancel):
		m.closeHistorySearch()
		return nil
	case key.Matches(msg, keys.History), key.Matches(msg, keys.NextSuggestion), msg.String() == "up":
		m.cycleHistorySearch(1)
		return nil
	case key.Matches(msg, keys.PrevSuggestion), msg.String() == "down":
		m.cycleHistorySearch(-1)
		return nil
	case msg.String() == "backspace" || msg.String() == "ctrl+h" || msg.String() == "delete":
		search := m.state.Interaction.HistorySearch
		if search == nil {
			m.openHistorySearch()
			return nil
		}
		m.updateHistorySearchQuery(trimLastRune(search.Query))
		return nil
	case msg.String() == "space":
		search := m.state.Interaction.HistorySearch
		if search == nil {
			m.openHistorySearch()
			search = m.state.Interaction.HistorySearch
		}
		m.updateHistorySearchQuery(search.Query + " ")
		return nil
	case len(msg.Text) > 0 && !msg.Mod.Contains(tea.ModAlt):
		search := m.state.Interaction.HistorySearch
		if search == nil {
			m.openHistorySearch()
			search = m.state.Interaction.HistorySearch
		}
		m.updateHistorySearchQuery(search.Query + msg.Text)
		return nil
	default:
		return nil
	}
}

func renderHistorySearch(interaction InteractionState) string {
	if interaction.ActiveMode != ModeHistorySearch || interaction.HistorySearch == nil {
		return ""
	}

	search := interaction.HistorySearch
	matches := filterHistorySearchEntries(interaction.SessionHistory, search.Query)
	lines := []string{
		appTheme.panelTitle.Render("Reverse search:"),
		appTheme.panelText.Render(fmt.Sprintf("query> %s", defaultHistorySearchQuery(search.Query))),
	}

	if len(interaction.SessionHistory) == 0 {
		lines = append(lines, appTheme.panelMuted.Render("No session history yet."), appTheme.panelHint.Render("esc close"))
		return strings.Join(lines, "\n")
	}

	if len(matches) == 0 {
		lines = append(lines, appTheme.panelMuted.Render("No fuzzy matches."), appTheme.panelHint.Render("ctrl+r keep searching | esc close"))
		return strings.Join(lines, "\n")
	}

	selected := wrapHistorySearchIndex(search.SelectedIndex, len(matches))
	lines = append(lines, appTheme.panelMuted.Render(fmt.Sprintf("%d match(es); newest first.", len(matches))))

	// Compute viewport scroll offset so the selected item is always visible.
	scrollOffset := max(0, selected-historySearchPreviewRows+1)
	viewEnd := min(len(matches), scrollOffset+historySearchPreviewRows)
	for i := scrollOffset; i < viewEnd; i++ {
		display := historySearchDisplaySQL(matches[i].SQL)
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

func historySearchStatus(interaction InteractionState) string {
	search := interaction.HistorySearch
	if search == nil {
		return "History search is unavailable."
	}

	matches := filterHistorySearchEntries(interaction.SessionHistory, search.Query)
	if len(interaction.SessionHistory) == 0 {
		return "History search opened; session history is empty."
	}
	if len(matches) == 0 {
		return fmt.Sprintf("History search for %q returned no matches.", search.Query)
	}

	selected := matches[wrapHistorySearchIndex(search.SelectedIndex, len(matches))]
	return fmt.Sprintf("History search matched %d entries; selected %q.", len(matches), historySearchDisplaySQL(selected.SQL))
}

func filterHistorySearchEntries(entries []HistoryEntryContext, query string) []HistoryEntryContext {
	matches := rankHistorySearchEntries(entries, query)
	filtered := make([]HistoryEntryContext, 0, len(matches))
	for _, match := range matches {
		filtered = append(filtered, match.Entry)
	}
	return filtered
}

func rankHistorySearchEntries(entries []HistoryEntryContext, query string) []historySearchMatch {
	trimmed := strings.TrimSpace(query)
	matches := make([]historySearchMatch, 0, len(entries))
	seen := make(map[string]struct{}, len(entries))
	for i := len(entries) - 1; i >= 0; i-- {
		entry := entries[i]
		// Collapse whitespace so entries that render identically in the popup
		// dedupe to a single row; the original SQL is preserved on the entry
		// and restored verbatim when the user picks it.
		key := historySearchDisplaySQL(entry.SQL)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}

		if trimmed == "" {
			matches = append(matches, historySearchMatch{Entry: entry})
			continue
		}

		score, ok := fuzzyHistoryMatch(trimmed, entry.SQL)
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

// historySearchDisplaySQL collapses runs of whitespace (including newlines) in
// sql into single spaces so that every history entry renders as exactly one
// visual row in the popup list. The original SQL is preserved separately and
// restored into the editor unchanged when the entry is selected.
func historySearchDisplaySQL(sql string) string {
	return strings.Join(strings.Fields(sql), " ")
}
