package app

import (
	"context"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

func newRunningStatementContext(label string, startedAt time.Time) *RunningStatementContext {
	if startedAt.IsZero() {
		startedAt = time.Now()
	}

	return &RunningStatementContext{
		Label:     strings.TrimSpace(label),
		StartedAt: startedAt,
	}
}

// runningTickMsg is the heartbeat that advances the Running Statement's spinner
// and elapsed time. StartedAt identifies the execution the tick belongs to, so
// stale ticks left over from a previous execution can be ignored.
type runningTickMsg struct {
	StartedAt time.Time
	Now       time.Time
}

// executionCoordinator owns the lifecycle of the Running Statement: the timeout
// context, the spinner/elapsed heartbeat, and cancellation. It produces
// RunningStatementContext values for Model to store on the Interaction State.
//
// It deliberately stays out of two neighbouring concerns: rendering (the
// running indicator's glyphs and elapsed formatting live in
// running_indicator.go) and Notifications (SetReady / SetPendingIntent and the
// Status Bar remain Model's responsibility, since the Notification slot also
// serves non-execution feedback).
type executionCoordinator struct {
	cancelFn context.CancelFunc
}

// begin starts a new in-flight execution. It cancels any previous one, opens a
// timeout context, and returns the initial Running Statement together with the
// batched execute + heartbeat command.
func (ec *executionCoordinator) begin(label string, execute func(context.Context, time.Time) tea.Cmd) (*RunningStatementContext, tea.Cmd) {
	if ec.cancelFn != nil {
		ec.cancelFn()
	}
	startedAt := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), defaultInteractiveExecutionTimeout)
	ec.cancelFn = cancel
	return newRunningStatementContext(label, startedAt), tea.Batch(execute(ctx, startedAt), runningTickCmd(startedAt))
}

// tick advances the Running Statement for a heartbeat message. It returns the
// advanced context and the follow-up heartbeat command. A stale tick — one
// whose StartedAt no longer matches the in-flight statement, or that arrives
// when nothing is running — leaves the context unchanged and returns a nil
// command, stopping that heartbeat chain.
func (ec *executionCoordinator) tick(running *RunningStatementContext, msg runningTickMsg) (*RunningStatementContext, tea.Cmd) {
	if running == nil || !running.StartedAt.Equal(msg.StartedAt) {
		return running, nil
	}
	updated := *running
	if msg.Now.After(updated.StartedAt) {
		updated.Elapsed = msg.Now.Sub(updated.StartedAt)
	}
	updated.SpinnerFrame = clampRunningSpinnerFrame(updated.SpinnerFrame + 1)
	return &updated, runningTickCmd(updated.StartedAt)
}

// cancel cancels the in-flight context without clearing the Running Statement,
// so the indicator stays visible until the cancelled statement reports back.
// Used for the esc key during execution.
func (ec *executionCoordinator) cancel() {
	if ec.cancelFn != nil {
		ec.cancelFn()
		ec.cancelFn = nil
	}
}

// complete ends the in-flight execution: it cancels the context and returns the
// cleared (nil) Running Statement for Model to store on the Interaction State.
func (ec *executionCoordinator) complete() *RunningStatementContext {
	ec.cancel()
	return nil
}

func runningTickCmd(startedAt time.Time) tea.Cmd {
	if startedAt.IsZero() {
		return nil
	}

	return tea.Tick(100*time.Millisecond, func(now time.Time) tea.Msg {
		return runningTickMsg{StartedAt: startedAt, Now: now}
	})
}
