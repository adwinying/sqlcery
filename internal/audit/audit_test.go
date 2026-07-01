package audit

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFileLogAppendsExternallyConsumableJSONL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sqlcery", "audit.log")
	log := NewFileLog(path)
	timestamp := time.Date(2026, time.July, 1, 2, 3, 4, 567, time.UTC)
	if err := log.AppendSubmitted(SubmittedEvent{
		Event: "submitted", ExecutionIdentity: "exec-1", ConnectionIdentity: "conn-1",
		ConnectionName: "local", ConnectionOrigin: "/project/connections.toml",
		Statement: "select 'secret result';", Timestamp: timestamp,
	}); err != nil {
		t.Fatalf("AppendSubmitted() error = %v", err)
	}
	rows := int64(2)
	if err := log.AppendCompleted(CompletedEvent{
		Event: "completed", ExecutionIdentity: "exec-1", Timestamp: timestamp.Add(time.Second),
		Outcome: OutcomeSuccess, ResultSummary: ResultSummary{RowsAffected: &rows},
	}); err != nil {
		t.Fatalf("AppendCompleted() error = %v", err)
	}

	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	var records []map[string]any
	for scanner.Scan() {
		var record map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			t.Fatalf("Audit line is not valid JSON: %v", err)
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("record count = %d, want 2", len(records))
	}
	if got := records[0]["timestamp"]; got != timestamp.Format(time.RFC3339Nano) {
		t.Fatalf("submitted timestamp = %v, want RFC3339Nano UTC", got)
	}
	if got := records[1]["execution_identity"]; got != records[0]["execution_identity"] {
		t.Fatalf("completed identity = %v, want %v", got, records[0]["execution_identity"])
	}
}

func TestFileLogDoesNotRotateLargeEvents(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.log")
	statement := "select '" + strings.Repeat("x", 2<<20) + "';"
	if err := NewFileLog(path).AppendSubmitted(SubmittedEvent{
		Event: "submitted", ExecutionIdentity: "exec-large", ConnectionIdentity: "conn",
		Statement: statement, Timestamp: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("AppendSubmitted() error = %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !strings.Contains(string(data), statement) {
		t.Fatal("Audit Log did not preserve the exact large Statement")
	}
	if matches, err := filepath.Glob(path + "*"); err != nil {
		t.Fatalf("Glob() error = %v", err)
	} else if len(matches) != 1 || matches[0] != path {
		t.Fatalf("Audit files = %#v, want only unrotated %q", matches, path)
	}
}

func TestDefaultPathUsesGlobalDataDirectory(t *testing.T) {
	dataHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataHome)
	got, err := DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath() error = %v", err)
	}
	want := filepath.Join(dataHome, "sqlcery", "audit.log")
	if got != want {
		t.Fatalf("DefaultPath() = %q, want %q", got, want)
	}
}
