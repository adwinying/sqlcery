package main

import (
	"context"
	"errors"
	"net"
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

func TestRunFormatsOpenErrorsForTerminalUsers(t *testing.T) {
	workingDir := t.TempDir()
	err := runWithDependencies([]string{"postgres://app:secret@db.example.com:5432/warehouse"}, func() (string, error) { return workingDir, nil }, runDependencies{
		open: func(context.Context, config.Connection) (*db.SQLAdapter, error) {
			return nil, &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}
		},
		start: func(context.Context, app.Session) error {
			t.Fatal("run() started app, want open error")
			return nil
		},
	})
	if err == nil {
		t.Fatal("run() error = nil, want error")
	}

	if got, want := err.Error(), "Network error while reaching the database. Check the host, port, SSH tunnel, or VPN."; !strings.Contains(got, want) {
		t.Fatalf("run() error = %q, want to contain %q", got, want)
	}

	if got, want := err.Error(), "connection refused"; !strings.Contains(got, want) {
		t.Fatalf("run() error = %q, want to contain %q", got, want)
	}
}

func TestRunOpensSQLiteConnectionAndStartsApp(t *testing.T) {
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

	started := false
	var session app.Session

	err := runWithDependencies(nil, func() (string, error) { return workingDir, nil }, runDependencies{
		open: db.Open,
		start: func(ctx context.Context, gotSession app.Session) error {
			started = true
			session = gotSession

			var value int
			if err := gotSession.Adapter.QueryRowContext(ctx, "select 1").Scan(&value); err != nil {
				return err
			}

			if got, want := value, 1; got != want {
				t.Fatalf("QueryRowContext() value = %d, want %d", got, want)
			}

			return nil
		},
	})
	if err != nil {
		t.Fatalf("run() error = %v", err)
	}

	if !started {
		t.Fatal("run() did not start app")
	}

	if got, want := session.ConnectionType, "sqlite"; got != want {
		t.Fatalf("session.ConnectionType = %q, want %q", got, want)
	}

	if _, err := os.Stat(databasePath); err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
}

func TestRunWithoutConfiguredConnectionDoesNotStartApp(t *testing.T) {
	workingDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	started := false
	err := runWithDependencies(nil, func() (string, error) { return workingDir, nil }, runDependencies{
		open: db.Open,
		start: func(context.Context, app.Session) error {
			started = true
			return nil
		},
	})
	if err != nil {
		t.Fatalf("run() error = %v", err)
	}

	if started {
		t.Fatal("run() started app without a resolved connection")
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
