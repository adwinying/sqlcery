package history

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	DirName              = "sqlcery"
	FileName             = "history.log"
	maxAuditLogSize    = 1 << 20
	rotatedAuditLogSuffix = ".1"
	maxLoadedEntries     = 1000
)

type Entry struct {
	SQL            string
	ConnectionName string
	ExecutedAt     time.Time
	ResultSummary  string
}

type store interface {
	Append(Entry) error
}

type History struct {
	entries []Entry
	store   store
}

func NewHistory() *History {
	return &History{}
}

func NewFileBackedHistory(path string) *History {
	if strings.TrimSpace(path) == "" {
		return NewHistory()
	}

	return &History{store: auditLogStore{path: path}}
}

func NewPersistentHistory(connectionName string) (*History, error) {
	path, err := DefaultPath()
	if err != nil {
		return nil, err
	}

	entries, err := LoadFromFile(path, connectionName)
	if err != nil {
		return nil, err
	}

	h := NewFileBackedHistory(path)
	h.entries = entries
	return h, nil
}

// LoadFromFile reads persisted history entries from path and path+".1" (if it
// exists), filters to the given connectionName, deduplicates keeping the most
// recent occurrence of each command, and returns at most maxLoadedEntries
// entries in chronological order.
func LoadFromFile(path string, connectionName string) ([]Entry, error) {
	var all []Entry
	// Read the older rotated file first so entries are in chronological order.
	for _, p := range []string{path + rotatedAuditLogSuffix, path} {
		entries, err := readAuditLogFile(p)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("read history file %s: %w", p, err)
		}
		all = append(all, entries...)
	}

	// Filter to the requested connection.
	filtered := make([]Entry, 0, len(all))
	for _, e := range all {
		if e.ConnectionName == connectionName {
			filtered = append(filtered, e)
		}
	}

	// Deduplicate: walk backwards so the first occurrence we encounter is the
	// most recent; skip any command we have already seen.
	seen := make(map[string]struct{}, len(filtered))
	deduped := make([]Entry, 0, len(filtered))
	for i := len(filtered) - 1; i >= 0; i-- {
		cmd := filtered[i].SQL
		if _, ok := seen[cmd]; ok {
			continue
		}
		seen[cmd] = struct{}{}
		deduped = append(deduped, filtered[i])
	}

	// Restore chronological (oldest-first) order.
	for i, j := 0, len(deduped)-1; i < j; i, j = i+1, j-1 {
		deduped[i], deduped[j] = deduped[j], deduped[i]
	}

	// Cap to the most recent maxLoadedEntries entries.
	if len(deduped) > maxLoadedEntries {
		deduped = deduped[len(deduped)-maxLoadedEntries:]
	}

	return deduped, nil
}

// readAuditLogFile reads all valid JSONL history entries from a single file.
// Malformed lines are silently skipped.
func readAuditLogFile(path string) ([]Entry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var entries []Entry
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var pe persistedEntry
		if err := json.Unmarshal([]byte(line), &pe); err != nil {
			continue // skip malformed lines
		}
		entries = append(entries, Entry{
			SQL:            pe.Command,
			ConnectionName: pe.Connection,
			ExecutedAt:     pe.Time,
			ResultSummary:  pe.Result,
		})
	}
	return entries, nil
}

func DefaultPath() (string, error) {
	dataHome, err := resolveDataHome(os.UserHomeDir, runtime.GOOS)
	if err != nil {
		return "", fmt.Errorf("resolve user data dir: %w", err)
	}

	return filepath.Join(dataHome, DirName, FileName), nil
}

func (s *History) Append(entry Entry) error {
	if s == nil || strings.TrimSpace(entry.SQL) == "" {
		return nil
	}

	s.entries = append(s.entries, entry)
	if s.store == nil {
		return nil
	}

	return s.store.Append(entry)
}

func (s *History) Entries() []Entry {
	if s == nil || len(s.entries) == 0 {
		return nil
	}

	entries := make([]Entry, len(s.entries))
	copy(entries, s.entries)
	return entries
}

func (s *History) Latest() (Entry, bool) {
	if s == nil || len(s.entries) == 0 {
		return Entry{}, false
	}

	return s.entries[len(s.entries)-1], true
}

func resolveDataHome(userHomeDir func() (string, error), goos string) (string, error) {
	if dir, ok := os.LookupEnv("XDG_DATA_HOME"); ok {
		if !filepath.IsAbs(dir) {
			return "", fmt.Errorf("XDG_DATA_HOME must be an absolute path")
		}
		return dir, nil
	}

	homeDir, err := userHomeDir()
	if err != nil {
		return "", err
	}

	if goos == "windows" {
		return filepath.Join(homeDir, "AppData", "Local"), nil
	}

	return filepath.Join(homeDir, ".local", "share"), nil
}

type auditLogStore struct {
	path string
}

type persistedEntry struct {
	Command    string    `json:"command"`
	Connection string    `json:"connection"`
	Time       time.Time `json:"time"`
	Result     string    `json:"result,omitempty"`
}

func newPersistedEntry(entry Entry) persistedEntry {
	return persistedEntry{
		Command:    strings.TrimRight(entry.SQL, "\n"),
		Connection: entry.ConnectionName,
		Time:       entry.ExecutedAt,
		Result:     boundResultSummary(entry.ResultSummary),
	}
}

func boundResultSummary(value string) string {
	const maxRunes = 120

	trimmed := strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if trimmed == "" {
		return ""
	}

	runes := []rune(trimmed)
	if len(runes) <= maxRunes {
		return trimmed
	}

	if maxRunes <= 3 {
		return string(runes[:maxRunes])
	}

	return string(runes[:maxRunes-3]) + "..."
}

func (s auditLogStore) Append(entry Entry) error {
	if strings.TrimSpace(entry.SQL) == "" {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("create audit log dir: %w", err)
	}

	line, err := json.Marshal(newPersistedEntry(entry))
	if err != nil {
		return fmt.Errorf("marshal history entry: %w", err)
	}
	line = append(line, '\n')

	if err := s.rotateIfNeeded(int64(len(line))); err != nil {
		return err
	}

	file, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open audit log: %w", err)
	}
	defer file.Close()

	if _, err := file.Write(line); err != nil {
		return fmt.Errorf("append audit log: %w", err)
	}

	return nil
}

func (s auditLogStore) rotateIfNeeded(nextWriteSize int64) error {
	info, err := os.Stat(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("stat audit log: %w", err)
	}

	if info.Size()+nextWriteSize <= maxAuditLogSize {
		return nil
	}

	rotatedPath := s.path + rotatedAuditLogSuffix
	if err := os.Remove(rotatedPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove rotated audit log: %w", err)
	}
	if err := os.Rename(s.path, rotatedPath); err != nil {
		return fmt.Errorf("rotate audit log: %w", err)
	}

	return nil
}
