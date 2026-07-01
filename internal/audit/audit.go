package audit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

type Outcome string

const (
	OutcomeSuccess Outcome = "success"
	OutcomeFailure Outcome = "failure"
)

type SubmittedEvent struct {
	Event              string    `json:"event"`
	ExecutionIdentity  string    `json:"execution_identity"`
	ConnectionIdentity string    `json:"connection_identity"`
	ConnectionName     string    `json:"connection_name,omitempty"`
	ConnectionOrigin   string    `json:"connection_origin"`
	Statement          string    `json:"statement"`
	Timestamp          time.Time `json:"timestamp"`
}

type ResultSummary struct {
	RowCount     *int   `json:"row_count,omitempty"`
	RowsAffected *int64 `json:"rows_affected,omitempty"`
	Message      string `json:"message,omitempty"`
	Error        string `json:"error,omitempty"`
}

type CompletedEvent struct {
	Event             string        `json:"event"`
	ExecutionIdentity string        `json:"execution_identity"`
	Timestamp         time.Time     `json:"timestamp"`
	Outcome           Outcome       `json:"outcome"`
	ResultSummary     ResultSummary `json:"result_summary"`
}

type Appender interface {
	AppendSubmitted(SubmittedEvent) error
	AppendCompleted(CompletedEvent) error
}

type Discard struct{}

func (Discard) AppendSubmitted(SubmittedEvent) error { return nil }
func (Discard) AppendCompleted(CompletedEvent) error { return nil }

type FileLog struct {
	path string
	mu   sync.Mutex
}

const DirName = "sqlcery"
const FileName = "audit.log"

func NewPersistentLog() (*FileLog, error) {
	path, err := DefaultPath()
	if err != nil {
		return nil, err
	}
	return NewFileLog(path), nil
}

func DefaultPath() (string, error) {
	dataHome := strings.TrimSpace(os.Getenv("XDG_DATA_HOME"))
	if dataHome != "" {
		if !filepath.IsAbs(dataHome) {
			return "", fmt.Errorf("XDG_DATA_HOME must be an absolute path")
		}
		return filepath.Join(dataHome, DirName, FileName), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user data dir: %w", err)
	}
	if runtime.GOOS == "windows" {
		return filepath.Join(home, "AppData", "Local", DirName, FileName), nil
	}
	return filepath.Join(home, ".local", "share", DirName, FileName), nil
}

func NewFileLog(path string) *FileLog {
	return &FileLog{path: path}
}

func (l *FileLog) AppendSubmitted(event SubmittedEvent) error { return l.append(event) }
func (l *FileLog) AppendCompleted(event CompletedEvent) error { return l.append(event) }

func (l *FileLog) append(event any) error {
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal Audit event: %w", err)
	}
	data = append(data, '\n')

	l.mu.Lock()
	defer l.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(l.path), 0o755); err != nil {
		return fmt.Errorf("create Audit directory: %w", err)
	}
	file, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open Audit Log: %w", err)
	}
	if n, err := file.Write(data); err != nil {
		_ = file.Close()
		return fmt.Errorf("append Audit event: %w", err)
	} else if n != len(data) {
		_ = file.Close()
		return fmt.Errorf("append Audit event: wrote %d of %d bytes", n, len(data))
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("sync Audit Log: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close Audit Log: %w", err)
	}
	return nil
}
