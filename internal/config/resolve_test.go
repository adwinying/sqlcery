package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveCLIConnectionNamedArgument(t *testing.T) {
	configHome := t.TempDir()
	workingDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)

	globalDir := filepath.Join(configHome, DirName)
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	connectionsPath := filepath.Join(globalDir, ConnectionsFileName)
	contents := "[connection.analytics]\n" +
		"type = \"postgres\"\n" +
		"[connection.analytics.postgres]\n" +
		"host = \"db.example.com\"\n" +
		"port = 5432\n" +
		"database = \"warehouse\"\n" +
		"username = \"app\"\n"
	if err := os.WriteFile(connectionsPath, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	resolved, err := ResolveCLIConnection(workingDir, []string{"analytics"})
	if err != nil {
		t.Fatalf("ResolveCLIConnection() error = %v", err)
	}

	if got, want := resolved.Name, "analytics"; got != want {
		t.Fatalf("resolved.Name = %q, want %q", got, want)
	}

	if got, want := resolved.Raw, "analytics"; got != want {
		t.Fatalf("resolved.Raw = %q, want %q", got, want)
	}

	if got, want := resolved.Connection.Type, "postgres"; got != want {
		t.Fatalf("resolved.Connection.Type = %q, want %q", got, want)
	}

	if got, want := resolved.Connection.Postgres.Host, "db.example.com"; got != want {
		t.Fatalf("resolved.Connection.Postgres.Host = %q, want %q", got, want)
	}
}

func TestResolveCLIConnectionDirectConnectionString(t *testing.T) {
	workingDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	resolved, err := ResolveCLIConnection(workingDir, []string{"postgres://app:secret@db.example.com:5433/warehouse"})
	if err != nil {
		t.Fatalf("ResolveCLIConnection() error = %v", err)
	}

	if resolved.Name != "" {
		t.Fatalf("resolved.Name = %q, want empty", resolved.Name)
	}

	if got, want := resolved.Connection.Type, "postgres"; got != want {
		t.Fatalf("resolved.Connection.Type = %q, want %q", got, want)
	}

	if got, want := resolved.Connection.Postgres.Host, "db.example.com"; got != want {
		t.Fatalf("resolved.Connection.Postgres.Host = %q, want %q", got, want)
	}

	if got, want := resolved.Connection.Postgres.Port, 5433; got != want {
		t.Fatalf("resolved.Connection.Postgres.Port = %d, want %d", got, want)
	}

	if got, want := resolved.Connection.Postgres.Database, "warehouse"; got != want {
		t.Fatalf("resolved.Connection.Postgres.Database = %q, want %q", got, want)
	}

	if got, want := resolved.Connection.Postgres.Username, "app"; got != want {
		t.Fatalf("resolved.Connection.Postgres.Username = %q, want %q", got, want)
	}

	if got, want := resolved.Connection.Postgres.Password, "secret"; got != want {
		t.Fatalf("resolved.Connection.Postgres.Password = %q, want %q", got, want)
	}
}

func TestResolveCLIConnectionUsesConfiguredDefault(t *testing.T) {
	configHome := t.TempDir()
	workingDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)

	globalDir := filepath.Join(configHome, DirName)
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	configPath := filepath.Join(globalDir, FileName)
	if err := os.WriteFile(configPath, []byte("connection = \"sqlite:tmp/sqlcery.db\"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(config) error = %v", err)
	}

	resolved, err := ResolveCLIConnection(workingDir, nil)
	if err != nil {
		t.Fatalf("ResolveCLIConnection() error = %v", err)
	}

	if got, want := resolved.Connection.Type, "sqlite"; got != want {
		t.Fatalf("resolved.Connection.Type = %q, want %q", got, want)
	}

	if got, want := resolved.Connection.SQLite.Database, "tmp/sqlcery.db"; got != want {
		t.Fatalf("resolved.Connection.SQLite.Database = %q, want %q", got, want)
	}
}

func TestResolveCLIConnectionReturnsUnknownConnectionError(t *testing.T) {
	workingDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	_, err := ResolveCLIConnection(workingDir, []string{"missing"})
	if err == nil {
		t.Fatal("ResolveCLIConnection() error = nil, want error")
	}

	if got, want := err.Error(), `unknown connection "missing"`; !strings.Contains(got, want) {
		t.Fatalf("ResolveCLIConnection() error = %q, want to contain %q", got, want)
	}

	if !errors.Is(err, ErrUnknownConnection) {
		t.Fatalf("ResolveCLIConnection() error = %v, want errors.Is(..., ErrUnknownConnection)", err)
	}

	var unknownErr *UnknownConnectionError
	if !errors.As(err, &unknownErr) {
		t.Fatalf("ResolveCLIConnection() error = %v, want UnknownConnectionError", err)
	}

	if got, want := unknownErr.Name, "missing"; got != want {
		t.Fatalf("unknownErr.Name = %q, want %q", got, want)
	}
}

func TestResolveCLIConnectionReturnsValidationErrorForBadConnectionString(t *testing.T) {
	workingDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	_, err := ResolveCLIConnection(workingDir, []string{"postgres://app@/warehouse"})
	if err == nil {
		t.Fatal("ResolveCLIConnection() error = nil, want error")
	}

	if got, want := err.Error(), "invalid connection string: postgres: host is required"; !strings.Contains(got, want) {
		t.Fatalf("ResolveCLIConnection() error = %q, want to contain %q", got, want)
	}
}

func TestResolveCLIConnectionRejectsExtraArguments(t *testing.T) {
	workingDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	_, err := ResolveCLIConnection(workingDir, []string{"one", "two"})
	if err == nil {
		t.Fatal("ResolveCLIConnection() error = nil, want error")
	}

	if got, want := err.Error(), "expected at most 1 argument, got 2"; !strings.Contains(got, want) {
		t.Fatalf("ResolveCLIConnection() error = %q, want to contain %q", got, want)
	}
}
