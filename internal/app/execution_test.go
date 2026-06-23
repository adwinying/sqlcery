package app

import (
	"context"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// captureExecute returns an execute func that records the context and start
// time it is handed, so tests can assert on the timeout wiring.
func captureExecute(ctx *context.Context, startedAt *time.Time) func(context.Context, time.Time) tea.Cmd {
	return func(c context.Context, s time.Time) tea.Cmd {
		*ctx = c
		*startedAt = s
		return func() tea.Msg { return nil }
	}
}

func TestExecutionCoordinatorBeginReturnsRunningAndWiresTimeout(t *testing.T) {
	var ec executionCoordinator
	var gotCtx context.Context
	var gotStart time.Time

	running, cmd := ec.begin("SQL", captureExecute(&gotCtx, &gotStart))

	if running == nil {
		t.Fatal("begin() running = nil, want context")
	}
	if got, want := running.Label, "SQL"; got != want {
		t.Fatalf("running.Label = %q, want %q", got, want)
	}
	if running.StartedAt.IsZero() {
		t.Fatal("running.StartedAt is zero, want a start time")
	}
	if running.SpinnerFrame != 0 || running.Elapsed != 0 {
		t.Fatalf("running = %+v, want zero SpinnerFrame and Elapsed", running)
	}
	if cmd == nil {
		t.Fatal("begin() cmd = nil, want batched execute + tick")
	}
	if !gotStart.Equal(running.StartedAt) {
		t.Fatalf("execute startedAt = %v, want %v", gotStart, running.StartedAt)
	}
	if gotCtx == nil {
		t.Fatal("execute received nil context")
	}
	if gotCtx.Err() != nil {
		t.Fatalf("execute context already done: %v", gotCtx.Err())
	}
	deadline, ok := gotCtx.Deadline()
	if !ok {
		t.Fatal("execute context has no deadline, want timeout")
	}
	if remaining := time.Until(deadline); remaining <= 0 || remaining > defaultInteractiveExecutionTimeout {
		t.Fatalf("context deadline remaining = %v, want within (0, %v]", remaining, defaultInteractiveExecutionTimeout)
	}
}

func TestExecutionCoordinatorBeginCancelsPrevious(t *testing.T) {
	var ec executionCoordinator
	var firstCtx context.Context
	var ignored time.Time

	ec.begin("first", captureExecute(&firstCtx, &ignored))
	if firstCtx.Err() != nil {
		t.Fatalf("first context done before second begin: %v", firstCtx.Err())
	}

	ec.begin("second", captureExecute(new(context.Context), &ignored))

	if firstCtx.Err() == nil {
		t.Fatal("first context still live after second begin, want cancelled")
	}
}

func TestExecutionCoordinatorTickAdvances(t *testing.T) {
	var ec executionCoordinator
	startedAt := time.Now()
	running := &RunningStatementContext{Label: "SQL", StartedAt: startedAt}

	updated, cmd := ec.tick(running, runningTickMsg{StartedAt: startedAt, Now: startedAt.Add(1500 * time.Millisecond)})

	if updated == nil {
		t.Fatal("tick() updated = nil, want advanced context")
	}
	if got, want := updated.Elapsed, 1500*time.Millisecond; got != want {
		t.Fatalf("updated.Elapsed = %v, want %v", got, want)
	}
	if got, want := updated.SpinnerFrame, 1; got != want {
		t.Fatalf("updated.SpinnerFrame = %d, want %d", got, want)
	}
	if cmd == nil {
		t.Fatal("tick() cmd = nil, want follow-up heartbeat")
	}
	if running.SpinnerFrame != 0 || running.Elapsed != 0 {
		t.Fatalf("tick() mutated input %+v, want it left unchanged", running)
	}
}

func TestExecutionCoordinatorTickWrapsSpinnerFrame(t *testing.T) {
	var ec executionCoordinator
	startedAt := time.Now()
	lastFrame := len(runningSpinnerFrames) - 1
	running := &RunningStatementContext{StartedAt: startedAt, SpinnerFrame: lastFrame}

	updated, _ := ec.tick(running, runningTickMsg{StartedAt: startedAt, Now: startedAt})

	if got, want := updated.SpinnerFrame, 0; got != want {
		t.Fatalf("updated.SpinnerFrame = %d, want %d (wrapped)", got, want)
	}
}

func TestExecutionCoordinatorTickStaleIsNoOp(t *testing.T) {
	var ec executionCoordinator
	startedAt := time.Now()
	running := &RunningStatementContext{StartedAt: startedAt, SpinnerFrame: 2}

	updated, cmd := ec.tick(running, runningTickMsg{StartedAt: startedAt.Add(time.Second), Now: startedAt.Add(time.Second)})

	if updated != running {
		t.Fatalf("tick() updated = %+v, want the unchanged input pointer", updated)
	}
	if cmd != nil {
		t.Fatal("tick() cmd != nil for stale tick, want nil to stop the heartbeat")
	}
}

func TestExecutionCoordinatorTickNilRunning(t *testing.T) {
	var ec executionCoordinator

	updated, cmd := ec.tick(nil, runningTickMsg{StartedAt: time.Now(), Now: time.Now()})

	if updated != nil {
		t.Fatalf("tick(nil) updated = %+v, want nil", updated)
	}
	if cmd != nil {
		t.Fatal("tick(nil) cmd != nil, want nil")
	}
}

func TestExecutionCoordinatorCancelKeepsCancellingSafe(t *testing.T) {
	var ec executionCoordinator
	var ctx context.Context
	var ignored time.Time
	ec.begin("SQL", captureExecute(&ctx, &ignored))

	ec.cancel()

	if ctx.Err() == nil {
		t.Fatal("context still live after cancel(), want cancelled")
	}
	// A second cancel with nothing in flight must not panic.
	ec.cancel()
}

func TestExecutionCoordinatorCompleteCancelsAndClears(t *testing.T) {
	var ec executionCoordinator
	var ctx context.Context
	var ignored time.Time
	ec.begin("SQL", captureExecute(&ctx, &ignored))

	cleared := ec.complete()

	if cleared != nil {
		t.Fatalf("complete() = %+v, want nil running", cleared)
	}
	if ctx.Err() == nil {
		t.Fatal("context still live after complete(), want cancelled")
	}
	// complete() with nothing in flight is safe and still reports cleared.
	if got := ec.complete(); got != nil {
		t.Fatalf("complete() with nothing in flight = %+v, want nil", got)
	}
}
