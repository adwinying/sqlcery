package app

import (
	tea "charm.land/bubbletea/v2"
)

func (m *Model) handleResultsPaneExportWizardKey(msg tea.KeyPressMsg) bool {
	if msg.String() != "ctrl+e" {
		return false
	}
	if m.state.Interaction.ActivePane != PaneResults {
		return false
	}

	latest := m.state.Interaction.LatestResult
	if latest == nil || latest.PreservedResult == nil || len(latest.PreservedResult.Rows) == 0 {
		m.state.SetPendingIntent(IntentNone, "export", "Results Pane has no rows to export.", NotificationInfo)
		return true
	}

	m.pushModal(&exportWizardModal{cwd: m.session.WorkingDir})
	return true
}

func selectedRowsForExport(latest *LatestResultContext, markedRows []int) []int {
	if latest == nil || latest.PreservedResult == nil {
		return nil
	}
	if len(markedRows) == 0 {
		return nil
	}
	rows := make([]int, 0, len(markedRows))
	for _, row := range markedRows {
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
