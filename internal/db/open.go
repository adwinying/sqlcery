package db

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/adwinying/sqlcery/internal/config"
	_ "modernc.org/sqlite"
)

const sqliteDriverName = "sqlite"

func Open(ctx context.Context, connection config.Connection) (*SQLAdapter, error) {
	if err := connection.Validate(); err != nil {
		return nil, err
	}

	settings := resolveLifecycleSettings(connection.Type, connection.Lifecycle)

	switch connection.Type {
	case "sqlite":
		return openSQLite(ctx, connection, settings)
	case "postgres":
		return openPostgres(ctx, connection, settings)
	case "mysql":
		return openMySQL(ctx, connection, settings)
	default:
		return nil, fmt.Errorf("opening %s connections is not implemented", connection.Type)
	}
}

// expandHomePath expands a leading ~ to the current user's home directory.
func expandHomePath(path string) (string, error) {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}

	if path == "~" {
		return home, nil
	}

	return filepath.Join(home, path[2:]), nil
}

func openSQLite(ctx context.Context, connection config.Connection, settings lifecycleSettings) (*SQLAdapter, error) {
	databasePath, err := expandHomePath(connection.Database)
	if err != nil {
		return nil, fmt.Errorf("expand sqlite database path %q: %w", connection.Database, err)
	}

	db, err := sql.Open(sqliteDriverName, databasePath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database %q: %w", connection.Database, err)
	}

	closed := false
	defer func() {
		if !closed {
			_ = db.Close()
		}
	}()

	applyLifecycleSettings(db, settings)

	if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys = ON"); err != nil {
		return nil, fmt.Errorf("configure sqlite database %q: %w", connection.Database, err)
	}

	if err := pingDatabase(ctx, db, settings); err != nil {
		return nil, fmt.Errorf("ping sqlite database %q: %w", connection.Database, err)
	}

	adapter, err := newAdapter(
		sqlRunner{db: db},
		SQLiteDialect(),
		sqliteMetadata{runner: sqlRunner{db: db}},
		wrapPingWithTimeout(db.PingContext, settings.HealthCheckTimeout),
		db.Close,
	)
	if err != nil {
		return nil, err
	}

	closed = true
	return adapter, nil
}
