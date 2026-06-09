package db

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/adwinying/sqlcery/internal/config"
	mysqldriver "github.com/go-sql-driver/mysql"
)

var openMySQLDB = func(connConfig *mysqldriver.Config) (*sql.DB, error) {
	connector, err := mysqldriver.NewConnector(connConfig)
	if err != nil {
		return nil, err
	}

	return sql.OpenDB(connector), nil
}

func openMySQL(ctx context.Context, connection config.Connection, settings lifecycleSettings) (*SQLAdapter, error) {
	connConfig := mysqlConnConfigWithLifecycle(connection, config.ConnectionLifecycleOptions{
		ConnectTimeout: config.Duration(settings.ConnectTimeout),
	})

	closeResources := func() error { return nil }
	if connection.SSHHost != "" {
		tunnel, err := openSSHTunnel(ctx, connection.SSHHost)
		if err != nil {
			return nil, fmt.Errorf("configure ssh tunnel for mysql database %q on %s:%d: %w", connection.Database, connection.Host, connection.Port, err)
		}

		connConfig.DialFunc = tunnel.dialContext
		closeResources = tunnel.close
	}

	db, err := openMySQLDB(connConfig)
	if err != nil {
		_ = closeResources()
		return nil, wrapConnectionError("mysql", fmt.Sprintf("open mysql database %q on %s:%d", connection.Database, connection.Host, connection.Port), err)
	}

	closed := false
	defer func() {
		if !closed {
			_ = db.Close()
			_ = closeResources()
		}
	}()

	applyLifecycleSettings(db, settings)

	if err := pingDatabase(ctx, db, settings); err != nil {
		return nil, wrapConnectionError("mysql", fmt.Sprintf("ping mysql database %q on %s:%d", connection.Database, connection.Host, connection.Port), err)
	}

	adapter, err := newAdapter(
		sqlRunner{db: db},
		MySQLDialect(),
		mysqlMetadata{runner: sqlRunner{db: db}, database: connection.Database},
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

func mysqlConnConfig(connection config.Connection) *mysqldriver.Config {
	return mysqlConnConfigWithLifecycle(connection, config.ConnectionLifecycleOptions{})
}

func mysqlConnConfigWithLifecycle(connection config.Connection, lifecycle config.ConnectionLifecycleOptions) *mysqldriver.Config {
	connConfig := mysqldriver.NewConfig()
	connConfig.User = connection.Username
	connConfig.Passwd = connection.Password
	connConfig.Net = "tcp"
	connConfig.Addr = net.JoinHostPort(connection.Host, strconv.Itoa(connection.Port))
	connConfig.DBName = connection.Database
	if timeout := lifecycle.ConnectTimeout.Duration(); timeout > 0 {
		connConfig.Timeout = timeout
	}

	return connConfig
}

func mysqlDSN(connection config.Connection) string {
	return mysqlConnConfig(connection).FormatDSN()
}

type mysqlMetadata struct {
	runner   Runner
	database string
}

func (m mysqlMetadata) Tables(ctx context.Context, filter TableFilter) ([]Table, error) {
	namespace := strings.TrimSpace(filter.Namespace)
	if namespace == "" {
		namespace = strings.TrimSpace(m.database)
	}

	const query = "SELECT table_schema, table_name, table_type FROM information_schema.tables WHERE table_schema = ? AND table_type IN ('BASE TABLE', 'VIEW') ORDER BY table_name"

	rows, err := m.runner.QueryContext(ctx, query, namespace)
	if err != nil {
		return nil, fmt.Errorf("list mysql tables for namespace %q: %w", namespace, err)
	}
	defer rows.Close()

	tables := make([]Table, 0)
	for rows.Next() {
		var table Table
		var tableType string
		if err := rows.Scan(&table.Namespace, &table.Name, &tableType); err != nil {
			return nil, fmt.Errorf("scan mysql table metadata: %w", err)
		}

		table.Type = normalizeTableType(tableType)
		tables = append(tables, table)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate mysql tables for namespace %q: %w", namespace, err)
	}

	return tables, nil
}

func (m mysqlMetadata) Columns(ctx context.Context, table TableRef) ([]Column, error) {
	namespace := strings.TrimSpace(table.Namespace)
	if namespace == "" {
		namespace = strings.TrimSpace(m.database)
	}

	const query = "SELECT column_name, ordinal_position, column_type, is_nullable, column_default FROM information_schema.columns WHERE table_schema = ? AND table_name = ? ORDER BY ordinal_position"

	rows, err := m.runner.QueryContext(ctx, query, namespace, table.Name)
	if err != nil {
		return nil, fmt.Errorf("list mysql columns for %s: %w", MySQLDialect().QuoteIdentifier(namespace, table.Name), err)
	}
	defer rows.Close()

	columns := make([]Column, 0)
	for rows.Next() {
		var column Column
		var nullable string
		var defaultValue sql.NullString
		if err := rows.Scan(&column.Name, &column.Position, &column.Type, &nullable, &defaultValue); err != nil {
			return nil, fmt.Errorf("scan mysql column metadata: %w", err)
		}

		column.Nullable = strings.EqualFold(nullable, "YES")
		column.DefaultValue = nullStringPointer(defaultValue)
		columns = append(columns, column)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate mysql columns for %s: %w", MySQLDialect().QuoteIdentifier(namespace, table.Name), err)
	}

	return columns, nil
}

func (m mysqlMetadata) PrimaryKeys(ctx context.Context, table TableRef) ([]PrimaryKey, error) {
	namespace := strings.TrimSpace(table.Namespace)
	if namespace == "" {
		namespace = strings.TrimSpace(m.database)
	}

	const query = "SELECT constraint_name, column_name, ordinal_position FROM information_schema.key_column_usage WHERE table_schema = ? AND table_name = ? AND constraint_name = 'PRIMARY' ORDER BY ordinal_position"

	rows, err := m.runner.QueryContext(ctx, query, namespace, table.Name)
	if err != nil {
		return nil, fmt.Errorf("list mysql primary keys for %s: %w", MySQLDialect().QuoteIdentifier(namespace, table.Name), err)
	}
	defer rows.Close()

	primaryKeys := make([]PrimaryKey, 0)
	for rows.Next() {
		var key PrimaryKey
		if err := rows.Scan(&key.Name, &key.Column, &key.Position); err != nil {
			return nil, fmt.Errorf("scan mysql primary key metadata: %w", err)
		}

		primaryKeys = append(primaryKeys, key)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate mysql primary keys for %s: %w", MySQLDialect().QuoteIdentifier(namespace, table.Name), err)
	}

	return primaryKeys, nil
}

func (mysqlMetadata) Types(context.Context) ([]TypeInfo, error) {
	return cloneTypes(mysqlTypeInfo), nil
}

var mysqlTypeInfo = []TypeInfo{
	{Name: "bigint"},
	{Name: "binary"},
	{Name: "blob"},
	{Name: "boolean"},
	{Name: "char"},
	{Name: "date"},
	{Name: "datetime"},
	{Name: "decimal"},
	{Name: "double"},
	{Name: "enum"},
	{Name: "float"},
	{Name: "int"},
	{Name: "json"},
	{Name: "longtext"},
	{Name: "mediumint"},
	{Name: "text"},
	{Name: "time"},
	{Name: "timestamp"},
	{Name: "tinyint"},
	{Name: "varchar"},
}
