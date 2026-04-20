package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/BurntSushi/toml"
)

func TestExampleConfigFilesDecodeAndValidate(t *testing.T) {
	root := repoRoot(t)

	var appConfig Config
	decodeExampleFile(t, filepath.Join(root, "examples", "config", FileName), &appConfig)
	if err := appConfig.Validate(); err != nil {
		t.Fatalf("Config.Validate() error = %v", err)
	}
	if got, want := appConfig.Connection, "mypgsql"; got != want {
		t.Fatalf("appConfig.Connection = %q, want %q", got, want)
	}

	var connections Connections
	decodeExampleFile(t, filepath.Join(root, "examples", "config", ConnectionsFileName), &connections)
	connections = connections.Normalized()
	if err := connections.Validate(); err != nil {
		t.Fatalf("Connections.Validate() error = %v", err)
	}

	resolved, err := ResolveConnectionReference(connections, appConfig.Connection)
	if err != nil {
		t.Fatalf("ResolveConnectionReference() error = %v", err)
	}
	if got, want := resolved.Connection.Type, "postgres"; got != want {
		t.Fatalf("resolved.Connection.Type = %q, want %q", got, want)
	}
	if got, want := resolved.Connection.SSHHost, "bastion"; got != want {
		t.Fatalf("resolved.Connection.SSHHost = %q, want %q", got, want)
	}

	legacy := connections.Connection["mysql"]
	if got, want := legacy.Host, "127.0.0.1"; got != want {
		t.Fatalf("legacy.Host = %q, want %q", got, want)
	}
	if got, want := legacy.Port, 3307; got != want {
		t.Fatalf("legacy.Port = %d, want %d", got, want)
	}
	if got, want := legacy.Database, "sqlcery"; got != want {
		t.Fatalf("legacy.Database = %q, want %q", got, want)
	}
	if got, want := legacy.Username, "root"; got != want {
		t.Fatalf("legacy.Username = %q, want %q", got, want)
	}
}

func decodeExampleFile(t *testing.T, path string, target any) {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}

	metadata, err := toml.Decode(string(data), target)
	if err != nil {
		t.Fatalf("toml.Decode(%q) error = %v", path, err)
	}

	if undecoded := metadata.Undecoded(); len(undecoded) > 0 {
		t.Fatalf("%s contains undecoded TOML keys: %v", path, undecoded)
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}

	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
}
