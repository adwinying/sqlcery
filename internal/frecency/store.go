package frecency

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

const (
	dirName  = "sqlcery"
	fileName = "frecency.json"
)

// Store is the public API for the frecency package. It wraps a scorer (pure
// in-memory scoring) with JSON persistence to the user's data directory.
type Store struct {
	path   string
	scorer *scorer
}

// DefaultPath returns the platform-appropriate path for the frecency file,
// following the same XDG_DATA_HOME convention as internal/history.
func DefaultPath() (string, error) {
	dataHome, err := resolveDataHome(os.UserHomeDir, runtime.GOOS)
	if err != nil {
		return "", fmt.Errorf("resolve user data dir: %w", err)
	}
	return filepath.Join(dataHome, dirName, fileName), nil
}

// Load reads the frecency file at path (or starts empty if it doesn't exist)
// and returns a Store bound to the given clock function.
//
// A missing file is not an error — the store starts empty. A corrupt file is
// treated as empty (consistent with history's silent-skip of malformed lines).
func Load(path string, now func() time.Time) (*Store, error) {
	if now == nil {
		now = time.Now
	}
	s := &Store{
		path:   path,
		scorer: newScorer(now),
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return s, nil // empty store, no error
		}
		return nil, fmt.Errorf("read frecency file: %w", err)
	}

	var entries map[string]entry
	if err := json.Unmarshal(data, &entries); err != nil {
		// Corrupt file — treat as empty, do not crash.
		return s, nil
	}

	s.scorer.entries = entries
	return s, nil
}

// RecordOpen decays the score for name, increments it by 1, and persists the
// updated store to disk (write-through).
func (s *Store) RecordOpen(name string) error {
	s.scorer.record(name)
	return s.persist()
}

// Order returns names sorted by current decayed frecency (descending).
// Names absent from the store are included at score 0. Ties break
// alphabetically so the result is deterministic.
func (s *Store) Order(names []string) []string {
	return s.scorer.order(names)
}

// persist serialises the in-memory entries to disk using a write-to-temp-then-rename
// pattern for atomicity.
func (s *Store) persist() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("create frecency dir: %w", err)
	}

	data, err := json.Marshal(s.scorer.entries)
	if err != nil {
		return fmt.Errorf("marshal frecency data: %w", err)
	}

	// Write to a sibling temp file, then rename for an atomic replacement.
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write frecency tmp file: %w", err)
	}

	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("rename frecency file: %w", err)
	}

	return nil
}

// resolveDataHome mirrors the function of the same name in internal/history.
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
