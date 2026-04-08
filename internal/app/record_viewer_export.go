package app

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/adwinying/sqlcery/internal/export"
	tea "github.com/charmbracelet/bubbletea"
)

func (m *Model) handleRecordViewerWriteKey(msg tea.KeyMsg) bool {
	if m.state.Query.ActiveMode != ModeRecordViewer {
		m.viewer.pendingAction = recordViewerPendingActionNone
		m.viewer.writeBuffer = ""
		return false
	}

	if m.viewer.pendingAction == recordViewerPendingActionWrite {
		return m.updateRecordViewerWritePrompt(msg)
	}

	if msg.Type == tea.KeyRunes && !msg.Alt && string(msg.Runes) == ":" {
		m.viewer.pendingAction = recordViewerPendingActionWrite
		m.viewer.writeBuffer = ":"
		m.state.SetPendingIntent(IntentNone, "viewer-export", "Type :w [filename] to export selected rows or the current result rows.")
		return true
	}

	return false
}

func (m *Model) updateRecordViewerWritePrompt(msg tea.KeyMsg) bool {
	switch msg.Type {
	case tea.KeyEsc:
		m.viewer.pendingAction = recordViewerPendingActionNone
		m.viewer.writeBuffer = ""
		m.state.SetPendingIntent(IntentNone, "viewer-export", "Cancelled export.")
		return true
	case tea.KeyEnter:
		command := strings.TrimSpace(m.viewer.writeBuffer)
		m.viewer.pendingAction = recordViewerPendingActionNone
		m.viewer.writeBuffer = ""
		return m.writeRecordViewerExport(command)
	case tea.KeyBackspace, tea.KeyDelete:
		if len(m.viewer.writeBuffer) > 0 {
			runes := []rune(m.viewer.writeBuffer)
			m.viewer.writeBuffer = string(runes[:len(runes)-1])
		}
		return true
	case tea.KeyRunes:
		if msg.Alt {
			return false
		}
		m.viewer.writeBuffer += string(msg.Runes)
		return true
	default:
		return true
	}
}

func (m *Model) writeRecordViewerExport(command string) bool {
	filename, ok := parseRecordViewerWriteCommand(command)
	if !ok {
		m.state.SetPendingIntent(IntentNone, "viewer-export", "Use :w [filename] with .csv, .tsv, .json, or .md while record viewer is focused.")
		return true
	}

	latest := m.state.Query.LatestResult
	if latest == nil || latest.PreservedResult == nil {
		m.state.SetPendingIntent(IntentNone, "viewer-export", "Record viewer has no rows to export.")
		return true
	}
	if len(latest.PreservedResult.Rows) == 0 {
		m.state.SetPendingIntent(IntentNone, "viewer-export", "Record viewer has no rows to export.")
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

func parseRecordViewerWriteCommand(input string) (string, bool) {
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
