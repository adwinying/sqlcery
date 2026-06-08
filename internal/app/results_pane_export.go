package app

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/adwinying/sqlcery/internal/export"
	tea "charm.land/bubbletea/v2"
)

func (m *Model) handleResultsPaneWriteKey(msg tea.KeyPressMsg) bool {
	if m.state.Interaction.ActiveMode != ModeResultsPane {
		m.resultsPane.pendingAction = resultsPanePendingActionNone
		m.resultsPane.writeBuffer = ""
		return false
	}

	if m.resultsPane.pendingAction == resultsPanePendingActionWrite {
		return m.updateResultsPaneWritePrompt(msg)
	}

	if msg.String() == ":" {
		m.resultsPane.pendingAction = resultsPanePendingActionWrite
		m.resultsPane.writeBuffer = ":"
		m.state.SetPendingIntent(IntentNone, "viewer-export", "Type :w [filename] to export selected rows or the current result rows.")
		return true
	}

	return false
}

func (m *Model) updateResultsPaneWritePrompt(msg tea.KeyPressMsg) bool {
	switch msg.String() {
	case "enter":
		command := strings.TrimSpace(m.resultsPane.writeBuffer)
		m.resultsPane.pendingAction = resultsPanePendingActionNone
		m.resultsPane.writeBuffer = ""
		return m.writeResultsPaneExport(command)
	case "backspace", "delete":
		if len(m.resultsPane.writeBuffer) > 0 {
			runes := []rune(m.resultsPane.writeBuffer)
			m.resultsPane.writeBuffer = string(runes[:len(runes)-1])
		}
		return true
	default:
		if len(msg.Text) > 0 {
			if msg.Mod.Contains(tea.ModAlt) {
				return false
			}
			m.resultsPane.writeBuffer += msg.Text
		}
		return true
	}
}

func (m *Model) writeResultsPaneExport(command string) bool {
	filename, ok := parseResultsPaneWriteCommand(command)
	if !ok {
		m.state.SetPendingIntent(IntentNone, "viewer-export", "Use :w [filename] with .csv, .tsv, .json, or .md while record viewer is focused.")
		return true
	}

	latest := m.state.Interaction.LatestResult
	if latest == nil || latest.PreservedResult == nil {
		m.state.SetPendingIntent(IntentNone, "viewer-export", "Results Pane has no rows to export.")
		return true
	}
	if len(latest.PreservedResult.Rows) == 0 {
		m.state.SetPendingIntent(IntentNone, "viewer-export", "Results Pane has no rows to export.")
		return true
	}

	rowIndices := selectedRowsForExport(latest)
	usedSelectedRows := len(latest.SelectedRows) > 0
	written, err := export.Write(export.WriteOptions{
		CWD:        m.session.WorkingDir,
		Filename:   filename,
		Result:     latest.PreservedResult,
		RowIndices: rowIndices,
	})
	if err != nil {
		m.state.SetPendingIntent(IntentNone, "viewer-export", fmt.Sprintf("Could not export rows: %v", err))
		return true
	}

	path := written.Path
	if rel, err := filepath.Rel(m.session.WorkingDir, written.Path); err == nil && rel != "" && rel != "." && !strings.HasPrefix(rel, "..") {
		path = rel
	}

	scope := "current result rows"
	if usedSelectedRows {
		scope = "selected rows"
	}
	m.state.SetPendingIntent(IntentNone, "viewer-export", fmt.Sprintf("Exported %d row(s) as %s from %s to %s.", written.Rows, strings.ToLower(string(written.Format)), scope, path))
	return true
}

func parseResultsPaneWriteCommand(input string) (string, bool) {
	trimmed := strings.TrimSpace(input)
	if !strings.HasPrefix(trimmed, ":") {
		return "", false
	}
	fields := strings.Fields(trimmed)
	if len(fields) != 2 || fields[0] != ":w" {
		return "", false
	}
	if strings.TrimSpace(fields[1]) == "" {
		return "", false
	}
	return fields[1], true
}

func selectedRowsForExport(latest *LatestResultContext) []int {
	if latest == nil || latest.PreservedResult == nil {
		return nil
	}
	if len(latest.SelectedRows) == 0 {
		return nil
	}
	rows := make([]int, 0, len(latest.SelectedRows))
	for _, row := range latest.SelectedRows {
		if row < 0 || row >= len(latest.PreservedResult.Rows) {
			continue
		}
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		return nil
	}
	return rows
}
