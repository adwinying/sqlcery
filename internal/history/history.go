package history

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/adwinying/sqlcery/internal/filelock"
)

const (
	DirName        = "sqlcery"
	HistoryDirName = "history"
	MaxEntries     = 1000
)

type store interface {
	Save([]string) error
}

type History struct {
	statements []string
	pending    []string
	store      store
}

func NewHistory() *History {
	return &History{}
}

func NewFileBackedHistory(path string) *History {
	if strings.TrimSpace(path) == "" {
		return NewHistory()
	}
	return &History{store: fileStore{path: path}}
}

func NewPersistentHistory(identity string) (*History, error) {
	path, err := DefaultPath(identity)
	if err != nil {
		return nil, err
	}
	statements, err := loadStatements(path)
	if err != nil {
		return nil, err
	}
	return &History{statements: statements, store: fileStore{path: path}}, nil
}

func DefaultPath(identity string) (string, error) {
	identity = strings.TrimSpace(identity)
	if identity == "" || filepath.Base(identity) != identity {
		return "", fmt.Errorf("connection identity is required")
	}
	dataHome, err := resolveDataHome(os.UserHomeDir, runtime.GOOS)
	if err != nil {
		return "", fmt.Errorf("resolve user data dir: %w", err)
	}
	return filepath.Join(dataHome, DirName, HistoryDirName, identity+".json"), nil
}

func loadStatements(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read history file %s: %w", path, err)
	}
	var statements []string
	if err := json.Unmarshal(data, &statements); err != nil {
		return nil, fmt.Errorf("decode history file %s: %w", path, err)
	}
	normalized := NewHistory()
	for _, statement := range statements {
		if err := normalized.Append(statement); err != nil {
			return nil, err
		}
	}
	return normalized.statements, nil
}

func (h *History) Append(statement string) error {
	if h == nil || strings.TrimSpace(statement) == "" {
		return nil
	}
	h.statements = appendStatement(h.statements, statement)
	if h.store == nil {
		return nil
	}
	h.pending = appendStatement(h.pending, statement)
	if err := h.store.Save(h.pending); err != nil {
		return err
	}
	h.pending = nil
	return nil
}

func appendStatement(statements []string, statement string) []string {
	for i, existing := range statements {
		if existing == statement {
			copy(statements[i:], statements[i+1:])
			statements = statements[:len(statements)-1]
			break
		}
	}
	statements = append(statements, statement)
	if len(statements) > MaxEntries {
		statements = statements[len(statements)-MaxEntries:]
	}
	return statements
}

func (h *History) Entries() []string {
	if h == nil || len(h.statements) == 0 {
		return nil
	}
	return append([]string(nil), h.statements...)
}

func (h *History) Latest() (string, bool) {
	if h == nil || len(h.statements) == 0 {
		return "", false
	}
	return h.statements[len(h.statements)-1], true
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

type fileStore struct {
	path string
}

func (s fileStore) Save(statements []string) (returnErr error) {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("create history dir: %w", err)
	}
	owner, err := filelock.Acquire(historyLockPath(s.path))
	if err != nil {
		return fmt.Errorf("acquire History lock: %w", err)
	}
	defer func() {
		if err := owner.Release(); err != nil && returnErr == nil {
			returnErr = fmt.Errorf("release History lock: %w", err)
		}
	}()

	persisted, err := loadStatements(s.path)
	if err != nil {
		return err
	}
	for _, statement := range statements {
		persisted = appendStatement(persisted, statement)
	}
	data, err := json.Marshal(persisted)
	if err != nil {
		return fmt.Errorf("marshal history: %w", err)
	}
	data = append(data, '\n')

	temporary, err := os.CreateTemp(filepath.Dir(s.path), ".history-*.tmp")
	if err != nil {
		return fmt.Errorf("create history temp file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)

	if err := temporary.Chmod(0o644); err != nil {
		temporary.Close()
		return fmt.Errorf("set history temp file permissions: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return fmt.Errorf("write history temp file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close history temp file: %w", err)
	}
	if err := os.Rename(temporaryPath, s.path); err != nil {
		return fmt.Errorf("replace history file: %w", err)
	}
	return nil
}

func historyLockPath(path string) string {
	return filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".lock")
}
