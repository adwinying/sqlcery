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

func (h *historySearchModal) HandleKey(msg tea.KeyPressMsg, ctx ModalContext) ModalResult {
	keys := defaultCommandModeKeys()

	switch {
	case msg.String() == "ctrl+c":
		return modalResultPendingStatus{intent: IntentNone, action: "history", status: "Exited history search.", dismiss: true}
	case key.Matches(msg, keys.Help):
		return modalResultForward{cmd: func() tea.Msg { return toggleHelpIntentMsg{} }}
	case key.Matches(msg, keys.RestoreHistory):
		return h.restore(ctx)
	case key.Matches(msg, keys.Cancel):
		return modalResultPendingStatus{intent: IntentNone, action: "history", status: "Exited history search.", dismiss: true}
	case key.Matches(msg, keys.History), key.Matches(msg, keys.NextSuggestion), msg.String() == "up":
		return h.cycle(ctx, 1)
	case key.Matches(msg, keys.PrevSuggestion), msg.String() == "down":
		return h.cycle(ctx, -1)
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

func (h *historySearchModal) restore(ctx ModalContext) ModalResult {
	matches := filterHistorySearchEntries(ctx.Interaction.History, h.filter)
	if len(matches) == 0 {
		return modalResultPendingStatus{intent: IntentHistory, action: "history", status: h.status(ctx.Interaction)}
	}
	idx := wrapHistorySearchIndex(h.selectedIndex, len(matches))
	return modalResultRestoreHistory{
		sql:    matches[idx].Statement,
		status: "Restored selected history entry into the editor.",
	}
}

func (h *historySearchModal) cycle(ctx ModalContext, delta int) ModalResult {
	matches := filterHistorySearchEntries(ctx.Interaction.History, h.filter)
	if len(matches) == 0 {
		return modalResultPendingStatus{intent: IntentHistory, action: "history", status: h.status(ctx.Interaction)}
	}
	h.selectedIndex = wrapHistorySearchIndex(h.selectedIndex+delta, len(matches))
	return modalResultPendingStatus{intent: IntentHistory, action: "history", status: h.status(ctx.Interaction)}
}

func (h *historySearchModal) updateFilter(ctx ModalContext, filter string) ModalResult {
	h.filter = filter
	h.selectedIndex = 0
	return modalResultPendingStatus{intent: IntentHistory, action: "history", status: h.status(ctx.Interaction)}
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
