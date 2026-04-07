package app

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

const historySearchPreviewRows = 4

type historySearchMatch struct {
	Entry HistoryEntryContext
	Score int
}

func (m *Model) openHistorySearch() {
	m.state.SetActiveMode(ModeHistorySearch)
	m.state.SetHistorySearchContext(&HistorySearchContext{})
	m.syncHistorySearchSelection()
	m.state.SetPendingIntent(IntentHistory, "history", historySearchStatus(m.state.Query))
}

func (m *Model) closeHistorySearch() {
	m.state.SetActiveMode(ModeCommand)
	m.state.SetHistorySearchContext(nil)
	m.state.SetSelectedHistoryEntry(nil)
	m.state.SetPendingIntent(IntentNone, "history", "Exited history search.")
}

func (m *Model) restoreSelectedHistoryEntry() {
	selected := m.state.Query.SelectedHistoryEntry
	if selected == nil {
		m.state.SetPendingIntent(IntentHistory, "history", historySearchStatus(m.state.Query))
		return
	}

	m.command.editor.SetValue(selected.SQL)
	m.command.editor.CursorEnd()
	m.command.syncScroll()
	m.command.selectedSuggestion = 0
	m.syncCurrentSQL()
	m.closeHistorySearch()
	m.state.SetPendingIntent(IntentNone, "history", "Restored selected history entry into the editor.")
}

func (m *Model) cycleHistorySearch(delta int) {
	search := m.state.Query.HistorySearch
	if search == nil {
		m.openHistorySearch()
		return
	}

	matches := filterHistorySearchEntries(m.state.Query.SessionHistory, search.Query)
	if len(matches) == 0 {
		m.syncHistorySearchSelection()
		m.state.SetPendingIntent(IntentHistory, "history", historySearchStatus(m.state.Query))
		return
	}

	search.SelectedIndex = wrapHistorySearchIndex(search.SelectedIndex+delta, len(matches))
	m.state.SetHistorySearchContext(search)
	m.syncHistorySearchSelection()
	m.state.SetPendingIntent(IntentHistory, "history", historySearchStatus(m.state.Query))
}

func (m *Model) updateHistorySearchQuery(query string) {
	search := m.state.Query.HistorySearch
	if search == nil {
		search = &HistorySearchContext{}
	}

	search.Query = query
	search.SelectedIndex = 0
	m.state.SetHistorySearchContext(search)
	m.syncHistorySearchSelection()
	m.state.SetPendingIntent(IntentHistory, "history", historySearchStatus(m.state.Query))
}

func (m *Model) syncHistorySearchSelection() {
	search := m.state.Query.HistorySearch
	if search == nil {
		m.state.SetSelectedHistoryEntry(nil)
		return
	}

	matches := filterHistorySearchEntries(m.state.Query.SessionHistory, search.Query)
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

func (m *Model) handleHistorySearchKey(msg tea.KeyMsg) tea.Cmd {
	keys := m.command.KeyMap()

	switch {
	case msg.String() == "ctrl+c":
		return tea.Quit
	case key.Matches(msg, keys.RestoreHistory):
		m.restoreSelectedHistoryEntry()
		return nil
	case key.Matches(msg, keys.Cancel):
		m.closeHistorySearch()
		return nil
	case key.Matches(msg, keys.History), key.Matches(msg, keys.NextSuggestion), msg.Type == tea.KeyUp:
		m.cycleHistorySearch(1)
		return nil
	case key.Matches(msg, keys.PrevSuggestion), msg.Type == tea.KeyDown:
		m.cycleHistorySearch(-1)
		return nil
	case msg.Type == tea.KeyBackspace || msg.Type == tea.KeyCtrlH || msg.Type == tea.KeyDelete:
		search := m.state.Query.HistorySearch
		if search == nil {
			m.openHistorySearch()
			return nil
		}
		m.updateHistorySearchQuery(trimLastRune(search.Query))
		return nil
	case msg.Type == tea.KeySpace:
		search := m.state.Query.HistorySearch
		if search == nil {
			m.openHistorySearch()
			search = m.state.Query.HistorySearch
		}
		m.updateHistorySearchQuery(search.Query + " ")
		return nil
	case msg.Type == tea.KeyRunes && !msg.Alt:
		search := m.state.Query.HistorySearch
		if search == nil {
			m.openHistorySearch()
			search = m.state.Query.HistorySearch
		}
		m.updateHistorySearchQuery(search.Query + string(msg.Runes))
		return nil
	default:
		return nil
	}
}

func renderHistorySearch(query QueryContext) string {
	if query.ActiveMode != ModeHistorySearch || query.HistorySearch == nil {
		return ""
	}

	search := query.HistorySearch
	matches := filterHistorySearchEntries(query.SessionHistory, search.Query)
	lines := []string{"Reverse search:", fmt.Sprintf("query> %s", defaultHistorySearchQuery(search.Query))}

	if len(query.SessionHistory) == 0 {
		lines = append(lines, "No session history yet.", "esc close")
		return strings.Join(lines, "\n")
	}

	if len(matches) == 0 {
		lines = append(lines, "No fuzzy matches.", "ctrl+r keep searching | esc close")
		return strings.Join(lines, "\n")
	}

	selected := wrapHistorySearchIndex(search.SelectedIndex, len(matches))
	lines = append(lines, fmt.Sprintf("%d match(es); newest first.", len(matches)))
	for i := 0; i < min(len(matches), historySearchPreviewRows); i++ {
		prefix := "  "
		if i == selected {
			prefix = "> "
		}
		lines = append(lines, prefix+matches[i].SQL)
	}
	lines = append(lines, "enter restore | ctrl+r older | alt+p newer | esc close")

	return strings.Join(lines, "\n")
}

func historySearchStatus(query QueryContext) string {
	search := query.HistorySearch
	if search == nil {
		return "History search is unavailable."
	}

	matches := filterHistorySearchEntries(query.SessionHistory, search.Query)
	if len(query.SessionHistory) == 0 {
		return "History search opened; session history is empty."
	}
	if len(matches) == 0 {
		return fmt.Sprintf("History search for %q returned no matches.", search.Query)
	}

	selected := matches[wrapHistorySearchIndex(search.SelectedIndex, len(matches))]
	return fmt.Sprintf("History search matched %d entries; selected %q.", len(matches), selected.SQL)
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
	for i := len(entries) - 1; i >= 0; i-- {
		entry := entries[i]
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
