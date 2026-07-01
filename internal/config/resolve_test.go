package config

import (
	"crypto/sha256"
	"encoding/hex"
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

	if got, want := resolved.Connection.Host, "db.example.com"; got != want {
		t.Fatalf("resolved.Connection.Host = %q, want %q", got, want)
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

	if got, want := resolved.Connection.Host, "db.example.com"; got != want {
		t.Fatalf("resolved.Connection.Host = %q, want %q", got, want)
	}

	if got, want := resolved.Connection.Port, 5433; got != want {
		t.Fatalf("resolved.Connection.Port = %d, want %d", got, want)
	}

	if got, want := resolved.Connection.Database, "warehouse"; got != want {
		t.Fatalf("resolved.Connection.Database = %q, want %q", got, want)
	}

	if got, want := resolved.Connection.Username, "app"; got != want {
		t.Fatalf("resolved.Connection.Username = %q, want %q", got, want)
	}

	if got, want := resolved.Connection.Password, "secret"; got != want {
		t.Fatalf("resolved.Connection.Password = %q, want %q", got, want)
	}
}

func TestResolveConnectionReferenceIdentitySeparatesOriginAndName(t *testing.T) {
	connection := Connection{Type: "sqlite", Database: "same.db"}
	resolve := func(name, origin string) ResolvedConnection {
		t.Helper()
		connection.Origin = origin
		resolved, err := ResolveConnectionReference(Connections{Connection: map[string]Connection{name: connection}}, name)
		if err != nil {
			t.Fatalf("ResolveConnectionReference() error = %v", err)
		}
		return resolved
	}

	global := resolve("analytics", "/config/sqlcery/connections.toml")
	project := resolve("analytics", "/work/project/connections.toml")
	otherName := resolve("reporting", "/config/sqlcery/connections.toml")

	if global.Identity == "" {
		t.Fatal("global.Identity = empty")
	}
	if global.Identity == project.Identity {
		t.Fatal("same name from global and project origins has the same identity")
	}
	if global.Identity == otherName.Identity {
		t.Fatal("different names from the same origin have the same identity")
	}
	if strings.Contains(string(global.Identity), global.Name) || strings.Contains(string(global.Identity), global.Connection.Origin) {
		t.Fatalf("global.Identity = %q, want opaque value", global.Identity)
	}
}

func TestDirectConnectionStringIdentityIsStableDomainSeparatedAndSecret(t *testing.T) {
	raw := "postgres://app:secret@db.example.com/warehouse?sslmode=require"
	first, ok, err := ParseConnectionString(raw)
	if err != nil || !ok {
		t.Fatalf("ParseConnectionString() = (%#v, %v, %v), want accepted", first, ok, err)
	}
	second, _, err := ParseConnectionString(raw)
	if err != nil {
		t.Fatalf("ParseConnectionString(second) error = %v", err)
	}

	wantSum := sha256.Sum256([]byte("sqlcery/connection-identity/connection-string/v1\x00" + raw))
	want := hex.EncodeToString(wantSum[:])
	if got := string(first.Identity); got != want {
		t.Fatalf("first.Identity = %q, want %q", got, want)
	}
	if first.Identity != second.Identity {
		t.Fatalf("identities differ for exact accepted string: %q != %q", first.Identity, second.Identity)
	}
	if strings.Contains(string(first.Identity), "secret") || strings.Contains(string(first.Identity), raw) {
		t.Fatalf("first.Identity = %q, want no raw connection string material", first.Identity)
	}

	different, _, err := ParseConnectionString("postgres://app:secret@db.example.com/warehouse?sslmode=disable")
	if err != nil {
		t.Fatalf("ParseConnectionString(different) error = %v", err)
	}
	if first.Identity == different.Identity {
		t.Fatal("different accepted connection strings have the same identity")
	}
}

func TestParseConnectionString(t *testing.T) {
	tests := []struct {
		name         string
		raw          string
		wantOK       bool
		wantType     string
		wantHost     string
		wantPort     int
		wantDatabase string
		wantUsername string
		wantPassword string
		wantErr      string
	}{
		{
			name:         "postgres alias",
			raw:          "postgresql://app:secret@db.example.com:5433/warehouse",
			wantOK:       true,
			wantType:     "postgres",
			wantHost:     "db.example.com",
			wantPort:     5433,
			wantDatabase: "warehouse",
			wantUsername: "app",
			wantPassword: "secret",
		},
		{
			name:         "mysql default port",
			raw:          "mysql://root:secret@db.example.com/sqlcery",
			wantOK:       true,
			wantType:     "mysql",
			wantHost:     "db.example.com",
			wantPort:     3306,
			wantDatabase: "sqlcery",
			wantUsername: "root",
			wantPassword: "secret",
		},
		{
			name:         "sqlite relative path",
			raw:          "sqlite:tmp/sqlcery.db",
			wantOK:       true,
			wantType:     "sqlite",
			wantDatabase: "tmp/sqlcery.db",
		},
		{
			name:         "sqlite memory database",
			raw:          "sqlite:///:memory:",
			wantOK:       true,
			wantType:     "sqlite",
			wantDatabase: ":memory:",
		},
		{
			name:    "unsupported scheme",
			raw:     "sqlserver://db.example.com/warehouse",
			wantOK:  true,
			wantErr: `invalid connection string: unsupported connection string scheme "sqlserver"`,
		},
		{
			name:   "not a connection string",
			raw:    "analytics",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolved, ok, err := ParseConnectionString(tt.raw)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}

			if tt.wantErr != "" {
				if err == nil {
					t.Fatal("ParseConnectionString() error = nil, want error")
				}
				if got := err.Error(); got != tt.wantErr {
					t.Fatalf("ParseConnectionString() error = %q, want %q", got, tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Fatalf("ParseConnectionString() error = %v", err)
			}
			if !tt.wantOK {
				return
			}

			if got, want := resolved.Raw, tt.raw; got != want {
				t.Fatalf("resolved.Raw = %q, want %q", got, want)
			}
			if got, want := resolved.Connection.Type, tt.wantType; got != want {
				t.Fatalf("resolved.Connection.Type = %q, want %q", got, want)
			}

			switch tt.wantType {
			case "postgres":
				if got, want := resolved.Connection.Host, tt.wantHost; got != want {
					t.Fatalf("resolved.Connection.Host = %q, want %q", got, want)
				}
				if got, want := resolved.Connection.Port, tt.wantPort; got != want {
					t.Fatalf("resolved.Connection.Port = %d, want %d", got, want)
				}
				if got, want := resolved.Connection.Database, tt.wantDatabase; got != want {
					t.Fatalf("resolved.Connection.Database = %q, want %q", got, want)
				}
				if got, want := resolved.Connection.Username, tt.wantUsername; got != want {
					t.Fatalf("resolved.Connection.Username = %q, want %q", got, want)
				}
				if got, want := resolved.Connection.Password, tt.wantPassword; got != want {
					t.Fatalf("resolved.Connection.Password = %q, want %q", got, want)
				}
			case "mysql":
				if got, want := resolved.Connection.Host, tt.wantHost; got != want {
					t.Fatalf("resolved.Connection.Host = %q, want %q", got, want)
				}
				if got, want := resolved.Connection.Port, tt.wantPort; got != want {
					t.Fatalf("resolved.Connection.Port = %d, want %d", got, want)
				}
				if got, want := resolved.Connection.Database, tt.wantDatabase; got != want {
					t.Fatalf("resolved.Connection.Database = %q, want %q", got, want)
				}
				if got, want := resolved.Connection.Username, tt.wantUsername; got != want {
					t.Fatalf("resolved.Connection.Username = %q, want %q", got, want)
				}
				if got, want := resolved.Connection.Password, tt.wantPassword; got != want {
					t.Fatalf("resolved.Connection.Password = %q, want %q", got, want)
				}
			case "sqlite":
				if got, want := resolved.Connection.Database, tt.wantDatabase; got != want {
					t.Fatalf("resolved.Connection.Database = %q, want %q", got, want)
				}
			}
		})
	}
}

func TestResolveCLIConnectionZeroArgsReturnsEmptyTarget(t *testing.T) {
	workingDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	// Zero args: no auto-connect target — the Connection Picker will be shown.
	resolved, err := ResolveCLIConnection(workingDir, nil)
	if err != nil {
		t.Fatalf("ResolveCLIConnection() error = %v", err)
	}

	if got := resolved.Connection.Type; got != "" {
		t.Fatalf("resolved.Connection.Type = %q, want empty (no auto-connect)", got)
	}

	if got := resolved.Name; got != "" {
		t.Fatalf("resolved.Name = %q, want empty (no auto-connect)", got)
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
