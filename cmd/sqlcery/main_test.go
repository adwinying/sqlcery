package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adwinying/sqlcery/internal/config"
)

func TestRunReturnsWorkingDirectoryErrors(t *testing.T) {
	wantErr := errors.New("boom")

	err := run(nil, func() (string, error) {
		return "", wantErr
	})
	if err == nil {
		t.Fatal("run() error = nil, want error")
	}

	if got, want := err.Error(), "resolve working directory: boom"; !strings.Contains(got, want) {
		t.Fatalf("run() error = %q, want to contain %q", got, want)
	}
}

func TestRunOpensSQLiteConnection(t *testing.T) {
	configHome := t.TempDir()
	workingDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)

	globalDir := filepath.Join(configHome, "sqlcery")
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	databasePath := filepath.Join(workingDir, "sqlcery.db")
	configPath := filepath.Join(globalDir, "sqlcery.toml")
	contents := "connection = \"sqlite:" + databasePath + "\"\n"
	if err := os.WriteFile(configPath, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if err := run(nil, func() (string, error) { return workingDir, nil }); err != nil {
		t.Fatalf("run() error = %v", err)
	}

	if _, err := os.Stat(databasePath); err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
}

func TestRunReturnsInvalidConfigError(t *testing.T) {
	configHome := t.TempDir()
	workingDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)

	globalDir := filepath.Join(configHome, "sqlcery")
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	configPath := filepath.Join(globalDir, "sqlcery.toml")
	if err := os.WriteFile(configPath, []byte("connection = \"   \"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	err := run(nil, func() (string, error) { return workingDir, nil })
	if err == nil {
		t.Fatal("run() error = nil, want error")
	}

	if !errors.Is(err, config.ErrInvalidConfig) {
		t.Fatalf("run() error = %v, want errors.Is(..., config.ErrInvalidConfig)", err)
	}

	if got, want := err.Error(), "connection must not be blank"; !strings.Contains(got, want) {
		t.Fatalf("run() error = %q, want to contain %q", got, want)
	}
}

func TestRunReturnsUnknownConnectionError(t *testing.T) {
	workingDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	err := run([]string{"missing"}, func() (string, error) { return workingDir, nil })
	if err == nil {
		t.Fatal("run() error = nil, want error")
	}

	if !errors.Is(err, config.ErrUnknownConnection) {
		t.Fatalf("run() error = %v, want errors.Is(..., config.ErrUnknownConnection)", err)
	}

	if got, want := err.Error(), `unknown connection "missing"`; !strings.Contains(got, want) {
		t.Fatalf("run() error = %q, want to contain %q", got, want)
	}
}
