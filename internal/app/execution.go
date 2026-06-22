package app

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"
)

// executionCoordinator owns the lifecycle of a single in-flight execution:
// the context cancel function, the timeout, and the running-tick heartbeat.
// Model holds one as a field; state management and notifications stay in Model.
type executionCoordinator struct {
	cancelFn context.CancelFunc
}

func (ec *executionCoordinator) start(execute func(context.Context, time.Time) tea.Cmd) (time.Time, tea.Cmd) {
	if ec.cancelFn != nil {
		ec.cancelFn()
	}
	startedAt := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), defaultInteractiveExecutionTimeout)
	ec.cancelFn = cancel
	return startedAt, tea.Batch(execute(ctx, startedAt), runningTickCmd(startedAt))
}

func (ec *executionCoordinator) cancel() {
	if ec.cancelFn != nil {
		ec.cancelFn()
		ec.cancelFn = nil
	}
}
