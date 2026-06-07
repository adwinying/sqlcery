package app

import (
	"fmt"
	"strings"
	"time"
)

var runningSpinnerFrames = []string{"-", `\`, "|", "/"}

func newRunningStatementContext(label string, startedAt time.Time) *RunningStatementContext {
	if startedAt.IsZero() {
		startedAt = time.Now()
	}

	return &RunningStatementContext{
		Label:     strings.TrimSpace(label),
		StartedAt: startedAt,
	}
}

func formatRunningIndicator(running *RunningStatementContext) string {
	if running == nil {
		return ""
	}

	frame := runningSpinnerFrames[clampRunningSpinnerFrame(running.SpinnerFrame)]
	parts := []string{frame}
	if label := strings.TrimSpace(running.Label); label != "" {
		parts = append(parts, label)
	}
	parts = append(parts, formatRunningElapsed(running.Elapsed))
	return strings.Join(parts, " ")
}

func formatRunningElapsed(elapsed time.Duration) string {
	if elapsed < 0 {
		elapsed = 0
	}

	if elapsed < time.Minute {
		return fmt.Sprintf("%.1fs", elapsed.Seconds())
	}

	minutes := int(elapsed / time.Minute)
	seconds := elapsed - time.Duration(minutes)*time.Minute
	return fmt.Sprintf("%dm%.1fs", minutes, seconds.Seconds())
}

func clampRunningSpinnerFrame(frame int) int {
	if len(runningSpinnerFrames) == 0 || frame < 0 {
		return 0
	}

	return frame % len(runningSpinnerFrames)
}
