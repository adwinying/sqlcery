package history

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSessionAppendAndLatest(t *testing.T) {
	session := NewHistory()
	stamp := time.Date(2026, time.April, 8, 12, 0, 0, 0, time.UTC)
	if err := session.Append(Entry{Statement: "select 1", ConnectionName: "local", ExecutedAt: stamp}); err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	if err := session.Append(Entry{Statement: "/tables", ConnectionName: "local", ExecutedAt: stamp.Add(time.Minute)}); err != nil {
		t.Fatalf("Append() error = %v", err)
	}

	entries := session.Entries()
	if got, want := len(entries), 2; got != want {
		t.Fatalf("len(Entries()) = %d, want %d", got, want)
	}
	if got, want := entries[0].Statement, "select 1"; got != want {
		t.Fatalf("entries[0].Statement = %q, want %q", got, want)
	}

	latest, ok := session.Latest()
	if !ok {
		t.Fatal("Latest() ok = false, want true")
	}
	if got, want := latest.Statement, "/tables"; got != want {
		t.Fatalf("latest.Statement = %q, want %q", got, want)
	}
}

func TestSessionAppendSkipsBlankCommandsAndClonesEntries(t *testing.T) {
	session := NewHistory()
	if err := session.Append(Entry{Statement: "   "}); err != nil {
		t.Fatalf("Append(blank) error = %v", err)
	}
	if err := session.Append(Entry{Statement: "select 1"}); err != nil {
		t.Fatalf("Append() error = %v", err)
	}

	entries := session.Entries()
	if got, want := len(entries), 1; got != want {
		t.Fatalf("len(Entries()) = %d, want %d", got, want)
	}

	entries[0].Statement = "changed"
	latest, ok := session.Latest()
	if !ok {
		t.Fatal("Latest() ok = false, want true")
	}
	if got, want := latest.Statement, "select 1"; got != want {
		t.Fatalf("latest.Statement = %q, want %q", got, want)
	}
}

func TestSessionAppendPersistsCommandsToFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), DirName, FileName)
	session := NewFileBackedHistory(path)
	stamp := time.Date(2026, time.April, 8, 12, 0, 0, 0, time.UTC)

	if err := session.Append(Entry{Statement: "select 1\n", ConnectionName: "local", ExecutedAt: stamp}); err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	if err := session.Append(Entry{Statement: "/tables", ConnectionName: "local", ExecutedAt: stamp.Add(time.Minute), ResultSummary: "Listed 3 tables."}); err != nil {
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
	if got, want := first.Statement, "select 1"; got != want {
		t.Fatalf("first.Statement = %q, want %q", got, want)
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
	if got, want := second.Statement, "/tables"; got != want {
		t.Fatalf("second.Statement = %q, want %q", got, want)
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
	rotatedPath := path + rotatedAuditLogSuffix
	original := strings.Repeat("x", maxAuditLogSize)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatalf("WriteFile(history) error = %v", err)
	}
	if err := os.WriteFile(rotatedPath, []byte("stale"), 0o644); err != nil {
		t.Fatalf("WriteFile(rotated) error = %v", err)
	}

	session := NewFileBackedHistory(path)
	stamp := time.Date(2026, time.April, 8, 12, 0, 0, 0, time.UTC)
	if err := session.Append(Entry{Statement: "select 1", ConnectionName: "local", ExecutedAt: stamp}); err != nil {
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
	if got, want := persisted.Statement, "select 1"; got != want {
		t.Fatalf("persisted.Statement = %q, want %q", got, want)
	}
}

// writeHistoryLines is a test helper that marshals entries as JSONL into path.
func writeHistoryLines(t *testing.T, path string, entries []Entry) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	var lines []string
	for _, e := range entries {
		b, err := json.Marshal(newPersistedEntry(e))
		if err != nil {
			t.Fatalf("json.Marshal() error = %v", err)
		}
		lines = append(lines, string(b))
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}

func TestLoadFromFileNoFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, DirName, FileName)

	entries, err := LoadFromFile(path, "local")
	if err != nil {
		t.Fatalf("LoadFromFile() error = %v, want nil", err)
	}
	if len(entries) != 0 {
		t.Fatalf("LoadFromFile() len = %d, want 0", len(entries))
	}
}

func TestLoadFromFileFiltersByConnection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, DirName, FileName)
	stamp := time.Date(2026, time.April, 8, 12, 0, 0, 0, time.UTC)

	writeHistoryLines(t, path, []Entry{
		{Statement: "select 1", ConnectionName: "local", ExecutedAt: stamp},
		{Statement: "select 2", ConnectionName: "remote", ExecutedAt: stamp.Add(time.Minute)},
		{Statement: "select 3", ConnectionName: "local", ExecutedAt: stamp.Add(2 * time.Minute)},
	})

	entries, err := LoadFromFile(path, "local")
	if err != nil {
		t.Fatalf("LoadFromFile() error = %v", err)
	}
	if got, want := len(entries), 2; got != want {
		t.Fatalf("len(entries) = %d, want %d", got, want)
	}
	if got, want := entries[0].Statement, "select 1"; got != want {
		t.Fatalf("entries[0].Statement = %q, want %q", got, want)
	}
	if got, want := entries[1].Statement, "select 3"; got != want {
		t.Fatalf("entries[1].Statement = %q, want %q", got, want)
	}
}

func TestLoadFromFileDeduplicatesKeepsMostRecent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, DirName, FileName)
	stamp := time.Date(2026, time.April, 8, 12, 0, 0, 0, time.UTC)

	writeHistoryLines(t, path, []Entry{
		{Statement: "select 1", ConnectionName: "local", ExecutedAt: stamp},
		{Statement: "select 2", ConnectionName: "local", ExecutedAt: stamp.Add(time.Minute)},
		{Statement: "select 1", ConnectionName: "local", ExecutedAt: stamp.Add(2 * time.Minute)},
	})

	entries, err := LoadFromFile(path, "local")
	if err != nil {
		t.Fatalf("LoadFromFile() error = %v", err)
	}
	if got, want := len(entries), 2; got != want {
		t.Fatalf("len(entries) = %d, want %d (duplicate should be removed)", got, want)
	}
	// The earlier "select 1" is removed; the later one is kept.
	// Chronological order: select 2 (older), select 1 (newer).
	if got, want := entries[0].Statement, "select 2"; got != want {
		t.Fatalf("entries[0].Statement = %q, want %q", got, want)
	}
	if got, want := entries[1].Statement, "select 1"; got != want {
		t.Fatalf("entries[1].Statement = %q, want %q", got, want)
	}
	// The kept "select 1" entry should be the most recent one.
	if got, want := entries[1].ExecutedAt, stamp.Add(2*time.Minute); !got.Equal(want) {
		t.Fatalf("entries[1].ExecutedAt = %v, want %v", got, want)
	}
}

func TestLoadFromFileReadsBothRotatedAndCurrentFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, DirName, FileName)
	rotatedPath := path + rotatedAuditLogSuffix
	stamp := time.Date(2026, time.April, 8, 12, 0, 0, 0, time.UTC)

	writeHistoryLines(t, rotatedPath, []Entry{
		{Statement: "select old", ConnectionName: "local", ExecutedAt: stamp},
	})
	writeHistoryLines(t, path, []Entry{
		{Statement: "select new", ConnectionName: "local", ExecutedAt: stamp.Add(time.Hour)},
	})

	entries, err := LoadFromFile(path, "local")
	if err != nil {
		t.Fatalf("LoadFromFile() error = %v", err)
	}
	if got, want := len(entries), 2; got != want {
		t.Fatalf("len(entries) = %d, want %d", got, want)
	}
	if got, want := entries[0].Statement, "select old"; got != want {
		t.Fatalf("entries[0].Statement = %q, want %q (older rotated entry should come first)", got, want)
	}
	if got, want := entries[1].Statement, "select new"; got != want {
		t.Fatalf("entries[1].Statement = %q, want %q", got, want)
	}
}

func TestLoadFromFileCapsAtMaxLoadedEntries(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, DirName, FileName)
	stamp := time.Date(2026, time.April, 8, 12, 0, 0, 0, time.UTC)

	all := make([]Entry, maxLoadedEntries+10)
	for i := range all {
		all[i] = Entry{
			Statement:        fmt.Sprintf("select %d", i),
			ConnectionName: "local",
			ExecutedAt:     stamp.Add(time.Duration(i) * time.Second),
		}
	}
	writeHistoryLines(t, path, all)

	entries, err := LoadFromFile(path, "local")
	if err != nil {
		t.Fatalf("LoadFromFile() error = %v", err)
	}
	if got, want := len(entries), maxLoadedEntries; got != want {
		t.Fatalf("len(entries) = %d, want %d", got, want)
	}
	// Should be the most recent maxLoadedEntries entries.
	if got, want := entries[0].Statement, fmt.Sprintf("select %d", 10); got != want {
		t.Fatalf("entries[0].Statement = %q, want %q (should be oldest of retained entries)", got, want)
	}
}

func TestLoadFromFileSkipsMalformedLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, DirName, FileName)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	stamp := time.Date(2026, time.April, 8, 12, 0, 0, 0, time.UTC)
	good, err := json.Marshal(newPersistedEntry(Entry{Statement: "select 1", ConnectionName: "local", ExecutedAt: stamp}))
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	contents := "not-json\n" + string(good) + "\n{broken\n"
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	entries, err := LoadFromFile(path, "local")
	if err != nil {
		t.Fatalf("LoadFromFile() error = %v", err)
	}
	if got, want := len(entries), 1; got != want {
		t.Fatalf("len(entries) = %d, want %d", got, want)
	}
	if got, want := entries[0].Statement, "select 1"; got != want {
		t.Fatalf("entries[0].Statement = %q, want %q", got, want)
	}
}

func TestNewPersistentSessionSeedsEntriesFromDisk(t *testing.T) {
	dataHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataHome)

	path, err := DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath() error = %v", err)
	}

	stamp := time.Date(2026, time.April, 8, 12, 0, 0, 0, time.UTC)
	writeHistoryLines(t, path, []Entry{
		{Statement: "select 1", ConnectionName: "local", ExecutedAt: stamp},
		{Statement: "select 2", ConnectionName: "remote", ExecutedAt: stamp.Add(time.Minute)},
		{Statement: "select 3", ConnectionName: "local", ExecutedAt: stamp.Add(2 * time.Minute)},
	})

	session, err := NewPersistentHistory("local")
	if err != nil {
		t.Fatalf("NewPersistentSession() error = %v", err)
	}

	entries := session.Entries()
	if got, want := len(entries), 2; got != want {
		t.Fatalf("len(session.Entries()) = %d, want %d", got, want)
	}
	if got, want := entries[0].Statement, "select 1"; got != want {
		t.Fatalf("entries[0].Statement = %q, want %q", got, want)
	}
	if got, want := entries[1].Statement, "select 3"; got != want {
		t.Fatalf("entries[1].Statement = %q, want %q", got, want)
	}
}

func TestNewPersistentSessionAppendsToExistingFile(t *testing.T) {
	dataHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataHome)

	path, err := DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath() error = %v", err)
	}

	stamp := time.Date(2026, time.April, 8, 12, 0, 0, 0, time.UTC)
	writeHistoryLines(t, path, []Entry{
		{Statement: "select 1", ConnectionName: "local", ExecutedAt: stamp},
	})

	session, err := NewPersistentHistory("local")
	if err != nil {
		t.Fatalf("NewPersistentSession() error = %v", err)
	}

	if err := session.Append(Entry{Statement: "select 2", ConnectionName: "local", ExecutedAt: stamp.Add(time.Minute)}); err != nil {
		t.Fatalf("Append() error = %v", err)
	}

	entries := session.Entries()
	if got, want := len(entries), 2; got != want {
		t.Fatalf("len(session.Entries()) = %d, want %d", got, want)
	}
	if got, want := entries[0].Statement, "select 1"; got != want {
		t.Fatalf("entries[0].Statement = %q, want %q", got, want)
	}
	if got, want := entries[1].Statement, "select 2"; got != want {
		t.Fatalf("entries[1].Statement = %q, want %q", got, want)
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
