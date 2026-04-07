package history

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSessionAppendAndLatest(t *testing.T) {
	session := NewSession()
	stamp := time.Date(2026, time.April, 8, 12, 0, 0, 0, time.UTC)
	if err := session.Append(Entry{Command: "select 1", ConnectionName: "local", ExecutedAt: stamp}); err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	if err := session.Append(Entry{Command: "/tables", ConnectionName: "local", ExecutedAt: stamp.Add(time.Minute)}); err != nil {
		t.Fatalf("Append() error = %v", err)
	}

	entries := session.Entries()
	if got, want := len(entries), 2; got != want {
		t.Fatalf("len(Entries()) = %d, want %d", got, want)
	}
	if got, want := entries[0].Command, "select 1"; got != want {
		t.Fatalf("entries[0].Command = %q, want %q", got, want)
	}

	latest, ok := session.Latest()
	if !ok {
		t.Fatal("Latest() ok = false, want true")
	}
	if got, want := latest.Command, "/tables"; got != want {
		t.Fatalf("latest.Command = %q, want %q", got, want)
	}
}

func TestSessionAppendSkipsBlankCommandsAndClonesEntries(t *testing.T) {
	session := NewSession()
	if err := session.Append(Entry{Command: "   "}); err != nil {
		t.Fatalf("Append(blank) error = %v", err)
	}
	if err := session.Append(Entry{Command: "select 1"}); err != nil {
		t.Fatalf("Append() error = %v", err)
	}

	entries := session.Entries()
	if got, want := len(entries), 1; got != want {
		t.Fatalf("len(Entries()) = %d, want %d", got, want)
	}

	entries[0].Command = "changed"
	latest, ok := session.Latest()
	if !ok {
		t.Fatal("Latest() ok = false, want true")
	}
	if got, want := latest.Command, "select 1"; got != want {
		t.Fatalf("latest.Command = %q, want %q", got, want)
	}
}

func TestSessionAppendPersistsCommandsToFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), DirName, FileName)
	session := NewFileBackedSession(path)
	stamp := time.Date(2026, time.April, 8, 12, 0, 0, 0, time.UTC)

	if err := session.Append(Entry{Command: "select 1\n", ConnectionName: "local", ExecutedAt: stamp}); err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	if err := session.Append(Entry{Command: "/tables", ConnectionName: "local", ExecutedAt: stamp.Add(time.Minute), ResultSummary: "Listed 3 tables."}); err != nil {
		t.Fatalf("Append() error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if got, want := len(lines), 2; got != want {
		t.Fatalf("persisted line count = %d, want %d", got, want)
	}

	var first persistedEntry
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatalf("Unmarshal(first line) error = %v", err)
	}
	if got, want := first.Command, "select 1"; got != want {
		t.Fatalf("first.Command = %q, want %q", got, want)
	}
	if got, want := first.Connection, "local"; got != want {
		t.Fatalf("first.Connection = %q, want %q", got, want)
	}
	if got, want := first.Time, stamp; !got.Equal(want) {
		t.Fatalf("first.Time = %v, want %v", got, want)
	}

	var second persistedEntry
	if err := json.Unmarshal([]byte(lines[1]), &second); err != nil {
		t.Fatalf("Unmarshal(second line) error = %v", err)
	}
	if got, want := second.Command, "/tables"; got != want {
		t.Fatalf("second.Command = %q, want %q", got, want)
	}
	if got, want := second.Connection, "local"; got != want {
		t.Fatalf("second.Connection = %q, want %q", got, want)
	}
	if got, want := second.Result, "Listed 3 tables."; got != want {
		t.Fatalf("second.Result = %q, want %q", got, want)
	}
	if got, want := second.Time, stamp.Add(time.Minute); !got.Equal(want) {
		t.Fatalf("second.Time = %v, want %v", got, want)
	}
}

func TestSessionAppendRotatesHistoryLogWhenItWouldGrowPastLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), DirName, FileName)
	rotatedPath := path + rotatedHistorySuffix
	original := strings.Repeat("x", maxHistoryLogSize)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatalf("WriteFile(history) error = %v", err)
	}
	if err := os.WriteFile(rotatedPath, []byte("stale"), 0o644); err != nil {
		t.Fatalf("WriteFile(rotated) error = %v", err)
	}

	session := NewFileBackedSession(path)
	stamp := time.Date(2026, time.April, 8, 12, 0, 0, 0, time.UTC)
	if err := session.Append(Entry{Command: "select 1", ConnectionName: "local", ExecutedAt: stamp}); err != nil {
		t.Fatalf("Append() error = %v", err)
	}

	rotatedData, err := os.ReadFile(rotatedPath)
	if err != nil {
		t.Fatalf("ReadFile(rotated) error = %v", err)
	}
	if got := string(rotatedData); got != original {
		t.Fatalf("rotated log contents = %q, want original log contents", got)
	}

	currentData, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(current) error = %v", err)
	}

	var persisted persistedEntry
	if err := json.Unmarshal(currentData, &persisted); err != nil {
		t.Fatalf("Unmarshal(current) error = %v", err)
	}
	if got, want := persisted.Command, "select 1"; got != want {
		t.Fatalf("persisted.Command = %q, want %q", got, want)
	}
}

func TestBoundResultSummary(t *testing.T) {
	long := "  Query\nreturned\t" + strings.Repeat("1234567890", 12) + "  "
	got := boundResultSummary(long)

	if strings.Contains(got, "\n") || strings.Contains(got, "\t") {
		t.Fatalf("boundResultSummary() = %q, want normalized whitespace", got)
	}
	if got == "" {
		t.Fatal("boundResultSummary() = empty, want bounded summary")
	}
	if runeCount := len([]rune(got)); runeCount > 120 {
		t.Fatalf("len([]rune(summary)) = %d, want <= 120", runeCount)
	}
	if !strings.HasSuffix(got, "...") {
		t.Fatalf("boundResultSummary() = %q, want ellipsis suffix", got)
	}
}

func TestDefaultPathUsesXDGDataHome(t *testing.T) {
	dataHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataHome)

	path, err := DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath() error = %v", err)
	}

	if got, want := path, filepath.Join(dataHome, DirName, FileName); got != want {
		t.Fatalf("DefaultPath() = %q, want %q", got, want)
	}
}

func TestResolveDataHomeUsesMacStyleFallback(t *testing.T) {
	homeDir := filepath.Join(string(filepath.Separator), "Users", "tester")

	dataHome, err := resolveDataHome(func() (string, error) { return homeDir, nil }, "darwin")
	if err != nil {
		t.Fatalf("resolveDataHome() error = %v", err)
	}

	if got, want := dataHome, filepath.Join(homeDir, ".local", "share"); got != want {
		t.Fatalf("resolveDataHome() = %q, want %q", got, want)
	}
}

func TestResolveDataHomeRejectsRelativeXDGDataHome(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "relative/path")

	_, err := resolveDataHome(func() (string, error) { return t.TempDir(), nil }, "linux")
	if err == nil {
		t.Fatal("resolveDataHome() error = nil, want error")
	}
	if got, want := err.Error(), "XDG_DATA_HOME must be an absolute path"; !strings.Contains(got, want) {
		t.Fatalf("resolveDataHome() error = %q, want to contain %q", got, want)
	}
}
