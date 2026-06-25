package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adwinying/sqlcery/internal/app"
	"github.com/adwinying/sqlcery/internal/config"
	"github.com/adwinying/sqlcery/internal/db"
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

func TestRunZeroArgsStartsTUI(t *testing.T) {
	workingDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	started := false
	err := runWithDependencies(nil, func() (string, error) { return workingDir, nil }, runDependencies{
		open: db.Open,
		start: func(_ context.Context, _ string, _ config.ResolvedConnection, opts app.RunOptions) error {
			started = true
			// Verify that an open func was injected (not nil).
			if opts.Open == nil {
				t.Fatal("opts.Open = nil, want injected open func")
			}
			return nil
		},
	})
	if err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if !started {
		t.Fatal("run() did not call start, want TUI started for zero-arg launch")
	}
}

func TestRunWithExplicitConnectionStringPassesAutoConnectTarget(t *testing.T) {
	workingDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	var gotOpts app.RunOptions
	err := runWithDependencies(
		[]string{"sqlite::memory:"},
		func() (string, error) { return workingDir, nil },
		runDependencies{
			open: db.Open,
			start: func(_ context.Context, _ string, _ config.ResolvedConnection, opts app.RunOptions) error {
				gotOpts = opts
				return nil
			},
		},
	)
	if err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if got, want := gotOpts.AutoConnectTarget.Connection.Type, "sqlite"; got != want {
		t.Fatalf("opts.AutoConnectTarget.Connection.Type = %q, want %q", got, want)
	}
}

func TestRunWithExplicitNamedConnectionPassesAutoConnectTarget(t *testing.T) {
	configHome := t.TempDir()
	workingDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	globalDir := filepath.Join(configHome, "sqlcery")
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	connectionsPath := filepath.Join(globalDir, "connections.toml")
	contents := "[connection.local]\ntype = \"sqlite\"\ndatabase = \":memory:\"\n"
	if err := os.WriteFile(connectionsPath, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	var gotAutoConnect config.ResolvedConnection
	err := runWithDependencies(
		[]string{"local"},
		func() (string, error) { return workingDir, nil },
		runDependencies{
			open: db.Open,
			start: func(_ context.Context, _ string, _ config.ResolvedConnection, opts app.RunOptions) error {
				gotAutoConnect = opts.AutoConnectTarget
				return nil
			},
		},
	)
	if err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if got, want := gotAutoConnect.Name, "local"; got != want {
		t.Fatalf("autoConnectTarget.Name = %q, want %q", got, want)
	}
	if got, want := gotAutoConnect.Connection.Type, "sqlite"; got != want {
		t.Fatalf("autoConnectTarget.Connection.Type = %q, want %q", got, want)
	}
}
