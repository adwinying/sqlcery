package app

import "time"

// doubleClickWindow is the maximum interval between two clicks on the same row
// for them to be considered a double-click (standard desktop value).
const doubleClickWindow = 500 * time.Millisecond

// isDoubleClick returns true when newRow == prevRow and the time between the
// two clicks is within doubleClickWindow. A zero prevTime (no prior click)
// always returns false.
func isDoubleClick(prevRow int, prevTime time.Time, newRow int, newTime time.Time) bool {
	if prevTime.IsZero() {
		return false
	}
	if newRow != prevRow {
		return false
	}
	return newTime.Sub(prevTime) <= doubleClickWindow
}
