package config

import (
	"testing"
	"time"
)

func TestDurationUnmarshalText(t *testing.T) {
	var value Duration
	if err := value.UnmarshalText([]byte("15s")); err != nil {
		t.Fatalf("UnmarshalText() error = %v", err)
	}

	if got, want := value.Duration(), 15*time.Second; got != want {
		t.Fatalf("Duration() = %s, want %s", got, want)
	}
}

func TestConnectionLifecycleOptionsValidate(t *testing.T) {
	tests := []struct {
		name    string
		value   ConnectionLifecycleOptions
		wantErr string
	}{
		{
			name: "valid",
			value: ConnectionLifecycleOptions{
				ConnectTimeout:     Duration(5 * time.Second),
				HealthCheckTimeout: Duration(time.Second),
				MaxOpenConns:       4,
				MaxIdleConns:       2,
				ConnMaxLifetime:    Duration(time.Minute),
				ConnMaxIdleTime:    Duration(30 * time.Second),
			},
		},
		{
			name: "negative timeout",
			value: ConnectionLifecycleOptions{
				ConnectTimeout: Duration(-time.Second),
			},
			wantErr: "connect_timeout must not be negative",
		},
		{
			name: "idle exceeds open",
			value: ConnectionLifecycleOptions{
				MaxOpenConns: 2,
				MaxIdleConns: 3,
			},
			wantErr: "max_idle_conns must be less than or equal to max_open_conns",
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
				t.Fatal("Validate() error = nil, want error")
			}

			if got := err.Error(); got != tt.wantErr {
				t.Fatalf("Validate() error = %q, want %q", got, tt.wantErr)
			}
		})
	}
}

func TestConnectionValidateLifecycleForSQLite(t *testing.T) {
	err := Connection{
		Type: "sqlite",
		SQLite: SQLiteConnectionOptions{
			Database: ":memory:",
		},
		Lifecycle: ConnectionLifecycleOptions{
			MaxOpenConns: 2,
		},
	}.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, want error")
	}

	if got, want := err.Error(), "lifecycle: max_open_conns must be 1 or lower for sqlite"; got != want {
		t.Fatalf("Validate() error = %q, want %q", got, want)
	}
}

func TestDurationString(t *testing.T) {
	value := Duration(2 * time.Second)
	if got, want := value.String(), "2s"; got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
}
