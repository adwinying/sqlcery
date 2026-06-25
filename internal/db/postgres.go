package db

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/adwinying/sqlcery/internal/config"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

var openPostgresDB = func(connConfig pgx.ConnConfig) *sql.DB {
	return stdlib.OpenDB(connConfig)
}

func openPostgres(ctx context.Context, connection config.Connection, settings lifecycleSettings) (*SQLAdapter, error) {
	connConfig, err := postgresConnConfigWithLifecycle(connection, config.ConnectionLifecycleOptions{
		ConnectTimeout: config.Duration(settings.ConnectTimeout),
	})
	if err != nil {
		return nil, err
	}

	closeResources := func() error { return nil }
	if connection.SSHHost != "" {
		tunnel, err := openSSHTunnel(ctx, connection.SSHHost)
		if err != nil {
			return nil, fmt.Errorf("configure ssh tunnel for postgres database %q on %s:%d: %w", connection.Database, connection.Host, connection.Port, err)
		}

		connConfig.DialFunc = tunnel.dialContext
		closeResources = tunnel.close
	}

	db := openPostgresDB(*connConfig)
	closed := false
	defer func() {
		if !closed {
			_ = db.Close()
			_ = closeResources()
		}
	}()

	applyLifecycleSettings(db, settings)

	if err := pingDatabase(ctx, db, settings); err != nil {
		return nil, wrapConnectionError("postgres", fmt.Sprintf("ping postgres database %q on %s:%d", connection.Database, connection.Host, connection.Port), err)
	}

	adapter, err := newAdapter(
		sqlRunner{db: db},
		PostgresDialect(),
		postgresMetadata{runner: sqlRunner{db: db}},
		wrapPingWithTimeout(db.PingContext, settings.HealthCheckTimeout),
		func() error {
			closeErr := db.Close()
			if err := closeResources(); err != nil && closeErr == nil {
				closeErr = err
			}
			return closeErr
		},
	)
	if err != nil {
		return nil, err
	}

	closed = true
	return adapter, nil
}

func postgresConnConfigWithLifecycle(connection config.Connection, lifecycle config.ConnectionLifecycleOptions) (*pgx.ConnConfig, error) {
	connConfig, err := pgx.ParseConfig(postgresConnectionString(connection))
	if err != nil {
		return nil, fmt.Errorf("parse postgres connection config for database %q on %s:%d: %w", connection.Database, connection.Host, connection.Port, err)
	}

	connConfig.ConnectTimeout = resolveLifecycleSettings("postgres", lifecycle).ConnectTimeout

	return connConfig, nil
}

func postgresConnectionString(connection config.Connection) string {
	connectionURL := &url.URL{
		Scheme: "postgres",
		Host:   net.JoinHostPort(connection.Host, strconv.Itoa(connection.Port)),
		Path:   "/" + connection.Database,
	}

	if connection.Password == "" {
		connectionURL.User = url.User(connection.Username)
	} else {
		connectionURL.User = url.UserPassword(connection.Username, connection.Password)
	}

	return connectionURL.String()
}

type postgresMetadata struct {
	runner Runner
}

func (m postgresMetadata) Tables(ctx context.Context, filter TableFilter) ([]Table, error) {
	query := strings.Builder{}
	query.WriteString("SELECT table_catalog, table_schema, table_name, table_type FROM information_schema.tables WHERE table_type IN ('BASE TABLE', 'VIEW')")

	args := make([]any, 0, 2)
	if namespace := strings.TrimSpace(filter.Namespace); namespace != "" {
		args = append(args, namespace)
		query.WriteString(" AND table_schema = $")
		query.WriteString(strconv.Itoa(len(args)))
	} else {
		query.WriteString(" AND table_schema NOT IN ('information_schema', 'pg_catalog')")
	}

	if catalog := strings.TrimSpace(filter.Catalog); catalog != "" {
		args = append(args, catalog)
		query.WriteString(" AND table_catalog = $")
		query.WriteString(strconv.Itoa(len(args)))
	}

	query.WriteString(" ORDER BY table_schema, table_name")

	rows, err := m.runner.QueryContext(ctx, query.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("list postgres tables: %w", err)
	}
	defer rows.Close()

	tables := make([]Table, 0)
	for rows.Next() {
		var table Table
		var tableType string
		if err := rows.Scan(&table.Catalog, &table.Namespace, &table.Name, &tableType); err != nil {
			return nil, fmt.Errorf("scan postgres table metadata: %w", err)
		}

		table.Type = normalizeTableType(tableType)
		tables = append(tables, table)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate postgres tables: %w", err)
	}

	return tables, nil
}

func (m postgresMetadata) Columns(ctx context.Context, table TableRef) ([]Column, error) {
	namespace := strings.TrimSpace(table.Namespace)
	if namespace == "" {
		namespace = "public"
	}

	const query = "SELECT a.attname, a.attnum, pg_catalog.format_type(a.atttypid, a.atttypmod), NOT a.attnotnull, pg_catalog.pg_get_expr(ad.adbin, ad.adrelid) FROM pg_catalog.pg_attribute AS a JOIN pg_catalog.pg_class AS c ON c.oid = a.attrelid JOIN pg_catalog.pg_namespace AS n ON n.oid = c.relnamespace LEFT JOIN pg_catalog.pg_attrdef AS ad ON ad.adrelid = a.attrelid AND ad.adnum = a.attnum WHERE n.nspname = $1 AND c.relname = $2 AND a.attnum > 0 AND NOT a.attisdropped ORDER BY a.attnum"

	rows, err := m.runner.QueryContext(ctx, query, namespace, table.Name)
	if err != nil {
		return nil, fmt.Errorf("list postgres columns for %s: %w", PostgresDialect().QuoteIdentifier(namespace, table.Name), err)
	}
	defer rows.Close()

	columns := make([]Column, 0)
	for rows.Next() {
		var column Column
		var defaultValue sql.NullString
		if err := rows.Scan(&column.Name, &column.Position, &column.Type, &column.Nullable, &defaultValue); err != nil {
			return nil, fmt.Errorf("scan postgres column metadata: %w", err)
		}

		column.DefaultValue = nullStringPointer(defaultValue)
		columns = append(columns, column)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate postgres columns for %s: %w", PostgresDialect().QuoteIdentifier(namespace, table.Name), err)
	}

	return columns, nil
}

func (m postgresMetadata) PrimaryKeys(ctx context.Context, table TableRef) ([]PrimaryKey, error) {
	namespace := strings.TrimSpace(table.Namespace)
	if namespace == "" {
		namespace = "public"
	}

	const query = "SELECT tc.constraint_name, kcu.column_name, kcu.ordinal_position FROM information_schema.table_constraints AS tc JOIN information_schema.key_column_usage AS kcu ON tc.constraint_catalog = kcu.constraint_catalog AND tc.constraint_schema = kcu.constraint_schema AND tc.constraint_name = kcu.constraint_name WHERE tc.constraint_type = 'PRIMARY KEY' AND tc.table_schema = $1 AND tc.table_name = $2 ORDER BY kcu.ordinal_position"

	rows, err := m.runner.QueryContext(ctx, query, namespace, table.Name)
	if err != nil {
		return nil, fmt.Errorf("list postgres primary keys for %s: %w", PostgresDialect().QuoteIdentifier(namespace, table.Name), err)
	}
	defer rows.Close()

	primaryKeys := make([]PrimaryKey, 0)
	for rows.Next() {
		var key PrimaryKey
		if err := rows.Scan(&key.Name, &key.Column, &key.Position); err != nil {
			return nil, fmt.Errorf("scan postgres primary key metadata: %w", err)
		}

		primaryKeys = append(primaryKeys, key)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate postgres primary keys for %s: %w", PostgresDialect().QuoteIdentifier(namespace, table.Name), err)
	}

	return primaryKeys, nil
}

func (postgresMetadata) Types(context.Context) ([]TypeInfo, error) {
	return cloneTypes(postgresTypeInfo), nil
}

var postgresTypeInfo = []TypeInfo{
	{Namespace: "pg_catalog", Name: "bigint"},
	{Namespace: "pg_catalog", Name: "boolean"},
	{Namespace: "pg_catalog", Name: "bytea"},
	{Namespace: "pg_catalog", Name: "date"},
	{Namespace: "pg_catalog", Name: "double precision"},
	{Namespace: "pg_catalog", Name: "integer"},
	{Namespace: "pg_catalog", Name: "jsonb"},
	{Namespace: "pg_catalog", Name: "numeric"},
	{Namespace: "pg_catalog", Name: "real"},
	{Namespace: "pg_catalog", Name: "smallint"},
	{Namespace: "pg_catalog", Name: "text"},
	{Namespace: "pg_catalog", Name: "time"},
	{Namespace: "pg_catalog", Name: "timestamp"},
	{Namespace: "pg_catalog", Name: "timestamptz"},
	{Namespace: "pg_catalog", Name: "uuid"},
	{Namespace: "pg_catalog", Name: "varchar"},
}
