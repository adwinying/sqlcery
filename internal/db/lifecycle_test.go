package db

import (
	"context"
	"testing"
	"time"

	"github.com/adwinying/sqlcery/internal/config"
)

func TestResolveLifecycleSettingsDefaults(t *testing.T) {
	postgres := resolveLifecycleSettings("postgres", config.ConnectionLifecycleOptions{})
	if got, want := postgres.ConnectTimeout, defaultConnectTimeout; got != want {
		t.Fatalf("postgres.ConnectTimeout = %s, want %s", got, want)
	}
	if got, want := postgres.HealthCheckTimeout, defaultHealthCheckTimeout; got != want {
		t.Fatalf("postgres.HealthCheckTimeout = %s, want %s", got, want)
	}
	if got, want := postgres.MaxOpenConns, defaultMaxOpenConns; got != want {
		t.Fatalf("postgres.MaxOpenConns = %d, want %d", got, want)
	}
	if got, want := postgres.MaxIdleConns, defaultMaxIdleConns; got != want {
		t.Fatalf("postgres.MaxIdleConns = %d, want %d", got, want)
	}
	if got, want := postgres.ConnMaxLifetime, defaultConnMaxLifetime; got != want {
		t.Fatalf("postgres.ConnMaxLifetime = %s, want %s", got, want)
	}
	if got, want := postgres.ConnMaxIdleTime, defaultConnMaxIdleTime; got != want {
		t.Fatalf("postgres.ConnMaxIdleTime = %s, want %s", got, want)
	}

	sqlite := resolveLifecycleSettings("sqlite", config.ConnectionLifecycleOptions{})
	if got, want := sqlite.MaxOpenConns, 1; got != want {
		t.Fatalf("sqlite.MaxOpenConns = %d, want %d", got, want)
	}
	if got, want := sqlite.MaxIdleConns, 1; got != want {
		t.Fatalf("sqlite.MaxIdleConns = %d, want %d", got, want)
	}
	if got := sqlite.ConnMaxLifetime; got != 0 {
		t.Fatalf("sqlite.ConnMaxLifetime = %s, want 0s", got)
	}
	if got := sqlite.ConnMaxIdleTime; got != 0 {
		t.Fatalf("sqlite.ConnMaxIdleTime = %s, want 0s", got)
	}
}

func TestResolveLifecycleSettingsUsesOverrides(t *testing.T) {
	settings := resolveLifecycleSettings("mysql", config.ConnectionLifecycleOptions{
		ConnectTimeout:     config.Duration(9 * time.Second),
		HealthCheckTimeout: config.Duration(3 * time.Second),
		MaxOpenConns:       20,
		MaxIdleConns:       7,
		ConnMaxLifetime:    config.Duration(30 * time.Minute),
		ConnMaxIdleTime:    config.Duration(4 * time.Minute),
	})

	if got, want := settings.ConnectTimeout, 9*time.Second; got != want {
		t.Fatalf("ConnectTimeout = %s, want %s", got, want)
	}
	if got, want := settings.HealthCheckTimeout, 3*time.Second; got != want {
		t.Fatalf("HealthCheckTimeout = %s, want %s", got, want)
	}
	if got, want := settings.MaxOpenConns, 20; got != want {
		t.Fatalf("MaxOpenConns = %d, want %d", got, want)
	}
	if got, want := settings.MaxIdleConns, 7; got != want {
		t.Fatalf("MaxIdleConns = %d, want %d", got, want)
	}
	if got, want := settings.ConnMaxLifetime, 30*time.Minute; got != want {
		t.Fatalf("ConnMaxLifetime = %s, want %s", got, want)
	}
	if got, want := settings.ConnMaxIdleTime, 4*time.Minute; got != want {
		t.Fatalf("ConnMaxIdleTime = %s, want %s", got, want)
	}
}

func TestApplyLifecycleSettings(t *testing.T) {
	recorder := &lifecycleRecorder{}
	applyLifecycleSettings(recorder, lifecycleSettings{
		MaxOpenConns:    12,
		MaxIdleConns:    4,
		ConnMaxLifetime: 15 * time.Minute,
		ConnMaxIdleTime: 2 * time.Minute,
	})

	if got, want := recorder.maxOpenConns, 12; got != want {
		t.Fatalf("maxOpenConns = %d, want %d", got, want)
	}
	if got, want := recorder.maxIdleConns, 4; got != want {
		t.Fatalf("maxIdleConns = %d, want %d", got, want)
	}
	if got, want := recorder.connMaxLifetime, 15*time.Minute; got != want {
		t.Fatalf("connMaxLifetime = %s, want %s", got, want)
	}
	if got, want := recorder.connMaxIdleTime, 2*time.Minute; got != want {
		t.Fatalf("connMaxIdleTime = %s, want %s", got, want)
	}
}

func TestWrapPingWithTimeoutAppliesDeadline(t *testing.T) {
	var hasDeadline bool
	var until time.Duration

	healthCheck := wrapPingWithTimeout(func(ctx context.Context) error {
		deadline, ok := ctx.Deadline()
		hasDeadline = ok
		if ok {
			until = time.Until(deadline)
		}
		return nil
	}, 50*time.Millisecond)

	if err := healthCheck(context.Background()); err != nil {
		t.Fatalf("healthCheck() error = %v", err)
	}

	if !hasDeadline {
		t.Fatal("healthCheck() context missing deadline")
	}

	if until <= 0 || until > 50*time.Millisecond {
		t.Fatalf("healthCheck() deadline window = %s, want between 0 and 50ms", until)
	}
}

type lifecycleRecorder struct {
	maxOpenConns    int
	maxIdleConns    int
	connMaxLifetime time.Duration
	connMaxIdleTime time.Duration
}

func (r *lifecycleRecorder) SetMaxOpenConns(n int) {
	r.maxOpenConns = n
}

func (r *lifecycleRecorder) SetMaxIdleConns(n int) {
	r.maxIdleConns = n
}

func (r *lifecycleRecorder) SetConnMaxLifetime(d time.Duration) {
	r.connMaxLifetime = d
}

func (r *lifecycleRecorder) SetConnMaxIdleTime(d time.Duration) {
	r.connMaxIdleTime = d
}
