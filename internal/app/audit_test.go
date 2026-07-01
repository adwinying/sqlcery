package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	appaudit "github.com/adwinying/sqlcery/internal/audit"
	"github.com/adwinying/sqlcery/internal/config"
)

type fakeAudit struct {
	appendSubmitted func(appaudit.SubmittedEvent) error
	appendCompleted func(appaudit.CompletedEvent) error
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

func TestAuditDefersCancellationCompletionClassification(t *testing.T) {
	adapter := openTestAdapter(t)
	t.Cleanup(func() { _ = adapter.Close() })
	completedCalls := 0
	model := newModelWithDependencies(Session{Adapter: adapter}, modelDependencies{
		audit: fakeAudit{appendCompleted: func(appaudit.CompletedEvent) error {
			completedCalls++
			return nil
		}},
	})
	model.state.SetReady("", NotificationNone)
	model.command.editor.SetValue("select 1;")
	model.syncCurrentSQL()

	next, cmd := model.Update(submitIntentMsg{})
	model = next.(Model)
	next, _ = model.Update(cancelRunningIntentMsg{})
	model = next.(Model)
	_ = firstCommandMessageForTest[statementExecutedMsg](t, cmd)

	if completedCalls != 0 {
		t.Fatalf("completed Audit calls = %d, want cancellation classification deferred", completedCalls)
	}
}
