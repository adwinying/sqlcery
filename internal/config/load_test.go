package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestDiscoverPaths(t *testing.T) {
	configHome := t.TempDir()
	workingDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)

	paths, err := discoverPaths(
		func() (string, error) { return configHome, nil },
		func() (string, error) { return "", nil },
		workingDir,
		FileName,
	)
	if err != nil {
		t.Fatalf("discoverPaths() error = %v", err)
	}

	if got, want := paths.Global, filepath.Join(configHome, DirName, FileName); got != want {
		t.Fatalf("paths.Global = %q, want %q", got, want)
	}

	if got, want := paths.Local, filepath.Join(workingDir, FileName); got != want {
		t.Fatalf("paths.Local = %q, want %q", got, want)
	}
}

func TestDiscoverConnectionPaths(t *testing.T) {
	configHome := t.TempDir()
	workingDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)

	paths, err := DiscoverConnectionPaths(workingDir)
	if err != nil {
		t.Fatalf("DiscoverConnectionPaths() error = %v", err)
	}

	if got, want := paths.Global, filepath.Join(configHome, DirName, ConnectionsFileName); got != want {
		t.Fatalf("paths.Global = %q, want %q", got, want)
	}

	if got, want := paths.Local, filepath.Join(workingDir, ConnectionsFileName); got != want {
		t.Fatalf("paths.Local = %q, want %q", got, want)
	}
}

func TestLoadLayersGlobalAndLocal(t *testing.T) {
	configHome := t.TempDir()
	workingDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)

	globalDir := filepath.Join(configHome, DirName)
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	globalPath := filepath.Join(globalDir, FileName)
	if err := os.WriteFile(globalPath, []byte("mouse_disabled = false\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(global) error = %v", err)
	}

	localPath := filepath.Join(workingDir, FileName)
	if err := os.WriteFile(localPath, []byte("mouse_disabled = true\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(local) error = %v", err)
	}

	result, err := Load[Config](workingDir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if got, want := result.Paths.Global, globalPath; got != want {
		t.Fatalf("result.Paths.Global = %q, want %q", got, want)
	}

	if got, want := result.Paths.Local, localPath; got != want {
		t.Fatalf("result.Paths.Local = %q, want %q", got, want)
	}

	if got, want := result.Loaded, []string{globalPath, localPath}; !reflect.DeepEqual(got, want) {
		t.Fatalf("result.Loaded = %#v, want %#v", got, want)
	}

	// Local file takes precedence over global; mouse_disabled = true is the local value.
	if got, want := result.Value.MouseDisabled, true; got != want {
		t.Fatalf("result.Value.MouseDisabled = %v, want %v", got, want)
	}
}

func TestLoadWithoutConfigFiles(t *testing.T) {
	workingDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	result, err := Load[Config](workingDir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if len(result.Loaded) != 0 {
		t.Fatalf("len(result.Loaded) = %d, want 0", len(result.Loaded))
	}

	if !reflect.DeepEqual(result.Value, Config{}) {
		t.Fatalf("result.Value = %#v, want zero value", result.Value)
	}
}

func TestLoadDecodesMouseDisabled(t *testing.T) {
	t.Run("mouse_disabled = true", func(t *testing.T) {
		workingDir := t.TempDir()
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())

		localPath := filepath.Join(workingDir, FileName)
		if err := os.WriteFile(localPath, []byte("mouse_disabled = true\n"), 0o644); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}

		result, err := Load[Config](workingDir)
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}

		if got, want := result.Value.MouseDisabled, true; got != want {
			t.Fatalf("result.Value.MouseDisabled = %v, want %v", got, want)
		}
	})

	t.Run("mouse_disabled omitted yields false", func(t *testing.T) {
		workingDir := t.TempDir()
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())

		result, err := Load[Config](workingDir)
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}

		if got, want := result.Value.MouseDisabled, false; got != want {
			t.Fatalf("result.Value.MouseDisabled = %v, want %v", got, want)
		}
	})
}

func TestLoadReturnsDecodeErrors(t *testing.T) {
	configHome := t.TempDir()
	workingDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)

	globalDir := filepath.Join(configHome, DirName)
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	localPath := filepath.Join(workingDir, FileName)
	if err := os.WriteFile(localPath, []byte("theme = [\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := Load[Config](workingDir)
	if err == nil {
		t.Fatal("Load() error = nil, want decode error")
	}

	if got, want := err.Error(), "decode "+localPath+":"; len(got) < len(want) || got[:len(want)] != want {
		t.Fatalf("Load() error = %q, want prefix %q", got, want)
	}

	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("Load() error = %v, want errors.Is(..., ErrInvalidConfig)", err)
	}

	var invalidErr *InvalidConfigError
	if !errors.As(err, &invalidErr) {
		t.Fatalf("Load() error = %v, want InvalidConfigError", err)
	}

	if got, want := invalidErr.Op, "decode"; got != want {
		t.Fatalf("invalidErr.Op = %q, want %q", got, want)
	}

	if got, want := invalidErr.Path, localPath; got != want {
		t.Fatalf("invalidErr.Path = %q, want %q", got, want)
	}
}

func TestLoadConfigReturnsValidationErrors(t *testing.T) {
	// Config.Validate() now always returns nil (connection field removed).
	// Verify that valid TOML with mouse_disabled loads without error.
	workingDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	localPath := filepath.Join(workingDir, FileName)
	if err := os.WriteFile(localPath, []byte("mouse_disabled = true\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	result, err := Load[Config](workingDir)
	if err != nil {
		t.Fatalf("Load() error = %v, want nil (Config.Validate always succeeds)", err)
	}

	if got, want := result.Value.MouseDisabled, true; got != want {
		t.Fatalf("result.Value.MouseDisabled = %v, want %v", got, want)
	}
}

func TestLoadConnectionsLayersGlobalAndLocal(t *testing.T) {
	configHome := t.TempDir()
	workingDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)

	globalDir := filepath.Join(configHome, DirName)
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	globalPath := filepath.Join(globalDir, ConnectionsFileName)
	if err := os.WriteFile(globalPath, []byte("[connection.analytics]\ntype = \"postgres\"\nhost = \"global-db\"\nport = 5432\ndatabase = \"warehouse\"\nusername = \"root\"\n[connection.globalonly]\ntype = \"sqlite\"\ndatabase = \"global.db\"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(global) error = %v", err)
	}

	localPath := filepath.Join(workingDir, ConnectionsFileName)
	if err := os.WriteFile(localPath, []byte("[connection.analytics]\ntype = \"postgres\"\nhost = \"local-db\"\nport = 5432\ndatabase = \"warehouse\"\nusername = \"app\"\n[connection.cache]\ntype = \"mysql\"\nhost = \"cache-db\"\nport = 3306\ndatabase = \"cache\"\nusername = \"cache-user\"\n[connection.local]\ntype = \"sqlite\"\ndatabase = \"tmp/sqlcery.db\"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(local) error = %v", err)
	}

	result, err := LoadConnections[Connections](workingDir)
	if err != nil {
		t.Fatalf("LoadConnections() error = %v", err)
	}

	if got, want := result.Paths.Global, globalPath; got != want {
		t.Fatalf("result.Paths.Global = %q, want %q", got, want)
	}

	if got, want := result.Paths.Local, localPath; got != want {
		t.Fatalf("result.Paths.Local = %q, want %q", got, want)
	}

	if got, want := result.Loaded, []string{globalPath, localPath}; !reflect.DeepEqual(got, want) {
		t.Fatalf("result.Loaded = %#v, want %#v", got, want)
	}

	analytics, ok := result.Value.Connection["analytics"]
	if !ok {
		t.Fatal("result.Value.Connection[\"analytics\"] missing")
	}

	if got, want := analytics.Type, "postgres"; got != want {
		t.Fatalf("analytics.Type = %q, want %q", got, want)
	}

	if got, want := analytics.Host, "local-db"; got != want {
		t.Fatalf("analytics.Host = %q, want %q", got, want)
	}

	if got, want := analytics.Port, 5432; got != want {
		t.Fatalf("analytics.Port = %d, want %d", got, want)
	}

	if got, want := analytics.Database, "warehouse"; got != want {
		t.Fatalf("analytics.Database = %q, want %q", got, want)
	}

	if got, want := analytics.Username, "app"; got != want {
		t.Fatalf("analytics.Username = %q, want %q", got, want)
	}

	canonicalLocalPath, err := filepath.EvalSymlinks(localPath)
	if err != nil {
		t.Fatalf("EvalSymlinks(localPath) error = %v", err)
	}
	if got, want := analytics.Origin, canonicalLocalPath; got != want {
		t.Fatalf("analytics.Origin = %q, want %q", got, want)
	}

	cache, ok := result.Value.Connection["cache"]
	if !ok {
		t.Fatal("result.Value.Connection[\"cache\"] missing")
	}

	if got, want := cache.Type, "mysql"; got != want {
		t.Fatalf("cache.Type = %q, want %q", got, want)
	}

	if got, want := cache.Host, "cache-db"; got != want {
		t.Fatalf("cache.Host = %q, want %q", got, want)
	}

	if got, want := cache.Port, 3306; got != want {
		t.Fatalf("cache.Port = %d, want %d", got, want)
	}

	if got, want := cache.Database, "cache"; got != want {
		t.Fatalf("cache.Database = %q, want %q", got, want)
	}

	if got, want := cache.Username, "cache-user"; got != want {
		t.Fatalf("cache.Username = %q, want %q", got, want)
	}

	if got, want := cache.Origin, canonicalLocalPath; got != want {
		t.Fatalf("cache.Origin = %q, want %q", got, want)
	}

	canonicalGlobalPath, err := filepath.EvalSymlinks(globalPath)
	if err != nil {
		t.Fatalf("EvalSymlinks(globalPath) error = %v", err)
	}
	if got, want := result.Value.Connection["globalonly"].Origin, canonicalGlobalPath; got != want {
		t.Fatalf("globalonly.Origin = %q, want %q", got, want)
	}

	local, ok := result.Value.Connection["local"]
	if !ok {
		t.Fatal("result.Value.Connection[\"local\"] missing")
	}

	if got, want := local.Type, "sqlite"; got != want {
		t.Fatalf("local.Type = %q, want %q", got, want)
	}

	if got, want := local.Database, "tmp/sqlcery.db"; got != want {
		t.Fatalf("local.Database = %q, want %q", got, want)
	}
}

func TestLoadConnectionsReturnsValidationErrors(t *testing.T) {
	configHome := t.TempDir()
	workingDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)

	globalDir := filepath.Join(configHome, DirName)
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	localPath := filepath.Join(workingDir, ConnectionsFileName)
	if err := os.WriteFile(localPath, []byte("[connection.analytics]\ntype = \"postgres\"\nport = 5432\ndatabase = \"warehouse\"\nusername = \"app\"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := LoadConnections[Connections](workingDir)
	if err == nil {
		t.Fatal("LoadConnections() error = nil, want validation error")
	}

	if got, want := err.Error(), fmt.Sprintf("validate %s: connection \"analytics\": postgres: host is required", localPath); got != want {
		t.Fatalf("LoadConnections() error = %q, want %q", got, want)
	}

	if got, want := err.Error(), `connection "analytics": postgres: host is required`; !strings.Contains(got, want) {
		t.Fatalf("LoadConnections() error = %q, want to contain %q", got, want)
	}

	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("LoadConnections() error = %v, want errors.Is(..., ErrInvalidConfig)", err)
	}
}

func TestLoadConnectionsDecodesLifecycleOptions(t *testing.T) {
	configHome := t.TempDir()
	workingDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)

	globalDir := filepath.Join(configHome, DirName)
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	localPath := filepath.Join(workingDir, ConnectionsFileName)
	if err := os.WriteFile(localPath, []byte("[connection.analytics]\ntype = \"postgres\"\nhost = \"db\"\nport = 5432\ndatabase = \"warehouse\"\nusername = \"app\"\n[connection.analytics.lifecycle]\nconnect_timeout = \"7s\"\nhealth_check_timeout = \"1500ms\"\nmax_open_conns = 9\nmax_idle_conns = 4\nconn_max_lifetime = \"45m\"\nconn_max_idle_time = \"3m\"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	result, err := LoadConnections[Connections](workingDir)
	if err != nil {
		t.Fatalf("LoadConnections() error = %v", err)
	}

	connection := result.Value.Connection["analytics"]
	if got, want := connection.Lifecycle.ConnectTimeout.Duration(), 7*time.Second; got != want {
		t.Fatalf("ConnectTimeout = %s, want %s", got, want)
	}
	if got, want := connection.Lifecycle.HealthCheckTimeout.Duration(), 1500*time.Millisecond; got != want {
		t.Fatalf("HealthCheckTimeout = %s, want %s", got, want)
	}
	if got, want := connection.Lifecycle.MaxOpenConns, 9; got != want {
		t.Fatalf("MaxOpenConns = %d, want %d", got, want)
	}
	if got, want := connection.Lifecycle.MaxIdleConns, 4; got != want {
		t.Fatalf("MaxIdleConns = %d, want %d", got, want)
	}
	if got, want := connection.Lifecycle.ConnMaxLifetime.Duration(), 45*time.Minute; got != want {
		t.Fatalf("ConnMaxLifetime = %s, want %s", got, want)
	}
	if got, want := connection.Lifecycle.ConnMaxIdleTime.Duration(), 3*time.Minute; got != want {
		t.Fatalf("ConnMaxIdleTime = %s, want %s", got, want)
	}
}

func TestLoadConnectionsDecodesSSHHost(t *testing.T) {
	configHome := t.TempDir()
	workingDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)

	globalDir := filepath.Join(configHome, DirName)
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	localPath := filepath.Join(workingDir, ConnectionsFileName)
	if err := os.WriteFile(localPath, []byte("[connection.analytics]\ntype = \"postgres\"\nssh_host = \"bastion\"\nhost = \"db.internal\"\nport = 5432\ndatabase = \"warehouse\"\nusername = \"app\"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	result, err := LoadConnections[Connections](workingDir)
	if err != nil {
		t.Fatalf("LoadConnections() error = %v", err)
	}

	connection := result.Value.Connection["analytics"]
	if got, want := connection.SSHHost, "bastion"; got != want {
		t.Fatalf("SSHHost = %q, want %q", got, want)
	}
}

func TestLoadConnectionsSupportsFlatMySQLFields(t *testing.T) {
	configHome := t.TempDir()
	workingDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)

	globalDir := filepath.Join(configHome, DirName)
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	localPath := filepath.Join(workingDir, ConnectionsFileName)
	if err := os.WriteFile(localPath, []byte("[connection.mysql]\ntype = \"mysql\"\nhost = \"127.0.0.1\"\nport = 3307\nusername = \"root\"\npassword = \"password\"\ndatabase = \"sqlcery\"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	result, err := LoadConnections[Connections](workingDir)
	if err != nil {
		t.Fatalf("LoadConnections() error = %v", err)
	}

	connection := result.Value.Connection["mysql"]
	if got, want := connection.Host, "127.0.0.1"; got != want {
		t.Fatalf("connection.Host = %q, want %q", got, want)
	}
	if got, want := connection.Port, 3307; got != want {
		t.Fatalf("connection.Port = %d, want %d", got, want)
	}
	if got, want := connection.Database, "sqlcery"; got != want {
		t.Fatalf("connection.Database = %q, want %q", got, want)
	}
	if got, want := connection.Username, "root"; got != want {
		t.Fatalf("connection.Username = %q, want %q", got, want)
	}
	if got, want := connection.Password, "password"; got != want {
		t.Fatalf("connection.Password = %q, want %q", got, want)
	}
}

func TestConnectionValidate(t *testing.T) {
	tests := []struct {
		name    string
		value   Connection
		wantErr string
	}{
		{
			name: "sqlite accepts database path",
			value: Connection{
				Type:     "sqlite",
				Database: filepath.Join("tmp", "sqlcery.db"),
			},
		},
		{
			name: "postgres requires host",
			value: Connection{
				Type:     "postgres",
				Port:     5432,
				Database: "warehouse",
				Username: "app",
			},
			wantErr: "host is required",
		},
		{
			name: "mysql requires valid port",
			value: Connection{
				Type:     "mysql",
				Host:     "db",
				Port:     0,
				Database: "warehouse",
				Username: "app",
			},
			wantErr: "port must be between 1 and 65535",
		},
		{
			name: "unsupported type fails",
			value: Connection{
				Type: "sqlserver",
			},
			wantErr: "type must be one of sqlite, postgres, mysql",
		},
		{
			name: "sqlite rejects ssh host",
			value: Connection{
				Type:     "sqlite",
				SSHHost:  "bastion",
				Database: filepath.Join("tmp", "sqlcery.db"),
			},
			wantErr: "ssh_host is only supported for postgres and mysql",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.value.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() error = %v", err)
				}
				return
			}

			if err == nil {
				t.Fatal("Validate() error = nil, want validation error")
			}

			if got := err.Error(); !strings.Contains(got, tt.wantErr) {
				t.Fatalf("Validate() error = %q, want to contain %q", got, tt.wantErr)
			}
		})
	}
}
