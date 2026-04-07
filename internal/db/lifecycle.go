package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/adwinying/sqlcery/internal/config"
)

const (
	defaultConnectTimeout     = 5 * time.Second
	defaultHealthCheckTimeout = 2 * time.Second
	defaultMaxOpenConns       = 10
	defaultMaxIdleConns       = 5
	defaultConnMaxLifetime    = time.Hour
	defaultConnMaxIdleTime    = 5 * time.Minute
)

type lifecycleSettings struct {
	ConnectTimeout     time.Duration
	HealthCheckTimeout time.Duration
	MaxOpenConns       int
	MaxIdleConns       int
	ConnMaxLifetime    time.Duration
	ConnMaxIdleTime    time.Duration
}

type lifecycleConfigurer interface {
	SetMaxOpenConns(n int)
	SetMaxIdleConns(n int)
	SetConnMaxLifetime(d time.Duration)
	SetConnMaxIdleTime(d time.Duration)
}

func resolveLifecycleSettings(connectionType string, options config.ConnectionLifecycleOptions) lifecycleSettings {
	settings := lifecycleSettings{
		ConnectTimeout:     durationOrDefault(options.ConnectTimeout.Duration(), defaultConnectTimeout),
		HealthCheckTimeout: durationOrDefault(options.HealthCheckTimeout.Duration(), defaultHealthCheckTimeout),
		ConnMaxLifetime:    options.ConnMaxLifetime.Duration(),
		ConnMaxIdleTime:    options.ConnMaxIdleTime.Duration(),
	}

	switch connectionType {
	case "sqlite":
		settings.MaxOpenConns = 1
		settings.MaxIdleConns = 1
	default:
		settings.MaxOpenConns = intOrDefault(options.MaxOpenConns, defaultMaxOpenConns)
		settings.MaxIdleConns = intOrDefault(options.MaxIdleConns, defaultMaxIdleConns)
		settings.ConnMaxLifetime = durationOrDefault(options.ConnMaxLifetime.Duration(), defaultConnMaxLifetime)
		settings.ConnMaxIdleTime = durationOrDefault(options.ConnMaxIdleTime.Duration(), defaultConnMaxIdleTime)
	}

	if connectionType == "sqlite" {
		if options.MaxOpenConns > 0 {
			settings.MaxOpenConns = options.MaxOpenConns
		}
		if options.MaxIdleConns > 0 {
			settings.MaxIdleConns = options.MaxIdleConns
		}
	}

	if settings.MaxOpenConns > 0 && settings.MaxIdleConns > settings.MaxOpenConns {
		settings.MaxIdleConns = settings.MaxOpenConns
	}

	return settings
}

func applyLifecycleSettings(target lifecycleConfigurer, settings lifecycleSettings) {
	target.SetMaxOpenConns(settings.MaxOpenConns)
	target.SetMaxIdleConns(settings.MaxIdleConns)
	target.SetConnMaxLifetime(settings.ConnMaxLifetime)
	target.SetConnMaxIdleTime(settings.ConnMaxIdleTime)
}

func wrapPingWithTimeout(ping func(context.Context) error, timeout time.Duration) func(context.Context) error {
	if ping == nil {
		return nil
	}

	return func(ctx context.Context) error {
		healthCtx, cancel := contextWithTimeout(ctx, timeout)
		defer cancel()
		return ping(healthCtx)
	}
}

func pingDatabase(ctx context.Context, db *sql.DB, settings lifecycleSettings) error {
	pingCtx, cancel := contextWithTimeout(ctx, settings.ConnectTimeout)
	defer cancel()
	return db.PingContext(pingCtx)
}

func contextWithTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return context.WithCancel(ctx)
	}

	return context.WithTimeout(ctx, timeout)
}

func durationOrDefault(value time.Duration, fallback time.Duration) time.Duration {
	if value > 0 {
		return value
	}

	return fallback
}

func intOrDefault(value int, fallback int) int {
	if value > 0 {
		return value
	}

	return fallback
}

func healthCheckError(dialect string, err error) error {
	if err == nil {
		return nil
	}

	return fmt.Errorf("health check %s connection: %w", dialect, err)
}
