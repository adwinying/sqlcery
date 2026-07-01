package history

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestFileBackedHistoryPersistsOnlyOrderedStatementStrings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history", "identity.json")
	history := NewFileBackedHistory(path)
	if err := history.Append("select 1;"); err != nil {
		t.Fatalf("Append(first) error = %v", err)
	}
	if err := history.Append("select 2;"); err != nil {
		t.Fatalf("Append(second) error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	var got []string
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	want := []string{"select 1;", "select 2;"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("persisted history = %#v, want %#v", got, want)
	}
}

func TestAppendMovesExactDuplicateToLatestPosition(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.json")
	history := NewFileBackedHistory(path)
	for _, statement := range []string{"select 1;", "select 2;", "select 1;", " select 1;"} {
		if err := history.Append(statement); err != nil {
			t.Fatalf("Append(%q) error = %v", statement, err)
		}
	}

	entries := history.Entries()
	got := entries
	want := []string{"select 2;", "select 1;", " select 1;"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Entries() = %#v, want %#v", got, want)
	}
}

func TestHistoryRetainsOnlyThousandMostRecentUniqueStatements(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.json")
	history := NewFileBackedHistory(path)
	for i := 0; i < MaxEntries+2; i++ {
		statement := fmt.Sprintf("select %d;", i)
		if err := history.Append(statement); err != nil {
			t.Fatalf("Append(%q) error = %v", statement, err)
		}
	}

	entries := history.Entries()
	if got, want := len(entries), MaxEntries; got != want {
		t.Fatalf("len(Entries()) = %d, want %d", got, want)
	}
	if got, want := entries[0], "select 2;"; got != want {
		t.Fatalf("Entries()[0] = %q, want %q", got, want)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	var persisted []string
	if err := json.Unmarshal(data, &persisted); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if got, want := len(persisted), MaxEntries; got != want {
		t.Fatalf("len(persisted) = %d, want %d", got, want)
	}
}

func TestNewPersistentHistoryBoundsAndDeduplicatesRestoredStatements(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	path, err := DefaultPath("opaque-identity")
	if err != nil {
		t.Fatalf("DefaultPath() error = %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	statements := make([]string, 0, MaxEntries+2)
	statements = append(statements, "duplicate;")
	for i := 0; i < MaxEntries; i++ {
		statements = append(statements, fmt.Sprintf("select %d;", i))
	}
	statements = append(statements, "duplicate;")
	data, err := json.Marshal(statements)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	history, err := NewPersistentHistory("opaque-identity")
	if err != nil {
		t.Fatalf("NewPersistentHistory() error = %v", err)
	}
	entries := history.Entries()
	if got, want := len(entries), MaxEntries; got != want {
		t.Fatalf("len(Entries()) = %d, want %d", got, want)
	}
	if got, want := entries[0], "select 1;"; got != want {
		t.Fatalf("Entries()[0] = %q, want %q", got, want)
	}
	if got, want := entries[len(entries)-1], "duplicate;"; got != want {
		t.Fatalf("latest Statement = %q, want %q", got, want)
	}
}

func TestPersistenceFailureKeepsMemoryAndLaterSaveRecoversSnapshot(t *testing.T) {
	root := t.TempDir()
	blockedDirectory := filepath.Join(root, "blocked")
	if err := os.WriteFile(blockedDirectory, []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("WriteFile(blocker) error = %v", err)
	}
	path := filepath.Join(blockedDirectory, "history.json")
	history := NewFileBackedHistory(path)

	if err := history.Append("select failed-write;"); err == nil {
		t.Fatal("Append(first) error = nil, want persistence error")
	}
	if latest, ok := history.Latest(); !ok || latest != "select failed-write;" {
		t.Fatalf("Latest() = (%#v, %v), want failed-write Statement in memory", latest, ok)
	}

	if err := os.Remove(blockedDirectory); err != nil {
		t.Fatalf("Remove(blocker) error = %v", err)
	}
	if err := history.Append("select recovered;"); err != nil {
		t.Fatalf("Append(second) error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	var got []string
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	want := []string{"select failed-write;", "select recovered;"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("persisted history = %#v, want %#v", got, want)
	}
}

func TestPersistentHistoryRestoresAndIsolatesConnectionIdentities(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	first, err := NewPersistentHistory("identity-one")
	if err != nil {
		t.Fatalf("NewPersistentHistory(first) error = %v", err)
	}
	if err := first.Append("select one;"); err != nil {
		t.Fatalf("Append() error = %v", err)
	}

	reopened, err := NewPersistentHistory("identity-one")
	if err != nil {
		t.Fatalf("NewPersistentHistory(reopened) error = %v", err)
	}
	if latest, ok := reopened.Latest(); !ok || latest != "select one;" {
		t.Fatalf("reopened.Latest() = (%#v, %v), want restored Statement", latest, ok)
	}

	other, err := NewPersistentHistory("identity-two")
	if err != nil {
		t.Fatalf("NewPersistentHistory(other) error = %v", err)
	}
	if entries := other.Entries(); len(entries) != 0 {
		t.Fatalf("other.Entries() = %#v, want isolated empty history", entries)
	}
}
