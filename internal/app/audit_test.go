package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	appaudit "github.com/adwinying/sqlcery/internal/audit"
	"github.com/adwinying/sqlcery/internal/config"
)

type fakeAudit struct {
	appendSubmitted func(appaudit.SubmittedEvent) error
	appendCompleted func(appaudit.CompletedEvent) error
}

type failFirstCompletionFileAudit struct {
	log             *appaudit.FileLog
	completionCalls int
}

func (a *failFirstCompletionFileAudit) AppendSubmitted(event appaudit.SubmittedEvent) error {
	return a.log.AppendSubmitted(event)
}

func (a *failFirstCompletionFileAudit) AppendCompleted(event appaudit.CompletedEvent) error {
	a.completionCalls++
	if a.completionCalls == 1 {
		return errors.New("injected completion failure")
	}
	return a.log.AppendCompleted(event)
}

func (f fakeAudit) AppendSubmitted(event appaudit.SubmittedEvent) error {
	if f.appendSubmitted == nil {
		return nil
	}
	return f.appendSubmitted(event)
}

func (f fakeAudit) AppendCompleted(event appaudit.CompletedEvent) error {
	if f.appendCompleted == nil {
		return nil
	}
	return f.appendCompleted(event)
}

func TestAuditSubmissionFailurePreventsAdapterExecution(t *testing.T) {
	adapter := openTestAdapter(t)
	t.Cleanup(func() { _ = adapter.Close() })
	if _, err := adapter.ExecContext(context.Background(), `create table widgets (id integer)`); err != nil {
		t.Fatalf("create table: %v", err)
	}

	wantErr := errors.New("disk unavailable")
	model := newModelWithDependencies(Session{
		ConnectionName: "local",
		Adapter:        adapter,
	}, modelDependencies{
		audit: fakeAudit{appendSubmitted: func(appaudit.SubmittedEvent) error { return wantErr }},
	})
	model.state.SetReady("", NotificationNone)
	statement := "insert into widgets values (1);"
	model.command.editor.SetValue(statement)
	model.syncCurrentSQL()

	next, cmd := model.Update(submitIntentMsg{})
	model = next.(Model)
	executed := firstCommandMessageForTest[statementExecutedMsg](t, cmd)
	next, _ = model.Update(executed)
	model = next.(Model)

	var count int
	if err := adapter.QueryRowContext(context.Background(), "select count(*) from widgets").Scan(&count); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if count != 0 {
		t.Fatalf("row count = %d, want 0 because Audit submission failed", count)
	}
	if got := model.command.Value(); got != statement {
		t.Fatalf("editor = %q, want Statement preserved for retry", got)
	}
	if got := model.state.Notification.Text; !strings.Contains(got, wantErr.Error()) {
		t.Fatalf("notification = %q, want Audit error", got)
	}
	if model.state.Interaction.PendingAuditCompletion != nil {
		t.Fatalf("pending completion = %#v, want submission failure to remain an editor retry", model.state.Interaction.PendingAuditCompletion)
	}
}

func TestAuditEventsDurablyWrapAdapterExecution(t *testing.T) {
	adapter := openTestAdapter(t)
	t.Cleanup(func() { _ = adapter.Close() })
	if _, err := adapter.ExecContext(context.Background(), `create table widgets (id integer)`); err != nil {
		t.Fatalf("create table: %v", err)
	}

	rowCount := func() int {
		t.Helper()
		var count int
		if err := adapter.QueryRowContext(context.Background(), "select count(*) from widgets").Scan(&count); err != nil {
			t.Fatalf("count rows: %v", err)
		}
		return count
	}
	var submitted appaudit.SubmittedEvent
	var completed appaudit.CompletedEvent
	auditLog := fakeAudit{
		appendSubmitted: func(event appaudit.SubmittedEvent) error {
			submitted = event
			if got := rowCount(); got != 0 {
				t.Fatalf("row count during submitted append = %d, want 0", got)
			}
			return nil
		},
		appendCompleted: func(event appaudit.CompletedEvent) error {
			completed = event
			if got := rowCount(); got != 1 {
				t.Fatalf("row count during completed append = %d, want 1", got)
			}
			return nil
		},
	}
	model := newModelWithDependencies(Session{
		ConnectionName:     "local",
		ConnectionIdentity: "opaque-connection",
		ConnectionOrigin:   "/project/connections.toml",
		Adapter:            adapter,
	}, modelDependencies{
		audit:                auditLog,
		newExecutionIdentity: func() string { return "opaque-execution" },
	})
	model.state.SetReady("", NotificationNone)
	model.command.editor.SetValue("insert into widgets values (1);")
	model.syncCurrentSQL()

	_, cmd := model.Update(submitIntentMsg{})
	_ = firstCommandMessageForTest[statementExecutedMsg](t, cmd)

	if submitted.ExecutionIdentity != "opaque-execution" || completed.ExecutionIdentity != submitted.ExecutionIdentity {
		t.Fatalf("execution identities = submitted %q, completed %q; want correlated opaque identity", submitted.ExecutionIdentity, completed.ExecutionIdentity)
	}
	if completed.Outcome != appaudit.OutcomeSuccess {
		t.Fatalf("completed outcome = %q, want success", completed.Outcome)
	}
}

func TestAuditCompletionFailureIsVisibleAndHistoryRemainsIndependent(t *testing.T) {
	adapter := openTestAdapter(t)
	t.Cleanup(func() { _ = adapter.Close() })
	wantErr := errors.New("completion fsync failed")
	model := newModelWithDependencies(Session{
		ConnectionName: "local",
		Adapter:        adapter,
	}, modelDependencies{
		audit: fakeAudit{appendCompleted: func(appaudit.CompletedEvent) error { return wantErr }},
	})
	model.state.SetReady("", NotificationNone)
	statement := "select 1;"
	model.command.editor.SetValue(statement)
	model.syncCurrentSQL()

	next, cmd := model.Update(submitIntentMsg{})
	model = next.(Model)
	executed := firstCommandMessageForTest[statementExecutedMsg](t, cmd)
	next, _ = model.Update(executed)
	model = next.(Model)

	if got := model.state.Notification.Text; !strings.Contains(got, wantErr.Error()) {
		t.Fatalf("notification = %q, want completion Audit error", got)
	}
	if latest, ok := model.history.Latest(); !ok || latest != statement {
		t.Fatalf("History latest = %q, %v; want independently retained Statement", latest, ok)
	}
}

func TestAuditCompletionFailureRetainsExactEventAndPersistentFeedback(t *testing.T) {
	adapter := openTestAdapter(t)
	t.Cleanup(func() { _ = adapter.Close() })
	wantErr := errors.New("completion fsync failed")
	var failedEvent appaudit.CompletedEvent
	model := newModelWithDependencies(Session{Adapter: adapter}, modelDependencies{
		audit: fakeAudit{appendCompleted: func(event appaudit.CompletedEvent) error {
			failedEvent = event
			return wantErr
		}},
		newExecutionIdentity: func() string { return "failed-completion" },
	})
	model.state.SetReady("", NotificationNone)
	model.command.editor.SetValue("select 1;")
	model.syncCurrentSQL()

	next, cmd := model.Update(submitIntentMsg{})
	model = next.(Model)
	next, clearCmd := model.Update(firstCommandMessageForTest[statementExecutedMsg](t, cmd))
	model = next.(Model)

	if model.state.Interaction.PendingAuditCompletion == nil {
		t.Fatal("pending Audit completion = nil, want failed correlated event retained")
	}
	if got := *model.state.Interaction.PendingAuditCompletion; got != failedEvent {
		t.Fatalf("pending Audit completion = %#v, want exact failed event %#v", got, failedEvent)
	}
	if got := model.statusBarView(); !strings.Contains(got, wantErr.Error()) {
		t.Fatalf("status bar = %q, want persistent Audit error", got)
	}
	for _, msg := range collectCommandMessagesForTest(t, clearCmd) {
		if clear, ok := msg.(notificationClearMsg); ok {
			next, _ = model.Update(clear)
			model = next.(Model)
		}
	}
	if got := model.statusBarView(); !strings.Contains(got, wantErr.Error()) {
		t.Fatalf("status bar after Notification clear = %q, want persistent Audit error", got)
	}
}

func TestPendingAuditCompletionRetriesBeforeNextStatementExecution(t *testing.T) {
	adapter := openTestAdapter(t)
	t.Cleanup(func() { _ = adapter.Close() })
	if _, err := adapter.ExecContext(context.Background(), `create table widgets (id integer)`); err != nil {
		t.Fatalf("create table: %v", err)
	}

	completionCalls := 0
	submittedCalls := 0
	identityCalls := 0
	var failedEvent, retriedEvent appaudit.CompletedEvent
	model := newModelWithDependencies(Session{Adapter: adapter}, modelDependencies{
		audit: fakeAudit{
			appendSubmitted: func(appaudit.SubmittedEvent) error {
				submittedCalls++
				return nil
			},
			appendCompleted: func(event appaudit.CompletedEvent) error {
				completionCalls++
				if completionCalls == 1 {
					failedEvent = event
					return errors.New("completion unavailable")
				}
				if completionCalls == 2 {
					retriedEvent = event
				}
				return nil
			},
		},
		newExecutionIdentity: func() string {
			identityCalls++
			return fmt.Sprintf("execution-%d", identityCalls)
		},
	})
	model.state.SetReady("", NotificationNone)
	model.command.editor.SetValue("insert into widgets values (1);")
	model.syncCurrentSQL()
	next, cmd := model.Update(submitIntentMsg{})
	model = next.(Model)
	next, _ = model.Update(firstCommandMessageForTest[statementExecutedMsg](t, cmd))
	model = next.(Model)

	model.command.editor.SetValue("insert into widgets values (2);")
	model.syncCurrentSQL()
	next, retryCmd := model.Update(submitIntentMsg{})
	model = next.(Model)
	if submittedCalls != 1 || identityCalls != 1 {
		t.Fatalf("before retry: submitted calls = %d, identity calls = %d; want 1, 1", submittedCalls, identityCalls)
	}
	if completionCalls != 1 {
		t.Fatalf("completion calls during Update = %d, want retry filesystem work deferred", completionCalls)
	}
	_, duplicateRetryCmd := model.Update(submitIntentMsg{})
	if duplicateRetryCmd != nil {
		t.Fatal("second Enter while retry is in flight returned a command, want one pending completion slot")
	}
	var count int
	if err := adapter.QueryRowContext(context.Background(), "select count(*) from widgets").Scan(&count); err != nil {
		t.Fatalf("count rows before retry: %v", err)
	}
	if count != 1 {
		t.Fatalf("row count before retry = %d, want no new Adapter execution", count)
	}

	retried := firstCommandMessageForTest[auditCompletionRetriedMsg](t, retryCmd)
	next, executeCmd := model.Update(retried)
	model = next.(Model)
	if retriedEvent != failedEvent {
		t.Fatalf("retried completion = %#v, want exact failed event %#v", retriedEvent, failedEvent)
	}
	if identityCalls != 2 {
		t.Fatalf("identity calls after successful retry = %d, want next execution created", identityCalls)
	}
	_ = firstCommandMessageForTest[statementExecutedMsg](t, executeCmd)
	if submittedCalls != 2 {
		t.Fatalf("submitted calls after retry = %d, want requested Statement submitted", submittedCalls)
	}
	if err := adapter.QueryRowContext(context.Background(), "select count(*) from widgets").Scan(&count); err != nil {
		t.Fatalf("count rows after retry: %v", err)
	}
	if count != 2 {
		t.Fatalf("row count after retry = %d, want requested Statement executed", count)
	}
}

func TestFailedAuditCompletionRetryKeepsSameEventAndBlocksExecution(t *testing.T) {
	adapter := openTestAdapter(t)
	t.Cleanup(func() { _ = adapter.Close() })
	completionCalls := 0
	submittedCalls := 0
	identityCalls := 0
	var failedEvent appaudit.CompletedEvent
	model := newModelWithDependencies(Session{Adapter: adapter}, modelDependencies{
		audit: fakeAudit{
			appendSubmitted: func(appaudit.SubmittedEvent) error {
				submittedCalls++
				return nil
			},
			appendCompleted: func(event appaudit.CompletedEvent) error {
				completionCalls++
				if completionCalls == 1 {
					failedEvent = event
					return errors.New("first failure")
				}
				return errors.New("retry still unavailable")
			},
		},
		newExecutionIdentity: func() string {
			identityCalls++
			return fmt.Sprintf("execution-%d", identityCalls)
		},
	})
	model.state.SetReady("", NotificationNone)
	model.command.editor.SetValue("select 1;")
	model.syncCurrentSQL()
	next, cmd := model.Update(submitIntentMsg{})
	model = next.(Model)
	next, _ = model.Update(firstCommandMessageForTest[statementExecutedMsg](t, cmd))
	model = next.(Model)

	model.command.editor.SetValue("select 2;")
	model.syncCurrentSQL()
	next, retryCmd := model.Update(submitIntentMsg{})
	model = next.(Model)
	next, executeCmd := model.Update(firstCommandMessageForTest[auditCompletionRetriedMsg](t, retryCmd))
	model = next.(Model)

	if executeCmd != nil {
		t.Fatal("failed retry returned execution command, want execution blocked")
	}
	if model.state.Interaction.PendingAuditCompletion == nil || *model.state.Interaction.PendingAuditCompletion != failedEvent {
		t.Fatalf("pending completion = %#v, want same event %#v", model.state.Interaction.PendingAuditCompletion, failedEvent)
	}
	if submittedCalls != 1 || identityCalls != 1 {
		t.Fatalf("after failed retry: submitted calls = %d, identity calls = %d; want 1, 1", submittedCalls, identityCalls)
	}
	if got := model.statusBarView(); !strings.Contains(got, "retry still unavailable") {
		t.Fatalf("status bar = %q, want updated persistent retry error", got)
	}
}

func TestPendingAuditCompletionAllowsQuitWithoutFabricatingOutcome(t *testing.T) {
	completionCalls := 0
	model := newModelWithDependencies(Session{}, modelDependencies{
		audit: fakeAudit{appendCompleted: func(appaudit.CompletedEvent) error {
			completionCalls++
			return errors.New("completion unavailable")
		}},
	})
	model.state.SetReady("", NotificationNone)
	pending := appaudit.CompletedEvent{Event: "completed", ExecutionIdentity: "unmatched", Outcome: appaudit.OutcomeSuccess}
	model.state.Interaction.PendingAuditCompletion = &pending
	model.state.Interaction.AuditFailure = "Audit completion was not persisted"

	next, cmd := model.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	model = next.(Model)
	if cmd == nil {
		t.Fatal("first ctrl+c command = nil, want normal quit confirmation timer")
	}
	next, cmd = model.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	model = next.(Model)
	if quitMsg := cmd(); quitMsg != (tea.QuitMsg{}) {
		t.Fatalf("second ctrl+c message = %T, want tea.QuitMsg", quitMsg)
	}
	if completionCalls != 0 {
		t.Fatalf("completion calls while quitting = %d, want no fabricated completion", completionCalls)
	}
	if model.state.Interaction.PendingAuditCompletion == nil || *model.state.Interaction.PendingAuditCompletion != pending {
		t.Fatalf("pending completion after quit = %#v, want unmatched event retained until process exits", model.state.Interaction.PendingAuditCompletion)
	}
}

func TestAuditRecoveryLeavesValidUnmatchedSubmissionThenWritesOneCompletion(t *testing.T) {
	adapter := openTestAdapter(t)
	t.Cleanup(func() { _ = adapter.Close() })
	path := filepath.Join(t.TempDir(), "audit.log")
	auditLog := &failFirstCompletionFileAudit{log: appaudit.NewFileLog(path)}
	model := newModelWithDependencies(Session{Adapter: adapter}, modelDependencies{
		audit:                auditLog,
		newExecutionIdentity: func() string { return "artifact-execution" },
	})
	model.state.SetReady("", NotificationNone)
	model.command.editor.SetValue("select 1;")
	model.syncCurrentSQL()

	next, cmd := model.Update(submitIntentMsg{})
	model = next.(Model)
	next, _ = model.Update(firstCommandMessageForTest[statementExecutedMsg](t, cmd))
	model = next.(Model)

	readEvents := func() []map[string]any {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile() error = %v", err)
		}
		lines := strings.Split(strings.TrimSpace(string(data)), "\n")
		events := make([]map[string]any, 0, len(lines))
		for _, line := range lines {
			var event map[string]any
			if err := json.Unmarshal([]byte(line), &event); err != nil {
				t.Fatalf("Audit line %q is invalid JSON: %v", line, err)
			}
			events = append(events, event)
		}
		return events
	}

	events := readEvents()
	if len(events) != 1 || events[0]["event"] != "submitted" {
		t.Fatalf("events after failed completion = %#v, want one valid unmatched submission", events)
	}

	next, retryCmd := model.Update(submitIntentMsg{})
	model = next.(Model)
	next, _ = model.Update(firstCommandMessageForTest[auditCompletionRetriedMsg](t, retryCmd))
	model = next.(Model)
	events = readEvents()
	if len(events) != 2 || events[1]["event"] != "completed" {
		t.Fatalf("events after retry = %#v, want one submitted and one completed event", events)
	}
	if events[0]["execution_identity"] != events[1]["execution_identity"] {
		t.Fatalf("artifact identities = %#v and %#v, want correlated events", events[0], events[1])
	}
}

func TestAuditQueryCompletionContainsCountButNoResultData(t *testing.T) {
	adapter := openTestAdapter(t)
	t.Cleanup(func() { _ = adapter.Close() })
	for _, statement := range []string{
		`create table widgets (name text)`,
		`insert into widgets values ('classified-cell-value')`,
	} {
		if _, err := adapter.ExecContext(context.Background(), statement); err != nil {
			t.Fatalf("setup %q: %v", statement, err)
		}
	}

	var completion appaudit.CompletedEvent
	model := newModelWithDependencies(Session{Adapter: adapter}, modelDependencies{
		audit: fakeAudit{appendCompleted: func(event appaudit.CompletedEvent) error {
			completion = event
			return nil
		}},
	})
	model.state.SetReady("", NotificationNone)
	model.command.editor.SetValue("select name from widgets;")
	model.syncCurrentSQL()
	_, cmd := model.Update(submitIntentMsg{})
	_ = firstCommandMessageForTest[statementExecutedMsg](t, cmd)

	if completion.ResultSummary.RowCount == nil || *completion.ResultSummary.RowCount != 1 {
		t.Fatalf("row count = %#v, want 1", completion.ResultSummary.RowCount)
	}
	data, err := json.Marshal(completion)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	for _, forbidden := range []string{"classified-cell-value", "columns", "rows"} {
		if strings.Contains(string(data), forbidden) {
			t.Fatalf("completion JSON %s contains forbidden result data %q", data, forbidden)
		}
	}
}

func TestAuditFailureErrorIsBoundedValidUTF8WithMarker(t *testing.T) {
	adapter := openTestAdapter(t)
	t.Cleanup(func() { _ = adapter.Close() })
	var completion appaudit.CompletedEvent
	model := newModelWithDependencies(Session{Adapter: adapter}, modelDependencies{
		audit: fakeAudit{appendCompleted: func(event appaudit.CompletedEvent) error {
			completion = event
			return nil
		}},
	})
	model.state.SetReady("", NotificationNone)
	model.command.editor.SetValue(`select * from "` + strings.Repeat("界", 2000) + `";`)
	model.syncCurrentSQL()
	_, cmd := model.Update(submitIntentMsg{})
	_ = firstCommandMessageForTest[statementExecutedMsg](t, cmd)

	if completion.Outcome != appaudit.OutcomeFailure {
		t.Fatalf("outcome = %q, want failure", completion.Outcome)
	}
	got := completion.ResultSummary.Error
	if len(got) > 4*1024 {
		t.Fatalf("error length = %d bytes, want at most 4096", len(got))
	}
	if !utf8.ValidString(got) {
		t.Fatal("bounded error is not valid UTF-8")
	}
	if !strings.HasSuffix(got, "... [truncated]") {
		t.Fatalf("bounded error suffix = %q, want explicit truncation marker", got[len(got)-32:])
	}
}

func TestClientSideRejectedAndExpandedInputEmitsNoAuditEvent(t *testing.T) {
	adapter := openTestAdapter(t)
	t.Cleanup(func() { _ = adapter.Close() })
	for _, tc := range []struct {
		name      string
		session   Session
		statement string
	}{
		{name: "no live Session", session: Session{}, statement: "select 1;"},
		{name: "incomplete SQL", session: Session{Adapter: adapter}, statement: "select 1"},
		{name: "Statement Expansion", session: Session{Adapter: adapter}, statement: "/select widgets"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			auditLog := fakeAudit{
				appendSubmitted: func(appaudit.SubmittedEvent) error { calls++; return nil },
				appendCompleted: func(appaudit.CompletedEvent) error { calls++; return nil },
			}
			model := newModelWithDependencies(tc.session, modelDependencies{audit: auditLog})
			model.state.SetReady("", NotificationNone)
			model.command.editor.SetValue(tc.statement)
			model.syncCurrentSQL()
			_, cmd := model.Update(submitIntentMsg{})
			_ = collectCommandMessagesForTest(t, cmd)
			if calls != 0 {
				t.Fatalf("Audit calls = %d, want 0", calls)
			}
		})
	}
}

func TestAuditFilesystemWorkIsDeferredToExecutionCommand(t *testing.T) {
	adapter := openTestAdapter(t)
	t.Cleanup(func() { _ = adapter.Close() })
	calls := 0
	model := newModelWithDependencies(Session{Adapter: adapter}, modelDependencies{
		audit: fakeAudit{appendSubmitted: func(appaudit.SubmittedEvent) error {
			calls++
			return nil
		}},
	})
	model.state.SetReady("", NotificationNone)
	model.command.editor.SetValue("select 1;")
	model.syncCurrentSQL()

	_, cmd := model.Update(submitIntentMsg{})
	if calls != 0 {
		t.Fatalf("Audit calls during Update = %d, want 0", calls)
	}
	_ = firstCommandMessageForTest[statementExecutedMsg](t, cmd)
	if calls != 1 {
		t.Fatalf("Audit calls after worker command = %d, want 1", calls)
	}
}

func TestAuditSubmissionDoesNotExposeDirectConnectionString(t *testing.T) {
	rawDSN := "postgres://alice:super-secret@db.example.test/app"
	resolved, ok, err := config.ParseConnectionString(rawDSN)
	if err != nil || !ok {
		t.Fatalf("ParseConnectionString() = %#v, %v, %v", resolved, ok, err)
	}
	adapter := openTestAdapter(t)
	t.Cleanup(func() { _ = adapter.Close() })
	var submitted appaudit.SubmittedEvent
	model := newModelWithDependencies(Session{
		ConnectionIdentity: resolved.Identity,
		Adapter:            adapter,
	}, modelDependencies{
		audit: fakeAudit{appendSubmitted: func(event appaudit.SubmittedEvent) error {
			submitted = event
			return nil
		}},
	})
	model.state.SetReady("", NotificationNone)
	model.command.editor.SetValue("select 1;")
	model.syncCurrentSQL()
	_, cmd := model.Update(submitIntentMsg{})
	_ = firstCommandMessageForTest[statementExecutedMsg](t, cmd)

	data, err := json.Marshal(submitted)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	for _, secret := range []string{rawDSN, "super-secret"} {
		if strings.Contains(string(data), secret) {
			t.Fatalf("Audit submission %s exposes %q", data, secret)
		}
	}
	if submitted.ConnectionIdentity != string(resolved.Identity) {
		t.Fatalf("connection identity = %q, want opaque resolved identity", submitted.ConnectionIdentity)
	}
}

func TestAuditClassifiesManualCancellationAsCancelled(t *testing.T) {
	adapter := openTestAdapter(t)
	t.Cleanup(func() { _ = adapter.Close() })
	var completed appaudit.CompletedEvent
	wantAuditErr := errors.New("cancelled completion unavailable")
	model := newModelWithDependencies(Session{Adapter: adapter}, modelDependencies{
		audit: fakeAudit{appendCompleted: func(event appaudit.CompletedEvent) error {
			completed = event
			return wantAuditErr
		}},
		newExecutionIdentity: func() string { return "cancelled-execution" },
	})
	model.state.SetReady("", NotificationNone)
	model.command.editor.SetValue("select 1;")
	model.syncCurrentSQL()

	next, cmd := model.Update(submitIntentMsg{})
	model = next.(Model)
	next, _ = model.Update(cancelRunningIntentMsg{})
	model = next.(Model)
	executed := firstCommandMessageForTest[statementExecutedMsg](t, cmd)
	next, _ = model.Update(executed)
	model = next.(Model)

	if completed.ExecutionIdentity != "cancelled-execution" {
		t.Fatalf("completion execution identity = %q, want correlated identity", completed.ExecutionIdentity)
	}
	if completed.Outcome != appaudit.OutcomeCancelled {
		t.Fatalf("completion outcome = %q, want cancelled", completed.Outcome)
	}
	if completed.ResultSummary.Error == "" {
		t.Fatal("completion summary error is empty, want useful cancellation metadata")
	}
	if pending := model.state.Interaction.PendingAuditCompletion; pending == nil || *pending != completed {
		t.Fatalf("pending Audit completion = %#v, want exact cancelled event %#v", pending, completed)
	}
	if latest, ok := model.history.Latest(); !ok || latest != "select 1;" {
		t.Fatalf("History latest = %q, %v; want cancelled Statement retained", latest, ok)
	}
	if got := model.state.Notification.Text; !strings.Contains(got, "Cancelled SQL") || !strings.Contains(got, wantAuditErr.Error()) {
		t.Fatalf("notification = %q, want friendly cancellation and Audit recovery feedback", got)
	}
}

func TestAuditClassifiesDeadlineAndRetainsTimedOutStatementInHistory(t *testing.T) {
	adapter := openTestAdapter(t)
	t.Cleanup(func() { _ = adapter.Close() })
	var completed appaudit.CompletedEvent
	model := newModelWithDependencies(Session{Adapter: adapter}, modelDependencies{
		audit: fakeAudit{appendCompleted: func(event appaudit.CompletedEvent) error {
			completed = event
			return nil
		}},
		newExecutionIdentity: func() string { return "timed-out-execution" },
		executionTimeout:     time.Nanosecond,
	})
	model.state.SetReady("", NotificationNone)
	statement := "select 1;"
	model.command.editor.SetValue(statement)
	model.syncCurrentSQL()

	next, cmd := model.Update(submitIntentMsg{})
	model = next.(Model)
	next, _ = model.Update(firstCommandMessageForTest[statementExecutedMsg](t, cmd))
	model = next.(Model)

	if completed.ExecutionIdentity != "timed-out-execution" {
		t.Fatalf("completion execution identity = %q, want correlated identity", completed.ExecutionIdentity)
	}
	if completed.Outcome != appaudit.OutcomeTimedOut {
		t.Fatalf("completion outcome = %q, want timed_out", completed.Outcome)
	}
	if completed.ResultSummary.Error == "" {
		t.Fatal("completion summary error is empty, want useful timeout metadata")
	}
	if latest, ok := model.history.Latest(); !ok || latest != statement {
		t.Fatalf("History latest = %q, %v; want timed-out Statement retained", latest, ok)
	}
	if got := model.state.Notification.Text; !strings.Contains(got, "timed out") {
		t.Fatalf("notification = %q, want friendly timeout feedback", got)
	}
}

func TestStaleInterruptedExecutionMessageDoesNotCompleteNewerExecution(t *testing.T) {
	model := NewModel(Session{})
	model.state.SetReady("new execution still running", NotificationInfo)
	model.state.SetRunningStatementContext(&RunningStatementContext{
		Label:             "SQL",
		ExecutionIdentity: "new-execution",
	})

	next, _ := model.Update(statementExecutedMsg{
		ExecutionIdentity: "old-execution",
		Statement:         "select old_work();",
		Err:               context.Canceled,
	})
	model = next.(Model)

	if running := model.state.Interaction.Running; running == nil || running.ExecutionIdentity != "new-execution" {
		t.Fatalf("Running Statement = %#v, want newer execution unchanged", running)
	}
	if _, ok := model.history.Latest(); ok {
		t.Fatal("History changed for stale execution message")
	}
	if got := model.state.Notification.Text; got != "new execution still running" {
		t.Fatalf("notification = %q, want newer execution feedback unchanged", got)
	}
}
