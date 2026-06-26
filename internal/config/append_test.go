package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---- tomlEscapeValue -------------------------------------------------------

func TestTomlEscapeValue(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "plain text unchanged", input: "hello", want: "hello"},
		{name: "backslash doubled", input: `a\b`, want: `a\\b`},
		{name: "double quote escaped", input: `say "hi"`, want: `say \"hi\"`},
		{name: "newline escaped", input: "line1\nline2", want: `line1\nline2`},
		{name: "tab escaped", input: "col1\tcol2", want: `col1\tcol2`},
		{name: "carriage return escaped", input: "word\rend", want: `word\rend`},
		{name: "control char 0x01 as unicode", input: "\x01", want: "\\u0001"},
		{name: "control char 0x1F as unicode", input: "\x1f", want: "\\u001F"},
		{name: "control char 0x00 as unicode", input: "\x00", want: "\\u0000"},
		// mixed: actual quote + backslash + newline all escaped together
		{name: "mixed: quote backslash newline", input: "\"a\\\nb\"", want: `\"a\\\nb\"`},
		{name: "normal unicode passthrough", input: "café", want: "café"},
		{name: "empty string", input: "", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tomlEscapeValue(tt.input)
			if got != tt.want {
				t.Fatalf("tomlEscapeValue(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// ---- isBareKey / tomlTableKey ----------------------------------------------

func TestIsBareKey(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{name: "simple letters", input: "orders", want: true},
		{name: "letters and digits", input: "db01", want: true},
		{name: "underscore", input: "my_db", want: true},
		{name: "hyphen", input: "my-db", want: true},
		{name: "uppercase", input: "MyDB", want: true},
		{name: "at sign not bare-safe", input: "db@host", want: false},
		{name: "space not bare-safe", input: "my db", want: false},
		{name: "dot not bare-safe", input: "a.b", want: false},
		{name: "empty string not bare-safe", input: "", want: false},
		{name: "DSN-style name", input: "warehouse@prod-db", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isBareKey(tt.input)
			if got != tt.want {
				t.Fatalf("isBareKey(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestTomlTableKey(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "bare-safe emits bare key", input: "orders", want: "orders"},
		{name: "hyphenated bare key", input: "my-db", want: "my-db"},
		{name: "at sign quoted", input: "db@host", want: `"db@host"`},
		{name: "space quoted", input: "my db", want: `"my db"`},
		{name: "dot quoted", input: "a.b", want: `"a.b"`},
		{name: "DSN name quoted", input: "warehouse@prod", want: `"warehouse@prod"`},
		{name: "name with quote char", input: "a\"b", want: `"a\"b"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tomlTableKey(tt.input)
			if got != tt.want {
				t.Fatalf("tomlTableKey(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// ---- helpers ---------------------------------------------------------------

func setupConnTestDirs(t *testing.T) (workingDir string) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	return t.TempDir()
}

// ---- AppendConnection round-trips ------------------------------------------

func TestAppendConnectionRoundTrip(t *testing.T) {
	tests := []struct {
		label string
		key   string // connection name (map key + table-key name)
		conn  Connection
	}{
		{
			label: "sqlite",
			key:   "localdb",
			conn: Connection{
				Type:     "sqlite",
				Database: "tmp/sqlcery.db",
			},
		},
		{
			// password contains characters that need escaping: @, ", \, n
			label: "postgres with password",
			key:   "analytics",
			conn: Connection{
				Type:     "postgres",
				Host:     "db.prod",
				Port:     5432,
				Database: "warehouse",
				Username: "app",
				Password: `p@ss"word\n`,
			},
		},
		{
			// @ in name → quoted key: [connection."warehouse@prod-db"]
			label: "name needing quoted key",
			key:   "warehouse@prod-db",
			conn: Connection{
				Type:     "sqlite",
				Database: "data.db",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			workingDir := setupConnTestDirs(t)
			targetPath := filepath.Join(workingDir, ConnectionsFileName)

			if err := AppendConnection(targetPath, tt.key, tt.conn); err != nil {
				t.Fatalf("AppendConnection() error = %v", err)
			}

			result, err := LoadConnections[Connections](workingDir)
			if err != nil {
				t.Fatalf("LoadConnections() error = %v", err)
			}

			got, ok := result.Value.Connection[tt.key]
			if !ok {
				t.Fatalf("LoadConnections(): connection %q missing from decoded result", tt.key)
			}

			if got.Type != tt.conn.Type {
				t.Errorf("Type: got %q, want %q", got.Type, tt.conn.Type)
			}
			if got.Host != tt.conn.Host {
				t.Errorf("Host: got %q, want %q", got.Host, tt.conn.Host)
			}
			if got.Port != tt.conn.Port {
				t.Errorf("Port: got %d, want %d", got.Port, tt.conn.Port)
			}
			if got.Database != tt.conn.Database {
				t.Errorf("Database: got %q, want %q", got.Database, tt.conn.Database)
			}
			if got.Username != tt.conn.Username {
				t.Errorf("Username: got %q, want %q", got.Username, tt.conn.Username)
			}
			if got.Password != tt.conn.Password {
				t.Errorf("Password: got %q, want %q", got.Password, tt.conn.Password)
			}
			if got.SSHHost != tt.conn.SSHHost {
				t.Errorf("SSHHost: got %q, want %q", got.SSHHost, tt.conn.SSHHost)
			}
		})
	}
}

// ---- AppendConnection preserves prior bytes ---------------------------------

func TestAppendConnectionPreservesPriorBytes(t *testing.T) {
	workingDir := setupConnTestDirs(t)
	targetPath := filepath.Join(workingDir, ConnectionsFileName)

	// Write an initial file with a comment and an existing connection.
	initial := "# My connections\n# Hand-crafted config\n\n[connection.foo]\ntype = \"sqlite\"\ndatabase = \"foo.db\"\n"
	if err := os.WriteFile(targetPath, []byte(initial), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if err := AppendConnection(targetPath, "bar", Connection{Type: "sqlite", Database: "bar.db"}); err != nil {
		t.Fatalf("AppendConnection() error = %v", err)
	}

	content, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	// Original bytes must be present verbatim at the start of the file.
	if !strings.HasPrefix(string(content), initial) {
		t.Fatalf("file does not start with original bytes:\ngot:  %q\nwant prefix: %q", string(content), initial)
	}

	// Both connections must decode correctly.
	result, err := LoadConnections[Connections](workingDir)
	if err != nil {
		t.Fatalf("LoadConnections() error = %v", err)
	}
	if _, ok := result.Value.Connection["foo"]; !ok {
		t.Fatal("original connection \"foo\" missing after append")
	}
	got, ok := result.Value.Connection["bar"]
	if !ok {
		t.Fatal("appended connection \"bar\" missing")
	}
	if got.Database != "bar.db" {
		t.Fatalf("bar.Database = %q, want %q", got.Database, "bar.db")
	}
}

// ---- AppendConnection adds separator newline when file lacks trailing newline

func TestAppendConnectionAddsNewlineBeforeBlock(t *testing.T) {
	workingDir := setupConnTestDirs(t)
	targetPath := filepath.Join(workingDir, ConnectionsFileName)

	// Write content without trailing newline.
	if err := os.WriteFile(targetPath, []byte("[connection.alpha]\ntype = \"sqlite\"\ndatabase = \"a.db\""), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if err := AppendConnection(targetPath, "beta", Connection{Type: "sqlite", Database: "b.db"}); err != nil {
		t.Fatalf("AppendConnection() error = %v", err)
	}

	result, err := LoadConnections[Connections](workingDir)
	if err != nil {
		t.Fatalf("LoadConnections() error = %v", err)
	}
	if _, ok := result.Value.Connection["alpha"]; !ok {
		t.Fatal("connection \"alpha\" missing")
	}
	if _, ok := result.Value.Connection["beta"]; !ok {
		t.Fatal("connection \"beta\" missing")
	}
}

// ---- AppendConnection creates missing parent directories -------------------

func TestAppendConnectionMkdirAll(t *testing.T) {
	base := t.TempDir()
	// Nested path whose intermediate directories don't yet exist.
	targetPath := filepath.Join(base, "level1", "level2", ConnectionsFileName)

	if err := AppendConnection(targetPath, "new", Connection{Type: "sqlite", Database: "test.db"}); err != nil {
		t.Fatalf("AppendConnection() error = %v", err)
	}

	if _, err := os.Stat(targetPath); err != nil {
		t.Fatalf("file not created after MkdirAll: %v", err)
	}
}

// ---- AppendConnection surfaces write errors --------------------------------

func TestAppendConnectionWriteFailure(t *testing.T) {
	base := t.TempDir()
	// Create a regular file where the parent directory is expected.
	obstacle := filepath.Join(base, "obstacle")
	if err := os.WriteFile(obstacle, []byte(""), 0o644); err != nil {
		t.Fatalf("setup WriteFile() error = %v", err)
	}

	// target's Dir is the regular file "obstacle" — MkdirAll will fail.
	target := filepath.Join(obstacle, ConnectionsFileName)
	err := AppendConnection(target, "x", Connection{Type: "sqlite", Database: "x.db"})
	if err == nil {
		t.Fatal("AppendConnection() error = nil, want non-nil error for unwritable path")
	}
}

// ---- ValidatePort ----------------------------------------------------------

func TestValidatePort(t *testing.T) {
	tests := []struct {
		name    string
		port    int
		wantErr bool
	}{
		{name: "zero is invalid", port: 0, wantErr: true},
		{name: "one is valid", port: 1, wantErr: false},
		{name: "typical postgres port", port: 5432, wantErr: false},
		{name: "max port", port: 65535, wantErr: false},
		{name: "above max is invalid", port: 65536, wantErr: true},
		{name: "negative is invalid", port: -1, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePort(tt.port)
			if tt.wantErr && err == nil {
				t.Fatalf("ValidatePort(%d) = nil, want error", tt.port)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("ValidatePort(%d) = %v, want nil", tt.port, err)
			}
			if tt.wantErr && err != nil {
				if got := err.Error(); !strings.Contains(got, "port must be between 1 and 65535") {
					t.Fatalf("ValidatePort(%d) error = %q, want to contain %q", tt.port, got, "port must be between 1 and 65535")
				}
			}
		})
	}
}
